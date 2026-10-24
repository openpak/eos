package main

// POST /auth/v1/oauth/token, the one OAuth endpoint behind every EOS login:
//
//   - grant_type=client_credentials: the title authenticating itself (HTTP basic with its client
//     id and secret). The SDK wants this token before anything else after the config document.
//   - grant_type=external_auth: Connect login. external_auth_type names the credential; on a
//     console it is the Nintendo NSA id_token, which nx-baas minted and verifies here. The answer
//     is a product user id (PUID) plus an access token and an id_token for it.
//
// The PUID is derived, not stored: an HMAC of the deployment and the account's BAAS user id, so
// the same player gets the same PUID in a title on every login and on every restart, and a
// different one in a different title, which is how Epic scopes them. No database.

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

const (
	issuer   = "https://api.epicgames.dev/auth/v1/oauth"
	tokenTTL = time.Hour
)

// nintendoTypes are the external_auth_type values that carry a Nintendo id_token.
var nintendoTypes = map[string]bool{
	"nintendo_nsa_id_token": true,
	"nintendo_id_token":     true,
	"nsa_id_token":          true, // EOS Auth's name for it (Fall Guys, SDK 1.17)
}

func (s *server) token(w http.ResponseWriter, r *http.Request) {
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
	extType := r.PostForm.Get("external_auth_type")
	log.Printf("[eos] token grant=%s external_auth_type=%s client=%s fields=%s", grant, extType,
		redact.Tag(clientID), formKeys(r.PostForm))

	switch grant {
	case "client_credentials":
		s.clientToken(w, r, clientID)
	case "external_auth":
		s.connectLogin(w, r, clientID, extType)
	default:
		writeError(w, 400, "errors.com.epicgames.common.oauth.unsupported_grant_type", "unsupported grant type")
	}
}

// ids is what a token says it was issued for. The SDK sends the deployment; product and
// sandbox are not on the wire, so they are left empty until a title is shown to need them.
type ids struct {
	client, deployment string
}

func (s *server) clientToken(w http.ResponseWriter, r *http.Request, clientID string) {
	in := ids{client: clientID, deployment: r.PostForm.Get("deployment_id")}
	now := time.Now()
	exp := now.Add(tokenTTL)
	tok := s.signer.sign(map[string]any{
		"iss": issuer, "sub": clientID, "clid": clientID, "aud": clientID,
		"t": "client", "pfdid": in.deployment,
		"iat": now.Unix(), "exp": exp.Unix(), "jti": randomHex(32),
	})
	writeJSON(w, 200, map[string]any{
		"access_token":    tok,
		"token_type":      "bearer",
		"expires_at":      exp.UTC().Format(time.RFC3339Nano),
		"expires_in":      int(tokenTTL.Seconds()) - 1,
		"features":        []string{"AntiCheat", "Connect", "Lobbies", "Matchmaking", "P2P", "Sessions"},
		"organization_id": "",
		"product_id":      "",
		"sandbox_id":      "",
		"deployment_id":   in.deployment,
	})
}

func (s *server) connectLogin(w http.ResponseWriter, r *http.Request, clientID, extType string) {
	in := ids{client: clientID, deployment: r.PostForm.Get("deployment_id")}
	ext := r.PostForm.Get("external_auth_token")
	var account string // who the player is to us: a BAAS user id or a device id
	switch {
	case nintendoTypes[extType]:
		p, err := s.ids.byIDToken(ext)
		switch {
		case errors.Is(err, errTokenRejected):
			log.Printf("[eos] connect login refused: nx-baas rejected the id_token")
			writeError(w, 401, "errors.com.epicgames.eos.auth.invalid_external_token", "external token not valid")
			return
		case errors.Is(err, errAccountBanned):
			log.Printf("[eos] connect login refused: account banned")
			writeError(w, 403, "errors.com.epicgames.eos.auth.account_banned", "account banned")
			return
		case err != nil:
			log.Printf("[eos] connect login failed: nx-baas: %s", redact.Err(err))
			writeError(w, 503, "errors.com.epicgames.common.server_error", "identity unavailable")
			return
		}
		account = "baas:" + p.BaasUserID
	case extType == "epicgames_access_token" || extType == "epicgames_id_token":
		// Connect on top of EOS Auth: the Epic account token epicauth.go issued.
		c, err := s.signer.verify(ext)
		if err != nil || (c["t"] != "epic" && c["t"] != "epic_id") {
			writeError(w, 401, "errors.com.epicgames.eos.auth.invalid_external_token", "external token not valid")
			return
		}
		account = "epic:" + str(c, "sub")
	case extType == "deviceid_access_token":
		c, err := s.signer.verify(ext)
		if err != nil || c["t"] != "device" {
			writeError(w, 401, "errors.com.epicgames.eos.auth.invalid_external_token", "external token not valid")
			return
		}
		account = "device:" + str(c, "sub")
	default:
		log.Printf("[eos] connect login: unsupported external_auth_type %q", extType)
		writeError(w, 400, "errors.com.epicgames.eos.auth.unsupported_external_auth_type", "unsupported external auth type")
		return
	}

	puid := s.puid(in.deployment, account)
	now := time.Now()
	exp := now.Add(tokenTTL)
	common := map[string]any{
		"iss": issuer, "sub": puid, "aud": clientID, "clid": clientID,
		"pfdid": in.deployment, "iat": now.Unix(), "exp": exp.Unix(),
	}
	access := clone(common)
	access["t"] = "connect"
	access["jti"] = randomHex(32)
	if n := r.PostForm.Get("nonce"); n != "" {
		access["nonce"] = n
	}
	id := clone(common)
	id["act"] = map[string]any{"pltfm": "switch", "eat": extType, "eaid": s.externalID(account)}
	if n := r.PostForm.Get("nonce"); n != "" {
		id["nonce"] = n
	}
	log.Printf("[eos] connect login ok: type=%s puid=%s", extType, redact.Tag(puid))
	writeJSON(w, 200, map[string]any{
		"access_token":            s.signer.sign(access),
		"token_type":              "bearer",
		"expires_at":              exp.UTC().Format(time.RFC3339Nano),
		"expires_in":              int(tokenTTL.Seconds()) - 1,
		"nonce":                   r.PostForm.Get("nonce"),
		"organization_id":         "",
		"product_id":              "",
		"sandbox_id":              "",
		"deployment_id":           in.deployment,
		"organization_user_id":    puid,
		"product_user_id":         puid,
		"product_user_id_created": false,
		"id_token":                s.signer.sign(id),
	})
}

// puid derives the product user id: 32 hex digits, stable per deployment and account.
func (s *server) puid(deployment, account string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("puid\x00" + deployment + "\x00" + account))
	return hex.EncodeToString(m.Sum(nil)[:16])
}

// externalID is what the id_token names as the external account: the BAAS user id for a
// Nintendo login (the NSA id titles already see), the device id otherwise.
func (s *server) externalID(account string) string {
	_, v, _ := strings.Cut(account, ":")
	return v
}

// POST /auth/v1/accounts/deviceid: EOS_Connect_CreateDeviceId. The device id access token is
// ours to mint; a later external_auth login with deviceid_access_token turns it into a PUID.
func (s *server) createDeviceID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "errors.com.epicgames.common.method_not_allowed", "method not allowed")
		return
	}
	_ = r.ParseForm()
	log.Printf("[eos] create device id fields=%s", formKeys(r.PostForm))
	now := time.Now()
	tok := s.signer.sign(map[string]any{
		"iss": issuer, "sub": randomHex(32), "t": "device",
		"iat": now.Unix(), "exp": now.Add(10 * 365 * 24 * time.Hour).Unix(),
	})
	writeJSON(w, 200, map[string]any{"access_token": tok})
}

// user returns the PUID of the bearer token on r, or "" when it has none of ours.
func (s *server) user(r *http.Request) string {
	tok, err := bearer(r.Header.Get("Authorization"))
	if err != nil {
		return ""
	}
	c, err := s.signer.verify(tok)
	if err != nil || c["t"] != "connect" {
		return ""
	}
	return str(c, "sub")
}

func str(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

func clone(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
