// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func TestRunRejectsUnknownFormat(t *testing.T) {
	err := Run(context.Background(), Options{Format: "yaml"})
	if err == nil || !strings.Contains(err.Error(), "table or json") {
		t.Fatalf("Run() error = %v, want the format error", err)
	}
}

func TestConnectionNeedsClusterAccess(t *testing.T) {
	_, err := clientFor(context.Background(), Options{
		Connection: "default",
		Kubeconfig: filepath.Join(t.TempDir(), "missing"),
	})
	if err == nil {
		t.Fatal("clientFor() succeeded without a kubeconfig")
	}
}

func TestConnectionReadsTheCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	secret := &corev1.Secret{
		Data: map[string][]byte{"username": []byte("admin"), "password": []byte("secret")},
	}
	secret.Namespace, secret.Name = "ziti", "cred"
	cm := &corev1.ConfigMap{
		Data: map[string]string{"ca.crt": testCA(t)},
	}
	cm.Namespace, cm.Name = "ziti", "ca"
	conn := &zitiv1.ZitiConnection{
		Spec: zitiv1.ZitiConnectionSpec{
			ManagementURL: "https://ziti.example.com/edge/management/v1",
			CABundle: zitiv1.CABundle{ConfigMapRef: zitiv1.ConfigMapKeyRef{
				Namespace: "ziti", Name: "ca", Key: "ca.crt",
			}},
			Auth: zitiv1.ConnectionAuth{Updb: &zitiv1.UpdbAuth{SecretRef: zitiv1.SecretRef{
				Namespace: "ziti", Name: "cred",
			}}},
			HostingRouters: []string{"router-a"},
		},
	}
	conn.Name = "default"
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, cm, conn).Build()

	zc, err := connectionClientFor(context.Background(), k, Options{Connection: "default", RequestsPerSecond: 1})
	if err != nil {
		t.Fatalf("connectionClientFor() error = %v", err)
	}
	if zc == nil {
		t.Fatal("connectionClientFor() returned no client")
	}
}

func TestMainRejectsArguments(t *testing.T) {
	if got := Main([]string{"unexpected"}); got != 2 {
		t.Fatalf("Main() = %d, want 2", got)
	}
}

func testCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
