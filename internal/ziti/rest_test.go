// SPDX-License-Identifier: Apache-2.0

package ziti

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

type seen struct {
	method, path, contentType, session, body string
}

func newTestREST(t *testing.T, h http.HandlerFunc) (*REST, *[]seen) {
	t.Helper()
	var log []seen
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		log = append(log, seen{r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("zt-session"), string(b)})
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	c, err := NewREST(srv.URL+"/edge/management/v1", &stubAuth{hc: srv.Client()}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return c, &log
}

func TestRESTWriteVerbs(t *testing.T) {
	c, log := newTestREST(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":"new-1"}}`))
	})
	ctx := t.Context()

	if id, err := c.Create(ctx, Services, Entity{"name": "s"}); err != nil || id != "new-1" {
		t.Fatalf("create = %q, %v", id, err)
	}
	if err := c.Update(ctx, Services, "s1", Entity{"name": "s"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Patch(ctx, Services, "s1", Entity{"tags": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(ctx, CertificateAuthorities, "ca1", "-----BEGIN CERTIFICATE-----"); err != nil {
		t.Fatal(err)
	}
	if err := c.ReEnroll(ctx, EdgeRouters, "r1"); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, Services, "s1"); err != nil {
		t.Fatal(err)
	}

	want := []struct{ method, path, contentType, body string }{
		{"POST", "/edge/management/v1/services", "application/json", `{"name":"s"}`},
		{"PUT", "/edge/management/v1/services/s1", "application/json", `{"name":"s"}`},
		{"PATCH", "/edge/management/v1/services/s1", "application/json", `{"tags":{}}`},
		{"POST", "/edge/management/v1/cas/ca1/verify", "text/plain", "-----BEGIN CERTIFICATE-----"},
		{"POST", "/edge/management/v1/edge-routers/r1/re-enroll", "application/json", `{}`},
		{"DELETE", "/edge/management/v1/services/s1", "application/json", ""},
	}
	if len(*log) != len(want) {
		t.Fatalf("requests = %d, want %d", len(*log), len(want))
	}
	for i, w := range want {
		g := (*log)[i]
		if g.method != w.method || g.path != w.path || g.contentType != w.contentType || g.body != w.body {
			t.Errorf("request %d = %+v, want %+v", i, g, w)
		}
		if g.session == "" {
			t.Errorf("request %d has no session header", i)
		}
	}
}

func TestRESTCreateWithoutIDFails(t *testing.T) {
	c, _ := newTestREST(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"data":{}}`)) })
	if _, err := c.Create(t.Context(), Services, Entity{}); err == nil {
		t.Error("want error when the response has no id")
	}
}

func TestRESTDeleteIgnoresNotFoundOnly(t *testing.T) {
	status := http.StatusNotFound
	c, _ := newTestREST(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
	if err := c.Delete(t.Context(), Services, "gone"); err != nil {
		t.Errorf("404 must count as deleted: %v", err)
	}
	status = http.StatusConflict
	if err := c.Delete(t.Context(), Services, "busy"); err == nil {
		t.Error("409 must surface")
	}
}

func TestRESTEnrollment(t *testing.T) {
	c, _ := newTestREST(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"enrollmentJwt":"JWT","enrollmentExpiresAt":"2030-01-02T03:04:05Z"}}`))
	})
	jwt, exp, err := c.Enrollment(t.Context(), EdgeRouters, "r1")
	if err != nil || jwt != "JWT" || !exp.Equal(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("got %q, %v, %v", jwt, exp, err)
	}

	c, _ = newTestREST(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"data":{}}`)) })
	if jwt, exp, err = c.Enrollment(t.Context(), EdgeRouters, "r1"); err != nil || jwt != "" || !exp.IsZero() {
		t.Errorf("no enrollment: %q, %v, %v", jwt, exp, err)
	}
}

func TestRESTVersion(t *testing.T) {
	c, log := newTestREST(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"version":"v2.0.4"}}`))
	})
	if v, err := c.Version(t.Context()); err != nil || v != "v2.0.4" {
		t.Errorf("got %q, %v", v, err)
	}
	if (*log)[0].path != "/edge/management/v1/version" {
		t.Errorf("path = %s", (*log)[0].path)
	}
}

func TestRESTListPaginatesAndFilters(t *testing.T) {
	total := pageSize + 3
	c, log := newTestREST(t, func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		n := min(pageSize, total-offset)
		items := make([]Entity, n)
		for i := range items {
			items[i] = Entity{"id": fmt.Sprint(offset + i)}
		}
		b, _ := json.Marshal(map[string]any{"data": items, "meta": map[string]any{"pagination": map[string]any{"totalCount": total}}})
		_, _ = w.Write(b)
	})
	got, err := c.List(t.Context(), Services, `tags.x="y"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != total || len(*log) != 2 {
		t.Errorf("items = %d, requests = %d", len(got), len(*log))
	}
}

func TestRESTListBadBody(t *testing.T) {
	c, _ := newTestREST(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"data":"nope"}`)) })
	if _, err := c.List(t.Context(), Services, ""); err == nil {
		t.Error("want decode error")
	}
}

func TestAPIErrorFormatsCauseAndClassifies(t *testing.T) {
	c, _ := newTestREST(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"COULD_NOT_VALIDATE","message":"bad","cause":{"field":"name"}}}`))
	})
	_, err := c.Create(t.Context(), Services, Entity{})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 400 || ae.Code != "COULD_NOT_VALIDATE" {
		t.Fatalf("err = %v", err)
	}
	if want := `ziti api 400 COULD_NOT_VALIDATE: bad: {"field":"name"}`; ae.Error() != want {
		t.Errorf("message = %q, want %q", ae.Error(), want)
	}
	if !IsSpecError(err) {
		t.Error("400 is a spec error")
	}
}

func TestErrorClassification(t *testing.T) {
	for status, wantSpec := range map[int]bool{400: true, 404: true, 409: true, 401: false, 429: false, 500: false, 503: false} {
		err := &APIError{Status: status}
		if IsSpecError(err) != wantSpec {
			t.Errorf("IsSpecError(%d) = %v, want %v", status, !wantSpec, wantSpec)
		}
		if IsNotFound(err) != (status == 404) {
			t.Errorf("IsNotFound(%d)", status)
		}
	}
	if IsSpecError(errors.New("x")) || IsNotFound(nil) {
		t.Error("non-API errors are neither")
	}
}

func TestRESTSecondUnauthorizedSurfaces(t *testing.T) {
	c, log := newTestREST(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	_, err := c.Version(t.Context())
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 401 {
		t.Fatalf("err = %v", err)
	}
	if len(*log) != 2 {
		t.Errorf("requests = %d, want one retry only", len(*log))
	}
}
