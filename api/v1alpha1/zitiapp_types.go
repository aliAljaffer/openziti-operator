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
	"k8s.io/apimachinery/pkg/util/intstr"
)

type ManagementPolicy string

const (
	ManagementManage  ManagementPolicy = "Manage"
	ManagementAdopt   ManagementPolicy = "Adopt"
	ManagementObserve ManagementPolicy = "Observe"
)

type DeletionPolicy string

const (
	DeletionPolicyDelete DeletionPolicy = "Delete"
	DeletionPolicyOrphan DeletionPolicy = "Orphan"
)

// +kubebuilder:validation:XValidation:rule="has(self.selector) || (has(self.addresses) && size(self.addresses) > 0 && has(self.ports) && size(self.ports) > 0)",message="expose needs addresses and ports unless selector is set"
// Expose is how clients reach the app.
type Expose struct {
	// addresses are the hostnames, IPs, or CIDRs clients dial.
	// When selector is set and addresses are empty, the operator uses the DNS names of selected Services.
	// +kubebuilder:validation:MaxItems=16
	// +optional
	Addresses []string `json:"addresses,omitempty"`

	// ports are port numbers or "low-high" ranges, for example [443, "8000-8005"]. These are the ports clients
	// dial. The operator checks range syntax and reports InvalidSpec. When selector is set and ports are empty,
	// the operator uses the selected Services' ports. See targets[].exposedPorts to route a subset to another backend.
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:items:XValidation:rule="type(self) != int || (self >= 1 && self <= 65535)",message="a port is 1-65535"
	// +optional
	Ports []intstr.IntOrString `json:"ports,omitempty"`

	// selector selects Kubernetes Services in this namespace. When set, the operator derives default
	// addresses, ports, and targets from the matching Services. Do not set targets with selector.
	// +kubebuilder:validation:XValidation:rule="(has(self.matchLabels) && size(self.matchLabels) > 0) || (has(self.matchExpressions) && size(self.matchExpressions) > 0)",message="selector must have at least one label or expression"
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// protocols the clients may use. The targets always accept the same protocols.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:Enum=tcp;udp
	// +optional
	Protocols []string `json:"protocols,omitempty"`
}

// Target is one place the app runs. Set exactly one of address and kubernetesService.
// +kubebuilder:validation:XValidation:rule="has(self.address) != has(self.kubernetesService)",message="set exactly one of address and kubernetesService"
// +kubebuilder:validation:XValidation:rule="!has(self.kubernetesService) || has(self.port)",message="port is required with kubernetesService"
type Target struct {
	// address is an IP or hostname the hosting router can reach.
	// +optional
	Address string `json:"address,omitempty"`

	// kubernetesService is a Service name. It defaults to this namespace.
	// +optional
	KubernetesService string `json:"kubernetesService,omitempty"`

	// namespace is the namespace of kubernetesService. It defaults to the ZitiApp namespace. A
	// cross-namespace target needs the annotation ziti.alialjaffer.com/allow-expose=true on the Service.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// port is where the app is actually served, the backend port the router dials. It is one number per target.
	// Without it the router uses the port the client dialed. Think of "exposedPorts:port" like "docker run -p".
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// exposedPorts are the client-dialed ports this target is exposed on. A client dials one of these and reaches
	// port. It is a subset of expose.ports and sorts targets into groups: targets with the same set become one
	// Ziti service. When empty the target is exposed on every exposed port. Set it to send some exposed ports to
	// another backend port or address, for example the TLS port. Every exposed port must be served by exactly
	// one group. The operator checks this and reports InvalidSpec.
	// +kubebuilder:validation:MaxItems=16
	// +optional
	ExposedPorts []intstr.IntOrString `json:"exposedPorts,omitempty"`

	// cost ranks targets. Ziti prefers the lowest cost. Targets with equal cost share the load.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Cost int32 `json:"cost,omitempty"`

	// precedence ranks this target against others: required, default, or failed.
	// +kubebuilder:validation:Enum=required;default;failed
	// +optional
	Precedence string `json:"precedence,omitempty"`
}

// Allow says who may connect. Groups and identities are combined.
type Allow struct {
	// groups are identity role attributes. Every identity with one of them may connect.
	// +optional
	Groups []string `json:"groups,omitempty"`

	// identities are names of identities in Ziti. They may exist outside Kubernetes.
	// A name that does not exist yet is reported in the AccessResolved condition.
	// +optional
	Identities []string `json:"identities,omitempty"`

	// postureCheckRoles are posture check role attributes that the Dial policy requires. "#attr" selects
	// by role attribute and follows the connection roleScope. Use ZitiAccessPolicy for "@name" selectors.
	// +optional
	PostureCheckRoles []string `json:"postureCheckRoles,omitempty"`
}

// AppService sets service-level Ziti fields that the app would otherwise default.
type AppService struct {
	// encryptionRequired makes the traffic end-to-end encrypted. It defaults to true.
	// +optional
	EncryptionRequired *bool `json:"encryptionRequired,omitempty"`

	// terminatorStrategy is how Ziti picks a terminator. It defaults to smartrouting.
	// +optional
	TerminatorStrategy string `json:"terminatorStrategy,omitempty"`

	// maxIdleTimeMillis closes a circuit after this idle time. Zero means never.
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxIdleTimeMillis int64 `json:"maxIdleTimeMillis,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.zitiName) || (has(self.zitiName) && self.zitiName == oldSelf.zitiName)",message="zitiName is immutable"
// +kubebuilder:validation:XValidation:rule="self.deletionPolicy == oldSelf.deletionPolicy",message="deletionPolicy is immutable"
// +kubebuilder:validation:XValidation:rule="self.managementPolicy == 'Observe' || (has(self.expose) && (has(self.expose.selector) || (has(self.targets) && size(self.targets) > 0)))",message="expose and targets or expose.selector are required unless managementPolicy is Observe"
// +kubebuilder:validation:XValidation:rule="!has(self.expose) || !has(self.expose.selector) || !has(self.targets) || size(self.targets) == 0",message="expose.selector derives targets; do not set targets"
type ZitiAppSpec struct {
	// connectionRef is the name of the ZitiConnection to use. It defaults to "default".
	// +kubebuilder:default=default
	// +optional
	ConnectionRef string `json:"connectionRef,omitempty"`

	// zitiName is the service name in Ziti. It defaults to <namespace>.<name>.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1000
	// +optional
	ZitiName string `json:"zitiName,omitempty"`

	// managementPolicy Manage creates and updates the Ziti entities. Adopt does the same, and also takes over
	// existing entities that have the names this app would create. It updates them with a PATCH, so fields the spec cannot set
	// (such as posture check roles) stay. Config data is replaced as a whole.
	// Adopted entities are released on delete, never deleted. Observe only reads the existing service
	// named zitiName and reports its status. It never writes to Ziti.
	// +kubebuilder:default=Manage
	// +kubebuilder:validation:Enum=Manage;Adopt;Observe
	// +optional
	ManagementPolicy ManagementPolicy `json:"managementPolicy,omitempty"`

	// deletionPolicy Delete removes the Ziti entities when this resource is deleted.
	// Orphan removes only the operator tags and keeps the entities. It cannot change after creation.
	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Enum=Delete;Orphan
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`

	// expose is how clients reach the app. It is required unless managementPolicy is Observe.
	// +optional
	Expose Expose `json:"expose,omitzero"`

	// targets are the places the app runs. Each one becomes a terminator in Ziti. Set targets[].port to choose
	// the backend port and targets[].exposedPorts to choose which client ports the target serves. Targets with the
	// same targets[].exposedPorts become one Ziti service; different sets become several.
	// +kubebuilder:validation:MaxItems=16
	// +optional
	Targets []Target `json:"targets,omitempty"`

	// allow creates a Dial policy for this app.
	// +optional
	Allow Allow `json:"allow,omitempty"`

	// memberOf are groups the app belongs to. Existing policies that grant access to a group apply to it.
	// Groups follow the connection roleScope.
	// +optional
	MemberOf []string `json:"memberOf,omitempty"`

	// hostedBy is the router that runs the targets. It must be in the connection hostingRouters.
	// It defaults to the first entry.
	// +optional
	HostedBy string `json:"hostedBy,omitempty"`

	// hostedByAnyOf are additional routers that may host the same targets. Together with hostedBy they
	// remove the single-router failure mode. Every name must be in the connection hostingRouters.
	// +optional
	HostedByAnyOf []string `json:"hostedByAnyOf,omitempty"`

	// service sets service-level Ziti fields. Empty keeps the current defaults.
	// +optional
	Service AppService `json:"service,omitzero"`

	// additionalConfigs are names of Ziti configs to attach to every service this app creates. Use them
	// for config types that the app does not generate itself.
	// +optional
	AdditionalConfigs []string `json:"additionalConfigs,omitempty"`

	// entryRouters are the routers clients connect through. The hosting router is always allowed for the app.
	// When set, an edge router policy gives the allowed identities access to them.
	// +optional
	EntryRouters []string `json:"entryRouters,omitempty"`
}

type EntityIDs struct {
	// service is the Ziti service ID.
	Service string `json:"service,omitempty"`
	// intercept is the intercept config ID.
	Intercept string `json:"intercept,omitempty"`
	// host is the host config ID.
	Host string `json:"host,omitempty"`
	// bind is the Bind service policy ID.
	Bind string `json:"bind,omitempty"`
	// serp is the service edge router policy ID.
	SERP string `json:"serp,omitempty"`
	// dial is the Dial service policy ID.
	Dial string `json:"dial,omitempty"`
	// erp is the edge router policy ID.
	ERP string `json:"erp,omitempty"`
}

type Terminator struct {
	// router is the name of the edge router that hosts the service.
	Router string `json:"router"`
}

// ServiceStatus reports one Ziti service that an app owns.
type ServiceStatus struct {
	// name is the service name in Ziti.
	// +optional
	Name string `json:"name,omitempty"`

	// ids are the Ziti ids of the entities of this service.
	// +optional
	IDs EntityIDs `json:"ids,omitzero"`

	// terminators are the routers that host this service.
	// +optional
	Terminators []Terminator `json:"terminators,omitempty"`
}

type ZitiAppStatus struct {
	// zitiName is the service name in Ziti.
	// +optional
	ZitiName string `json:"zitiName,omitempty"`
	// ids are the Ziti IDs of the app entities.
	// +optional
	IDs EntityIDs `json:"ids,omitzero"`
	// terminators are the routers that host the app.
	// +optional
	Terminators []Terminator `json:"terminators,omitempty"`
	// observedGeneration is the metadata generation that this status reflects.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// services are the Ziti services this app owns, one per distinct target port set.
	// For a single service this repeats zitiName, ids, and terminators.
	// +optional
	Services []ServiceStatus `json:"services,omitempty"`

	// conditions report the state of the resource. Ready is the summary.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ztapp,categories=ziti
// +kubebuilder:webhook:path=/validate-ziti-alialjaffer-com-v1alpha1-zitiapp,mutating=false,failurePolicy=Ignore,sideEffects=None,groups=ziti.alialjaffer.com,resources=zitiapps,verbs=create;update,versions=v1alpha1,name=vzitiapp.kb.io,admissionReviewVersions=v1
// +kubebuilder:printcolumn:name="Policy",type=string,JSONPath=".spec.managementPolicy"
// +kubebuilder:printcolumn:name="Ziti Name",type=string,JSONPath=".status.zitiName"
// +kubebuilder:printcolumn:name="Hosted",type=string,JSONPath=".status.conditions[?(@.type=='Hosted')].status"
// +kubebuilder:printcolumn:name="Dialable",type=string,JSONPath=".status.conditions[?(@.type=='Dialable')].status"
// +kubebuilder:printcolumn:name="RoutePath",type=string,JSONPath=".status.conditions[?(@.type=='RoutePath')].status"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].message",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ZitiApp is the Schema for the zitiapps API
type ZitiApp struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ZitiApp
	// +required
	Spec ZitiAppSpec `json:"spec"`

	// status defines the observed state of ZitiApp
	// +optional
	Status ZitiAppStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ZitiAppList contains a list of ZitiApp
type ZitiAppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ZitiApp `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ZitiApp{}, &ZitiAppList{})
		return nil
	})
}
