// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"slices"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/check"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type routerEnv struct {
	r   *ZitiRouterReconciler
	zc  *ziti.Fake
	k   client.Client
	key types.NamespacedName
}

func setupRouter(t *testing.T, mut func(*zitiv1.ZitiRouter), objs ...client.Object) *routerEnv {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	conn := &zitiv1.ZitiConnection{Name: "default"}
	rt := &zitiv1.ZitiRouter{
		Name: "edge-1", UID: "uid-rt", Generation: 1,
		Spec: zitiv1.ZitiRouterSpec{
			ConnectionRef: "default", DeletionPolicy: zitiv1.DeletionPolicyDelete, RoleAttributes: []string{"edge"}, TunnelerEnabled: true, Cost: 10,
			EnrollmentSecretRef: &zitiv1.SecretRef{Namespace: "routers", Name: "edge-1-enrollment"},
		},
	}
	if mut != nil {
		mut(rt)
	}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(objs, conn, rt)...).
		WithStatusSubresource(&zitiv1.ZitiRouter{}, &appsv1.Deployment{}).Build()
	zc := ziti.NewFake()
	return &routerEnv{
		r:  &ZitiRouterReconciler{Client: k, Scheme: scheme, Clients: staticProvider{zc}, Recorder: record.NewFakeRecorder(50)},
		zc: zc, k: k, key: types.NamespacedName{Name: "edge-1"},
	}
}

func (e *routerEnv) reconcile(t *testing.T) *zitiv1.ZitiRouter {
	t.Helper()
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var rt zitiv1.ZitiRouter
	if err := e.k.Get(t.Context(), e.key, &rt); err != nil {
		return nil
	}
	return &rt
}

func (e *routerEnv) secret(t *testing.T) (*corev1.Secret, error) {
	var s corev1.Secret
	return &s, e.k.Get(t.Context(), types.NamespacedName{Namespace: "routers", Name: "edge-1-enrollment"}, &s)
}

func (e *routerEnv) router() ziti.Entity { return only(e.zc.Objects[ziti.EdgeRouters]) }

func routerCond(rt *zitiv1.ZitiRouter, typ string) metav1.Condition {
	for _, c := range rt.Status.Conditions {
		if c.Type == typ {
			return c
		}
	}
	return metav1.Condition{}
}

func TestRouterIsCreatedAndItsJWTGoesToTheSecretOnly(t *testing.T) {
	e := setupRouter(t, nil)
	rt := e.reconcile(t)

	got := e.router()
	if got.Name() != "default-edge-1" || got["isTunnelerEnabled"] != true || got["cost"] != float64(10) && got["cost"] != int32(10) || got["noTraversal"] != false {
		t.Errorf("router = %v", got)
	}
	s, err := e.secret(t)
	if err != nil {
		t.Fatal(err)
	}
	jwt := string(s.Data[SecretKeyJWT])
	if jwt == "" || !metav1.IsControlledBy(s, rt) || s.Labels[ManagedByLabel] != ManagedByLabelValue {
		t.Fatalf("secret = %+v", s)
	}
	if rt.Status.RouterID != got.ID() || rt.Status.Enrolled || rt.Status.EnrollmentExpiresAt == nil {
		t.Errorf("status = %+v", rt.Status)
	}
	if routerCond(rt, CondReady).Status != metav1.ConditionFalse || routerCond(rt, CondSynced).Status != metav1.ConditionTrue {
		t.Errorf("conditions = %+v", rt.Status.Conditions)
	}
	if list, _ := e.zc.List(t.Context(), ziti.EdgeRouters, ""); list[0]["enrollmentJwt"] != nil {
		t.Error("List must not return the enrollment JWT")
	}

	e.zc.Calls = nil
	e.reconcile(t)
	for _, c := range e.zc.Calls {
		if strings.HasPrefix(c, "create") || strings.HasPrefix(c, "update") || strings.HasPrefix(c, "delete") || strings.HasPrefix(c, "re-enroll") {
			t.Errorf("second reconcile wrote: %s", c)
		}
	}
}

func TestRouterExpiredJWTIsReplaced(t *testing.T) {
	e := setupRouter(t, nil)
	e.reconcile(t)
	s, _ := e.secret(t)
	old := string(s.Data[SecretKeyJWT])
	e.router()["enrollmentExpiresAt"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)

	e.reconcile(t)
	s, _ = e.secret(t)
	if got := string(s.Data[SecretKeyJWT]); got == "" || got == old {
		t.Errorf("jwt = %q, old %q", got, old)
	}
	var reenrolled bool
	for _, c := range e.zc.Calls {
		reenrolled = reenrolled || strings.HasPrefix(c, "re-enroll")
	}
	if !reenrolled {
		t.Error("an expired JWT must be replaced by Ziti")
	}
}

func TestRouterEnrolledDropsTheJWTAndReportsOnline(t *testing.T) {
	e := setupRouter(t, nil)
	e.reconcile(t)
	e.router()["isVerified"] = true
	rt := e.reconcile(t)
	if s, _ := e.secret(t); len(s.Data[SecretKeyJWT]) != 0 {
		t.Error("the JWT must leave the Secret once the router has enrolled")
	}
	if !rt.Status.Enrolled || rt.Status.EnrollmentExpiresAt != nil || routerCond(rt, CondEnrolled).Status != metav1.ConditionTrue {
		t.Errorf("status = %+v", rt.Status)
	}
	if routerCond(rt, CondReady).Status != metav1.ConditionFalse || !strings.Contains(routerCond(rt, CondReady).Message, "not connected") {
		t.Errorf("enrolled but offline: %+v", routerCond(rt, CondReady))
	}

	e.router()["isOnline"] = true
	rt = e.reconcile(t)
	if !rt.Status.Online || routerCond(rt, CondReady).Status != metav1.ConditionTrue {
		t.Errorf("online: %+v", rt.Status)
	}
}

func TestRouterWithoutASecretRefStillWorks(t *testing.T) {
	e := setupRouter(t, func(r *zitiv1.ZitiRouter) { r.Spec.EnrollmentSecretRef = nil })
	rt := e.reconcile(t)
	if rt.Status.RouterID == "" || rt.Status.EnrollmentExpiresAt == nil {
		t.Errorf("status = %+v", rt.Status)
	}
	if _, err := e.secret(t); err == nil {
		t.Error("no Secret may be created without a secretRef")
	}
}

func TestRouterSpecChangesReachZitiWithoutLosingTheEnrollment(t *testing.T) {
	e := setupRouter(t, nil)
	rt := e.reconcile(t)
	rt.Spec.RoleAttributes, rt.Spec.Cost, rt.Spec.NoTraversal = []string{"edge", "eu"}, 20, true
	if err := e.k.Update(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	got := e.router()
	if len(got["roleAttributes"].([]any)) != 2 || got["noTraversal"] != true {
		t.Errorf("router = %v", got)
	}
	if jwt, _, _ := e.zc.Enrollment(t.Context(), ziti.EdgeRouters, got.ID()); jwt == "" {
		t.Error("an update must keep the pending enrollment")
	}
}

func TestRouterSecretConflictNamespaceLimitAndDelete(t *testing.T) {
	foreign := &corev1.Secret{Name: "edge-1-enrollment", Namespace: "routers"}
	e := setupRouter(t, nil, foreign)
	if c := routerCond(e.reconcile(t), CondSynced); c.Reason != "SecretConflict" {
		t.Errorf("foreign Secret: %+v", c)
	}

	e = setupRouter(t, nil)
	e.r.SecretNamespaces = []string{"other"}
	if c := routerCond(e.reconcile(t), CondSynced); c.Reason != "SecretNamespaceNotAllowed" {
		t.Errorf("namespace limit: %+v", c)
	}
	if len(e.zc.Objects[ziti.EdgeRouters]) != 0 {
		t.Error("nothing may be created when the Secret namespace is not allowed")
	}

	for _, policy := range []zitiv1.DeletionPolicy{zitiv1.DeletionPolicyDelete, zitiv1.DeletionPolicyOrphan} {
		e := setupRouter(t, func(r *zitiv1.ZitiRouter) { r.Spec.DeletionPolicy = policy })
		rt := e.reconcile(t)
		if err := e.k.Delete(t.Context(), rt); err != nil {
			t.Fatal(err)
		}
		e.reconcile(t)
		want := 0
		if policy == zitiv1.DeletionPolicyOrphan {
			want = 1
		}
		if n := len(e.zc.Objects[ziti.EdgeRouters]); n != want {
			t.Errorf("%s: routers = %d, want %d", policy, n, want)
		}
	}
}

func TestRouterReportsWhatItTerminates(t *testing.T) {
	e := setupRouter(t, nil)
	e.reconcile(t)
	if c := routerCond(e.reconcile(t), CondServing); c.Status != metav1.ConditionTrue {
		t.Errorf("a router no policy picked for must serve: %+v", c)
	}

	rid := e.router().ID()
	sid := e.zc.Put(ziti.Services, ziti.Entity{"name": "billing"})
	e.zc.Put(ziti.ServiceEdgeRouterPolicies, ziti.Entity{
		"name": "billing-serp", "semantic": "AnyOf",
		"edgeRouterRoles": []any{"@" + rid}, "serviceRoles": []any{"@" + sid},
	})
	e.router()["isVerified"], e.router()["isOnline"] = true, true

	rt := e.reconcile(t)
	c := routerCond(rt, CondServing)
	if c.Status != metav1.ConditionFalse || c.Reason != check.NoTerminator || !strings.Contains(c.Message, "billing") {
		t.Errorf("picked but not terminating: %+v", c)
	}
	if routerCond(rt, CondReady).Status != metav1.ConditionTrue {
		t.Error("Serving must not fail Ready, an online router that has no terminator is not a failure")
	}

	e.zc.Put(ziti.Terminators, ziti.Entity{"serviceId": sid, "routerId": rid})
	rt = e.reconcile(t)
	if c := routerCond(rt, CondServing); c.Status != metav1.ConditionTrue {
		t.Errorf("terminating: %+v", c)
	}
	if len(rt.Status.Services) != 1 || rt.Status.Services[0] != "billing" {
		t.Errorf("status.services = %v", rt.Status.Services)
	}
}

// setupDeployedRouter turns on spec.deployment and drops the named enrollment Secret, so the operator keeps its own.
func setupDeployedRouter(t *testing.T, mut func(*zitiv1.ZitiRouter), objs ...client.Object) *routerEnv {
	e := setupRouter(t, func(r *zitiv1.ZitiRouter) {
		r.Spec.EnrollmentSecretRef = nil
		r.Spec.AdvertisedAddress = "edge.example.com"
		r.Spec.Deployment = &zitiv1.ZitiRouterDeployment{Namespace: "routers"}
		if mut != nil {
			mut(r)
		}
	}, objs...)
	var conn zitiv1.ZitiConnection
	if err := e.k.Get(t.Context(), types.NamespacedName{Name: "default"}, &conn); err != nil {
		t.Fatal(err)
	}
	conn.Status.ControllerVersion = "v2.0.4"
	if err := e.k.Update(t.Context(), &conn); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestRouterEnrollmentIsNotReplacedWhileItIsStillValid(t *testing.T) {
	e := setupRouter(t, nil)
	e.reconcile(t)

	// The router takes the JWT and Ziti stops returning one, but isVerified is not true yet. That is the window in
	// which re-enrolling destroys an enrollment that is about to succeed.
	e.router()["enrollmentJwt"] = nil
	e.zc.Calls = nil
	e.reconcile(t)
	for _, c := range e.zc.Calls {
		if strings.HasPrefix(c, "re-enroll") {
			t.Errorf("a consumed JWT with a live enrollment must not be replaced: %v", e.zc.Calls)
		}
	}

	// Once the enrollment itself has run out, a new JWT is due.
	e.router()["enrollmentExpiresAt"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	e.zc.Calls = nil
	e.reconcile(t)
	var reenrolled bool
	for _, c := range e.zc.Calls {
		reenrolled = reenrolled || strings.HasPrefix(c, "re-enroll")
	}
	if !reenrolled {
		t.Errorf("an expired unused JWT must be replaced by Ziti: %v", e.zc.Calls)
	}
}

func TestRouterDeploymentRunsTheRouterInTheCluster(t *testing.T) {
	e := setupDeployedRouter(t, nil)
	rt := e.reconcile(t)

	key := types.NamespacedName{Namespace: "routers", Name: "edge-1"}
	var dep appsv1.Deployment
	if err := e.k.Get(t.Context(), key, &dep); err != nil {
		t.Fatalf("deployment: %v", err)
	}
	var pvc corev1.PersistentVolumeClaim
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "routers", Name: "edge-1-data"}, &pvc); err != nil {
		t.Fatalf("claim: %v", err)
	}
	// The operator must not write Services, so the Service is a manifest in the Secret instead.
	var svc corev1.Service
	if err := e.k.Get(t.Context(), key, &svc); err == nil {
		t.Error("the operator must not create a Service, it has no cluster-wide write access to one")
	}
	secret, err := e.secret(t)
	if err != nil {
		t.Fatal(err)
	}
	var want corev1.Service
	if err := yaml.Unmarshal(secret.Data[SecretKeyService], &want); err != nil {
		t.Fatalf("service.yaml: %v\n%s", err, secret.Data[SecretKeyService])
	}
	if want.Name != "edge-1" || want.Namespace != "routers" || len(want.Spec.Ports) != 1 {
		t.Errorf("service manifest = %+v", want)
	}
	// The operator owns all three, so deleting the router removes them. A cluster-scoped owner of a namespaced
	// object is allowed by Kubernetes garbage collection.
	for _, o := range []client.Object{&dep, &pvc} {
		if !metav1.IsControlledBy(o, rt) {
			t.Errorf("%s is not owned by the router", o.GetName())
		}
	}
	// The operator keeps its own Secret when the spec does not name one, and the pod reads the token by reference.
	s, err := e.secret(t)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if string(s.Data[SecretKeyJWT]) == "" {
		t.Errorf("secret has no JWT: %v", s.Data)
	}
	env := dep.Spec.Template.Spec.Containers[0].Env[0]
	if env.Name != "ZITI_ENROLL_TOKEN" || env.ValueFrom == nil || env.ValueFrom.SecretKeyRef.Name != "edge-1-enrollment" {
		t.Errorf("env = %+v", env)
	}
	if c := routerCond(rt, CondWorkload); c.Status != metav1.ConditionFalse || c.Reason != "DeploymentUnavailable" {
		t.Errorf("workload before a ready pod: %+v", c)
	}

	dep.Status.ReadyReplicas = 1
	if err := e.k.Status().Update(t.Context(), &dep); err != nil {
		t.Fatal(err)
	}
	if c := routerCond(e.reconcile(t), CondWorkload); c.Status != metav1.ConditionTrue || c.Reason != "Running" {
		t.Errorf("workload = %+v", c)
	}
}

func TestRouterDeploymentIsSteadyAndFollowsTheSpec(t *testing.T) {
	e := setupDeployedRouter(t, nil)
	e.reconcile(t)

	e.zc.Calls = nil
	e.reconcile(t)
	for _, c := range e.zc.Calls {
		if strings.HasPrefix(c, "create") || strings.HasPrefix(c, "update") || strings.HasPrefix(c, "delete") || strings.HasPrefix(c, "re-enroll") {
			t.Errorf("second reconcile wrote: %s", c)
		}
	}

	var dep appsv1.Deployment
	key := types.NamespacedName{Namespace: "routers", Name: "edge-1"}
	if err := e.k.Get(t.Context(), key, &dep); err != nil {
		t.Fatal(err)
	}
	before := dep.ResourceVersion
	e.reconcile(t)
	if err := e.k.Get(t.Context(), key, &dep); err != nil {
		t.Fatal(err)
	}
	if dep.ResourceVersion != before {
		t.Errorf("a steady reconcile rewrote the Deployment: %v -> %v", before, dep.ResourceVersion)
	}

	rt := e.reconcile(t)
	rt.Spec.Deployment.ServiceType = "NodePort"
	rt.Spec.Port = 4100
	if err := e.k.Update(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	if err := e.k.Get(t.Context(), key, &dep); err != nil {
		t.Fatal(err)
	}
	if dep.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort != 4100 {
		t.Errorf("port not applied: %d", dep.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort)
	}
	secret, err := e.secret(t)
	if err != nil {
		t.Fatal(err)
	}
	var manifest corev1.Service
	if err := yaml.Unmarshal(secret.Data[SecretKeyService], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Spec.Type != corev1.ServiceTypeNodePort || manifest.Spec.Ports[0].Port != 4100 {
		t.Errorf("service manifest not applied: %+v", manifest.Spec)
	}
	// The claim spec is immutable, so a port change must not touch it.
	var pvc corev1.PersistentVolumeClaim
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "routers", Name: "edge-1-data"}, &pvc); err != nil {
		t.Fatal(err)
	}
	if pvc.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("claim was rewritten: %+v", pvc.Spec)
	}
}

func TestRouterDeploymentRefusesForeignObjects(t *testing.T) {
	for _, foreign := range []client.Object{
		&appsv1.Deployment{Name: "edge-1", Namespace: "routers"},
		&corev1.PersistentVolumeClaim{Name: "edge-1-data", Namespace: "routers"},
	} {
		e := setupDeployedRouter(t, nil, foreign.DeepCopyObject().(client.Object))
		rt := e.reconcile(t)
		if c := routerCond(rt, CondSynced); c.Reason != "NameConflict" {
			t.Errorf("%s: synced = %+v", foreign.GetName(), c)
		}
		// The operator must leave it exactly as it found it.
		if err := e.k.Get(t.Context(), client.ObjectKeyFromObject(foreign), foreign); err != nil {
			t.Fatal(err)
		}
		if len(foreign.GetOwnerReferences()) != 0 {
			t.Errorf("%s: the operator took over a foreign object: %+v", foreign.GetName(), foreign.GetOwnerReferences())
		}
	}
}

func TestRouterDeploymentNeedsTheControllerVersion(t *testing.T) {
	e := setupDeployedRouter(t, nil)
	var conn zitiv1.ZitiConnection
	if err := e.k.Get(t.Context(), types.NamespacedName{Name: "default"}, &conn); err != nil {
		t.Fatal(err)
	}
	conn.Status.ControllerVersion = ""
	if err := e.k.Update(t.Context(), &conn); err != nil {
		t.Fatal(err)
	}
	rt := e.reconcile(t)
	if c := routerCond(rt, CondWorkload); c.Reason != "ControllerVersionUnknown" {
		t.Errorf("workload = %+v", c)
	}
	var dep appsv1.Deployment
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "routers", Name: "edge-1"}, &dep); err == nil {
		t.Error("no Deployment may be created without an image")
	}
	// An explicit image needs no controller version.
	e = setupDeployedRouter(t, func(r *zitiv1.ZitiRouter) { r.Spec.Deployment.Image = "registry.example.com/router:1" })
	if err := e.k.Get(t.Context(), types.NamespacedName{Name: "default"}, &conn); err != nil {
		t.Fatal(err)
	}
	conn.Status.ControllerVersion = ""
	if err := e.k.Update(t.Context(), &conn); err != nil {
		t.Fatal(err)
	}
	if c := routerCond(e.reconcile(t), CondWorkload); c.Reason != "DeploymentUnavailable" {
		t.Errorf("workload = %+v", c)
	}
}

func TestRouterWithoutADeploymentHasNoWorkloadCondition(t *testing.T) {
	e := setupDeployedRouter(t, nil)
	e.reconcile(t)
	rt := e.reconcile(t)
	rt.Spec.Deployment = nil
	if err := e.k.Update(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	rt = e.reconcile(t)
	if c := routerCond(rt, CondWorkload); c.Type != "" {
		t.Errorf("workload condition must go away with the spec: %+v", c)
	}
	// The workload is left for garbage collection through its owner reference.
	var dep appsv1.Deployment
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "routers", Name: "edge-1"}, &dep); err != nil {
		t.Fatalf("the operator must not delete the workload itself: %v", err)
	}
}

func TestRouterDeploymentReportsAStorageClassItCannotChange(t *testing.T) {
	e := setupDeployedRouter(t, func(r *zitiv1.ZitiRouter) { r.Spec.StorageClassName = "wanted" })
	e.reconcile(t)

	// A bound claim keeps the class it was created with, so the operator must say so instead of waiting.
	rt := e.reconcile(t)
	rt.Spec.StorageClassName = "other"
	if err := e.k.Update(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	rt = e.reconcile(t)
	c := routerCond(rt, CondWorkload)
	if c.Status != metav1.ConditionFalse || c.Reason != "StorageClassLocked" || !strings.Contains(c.Message, "wanted") {
		t.Errorf("workload = %+v", c)
	}
	// The storage class must not become the reason the router is not ready: that reason is the enrollment state.
	if r := routerCond(rt, CondReady).Reason; r == "StorageClassLocked" {
		t.Errorf("a storage class problem must not drive Ready: %+v", routerCond(rt, CondReady))
	}
}

func TestRouterDeploymentSecretKeepsNoManifestsForTheOperatorsOwn(t *testing.T) {
	e := setupDeployedRouter(t, nil)
	e.reconcile(t)
	s, err := e.secret(t)
	if err != nil {
		t.Fatal(err)
	}
	// The manifests are for a hand-installed router. With no enrollmentSecretRef the pod only needs the JWT.
	if len(s.Data[SecretKeyCompose]) != 0 || len(s.Data[SecretKeyDeployment]) != 0 {
		t.Errorf("secret keys = %v", keysOf(s.Data))
	}
	if string(s.Data[SecretKeyJWT]) == "" {
		t.Error("the pod still needs the JWT before it enrolls")
	}

	// A named Secret keeps them, because that is how a user installs a router by hand.
	e = setupRouter(t, func(r *zitiv1.ZitiRouter) {
		r.Spec.AdvertisedAddress = "edge.example.com"
		r.Spec.EnrollmentSecretRef = &zitiv1.SecretRef{Namespace: "routers", Name: "edge-1-enrollment"}
	})
	var conn zitiv1.ZitiConnection
	if err := e.k.Get(t.Context(), types.NamespacedName{Name: "default"}, &conn); err != nil {
		t.Fatal(err)
	}
	conn.Status.ControllerVersion = "v2.0.4"
	if err := e.k.Update(t.Context(), &conn); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	s, err = e.secret(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Data[SecretKeyCompose]) == 0 || len(s.Data[SecretKeyDeployment]) == 0 {
		t.Errorf("named secret keys = %v", keysOf(s.Data))
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestRouterManifestsFollowTheAddressAndLeaveWithTheJWT(t *testing.T) {
	e := setupRouter(t, nil)
	var conn zitiv1.ZitiConnection
	if err := e.k.Get(t.Context(), types.NamespacedName{Name: "default"}, &conn); err != nil {
		t.Fatal(err)
	}
	conn.Status.ControllerVersion = "v2.0.4"
	if err := e.k.Update(t.Context(), &conn); err != nil {
		t.Fatal(err)
	}

	e.reconcile(t)
	s, _ := e.secret(t)
	if c := string(s.Data[SecretKeyCompose]); !strings.Contains(c, "CHANGE_ME.invalid") || !strings.Contains(c, string(s.Data[SecretKeyJWT])) ||
		!strings.Contains(c, "ziti-router:2.0.4") || len(s.Data[SecretKeyDeployment]) == 0 {
		t.Errorf("manifests = %q", s.Data)
	}

	rt := e.reconcile(t)
	rt.Spec.AdvertisedAddress, rt.Spec.Port = "vm1.example.com", 4000
	if err := e.k.Update(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	s, _ = e.secret(t)
	if c := string(s.Data[SecretKeyCompose]); !strings.Contains(c, "vm1.example.com") || !strings.Contains(c, `"4000:4000"`) || strings.Contains(c, "CHANGE_ME") {
		t.Errorf("compose = %s", c)
	}

	e.router()["isVerified"] = true
	e.reconcile(t)
	s, _ = e.secret(t)
	if len(s.Data[SecretKeyJWT])+len(s.Data[SecretKeyCompose])+len(s.Data[SecretKeyDeployment]) != 0 {
		t.Errorf("the JWT and manifests must leave the Secret once the router has enrolled: %v", s.Data)
	}
}
