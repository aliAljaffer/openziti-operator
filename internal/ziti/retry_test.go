// SPDX-License-Identifier: Apache-2.0

package ziti

import (
	"net/http"
	"testing"
	"time"
)

func TestRetryable(t *testing.T) {
	cases := []struct {
		method string
		status int
		want   bool
	}{
		{http.MethodGet, 503, true}, {http.MethodPost, 503, true}, {http.MethodPost, 429, true},
		{http.MethodGet, 502, true}, {http.MethodPost, 502, false}, {http.MethodGet, 404, false}, {http.MethodGet, 200, false},
	}
	for _, c := range cases {
		if got := retryable(c.method, c.status, nil); got != c.want {
			t.Errorf("%s %d: got %v", c.method, c.status, got)
		}
	}
	if retryable(http.MethodPost, 0, http.ErrHandlerTimeout) || !retryable(http.MethodPut, 0, http.ErrHandlerTimeout) {
		t.Error("network errors retry only for idempotent methods")
	}
	for a := range 10 {
		if d := retryDelay(a); d <= 0 || d > retryCap+time.Nanosecond {
			t.Errorf("attempt %d: delay %v out of range", a, d)
		}
	}
}
