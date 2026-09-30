//go:build integration

package controller

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/openziti/edge-api/rest_util"
	ctrl "sigs.k8s.io/controller-runtime"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func TestTerminatorAgainstRealController(t *testing.T) {
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

	svcID, err := real.Create(t.Context(), ziti.Services, ziti.Entity{"name": "it-term-svc", "encryptionRequired": true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = real.Delete(context.Background(), ziti.Services, svcID) })

	e := setupTerminator(t, zitiv1.RoleScopeGlobal, func(x *zitiv1.ZitiTerminator) {
		x.Spec.Service, x.Spec.Router, x.Spec.Cost = "it-term-svc", "router-instance-1", 10
	})
	e.r.Clients = staticProvider{wc}
	t.Cleanup(func() {
		ctx := context.Background()
		var cur zitiv1.ZitiTerminator
		if e.k.Get(ctx, e.key, &cur) == nil {
			_ = e.k.Delete(ctx, &cur)
			_, _ = e.r.Reconcile(ctx, ctrl.Request{NamespacedName: e.key})
		}
	})

	term := e.reconcile(t)
	if term.Status.ZitiID == "" || termCond(term, CondReady).Status != "True" {
		t.Fatalf("status = %+v conditions %+v", term.Status, term.Status.Conditions)
	}
	wc.writes = nil
	e.reconcile(t)
	if len(wc.writes) != 0 {
		t.Fatalf("second reconcile wrote: %v", wc.writes)
	}

	var cur zitiv1.ZitiTerminator
	if err := e.k.Get(t.Context(), e.key, &cur); err != nil {
		t.Fatal(err)
	}
	cur.Spec.Address, cur.Spec.Cost, cur.Spec.Precedence = "tcp:10.0.0.6:9443", 25, "required"
	if err := e.k.Update(t.Context(), &cur); err != nil {
		t.Fatal(err)
	}
	if got := e.reconcile(t).Status.ZitiID; got != term.Status.ZitiID {
		t.Errorf("update must keep the terminator: %q -> %q", term.Status.ZitiID, got)
	}
	list, _ := real.List(t.Context(), ziti.Terminators, tagFilter("uid-term"))
	if len(list) != 1 || list[0]["address"] != "tcp:10.0.0.6:9443" || list[0]["cost"] != float64(25) || list[0]["precedence"] != "required" {
		t.Fatalf("update did not reach Ziti: %v", list)
	}
	wc.writes = nil
	e.reconcile(t)
	if len(wc.writes) != 0 {
		t.Fatalf("steady state wrote: %v", wc.writes)
	}

	if err := e.k.Get(t.Context(), e.key, &cur); err != nil {
		t.Fatal(err)
	}
	if err := e.k.Delete(t.Context(), &cur); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	if list, _ := real.List(t.Context(), ziti.Terminators, tagFilter("uid-term")); len(list) != 0 {
		t.Fatalf("terminator left behind: %v", list)
	}
}
