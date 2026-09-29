// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func builder(mut func(*zitiv1.ZitiService, *zitiv1.ZitiConnection)) *Builder {
	svc := &zitiv1.ZitiService{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a", UID: "uid-1"},
		Spec: zitiv1.ZitiServiceSpec{
			Intercept: zitiv1.Intercept{Addresses: []string{"app.example.com"}, Ports: []int32{443}, Protocols: []string{"tcp"}},
			Host:      zitiv1.Host{Address: "10.0.0.5", Port: 8443},
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
	b := builder(func(s *zitiv1.ZitiService, _ *zitiv1.ZitiConnection) { s.Spec.ZitiName = "app.example.com" })
	if got := b.ZitiName(); got != "app.example.com" {
		t.Errorf("custom name = %q", got)
	}
}

func TestTags(t *testing.T) {
	tags := builder(nil).Tags()
	want := map[string]any{
		TagCluster: "default", TagKind: "ZitiService", TagNamespace: "team-a", TagName: "app", TagUID: "uid-1",
	}
	if !reflect.DeepEqual(tags, want) {
		t.Errorf("tags = %v", tags)
	}
}

func TestHostConfig(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*zitiv1.ZitiService, *zitiv1.ZitiConnection)
		want map[string]any
		err  string
	}{
		{"address tcp", nil, map[string]any{"address": "10.0.0.5", "port": int32(8443), "protocol": "tcp"}, ""},
		{"service ref", func(s *zitiv1.ZitiService, _ *zitiv1.ZitiConnection) {
			s.Spec.Host = zitiv1.Host{ServiceRef: &zitiv1.ServiceRef{Name: "web", Port: 80}}
		}, map[string]any{"address": "web.team-a.svc", "port": int32(80), "protocol": "tcp"}, ""},
		{"udp only", func(s *zitiv1.ZitiService, _ *zitiv1.ZitiConnection) { s.Spec.Intercept.Protocols = []string{"udp"} },
			map[string]any{"address": "10.0.0.5", "port": int32(8443), "protocol": "udp"}, ""},
		{"forward protocol", func(s *zitiv1.ZitiService, _ *zitiv1.ZitiConnection) {
			s.Spec.Intercept.Protocols = []string{"tcp", "udp"}
			s.Spec.Host.ForwardProtocol = true
		}, map[string]any{"address": "10.0.0.5", "port": int32(8443), "forwardProtocol": true, "allowedProtocols": []string{"tcp", "udp"}}, ""},
		{"udp mismatch", func(s *zitiv1.ZitiService, _ *zitiv1.ZitiConnection) {
			s.Spec.Intercept.Protocols = []string{"tcp", "udp"}
		},
			nil, "forwardProtocol"},
		{"both targets", func(s *zitiv1.ZitiService, _ *zitiv1.ZitiConnection) {
			s.Spec.Host.ServiceRef = &zitiv1.ServiceRef{Name: "web", Port: 80}
		}, nil, "exactly one"},
		{"no target", func(s *zitiv1.ZitiService, _ *zitiv1.ZitiConnection) { s.Spec.Host = zitiv1.Host{} }, nil, "exactly one"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, err := builder(tc.mut).HostConfig("ct-host")
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(e["data"], tc.want) {
				t.Errorf("data = %v, want %v", e["data"], tc.want)
			}
			if e.Name() != "team-a.app-host.v1" || e["configTypeId"] != "ct-host" {
				t.Errorf("name/type = %v/%v", e.Name(), e["configTypeId"])
			}
		})
	}
}

func TestInterceptConfig(t *testing.T) {
	b := builder(func(s *zitiv1.ZitiService, _ *zitiv1.ZitiConnection) {
		s.Spec.Intercept.Ports = []int32{80, 443}
		s.Spec.Intercept.Protocols = nil
	})
	data := b.InterceptConfig("ct-i")["data"].(map[string]any)
	if !reflect.DeepEqual(data["protocols"], []string{"tcp"}) {
		t.Errorf("protocols = %v", data["protocols"])
	}
	if len(data["portRanges"].([]map[string]any)) != 2 {
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
		return builder(func(s *zitiv1.ZitiService, c *zitiv1.ZitiConnection) {
			s.Spec.RoleAttributes = []string{"tenant"}
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
	b := builder(func(s *zitiv1.ZitiService, _ *zitiv1.ZitiConnection) { s.Spec.EdgeRouters = []string{"r-x", "r-admin"} })
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
	b := builder(func(s *zitiv1.ZitiService, _ *zitiv1.ZitiConnection) { s.Spec.HostingRouter = "r-x" })
	if _, err := b.Bind("s"); err == nil || !strings.Contains(err.Error(), "hostingRouters") {
		t.Errorf("router outside allow-list: %v", err)
	}
	b = builder(func(_ *zitiv1.ZitiService, c *zitiv1.ZitiConnection) { c.Spec.DefaultEdgeRouters = []string{"missing"} })
	if _, err := b.SERP("s"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown router: %v", err)
	}
	b = builder(func(s *zitiv1.ZitiService, _ *zitiv1.ZitiConnection) { s.Spec.HostingRouter = "r-admin" })
	bind, _ := b.Bind("s")
	if !reflect.DeepEqual(bind["identityRoles"], []string{"@id-admin"}) {
		t.Errorf("explicit hosting router: %v", bind["identityRoles"])
	}
}

func TestDial(t *testing.T) {
	e, err := builder(nil).Dial("svc-1")
	if e != nil || err != nil {
		t.Errorf("no access roles: %v %v", e, err)
	}
	b := builder(func(s *zitiv1.ZitiService, _ *zitiv1.ZitiConnection) {
		s.Spec.Access.IdentityRoles = []string{"#staff"}
	})
	e, err = b.Dial("svc-1")
	if err != nil || !reflect.DeepEqual(e["identityRoles"], []string{"#team-a.staff"}) || e["type"] != "Dial" {
		t.Errorf("dial = %v %v", e, err)
	}
	b = builder(func(s *zitiv1.ZitiService, _ *zitiv1.ZitiConnection) { s.Spec.Access.IdentityRoles = []string{"#all"} })
	if _, err := b.Dial("svc-1"); err == nil {
		t.Error("namespaced #all must fail")
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
