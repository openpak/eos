package main

// KWS (Kids Web Services), the SDK's age and permission interface. Among Us asks for both right
// after Connect login and will not sign in without them:
//
//	GET /kws/v1/country/ageGateInfo           the age of consent where the player is
//	GET /kws/v1/users/<puid>/permissions      the player's KWS record and permissions
//
// OpenPak has no age record of its own: the console's parental controls (pctl, served by
// nx-pctl) are where a parent restricts communication, and the SDK checks the Nintendo free
// communication permission itself before any of this. So every player is answered as an adult
// with every permission the title asks for granted. Field names are the SDK's own (docs/protocol.md
// §6); the permission keys are the ones Among Us looks up.

import (
	"net/http"
	"strings"
)

// grantedPermissions are the KWS permission keys the titles here query.
var grantedPermissions = []string{
	// Among Us
	"freeChat", "customDisplayName", "friendsList", "accountLinking",
}

func (s *server) kws(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/kws")
	switch {
	case r.Method == http.MethodGet && p == "/v1/country/ageGateInfo":
		writeJSON(w, 200, map[string]any{"country": "US", "consentAgeForCountry": 13})
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/v1/users/") && strings.HasSuffix(p, "/permissions"):
		puid := strings.TrimSuffix(strings.TrimPrefix(p, "/v1/users/"), "/permissions")
		perms := map[string]string{}
		for _, k := range grantedPermissions {
			perms[k] = "GRANTED"
		}
		writeJSON(w, 200, map[string]any{
			"kwsUserId":   puid,
			"dateOfBirth": "1990-01-01",
			"isMinor":     false,
			"parentEmail": "",
			"permissions": perms,
		})
	default:
		s.unhandled(w, r)
	}
}
