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

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.zitiName) || (has(self.zitiName) && self.zitiName == oldSelf.zitiName)",message="zitiName is immutable"
// +kubebuilder:validation:XValidation:rule="self.deletionPolicy == oldSelf.deletionPolicy",message="deletionPolicy is immutable"
// +kubebuilder:validation:XValidation:rule="self.enrollmentMode == oldSelf.enrollmentMode",message="enrollmentMode is immutable"
// +kubebuilder:validation:XValidation:rule="!(has(self.serviceAccount) && has(self.externalId))",message="set only one of serviceAccount and externalId"
// +kubebuilder:validation:XValidation:rule="!has(self.certificate) || (self.enrollmentMode == 'None' && has(self.externalId))",message="certificate needs enrollmentMode None and externalId"
// +kubebuilder:validation:XValidation:rule="self.enrollmentMode != 'None' || (has(self.authPolicy) && (has(self.serviceAccount) || has(self.externalId)))",message="enrollmentMode None needs authPolicy and serviceAccount or externalId"
type ZitiIdentitySpec struct {
	// connectionRef is the name of the ZitiConnection to use. It defaults to "default".
	// +kubebuilder:default=default
	// +optional
	ConnectionRef string `json:"connectionRef,omitempty"`

	// zitiName defaults to <namespace>.<name>.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1000
	// +optional
	ZitiName string `json:"zitiName,omitempty"`

	// roleAttributes follow the connection roleScope, like ZitiApp roleAttributes.
	// +optional
	RoleAttributes []string `json:"roleAttributes,omitempty"`

	// authPolicy is a Ziti auth policy name. It defaults to "Default".
	// +optional
	AuthPolicy string `json:"authPolicy,omitempty"`

	// secretName receives "enrollment.jwt" or "identity.json", by enrollmentMode. It defaults to the CR name.
	// +kubebuilder:validation:MaxLength=253
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// serviceAccount is a ServiceAccount name in this namespace. Its token logs in to Ziti as this identity.
	// It sets externalId to system:serviceaccount:<namespace>:<name>. Needs a ZitiJwtSigner and an authPolicy that allows it.
	// +kubebuilder:validation:MaxLength=253
	// +optional
	ServiceAccount string `json:"serviceAccount,omitempty"`

	// externalId is the value that a token claim must match to log in as this identity, for an identity provider
	// other than Kubernetes. It needs roleScope Global on the connection, because it can name any subject.
	// +kubebuilder:validation:MaxLength=1000
	// +optional
	ExternalID string `json:"externalId,omitempty"`

	// enrollmentMode JwtOnly writes the enrollment JWT to the Secret. A person or workload enrolls with it.
	// OperatorEnrolled makes the operator enroll and write identity.json to the Secret.
	// None creates an identity without enrollment. It logs in with a token, so authPolicy is required.
	// +kubebuilder:default=JwtOnly
	// +kubebuilder:validation:Enum=JwtOnly;OperatorEnrolled;None
	// +optional
	EnrollmentMode EnrollmentMode `json:"enrollmentMode,omitempty"`

	// certificate makes the operator create a cert-manager Certificate for this identity. Its common name is externalId
	// and it is stored in the Secret secretName (tls.crt, tls.key). The workload logs in with it.
	// The issuer must belong to a CA that a ZitiCA registered in Ziti. Needs enrollmentMode None and externalId.
	// +optional
	Certificate *WorkloadCertificate `json:"certificate,omitempty"`

	// managementPolicy Manage creates the identity. Adopt takes over an existing identity named zitiName:
	// it adds ownership tags, sets roleAttributes and authPolicy, and never deletes the identity.
	// Observe only reads the existing identity and never writes to Ziti.
	// +kubebuilder:default=Manage
	// +kubebuilder:validation:Enum=Manage;Adopt;Observe
	// +optional
	ManagementPolicy ManagementPolicy `json:"managementPolicy,omitempty"`

	// deletionPolicy does not apply to adopted identities. Those are released, never deleted.
	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Enum=Delete;Orphan
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

// WorkloadCertificate describes the cert-manager Certificate that the operator creates for an identity.
type WorkloadCertificate struct {
	// issuerRef names the cert-manager issuer that signs the certificate.
	// +required
	IssuerRef CertificateIssuerRef `json:"issuerRef"`

	// duration is the lifetime of the certificate. Empty uses the cert-manager default (90 days).
	// +optional
	Duration *metav1.Duration `json:"duration,omitempty"`
}

// CertificateIssuerRef points to a cert-manager Issuer or ClusterIssuer. An Issuer must be in the namespace of the identity.
type CertificateIssuerRef struct {
	// name of the issuer.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// kind is Issuer or ClusterIssuer.
	// +kubebuilder:default=ClusterIssuer
	// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
	// +optional
	Kind string `json:"kind,omitempty"`
}

type EnrollmentMode string

const (
	EnrollmentJWTOnly  EnrollmentMode = "JwtOnly"
	EnrollmentOperator EnrollmentMode = "OperatorEnrolled"
	EnrollmentNone     EnrollmentMode = "None"
)

type ZitiIdentityStatus struct {
	// +optional
	ZitiID string `json:"zitiId,omitempty"`
	// +optional
	Enrolled bool `json:"enrolled,omitempty"`
	// +optional
	EnrollmentExpiresAt *metav1.Time `json:"enrollmentExpiresAt,omitempty"`
	// certNotAfter is the earliest expiry of the identity's client certificates.
	// +optional
	CertNotAfter *metav1.Time `json:"certNotAfter,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ztid,categories=ziti
// +kubebuilder:printcolumn:name="Policy",type=string,JSONPath=".spec.managementPolicy"
// +kubebuilder:printcolumn:name="Enrolled",type=boolean,JSONPath=".status.enrolled"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Enrollment Expires",type=string,JSONPath=".status.enrollmentExpiresAt"
// +kubebuilder:printcolumn:name="Cert Expires",type=string,JSONPath=".status.certNotAfter"
// +kubebuilder:printcolumn:name="Ziti ID",type=string,JSONPath=".status.zitiId",priority=1
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiIdentity is the Schema for the zitiidentities API
type ZitiIdentity struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiIdentity
	// +required
	Spec ZitiIdentitySpec `json:"spec"`

	// status defines the observed state of ZitiIdentity
	// +optional
	Status ZitiIdentityStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiIdentityList contains a list of ZitiIdentity
type ZitiIdentityList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiIdentity `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiIdentity{}, &ZitiIdentityList{})
		return nil
	})
}
