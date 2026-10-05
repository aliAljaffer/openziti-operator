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
type ZitiRouterSpec struct {
	// connectionRef is the name of the ZitiConnection to use. It defaults to "default".
	// +kubebuilder:default=default
	// +optional
	ConnectionRef string `json:"connectionRef,omitempty"`

	// zitiName is the router name in Ziti. It defaults to <clusterId>-<name>. Routers created before this default keep the resource name. It cannot change later.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1000
	// +optional
	ZitiName string `json:"zitiName,omitempty"`

	// roleAttributes are the groups the router belongs to. Edge router policies select routers by them.
	// They are used as written, whatever the connection roleScope. Only cluster admins can create a ZitiRouter.
	// +optional
	RoleAttributes []string `json:"roleAttributes,omitempty"`

	// tunnelerEnabled lets the router host and dial services itself. Routers that host services need it.
	// +optional
	TunnelerEnabled bool `json:"tunnelerEnabled,omitempty"`

	// cost makes Ziti prefer routers with a lower cost.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Cost int32 `json:"cost,omitempty"`

	// noTraversal keeps other traffic off this router. The router only serves its own edge connections.
	// +optional
	NoTraversal bool `json:"noTraversal,omitempty"`

	// disabled takes the router out of service without deleting it.
	// +optional
	Disabled bool `json:"disabled,omitempty"`

	// enrollmentSecretRef names the Secret that receives the enrollment JWT under the key "enrollment.jwt".
	// Give it to the router at install, for example as the enrollmentJwt of the ziti-router Helm chart.
	// With advertisedAddress set, the Secret also holds the keys "docker-compose.yml" and "deployment.yaml".
	// The operator removes the key once the router has enrolled, and issues a new JWT when an unused one expires.
	// Without it, the JWT stays in Ziti.
	// +optional
	EnrollmentSecretRef *SecretRef `json:"enrollmentSecretRef,omitempty"`

	// advertisedAddress is the DNS name or IP where clients and other routers reach this router. The router needs it to start.
	// With enrollmentSecretRef and this field, the operator also writes ready-made "docker-compose.yml" and
	// "deployment.yaml" files to the Secret, so the router can be started on a VM or in a Kubernetes cluster without router configuration.
	// Without it, only the JWT is written and the condition ManifestsReady says why.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9]([A-Za-z0-9.:-]*[A-Za-z0-9])?$`
	// +optional
	AdvertisedAddress string `json:"advertisedAddress,omitempty"`

	// port is the port the router listens on for clients and links. It is used in the generated manifests.
	// It must be 1024 or higher, because the generated manifests drop all capabilities.
	// +kubebuilder:default=3022
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// storageClassName is the StorageClass of the volume in the generated deployment.yaml. Empty uses the cluster default.
	// Check its reclaim policy: with Retain, deleting the volume claim leaves the volume behind.
	// +kubebuilder:validation:MaxLength=253
	// +optional
	StorageClassName string `json:"storageClassName,omitempty"`

	// deletionPolicy Delete removes the router from Ziti when this resource is deleted.
	// Orphan removes only the operator tags and keeps the router. It cannot change after creation.
	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Enum=Delete;Orphan
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

type ZitiRouterStatus struct {
	// +optional
	RouterID string `json:"routerId,omitempty"`
	// zitiName is the name the router has in Ziti. It is set once and does not change.
	// +optional
	ZitiName string `json:"zitiName,omitempty"`
	// enrolled is true once the router has enrolled with its JWT.
	// +optional
	Enrolled bool `json:"enrolled,omitempty"`
	// online is true while the router is connected to the controller.
	// +optional
	Online bool `json:"online,omitempty"`
	// enrollmentExpiresAt is when the pending JWT expires. Empty once the router has enrolled.
	// +optional
	EnrollmentExpiresAt *metav1.Time `json:"enrollmentExpiresAt,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ztrouter,categories=ziti
// +kubebuilder:printcolumn:name="Enrolled",type=boolean,JSONPath=".status.enrolled"
// +kubebuilder:printcolumn:name="Online",type=boolean,JSONPath=".status.online"
// +kubebuilder:printcolumn:name="Enrollment Expires",type=string,JSONPath=".status.enrollmentExpiresAt"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiRouter is the Schema for the zitirouters API
type ZitiRouter struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiRouter
	// +required
	Spec ZitiRouterSpec `json:"spec"`

	// status defines the observed state of ZitiRouter
	// +optional
	Status ZitiRouterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiRouterList contains a list of ZitiRouter
type ZitiRouterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiRouter `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiRouter{}, &ZitiRouterList{})
		return nil
	})
}
