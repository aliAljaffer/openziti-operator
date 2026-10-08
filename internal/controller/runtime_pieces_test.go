// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func TestJitterStaysWithinTenPercentAndNeverPanics(t *testing.T) {
	for _, d := range []time.Duration{0, 1, 9, 10, time.Minute} {
		got := jitter(d)
		if got < d || got > d+d/10 {
			t.Errorf("jitter(%v) = %v", d, got)
		}
	}
}

func TestSweeperStartSweepsThenStopsOnCancel(t *testing.T) {
	s, zc := sweepSetup(t, OrphanDelete)
	s.Interval = time.Hour
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()

	deadline := time.After(5 * time.Second)
	for {
		if _, gone := zc.Objects["services"]["orphan"]; !gone {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the first sweep did not delete the orphan")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
	if !s.NeedLeaderElection() {
		t.Error("the sweeper must run on the leader only")
	}
}

func TestConditionCollectorCountsByKindTypeAndStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	ready := func(s metav1.ConditionStatus) []metav1.Condition {
		return []metav1.Condition{{Type: "Ready", Status: s}}
	}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&zitiv1.ZitiApp{Name: "a", Namespace: "n", Status: zitiv1.ZitiAppStatus{Conditions: ready(metav1.ConditionTrue)}},
		&zitiv1.ZitiApp{Name: "b", Namespace: "n", Status: zitiv1.ZitiAppStatus{Conditions: ready(metav1.ConditionTrue)}},
		&zitiv1.ZitiApp{Name: "c", Namespace: "n", Status: zitiv1.ZitiAppStatus{Conditions: ready(metav1.ConditionFalse)}},
		&zitiv1.ZitiConnection{Name: "default", Status: zitiv1.ZitiConnectionStatus{Conditions: ready(metav1.ConditionTrue)}},
	).Build()

	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(&ConditionCollector{Reader: k})
	want := `# HELP ziti_operator_resource_conditions Custom resources by kind, condition type, and condition status.
# TYPE ziti_operator_resource_conditions gauge
ziti_operator_resource_conditions{kind="ZitiApp",status="False",type="Ready"} 1
ziti_operator_resource_conditions{kind="ZitiApp",status="True",type="Ready"} 2
ziti_operator_resource_conditions{kind="ZitiConnection",status="True",type="Ready"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want)); err != nil {
		t.Error(err)
	}
}

func TestClusterKeysFetch(t *testing.T) {
	status := http.StatusOK
	discovery := `{"issuer":"https://kubernetes.default.svc"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_, _ = w.Write([]byte(discovery))
		case "/openid/v1/jwks":
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"keys":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	keys, err := NewClusterKeys(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	issuer, jwks, err := keys.Fetch(t.Context())
	if err != nil || issuer != "https://kubernetes.default.svc" || string(jwks) != `{"keys":[]}` {
		t.Fatalf("got %q, %s, %v", issuer, jwks, err)
	}

	status = http.StatusForbidden
	if _, _, err = keys.Fetch(t.Context()); err == nil || !strings.Contains(err.Error(), "JWKS") {
		t.Errorf("jwks failure: %v", err)
	}

	status, discovery = http.StatusOK, `{"issuer":""}`
	if _, _, err = keys.Fetch(t.Context()); err == nil || !strings.Contains(err.Error(), "no issuer") {
		t.Errorf("missing issuer: %v", err)
	}
	discovery = `not json`
	if _, _, err = keys.Fetch(t.Context()); err == nil {
		t.Error("bad discovery document must fail")
	}

	dead, err := NewClusterKeys(&rest.Config{Host: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = dead.Fetch(t.Context()); err == nil || !strings.Contains(err.Error(), "discovery") {
		t.Errorf("unreachable server: %v", err)
	}
}
