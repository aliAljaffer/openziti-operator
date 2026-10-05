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
	"time"

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

	wc := &writeCounter{Client: real}
	e := setupIdentity(t)
	e.r.Clients = staticProvider{wc}
	e.reconcile(t)
	z := e.get(t)
	if z.Status.ZitiID == "" || z.Status.Enrolled || z.Status.EnrollmentExpiresAt == nil || len(e.secret(t).Data[SecretKeyJWT]) < 20 {
		t.Fatalf("after create: %+v", z.Status)
	}
	wc.writes = nil
	e.reconcile(t)
	if len(wc.writes) != 0 {
		t.Fatalf("second reconcile wrote: %v", wc.writes)
	}

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
	if z.Status.CertNotAfter == nil || time.Until(z.Status.CertNotAfter.Time) < 300*24*time.Hour {
		t.Fatalf("certNotAfter = %v", z.Status.CertNotAfter)
	}
	if condStatusOf(z, CondCertValid) != "True" {
		t.Fatalf("CertificateValid = %+v", z.Status.Conditions)
	}
	e.reconcile(t)
	got, _ := real.List(t.Context(), ziti.Identities, tagFilter(z.UID))
	if len(got) != 1 || len(got[0]["authenticators"].(map[string]any)) == 0 {
		t.Fatalf("ziti identity not enrolled: %v", got)
	}
}

func TestAdoptAgainstRealController(t *testing.T) {
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

	zid, err := real.Create(t.Context(), ziti.Identities, ziti.Entity{
		"name": "default-team-a-backend", "type": "Default", "isAdmin": false, "roleAttributes": []string{"old"},
		"externalId": "ext-adopt-test", "appData": map[string]any{"k": "v"}, "tags": map[string]any{"owner": "human"},
		"enrollment": map[string]any{"ott": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = real.Delete(context.Background(), ziti.Identities, zid) })

	e := setupIdentity(t)
	e.r.Clients = staticProvider{real}
	setPolicy(t, e, zitiv1.ManagementAdopt)
	e.reconcile(t)

	got, _ := real.List(t.Context(), ziti.Identities, `name="default-team-a-backend"`)
	if len(got) != 1 {
		t.Fatalf("identities = %v", got)
	}
	tags := got[0].Tags()
	if tags["owner"] != "human" || tags["ziti-operator-uid"] != "uid-1" || tags["ziti-operator-adopted"] != "true" {
		t.Fatalf("tags = %v", tags)
	}
	if got[0]["externalId"] != "ext-adopt-test" || got[0]["appData"].(map[string]any)["k"] != "v" {
		t.Fatalf("hand-made fields lost: %v", got[0])
	}
	if attrs := got[0]["roleAttributes"].([]any); len(attrs) != 1 || attrs[0] != "team-a.web" {
		t.Fatalf("roleAttributes = %v", attrs)
	}
	if e.get(t).Status.ZitiID != zid || len(e.secret(t).Data[SecretKeyJWT]) < 20 {
		t.Fatalf("status = %+v", e.get(t).Status)
	}

	if err := e.k.Delete(t.Context(), e.get(t)); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	got, _ = real.List(t.Context(), ziti.Identities, `name="default-team-a-backend"`)
	if len(got) != 1 || len(got[0].Tags()) != 1 || got[0].Tags()["owner"] != "human" {
		t.Fatalf("after release: %v", got)
	}
}

func TestCertificateRenewalAgainstRealController(t *testing.T) {
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

	first := e.secret(t).Data[SecretKeyIdentity]
	_, firstEnd, _ := ziti.CertValidity(first)

	time.Sleep(2 * time.Second)
	e.r.RenewBefore = 400 * 24 * time.Hour
	e.reconcile(t)
	second := e.secret(t).Data[SecretKeyIdentity]
	_, secondEnd, err := ziti.CertValidity(second)
	if err != nil || string(first) == string(second) || !secondEnd.After(firstEnd) {
		t.Fatalf("identity file not renewed: %v %v %v", firstEnd, secondEnd, err)
	}
	if got := e.get(t); !got.Status.Enrolled || condStatusOf(got, CondReady) != "True" {
		t.Fatalf("status = %+v", got.Status)
	}

	// the renewed file must still authenticate
	auths, _ := real.List(t.Context(), ziti.Authenticators, `identity="`+e.get(t).Status.ZitiID+`"`)
	if len(auths) != 1 {
		t.Fatalf("authenticators = %v", auths)
	}
	if _, err := ziti.ExtendCert(second, auths[0].ID()); err != nil {
		t.Fatalf("renewed identity file does not authenticate: %v", err)
	}
}
