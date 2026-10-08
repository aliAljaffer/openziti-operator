// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func routerWithDeployment(mut func(*zitiv1.ZitiRouter)) *zitiv1.ZitiRouter {
	rt := &zitiv1.ZitiRouter{}
	rt.Name = "Edge_1"
	rt.Spec.AdvertisedAddress = "edge.example.com"
	rt.Spec.Deployment = &zitiv1.ZitiRouterDeployment{Namespace: "routers"}
	if mut != nil {
		mut(rt)
	}
	return rt
}

func TestRouterWorkloadDefaults(t *testing.T) {
	w := NewRouterWorkload(routerWithDeployment(nil), "v2.0.4")
	dep, svc, claim := w.Names()
	if dep != "edge-1" || svc != "edge-1" || claim != "edge-1-data" || w.SecretName() != "edge-1-enrollment" {
		t.Errorf("names = %q %q %q %q", dep, svc, claim, w.SecretName())
	}
	if w.Port != 3022 || w.ImagePullPolicy != corev1.PullIfNotPresent || w.ServiceType != corev1.ServiceTypeLoadBalancer {
		t.Errorf("defaults = %+v", w)
	}
	if w.image() != "openziti/ziti-router:2.0.4@sha256:95d29bef1fb488345eccaa8db8c7bedd22cc31a6f265645dbc222512857d964f" {
		t.Errorf("image = %s", w.image())
	}
}

func TestRouterWorkloadOverrides(t *testing.T) {
	rt := routerWithDeployment(func(r *zitiv1.ZitiRouter) {
		r.Spec.Port = 4000
		r.Spec.StorageClassName = "fast"
		r.Spec.Deployment.Image = "registry.example.com/ziti-router:custom"
		r.Spec.Deployment.ImagePullPolicy = "Always"
		r.Spec.Deployment.ServiceType = "NodePort"
	})
	w := NewRouterWorkload(rt, "v9.9.9")
	if w.image() != "registry.example.com/ziti-router:custom" || w.Port != 4000 {
		t.Errorf("workload = %+v", w)
	}
	if w.ImagePullPolicy != corev1.PullAlways || w.ServiceType != corev1.ServiceTypeNodePort {
		t.Errorf("policies = %v %v", w.ImagePullPolicy, w.ServiceType)
	}
	pvc := w.PersistentVolumeClaim()
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "fast" {
		t.Errorf("storage class = %v", pvc.Spec.StorageClassName)
	}
	if got := pvc.Spec.Resources.Requests.Storage().String(); got != StorageRequested {
		t.Errorf("storage = %s", got)
	}
	// A class the user did not set must stay unset, or the cluster default is bypassed.
	if pvc := NewRouterWorkload(routerWithDeployment(nil), "v2.0.4").PersistentVolumeClaim(); pvc.Spec.StorageClassName != nil {
		t.Errorf("storage class = %v, want nil", pvc.Spec.StorageClassName)
	}
}

func TestRouterWorkloadWithoutDeployment(t *testing.T) {
	rt := &zitiv1.ZitiRouter{}
	rt.Name = "edge-1"
	w := NewRouterWorkload(rt, "v2.0.4")
	dep, svc, claim := w.Names()
	if w.Namespace != "" || dep != "edge-1" || svc != "edge-1" || claim != "edge-1-data" {
		t.Errorf("workload = %+v", w)
	}
}

func TestRouterDeploymentRunsOneReplicaAsTheImageUser(t *testing.T) {
	w := NewRouterWorkload(routerWithDeployment(nil), "v2.0.4")
	dep := w.Deployment("edge-1-enrollment")

	if dep.Namespace != "routers" || dep.Name != "edge-1" {
		t.Errorf("metadata = %s/%s", dep.Namespace, dep.Name)
	}
	if *dep.Spec.Replicas != 1 || dep.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Errorf("replicas/strategy = %d %s", *dep.Spec.Replicas, dep.Spec.Strategy.Type)
	}
	// A second router process with the same identity would fight the first.
	if !slices.ContainsFunc(dep.Spec.Template.Spec.Containers, func(c corev1.Container) bool { return c.Name == "router" }) {
		t.Fatalf("containers = %+v", dep.Spec.Template.Spec.Containers)
	}
	c := dep.Spec.Template.Spec.Containers[0]
	if c.Image != w.image() || c.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Errorf("image = %s %s", c.Image, c.ImagePullPolicy)
	}
	sc := dep.Spec.Template.Spec.SecurityContext
	if sc == nil || sc.FSGroup == nil || *sc.FSGroup != 2171 || sc.RunAsUser == nil || *sc.RunAsUser != 2171 || !*sc.RunAsNonRoot {
		t.Errorf("pod security context = %+v", sc)
	}
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != 3022 {
		t.Errorf("ports = %+v", c.Ports)
	}
	if c.LivenessProbe == nil || c.LivenessProbe.TCPSocket == nil || c.LivenessProbe.TCPSocket.Port.IntValue() != 3022 {
		t.Errorf("liveness probe = %+v", c.LivenessProbe)
	}
	if len(c.VolumeMounts) != 1 || c.VolumeMounts[0].MountPath != DataDir {
		t.Errorf("volume mounts = %+v", c.VolumeMounts)
	}
	if len(dep.Spec.Template.Spec.Volumes) != 1 || dep.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != "edge-1-data" {
		t.Errorf("volumes = %+v", dep.Spec.Template.Spec.Volumes)
	}
	if dep.Spec.Selector.MatchLabels["app"] != "edge-1" {
		t.Errorf("selector = %+v", dep.Spec.Selector)
	}
}

func TestRouterDeploymentReadsTheTokenFromTheSecret(t *testing.T) {
	w := NewRouterWorkload(routerWithDeployment(func(r *zitiv1.ZitiRouter) { r.Spec.Port = 4000 }), "v2.0.4")
	dep := w.Deployment("my-secret")

	var token *corev1.EnvVar
	names := []string{}
	for _, e := range dep.Spec.Template.Spec.Containers[0].Env {
		names = append(names, e.Name)
		if e.Name == "ZITI_ENROLL_TOKEN" {
			token = &e
		}
	}
	// The operator removes the JWT once the router has enrolled, so the pod must start without it.
	if token == nil || token.ValueFrom == nil || token.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("token env = %+v", token)
	}
	ref := token.ValueFrom.SecretKeyRef
	if ref.Name != "my-secret" || ref.Key != EnrollTokenKey || ref.Optional == nil || !*ref.Optional {
		t.Errorf("secret key ref = %+v", ref)
	}
	if token.Value != "" {
		t.Error("the token must never be inlined in the pod spec")
	}
	for _, want := range []string{"ZITI_BOOTSTRAP", "ZITI_BOOTSTRAP_CONFIG", "ZITI_BOOTSTRAP_ENROLLMENT",
		"ZITI_AUTO_RENEW_CERTS", "ZITI_ROUTER_ADVERTISED_ADDRESS", "ZITI_ROUTER_PORT"} {
		if !slices.Contains(names, want) {
			t.Errorf("%s missing from %v", want, names)
		}
	}
}

func TestRouterService(t *testing.T) {
	w := NewRouterWorkload(routerWithDeployment(func(r *zitiv1.ZitiRouter) {
		r.Spec.Deployment.ServiceType = "NodePort"
		r.Spec.Port = 4000
	}), "v2.0.4")
	svc := w.Service()
	if svc.Name != "edge-1" || svc.Namespace != "routers" || svc.Spec.Type != corev1.ServiceTypeNodePort {
		t.Errorf("service = %+v", svc)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 4000 || svc.Spec.Ports[0].TargetPort.IntValue() != 4000 {
		t.Errorf("ports = %+v", svc.Spec.Ports)
	}
	if svc.Spec.Selector["app"] != "edge-1" || svc.Spec.Selector["app"] != w.Deployment("s").Spec.Template.Labels["app"] {
		t.Errorf("selector = %+v", svc.Spec.Selector)
	}
}
