//go:build integration

package controller

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/openziti/edge-api/rest_util"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func TestOneToOneKindsAgainstRealController(t *testing.T) {
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

	named := func(n string) zitiv1.EntitySpec {
		c := common()
		c.ZitiName = n
		return c
	}
	cfgIntercept := &zitiv1.ZitiConfig{ObjectMeta: entityMeta("it-intercept"), Spec: zitiv1.ZitiConfigSpec{EntitySpec: named("it-fc-intercept"), Type: "intercept.v1",
		Data: apiextensionsv1.JSON{Raw: []byte(`{"protocols":["tcp"],"addresses":["it-fc.example.test"],"portRanges":[{"low":80,"high":80},{"low":8000,"high":8005}]}`)}}}
	cfgHost := &zitiv1.ZitiConfig{ObjectMeta: entityMeta("it-host"), Spec: zitiv1.ZitiConfigSpec{EntitySpec: named("it-fc-host"), Type: "host.v2",
		Data: apiextensionsv1.JSON{Raw: []byte(`{"terminators":[{"address":"10.0.0.5","port":8443,"protocol":"tcp"}]}`)}}}
	svc := &zitiv1.ZitiService{ObjectMeta: entityMeta("it-svc"), Spec: zitiv1.ZitiServiceSpec{EntitySpec: named("it-fc.example.test"),
		Configs: []string{"it-fc-intercept", "it-fc-host"}, RoleAttributes: []string{"it-fc"}}}
	bind := &zitiv1.ZitiServicePolicy{ObjectMeta: entityMeta("it-bind"), Spec: zitiv1.ZitiServicePolicySpec{EntitySpec: named("it-fc-bind"), Type: "Bind",
		IdentityRoles: []string{"@router-instance-1"}, ServiceRoles: []string{"@it-fc.example.test"}}}
	dial := &zitiv1.ZitiServicePolicy{ObjectMeta: entityMeta("it-dial"), Spec: zitiv1.ZitiServicePolicySpec{EntitySpec: named("it-fc-dial"), Type: "Dial",
		IdentityRoles: []string{"#it-fc-users"}, ServiceRoles: []string{"#it-fc"}}}
	serp := &zitiv1.ZitiServiceEdgeRouterPolicy{ObjectMeta: entityMeta("it-serp"), Spec: zitiv1.ZitiServiceEdgeRouterPolicySpec{EntitySpec: named("it-fc-serp"),
		ServiceRoles: []string{"#it-fc"}, EdgeRouterRoles: []string{"@router-instance-1"}}}
	erp := &zitiv1.ZitiEdgeRouterPolicy{ObjectMeta: entityMeta("it-erp"), Spec: zitiv1.ZitiEdgeRouterPolicySpec{EntitySpec: named("it-fc-erp"),
		Semantic: "AllOf", IdentityRoles: []string{"#it-fc-users"}, EdgeRouterRoles: []string{"@router-instance-1"}}}

	e := newEntityEnv(t, zitiv1.RoleScopeGlobal, cfgIntercept, cfgHost, svc, bind, dial, serp, erp)
	e.zc = nil
	provider := staticProvider{wc}
	reconcile := func(t *testing.T, name string, run func(r ctrl.Request) error) {
		t.Helper()
		if err := run(ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: name}}); err != nil {
			t.Fatalf("reconcile %s: %v", name, err)
		}
	}
	rc := newEntityReconciler(e.k, e.scheme, provider, e.rec, "cfg", ziti.Configs, func() *zitiv1.ZitiConfig { return &zitiv1.ZitiConfig{} }, buildConfig)
	rs := newEntityReconciler(e.k, e.scheme, provider, e.rec, "svc", ziti.Services, func() *zitiv1.ZitiService { return &zitiv1.ZitiService{} }, buildService)
	rp := newEntityReconciler(e.k, e.scheme, provider, e.rec, "sp", ziti.ServicePolicies, func() *zitiv1.ZitiServicePolicy { return &zitiv1.ZitiServicePolicy{} }, buildServicePolicy)
	rse := newEntityReconciler(e.k, e.scheme, provider, e.rec, "serp", ziti.ServiceEdgeRouterPolicies, func() *zitiv1.ZitiServiceEdgeRouterPolicy { return &zitiv1.ZitiServiceEdgeRouterPolicy{} }, buildServiceEdgeRouterPolicy)
	re := newEntityReconciler(e.k, e.scheme, provider, e.rec, "erp", ziti.EdgeRouterPolicies, func() *zitiv1.ZitiEdgeRouterPolicy { return &zitiv1.ZitiEdgeRouterPolicy{} }, buildEdgeRouterPolicy)
	all := []struct {
		name string
		run  func(ctrl.Request) error
	}{
		{"it-intercept", func(r ctrl.Request) error { _, err := rc.Reconcile(t.Context(), r); return err }},
		{"it-host", func(r ctrl.Request) error { _, err := rc.Reconcile(t.Context(), r); return err }},
		{"it-svc", func(r ctrl.Request) error { _, err := rs.Reconcile(t.Context(), r); return err }},
		{"it-bind", func(r ctrl.Request) error { _, err := rp.Reconcile(t.Context(), r); return err }},
		{"it-dial", func(r ctrl.Request) error { _, err := rp.Reconcile(t.Context(), r); return err }},
		{"it-serp", func(r ctrl.Request) error { _, err := rse.Reconcile(t.Context(), r); return err }},
		{"it-erp", func(r ctrl.Request) error { _, err := re.Reconcile(t.Context(), r); return err }},
	}
	t.Cleanup(func() {
		ctx := context.Background()
		for _, kind := range []ziti.Kind{ziti.ServicePolicies, ziti.ServiceEdgeRouterPolicies, ziti.EdgeRouterPolicies, ziti.Services, ziti.Configs} {
			for _, uid := range []string{"uid-it-intercept", "uid-it-host", "uid-it-svc", "uid-it-bind", "uid-it-dial", "uid-it-serp", "uid-it-erp"} {
				list, _ := real.List(ctx, kind, tagFilter(types.UID(uid)))
				for _, x := range list {
					_ = real.Delete(ctx, kind, x.ID())
				}
			}
		}
	})

	for _, step := range all {
		reconcile(t, step.name, step.run)
	}
	wc.writes = nil
	for _, step := range all {
		reconcile(t, step.name, step.run)
	}
	if len(wc.writes) != 0 {
		t.Fatalf("second pass wrote: %v", wc.writes)
	}

	for kind, want := range map[ziti.Kind]int{ziti.Configs: 2, ziti.Services: 1, ziti.ServicePolicies: 2, ziti.ServiceEdgeRouterPolicies: 1, ziti.EdgeRouterPolicies: 1} {
		n := 0
		for _, uid := range []string{"uid-it-intercept", "uid-it-host", "uid-it-svc", "uid-it-bind", "uid-it-dial", "uid-it-serp", "uid-it-erp"} {
			list, _ := real.List(t.Context(), kind, tagFilter(types.UID(uid)))
			n += len(list)
		}
		if n != want {
			t.Errorf("%s = %d, want %d", kind, n, want)
		}
	}
}
