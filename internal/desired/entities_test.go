// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func connWith(scope zitiv1.RoleScope) *zitiv1.ZitiConnection {
	return &zitiv1.ZitiConnection{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec:       zitiv1.ZitiConnectionSpec{RoleScope: scope, ClusterID: "c1"},
	}
}

func meta() metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: "web", Namespace: "team-a", UID: "uid-1"}
}

func assertOwned(t *testing.T, e ziti.Entity, kind string) {
	t.Helper()
	tags, ok := e["tags"].(map[string]any)
	if !ok {
		t.Fatalf("tags missing or wrong type: %#v", e["tags"])
	}
	want := map[string]any{
		TagCluster: "c1", TagKind: kind, TagNamespace: "team-a", TagName: "web", TagUID: "uid-1",
	}
	if !reflect.DeepEqual(tags, want) {
		t.Errorf("tags = %v, want %v", tags, want)
	}
}

func TestEntityName(t *testing.T) {
	m := meta()
	if got := EntityName(&zitiv1.EntitySpec{}, &m); got != "team-a.web" {
		t.Errorf("default = %q", got)
	}
	if got := EntityName(&zitiv1.EntitySpec{ZitiName: "custom"}, &m); got != "custom" {
		t.Errorf("custom = %q", got)
	}
}

func TestMissingErrorMessage(t *testing.T) {
	var err error = &MissingError{Msg: "boom"}
	var me *MissingError
	if !errors.As(err, &me) || err.Error() != "boom" {
		t.Errorf("got %v", err)
	}
}

func TestResolveRoles(t *testing.T) {
	lookup := func(kind ziti.Kind, name string) (string, bool) {
		if name == "known" {
			return "id-" + string(kind), true
		}
		return "", false
	}
	tests := []struct {
		name    string
		scope   zitiv1.RoleScope
		roles   []string
		want    []string
		wantErr string
		missing bool
	}{
		{name: "namespaced scopes attributes", scope: zitiv1.RoleScopeNamespaced, roles: []string{"#web"}, want: []string{"#team-a.web"}},
		{name: "default scope is namespaced", scope: "", roles: []string{"#web"}, want: []string{"#team-a.web"}},
		{name: "namespaced rejects #all", scope: zitiv1.RoleScopeNamespaced, roles: []string{"#all"}, wantErr: "not allowed"},
		{name: "namespaced rejects @name", scope: zitiv1.RoleScopeNamespaced, roles: []string{"@known"}, wantErr: "not allowed"},
		{name: "global passes attributes", scope: zitiv1.RoleScopeGlobal, roles: []string{"#web", "#all"}, want: []string{"#web", "#all"}},
		{name: "global resolves names to ids", scope: zitiv1.RoleScopeGlobal, roles: []string{"@known"}, want: []string{"@id-" + string(ziti.Services)}},
		{name: "global unknown name is MissingError", scope: zitiv1.RoleScopeGlobal, roles: []string{"@nope"}, wantErr: "no service named", missing: true},
		{name: "no prefix", scope: zitiv1.RoleScopeGlobal, roles: []string{"web"}, wantErr: "must start with"},
		{name: "empty role", scope: zitiv1.RoleScopeGlobal, roles: []string{"#"}, wantErr: "is empty"},
		{name: "empty list", scope: zitiv1.RoleScopeNamespaced, roles: nil, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveRoles(connWith(tt.scope), "team-a", "serviceRoles", tt.roles, ziti.Services, lookup)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), "serviceRoles") {
					t.Errorf("error does not name the field: %v", err)
				}
				var me *MissingError
				if errors.As(err, &me) != tt.missing {
					t.Errorf("MissingError = %v, want %v", errors.As(err, &me), tt.missing)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigEntity(t *testing.T) {
	conn := connWith("")
	o := &zitiv1.ZitiConfig{ObjectMeta: meta(), Spec: zitiv1.ZitiConfigSpec{
		Data: apiextensionsv1.JSON{Raw: []byte(`{"address":"a.example.com","port":443}`)},
	}}
	e, err := ConfigEntity(o, conn, "type-1")
	if err != nil {
		t.Fatal(err)
	}
	if e["name"] != "team-a.web" || e["configTypeId"] != "type-1" {
		t.Errorf("entity = %v", e)
	}
	data := e["data"].(map[string]any)
	if data["address"] != "a.example.com" {
		t.Errorf("data = %v", data)
	}
	assertOwned(t, e, "ZitiConfig")

	for _, raw := range []string{`null`, `"str"`, `[1]`, ``} {
		o.Spec.Data.Raw = []byte(raw)
		if _, err := ConfigEntity(o, conn, "t"); err == nil {
			t.Errorf("data %q: want error", raw)
		}
	}
}

func TestServiceEntity(t *testing.T) {
	f := false
	o := &zitiv1.ZitiService{ObjectMeta: meta(), Spec: zitiv1.ZitiServiceSpec{RoleAttributes: []string{"web"}}}

	e, err := ServiceEntity(o, connWith(zitiv1.RoleScopeNamespaced), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(e["roleAttributes"], []string{"team-a.web"}) {
		t.Errorf("roleAttributes = %v", e["roleAttributes"])
	}
	if !reflect.DeepEqual(e["configs"], []string{}) {
		t.Errorf("nil configs must become an empty list, got %#v", e["configs"])
	}
	if e["terminatorStrategy"] != "smartrouting" || e["encryptionRequired"] != true {
		t.Errorf("defaults wrong: %v", e)
	}
	assertOwned(t, e, "ZitiService")

	o.Spec.TerminatorStrategy = "weighted"
	o.Spec.EncryptionRequired = &f
	o.Spec.MaxIdleTimeMillis = 5000
	e, err = ServiceEntity(o, connWith(zitiv1.RoleScopeGlobal), []string{"cfg-1"})
	if err != nil {
		t.Fatal(err)
	}
	if e["terminatorStrategy"] != "weighted" || e["encryptionRequired"] != false || e["maxIdleTimeMillis"] != int64(5000) {
		t.Errorf("overrides lost: %v", e)
	}
	if !reflect.DeepEqual(e["roleAttributes"], []string{"web"}) || !reflect.DeepEqual(e["configs"], []string{"cfg-1"}) {
		t.Errorf("global scope entity = %v", e)
	}

	o.Spec.RoleAttributes = []string{"all"}
	if _, err := ServiceEntity(o, connWith(zitiv1.RoleScopeNamespaced), nil); err == nil || !strings.Contains(err.Error(), "roleAttributes") {
		t.Errorf("namespaced #all must fail naming roleAttributes, got %v", err)
	}
}

func TestPolicyEntities(t *testing.T) {
	conn := connWith("")

	sp := &zitiv1.ZitiServicePolicy{ObjectMeta: meta(), Spec: zitiv1.ZitiServicePolicySpec{Type: "Dial"}}
	e := ServicePolicyEntity(sp, conn, []string{"#i"}, []string{"#s"}, nil)
	if e["type"] != "Dial" || e["semantic"] != "AnyOf" || !reflect.DeepEqual(e["postureCheckRoles"], []string{}) {
		t.Errorf("service policy defaults: %v", e)
	}
	assertOwned(t, e, "ZitiServicePolicy")
	sp.Spec.Semantic = "AllOf"
	if got := ServicePolicyEntity(sp, conn, nil, nil, []string{"#p"}); got["semantic"] != "AllOf" || !reflect.DeepEqual(got["postureCheckRoles"], []string{"#p"}) {
		t.Errorf("service policy overrides: %v", got)
	}

	erp := &zitiv1.ZitiEdgeRouterPolicy{ObjectMeta: meta()}
	e = EdgeRouterPolicyEntity(erp, conn, []string{"#i"}, []string{"#r"})
	if e["semantic"] != "AnyOf" || !reflect.DeepEqual(e["edgeRouterRoles"], []string{"#r"}) || !reflect.DeepEqual(e["identityRoles"], []string{"#i"}) {
		t.Errorf("edge router policy: %v", e)
	}
	assertOwned(t, e, "ZitiEdgeRouterPolicy")

	serp := &zitiv1.ZitiServiceEdgeRouterPolicy{ObjectMeta: meta()}
	e = ServiceEdgeRouterPolicyEntity(serp, conn, []string{"#s"}, []string{"#r"})
	if e["semantic"] != "AnyOf" || !reflect.DeepEqual(e["serviceRoles"], []string{"#s"}) || !reflect.DeepEqual(e["edgeRouterRoles"], []string{"#r"}) {
		t.Errorf("service edge router policy: %v", e)
	}
	assertOwned(t, e, "ZitiServiceEdgeRouterPolicy")
}

func TestTerminatorEntity(t *testing.T) {
	o := &zitiv1.ZitiTerminator{ObjectMeta: meta(), Spec: zitiv1.ZitiTerminatorSpec{Address: "tcp:10.0.0.5:80", Cost: 3}}
	e := TerminatorEntity(o, connWith(""), "svc-1", "rtr-1")
	if e["service"] != "svc-1" || e["router"] != "rtr-1" || e["binding"] != "transport" || e["precedence"] != "default" ||
		e["address"] != "tcp:10.0.0.5:80" || e["cost"] != int32(3) {
		t.Errorf("entity = %v", e)
	}
	assertOwned(t, e, "ZitiTerminator")

	o.Spec.Binding, o.Spec.Precedence = "edge", "required"
	e = TerminatorEntity(o, connWith(""), "s", "r")
	if e["binding"] != "edge" || e["precedence"] != "required" {
		t.Errorf("overrides lost: %v", e)
	}
}
