// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func TestCA(t *testing.T) {
	ca := &zitiv1.ZitiCA{ObjectMeta: meta()}
	if got := CAName(ca); got != "web" {
		t.Errorf("name = %q", got)
	}
	e := CA(ca, connWith(""), "PEM")
	if e["certPem"] != "PEM" || e["isAuthEnabled"] != true || e["isAutoCaEnrollmentEnabled"] != false ||
		e["isOttCaEnrollmentEnabled"] != false || e["identityNameFormat"] != "[caName]-[commonName]" ||
		!reflect.DeepEqual(e["identityRoles"], []string{}) {
		t.Errorf("defaults = %v", e)
	}
	claim := e["externalIdClaim"].(map[string]any)
	if claim["location"] != "COMMON_NAME" || claim["matcher"] != "ALL" || claim["parser"] != "NONE" {
		t.Errorf("claim defaults = %v", claim)
	}
	assertOwned(t, e, "ZitiCA")

	f := false
	ca.Spec.ZitiName = "custom"
	ca.Spec.AuthEnabled = &f
	ca.Spec.AutoEnrollment = &zitiv1.AutoEnrollment{IdentityRoles: []string{"r"}, IdentityNameFormat: "[commonName]"}
	ca.Spec.ExternalIDClaim = zitiv1.ExternalIDClaim{Location: "SAN_EMAIL", Matcher: "PREFIX", MatcherCriteria: "a", Parser: "SPLIT", ParserCriteria: "-", Index: 2}
	e = CA(ca, connWith(""), "PEM")
	if e["name"] != "custom" || e["isAuthEnabled"] != false || e["isAutoCaEnrollmentEnabled"] != true ||
		e["identityNameFormat"] != "[commonName]" || !reflect.DeepEqual(e["identityRoles"], []string{"r"}) {
		t.Errorf("overrides = %v", e)
	}
	want := map[string]any{"location": "SAN_EMAIL", "matcher": "PREFIX", "matcherCriteria": "a", "parser": "SPLIT", "parserCriteria": "-", "index": int32(2)}
	if !reflect.DeepEqual(e["externalIdClaim"], want) {
		t.Errorf("claim = %v", e["externalIdClaim"])
	}

	ca.Spec.AutoEnrollment = &zitiv1.AutoEnrollment{}
	if e = CA(ca, connWith(""), "PEM"); e["identityNameFormat"] != "[caName]-[commonName]" {
		t.Errorf("empty format must fall back, got %v", e["identityNameFormat"])
	}
}

func TestWorkloadCertificate(t *testing.T) {
	id := identity()
	id.Spec.ExternalID = "cn-1"
	id.Spec.Certificate = &zitiv1.WorkloadCertificate{IssuerRef: zitiv1.CertificateIssuerRef{Name: "iss"}}
	if got := WorkloadCertificateName(id); got != "web" {
		t.Errorf("name = %q", got)
	}
	u := WorkloadCertificate(id)
	if u.GetAPIVersion() != "cert-manager.io/v1" || u.GetKind() != "Certificate" || u.GetNamespace() != "team-a" || u.GetName() != "web" {
		t.Errorf("object = %v", u.Object)
	}
	spec := u.Object["spec"].(map[string]any)
	ref := spec["issuerRef"].(map[string]any)
	if spec["commonName"] != "cn-1" || spec["secretName"] != "web" || ref["kind"] != "ClusterIssuer" || ref["name"] != "iss" || ref["group"] != "cert-manager.io" {
		t.Errorf("spec = %v", spec)
	}
	if _, ok := spec["duration"]; ok {
		t.Error("duration must be absent when unset")
	}

	id.Spec.SecretName = "tls"
	id.Spec.Certificate.IssuerRef.Kind = "Issuer"
	id.Spec.Certificate.Duration = &metav1.Duration{Duration: 2 * time.Hour}
	spec = WorkloadCertificate(id).Object["spec"].(map[string]any)
	if spec["secretName"] != "tls" || spec["duration"] != "2h0m0s" || spec["issuerRef"].(map[string]any)["kind"] != "Issuer" {
		t.Errorf("overrides = %v", spec)
	}
}

func TestCAProofCertificate(t *testing.T) {
	ca := &zitiv1.ZitiCA{ObjectMeta: meta()}
	ca.Spec.Verification.IssuerRef = &zitiv1.VerificationIssuer{Name: "iss", Namespace: "cert-ns"}
	if got := CAProofCertificateName(ca); got != "web-verify" {
		t.Errorf("name = %q", got)
	}
	u := CAProofCertificate(ca, "tok")
	if u.GetNamespace() != "cert-ns" || u.GetName() != "web-verify" || u.GetKind() != "Certificate" {
		t.Errorf("object = %v", u.Object)
	}
	spec := u.Object["spec"].(map[string]any)
	if spec["commonName"] != "tok" || spec["duration"] != "1h" || spec["secretName"] != "web-verify" ||
		spec["issuerRef"].(map[string]any)["kind"] != "ClusterIssuer" {
		t.Errorf("spec = %v", spec)
	}
	ca.Spec.Verification.IssuerRef.Kind = "Issuer"
	if k := CAProofCertificate(ca, "tok").Object["spec"].(map[string]any)["issuerRef"].(map[string]any)["kind"]; k != "Issuer" {
		t.Errorf("kind = %v", k)
	}
}

func TestRouterName(t *testing.T) {
	conn := connWith("")
	tests := []struct {
		name string
		mut  func(*zitiv1.ZitiRouter)
		want string
	}{
		{"new router", func(*zitiv1.ZitiRouter) {}, "c1-edge"},
		{"spec name wins", func(r *zitiv1.ZitiRouter) { r.Spec.ZitiName = "spec"; r.Status.ZitiName = "st" }, "spec"},
		{"stored name kept", func(r *zitiv1.ZitiRouter) { r.Status.ZitiName = "st" }, "st"},
		{"existing without stored name keeps resource name", func(r *zitiv1.ZitiRouter) { r.Status.RouterID = "id" }, "edge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &zitiv1.ZitiRouter{ObjectMeta: metav1.ObjectMeta{Name: "edge", UID: "u"}}
			tt.mut(r)
			if got := RouterName(r, conn); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRouterEntity(t *testing.T) {
	r := &zitiv1.ZitiRouter{ObjectMeta: metav1.ObjectMeta{Name: "edge", UID: "u"}, Spec: zitiv1.ZitiRouterSpec{
		RoleAttributes: []string{"public"}, TunnelerEnabled: true, Cost: 5, NoTraversal: true, Disabled: true,
	}}
	e := Router(r, connWith(""))
	if e["name"] != "c1-edge" || !reflect.DeepEqual(e["roleAttributes"], []string{"public"}) || e["isTunnelerEnabled"] != true ||
		e["cost"] != int32(5) || e["noTraversal"] != true || e["disabled"] != true {
		t.Errorf("entity = %v", e)
	}
	e["roleAttributes"].([]string)[0] = "changed"
	if r.Spec.RoleAttributes[0] != "public" {
		t.Error("roleAttributes must be a copy of the spec slice")
	}
	r.Spec.RoleAttributes = nil
	if got := Router(r, connWith(""))["roleAttributes"]; !reflect.DeepEqual(got, []string{}) {
		t.Errorf("nil attributes must become an empty list, got %#v", got)
	}
	tags := Router(r, connWith(""))["tags"].(map[string]any)
	if tags[TagKind] != "ZitiRouter" || tags[TagUID] != "u" {
		t.Errorf("tags = %v", tags)
	}
}

func TestAccessPolicies(t *testing.T) {
	ap := &zitiv1.ZitiAccessPolicy{ObjectMeta: meta(), Spec: zitiv1.ZitiAccessPolicySpec{
		IdentityRoles: []string{"#dev"}, ServiceRoles: []string{"#web"},
	}}
	if got := AccessName(ap); got != "team-a.web" {
		t.Errorf("name = %q", got)
	}
	ap.Spec.ZitiName = "custom"
	if got := AccessName(ap); got != "custom" {
		t.Errorf("name = %q", got)
	}

	e, err := AccessDial(ap, connWith(""))
	if err != nil {
		t.Fatal(err)
	}
	if e["name"] != "custom.dial" || e["type"] != "Dial" || e["semantic"] != "AnyOf" ||
		!reflect.DeepEqual(e["identityRoles"], []string{"#team-a.dev"}) || !reflect.DeepEqual(e["serviceRoles"], []string{"#team-a.web"}) ||
		!reflect.DeepEqual(e["postureCheckRoles"], []string{}) {
		t.Errorf("dial = %v", e)
	}
	assertOwned(t, e, "ZitiAccessPolicy")

	ap.Spec.ServiceRoles = []string{"#all"}
	if _, err := AccessDial(ap, connWith(zitiv1.RoleScopeNamespaced)); err == nil {
		t.Error("namespaced #all in serviceRoles must fail")
	}
	ap.Spec.ServiceRoles, ap.Spec.IdentityRoles = []string{"#web"}, []string{"#all"}
	if _, err := AccessDial(ap, connWith(zitiv1.RoleScopeNamespaced)); err == nil {
		t.Error("namespaced #all in identityRoles must fail")
	}
	ap.Spec.IdentityRoles, ap.Spec.PostureCheckRoles = []string{"#dev"}, []string{"#all"}
	if _, err := AccessDial(ap, connWith(zitiv1.RoleScopeNamespaced)); err == nil {
		t.Error("namespaced #all in postureCheckRoles must fail")
	}
}

func TestAccessERP(t *testing.T) {
	ap := &zitiv1.ZitiAccessPolicy{ObjectMeta: meta(), Spec: zitiv1.ZitiAccessPolicySpec{IdentityRoles: []string{"#dev"}}}
	conn := connWith("")
	if e, err := AccessERP(ap, conn, nil); e != nil || err != nil {
		t.Errorf("no edgeRouters must give nil, nil: %v, %v", e, err)
	}

	ap.Spec.EdgeRouters = []string{"r1"}
	e, err := AccessERP(ap, conn, map[string]string{"r1": "id-1"})
	if err != nil {
		t.Fatal(err)
	}
	if e["name"] != "team-a.web.erp" || !reflect.DeepEqual(e["edgeRouterRoles"], []string{"@id-1"}) ||
		!reflect.DeepEqual(e["identityRoles"], []string{"#team-a.dev"}) {
		t.Errorf("erp = %v", e)
	}

	if _, err := AccessERP(ap, conn, map[string]string{}); err == nil {
		t.Error("router unknown to Ziti must fail")
	}

	restricted := connWith("")
	restricted.Spec.EntryRouters = []string{"other"}
	if _, err := AccessERP(ap, restricted, map[string]string{"r1": "id-1"}); err == nil {
		t.Error("router outside entryRouters must fail")
	}

	ap.Spec.IdentityRoles = []string{"#all"}
	if _, err := AccessERP(ap, connWith(zitiv1.RoleScopeNamespaced), map[string]string{"r1": "id-1"}); err == nil {
		t.Error("namespaced #all must fail")
	}
}
