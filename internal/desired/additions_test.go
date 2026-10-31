// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func TestHostedByAnyOfBindsEveryRouter(t *testing.T) {
	b := builder(func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) {
		s.Spec.HostedBy = "r-main"
		s.Spec.HostedByAnyOf = []string{"r-admin"}
	})
	bind, err := b.Bind("svc-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := bind["identityRoles"]; !reflect.DeepEqual(got, []string{"@id-main", "@id-admin"}) {
		t.Errorf("identityRoles = %v", got)
	}
}

func TestDialIncludesScopedPostureRoles(t *testing.T) {
	b := builder(func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) {
		s.Spec.Allow = zitiv1.Allow{Groups: []string{"staff"}, PostureCheckRoles: []string{"#managed"}}
	})
	dial, err := b.Dial("svc-1", []string{"#team-a.staff"})
	if err != nil {
		t.Fatal(err)
	}
	if got := dial["postureCheckRoles"]; !reflect.DeepEqual(got, []string{"#team-a.managed"}) {
		t.Errorf("postureCheckRoles = %v", got)
	}
}

func TestServiceOptionsAndAdditionalConfigs(t *testing.T) {
	b := builder(func(s *zitiv1.ZitiApp, _ *zitiv1.ZitiConnection) {
		enc := false
		s.Spec.Service = zitiv1.AppService{EncryptionRequired: &enc, TerminatorStrategy: "random", MaxIdleTimeMillis: 60}
	})
	svc, err := b.ServiceWithConfigs("ic-1", "hc-1", []string{"extra-1"})
	if err != nil {
		t.Fatal(err)
	}
	if svc["encryptionRequired"] != false || svc["terminatorStrategy"] != "random" || svc["maxIdleTimeMillis"] != int64(60) {
		t.Errorf("service options = %v", svc)
	}
	if got := svc["configs"]; !reflect.DeepEqual(got, []string{"ic-1", "hc-1", "extra-1"}) {
		t.Errorf("configs = %v", got)
	}
}

func TestAuthPolicyBuildsZitiBody(t *testing.T) {
	allowed, updb := true, false
	policy := &zitiv1.ZitiAuthPolicy{
		Name: "humans", Namespace: "team-a",
		Spec: zitiv1.ZitiAuthPolicySpec{
			EntitySpec: zitiv1.EntitySpec{ZitiName: "humans"},
			Primary: zitiv1.AuthPolicyPrimary{
				Cert:   zitiv1.AuthPolicyCert{Allowed: &allowed},
				Updb:   zitiv1.AuthPolicyUpdb{Allowed: &updb},
				ExtJWT: zitiv1.AuthPolicyExtJWT{Allowed: &allowed, AllowedSigners: []string{"k8s"}},
			},
			Secondary: zitiv1.AuthPolicySecondary{RequireTotp: true},
		},
	}
	body, err := AuthPolicyEntity(policy, builder(nil).Conn, map[string]string{"k8s": "signer-1"})
	if err != nil {
		t.Fatal(err)
	}
	primary := body["primary"].(map[string]any)
	ext := primary["extJwt"].(map[string]any)
	if !reflect.DeepEqual(ext["allowedSigners"], []string{"signer-1"}) {
		t.Errorf("allowedSigners = %v", ext["allowedSigners"])
	}
	if body["secondary"].(map[string]any)["requireTotp"] != true {
		t.Errorf("requireTotp = %v", body["secondary"])
	}
}

func TestPostureCheckBuildsTypeAndScopesAttributes(t *testing.T) {
	check := &zitiv1.ZitiPostureCheck{
		Name: "managed", Namespace: "team-a",
		Spec: zitiv1.ZitiPostureCheckSpec{
			EntitySpec:     zitiv1.EntitySpec{ZitiName: "managed"},
			Type:           "OS",
			RoleAttributes: []string{"managed"},
		},
	}
	check.Spec.Data.Raw = []byte(`{"operatingSystems":[{"type":"Windows","versions":["10"]}]}`)
	body, err := PostureCheckEntity(check, builder(nil).Conn)
	if err != nil {
		t.Fatal(err)
	}
	if body["typeId"] != "OS" || !reflect.DeepEqual(body["roleAttributes"], []string{"team-a.managed"}) {
		t.Errorf("body = %v", body)
	}
	if body["operatingSystems"] == nil {
		t.Errorf("data missing: %v", body)
	}
}

func TestGroupMembershipUsesNamesAndLabels(t *testing.T) {
	group := &zitiv1.ZitiGroup{
		Name: "platform", Namespace: "team-a",
		Spec: zitiv1.ZitiGroupSpec{
			Members:  []string{"alice"},
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "platform"}},
		},
	}
	ids := []zitiv1.ZitiIdentity{
		{Name: "alice", Namespace: "team-a"},
		{Name: "bob", Namespace: "team-a", Labels: map[string]string{"team": "platform"}},
		{Name: "carol", Namespace: "team-a"},
	}
	if got := GroupMembers(group, ids); !reflect.DeepEqual(got, []string{"alice", "bob"}) {
		t.Errorf("members = %v", got)
	}
	if got := GroupRoleAttribute(group); got != "group.platform" {
		t.Errorf("role = %q", got)
	}
}
