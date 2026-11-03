package httplog

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func capture(t *testing.T) *bytes.Buffer {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(nil); log.SetFlags(log.LstdFlags) })
	return &buf
}

func TestMiddlewareLogsStatusDurationNoSecrets(t *testing.T) {
	buf := capture(t)
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte("secret-body"))
	}))
	r := httptest.NewRequest("POST", "/ac?authtoken=SEKRET&challenge=CHAL", strings.NewReader("password=hunter2"))
	r.Header.Set("Authorization", "Bearer TOKEN")
	r.AddCookie(&http.Cookie{Name: "s", Value: "COOKIE"})
	h.ServeHTTP(httptest.NewRecorder(), r)
	out := buf.String()
	if !regexp.MustCompile(`^http POST /ac -> 418 \d+ms\n$`).MatchString(out) {
		t.Fatalf("unexpected line %q", out)
	}
	for _, s := range []string{"SEKRET", "CHAL", "TOKEN", "COOKIE", "hunter2", "secret-body"} {
		if strings.Contains(out, s) {
			t.Fatalf("log leaks %q: %q", s, out)
		}
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("want one line: %q", out)
	}
}

func TestMiddlewareDefaultStatusAndHealthzSkipped(t *testing.T) {
	buf := capture(t)
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("x")) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/healthz", nil))
	if buf.Len() != 0 {
		t.Fatalf("healthz logged: %q", buf.String())
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	if !strings.HasPrefix(buf.String(), "http GET /x -> 200 ") {
		t.Fatalf("got %q", buf.String())
	}
}

func TestPathCannotForgeLines(t *testing.T) {
	buf := capture(t)
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/a%0Ahttp%20GET%20/forged", nil))
	if strings.Count(buf.String(), "\n") != 1 {
		t.Fatalf("forged line: %q", buf.String())
	}
}

func TestFlusherAndHijackerPreserved(t *testing.T) {
	capture(t)
	var flusher, hijack bool
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, flusher = w.(http.Flusher)
		_, hijack = w.(http.Hijacker)
		w.(http.Flusher).Flush()
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("controller flush: %v", err)
		}
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/s", nil))
	if !flusher || !hijack || !rec.Flushed {
		t.Fatalf("flusher=%v hijack=%v flushed=%v", flusher, hijack, rec.Flushed)
	}
}

func TestCommandLine(t *testing.T) {
	buf := capture(t)
	Command("gpcm", "login", "ok", time.Now().Add(-20*time.Millisecond))
	if !regexp.MustCompile(`^gpcm cmd login -> ok (\d+)ms\n$`).MatchString(buf.String()) {
		t.Fatalf("got %q", buf.String())
	}
	buf.Reset()
	Command("gpcm", "evil\nname with space", "ok", time.Now())
	if strings.Count(buf.String(), "\n") != 1 || strings.Contains(buf.String(), " with") {
		t.Fatalf("got %q", buf.String())
	}
}

func TestInternalPollsAreCountedNotLogged(t *testing.T) {
	buf := capture(t)
	pollCount.Store(0)
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fail") != "" {
			w.WriteHeader(500)
		}
	}))
	for i := 0; i < 3; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/internal/v1/gpcm/sessions?k=SEKRET", nil))
	}
	if buf.Len() != 0 {
		t.Fatalf("poll logged: %q", buf.String())
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/internal/v1/gpcm/sessions?fail=1", nil))
	if !strings.HasPrefix(buf.String(), "http POST /internal/v1/gpcm/sessions -> 500 ") {
		t.Fatalf("failure not logged: %q", buf.String())
	}
	buf.Reset()
	flushPolls()
	if buf.String() != "internal poll: 3 calls in last minute\n" {
		t.Fatalf("got %q", buf.String())
	}
	buf.Reset()
	flushPolls()
	if buf.Len() != 0 {
		t.Fatalf("empty summary logged: %q", buf.String())
	}
}

func TestPathFuncRedactsIds(t *testing.T) {
	buf := capture(t)
	PathFunc = func(p string) string { return strings.ReplaceAll(p, "0123456789abcdef", "<tag>") }
	t.Cleanup(func() { PathFunc = func(p string) string { return p } })
	Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/lobby/0123456789abcdef", nil))
	if strings.Contains(buf.String(), "0123456789abcdef") || !strings.Contains(buf.String(), "/lobby/<tag>") {
		t.Fatalf("got %q", buf.String())
	}
}
