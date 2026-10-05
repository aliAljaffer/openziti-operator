// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
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
	conn := &zitiv1.ZitiConnection{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	rt := &zitiv1.ZitiRouter{
		ObjectMeta: metav1.ObjectMeta{Name: "edge-1", UID: "uid-rt", Generation: 1},
		Spec: zitiv1.ZitiRouterSpec{
			ConnectionRef: "default", DeletionPolicy: zitiv1.DeletionPolicyDelete, RoleAttributes: []string{"edge"}, TunnelerEnabled: true, Cost: 10,
			EnrollmentSecretRef: &zitiv1.SecretRef{Namespace: "routers", Name: "edge-1-enrollment"},
		},
	}
	if mut != nil {
		mut(rt)
	}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(objs, conn, rt)...).WithStatusSubresource(&zitiv1.ZitiRouter{}).Build()
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
	foreign := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "edge-1-enrollment", Namespace: "routers"}}
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
