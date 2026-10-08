// SPDX-License-Identifier: Apache-2.0

package check

import (
	"slices"
	"strings"
	"time"

	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type Graph struct {
	Services        []ziti.Entity
	Configs         []ziti.Entity
	ConfigTypes     []ziti.Entity
	ServicePolicies []ziti.Entity
	SERPs           []ziti.Entity
	ERPs            []ziti.Entity
	Routers         []ziti.Entity
	Identities      []ziti.Entity
	Terminators     []ziti.Entity
}

const (
	NoTerminator     = "NoTerminator"
	NoBind           = "NoBind"
	InertBind        = "InertBind"
	NoDialer         = "NoDialer"
	NoCommonRouter   = "NoCommonRouter"
	OfflinePath      = "OfflinePath"
	ProtocolMismatch = "ProtocolMismatch"
	MissingConfig    = "MissingConfig"
	UnusedConfig     = "UnusedConfig"
	EmptyRoles       = "EmptyRoles"
	ExpiredEnroll    = "ExpiredEnrollment"
)

type Finding struct {
	Code    string `json:"code"`
	Entity  string `json:"entity"`
	Message string `json:"message"`
}

type ServiceReport struct {
	Hosted    bool
	Dialable  bool
	RoutePath bool
	Dialers   []string
	Findings  []Finding
}

func str(e ziti.Entity, k string) string { s, _ := e[k].(string); return s }

func strs(e ziti.Entity, k string) []string {
	l, _ := e[k].([]any)
	out := make([]string, 0, len(l))
	for _, v := range l {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Selects reports whether roles select the entity. Roles are "#all", "#attribute", or "@id".
func Selects(roles []string, semantic string, id string, attrs []string) bool {
	if len(roles) == 0 {
		return false
	}
	match := func(r string) bool {
		switch {
		case r == "#all":
			return true
		case strings.HasPrefix(r, "#"):
			return slices.Contains(attrs, r[1:])
		case strings.HasPrefix(r, "@"):
			return r[1:] == id
		}
		return false
	}
	if semantic == "AllOf" {
		return slices.ContainsFunc(roles, func(r string) bool { return r == "#all" }) || allMatch(roles, match)
	}
	return slices.ContainsFunc(roles, match)
}

func allMatch(roles []string, match func(string) bool) bool {
	for _, r := range roles {
		if !match(r) {
			return false
		}
	}
	return true
}

func selectedBy(policy ziti.Entity, rolesKey string, target ziti.Entity) bool {
	return Selects(strs(policy, rolesKey), str(policy, "semantic"), str(target, "id"), strs(target, "roleAttributes"))
}

func (g *Graph) ids(entities []ziti.Entity, policy ziti.Entity, rolesKey string) []string {
	var out []string
	for _, e := range entities {
		if selectedBy(policy, rolesKey, e) {
			out = append(out, str(e, "id"))
		}
	}
	return out
}

// Selected returns the ids of the entities a policy selects through rolesKey.
func Selected(entities []ziti.Entity, policy ziti.Entity, rolesKey string) []string {
	return (*Graph)(nil).ids(entities, policy, rolesKey)
}

func (g *Graph) policiesFor(policies []ziti.Entity, svc ziti.Entity, typ string) []ziti.Entity {
	var out []ziti.Entity
	for _, p := range policies {
		if (typ == "" || str(p, "type") == typ) && selectedBy(p, "serviceRoles", svc) {
			out = append(out, p)
		}
	}
	return out
}

func (g *Graph) router(id string) (ziti.Entity, bool) {
	for _, r := range g.Routers {
		if str(r, "id") == id {
			return r, true
		}
	}
	return nil, false
}

func (g *Graph) Service(svc ziti.Entity) ServiceReport {
	var r ServiceReport
	name := str(svc, "name")
	add := func(code, msg string) { r.Findings = append(r.Findings, Finding{code, name, msg}) }

	serpRouters := map[string]bool{}
	for _, p := range g.policiesFor(g.SERPs, svc, "") {
		for _, id := range g.ids(g.Routers, p, "edgeRouterRoles") {
			serpRouters[id] = true
		}
	}

	r.Hosted = slices.ContainsFunc(g.Terminators, func(t ziti.Entity) bool { return str(t, "serviceId") == str(svc, "id") })
	if !r.Hosted {
		add(NoTerminator, "no router hosts this service")
	}

	binds := g.policiesFor(g.ServicePolicies, svc, "Bind")
	if len(binds) == 0 {
		add(NoBind, "no Bind policy selects this service")
	}
	for _, p := range binds {
		for _, id := range g.ids(g.Identities, p, "identityRoles") {
			if _, isRouter := g.router(id); isRouter && !serpRouters[id] {
				add(InertBind, "router "+id+" binds but is not in a service edge router policy for this service")
			}
		}
	}

	dialers := map[string]bool{}
	for _, p := range g.policiesFor(g.ServicePolicies, svc, "Dial") {
		for _, id := range g.ids(g.Identities, p, "identityRoles") {
			dialers[id] = true
		}
	}
	r.Dialers = sortedKeys(dialers)
	r.Dialable = len(r.Dialers) > 0
	if !r.Dialable {
		add(NoDialer, "no Dial policy selects an identity")
	}

	var offline, noCommon []string
	for _, id := range r.Dialers {
		ident := g.identity(id)
		var common, online int
		for _, erp := range g.ERPs {
			if !selectedBy(erp, "identityRoles", ident) {
				continue
			}
			for _, rid := range g.ids(g.Routers, erp, "edgeRouterRoles") {
				if !serpRouters[rid] {
					continue
				}
				common++
				if rt, _ := g.router(rid); rt["isOnline"] == true {
					online++
				}
			}
		}
		switch {
		case online > 0:
			r.RoutePath = true
		case common > 0:
			offline = append(offline, str(ident, "name"))
		default:
			noCommon = append(noCommon, str(ident, "name"))
		}
	}
	if len(offline) > 0 {
		add(OfflinePath, "dialers only reach this service through offline routers: "+strings.Join(offline, ", "))
	}
	if len(noCommon) > 0 {
		add(NoCommonRouter, "dialers have no edge router policy router that is in a service edge router policy: "+strings.Join(noCommon, ", "))
	}

	r.Findings = append(r.Findings, g.protocolFindings(svc)...)
	return r
}

func (g *Graph) identity(id string) ziti.Entity {
	for _, i := range g.Identities {
		if str(i, "id") == id {
			return i
		}
	}
	return ziti.Entity{"id": id}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func (g *Graph) configOf(svc ziti.Entity, typeName string) (ziti.Entity, bool) {
	typeIDs := map[string]bool{}
	for _, t := range g.ConfigTypes {
		if str(t, "name") == typeName {
			typeIDs[str(t, "id")] = true
		}
	}
	for _, cid := range strs(svc, "configs") {
		for _, c := range g.Configs {
			if str(c, "id") == cid && typeIDs[str(c, "configTypeId")] {
				return c, true
			}
		}
	}
	return nil, false
}

// hostConfig finds the host.v2 or host.v1 config of a service.
func (g *Graph) hostConfig(svc ziti.Entity) (ziti.Entity, string, bool) {
	for _, typeName := range []string{"host.v2", "host.v1"} {
		if c, ok := g.configOf(svc, typeName); ok {
			return c, typeName, true
		}
	}
	return nil, "", false
}

func (g *Graph) protocolFindings(svc ziti.Entity) []Finding {
	name := str(svc, "name")
	icpt, iok := g.configOf(svc, "intercept.v1")
	host, hostType, hok := g.hostConfig(svc)
	var out []Finding
	if !iok {
		out = append(out, Finding{MissingConfig, name, "no intercept.v1 config"})
	}
	if !hok {
		out = append(out, Finding{MissingConfig, name, "no host.v1 or host.v2 config"})
	}
	if !iok || !hok {
		return out
	}
	hostData, _ := host["data"].(map[string]any)
	icptData, _ := icpt["data"].(map[string]any)
	allowed := hostProtocols(hostType, hostData)
	if allowed == nil {
		return out
	}
	for _, p := range anyStrings(icptData["protocols"]) {
		if !slices.Contains(allowed, p) {
			out = append(out, Finding{ProtocolMismatch, name, "intercept captures " + p + " but " + hostType + " does not allow it"})
		}
	}
	return out
}

// hostProtocols returns nil when the host config allows every protocol.
// For host.v2 it returns the union over all terminators.
func hostProtocols(typeName string, d map[string]any) []string {
	if typeName != "host.v2" {
		return terminatorProtocols(d)
	}
	var union []string
	terms, _ := d["terminators"].([]any)
	for _, t := range terms {
		td, _ := t.(map[string]any)
		p := terminatorProtocols(td)
		if p == nil {
			return nil
		}
		for _, x := range p {
			if !slices.Contains(union, x) {
				union = append(union, x)
			}
		}
	}
	return union
}

func terminatorProtocols(d map[string]any) []string {
	if fwd, _ := d["forwardProtocol"].(bool); fwd {
		if a := anyStrings(d["allowedProtocols"]); len(a) > 0 {
			return a
		}
		return nil
	}
	if p, _ := d["protocol"].(string); p != "" {
		return []string{p}
	}
	return nil
}

func anyStrings(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))
	for _, x := range l {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (g *Graph) Audit(now time.Time) []Finding {
	var out []Finding
	for _, s := range g.Services {
		out = append(out, g.Service(s).Findings...)
	}
	for _, r := range g.Routers {
		out = append(out, g.Router(r).NotTerminating(str(r, "name"))...)
	}

	used := map[string]bool{}
	for _, s := range g.Services {
		for _, id := range strs(s, "configs") {
			used[id] = true
		}
	}
	for _, c := range g.Configs {
		if !used[str(c, "id")] {
			out = append(out, Finding{UnusedConfig, str(c, "name"), "no service uses this config"})
		}
	}

	for _, set := range []struct {
		policies []ziti.Entity
		keys     []string
	}{
		{g.ServicePolicies, []string{"identityRoles", "serviceRoles"}},
		{g.SERPs, []string{"edgeRouterRoles", "serviceRoles"}},
		{g.ERPs, []string{"identityRoles", "edgeRouterRoles"}},
	} {
		for _, p := range set.policies {
			if sys, _ := p["isSystem"].(bool); sys {
				continue
			}
			for _, k := range set.keys {
				if len(strs(p, k)) == 0 {
					out = append(out, Finding{EmptyRoles, str(p, "name"), k + " is empty"})
				}
			}
		}
	}

	for _, i := range g.Identities {
		enr, _ := i["enrollment"].(map[string]any)
		for method, v := range enr {
			m, _ := v.(map[string]any)
			exp, _ := m["expiresAt"].(string)
			if t, err := time.Parse(time.RFC3339, exp); err == nil && t.Before(now) {
				out = append(out, Finding{ExpiredEnroll, str(i, "name"), method + " enrollment expired " + exp})
			}
		}
	}
	return out
}
