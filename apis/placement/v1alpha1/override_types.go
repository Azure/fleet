/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",categories={fleet,fleet-placement}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterResourceOverride defines a group of override policies about how to override the selected cluster scope resources
// to target clusters.
type ClusterResourceOverride struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ClusterResourceOverrideSpec.
	// +required
	Spec ClusterResourceOverrideSpec `json:"spec"`
}

// ClusterResourceOverrideSpec defines the desired state of the Override.
// The ClusterResourceOverride create or update will fail when the resource has been selected by the existing ClusterResourceOverride.
// If the resource is selected by both ClusterResourceOverride and ResourceOverride, ResourceOverride will win when resolving
// conflicts.
type ClusterResourceOverrideSpec struct {
	// Placement defines whether the override is applied to a specific placement or not.
	// If set, the override will trigger the placement rollout immediately when the rollout strategy type is RollingUpdate.
	// Otherwise, it will be applied to the next rollout.
	// The recommended way is to set the placement so that the override can be rolled out immediately.
	// +optional
	Placement *PlacementRef `json:"placement,omitempty"`

	// ClusterResourceSelectors is an array of selectors used to select cluster scoped resources. The selectors are `ORed`.
	// If a namespace is selected, ALL the resources under the namespace are selected automatically.
	// LabelSelector is not supported.
	// You can have 1-20 selectors.
	// We only support Name selector for now.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	// +required
	ClusterResourceSelectors []placementv1beta1.ClusterResourceSelector `json:"clusterResourceSelectors"`

	// Policy defines how to override the selected resources on the target clusters.
	// +required
	Policy *OverridePolicy `json:"policy"`
}

// PlacementRef is the reference to a placement.
// For now, we only support ClusterResourcePlacement.
type PlacementRef struct {
	// Name is the reference to the name of placement.
	// +required
	Name string `json:"name"`
}

// OverridePolicy defines how to override the selected resources on the target clusters.
// More is to be added.
type OverridePolicy struct {
	// OverrideRules defines an array of override rules to be applied on the selected resources.
	// The order of the rules determines the override order.
	// When there are two rules selecting the same fields on the target cluster, the last one will win.
	// You can have 1-20 rules.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	// +required
	OverrideRules []OverrideRule `json:"overrideRules"`
}

// OverrideRule defines how to override the selected resources on the target clusters.
type OverrideRule struct {
	// ClusterSelectors selects the target clusters.
	// The resources will be overridden before applying to the matching clusters.
	// An empty clusterSelector selects ALL the member clusters.
	// A nil clusterSelector selects NO member clusters.
	// For now, only labelSelector is supported.
	// +optional
	ClusterSelector *placementv1beta1.ClusterSelector `json:"clusterSelector,omitempty"`

	// OverrideType defines the type of the override rules.
	// +kubebuilder:validation:Enum=JSONPatch;Delete
	// +kubebuilder:default=JSONPatch
	// +optional
	OverrideType OverrideType `json:"overrideType,omitempty"`

	// JSONPatchOverrides defines a list of JSON patch override rules.
	// This field is only allowed when OverrideType is JSONPatch.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	// +optional
	JSONPatchOverrides []JSONPatchOverride `json:"jsonPatchOverrides,omitempty"`
}

// OverrideType defines the type of Override
type OverrideType string

const (
	// JSONPatchOverrideType applies a JSON patch on the selected resources following [RFC 6902](https://datatracker.ietf.org/doc/html/rfc6902).
	JSONPatchOverrideType OverrideType = "JSONPatch"

	// DeleteOverrideType deletes the selected resources on the target clusters.
	DeleteOverrideType OverrideType = "Delete"
)

// +genclient
// +genclient:Namespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Namespaced",categories={fleet,fleet-placement}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ResourceOverride defines a group of override policies about how to override the selected namespaced scope resources
// to target clusters.
type ResourceOverride struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ResourceOverrideSpec.
	// +required
	Spec ResourceOverrideSpec `json:"spec"`
}

// ResourceOverrideSpec defines the desired state of the Override.
// The ResourceOverride create or update will fail when the resource has been selected by the existing ResourceOverride.
// If the resource is selected by both ClusterResourceOverride and ResourceOverride, ResourceOverride will win when resolving
// conflicts.
type ResourceOverrideSpec struct {
	// Placement defines whether the override is applied to a specific placement or not.
	// If set, the override will trigger the placement rollout immediately when the rollout strategy type is RollingUpdate.
	// Otherwise, it will be applied to the next rollout.
	// The recommended way is to set the placement so that the override can be rolled out immediately.
	// +optional
	Placement *PlacementRef `json:"placement,omitempty"`

	// ResourceSelectors is an array of selectors used to select namespace scoped resources. The selectors are `ORed`.
	// You can have 1-20 selectors.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	// +required
	ResourceSelectors []ResourceSelector `json:"resourceSelectors"`

	// Policy defines how to override the selected resources on the target clusters.
	// +required
	Policy *OverridePolicy `json:"policy"`
}

// ResourceSelector is used to select namespace scoped resources as the target resources to be placed.
// All the fields are `ANDed`. In other words, a resource must match all the fields to be selected.
// The resource namespace will inherit from the parent object scope.
type ResourceSelector struct {
	// Group name of the namespace-scoped resource.
	// Use an empty string to select resources under the core API group (e.g., services).
	// +required
	Group string `json:"group"`

	// Version of the namespace-scoped resource.
	// +required
	Version string `json:"version"`

	// Kind of the namespace-scoped resource.
	// +required
	Kind string `json:"kind"`

	// Name of the namespace-scoped resource.
	// +required
	Name string `json:"name"`
}

// JSONPatchOverride applies a JSON patch on the selected resources following [RFC 6902](https://datatracker.ietf.org/doc/html/rfc6902).
type JSONPatchOverride struct {
	// Operator defines the operation on the target field.
	// +kubebuilder:validation:Enum=add;remove;replace
	// +required
	Operator JSONPatchOverrideOperator `json:"op"`
	// Path defines the target location.
	// Note: override will fail if the resource path does not exist.
	// +required
	Path string `json:"path"`
	// Value defines the content to be applied on the target location.
	// Value should be empty when operator is `remove`.
	// We have reserved a few variables in this field that will be replaced by the actual values.
	// Those variables all start with `$` and are case sensitive.
	// Here is the list of currently supported variables:
	// `${MEMBER-CLUSTER-NAME}`:  this will be replaced by the name of the memberCluster CR that represents this cluster.
	// +optional
	Value apiextensionsv1.JSON `json:"value,omitempty"`
}

// JSONPatchOverrideOperator defines the supported JSON patch operator.
type JSONPatchOverrideOperator string

const (
	// JSONPatchOverrideOpAdd adds the value to the target location.
	// An example target JSON document:
	//
	//   { "foo": [ "bar", "baz" ] }
	//
	//   A JSON Patch override:
	//
	//   [
	//     { "op": "add", "path": "/foo/1", "value": "qux" }
	//   ]
	//
	//   The resulting JSON document:
	//
	//   { "foo": [ "bar", "qux", "baz" ] }
	JSONPatchOverrideOpAdd JSONPatchOverrideOperator = "add"
	// JSONPatchOverrideOpRemove removes the value from the target location.
	// An example target JSON document:
	//
	//   {
	//     "baz": "qux",
	//     "foo": "bar"
	//   }
	//   A JSON Patch override:
	//
	//   [
	//     { "op": "remove", "path": "/baz" }
	//   ]
	//
	//   The resulting JSON document:
	//
	//   { "foo": "bar" }
	JSONPatchOverrideOpRemove JSONPatchOverrideOperator = "remove"
	// JSONPatchOverrideOpReplace replaces the value at the target location with a new value.
	// An example target JSON document:
	//
	//   {
	//     "baz": "qux",
	//     "foo": "bar"
	//   }
	//
	//   A JSON Patch override:
	//
	//   [
	//     { "op": "replace", "path": "/baz", "value": "boo" }
	//   ]
	//
	//   The resulting JSON document:
	//
	//   {
	//     "baz": "boo",
	//     "foo": "bar"
	//   }
	JSONPatchOverrideOpReplace JSONPatchOverrideOperator = "replace"
)

// ClusterResourceOverrideList contains a list of ClusterResourceOverride.
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterResourceOverrideList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterResourceOverride `json:"items"`
}

// ResourceOverrideList contains a list of ResourceOverride.
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ResourceOverrideList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceOverride `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&ClusterResourceOverride{}, &ClusterResourceOverrideList{},
		&ResourceOverride{}, &ResourceOverrideList{},
	)
}
