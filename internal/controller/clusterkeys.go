// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ClusterKeys gives the token issuer and the signing keys of the Kubernetes cluster.
type ClusterKeys interface {
	Fetch(ctx context.Context) (issuer string, jwks []byte, err error)
}

// +kubebuilder:rbac:urls=/openid/v1/jwks;/.well-known/openid-configuration,verbs=get

type discoveryKeys struct{ rc rest.Interface }

// NewClusterKeys reads the OIDC discovery document and the JWKS from the API server.
func NewClusterKeys(cfg *rest.Config) (ClusterKeys, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &discoveryKeys{rc: cs.Discovery().RESTClient()}, nil
}

func (d *discoveryKeys) Fetch(ctx context.Context) (string, []byte, error) {
	doc, err := d.rc.Get().AbsPath("/.well-known/openid-configuration").DoRaw(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("read the OIDC discovery document: %w", err)
	}
	var cfg struct {
		Issuer string `json:"issuer"`
	}
	if err := json.Unmarshal(doc, &cfg); err != nil || cfg.Issuer == "" {
		return "", nil, fmt.Errorf("the OIDC discovery document has no issuer")
	}
	jwks, err := d.rc.Get().AbsPath("/openid/v1/jwks").DoRaw(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("read the JWKS: %w", err)
	}
	return cfg.Issuer, jwks, nil
}
