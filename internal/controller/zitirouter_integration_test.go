//go:build integration

package controller

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/openziti/edge-api/rest_util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func TestRouterAgainstRealController(t *testing.T) {
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

	e := setupRouter(t, func(r *zitiv1.ZitiRouter) { r.Spec.ZitiName = "it-router" })
	e.r.Clients = staticProvider{wc}
	t.Cleanup(func() {
		ctx := context.Background()
		var rt zitiv1.ZitiRouter
		if e.k.Get(ctx, e.key, &rt) == nil {
			_ = e.k.Delete(ctx, &rt)
			_, _ = e.r.Reconcile(ctx, ctrl.Request{NamespacedName: e.key})
		}
	})

	rt := e.reconcile(t)
	if rt.Status.RouterID == "" || rt.Status.Enrolled || rt.Status.EnrollmentExpiresAt == nil {
		t.Fatalf("status = %+v", rt.Status)
	}
	var secret corev1.Secret
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "routers", Name: "edge-1-enrollment"}, &secret); err != nil {
		t.Fatal(err)
	}
	if jwt := string(secret.Data[SecretKeyJWT]); len(jwt) < 50 || jwt[:3] != "eyJ" {
		t.Fatalf("the Secret does not hold a JWT: %q", jwt)
	}
	wc.writes = nil
	e.reconcile(t)
	if len(wc.writes) != 0 {
		t.Fatalf("second reconcile wrote: %v", wc.writes)
	}

	rt = e.reconcile(t)
	rt.Spec.RoleAttributes, rt.Spec.Cost = []string{"edge", "eu"}, 25
	if err := e.k.Update(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	wc.writes = nil
	e.reconcile(t)
	got, _ := real.List(t.Context(), ziti.EdgeRouters, `name="it-router"`)
	if len(got) != 1 || len(got[0]["roleAttributes"].([]any)) != 2 || got[0]["cost"] != float64(25) {
		t.Fatalf("update did not reach Ziti: %v", got)
	}
	if jwt, _, err := real.Enrollment(t.Context(), ziti.EdgeRouters, rt.Status.RouterID); err != nil || jwt == "" {
		t.Fatalf("an update must keep the pending enrollment: %v", err)
	}
	wc.writes = nil
	e.reconcile(t)
	if len(wc.writes) != 0 {
		t.Fatalf("steady state wrote: %v", wc.writes)
	}
}
