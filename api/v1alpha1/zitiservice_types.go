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

type DeletionPolicy string

const (
	DeletionPolicyDelete DeletionPolicy = "Delete"
	DeletionPolicyOrphan DeletionPolicy = "Orphan"
)

type Intercept struct {
	// +kubebuilder:validation:MinItems=1
	Addresses []string `json:"addresses"`

	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:Minimum=1
	// +kubebuilder:validation:items:Maximum=65535
	Ports []int32 `json:"ports"`

	// +kubebuilder:default={tcp}
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:Enum=tcp;udp
	// +optional
	Protocols []string `json:"protocols,omitempty"`
}

type ServiceRef struct {
	Name string `json:"name"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
}

// Host sets the target. Set exactly one of serviceRef and address.
// +kubebuilder:validation:XValidation:rule="has(self.serviceRef) != has(self.address)",message="set exactly one of serviceRef and address"
// +kubebuilder:validation:XValidation:rule="has(self.address) == has(self.port)",message="port is required with address"
type Host struct {
	// +optional
	ServiceRef *ServiceRef `json:"serviceRef,omitempty"`
	// +optional
	Address string `json:"address,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// forwardProtocol true hosts every protocol in intercept.protocols. False hosts only the first one.
	// +optional
	ForwardProtocol bool `json:"forwardProtocol,omitempty"`
}

type Access struct {
	// identityRoles creates a Dial policy for this service when set.
	// +optional
	IdentityRoles []string `json:"identityRoles,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.zitiName) || (has(self.zitiName) && self.zitiName == oldSelf.zitiName)",message="zitiName is immutable"
// +kubebuilder:validation:XValidation:rule="self.deletionPolicy == oldSelf.deletionPolicy",message="deletionPolicy is immutable"
// +kubebuilder:validation:XValidation:rule="!has(self.intercept.protocols) || size(self.intercept.protocols) < 2 || (has(self.host.forwardProtocol) && self.host.forwardProtocol)",message="more than one intercept protocol requires host.forwardProtocol"
type ZitiServiceSpec struct {
	// +kubebuilder:default=default
	// +optional
	ConnectionRef string `json:"connectionRef,omitempty"`

	// zitiName defaults to <namespace>.<name>.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1000
	// +optional
	ZitiName string `json:"zitiName,omitempty"`

	// +optional
	RoleAttributes []string `json:"roleAttributes,omitempty"`

	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Enum=Delete;Orphan
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`

	Intercept Intercept `json:"intercept"`
	Host      Host      `json:"host"`

	// hostingRouter must be in the connection's hostingRouters. It defaults to the first entry.
	// +optional
	HostingRouter string `json:"hostingRouter,omitempty"`

	// edgeRouters are extra entry routers. The hosting router is always included.
	// +optional
	EdgeRouters []string `json:"edgeRouters,omitempty"`

	// +optional
	Access Access `json:"access,omitempty"`
}

type EntityIDs struct {
	Service   string `json:"service,omitempty"`
	Intercept string `json:"intercept,omitempty"`
	Host      string `json:"host,omitempty"`
	Bind      string `json:"bind,omitempty"`
	SERP      string `json:"serp,omitempty"`
	Dial      string `json:"dial,omitempty"`
}

type Terminator struct {
	Router string `json:"router"`
}

type ZitiServiceStatus struct {
	// +optional
	ZitiName string `json:"zitiName,omitempty"`
	// +optional
	IDs EntityIDs `json:"ids,omitzero"`
	// +optional
	Terminators []Terminator `json:"terminators,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ztsvc
// +kubebuilder:printcolumn:name="Ziti Name",type=string,JSONPath=".status.zitiName"
// +kubebuilder:printcolumn:name="Hosted",type=string,JSONPath=".status.conditions[?(@.type=='Hosted')].status"
// +kubebuilder:printcolumn:name="Dialable",type=string,JSONPath=".status.conditions[?(@.type=='Dialable')].status"
// +kubebuilder:printcolumn:name="RoutePath",type=string,JSONPath=".status.conditions[?(@.type=='RoutePath')].status"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiService is the Schema for the zitiservices API
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
