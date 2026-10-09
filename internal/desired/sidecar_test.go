// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func sidecar(mut func(*zitiv1.ZitiSidecar)) *zitiv1.ZitiSidecar {
	sc := &zitiv1.ZitiSidecar{}
	sc.Name = "side"
	sc.Namespace = "team-a"
	sc.Spec = zitiv1.ZitiSidecarSpec{IdentityRef: "alice", Mode: "Host",
		Resolver: "udp://kube-dns.kube-system.svc.cluster.local:53", ServicePollRate: 15}
	if mut != nil {
		mut(sc)
	}
	return sc
}

func TestSidecarPatchIsValidYAMLAndAValidStrategicPatch(t *testing.T) {
	patch := SidecarPatch(sidecar(nil), "v2.0.4")
	var doc struct {
		Spec struct {
			Template struct {
				Spec struct {
					Volumes    []map[string]any `json:"volumes"`
					Containers []struct {
						Name         string   `json:"name"`
						Image        string   `json:"image"`
						Command      []string `json:"command"`
						VolumeMounts []struct {
							Name      string `json:"name"`
							MountPath string `json:"mountPath"`
							ReadOnly  bool   `json:"readOnly"`
						} `json:"volumeMounts"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal([]byte(patch), &doc); err != nil {
		t.Fatalf("the patch must be valid YAML: %v\n%s", err, patch)
	}
	if len(doc.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("patch must add exactly one container: %s", patch)
	}
	c := doc.Spec.Template.Spec.Containers[0]
	if c.Name != "ziti-tunneler" || c.Image != "openziti/ziti-router:2.0.4" {
		t.Errorf("container = %+v", c)
	}
	want := []string{"ziti", "tunnel", "host", "-i", "/ziti/identity.json", "--svcPollRate", "15",
		"-r", "udp://kube-dns.kube-system.svc.cluster.local:53"}
	if strings.Join(c.Command, " ") != strings.Join(want, " ") {
		t.Errorf("command = %v, want %v", c.Command, want)
	}
	if len(doc.Spec.Template.Spec.Volumes) != 1 || len(c.VolumeMounts) != 1 {
		t.Fatalf("volumes = %+v mounts = %+v", doc.Spec.Template.Spec.Volumes, c.VolumeMounts)
	}
	if doc.Spec.Template.Spec.Volumes[0]["secret"] == nil || c.VolumeMounts[0].MountPath != IdentityMountPath || !c.VolumeMounts[0].ReadOnly {
		t.Errorf("volumes = %+v mounts = %+v", doc.Spec.Template.Spec.Volumes, c.VolumeMounts)
	}
}

func TestSidecarProxyModeNeedsNoResolverOrCapabilities(t *testing.T) {
	patch := SidecarPatch(sidecar(func(s *zitiv1.ZitiSidecar) { s.Spec.Mode = "Proxy" }), "v2.0.4")
	if strings.Contains(patch, "-r ") || strings.Contains(patch, "udp://") {
		t.Errorf("proxy mode must not pass a resolver: %s", patch)
	}
	if strings.Contains(patch, "NET_ADMIN") || strings.Contains(patch, "capabilities") {
		t.Errorf("proxy mode must not ask for capabilities: %s", patch)
	}
	if !strings.Contains(patch, "            - \"proxy\"\n") {
		t.Errorf("proxy mode must run the proxy subcommand: %s", patch)
	}
}

func TestSidecarHostModeAsksForTheTproxyCapabilities(t *testing.T) {
	patch := SidecarPatch(sidecar(nil), "v2.0.4")
	for _, want := range []string{"NET_ADMIN", "NET_RAW", "allowPrivilegeEscalation: false", "readOnlyRootFilesystem: false"} {
		if !strings.Contains(patch, want) {
			t.Errorf("host mode patch is missing %q: %s", want, patch)
		}
	}
	// The image default cannot resolve anything inside a pod.
	if strings.Contains(patch, "127.0.0.1:53") {
		t.Errorf("the patch must not use the image default resolver: %s", patch)
	}
}

func TestSidecarImageStripsTheVPrefix(t *testing.T) {
	if got := sidecarImage("v2.0.4"); got != "openziti/ziti-router:2.0.4" {
		t.Errorf("image = %s", got)
	}
	if got := sidecarImage("2.0.4"); got != "openziti/ziti-router:2.0.4" {
		t.Errorf("image = %s", got)
	}
}

func TestSidecarPatchMentionsHowToApplyIt(t *testing.T) {
	patch := SidecarPatch(sidecar(nil), "v2.0.4")
	if !strings.Contains(patch, "--patch-file") || !strings.Contains(patch, "--type=strategic") {
		t.Errorf("the patch must say how to apply it: %s", patch)
	}
	if !strings.Contains(patch, "team-a") {
		t.Errorf("the patch must name the namespace: %s", patch)
	}
}
