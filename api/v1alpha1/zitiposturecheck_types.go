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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ZitiPostureCheckSpec is one Ziti posture check. type selects the body shape; data carries the
// type-specific fields from the Ziti Edge Management API.
type ZitiPostureCheckSpec struct {
	EntitySpec `json:",inline"`

	// type is the posture check type.
	// +kubebuilder:validation:Enum=OS;DOMAIN;MAC;PROCESS;PROCESS_MULTI;MFA
	Type string `json:"type"`

	// roleAttributes are the groups this posture check belongs to. They follow the connection roleScope.
	// +optional
	RoleAttributes []string `json:"roleAttributes,omitempty"`

	// data is the type-specific body. For OS use operatingSystems, for DOMAIN use domains, for MAC use
	// macAddresses, for PROCESS use process, for PROCESS_MULTI use processes and semantic, and for MFA use
	// the MFA properties such as promptOnUnlock and timeoutSeconds.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	// +optional
	Data apiextensionsv1.JSON `json:"data,omitzero"`
}

type ZitiPostureCheckStatus struct {
	EntityStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ztposture,categories=ziti
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Ziti ID",type=string,JSONPath=".status.zitiId",priority=1
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiPostureCheck is a Ziti posture check, one to one.
type ZitiPostureCheck struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiPostureCheck
	// +required
	Spec ZitiPostureCheckSpec `json:"spec"`

	// status defines the observed state of ZitiPostureCheck
	// +optional
	Status ZitiPostureCheckStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiPostureCheckList contains a list of ZitiPostureCheck
type ZitiPostureCheckList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiPostureCheck `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiPostureCheck{}, &ZitiPostureCheckList{})
		return nil
	})
}
