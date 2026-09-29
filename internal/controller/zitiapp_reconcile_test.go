// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type staticProvider struct{ c ziti.Client }

func (p staticProvider) For(context.Context, *zitiv1.ZitiConnection) (ziti.Client, error) {
	return p.c, nil
}

type env struct {
	r   *ZitiAppReconciler
	zc  *ziti.Fake
	k   client.Client
	svc types.NamespacedName
}

func setup(t *testing.T, mut func(*zitiv1.ZitiApp)) *env {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	conn := &zitiv1.ZitiConnection{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: zitiv1.ZitiConnectionSpec{
			RoleScope:      zitiv1.RoleScopeGlobal,
			HostingRouters: []string{"r-main"},
		},
	}
	svc := &zitiv1.ZitiApp{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a", UID: "uid-1", Generation: 1},
		Spec: zitiv1.ZitiAppSpec{
			ConnectionRef:  "default",
			ZitiName:       "app.example.com",
			MemberOf:       []string{"tenant"},
			DeletionPolicy: zitiv1.DeletionPolicyDelete,
			Expose:         zitiv1.Expose{Addresses: []string{"app.example.com"}, Ports: []intstr.IntOrString{intstr.FromInt32(443)}, Protocols: []string{"tcp"}},
			Targets:        []zitiv1.Target{{Address: "10.0.0.5", Port: 8443}},
		},
	}
	if mut != nil {
		mut(svc)
	}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(conn, svc).
		WithStatusSubresource(&zitiv1.ZitiApp{}, &zitiv1.ZitiConnection{}).Build()

	zc := ziti.NewFake()
	zc.Put(ziti.ConfigTypes, ziti.Entity{"name": "intercept.v1"})
	zc.Put(ziti.ConfigTypes, ziti.Entity{"name": "host.v2"})
	zc.Put(ziti.EdgeRouters, ziti.Entity{"id": "id-main", "name": "r-main", "isOnline": true})

	return &env{
		r:   &ZitiAppReconciler{Client: k, Scheme: scheme, Clients: staticProvider{zc}, Recorder: record.NewFakeRecorder(100)},
		zc:  zc,
		k:   k,
		svc: types.NamespacedName{Namespace: "team-a", Name: "app"},
	}
}

func (e *env) reconcile(t *testing.T) ctrl.Result {
	t.Helper()
	res, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.svc})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return res
}

func (e *env) get(t *testing.T) *zitiv1.ZitiApp {
	t.Helper()
	var s zitiv1.ZitiApp
	if err := e.k.Get(t.Context(), e.svc, &s); err != nil {
		t.Fatal(err)
	}
	return &s
}

func (e *env) count(kind ziti.Kind) int { return len(e.zc.Objects[kind]) }

func (e *env) writes() []string {
	var w []string
	for _, c := range e.zc.Calls {
		if !strings.HasPrefix(c, "list") {
			w = append(w, c)
		}
	}
	return w
}

func condStatus(s *zitiv1.ZitiApp, typ string) metav1.ConditionStatus {
	c := meta.FindStatusCondition(s.Status.Conditions, typ)
	if c == nil {
		return "missing"
	}
	return c.Status
}

func TestCreatesEntitiesAndIsIdempotent(t *testing.T) {
	e := setup(t, nil)
	res := e.reconcile(t)
	if res.RequeueAfter < serviceResync {
		t.Errorf("requeue = %v", res.RequeueAfter)
	}

	if e.count(ziti.Configs) != 2 || e.count(ziti.Services) != 1 || e.count(ziti.ServicePolicies) != 1 || e.count(ziti.ServiceEdgeRouterPolicies) != 1 {
		t.Fatalf("objects: %v", e.zc.Calls)
	}
	s := e.get(t)
	if !slices.Contains(s.Finalizers, Finalizer) {
		t.Error("finalizer missing")
	}
	if s.Status.IDs.Service == "" || s.Status.IDs.Bind == "" || s.Status.IDs.SERP == "" || s.Status.IDs.Dial != "" {
		t.Errorf("ids = %+v", s.Status.IDs)
	}
	if condStatus(s, CondSynced) != metav1.ConditionTrue || condStatus(s, CondHosted) != metav1.ConditionFalse || condStatus(s, CondReady) != metav1.ConditionFalse {
		t.Errorf("conditions = %+v", s.Status.Conditions)
	}

	e.zc.Calls = nil
	e.reconcile(t)
	if w := e.writes(); len(w) != 0 {
		t.Errorf("second reconcile wrote: %v", w)
	}
}

func TestRestoresDeletedPolicy(t *testing.T) {
	e := setup(t, nil)
	e.reconcile(t)
	for id := range e.zc.Objects[ziti.ServicePolicies] {
		delete(e.zc.Objects[ziti.ServicePolicies], id)
	}
	e.reconcile(t)
	if e.count(ziti.ServicePolicies) != 1 {
		t.Errorf("bind policy not restored")
	}
}

func TestRevertsDriftedEntity(t *testing.T) {
	e := setup(t, nil)
	e.reconcile(t)
	for _, s := range e.zc.Objects[ziti.ServiceEdgeRouterPolicies] {
		s["edgeRouterRoles"] = []any{"#all"}
	}
	e.reconcile(t)
	for _, s := range e.zc.Objects[ziti.ServiceEdgeRouterPolicies] {
		if roles := s["edgeRouterRoles"].([]any); len(roles) != 1 || roles[0] != "@id-main" {
			t.Errorf("roles = %v", roles)
		}
	}
}

func TestDialPolicyAddedAndRemoved(t *testing.T) {
	e := setup(t, func(s *zitiv1.ZitiApp) { s.Spec.Allow.Groups = []string{"staff"} })
	e.reconcile(t)
	if e.count(ziti.ServicePolicies) != 2 || e.get(t).Status.IDs.Dial == "" {
		t.Fatal("dial policy missing")
	}
	s := e.get(t)
	s.Spec.Allow = zitiv1.Allow{}
	if err := e.k.Update(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	if e.count(ziti.ServicePolicies) != 1 {
		t.Error("dial policy not removed")
	}
}

func TestNameConflictLeavesUntaggedEntityAlone(t *testing.T) {
	e := setup(t, nil)
	e.zc.Put(ziti.Services, ziti.Entity{"id": "hand-made", "name": "app.example.com"})
	e.reconcile(t)
	s := e.get(t)
	c := meta.FindStatusCondition(s.Status.Conditions, CondReady)
	if c == nil || c.Reason != "NameConflict" {
		t.Errorf("ready = %+v", c)
	}
	if _, ok := e.zc.Objects[ziti.Services]["hand-made"]; !ok || e.count(ziti.Services) != 1 {
		t.Error("untagged service was touched")
	}
}

func TestNeverDeletesUntaggedEntities(t *testing.T) {
	e := setup(t, nil)
	e.zc.Put(ziti.ServicePolicies, ziti.Entity{"id": "other", "name": "other-bind"})
	e.reconcile(t)
	s := e.get(t)
	now := metav1.Now()
	s.DeletionTimestamp = &now
	if err := e.k.Delete(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	if _, ok := e.zc.Objects[ziti.ServicePolicies]["other"]; !ok {
		t.Error("untagged policy deleted")
	}
	if e.count(ziti.Configs)+e.count(ziti.Services)+e.count(ziti.ServiceEdgeRouterPolicies) != 0 || e.count(ziti.ServicePolicies) != 1 {
		t.Errorf("owned entities remain: %v", e.zc.Calls)
	}
	var gone zitiv1.ZitiApp
	if err := e.k.Get(t.Context(), e.svc, &gone); err == nil {
		t.Error("service still exists after finalize")
	}
}

func TestOrphanKeepsEntities(t *testing.T) {
	e := setup(t, func(s *zitiv1.ZitiApp) { s.Spec.DeletionPolicy = zitiv1.DeletionPolicyOrphan })
	e.reconcile(t)
	if err := e.k.Delete(t.Context(), e.get(t)); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	if e.count(ziti.Services) != 1 || e.count(ziti.Configs) != 2 {
		t.Error("orphan policy deleted entities")
	}
}

func TestInvalidSpecDoesNotCreateAnything(t *testing.T) {
	e := setup(t, func(s *zitiv1.ZitiApp) { s.Spec.HostedBy = "r-other" })
	e.reconcile(t)
	if e.count(ziti.Configs)+e.count(ziti.Services) != 0 {
		t.Errorf("created entities for an invalid spec: %v", e.zc.Calls)
	}
	c := meta.FindStatusCondition(e.get(t).Status.Conditions, CondReady)
	if c == nil || c.Reason != "InvalidSpec" {
		t.Errorf("ready = %+v", c)
	}
}

func TestHealthyServiceIsReady(t *testing.T) {
	e := setup(t, func(s *zitiv1.ZitiApp) { s.Spec.Allow.Groups = []string{"staff"} })
	e.reconcile(t)
	svcID := e.get(t).Status.IDs.Service
	e.zc.Put(ziti.Terminators, ziti.Entity{"serviceId": svcID, "routerId": "id-main"})
	e.zc.Put(ziti.Identities, ziti.Entity{"id": "u1", "name": "u1", "roleAttributes": []any{"staff"}})
	e.zc.Put(ziti.EdgeRouterPolicies, ziti.Entity{"name": "staff.erp", "semantic": "AnyOf", "identityRoles": []any{"#staff"}, "edgeRouterRoles": []any{"@id-main"}})
	e.reconcile(t)
	s := e.get(t)
	for _, c := range []string{CondSynced, CondHosted, CondDialable, CondRoutePath, CondReady} {
		if condStatus(s, c) != metav1.ConditionTrue {
			t.Errorf("%s = %+v", c, meta.FindStatusCondition(s.Status.Conditions, c))
		}
	}
	if len(s.Status.Terminators) != 1 || s.Status.Terminators[0].Router != "r-main" {
		t.Errorf("terminators = %v", s.Status.Terminators)
	}
}

func TestObserveReadsExistingServiceWithoutWriting(t *testing.T) {
	e := setup(t, func(s *zitiv1.ZitiApp) {
		s.Spec.ManagementPolicy = zitiv1.ManagementObserve
		s.Spec.Expose, s.Spec.Targets = zitiv1.Expose{}, nil
	})
	e.reconcile(t)
	if c := meta.FindStatusCondition(e.get(t).Status.Conditions, CondSynced); c == nil || c.Reason != "NotFound" {
		t.Fatalf("missing service: %+v", c)
	}

	var icType, hostType string
	for id, ct := range e.zc.Objects[ziti.ConfigTypes] {
		if ct.Name() == "intercept.v1" {
			icType = id
		} else {
			hostType = id
		}
	}
	ic := e.zc.Put(ziti.Configs, ziti.Entity{"name": "ic", "configTypeId": icType, "data": map[string]any{"protocols": []string{"tcp"}}})
	hc := e.zc.Put(ziti.Configs, ziti.Entity{"name": "hc", "configTypeId": hostType, "data": map[string]any{"protocol": "tcp"}})
	svcID := e.zc.Put(ziti.Services, ziti.Entity{"name": "app.example.com", "configs": []string{ic, hc}})
	e.zc.Put(ziti.Terminators, ziti.Entity{"serviceId": svcID, "routerId": "id-main"})
	e.zc.Calls = nil

	e.reconcile(t)
	if w := e.writes(); len(w) != 0 {
		t.Errorf("writes = %v", w)
	}
	s := e.get(t)
	if s.Status.IDs.Service != svcID || s.Status.IDs.Intercept != ic || s.Status.IDs.Host != hc {
		t.Errorf("ids = %+v", s.Status.IDs)
	}
	if len(s.Status.Terminators) != 1 || s.Status.Terminators[0].Router != "r-main" {
		t.Errorf("terminators = %+v", s.Status.Terminators)
	}
	if condStatus(s, CondSynced) != metav1.ConditionTrue {
		t.Errorf("conditions = %+v", s.Status.Conditions)
	}
	if len(s.Finalizers) != 0 {
		t.Errorf("finalizers = %v", s.Finalizers)
	}
}

func TestAllowResolvesIdentityNamesFromOutsideKubernetes(t *testing.T) {
	e := setup(t, func(s *zitiv1.ZitiApp) {
		s.Spec.Allow = zitiv1.Allow{Groups: []string{"staff"}, Identities: []string{"alice", "ghost"}}
	})
	e.zc.Put(ziti.Identities, ziti.Entity{"id": "id-alice", "name": "alice"})
	e.reconcile(t)

	var dial ziti.Entity
	for _, p := range e.zc.Objects[ziti.ServicePolicies] {
		if p.Name() == "app.example.com-dial" {
			dial = p
		}
	}
	if dial == nil {
		t.Fatal("dial policy missing")
	}
	got, _ := json.Marshal(dial["identityRoles"])
	if string(got) != `["#staff","@id-alice"]` && string(got) != `["@id-alice","#staff"]` {
		t.Errorf("identityRoles = %s", got)
	}
	c := meta.FindStatusCondition(e.get(t).Status.Conditions, CondAccess)
	if c == nil || c.Status != metav1.ConditionFalse || c.Reason != "IdentityNotFound" || !strings.Contains(c.Message, "ghost") {
		t.Fatalf("AccessResolved = %+v", c)
	}

	e.zc.Put(ziti.Identities, ziti.Entity{"id": "id-ghost", "name": "ghost"})
	e.reconcile(t)
	if c := meta.FindStatusCondition(e.get(t).Status.Conditions, CondAccess); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("AccessResolved after the identity appeared = %+v", c)
	}
	dial = nil
	for _, p := range e.zc.Objects[ziti.ServicePolicies] {
		if p.Name() == "app.example.com-dial" {
			dial = p
		}
	}
	if roles := dial["identityRoles"].([]any); len(roles) != 3 {
		t.Errorf("identityRoles = %v", roles)
	}
}

func TestEntryRoutersCreateAndRemoveClientRouterPolicy(t *testing.T) {
	e := setup(t, func(s *zitiv1.ZitiApp) {
		s.Spec.Allow.Groups = []string{"staff"}
		s.Spec.EntryRouters = []string{"r-main"}
	})
	e.reconcile(t)
	if e.count(ziti.EdgeRouterPolicies) != 1 || e.get(t).Status.IDs.ERP == "" {
		t.Fatalf("erp missing: %v", e.zc.Calls)
	}
	for _, p := range e.zc.Objects[ziti.EdgeRouterPolicies] {
		if p.Name() != "app.example.com-erp" {
			t.Errorf("erp name = %q", p.Name())
		}
	}
	s := e.get(t)
	s.Spec.EntryRouters = nil
	if err := e.k.Update(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	if e.count(ziti.EdgeRouterPolicies) != 0 {
		t.Error("erp not removed")
	}
}

func TestManyTargetsAndPortRangesReachZiti(t *testing.T) {
	e := setup(t, func(s *zitiv1.ZitiApp) {
		s.Spec.Expose.Ports = []intstr.IntOrString{intstr.FromInt32(443), intstr.FromString("8000-8005")}
		s.Spec.Targets = []zitiv1.Target{{Address: "10.0.0.5"}, {Address: "10.0.0.6", Cost: 5}}
	})
	e.reconcile(t)
	var host, icpt ziti.Entity
	for _, c := range e.zc.Objects[ziti.Configs] {
		if strings.HasSuffix(c.Name(), "-host.v2") {
			host = c
		} else {
			icpt = c
		}
	}
	terms := host["data"].(map[string]any)["terminators"].([]any)
	if len(terms) != 2 {
		t.Fatalf("terminators = %v", terms)
	}
	if ranges := icpt["data"].(map[string]any)["portRanges"].([]any); len(ranges) != 2 {
		t.Errorf("portRanges = %v", ranges)
	}
	e.zc.Calls = nil
	e.reconcile(t)
	if w := e.writes(); len(w) != 0 {
		t.Errorf("second reconcile wrote: %v", w)
	}
}

func TestBadPortRangeIsInvalidSpec(t *testing.T) {
	e := setup(t, func(s *zitiv1.ZitiApp) { s.Spec.Expose.Ports = []intstr.IntOrString{intstr.FromString("9000-8000")} })
	e.reconcile(t)
	if c := meta.FindStatusCondition(e.get(t).Status.Conditions, CondReady); c == nil || c.Reason != "InvalidSpec" || e.count(ziti.Configs) != 0 {
		t.Errorf("ready = %+v", c)
	}
}
