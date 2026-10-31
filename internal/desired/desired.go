// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

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
	TagAdopted   = TagPrefix + "adopted"
)

type Builder struct {
	Svc       *zitiv1.ZitiApp
	Conn      *zitiv1.ZitiConnection
	RouterIDs map[string]string
	group     *Group
}

// WithGroup returns a builder that builds the entities of one group instead of the whole app.
func (b *Builder) WithGroup(g Group) *Builder {
	nb := *b
	nb.group = &g
	return &nb
}

func (b *Builder) ZitiName() string {
	if b.group != nil {
		return b.group.Name
	}
	if b.Svc.Spec.ZitiName != "" {
		return b.Svc.Spec.ZitiName
	}
	return b.Svc.Namespace + "." + b.Svc.Name
}

func (b *Builder) exposePorts() []intstr.IntOrString {
	if b.group != nil {
		return b.group.Ports
	}
	return b.Svc.Spec.Expose.Ports
}

func (b *Builder) groupTargets() []zitiv1.Target {
	if b.group != nil {
		return b.group.Targets
	}
	return b.Svc.Spec.Targets
}

func (b *Builder) Tags() map[string]any {
	return ownerTags(b.Conn, "ZitiApp", &b.Svc.ObjectMeta)
}

func ownerTags(conn *zitiv1.ZitiConnection, kind string, m *metav1.ObjectMeta) map[string]any {
	cluster := conn.Spec.ClusterID
	if cluster == "" {
		cluster = "default"
	}
	return map[string]any{
		TagCluster:   cluster,
		TagKind:      kind,
		TagNamespace: m.Namespace,
		TagName:      m.Name,
		TagUID:       string(m.UID),
	}
}

func (b *Builder) HostingRouter() (string, error) {
	routers, err := b.HostingRouters()
	if err != nil {
		return "", err
	}
	return routers[0], nil
}

func (b *Builder) HostingRouters() ([]string, error) {
	if len(b.Conn.Spec.HostingRouters) == 0 {
		return nil, fmt.Errorf("connection %q has no hostingRouters", b.Conn.Name)
	}
	names := make([]string, 0, 1+len(b.Svc.Spec.HostedByAnyOf))
	if b.Svc.Spec.HostedBy != "" {
		names = append(names, b.Svc.Spec.HostedBy)
	}
	for _, n := range b.Svc.Spec.HostedByAnyOf {
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		names = append(names, b.Conn.Spec.HostingRouters[0])
	}
	for _, n := range names {
		if !slices.Contains(b.Conn.Spec.HostingRouters, n) {
			return nil, fmt.Errorf("hostedBy %q is not in the connection hostingRouters", n)
		}
	}
	return names, nil
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

// PortRanges turns [443, "8000-8005"] into Ziti port ranges.
func PortRanges(ports []intstr.IntOrString) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(ports))
	for _, p := range ports {
		low, high := 0, 0
		if p.Type == intstr.Int {
			low, high = int(p.IntVal), int(p.IntVal)
		} else {
			lo, hi, isRange := strings.Cut(strings.TrimSpace(p.StrVal), "-")
			var err error
			if low, err = strconv.Atoi(lo); err != nil {
				return nil, fmt.Errorf("port %q is not a number or a low-high range", p.StrVal)
			}
			high = low
			if isRange {
				if high, err = strconv.Atoi(hi); err != nil {
					return nil, fmt.Errorf("port %q is not a number or a low-high range", p.StrVal)
				}
			}
		}
		if low < 1 || high > 65535 || low > high {
			return nil, fmt.Errorf("port %v must be 1-65535 and low must not exceed high", p.String())
		}
		out = append(out, map[string]any{"low": low, "high": high})
	}
	return out, nil
}

func (b *Builder) InterceptConfig(typeID string) (ziti.Entity, error) {
	x := b.Svc.Spec.Expose
	ranges, err := PortRanges(b.exposePorts())
	if err != nil {
		return nil, err
	}
	e := b.named("-intercept.v1")
	e["configTypeId"] = typeID
	e["data"] = map[string]any{
		"addresses":  x.Addresses,
		"portRanges": ranges,
		"protocols":  b.protocols(),
	}
	return e, nil
}

func (b *Builder) protocols() []string {
	if p := b.Svc.Spec.Expose.Protocols; len(p) > 0 {
		return p
	}
	return []string{"tcp"}
}

// HostConfig builds a host.v2 config with one terminator per target. Targets always accept every exposed
// protocol. A target without a port receives the port the client dialed.
func (b *Builder) HostConfig(typeID string) (ziti.Entity, error) {
	targets := b.groupTargets()
	if len(targets) == 0 {
		return nil, fmt.Errorf("targets needs at least one entry")
	}
	ranges, err := PortRanges(b.exposePorts())
	if err != nil {
		return nil, err
	}
	terminators := make([]map[string]any, 0, len(targets))
	for _, t := range targets {
		address := t.Address
		namespace := t.Namespace
		if namespace == "" {
			namespace = b.Svc.Namespace
		}
		switch {
		case t.KubernetesService != "" && t.Address == "":
			address = t.KubernetesService + "." + namespace + ".svc"
		case t.Address == "":
			return nil, fmt.Errorf("each target needs address or kubernetesService")
		case t.KubernetesService != "":
			return nil, fmt.Errorf("a target sets address or kubernetesService, not both")
		}
		term := map[string]any{
			"address":          address,
			"forwardProtocol":  true,
			"allowedProtocols": b.protocols(),
		}
		if t.Port > 0 {
			term["port"] = t.Port
		} else {
			term["forwardPort"] = true
			term["allowedPortRanges"] = ranges
		}
		if t.Cost > 0 {
			term["listenOptions"] = map[string]any{"cost": t.Cost}
		}
		if t.Precedence != "" {
			term["precedence"] = t.Precedence
		}
		terminators = append(terminators, term)
	}
	e := b.named("-host.v2")
	e["configTypeId"] = typeID
	e["data"] = map[string]any{"terminators": terminators}
	return e, nil
}

func (b *Builder) Service(interceptID, hostID string) (ziti.Entity, error) {
	return b.ServiceWithConfigs(interceptID, hostID, nil)
}

func (b *Builder) ServiceWithConfigs(interceptID, hostID string, extra []string) (ziti.Entity, error) {
	attrs, err := ScopeAttributes(b.scope(), b.Svc.Namespace, b.Svc.Spec.MemberOf)
	if err != nil {
		return nil, err
	}
	configs := make([]string, 0, 2+len(extra))
	configs = append(configs, interceptID, hostID)
	configs = append(configs, extra...)
	strategy := b.Svc.Spec.Service.TerminatorStrategy
	if strategy == "" {
		strategy = "smartrouting"
	}
	e := ziti.Entity{"name": b.ZitiName(), "tags": b.Tags()}
	e["encryptionRequired"] = boolOrTrue(b.Svc.Spec.Service.EncryptionRequired)
	e["terminatorStrategy"] = strategy
	e["configs"] = configs
	e["roleAttributes"] = attrs
	if b.Svc.Spec.Service.MaxIdleTimeMillis > 0 {
		e["maxIdleTimeMillis"] = b.Svc.Spec.Service.MaxIdleTimeMillis
	}
	return e, nil
}

func (b *Builder) Bind(serviceID string) (ziti.Entity, error) {
	routers, err := b.HostingRouters()
	if err != nil {
		return nil, err
	}
	roles := make([]string, 0, len(routers))
	for _, name := range routers {
		role, err := b.routerRole(name)
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	e := b.named("-bind")
	e["type"] = "Bind"
	e["semantic"] = "AnyOf"
	e["identityRoles"] = roles
	e["serviceRoles"] = []string{"@" + serviceID}
	return e, nil
}

func (b *Builder) SERP(serviceID string) (ziti.Entity, error) {
	routers, err := b.HostingRouters()
	if err != nil {
		return nil, err
	}
	names := append(slices.Clone(routers), b.Svc.Spec.EntryRouters...)
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

// ScopeAttributes applies ScopeRoles to plain role attributes, so they match the scoped "#attr" roles in policies.
func ScopeAttributes(scope zitiv1.RoleScope, namespace string, attrs []string) ([]string, error) {
	roles := make([]string, len(attrs))
	for n, a := range attrs {
		roles[n] = "#" + a
	}
	scoped, err := ScopeRoles(scope, namespace, roles)
	for n := range scoped {
		scoped[n] = strings.TrimPrefix(scoped[n], "#")
	}
	return scoped, err
}

// AllowedRoles turns allow.groups into scoped "#group" roles and allow.identities into "@id" roles.
// It returns the identity names that Ziti does not know. identityIDs maps identity names to ids.
func (b *Builder) AllowedRoles(identityIDs map[string]string) (roles, missing []string, err error) {
	a := b.Svc.Spec.Allow
	hashed := make([]string, len(a.Groups))
	for n, g := range a.Groups {
		hashed[n] = "#" + g
	}
	if roles, err = ScopeRoles(b.scope(), b.Svc.Namespace, hashed); err != nil {
		return nil, nil, err
	}
	for _, name := range a.Identities {
		if id, ok := identityIDs[name]; ok {
			roles = append(roles, "@"+id)
		} else {
			missing = append(missing, name)
		}
	}
	return roles, missing, nil
}

func (b *Builder) Dial(serviceID string, roles []string) (ziti.Entity, error) {
	if len(b.Svc.Spec.Allow.Groups)+len(b.Svc.Spec.Allow.Identities) == 0 {
		return nil, nil
	}
	for _, r := range b.Svc.Spec.Allow.PostureCheckRoles {
		if strings.HasPrefix(r, "@") {
			return nil, fmt.Errorf("postureCheckRoles: %q uses @name; use #attribute, or ZitiAccessPolicy for @name", r)
		}
	}
	posture, err := ScopeRoles(b.scope(), b.Svc.Namespace, b.Svc.Spec.Allow.PostureCheckRoles)
	if err != nil {
		return nil, fmt.Errorf("postureCheckRoles: %w", err)
	}
	if posture == nil {
		posture = []string{}
	}
	e := b.named("-dial")
	e["type"] = "Dial"
	e["semantic"] = "AnyOf"
	e["identityRoles"] = roles
	e["serviceRoles"] = []string{"@" + serviceID}
	e["postureCheckRoles"] = posture
	return e, nil
}

// ERP gives the allowed identities access to the entry routers. It returns nil without entryRouters or roles.
func (b *Builder) ERP(roles []string) (ziti.Entity, error) {
	if len(b.Svc.Spec.EntryRouters) == 0 {
		return nil, nil
	}
	if err := CheckEntryRouters(b.Conn, b.Svc.Spec.EntryRouters); err != nil {
		return nil, err
	}
	if len(roles) == 0 {
		return nil, nil
	}
	names := append(slices.Clone(b.Svc.Spec.EntryRouters), b.Conn.Spec.DefaultEdgeRouters...)
	var routerRoles []string
	for _, n := range names {
		role, err := b.routerRole(n)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(routerRoles, role) {
			routerRoles = append(routerRoles, role)
		}
	}
	e := b.named("-erp")
	e["semantic"] = "AnyOf"
	e["identityRoles"] = roles
	e["edgeRouterRoles"] = routerRoles
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
		if len(w) == 0 {
			return len(g) == 0
		}
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
	case string:
		g, ok := got.(string)
		return ok && strings.TrimSpace(w) == strings.TrimSpace(g)
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
