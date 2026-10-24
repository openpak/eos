package main

// backend.innersloth.com: Among Us's own account backend, which the game calls right after its
// EOS Connect login and before it lets the player online ("Could not connect to your Among Us
// Account. (Error FriendCodeFail)" when it is not answered, 2026-10-10 on Harbor).
//
// It lives here rather than in platforms/switch/games/among-us because its whole identity is the EOS one:
// every request carries the EOS id_token eosd minted, so eosd checks it with its own key and
// needs nobody's JWKS. Impostor (platforms/switch/games/among-us) stays the matchmaker and game server.
//
// The paths are the game's (https://backend.innersloth.com/api/ + user/...), the JSON names its
// [JsonProperty] names, JSON:API style: {"data": {"type": ..., "attributes": {...}}}. What the
// account is:
//
//   - The friend code is username#discriminator. Both are derived from the PUID, so a player
//     keeps one friend code for good with nothing stored: two words, four digits.
//   - Friends, friend requests, blocks and lobby invites are kept in memory. Losing them on a
//     restart loses a friends list, never a sign-in.

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"openpak/eos/redact"
)

// Short words, so any two fit the game's name length (10).
var (
	nameFirst = []string{"brave", "calm", "cosmic", "crisp", "fuzzy", "happy", "jolly", "lucky", "mellow",
		"misty", "nifty", "plucky", "quick", "rapid", "rosy", "shiny", "silly", "sunny", "swift", "tidy",
		"witty", "zesty", "bold", "cozy", "lunar", "polar", "sly", "spry", "keen", "mild"}
	nameSecond = []string{"bean", "comet", "crab", "duck", "fern", "fox", "frog", "gecko", "kiwi", "lark",
		"lime", "moth", "newt", "otter", "owl", "panda", "pear", "plum", "pug", "seal", "slug", "squid",
		"toad", "vole", "wasp", "wren", "yak", "moon", "star", "orb"}
)

// friendCode returns the PUID's username and discriminator.
func friendCode(puid string) (string, string) {
	sum := sha256.Sum256([]byte("friendcode\x00" + puid))
	a := binary.BigEndian.Uint32(sum[0:4])
	b := binary.BigEndian.Uint32(sum[4:8])
	c := binary.BigEndian.Uint32(sum[8:12])
	name := nameFirst[a%uint32(len(nameFirst))] + nameSecond[b%uint32(len(nameSecond))]
	if len(name) > 10 {
		name = name[:10]
	}
	return name, fmt.Sprintf("%04d", c%10000)
}

func fullCode(puid string) string {
	n, d := friendCode(puid)
	return n + "#" + d
}

// social is the in-memory friends graph.
type social struct {
	mu       sync.Mutex
	byCode   map[string]string          // friend code -> PUID, for everyone seen since boot
	friends  map[string]map[string]bool // PUID -> friend PUIDs
	requests map[string]map[string]bool // recipient -> senders
	blocks   map[string]map[string]bool // blocker -> blocked
	invites  map[string][]lobbyInvite   // recipient -> invites
	deleting map[string]bool            // PUIDs with a pending account deletion
}

func (g *social) isDeleting(puid string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.deleting[puid]
}

func (g *social) setDeleting(puid string, on bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if on {
		g.deleting[puid] = true
	} else {
		delete(g.deleting, puid)
	}
}

type lobbyInvite struct {
	Sender, Recipient string
	Room              json.RawMessage // room_code exactly as the sender's client wrote it
}

func newSocial() *social {
	return &social{byCode: map[string]string{}, friends: map[string]map[string]bool{},
		requests: map[string]map[string]bool{}, blocks: map[string]map[string]bool{},
		invites: map[string][]lobbyInvite{}, deleting: map[string]bool{}}
}

func addTo(m map[string]map[string]bool, k, v string) {
	if m[k] == nil {
		m[k] = map[string]bool{}
	}
	m[k][v] = true
}

func (s *server) innersloth(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	tok, err := bearer(r.Header.Get("Authorization"))
	var puid string
	if err == nil {
		if c, err := s.signer.verify(tok); err == nil {
			puid = str(c, "sub")
		}
	}
	if puid == "" {
		log.Printf("[innersloth] %s %s -> 401 (no EOS token of ours; headers=%s)", r.Method, logPath(r.URL.Path), headerNames(r.Header))
		writeJSON(w, 401, map[string]any{"errors": []any{map[string]any{"code": "401", "title": "Unauthorized"}}})
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	g := s.social
	g.mu.Lock()
	g.byCode[strings.ToLower(fullCode(puid))] = puid
	g.mu.Unlock()

	status := 200
	var out any
	q := r.URL.Query()
	switch {
	case path == "user/username" && r.Method == http.MethodGet:
		out = s.usernameDoc(puid)
	case path == "user/username":
		// Renaming: the friend code is derived, so the name stays; answer with it.
		out = s.usernameDoc(puid)
	case path == "user/friends" && r.Method == http.MethodGet:
		out = data("friends", g.friendsDoc(puid))
	case path == "user/friends" && r.Method == http.MethodDelete:
		g.unfriend(puid, q.Get("friend_puid"))
		out = data("friends", map[string]any{})
	case path == "user/friends/received-requests":
		out = data("friend_requests", g.requestsDoc(puid))
	case path == "user/friends/request-by-friendcode" && r.Method == http.MethodPost:
		status, out = g.requestByCode(puid, body)
	case path == "user/friends/request" && r.Method == http.MethodPost:
		status, out = g.request(puid, attr(body, "recipient_puid"))
	case path == "user/friends/request" && r.Method == http.MethodPut:
		g.accept(puid, firstNonEmpty(q.Get("sender_puid"), attr(body, "sender_puid")))
		out = data("friend_request", map[string]any{})
	case path == "user/friends/request" && r.Method == http.MethodDelete:
		g.deny(puid, firstNonEmpty(q.Get("sender_puid"), attr(body, "sender_puid")))
		out = data("friend_request", map[string]any{})
	case path == "user/blocks" && r.Method == http.MethodGet:
		out = data("blocks", g.blocksDoc(puid))
	case path == "user/blocks" && r.Method == http.MethodPost:
		g.block(puid, attr(body, "recipient_puid"))
		out = data("blocks", g.blocksDoc(puid))
	case path == "user/blocks" && r.Method == http.MethodDelete:
		g.unblock(puid, q.Get("recipient_puid"))
		out = data("blocks", g.blocksDoc(puid))
	case path == "user/lobby/invites" && r.Method == http.MethodGet:
		out = data("lobby_invites", g.invitesDoc(puid))
	case path == "user/lobby/invites" && r.Method == http.MethodPost:
		g.invite(puid, attr(body, "recipient_puid"), attrRaw(body, "room_code"))
		out = data("lobby_invites", map[string]any{})
	case path == "user/lobby/invites" && r.Method == http.MethodDelete:
		g.dropInvite(puid, q.Get("sender_puid"))
		out = data("lobby_invites", map[string]any{})
	case path == "user/report-player" && r.Method == http.MethodPost:
		// PlayerReportRequest {game_server_id, game_id, reportee_client_id, reason_id}. Moderation
		// is not built; the reason is a fixed vocabulary (InnerNet.ReportReasons), safe to log.
		log.Printf("[innersloth] report-player reason=%s", attr(body, "reason_id"))
		out = data("report", map[string]any{})
	case path == "user/accounts/deletion" && r.Method == http.MethodGet:
		// The deletion STATUS: the client reads any 200 as "Account Deletion Pending" (measured
		// 2026-10-24, Harbor), so a player with no request gets 404.
		if !g.isDeleting(puid) {
			status = 404
			out = map[string]any{"errors": []any{map[string]any{"code": "404", "title": "Not Found"}}}
			break
		}
		out = data("account_deletion", map[string]any{})
	case path == "user/accounts/deletion" && r.Method == http.MethodDelete:
		// Cancelling a pending deletion.
		g.setDeleting(puid, false)
		out = data("account_deletion", map[string]any{})
	case path == "user/accounts/deletion":
		g.forget(puid)
		g.setDeleting(puid, true)
		out = data("account_deletion", map[string]any{})
	case path == "announcements" && r.Method == http.MethodGet:
		// AnnouncementList {announcements, page, pages}. No in-game news on OpenPak yet.
		out = data("announcements", map[string]any{"announcements": []any{}, "page": 1, "pages": 1})
	case path == "user/redeem-currency" && r.Method == http.MethodPost:
		// RedeemCurrency {item_id, currency_type, platform} -> ResponsePurchase. No wallet is kept
		// here yet: the item is granted at no cost.
		out = data("purchase", map[string]any{"item_id": attrRaw(body, "item_id"),
			"currency_type": attrRaw(body, "currency_type"), "platform": attrRaw(body, "platform"), "item_cost": 0})
	case (path == "user/validate-receipt" || path == "user/validate-receipt-batch") && r.Method == http.MethodPost:
		kind := receiptKind(body)
		log.Printf("[innersloth] %s entitlement_type=%s kind=%s body=%s", path, attr(body, "entitlement_type"), kind,
			bodyShape(r.Header.Get("Content-Type"), body))
		if kind != "console" {
			status = 403
			out = map[string]any{"errors": []any{map[string]any{"code": "403", "title": "Forbidden",
				"detail": "receipts from " + kind + " cannot be verified on OpenPak"}}}
			break
		}
		// Reply shape unmeasured: echo the request's attributes.
		out = data("receipt", attrsOf(body))
	default:
		log.Printf("[innersloth] !! unhandled %s %s%s headers=%s body=%s", r.Method, logPath(r.URL.Path),
			logQuery(r.URL.RawQuery), headerNames(r.Header), bodyShape(r.Header.Get("Content-Type"), body))
		writeJSON(w, 404, map[string]any{"errors": []any{map[string]any{"code": "404", "title": "Not Found"}}})
		return
	}
	log.Printf("[innersloth] %s %s%s user=%s -> %d", r.Method, logPath(r.URL.Path), logQuery(r.URL.RawQuery), redact.Tag(puid), status)
	writeJSON(w, status, out)
}

func data(typ string, attrs any) map[string]any {
	return map[string]any{"data": map[string]any{"type": typ, "attributes": attrs}}
}

func (s *server) usernameDoc(puid string) map[string]any {
	n, d := friendCode(puid)
	return data("user", map[string]any{"username": n, "discriminator": d})
}

// receiptKind is the per-platform seam for purchases. ValidateReceipt carries entitlement_type
// ("dlc", "in-app-purchase", "innersloth-receipt") and a receipt whose shape names the store:
//
//   - {items: [...]}: Switch on-disk DLC (SwitchOnDiskDLCReceipt) or an InnerslothReceipt. The
//     console checked ownership itself (aoc), so the list is taken as sent: "console".
//   - {receiptJSON, productID} or {Store, TransactionID, Payload}: Apple / Google ("mobile").
//   - {item_id, token}: Microsoft Store, an XSTS token ("xbox").
//   - {catalog_item_id, entitlement_id}: Epic Games Store ("epic").
//
// Only "console" is accepted: the others need the real store to verify, which OpenPak is not.
// A batch is "console" only when every receipt in it is.
func receiptKind(body []byte) string {
	one := func(r map[string]json.RawMessage) string {
		switch {
		case r["items"] != nil:
			return "console"
		case r["token"] != nil:
			return "xbox"
		case r["catalog_item_id"] != nil || r["entitlement_id"] != nil:
			return "epic"
		case r["receiptJSON"] != nil || r["Payload"] != nil:
			return "mobile"
		}
		return "unknown"
	}
	a := attrsOf(body)
	var list []struct {
		Receipt map[string]json.RawMessage `json:"receipt"`
	}
	if json.Unmarshal(a["receipts"], &list) == nil && len(list) > 0 {
		for _, r := range list {
			if k := one(r.Receipt); k != "console" {
				return k
			}
		}
		return "console"
	}
	var r map[string]json.RawMessage
	if json.Unmarshal(a["receipt"], &r) != nil {
		// A receipt sent as a JSON string (the client's own serialised object).
		var str string
		if json.Unmarshal(a["receipt"], &str) != nil || json.Unmarshal([]byte(str), &r) != nil {
			return "unknown"
		}
	}
	return one(r)
}

// attrsOf returns a request body's data.attributes, or the body itself when it is not JSON:API.
func attrsOf(body []byte) map[string]json.RawMessage {
	var in struct {
		Data struct {
			Attributes map[string]json.RawMessage `json:"attributes"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &in) == nil && in.Data.Attributes != nil {
		return in.Data.Attributes
	}
	var flat map[string]json.RawMessage
	if json.Unmarshal(body, &flat) == nil && flat != nil {
		return flat
	}
	return map[string]json.RawMessage{}
}

// attrRaw reads data.attributes.<key> (or a top-level <key>) as raw JSON, null when absent.
func attrRaw(body []byte, key string) json.RawMessage {
	if v, ok := attrsOf(body)[key]; ok {
		return v
	}
	return json.RawMessage("null")
}

// attr reads data.attributes.<key> (or a top-level <key>) as text: a string unquoted, a
// number as written, "" for anything else.
func attr(body []byte, key string) string {
	v := attrRaw(body, key)
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(v, &n) == nil {
		return n.String()
	}
	return ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (g *social) friendsDoc(puid string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	friends := []any{}
	for f := range g.friends[puid] {
		friends = append(friends, map[string]any{"friend_puid": f, "friend_friendcode": fullCode(f)})
	}
	return map[string]any{"friends": friends}
}

func (g *social) requestsDoc(puid string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	senders := []any{}
	for s := range g.requests[puid] {
		senders = append(senders, map[string]any{"sender_puid": s, "sender_friendcode": fullCode(s)})
	}
	return map[string]any{"senders": senders}
}

func (g *social) requestByCode(puid string, body []byte) (int, any) {
	code := strings.ToLower(firstNonEmpty(attr(body, "recipient_friendcode"), attr(body, "username")))
	g.mu.Lock()
	to, ok := g.byCode[code]
	g.mu.Unlock()
	if !ok {
		return 404, map[string]any{"errors": []any{map[string]any{"code": "404", "title": "Not Found", "detail": "no player with that friend code"}}}
	}
	return g.request(puid, to)
}

func (g *social) request(from, to string) (int, any) {
	if to == "" || to == from {
		return 400, map[string]any{"errors": []any{map[string]any{"code": "400", "title": "Bad Request"}}}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.requests[from][to] {
		// They had already asked us: that makes us friends.
		delete(g.requests[from], to)
		addTo(g.friends, from, to)
		addTo(g.friends, to, from)
	} else if !g.friends[from][to] {
		addTo(g.requests, to, from)
	}
	return 200, data("friend_request", map[string]any{"recipient_puid": to, "recipient_friendcode": fullCode(to)})
}

func (g *social) accept(puid, sender string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.requests[puid][sender] {
		delete(g.requests[puid], sender)
		addTo(g.friends, puid, sender)
		addTo(g.friends, sender, puid)
	}
}

func (g *social) deny(puid, sender string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.requests[puid], sender)
}

func (g *social) unfriend(puid, other string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.friends[puid], other)
	delete(g.friends[other], puid)
}

func (g *social) blocksDoc(puid string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	// ResponseBlockedWrapper: blocking holds {recipient_puid, recipient_friendcode},
	// blocked_by holds {sender_puid, sender_friendcode}.
	blocking, blockedBy := []any{}, []any{}
	for b := range g.blocks[puid] {
		blocking = append(blocking, map[string]any{"recipient_puid": b, "recipient_friendcode": fullCode(b)})
	}
	for b, set := range g.blocks {
		if set[puid] {
			blockedBy = append(blockedBy, map[string]any{"sender_puid": b, "sender_friendcode": fullCode(b)})
		}
	}
	return map[string]any{"blocking": blocking, "blocked_by": blockedBy}
}

// forget drops everything kept about a player (user/accounts/deletion). The friend code is
// derived from the PUID, so there is nothing else to delete.
func (g *social) forget(puid string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.byCode, strings.ToLower(fullCode(puid)))
	for _, m := range []map[string]map[string]bool{g.friends, g.requests, g.blocks} {
		delete(m, puid)
		for _, set := range m {
			delete(set, puid)
		}
	}
	delete(g.invites, puid)
	for to, list := range g.invites {
		kept := list[:0:0]
		for _, i := range list {
			if i.Sender != puid {
				kept = append(kept, i)
			}
		}
		g.invites[to] = kept
	}
}

func (g *social) block(puid, other string) {
	if other == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	addTo(g.blocks, puid, other)
}

func (g *social) unblock(puid, other string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.blocks[puid], other)
}

func (g *social) invitesDoc(puid string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	inv := []any{}
	for _, i := range g.invites[puid] {
		inv = append(inv, map[string]any{"sender_puid": i.Sender, "recipient_puid": i.Recipient, "room_code": i.Room,
			"sender_friendcode": fullCode(i.Sender), "recipient_friendcode": fullCode(i.Recipient)})
	}
	return map[string]any{"next_poll_seconds": 15, "invites": inv}
}

func (g *social) invite(from, to string, room json.RawMessage) {
	if to == "" || string(room) == "null" || string(room) == `""` {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	list := g.invites[to][:0:0]
	for _, i := range g.invites[to] {
		if i.Sender != from {
			list = append(list, i)
		}
	}
	g.invites[to] = append(list, lobbyInvite{Sender: from, Recipient: to, Room: room})
}

func (g *social) dropInvite(puid, sender string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	list := g.invites[puid][:0:0]
	for _, i := range g.invites[puid] {
		if sender != "" && i.Sender != sender {
			list = append(list, i)
		}
	}
	g.invites[puid] = list
}
