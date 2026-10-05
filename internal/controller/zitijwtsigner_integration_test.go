//go:build integration

package controller

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/openziti/edge-api/rest_util"
	ctrl "sigs.k8s.io/controller-runtime"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	enc := base64.RawURLEncoding.EncodeToString
	head, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid})
	body, _ := json.Marshal(claims)
	signing := enc(head) + "." + enc(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + enc(sig)
}

func TestSignerAndTokenLoginAgainstRealController(t *testing.T) {
	mgmt := os.Getenv("ZITI_MGMT_URL")
	if mgmt == "" {
		t.Skip("ZITI_MGMT_URL not set")
	}
	u, _ := url.Parse(mgmt)
	pool, err := rest_util.GetControllerWellKnownCaPool("https://" + u.Host)
	if err != nil {
		t.Fatal(err)
	}
	auth := rest_util.NewAuthenticatorUpdb(os.Getenv("ZITI_USERNAME"), os.Getenv("ZITI_PASSWORD"))
	auth.RootCas = pool
	real, err := ziti.NewREST(mgmt, auth, 10)
	if err != nil {
		t.Fatal(err)
	}
	wc := &writeCounter{Client: real}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	jwks, _ := json.Marshal(map[string]any{"keys": []map[string]any{{"kid": "it-key", "kty": "RSA", "n": enc(key.N.Bytes()), "e": enc([]byte{1, 0, 1})}}})
	const issuer = "https://integration-test.invalid"

	se := setupSigner(t, func(sg *zitiv1.ZitiJwtSigner) { sg.Spec.ZitiName = "it-signer" }, jwksSet{raw: jwks}, "")
	se.keys.issuer = issuer
	se.r.Clients = staticProvider{wc}
	t.Cleanup(func() {
		ctx := context.Background()
		var sg zitiv1.ZitiJwtSigner
		if se.k.Get(ctx, se.key, &sg) == nil {
			_ = se.k.Delete(ctx, &sg)
			_, _ = se.r.Reconcile(ctx, ctrl.Request{NamespacedName: se.key})
		}
	})

	sg := se.reconcile(t)
	if sg.Status.SignerID == "" || sg.Status.AuthPolicyID == "" || sg.Status.KeyID != "it-key" {
		t.Fatalf("status = %+v", sg.Status)
	}
	wc.writes = nil
	se.reconcile(t)
	if len(wc.writes) != 0 {
		t.Fatalf("second reconcile wrote: %v", wc.writes)
	}

	ie := setupIdentity(t)
	ie.r.Clients = staticProvider{wc}
	setSpec(t, ie, func(s *zitiv1.ZitiIdentitySpec) {
		s.EnrollmentMode, s.AuthPolicy, s.ServiceAccount = zitiv1.EnrollmentNone, "it-signer", "web"
	})
	t.Cleanup(func() {
		ctx := context.Background()
		var cr zitiv1.ZitiIdentity
		if ie.k.Get(ctx, ie.key, &cr) == nil {
			_ = ie.k.Delete(ctx, &cr)
			_, _ = ie.r.Reconcile(ctx, ctrl.Request{NamespacedName: ie.key})
		}
	})
	ie.reconcile(t)
	if z := ie.get(t); z.Status.ZitiID == "" || findCond(z, CondReady).Reason != "TokenLogin" {
		t.Fatalf("identity status = %+v", z.Status)
	}
	wc.writes = nil
	ie.reconcile(t)
	if len(wc.writes) != 0 {
		t.Fatalf("identity second reconcile wrote: %v", wc.writes)
	}

	login := func(token string) (int, string) {
		hc := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
		req, _ := http.NewRequest(http.MethodPost, "https://"+u.Host+"/edge/client/v1/authenticate?method=ext-jwt", bytes.NewReader([]byte("{}")))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Data struct {
				Identity struct{ Name string } `json:"identity"`
			} `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.Data.Identity.Name
	}
	claims := func(aud, sub string) map[string]any {
		return map[string]any{"iss": issuer, "aud": aud, "sub": sub, "exp": time.Now().Add(10 * time.Minute).Unix(), "iat": time.Now().Unix()}
	}
	if code, name := login(signRS256(t, key, "it-key", claims("ziti", "system:serviceaccount:team-a:web"))); code != http.StatusOK || name != "default-team-a-backend" {
		t.Errorf("valid token: status %d, identity %q", code, name)
	}
	if code, _ := login(signRS256(t, key, "it-key", claims("other", "system:serviceaccount:team-a:web"))); code == http.StatusOK {
		t.Error("a token with another audience must be rejected")
	}
	if code, _ := login(signRS256(t, key, "it-key", claims("ziti", "system:serviceaccount:team-b:web"))); code == http.StatusOK {
		t.Error("a token of another service account must be rejected")
	}
}
