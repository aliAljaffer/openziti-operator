// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"encoding/json"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/check"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type entityEnv struct {
	scheme *runtime.Scheme
	k      client.Client
	zc     *ziti.Fake
	rec    *record.FakeRecorder
}

func newEntityEnv(t *testing.T, scope zitiv1.RoleScope, objs ...client.Object) *entityEnv {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	conn := &zitiv1.ZitiConnection{Name: "default", Spec: zitiv1.ZitiConnectionSpec{RoleScope: scope}}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(objs, conn)...).
		WithStatusSubresource(&zitiv1.ZitiConfig{}, &zitiv1.ZitiService{}, &zitiv1.ZitiServicePolicy{},
			&zitiv1.ZitiEdgeRouterPolicy{}, &zitiv1.ZitiServiceEdgeRouterPolicy{}).Build()
	zc := ziti.NewFake()
	zc.Put(ziti.ConfigTypes, ziti.Entity{"id": "intercept.v1", "name": "intercept.v1"})
	zc.Put(ziti.EdgeRouters, ziti.Entity{"id": "r-1", "name": "router-a"})
	zc.Put(ziti.Identities, ziti.Entity{"id": "i-1", "name": "alice"})
	zc.Put(ziti.Services, ziti.Entity{"id": "s-1", "name": "legacy"})
	return &entityEnv{scheme: scheme, k: k, zc: zc, rec: record.NewFakeRecorder(50)}
}

func entityMeta(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: "team-a", UID: types.UID("uid-" + name), Generation: 1}
}

func common() zitiv1.EntitySpec {
	return zitiv1.EntitySpec{ConnectionRef: "default", DeletionPolicy: zitiv1.DeletionPolicyDelete}
}

func reconcileEntity[T interface {
	client.Object
	zitiv1.EntityObject
}](t *testing.T, e *entityEnv, name string, kind ziti.Kind, newObj func() T, build func(*resolver, T, *zitiv1.ZitiConnection) (ziti.Entity, error)) (T, ctrl.Result) {
	t.Helper()
	r := newEntityReconciler(e.k, e.scheme, staticProvider{e.zc}, e.rec, "test", kind, newObj, build)
	res, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "team-a", Name: name})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	obj := newObj()
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "team-a", Name: name}, obj); err != nil {
		t.Fatal(err)
	}
	return obj, res
}

func readyOf(o zitiv1.EntityObject) metav1.Condition {
	for _, c := range o.EntityState().Conditions {
		if c.Type == CondReady {
			return c
		}
	}
	return metav1.Condition{}
}

func rolesOf(e ziti.Entity, key string) string { b, _ := json.Marshal(e[key]); return string(b) }

func TestEntityConfigResolvesTheTypeAndKeepsData(t *testing.T) {
	cfg := &zitiv1.ZitiConfig{ObjectMeta: entityMeta("web-intercept"), Spec: zitiv1.ZitiConfigSpec{
		EntitySpec: common(), Type: "intercept.v1",
		Data: apiextensionsv1.JSON{Raw: []byte(`{"protocols":["tcp"],"addresses":["web.example.com"],"portRanges":[{"low":80,"high":80}]}`)},
	}}
	e := newEntityEnv(t, zitiv1.RoleScopeGlobal, cfg)
	newObj := func() *zitiv1.ZitiConfig { return &zitiv1.ZitiConfig{} }
	obj, _ := reconcileEntity(t, e, "web-intercept", ziti.Configs, newObj, buildConfig)

	got := only(e.zc.Objects[ziti.Configs])
	if got.Name() != "team-a.web-intercept" || got["configTypeId"] != "intercept.v1" {
		t.Errorf("config = %v", got)
	}
	if addr := got["data"].(map[string]any)["addresses"].([]any); addr[0] != "web.example.com" {
		t.Errorf("data = %v", got["data"])
	}
	if obj.Status.ZitiID != got.ID() || readyOf(obj).Status != metav1.ConditionTrue {
		t.Errorf("status = %+v", obj.Status)
	}

	e.zc.Calls = nil
	reconcileEntity(t, e, "web-intercept", ziti.Configs, newObj, buildConfig)
	for _, c := range e.zc.Calls {
		if !strings.HasPrefix(c, "list") {
			t.Errorf("second reconcile wrote: %s", c)
		}
	}
}

func TestEntityConfigWithAnUnknownTypeOrBadDataIsInvalid(t *testing.T) {
	bad := &zitiv1.ZitiConfig{ObjectMeta: entityMeta("a"), Spec: zitiv1.ZitiConfigSpec{EntitySpec: common(), Type: "nope.v1", Data: apiextensionsv1.JSON{Raw: []byte(`{}`)}}}
	arr := &zitiv1.ZitiConfig{ObjectMeta: entityMeta("b"), Spec: zitiv1.ZitiConfigSpec{EntitySpec: common(), Type: "intercept.v1", Data: apiextensionsv1.JSON{Raw: []byte(`[1]`)}}}
	e := newEntityEnv(t, zitiv1.RoleScopeGlobal, bad, arr)
	newObj := func() *zitiv1.ZitiConfig { return &zitiv1.ZitiConfig{} }
	for name, want := range map[string]string{"a": "not found in Ziti", "b": "data must be an object"} {
		obj, _ := reconcileEntity(t, e, name, ziti.Configs, newObj, buildConfig)
		if c := readyOf(obj); c.Reason != "InvalidSpec" || !strings.Contains(c.Message, want) {
			t.Errorf("%s: %+v", name, c)
		}
	}
	if len(e.zc.Objects[ziti.Configs]) != 0 {
		t.Error("nothing may be created for an invalid spec")
	}
}

func TestEntityServiceUsesConfigNamesAndRetriesSoonWhenOneIsMissing(t *testing.T) {
	svc := &zitiv1.ZitiService{ObjectMeta: entityMeta("web"), Spec: zitiv1.ZitiServiceSpec{
		EntitySpec: common(), RoleAttributes: []string{"tenant"}, Configs: []string{"team-a.web-intercept"},
	}}
	e := newEntityEnv(t, zitiv1.RoleScopeNamespaced, svc)
	newObj := func() *zitiv1.ZitiService { return &zitiv1.ZitiService{} }

	obj, res := reconcileEntity(t, e, "web", ziti.Services, newObj, buildService)
	if c := readyOf(obj); c.Reason != "TargetNotFound" || !strings.Contains(c.Message, "team-a.web-intercept") {
		t.Fatalf("ready = %+v", c)
	}
	if res.RequeueAfter != dependencyRetry {
		t.Errorf("requeue = %v, want %v", res.RequeueAfter, dependencyRetry)
	}
	if len(e.zc.Objects[ziti.Services]) != 2-1 { // only the seeded "legacy" service
		t.Errorf("services = %d", len(e.zc.Objects[ziti.Services]))
	}

	e.zc.Put(ziti.Configs, ziti.Entity{"id": "c-1", "name": "team-a.web-intercept"})
	obj, res = reconcileEntity(t, e, "web", ziti.Services, newObj, buildService)
	var created ziti.Entity
	for _, s := range e.zc.Objects[ziti.Services] {
		if s.Name() == "team-a.web" {
			created = s
		}
	}
	if created == nil || rolesOf(created, "configs") != `["c-1"]` || rolesOf(created, "roleAttributes") != `["team-a.tenant"]` ||
		created["terminatorStrategy"] != "smartrouting" || created["encryptionRequired"] != true {
		t.Fatalf("service = %v", created)
	}
	if readyOf(obj).Status != metav1.ConditionTrue || res.RequeueAfter < serviceResync {
		t.Errorf("ready = %+v requeue %v", readyOf(obj), res.RequeueAfter)
	}
}

func TestEntityServicePolicyRolesFollowTheScope(t *testing.T) {
	mk := func() *zitiv1.ZitiServicePolicy {
		return &zitiv1.ZitiServicePolicy{ObjectMeta: entityMeta("dial"), Spec: zitiv1.ZitiServicePolicySpec{
			EntitySpec: common(), Type: "Dial", IdentityRoles: []string{"#staff", "@alice"}, ServiceRoles: []string{"#web"},
		}}
	}
	newObj := func() *zitiv1.ZitiServicePolicy { return &zitiv1.ZitiServicePolicy{} }

	e := newEntityEnv(t, zitiv1.RoleScopeGlobal, mk())
	reconcileEntity(t, e, "dial", ziti.ServicePolicies, newObj, buildServicePolicy)
	p := only(e.zc.Objects[ziti.ServicePolicies])
	if p["type"] != "Dial" || p["semantic"] != "AnyOf" || rolesOf(p, "identityRoles") != `["#staff","@i-1"]` || rolesOf(p, "serviceRoles") != `["#web"]` {
		t.Errorf("global policy = %v", p)
	}

	e = newEntityEnv(t, zitiv1.RoleScopeNamespaced, mk())
	obj, _ := reconcileEntity(t, e, "dial", ziti.ServicePolicies, newObj, buildServicePolicy)
	if c := readyOf(obj); c.Reason != "InvalidSpec" || !strings.Contains(c.Message, "@alice") {
		t.Errorf("namespaced must reject @name: %+v", c)
	}

	ok := mk()
	ok.Spec.IdentityRoles = []string{"#staff"}
	e = newEntityEnv(t, zitiv1.RoleScopeNamespaced, ok)
	reconcileEntity(t, e, "dial", ziti.ServicePolicies, newObj, buildServicePolicy)
	if got := only(e.zc.Objects[ziti.ServicePolicies]); rolesOf(got, "identityRoles") != `["#team-a.staff"]` || rolesOf(got, "serviceRoles") != `["#team-a.web"]` {
		t.Errorf("namespaced policy = %v", got)
	}
}

func TestEntityPolicyNamesThatDoNotExistRetrySoon(t *testing.T) {
	p := &zitiv1.ZitiServicePolicy{ObjectMeta: entityMeta("bind"), Spec: zitiv1.ZitiServicePolicySpec{
		EntitySpec: common(), Type: "Bind", IdentityRoles: []string{"@ghost"}, ServiceRoles: []string{"@legacy"},
	}}
	e := newEntityEnv(t, zitiv1.RoleScopeGlobal, p)
	obj, res := reconcileEntity(t, e, "bind", ziti.ServicePolicies, func() *zitiv1.ZitiServicePolicy { return &zitiv1.ZitiServicePolicy{} }, buildServicePolicy)
	if c := readyOf(obj); c.Reason != "TargetNotFound" || !strings.Contains(c.Message, `"ghost"`) || res.RequeueAfter != dependencyRetry {
		t.Errorf("ready = %+v requeue %v", c, res.RequeueAfter)
	}
	if len(e.zc.Objects[ziti.ServicePolicies]) != 0 {
		t.Error("nothing may be created while a role target is missing")
	}
}

func TestEntityRouterPolicies(t *testing.T) {
	erp := &zitiv1.ZitiEdgeRouterPolicy{ObjectMeta: entityMeta("erp"), Spec: zitiv1.ZitiEdgeRouterPolicySpec{
		EntitySpec: common(), Semantic: "AllOf", IdentityRoles: []string{"@alice"}, EdgeRouterRoles: []string{"@router-a"}}}
	serp := &zitiv1.ZitiServiceEdgeRouterPolicy{ObjectMeta: entityMeta("serp"), Spec: zitiv1.ZitiServiceEdgeRouterPolicySpec{
		EntitySpec: common(), ServiceRoles: []string{"#web"}, EdgeRouterRoles: []string{"#all"}}}
	e := newEntityEnv(t, zitiv1.RoleScopeGlobal, erp, serp)

	reconcileEntity(t, e, "erp", ziti.EdgeRouterPolicies, func() *zitiv1.ZitiEdgeRouterPolicy { return &zitiv1.ZitiEdgeRouterPolicy{} }, buildEdgeRouterPolicy)
	got := only(e.zc.Objects[ziti.EdgeRouterPolicies])
	if got["semantic"] != "AllOf" || rolesOf(got, "identityRoles") != `["@i-1"]` || rolesOf(got, "edgeRouterRoles") != `["@r-1"]` {
		t.Errorf("erp = %v", got)
	}
	reconcileEntity(t, e, "serp", ziti.ServiceEdgeRouterPolicies, func() *zitiv1.ZitiServiceEdgeRouterPolicy { return &zitiv1.ZitiServiceEdgeRouterPolicy{} }, buildServiceEdgeRouterPolicy)
	got = only(e.zc.Objects[ziti.ServiceEdgeRouterPolicies])
	if got["semantic"] != "AnyOf" || rolesOf(got, "serviceRoles") != `["#web"]` || rolesOf(got, "edgeRouterRoles") != `["#all"]` {
		t.Errorf("serp = %v", got)
	}
}

func TestEntityNameConflictDeleteAndOrphan(t *testing.T) {
	newObj := func() *zitiv1.ZitiConfig { return &zitiv1.ZitiConfig{} }
	mk := func(policy zitiv1.DeletionPolicy) *zitiv1.ZitiConfig {
		c := common()
		c.DeletionPolicy = policy
		return &zitiv1.ZitiConfig{ObjectMeta: entityMeta("cfg"), Spec: zitiv1.ZitiConfigSpec{EntitySpec: c, Type: "intercept.v1",
			Data: apiextensionsv1.JSON{Raw: []byte(`{"k":"v"}`)}}}
	}

	e := newEntityEnv(t, zitiv1.RoleScopeGlobal, mk(zitiv1.DeletionPolicyDelete))
	e.zc.Put(ziti.Configs, ziti.Entity{"name": "team-a.cfg"})
	obj, _ := reconcileEntity(t, e, "cfg", ziti.Configs, newObj, buildConfig)
	if readyOf(obj).Reason != "NameConflict" {
		t.Errorf("ready = %+v", readyOf(obj))
	}

	for _, policy := range []zitiv1.DeletionPolicy{zitiv1.DeletionPolicyDelete, zitiv1.DeletionPolicyOrphan} {
		e := newEntityEnv(t, zitiv1.RoleScopeGlobal, mk(policy))
		obj, _ := reconcileEntity(t, e, "cfg", ziti.Configs, newObj, buildConfig)
		if err := e.k.Delete(t.Context(), obj); err != nil {
			t.Fatal(err)
		}
		r := newEntityReconciler(e.k, e.scheme, staticProvider{e.zc}, e.rec, "test", ziti.Configs, newObj, buildConfig)
		if _, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "team-a", Name: "cfg"}); err != nil {
			t.Fatal(err)
		}
		want := 0
		if policy == zitiv1.DeletionPolicyOrphan {
			want = 1
		}
		if n := len(e.zc.Objects[ziti.Configs]); n != want {
			t.Errorf("%s: configs = %d, want %d", policy, n, want)
		}
		for _, c := range e.zc.Objects[ziti.Configs] {
			if len(c.Tags()) != 0 || c["data"] == nil {
				t.Errorf("released config = %v", c)
			}
		}
	}
}

func TestEntityConfigReportsThatNoServiceUsesIt(t *testing.T) {
	newObj := func() *zitiv1.ZitiConfig { return &zitiv1.ZitiConfig{} }
	e := newEntityEnv(t, zitiv1.RoleScopeGlobal, &zitiv1.ZitiConfig{
		ObjectMeta: entityMeta("web-intercept"),
		Spec: zitiv1.ZitiConfigSpec{EntitySpec: common(), Type: "intercept.v1",
			Data: apiextensionsv1.JSON{Raw: []byte(`{"protocols":["tcp"]}`)}},
	})
	r := newEntityReconciler(e.k, e.scheme, staticProvider{e.zc}, e.rec, "cfg", ziti.Configs, newObj, buildConfig)
	r.Audit = auditConfig
	key := types.NamespacedName{Namespace: "team-a", Name: "web-intercept"}
	reconcile := func() *zitiv1.ZitiConfig {
		if _, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatal(err)
		}
		obj := &zitiv1.ZitiConfig{}
		if err := e.k.Get(t.Context(), key, obj); err != nil {
			t.Fatal(err)
		}
		return obj
	}
	inUseOf := func(o *zitiv1.ZitiConfig) metav1.Condition {
		for _, c := range o.Status.Conditions {
			if c.Type == CondInUse {
				return c
			}
		}
		return metav1.Condition{}
	}

	obj := reconcile()
	if c := inUseOf(obj); c.Status != metav1.ConditionFalse || c.Reason != check.UnusedConfig || c.Message != "no service uses this config" {
		t.Errorf("unused: %+v", c)
	}
	if readyOf(obj).Status != metav1.ConditionTrue {
		t.Error("an unused config is a warning, it must not fail Ready")
	}

	e.zc.Put(ziti.Services, ziti.Entity{"id": "s-2", "name": "billing", "configs": []any{obj.Status.ZitiID}})
	if c := inUseOf(reconcile()); c.Status != metav1.ConditionTrue || c.Reason != "InUse" {
		t.Errorf("in use: %+v", c)
	}
}

func TestEntityAdoptTakesOverKeepsHandMadeFieldsAndReleasesOnDelete(t *testing.T) {
	newObj := func() *zitiv1.ZitiServicePolicy { return &zitiv1.ZitiServicePolicy{} }
	c := common()
	c.ManagementPolicy = zitiv1.ManagementAdopt
	pol := &zitiv1.ZitiServicePolicy{ObjectMeta: entityMeta("dial"), Spec: zitiv1.ZitiServicePolicySpec{EntitySpec: c, Type: "Dial",
		IdentityRoles: []string{"#users"}, ServiceRoles: []string{"#web"}}}
	e := newEntityEnv(t, zitiv1.RoleScopeGlobal, pol)
	e.zc.Put(ziti.ServicePolicies, ziti.Entity{"id": "hand", "name": "team-a.dial", "postureCheckRoles": []any{"#mfa"}, "tags": map[string]any{"team": "x"}})

	obj, _ := reconcileEntity(t, e, "dial", ziti.ServicePolicies, newObj, buildServicePolicy)
	if obj.Status.ZitiID != "hand" || readyOf(obj).Status != metav1.ConditionTrue {
		t.Fatalf("status = %+v, ready %+v", obj.Status, readyOf(obj))
	}
	got := e.zc.Objects[ziti.ServicePolicies]["hand"]
	if rolesOf(got, "postureCheckRoles") != `["#mfa"]` || rolesOf(got, "identityRoles") != `["#users"]` ||
		got.Tags()["team"] != "x" || got.Tags()["ziti-operator-adopted"] != "true" {
		t.Errorf("adopted = %v", got)
	}

	if err := e.k.Delete(t.Context(), obj); err != nil {
		t.Fatal(err)
	}
	r := newEntityReconciler(e.k, e.scheme, staticProvider{e.zc}, e.rec, "test", ziti.ServicePolicies, newObj, buildServicePolicy)
	if _, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "team-a", Name: "dial"}); err != nil {
		t.Fatal(err)
	}
	got, ok := e.zc.Objects[ziti.ServicePolicies]["hand"]
	if !ok || got.Tags()["team"] != "x" || got.Tags()["ziti-operator-adopted"] != nil || got.Tags()["ziti-operator-uid"] != nil {
		t.Errorf("after delete: %v", got)
	}
}

func TestEntityObserveReadsWithoutWriting(t *testing.T) {
	newObj := func() *zitiv1.ZitiService { return &zitiv1.ZitiService{} }
	c := common()
	c.ManagementPolicy, c.ZitiName = zitiv1.ManagementObserve, "legacy"
	svc := &zitiv1.ZitiService{ObjectMeta: entityMeta("legacy"), Spec: zitiv1.ZitiServiceSpec{EntitySpec: c}}
	e := newEntityEnv(t, zitiv1.RoleScopeGlobal, svc)

	obj, _ := reconcileEntity(t, e, "legacy", ziti.Services, newObj, buildService)
	if obj.Status.ZitiID != "s-1" || readyOf(obj).Reason != "Observed" {
		t.Fatalf("status = %+v, ready %+v", obj.Status, readyOf(obj))
	}
	for _, call := range e.zc.Calls {
		if !strings.HasPrefix(call, "list") {
			t.Errorf("observe wrote: %s", call)
		}
	}
	if len(obj.Finalizers) != 0 {
		t.Errorf("finalizers = %v", obj.Finalizers)
	}

	c.ZitiName = "nope"
	obj.Spec.EntitySpec = c
	if err := e.k.Update(t.Context(), obj); err != nil {
		t.Fatal(err)
	}
	obj, _ = reconcileEntity(t, e, "legacy", ziti.Services, newObj, buildService)
	if readyOf(obj).Reason != "NotFound" {
		t.Errorf("missing object: %+v", readyOf(obj))
	}
}
