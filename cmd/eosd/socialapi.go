package main

// Among Us's friends API on top of the account core. One socialCall per request: `me` is the caller's
// core account as eosd knows it (nil when the PUID has no Switch link or the core is unreachable).

import (
	"context"
	"encoding/json"
	"log"
	"strings"
)

type socialCall struct {
	s    *server
	ctx  context.Context
	me   *acctRec
	puid string
}

// ensureMe maps the caller's PUID to a core account (PUID -> BAAS user id from the Connect login, BAAS
// -> account through the core's link) and remembers it, with the derived friend code.
func (s *server) ensureMe(ctx context.Context, puid string) *acctRec {
	if r := s.acct.get(ctx, puid); r != nil {
		return r
	}
	if s.core == nil {
		return nil
	}
	baas, ok := s.owner(kindPUID, puid)
	if !ok {
		return nil
	}
	acc, err := s.core.AccountOf(ctx, baas)
	if err != nil {
		log.Printf("[innersloth] core account lookup failed: %v", err)
		return nil
	}
	r := &acctRec{PUID: puid, Account: acc, Baas: baas, Code: strings.ToLower(fullCode(puid))}
	s.acct.put(ctx, r)
	return r
}

func notFound() (int, any) {
	return 404, map[string]any{"errors": []any{map[string]any{"code": "404", "title": "Not Found", "detail": "no player with that friend code"}}}
}

func unavailable() (int, any) {
	return 503, map[string]any{"errors": []any{map[string]any{"code": "503", "title": "Friends unavailable"}}}
}

// puids turns core account ids into the PUIDs of those who play Among Us here; others are skipped.
func (g *socialCall) puids(accounts []string) []string {
	var out []string
	for _, a := range accounts {
		if r := g.s.acct.getByAccount(g.ctx, a); r != nil {
			out = append(out, r.PUID)
		}
	}
	return out
}

func (g *socialCall) list(f func(friendsCore, context.Context, string) ([]string, error)) []string {
	if g.me == nil || g.s.core == nil {
		return nil
	}
	ids, err := f(g.s.core, g.ctx, g.me.Account)
	if err != nil {
		log.Printf("[innersloth] core list failed: %v", err)
		return nil
	}
	return g.puids(ids)
}

func (g *socialCall) friendsDoc() map[string]any {
	friends := []any{}
	for _, p := range g.list(friendsCore.Friends) {
		friends = append(friends, map[string]any{"friend_puid": p, "friend_friendcode": fullCode(p)})
	}
	return map[string]any{"friends": friends}
}

func (g *socialCall) requestsDoc() map[string]any {
	senders := []any{}
	for _, p := range g.list(friendsCore.Incoming) {
		senders = append(senders, map[string]any{"sender_puid": p, "sender_friendcode": fullCode(p)})
	}
	return map[string]any{"senders": senders}
}

func (g *socialCall) blocksDoc() map[string]any {
	// ResponseBlockedWrapper: blocking holds {recipient_puid, recipient_friendcode}. The core never says
	// who blocked you, so blocked_by (sender_puid, sender_friendcode) stays empty.
	blocking := []any{}
	for _, p := range g.list(friendsCore.Blocks) {
		blocking = append(blocking, map[string]any{"recipient_puid": p, "recipient_friendcode": fullCode(p)})
	}
	return map[string]any{"blocking": blocking, "blocked_by": []any{}}
}

func (g *socialCall) other(puid string) *acctRec {
	if puid == "" {
		return nil
	}
	return g.s.acct.get(g.ctx, puid)
}

func (g *socialCall) requestByCode(body []byte) (int, any) {
	code := firstNonEmpty(attr(body, "recipient_friendcode"), attr(body, "username"))
	r := g.s.acct.getByCode(g.ctx, code)
	if r == nil {
		return notFound()
	}
	return g.request(r.PUID)
}

func (g *socialCall) request(to string) (int, any) {
	if to == "" || to == g.puid {
		return 400, map[string]any{"errors": []any{map[string]any{"code": "400", "title": "Bad Request"}}}
	}
	if g.me == nil || g.s.core == nil {
		return unavailable()
	}
	o := g.other(to)
	if o == nil {
		return notFound()
	}
	if err := g.s.core.Request(g.ctx, g.me.Account, o.Account); err != nil {
		// They had already asked us (or are friends): that makes us friends.
		if err2 := g.s.core.Accept(g.ctx, g.me.Account, o.Account); err2 != nil {
			log.Printf("[innersloth] friend request failed: %v", err)
			return unavailable()
		}
	}
	return 200, data("friend_request", map[string]any{"recipient_puid": to, "recipient_friendcode": fullCode(to)})
}

// act runs one core call against another player; failures are logged, the game gets its usual 200.
func (g *socialCall) act(puid string, f func(friendsCore, string, string) error) {
	o := g.other(puid)
	if g.me == nil || g.s.core == nil || o == nil {
		return
	}
	if err := f(g.s.core, g.me.Account, o.Account); err != nil {
		log.Printf("[innersloth] core call failed: %v", err)
	}
}

func (g *socialCall) accept(sender string) {
	g.act(sender, func(c friendsCore, me, o string) error { return c.Accept(g.ctx, me, o) })
}
func (g *socialCall) deny(sender string) {
	g.act(sender, func(c friendsCore, me, o string) error { return c.Remove(g.ctx, me, o) })
}
func (g *socialCall) unfriend(other string) { g.deny(other) }
func (g *socialCall) block(other string) {
	g.act(other, func(c friendsCore, me, o string) error { return c.Block(g.ctx, me, o) })
}
func (g *socialCall) unblock(other string) {
	g.act(other, func(c friendsCore, me, o string) error { return c.Unblock(g.ctx, me, o) })
}

// forget drops what the core holds about the player's Among Us social life (user/accounts/deletion):
// friendships, requests and blocks. The account itself is the core's to delete.
func (g *socialCall) forget() {
	if g.me == nil || g.s.core == nil {
		return
	}
	c := g.s.core
	for _, l := range []func(friendsCore, context.Context, string) ([]string, error){friendsCore.Friends, friendsCore.Incoming, friendsCore.Outgoing} {
		if ids, err := l(c, g.ctx, g.me.Account); err == nil {
			for _, id := range ids {
				_ = c.Remove(g.ctx, g.me.Account, id)
			}
		}
	}
	if ids, err := c.Blocks(g.ctx, g.me.Account); err == nil {
		for _, id := range ids {
			_ = c.Unblock(g.ctx, g.me.Account, id)
		}
	}
	g.s.acct.dropInvites(g.ctx, g.puid, "")
	g.s.acct.dropFrom(g.ctx, g.puid)
}

func (g *socialCall) isDeleting() bool { return g.me != nil && g.me.Deleting }

func (g *socialCall) setDeleting(on bool) {
	if g.me != nil {
		g.s.acct.setDeleting(g.ctx, g.puid, on)
		c := *g.me
		c.Deleting = on
		g.me = &c
	}
}

func (g *socialCall) invitesDoc() map[string]any {
	inv := []any{}
	for _, i := range g.s.acct.invitesFor(g.ctx, g.puid) {
		inv = append(inv, map[string]any{"sender_puid": i.Sender, "recipient_puid": i.Recipient, "room_code": i.Room,
			"sender_friendcode": fullCode(i.Sender), "recipient_friendcode": fullCode(i.Recipient)})
	}
	return map[string]any{"next_poll_seconds": 15, "invites": inv}
}

func (g *socialCall) invite(to string, room json.RawMessage) {
	if to == "" || string(room) == "null" || string(room) == `""` {
		return
	}
	g.s.acct.addInvite(g.ctx, lobbyInvite{Sender: g.puid, Recipient: to, Room: room})
}

func (g *socialCall) dropInvite(sender string) { g.s.acct.dropInvites(g.ctx, g.puid, sender) }
