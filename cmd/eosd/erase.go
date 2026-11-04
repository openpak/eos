package main

// Account erasure (card openpak/prds#120). DELETE /internal/accounts/{account_id}/data on the health
// listener (never routed by Traefik), Bearer ACCOUNT_ERASE_KEY. Erases the account's rows in
// eos_accounts, eos_invites (sent and received) and eos_owners (PUID and Epic id -> BAAS maps), and the
// in-memory caches in front of them. Friends, requests and blocks are the account core's.

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"

	accountv1 "github.com/openpak/account-api/openpak/account/v1"
)

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// switchSubjects lists the account's Switch link subjects (BAAS user ids) in the core.
type switchSubjects interface {
	SwitchSubjects(ctx context.Context, account string) ([]string, error)
}

func (c *grpcCore) SwitchSubjects(ctx context.Context, account string) ([]string, error) {
	r, err := c.links.ListAccountLinks(ctx, &accountv1.ListAccountLinksRequest{AccountId: account, Namespace: switchNamespace})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range r.GetLinks() {
		if l.GetSubjectId() != "" {
			out = append(out, l.GetSubjectId())
		}
	}
	return out, nil
}

// Erase removes everything eosd holds for the core account; a second run removes 0 rows.
func (s *server) Erase(ctx context.Context, account string) (int, error) {
	db := s.db.Load()
	pool := s.acct.db()
	baas := map[string]bool{}
	puids := map[string]bool{}

	// Memory caches (also the only copy when Postgres is not configured).
	s.acct.mu.Lock()
	for p, r := range s.acct.byPUID {
		if r.Account == account {
			puids[p] = true
			baas[r.Baas] = true
		}
	}
	s.acct.mu.Unlock()
	if pool != nil {
		rows, err := pool.Query(ctx, `SELECT puid, baas_user_id FROM eos_accounts WHERE account_id = $1`, account)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var p, b string
			if err := rows.Scan(&p, &b); err != nil {
				rows.Close()
				return 0, err
			}
			puids[p], baas[b] = true, true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, err
		}
	}
	if sc, ok := s.core.(switchSubjects); ok {
		subs, err := sc.SwitchSubjects(ctx, account)
		if err != nil {
			return 0, err
		}
		for _, b := range subs {
			baas[b] = true
		}
	}
	// PUIDs issued at Connect login for those BAAS ids, recorded only in eos_owners.
	if db != nil {
		for b := range baas {
			rows, err := db.pool.Query(ctx, `SELECT id FROM eos_owners WHERE kind = $1 AND baas_user_id = $2`, kindPUID, b)
			if err != nil {
				return 0, err
			}
			for rows.Next() {
				var p string
				if err := rows.Scan(&p); err != nil {
					rows.Close()
					return 0, err
				}
				puids[p] = true
			}
			rows.Close()
		}
	}
	pl, bl := keysOf(puids), keysOf(baas)

	n := 0
	if pool != nil {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return 0, err
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM eos_invites WHERE sender_puid = ANY($1) OR recipient_puid = ANY($1)`, []any{pl}},
			{`DELETE FROM eos_accounts WHERE account_id = $1 OR puid = ANY($2)`, []any{account, pl}},
			{`DELETE FROM eos_owners WHERE baas_user_id = ANY($1) OR (kind = 'puid' AND id = ANY($2))`, []any{bl, pl}},
		} {
			t, err := tx.Exec(ctx, q.sql, q.args...)
			if err != nil {
				return 0, err
			}
			n += int(t.RowsAffected())
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
	}

	if pool == nil {
		n = len(puids) // memory-only mode: the cached players are the rows
	}
	// Caches.
	s.acct.mu.Lock()
	for p := range puids {
		if r := s.acct.byPUID[p]; r != nil {
			delete(s.acct.byCode, r.Code)
			delete(s.acct.byAccount, r.Account)
		}
		delete(s.acct.byPUID, p)
		delete(s.acct.invites, p)
	}
	delete(s.acct.byAccount, account)
	for to, list := range s.acct.invites {
		kept := list[:0:0]
		for _, i := range list {
			if !puids[i.Sender] {
				kept = append(kept, i)
			}
		}
		s.acct.invites[to] = kept
	}
	s.acct.mu.Unlock()
	for p := range puids {
		s.owners.Delete(p)
	}
	for _, m := range []*sync.Map{&s.owners, &s.epicOwners} {
		m.Range(func(k, v any) bool {
			if baas[v.(string)] {
				m.Delete(k)
			}
			return true
		})
	}
	s.persisted.Range(func(k, _ any) bool {
		parts := strings.Split(k.(string), "\x00")
		if len(parts) == 3 && baas[parts[2]] {
			s.persisted.Delete(k)
		}
		return true
	})
	log.Printf("account erased account=%s rows=%d service=eos", account, n)
	return n, nil
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// eraseHandler serves DELETE /internal/accounts/{account_id}/data.
func (s *server) eraseHandler(w http.ResponseWriter, r *http.Request) {
	key := os.Getenv("ACCOUNT_ERASE_KEY")
	if key == "" {
		http.Error(w, "erase not configured", http.StatusServiceUnavailable)
		return
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/internal/accounts/"), "/data")
	if !uuidRE.MatchString(id) {
		http.Error(w, "bad account id", http.StatusBadRequest)
		return
	}
	n, err := s.Erase(r.Context(), strings.ToLower(id))
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("[eos] ERROR erase failed: %v", err)
		}
		http.Error(w, "erase failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, map[string]any{"service": "eos", "rows": n})
}
