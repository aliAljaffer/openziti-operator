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

// KubernetesKeys takes the issuer and the signing keys from the Kubernetes cluster the operator runs in.
type KubernetesKeys struct{}

// JwksEndpoint names a JWKS URL that the Ziti controller fetches itself. It follows key rotation.
type JwksEndpoint struct {
	// url is the JWKS endpoint. The Ziti controller must be able to reach it.
	// +kubebuilder:validation:Pattern=`^https://`
	URL string `json:"url"`

	// issuer must equal the iss claim of the tokens.
	// +kubebuilder:validation:MinLength=1
	Issuer string `json:"issuer"`
}

// SignerKeys tells Ziti how to verify token signatures. Set exactly one field.
// +kubebuilder:validation:XValidation:rule="has(self.kubernetes) != has(self.jwksEndpoint)",message="set exactly one of kubernetes and jwksEndpoint"
type SignerKeys struct {
	// kubernetes reads the issuer and the keys from this cluster. The operator picks the key that signs
	// its own service account token. When the cluster rotates the key, the next sync updates the signer.
	// +optional
	Kubernetes *KubernetesKeys `json:"kubernetes,omitempty"`

	// jwksEndpoint lets the Ziti controller fetch the keys, for any identity provider.
	// +optional
	JwksEndpoint *JwksEndpoint `json:"jwksEndpoint,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.zitiName) || (has(self.zitiName) && self.zitiName == oldSelf.zitiName)",message="zitiName is immutable"
// +kubebuilder:validation:XValidation:rule="self.deletionPolicy == oldSelf.deletionPolicy",message="deletionPolicy is immutable"
type ZitiJwtSignerSpec struct {
	// connectionRef is the name of the ZitiConnection to use. It defaults to "default".
	// +kubebuilder:default=default
	// +optional
	ConnectionRef string `json:"connectionRef,omitempty"`

	// zitiName is the signer name in Ziti. It defaults to the resource name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1000
	// +optional
	ZitiName string `json:"zitiName,omitempty"`

	// audience must equal the aud claim of the tokens. Use a value made for Ziti, for example "ziti".
	// +kubebuilder:validation:MinLength=1
	Audience string `json:"audience"`

	// claimsProperty is the token claim that names the identity.
	// +kubebuilder:default=sub
	// +optional
	ClaimsProperty string `json:"claimsProperty,omitempty"`

	// useExternalId matches the claim to the identity externalId. False matches it to the identity id.
	// +kubebuilder:default=true
	// +optional
	UseExternalID *bool `json:"useExternalId,omitempty"`

	// keys tells Ziti how to verify signatures.
	Keys SignerKeys `json:"keys"`

	// createAuthPolicy creates an auth policy with the same name that accepts only this signer.
	// Identities select it with spec.authPolicy.
	// +kubebuilder:default=true
	// +optional
	CreateAuthPolicy *bool `json:"createAuthPolicy,omitempty"`

	// deletionPolicy Delete removes the Ziti entities when this resource is deleted.
	// Orphan removes only the operator tags and keeps the entities. It cannot change after creation.
	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Enum=Delete;Orphan
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

type ZitiJwtSignerStatus struct {
	// +optional
	SignerID string `json:"signerId,omitempty"`
	// +optional
	AuthPolicyID string `json:"authPolicyId,omitempty"`
	// issuer is the token issuer that Ziti accepts.
	// +optional
	Issuer string `json:"issuer,omitempty"`
	// keyId is the id of the signing key that Ziti uses. Empty with jwksEndpoint.
	// +optional
	KeyID string `json:"keyId,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ztjwt,categories=ziti
// +kubebuilder:printcolumn:name="Issuer",type=string,JSONPath=".status.issuer"
// +kubebuilder:printcolumn:name="Key",type=string,JSONPath=".status.keyId"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiJwtSigner is the Schema for the zitijwtsigners API
type ZitiJwtSigner struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiJwtSigner
	// +required
	Spec ZitiJwtSignerSpec `json:"spec"`

	// status defines the observed state of ZitiJwtSigner
	// +optional
	Status ZitiJwtSignerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiJwtSignerList contains a list of ZitiJwtSigner
type ZitiJwtSignerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiJwtSigner `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiJwtSigner{}, &ZitiJwtSignerList{})
		return nil
	})
}
