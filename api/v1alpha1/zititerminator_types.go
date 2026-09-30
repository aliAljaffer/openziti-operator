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

// ZitiTerminatorSpec is one terminator: a router that forwards traffic of a service to an address.
// Most apps do not need it, because the host config of a ZitiApp lets the hosting router create terminators itself.
// Use it for a fixed route, for example a service that a router reaches at one address.
// +kubebuilder:validation:XValidation:rule="self.service == oldSelf.service && self.router == oldSelf.router && self.binding == oldSelf.binding",message="service, router, and binding are immutable"
type ZitiTerminatorSpec struct {
	// connectionRef is the name of the ZitiConnection to use. It defaults to "default".
	// +kubebuilder:default=default
	// +optional
	ConnectionRef string `json:"connectionRef,omitempty"`

	// service is the name of the Ziti service. It must exist. The connection needs roleScope Global, because the name
	// can be any service in the network.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1000
	Service string `json:"service"`

	// router is the name of the edge router that forwards the traffic.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1000
	Router string `json:"router"`

	// address is where the router sends the traffic, for example tcp:10.0.0.5:8443 or udp:10.0.0.5:53.
	// +kubebuilder:validation:Pattern=`^(tcp|udp):[^:\s]+:[0-9]{1,5}$`
	Address string `json:"address"`

	// binding is the terminator binding. Keep the default unless you know that you need another.
	// +kubebuilder:default=transport
	// +kubebuilder:validation:MinLength=1
	// +optional
	Binding string `json:"binding,omitempty"`

	// cost makes Ziti prefer terminators with a lower cost.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Cost int32 `json:"cost,omitempty"`

	// precedence orders terminators: required first, default next, failed last.
	// +kubebuilder:default=default
	// +kubebuilder:validation:Enum=default;required;failed
	// +optional
	Precedence string `json:"precedence,omitempty"`

	// deletionPolicy Delete removes the terminator from Ziti when this resource is deleted.
	// Orphan removes only the operator tags and keeps the terminator. It cannot change after creation.
	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Enum=Delete;Orphan
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

// ZitiTerminatorStatus is the observed state of a ZitiTerminator.
type ZitiTerminatorStatus struct {
	// zitiId is the ID of the terminator in Ziti.
	// +optional
	ZitiID string `json:"zitiId,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ztterm,categories=ziti
// +kubebuilder:printcolumn:name="Service",type=string,JSONPath=".spec.service"
// +kubebuilder:printcolumn:name="Router",type=string,JSONPath=".spec.router"
// +kubebuilder:printcolumn:name="Address",type=string,JSONPath=".spec.address"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiTerminator is the Schema for the zititerminators API
type ZitiTerminator struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiTerminator
	// +required
	Spec ZitiTerminatorSpec `json:"spec"`

	// status defines the observed state of ZitiTerminator
	// +optional
	Status ZitiTerminatorStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiTerminatorList contains a list of ZitiTerminator
type ZitiTerminatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiTerminator `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiTerminator{}, &ZitiTerminatorList{})
		return nil
	})
}
