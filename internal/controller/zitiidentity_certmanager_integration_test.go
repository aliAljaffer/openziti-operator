//go:build integration

package controller

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
)

// Applies the Certificate that the operator builds to a cluster with cert-manager and waits until cert-manager issues it.
// Needs ZITI_KUBECTL (kubectl and its cluster flags) and ZITI_K8S_NAMESPACE, an existing namespace to use.
func TestWorkloadCertificateIsIssuedByCertManager(t *testing.T) {
	kubectl, ns := strings.Fields(os.Getenv("ZITI_KUBECTL")), os.Getenv("ZITI_K8S_NAMESPACE")
	if len(kubectl) == 0 || ns == "" {
		t.Skip("ZITI_KUBECTL or ZITI_K8S_NAMESPACE not set")
	}
	k := func(stdin string, args ...string) ([]byte, error) {
		cmd := exec.Command(kubectl[0], append(append([]string{}, kubectl[1:]...), append([]string{"-n", ns}, args...)...)...)
		cmd.Stdin = strings.NewReader(stdin)
		return cmd.CombinedOutput()
	}
	issuer := `{"apiVersion":"cert-manager.io/v1","kind":"Issuer","metadata":{"name":"it-selfsigned"},"spec":{"selfSigned":{}}}`
	id := &zitiv1.ZitiIdentity{
		Name: "it-workload", Namespace: ns,
		Spec: zitiv1.ZitiIdentitySpec{
			ExternalID:  "team-a.it-workload",
			Certificate: &zitiv1.WorkloadCertificate{IssuerRef: zitiv1.CertificateIssuerRef{Name: "it-selfsigned", Kind: "Issuer"}, Duration: &metav1.Duration{Duration: 24 * time.Hour}},
		},
	}
	raw, err := json.Marshal(desired.WorkloadCertificate(id).Object)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = k("", "delete", "certificate/it-workload", "issuer/it-selfsigned", "secret/it-workload", "--ignore-not-found", "--wait=true", "--timeout=60s")
	})
	for _, doc := range []string{issuer, string(raw)} {
		if out, err := k(doc, "apply", "-f", "-"); err != nil {
			t.Fatalf("kubectl apply: %v: %s", err, out)
		}
	}
	if out, err := k("", "wait", "certificate/it-workload", "--for=condition=Ready", "--timeout=90s"); err != nil {
		desc, _ := k("", "describe", "certificate/it-workload")
		t.Fatalf("not issued: %v: %s\n%s", err, out, desc)
	}
	out, err := k("", "get", "secret/it-workload", "-o", "jsonpath={.data.tls\\.crt}")
	if err != nil || len(out) < 100 {
		t.Fatalf("no certificate in the Secret: %v: %s", err, out)
	}
}
