// SPDX-License-Identifier: Apache-2.0

package check

import (
	"context"

	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

// Load reads the whole network. It only lists, so it is safe on a live controller.
func Load(ctx context.Context, zc ziti.Client) (*Graph, error) {
	g := &Graph{}
	if err := LoadInto(ctx, zc, g, allKinds...); err != nil {
		return nil, err
	}
	return g, nil
}

var allKinds = []ziti.Kind{
	ziti.Services, ziti.Configs, ziti.ConfigTypes, ziti.ServicePolicies,
	ziti.ServiceEdgeRouterPolicies, ziti.EdgeRouterPolicies,
	ziti.EdgeRouters, ziti.Identities, ziti.Terminators,
}

var graphField = map[ziti.Kind]func(*Graph, []ziti.Entity){
	ziti.Services:                  func(g *Graph, l []ziti.Entity) { g.Services = l },
	ziti.Configs:                   func(g *Graph, l []ziti.Entity) { g.Configs = l },
	ziti.ConfigTypes:               func(g *Graph, l []ziti.Entity) { g.ConfigTypes = l },
	ziti.ServicePolicies:           func(g *Graph, l []ziti.Entity) { g.ServicePolicies = l },
	ziti.ServiceEdgeRouterPolicies: func(g *Graph, l []ziti.Entity) { g.SERPs = l },
	ziti.EdgeRouterPolicies:        func(g *Graph, l []ziti.Entity) { g.ERPs = l },
	ziti.EdgeRouters:               func(g *Graph, l []ziti.Entity) { g.Routers = l },
	ziti.Identities:                func(g *Graph, l []ziti.Entity) { g.Identities = l },
	ziti.Terminators:               func(g *Graph, l []ziti.Entity) { g.Terminators = l },
}

// LoadInto reads the named kinds into g. A reconciler passes only the kinds its check reads, because
// Ziti cannot filter terminators by router and cannot filter services by config.
func LoadInto(ctx context.Context, zc ziti.Client, g *Graph, kinds ...ziti.Kind) error {
	for _, kind := range kinds {
		list, err := zc.List(ctx, kind, "")
		if err != nil {
			return err
		}
		if set := graphField[kind]; set != nil {
			set(g, list)
		}
	}
	return nil
}
