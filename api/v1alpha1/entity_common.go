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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// EntitySpec holds the fields that every kind with a one-to-one Ziti object shares.
type EntitySpec struct {
	// connectionRef is the name of the ZitiConnection to use. It defaults to "default".
	// +kubebuilder:default=default
	// +optional
	ConnectionRef string `json:"connectionRef,omitempty"`

	// zitiName is the name of the object in Ziti. It defaults to <namespace>.<name>. It cannot change later.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1000
	// +optional
	ZitiName string `json:"zitiName,omitempty"`

	// managementPolicy Manage creates the object. Adopt takes over an existing object named zitiName: it adds ownership
	// tags and updates only the fields this resource sets, and it is released on delete, never deleted.
	// Observe only reads the existing object and never writes to Ziti.
	// +kubebuilder:default=Manage
	// +kubebuilder:validation:Enum=Manage;Adopt;Observe
	// +optional
	ManagementPolicy ManagementPolicy `json:"managementPolicy,omitempty"`

	// deletionPolicy Delete removes the object from Ziti when this resource is deleted.
	// Orphan removes only the operator tags and keeps the object. It cannot change after creation.
	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Enum=Delete;Orphan
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

// EntityStatus is the status that every kind with a one-to-one Ziti object shares.
type EntityStatus struct {
	// zitiId is the ID of the object in Ziti.
	// +optional
	ZitiID string `json:"zitiId,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// EntityObject is what the shared reconciler needs from a kind with a one-to-one Ziti object.
// +kubebuilder:object:generate=false
type EntityObject interface {
	metav1.Object
	EntityCommon() *EntitySpec
	EntityState() *EntityStatus
}

func (o *ZitiConfig) EntityCommon() *EntitySpec                   { return &o.Spec.EntitySpec }
func (o *ZitiConfig) EntityState() *EntityStatus                  { return &o.Status.EntityStatus }
func (o *ZitiService) EntityCommon() *EntitySpec                  { return &o.Spec.EntitySpec }
func (o *ZitiService) EntityState() *EntityStatus                 { return &o.Status.EntityStatus }
func (o *ZitiServicePolicy) EntityCommon() *EntitySpec            { return &o.Spec.EntitySpec }
func (o *ZitiServicePolicy) EntityState() *EntityStatus           { return &o.Status.EntityStatus }
func (o *ZitiEdgeRouterPolicy) EntityCommon() *EntitySpec         { return &o.Spec.EntitySpec }
func (o *ZitiEdgeRouterPolicy) EntityState() *EntityStatus        { return &o.Status.EntityStatus }
func (o *ZitiServiceEdgeRouterPolicy) EntityCommon() *EntitySpec  { return &o.Spec.EntitySpec }
func (o *ZitiServiceEdgeRouterPolicy) EntityState() *EntityStatus { return &o.Status.EntityStatus }
