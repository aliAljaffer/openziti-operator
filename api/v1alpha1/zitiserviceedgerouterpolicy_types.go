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
type ZitiServiceEdgeRouterPolicySpec struct {
	EntitySpec `json:",inline"`

	// semantic is AnyOf (a service needs one of the roles) or AllOf (it needs all of them).
	// +kubebuilder:default=AnyOf
	// +kubebuilder:validation:Enum=AnyOf;AllOf
	// +optional
	Semantic string `json:"semantic,omitempty"`

	// serviceRoles select the services. "#attr" selects by role attribute, "#all" every service, "@name" one service.
	// +kubebuilder:validation:MinItems=1
	ServiceRoles []string `json:"serviceRoles"`

	// edgeRouterRoles select the edge routers that carry the services. "#attr" selects by role attribute,
	// "#all" every router, "@name" one router.
	// +kubebuilder:validation:MinItems=1
	EdgeRouterRoles []string `json:"edgeRouterRoles"`
}

type ZitiServiceEdgeRouterPolicyStatus struct {
	EntityStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ztserp,categories=ziti
// +kubebuilder:printcolumn:name="Ziti ID",type=string,JSONPath=".status.zitiId",priority=1
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiServiceEdgeRouterPolicy is a Ziti service edge router policy, one to one. It says which routers may carry a service.
type ZitiServiceEdgeRouterPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiServiceEdgeRouterPolicy
	// +required
	Spec ZitiServiceEdgeRouterPolicySpec `json:"spec"`

	// status defines the observed state of ZitiServiceEdgeRouterPolicy
	// +optional
	Status ZitiServiceEdgeRouterPolicyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiServiceEdgeRouterPolicyList contains a list of ZitiServiceEdgeRouterPolicy
type ZitiServiceEdgeRouterPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiServiceEdgeRouterPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiServiceEdgeRouterPolicy{}, &ZitiServiceEdgeRouterPolicyList{})
		return nil
	})
}
