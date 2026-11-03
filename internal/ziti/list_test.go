// SPDX-License-Identifier: Apache-2.0

package ziti

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// rendezvous blocks a List until both calls have started, so a sequential ListAll times out.
type rendezvous struct {
	*Fake
	mu    sync.Mutex
	n     int
	ready chan struct{}
}

func (r *rendezvous) List(ctx context.Context, kind Kind, filter string) ([]Entity, error) {
	r.mu.Lock()
	r.n++
	if r.n == 2 {
		close(r.ready)
	}
	r.mu.Unlock()
	select {
	case <-r.ready:
	case <-time.After(2 * time.Second):
		return nil, errors.New("list calls did not run concurrently")
	}
	return r.Fake.List(ctx, kind, filter)
}

func TestListAllRunsConcurrentlyAndKeepsOrder(t *testing.T) {
	f := NewFake()
	f.Put(Services, Entity{"id": "s", "name": "svc"})
	f.Put(Configs, Entity{"id": "c", "name": "cfg"})
	c := &rendezvous{Fake: f, ready: make(chan struct{})}

	lists, err := ListAll(t.Context(), c, []Kind{Configs, Services}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 2 || lists[0][0].ID() != "c" || lists[1][0].ID() != "s" {
		t.Errorf("lists = %v, want configs then services", lists)
	}
}

func TestListAllNamesTheFailingKind(t *testing.T) {
	f := NewFake()
	f.Put(Services, Entity{"id": "s", "name": "svc"})
	_, err := ListAll(t.Context(), f, []Kind{Services}, "not a filter")
	if err == nil || !strings.Contains(err.Error(), "list services") {
		t.Errorf("error = %v, want the kind in the message", err)
	}
}
