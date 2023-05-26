/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CRPTrackingLabel is the label that points to the cluster resource policy that creates a resource binding.
const CRPTrackingLabel = labelPrefix + "parentCRP"

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={fleet},shortName=rb
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ResourceBindingApplied")].status`,name="Applied",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// ClusterResourceBinding is represents a scheduling decision that binds a group of resources to a cluster.
// it must have CRPTrackingLabel that points to the cluster resource policy that creates it.
type ClusterResourceBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ClusterResourceBinding.
	// +required
	Spec ResourceBindingSpec `json:"spec"`

	// The observed status of ClusterResourceBinding.
	// +optional
	Status ResourceBindingStatus `json:"status,omitempty"`
}

// ResourceBindingSpec defines the desired state of ClusterResourceBinding.
type ResourceBindingSpec struct {
	// ResourcePolicyName is the name of the resource policy that this resource binding points to.
	ResourcePolicyName string `json:"resourcePolicyName"`

	// ResourceSnapshotIndex is the index of the resource snapshot to which
	// that this resource binding points to.
	ResourceSnapshotIndex int32 `json:"resourceSnapshotIndex"`

	// TargetCluster is the name of the cluster that the scheduler assigns the resources to.
	TargetCluster string `json:"targetCluster"`
}

// ResourceBindingStatus represents the current status of a ClusterResourceBinding.
type ResourceBindingStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type

	// Conditions is an array of current observed conditions for ClusterResourceBinding.
	// +optional
	Conditions []metav1.Condition `json:"conditions"`
}

// ResourceBindingConditionType identifies a specific condition of the ClusterResourceBinding.
type ResourceBindingConditionType string

const (
	// ResourceBindingBound indicates the bound condition of the given resources.
	// Its condition status can be one of the following:
	// - "True" means the corresponding work CR is created in the target cluster's namespace.
	// - "False" means the corresponding work CR is not created yet.
	// - "Unknown" means it is unknown.
	ResourceBindingBound ResourceBindingConditionType = "Bound"

	// ResourceBindingApplied indicates the applied condition of the given resources.
	// Its condition status can be one of the following:
	// - "True" means all the resources are created in the target cluster.
	// - "False" means not all the resources are created in the target cluster yet.
	// - "Unknown" means it is unknown.
	ResourceBindingApplied ResourceBindingConditionType = "Applied"
)

// ClusterResourceBindingList is a collection of ClusterResourceBinding.
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterResourceBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// items is the list of ClusterResourceBindings.
	Items []ClusterResourceBinding `json:"items"`
}

// SetConditions set the given conditions on the ClusterResourceBinding.
func (m *ClusterResourceBinding) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&m.Status.Conditions, c)
	}
}

// GetCondition returns the condition of the given ClusterResourceBinding.
func (m *ClusterResourceBinding) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, conditionType)
}

func init() {
	SchemeBuilder.Register(&ClusterResourceBinding{}, &ClusterResourceBindingList{})
}
