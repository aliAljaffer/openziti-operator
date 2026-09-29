//go:build integration

package controller

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/openziti/edge-api/rest_util"
	corev1 "k8s.io/api/core/v1"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func TestCertAuthAgainstRealController(t *testing.T) {
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
	admin, err := ziti.NewREST(mgmt, auth, 10)
	if err != nil {
		t.Fatal(err)
	}

	id, err := admin.Create(t.Context(), ziti.Identities, ziti.Entity{"name": "operator-cert", "type": "Default", "isAdmin": true, "enrollment": map[string]any{"ott": true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Delete(context.Background(), ziti.Identities, id) })
	ens, err := admin.List(t.Context(), ziti.Enrollments, `identity="`+id+`"`)
	if err != nil || len(ens) != 1 {
		t.Fatalf("enrollments = %v %v", ens, err)
	}
	file, err := ziti.EnrollOTT(ens[0]["jwt"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		ID struct{ Key, Cert, CA string } `json:"id"`
	}
	if err := json.Unmarshal(file, &f); err != nil {
		t.Fatal(err)
	}

	secret := &corev1.Secret{Data: map[string][]byte{
		corev1.TLSCertKey:       []byte(strings.TrimPrefix(f.ID.Cert, "pem:")),
		corev1.TLSPrivateKeyKey: []byte(strings.TrimPrefix(f.ID.Key, "pem:")),
	}}
	secret.Name, secret.Namespace = "cred", "ns"
	p := providerFor(t, secret, strings.TrimPrefix(f.ID.CA, "pem:"))
	conn := connWith(zitiv1.ConnectionAuth{Cert: &zitiv1.CertAuth{SecretRef: zitiv1.SecretRef{Namespace: "ns", Name: "cred"}}})
	conn.Spec.ManagementURL = mgmt

	c, err := p.For(t.Context(), conn)
	if err != nil {
		t.Fatal(err)
	}
	version, err := c.Version(t.Context())
	if err != nil || version == "" {
		t.Fatalf("version = %q, %v", version, err)
	}
	if list, err := c.List(t.Context(), ziti.EdgeRouters, ""); err != nil || len(list) == 0 {
		t.Fatalf("admin call with certificate failed: %v %v", list, err)
	}
}
