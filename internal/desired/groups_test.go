// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func app(targets ...zitiv1.Target) *Builder {
	svc := &zitiv1.ZitiApp{
		ObjectMeta: meta(),
		Spec: zitiv1.ZitiAppSpec{
			ZitiName: "web.example.com",
			Expose:   zitiv1.Expose{Addresses: []string{"web.example.com"}, Ports: []intstr.IntOrString{intstr.FromInt32(80), intstr.FromInt32(443)}, Protocols: []string{"tcp"}},
			Targets:  targets,
		},
	}
	conn := &zitiv1.ZitiConnection{Spec: zitiv1.ZitiConnectionSpec{HostingRouters: []string{"r-main"}}}
	return &Builder{Svc: svc, Conn: conn, RouterIDs: map[string]string{"r-main": "id-main"}}
}

func TestGroupsSingleWhenNoTargetPorts(t *testing.T) {
	groups, err := app(zitiv1.Target{Address: "10.0.0.1", Port: 8080}).Groups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Name != "web.example.com" {
		t.Fatalf("groups = %+v", groups)
	}
}

func TestGroupsSplitByPortSet(t *testing.T) {
	b := app(
		zitiv1.Target{Address: "10.0.0.1", Port: 8080, ExposedPorts: []intstr.IntOrString{intstr.FromInt32(80)}},
		zitiv1.Target{Address: "10.0.0.1", Port: 8443, ExposedPorts: []intstr.IntOrString{intstr.FromInt32(443)}},
	)
	groups, err := b.Groups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %+v", groups)
	}
	if groups[0].Name != "web.example.com-80" || groups[1].Name != "web.example.com-443" {
		t.Errorf("names = %q, %q", groups[0].Name, groups[1].Name)
	}
	if len(groups[0].Targets) != 1 || groups[0].Targets[0].Port != 8080 || groups[1].Targets[0].Port != 8443 {
		t.Errorf("targets = %+v %+v", groups[0].Targets, groups[1].Targets)
	}

	ic, err := b.WithGroup(groups[1]).InterceptConfig("ct-intercept")
	if err != nil {
		t.Fatal(err)
	}
	ports := ic["data"].(map[string]any)["portRanges"].([]map[string]any)
	if len(ports) != 1 || ports[0]["low"] != 443 {
		t.Errorf("intercept ranges = %v", ports)
	}
	hc, err := b.WithGroup(groups[1]).HostConfig("ct-host")
	if err != nil {
		t.Fatal(err)
	}
	terms := hc["data"].(map[string]any)["terminators"].([]map[string]any)
	if len(terms) != 1 || terms[0]["port"] != int32(8443) {
		t.Errorf("host terminators = %v", terms)
	}
}

func TestGroupsSamePortSetSharesService(t *testing.T) {
	groups, err := app(
		zitiv1.Target{Address: "10.0.0.1", Port: 8080, ExposedPorts: []intstr.IntOrString{intstr.FromInt32(80), intstr.FromInt32(443)}},
		zitiv1.Target{Address: "10.0.0.2", Port: 8080, ExposedPorts: []intstr.IntOrString{intstr.FromInt32(443), intstr.FromInt32(80)}},
	).Groups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Targets) != 2 || groups[0].Name != "web.example.com" {
		t.Fatalf("groups = %+v", groups)
	}
}

func TestGroupsErrors(t *testing.T) {
	tests := map[string]struct {
		targets []zitiv1.Target
		want    string
	}{
		"port not exposed": {[]zitiv1.Target{{Address: "a", Port: 1, ExposedPorts: []intstr.IntOrString{intstr.FromInt32(22)}}}, "not in expose.ports"},
		"port unserved":    {[]zitiv1.Target{{Address: "a", Port: 1, ExposedPorts: []intstr.IntOrString{intstr.FromInt32(80)}}}, "is not served by any target"},
		"port overlap": {
			[]zitiv1.Target{
				{Address: "a", Port: 1, ExposedPorts: []intstr.IntOrString{intstr.FromInt32(80), intstr.FromInt32(443)}},
				{Address: "b", Port: 2, ExposedPorts: []intstr.IntOrString{intstr.FromInt32(443)}},
			}, "more than one target group"},
	}
	for name, tc := range tests {
		if _, err := app(tc.targets...).Groups(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestGroupsFullPortSetOverlapsSubset(t *testing.T) {
	_, err := app(
		zitiv1.Target{Address: "10.0.0.1", Port: 8080},
		zitiv1.Target{Address: "10.0.0.1", Port: 8443, ExposedPorts: []intstr.IntOrString{intstr.FromInt32(443)}},
	).Groups()
	if err == nil || !strings.Contains(err.Error(), "more than one target group") {
		t.Fatalf("err = %v", err)
	}
}
