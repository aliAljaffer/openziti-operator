// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type termEnv struct {
	r   *ZitiTerminatorReconciler
	zc  *ziti.Fake
	k   client.Client
	key types.NamespacedName
}

func setupTerminator(t *testing.T, scope zitiv1.RoleScope, mut func(*zitiv1.ZitiTerminator)) *termEnv {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	conn := &zitiv1.ZitiConnection{Name: "default", Spec: zitiv1.ZitiConnectionSpec{RoleScope: scope}}
	term := &zitiv1.ZitiTerminator{Name: "web-term", Namespace: "team-a", UID: "uid-term", Generation: 1,
		Spec: zitiv1.ZitiTerminatorSpec{ConnectionRef: "default", Service: "web", Router: "router-a", Address: "tcp:10.0.0.5:8443",
			Binding: "transport", Precedence: "default", DeletionPolicy: zitiv1.DeletionPolicyDelete}}
	if mut != nil {
		mut(term)
	}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(conn, term).WithStatusSubresource(&zitiv1.ZitiTerminator{}).Build()
	zc := ziti.NewFake()
	zc.Put(ziti.Services, ziti.Entity{"id": "s-web", "name": "web"})
	zc.Put(ziti.Services, ziti.Entity{"id": "s-api", "name": "api"})
	zc.Put(ziti.EdgeRouters, ziti.Entity{"id": "r-a", "name": "router-a"})
	return &termEnv{
		r:  &ZitiTerminatorReconciler{Client: k, Scheme: scheme, Clients: staticProvider{zc}, Recorder: record.NewFakeRecorder(50)},
		zc: zc, k: k, key: types.NamespacedName{Namespace: "team-a", Name: "web-term"},
	}
}

func (e *termEnv) reconcile(t *testing.T) *zitiv1.ZitiTerminator {
	t.Helper()
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var out zitiv1.ZitiTerminator
	if err := e.k.Get(t.Context(), e.key, &out); err != nil {
		return nil
	}
	return &out
}

func termCond(t *zitiv1.ZitiTerminator, typ string) metav1.Condition {
	for _, c := range t.Status.Conditions {
		if c.Type == typ {
			return c
		}
	}
	return metav1.Condition{}
}

func (e *termEnv) writes() []string {
	var w []string
	for _, c := range e.zc.Calls {
		if !strings.HasPrefix(c, "list") {
			w = append(w, c)
		}
	}
	return w
}

func TestTerminatorIsCreatedOnceAndOwned(t *testing.T) {
	e := setupTerminator(t, zitiv1.RoleScopeGlobal, nil)
	term := e.reconcile(t)
	if len(e.zc.Objects[ziti.Terminators]) != 1 || term.Status.ZitiID == "" || termCond(term, CondReady).Status != metav1.ConditionTrue {
		t.Fatalf("status = %+v, calls %v", term.Status, e.zc.Calls)
	}
	got := e.zc.Objects[ziti.Terminators][term.Status.ZitiID]
	if refID(got["service"]) != "s-web" || refID(got["router"]) != "r-a" || got["address"] != "tcp:10.0.0.5:8443" || got.Tags()["ziti-operator-uid"] != "uid-term" {
		t.Errorf("terminator = %v", got)
	}
	e.zc.Calls = nil
	e.reconcile(t)
	if w := e.writes(); len(w) != 0 {
		t.Errorf("second reconcile wrote: %v", w)
	}
}

func TestTerminatorFollowsSpecChanges(t *testing.T) {
	e := setupTerminator(t, zitiv1.RoleScopeGlobal, nil)
	first := e.reconcile(t).Status.ZitiID
	edit := func(f func(*zitiv1.ZitiTerminatorSpec)) {
		var cur zitiv1.ZitiTerminator
		if err := e.k.Get(t.Context(), e.key, &cur); err != nil {
			t.Fatal(err)
		}
		f(&cur.Spec)
		if err := e.k.Update(t.Context(), &cur); err != nil {
			t.Fatal(err)
		}
	}

	edit(func(s *zitiv1.ZitiTerminatorSpec) {
		s.Address, s.Cost, s.Precedence = "tcp:10.0.0.6:8443", 20, "required"
	})
	e.zc.Calls = nil
	if got := e.reconcile(t).Status.ZitiID; got != first {
		t.Errorf("an address, cost, or precedence change must update in place: %q -> %q", first, got)
	}
	cur := e.zc.Objects[ziti.Terminators][first]
	if cur["address"] != "tcp:10.0.0.6:8443" || cur["precedence"] != "required" || cur["cost"] != int32(20) && cur["cost"] != float64(20) {
		t.Errorf("terminator = %v", cur)
	}
	e.zc.Calls = nil
	e.reconcile(t)
	if w := e.writes(); len(w) != 0 {
		t.Errorf("steady state wrote: %v", w)
	}

	// The CRD forbids a new service, but the controller must still replace the terminator if it happens.
	edit(func(s *zitiv1.ZitiTerminatorSpec) { s.Service = "api" })
	second := e.reconcile(t).Status.ZitiID
	if second == first || len(e.zc.Objects[ziti.Terminators]) != 1 || refID(e.zc.Objects[ziti.Terminators][second]["service"]) != "s-api" {
		t.Errorf("replace: %v", e.zc.Objects[ziti.Terminators])
	}
}

func TestTerminatorNeedsGlobalScopeAndExistingTargets(t *testing.T) {
	e := setupTerminator(t, zitiv1.RoleScopeNamespaced, nil)
	if c := termCond(e.reconcile(t), CondReady); c.Reason != "InvalidSpec" || !strings.Contains(c.Message, "roleScope Global") {
		t.Errorf("namespaced: %+v", c)
	}
	if len(e.zc.Objects[ziti.Terminators]) != 0 {
		t.Error("nothing may be created")
	}

	e = setupTerminator(t, zitiv1.RoleScopeGlobal, func(t *zitiv1.ZitiTerminator) { t.Spec.Service = "missing" })
	res, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key})
	if err != nil {
		t.Fatal(err)
	}
	var cur zitiv1.ZitiTerminator
	_ = e.k.Get(t.Context(), e.key, &cur)
	if c := termCond(&cur, CondReady); c.Reason != "TargetNotFound" || res.RequeueAfter != dependencyRetry {
		t.Errorf("missing service: %+v, requeue %v", c, res.RequeueAfter)
	}
}

func TestTerminatorDeleteAndOrphan(t *testing.T) {
	for _, policy := range []zitiv1.DeletionPolicy{zitiv1.DeletionPolicyDelete, zitiv1.DeletionPolicyOrphan} {
		e := setupTerminator(t, zitiv1.RoleScopeGlobal, func(t *zitiv1.ZitiTerminator) { t.Spec.DeletionPolicy = policy })
		term := e.reconcile(t)
		if err := e.k.Delete(t.Context(), term); err != nil {
			t.Fatal(err)
		}
		e.reconcile(t)
		want := 0
		if policy == zitiv1.DeletionPolicyOrphan {
			want = 1
		}
		if n := len(e.zc.Objects[ziti.Terminators]); n != want {
			t.Errorf("%s: terminators = %d, want %d", policy, n, want)
		}
		for _, o := range e.zc.Objects[ziti.Terminators] {
			if len(o.Tags()) != 0 {
				t.Errorf("orphaned terminator keeps tags: %v", o.Tags())
			}
		}
	}
}
