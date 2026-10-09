// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

type ingressEnv struct {
	r   *IngressReconciler
	k   client.Client
	rec *record.FakeRecorder
	key types.NamespacedName
}

func setupIngress(t *testing.T, ann map[string]string, extra ...client.Object) *ingressEnv {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{zitiv1.AddToScheme, corev1.AddToScheme, networkingv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	pathType := networkingv1.PathTypePrefix
	ing := &networkingv1.Ingress{Name: "billing", Namespace: "team-a", UID: "ing-uid", Annotations: ann,
		Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "billing.example.com",
			HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Path: "/", PathType: &pathType,
				Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "billing", Port: networkingv1.ServiceBackendPort{Number: 8080}}},
			}}}}}}}
	backend := &corev1.Service{Name: "billing", Namespace: "team-a", Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "web", Port: 8080}}}}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(extra, ing, backend)...).Build()
	rec := record.NewFakeRecorder(10)
	return &ingressEnv{r: &IngressReconciler{Client: k, Scheme: scheme, Recorder: rec}, k: k, rec: rec,
		key: types.NamespacedName{Namespace: "team-a", Name: "billing"}}
}

func (e *ingressEnv) reconcile(t *testing.T) {
	t.Helper()
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err != nil {
		t.Fatal(err)
	}
}

func (e *ingressEnv) app() (*zitiv1.ZitiApp, error) {
	var app zitiv1.ZitiApp
	key := types.NamespacedName{Namespace: "team-a", Name: "ingress-billing"}
	return &app, e.k.Get(context.Background(), key, &app)
}

func (e *ingressEnv) event() string {
	select {
	case ev := <-e.rec.Events:
		return ev
	default:
		return ""
	}
}

func TestIngressCreatesOwnedAppForItsHostAndBackend(t *testing.T) {
	e := setupIngress(t, map[string]string{desired.AnnExpose: "true", desired.AnnAllowGroups: "staff"})
	e.reconcile(t)
	app, err := e.app()
	if err != nil {
		t.Fatal(err)
	}
	var ing networkingv1.Ingress
	if err := e.k.Get(t.Context(), e.key, &ing); err != nil {
		t.Fatal(err)
	}
	if !metav1.IsControlledBy(app, &ing) {
		t.Errorf("owner refs = %v", app.OwnerReferences)
	}
	if app.Spec.Targets[0].KubernetesService != "billing" || app.Spec.Targets[0].Port != 8080 ||
		len(app.Spec.Expose.Addresses) != 1 || app.Spec.Expose.Addresses[0] != "billing.example.com" || app.Spec.Allow.Groups[0] != "staff" {
		t.Errorf("spec = %+v", app.Spec)
	}

	ing.Annotations[desired.AnnAllowGroups] = "staff,ops"
	if err := e.k.Update(t.Context(), &ing); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	app, _ = e.app()
	if len(app.Spec.Allow.Groups) != 2 {
		t.Errorf("groups = %v", app.Spec.Allow.Groups)
	}

	ing.Annotations[desired.AnnExpose] = "false"
	if err := e.k.Update(t.Context(), &ing); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	if _, err := e.app(); !apierrors.IsNotFound(err) {
		t.Errorf("ZitiApp must be deleted when expose is removed: %v", err)
	}
}

func TestIngressNeverTouchesForeignApp(t *testing.T) {
	foreign := &zitiv1.ZitiApp{Name: "ingress-billing", Namespace: "team-a", Spec: zitiv1.ZitiAppSpec{ZitiName: "hand-made"}}
	e := setupIngress(t, map[string]string{desired.AnnExpose: "true"}, foreign)
	e.reconcile(t)
	app, _ := e.app()
	if app.Spec.ZitiName != "hand-made" || len(app.Spec.Targets) != 0 {
		t.Errorf("foreign app changed: %+v", app.Spec)
	}
	if ev := e.event(); !strings.Contains(ev, "AppConflict") {
		t.Errorf("event = %q", ev)
	}
}

func TestIngressInvalidRoutesOnlyRaiseAnEvent(t *testing.T) {
	e := setupIngress(t, map[string]string{desired.AnnExpose: "true"})
	var ing networkingv1.Ingress
	if err := e.k.Get(t.Context(), e.key, &ing); err != nil {
		t.Fatal(err)
	}
	pathType := networkingv1.PathTypePrefix
	other := networkingv1.HTTPIngressPath{Path: "/other", PathType: &pathType,
		Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "other", Port: networkingv1.ServiceBackendPort{Number: 8080}}}}
	ing.Spec.Rules[0].HTTP.Paths = append(ing.Spec.Rules[0].HTTP.Paths, other)
	if err := e.k.Update(t.Context(), &ing); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	if _, err := e.app(); !apierrors.IsNotFound(err) {
		t.Error("ambiguous Ingress must not create an app")
	}
	if ev := e.event(); !strings.Contains(ev, "InvalidAnnotations") || !strings.Contains(ev, "same Service") {
		t.Errorf("event = %q", ev)
	}
}

func TestIngressTLSNeedsAnExplicitZitiApp(t *testing.T) {
	e := setupIngress(t, map[string]string{desired.AnnExpose: "true"})
	var ing networkingv1.Ingress
	if err := e.k.Get(t.Context(), e.key, &ing); err != nil {
		t.Fatal(err)
	}
	ing.Spec.TLS = []networkingv1.IngressTLS{{Hosts: []string{"billing.example.com"}, SecretName: "tls"}}
	if err := e.k.Update(t.Context(), &ing); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	if _, err := e.app(); !apierrors.IsNotFound(err) {
		t.Error("TLS Ingress must not create an app with different termination semantics")
	}
	if ev := e.event(); !strings.Contains(ev, "TLS Ingresses") {
		t.Errorf("event = %q", ev)
	}
}
