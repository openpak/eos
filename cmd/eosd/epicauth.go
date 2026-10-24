package main

// EOS Auth: the Epic account a title asks for with EOS_Auth_Login. Connect (auth.go) gives a
// product user id; Auth gives an Epic account id and a user token. Titles whose own backend
// trusts Epic accounts need it. Fall Guys hands that token to its login service.
//
// POST /epic/oauth/v2/token with grant_type=external_auth and a Nintendo NSA id_token. Epic
// creates a "headless" account for a console player who never linked one, so the answer is a
// token straight away and there is no continuation step. Here the account id is derived like
// the PUID: an HMAC of the BAAS user id, the same in every title. An Epic account is not scoped
// to a deployment.
//
// grant_type=refresh_token renews it. GET /epic/oauth/v2/tokenInfo describes a token. Connect
// accepts the access or id token as epicgames_access_token / epicgames_id_token (auth.go). Shapes
// follow Epic's public Web API documentation for the account services token endpoint.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"openpak/eos/redact"
)

const epicIssuer = "https://api.epicgames.dev/epic/oauth/v2"

func (s *server) epicToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "errors.com.epicgames.common.method_not_allowed", "method not allowed")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, 400, "errors.com.epicgames.common.oauth.invalid_request", "bad form")
		return
	}
	clientID, _, _ := r.BasicAuth()
	grant := r.PostForm.Get("grant_type")
	log.Printf("[eos] epic token grant=%s external_auth_type=%s client=%s fields=%s", grant,
		r.PostForm.Get("external_auth_type"), redact.Tag(clientID), formKeys(r.PostForm))

	var baasID string
	switch grant {
	case "external_auth":
		extType := r.PostForm.Get("external_auth_type")
		if !nintendoTypes[extType] {
			writeError(w, 400, "errors.com.epicgames.account.oauth.external_auth_type_not_supported", "unsupported external auth type")
			return
		}
		p, err := s.ids.byIDToken(r.PostForm.Get("external_auth_token"))
		switch {
		case errors.Is(err, errTokenRejected):
			writeError(w, 400, "errors.com.epicgames.account.oauth.external_auth_validate_failed", "external token not valid")
			return
		case errors.Is(err, errAccountBanned):
			writeError(w, 403, "errors.com.epicgames.account.account_banned", "account banned")
			return
		case err != nil:
			log.Printf("[eos] epic login failed: nx-baas: %s", redact.Err(err))
			writeError(w, 503, "errors.com.epicgames.common.server_error", "identity unavailable")
			return
		}
		baasID = p.BaasUserID
	case "refresh_token":
		c, err := s.signer.verify(r.PostForm.Get("refresh_token"))
		if err != nil || c["t"] != "epic_refresh" {
			writeError(w, 400, "errors.com.epicgames.account.auth_token.invalid_refresh_token", "refresh token not valid")
			return
		}
		baasID = str(c, "baas")
	default:
		writeError(w, 400, "errors.com.epicgames.common.oauth.unsupported_grant_type", "unsupported grant type")
		return
	}
	writeJSON(w, 200, s.epicTokens(clientID, baasID, r.PostForm.Get("scope")))
}

func (s *server) epicTokens(clientID, baasID, scope string) map[string]any {
	acct := s.epicAccountID(baasID)
	now := time.Now()
	exp, rexp := now.Add(tokenTTL), now.Add(30*24*time.Hour)
	if scope == "" {
		scope = "basic_profile friends_list presence"
	}
	access := s.signer.sign(map[string]any{
		"iss": epicIssuer, "sub": acct, "aud": clientID, "clid": clientID, "t": "epic",
		"scope": scope, "iat": now.Unix(), "exp": exp.Unix(), "jti": randomHex(32),
	})
	refresh := s.signer.sign(map[string]any{
		"iss": epicIssuer, "sub": acct, "aud": clientID, "t": "epic_refresh", "baas": baasID,
		"iat": now.Unix(), "exp": rexp.Unix(), "jti": randomHex(32),
	})
	id := s.signer.sign(map[string]any{
		"iss": epicIssuer, "sub": acct, "aud": clientID, "t": "epic_id",
		"iat": now.Unix(), "exp": exp.Unix(), "auth_time": now.Unix(),
	})
	log.Printf("[eos] epic login ok: account=%s", redact.Tag(acct))
	return map[string]any{
		"access_token":        access,
		"token_type":          "bearer",
		"expires_in":          int(tokenTTL.Seconds()) - 1,
		"expires_at":          exp.UTC().Format(time.RFC3339Nano),
		"refresh_token":       refresh,
		"refresh_expires":     int((30 * 24 * time.Hour).Seconds()),
		"refresh_expires_at":  rexp.UTC().Format(time.RFC3339Nano),
		"account_id":          acct,
		"client_id":           clientID,
		"internal_client":     false,
		"client_service":      "game",
		"application_id":      "",
		"selected_account_id": acct,
		"merged_accounts":     []string{},
		"scope":               scope,
		"acr":                 "urn:epic:loa:aal1",
		"auth_time":           now.UTC().Format(time.RFC3339Nano),
		"id_token":            id,
	}
}

// epicAccountID is a 32-hex Epic account id, stable per console account across titles.
func (s *server) epicAccountID(baasID string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("epic\x00" + baasID))
	return hex.EncodeToString(m.Sum(nil)[:16])
}

// GET /epic/oauth/v2/tokenInfo (or POST with token=): what an Epic access token says.
func (s *server) epicTokenInfo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	tok := r.Form.Get("token")
	if tok == "" {
		tok, _ = bearer(r.Header.Get("Authorization"))
	}
	c, err := s.signer.verify(tok)
	if err != nil || c["t"] != "epic" {
		writeJSON(w, 200, map[string]any{"active": false})
		return
	}
	exp, _ := c["exp"].(float64)
	writeJSON(w, 200, map[string]any{
		"active": true, "scope": str(c, "scope"), "token_type": "bearer",
		"client_id": str(c, "clid"), "account_id": str(c, "sub"),
		"expires_in": int64(exp) - time.Now().Unix(),
		"expires_at": time.Unix(int64(exp), 0).UTC().Format(time.RFC3339Nano),
	})
}

func isEpicAuthPath(p string) bool {
	return strings.HasPrefix(p, "/epic/oauth/v2/") || strings.HasPrefix(p, "/epic/oauth/v1/")
}

func (s *server) epicAuth(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/epic/oauth/v2"), "/epic/oauth/v1") {
	case "/token":
		s.epicToken(w, r)
	case "/tokenInfo":
		s.epicTokenInfo(w, r)
	default:
		s.unhandled(w, r)
	}
}
