package main

// Who the console is, asked of nx-baas rather than worked out here.
//
// The Nintendo id_token an EOS Connect login carries is a BAAS id_token OpenPak itself signed.
// /internal/switch/identity verifies it and also answers what a signature cannot: whether the
// account is banned, and "this player is online now", which the titles grid counts playtime from.
// Same call as platforms/switch/games/klei.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// ponytail: one client, no cache. A Connect login happens once per boot and once an hour.
var httpc = &http.Client{Timeout: 5 * time.Second}

// player is nx-baas's view of a console's owner (its /internal/switch/identity reply).
type player struct {
	PID        uint64 `json:"pid"`
	BaasUserID string `json:"baas_user_id"` // the NSA id titles see
	Nickname   string `json:"nickname"`
}

// errTokenRejected means nx-baas refused the token itself, as opposed to not being reachable.
var (
	errTokenRejected = fmt.Errorf("token rejected")
	errAccountBanned = fmt.Errorf("account banned")
)

type identityClient struct {
	url, key string
}

func newIdentityClient() identityClient {
	return identityClient{
		url: envOr("NX_INTERNAL_URL", "http://127.0.0.1:20070"), // nx-baas game/internal API (ports.md)
		key: os.Getenv("NX_INTERNAL_KEY"),
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func (c identityClient) byIDToken(tok string) (*player, error) {
	body, err := json.Marshal(map[string]any{"id_token": tok})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.url+"/internal/switch/identity", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Key", c.key)

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("nx-baas: %s: %w", resp.Status, errTokenRejected)
	case http.StatusForbidden:
		return nil, fmt.Errorf("nx-baas: %s: %w", resp.Status, errAccountBanned)
	case http.StatusOK:
	default:
		return nil, fmt.Errorf("nx-baas identity: %s", resp.Status)
	}
	var p player
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	if p.BaasUserID == "" {
		return nil, fmt.Errorf("nx-baas identity: incomplete reply")
	}
	return &p, nil
}
