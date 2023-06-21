/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DispatcherFinalizer is added by the dispatcher to make sure that a binding can only be deleted if the dispatcher
	// has removed all selected resources from the bound cluster.
	DispatcherFinalizer = fleetPrefix + "dispatcher-cleanup"

	// SchedulerFinalizer is added by the scheduler to make sure that a binding can only be deleted if the scheduler
	// has relieved it from scheduling consideration.
	SchedulerFinalizer = fleetPrefix + "scheduler-cleanup"

	// ActiveBindingLabel is added by the update controller to mark that a binding is active, i.e., the dispatcher
	// should place resources to it.
	//
	// Note that an active binding may not be associated with the latest scheduling policy snapshot or the latest
	// resource snapshot. It may be up to another controller, e.g., the rolling update controller, to modify the
	// association (if applicable). In certain cases (e.g., not enough fitting clusters), the binding may not even
	// has a target cluster.
	//
	// Note also that it is not the scheduler's responsibility to add this label, even though it does
	// reads this label to inform the scheduling cycle..
	ActiveBindingLabel = fleetPrefix + "is-active-binding"

	// CreatingBindingLabel is added by the scheduler to mark that a binding is being created. Any binding in
	// this state should not be picked up by the dispatcher.
	//
	// Note that the scheduler **always** produces enough number of bindings, as user specified, after a scheduling run,
	// even if there might not be enough number of fitting clusters.
	//
	// Note also that it is up to another controller, e.g., the rolling update controller, to mark a creating
	// binding as active.
	CreatingBindingLabel = fleetPrefix + "is-creating-binding"

	// ObsoleteBindingLabel is added by the scheduler to mark that a binding is no longer needed, i.e., its
	// associated cluster (if any) no longer fits the current (as seen by the scheduler) scheduling policy.
	//
	// Note that it is up to another controller, e.g, the rolling update controller, to actually delete the
	// binding.
	ObsoleteBindingLabel = fleetPrefix + "is-obsolete-binding"

	// NoTargetClusterBindingLabel is added by the scheduler to mark that a binding does not have a target cluster.
	// This usually happens when there is not enough number of fitting clusters in the system.
	NoTargetClusterBindingLabel = fleetPrefix + "no-target-cluster-binding"
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
	// ResourceSnapshotName is the name of the resource snapshot that this resource binding points to.
	// If the resources are divided into multiple snapshots because of the resource size limit,
	// it points to the name of the leading snapshot of the index group.
	ResourceSnapshotName string `json:"resourceSnapshotName"`

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
