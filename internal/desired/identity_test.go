// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"reflect"
	"strings"
	"testing"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func identity() *zitiv1.ZitiIdentity {
	return &zitiv1.ZitiIdentity{ObjectMeta: meta()}
}

func TestIdentityName(t *testing.T) {
	conn := connWith("")
	tests := []struct {
		name string
		mut  func(*zitiv1.ZitiIdentity)
		want string
	}{
		{"new identity uses cluster-namespace-name", func(*zitiv1.ZitiIdentity) {}, "c1-team-a-web"},
		{"spec name wins", func(i *zitiv1.ZitiIdentity) { i.Spec.ZitiName = "spec"; i.Status.ZitiName = "stored" }, "spec"},
		{"stored name kept", func(i *zitiv1.ZitiIdentity) { i.Status.ZitiName = "stored" }, "stored"},
		{"existing without stored name keeps legacy form", func(i *zitiv1.ZitiIdentity) { i.Status.ZitiID = "id-1" }, "team-a.web"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := identity()
			tt.mut(id)
			if got := IdentityName(id, conn); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
	noCluster := connWith("")
	noCluster.Spec.ClusterID = ""
	if got := IdentityName(identity(), noCluster); got != "default-team-a-web" {
		t.Errorf("no clusterId: got %q", got)
	}
}

func TestIdentityEntity(t *testing.T) {
	id := identity()
	id.Spec.RoleAttributes = []string{"dev"}
	id.Spec.ServiceAccount = "sa"

	e, err := Identity(id, connWith(""), "ap-1")
	if err != nil {
		t.Fatal(err)
	}
	if e["type"] != "Default" || e["isAdmin"] != false || e["authPolicyId"] != "ap-1" || e["name"] != "c1-team-a-web" {
		t.Errorf("entity = %v", e)
	}
	if !reflect.DeepEqual(e["roleAttributes"], []string{"team-a.dev"}) {
		t.Errorf("roleAttributes = %v", e["roleAttributes"])
	}
	if e["externalId"] != "system:serviceaccount:team-a:sa" {
		t.Errorf("externalId = %v", e["externalId"])
	}
	assertOwned(t, e, "ZitiIdentity")
	if _, ok := e["enrollment"]; ok {
		t.Error("body must not carry enrollment, a PUT would reset it")
	}

	id.Spec.ServiceAccount = ""
	if e, _ = Identity(id, connWith(""), "ap"); e["externalId"] != nil {
		t.Errorf("externalId must be absent, got %v", e["externalId"])
	}

	id.Spec.RoleAttributes = []string{"all"}
	if _, err := Identity(id, connWith(zitiv1.RoleScopeNamespaced), "ap"); err == nil {
		t.Error("namespaced #all must fail")
	}
	if _, err := Identity(id, connWith(zitiv1.RoleScopeGlobal), "ap"); err != nil {
		t.Errorf("global scope allows it: %v", err)
	}

	id.Spec.RoleAttributes = nil
	id.Spec.ExternalID = "sub-1"
	if _, err := Identity(id, connWith(zitiv1.RoleScopeNamespaced), "ap"); err == nil {
		t.Error("externalId must fail without Global scope")
	}
}

func TestExternalID(t *testing.T) {
	id := identity()
	if got, err := ExternalID(id, connWith("")); got != "" || err != nil {
		t.Errorf("unset = %q, %v", got, err)
	}

	id.Spec.ExternalID = "any-subject"
	if _, err := ExternalID(id, connWith("")); err == nil || !strings.Contains(err.Error(), "roleScope Global") {
		t.Errorf("default scope must reject a free externalId, got %v", err)
	}
	if got, err := ExternalID(id, connWith(zitiv1.RoleScopeGlobal)); got != "any-subject" || err != nil {
		t.Errorf("global = %q, %v", got, err)
	}

	id.Spec.ServiceAccount = "sa"
	if got, err := ExternalID(id, connWith("")); got != "system:serviceaccount:team-a:sa" || err != nil {
		t.Errorf("serviceAccount wins and is safe in every scope: %q, %v", got, err)
	}
}

func TestAdoptTags(t *testing.T) {
	existing := map[string]any{"owner": "ops", TagUID: "stale"}
	got := AdoptTags(connWith(""), identity(), existing)
	if got["owner"] != "ops" {
		t.Error("foreign tags must survive, Ziti replaces the whole map on PATCH")
	}
	if got[TagUID] != "uid-1" || got[TagAdopted] != "true" || got[TagKind] != "ZitiIdentity" {
		t.Errorf("ownership tags missing: %v", got)
	}
	if existing[TagUID] != "stale" || len(existing) != 2 {
		t.Error("input map was modified")
	}
	if got := AdoptTags(connWith(""), identity(), nil); got[TagAdopted] != "true" {
		t.Errorf("nil existing: %v", got)
	}
}

func TestReleaseTags(t *testing.T) {
	got := ReleaseTags(map[string]any{
		"owner": "ops", TagUID: "u", TagAdopted: "true", TagCluster: "c",
	})
	if !reflect.DeepEqual(got, map[string]any{"owner": "ops"}) {
		t.Errorf("got %v", got)
	}
	if got := ReleaseTags(nil); got == nil || len(got) != 0 {
		t.Errorf("nil input must give an empty non-nil map (PATCH needs it to clear tags), got %#v", got)
	}
}
