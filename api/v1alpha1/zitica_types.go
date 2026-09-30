/*
Copyright 2026 The openziti-operator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// CertificateSource points to a Secret that holds the CA certificate. A cert-manager CA Issuer keeps its
// certificate and key in such a Secret (tls.crt and tls.key).
type CertificateSource struct {
	// secretRef names the Secret.
	SecretRef SecretRef `json:"secretRef"`

	// certKey is the key in the Secret that holds the PEM certificate. The first certificate is used.
	// +kubebuilder:default=tls.crt
	// +optional
	CertKey string `json:"certKey,omitempty"`
}

// VerificationIssuer names the cert-manager issuer that proves ownership of the CA without the CA key.
type VerificationIssuer struct {
	// name of the issuer. It must be the issuer whose CA certificate this ZitiCA registers.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// kind is Issuer or ClusterIssuer.
	// +kubebuilder:default=ClusterIssuer
	// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
	// +optional
	Kind string `json:"kind,omitempty"`

	// namespace is where the operator creates the short-lived proof Certificate and its Secret. For an Issuer it is
	// the namespace of the issuer. Both are removed once Ziti has verified the CA.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}

// CAVerification says how the operator proves to Ziti that it controls the CA.
// +kubebuilder:validation:XValidation:rule="!(self.signWithSecretKey && has(self.issuerRef))",message="set only one of signWithSecretKey and issuerRef"
type CAVerification struct {
	// signWithSecretKey lets the operator read the private key (key tls.key) of the same Secret and use it, in memory
	// only, to sign the one-time proof that Ziti asks for. The key is never stored, logged, or sent to Ziti.
	// Without it, status.verificationToken holds the token, and someone with the key signs a certificate
	// whose common name is that token and sends it to Ziti.
	// +kubebuilder:default=false
	// +optional
	SignWithSecretKey bool `json:"signWithSecretKey,omitempty"`

	// issuerRef proves ownership through cert-manager, so the operator never reads the CA key. The operator asks the
	// issuer for a short-lived certificate whose common name is Ziti's verification token, reads the certificate
	// (not its key use) from the Secret, and sends it to Ziti. Set only one of signWithSecretKey and issuerRef.
	// +optional
	IssuerRef *VerificationIssuer `json:"issuerRef,omitempty"`
}

// ExternalIDClaim says where in a client certificate Ziti finds the value that names the identity.
type ExternalIDClaim struct {
	// location is the certificate field to read.
	// +kubebuilder:default=COMMON_NAME
	// +kubebuilder:validation:Enum=COMMON_NAME;SAN_URI;SAN_EMAIL
	// +optional
	Location string `json:"location,omitempty"`

	// matcher selects which values count. ALL takes every value.
	// +kubebuilder:default=ALL
	// +kubebuilder:validation:Enum=ALL;PREFIX;SUFFIX;SCHEME
	// +optional
	Matcher string `json:"matcher,omitempty"`

	// matcherCriteria is the prefix, suffix, or scheme for the matcher. Empty for ALL.
	// +optional
	MatcherCriteria string `json:"matcherCriteria,omitempty"`

	// parser cuts the value. SPLIT splits it at parserCriteria and takes the part at index.
	// +kubebuilder:default=NONE
	// +kubebuilder:validation:Enum=NONE;SPLIT
	// +optional
	Parser string `json:"parser,omitempty"`

	// parserCriteria is the separator for SPLIT. Empty for NONE.
	// +optional
	ParserCriteria string `json:"parserCriteria,omitempty"`

	// index is the part to take after SPLIT.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Index int32 `json:"index,omitempty"`
}

// AutoEnrollment creates identities the first time a certificate of the CA logs in.
type AutoEnrollment struct {
	// identityRoles are the role attributes of the identities that are created.
	// +optional
	IdentityRoles []string `json:"identityRoles,omitempty"`

	// identityNameFormat names the new identities. Ziti fills [caName], [commonName], and [requestedName].
	// +kubebuilder:default="[caName]-[commonName]"
	// +optional
	IdentityNameFormat string `json:"identityNameFormat,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.zitiName) || (has(self.zitiName) && self.zitiName == oldSelf.zitiName)",message="zitiName is immutable"
// +kubebuilder:validation:XValidation:rule="self.deletionPolicy == oldSelf.deletionPolicy",message="deletionPolicy is immutable"
type ZitiCASpec struct {
	// connectionRef is the name of the ZitiConnection to use. It defaults to "default".
	// +kubebuilder:default=default
	// +optional
	ConnectionRef string `json:"connectionRef,omitempty"`

	// zitiName is the CA name in Ziti. It defaults to the resource name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1000
	// +optional
	ZitiName string `json:"zitiName,omitempty"`

	// certificate is where the CA certificate comes from.
	Certificate CertificateSource `json:"certificate"`

	// verification says how the operator proves that it controls the CA. Ziti trusts a CA only after that.
	// +optional
	Verification CAVerification `json:"verification,omitempty"`

	// authEnabled lets identities log in with a certificate that this CA issued.
	// +kubebuilder:default=true
	// +optional
	AuthEnabled *bool `json:"authEnabled,omitempty"`

	// externalIdClaim says which certificate field names the identity. Ziti matches it to identity.externalId,
	// so a renewed certificate still finds its identity.
	// +optional
	ExternalIDClaim ExternalIDClaim `json:"externalIdClaim,omitempty"`

	// autoEnrollment creates identities on the first login of a certificate. Leave it out to create identities yourself.
	// +optional
	AutoEnrollment *AutoEnrollment `json:"autoEnrollment,omitempty"`

	// deletionPolicy Delete removes the CA from Ziti when this resource is deleted.
	// Orphan removes only the operator tags and keeps the CA. It cannot change after creation.
	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Enum=Delete;Orphan
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

type ZitiCAStatus struct {
	// +optional
	CAID string `json:"caId,omitempty"`
	// fingerprint is the SHA-1 fingerprint of the CA certificate in Ziti.
	// +optional
	Fingerprint string `json:"fingerprint,omitempty"`
	// verified is true when Ziti accepts the proof of ownership.
	// +optional
	Verified bool `json:"verified,omitempty"`
	// verificationToken is the common name of the certificate to sign and send to Ziti. Set only while unverified.
	// +optional
	VerificationToken string `json:"verificationToken,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ztca,categories=ziti
// +kubebuilder:printcolumn:name="Verified",type=boolean,JSONPath=".status.verified"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiCA is the Schema for the ziticas API
type ZitiCA struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiCA
	// +required
	Spec ZitiCASpec `json:"spec"`

	// status defines the observed state of ZitiCA
	// +optional
	Status ZitiCAStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiCAList contains a list of ZitiCA
type ZitiCAList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiCA `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiCA{}, &ZitiCAList{})
		return nil
	})
}
