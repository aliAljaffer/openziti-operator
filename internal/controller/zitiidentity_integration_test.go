//go:build integration

// Usage: ZITI_MGMT_URL=https://host:port/edge/management/v1 ZITI_USERNAME=... ZITI_PASSWORD=... \
//
//	go test -tags integration -run RealController ./internal/controller/
package controller

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/url"
	"os"
	"testing"

	"github.com/openziti/edge-api/rest_util"
	ctrl "sigs.k8s.io/controller-runtime"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func TestIdentityAgainstRealController(t *testing.T) {
	mgmt := os.Getenv("ZITI_MGMT_URL")
	if mgmt == "" {
		t.Skip("ZITI_MGMT_URL not set")
	}
	u, _ := url.Parse(mgmt)
	var pool *x509.CertPool
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

	e := setupIdentity(t)
	e.r.Clients = staticProvider{real}
	e.reconcile(t)
	z := e.get(t)
	if z.Status.ZitiID == "" || z.Status.Enrolled || z.Status.EnrollmentExpiresAt == nil || len(e.secret(t).Data[SecretKeyJWT]) < 20 {
		t.Fatalf("after create: %+v", z.Status)
	}
	e.reconcile(t)

	z = e.get(t)
	z.Spec.RoleAttributes = []string{"web", "db"}
	if err := e.k.Update(t.Context(), z); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	got, err := real.List(t.Context(), ziti.Identities, tagFilter(z.UID))
	if err != nil || len(got) != 1 || len(got[0]["roleAttributes"].([]any)) != 2 {
		t.Fatalf("update: %v %v", got, err)
	}

	if err := e.k.Delete(t.Context(), e.get(t)); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	if got, _ := real.List(t.Context(), ziti.Identities, tagFilter(z.UID)); len(got) != 0 {
		t.Fatalf("identity not deleted: %v", got)
	}
}

func TestOperatorEnrolledAgainstRealController(t *testing.T) {
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

	e := setupIdentity(t)
	e.r.Clients = staticProvider{real}
	z := e.get(t)
	z.Spec.EnrollmentMode = zitiv1.EnrollmentOperator
	if err := e.k.Update(t.Context(), z); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	t.Cleanup(func() {
		ctx := context.Background()
		var cr zitiv1.ZitiIdentity
		_ = e.k.Get(ctx, e.key, &cr)
		_ = e.k.Delete(ctx, &cr)
		_, _ = e.r.Reconcile(ctx, ctrl.Request{NamespacedName: e.key})
	})

	z = e.get(t)
	file := e.secret(t).Data[SecretKeyIdentity]
	var parsed struct {
		ZtAPI string                         `json:"ztAPI"`
		ID    struct{ Key, Cert, CA string } `json:"id"`
	}
	if err := json.Unmarshal(file, &parsed); err != nil || parsed.ZtAPI == "" || parsed.ID.Key == "" || parsed.ID.Cert == "" {
		t.Fatalf("identity.json = %s (%v)", file, err)
	}
	if !z.Status.Enrolled {
		t.Fatalf("status = %+v", z.Status)
	}
	e.reconcile(t)
	got, _ := real.List(t.Context(), ziti.Identities, tagFilter(z.UID))
	if len(got) != 1 || len(got[0]["authenticators"].(map[string]any)) == 0 {
		t.Fatalf("ziti identity not enrolled: %v", got)
	}
}
