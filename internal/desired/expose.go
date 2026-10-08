// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

const AnnotationPrefix = "ziti.alialjaffer.com/"

// Annotations on a Kubernetes Service. Only expose is required. Lists are comma separated.
const (
	AnnExpose         = AnnotationPrefix + "expose"
	AnnConnection     = AnnotationPrefix + "connection"
	AnnName           = AnnotationPrefix + "name"
	AnnAddresses      = AnnotationPrefix + "addresses"
	AnnPorts          = AnnotationPrefix + "ports"
	AnnProtocols      = AnnotationPrefix + "protocols"
	AnnAllowGroups    = AnnotationPrefix + "allow-groups"
	AnnAllowIdentites = AnnotationPrefix + "allow-identities"
	AnnMemberOf       = AnnotationPrefix + "member-of"
	AnnEntryRouters   = AnnotationPrefix + "entry-routers"
	AnnHostedBy       = AnnotationPrefix + "hosted-by"
)

// Exposed reports whether the Service asks to be exposed.
func Exposed(annotations map[string]string) bool { return annotations[AnnExpose] == "true" }

func list(v string) []string {
	var out []string
	for s := range strings.SplitSeq(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func port(s string) intstr.IntOrString {
	if n, err := strconv.Atoi(s); err == nil {
		return intstr.FromInt32(int32(n))
	}
	return intstr.FromString(s)
}

// AppSpec builds the ZitiApp spec for an annotated Service. Clients dial the Service DNS name on the Service ports,
// and the app forwards the dialed port to the Service.
func AppSpec(svc *corev1.Service) (zitiv1.ZitiAppSpec, error) {
	a := svc.Annotations
	if svc.Spec.Type == corev1.ServiceTypeExternalName {
		return zitiv1.ZitiAppSpec{}, fmt.Errorf("a Service of type ExternalName has no endpoints to expose")
	}
	dns := svc.Name + "." + svc.Namespace + ".svc"

	spec := zitiv1.ZitiAppSpec{
		ConnectionRef: a[AnnConnection],
		ZitiName:      a[AnnName],
		MemberOf:      list(a[AnnMemberOf]),
		HostedBy:      a[AnnHostedBy],
		EntryRouters:  list(a[AnnEntryRouters]),
		Allow:         zitiv1.Allow{Groups: list(a[AnnAllowGroups]), Identities: list(a[AnnAllowIdentites])},
		Expose:        zitiv1.Expose{Addresses: []string{dns}},
		Targets:       []zitiv1.Target{{Address: dns}},
	}
	if v := list(a[AnnAddresses]); len(v) > 0 {
		spec.Expose.Addresses = v
	}

	if v := list(a[AnnPorts]); len(v) > 0 {
		for _, p := range v {
			spec.Expose.Ports = append(spec.Expose.Ports, port(p))
		}
	} else {
		for _, p := range svc.Spec.Ports {
			spec.Expose.Ports = append(spec.Expose.Ports, intstr.FromInt32(p.Port))
		}
	}
	if len(spec.Expose.Ports) == 0 {
		return spec, fmt.Errorf("the Service has no ports")
	}
	if _, err := PortRanges(spec.Expose.Ports); err != nil {
		return spec, fmt.Errorf("%s: %w", AnnPorts, err)
	}

	if v := list(a[AnnProtocols]); len(v) > 0 {
		for _, p := range v {
			if p != "tcp" && p != "udp" {
				return spec, fmt.Errorf("%s: %q is not tcp or udp", AnnProtocols, p)
			}
		}
		spec.Expose.Protocols = v
	} else {
		for _, p := range svc.Spec.Ports {
			switch p.Protocol {
			case corev1.ProtocolUDP:
				if !slices.Contains(spec.Expose.Protocols, "udp") {
					spec.Expose.Protocols = append(spec.Expose.Protocols, "udp")
				}
			case corev1.ProtocolSCTP:
				return spec, fmt.Errorf("SCTP ports are not supported by Ziti")
			default:
				if !slices.Contains(spec.Expose.Protocols, "tcp") {
					spec.Expose.Protocols = append(spec.Expose.Protocols, "tcp")
				}
			}
		}
	}
	return spec, nil
}

// IngressAppSpec builds an app for an HTTP Ingress whose rules all target the same Service port.
// Ziti cannot preserve path routing across different backends, so mixed backends and TLS termination
// are rejected instead of silently changing the Ingress behavior.
func IngressAppSpec(ing *networkingv1.Ingress, backend *corev1.Service) (zitiv1.ZitiAppSpec, error) {
	if len(ing.Spec.TLS) > 0 {
		return zitiv1.ZitiAppSpec{}, fmt.Errorf("TLS Ingresses are not exposed automatically; use a ZitiApp to choose the TLS backend")
	}

	var hosts []string
	var backendName string
	var backendPort int32
	setBackend := func(b networkingv1.IngressBackend) error {
		if b.Service == nil || b.Resource != nil {
			return fmt.Errorf("only Service backends are supported")
		}
		port, err := ingressServicePort(backend, b.Service.Port)
		if err != nil {
			return err
		}
		if backendName != "" && (backendName != b.Service.Name || backendPort != port) {
			return fmt.Errorf("all Ingress paths must target the same Service and port")
		}
		backendName, backendPort = b.Service.Name, port
		return nil
	}

	if ing.Spec.DefaultBackend != nil {
		if err := setBackend(*ing.Spec.DefaultBackend); err != nil {
			return zitiv1.ZitiAppSpec{}, err
		}
	}
	for _, rule := range ing.Spec.Rules {
		if rule.Host != "" {
			hosts = append(hosts, rule.Host)
		}
		if rule.HTTP == nil || len(rule.HTTP.Paths) == 0 {
			return zitiv1.ZitiAppSpec{}, fmt.Errorf("every Ingress rule needs an HTTP path")
		}
		for _, path := range rule.HTTP.Paths {
			if err := setBackend(path.Backend); err != nil {
				return zitiv1.ZitiAppSpec{}, err
			}
		}
	}

	if len(hosts) == 0 && len(list(ing.Annotations[AnnAddresses])) == 0 {
		return zitiv1.ZitiAppSpec{}, fmt.Errorf("ingress needs a host or the %s annotation", AnnAddresses)
	}
	if backendName == "" {
		return zitiv1.ZitiAppSpec{}, fmt.Errorf("ingress needs a Service backend")
	}

	fakeService := &corev1.Service{
		Name: backendName, Namespace: ing.Namespace, Annotations: ing.Annotations,
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}}},
	}
	spec, err := AppSpec(fakeService)
	if err != nil {
		return spec, err
	}
	if len(list(ing.Annotations[AnnAddresses])) == 0 {
		slices.Sort(hosts)
		spec.Expose.Addresses = slices.Compact(hosts)
	}
	spec.Targets = []zitiv1.Target{{KubernetesService: backendName, Port: backendPort}}
	return spec, nil
}

// IngressBackendName returns the one Service all rules in an Ingress target. The full port and TLS checks
// happen in IngressAppSpec once the controller has fetched that Service.
func IngressBackendName(ing *networkingv1.Ingress) (string, error) {
	name := ""
	add := func(b networkingv1.IngressBackend) error {
		if b.Service == nil || b.Resource != nil {
			return fmt.Errorf("only Service backends are supported")
		}
		if name != "" && name != b.Service.Name {
			return fmt.Errorf("all Ingress paths must target the same Service")
		}
		name = b.Service.Name
		return nil
	}
	if ing.Spec.DefaultBackend != nil {
		if err := add(*ing.Spec.DefaultBackend); err != nil {
			return "", err
		}
	}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			return "", fmt.Errorf("every Ingress rule needs an HTTP path")
		}
		for _, path := range rule.HTTP.Paths {
			if err := add(path.Backend); err != nil {
				return "", err
			}
		}
	}
	if name == "" {
		return "", fmt.Errorf("ingress needs a Service backend")
	}
	return name, nil
}

func ingressServicePort(svc *corev1.Service, port networkingv1.ServiceBackendPort) (int32, error) {
	for _, p := range svc.Spec.Ports {
		if port.Name != "" && p.Name == port.Name || port.Number != 0 && p.Port == port.Number {
			return p.Port, nil
		}
	}
	return 0, fmt.Errorf("backend Service %q has no port matching the Ingress backend", svc.Name)
}
