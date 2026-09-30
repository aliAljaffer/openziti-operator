// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

// CertManagerGVK is the version of the cert-manager Certificate that the operator creates.
var CertManagerGVK = struct{ Group, Version, Kind string }{"cert-manager.io", "v1", "Certificate"}

// WorkloadCertificateName is the name of the Certificate and of its Secret when the identity sets no secretName.
func WorkloadCertificateName(id *zitiv1.ZitiIdentity) string { return id.Name }

// WorkloadCertificate builds the cert-manager Certificate for an identity that logs in with a certificate.
// The common name is the externalId, which Ziti matches to the identity.
func WorkloadCertificate(id *zitiv1.ZitiIdentity) *unstructured.Unstructured {
	secret := id.Spec.SecretName
	if secret == "" {
		secret = id.Name
	}
	ref := id.Spec.Certificate.IssuerRef
	kind := ref.Kind
	if kind == "" {
		kind = "ClusterIssuer"
	}
	spec := map[string]any{
		"commonName": id.Spec.ExternalID,
		"secretName": secret,
		"usages":     []any{"client auth"},
		"privateKey": map[string]any{"algorithm": "ECDSA", "size": int64(256)},
		"issuerRef":  map[string]any{"name": ref.Name, "kind": kind, "group": "cert-manager.io"},
	}
	if d := id.Spec.Certificate.Duration; d != nil {
		spec["duration"] = d.Duration.String()
	}
	u := &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
	u.SetAPIVersion(CertManagerGVK.Group + "/" + CertManagerGVK.Version)
	u.SetKind(CertManagerGVK.Kind)
	u.SetNamespace(id.Namespace)
	u.SetName(WorkloadCertificateName(id))
	return u
}
