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
type ZitiIdentitySpec struct {
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

	// enrollmentMode JwtOnly writes the enrollment JWT to the Secret. A person or workload enrolls with it.
	// OperatorEnrolled makes the operator enroll and write identity.json to the Secret.
	// +kubebuilder:default=JwtOnly
	// +kubebuilder:validation:Enum=JwtOnly;OperatorEnrolled
	// +optional
	EnrollmentMode EnrollmentMode `json:"enrollmentMode,omitempty"`

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

type EnrollmentMode string

const (
	EnrollmentJWTOnly  EnrollmentMode = "JwtOnly"
	EnrollmentOperator EnrollmentMode = "OperatorEnrolled"
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
// +kubebuilder:resource:shortName=ztid
// +kubebuilder:printcolumn:name="Policy",type=string,JSONPath=".spec.managementPolicy"
// +kubebuilder:printcolumn:name="Enrolled",type=boolean,JSONPath=".status.enrolled"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Enrollment Expires",type=date,JSONPath=".status.enrollmentExpiresAt"
// +kubebuilder:printcolumn:name="Cert Expires",type=date,JSONPath=".status.certNotAfter"
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
