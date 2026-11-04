package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const (
	acctA = "11111111-1111-4111-8111-111111111111"
	acctB = "22222222-2222-4222-8222-222222222222"
)

func seedErase(t *testing.T, s *server) {
	ctx := context.Background()
	s.acct.put(ctx, &acctRec{PUID: "pa", Account: acctA, Baas: "baasa", Code: "codea"})
	s.acct.put(ctx, &acctRec{PUID: "pb", Account: acctB, Baas: "baasb", Code: "codeb"})
	s.acct.addInvite(ctx, lobbyInvite{Sender: "pa", Recipient: "pb", Room: []byte(`"x"`)})
	s.acct.addInvite(ctx, lobbyInvite{Sender: "pb", Recipient: "pa", Room: []byte(`"y"`)})
	s.remember(kindPUID, "pa", "baasa")
	s.remember(kindEpic, "epica", "baasa")
	s.remember(kindPUID, "pb", "baasb")
}

func TestEraseRemovesOnlyThatAccount(t *testing.T) {
	s := testServer(t)
	if url := os.Getenv("EOS_TEST_DATABASE_URL"); url != "" {
		s.startStore(context.Background(), url)
		if _, err := s.Erase(context.Background(), acctA); err != nil { // clean slate
			t.Fatal(err)
		}
		if _, err := s.Erase(context.Background(), acctB); err != nil {
			t.Fatal(err)
		}
	}
	seedErase(t, s)
	n, err := s.Erase(context.Background(), acctA)
	if err != nil || n == 0 {
		t.Fatalf("erase: %d %v", n, err)
	}
	if s.acct.get(context.Background(), "pa") != nil {
		t.Fatal("account row kept")
	}
	if _, ok := s.owner(kindPUID, "pa"); ok {
		t.Fatal("puid owner kept")
	}
	if _, ok := s.owner(kindEpic, "epica"); ok {
		t.Fatal("epic owner kept")
	}
	if got := s.acct.invitesFor(context.Background(), "pa"); len(got) != 0 {
		t.Fatalf("received invites kept: %v", got)
	}
	if got := s.acct.invitesFor(context.Background(), "pb"); len(got) != 0 {
		t.Fatalf("sent invites kept: %v", got)
	}
	if s.acct.get(context.Background(), "pb") == nil {
		t.Fatal("other account erased")
	}
	if v, ok := s.owner(kindPUID, "pb"); !ok || v != "baasb" {
		t.Fatal("other owner erased")
	}
	n, err = s.Erase(context.Background(), acctA)
	if err != nil || n != 0 {
		t.Fatalf("second erase: %d %v", n, err)
	}
}

func TestEraseEndpoint(t *testing.T) {
	s := testServer(t)
	call := func(method, id, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/internal/accounts/"+id+"/data", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		http.HandlerFunc(s.eraseHandler).ServeHTTP(rec, req)
		return rec
	}
	t.Setenv("ACCOUNT_ERASE_KEY", "")
	if r := call("DELETE", acctA, "Bearer k"); r.Code != 503 {
		t.Fatalf("unset key: %d", r.Code)
	}
	t.Setenv("ACCOUNT_ERASE_KEY", "k")
	if r := call("DELETE", acctA, ""); r.Code != 401 {
		t.Fatalf("no key: %d", r.Code)
	}
	if r := call("DELETE", acctA, "Bearer bad"); r.Code != 401 {
		t.Fatalf("wrong key: %d", r.Code)
	}
	if r := call("DELETE", "not-a-uuid", "Bearer k"); r.Code != 400 {
		t.Fatalf("bad id: %d", r.Code)
	}
	seedErase(t, s)
	r := call("DELETE", acctA, "Bearer k")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"service":"eos"`) || !strings.Contains(r.Body.String(), `"rows":1`) {
		t.Fatalf("erase: %d %s", r.Code, r.Body.String())
	}
}
