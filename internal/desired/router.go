// SPDX-License-Identifier: Apache-2.0

package desired

import (
	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func RouterName(r *zitiv1.ZitiRouter) string {
	if r.Spec.ZitiName != "" {
		return r.Spec.ZitiName
	}
	return r.Name
}

// Router builds the edge router. Role attributes are used as written: a ZitiRouter is cluster-scoped, so only admins create it.
func Router(r *zitiv1.ZitiRouter, conn *zitiv1.ZitiConnection) ziti.Entity {
	attrs := append([]string{}, r.Spec.RoleAttributes...)
	return ziti.Entity{
		"name":              RouterName(r),
		"roleAttributes":    attrs,
		"isTunnelerEnabled": r.Spec.TunnelerEnabled,
		"cost":              r.Spec.Cost,
		"noTraversal":       r.Spec.NoTraversal,
		"disabled":          r.Spec.Disabled,
		"tags":              ownerTags(conn, "ZitiRouter", &r.ObjectMeta),
	}
}
