package main

// The tokens eosd mints: JWTs signed RS256 with one key, published at /auth/v1/oauth/jwks so the
// SDK (or a title's own server) can check an id_token the way it checks Epic's. Only eosd reads
// the access tokens back, but they are JWTs too because the SDK reads claims out of them.

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"
)

type signer struct {
	key *rsa.PrivateKey
	kid string
}

func loadSigner(path string) (*signer, error) {
	var key *rsa.PrivateKey
	if path == "" {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, err
		}
		key = k
	} else {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		blk, _ := pem.Decode(raw)
		if blk == nil {
			return nil, errors.New("no PEM block")
		}
		if k, err := x509.ParsePKCS1PrivateKey(blk.Bytes); err == nil {
			key = k
		} else {
			anyKey, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
			if err != nil {
				return nil, err
			}
			k, ok := anyKey.(*rsa.PrivateKey)
			if !ok {
				return nil, errors.New("not an RSA key")
			}
			key = k
		}
	}
	sum := sha256.Sum256(key.PublicKey.N.Bytes())
	return &signer{key: key, kid: hex.EncodeToString(sum[:8])}, nil
}

var b64 = base64.RawURLEncoding

func (s *signer) sign(claims map[string]any) string {
	hdr, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": s.kid})
	body, _ := json.Marshal(claims)
	in := b64.EncodeToString(hdr) + "." + b64.EncodeToString(body)
	sum := sha256.Sum256([]byte(in))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, sum[:])
	if err != nil {
		panic(err) // only on a broken key, which loadSigner refuses
	}
	return in + "." + b64.EncodeToString(sig)
}

// verify checks a token this signer minted and returns its claims; expiry is checked too.
func (s *signer) verify(tok string) (map[string]any, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, errors.New("not a JWT")
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&s.key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		return nil, errors.New("bad signature")
	}
	raw, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var c map[string]any
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	if exp, ok := c["exp"].(float64); ok && time.Now().Unix() > int64(exp) {
		return nil, errors.New("expired")
	}
	return c, nil
}

func (s *signer) jwks() map[string]any {
	pub := s.key.PublicKey
	return map[string]any{"keys": []any{map[string]any{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": s.kid,
		"n": b64.EncodeToString(pub.N.Bytes()),
		"e": b64.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}}
}

func randomHex(n int) string {
	b := make([]byte, n/2)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// bearer returns the token of an "Authorization: Bearer x" header.
func bearer(h string) (string, error) {
	tok, ok := strings.CutPrefix(h, "Bearer ")
	if !ok {
		tok, ok = strings.CutPrefix(h, "bearer ")
	}
	if !ok || tok == "" {
		return "", fmt.Errorf("no bearer token")
	}
	return tok, nil
}
