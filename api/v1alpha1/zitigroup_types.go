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

// ZitiGroupSpec selects ZitiIdentity resources in this namespace and gives their Ziti identities one
// shared role attribute. Ziti has no group object, so this resource owns no Ziti entity. The identity
// controller is the only writer: it merges the group role attribute into the identities it owns.
type ZitiGroupSpec struct {
	// members are ZitiIdentity names in this namespace. A name that does not exist is reported, not created.
	// +optional
	Members []string `json:"members,omitempty"`

	// selector selects ZitiIdentity resources in this namespace by label. It is combined with members.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// roleAttribute is the Ziti role attribute every member gets. It defaults to "group.<name>".
	// Policies and apps select the group with "#<roleAttribute>".
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9._:-]*$`
	// +optional
	RoleAttribute string `json:"roleAttribute,omitempty"`
}

// ZitiGroupStatus reports which identities the group resolved to.
type ZitiGroupStatus struct {
	// members are the ZitiIdentity names that currently belong to the group.
	// +optional
	Members []string `json:"members,omitempty"`

	// memberCount is the number of members. It is used by the printer column.
	// +optional
	MemberCount int `json:"memberCount,omitempty"`

	// observedGeneration is the metadata generation that this status reflects.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions report the state of the resource. Ready is the summary.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ztg,categories=ziti
// +kubebuilder:printcolumn:name="Members",type=integer,JSONPath=".status.memberCount"
// +kubebuilder:printcolumn:name="Role",type=string,JSONPath=".spec.roleAttribute"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiGroup is a Kubernetes-only group of ZitiIdentity resources.
type ZitiGroup struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiGroup
	// +required
	Spec ZitiGroupSpec `json:"spec"`

	// status defines the observed state of ZitiGroup
	// +optional
	Status ZitiGroupStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiGroupList contains a list of ZitiGroup
type ZitiGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiGroup{}, &ZitiGroupList{})
		return nil
	})
}
