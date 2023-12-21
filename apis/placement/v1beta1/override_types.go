/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",categories={fleet,fleet-placement}
// +kubebuilder:subresource:status
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Override defines a group of override policies about how to override the selected resources and how to apply these resources
// to the target clusters.
// Note: override may fail, and it will be reflected on the placement status.
type Override struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ClusterResourcePlacement.
	// +required
	Spec OverrideSpec `json:"spec"`
}

// OverrideSpec defines the desired state of the Override.
type OverrideSpec struct {
	// If none of the ClusterResourceSelectors and ResourceSelectors are specified, it means selecting all resources.
	// The ClusterResourceSelectors and ResourceSelectors are `ORed`.

	// ClusterResourceSelectors is an array of selectors used to select cluster scoped resources. The selectors are `ORed`.
	// If a namespace is selected, ALL the resources under the namespace are selected automatically.
	// You can have 1-100 selectors.
	// +optional
	ClusterResourceSelectors []ClusterResourceSelector `json:"clusterResourceSelectors,omitempty"`

	// ResourceSelectors is an array of selectors used to select namespace scoped resources. The selectors are `ORed`.
	// You can have 1-100 selectors.
	// +optional
	ResourceSelectors []ResourceSelector `json:"resourceSelectors,omitempty"`

	// ClusterSelectors selects the target clusters.
	// The resources will be overridden before applying to the matching clusters.
	// If ClusterSelector is not set, it means selecting ALL the member clusters.
	// +optional
	ClusterSelector *ClusterSelector `json:"clusterSelector,omitempty"`

	// OverridePolices defines how to override the selected resources.
	// +optional
	OverridePolices *OverridePolices `json:"overridePolices,omitempty"`

	// ApplyMode defines how to apply resources if the resources exist on the target clusters.
	// It can be "CreateOnly", "ServerSideApply" or "ServerSideApplyWithForceConflicts". Default is CreateOnly.
	// +kubebuilder:validation:Enum=CreateOnly;ServerSideApply;ServerSideApplyWithForceConflicts
	// +kubebuilder:default=CreateOnly
	// +optional
	ApplyMode ApplyMode `json:"applyMode,omitempty"`

	// Priority is an integer defining the relative importance of this override compared to others.
	// Lower number is considered higher priority.
	// And resources will be applied by order from lower priority to higher.
	// That means override with the lowest value will win.
	// If multiple overrides have the same priority value, it will be sorted by name.
	// For example, "override-1" will be applied first and then "override-2".
	//
	// +optional
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=500
	Priority int32 `json:"priority,omitempty"`
}

// ApplyMode defines how resources are applied on the target clusters.
type ApplyMode string

const (
	// ApplyModeCreateOnly creates the resources on the target clusters and will fail if the resources exist on the target
	// cluster.
	ApplyModeCreateOnly ApplyMode = "CreateOnly"
	// ApplyModeServerSideApply applies the resources on the target cluster using server-side apply.
	// https://kubernetes.io/docs/reference/using-api/server-side-apply/
	ApplyModeServerSideApply ApplyMode = "ServerSideApply"
	// ApplyModeServerSideApplyWithForceConflicts applies the resources on the target cluster using server-side apply with
	// force-conflicts option. It forces the operation to succeed when resolving the conflicts.
	ApplyModeServerSideApplyWithForceConflicts ApplyMode = "ServerSideApplyWithForceConflicts"
)

// OverridePolices defines a group of override polices applied on the resources.
// More is to be added.
type OverridePolices struct {
	// JSONPatchOverrides defines a list of JSON patch override rules.
	// +optional
	JSONPatchOverrides []JSONPatchOverride `json:"jsonPatchOverrides,omitempty"`
}

// JSONPatchOverride applies a JSON patch on the selected resources following [RFC 6902](https://datatracker.ietf.org/doc/html/rfc6902).
type JSONPatchOverride struct {
	// Operator defines the operation on the target field.
	// +kubebuilder:validation:Enum=Add;Remove;Replace
	// +required
	Operator JSONPatchOverrideOperator `json:"operator"`
	// Path defines the target location.
	// Note: override will fail if the resource path does not exist.
	// +required
	Path string `json:"path"`
	// Value defines the content to be applied on the target location.
	// Value should be empty when operator is Remove.
	// +optional
	Value string `json:"value,omitempty"`
}

// JSONPatchOverrideOperator defines the supported JSON patch operator.
type JSONPatchOverrideOperator string

const (
	// JSONPatchOverrideOpAdd adds the value to the target location.
	JSONPatchOverrideOpAdd JSONPatchOverrideOperator = "Add"
	// JSONPatchOverrideOpRemove removes the value from the target location.
	JSONPatchOverrideOpRemove JSONPatchOverrideOperator = "Remove"
	// JSONPatchOverrideOpReplace replaces the value at the target location with a new value.
	JSONPatchOverrideOpReplace JSONPatchOverrideOperator = "Replace"
)
