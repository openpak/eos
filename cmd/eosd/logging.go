package main

// What reaches the log: shapes, never values. Every EOS request carries a credential or a
// person (client secrets in basic auth, Nintendo id_tokens, PUIDs in paths, display names in
// bodies), so a line names the method, the path with ids replaced by tags, the header names, and
// the body's keys with the length of each value. That is enough to write the next handler from.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"openpak/eos/redact"
)

// idSegment matches a path segment that is an id: 16+ hex digits (PUIDs, deployment, lobby and
// client ids), a JWT, or anything long and opaque.
var idSegment = regexp.MustCompile(`^([0-9a-fA-F-]{16,}|eyJ.*|[A-Za-z0-9_+=-]{24,})$`)

// safeQuery are query keys whose values are a fixed vocabulary.
var safeQuery = map[string]bool{"platformId": true, "platform": true, "type": true, "limit": true}

func logPath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if idSegment.MatchString(s) {
			segs[i] = "<" + redact.Tag(s) + ">"
		}
	}
	return strings.Join(segs, "/")
}

func logQuery(raw string) string {
	if raw == "" {
		return ""
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		return fmt.Sprintf("?<%d bytes>", len(raw))
	}
	return "?" + shapeValues(q)
}

func formKeys(v url.Values) string { return shapeValues(v) }

// shapeValues renders k=<len> for every key, k=value for safe keys and auth types.
func shapeValues(v url.Values) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		val := v.Get(k)
		if safeQuery[k] || k == "grant_type" || k == "external_auth_type" {
			parts = append(parts, k+"="+val)
		} else {
			parts = append(parts, fmt.Sprintf("%s=<%d>", k, len(val)))
		}
	}
	return strings.Join(parts, "&")
}

func headerNames(h http.Header) string {
	names := make([]string, 0, len(h))
	for k := range h {
		if k == "User-Agent" {
			// The SDK and title versions: no person in it.
			names = append(names, k+"="+h.Get(k))
			continue
		}
		names = append(names, k)
	}
	sort.Strings(names)
	return "[" + strings.Join(names, " ") + "]"
}

// bodyShape renders a body as its structure: JSON keys with value types and lengths, form keys
// with lengths, or just a size.
func bodyShape(ctype string, body []byte) string {
	if len(bytes.TrimSpace(body)) == 0 {
		return "<empty>"
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if dec.Decode(&v) == nil {
		out, _ := json.Marshal(shape(v, 0))
		s := string(out)
		if len(s) > 1500 {
			s = s[:1500] + "…"
		}
		return s
	}
	if strings.Contains(ctype, "form") {
		if q, err := url.ParseQuery(string(body)); err == nil {
			return "form{" + shapeValues(q) + "}"
		}
	}
	return fmt.Sprintf("<%d bytes %s>", len(body), ctype)
}

func shape(v any, depth int) any {
	if depth > 6 {
		return "…"
	}
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, x := range t {
			out[k] = shape(x, depth+1)
		}
		return out
	case []any:
		if len(t) == 0 {
			return []any{}
		}
		return []any{shape(t[0], depth+1), fmt.Sprintf("×%d", len(t))}
	case string:
		return fmt.Sprintf("str%d", len(t))
	case json.Number:
		return "num"
	case bool:
		return "bool"
	case nil:
		return "null"
	}
	return "?"
}
