//go:build integration

package ziti

import (
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/openziti/edge-api/rest_util"
)

func TestExtendCertAgainstRealController(t *testing.T) {
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
	c, err := NewREST(mgmt, auth, 10)
	if err != nil {
		t.Fatal(err)
	}

	id, err := c.Create(t.Context(), Identities, Entity{"name": "extend-test", "type": "Default", "isAdmin": false, "enrollment": map[string]any{"ott": true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Delete(t.Context(), Identities, id) })
	ens, err := c.List(t.Context(), Enrollments, `identity="`+id+`"`)
	if err != nil || len(ens) != 1 {
		t.Fatalf("enrollments = %v, %v", ens, err)
	}
	file, err := EnrollOTT(ens[0]["jwt"].(string))
	if err != nil {
		t.Fatal(err)
	}
	_, before, err := CertValidity(file)
	if err != nil {
		t.Fatal(err)
	}

	auths, err := c.List(t.Context(), Authenticators, `identity="`+id+`"`)
	if err != nil || len(auths) != 1 {
		t.Fatalf("authenticators = %v, %v", auths, err)
	}
	time.Sleep(2 * time.Second)
	next, err := ExtendCert(file, auths[0].ID())
	if err != nil {
		t.Fatal(err)
	}
	_, after, err := CertValidity(next)
	if err != nil {
		t.Fatal(err)
	}
	if !after.After(before) {
		t.Errorf("notAfter did not move: before %v after %v", before, after)
	}
	// the new file must work: extend again with it
	if _, err := ExtendCert(next, auths[0].ID()); err != nil {
		t.Fatalf("new identity file does not authenticate: %v", err)
	}
	// informational: does the old certificate still work?
	if _, err := ExtendCert(file, auths[0].ID()); err != nil {
		t.Logf("old certificate rejected after extend: %v", err)
	} else {
		t.Log("old certificate still accepted after extend")
	}
}
