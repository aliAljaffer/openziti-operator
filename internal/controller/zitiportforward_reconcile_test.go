// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

type portForwardEnv struct {
	r   *ZitiPortForwardReconciler
	k   client.Client
	key types.NamespacedName
}

func setupPortForward(t *testing.T, identityReady bool, objs ...client.Object) *portForwardEnv {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{zitiv1.AddToScheme, corev1.AddToScheme, appsv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	conn := &zitiv1.ZitiConnection{Name: "default"}
	conn.Status.ControllerVersion = "v2.0.4"
	id := &zitiv1.ZitiIdentity{Name: "client", Namespace: "team-a", UID: "uid-client", Spec: zitiv1.ZitiIdentitySpec{SecretName: "client-identity"}}
	if identityReady {
		id.Status.Enrolled = true
		id.Status.ZitiName = "client-ziti"
		id.Status.Conditions = []metav1.Condition{{Type: CondReady, Status: metav1.ConditionTrue, Reason: "Enrolled"}}
	}
	secret := &corev1.Secret{Name: "client-identity", Namespace: "team-a", Data: map[string][]byte{desired.IdentityFileKey: []byte(`{"identity":"test"}`)}}
	pf := &zitiv1.ZitiPortForward{Name: "billing", Namespace: "team-a", UID: "uid-pf", Generation: 1,
		Spec: zitiv1.ZitiPortForwardSpec{ConnectionRef: "default", IdentityRef: "client", Service: "billing-service", Port: 8080}}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(objs, conn, id, secret, pf)...).
		WithStatusSubresource(&zitiv1.ZitiPortForward{}, &zitiv1.ZitiIdentity{}, &appsv1.Deployment{}).Build()
	r := &ZitiPortForwardReconciler{Client: k, Scheme: scheme, Clients: staticProvider{ziti.NewFake()}, Recorder: record.NewFakeRecorder(50)}
	return &portForwardEnv{r: r, k: k, key: types.NamespacedName{Namespace: "team-a", Name: "billing"}}
}

func (e *portForwardEnv) reconcile(t *testing.T) *zitiv1.ZitiPortForward {
	t.Helper()
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var pf zitiv1.ZitiPortForward
	if err := e.k.Get(t.Context(), e.key, &pf); err != nil {
		t.Fatal(err)
	}
	return &pf
}

func portForwardCond(pf *zitiv1.ZitiPortForward, typ string) metav1.Condition {
	for _, c := range pf.Status.Conditions {
		if c.Type == typ {
			return c
		}
	}
	return metav1.Condition{}
}

func TestPortForwardCreatesOwnedProxyDeployment(t *testing.T) {
	e := setupPortForward(t, true)
	pf := e.reconcile(t)
	var dep appsv1.Deployment
	key := types.NamespacedName{Namespace: "team-a", Name: "billing"}
	if err := e.k.Get(t.Context(), key, &dep); err != nil {
		t.Fatalf("deployment: %v", err)
	}
	if !metav1.IsControlledBy(&dep, pf) || pf.Status.DeploymentName != "billing" {
		t.Errorf("owner/status = %+v %+v", dep.OwnerReferences, pf.Status)
	}
	c := dep.Spec.Template.Spec.Containers[0]
	if c.Command[len(c.Command)-1] != "billing-service:8080" || c.Image != "openziti/ziti-router:2.0.4" {
		t.Errorf("container = %+v", c)
	}
	if portForwardCond(pf, CondPortForwardIdentity).Status != metav1.ConditionTrue || portForwardCond(pf, CondReady).Status != metav1.ConditionFalse {
		t.Errorf("conditions = %+v", pf.Status.Conditions)
	}
	dep.Status.ReadyReplicas = 1
	if err := e.k.Status().Update(t.Context(), &dep); err != nil {
		t.Fatal(err)
	}
	if c := portForwardCond(e.reconcile(t), CondReady); c.Status != metav1.ConditionTrue || c.Reason != "Ready" {
		t.Errorf("ready = %+v", c)
	}
}

func TestPortForwardWaitsUntilIdentityFileExists(t *testing.T) {
	e := setupPortForward(t, false)
	pf := e.reconcile(t)
	if c := portForwardCond(pf, CondReady); c.Status != metav1.ConditionFalse || c.Reason != "IdentityNotEnrolled" {
		t.Errorf("ready = %+v", c)
	}
	var dep appsv1.Deployment
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "team-a", Name: "billing"}, &dep); err == nil {
		t.Error("no proxy Deployment before identity enrollment")
	}
}

func TestPortForwardRemovesItsWorkloadWhenIdentityStopsBeingReady(t *testing.T) {
	e := setupPortForward(t, true)
	e.reconcile(t)
	var id zitiv1.ZitiIdentity
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "team-a", Name: "client"}, &id); err != nil {
		t.Fatal(err)
	}
	id.Status.Conditions = []metav1.Condition{{Type: CondReady, Status: metav1.ConditionFalse, Reason: "Expired"}}
	if err := e.k.Status().Update(t.Context(), &id); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	var dep appsv1.Deployment
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "team-a", Name: "billing"}, &dep); err == nil {
		t.Error("proxy Deployment must stop when identity is not ready")
	}
}

func TestPortForwardDoesNotTakeOverForeignDeployment(t *testing.T) {
	foreign := &appsv1.Deployment{Name: "billing", Namespace: "team-a"}
	e := setupPortForward(t, true, foreign)
	pf := e.reconcile(t)
	if c := portForwardCond(pf, CondSynced); c.Reason != "NameConflict" {
		t.Errorf("synced = %+v", c)
	}
	var got appsv1.Deployment
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "team-a", Name: "billing"}, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Spec.Template.Spec.Containers) != 0 || len(got.OwnerReferences) != 0 {
		t.Errorf("foreign Deployment changed: %+v", got)
	}
}
