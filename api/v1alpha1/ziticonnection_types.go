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

type RoleScope string

const (
	RoleScopeNamespaced RoleScope = "Namespaced"
	RoleScopeGlobal     RoleScope = "Global"
)

type ConfigMapKeyRef struct {
	// namespace of the ConfigMap.
	Namespace string `json:"namespace"`
	// name of the ConfigMap.
	Name string `json:"name"`
	// key in the ConfigMap that holds the PEM certificates.
	Key string `json:"key"`
}

type SecretRef struct {
	// namespace of the Secret.
	Namespace string `json:"namespace"`
	// name of the Secret.
	Name string `json:"name"`
}

// CABundle points to the certificates that sign the Ziti controller certificate.
type CABundle struct {
	// configMapRef names the ConfigMap that holds the CA certificates.
	ConfigMapRef ConfigMapKeyRef `json:"configMapRef"`
}

// UpdbAuth reads the keys "username" and "password" from the Secret.
type UpdbAuth struct {
	// secretRef names the Secret with the keys username and password.
	SecretRef SecretRef `json:"secretRef"`
}

// CertAuth reads the keys "tls.crt" and "tls.key" from the Secret, the format cert-manager writes.
// The certificate must belong to a Ziti identity that may call the Edge Management API.
type CertAuth struct {
	// secretRef names the Secret with the keys tls.crt and tls.key.
	SecretRef SecretRef `json:"secretRef"`
}

// ConnectionAuth sets exactly one of updb and cert.
// +kubebuilder:validation:XValidation:rule="has(self.updb) != has(self.cert)",message="set exactly one of updb and cert"
type ConnectionAuth struct {
	// +optional
	Updb *UpdbAuth `json:"updb,omitempty"`
	// +optional
	Cert *CertAuth `json:"cert,omitempty"`
}

// ZitiConnectionSpec defines how the operator reaches a Ziti Edge Management API.
type ZitiConnectionSpec struct {
	// managementUrl is the Edge Management API URL, for example https://controller:443/edge/management/v1.
	// +kubebuilder:validation:Pattern=`^https://`
	ManagementURL string `json:"managementUrl"`

	// caBundle holds the CA certificates that sign the controller certificate.
	CABundle CABundle `json:"caBundle"`

	// auth is how the operator logs in. Set exactly one of updb and cert.
	Auth ConnectionAuth `json:"auth"`

	// clusterId isolates operators that share one Ziti network, for example staging and production.
	// +kubebuilder:default=default
	// +kubebuilder:validation:Pattern=`^[a-z]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	// +optional
	ClusterID string `json:"clusterId,omitempty"`

	// roleScope Namespaced rewrites "#attr" to "#<namespace>.attr". Global passes roles through unchanged.
	// +kubebuilder:default=Namespaced
	// +kubebuilder:validation:Enum=Namespaced;Global
	// +optional
	RoleScope RoleScope `json:"roleScope,omitempty"`

	// allowedNamespaces selects the namespaces that may use this connection. Empty selects all.
	// +optional
	AllowedNamespaces *metav1.LabelSelector `json:"allowedNamespaces,omitempty"`

	// hostingRouters are the router names a ZitiApp may bind through. The first entry is the default.
	// +kubebuilder:validation:MinItems=1
	HostingRouters []string `json:"hostingRouters"`

	// entryRouters are the router names that ZitiApp entryRouters and ZitiAccessPolicy edgeRouters may use,
	// together with defaultEdgeRouters. Empty allows any router.
	// +optional
	EntryRouters []string `json:"entryRouters,omitempty"`

	// defaultEdgeRouters are router names added to every service edge router policy.
	// +optional
	DefaultEdgeRouters []string `json:"defaultEdgeRouters,omitempty"`
}

type ZitiConnectionStatus struct {
	// +optional
	ControllerVersion string `json:"controllerVersion,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ztconn,categories=ziti
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=".spec.managementUrl"
// +kubebuilder:printcolumn:name="Connected",type=string,JSONPath=".status.conditions[?(@.type=='Connected')].status"
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=".status.controllerVersion"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiConnection is the Schema for the ziticonnections API
type ZitiConnection struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiConnection
	// +required
	Spec ZitiConnectionSpec `json:"spec"`

	// status defines the observed state of ZitiConnection
	// +optional
	Status ZitiConnectionStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiConnectionList contains a list of ZitiConnection
type ZitiConnectionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiConnection `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiConnection{}, &ZitiConnectionList{})
		return nil
	})
}
