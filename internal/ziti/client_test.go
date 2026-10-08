// SPDX-License-Identifier: Apache-2.0

package ziti

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openziti/edge-api/rest_model"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/aliAljaffer/openziti-operator/internal/metrics"
)

type stubAuth struct {
	hc     *http.Client
	logins atomic.Int32
}

func (a *stubAuth) Authenticate(*url.URL) (*rest_model.CurrentAPISessionDetail, error) {
	tok := "t" + string('0'+a.logins.Add(1))
	return &rest_model.CurrentAPISessionDetail{Token: &tok}, nil
}
func (a *stubAuth) BuildHttpClient() (*http.Client, error)           { return a.hc, nil }
func (a *stubAuth) SetInfo(*rest_model.EnvInfo, *rest_model.SdkInfo) {}

const identityJSON = `{"data":[{"id":"i1","name":"u","enrollment":{"ott":{"jwt":"SECRET-JWT","token":"SECRET-TOKEN","expiresAt":"2030-01-01T00:00:00Z"}}}],"meta":{"pagination":{"totalCount":1}}}`

func TestRESTStripsEnrollmentSecretsAndRenewsSession(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("zt-session") == "t1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(identityJSON))
	}))
	defer srv.Close()

	auth := &stubAuth{hc: srv.Client()}
	c, err := NewREST(srv.URL+"/edge/management/v1", auth, 100)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := c.List(t.Context(), Identities, "")
	if err != nil {
		t.Fatal(err)
	}
	if auth.logins.Load() != 2 {
		t.Errorf("logins = %d, want 2", auth.logins.Load())
	}
	for _, s := range []string{"SECRET-JWT", "SECRET-TOKEN"} {
		if strings.Contains(stringify(ids), s) {
			t.Errorf("%s reached the caller", s)
		}
	}
	if !strings.Contains(stringify(ids), "expiresAt") {
		t.Error("expiresAt was stripped")
	}
}

func TestRESTErrorMapping(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"COULD_NOT_VALIDATE","message":"bad"}}`))
	}))
	defer srv.Close()
	c, _ := NewREST(srv.URL, &stubAuth{hc: srv.Client()}, 100)
	_, err := c.Create(t.Context(), Services, Entity{"name": "x"})
	if !IsSpecError(err) {
		t.Fatalf("want spec error, got %v", err)
	}
}

func TestFakeFilterAndStrip(t *testing.T) {
	f := NewFake()
	f.Put(Configs, Entity{"name": "a", "tags": map[string]any{"ziti-operator-uid": "u1"}})
	f.Put(Configs, Entity{"name": "b", "tags": map[string]any{"ziti-operator-uid": "u2"}})
	f.Put(Identities, Entity{"name": "i", "enrollment": map[string]any{"ott": map[string]any{"jwt": "SECRET-JWT"}}})

	got, err := f.List(t.Context(), Configs, `tags.ziti-operator-uid="u1"`)
	if err != nil || len(got) != 1 || got[0].Name() != "a" {
		t.Fatalf("tag filter: %v %v", got, err)
	}
	got, _ = f.List(t.Context(), Configs, `name in ["a","b"] and tags.ziti-operator-uid="u2"`)
	if len(got) != 1 || got[0].Name() != "b" {
		t.Fatalf("in filter: %v", got)
	}
	ids, _ := f.List(t.Context(), Identities, "")
	if strings.Contains(stringify(ids), "SECRET-JWT") {
		t.Error("fake leaked jwt")
	}
	if _, err := f.Create(t.Context(), Configs, Entity{"name": "a"}); !IsSpecError(err) {
		t.Errorf("duplicate name: %v", err)
	}
}

func stringify(v []Entity) string {
	var b strings.Builder
	for _, e := range v {
		b.WriteString(string(mustJSON(e)))
	}
	return b.String()
}

func TestMetricsCountRequestsByKindAndCode(t *testing.T) {
	if kindOf("services/abc123") != "services" || kindOf("/service-policies") != "service-policies" || kindOf("version") != "version" {
		t.Error("kindOf must keep only the first path segment")
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusConflict)
			return
		}
		_, _ = w.Write([]byte(`{"data":[],"meta":{"pagination":{"totalCount":0}}}`))
	}))
	defer srv.Close()
	c, err := NewREST(srv.URL+"/edge/management/v1", &stubAuth{hc: srv.Client()}, 100)
	if err != nil {
		t.Fatal(err)
	}
	ok := metrics.APIRequests.WithLabelValues("GET", "terminators", "200")
	conflict := metrics.APIRequests.WithLabelValues("DELETE", "terminators", "409")
	okBefore, conflictBefore := testutil.ToFloat64(ok), testutil.ToFloat64(conflict)
	_, _ = c.List(t.Context(), Terminators, "")
	_ = c.Delete(t.Context(), Terminators, "x")
	if testutil.ToFloat64(ok)-okBefore != 1 || testutil.ToFloat64(conflict)-conflictBefore != 1 {
		t.Errorf("counters: get200 +%v, delete409 +%v", testutil.ToFloat64(ok)-okBefore, testutil.ToFloat64(conflict)-conflictBefore)
	}
}

func TestRESTRouterJWTIsOnlyAvailableThroughEnrollment(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/re-enroll"):
			_, _ = w.Write([]byte(`{"data":{}}`))
		case strings.HasSuffix(r.URL.Path, "/edge-routers/r1"):
			_, _ = w.Write([]byte(`{"data":{"id":"r1","enrollmentJwt":"ROUTER-JWT","enrollmentToken":"ROUTER-TOKEN","enrollmentExpiresAt":"2030-01-01T00:00:00Z"}}`))
		default:
			_, _ = w.Write([]byte(`{"data":[{"id":"r1","name":"r","enrollmentJwt":"ROUTER-JWT","enrollmentToken":"ROUTER-TOKEN","isVerified":false}],"meta":{"pagination":{"totalCount":1}}}`))
		}
	}))
	defer srv.Close()
	c, err := NewREST(srv.URL+"/edge/management/v1", &stubAuth{hc: srv.Client()}, 100)
	if err != nil {
		t.Fatal(err)
	}
	list, err := c.List(t.Context(), EdgeRouters, "")
	if err != nil {
		t.Fatal(err)
	}
	if s := stringify(list); strings.Contains(s, "ROUTER-JWT") || strings.Contains(s, "ROUTER-TOKEN") {
		t.Errorf("the enrollment secrets reached the caller: %s", s)
	}
	if list[0]["isVerified"] != false {
		t.Error("other fields must stay")
	}
	jwt, exp, err := c.Enrollment(t.Context(), EdgeRouters, "r1")
	if err != nil || jwt != "ROUTER-JWT" || exp.Year() != 2030 {
		t.Errorf("Enrollment = %q %v %v", jwt, exp, err)
	}
	if err := c.ReEnroll(t.Context(), EdgeRouters, "r1"); err != nil {
		t.Errorf("ReEnroll: %v", err)
	}
}
