// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"sync"

	"github.com/openziti/edge-api/rest_util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	var ref zitiv1.SecretRef
	switch {
	case conn.Spec.Auth.Cert != nil:
		ref = conn.Spec.Auth.Cert.SecretRef
	case conn.Spec.Auth.Updb != nil:
		ref = conn.Spec.Auth.Updb.SecretRef
	default:
		return nil, fmt.Errorf("connection %q sets neither auth.updb nor auth.cert", conn.Name)
	}
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

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(cm.Data[cmRef.Key])) {
		return nil, fmt.Errorf("CA bundle %s/%s key %q has no PEM certificates", cmRef.Namespace, cmRef.Name, cmRef.Key)
	}
	var auth rest_util.Authenticator
	if conn.Spec.Auth.Cert != nil {
		a, err := certAuthenticator(&secret, ref)
		if err != nil {
			return nil, err
		}
		a.RootCas = pool
		auth = a
	} else {
		user, pass := string(secret.Data["username"]), string(secret.Data["password"])
		if user == "" || pass == "" {
			return nil, fmt.Errorf("credential secret %s/%s needs keys username and password", ref.Namespace, ref.Name)
		}
		a := rest_util.NewAuthenticatorUpdb(user, pass)
		a.RootCas = pool
		auth = a
	}
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

func certAuthenticator(secret *corev1.Secret, ref zitiv1.SecretRef) (*rest_util.AuthenticatorCert, error) {
	pair, err := tls.X509KeyPair(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil {
		return nil, fmt.Errorf("credential secret %s/%s needs keys %s and %s with a matching certificate and key: %w",
			ref.Namespace, ref.Name, corev1.TLSCertKey, corev1.TLSPrivateKeyKey, err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	return rest_util.NewAuthenticatorCert(leaf, pair.PrivateKey), nil
}

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// namespaceAllowed enforces ZitiConnection.spec.allowedNamespaces. An empty selector allows every namespace.
func namespaceAllowed(ctx context.Context, k client.Reader, conn *zitiv1.ZitiConnection, namespace string) error {
	sel := conn.Spec.AllowedNamespaces
	if sel == nil || (len(sel.MatchLabels) == 0 && len(sel.MatchExpressions) == 0) {
		return nil
	}
	selector, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		return &specError{"InvalidConnection", "allowedNamespaces: " + err.Error()}
	}
	var ns corev1.Namespace
	if err := k.Get(ctx, types.NamespacedName{Name: namespace}, &ns); err != nil {
		return err
	}
	if !selector.Matches(labels.Set(ns.Labels)) {
		return &specError{"NamespaceNotAllowed", fmt.Sprintf("namespace %q is not allowed to use connection %q", namespace, conn.Name)}
	}
	return nil
}
