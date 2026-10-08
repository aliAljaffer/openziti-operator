// SPDX-License-Identifier: Apache-2.0

package check

import (
	"slices"
	"testing"
	"time"

	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func TestSelects(t *testing.T) {
	tests := []struct {
		name     string
		roles    []string
		semantic string
		id       string
		attrs    []string
		want     bool
	}{
		{"empty roles select nothing", nil, "AnyOf", "a", []string{"x"}, false},
		{"all", []string{"#all"}, "AnyOf", "a", nil, true},
		{"attribute", []string{"#x"}, "AnyOf", "a", []string{"x"}, true},
		{"attribute missing", []string{"#y"}, "AnyOf", "a", []string{"x"}, false},
		{"id", []string{"@a"}, "AnyOf", "a", nil, true},
		{"other id", []string{"@b"}, "AnyOf", "a", nil, false},
		{"anyof one of two", []string{"#x", "#y"}, "AnyOf", "a", []string{"x"}, true},
		{"allof one of two", []string{"#x", "#y"}, "AllOf", "a", []string{"x"}, false},
		{"allof both", []string{"#x", "#y"}, "AllOf", "a", []string{"x", "y"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Selects(tc.roles, tc.semantic, tc.id, tc.attrs); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func e(kv ...any) ziti.Entity {
	out := ziti.Entity{}
	for i := 0; i < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func l(s ...string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func codes(fs []Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Code)
	}
	slices.Sort(out)
	return out
}

func healthy() *Graph {
	return &Graph{
		ConfigTypes: []ziti.Entity{e("id", "ti", "name", "intercept.v1"), e("id", "th", "name", "host.v1")},
		Configs: []ziti.Entity{
			e("id", "ci", "name", "s-intercept", "configTypeId", "ti", "data", map[string]any{"protocols": l("tcp")}),
			e("id", "ch", "name", "s-host", "configTypeId", "th", "data", map[string]any{"protocol": "tcp"}),
		},
		Services: []ziti.Entity{e("id", "s", "name", "s", "roleAttributes", l("tenant"), "configs", l("ci", "ch"))},
		Routers: []ziti.Entity{
			e("id", "r-host", "name", "r-host", "isOnline", true, "roleAttributes", l()),
			e("id", "r-entry", "name", "r-entry", "isOnline", true, "roleAttributes", l("entry")),
		},
		Identities: []ziti.Entity{
			e("id", "r-host", "name", "r-host", "roleAttributes", l()),
			e("id", "u1", "name", "u1", "roleAttributes", l("tenant")),
		},
		ServicePolicies: []ziti.Entity{
			e("id", "pb", "name", "s-bind", "type", "Bind", "semantic", "AnyOf", "identityRoles", l("@r-host"), "serviceRoles", l("@s")),
			e("id", "pd", "name", "tenant.dial", "type", "Dial", "semantic", "AnyOf", "identityRoles", l("#tenant"), "serviceRoles", l("#tenant")),
		},
		SERPs:       []ziti.Entity{e("id", "sp", "name", "s-serp", "semantic", "AnyOf", "edgeRouterRoles", l("@r-host", "@r-entry"), "serviceRoles", l("@s"))},
		ERPs:        []ziti.Entity{e("id", "ep", "name", "tenant.erp", "semantic", "AnyOf", "identityRoles", l("#tenant"), "edgeRouterRoles", l("#entry"))},
		Terminators: []ziti.Entity{e("id", "t", "serviceId", "s", "routerId", "r-host")},
	}
}

func TestServiceHealthy(t *testing.T) {
	g := healthy()
	r := g.Service(g.Services[0])
	if !r.Hosted || !r.Dialable || !r.RoutePath || len(r.Findings) != 0 {
		t.Errorf("report = %+v", r)
	}
	if !slices.Equal(r.Dialers, []string{"u1"}) {
		t.Errorf("dialers = %v", r.Dialers)
	}
}

func TestServiceProblems(t *testing.T) {
	tests := []struct {
		name      string
		mut       func(*Graph)
		hosted    bool
		dialable  bool
		routePath bool
		codes     []string
	}{
		{"no terminator", func(g *Graph) { g.Terminators = nil }, false, true, true, []string{NoTerminator}},
		{"no dialer", func(g *Graph) { g.ServicePolicies = g.ServicePolicies[:1] }, true, false, false, []string{NoDialer}},
		{"empty dial roles", func(g *Graph) { g.ServicePolicies[1]["serviceRoles"] = l() }, true, false, false, []string{NoDialer}},
		{"no bind", func(g *Graph) { g.ServicePolicies = g.ServicePolicies[1:] }, true, true, true, []string{NoBind}},
		{"inert bind", func(g *Graph) {
			g.SERPs[0]["edgeRouterRoles"] = l("@r-entry")
		}, true, true, true, []string{InertBind}},
		{"offline entry router", func(g *Graph) { g.Routers[1]["isOnline"] = false }, true, true, false, []string{OfflinePath}},
		{"erp router not in serp", func(g *Graph) {
			g.ERPs[0]["edgeRouterRoles"] = l("@r-host", "#nomatch")
			g.SERPs[0]["edgeRouterRoles"] = l("@r-entry", "@r-host")
			g.Routers[0]["isOnline"] = true
		}, true, true, true, nil},
		{"no common router", func(g *Graph) { g.ERPs[0]["edgeRouterRoles"] = l("#nomatch") }, true, true, false, []string{NoCommonRouter}},
		{"udp intercepted not hosted", func(g *Graph) {
			g.Configs[0]["data"] = map[string]any{"protocols": l("tcp", "udp")}
			g.Configs[1]["data"] = map[string]any{"forwardProtocol": true, "allowedProtocols": l("tcp")}
		}, true, true, true, []string{ProtocolMismatch}},
		{"forward all protocols", func(g *Graph) {
			g.Configs[0]["data"] = map[string]any{"protocols": l("tcp", "udp")}
			g.Configs[1]["data"] = map[string]any{"forwardProtocol": true}
		}, true, true, true, nil},
		{"single protocol mismatch", func(g *Graph) { g.Configs[0]["data"] = map[string]any{"protocols": l("udp")} }, true, true, true, []string{ProtocolMismatch}},
		{"missing host config", func(g *Graph) { g.Services[0]["configs"] = l("ci") }, true, true, true, []string{MissingConfig}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := healthy()
			tc.mut(g)
			r := g.Service(g.Services[0])
			if r.Hosted != tc.hosted || r.Dialable != tc.dialable || r.RoutePath != tc.routePath {
				t.Errorf("hosted/dialable/routePath = %v/%v/%v, want %v/%v/%v", r.Hosted, r.Dialable, r.RoutePath, tc.hosted, tc.dialable, tc.routePath)
			}
			if got := codes(r.Findings); !slices.Equal(got, tc.codes) {
				t.Errorf("codes = %v, want %v", got, tc.codes)
			}
		})
	}
}

func TestRouterReport(t *testing.T) {
	tests := []struct {
		name     string
		mut      func(*Graph)
		router   int
		services []string
		expected []string
		missing  []string
	}{
		{"terminates what it was picked for", nil, 0, []string{"s"}, []string{"s"}, nil},
		{"picked for a service it does not terminate", nil, 1, nil, []string{"s"}, []string{"s"}},
		{"no serp picks this router", func(g *Graph) {
			g.SERPs[0]["edgeRouterRoles"] = l("@r-host")
		}, 1, nil, nil, nil},
		{"picked by attribute", func(g *Graph) {
			g.SERPs[0]["edgeRouterRoles"] = l("#edge")
			g.Routers[1]["roleAttributes"] = l("edge")
		}, 1, nil, []string{"s"}, []string{"s"}},
		{"terminator for an unknown service is ignored", func(g *Graph) {
			g.Terminators = append(g.Terminators, e("id", "t2", "serviceId", "gone", "routerId", "r-host"))
		}, 0, []string{"s"}, []string{"s"}, nil},
		{"terminator on another router is ignored", func(g *Graph) {
			g.Terminators = append(g.Terminators, e("id", "t3", "serviceId", "s", "routerId", "r-other"))
		}, 0, []string{"s"}, []string{"s"}, nil},
		{"two services, one missing", func(g *Graph) {
			g.Services = append(g.Services, e("id", "s2", "name", "s2"))
			g.SERPs[0]["serviceRoles"] = l("@s", "@s2")
		}, 0, []string{"s"}, []string{"s", "s2"}, []string{"s2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := healthy()
			if tc.mut != nil {
				tc.mut(g)
			}
			rt := g.Routers[tc.router]
			r := g.Router(rt)
			if !slices.Equal(r.Services, tc.services) {
				t.Errorf("services = %v, want %v", r.Services, tc.services)
			}
			if !slices.Equal(r.Expected, tc.expected) {
				t.Errorf("expected = %v, want %v", r.Expected, tc.expected)
			}
			if !slices.Equal(r.Missing(), tc.missing) {
				t.Errorf("missing = %v, want %v", r.Missing(), tc.missing)
			}
		})
	}
}

func TestRouterNotTerminating(t *testing.T) {
	g := healthy()
	fs := g.Router(g.Routers[1]).NotTerminating(str(g.Routers[1], "name"))
	if len(fs) != 1 || fs[0].Code != NoTerminator || fs[0].Entity != "r-entry" {
		t.Errorf("findings = %+v", fs)
	}
	if fs := g.Router(g.Routers[0]).NotTerminating("r-host"); len(fs) != 0 {
		t.Errorf("findings = %+v", fs)
	}
}

func TestConfigUsers(t *testing.T) {
	g := healthy()
	if got := ConfigUsers(g.Services, "ci"); !slices.Equal(got, []string{"s"}) {
		t.Errorf("users = %v", got)
	}
	if got := ConfigUsers(g.Services, "nope"); len(got) != 0 {
		t.Errorf("users = %v", got)
	}
	g.Services = append(g.Services, e("id", "s2", "name", "a-svc", "configs", l("ci", "ch")))
	if got := ConfigUsers(g.Services, "ci"); !slices.Equal(got, []string{"a-svc", "s"}) {
		t.Errorf("users = %v, want sorted", got)
	}
}

func TestLoadInto(t *testing.T) {
	f := ziti.NewFake()
	f.Put(ziti.Services, ziti.Entity{"id": "s", "name": "s", "configs": []any{"ci"}})
	f.Put(ziti.Configs, ziti.Entity{"id": "ci", "name": "c"})
	f.Put(ziti.EdgeRouters, ziti.Entity{"id": "r", "name": "r"})
	var g Graph
	if err := LoadInto(t.Context(), f, &g, ziti.Services, ziti.Configs, ziti.EdgeRouters, ziti.Kind("unknown")); err != nil {
		t.Fatal(err)
	}
	if len(g.Services) != 1 || len(g.Configs) != 1 || len(g.Routers) != 1 || g.Terminators != nil {
		t.Errorf("graph = %+v", g)
	}
	if got := ConfigUsers(g.Services, "ci"); !slices.Equal(got, []string{"s"}) {
		t.Errorf("users = %v", got)
	}
}

func TestAudit(t *testing.T) {
	g := healthy()
	g.Configs = append(g.Configs, e("id", "cx", "name", "orphan", "configTypeId", "ti"))
	g.SERPs = append(g.SERPs, e("id", "se", "name", "hoop.serp", "semantic", "AnyOf", "edgeRouterRoles", l("@r-host"), "serviceRoles", l()))
	g.ERPs = append(g.ERPs, e("id", "es", "name", "edge-router-x-system", "isSystem", true, "identityRoles", l(), "edgeRouterRoles", l()))
	g.Identities = append(g.Identities,
		e("id", "u2", "name", "stale", "roleAttributes", l(), "enrollment", map[string]any{"ott": map[string]any{"expiresAt": "2026-01-01T00:00:00Z"}}),
		e("id", "u3", "name", "pending", "roleAttributes", l(), "enrollment", map[string]any{"ott": map[string]any{"expiresAt": "2027-01-01T00:00:00Z"}}),
	)
	got := codes(g.Audit(time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)))
	want := []string{EmptyRoles, ExpiredEnroll, NoTerminator, UnusedConfig}
	if !slices.Equal(got, want) {
		t.Errorf("codes = %v, want %v", got, want)
	}
}

func TestLoadAndAudit(t *testing.T) {
	f := ziti.NewFake()
	f.Put(ziti.Configs, ziti.Entity{"name": "orphan-config"})
	f.Put(ziti.ServicePolicies, ziti.Entity{"name": "empty-dial", "type": "Dial", "identityRoles": []string{}, "serviceRoles": []string{"#x"}})
	g, err := Load(t.Context(), f)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, fd := range g.Audit(time.Now()) {
		got[fd.Code] = fd.Entity
	}
	if got[UnusedConfig] != "orphan-config" || got[EmptyRoles] != "empty-dial" {
		t.Errorf("findings = %v", got)
	}
}

func TestHostV2ProtocolCheck(t *testing.T) {
	build := func(allowed []any) *Graph {
		return &Graph{
			ConfigTypes: []ziti.Entity{{"id": "ti", "name": "intercept.v1"}, {"id": "th2", "name": "host.v2"}},
			Configs: []ziti.Entity{
				{"id": "ci", "configTypeId": "ti", "data": map[string]any{"protocols": []any{"tcp", "udp"}}},
				{"id": "ch", "configTypeId": "th2", "data": map[string]any{"terminators": []any{
					map[string]any{"forwardProtocol": true, "allowedProtocols": allowed},
					map[string]any{"protocol": "tcp"},
				}}},
			},
		}
	}
	svc := ziti.Entity{"id": "s", "name": "s", "configs": []any{"ci", "ch"}}
	fs := build([]any{"tcp"}).protocolFindings(svc)
	if len(fs) != 1 || fs[0].Code != ProtocolMismatch {
		t.Errorf("tcp only hosts: %v", fs)
	}
	if fs := build([]any{"tcp", "udp"}).protocolFindings(svc); len(fs) != 0 {
		t.Errorf("one terminator allows udp: %v", fs)
	}
}
