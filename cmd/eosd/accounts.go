package main

// The EOS side of social: which core account a PUID is, and the few things the core has no place for
// (the derived friend code, a pending account-deletion flag, lobby invites). They live in Postgres
// (EOS_DATABASE_URL: eos_accounts, eos_invites) with memory only as a cache in front. Friends,
// requests and blocks are not here: they are the core's (corefriends.go).
//
// Lobby invites stay here because the core's Invitations need a live game session registered per
// recipient and title, which eosd does not open; they are short-lived notifications keyed by
// (sender, recipient) and expire after an hour.

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const inviteTTL = time.Hour

type acctRec struct {
	PUID, Account, Baas, Code string
	Deleting                  bool
}

type lobbyInvite struct {
	Sender, Recipient string
	Room              json.RawMessage // room_code exactly as the sender's client wrote it
	At                time.Time
}

const acctSchema = `CREATE TABLE IF NOT EXISTS eos_accounts (
	puid         text PRIMARY KEY,
	account_id   text NOT NULL,
	baas_user_id text NOT NULL,
	code         text NOT NULL,
	deleting     boolean NOT NULL DEFAULT false,
	last_seen    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS eos_accounts_account ON eos_accounts (account_id);
CREATE UNIQUE INDEX IF NOT EXISTS eos_accounts_code ON eos_accounts (code);
CREATE TABLE IF NOT EXISTS eos_invites (
	sender_puid    text NOT NULL,
	recipient_puid text NOT NULL,
	room           text NOT NULL,
	created_at     timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (sender_puid, recipient_puid)
)`

// accounts is the cache plus the optional Postgres behind it.
type accounts struct {
	mu        sync.Mutex
	byPUID    map[string]*acctRec
	byAccount map[string]string
	byCode    map[string]string
	invites   map[string][]lobbyInvite // recipient -> invites (cache of eos_invites)
	pool      *pgxpool.Pool
}

func newAccounts() *accounts {
	return &accounts{byPUID: map[string]*acctRec{}, byAccount: map[string]string{}, byCode: map[string]string{},
		invites: map[string][]lobbyInvite{}}
}

func (a *accounts) attach(ctx context.Context, p *pgxpool.Pool) error {
	if _, err := p.Exec(ctx, acctSchema); err != nil {
		return err
	}
	a.mu.Lock()
	a.pool = p
	a.mu.Unlock()
	return nil
}

func (a *accounts) db() *pgxpool.Pool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pool
}

func (a *accounts) cacheRec(r *acctRec) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.byPUID[r.PUID] = r
	a.byAccount[r.Account] = r.PUID
	a.byCode[r.Code] = r.PUID
}

const acctCols = `puid, account_id, baas_user_id, code, deleting`

func scanRec(row pgx.Row) (*acctRec, error) {
	var r acctRec
	err := row.Scan(&r.PUID, &r.Account, &r.Baas, &r.Code, &r.Deleting)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (a *accounts) lookup(ctx context.Context, where, arg string, cached func() (*acctRec, bool)) *acctRec {
	if r, ok := cached(); ok {
		return r
	}
	p := a.db()
	if p == nil {
		return nil
	}
	r, err := scanRec(p.QueryRow(ctx, `SELECT `+acctCols+` FROM eos_accounts WHERE `+where+` = $1`, arg))
	if err != nil {
		log.Printf("[eos] ERROR reading eos_accounts: %v", err)
		return nil
	}
	if r != nil {
		a.cacheRec(r)
	}
	return r
}

func (a *accounts) get(ctx context.Context, puid string) *acctRec {
	return a.lookup(ctx, "puid", puid, func() (*acctRec, bool) {
		a.mu.Lock()
		defer a.mu.Unlock()
		r, ok := a.byPUID[puid]
		return r, ok
	})
}

func (a *accounts) getByAccount(ctx context.Context, account string) *acctRec {
	return a.lookup(ctx, "account_id", account, func() (*acctRec, bool) {
		a.mu.Lock()
		defer a.mu.Unlock()
		p, ok := a.byAccount[account]
		return a.byPUID[p], ok
	})
}

func (a *accounts) getByCode(ctx context.Context, code string) *acctRec {
	code = strings.ToLower(code)
	return a.lookup(ctx, "code", code, func() (*acctRec, bool) {
		a.mu.Lock()
		defer a.mu.Unlock()
		p, ok := a.byCode[code]
		return a.byPUID[p], ok
	})
}

// put records a player once (and again if anything about them changed).
func (a *accounts) put(ctx context.Context, r *acctRec) {
	a.mu.Lock()
	old := a.byPUID[r.PUID]
	a.mu.Unlock()
	if old != nil && *old == *r {
		return
	}
	if p := a.db(); p != nil {
		_, err := p.Exec(ctx, `INSERT INTO eos_accounts (puid, account_id, baas_user_id, code, deleting, last_seen)
			VALUES ($1,$2,$3,$4,$5,now())
			ON CONFLICT (puid) DO UPDATE SET account_id=EXCLUDED.account_id, baas_user_id=EXCLUDED.baas_user_id,
			code=EXCLUDED.code, deleting=EXCLUDED.deleting, last_seen=now()`, r.PUID, r.Account, r.Baas, r.Code, r.Deleting)
		if err != nil {
			log.Printf("[eos] ERROR saving eos_accounts (retried at the next request): %v", err)
			return
		}
	}
	a.cacheRec(r)
}

func (a *accounts) setDeleting(ctx context.Context, puid string, on bool) {
	if r := a.get(ctx, puid); r != nil {
		c := *r
		c.Deleting = on
		a.put(ctx, &c)
	}
}

// ---- invites ----

func (a *accounts) addInvite(ctx context.Context, i lobbyInvite) {
	i.At = time.Now()
	if p := a.db(); p != nil {
		if _, err := p.Exec(ctx, `INSERT INTO eos_invites (sender_puid, recipient_puid, room, created_at) VALUES ($1,$2,$3,now())
			ON CONFLICT (sender_puid, recipient_puid) DO UPDATE SET room=EXCLUDED.room, created_at=now()`,
			i.Sender, i.Recipient, string(i.Room)); err != nil {
			log.Printf("[eos] ERROR saving eos_invites: %v", err)
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	list := a.invites[i.Recipient][:0:0]
	for _, o := range a.invites[i.Recipient] {
		if o.Sender != i.Sender {
			list = append(list, o)
		}
	}
	a.invites[i.Recipient] = append(list, i)
}

func (a *accounts) invitesFor(ctx context.Context, to string) []lobbyInvite {
	if p := a.db(); p != nil {
		rows, err := p.Query(ctx, `SELECT sender_puid, room, created_at FROM eos_invites
			WHERE recipient_puid = $1 AND created_at > now() - $2::interval ORDER BY created_at`, to, inviteTTL.String())
		if err == nil {
			defer rows.Close()
			var out []lobbyInvite
			for rows.Next() {
				i := lobbyInvite{Recipient: to}
				var room string
				if rows.Scan(&i.Sender, &room, &i.At) == nil {
					i.Room = json.RawMessage(room)
					out = append(out, i)
				}
			}
			return out
		}
		log.Printf("[eos] ERROR reading eos_invites: %v", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []lobbyInvite
	for _, i := range a.invites[to] {
		if time.Since(i.At) < inviteTTL {
			out = append(out, i)
		}
	}
	return out
}

// dropInvites removes the invites to `to` from sender (all of them when sender is empty).
func (a *accounts) dropInvites(ctx context.Context, to, sender string) {
	if p := a.db(); p != nil {
		if _, err := p.Exec(ctx, `DELETE FROM eos_invites WHERE recipient_puid = $1 AND ($2 = '' OR sender_puid = $2)`, to, sender); err != nil {
			log.Printf("[eos] ERROR deleting eos_invites: %v", err)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	kept := a.invites[to][:0:0]
	for _, i := range a.invites[to] {
		if sender != "" && i.Sender != sender {
			kept = append(kept, i)
		}
	}
	a.invites[to] = kept
}

// dropFrom removes every invite a player sent.
func (a *accounts) dropFrom(ctx context.Context, sender string) {
	if p := a.db(); p != nil {
		if _, err := p.Exec(ctx, `DELETE FROM eos_invites WHERE sender_puid = $1`, sender); err != nil {
			log.Printf("[eos] ERROR deleting eos_invites: %v", err)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for to, list := range a.invites {
		kept := list[:0:0]
		for _, i := range list {
			if i.Sender != sender {
				kept = append(kept, i)
			}
		}
		a.invites[to] = kept
	}
}
