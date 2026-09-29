// SPDX-License-Identifier: Apache-2.0

package ziti

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/openziti/edge-api/rest_util"
	"golang.org/x/time/rate"
)

type Kind string

const (
	Configs                   Kind = "configs"
	ConfigTypes               Kind = "config-types"
	Services                  Kind = "services"
	ServicePolicies           Kind = "service-policies"
	ServiceEdgeRouterPolicies Kind = "service-edge-router-policies"
	EdgeRouterPolicies        Kind = "edge-router-policies"
	EdgeRouters               Kind = "edge-routers"
	Identities                Kind = "identities"
	Terminators               Kind = "terminators"
	Enrollments               Kind = "enrollments"
	AuthPolicies              Kind = "auth-policies"
	Authenticators            Kind = "authenticators"
)

type Entity map[string]any

func (e Entity) ID() string   { s, _ := e["id"].(string); return s }
func (e Entity) Name() string { s, _ := e["name"].(string); return s }

func (e Entity) Tags() map[string]any {
	t, _ := e["tags"].(map[string]any)
	return t
}

type Client interface {
	Version(ctx context.Context) (string, error)
	List(ctx context.Context, kind Kind, filter string) ([]Entity, error)
	Create(ctx context.Context, kind Kind, body Entity) (string, error)
	Update(ctx context.Context, kind Kind, id string, body Entity) error
	// Patch changes only the fields in body. A "tags" field replaces the whole tag map.
	Patch(ctx context.Context, kind Kind, id string, body Entity) error
	Delete(ctx context.Context, kind Kind, id string) error
}

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ziti api %d %s: %s", e.Status, e.Code, e.Message)
}

func IsSpecError(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500 &&
		ae.Status != http.StatusUnauthorized && ae.Status != http.StatusTooManyRequests
}

func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

type REST struct {
	base    *url.URL
	auth    rest_util.Authenticator
	http    *http.Client
	limiter *rate.Limiter

	mu    sync.Mutex
	token string
}

var _ Client = (*REST)(nil)

func NewREST(managementURL string, auth rest_util.Authenticator, requestsPerSecond float64) (*REST, error) {
	u, err := url.Parse(managementURL)
	if err != nil {
		return nil, err
	}
	hc, err := auth.BuildHttpClient()
	if err != nil {
		return nil, err
	}
	return &REST{base: u, auth: auth, http: hc, limiter: rate.NewLimiter(rate.Limit(requestsPerSecond), 1)}, nil
}

func (c *REST) session(renew bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && !renew {
		return c.token, nil
	}
	s, err := c.auth.Authenticate(c.base)
	if err != nil {
		return "", fmt.Errorf("ziti login: %w", err)
	}
	if s.Token == nil || *s.Token == "" {
		return "", errors.New("ziti login: empty session token")
	}
	c.token = *s.Token
	return c.token, nil
}

type envelope struct {
	Data json.RawMessage `json:"data"`
	Meta struct {
		Pagination struct {
			TotalCount int `json:"totalCount"`
		} `json:"pagination"`
	} `json:"meta"`
	Error *struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Cause   json.RawMessage `json:"cause"`
	} `json:"error"`
}

func (c *REST) do(ctx context.Context, method, path string, query url.Values, body any) (*envelope, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	u := c.base.JoinPath(path)
	u.RawQuery = query.Encode()

	for attempt := 0; ; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		token, err := c.session(attempt > 0)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("zt-session", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			continue
		}
		var env envelope
		_ = json.Unmarshal(raw, &env)
		if resp.StatusCode >= 300 {
			ae := &APIError{Status: resp.StatusCode}
			if env.Error != nil {
				ae.Code, ae.Message = env.Error.Code, env.Error.Message
				if len(env.Error.Cause) > 0 && string(env.Error.Cause) != "null" {
					ae.Message += ": " + string(env.Error.Cause)
				}
			}
			return nil, ae
		}
		return &env, nil
	}
}

const pageSize = 500

func (c *REST) List(ctx context.Context, kind Kind, filter string) ([]Entity, error) {
	var all []Entity
	for offset := 0; ; offset += pageSize {
		q := url.Values{"limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)}}
		if filter != "" {
			q.Set("filter", filter)
		}
		env, err := c.do(ctx, http.MethodGet, string(kind), q, nil)
		if err != nil {
			return nil, err
		}
		var page []Entity
		if err := json.Unmarshal(env.Data, &page); err != nil {
			return nil, fmt.Errorf("decode %s: %w", kind, err)
		}
		all = append(all, page...)
		if len(page) < pageSize || len(all) >= env.Meta.Pagination.TotalCount {
			break
		}
	}
	if kind == Identities {
		for _, e := range all {
			stripEnrollmentSecrets(e)
		}
	}
	return all, nil
}

func stripEnrollmentSecrets(e Entity) {
	enr, _ := e["enrollment"].(map[string]any)
	for _, v := range enr {
		if m, ok := v.(map[string]any); ok {
			delete(m, "jwt")
			delete(m, "token")
		}
	}
}

func (c *REST) Create(ctx context.Context, kind Kind, body Entity) (string, error) {
	env, err := c.do(ctx, http.MethodPost, string(kind), nil, body)
	if err != nil {
		return "", err
	}
	var d struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil || d.ID == "" {
		return "", fmt.Errorf("create %s: no id in response", kind)
	}
	return d.ID, nil
}

// Update replaces the entity. Fields missing from body, including tags, are cleared.
func (c *REST) Update(ctx context.Context, kind Kind, id string, body Entity) error {
	_, err := c.do(ctx, http.MethodPut, string(kind)+"/"+id, nil, body)
	return err
}

func (c *REST) Patch(ctx context.Context, kind Kind, id string, body Entity) error {
	_, err := c.do(ctx, http.MethodPatch, string(kind)+"/"+id, nil, body)
	return err
}

func (c *REST) Delete(ctx context.Context, kind Kind, id string) error {
	_, err := c.do(ctx, http.MethodDelete, string(kind)+"/"+id, nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}

func (c *REST) Version(ctx context.Context) (string, error) {
	env, err := c.do(ctx, http.MethodGet, "version", nil, nil)
	if err != nil {
		return "", err
	}
	var d struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		return "", err
	}
	return d.Version, nil
}
