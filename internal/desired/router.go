// SPDX-License-Identifier: Apache-2.0

package desired

import (
	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

// RouterName keeps the stored name. A router that exists without a stored name predates the
// <clusterId>-<name> default and keeps the resource name.
func RouterName(r *zitiv1.ZitiRouter, conn *zitiv1.ZitiConnection) string {
	switch {
	case r.Spec.ZitiName != "":
		return r.Spec.ZitiName
	case r.Status.ZitiName != "":
		return r.Status.ZitiName
	case r.Status.RouterID != "":
		return r.Name
	}
	return clusterOf(conn) + "-" + r.Name
}

// Router builds the edge router. Role attributes are used as written: a ZitiRouter is cluster-scoped, so only admins create it.
func Router(r *zitiv1.ZitiRouter, conn *zitiv1.ZitiConnection) ziti.Entity {
	attrs := append([]string{}, r.Spec.RoleAttributes...)
	return ziti.Entity{
		"name":              RouterName(r, conn),
		"roleAttributes":    attrs,
		"isTunnelerEnabled": r.Spec.TunnelerEnabled,
		"cost":              r.Spec.Cost,
		"noTraversal":       r.Spec.NoTraversal,
		"disabled":          r.Spec.Disabled,
		"tags":              ownerTags(conn, "ZitiRouter", &r.ObjectMeta),
	}
}
