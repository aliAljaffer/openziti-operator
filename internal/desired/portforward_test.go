// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func TestPortForwardDeploymentRunsTheProxyWithAnIdentitySecret(t *testing.T) {
	pf := &zitiv1.ZitiPortForward{}
	pf.Name, pf.Namespace = "billing", "team-a"
	pf.Spec.Service, pf.Spec.Port = "billing-service", 8080
	dep := PortForwardDeployment(pf, "billing-identity", "v2.0.4")

	if dep.Name != "billing" || dep.Namespace != "team-a" || *dep.Spec.Replicas != 1 || dep.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Errorf("deployment = %+v", dep.Spec)
	}
	if dep.Spec.Selector.MatchLabels["app"] != "billing" || dep.Spec.Template.Labels["app"] != "billing" {
		t.Errorf("labels = %+v %+v", dep.Spec.Selector, dep.Spec.Template.Labels)
	}
	c := dep.Spec.Template.Spec.Containers[0]
	if c.Image != "openziti/ziti-router:2.0.4" || c.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Errorf("image = %q %q", c.Image, c.ImagePullPolicy)
	}
	wantCommand := []string{"ziti", "tunnel", "proxy", "-i", "/ziti/identity.json", "--svcPollRate", "15", "billing-service:8080"}
	if !slices.Equal(c.Command, wantCommand) {
		t.Errorf("command = %v, want %v", c.Command, wantCommand)
	}
	if len(dep.Spec.Template.Spec.Volumes) != 1 || dep.Spec.Template.Spec.Volumes[0].Secret.SecretName != "billing-identity" {
		t.Errorf("volumes = %+v", dep.Spec.Template.Spec.Volumes)
	}
	if len(c.VolumeMounts) != 1 || c.VolumeMounts[0].MountPath != "/ziti" || !c.VolumeMounts[0].ReadOnly {
		t.Errorf("mounts = %+v", c.VolumeMounts)
	}
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != 8080 || c.ReadinessProbe == nil || c.ReadinessProbe.TCPSocket == nil {
		t.Errorf("ports/probe = %+v %+v", c.Ports, c.ReadinessProbe)
	}
	if c.SecurityContext == nil || c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation || c.SecurityContext.Capabilities != nil {
		t.Errorf("security = %+v", c.SecurityContext)
	}
}

func TestPortForwardDeploymentTrimsImageTagPrefix(t *testing.T) {
	pf := &zitiv1.ZitiPortForward{Name: "pf", Namespace: "ns", Spec: zitiv1.ZitiPortForwardSpec{Service: "svc", Port: 80}}
	if got := PortForwardDeployment(pf, "id", "2.0.4").Spec.Template.Spec.Containers[0].Image; got != "openziti/ziti-router:2.0.4" {
		t.Errorf("image = %q", got)
	}
}
