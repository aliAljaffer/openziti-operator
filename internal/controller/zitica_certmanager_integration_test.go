//go:build integration

package controller

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/openziti/edge-api/rest_util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

// A real Ziti controller must accept a proof certificate that a real cert-manager issued. The reconciler runs against a fake
// Kubernetes client. The test copies the proof Certificate to a real cluster, waits for cert-manager, and copies the Secret back.
// Needs ZITI_MGMT_URL, ZITI_KUBECTL (kubectl and its cluster flags) and ZITI_K8S_NAMESPACE, an existing namespace to use.
func TestCAVerifiedByCertManagerAgainstRealController(t *testing.T) {
	mgmt, kubectl, ns := os.Getenv("ZITI_MGMT_URL"), strings.Fields(os.Getenv("ZITI_KUBECTL")), os.Getenv("ZITI_K8S_NAMESPACE")
	if mgmt == "" || len(kubectl) == 0 || ns == "" {
		t.Skip("ZITI_MGMT_URL, ZITI_KUBECTL or ZITI_K8S_NAMESPACE not set")
	}
	u, _ := url.Parse(mgmt)
	pool, err := rest_util.GetControllerWellKnownCaPool("https://" + u.Host)
	if err != nil {
		t.Fatal(err)
	}
	auth := rest_util.NewAuthenticatorUpdb(os.Getenv("ZITI_USERNAME"), os.Getenv("ZITI_PASSWORD"))
	auth.RootCas = pool
	real, err := ziti.NewREST(mgmt, auth, 10)
	if err != nil {
		t.Fatal(err)
	}
	k := func(stdin string, args ...string) ([]byte, error) {
		cmd := exec.Command(kubectl[0], append(append([]string{}, kubectl[1:]...), append([]string{"-n", ns}, args...)...)...)
		cmd.Stdin = strings.NewReader(stdin)
		return cmd.CombinedOutput()
	}
	t.Cleanup(func() {
		_, _ = k("", "delete", "certificate/it-ca-root", "certificate/cm-verify", "issuer/it-bootstrap", "issuer/it-ca", "secret/it-ca-root", "secret/cm-verify", "--ignore-not-found", "--wait=true", "--timeout=60s")
	})
	for _, doc := range []string{
		`{"apiVersion":"cert-manager.io/v1","kind":"Issuer","metadata":{"name":"it-bootstrap"},"spec":{"selfSigned":{}}}`,
		`{"apiVersion":"cert-manager.io/v1","kind":"Certificate","metadata":{"name":"it-ca-root"},"spec":{"isCA":true,"commonName":"it-ca-root","secretName":"it-ca-root","privateKey":{"algorithm":"ECDSA","size":256},"issuerRef":{"name":"it-bootstrap","kind":"Issuer"}}}`,
	} {
		if out, err := k(doc, "apply", "-f", "-"); err != nil {
			t.Fatalf("apply: %v: %s", err, out)
		}
	}
	if out, err := k("", "wait", "certificate/it-ca-root", "--for=condition=Ready", "--timeout=90s"); err != nil {
		t.Fatalf("CA not issued: %v: %s", err, out)
	}
	if out, err := k(`{"apiVersion":"cert-manager.io/v1","kind":"Issuer","metadata":{"name":"it-ca"},"spec":{"ca":{"secretName":"it-ca-root"}}}`, "apply", "-f", "-"); err != nil {
		t.Fatalf("apply issuer: %v: %s", err, out)
	}
	crt, err := k("", "get", "secret/it-ca-root", "-o", "go-template={{index .data \"tls.crt\" | base64decode}}")
	if err != nil {
		t.Fatalf("read CA: %v: %s", err, crt)
	}

	// The fake cluster holds the CA certificate only. The operator never sees the CA key.
	caSec := &corev1.Secret{Name: "issuer-ca", Namespace: "cert-manager", Data: map[string][]byte{corev1.TLSCertKey: crt}}
	ce := setupCA(t, caSec, func(ca *zitiv1.ZitiCA) {
		ca.Spec.ZitiName = "it-cm-ca"
		ca.Spec.Verification.IssuerRef = &zitiv1.VerificationIssuer{Name: "it-ca", Kind: "Issuer", Namespace: ns}
	})
	ce.r.Clients = staticProvider{real}
	t.Cleanup(func() {
		ctx := context.Background()
		var ca zitiv1.ZitiCA
		if ce.k.Get(ctx, ce.key, &ca) == nil {
			_ = ce.k.Delete(ctx, &ca)
			_, _ = ce.r.Reconcile(ctx, ctrl.Request{NamespacedName: ce.key})
		}
	})

	ce.reconcile(t)
	fake := &unstructured.Unstructured{}
	fake.SetGroupVersionKind(schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"})
	proofKey := types.NamespacedName{Namespace: ns, Name: "cm-verify"}
	if err := ce.k.Get(t.Context(), proofKey, fake); err != nil {
		t.Fatalf("no proof Certificate: %v", err)
	}
	manifest := map[string]any{"apiVersion": "cert-manager.io/v1", "kind": "Certificate", "metadata": map[string]any{"name": "cm-verify"}, "spec": fake.Object["spec"]}
	raw, _ := json.Marshal(manifest)
	if out, err := k(string(raw), "apply", "-f", "-"); err != nil {
		t.Fatalf("apply proof: %v: %s", err, out)
	}
	if out, err := k("", "wait", "certificate/cm-verify", "--for=condition=Ready", "--timeout=90s"); err != nil {
		t.Fatalf("proof not issued: %v: %s", err, out)
	}
	leaf, err := k("", "get", "secret/cm-verify", "-o", "go-template={{index .data \"tls.crt\" | base64decode}}")
	if err != nil {
		t.Fatalf("read proof: %v: %s", err, leaf)
	}
	fake.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}
	if err := ce.k.Update(t.Context(), fake); err != nil {
		t.Fatal(err)
	}
	if err := ce.k.Create(t.Context(), &corev1.Secret{Namespace: ns, Name: "cm-verify",
		Annotations: map[string]string{"cert-manager.io/certificate-name": "cm-verify"}, Data: map[string][]byte{corev1.TLSCertKey: leaf}}); err != nil {
		t.Fatal(err)
	}

	ca := ce.reconcile(t)
	if !ca.Status.Verified || condOf(ca, CondReady).Status != "True" {
		t.Fatalf("Ziti did not accept the cert-manager proof: %+v %+v", ca.Status, ca.Status.Conditions)
	}
}
