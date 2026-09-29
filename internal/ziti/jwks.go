// SPDX-License-Identifier: Apache-2.0

package ziti

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// JWK is one public signing key from a JWKS document.
type JWK struct {
	Kid string
	Key crypto.PublicKey
}

// ParseJWKS reads the RSA and EC keys of a JWKS document. Other key types are skipped.
func ParseJWKS(raw []byte) ([]JWK, error) {
	var set struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Crv string `json:"crv"`
			N   string `json:"n"`
			E   string `json:"e"`
			X   string `json:"x"`
			Y   string `json:"y"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		return nil, fmt.Errorf("parse JWKS: %w", err)
	}
	b64 := func(s string) *big.Int {
		b, _ := base64.RawURLEncoding.DecodeString(s)
		return new(big.Int).SetBytes(b)
	}
	var out []JWK
	for _, k := range set.Keys {
		if k.Kid == "" {
			continue
		}
		switch k.Kty {
		case "RSA":
			if k.N == "" || k.E == "" {
				continue
			}
			out = append(out, JWK{Kid: k.Kid, Key: &rsa.PublicKey{N: b64(k.N), E: int(b64(k.E).Int64())}})
		case "EC":
			curve := map[string]elliptic.Curve{"P-256": elliptic.P256(), "P-384": elliptic.P384(), "P-521": elliptic.P521()}[k.Crv]
			if curve == nil {
				continue
			}
			pub := &ecdsa.PublicKey{Curve: curve, X: b64(k.X), Y: b64(k.Y)}
			if !curve.IsOnCurve(pub.X, pub.Y) {
				continue
			}
			out = append(out, JWK{Kid: k.Kid, Key: pub})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("the JWKS has no usable RSA or EC keys with a kid")
	}
	return out, nil
}

// KeyCertificate wraps a public key in a self-signed certificate, because Ziti takes signer keys as certificates.
// The result is the same for the same kid and key, so an unchanged key never causes an update.
func KeyCertificate(kid string, pub crypto.PublicKey) (string, error) {
	seed := sha256.Sum256([]byte("ziti-operator/jwks/" + kid))
	signer := ed25519.NewKeyFromSeed(seed[:])
	tmpl := &x509.Certificate{
		SerialNumber:          new(big.Int).SetBytes(seed[:8]),
		Subject:               pkix.Name{CommonName: kid},
		NotBefore:             time.Unix(1577836800, 0), // 2020-01-01
		NotAfter:              time.Unix(4102444800, 0), // 2100-01-01
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, signer)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), nil
}

// TokenKid returns the kid in the header of a JWT, or an empty string.
func TokenKid(token string) string {
	var head string
	for i, c := range token {
		if c == '.' {
			head = token[:i]
			break
		}
	}
	raw, err := base64.RawURLEncoding.DecodeString(head)
	if err != nil {
		return ""
	}
	var h struct {
		Kid string `json:"kid"`
	}
	_ = json.Unmarshal(raw, &h)
	return h.Kid
}
