// SPDX-License-Identifier: Apache-2.0

package check

import (
	"context"

	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

// Load reads the whole network. It only lists, so it is safe on a live controller.
func Load(ctx context.Context, zc ziti.Client) (*Graph, error) {
	g := &Graph{}
	for _, l := range []struct {
		kind ziti.Kind
		dst  *[]ziti.Entity
	}{
		{ziti.Services, &g.Services},
		{ziti.Configs, &g.Configs},
		{ziti.ConfigTypes, &g.ConfigTypes},
		{ziti.ServicePolicies, &g.ServicePolicies},
		{ziti.ServiceEdgeRouterPolicies, &g.SERPs},
		{ziti.EdgeRouterPolicies, &g.ERPs},
		{ziti.EdgeRouters, &g.Routers},
		{ziti.Identities, &g.Identities},
		{ziti.Terminators, &g.Terminators},
	} {
		list, err := zc.List(ctx, l.kind, "")
		if err != nil {
			return nil, err
		}
		*l.dst = list
	}
	return g, nil
}
