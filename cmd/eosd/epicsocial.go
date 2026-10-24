package main

// Epic account social calls the SDK makes after EOS Auth (measured on Fall Guys, Harbor
// 2026-10-24). OpenPak keeps no Epic friends graph and no sanctions: each answers "none".

import (
	"io"
	"net/http"
	"strings"
)

// epicSocial answers what it knows and reports false for anything else.
func (s *server) epicSocial(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/epic/friends/v1/") && strings.Count(p, "/") == 4:
		// GET /epic/friends/v1/<account>: the Epic friends list, polled every few seconds.
		writeJSON(w, 200, []any{})
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/sanctions/v1/productUser/") && strings.HasSuffix(p, "/active/"):
		// GET /sanctions/v1/productUser/<puid>/active/: active sanctions.
		writeJSON(w, 200, map[string]any{"elements": []any{}, "paging": map[string]any{"offset": 0, "limit": 100, "total": 0}})
	case r.Method == http.MethodPost && p == "/user/v1/accounts/search":
		// Epic accounts linked to these Nintendo accounts (the console friends): none.
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<16))
		writeJSON(w, 200, []any{})
	default:
		return false
	}
	return true
}
