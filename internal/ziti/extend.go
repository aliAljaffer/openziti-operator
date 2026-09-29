// SPDX-License-Identifier: Apache-2.0

package ziti

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type identityFile struct {
	ZtAPI string `json:"ztAPI"`
	ID    struct {
		Key  string `json:"key"`
		Cert string `json:"cert"`
		CA   string `json:"ca"`
	} `json:"id"`
}

func parseIdentityFile(raw []byte) (*identityFile, error) {
	var f identityFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	for _, v := range []string{f.ID.Key, f.ID.Cert} {
		if !strings.HasPrefix(v, "pem:") {
			return nil, errors.New("identity file must hold pem: key and cert")
		}
	}
	return &f, nil
}

// CertValidity returns the validity period of the client certificate in an identity.json.
func CertValidity(identityJSON []byte) (notBefore, notAfter time.Time, err error) {
	f, err := parseIdentityFile(identityJSON)
	if err != nil {
		return
	}
	block, _ := pem.Decode([]byte(strings.TrimPrefix(f.ID.Cert, "pem:")))
	if block == nil {
		err = errors.New("no certificate in identity file")
		return
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return
	}
	return cert.NotBefore, cert.NotAfter, nil
}

// ExtendCert asks the controller for a new client certificate for the cert authenticator authenticatorID.
// It authenticates with the identity's current certificate, uses a new private key, and returns the updated identity.json.
func ExtendCert(identityJSON []byte, authenticatorID string) ([]byte, error) {
	f, err := parseIdentityFile(identityJSON)
	if err != nil {
		return nil, err
	}
	cur, err := tls.X509KeyPair([]byte(strings.TrimPrefix(f.ID.Cert, "pem:")), []byte(strings.TrimPrefix(f.ID.Key, "pem:")))
	if err != nil {
		return nil, fmt.Errorf("identity file key pair: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(strings.TrimPrefix(f.ID.CA, "pem:"))) {
		return nil, errors.New("identity file has no CA certificates")
	}
	hc := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{cur}, RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
	base := strings.TrimRight(f.ZtAPI, "/")

	call := func(path, session string, body any, out any) error {
		payload, _ := json.Marshal(body)
		req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if session != "" {
			req.Header.Set("zt-session", session)
		}
		resp, err := hc.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			return &APIError{Status: resp.StatusCode, Code: "EXTEND", Message: path}
		}
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			return err
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(env.Data, out)
	}

	var sess struct {
		Token string `json:"token"`
	}
	if err := call("/authenticate?method=cert", "", map[string]any{}, &sess); err != nil {
		return nil, fmt.Errorf("authenticate with certificate: %w", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, err
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "ziti-operator"}}, key)
	if err != nil {
		return nil, err
	}
	csr := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))

	var ext struct {
		ClientCert string `json:"clientCert"`
		CA         string `json:"ca"`
	}
	if err := call("/current-identity/authenticators/"+authenticatorID+"/extend", sess.Token, map[string]any{"clientCertCsr": csr}, &ext); err != nil {
		return nil, fmt.Errorf("extend certificate: %w", err)
	}
	if err := call("/current-identity/authenticators/"+authenticatorID+"/extend-verify", sess.Token, map[string]any{"clientCert": ext.ClientCert}, nil); err != nil {
		return nil, fmt.Errorf("verify extended certificate: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	var rest map[string]json.RawMessage
	_ = json.Unmarshal(identityJSON, &rest)
	id := map[string]string{}
	_ = json.Unmarshal(rest["id"], &id)
	id["key"] = "pem:" + string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	id["cert"] = "pem:" + ext.ClientCert
	rest["id"], _ = json.Marshal(id)
	return json.MarshalIndent(rest, "", "  ")
}
