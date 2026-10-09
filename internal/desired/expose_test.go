// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func svc(ann map[string]string, ports ...corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		Name: "web", Namespace: "team-a", Annotations: ann,
		Spec: corev1.ServiceSpec{Ports: ports},
	}
}

func TestExposed(t *testing.T) {
	if !Exposed(map[string]string{AnnExpose: "true"}) || Exposed(map[string]string{AnnExpose: "false"}) || Exposed(nil) || Exposed(map[string]string{AnnExpose: "True"}) {
		t.Error("only expose=true exposes")
	}
}

func TestAppSpecDefaultsFromService(t *testing.T) {
	got, err := AppSpec(svc(map[string]string{AnnExpose: "true"},
		corev1.ServicePort{Port: 80, Protocol: corev1.ProtocolTCP}, corev1.ServicePort{Port: 53, Protocol: corev1.ProtocolUDP}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Expose.Addresses, []string{"web.team-a.svc"}) ||
		!reflect.DeepEqual(got.Expose.Ports, []intstr.IntOrString{intstr.FromInt32(80), intstr.FromInt32(53)}) ||
		!reflect.DeepEqual(got.Expose.Protocols, []string{"tcp", "udp"}) {
		t.Errorf("expose = %+v", got.Expose)
	}
	if len(got.Targets) != 1 || got.Targets[0].Address != "web.team-a.svc" || got.Targets[0].Port != 0 {
		t.Errorf("targets = %+v", got.Targets)
	}
}

func TestAppSpecFromAnnotations(t *testing.T) {
	got, err := AppSpec(svc(map[string]string{
		AnnExpose: "true", AnnConnection: "prod", AnnName: "web.example.com", AnnAddresses: "web.example.com, alt.example.com",
		AnnPorts: "443, 8000-8005", AnnProtocols: "tcp", AnnAllowGroups: "team-a,ops", AnnAllowIdentites: "alice",
		AnnMemberOf: "tenant", AnnEntryRouters: "edge-1", AnnHostedBy: "r-main",
	}, corev1.ServicePort{Port: 80}))
	if err != nil {
		t.Fatal(err)
	}
	if got.ConnectionRef != "prod" || got.ZitiName != "web.example.com" || got.HostedBy != "r-main" ||
		!reflect.DeepEqual(got.Expose.Addresses, []string{"web.example.com", "alt.example.com"}) ||
		!reflect.DeepEqual(got.Expose.Ports, []intstr.IntOrString{intstr.FromInt32(443), intstr.FromString("8000-8005")}) ||
		!reflect.DeepEqual(got.Allow.Groups, []string{"team-a", "ops"}) || !reflect.DeepEqual(got.Allow.Identities, []string{"alice"}) ||
		!reflect.DeepEqual(got.MemberOf, []string{"tenant"}) || !reflect.DeepEqual(got.EntryRouters, []string{"edge-1"}) {
		t.Errorf("spec = %+v", got)
	}
}

func TestAppSpecErrors(t *testing.T) {
	tests := map[string]struct {
		s   *corev1.Service
		err string
	}{
		"external name": {&corev1.Service{Name: "x", Namespace: "n", Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName}}, "ExternalName"},
		"no ports":      {svc(map[string]string{AnnExpose: "true"}), "no ports"},
		"bad range":     {svc(map[string]string{AnnPorts: "9000-8000"}, corev1.ServicePort{Port: 80}), "low must not exceed high"},
		"bad protocol":  {svc(map[string]string{AnnProtocols: "icmp"}, corev1.ServicePort{Port: 80}), "not tcp or udp"},
		"sctp":          {svc(nil, corev1.ServicePort{Port: 80, Protocol: corev1.ProtocolSCTP}), "SCTP"},
	}
	for name, tc := range tests {
		if _, err := AppSpec(tc.s); err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.err)
		}
	}
}

func TestIngressAppSpecUsesHostsAndOneServiceBackend(t *testing.T) {
	pathType := networkingv1.PathTypePrefix
	ing := &networkingv1.Ingress{
		Name: "billing", Namespace: "team-a",
		Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{
			{Host: "billing.example.com", HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
				Path: "/", PathType: &pathType, Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "billing", Port: networkingv1.ServiceBackendPort{Number: 8080}}},
			}}}},
			{Host: "billing-alt.example.com", HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
				Path: "/v2", PathType: &pathType, Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "billing", Port: networkingv1.ServiceBackendPort{Name: "web"}}},
			}}}},
		}},
	}
	svc := &corev1.Service{Name: "billing", Namespace: "team-a", Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "web", Port: 8080}}}}
	spec, err := IngressAppSpec(ing, svc)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec.Expose.Addresses, []string{"billing-alt.example.com", "billing.example.com"}) {
		t.Errorf("addresses = %v", spec.Expose.Addresses)
	}
	if len(spec.Expose.Ports) != 1 || spec.Expose.Ports[0] != intstr.FromInt32(80) {
		t.Errorf("exposed ports = %v", spec.Expose.Ports)
	}
	if len(spec.Targets) != 1 || spec.Targets[0].KubernetesService != "billing" || spec.Targets[0].Port != 8080 {
		t.Errorf("targets = %+v", spec.Targets)
	}
}

func TestIngressAppSpecHonorsAnnotationsAndRejectsAmbiguousRoutes(t *testing.T) {
	pathType := networkingv1.PathTypePrefix
	backend := func(name string, port int32) networkingv1.IngressBackend {
		return networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: name, Port: networkingv1.ServiceBackendPort{Number: port}}}
	}
	base := &networkingv1.Ingress{Name: "app", Namespace: "team-a", Annotations: map[string]string{
		AnnConnection: "edge", AnnAddresses: "custom.example.com", AnnAllowGroups: "staff, admins", AnnPorts: "8443",
	}, Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "ignored.example.com", HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Path: "/", PathType: &pathType, Backend: backend("app", 3000)}}}}}}}
	svc := &corev1.Service{Name: "app", Namespace: "team-a", Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 3000}}}}
	spec, err := IngressAppSpec(base, svc)
	if err != nil {
		t.Fatal(err)
	}
	if spec.ConnectionRef != "edge" || !reflect.DeepEqual(spec.Expose.Addresses, []string{"custom.example.com"}) ||
		!reflect.DeepEqual(spec.Allow.Groups, []string{"staff", "admins"}) || len(spec.Expose.Ports) != 1 || spec.Expose.Ports[0] != intstr.FromInt32(8443) {
		t.Errorf("spec = %+v", spec)
	}
	base.Spec.TLS = []networkingv1.IngressTLS{{Hosts: []string{"custom.example.com"}, SecretName: "tls"}}
	if _, err := IngressAppSpec(base, svc); err == nil || !strings.Contains(err.Error(), "TLS Ingresses") {
		t.Errorf("TLS error = %v", err)
	}
	base.Spec.TLS = nil
	base.Spec.Rules[0].HTTP.Paths = append(base.Spec.Rules[0].HTTP.Paths, networkingv1.HTTPIngressPath{Path: "/other", PathType: &pathType, Backend: backend("other", 3000)})
	if _, err := IngressAppSpec(base, svc); err == nil || !strings.Contains(err.Error(), "same Service and port") {
		t.Errorf("mixed backends error = %v", err)
	}
}
