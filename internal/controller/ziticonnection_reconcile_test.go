// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/providerflags"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func newConnectionScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func connectionFlags() providerflags.Flags {
	return providerflags.Flags{
		ManagementURL:     "https://ctrl/edge/management/v1",
		CABundleConfigMap: "ziti-root-ca",
		AuthUpdbSecret:    "ziti-operator-credential",
		HostingRouters:    "router1",
		RoleScope:         "Namespaced",
		ClusterID:         "default",
	}
}

func TestConnectionReconcilerCreatesDefaultFromFlags(t *testing.T) {
	scheme := newConnectionScheme(t)
	k := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&zitiv1.ZitiConnection{}).Build()
	r := &ZitiConnectionReconciler{
		Client: k, Scheme: scheme, Clients: staticProvider{ziti.NewFake()},
		CreateConnection: true, ConnectionFlags: connectionFlags(), Namespace: "ziti-operator-system",
	}

	if _, err := r.Reconcile(t.Context(), ctrl.Request{Name: "default"}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var conn zitiv1.ZitiConnection
	if err := k.Get(t.Context(), types.NamespacedName{Name: "default"}, &conn); err != nil {
		t.Fatalf("the connection was not created: %v", err)
	}
	if conn.Spec.ManagementURL != "https://ctrl/edge/management/v1" {
		t.Errorf("managementUrl = %q", conn.Spec.ManagementURL)
	}
	if conn.Spec.CABundle.ConfigMapRef.Namespace != "ziti-operator-system" {
		t.Errorf("ca bundle namespace = %q", conn.Spec.CABundle.ConfigMapRef.Namespace)
	}
	if conn.Spec.Auth.Updb == nil || conn.Spec.Auth.Updb.SecretRef.Name != "ziti-operator-credential" {
		t.Errorf("auth = %+v", conn.Spec.Auth)
	}
	if _, err := r.Reconcile(t.Context(), ctrl.Request{Name: "default"}); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
}

func TestConnectionReconcilerLeavesAnExistingConnectionAlone(t *testing.T) {
	scheme := newConnectionScheme(t)
	existing := &zitiv1.ZitiConnection{
		Name: "default",
		Spec: zitiv1.ZitiConnectionSpec{ManagementURL: "https://mine/edge/management/v1"},
	}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).WithStatusSubresource(&zitiv1.ZitiConnection{}).Build()
	r := &ZitiConnectionReconciler{
		Client: k, Scheme: scheme, Clients: staticProvider{ziti.NewFake()},
		CreateConnection: true, ConnectionFlags: connectionFlags(), Namespace: "ziti-operator-system",
	}

	if _, err := r.Reconcile(t.Context(), ctrl.Request{Name: "default"}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var conn zitiv1.ZitiConnection
	if err := k.Get(t.Context(), types.NamespacedName{Name: "default"}, &conn); err != nil {
		t.Fatal(err)
	}
	if conn.Spec.ManagementURL != "https://mine/edge/management/v1" {
		t.Errorf("an existing connection was overwritten: %q", conn.Spec.ManagementURL)
	}
}

func TestConnectionReconcilerDoesNotCreateByDefault(t *testing.T) {
	scheme := newConnectionScheme(t)
	k := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&zitiv1.ZitiConnection{}).Build()
	r := &ZitiConnectionReconciler{Client: k, Scheme: scheme, Clients: staticProvider{ziti.NewFake()}}

	if _, err := r.Reconcile(t.Context(), ctrl.Request{Name: "default"}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var list zitiv1.ZitiConnectionList
	if err := k.List(t.Context(), &list, client.InNamespace("")); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Errorf("created %d connections without CreateConnection", len(list.Items))
	}
}
