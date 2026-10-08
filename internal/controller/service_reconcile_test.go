// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
)

type exposeEnv struct {
	r   *ServiceReconciler
	k   client.Client
	rec *record.FakeRecorder
	key types.NamespacedName
}

func setupExpose(t *testing.T, ann map[string]string, objs ...client.Object) *exposeEnv {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	svc := &corev1.Service{
		Name: "web", Namespace: "team-a", UID: "svc-uid", Annotations: ann,
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}}},
	}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(objs, svc)...).Build()
	rec := record.NewFakeRecorder(10)
	return &exposeEnv{r: &ServiceReconciler{Client: k, Scheme: scheme, Recorder: rec}, k: k, rec: rec, key: types.NamespacedName{Namespace: "team-a", Name: "web"}}
}

func (e *exposeEnv) reconcile(t *testing.T) {
	t.Helper()
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err != nil {
		t.Fatal(err)
	}
}

func (e *exposeEnv) app() (*zitiv1.ZitiApp, error) {
	var a zitiv1.ZitiApp
	return &a, e.k.Get(context.Background(), e.key, &a)
}

func (e *exposeEnv) event() string {
	select {
	case ev := <-e.rec.Events:
		return ev
	default:
		return ""
	}
}

func TestExposeCreatesOwnedAppAndFollowsAnnotations(t *testing.T) {
	e := setupExpose(t, map[string]string{desired.AnnExpose: "true", desired.AnnAllowGroups: "staff"})
	e.reconcile(t)
	app, err := e.app()
	if err != nil {
		t.Fatal(err)
	}
	if !metav1.IsControlledBy(app, &corev1.Service{UID: "svc-uid"}) {
		t.Errorf("owner refs = %v", app.OwnerReferences)
	}
	if app.Spec.Targets[0].Address != "web.team-a.svc" || app.Spec.Allow.Groups[0] != "staff" {
		t.Errorf("spec = %+v", app.Spec)
	}

	var svc corev1.Service
	if err := e.k.Get(t.Context(), e.key, &svc); err != nil {
		t.Fatal(err)
	}
	svc.Annotations[desired.AnnAllowGroups] = "staff,ops"
	if err := e.k.Update(t.Context(), &svc); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	app, _ = e.app()
	if len(app.Spec.Allow.Groups) != 2 {
		t.Errorf("groups = %v", app.Spec.Allow.Groups)
	}

	svc.Annotations[desired.AnnExpose] = "false"
	if err := e.k.Update(t.Context(), &svc); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	if _, err := e.app(); !errors.IsNotFound(err) {
		t.Errorf("ZitiApp must be deleted when expose is removed: %v", err)
	}
}

func TestExposeNeverTouchesForeignApp(t *testing.T) {
	foreign := &zitiv1.ZitiApp{Name: "web", Namespace: "team-a", Spec: zitiv1.ZitiAppSpec{ZitiName: "hand-made"}}
	e := setupExpose(t, map[string]string{desired.AnnExpose: "true"}, foreign)
	e.reconcile(t)
	app, _ := e.app()
	if app.Spec.ZitiName != "hand-made" || len(app.Spec.Targets) != 0 {
		t.Errorf("foreign app changed: %+v", app.Spec)
	}
	if ev := e.event(); !strings.Contains(ev, "AppConflict") {
		t.Errorf("event = %q", ev)
	}

	var svc corev1.Service
	_ = e.k.Get(t.Context(), e.key, &svc)
	svc.Annotations[desired.AnnExpose] = "false"
	_ = e.k.Update(t.Context(), &svc)
	e.reconcile(t)
	if _, err := e.app(); err != nil {
		t.Error("foreign app was deleted")
	}
}

func TestExposeInvalidAnnotationsOnlyRaiseAnEvent(t *testing.T) {
	e := setupExpose(t, map[string]string{desired.AnnExpose: "true", desired.AnnPorts: "9000-8000"})
	e.reconcile(t)
	if _, err := e.app(); !errors.IsNotFound(err) {
		t.Error("an invalid Service must not create an app")
	}
	if ev := e.event(); !strings.Contains(ev, "InvalidAnnotations") {
		t.Errorf("event = %q", ev)
	}
}

func TestExposeIgnoresPlainService(t *testing.T) {
	e := setupExpose(t, nil)
	e.reconcile(t)
	if _, err := e.app(); !errors.IsNotFound(err) {
		t.Error("plain Service created an app")
	}
}
