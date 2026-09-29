// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"crypto/x509"
	"fmt"
	"sync"

	"github.com/openziti/edge-api/rest_util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type ClientProvider interface {
	For(ctx context.Context, conn *zitiv1.ZitiConnection) (ziti.Client, error)
}

type SecretClientProvider struct {
	Reader            client.Reader
	RequestsPerSecond float64

	mu    sync.Mutex
	cache map[string]cachedClient
}

type cachedClient struct {
	key    string
	client ziti.Client
}

var _ ClientProvider = (*SecretClientProvider)(nil)

func (p *SecretClientProvider) For(ctx context.Context, conn *zitiv1.ZitiConnection) (ziti.Client, error) {
	ref := conn.Spec.Auth.Updb.SecretRef
	var secret corev1.Secret
	if err := p.Reader.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &secret); err != nil {
		return nil, fmt.Errorf("credential secret: %w", err)
	}
	cmRef := conn.Spec.CABundle.ConfigMapRef
	var cm corev1.ConfigMap
	if err := p.Reader.Get(ctx, types.NamespacedName{Namespace: cmRef.Namespace, Name: cmRef.Name}, &cm); err != nil {
		return nil, fmt.Errorf("CA bundle configmap: %w", err)
	}

	key := fmt.Sprintf("%s/%d/%s/%s", conn.UID, conn.Generation, secret.ResourceVersion, cm.ResourceVersion)
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.cache[conn.Name]; ok && c.key == key {
		return c.client, nil
	}

	user, pass := string(secret.Data["username"]), string(secret.Data["password"])
	if user == "" || pass == "" {
		return nil, fmt.Errorf("credential secret %s/%s needs keys username and password", ref.Namespace, ref.Name)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(cm.Data[cmRef.Key])) {
		return nil, fmt.Errorf("CA bundle %s/%s key %q has no PEM certificates", cmRef.Namespace, cmRef.Name, cmRef.Key)
	}
	auth := rest_util.NewAuthenticatorUpdb(user, pass)
	auth.RootCas = pool
	c, err := ziti.NewREST(conn.Spec.ManagementURL, auth, p.RequestsPerSecond)
	if err != nil {
		return nil, err
	}
	if p.cache == nil {
		p.cache = map[string]cachedClient{}
	}
	p.cache[conn.Name] = cachedClient{key: key, client: c}
	return c, nil
}
