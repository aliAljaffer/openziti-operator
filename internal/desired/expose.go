// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
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
	for _, s := range strings.Split(v, ",") {
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
