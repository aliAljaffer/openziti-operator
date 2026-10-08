/*
Copyright 2026 The openziti-operator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:validation:XValidation:rule="self.identityRef == oldSelf.identityRef",message="identityRef is immutable"
// +kubebuilder:validation:XValidation:rule="self.service == oldSelf.service",message="service is immutable"
// +kubebuilder:validation:XValidation:rule="self.port == oldSelf.port",message="port is immutable"
type ZitiPortForwardSpec struct {
	// connectionRef is the name of the ZitiConnection to use. It defaults to "default".
	// +kubebuilder:default=default
	// +optional
	ConnectionRef string `json:"connectionRef,omitempty"`

	// identityRef is the name of an enrolled ZitiIdentity in this namespace.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	IdentityRef string `json:"identityRef"`

	// service is the Ziti service name to dial.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1000
	Service string `json:"service"`

	// port is the local port the proxy listens on inside the pod. Forward a workstation port to it with
	// kubectl port-forward deployment/<resource-name> <local-port>:<port>.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
}

type ZitiPortForwardStatus struct {
	// deploymentName is the Deployment that runs the proxy.
	// +optional
	DeploymentName string `json:"deploymentName,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ztpf,categories=ziti
// +kubebuilder:printcolumn:name="Service",type=string,JSONPath=".spec.service"
// +kubebuilder:printcolumn:name="Port",type=integer,JSONPath=".spec.port"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiPortForward is a temporary proxy workload used with kubectl port-forward.
type ZitiPortForward struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiPortForward
	// +required
	Spec ZitiPortForwardSpec `json:"spec"`

	// status defines the observed state of ZitiPortForward
	// +optional
	Status ZitiPortForwardStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiPortForwardList contains a list of ZitiPortForward
type ZitiPortForwardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiPortForward `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiPortForward{}, &ZitiPortForwardList{})
		return nil
	})
}
