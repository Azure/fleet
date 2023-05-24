/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={fleet},shortName=rb
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Joined")].status`,name="Joined",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// ResourceBinding is a resource created in the hub cluster to represent a member cluster within a fleet.
type ResourceBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of MemberCluster.
	// +required
	Spec ResourceBindingSpec `json:"spec"`

	// The observed status of MemberCluster.
	// +optional
	Status ResourceBindingStatus `json:"status,omitempty"`
}

// ResourceBindingSpec defines the desired state of ResourceBinding.
type ResourceBindingSpec struct {
	// ResourceSnapshotName is the name of the resource snapshot to which
	// that this resource binding points to within the same namespace.
	ResourceSnapshotIndex int32 `json:"resourceSnapshotIndex"`

	// TargetCluster is the name of the cluster that the scheduler assigns the resources to.
	TargetCluster string `json:"targetCluster"`
}

// ResourceBindingStatus represents the current status of a ResourceBinding.
type ResourceBindingStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type

	// Conditions is an array of current observed conditions for ResourceBinding.
	// +optional
	Conditions []metav1.Condition `json:"conditions"`
}

// ResourceBindingConditionType identifies a specific condition of the ResourceBinding.
type ResourceBindingConditionType string

const (
	// Bound indicates the bound condition of the given resources.
	// Its condition status can be one of the following:
	// - "True" means the corresponding work CR is created in the target cluster's namespace.
	// - "False" means the corresponding work CR is not created yet.
	// - "Unknown" means it is unknown.
	ResourceBindingBound ResourceBindingConditionType = "Bound"

	// Applied indicates the applied condition of the given resources.
	// Its condition status can be one of the following:
	// - "True" means all the resources are created in the target cluster.
	// - "False" means not all the resources are created in the target cluster yet.
	// - "Unknown" means it is unknown.
	ResourceBindingApplied ResourceBindingConditionType = "Applied"
)

// ResourceBindingList is a collection of ResourceBinding.
type ResourceBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// items is the list of ResourceBindings.
	Items []ResourceBinding `json:"items"`
}
