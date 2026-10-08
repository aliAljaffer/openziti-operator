// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type sidecarEnv struct {
	r   *ZitiSidecarReconciler
	k   client.Client
	zc  *ziti.Fake
	key types.NamespacedName
}

func enrolledIdentity(name string) *zitiv1.ZitiIdentity {
	id := &zitiv1.ZitiIdentity{Name: name, Namespace: "team-a", UID: types.UID("uid-" + name), Generation: 1}
	id.Status.ZitiName = name
	meta.SetStatusCondition(&id.Status.Conditions, metav1.Condition{Type: CondReady, Status: metav1.ConditionTrue, Reason: "Enrolled"})
	return id
}

func setupSidecar(t *testing.T, mut func(*zitiv1.ZitiSidecar), objs ...client.Object) *sidecarEnv {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{zitiv1.AddToScheme, corev1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	conn := &zitiv1.ZitiConnection{Name: "default"}
	conn.Status.ControllerVersion = "v2.0.4"
	sc := &zitiv1.ZitiSidecar{Name: "tun", Namespace: "team-a", UID: "uid-sc", Generation: 1,
		Spec: zitiv1.ZitiSidecarSpec{ConnectionRef: "default", IdentityRef: "alice", Mode: "Host",
			ManifestSecretRef: &zitiv1.SecretRef{Namespace: "team-a", Name: "tun-patch"}}}
	if mut != nil {
		mut(sc)
	}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(objs, conn, sc)...).
		WithStatusSubresource(&zitiv1.ZitiSidecar{}, &zitiv1.ZitiIdentity{}).Build()
	zc := ziti.NewFake()
	return &sidecarEnv{r: &ZitiSidecarReconciler{Client: k, Scheme: scheme, Clients: staticProvider{zc},
		Recorder: record.NewFakeRecorder(50)}, k: k, zc: zc, key: types.NamespacedName{Namespace: "team-a", Name: "tun"}}
}

func (e *sidecarEnv) reconcile(t *testing.T) *zitiv1.ZitiSidecar {
	t.Helper()
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var sc zitiv1.ZitiSidecar
	if err := e.k.Get(t.Context(), e.key, &sc); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil
		}
		t.Fatal(err)
	}
	return &sc
}

func sidecarCond(sc *zitiv1.ZitiSidecar, typ string) metav1.Condition {
	for _, c := range sc.Status.Conditions {
		if c.Type == typ {
			return c
		}
	}
	return metav1.Condition{}
}

func TestSidecarWritesThePatchWhenTheIdentityIsEnrolled(t *testing.T) {
	e := setupSidecar(t, nil, enrolledIdentity("alice"))
	sc := e.reconcile(t)
	if !sc.Status.IdentityEnrolled || sc.Status.ZitiName != "alice" {
		t.Errorf("status = %+v", sc.Status)
	}
	if sidecarCond(sc, CondReady).Status != metav1.ConditionTrue || sidecarCond(sc, CondIdentityReady).Status != metav1.ConditionTrue {
		t.Errorf("conditions = %+v", sc.Status.Conditions)
	}
	var s corev1.Secret
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "team-a", Name: "tun-patch"}, &s); err != nil {
		t.Fatalf("secret: %v", err)
	}
	patch := string(s.Data[desired.SidecarSecretKey])
	for _, want := range []string{"ziti-tunneler", "openziti/ziti-router:2.0.4", "NET_ADMIN", "--patch-file"} {
		if !strings.Contains(patch, want) {
			t.Errorf("patch is missing %q: %s", want, patch)
		}
	}
	if !metav1.IsControlledBy(&s, e.reconcile(t)) {
		t.Error("the operator must own the Secret it writes, so deleting the resource cleans up")
	}
}

func TestSidecarWaitsForTheIdentityAndWritesNothing(t *testing.T) {
	id := enrolledIdentity("alice")
	meta.SetStatusCondition(&id.Status.Conditions, metav1.Condition{Type: CondReady, Status: metav1.ConditionFalse, Reason: "PendingEnrollment"})
	e := setupSidecar(t, nil, id)
	sc := e.reconcile(t)
	if sc.Status.IdentityEnrolled || sidecarCond(sc, CondIdentityReady).Status != metav1.ConditionFalse {
		t.Errorf("status = %+v", sc.Status)
	}
	if c := sidecarCond(sc, CondReady); c.Status != metav1.ConditionFalse || c.Reason != "IdentityNotEnrolled" || !strings.Contains(c.Message, "identity file") {
		t.Errorf("ready = %+v", c)
	}
	var s corev1.Secret
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "team-a", Name: "tun-patch"}, &s); err == nil {
		t.Error("no patch may be written before the identity is enrolled")
	}
}

func TestSidecarReportsAMissingIdentity(t *testing.T) {
	e := setupSidecar(t, nil)
	sc := e.reconcile(t)
	if c := sidecarCond(sc, CondSynced); c.Status != metav1.ConditionFalse || c.Reason != "TargetNotFound" ||
		!strings.Contains(c.Message, "alice") {
		t.Errorf("synced = %+v", c)
	}
}

func TestSidecarWithoutASecretOnlyReports(t *testing.T) {
	e := setupSidecar(t, func(s *zitiv1.ZitiSidecar) { s.Spec.ManifestSecretRef = nil }, enrolledIdentity("alice"))
	sc := e.reconcile(t)
	if sidecarCond(sc, CondReady).Status != metav1.ConditionTrue || sidecarCond(sc, CondReady).Reason != "Reported" {
		t.Errorf("ready = %+v", sidecarCond(sc, CondReady))
	}
	var list corev1.SecretList
	if err := e.k.List(t.Context(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Errorf("secrets = %d, want none", len(list.Items))
	}
}

func TestSidecarSecretConflictNamespaceLimitAndDelete(t *testing.T) {
	foreign := &corev1.Secret{Name: "tun-patch", Namespace: "team-a"}
	e := setupSidecar(t, nil, enrolledIdentity("alice"), foreign)
	if c := sidecarCond(e.reconcile(t), CondSynced); c.Reason != "SecretConflict" {
		t.Errorf("foreign secret: %+v", c)
	}

	e = setupSidecar(t, nil, enrolledIdentity("alice"))
	e.r.SecretNamespaces = []string{"other"}
	if c := sidecarCond(e.reconcile(t), CondSynced); c.Reason != "SecretNamespaceNotAllowed" {
		t.Errorf("namespace limit: %+v", c)
	}

	e = setupSidecar(t, nil, enrolledIdentity("alice"))
	sc := e.reconcile(t)
	if err := e.k.Delete(t.Context(), sc); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	var s corev1.Secret
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "team-a", Name: "tun-patch"}, &s); err == nil {
		// The operator clears the key. The now empty Secret goes with the resource through its owner reference.
		if len(s.Data[desired.SidecarSecretKey]) != 0 {
			t.Errorf("deleting the resource must clear the patch: %v", s.Data)
		}
	}
}

func TestSidecarNeedsTheControllerVersion(t *testing.T) {
	e := setupSidecar(t, nil, enrolledIdentity("alice"))
	var conn zitiv1.ZitiConnection
	if err := e.k.Get(t.Context(), types.NamespacedName{Name: "default"}, &conn); err != nil {
		t.Fatal(err)
	}
	conn.Status.ControllerVersion = ""
	if err := e.k.Update(t.Context(), &conn); err != nil {
		t.Fatal(err)
	}
	if c := sidecarCond(e.reconcile(t), CondSynced); c.Reason != "ConnectionNotReady" {
		t.Errorf("synced = %+v", c)
	}
}
