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
// +kubebuilder:validation:XValidation:rule="self.type == oldSelf.type",message="type is immutable"
type ZitiServicePolicySpec struct {
	EntitySpec `json:",inline"`

	// type is Dial (who may connect) or Bind (who may host). It cannot change later.
	// +kubebuilder:validation:Enum=Dial;Bind
	Type string `json:"type"`

	// semantic is AnyOf (an identity needs one of the roles) or AllOf (it needs all of them).
	// +kubebuilder:default=AnyOf
	// +kubebuilder:validation:Enum=AnyOf;AllOf
	// +optional
	Semantic string `json:"semantic,omitempty"`

	// identityRoles select the identities. "#attr" selects by role attribute, "#all" every identity, "@name" one identity.
	// +kubebuilder:validation:MinItems=1
	IdentityRoles []string `json:"identityRoles"`

	// serviceRoles select the services. "#attr" selects by role attribute, "#all" every service, "@name" one service.
	// +kubebuilder:validation:MinItems=1
	ServiceRoles []string `json:"serviceRoles"`

	// postureCheckRoles select the posture checks that identities must pass.
	// +optional
	PostureCheckRoles []string `json:"postureCheckRoles,omitempty"`
}

type ZitiServicePolicyStatus struct {
	EntityStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ztsp,categories=ziti
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Ziti ID",type=string,JSONPath=".status.zitiId",priority=1
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiServicePolicy is a Ziti service policy (Dial or Bind), one to one.
type ZitiServicePolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiServicePolicy
	// +required
	Spec ZitiServicePolicySpec `json:"spec"`

	// status defines the observed state of ZitiServicePolicy
	// +optional
	Status ZitiServicePolicyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiServicePolicyList contains a list of ZitiServicePolicy
type ZitiServicePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiServicePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiServicePolicy{}, &ZitiServicePolicyList{})
		return nil
	})
}
