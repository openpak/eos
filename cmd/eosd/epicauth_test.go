package main

import (
	"encoding/json"
	"net/url"
	"testing"
)

func epicLogin(t *testing.T, s *server) map[string]any {
	t.Helper()
	body, hdr := form(url.Values{"grant_type": {"external_auth"}, "external_auth_type": {"nintendo_nsa_id_token"},
		"external_auth_token": {"eyJhbGciOi.eyJzdWIi.c2ln"}})
	rec := do(s, "POST", "https://api.epicgames.dev/epic/oauth/v2/token", body, hdr)
	if rec.Code != 200 {
		t.Fatalf("epic login: %d %s", rec.Code, rec.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func TestEpicAuthLoginRefreshAndConnect(t *testing.T) {
	s := testServer(t)
	a, b := epicLogin(t, s), epicLogin(t, s)
	acct, _ := a["account_id"].(string)
	if len(acct) != 32 || acct != b["account_id"] {
		t.Fatalf("account id not stable: %v %v", a["account_id"], b["account_id"])
	}

	// The access token verifies against the published key set and names the account.
	c, err := s.signer.verify(a["access_token"].(string))
	if err != nil || c["sub"] != acct {
		t.Fatalf("access token: %v %v", c, err)
	}
	if rec := do(s, "GET", "https://api.epicgames.dev/epic/oauth/v2/.well-known/jwks.json", "", nil); rec.Code != 200 {
		t.Fatal("jwks", rec.Code)
	}

	// tokenInfo
	rec := do(s, "GET", "https://api.epicgames.dev/epic/oauth/v2/tokenInfo", "", map[string]string{"Authorization": "bearer " + a["access_token"].(string)})
	var info map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &info)
	if info["active"] != true || info["account_id"] != acct {
		t.Fatalf("tokenInfo: %s", rec.Body)
	}

	// refresh keeps the account
	body, hdr := form(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {a["refresh_token"].(string)}})
	rec = do(s, "POST", "https://api.epicgames.dev/epic/oauth/v2/token", body, hdr)
	var r map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if rec.Code != 200 || r["account_id"] != acct {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body)
	}
	// an access token is not a refresh token
	body, hdr = form(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {a["access_token"].(string)}})
	if rec := do(s, "POST", "https://api.epicgames.dev/epic/oauth/v2/token", body, hdr); rec.Code != 400 {
		t.Fatalf("access token accepted as refresh: %d", rec.Code)
	}

	// Connect on the Epic id token gives a stable product user id.
	puids := map[any]bool{}
	for _, tok := range []string{a["id_token"].(string), b["id_token"].(string)} {
		body, hdr = form(url.Values{"grant_type": {"external_auth"}, "external_auth_type": {"epicgames_id_token"},
			"external_auth_token": {tok}, "deployment_id": {"0123456789abcdef0123456789abcdef"}})
		rec = do(s, "POST", "https://api.epicgames.dev/auth/v1/oauth/token", body, hdr)
		var cl map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &cl)
		if rec.Code != 200 {
			t.Fatalf("connect on epic token: %d %s", rec.Code, rec.Body)
		}
		puids[cl["product_user_id"]] = true
	}
	if len(puids) != 1 {
		t.Fatalf("puid not stable: %v", puids)
	}
}

func TestEpicAuthRefusesWhatNxBaasRefuses(t *testing.T) {
	sg, _ := loadSigner("")
	s := newServer(stubIdentity(t, 401, `{}`), sg, []byte("secret"))
	body, hdr := form(url.Values{"grant_type": {"external_auth"}, "external_auth_type": {"nintendo_nsa_id_token"},
		"external_auth_token": {"x.y.z"}})
	if rec := do(s, "POST", "https://api.epicgames.dev/epic/oauth/v2/token", body, hdr); rec.Code != 400 {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}
