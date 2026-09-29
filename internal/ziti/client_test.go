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
)

type stubAuth struct {
	hc     *http.Client
	logins atomic.Int32
}

func (a *stubAuth) Authenticate(*url.URL) (*rest_model.CurrentAPISessionDetail, error) {
	tok := "t" + string(rune('0'+a.logins.Add(1)))
	return &rest_model.CurrentAPISessionDetail{APISessionDetail: rest_model.APISessionDetail{Token: &tok}}, nil
}
func (a *stubAuth) BuildHttpClient() (*http.Client, error)         { return a.hc, nil }
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
