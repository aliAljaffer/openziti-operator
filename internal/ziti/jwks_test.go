// SPDX-License-Identifier: Apache-2.0

package ziti

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"
)

func jwksFor(t *testing.T) ([]byte, *rsa.PrivateKey, *ecdsa.PrivateKey) {
	t.Helper()
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ek, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	enc := base64.RawURLEncoding.EncodeToString
	raw, _ := json.Marshal(map[string]any{"keys": []map[string]any{
		{"kid": "rsa-1", "kty": "RSA", "alg": "RS256", "n": enc(rk.N.Bytes()), "e": enc([]byte{1, 0, 1})},
		{"kid": "ec-1", "kty": "EC", "crv": "P-256", "x": enc(ek.X.Bytes()), "y": enc(ek.Y.Bytes())},
		{"kid": "", "kty": "RSA", "n": "AQAB", "e": "AQAB"},
		{"kid": "oct-1", "kty": "oct"},
	}})
	return raw, rk, ek
}

func TestParseJWKSReadsRSAAndECAndSkipsTheRest(t *testing.T) {
	raw, rk, ek := jwksFor(t)
	keys, err := ParseJWKS(raw)
	if err != nil || len(keys) != 2 {
		t.Fatalf("keys = %v, %v", keys, err)
	}
	if !keys[0].Key.(*rsa.PublicKey).Equal(&rk.PublicKey) || !keys[1].Key.(*ecdsa.PublicKey).Equal(&ek.PublicKey) {
		t.Error("parsed keys differ from the originals")
	}
	if _, err := ParseJWKS([]byte(`{"keys":[{"kid":"x","kty":"oct"}]}`)); err == nil {
		t.Error("a JWKS without usable keys must fail")
	}
	if _, err := ParseJWKS([]byte("not json")); err == nil {
		t.Error("bad JSON must fail")
	}
}

func TestKeyCertificateIsDeterministicAndCarriesTheKey(t *testing.T) {
	raw, rk, _ := jwksFor(t)
	keys, _ := ParseJWKS(raw)
	a, err := KeyCertificate("rsa-1", keys[0].Key)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := KeyCertificate("rsa-1", keys[0].Key)
	if a != b {
		t.Fatal("the same key must give the same certificate, or the signer updates on every sync")
	}
	if other, _ := KeyCertificate("rsa-2", keys[0].Key); other == a {
		t.Error("another kid must give another certificate")
	}
	block, _ := pem.Decode([]byte(a))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !cert.PublicKey.(*rsa.PublicKey).Equal(&rk.PublicKey) || cert.Subject.CommonName != "rsa-1" {
		t.Errorf("certificate = %v", cert.Subject)
	}
}

func TestTokenKid(t *testing.T) {
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"abc"}`))
	if got := TokenKid(head + ".payload.sig"); got != "abc" {
		t.Errorf("kid = %q", got)
	}
	for _, bad := range []string{"", "nodots", "!!!.x.y"} {
		if TokenKid(bad) != "" {
			t.Errorf("%q must give no kid", bad)
		}
	}
}
