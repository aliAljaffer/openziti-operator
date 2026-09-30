// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"fmt"
	"maps"
	"strings"

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
	e := ziti.Entity{
		"name":           IdentityName(id),
		"type":           "Default",
		"isAdmin":        false,
		"roleAttributes": attrs,
		"authPolicyId":   authPolicyID,
		"tags":           ownerTags(conn, "ZitiIdentity", &id.ObjectMeta),
	}
	ext, err := ExternalID(id, conn)
	if err != nil {
		return nil, err
	}
	if ext != "" {
		e["externalId"] = ext
	}
	return e, nil
}

// ExternalID returns the value a token claim must match. serviceAccount is safe in every scope. A free externalId
// can name any subject, so it needs roleScope Global.
func ExternalID(id *zitiv1.ZitiIdentity, conn *zitiv1.ZitiConnection) (string, error) {
	switch {
	case id.Spec.ServiceAccount != "":
		return "system:serviceaccount:" + id.Namespace + ":" + id.Spec.ServiceAccount, nil
	case id.Spec.ExternalID != "":
		if conn.Spec.RoleScope != zitiv1.RoleScopeGlobal {
			return "", fmt.Errorf("externalId needs roleScope Global on the connection, use serviceAccount instead")
		}
		return id.Spec.ExternalID, nil
	}
	return "", nil
}

// AdoptTags returns the existing tags plus the ownership tags. Ziti replaces the whole tag map on PATCH.
func AdoptTags(conn *zitiv1.ZitiConnection, id *zitiv1.ZitiIdentity, existing map[string]any) map[string]any {
	out := map[string]any{}
	maps.Copy(out, existing)
	maps.Copy(out, ownerTags(conn, "ZitiIdentity", &id.ObjectMeta))
	out[TagAdopted] = "true"
	return out
}

// ReleaseTags returns the existing tags without any operator tag.
func ReleaseTags(existing map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range existing {
		if !strings.HasPrefix(k, TagPrefix) {
			out[k] = v
		}
	}
	return out
}
