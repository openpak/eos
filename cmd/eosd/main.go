// Command eosd answers the Epic Online Services SDK inside Switch titles once the console's DNS
// points api.epicgames.dev at OpenPak: Among Us, TMNT: Shredder's Revenge and Fall Guys.
//
// The SDK's first request is the SDK config document, its service map; serving our own document
// is what decides that every later call comes back to this host. After it come the OAuth token
// grants: client credentials for the title itself, then Connect login on the console's Nintendo
// NSA id_token, which nx-baas minted and therefore verifies.
//
// Plain HTTP inside the pod network; Traefik terminates the console's TLS for the EOS names and
// forwards the stream, the same arrangement as platforms/switch/games/klei and servers/t17.
//
// What the SDK sends was measured, not assumed: docs/protocol.md, kept in step with this code.
package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	addr       = flag.String("addr", ":22620", "HTTP listen address (Traefik terminates TLS in front)")
	healthAddr = flag.String("health-addr", ":22621", "health listen address")
	idSecret   = flag.String("id-secret", os.Getenv("EOS_ID_SECRET"),
		"HMAC secret product user ids are derived with; must stay the same across restarts")
	keyFile = flag.String("signing-key", os.Getenv("EOS_SIGNING_KEY_FILE"),
		"PEM RSA key the access and id tokens are signed with; empty means one random per boot")
)

func main() {
	flag.Parse()
	signer, err := loadSigner(*keyFile)
	if err != nil {
		log.Fatalf("[eos] signing key: %v", err)
	}
	if *idSecret == "" {
		log.Printf("[eos] EOS_ID_SECRET is empty: product user ids will change on every restart")
		*idSecret = randomHex(32)
	}
	s := newServer(newIdentityClient(), signer, []byte(*idSecret))

	go func() {
		h := http.NewServeMux()
		h.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, map[string]any{"status": "ok"})
		})
		log.Printf("[eos] health on %s", *healthAddr)
		log.Fatal(http.ListenAndServe(*healthAddr, h))
	}()

	log.Printf("[eos] serving the EOS API on %s", *addr)
	srv := &http.Server{Addr: *addr, Handler: s, ReadHeaderTimeout: 30 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

type server struct {
	ids    identityClient
	signer *signer
	secret []byte
	social *social
	// epicNames: Epic account id -> console nickname, learned at EOS Auth login.
	epicNames sync.Map
}

func newServer(ids identityClient, sg *signer, secret []byte) *server {
	return &server{ids: ids, signer: sg, secret: secret, social: newSocial()}
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if host, _, _ := strings.Cut(r.Host, ":"); host == "backend.innersloth.com" {
		s.innersloth(w, r)
		return
	}
	rec := &recorder{ResponseWriter: w, status: 200}
	start := time.Now()
	handled := s.route(rec, r)
	if handled {
		log.Printf("[eos] %s %s%s -> %d (%s)", r.Method, logPath(r.URL.Path), logQuery(r.URL.RawQuery),
			rec.status, time.Since(start).Round(time.Millisecond))
	}
}

// route dispatches one request; false means it went to the catch-all, which logs on its own.
func (s *server) route(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	if kind := socketKind(p); kind != "" {
		s.socket(w, r, kind)
		return true
	}
	switch {
	case strings.HasPrefix(p, "/sdk/v1/"):
		s.sdkConfig(w, r)
	case p == "/auth/v1/oauth/token":
		s.token(w, r)
	case p == "/auth/v1/oauth/jwks" || p == "/auth/v1/oauth/.well-known/jwks.json":
		writeJSON(w, 200, s.signer.jwks())
	case p == "/epic/oauth/v2/token":
		s.epicToken(w, r)
	case p == "/epic/id/v2/sdk/accounts":
		s.epicAccounts(w, r)
	case p == "/epic/oauth/v2/revoke":
		s.epicRevoke(w, r)
	case p == "/epic/oauth/v2/tokenInfo":
		s.epicTokenInfo(w, r)
	case p == "/epic/oauth/v2/.well-known/jwks.json":
		writeJSON(w, 200, s.signer.jwks())
	case p == "/auth/v1/accounts/deviceid":
		s.createDeviceID(w, r)
	case strings.HasPrefix(p, "/kws/"):
		s.kws(w, r)
	case strings.HasPrefix(p, "/telemetry/") || strings.HasPrefix(p, "/datarouter/"):
		// SDK analytics. Nothing waits on it; read and drop.
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		w.WriteHeader(http.StatusNoContent)
		return false // too frequent to log
	default:
		if s.epicSocial(w, r) {
			return true
		}
		s.unhandled(w, r)
		return false
	}
	return true
}

// unhandled logs a request nothing answers yet: method, path, header names and the body's keys,
// never a value. Every line here is the specification of the next handler.
func (s *server) unhandled(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	log.Printf("[eos] !! unhandled %s %s %s%s headers=%s body=%s", r.Method, r.Host, logPath(r.URL.Path),
		logQuery(r.URL.RawQuery), headerNames(r.Header), bodyShape(r.Header.Get("Content-Type"), body))
	if trace {
		log.Printf("[eos] !! unhandled body %s", body)
	}
	writeError(w, 404, "errors.com.epicgames.common.not_found", "not found")
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) { r.status = code; r.ResponseWriter.WriteHeader(code) }

// Unwrap lets http.ResponseController reach the connection (a websocket upgrade).
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError answers in the shape Epic's services use for errors.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"errorCode":          code,
		"errorMessage":       msg,
		"numericErrorCode":   status,
		"originatingService": "openpak-eos",
		"intent":             "prod",
	})
}
