/*
Copyright 2026 The KubeFleet Authors.

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
)

const (
	// The condition types of StagedUpdateRun and ClusterStagedUpdateRun API objects.
	StagedUpdateRunCondTypeInitialized = "Initialized"
	StagedUpdateRunCondTypeStarted     = "Started"
	StagedUpdateRunCondTypeCompleted   = "Completed"

	// The condition type of stages to run in StagedUpdateRun and ClusterStagedUpdateRun API objects.
	StagedUpdateRunPerStageCondTypeStarted   = "Started"
	StagedUpdateRunPerStageCondTypeCompleted = "Completed"

	// The condition type of clusters to roll out to in StagedUpdateRun and ClusterStagedUpdateRun API objects.
	StagedUpdateRunPerClusterCondTypeStarted   = "Started"
	StagedUpdateRunPerClusterCondTypeCompleted = "Completed"

	// The condition type of stage tasks to execute in StagedUpdateRun and ClusterStagedUpdateRun API objects.
	StagedUpdateRunTaskCondTypeApprovalRequestCreated  = "ApprovalRequestCreated"
	StagedUpdateRunTaskCondTypeApprovalRequestApproved = "ApprovalRequestApproved"
	StagedUpdateRunTaskCondTypeTimedWaitStarted        = "TimedWaitStarted"
	StagedUpdateRunTaskCondTypeWaitTimeElapsed         = "WaitTimeElapsed"
)

// The reasons for the respective condition types.
const (
	StagedUpdateRunInitializedCondReasonPreppedResourceSnapshotAndAllStages = "PreppedResourceSnapshotAndAllStages"

	StagedUpdateRunTaskApprovalRequestCreatedCondReasonCreated   = "RequestCreated"
	StagedUpdateRunTaskApprovalRequestApprovedCondReasonApproved = "RequestApproved"

	StagedUpdateRunTaskTimedWaitStartedCondReasonTimerStarted = "TimerStarted"
	StagedUpdateRunTaskWaitTimeElapsedCondReasonTimerElapsed  = "TimerElapsed"
)

// StagedUpdateRun is the KubeFleet API that enables users to roll out resource changes for a placement policy
// in a staged manner, where clusters are grouped into stages and KubeFleet applies resource changes to each stage
// sequentially.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={kubefleet,kubefleet-rollout}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion
type StagedUpdateRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification of the StagedUpdateRun API object.
	//
	// +kubebuilder:validation:Required
	Spec StagedUpdateRunSpec `json:"spec"`

	// The observed status of the StagedUpdateRun API object.
	//
	// +kubebuilder:validation:Optional
	Status StagedUpdateRunStatus `json:"status,omitempty"`
}

// ClusterStagedUpdateRun is the KubeFleet API that enables users to roll out resource changes for a cluster
// placement policy in a staged manner, where clusters are grouped into stages and KubeFleet applies resource changes to each stage
// sequentially.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={kubefleet,kubefleet-rollout}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion
type ClusterStagedUpdateRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification of the ClusterStagedUpdateRun API object.
	//
	// +kubebuilder:validation:Required
	Spec StagedUpdateRunSpec `json:"spec"`

	// The observed status of the ClusterStagedUpdateRun API object.
	//
	// +kubebuilder:validation:Optional
	Status StagedUpdateRunStatus `json:"status,omitempty"`
}

// StagedUpdateRunSpec is the spec of the StagedUpdateRun API object.
//
// +kubebuilder:validation:XValidation:rule="has(self.resourceSnapshotName) == has(oldSelf.resourceSnapshotName)",message="resourceSnapshotName cannot be added or removed after creation"
// +kubebuilder:validation:XValidation:rule="has(self.failurePolicy) == has(oldSelf.failurePolicy)",message="failurePolicy cannot be added or removed after creation"
type StagedUpdateRunSpec struct {
	// The name of the placement policy that the StagedUpdateRun API object is associated with, i.e.,
	// the name of the placement policy where resource changes are rolled out.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="placementPolicyName is immutable"
	PlacementPolicyName string `json:"placementPolicyName"`

	// The name of the resource snapshot that the StagedUpdateRun API object is associated with, i.e.,
	// the name of the resource snapshot where resource changes are introduced.
	//
	// If left empty, KubeFleet will request a new resource snapshot to be created based on the current state
	// of selected resources in the placement policy (if applicable).
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="resourceSnapshotName is immutable"
	// +kubebuilder:validation:MaxLength=253
	ResourceSnapshotName string `json:"resourceSnapshotName,omitempty"`

	// The name of the staged update strategy that the StagedUpdateRun API object is associated with, i.e.,
	// the name of the staged update strategy that organizes clusters into stages and defines how the rollout is executed.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="stagedUpdateStrategyName is immutable"
	StagedUpdateStrategyName string `json:"stagedUpdateStrategyName"`

	// Whether the staged update run is suspended. If set to true, KubeFleet will pause the rollout.
	//
	// If the staged update run is created with this field set to true, KubeFleet will initialize the staged update run,
	// i.e., decide on the resource snapshot to roll out and determine the stages and the clusters in each stage,
	// but will not start rolling out resource changes until the field is set back to false.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	Suspended bool `json:"suspended,omitempty"`

	// The failure policy of the staged update run. It helps KubeFleet determine when to stop the staged update run
	// when there are too many failures, so as to keep the impact radius under control.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="failurePolicy is immutable"
	FailurePolicy *StagedUpdateRunFailurePolicy `json:"failurePolicy,omitempty"`
}

type StagedUpdateRunFailurePolicy struct {
	// The maximum number of clusters that can fail the rollout before KubeFleet stops the staged
	// update run. The count is tracked across all stages and clusters. A cluster is considered to have
	// failed the rollout if the new (updated) placement does not become available within the maximum
	// wait time.
	//
	// The default value is 1.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	MaxAllowedClusterFailures int32 `json:"maxAllowedClusterFailures,omitempty"`

	// The maximum time to wait for a rollout to a cluster to complete before KubeFleet considers the rollout to have
	// failed for the cluster.
	//
	// The default value is 5 minutes.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=5
	// +kubebuilder:validation:Minimum=1
	MaxWaitTimePerClusterMinutes int32 `json:"maxWaitTimePerClusterMinutes,omitempty"`
}

type StagedUpdateRunStatus struct {
	// A list of observed conditions of the staged update run.
	//
	// +kubebuilder:validation:Optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// The name of the resource snapshot that is being rolled out in the staged update run.
	//
	// If the staged update run is created with the resourceSnapshotName field in the spec set to a specific value,
	// this field will be set to the same value; otherwise, KubeFleet will request a new resource snapshot to be created (if applicable)
	// and populate this field with the name of the newly created resource snapshot.
	//
	// +kubebuilder:validation:Optional
	ResourceSnapshotNameToRollout string `json:"resourceSnapshotNameToRollout,omitempty"`

	// The status of each stage in the staged update run, including the clusters in each stage and their rollout status.
	//
	// +kubebuilder:validation:Optional
	Stages []PerStageStatus `json:"stages,omitempty"`
}

type PerStageStatus struct {
	// The name of the stage.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="^[a-z0-9]+$"
	StageName string `json:"stageName"`

	// The list of clusters featured in the stage, and their individual rollout status.
	//
	// +kubebuilder:validation:Optional
	Clusters []PerClusterStatus `json:"clusters,omitempty"`

	// The list of tasks to be executed before starting the rollout in the stage, and their execution status.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=1
	BeforeStageTasks []PerStageTaskStatus `json:"beforeStageTasks,omitempty"`

	// The list of tasks to be executed after completing the rollout in the stage, and their execution status.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=2
	AfterStageTasks []PerStageTaskStatus `json:"afterStageTasks,omitempty"`

	// A list of observed conditions about the rollout progress in the stage.
	//
	// +kubebuilder:validation:Optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// The timestamp when the rollout in the stage started. If unset, the rollout has not started in the stage yet.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	StartedTimestamp *metav1.Time `json:"startedTimestamp,omitempty"`

	// The timestamp when the rollout in the stage completed. If unset, the rollout has not completed in the stage yet.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	CompletedTimestamp *metav1.Time `json:"completedTimestamp,omitempty"`

	// The (resolved) maximum number of clusters that can be updated concurrently within this stage.
	//
	// +kubebuilder:validation:Optional
	MaxConcurrency *int32 `json:"maxConcurrency,omitempty"`
}

type PerClusterStatus struct {
	// The name of the cluster.
	//
	// +kubebuilder:validation:Required
	ClusterName string `json:"clusterName"`

	// The name of the placement binding.
	//
	// +kubebuilder:validation:Required
	PlacementBindingName string `json:"placementBindingName"`

	// A list of observed conditions about the rollout progress of the cluster.
	//
	// +kubebuilder:validation:Optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type PerStageTaskStatus struct {
	// The type of the stage task.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=TimedWait;Approval
	Type StageTaskType `json:"type"`

	// The name of the approval request that is created for the stage task.
	//
	// This field is only set when the task type is Approval.
	//
	// +kubebuilder:validation:Optional
	ApprovalRequestName string `json:"approvalRequestName,omitempty"`

	// The time to wait in a TimedWait task.
	//
	// +kubebuilder:validation:Optional
	WaitTime *metav1.Duration `json:"waitTime,omitempty"`

	// A list of observed conditions about the progress of the stage task.
	//
	// +kubebuilder:validation:Optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// The list objects for StagedUpdateRun and ClusterStagedUpdateRun APIs.

// StagedUpdateRunList contains a list of StagedUpdateRun API objects.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type StagedUpdateRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []StagedUpdateRun `json:"items"`
}

// ClusterStagedUpdateRunList contains a list of ClusterStagedUpdateRun API objects.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterStagedUpdateRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ClusterStagedUpdateRun `json:"items"`
}

// Set up the API types with the scheme builder.
func init() {
	SchemeBuilder.Register(&StagedUpdateRun{}, &StagedUpdateRunList{})
	SchemeBuilder.Register(&ClusterStagedUpdateRun{}, &ClusterStagedUpdateRunList{})
}
