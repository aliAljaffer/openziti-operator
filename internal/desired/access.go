// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"fmt"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func AccessName(ap *zitiv1.ZitiAccessPolicy) string {
	if ap.Spec.ZitiName != "" {
		return ap.Spec.ZitiName
	}
	return ap.Namespace + "." + ap.Name
}

func scopeOf(conn *zitiv1.ZitiConnection) zitiv1.RoleScope {
	if conn.Spec.RoleScope == "" {
		return zitiv1.RoleScopeNamespaced
	}
	return conn.Spec.RoleScope
}

func access(ap *zitiv1.ZitiAccessPolicy, conn *zitiv1.ZitiConnection, suffix string) ziti.Entity {
	return ziti.Entity{
		"name":     AccessName(ap) + suffix,
		"semantic": "AnyOf",
		"tags":     ownerTags(conn, "ZitiAccessPolicy", &ap.ObjectMeta),
	}
}

// AccessDial builds <name>.dial. Roles pass through ScopeRoles, so a namespaced CR only reaches its own namespace.
func AccessDial(ap *zitiv1.ZitiAccessPolicy, conn *zitiv1.ZitiConnection) (ziti.Entity, error) {
	scope, ns := scopeOf(conn), ap.Namespace
	ident, err := ScopeRoles(scope, ns, ap.Spec.IdentityRoles)
	if err != nil {
		return nil, fmt.Errorf("identityRoles: %w", err)
	}
	svc, err := ScopeRoles(scope, ns, ap.Spec.ServiceRoles)
	if err != nil {
		return nil, fmt.Errorf("serviceRoles: %w", err)
	}
	posture, err := ScopeRoles(scope, ns, ap.Spec.PostureCheckRoles)
	if err != nil {
		return nil, fmt.Errorf("postureCheckRoles: %w", err)
	}
	if posture == nil {
		posture = []string{}
	}
	e := access(ap, conn, ".dial")
	e["type"] = "Dial"
	e["identityRoles"] = ident
	e["serviceRoles"] = svc
	e["postureCheckRoles"] = posture
	return e, nil
}

// AccessERP builds <name>.erp. It returns nil when the CR lists no edgeRouters.
func AccessERP(ap *zitiv1.ZitiAccessPolicy, conn *zitiv1.ZitiConnection, routerIDs map[string]string) (ziti.Entity, error) {
	if len(ap.Spec.EdgeRouters) == 0 {
		return nil, nil
	}
	ident, err := ScopeRoles(scopeOf(conn), ap.Namespace, ap.Spec.IdentityRoles)
	if err != nil {
		return nil, fmt.Errorf("identityRoles: %w", err)
	}
	roles := make([]string, 0, len(ap.Spec.EdgeRouters))
	for _, n := range ap.Spec.EdgeRouters {
		id, ok := routerIDs[n]
		if !ok {
			return nil, fmt.Errorf("router %q not found in Ziti", n)
		}
		roles = append(roles, "@"+id)
	}
	e := access(ap, conn, ".erp")
	e["identityRoles"] = ident
	e["edgeRouterRoles"] = roles
	return e, nil
}
