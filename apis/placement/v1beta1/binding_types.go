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
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-placement},shortName=rb
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Bound")].status`,name="WorkCreated",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Applied")].status`,name="ResourcesApplied",type=string
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
	// The desired state of the binding. Possible values: Scheduled, Bound, Unscheduled.
	// +required
	State BindingState `json:"state"`

	// ResourceSnapshotName is the name of the resource snapshot that this resource binding points to.
	// If the resources are divided into multiple snapshots because of the resource size limit,
	// it points to the name of the leading snapshot of the index group.
	ResourceSnapshotName string `json:"resourceSnapshotName"`

	// ResourceOverrideSnapshots is a list of ResourceOverride snapshots associated with the selected resources.
	ResourceOverrideSnapshots []NamespacedName `json:"resourceOverrideSnapshots,omitempty"`

	// ClusterResourceOverrides contains a list of applicable ClusterResourceOverride snapshot names associated with the
	// selected resources.
	ClusterResourceOverrideSnapshots []string `json:"clusterResourceOverrideSnapshots,omitempty"`

	// SchedulingPolicySnapshotName is the name of the scheduling policy snapshot that this resource binding
	// points to; more specifically, the scheduler creates this bindings in accordance with this
	// scheduling policy snapshot.
	SchedulingPolicySnapshotName string `json:"schedulingPolicySnapshotName"`

	// TargetCluster is the name of the cluster that the scheduler assigns the resources to.
	TargetCluster string `json:"targetCluster"`

	// ClusterDecision explains why the scheduler selected this cluster.
	ClusterDecision ClusterDecision `json:"clusterDecision"`

	// ApplyStrategy describes how to resolve the conflict if the resource to be placed already exists in the target cluster
	// and is owned by other appliers.
	// +optional
	ApplyStrategy *ApplyStrategy `json:"applyStrategy,omitempty"`
}

// BindingState is the state of the binding.
type BindingState string

const (
	// BindingStateScheduled means the binding is scheduled but need to be bound to the target cluster.
	BindingStateScheduled BindingState = "Scheduled"

	// BindingStateBound means the binding is bound to the target cluster.
	BindingStateBound BindingState = "Bound"

	// BindingStateUnscheduled means the binding is not scheduled on to the target cluster anymore.
	// This is a state that rollout controller cares about.
	// The work generator still treat this as bound until rollout controller deletes the binding.
	BindingStateUnscheduled BindingState = "Unscheduled"
)

// ResourceBindingStatus represents the current status of a ClusterResourceBinding.
type ResourceBindingStatus struct {
	// +kubebuilder:validation:MaxItems=100

	// FailedPlacements is a list of all the resources failed to be placed to the given cluster or the resource is unavailable.
	// Note that we only include 100 failed resource placements even if there are more than 100.
	// +optional
	FailedPlacements []FailedResourcePlacement `json:"failedPlacements,omitempty"`

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
	// ResourceBindingRolloutStarted indicates whether the binding can roll out the latest changes of the given resources.
	// Its condition status can be one of the following:
	// - "True" means the spec of the binding is updated to the latest resource snapshots and their overrides and rollout
	// has been started.
	// - "False" means the spec of the binding cannot be updated to the latest resource snapshots and their overrides because
	// of the rollout strategy.
	// - "Unknown" means it is unknown.
	ResourceBindingRolloutStarted ResourceBindingConditionType = "RolloutStarted"

	// ResourceBindingOverridden indicates the overridden condition of the given resources.
	// Its condition status can be one of the following:
	// - "True" means the resources are overridden successfully or there are no overrides associated before applying
	// to the target cluster.
	// - "False" means the resources fail to be overridden before applying to the target cluster.
	// - "Unknown" means it is unknown.
	ResourceBindingOverridden ResourceBindingConditionType = "Overridden"

	// ResourceBindingWorkSynchronized indicates the work synchronized condition of the given resources.
	// Its condition status can be one of the following:
	// - "True" means all corresponding works are created or updated in the target cluster's namespace.
	// - "False" means not all corresponding works are created or updated in the target cluster's namespace yet.
	// - "Unknown" means it is unknown.
	ResourceBindingWorkSynchronized ResourceBindingConditionType = "WorkSynchronized"

	// ResourceBindingApplied indicates the applied condition of the given resources.
	// Its condition status can be one of the following:
	// - "True" means all the resources are created in the target cluster.
	// - "False" means not all the resources are created in the target cluster yet.
	// - "Unknown" means it is unknown.
	ResourceBindingApplied ResourceBindingConditionType = "Applied"

	// ResourceBindingAvailable indicates the available condition of the given resources.
	// Its condition status can be one of the following:
	// - "True" means all the resources are available in the target cluster.
	// - "False" means not all the resources are available in the target cluster yet.
	// - "Unknown" means we haven't finished the apply yet so that we cannot check the resource availability.
	ResourceBindingAvailable ResourceBindingConditionType = "Available"
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
func (b *ClusterResourceBinding) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&b.Status.Conditions, c)
	}
}

// GetCondition returns the condition of the given ClusterResourceBinding.
func (b *ClusterResourceBinding) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(b.Status.Conditions, conditionType)
}

func init() {
	SchemeBuilder.Register(&ClusterResourceBinding{}, &ClusterResourceBindingList{})
}
