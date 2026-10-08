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

// +kubebuilder:validation:XValidation:rule="self.connectionRef == oldSelf.connectionRef",message="connectionRef is immutable"
// +kubebuilder:validation:XValidation:rule="self.identityRef == oldSelf.identityRef",message="identityRef is immutable"
// +kubebuilder:validation:XValidation:rule="self.mode == oldSelf.mode",message="mode is immutable"
type ZitiSidecarSpec struct {
	// connectionRef is the name of the ZitiConnection to use. It defaults to "default".
	// +kubebuilder:default=default
	// +optional
	ConnectionRef string `json:"connectionRef,omitempty"`

	// identityRef is the name of the ZitiIdentity in this namespace whose identity file the tunneler uses.
	// It cannot change later.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	IdentityRef string `json:"identityRef"`

	// mode is Host for tproxy interception, which needs NET_ADMIN and usually hostNetwork, or Proxy for a
	// transparent proxy that needs neither. It cannot change later.
	// +kubebuilder:validation:Enum=Host;Proxy
	// +kubebuilder:default=Host
	// +optional
	Mode string `json:"mode,omitempty"`

	// resolver is the DNS upstream the tunneler uses. The default is the cluster DNS, which the image default
	// of 127.0.0.1 cannot reach.
	// +kubebuilder:default="udp://kube-dns.kube-system.svc.cluster.local:53"
	// +kubebuilder:validation:MaxLength=253
	// +optional
	Resolver string `json:"resolver,omitempty"`

	// servicePollRate is how often the tunneler asks Ziti for service changes, in seconds.
	// +kubebuilder:default=15
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3600
	// +optional
	ServicePollRate int32 `json:"servicePollRate,omitempty"`

	// manifestSecretRef names the Secret that receives the patch under the key "sidecar-patch.yaml".
	// Without it the operator writes nothing and only reports.
	// +optional
	ManifestSecretRef *SecretRef `json:"manifestSecretRef,omitempty"`
}

type ZitiSidecarStatus struct {
	// zitiName is the identity name in Ziti.
	// +optional
	ZitiName string `json:"zitiName,omitempty"`
	// identityEnrolled is true once the identity has an identity file for the tunneler to use.
	// +optional
	IdentityEnrolled bool `json:"identityEnrolled,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ztc,categories=ziti
// +kubebuilder:printcolumn:name="Identity",type=string,JSONPath=".spec.identityRef"
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=".spec.mode"
// +kubebuilder:printcolumn:name="Identity Ready",type=boolean,JSONPath=".status.identityEnrolled"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiSidecar is the Schema for the zitisidecars API
type ZitiSidecar struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiSidecar
	// +required
	Spec ZitiSidecarSpec `json:"spec"`

	// status defines the observed state of ZitiSidecar
	// +optional
	Status ZitiSidecarStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiSidecarList contains a list of ZitiSidecar
type ZitiSidecarList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiSidecar `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiSidecar{}, &ZitiSidecarList{})
		return nil
	})
}
