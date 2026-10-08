// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func TestResolveServiceSelectorDerivesAddressesPortsProtocolsAndTargets(t *testing.T) {
	spec := zitiv1.ZitiAppSpec{Expose: zitiv1.Expose{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "billing"}}}}
	services := []corev1.Service{
		{Name: "z-billing", Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 443, Protocol: corev1.ProtocolTCP}, {Port: 53, Protocol: corev1.ProtocolUDP}}}},
		{Name: "a-billing", Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}}}},
	}
	if err := ResolveServiceSelector(&spec, "team-a", services); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec.Expose.Addresses, []string{"a-billing.team-a.svc", "z-billing.team-a.svc"}) {
		t.Errorf("addresses = %v", spec.Expose.Addresses)
	}
	if !reflect.DeepEqual(spec.Expose.Ports, []intstr.IntOrString{intstr.FromInt32(53), intstr.FromInt32(80), intstr.FromInt32(443)}) {
		t.Errorf("ports = %v", spec.Expose.Ports)
	}
	if !reflect.DeepEqual(spec.Expose.Protocols, []string{"tcp", "udp"}) {
		t.Errorf("protocols = %v", spec.Expose.Protocols)
	}
	if len(spec.Targets) != 3 || spec.Targets[0].KubernetesService != "a-billing" {
		t.Errorf("targets = %+v", spec.Targets)
	}
}

func TestResolveServiceSelectorKeepsExplicitExposeValues(t *testing.T) {
	spec := zitiv1.ZitiAppSpec{Expose: zitiv1.Expose{
		Addresses: []string{"billing.example.com"}, Ports: []intstr.IntOrString{intstr.FromInt32(8443)}, Protocols: []string{"tcp"},
		Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "billing"}},
	}}
	services := []corev1.Service{{Name: "billing", Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 8080}}}}}
	if err := ResolveServiceSelector(&spec, "team-a", services); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec.Expose.Addresses, []string{"billing.example.com"}) ||
		!reflect.DeepEqual(spec.Expose.Ports, []intstr.IntOrString{intstr.FromInt32(8443)}) ||
		spec.Targets[0].Port != 8080 {
		t.Errorf("spec = %+v", spec)
	}
}

func TestResolveServiceSelectorRejectsUnsafeOrEmptyTargets(t *testing.T) {
	tests := []struct {
		name     string
		services []corev1.Service
		want     string
	}{
		{"no matches", nil, "matches no Services"},
		{"external name", []corev1.Service{{Name: "ext", Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName}}}, "ExternalName"},
		{"no service ports", []corev1.Service{{Name: "empty"}}, "no ports"},
		{"sctp", []corev1.Service{{Name: "sctp", Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 9000, Protocol: corev1.ProtocolSCTP}}}}}, "SCTP"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := zitiv1.ZitiAppSpec{Expose: zitiv1.Expose{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}}}}
			if err := ResolveServiceSelector(&spec, "team-a", tc.services); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
