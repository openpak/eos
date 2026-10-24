package main

import (
	"encoding/json"
	"testing"
)

// The search TMNT sends (real console, 2026-10-24).
const tmntSearch = `{"criteria":[{"key":"attributes.VERSION_l","op":"EQUAL","value":8},` +
	`{"key":"attributes.ONLINEGAMEPRESENCE_l","op":"EQUAL","value":3},` +
	`{"key":"bucket","op":"CONTAINS","value":"NintendoSwitch"},` +
	`{"key":"attributes.GAMETYPE_l","op":"NOT_EQUAL","value":1},` +
	`{"key":"attributes.SLOTS_l","op":"GREATER_THAN_OR_EQUAL","value":1}],"maxResults":5}`

const dep = "0123456789abcdef0123456789abcdef"

func search(t *testing.T, s *server, token string) map[string]any {
	t.Helper()
	rec := do(s, "POST", "https://api.epicgames.dev/lobby/v1/"+dep+"/lobbies/filter", tmntSearch,
		map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"})
	if rec.Code != 200 {
		t.Fatalf("filter: %d %s", rec.Code, rec.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLobbySearchEmpty(t *testing.T) {
	s := testServer(t)
	tok := connectLogin(t, s)["access_token"].(string)
	out := search(t, s, tok)
	if out["count"] != 0.0 || out["sessions"] == nil || len(out["sessions"].([]any)) != 0 {
		t.Fatalf("empty search = %v, want sessions [] count 0", out)
	}
	rec := do(s, "POST", "https://api.epicgames.dev/lobby/v1/"+dep+"/lobbies/filter", tmntSearch, nil)
	if rec.Code == 200 {
		t.Fatal("search without a Connect token answered 200")
	}
}

func TestLobbySearchMatchesCriteria(t *testing.T) {
	s := testServer(t)
	tok := connectLogin(t, s)["access_token"].(string)
	mk := func(id string, version, gametype, slots float64, advertise bool) *lobby {
		return &lobby{Deployment: dep, ID: id, Bucket: "NintendoSwitch:EU", Owner: "owner" + id,
			Settings:      lobbySettings{MaxPublicPlayers: 6, ShouldAdvertise: advertise, RejoinAfterKick: "open"},
			Attributes:    map[string]any{"VERSION_l": version, "ONLINEGAMEPRESENCE_l": 3.0, "GAMETYPE_l": gametype, "SLOTS_l": slots},
			PublicPlayers: []string{"owner" + id}, TotalPlayers: 1, OpenPublicPlayers: 5, LastUpdated: "2026-10-24T12:00:0" + id + ".000Z"}
	}
	for _, l := range []*lobby{mk("1", 8, 0, 3, true), mk("2", 7, 0, 3, true), mk("3", 8, 1, 3, true),
		mk("4", 8, 0, 0, true), mk("5", 8, 0, 2, false), mk("6", 8, 2, 1, true)} {
		s.lobbies.lobbies[l.ID] = l
	}
	out := search(t, s, tok)
	got := out["sessions"].([]any)
	if out["count"] != 2.0 || len(got) != 2 {
		t.Fatalf("search = %v, want lobbies 6 and 1", out)
	}
	first := got[0].(map[string]any)
	if first["id"] != "6" || got[1].(map[string]any)["id"] != "1" {
		t.Fatalf("order = %v, %v; want newest first", first["id"], got[1].(map[string]any)["id"])
	}
	for _, k := range []string{"deployment", "id", "bucket", "settings", "totalPlayers", "openPublicPlayers",
		"publicPlayers", "started", "lastUpdated", "attributes", "owner"} {
		if _, ok := first[k]; !ok {
			t.Errorf("session lacks %q, which the SDK requires", k)
		}
	}
}
