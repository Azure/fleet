/*
Copyright 2025 The KubeFleet Authors.

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

package v1beta1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/apis"
)

const (
	// SchedulerBindingCleanupFinalizer is a finalizer added to bindings to ensure we can look up the
	// corresponding CRP name for deleting bindings to trigger a new scheduling cycle.
	// TODO: migrate the finalizer to the new name "scheduler-binding-cleanup" in the future.
	SchedulerBindingCleanupFinalizer = FleetPrefix + "scheduler-crb-cleanup"
)

// make sure the BindingObj and BindingObjList interfaces are implemented by the
// ClusterResourceBinding and ResourceBinding types.
var _ BindingObj = &ClusterResourceBinding{}
var _ BindingObj = &ResourceBinding{}
var _ BindingObjList = &ClusterResourceBindingList{}
var _ BindingObjList = &ResourceBindingList{}

// A BindingSpecGetterSetter offers binding spec getter and setter methods.
// +kubebuilder:object:generate=false
type BindingSpecGetterSetter interface {
	GetBindingSpec() *ResourceBindingSpec
	SetBindingSpec(ResourceBindingSpec)
}

// A BindingStatusGetterSetter offers binding status getter and setter methods.
// +kubebuilder:object:generate=false
type BindingStatusGetterSetter interface {
	GetBindingStatus() *ResourceBindingStatus
	SetBindingStatus(ResourceBindingStatus)
}

// A BindingObj offers an abstract way to work with fleet binding objects.
// +kubebuilder:object:generate=false
type BindingObj interface {
	apis.ConditionedObj
	BindingSpecGetterSetter
	BindingStatusGetterSetter
	RemoveCondition(string)
}

// A BindingListItemGetter offers a method to get binding objects from a list.
// +kubebuilder:object:generate=false
type BindingListItemGetter interface {
	GetBindingObjs() []BindingObj
}

// A BindingObjList offers an abstract way to work with list binding objects.
// +kubebuilder:object:generate=false
type BindingObjList interface {
	client.ObjectList
	BindingListItemGetter
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-placement},shortName=crb
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="WorkSynchronized")].status`,name="WorkSynchronized",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Applied")].status`,name="ResourcesApplied",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Available")].status`,name="ResourceAvailable",priority=1,type=string
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

	// DriftedPlacements is a list of resources that have drifted from their desired states
	// kept in the hub cluster, as found by Fleet using the drift detection mechanism.
	//
	// To control the object size, only the first 100 drifted resources will be included.
	// This field is only meaningful if the `ClusterName` is not empty.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=100
	DriftedPlacements []DriftedResourcePlacement `json:"driftedPlacements,omitempty"`

	// DiffedPlacements is a list of resources that have configuration differences from their
	// corresponding hub cluster manifests. Fleet will report such differences when:
	//
	// * The CRP uses the ReportDiff apply strategy, which instructs Fleet to compare the hub
	//   cluster manifests against the live resources without actually performing any apply op; or
	// * Fleet finds a pre-existing resource on the member cluster side that does not match its
	//   hub cluster counterpart, and the CRP has been configured to only take over a resource if
	//   no configuration differences are found.
	//
	// To control the object size, only the first 100 diffed resources will be included.
	// This field is only meaningful if the `ClusterName` is not empty.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=100
	DiffedPlacements []DiffedResourcePlacement `json:"diffedPlacements,omitempty"`

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

	// ResourceBindingDiffReported indicates that Fleet has successfully reported configuration
	// differences between the hub cluster and a specific member cluster for the given resources.
	//
	// This condition is added only when the ReportDiff apply strategy is used.
	//
	// It can have the following condition statuses:
	// * True: Fleet has successfully reported configuration differences for all resources.
	// * False: Fleet has not yet reported configuration differences for some resources, or an
	//   error has occurred.
	// * Unknown: Fleet has not finished processing the diff reporting yet.
	ResourceBindingDiffReported ResourceBindingConditionType = "DiffReported"
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

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet,fleet-placement},shortName=rb
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="WorkSynchronized")].status`,name="WorkSynchronized",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Applied")].status`,name="ResourcesApplied",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Available")].status`,name="ResourceAvailable",priority=1,type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// ResourceBinding represents a scheduling decision that binds a group of resources to a cluster.
// It MUST have a label named `CRPTrackingLabel` that points to the resource placement that creates it.
type ResourceBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ResourceBinding.
	// +required
	Spec ResourceBindingSpec `json:"spec"`

	// The observed status of ResourceBinding.
	// +optional
	Status ResourceBindingStatus `json:"status,omitempty"`
}

// ResourceBindingList is a collection of ResourceBinding.
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ResourceBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// items is the list of ResourceBindings.
	Items []ResourceBinding `json:"items"`
}

// GetBindingObjs returns the binding objects in the list.
func (c *ClusterResourceBindingList) GetBindingObjs() []BindingObj {
	objs := make([]BindingObj, 0, len(c.Items))
	for i := range c.Items {
		objs = append(objs, &c.Items[i])
	}
	return objs
}

// GetBindingObjs returns the binding objects in the list.
func (r *ResourceBindingList) GetBindingObjs() []BindingObj {
	objs := make([]BindingObj, 0, len(r.Items))
	for i := range r.Items {
		objs = append(objs, &r.Items[i])
	}
	return objs
}

// SetConditions set the given conditions on the ClusterResourceBinding.
func (b *ClusterResourceBinding) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&b.Status.Conditions, c)
	}
}

// RemoveCondition removes the condition of the given ClusterResourceBinding.
func (b *ClusterResourceBinding) RemoveCondition(conditionType string) {
	meta.RemoveStatusCondition(&b.Status.Conditions, conditionType)
}

// GetCondition returns the condition of the given ClusterResourceBinding.
func (b *ClusterResourceBinding) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(b.Status.Conditions, conditionType)
}

// GetBindingSpec returns the binding spec.
func (b *ClusterResourceBinding) GetBindingSpec() *ResourceBindingSpec {
	return &b.Spec
}

// SetBindingSpec sets the binding spec.
func (b *ClusterResourceBinding) SetBindingSpec(spec ResourceBindingSpec) {
	spec.DeepCopyInto(&b.Spec)
}

// GetBindingStatus returns the binding status.
func (b *ClusterResourceBinding) GetBindingStatus() *ResourceBindingStatus {
	return &b.Status
}

// SetBindingStatus sets the binding status.
func (b *ClusterResourceBinding) SetBindingStatus(status ResourceBindingStatus) {
	status.DeepCopyInto(&b.Status)
}

// SetConditions set the given conditions on the ResourceBinding.
func (b *ResourceBinding) SetConditions(conditions ...metav1.Condition) {
	for _, c := range conditions {
		meta.SetStatusCondition(&b.Status.Conditions, c)
	}
}

// RemoveCondition removes the condition of the given ResourceBinding.
func (b *ResourceBinding) RemoveCondition(conditionType string) {
	meta.RemoveStatusCondition(&b.Status.Conditions, conditionType)
}

// GetCondition returns the condition of the given ResourceBinding.
func (b *ResourceBinding) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(b.Status.Conditions, conditionType)
}

// GetBindingSpec returns the binding spec.
func (b *ResourceBinding) GetBindingSpec() *ResourceBindingSpec {
	return &b.Spec
}

// SetBindingSpec sets the binding spec.
func (b *ResourceBinding) SetBindingSpec(spec ResourceBindingSpec) {
	spec.DeepCopyInto(&b.Spec)
}

// GetBindingStatus returns the binding status.
func (b *ResourceBinding) GetBindingStatus() *ResourceBindingStatus {
	return &b.Status
}

// SetBindingStatus sets the binding status.
func (b *ResourceBinding) SetBindingStatus(status ResourceBindingStatus) {
	status.DeepCopyInto(&b.Status)
}

func init() {
	SchemeBuilder.Register(&ClusterResourceBinding{}, &ClusterResourceBindingList{}, &ResourceBinding{}, &ResourceBindingList{})
}
