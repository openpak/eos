package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The reply shapes follow the client's own DTOs (Assets.InnerNet.*, read from the metadata).
func TestInnerslothShapes(t *testing.T) {
	s := testServer(t)
	const a, b = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	playsAmongUs(s, a, b)
	tokA := s.signer.sign(map[string]any{"sub": a, "t": "connect"})
	tokB := s.signer.sign(map[string]any{"sub": b, "t": "connect"})
	call := func(tok, method, path, body string) map[string]any {
		t.Helper()
		rec := do(s, method, "https://backend.innersloth.com/api/"+path, body, map[string]string{"Authorization": "Bearer " + tok})
		if rec.Code != 200 {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out["data"].(map[string]any)["attributes"].(map[string]any)
	}
	call(tokB, "GET", "user/username", "")

	// Blocks are objects, not bare PUIDs (ResponseBlockedPlayer / ResponseBlockedBy).
	call(tokA, "POST", "user/blocks", `{"data":{"type":"blocks","attributes":{"recipient_puid":"`+b+`"}}}`)
	blocking := call(tokA, "GET", "user/blocks", "")["blocking"].([]any)
	if len(blocking) != 1 || blocking[0].(map[string]any)["recipient_puid"] != b ||
		blocking[0].(map[string]any)["recipient_friendcode"] != fullCode(b) {
		t.Fatalf("blocking: %v", blocking)
	}
	// The core never says who blocked you.
	if by := call(tokB, "GET", "user/blocks", "")["blocked_by"].([]any); len(by) != 0 {
		t.Fatalf("blocked_by: %v", by)
	}

	// A numeric room code comes back as a number.
	call(tokA, "POST", "user/lobby/invites", `{"data":{"type":"lobby_invite","attributes":{"recipient_puid":"`+b+`","room_code":-2147483000}}}`)
	inv := call(tokB, "GET", "user/lobby/invites", "")["invites"].([]any)
	if len(inv) != 1 || inv[0].(map[string]any)["room_code"] != float64(-2147483000) {
		t.Fatalf("invites: %v", inv)
	}

	// Friend request by code in the username field (FriendsListRequest.username).
	call(tokA, "POST", "user/friends/request-by-friendcode", `{"data":{"type":"friend_request","attributes":{"username":"`+fullCode(b)+`"}}}`)
	if got := call(tokB, "GET", "user/friends/received-requests", "")["senders"].([]any); len(got) != 1 {
		t.Fatalf("B sees %d requests", len(got))
	}

	p := call(tokA, "POST", "user/redeem-currency", `{"data":{"type":"redeem","attributes":{"item_id":"hat_x","currency_type":1,"platform":8}}}`)
	if p["item_id"] != "hat_x" || p["currency_type"] != float64(1) || p["item_cost"] != float64(0) {
		t.Fatalf("redeem: %v", p)
	}
	r := call(tokA, "POST", "user/validate-receipt", `{"data":{"type":"receipt","attributes":{"entitlement_type":"dlc","receipt":{"items":["a"]}}}}`)
	if r["entitlement_type"] != "dlc" {
		t.Fatalf("receipt: %v", r)
	}
	for body, want := range map[string]int{
		`{"data":{"attributes":{"entitlement_type":"in-app-purchase","receipt":{"item_id":"x","token":"t"}}}}`:     403,
		`{"data":{"attributes":{"entitlement_type":"in-app-purchase","receipt":"{\"Store\":\"AppleAppStore\"}"}}}`: 403,
		`{"data":{"attributes":{"receipts":[{"entitlement_type":"dlc","receipt":{"items":["a"]}}]}}}`:              200,
	} {
		path := "user/validate-receipt"
		if strings.Contains(body, "receipts") {
			path += "-batch"
		}
		if rec := do(s, "POST", "https://backend.innersloth.com/api/"+path, body, map[string]string{"Authorization": "Bearer " + tokA}); rec.Code != want {
			t.Errorf("%s: %d, want %d", body, rec.Code, want)
		}
	}
	call(tokA, "POST", "user/report-player", `{"data":{"type":"report","attributes":{"game_server_id":1,"game_id":2,"reportee_client_id":3,"reason_id":1}}}`)

	if rec := do(s, "GET", "https://backend.innersloth.com/api/user/accounts/deletion", "", map[string]string{"Authorization": "Bearer " + tokA}); rec.Code != 404 {
		t.Fatalf("deletion status with no request: %d, want 404", rec.Code)
	}
	if got := call(tokA, "GET", "announcements?console=8&id=0&lang=0", ""); got["pages"] != float64(1) {
		t.Fatalf("announcements: %v", got)
	}
	call(tokA, "POST", "user/accounts/deletion", "")
	call(tokA, "GET", "user/accounts/deletion", "")
	call(tokA, "DELETE", "user/accounts/deletion", "")
	if rec := do(s, "GET", "https://backend.innersloth.com/api/user/accounts/deletion", "", map[string]string{"Authorization": "Bearer " + tokA}); rec.Code != 404 {
		t.Fatalf("deletion status after cancel: %d, want 404", rec.Code)
	}
	if got := call(tokA, "GET", "user/blocks", "")["blocking"].([]any); len(got) != 0 {
		t.Fatalf("deleted account still blocks: %v", got)
	}
	if got := call(tokB, "GET", "user/friends/received-requests", "")["senders"].([]any); len(got) != 0 {
		t.Fatalf("deleted account's request kept: %v", got)
	}
	if got := call(tokB, "GET", "user/lobby/invites", "")["invites"].([]any); len(got) != 0 {
		t.Fatalf("deleted account's invite kept: %v", got)
	}
}

// Against a real Postgres (EOS_TEST_DATABASE_URL): a restarted eosd, with an empty memory and only the
// core (the fake graph) and Postgres behind it, still knows who is whose friend, resolves friend codes
// and keeps invites and the pending-deletion flag.
func TestSocialSurvivesRestart(t *testing.T) {
	url := os.Getenv("EOS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("EOS_TEST_DATABASE_URL not set")
	}
	const a, b, c = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1", "b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2", "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3"
	core := newFakeCore() // the account core: survives eosd restarts
	boot := func() (*server, map[string]string) {
		s := testServer(t)
		s.core = core
		s.startStore(context.Background(), url)
		if s.db.Load() == nil {
			t.Fatal("no store")
		}
		playsAmongUs(s, a, b, c)
		return s, map[string]string{
			a: s.signer.sign(map[string]any{"sub": a, "t": "connect"}),
			b: s.signer.sign(map[string]any{"sub": b, "t": "connect"}),
			c: s.signer.sign(map[string]any{"sub": c, "t": "connect"}),
		}
	}
	call := func(s *server, tok, method, path, body string) map[string]any {
		t.Helper()
		rec := do(s, method, "https://backend.innersloth.com/api/"+path, body, map[string]string{"Authorization": "Bearer " + tok})
		if rec.Code != 200 {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
		}
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out["data"].(map[string]any)["attributes"].(map[string]any)
	}

	one, tok := boot()
	for _, p := range []string{a, b, c} {
		call(one, tok[p], "GET", "user/username", "") // everyone is seen once
	}
	call(one, tok[a], "POST", "user/friends/request", `{"data":{"attributes":{"recipient_puid":"`+b+`"}}}`)
	call(one, tok[b], "PUT", "user/friends/request?sender_puid="+a, "")
	call(one, tok[a], "POST", "user/lobby/invites", `{"data":{"attributes":{"recipient_puid":"`+b+`","room_code":"ABCDEF"}}}`)
	call(one, tok[c], "POST", "user/accounts/deletion", "")

	two, tok := boot() // restart: empty memory, same Postgres, same core
	if got := call(two, tok[a], "GET", "user/friends", "")["friends"].([]any); len(got) != 1 ||
		got[0].(map[string]any)["friend_puid"] != b {
		t.Fatalf("friends after restart: %v", got)
	}
	if got := call(two, tok[b], "GET", "user/lobby/invites", "")["invites"].([]any); len(got) != 1 {
		t.Fatalf("invites after restart: %v", got)
	}
	if rec := do(two, "GET", "https://backend.innersloth.com/api/user/accounts/deletion", "", map[string]string{"Authorization": "Bearer " + tok[c]}); rec.Code != 200 {
		t.Fatalf("deletion flag after restart: %d", rec.Code)
	}
	// c never spoke to this process, but its friend code resolves from Postgres.
	call(two, tok[b], "POST", "user/friends/request-by-friendcode", `{"data":{"attributes":{"recipient_friendcode":"`+fullCode(c)+`"}}}`)
	if got := call(two, tok[c], "GET", "user/friends/received-requests", "")["senders"].([]any); len(got) != 1 {
		t.Fatalf("request by code after restart: %v", got)
	}
}
