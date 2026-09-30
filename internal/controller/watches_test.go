// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func TestListRequests(t *testing.T) {
	s := runtime.NewScheme()
	_ = zitiv1.AddToScheme(s)
	app := func(ns, name, ref string) *zitiv1.ZitiApp {
		return &zitiv1.ZitiApp{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}, Spec: zitiv1.ZitiAppSpec{ConnectionRef: ref}}
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(app("a", "x", ""), app("a", "y", "other"), app("b", "z", "")).Build()
	newList := func() client.ObjectList { return &zitiv1.ZitiAppList{} }

	byConn := listRequests(context.Background(), c, newList, func(o *zitiv1.ZitiApp) bool {
		return refersTo(o.Spec.ConnectionRef, "default")
	})
	if len(byConn) != 2 {
		t.Fatalf("connection match: got %v", byConn)
	}
	byNS := listRequests(context.Background(), c, newList, func(*zitiv1.ZitiApp) bool { return true }, client.InNamespace("a"))
	if len(byNS) != 2 {
		t.Fatalf("namespace match: got %v", byNS)
	}
}
