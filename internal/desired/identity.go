// SPDX-License-Identifier: Apache-2.0

package desired

import (
	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func IdentityName(id *zitiv1.ZitiIdentity) string {
	if id.Spec.ZitiName != "" {
		return id.Spec.ZitiName
	}
	return id.Namespace + "." + id.Name
}

// Identity builds the body for create and update. It has no enrollment, so a PUT keeps the existing one.
func Identity(id *zitiv1.ZitiIdentity, conn *zitiv1.ZitiConnection, authPolicyID string) (ziti.Entity, error) {
	scope := conn.Spec.RoleScope
	if scope == "" {
		scope = zitiv1.RoleScopeNamespaced
	}
	attrs, err := ScopeAttributes(scope, id.Namespace, id.Spec.RoleAttributes)
	if err != nil {
		return nil, err
	}
	return ziti.Entity{
		"name":           IdentityName(id),
		"type":           "Default",
		"isAdmin":        false,
		"roleAttributes": attrs,
		"authPolicyId":   authPolicyID,
		"tags":           ownerTags(conn, "ZitiIdentity", &id.ObjectMeta),
	}, nil
}
