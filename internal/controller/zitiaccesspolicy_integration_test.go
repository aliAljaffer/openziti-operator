//go:build integration

package controller

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/openziti/edge-api/rest_util"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type writeCounter struct {
	ziti.Client
	writes []string
}

func (w *writeCounter) Create(ctx context.Context, k ziti.Kind, b ziti.Entity) (string, error) {
	w.writes = append(w.writes, "create "+string(k))
	return w.Client.Create(ctx, k, b)
}

func (w *writeCounter) Update(ctx context.Context, k ziti.Kind, id string, b ziti.Entity) error {
	w.writes = append(w.writes, "update "+string(k))
	return w.Client.Update(ctx, k, id, b)
}

func TestAccessPolicyAgainstRealController(t *testing.T) {
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

	e := setupAccess(t, zitiv1.RoleScopeNamespaced, func(ap *zitiv1.ZitiAccessPolicy, _ *zitiv1.ZitiConnection) {
		ap.Spec.EdgeRouters = []string{"router-instance-1"}
	})
	e.r.Clients = staticProvider{wc}
	t.Cleanup(func() {
		for _, k := range accessKinds {
			list, _ := real.List(context.Background(), k, tagFilter("uid-ap"))
			for _, x := range list {
				_ = real.Delete(context.Background(), k, x.ID())
			}
		}
	})

	ap := e.reconcile(t)
	if ap.Status.DialPolicyID == "" || ap.Status.EdgeRouterPolicyID == "" {
		t.Fatalf("status = %+v", ap.Status)
	}
	wc.writes = nil
	e.reconcile(t)
	if len(wc.writes) != 0 {
		t.Fatalf("second reconcile wrote: %v", wc.writes)
	}
	ap.Spec.PostureCheckRoles = nil
	ap.Spec.IdentityRoles = []string{"#users", "#admins"}
	if err := e.k.Update(t.Context(), ap); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	if len(wc.writes) != 2 {
		t.Fatalf("update writes = %v", wc.writes)
	}

	cur := e.reconcile(t)
	if err := e.k.Delete(t.Context(), cur); err != nil {
		t.Fatal(err)
	}
	_, _ = e.r.Reconcile(t.Context(), e.reqFor())
	for _, k := range accessKinds {
		if list, _ := real.List(t.Context(), k, tagFilter("uid-ap")); len(list) != 0 {
			t.Fatalf("%s left behind: %v", k, list)
		}
	}
}
