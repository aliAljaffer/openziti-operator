// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func caPEM(t *testing.T) (string, *ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(der)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), key, cert
}

func clientCertSecret(t *testing.T, ca *ecdsa.PrivateKey, caCert *x509.Certificate) *corev1.Secret {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "operator"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, ca)
	keyDER, _ := x509.MarshalECPrivateKey(key)
	return &corev1.Secret{
		Name: "cred", Namespace: "ns",
		Data: map[string][]byte{
			corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		},
	}
}

func providerFor(t *testing.T, secret *corev1.Secret, ca string) *SecretClientProvider {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	cm := &corev1.ConfigMap{Name: "ca", Namespace: "ns", Data: map[string]string{"ca.crt": ca}}
	objs := []client.Object{cm}
	if secret != nil {
		objs = append(objs, secret)
	}
	return &SecretClientProvider{Reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(), RequestsPerSecond: 10}
}

func connWith(auth zitiv1.ConnectionAuth) *zitiv1.ZitiConnection {
	c := &zitiv1.ZitiConnection{Name: "default", UID: "u", Spec: zitiv1.ZitiConnectionSpec{ManagementURL: "https://ctrl.invalid/edge/management/v1", Auth: auth}}
	c.Spec.CABundle.ConfigMapRef = zitiv1.ConfigMapKeyRef{Namespace: "ns", Name: "ca", Key: "ca.crt"}
	return c
}

func TestProviderCertAuth(t *testing.T) {
	ca, key, caCert := caPEM(t)
	certAuth := zitiv1.ConnectionAuth{Cert: &zitiv1.CertAuth{SecretRef: zitiv1.SecretRef{Namespace: "ns", Name: "cred"}}}

	p := providerFor(t, clientCertSecret(t, key, caCert), ca)
	if c, err := p.For(t.Context(), connWith(certAuth)); err != nil || c == nil {
		t.Fatalf("valid cert secret: %v", err)
	}

	bad := clientCertSecret(t, key, caCert)
	bad.Data[corev1.TLSPrivateKeyKey] = []byte("not a key")
	if _, err := providerFor(t, bad, ca).For(t.Context(), connWith(certAuth)); err == nil || !strings.Contains(err.Error(), "tls.crt") {
		t.Errorf("bad key: %v", err)
	}
	if _, err := providerFor(t, nil, ca).For(t.Context(), connWith(certAuth)); err == nil || !strings.Contains(err.Error(), "credential secret") {
		t.Errorf("missing secret: %v", err)
	}
}

func TestProviderUpdbAndMissingAuth(t *testing.T) {
	ca, _, _ := caPEM(t)
	updb := zitiv1.ConnectionAuth{Updb: &zitiv1.UpdbAuth{SecretRef: zitiv1.SecretRef{Namespace: "ns", Name: "cred"}}}
	good := &corev1.Secret{Name: "cred", Namespace: "ns", Data: map[string][]byte{"username": []byte("u"), "password": []byte("p")}}
	if c, err := providerFor(t, good, ca).For(t.Context(), connWith(updb)); err != nil || c == nil {
		t.Fatalf("updb: %v", err)
	}
	empty := &corev1.Secret{Name: "cred", Namespace: "ns"}
	if _, err := providerFor(t, empty, ca).For(t.Context(), connWith(updb)); err == nil || !strings.Contains(err.Error(), "username and password") {
		t.Errorf("empty updb secret: %v", err)
	}
	if _, err := providerFor(t, good, ca).For(t.Context(), connWith(zitiv1.ConnectionAuth{})); err == nil || !strings.Contains(err.Error(), "neither") {
		t.Errorf("no auth: %v", err)
	}
}
