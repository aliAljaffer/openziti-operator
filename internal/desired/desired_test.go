// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func builder(mut func(*zitiv1.ZitiApp, *zitiv1.ZitiConnection)) *Builder {
	svc := &zitiv1.ZitiApp{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a", UID: "uid-1"},
		Spec: zitiv1.ZitiAppSpec{
			Expose:  zitiv1.Expose{Addresses: []string{"app.example.com"}, Ports: []intstr.IntOrString{intstr.FromInt32(443)}, Protocols: []string{"tcp"}},
			Targets: []zitiv1.Target{{Address: "10.0.0.5", Port: 8443}},
		},
	}
	conn := &zitiv1.ZitiConnection{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: zitiv1.ZitiConnectionSpec{
			HostingRouters:     []string{"r-main", "r-admin"},
			DefaultEdgeRouters: []string{"r-admin"},
		},
	}
	if mut != nil {
		mut(svc, conn)
	}
	return &Builder{Svc: svc, Conn: conn, RouterIDs: map[string]string{"r-main": "id-main", "r-admin": "id-admin", "r-x": "id-x"}}
}

func TestZitiName(t *testing.T) {
	if got := builder(nil).ZitiName(); got != "team-a.app" {
		t.Errorf("default name = %q", got)
	}
	b := builder(func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) { s.Spec.ZitiName = "app.example.com" })
	if got := b.ZitiName(); got != "app.example.com" {
		t.Errorf("custom name = %q", got)
	}
}

func TestTags(t *testing.T) {
	tags := builder(nil).Tags()
	want := map[string]any{
		TagCluster: "default", TagKind: "ZitiApp", TagNamespace: "team-a", TagName: "app", TagUID: "uid-1",
	}
	if !reflect.DeepEqual(tags, want) {
		t.Errorf("tags = %v", tags)
	}
}

func terminators(t *testing.T, e ziti.Entity) []map[string]any {
	t.Helper()
	return e["data"].(map[string]any)["terminators"].([]map[string]any)
}

func TestHostConfig(t *testing.T) {
	ports := func(p ...intstr.IntOrString) func(*zitiv1.ZitiApp, *zitiv1.ZitiConnection) {
		return func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) { s.Spec.Expose.Ports = p }
	}
	tests := []struct {
		name string
		mut  func(*zitiv1.ZitiApp, *zitiv1.ZitiConnection)
		want []map[string]any
		err  string
	}{
		{"fixed port", nil, []map[string]any{{"address": "10.0.0.5", "port": int32(8443), "forwardProtocol": true, "allowedProtocols": []string{"tcp"}}}, ""},
		{"kubernetes service", func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) {
			s.Spec.Targets = []zitiv1.Target{{KubernetesService: "web", Port: 80}}
		}, []map[string]any{{"address": "web.team-a.svc", "port": int32(80), "forwardProtocol": true, "allowedProtocols": []string{"tcp"}}}, ""},
		{"both protocols", func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) { s.Spec.Expose.Protocols = []string{"tcp", "udp"} },
			[]map[string]any{{"address": "10.0.0.5", "port": int32(8443), "forwardProtocol": true, "allowedProtocols": []string{"tcp", "udp"}}}, ""},
		{"no port forwards the dialed port", func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) {
			s.Spec.Expose.Ports = []intstr.IntOrString{intstr.FromInt32(443), intstr.FromString("8000-8005")}
			s.Spec.Targets = []zitiv1.Target{{Address: "10.0.0.5"}}
		}, []map[string]any{{"address": "10.0.0.5", "forwardPort": true, "forwardProtocol": true, "allowedProtocols": []string{"tcp"},
			"allowedPortRanges": []map[string]any{{"low": 443, "high": 443}, {"low": 8000, "high": 8005}}}}, ""},
		{"two targets with cost", func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) {
			s.Spec.Targets = []zitiv1.Target{{Address: "10.0.0.5", Port: 8443}, {Address: "10.0.0.6", Port: 8443, Cost: 10}}
		}, []map[string]any{
			{"address": "10.0.0.5", "port": int32(8443), "forwardProtocol": true, "allowedProtocols": []string{"tcp"}},
			{"address": "10.0.0.6", "port": int32(8443), "forwardProtocol": true, "allowedProtocols": []string{"tcp"}, "listenOptions": map[string]any{"cost": int32(10)}},
		}, ""},
		{"both address and service", func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) { s.Spec.Targets[0].KubernetesService = "web" }, nil, "not both"},
		{"neither", func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) { s.Spec.Targets = []zitiv1.Target{{Port: 1}} }, nil, "address or kubernetesService"},
		{"no targets", func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) { s.Spec.Targets = nil }, nil, "at least one"},
		{"bad range", ports(intstr.FromString("9000-8000")), nil, "low must not exceed high"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, err := builder(tc.mut).HostConfig("host.v2")
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := terminators(t, e); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("terminators = %v, want %v", got, tc.want)
			}
			if e.Name() != "team-a.app-host.v2" || e["configTypeId"] != "host.v2" {
				t.Errorf("name/type = %v/%v", e.Name(), e["configTypeId"])
			}
		})
	}
}

func TestPortRanges(t *testing.T) {
	got, err := PortRanges([]intstr.IntOrString{intstr.FromInt32(443), intstr.FromString("8000-8005"), intstr.FromString("22")})
	want := []map[string]any{{"low": 443, "high": 443}, {"low": 8000, "high": 8005}, {"low": 22, "high": 22}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, %v", got, err)
	}
	for _, bad := range []intstr.IntOrString{intstr.FromInt32(0), intstr.FromInt32(70000), intstr.FromString("80-"), intstr.FromString("a-b"), intstr.FromString("9-8"), intstr.FromString("1-70000")} {
		if _, err := PortRanges([]intstr.IntOrString{bad}); err == nil {
			t.Errorf("%v must be rejected", bad.String())
		}
	}
}

func TestInterceptConfig(t *testing.T) {
	b := builder(func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) {
		s.Spec.Expose.Ports = []intstr.IntOrString{intstr.FromInt32(80), intstr.FromString("8000-8005")}
		s.Spec.Expose.Protocols = nil
	})
	e, err := b.InterceptConfig("ct-i")
	if err != nil {
		t.Fatal(err)
	}
	data := e["data"].(map[string]any)
	if !reflect.DeepEqual(data["protocols"], []string{"tcp"}) {
		t.Errorf("protocols = %v", data["protocols"])
	}
	if !reflect.DeepEqual(data["portRanges"], []map[string]any{{"low": 80, "high": 80}, {"low": 8000, "high": 8005}}) {
		t.Errorf("portRanges = %v", data["portRanges"])
	}
}

func TestScopeRoles(t *testing.T) {
	tests := []struct {
		name  string
		scope zitiv1.RoleScope
		in    []string
		want  []string
		err   bool
	}{
		{"namespaced rewrites", zitiv1.RoleScopeNamespaced, []string{"#x", "#y"}, []string{"#ns.x", "#ns.y"}, false},
		{"namespaced rejects id", zitiv1.RoleScopeNamespaced, []string{"@abc"}, nil, true},
		{"namespaced rejects all", zitiv1.RoleScopeNamespaced, []string{"#all"}, nil, true},
		{"global passes through", zitiv1.RoleScopeGlobal, []string{"#all", "@abc", "#x"}, []string{"#all", "@abc", "#x"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ScopeRoles(tc.scope, "ns", tc.in)
			if (err != nil) != tc.err {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestServiceRoleAttributes(t *testing.T) {
	set := func(scope zitiv1.RoleScope) *Builder {
		return builder(func(s *zitiv1.ZitiApp, c *zitiv1.ZitiConnection) {
			s.Spec.MemberOf = []string{"tenant"}
			c.Spec.RoleScope = scope
		})
	}
	e, err := set(zitiv1.RoleScopeNamespaced).Service("c1", "c2")
	if err != nil || !reflect.DeepEqual(e["roleAttributes"], []string{"team-a.tenant"}) {
		t.Errorf("namespaced: %v %v", e["roleAttributes"], err)
	}
	e, err = set(zitiv1.RoleScopeGlobal).Service("c1", "c2")
	if err != nil || !reflect.DeepEqual(e["roleAttributes"], []string{"tenant"}) {
		t.Errorf("global: %v %v", e["roleAttributes"], err)
	}
	if e["encryptionRequired"] != true || e["terminatorStrategy"] != "smartrouting" {
		t.Errorf("service defaults: %v", e)
	}
}

func TestBindAndSERP(t *testing.T) {
	b := builder(func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) { s.Spec.EntryRouters = []string{"r-x", "r-admin"} })
	bind, err := b.Bind("svc-1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bind["identityRoles"], []string{"@id-main"}) || !reflect.DeepEqual(bind["serviceRoles"], []string{"@svc-1"}) || bind["type"] != "Bind" {
		t.Errorf("bind = %v", bind)
	}
	serp, err := b.SERP("svc-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"@id-main", "@id-x", "@id-admin"}
	if !reflect.DeepEqual(serp["edgeRouterRoles"], want) {
		t.Errorf("serp routers = %v, want %v", serp["edgeRouterRoles"], want)
	}
}

func TestHostingRouterErrors(t *testing.T) {
	b := builder(func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) { s.Spec.HostedBy = "r-x" })
	if _, err := b.Bind("s"); err == nil || !strings.Contains(err.Error(), "hostingRouters") {
		t.Errorf("router outside allow-list: %v", err)
	}
	b = builder(func(_ *zitiv1.ZitiApp, c *zitiv1.ZitiConnection) { c.Spec.DefaultEdgeRouters = []string{"missing"} })
	if _, err := b.SERP("s"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown router: %v", err)
	}
	b = builder(func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) { s.Spec.HostedBy = "r-admin" })
	bind, _ := b.Bind("s")
	if !reflect.DeepEqual(bind["identityRoles"], []string{"@id-admin"}) {
		t.Errorf("explicit hosting router: %v", bind["identityRoles"])
	}
}

func TestAllowedRolesDialAndERP(t *testing.T) {
	b := builder(nil)
	roles, missing, err := b.AllowedRoles(nil)
	if err != nil || len(roles) != 0 || len(missing) != 0 || b.Dial("svc-1", roles) != nil {
		t.Errorf("no allow: %v %v %v", roles, missing, err)
	}

	b = builder(func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) {
		s.Spec.Allow = zitiv1.Allow{Groups: []string{"staff"}, Identities: []string{"alice", "ghost"}}
		s.Spec.EntryRouters = []string{"r-x"}
	})
	roles, missing, err = b.AllowedRoles(map[string]string{"alice": "id-alice"})
	if err != nil || !reflect.DeepEqual(roles, []string{"#team-a.staff", "@id-alice"}) || !reflect.DeepEqual(missing, []string{"ghost"}) {
		t.Fatalf("roles = %v missing = %v err = %v", roles, missing, err)
	}
	dial := b.Dial("svc-1", roles)
	if dial["type"] != "Dial" || dial.Name() != "team-a.app-dial" || !reflect.DeepEqual(dial["serviceRoles"], []string{"@svc-1"}) {
		t.Errorf("dial = %v", dial)
	}
	erp, err := b.ERP(roles)
	if err != nil || erp.Name() != "team-a.app-erp" ||
		!reflect.DeepEqual(erp["identityRoles"], roles) || !reflect.DeepEqual(erp["edgeRouterRoles"], []string{"@id-x", "@id-admin"}) {
		t.Errorf("erp = %v %v", erp, err)
	}

	if e, err := builder(nil).ERP([]string{"#x"}); e != nil || err != nil {
		t.Errorf("no entryRouters must give no ERP: %v %v", e, err)
	}
	if e, err := b.ERP(nil); e != nil || err != nil {
		t.Errorf("no roles must give no ERP: %v %v", e, err)
	}

	b = builder(func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) { s.Spec.Allow.Groups = []string{"all"} })
	if _, _, err := b.AllowedRoles(nil); err == nil {
		t.Error("namespaced group all must fail")
	}
	b = builder(func(s *zitiv1.ZitiApp, c *zitiv1.ZitiConnection) {
		s.Spec.Allow.Groups = []string{"tenant"}
		c.Spec.RoleScope = zitiv1.RoleScopeGlobal
	})
	if roles, _, _ := b.AllowedRoles(nil); !reflect.DeepEqual(roles, []string{"#tenant"}) {
		t.Errorf("global roles = %v", roles)
	}
}

func TestMatches(t *testing.T) {
	want := ziti.Entity{
		"name":          "n",
		"type":          "Bind",
		"identityRoles": []string{"@b", "@a"},
		"data":          map[string]any{"port": int32(443), "ranges": []map[string]any{{"low": 1, "high": 2}}},
		"tags":          map[string]any{"k": "v"},
	}
	actual := func(mut func(ziti.Entity)) ziti.Entity {
		e := ziti.Entity{
			"id":            "x",
			"name":          "n",
			"type":          "Bind",
			"identityRoles": []any{"@a", "@b"},
			"data":          map[string]any{"port": float64(443), "ranges": []any{map[string]any{"low": float64(1), "high": float64(2)}}, "extra": true},
			"tags":          map[string]any{"k": "v", "other": "y"},
			"createdAt":     "now",
		}
		if mut != nil {
			mut(e)
		}
		return e
	}
	tests := []struct {
		name string
		mut  func(ziti.Entity)
		want bool
	}{
		{"equal ignoring order and extras", nil, true},
		{"name differs", func(e ziti.Entity) { e["name"] = "m" }, false},
		{"role added", func(e ziti.Entity) { e["identityRoles"] = []any{"@a", "@b", "@c"} }, false},
		{"role changed", func(e ziti.Entity) { e["identityRoles"] = []any{"@a", "@c"} }, false},
		{"port differs", func(e ziti.Entity) { e["data"].(map[string]any)["port"] = float64(80) }, false},
		{"tag missing", func(e ziti.Entity) { e["tags"] = map[string]any{} }, false},
		{"field missing", func(e ziti.Entity) { delete(e, "type") }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Matches(want, actual(tc.mut)); got != tc.want {
				t.Errorf("Matches = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestScopeAttributes(t *testing.T) {
	got, err := ScopeAttributes(zitiv1.RoleScopeNamespaced, "ns", []string{"a", "b"})
	if err != nil || !slices.Equal(got, []string{"ns.a", "ns.b"}) {
		t.Errorf("namespaced = %v, %v", got, err)
	}
	got, err = ScopeAttributes(zitiv1.RoleScopeGlobal, "ns", []string{"a"})
	if err != nil || !slices.Equal(got, []string{"a"}) {
		t.Errorf("global = %v, %v", got, err)
	}
	if _, err = ScopeAttributes(zitiv1.RoleScopeNamespaced, "ns", []string{"all"}); err == nil {
		t.Error("attribute all must be rejected when namespaced")
	}
}

func TestMatchesTreatsEmptyListAsMissing(t *testing.T) {
	want := ziti.Entity{"name": "p", "postureCheckRoles": []string{}}
	for name, got := range map[string]ziti.Entity{
		"missing": {"name": "p"},
		"null":    {"name": "p", "postureCheckRoles": nil},
		"empty":   {"name": "p", "postureCheckRoles": []any{}},
	} {
		if !Matches(want, got) {
			t.Errorf("%s: want match", name)
		}
	}
	if Matches(want, ziti.Entity{"name": "p", "postureCheckRoles": []any{"#x"}}) {
		t.Error("non-empty actual must not match empty desired")
	}
}
