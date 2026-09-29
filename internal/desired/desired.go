// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

const TagPrefix = "ziti-operator-"

const (
	TagCluster   = TagPrefix + "cluster"
	TagKind      = TagPrefix + "kind"
	TagNamespace = TagPrefix + "namespace"
	TagName      = TagPrefix + "name"
	TagUID       = TagPrefix + "uid"
)

type Builder struct {
	Svc       *zitiv1.ZitiService
	Conn      *zitiv1.ZitiConnection
	RouterIDs map[string]string
}

func (b *Builder) ZitiName() string {
	if b.Svc.Spec.ZitiName != "" {
		return b.Svc.Spec.ZitiName
	}
	return b.Svc.Namespace + "." + b.Svc.Name
}

func (b *Builder) Tags() map[string]any {
	cluster := b.Conn.Spec.ClusterID
	if cluster == "" {
		cluster = "default"
	}
	return map[string]any{
		TagCluster:   cluster,
		TagKind:      "ZitiService",
		TagNamespace: b.Svc.Namespace,
		TagName:      b.Svc.Name,
		TagUID:       string(b.Svc.UID),
	}
}

func (b *Builder) HostingRouter() (string, error) {
	r := b.Svc.Spec.HostingRouter
	if r == "" {
		if len(b.Conn.Spec.HostingRouters) == 0 {
			return "", fmt.Errorf("connection %q has no hostingRouters", b.Conn.Name)
		}
		r = b.Conn.Spec.HostingRouters[0]
	}
	if !slices.Contains(b.Conn.Spec.HostingRouters, r) {
		return "", fmt.Errorf("hostingRouter %q is not in the connection hostingRouters", r)
	}
	return r, nil
}

func (b *Builder) routerRole(name string) (string, error) {
	id, ok := b.RouterIDs[name]
	if !ok {
		return "", fmt.Errorf("router %q not found in Ziti", name)
	}
	return "@" + id, nil
}

func ScopeRoles(scope zitiv1.RoleScope, namespace string, roles []string) ([]string, error) {
	if scope == zitiv1.RoleScopeGlobal {
		return slices.Clone(roles), nil
	}
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		attr, ok := strings.CutPrefix(r, "#")
		if !ok || attr == "all" {
			return nil, fmt.Errorf("role %q is not allowed with roleScope Namespaced, use #attribute", r)
		}
		out = append(out, "#"+namespace+"."+attr)
	}
	return out, nil
}

func (b *Builder) scope() zitiv1.RoleScope {
	if b.Conn.Spec.RoleScope == "" {
		return zitiv1.RoleScopeNamespaced
	}
	return b.Conn.Spec.RoleScope
}

func (b *Builder) named(suffix string) ziti.Entity {
	return ziti.Entity{"name": b.ZitiName() + suffix, "tags": b.Tags()}
}

func (b *Builder) InterceptConfig(typeID string) ziti.Entity {
	e := b.named("-intercept.v1")
	i := b.Svc.Spec.Intercept
	ranges := make([]map[string]any, len(i.Ports))
	for n, p := range i.Ports {
		ranges[n] = map[string]any{"low": p, "high": p}
	}
	e["configTypeId"] = typeID
	e["data"] = map[string]any{
		"addresses":  i.Addresses,
		"portRanges": ranges,
		"protocols":  b.protocols(),
	}
	return e
}

func (b *Builder) protocols() []string {
	if p := b.Svc.Spec.Intercept.Protocols; len(p) > 0 {
		return p
	}
	return []string{"tcp"}
}

func (b *Builder) HostConfig(typeID string) (ziti.Entity, error) {
	h := b.Svc.Spec.Host
	data := map[string]any{}
	switch {
	case h.ServiceRef != nil && h.Address == "":
		data["address"] = h.ServiceRef.Name + "." + b.Svc.Namespace + ".svc"
		data["port"] = h.ServiceRef.Port
	case h.ServiceRef == nil && h.Address != "" && h.Port > 0:
		data["address"] = h.Address
		data["port"] = h.Port
	default:
		return nil, fmt.Errorf("host needs exactly one of serviceRef and address with port")
	}
	p := b.protocols()
	if len(p) > 1 && !h.ForwardProtocol {
		return nil, fmt.Errorf("more than one intercept protocol requires host.forwardProtocol")
	}
	if h.ForwardProtocol {
		data["forwardProtocol"] = true
		data["allowedProtocols"] = p
	} else {
		data["protocol"] = p[0]
	}
	e := b.named("-host.v1")
	e["configTypeId"] = typeID
	e["data"] = data
	return e, nil
}

func (b *Builder) Service(interceptID, hostID string) (ziti.Entity, error) {
	attrs, err := ScopeRoles(b.scope(), b.Svc.Namespace, hashed(b.Svc.Spec.RoleAttributes))
	if err != nil {
		return nil, err
	}
	for n := range attrs {
		attrs[n] = strings.TrimPrefix(attrs[n], "#")
	}
	e := ziti.Entity{"name": b.ZitiName(), "tags": b.Tags()}
	e["encryptionRequired"] = true
	e["terminatorStrategy"] = "smartrouting"
	e["configs"] = []string{interceptID, hostID}
	e["roleAttributes"] = attrs
	return e, nil
}

func hashed(attrs []string) []string {
	out := make([]string, len(attrs))
	for n, a := range attrs {
		out[n] = "#" + a
	}
	return out
}

func (b *Builder) Bind(serviceID string) (ziti.Entity, error) {
	r, err := b.HostingRouter()
	if err != nil {
		return nil, err
	}
	role, err := b.routerRole(r)
	if err != nil {
		return nil, err
	}
	e := b.named("-bind")
	e["type"] = "Bind"
	e["semantic"] = "AnyOf"
	e["identityRoles"] = []string{role}
	e["serviceRoles"] = []string{"@" + serviceID}
	return e, nil
}

func (b *Builder) SERP(serviceID string) (ziti.Entity, error) {
	host, err := b.HostingRouter()
	if err != nil {
		return nil, err
	}
	names := append([]string{host}, b.Svc.Spec.EdgeRouters...)
	names = append(names, b.Conn.Spec.DefaultEdgeRouters...)
	var roles []string
	for _, n := range names {
		role, err := b.routerRole(n)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(roles, role) {
			roles = append(roles, role)
		}
	}
	e := b.named("-serp")
	e["semantic"] = "AnyOf"
	e["edgeRouterRoles"] = roles
	e["serviceRoles"] = []string{"@" + serviceID}
	return e, nil
}

// Dial returns nil when the service has no access.identityRoles.
func (b *Builder) Dial(serviceID string) (ziti.Entity, error) {
	roles := b.Svc.Spec.Access.IdentityRoles
	if len(roles) == 0 {
		return nil, nil
	}
	scoped, err := ScopeRoles(b.scope(), b.Svc.Namespace, roles)
	if err != nil {
		return nil, err
	}
	e := b.named("-dial")
	e["type"] = "Dial"
	e["semantic"] = "AnyOf"
	e["identityRoles"] = scoped
	e["serviceRoles"] = []string{"@" + serviceID}
	return e, nil
}

// Matches reports whether actual already contains every field of desired. String lists compare as sets.
func Matches(desired, actual ziti.Entity) bool {
	return subset(normalize(desired), normalize(actual))
}

func normalize(v any) any {
	raw, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

func subset(want, got any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range w {
			if !subset(v, g[k]) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		if allStrings(w) && allStrings(g) {
			return slices.Equal(sortedStrings(w), sortedStrings(g))
		}
		for n := range w {
			if !subset(w[n], g[n]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(want, got)
	}
}

func allStrings(l []any) bool {
	for _, v := range l {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return true
}

func sortedStrings(l []any) []string {
	out := make([]string, len(l))
	for n, v := range l {
		out[n] = v.(string)
	}
	slices.Sort(out)
	return out
}
