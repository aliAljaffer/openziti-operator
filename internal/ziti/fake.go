// SPDX-License-Identifier: Apache-2.0

package ziti

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// Fake supports filters of the form `field="v"` and `field in ["a","b"]`, joined by " and ".
// Fields are top-level keys or tags.<key>.
type Fake struct {
	mu      sync.Mutex
	next    int
	Objects map[Kind]map[string]Entity
	Calls   []string
}

var _ Client = (*Fake)(nil)

func NewFake() *Fake {
	return &Fake{Objects: map[Kind]map[string]Entity{}}
}

func (f *Fake) Put(kind Kind, e Entity) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.put(kind, "", clone(e))
}

func (f *Fake) put(kind Kind, id string, e Entity) string {
	if id == "" {
		if id = e.ID(); id == "" {
			f.next++
			id = fmt.Sprintf("%s-%d", kind, f.next)
		}
	}
	e["id"] = id
	if f.Objects[kind] == nil {
		f.Objects[kind] = map[string]Entity{}
	}
	f.Objects[kind][id] = e
	return id
}

var term = regexp.MustCompile(`^\s*([A-Za-z_.-]+)\s*(=|in)\s*(.+?)\s*$`)

func match(e Entity, filter string) (bool, error) {
	if filter == "" {
		return true, nil
	}
	for _, t := range strings.Split(filter, " and ") {
		m := term.FindStringSubmatch(t)
		if m == nil {
			return false, &APIError{Status: http.StatusBadRequest, Code: "INVALID_FILTER", Message: t}
		}
		var want []string
		if m[2] == "=" {
			m[3] = "[" + m[3] + "]"
		}
		if err := json.Unmarshal([]byte(m[3]), &want); err != nil {
			return false, &APIError{Status: http.StatusBadRequest, Code: "INVALID_FILTER", Message: t}
		}
		var got any = e[m[1]]
		if k, ok := strings.CutPrefix(m[1], "tags."); ok {
			got = e.Tags()[k]
		}
		s, _ := got.(string)
		found := false
		for _, w := range want {
			found = found || s == w
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

func (f *Fake) List(_ context.Context, kind Kind, filter string) ([]Entity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "list "+string(kind))
	var out []Entity
	for _, e := range f.Objects[kind] {
		ok, err := match(e, filter)
		if err != nil {
			return nil, err
		}
		if ok {
			c := clone(e)
			if kind == Identities {
				stripEnrollmentSecrets(c)
			}
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *Fake) nameTaken(kind Kind, name, except string) bool {
	for id, e := range f.Objects[kind] {
		if id != except && e.Name() == name {
			return true
		}
	}
	return false
}

func (f *Fake) Create(_ context.Context, kind Kind, body Entity) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "create "+string(kind)+" "+body.Name())
	if f.nameTaken(kind, body.Name(), "") {
		return "", &APIError{Status: http.StatusBadRequest, Code: "COULD_NOT_VALIDATE", Message: "name must be unique"}
	}
	b := clone(body)
	delete(b, "id")
	return f.put(kind, "", b), nil
}

func (f *Fake) Update(_ context.Context, kind Kind, id string, body Entity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "update "+string(kind)+" "+body.Name())
	if _, ok := f.Objects[kind][id]; !ok {
		return &APIError{Status: http.StatusNotFound, Code: "NOT_FOUND"}
	}
	if f.nameTaken(kind, body.Name(), id) {
		return &APIError{Status: http.StatusBadRequest, Code: "COULD_NOT_VALIDATE", Message: "name must be unique"}
	}
	f.put(kind, id, clone(body))
	return nil
}

func (f *Fake) Delete(_ context.Context, kind Kind, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "delete "+string(kind)+" "+id)
	delete(f.Objects[kind], id)
	return nil
}

func clone(e Entity) Entity {
	b, _ := json.Marshal(e)
	var c Entity
	_ = json.Unmarshal(b, &c)
	return c
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
