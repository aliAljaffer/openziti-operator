// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"reflect"
	"testing"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func signer() *zitiv1.ZitiJwtSigner {
	return &zitiv1.ZitiJwtSigner{ObjectMeta: meta(), Spec: zitiv1.ZitiJwtSignerSpec{Audience: "ziti"}}
}

func TestSignerName(t *testing.T) {
	s := signer()
	if got := SignerName(s); got != "web" {
		t.Errorf("default = %q", got)
	}
	s.Spec.ZitiName = "custom"
	if got := SignerName(s); got != "custom" {
		t.Errorf("custom = %q", got)
	}
}

func TestBoolOrTrue(t *testing.T) {
	f, tr := false, true
	if !boolOrTrue(nil) || !boolOrTrue(&tr) || boolOrTrue(&f) {
		t.Error("nil and true give true, false gives false")
	}
}

func TestSignerWantsPolicy(t *testing.T) {
	s := signer()
	if !SignerWantsPolicy(s) {
		t.Error("default must be true")
	}
	f := false
	s.Spec.CreateAuthPolicy = &f
	if SignerWantsPolicy(s) {
		t.Error("explicit false must win")
	}
}

func TestJwtSigner(t *testing.T) {
	s := signer()
	e := JwtSigner(s, connWith(""), "https://issuer", "kid-1", "PEM")
	if e["name"] != "web" || e["enabled"] != true || e["issuer"] != "https://issuer" || e["audience"] != "ziti" ||
		e["claimsProperty"] != "sub" || e["useExternalId"] != true {
		t.Errorf("entity = %v", e)
	}
	if e["certPem"] != "PEM" || e["kid"] != "kid-1" {
		t.Errorf("static key fields missing: %v", e)
	}
	if _, ok := e["jwksEndpoint"]; ok {
		t.Error("jwksEndpoint must be absent with a static key")
	}
	assertOwned(t, e, "ZitiJwtSigner")

	f := false
	s.Spec.ClaimsProperty = "email"
	s.Spec.UseExternalID = &f
	s.Spec.Keys.JwksEndpoint = &zitiv1.JwksEndpoint{URL: "https://issuer/jwks"}
	e = JwtSigner(s, connWith(""), "https://issuer", "ignored", "ignored")
	if e["jwksEndpoint"] != "https://issuer/jwks" {
		t.Errorf("jwksEndpoint = %v", e["jwksEndpoint"])
	}
	if _, ok := e["certPem"]; ok {
		t.Error("certPem must be absent with a jwks endpoint")
	}
	if _, ok := e["kid"]; ok {
		t.Error("kid must be absent with a jwks endpoint")
	}
	if e["claimsProperty"] != "email" || e["useExternalId"] != false {
		t.Errorf("overrides lost: %v", e)
	}
}

func TestSignerAuthPolicyAcceptsOnlyTheSigner(t *testing.T) {
	e := SignerAuthPolicy(signer(), connWith(""), "signer-1")
	primary := e["primary"].(map[string]any)
	if primary["cert"].(map[string]any)["allowed"] != false {
		t.Error("cert auth must be off")
	}
	if primary["updb"].(map[string]any)["allowed"] != false {
		t.Error("updb auth must be off")
	}
	ext := primary["extJwt"].(map[string]any)
	if ext["allowed"] != true || !reflect.DeepEqual(ext["allowedSigners"], []string{"signer-1"}) {
		t.Errorf("extJwt = %v", ext)
	}
	if e["name"] != "web" {
		t.Errorf("name = %v", e["name"])
	}
	assertOwned(t, e, "ZitiJwtSigner")
}
