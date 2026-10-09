// SPDX-License-Identifier: Apache-2.0

package check

import (
	"slices"

	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

// RouterReport is what one edge router contributes to the service paths.
type RouterReport struct {
	// Services are the services that have a terminator on the router.
	Services []string
	// Expected are the services whose service edge router policy names the router.
	Expected []string
}

// Router reports the services a router terminates, and the services it was picked to terminate.
// A router needs a terminator only for the services it was picked for: one in an edge router policy
// alone only forwards traffic and needs none.
func (g *Graph) Router(rt ziti.Entity) RouterReport {
	id := str(rt, "id")
	attrs := strs(rt, "roleAttributes")

	hosting, expected := map[string]bool{}, map[string]bool{}
	for _, t := range g.Terminators {
		if str(t, "routerId") == id {
			if name := g.serviceName(str(t, "serviceId")); name != "" {
				hosting[name] = true
			}
		}
	}
	for _, p := range g.SERPs {
		if !Selects(strs(p, "edgeRouterRoles"), str(p, "semantic"), id, attrs) {
			continue
		}
		for _, s := range g.Services {
			if selectedBy(p, "serviceRoles", s) {
				expected[str(s, "name")] = true
			}
		}
	}
	return RouterReport{Services: sortedNames(hosting), Expected: sortedNames(expected)}
}

// Missing returns the services the router was picked to terminate but does not terminate, sorted.
func (r RouterReport) Missing() []string {
	have := map[string]bool{}
	for _, s := range r.Services {
		have[s] = true
	}
	out := make([]string, 0, len(r.Expected))
	for _, s := range r.Expected {
		if !have[s] {
			out = append(out, s)
		}
	}
	return out
}

// NotTerminating turns the missing terminators into audit findings for the router.
func (r RouterReport) NotTerminating(routerName string) []Finding {
	missing := r.Missing()
	out := make([]Finding, 0, len(missing))
	for _, s := range missing {
		out = append(out, Finding{NoTerminator, routerName, "router is in the service edge router policy of " + s + " but has no terminator for it"})
	}
	return out
}

// ConfigUsers returns the names of the services that use a config, sorted.
func ConfigUsers(services []ziti.Entity, configID string) []string {
	var out []string
	for _, s := range services {
		if slices.Contains(strs(s, "configs"), configID) {
			out = append(out, str(s, "name"))
		}
	}
	slices.Sort(out)
	return out
}

func (g *Graph) serviceName(id string) string {
	for _, s := range g.Services {
		if str(s, "id") == id {
			return str(s, "name")
		}
	}
	return ""
}

func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		if k != "" {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}
