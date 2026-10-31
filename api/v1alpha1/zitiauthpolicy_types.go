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

// AuthPolicyCert controls certificate login.
type AuthPolicyCert struct {
	// allowed lets an identity log in with a certificate.
	// +kubebuilder:default=true
	// +optional
	Allowed *bool `json:"allowed,omitempty"`

	// allowExpiredCerts accepts an expired certificate. Leave false unless you need it for migration.
	// +optional
	AllowExpiredCerts bool `json:"allowExpiredCerts,omitempty"`
}

// AuthPolicyUpdb controls username and password login.
type AuthPolicyUpdb struct {
	// allowed lets an identity log in with a password.
	// +kubebuilder:default=false
	// +optional
	Allowed *bool `json:"allowed,omitempty"`

	// minPasswordLength is the shortest accepted password.
	// +kubebuilder:validation:Minimum=0
	// +optional
	MinPasswordLength int64 `json:"minPasswordLength,omitempty"`

	// maxAttempts locks the account after this many failures. Zero uses the controller default.
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxAttempts int64 `json:"maxAttempts,omitempty"`

	// lockoutDurationMinutes is how long a locked account stays locked. Zero uses the controller default.
	// +kubebuilder:validation:Minimum=0
	// +optional
	LockoutDurationMinutes int64 `json:"lockoutDurationMinutes,omitempty"`

	// requireMixedCase requires upper and lower case letters.
	// +optional
	RequireMixedCase bool `json:"requireMixedCase,omitempty"`

	// requireNumberChar requires a digit.
	// +optional
	RequireNumberChar bool `json:"requireNumberChar,omitempty"`

	// requireSpecialChar requires a non-alphanumeric character.
	// +optional
	RequireSpecialChar bool `json:"requireSpecialChar,omitempty"`
}

// AuthPolicyExtJWT controls external JWT login.
type AuthPolicyExtJWT struct {
	// allowed lets an identity log in with an external JWT.
	// +kubebuilder:default=false
	// +optional
	Allowed *bool `json:"allowed,omitempty"`

	// allowedSigners are ZitiJwtSigner names. Empty with allowed true accepts every enabled signer.
	// +optional
	AllowedSigners []string `json:"allowedSigners,omitempty"`
}

// AuthPolicyPrimary sets the initial login method.
type AuthPolicyPrimary struct {
	// cert controls certificate login.
	// +optional
	Cert AuthPolicyCert `json:"cert,omitzero"`

	// updb controls username and password login.
	// +optional
	Updb AuthPolicyUpdb `json:"updb,omitzero"`

	// extJwt controls external JWT login.
	// +optional
	ExtJWT AuthPolicyExtJWT `json:"extJwt,omitzero"`
}

// AuthPolicySecondary sets the second factor.
type AuthPolicySecondary struct {
	// requireTotp requires a time-based one-time password after the primary login.
	// +optional
	RequireTotp bool `json:"requireTotp,omitempty"`

	// requireExtJwtSigner requires a JWT from this ZitiJwtSigner on every request.
	// +optional
	RequireExtJwtSigner string `json:"requireExtJwtSigner,omitempty"`
}

// ZitiAuthPolicySpec is the desired authentication policy. It maps one to one to a Ziti auth policy.
type ZitiAuthPolicySpec struct {
	EntitySpec `json:",inline"`

	// primary sets the initial login method.
	// +optional
	Primary AuthPolicyPrimary `json:"primary,omitzero"`

	// secondary sets the second factor.
	// +optional
	Secondary AuthPolicySecondary `json:"secondary,omitzero"`
}

type ZitiAuthPolicyStatus struct {
	EntityStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ztapolicy,categories=ziti
// +kubebuilder:printcolumn:name="Ziti ID",type=string,JSONPath=".status.zitiId",priority=1
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiAuthPolicy is a Ziti auth policy, one to one.
type ZitiAuthPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiAuthPolicy
	// +required
	Spec ZitiAuthPolicySpec `json:"spec"`

	// status defines the observed state of ZitiAuthPolicy
	// +optional
	Status ZitiAuthPolicyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiAuthPolicyList contains a list of ZitiAuthPolicy
type ZitiAuthPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiAuthPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiAuthPolicy{}, &ZitiAuthPolicyList{})
		return nil
	})
}
