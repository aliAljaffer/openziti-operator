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
type ZitiServiceSpec struct {
	EntitySpec `json:",inline"`

	// roleAttributes are the groups the service belongs to. They follow the connection roleScope.
	// +optional
	RoleAttributes []string `json:"roleAttributes,omitempty"`

	// configs are the names of the Ziti configs that the service uses, for example those made by ZitiConfig.
	// +optional
	Configs []string `json:"configs,omitempty"`

	// terminatorStrategy is how Ziti picks a terminator: smartrouting, random, or another strategy your controller knows.
	// +kubebuilder:default=smartrouting
	// +optional
	TerminatorStrategy string `json:"terminatorStrategy,omitempty"`

	// encryptionRequired makes the traffic end-to-end encrypted.
	// +kubebuilder:default=true
	// +optional
	EncryptionRequired *bool `json:"encryptionRequired,omitempty"`

	// maxIdleTimeMillis closes a circuit after this idle time. Zero means never.
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxIdleTimeMillis int64 `json:"maxIdleTimeMillis,omitempty"`
}

type ZitiServiceStatus struct {
	EntityStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ztsvc,categories=ziti
// +kubebuilder:printcolumn:name="Ziti ID",type=string,JSONPath=".status.zitiId",priority=1
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiService is a Ziti service, one to one. Use ZitiApp to publish an app with all its policies.
type ZitiService struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiService
	// +required
	Spec ZitiServiceSpec `json:"spec"`

	// status defines the observed state of ZitiService
	// +optional
	Status ZitiServiceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiServiceList contains a list of ZitiService
type ZitiServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiService `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiService{}, &ZitiServiceList{})
		return nil
	})
}
