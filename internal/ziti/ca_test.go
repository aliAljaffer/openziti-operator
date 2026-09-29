// SPDX-License-Identifier: Apache-2.0

package ziti

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func newCA(t *testing.T) (*x509.Certificate, string, *ecdsa.PrivateKey) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return cert, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), key
}

func TestProofCertificateIsSignedByTheCAAndCarriesTheToken(t *testing.T) {
	ca, _, key := newCA(t)
	proof, err := ProofCertificate("vuu.YiEeN", ca, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _, err := FirstCertificate([]byte(proof))
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Subject.CommonName != "vuu.YiEeN" {
		t.Errorf("CN = %q", leaf.Subject.CommonName)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Errorf("the proof does not verify against the CA: %v", err)
	}
}

func TestParsePrivateKeyReadsEveryCommonFormat(t *testing.T) {
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	sec1, _ := x509.MarshalECPrivateKey(ec)
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(rk)
	forms := map[string][]byte{
		"sec1":  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1}),
		"pkcs8": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}),
		"pkcs1": pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rk)}),
	}
	for name, raw := range forms {
		if k, err := ParsePrivateKey(raw); err != nil || k == nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, bad := range [][]byte{nil, []byte("not pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("junk")})} {
		if _, err := ParsePrivateKey(bad); err == nil {
			t.Error("a bad key must fail")
		}
	}
}

func TestFirstCertificateSkipsOtherBlocksAndTakesTheFirst(t *testing.T) {
	_, first, _ := newCA(t)
	_, second, _ := newCA(t)
	key := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("x")})
	cert, out, err := FirstCertificate([]byte(string(key) + first + second))
	if err != nil || out != first || cert == nil {
		t.Fatalf("got %v %v", out == first, err)
	}
	if _, _, err := FirstCertificate([]byte("nothing")); err == nil {
		t.Error("no certificate must fail")
	}
}
