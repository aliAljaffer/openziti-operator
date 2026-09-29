// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

// NameLookup finds the ID of the entity of a kind by name.
type NameLookup func(kind ziti.Kind, name string) (id string, ok bool)

// MissingError says that a role names an entity that Ziti does not have. It can fix itself, so callers retry soon.
type MissingError struct{ Msg string }

func (e *MissingError) Error() string { return e.Msg }

// EntityName is the name in Ziti: zitiName, or <namespace>.<name>.
func EntityName(c *zitiv1.EntitySpec, m *metav1.ObjectMeta) string {
	if c.ZitiName != "" {
		return c.ZitiName
	}
	return m.Namespace + "." + m.Name
}

func scopeFor(conn *zitiv1.ZitiConnection) zitiv1.RoleScope { return scopeOf(conn) }

// ResolveRoles checks the roles of one field and returns them in the form Ziti wants.
// With Namespaced scope, "#attr" becomes "#<namespace>.attr" and "@name" and "#all" are rejected.
// With Global scope, "#attr" and "#all" pass through and "@name" becomes "@<id>" of the entity of the given kind.
func ResolveRoles(conn *zitiv1.ZitiConnection, namespace, field string, roles []string, kind ziti.Kind, lookup NameLookup) ([]string, error) {
	for _, r := range roles {
		if !strings.HasPrefix(r, "#") && !strings.HasPrefix(r, "@") {
			return nil, fmt.Errorf("%s: role %q must start with # (attribute) or @ (name)", field, r)
		}
		if len(r) < 2 {
			return nil, fmt.Errorf("%s: role %q is empty", field, r)
		}
	}
	if scopeFor(conn) == zitiv1.RoleScopeNamespaced {
		out, err := ScopeRoles(zitiv1.RoleScopeNamespaced, namespace, roles)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}
		return out, nil
	}
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		name, byName := strings.CutPrefix(r, "@")
		if !byName {
			out = append(out, r)
			continue
		}
		id, ok := lookup(kind, name)
		if !ok {
			return nil, &MissingError{fmt.Sprintf("%s: no %s named %q in Ziti", field, strings.TrimSuffix(string(kind), "s"), name)}
		}
		out = append(out, "@"+id)
	}
	return out, nil
}

func entityTags(conn *zitiv1.ZitiConnection, kind string, m *metav1.ObjectMeta) map[string]any {
	return ownerTags(conn, kind, m)
}

// ConfigEntity builds a config. typeID is the ID of the config type named in spec.type.
func ConfigEntity(o *zitiv1.ZitiConfig, conn *zitiv1.ZitiConnection, typeID string) (ziti.Entity, error) {
	var data map[string]any
	if err := json.Unmarshal(o.Spec.Data.Raw, &data); err != nil || data == nil {
		return nil, fmt.Errorf("data must be an object")
	}
	return ziti.Entity{
		"name":         EntityName(&o.Spec.EntitySpec, &o.ObjectMeta),
		"configTypeId": typeID,
		"data":         data,
		"tags":         entityTags(conn, "ZitiConfig", &o.ObjectMeta),
	}, nil
}

// ServiceEntity builds a service. configIDs are the IDs of the configs named in spec.configs.
func ServiceEntity(o *zitiv1.ZitiService, conn *zitiv1.ZitiConnection, configIDs []string) (ziti.Entity, error) {
	attrs, err := ScopeAttributes(scopeFor(conn), o.Namespace, o.Spec.RoleAttributes)
	if err != nil {
		return nil, fmt.Errorf("roleAttributes: %w", err)
	}
	strategy := o.Spec.TerminatorStrategy
	if strategy == "" {
		strategy = "smartrouting"
	}
	if configIDs == nil {
		configIDs = []string{}
	}
	return ziti.Entity{
		"name":               EntityName(&o.Spec.EntitySpec, &o.ObjectMeta),
		"roleAttributes":     attrs,
		"configs":            configIDs,
		"terminatorStrategy": strategy,
		"encryptionRequired": boolOr(o.Spec.EncryptionRequired, true),
		"maxIdleTimeMillis":  o.Spec.MaxIdleTimeMillis,
		"tags":               entityTags(conn, "ZitiService", &o.ObjectMeta),
	}, nil
}

func semanticOr(v string) string {
	if v == "" {
		return "AnyOf"
	}
	return v
}

// ServicePolicyEntity builds a Dial or Bind policy. The roles are already resolved.
func ServicePolicyEntity(o *zitiv1.ZitiServicePolicy, conn *zitiv1.ZitiConnection, identityRoles, serviceRoles, postureRoles []string) ziti.Entity {
	if postureRoles == nil {
		postureRoles = []string{}
	}
	return ziti.Entity{
		"name":              EntityName(&o.Spec.EntitySpec, &o.ObjectMeta),
		"type":              o.Spec.Type,
		"semantic":          semanticOr(o.Spec.Semantic),
		"identityRoles":     identityRoles,
		"serviceRoles":      serviceRoles,
		"postureCheckRoles": postureRoles,
		"tags":              entityTags(conn, "ZitiServicePolicy", &o.ObjectMeta),
	}
}

// EdgeRouterPolicyEntity builds an edge router policy. The roles are already resolved.
func EdgeRouterPolicyEntity(o *zitiv1.ZitiEdgeRouterPolicy, conn *zitiv1.ZitiConnection, identityRoles, routerRoles []string) ziti.Entity {
	return ziti.Entity{
		"name":            EntityName(&o.Spec.EntitySpec, &o.ObjectMeta),
		"semantic":        semanticOr(o.Spec.Semantic),
		"identityRoles":   identityRoles,
		"edgeRouterRoles": routerRoles,
		"tags":            entityTags(conn, "ZitiEdgeRouterPolicy", &o.ObjectMeta),
	}
}

// ServiceEdgeRouterPolicyEntity builds a service edge router policy. The roles are already resolved.
func ServiceEdgeRouterPolicyEntity(o *zitiv1.ZitiServiceEdgeRouterPolicy, conn *zitiv1.ZitiConnection, serviceRoles, routerRoles []string) ziti.Entity {
	return ziti.Entity{
		"name":            EntityName(&o.Spec.EntitySpec, &o.ObjectMeta),
		"semantic":        semanticOr(o.Spec.Semantic),
		"serviceRoles":    serviceRoles,
		"edgeRouterRoles": routerRoles,
		"tags":            entityTags(conn, "ZitiServiceEdgeRouterPolicy", &o.ObjectMeta),
	}
}
