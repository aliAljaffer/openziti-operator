// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestRouterManifestsAreValidYAMLWithTheRightValues(t *testing.T) {
	m := RouterManifest{Name: "Edge_1", JWT: "eyJ.abc.def", Address: "vm1.example.com", Version: "v2.0.4", Port: 3022}

	var compose struct {
		Services map[string]struct {
			Image       string            `json:"image"`
			Environment map[string]string `json:"environment"`
		} `json:"services"`
	}
	if err := yaml.Unmarshal([]byte(m.Compose()), &compose); err != nil {
		t.Fatal(err)
	}
	r := compose.Services["router"]
	if r.Image != "openziti/ziti-router:2.0.4" || r.Environment["ZITI_ENROLL_TOKEN"] != "eyJ.abc.def" ||
		r.Environment["ZITI_ROUTER_ADVERTISED_ADDRESS"] != "vm1.example.com" || r.Environment["ZITI_ROUTER_PORT"] != "3022" {
		t.Errorf("router = %+v", r)
	}

	dep := m.Deployment()
	docs := strings.Split(dep, "\n---\n")
	if len(docs) != 4 {
		t.Fatalf("got %d documents", len(docs))
	}
	for _, d := range docs {
		var v map[string]any
		if err := yaml.Unmarshal([]byte(d), &v); err != nil {
			t.Fatalf("%v in:\n%s", err, d)
		}
	}
	for _, want := range []string{"name: edge-1-enrollment", "fsGroup: 2171", `"vm1.example.com"`} {
		if !strings.Contains(dep, want) {
			t.Errorf("deployment lacks %q", want)
		}
	}
	if strings.Contains(dep, "storageClassName") {
		t.Error("no storageClassName expected by default")
	}
	m.StorageClass = "fast"
	if dep := m.Deployment(); !strings.Contains(dep, `storageClassName: "fast"`) {
		t.Error("storageClassName missing")
	} else {
		for d := range strings.SplitSeq(dep, "\n---\n") {
			var v map[string]any
			if err := yaml.Unmarshal([]byte(d), &v); err != nil {
				t.Fatalf("%v in:\n%s", err, d)
			}
		}
	}
	m.StorageClass = ""
	if strings.Count(dep, "eyJ.abc.def") != 1 {
		t.Error("the token must appear only in the Secret")
	}
	if strings.Contains(dep, "CHANGE_ME") || strings.Contains(m.Compose(), "CHANGE_ME") {
		t.Error("placeholder used although the address is set")
	}
}

func TestRouterManifestsUsePlaceholderWithoutAddress(t *testing.T) {
	m := RouterManifest{Name: "e", JWT: "x", Version: "2.0.4", Port: 3022}
	for _, out := range []string{m.Compose(), m.Deployment()} {
		if !strings.Contains(out, AddressPlaceholder) || !strings.HasPrefix(out, "# Replace") {
			t.Errorf("no placeholder note:\n%s", out)
		}
	}
}
