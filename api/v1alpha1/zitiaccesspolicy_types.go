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
type ZitiAccessPolicySpec struct {
	// connectionRef is the name of the ZitiConnection to use. It defaults to "default".
	// +kubebuilder:default=default
	// +optional
	ConnectionRef string `json:"connectionRef,omitempty"`

	// zitiName defaults to <namespace>.<name>. The Dial policy is <zitiName>.dial and the router policy is <zitiName>.erp.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1000
	// +optional
	ZitiName string `json:"zitiName,omitempty"`

	// identityRoles select the identities that may dial. Roles follow the connection roleScope.
	// +kubebuilder:validation:MinItems=1
	IdentityRoles []string `json:"identityRoles"`

	// serviceRoles select the services they may dial. Roles follow the connection roleScope.
	// +kubebuilder:validation:MinItems=1
	ServiceRoles []string `json:"serviceRoles"`

	// postureCheckRoles add posture checks to the Dial policy.
	// +optional
	PostureCheckRoles []string `json:"postureCheckRoles,omitempty"`

	// edgeRouters are router names. When set, an edge router policy gives the identities access to them.
	// +optional
	EdgeRouters []string `json:"edgeRouters,omitempty"`

	// deletionPolicy Delete removes the Ziti entities when this resource is deleted.
	// Orphan removes only the operator tags and keeps the entities. It cannot change after creation.
	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Enum=Delete;Orphan
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

type ZitiAccessPolicyStatus struct {
	// +optional
	ZitiName string `json:"zitiName,omitempty"`
	// +optional
	DialPolicyID string `json:"dialPolicyId,omitempty"`
	// +optional
	EdgeRouterPolicyID string `json:"edgeRouterPolicyId,omitempty"`
	// identities is how many identities the Dial policy selects now.
	// +optional
	Identities int32 `json:"identities,omitempty"`
	// services is how many services the Dial policy selects now.
	// +optional
	Services int32 `json:"services,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ztap,categories=ziti
// +kubebuilder:printcolumn:name="Ziti Name",type=string,JSONPath=".status.zitiName"
// +kubebuilder:printcolumn:name="Identities",type=integer,JSONPath=".status.identities"
// +kubebuilder:printcolumn:name="Services",type=integer,JSONPath=".status.services"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiAccessPolicy is the Schema for the zitiaccesspolicies API
type ZitiAccessPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiAccessPolicy
	// +required
	Spec ZitiAccessPolicySpec `json:"spec"`

	// status defines the observed state of ZitiAccessPolicy
	// +optional
	Status ZitiAccessPolicyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiAccessPolicyList contains a list of ZitiAccessPolicy
type ZitiAccessPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiAccessPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiAccessPolicy{}, &ZitiAccessPolicyList{})
		return nil
	})
}
