// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
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
