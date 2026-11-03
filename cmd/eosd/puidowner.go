package main

// Which account a product user id belongs to, for the game servers that only ever see the PUID
// (Impostor, for Among Us rooms) and need the BAAS user id to tell rewards who played.
//
// A PUID is an HMAC of the account, so it cannot be turned back; the Connect login (auth.go) is the
// one place that knows both, and remembers the pair here, in Postgres (ownerstore.go) with a memory
// cache in front, so a restart forgets nobody. Served on the health listener,
// never on the public one, and only with EOS_INTERNAL_KEY.

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

func (s *server) rememberOwner(puid, baasID string) { s.remember(kindPUID, puid, baasID) }

// puidOwner answers GET /internal/v1/puid/<puid> with {"baas_user_id":"..."}; 404 when unknown.
func (s *server) puidOwner(w http.ResponseWriter, r *http.Request) {
	key := os.Getenv("EOS_INTERNAL_KEY")
	if key == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Internal-Key")), []byte(key)) != 1 {
		writeError(w, 401, "errors.com.epicgames.common.unauthorized", "unauthorized")
		return
	}
	puid := strings.TrimPrefix(r.URL.Path, "/internal/v1/puid/")
	v, ok := s.owner(kindPUID, puid)
	if r.Method != http.MethodGet || !ok {
		writeError(w, 404, "errors.com.epicgames.common.not_found", "not found")
		return
	}
	writeJSON(w, 200, map[string]any{"baas_user_id": v})
}
