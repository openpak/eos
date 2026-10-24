package main

// The websockets the SDK opens after login, routed by path:
//
//   - /lobby/v1/<deployment>/lobbies/connect: the lobby socket (lobby.go)
//   - /notifications/v1/<deployment>/connect: Stomp notifications
//   - /epic-connect[/v2]: Epic Connect messaging, which carries P2P signalling
//
// Until a socket's messages are measured it only logs what the client sends: the message's
// `name`/`type` (a fixed vocabulary) and the shape of the rest.

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"openpak/eos/redact"
)

// trace logs whole frames and bodies. They carry PUIDs, display names and game data, so it is
// off unless EOS_TRACE=1, which is for test accounts on a development run only.
var trace = os.Getenv("EOS_TRACE") == "1"

// socketKind names the websocket a path asks for, or "" when it is not one of ours.
func socketKind(p string) string {
	switch {
	case strings.HasPrefix(p, "/lobby/v1/") && strings.HasSuffix(p, "/lobbies/connect"):
		return "lobby"
	case strings.HasPrefix(p, "/notifications/v1/") && strings.HasSuffix(p, "/connect"):
		return "stomp"
	case strings.HasPrefix(p, "/epic-connect"):
		return "epic-connect"
	}
	return ""
}

func (s *server) socket(w http.ResponseWriter, r *http.Request, kind string) {
	user := s.user(r)
	log.Printf("[eos] %s socket: upgrade %s user=%s headers=%s", kind, logPath(r.URL.Path),
		redact.Tag(user), headerNames(r.Header))
	c, err := upgrade(w, r)
	if err != nil {
		log.Printf("[eos] %s socket: %s", kind, redact.Err(err))
		writeError(w, 400, "errors.com.epicgames.common.bad_request", "websocket upgrade required")
		return
	}
	defer c.Close()
	start := time.Now()
	for {
		op, p, err := c.next(45 * time.Second)
		if err != nil {
			log.Printf("[eos] %s socket: closed after %s (%s)", kind, time.Since(start).Round(time.Second), redact.Err(err))
			return
		}
		log.Printf("[eos] %s socket <<< op=%d %s", kind, op, frameShape(p))
		if trace {
			log.Printf("[eos] %s socket <<< %s", kind, p)
		}
	}
}

// frameShape is a frame's message name and the shape of its body.
func frameShape(p []byte) string {
	var m map[string]any
	if json.Unmarshal(p, &m) != nil {
		// Stomp: the command line is a fixed vocabulary (CONNECT, SUBSCRIBE, SEND...).
		if cmd, _, _ := strings.Cut(string(p), "\n"); len(cmd) <= 16 && cmd == strings.ToUpper(cmd) {
			return "stomp=" + strings.TrimSpace(cmd) + " " + bodyShape("", p)
		}
		return bodyShape("", p)
	}
	label := ""
	for _, k := range []string{"name", "type", "command"} {
		if v, ok := m[k].(string); ok {
			label += k + "=" + v + " "
		}
	}
	return label + bodyShape("application/json", p)
}
