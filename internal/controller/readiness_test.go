// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func TestZitiReadiness(t *testing.T) {
	connected := func(name string, status metav1.ConditionStatus) *zitiv1.ZitiConnection {
		conn := &zitiv1.ZitiConnection{
			Status: zitiv1.ZitiConnectionStatus{Conditions: []metav1.Condition{{
				Type: ConditionConnected, Status: status,
			}}},
		}
		conn.Name = name
		return conn
	}

	for _, tc := range []struct {
		name    string
		objects []client.Object
		wantErr string
	}{
		{"no connection yet", nil, ""},
		{"connected", []client.Object{connected("default", metav1.ConditionTrue)}, ""},
		{"one of two connected", []client.Object{
			connected("default", metav1.ConditionFalse),
			connected("other", metav1.ConditionTrue),
		}, ""},
		{"none connected", []client.Object{connected("default", metav1.ConditionFalse)}, "default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := zitiv1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objects...).Build()
			check := &ZitiReadiness{Reader: k}

			err := check.Check(httptest.NewRequest("GET", "/readyz/ziti", nil))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Check() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Check() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
