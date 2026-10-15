// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func conn(spec zitiv1.ZitiConnectionSpec) *zitiv1.ZitiConnection {
	return &zitiv1.ZitiConnection{ObjectMeta: metav1.ObjectMeta{Name: "default"}, Spec: spec}
}

func namespaceConn() *zitiv1.ZitiConnection {
	return conn(zitiv1.ZitiConnectionSpec{HostingRouters: []string{"router-a"}, EntryRouters: []string{"router-a"}})
}

func app() *zitiv1.ZitiApp {
	return &zitiv1.ZitiApp{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "ns"},
		Spec: zitiv1.ZitiAppSpec{
			Expose:  zitiv1.Expose{Ports: []intstr.IntOrString{intstr.FromInt(80)}},
			Targets: []zitiv1.Target{{Address: "web.ns.svc", Port: 8080}},
		},
	}
}

func TestApp(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*zitiv1.ZitiApp)
		conn    *zitiv1.ZitiConnection
		wantErr string
	}{
		{name: "valid", mutate: func(*zitiv1.ZitiApp) {}, conn: namespaceConn()},
		{name: "valid without connection", mutate: func(*zitiv1.ZitiApp) {}, conn: nil},
		{
			name:    "quote in name",
			mutate:  func(a *zitiv1.ZitiApp) { a.Spec.ZitiName = `has"quote` },
			conn:    namespaceConn(),
			wantErr: "quotes or backslashes",
		},
		{
			name:    "no targets",
			mutate:  func(a *zitiv1.ZitiApp) { a.Spec.Targets = nil },
			conn:    namespaceConn(),
			wantErr: "targets needs at least one",
		},
		{
			name: "unserved port",
			mutate: func(a *zitiv1.ZitiApp) {
				a.Spec.Expose.Ports = []intstr.IntOrString{intstr.FromInt(80), intstr.FromInt(81)}
				a.Spec.Targets = []zitiv1.Target{{Address: "web.ns.svc", ExposedPorts: []intstr.IntOrString{intstr.FromInt(80)}}}
			},
			conn:    namespaceConn(),
			wantErr: "not served by any target",
		},
		{
			name:    "hostedBy not in hostingRouters",
			mutate:  func(a *zitiv1.ZitiApp) { a.Spec.HostedBy = "router-z" },
			conn:    namespaceConn(),
			wantErr: "not in the connection hostingRouters",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := app()
			tc.mutate(a)
			err := App(a, tc.conn)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestAccessPolicy(t *testing.T) {
	valid := func() *zitiv1.ZitiAccessPolicy {
		return &zitiv1.ZitiAccessPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "ap", Namespace: "ns"},
			Spec: zitiv1.ZitiAccessPolicySpec{
				IdentityRoles: []string{"#developers"},
				ServiceRoles:  []string{"#apps"},
			},
		}
	}

	if err := AccessPolicy(valid(), namespaceConn()); err != nil {
		t.Fatalf("valid policy: %v", err)
	}

	all := valid()
	all.Spec.IdentityRoles = []string{"#all"}
	if err := AccessPolicy(all, namespaceConn()); err == nil || !strings.Contains(err.Error(), "roleScope Namespaced") {
		t.Fatalf("want a roleScope Namespaced error, got %v", err)
	}

	id := valid()
	id.Spec.IdentityRoles = []string{"@alice"}
	if err := AccessPolicy(id, namespaceConn()); err == nil || !strings.Contains(err.Error(), "roleScope Namespaced") {
		t.Fatalf("want a roleScope Namespaced error, got %v", err)
	}
}

func TestIdentity(t *testing.T) {
	valid := &zitiv1.ZitiIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "id", Namespace: "ns"},
		Spec:       zitiv1.ZitiIdentitySpec{RoleAttributes: []string{"web"}},
	}
	if err := Identity(valid, namespaceConn()); err != nil {
		t.Fatalf("valid identity: %v", err)
	}

	ext := valid.DeepCopy()
	ext.Spec.ExternalID = "any-subject"
	if err := Identity(ext, namespaceConn()); err == nil || !strings.Contains(err.Error(), "roleScope Global") {
		t.Fatalf("want a roleScope Global error, got %v", err)
	}
}

func TestConnection(t *testing.T) {
	if err := Connection(conn(zitiv1.ZitiConnectionSpec{})); err != nil {
		t.Fatalf("no selector: %v", err)
	}
	bad := conn(zitiv1.ZitiConnectionSpec{AllowedNamespaces: &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "tier", Operator: "NotAThing"}},
	}})
	if err := Connection(bad); err == nil || !strings.Contains(err.Error(), "allowedNamespaces") {
		t.Fatalf("want an allowedNamespaces error, got %v", err)
	}
}
