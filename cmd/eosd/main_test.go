package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// stubIdentity answers /internal/switch/identity like nx-baas.
func stubIdentity(t *testing.T, status int, body string) identityClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return identityClient{url: srv.URL}
}

func testServer(t *testing.T) *server {
	t.Helper()
	sg, err := loadSigner("")
	if err != nil {
		t.Fatal(err)
	}
	return newServer(stubIdentity(t, 200, `{"pid":918273645,"baas_user_id":"f00dfacecafe0123","nickname":"Zorblaxa"}`), sg, []byte("secret"))
}

func do(s *server, method, target string, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func form(v url.Values) (string, map[string]string) {
	return v.Encode(), map[string]string{
		"Content-Type":  "application/x-www-form-urlencoded",
		"Authorization": "Basic " + "Y2xpZW50OnNlY3JldA==", // client:secret
	}
}

func connectLogin(t *testing.T, s *server) map[string]any {
	t.Helper()
	body, hdr := form(url.Values{"grant_type": {"external_auth"}, "external_auth_type": {"nintendo_nsa_id_token"},
		"external_auth_token": {"eyJhbGciOi.eyJzdWIi.c2ln"}, "deployment_id": {"0123456789abcdef0123456789abcdef"},
		"nonce": {"n0nce"}, "display_name": {"Zorblaxa"}})
	rec := do(s, "POST", "https://api.epicgames.dev/auth/v1/oauth/token", body, hdr)
	if rec.Code != 200 {
		t.Fatalf("connect login: %d %s", rec.Code, rec.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func TestSDKConfigPointsEverythingHome(t *testing.T) {
	s := testServer(t)
	rec := do(s, "GET", "https://api.epicgames.dev/sdk/v1/default?platformId=SWT", "", nil)
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	// Same document for the per-product form SDK 1.16 asks for after the client token.
	if rec := do(s, "GET", "https://api.epicgames.dev/sdk/v1/product/abc?platformId=SWT&deploymentId=x", "", nil); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	var doc struct {
		Services map[string]struct{ BaseUrl string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &doc)
	if len(doc.Services) == 0 {
		t.Fatal("no services")
	}
	for name, svc := range doc.Services {
		if !strings.HasPrefix(svc.BaseUrl, "https://api.epicgames.dev") {
			t.Errorf("%s leaves for %s", name, svc.BaseUrl)
		}
	}
}

func TestConnectLoginIsStablePerDeployment(t *testing.T) {
	s := testServer(t)
	a, b := connectLogin(t, s), connectLogin(t, s)
	if a["product_user_id"] == "" || a["product_user_id"] != b["product_user_id"] {
		t.Fatalf("puid not stable: %v %v", a["product_user_id"], b["product_user_id"])
	}
	if len(a["product_user_id"].(string)) != 32 || a["nonce"] != "n0nce" {
		t.Fatalf("bad reply: %v", a)
	}
	// The access token is ours and names the PUID.
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+a["access_token"].(string))
	if s.user(req) != a["product_user_id"] {
		t.Fatal("access token does not verify")
	}
}

func TestConnectLoginRefusesWhatNxBaasRefuses(t *testing.T) {
	sg, _ := loadSigner("")
	for status, want := range map[int]int{401: 401, 403: 403, 500: 503} {
		s := newServer(stubIdentity(t, status, `{}`), sg, []byte("x"))
		body, hdr := form(url.Values{"grant_type": {"external_auth"}, "external_auth_type": {"nintendo_nsa_id_token"},
			"external_auth_token": {"t"}, "deployment_id": {"d"}})
		if rec := do(s, "POST", "https://api.epicgames.dev/auth/v1/oauth/token", body, hdr); rec.Code != want {
			t.Errorf("nx-baas %d: got %d, want %d", status, rec.Code, want)
		}
	}
}

func TestKWSAnswersAnAdult(t *testing.T) {
	s := testServer(t)
	puid := connectLogin(t, s)["product_user_id"].(string)
	rec := do(s, "GET", "https://api.epicgames.dev/kws/v1/users/"+puid+"/permissions", "", nil)
	var out struct {
		IsMinor     bool              `json:"isMinor"`
		Permissions map[string]string `json:"permissions"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || out.IsMinor || out.Permissions["freeChat"] != "GRANTED" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestInnerslothFriendsFlow(t *testing.T) {
	s := testServer(t)
	tokA := s.signer.sign(map[string]any{"sub": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "t": "connect"})
	tokB := s.signer.sign(map[string]any{"sub": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "t": "connect"})
	call := func(tok, method, path, body string) map[string]any {
		rec := do(s, method, "https://backend.innersloth.com/api/"+path, body, map[string]string{"Authorization": "Bearer " + tok})
		if rec.Code != 200 {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
		}
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out["data"].(map[string]any)["attributes"].(map[string]any)
	}
	a := call(tokA, "GET", "user/username", "")
	b := call(tokB, "GET", "user/username", "")
	if a["username"] == "" || len(a["discriminator"].(string)) != 4 {
		t.Fatalf("friend code: %v", a)
	}
	codeB := b["username"].(string) + "#" + b["discriminator"].(string)
	call(tokA, "POST", "user/friends/request-by-friendcode", `{"data":{"type":"friend_request","attributes":{"recipient_friendcode":"`+codeB+`"}}}`)
	if got := call(tokB, "GET", "user/friends/received-requests", "")["senders"].([]any); len(got) != 1 {
		t.Fatalf("B sees %d requests", len(got))
	}
	call(tokB, "PUT", "user/friends/request?sender_puid=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "")
	if got := call(tokA, "GET", "user/friends", "")["friends"].([]any); len(got) != 1 {
		t.Fatalf("A has %d friends", len(got))
	}
	call(tokA, "POST", "user/lobby/invites", `{"data":{"type":"lobby_invite","attributes":{"recipient_puid":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","room_code":"ABCDEF"}}}`)
	if got := call(tokB, "GET", "user/lobby/invites", "")["invites"].([]any); len(got) != 1 {
		t.Fatalf("B has %d invites", len(got))
	}
	if rec := do(s, "GET", "https://backend.innersloth.com/api/user/username", "", nil); rec.Code != 401 {
		t.Fatalf("no token: %d", rec.Code)
	}
}

// Every handler, driven with sentinel values, must leave no token, secret, PUID, nickname or
// free text in the log.
func TestLogsCarryNoPersonalData(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	s := testServer(t)
	login := connectLogin(t, s)
	puid := login["product_user_id"].(string)
	tok := login["access_token"].(string)
	auth := map[string]string{"Authorization": "Bearer " + tok, "Cookie": "sid=cookie-secret"}
	do(s, "GET", "https://api.epicgames.dev/kws/v1/users/"+puid+"/permissions", "", auth)
	do(s, "POST", "https://api.epicgames.dev/lobby/v1/dep/lobbies/"+puid, `{"name":"my-private-chat-line","owner":"Zorblaxa"}`,
		map[string]string{"Authorization": "Bearer " + tok, "Content-Type": "application/json"})
	do(s, "GET", "https://api.epicgames.dev/x?accountId=q-token-9f8e7d", "", auth)
	do(s, "GET", "https://backend.innersloth.com/api/user/username", "", auth)
	do(s, "POST", "https://backend.innersloth.com/api/user/nope", `{"username":"Zorblaxa"}`, auth)
	out := buf.String()
	for _, secret := range []string{tok, puid, "f00dfacecafe0123", "Zorblaxa", "my-private-chat-line",
		"cookie-secret", "q-token-9f8e7d", "eyJhbGciOi", "c2VjcmV0", "n0nce", "918273645"} {
		if strings.Contains(out, secret) {
			t.Errorf("log carries %q", secret)
		}
	}
	if !strings.Contains(out, "!! unhandled POST") {
		t.Error("unhandled request not logged")
	}
}
