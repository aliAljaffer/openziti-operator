// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

// call sends one admission request through the webhook and returns the response.
func call(t *testing.T, wh *admission.Webhook, op admissionv1.Operation, obj runtime.Object) admission.Response {
	t.Helper()
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: op,
		Object:    runtime.RawExtension{Raw: raw},
	}}
	if op == admissionv1.Update {
		req.OldObject = runtime.RawExtension{Raw: raw}
	}
	return wh.Handle(context.Background(), req)
}

func deniedWith(t *testing.T, resp admission.Response, want string) {
	t.Helper()
	if resp.Allowed {
		t.Fatalf("expected a denial containing %q, got allowed", want)
	}
	if resp.Result == nil || !strings.Contains(resp.Result.Message, want) {
		t.Fatalf("expected a denial containing %q, got %+v", want, resp.Result)
	}
}

func conn(name string) *zitiv1.ZitiConnection {
	return &zitiv1.ZitiConnection{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: zitiv1.ZitiConnectionSpec{
			ManagementURL:  "https://controller.example:443/edge/management/v1",
			HostingRouters: []string{"router-a"},
			EntryRouters:   []string{"router-a"},
		},
	}
}

func app(name string) *zitiv1.ZitiApp {
	return &zitiv1.ZitiApp{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		Spec: zitiv1.ZitiAppSpec{
			Expose:  zitiv1.Expose{Addresses: []string{"web.ns.svc"}, Ports: []intstr.IntOrString{intstr.FromInt(80)}},
			Targets: []zitiv1.Target{{Address: "web.ns.svc", Port: 8080}},
		},
	}
}

func TestZitiAppValidator(t *testing.T) {
	scheme := testScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(conn("default")).Build()
	wh := admission.WithValidator(scheme, &ZitiAppValidator{Client: client})

	if resp := call(t, wh, admissionv1.Create, app("good")); !resp.Allowed {
		t.Fatalf("valid app denied: %+v", resp.Result)
	}

	quote := app("bad")
	quote.Spec.ZitiName = `has"quote`
	deniedWith(t, call(t, wh, admissionv1.Create, quote), "quotes or backslashes")

	noTargets := app("bad")
	noTargets.Spec.Targets = nil
	deniedWith(t, call(t, wh, admissionv1.Create, noTargets), "targets needs at least one")

	badPort := app("bad")
	badPort.Spec.Expose.Ports = []intstr.IntOrString{intstr.FromString("70000")}
	deniedWith(t, call(t, wh, admissionv1.Create, badPort), "must be 1-65535")

	unserved := app("bad")
	unserved.Spec.Expose.Ports = []intstr.IntOrString{intstr.FromInt(80), intstr.FromInt(81)}
	unserved.Spec.Targets = []zitiv1.Target{{Address: "web.ns.svc", ExposedPorts: []intstr.IntOrString{intstr.FromInt(80)}}}
	deniedWith(t, call(t, wh, admissionv1.Create, unserved), "not served by any target")

	otherRouter := app("bad")
	otherRouter.Spec.HostedBy = "router-z"
	deniedWith(t, call(t, wh, admissionv1.Create, otherRouter), "not in the connection hostingRouters")

	if resp := call(t, wh, admissionv1.Update, app("good")); !resp.Allowed {
		t.Fatalf("valid update denied: %+v", resp.Result)
	}
}

func TestZitiAppValidatorWithoutConnection(t *testing.T) {
	scheme := testScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	wh := admission.WithValidator(scheme, &ZitiAppValidator{Client: client})

	resp := call(t, wh, admissionv1.Create, app("good"))
	if !resp.Allowed {
		t.Fatalf("app denied while the connection is missing: %+v", resp.Result)
	}
	found := false
	for _, w := range resp.Warnings {
		if strings.Contains(w, "does not exist yet") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a warning about the missing connection, got %v", resp.Warnings)
	}
}

func TestZitiAccessPolicyValidator(t *testing.T) {
	scheme := testScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(conn("default")).Build()
	wh := admission.WithValidator(scheme, &ZitiAccessPolicyValidator{Client: client})

	valid := &zitiv1.ZitiAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "ap", Namespace: "ns"},
		Spec: zitiv1.ZitiAccessPolicySpec{
			IdentityRoles: []string{"#developers"},
			ServiceRoles:  []string{"#apps"},
		},
	}
	if resp := call(t, wh, admissionv1.Create, valid); !resp.Allowed {
		t.Fatalf("valid policy denied: %+v", resp.Result)
	}

	all := valid.DeepCopy()
	all.Spec.IdentityRoles = []string{"#all"}
	deniedWith(t, call(t, wh, admissionv1.Create, all), "not allowed with roleScope Namespaced")

	id := valid.DeepCopy()
	id.Spec.IdentityRoles = []string{"@alice"}
	deniedWith(t, call(t, wh, admissionv1.Create, id), "not allowed with roleScope Namespaced")
}

func TestZitiIdentityValidator(t *testing.T) {
	scheme := testScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(conn("default")).Build()
	wh := admission.WithValidator(scheme, &ZitiIdentityValidator{Client: client})

	valid := &zitiv1.ZitiIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "id", Namespace: "ns"},
		Spec:       zitiv1.ZitiIdentitySpec{RoleAttributes: []string{"web"}},
	}
	if resp := call(t, wh, admissionv1.Create, valid); !resp.Allowed {
		t.Fatalf("valid identity denied: %+v", resp.Result)
	}

	ext := valid.DeepCopy()
	ext.Spec.ExternalID = "any-subject"
	deniedWith(t, call(t, wh, admissionv1.Create, ext), "roleScope Global")
}

func TestZitiConnectionValidator(t *testing.T) {
	scheme := testScheme(t)
	wh := admission.WithValidator(scheme, &ZitiConnectionValidator{})

	if resp := call(t, wh, admissionv1.Create, conn("default")); !resp.Allowed {
		t.Fatalf("valid connection denied: %+v", resp.Result)
	}

	bad := conn("default")
	bad.Spec.AllowedNamespaces = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
		{Key: "tier", Operator: "NotAThing"},
	}}
	deniedWith(t, call(t, wh, admissionv1.Create, bad), "allowedNamespaces")
}
