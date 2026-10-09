// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

// ResolveServiceSelector fills unset addresses, ports, protocols, and targets from selected Services.
func ResolveServiceSelector(spec *zitiv1.ZitiAppSpec, namespace string, services []corev1.Service) error {
	if spec.Expose.Selector == nil {
		return nil
	}
	if len(spec.Targets) != 0 {
		return fmt.Errorf("expose.selector derives targets; do not set targets")
	}
	if len(services) == 0 {
		return fmt.Errorf("expose.selector matches no Services in this namespace")
	}
	slices.SortFunc(services, func(a, b corev1.Service) int { return strings.Compare(a.Name, b.Name) })

	addresses := map[string]bool{}
	ports := map[string]intstr.IntOrString{}
	protocols := map[string]bool{}
	var targets []zitiv1.Target
	for _, svc := range services {
		if svc.Spec.Type == corev1.ServiceTypeExternalName {
			return fmt.Errorf("Service %q has type ExternalName", svc.Name)
		}
		if len(svc.Spec.Ports) == 0 {
			return fmt.Errorf("Service %q has no ports", svc.Name)
		}
		if len(spec.Expose.Addresses) == 0 {
			addresses[svc.Name+"."+namespace+".svc"] = true
		}
		for _, p := range svc.Spec.Ports {
			if len(spec.Expose.Ports) == 0 {
				ports[fmt.Sprint(p.Port)] = intstr.FromInt32(p.Port)
			}
			switch p.Protocol {
			case corev1.ProtocolSCTP:
				return fmt.Errorf("Service %q uses unsupported SCTP", svc.Name)
			case corev1.ProtocolUDP:
				protocols["udp"] = true
			default:
				protocols["tcp"] = true
			}
			targets = append(targets, zitiv1.Target{KubernetesService: svc.Name, Port: p.Port})
		}
	}
	if len(spec.Expose.Addresses) == 0 {
		for address := range addresses {
			spec.Expose.Addresses = append(spec.Expose.Addresses, address)
		}
		slices.Sort(spec.Expose.Addresses)
	}
	if len(spec.Expose.Ports) == 0 {
		for _, p := range ports {
			spec.Expose.Ports = append(spec.Expose.Ports, p)
		}
		slices.SortFunc(spec.Expose.Ports, func(a, b intstr.IntOrString) int { return a.IntValue() - b.IntValue() })
	}
	if len(spec.Expose.Protocols) == 0 {
		if protocols["tcp"] {
			spec.Expose.Protocols = append(spec.Expose.Protocols, "tcp")
		}
		if protocols["udp"] {
			spec.Expose.Protocols = append(spec.Expose.Protocols, "udp")
		}
	}
	if len(targets) > 16 {
		return fmt.Errorf("selector matches %d Service ports; the limit is 16", len(targets))
	}
	spec.Targets = targets
	return nil
}
