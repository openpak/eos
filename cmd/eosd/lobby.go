package main

// Lobbies, REST side: POST /lobby/v1/<deployment>/lobbies/filter (the SDK's EOS_LobbySearch_Find).
//
// The reply is {"sessions":[<lobby>...],"count":n}. The SDK parses each session in full and
// drops the whole reply if one of them is malformed, so every field below is always written.
// Lobbies are created over the lobby websocket (sockets.go), which is not served yet, so the
// store is empty and a search answers an empty list: the game's "no games found".

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// lobby is one lobby as the SDK reads it from a search result.
type lobby struct {
	Deployment        string         `json:"deployment"`
	ID                string         `json:"id"`
	Bucket            string         `json:"bucket"`
	Settings          lobbySettings  `json:"settings"`
	TotalPlayers      int            `json:"totalPlayers"`
	OpenPublicPlayers int            `json:"openPublicPlayers"`
	PublicPlayers     []string       `json:"publicPlayers"`
	Started           bool           `json:"started"`
	LastUpdated       string         `json:"lastUpdated"`
	Attributes        map[string]any `json:"attributes"`
	Owner             string         `json:"owner"`
}

type lobbySettings struct {
	MaxPublicPlayers     int    `json:"maxPublicPlayers"`
	AllowInvites         bool   `json:"allowInvites"`
	ShouldAdvertise      bool   `json:"shouldAdvertise"`
	AllowReadByID        bool   `json:"allowReadById"`
	AllowJoinViaPresence bool   `json:"allowJoinViaPresence"`
	AllowJoinInProgress  bool   `json:"allowJoinInProgress"`
	AllowConferenceRoom  bool   `json:"allowConferenceRoom"`
	AllowMigration       bool   `json:"allowMigration"`
	RejoinAfterKick      string `json:"rejoinAfterKick"` // "open" or "invite_only"
}

type lobbyStore struct {
	mu      sync.Mutex
	lobbies map[string]*lobby
}

func newLobbyStore() *lobbyStore { return &lobbyStore{lobbies: map[string]*lobby{}} }

type criterion struct {
	Key   string `json:"key"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// find returns the advertised lobbies of one deployment that match every criterion, newest
// first, at most max of them.
func (st *lobbyStore) find(deployment string, crit []criterion, max int) []lobby {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []lobby
	for _, l := range st.lobbies {
		if l.Deployment != deployment || !l.Settings.ShouldAdvertise {
			continue
		}
		ok := true
		for _, c := range crit {
			if !c.match(l) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, *l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastUpdated > out[j].LastUpdated })
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

// match applies one search criterion. Keys are "bucket" or "attributes.<name>"; an attribute
// the lobby lacks matches nothing (NOT_EQUAL included), as a missing field would on a server.
func (c criterion) match(l *lobby) bool {
	var have any
	switch {
	case c.Key == "bucket":
		have = l.Bucket
	case strings.HasPrefix(c.Key, "attributes."):
		v, ok := l.Attributes[strings.TrimPrefix(c.Key, "attributes.")]
		if !ok {
			return false
		}
		have = v
	default:
		return false
	}
	switch c.Op {
	case "EQUAL":
		return compare(have, c.Value) == 0
	case "NOT_EQUAL":
		return compare(have, c.Value) != 0
	case "GREATER_THAN":
		return compare(have, c.Value) > 0
	case "GREATER_THAN_OR_EQUAL":
		return compare(have, c.Value) >= 0
	case "LESS_THAN":
		return compare(have, c.Value) == -1
	case "LESS_THAN_OR_EQUAL":
		r := compare(have, c.Value)
		return r == -1 || r == 0
	case "CONTAINS":
		h, ok1 := have.(string)
		w, ok2 := c.Value.(string)
		return ok1 && ok2 && strings.Contains(h, w)
	case "ANY_OF", "NOT_ANY_OF":
		hit := false
		if list, ok := c.Value.([]any); ok {
			for _, v := range list {
				if compare(have, v) == 0 {
					hit = true
				}
			}
		}
		return hit == (c.Op == "ANY_OF")
	}
	return false
}

// compare orders two JSON values of the same kind: -1, 0, 1, or 2 when they cannot be compared.
func compare(a, b any) int {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		if !ok {
			return 2
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	case string:
		y, ok := b.(string)
		if !ok {
			return 2
		}
		return strings.Compare(x, y)
	case bool:
		y, ok := b.(bool)
		if !ok {
			return 2
		}
		if x == y {
			return 0
		}
		return 1
	}
	return 2
}

// lobbyREST serves /lobby/v1/<deployment>/lobbies/...; false sends the request to the catch-all.
func (s *server) lobbyREST(w http.ResponseWriter, r *http.Request) bool {
	rest := strings.TrimPrefix(r.URL.Path, "/lobby/v1/")
	deployment, sub, _ := strings.Cut(rest, "/")
	if r.Method != http.MethodPost || sub != "lobbies/filter" || s.user(r) == "" {
		return false
	}
	var q struct {
		Criteria   []criterion `json:"criteria"`
		MaxResults int         `json:"maxResults"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&q); err != nil {
		writeError(w, 400, "errors.com.epicgames.common.bad_request", "bad search")
		return true
	}
	found := s.lobbies.find(deployment, q.Criteria, q.MaxResults)
	if found == nil {
		found = []lobby{}
	}
	writeJSON(w, 200, map[string]any{"sessions": found, "count": len(found)})
	return true
}
