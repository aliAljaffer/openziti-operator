// SPDX-License-Identifier: Apache-2.0

package desired

import (
	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

// TerminatorEntity builds the static terminator. Ziti names a terminator by nothing, so the ownership tags are its only handle.
func TerminatorEntity(o *zitiv1.ZitiTerminator, conn *zitiv1.ZitiConnection, serviceID, routerID string) ziti.Entity {
	binding, precedence := o.Spec.Binding, o.Spec.Precedence
	if binding == "" {
		binding = "transport"
	}
	if precedence == "" {
		precedence = "default"
	}
	return ziti.Entity{
		"service":    serviceID,
		"router":     routerID,
		"binding":    binding,
		"address":    o.Spec.Address,
		"cost":       o.Spec.Cost,
		"precedence": precedence,
		"tags":       entityTags(conn, "ZitiTerminator", &o.ObjectMeta),
	}
}
