// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"encoding/json"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
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

type apEnv struct {
	r   *ZitiAccessPolicyReconciler
	zc  *ziti.Fake
	k   client.Client
	key types.NamespacedName
}

func setupAccess(t *testing.T, scope zitiv1.RoleScope, mut func(*zitiv1.ZitiAccessPolicy, *zitiv1.ZitiConnection)) *apEnv {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	conn := &zitiv1.ZitiConnection{ObjectMeta: metav1.ObjectMeta{Name: "default"}, Spec: zitiv1.ZitiConnectionSpec{RoleScope: scope}}
	ap := &zitiv1.ZitiAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "team", Namespace: "a", UID: "uid-ap", Generation: 1},
		Spec: zitiv1.ZitiAccessPolicySpec{
			ConnectionRef: "default", IdentityRoles: []string{"#users"}, ServiceRoles: []string{"#web"},
			DeletionPolicy: zitiv1.DeletionPolicyDelete,
		},
	}
	if mut != nil {
		mut(ap, conn)
	}
	k := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(conn, ap, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "a", Labels: map[string]string{"tier": "gold"}}}).
		WithStatusSubresource(&zitiv1.ZitiAccessPolicy{}).Build()
	zc := ziti.NewFake()
	zc.Put(ziti.EdgeRouters, ziti.Entity{"id": "id-r", "name": "r-entry", "isOnline": true})
	return &apEnv{
		r:  &ZitiAccessPolicyReconciler{Client: k, Scheme: scheme, Clients: staticProvider{zc}, Recorder: record.NewFakeRecorder(100)},
		zc: zc, k: k, key: types.NamespacedName{Namespace: "a", Name: "team"},
	}
}

func (e *apEnv) reconcile(t *testing.T) *zitiv1.ZitiAccessPolicy {
	t.Helper()
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var ap zitiv1.ZitiAccessPolicy
	if err := e.k.Get(t.Context(), e.key, &ap); err != nil {
		t.Fatal(err)
	}
	return &ap
}

func (e *apEnv) reqFor() ctrl.Request { return ctrl.Request{NamespacedName: e.key} }

func (e *apEnv) only(kind ziti.Kind) ziti.Entity {
	for _, v := range e.zc.Objects[kind] {
		return v
	}
	return nil
}

func roles(e ziti.Entity, key string) string {
	b, _ := json.Marshal(e[key])
	return string(b)
}

func TestAccessPolicyNamespacedScopesRolesAndCreatesERP(t *testing.T) {
	e := setupAccess(t, zitiv1.RoleScopeNamespaced, func(ap *zitiv1.ZitiAccessPolicy, _ *zitiv1.ZitiConnection) {
		ap.Spec.EdgeRouters = []string{"r-entry"}
	})
	e.zc.Put(ziti.Identities, ziti.Entity{"name": "u1", "roleAttributes": []string{"a.users"}})
	e.zc.Put(ziti.Identities, ziti.Entity{"name": "u2", "roleAttributes": []string{"b.users"}})
	e.zc.Put(ziti.Services, ziti.Entity{"name": "s1", "roleAttributes": []string{"a.web"}})
	e.zc.Put(ziti.Services, ziti.Entity{"name": "other-ns", "roleAttributes": []string{"b.web"}})

	ap := e.reconcile(t)
	dial, erp := e.only(ziti.ServicePolicies), e.only(ziti.EdgeRouterPolicies)
	if dial.Name() != "a.team.dial" || dial["type"] != "Dial" || roles(dial, "identityRoles") != `["#a.users"]` || roles(dial, "serviceRoles") != `["#a.web"]` {
		t.Errorf("dial = %v", dial)
	}
	if erp.Name() != "a.team.erp" || roles(erp, "edgeRouterRoles") != `["@id-r"]` || roles(erp, "identityRoles") != `["#a.users"]` {
		t.Errorf("erp = %v", erp)
	}
	if ap.Status.Identities != 1 || ap.Status.Services != 1 {
		t.Errorf("selected identities=%d services=%d, want 1 and 1", ap.Status.Identities, ap.Status.Services)
	}
	if condStatusOfList(ap.Status.Conditions, CondReady) != metav1.ConditionTrue {
		t.Errorf("conditions = %+v", ap.Status.Conditions)
	}

	e.zc.Calls = nil
	e.reconcile(t)
	for _, c := range e.zc.Calls {
		if c[:4] != "list" {
			t.Errorf("second reconcile wrote %q", c)
		}
	}
}

func condStatusOfList(cs []metav1.Condition, typ string) metav1.ConditionStatus {
	for _, c := range cs {
		if c.Type == typ {
			return c.Status
		}
	}
	return "missing"
}

func TestAccessPolicyNamespacedRejectsDirectRoles(t *testing.T) {
	for _, bad := range [][]string{{"@some-service-id"}, {"#all"}, {"plain"}} {
		e := setupAccess(t, zitiv1.RoleScopeNamespaced, func(ap *zitiv1.ZitiAccessPolicy, _ *zitiv1.ZitiConnection) {
			ap.Spec.ServiceRoles = bad
		})
		ap := e.reconcile(t)
		if c := findConditionByType(ap.Status.Conditions, CondSynced); c.Reason != "InvalidSpec" {
			t.Errorf("serviceRoles %v: synced = %+v", bad, c)
		}
		if len(e.zc.Objects[ziti.ServicePolicies]) != 0 {
			t.Errorf("serviceRoles %v created a policy", bad)
		}
	}
}

func findConditionByType(cs []metav1.Condition, typ string) metav1.Condition {
	for _, c := range cs {
		if c.Type == typ {
			return c
		}
	}
	return metav1.Condition{}
}

func TestAccessPolicyGlobalPassesRolesThrough(t *testing.T) {
	e := setupAccess(t, zitiv1.RoleScopeGlobal, func(ap *zitiv1.ZitiAccessPolicy, _ *zitiv1.ZitiConnection) {
		ap.Spec.IdentityRoles, ap.Spec.ServiceRoles = []string{"#tenant"}, []string{"#tenant"}
	})
	e.zc.Put(ziti.Identities, ziti.Entity{"name": "u", "roleAttributes": []string{"tenant"}})
	e.zc.Put(ziti.Services, ziti.Entity{"name": "s", "roleAttributes": []string{"tenant"}})
	ap := e.reconcile(t)
	if roles(e.only(ziti.ServicePolicies), "serviceRoles") != `["#tenant"]` || ap.Status.Identities != 1 || ap.Status.Services != 1 {
		t.Errorf("dial = %v status = %+v", e.only(ziti.ServicePolicies), ap.Status)
	}
	if len(e.zc.Objects[ziti.EdgeRouterPolicies]) != 0 {
		t.Error("no edgeRouters must mean no router policy")
	}
}

func TestAccessPolicyUpdateRemovesERPAndReportsNoMatch(t *testing.T) {
	e := setupAccess(t, zitiv1.RoleScopeGlobal, func(ap *zitiv1.ZitiAccessPolicy, _ *zitiv1.ZitiConnection) {
		ap.Spec.EdgeRouters = []string{"r-entry"}
	})
	ap := e.reconcile(t)
	if c := findConditionByType(ap.Status.Conditions, CondReady); c.Status != metav1.ConditionFalse || c.Reason != "NoMatch" {
		t.Errorf("ready = %+v", c)
	}
	if len(e.zc.Objects[ziti.EdgeRouterPolicies]) != 1 {
		t.Fatal("erp missing")
	}
	ap.Spec.EdgeRouters = nil
	ap.Spec.ServiceRoles = []string{"#other"}
	if err := e.k.Update(t.Context(), ap); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	if len(e.zc.Objects[ziti.EdgeRouterPolicies]) != 0 || roles(e.only(ziti.ServicePolicies), "serviceRoles") != `["#other"]` {
		t.Errorf("objects = %v", e.zc.Objects)
	}
}

func TestAccessPolicyNameConflictAndDelete(t *testing.T) {
	e := setupAccess(t, zitiv1.RoleScopeGlobal, nil)
	e.zc.Put(ziti.ServicePolicies, ziti.Entity{"name": "a.team.dial"})
	ap := e.reconcile(t)
	if c := findConditionByType(ap.Status.Conditions, CondSynced); c.Reason != "NameConflict" {
		t.Errorf("synced = %+v", c)
	}

	e = setupAccess(t, zitiv1.RoleScopeGlobal, nil)
	e.zc.Put(ziti.ServicePolicies, ziti.Entity{"name": "hand-made"})
	ap = e.reconcile(t)
	if err := e.k.Delete(t.Context(), ap); err != nil {
		t.Fatal(err)
	}
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range e.zc.Objects[ziti.ServicePolicies] {
		names = append(names, p.Name())
	}
	if !slices.Equal(names, []string{"hand-made"}) {
		t.Errorf("policies after delete = %v", names)
	}
}

func TestAllowedNamespacesIsEnforced(t *testing.T) {
	for label, want := range map[string]string{"gold": "", "silver": "NamespaceNotAllowed"} {
		e := setupAccess(t, zitiv1.RoleScopeGlobal, func(_ *zitiv1.ZitiAccessPolicy, c *zitiv1.ZitiConnection) {
			c.Spec.AllowedNamespaces = &metav1.LabelSelector{MatchLabels: map[string]string{"tier": label}}
		})
		ap := e.reconcile(t)
		c := findConditionByType(ap.Status.Conditions, CondSynced)
		if want == "" && c.Status != metav1.ConditionTrue || want != "" && c.Reason != want {
			t.Errorf("tier %s: synced = %+v", label, c)
		}
		if want != "" && len(e.zc.Objects[ziti.ServicePolicies]) != 0 {
			t.Errorf("tier %s: wrote to Ziti", label)
		}
	}
}

func typesName(name string) types.NamespacedName { return types.NamespacedName{Name: name} }

func TestAccessPolicyOrphanReleasesTags(t *testing.T) {
	e := setupAccess(t, zitiv1.RoleScopeGlobal, func(ap *zitiv1.ZitiAccessPolicy, _ *zitiv1.ZitiConnection) {
		ap.Spec.DeletionPolicy = zitiv1.DeletionPolicyOrphan
		ap.Spec.EdgeRouters = []string{"r-entry"}
	})
	ap := e.reconcile(t)
	if err := e.k.Delete(t.Context(), ap); err != nil {
		t.Fatal(err)
	}
	if _, err := e.r.Reconcile(t.Context(), e.reqFor()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range accessKinds {
		if len(e.zc.Objects[kind]) != 1 {
			t.Fatalf("%s = %d, want 1", kind, len(e.zc.Objects[kind]))
		}
		for _, o := range e.zc.Objects[kind] {
			if len(o.Tags()) != 0 {
				t.Errorf("%s tags = %v", kind, o.Tags())
			}
		}
	}
}

func typesName2(ns, name string) types.NamespacedName {
	return types.NamespacedName{Namespace: ns, Name: name}
}
