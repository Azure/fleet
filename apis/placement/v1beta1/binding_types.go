/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={fleet},shortName=rb
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ResourceBindingApplied")].status`,name="Applied",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// ClusterResourceBinding represents a scheduling decision that binds a group of resources to a cluster.
// It MUST have a label named `CRPTrackingLabel` that points to the cluster resource policy that creates it.
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
	// The desired state of the binding. Possible values: Creating, Active, Deleting.
	// +required
	State BindingState `json:"state"`

	// ResourceSnapshotName is the name of the resource snapshot that this resource binding points to.
	// If the resources are divided into multiple snapshots because of the resource size limit,
	// it points to the name of the leading snapshot of the index group.
	ResourceSnapshotName string `json:"resourceSnapshotName"`

	// TargetCluster is the name of the cluster that the scheduler assigns the resources to.
	TargetCluster string `json:"targetCluster"`
}

// BindingState is the state of the binding
type BindingState string

const (
	// BindingStateScheduled means the binding is scheduled but need to be bound to the target cluster.
	BindingStateScheduled BindingState = "Scheduled"

	// BindingStateBound means the binding is bound to the target cluster.
	BindingStateBound BindingState = "Bound"

	// BindingStateUnScheduled means the binding is not scheduled on to the target cluster anymore.
	BindingStateUnScheduled BindingState = "UnScheduled"
)

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
