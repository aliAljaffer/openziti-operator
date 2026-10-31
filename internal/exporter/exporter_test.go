// SPDX-License-Identifier: Apache-2.0

package exporter

import (
	"strings"
	"testing"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func TestExportDocsKeepsZitiNamesAndMarksObserve(t *testing.T) {
	lists := map[ziti.Kind][]ziti.Entity{
		ziti.ConfigTypes: {{"id": "ct-1", "name": "intercept.v1"}},
		ziti.Configs: {
			{"id": "cfg-1", "name": "web-intercept", "configTypeId": "ct-1", "data": map[string]any{"addresses": []any{"web.example.com"}}},
		},
		ziti.Services: {
			{"id": "svc-1", "name": "web.example.com", "configs": []any{"cfg-1"}, "roleAttributes": []any{"web"}},
		},
		ziti.Identities: {
			{"id": "id-1", "name": "alice", "authPolicyId": "ap-1", "type": "User", "disabled": true},
		},
		ziti.AuthPolicies: {
			{"id": "ap-1", "name": "humans", "primary": map[string]any{}, "secondary": map[string]any{}},
		},
	}
	names := nameIndex(lists)
	configTypes := map[string]string{"ct-1": "intercept.v1"}
	var report strings.Builder
	docs := exportDocs(lists, names, configTypes, Options{Namespace: "team-a", ManagementPolicy: "Observe"}, &report)

	find := func(kind string) []any {
		for _, d := range docs {
			if d.kind == kind {
				return d.objects
			}
		}
		return nil
	}
	if got := find("ZitiConfig"); len(got) != 1 || got[0].(*zitiv1.ZitiConfig).Spec.ZitiName != "web-intercept" {
		t.Fatalf("config docs = %#v", got)
	}
	if got := find("ZitiService"); len(got) != 1 || got[0].(*zitiv1.ZitiService).Spec.Configs[0] != "web-intercept" {
		t.Fatalf("service docs = %#v", got)
	}
	ids := find("ZitiIdentity")
	if len(ids) != 1 {
		t.Fatalf("identity docs = %#v", ids)
	}
	id := ids[0].(*zitiv1.ZitiIdentity)
	if id.Spec.ZitiName != "alice" || id.Spec.ManagementPolicy != zitiv1.ManagementObserve || !id.Spec.Disabled || id.Spec.Type != "User" {
		t.Errorf("identity = %#v", id.Spec)
	}
	if !strings.Contains(report.String(), "private key") {
		t.Errorf("report should warn about keys: %s", report.String())
	}
}

func TestSanitizeIsStableAndDNSFriendly(t *testing.T) {
	if got := sanitize("web.example.com"); got != "web-example-com" {
		t.Errorf("sanitize = %q", got)
	}
	if got := sanitize("web.example.com"); got != sanitize("web.example.com") {
		t.Errorf("sanitize is not stable: %q", got)
	}
	if strings.HasPrefix(sanitize("---"), "-") {
		t.Errorf("sanitize left a leading dash: %q", sanitize("---"))
	}
}
