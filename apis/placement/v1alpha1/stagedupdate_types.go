/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.goms.io/fleet/apis/placement/v1beta1"
)

// +genclient
// +genclient:Cluster
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-placement},shortName=crsur
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:printcolumn:JSONPath=`.spec.placementName`,name="Placement",type=string
// +kubebuilder:printcolumn:JSONPath=`.spec.resourceSnapshotIndex`,name="Resource-Snapshot",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.policySnapshotIndexUsed`,name="Policy-Snapshot",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Initialized")].status`,name="Initialized",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Succeeded")].status`,name="Succeeded",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +kubebuilder:printcolumn:JSONPath=`.spec.stagedRolloutStrategyName`,name="Strategy",priority=1,type=string
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) < 128",message="metadata.name max length is 127"

// ClusterStagedUpdateRun represents a stage by stage update process that applies ClusterResourcePlacement
// selected resources to specified clusters.
// Resources from unselected clusters are removed after all stages in the update strategy are completed.
// Each ClusterStagedUpdateRun object corresponds to a single release of a specific resource version.
// The release is abandoned if the ClusterStagedUpdateRun object is deleted or the scheduling decision changes.
// The name of the ClusterStagedUpdateRun must conform to RFC 1123.
type ClusterStagedUpdateRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ClusterStagedUpdateRun. The spec is immutable.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="The spec field is immutable"
	Spec StagedUpdateRunSpec `json:"spec"`

	// The observed status of ClusterStagedUpdateRun.
	// +kubebuilder:validation:Optional
	Status StagedUpdateRunStatus `json:"status,omitempty"`
}

// StagedUpdateRunSpec defines the desired rollout strategy and the snapshot indices of the resources to be updated.
// It specifies a stage-by-stage update process across selected clusters for the given ResourcePlacement object.
type StagedUpdateRunSpec struct {
	// PlacementName is the name of placement that this update run is applied to.
	// There can be multiple active update runs for each placement, but
	// it's up to the DevOps team to ensure they don't conflict with each other.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=255
	PlacementName string `json:"placementName"`

	// The resource snapshot index of the selected resources to be updated across clusters.
	// The index represents a group of resource snapshots that includes all the resources a ResourcePlacement selected.
	// +kubebuilder:validation:Required
	ResourceSnapshotIndex string `json:"resourceSnapshotIndex"`

	// The name of the update strategy that specifies the stages and the sequence
	// in which the selected resources will be updated on the member clusters. The stages
	// are computed according to the referenced strategy when the update run starts
	// and recorded in the status field.
	// +kubebuilder:validation:Required
	StagedUpdateStrategyName string `json:"stagedRolloutStrategyName"`
}

// +genclient
// +genclient:cluster
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-placement},shortName=sus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterStagedUpdateStrategy defines a reusable strategy that specifies the stages and the sequence
// in which the selected cluster resources will be updated on the member clusters.
type ClusterStagedUpdateStrategy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ClusterStagedUpdateStrategy.
	// +kubebuilder:validation:Required
	Spec StagedUpdateStrategySpec `json:"spec"`
}

// StagedUpdateStrategySpec defines the desired state of the StagedUpdateStrategy.
type StagedUpdateStrategySpec struct {
	// Stage specifies the configuration for each update stage.
	// +kubebuilder:validation:MaxItems=31
	// +kubebuilder:validation:Required
	Stages []StageConfig `json:"stages"`
}

// ClusterStagedUpdateStrategyList contains a list of StagedUpdateStrategy.
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterStagedUpdateStrategyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterStagedUpdateStrategy `json:"items"`
}

// StageConfig describes a single update stage.
// The clusters in each stage are updated sequentially.
// The update stops if any of the updates fail.
type StageConfig struct {
	// The name of the stage. This MUST be unique within the same StagedUpdateStrategy.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="^[a-z0-9]+$"
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// LabelSelector is a label query over all the joined member clusters. Clusters matching the query are selected
	// for this stage. There cannot be overlapping clusters between stages when the stagedUpdateRun is created.
	// If the label selector is absent, the stage includes all the selected clusters.
	// +kubebuilder:validation:Optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// The label key used to sort the selected clusters.
	// The clusters within the stage are updated sequentially following the rule below:
	//   - primary: Ascending order based on the value of the label key, interpreted as integers if present.
	//   - secondary: Ascending order based on the name of the cluster if the label key is absent or the label value is the same.
	// +kubebuilder:validation:Optional
	SortingLabelKey *string `json:"sortingLabelKey,omitempty"`

	// The collection of tasks that each stage needs to complete successfully before moving to the next stage.
	// Each task is executed in parallel and there cannot be more than one task of the same type.
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:Optional
	AfterStageTasks []AfterStageTask `json:"afterStageTasks,omitempty"`
}

// AfterStageTask is the collection of post-stage tasks that ALL need to be completed before moving to the next stage.
type AfterStageTask struct {
	// The type of the after-stage task.
	// +kubebuilder:validation:Enum=TimedWait;Approval
	// +kubebuilder:validation:Required
	Type AfterStageTaskType `json:"type"`

	// The time to wait after all the clusters in the current stage complete the update before moving to the next stage.
	// +kubebuilder:default="1h"
	// +kubebuilder:validation:Pattern="^0|([0-9]+(\\.[0-9]+)?(s|m|h))+$"
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Optional
	WaitTime metav1.Duration `json:"waitTime,omitempty"`
}

// StagedUpdateRunStatus defines the observed state of the ClusterStagedUpdateRun.
type StagedUpdateRunStatus struct {
	// PolicySnapShotIndexUsed records the policy snapshot index of the ClusterResourcePlacement (CRP) that
	// the update run is based on. The index represents the latest policy snapshot at the start of the update run.
	// If a newer policy snapshot is detected after the run starts, the staged update run is abandoned.
	// The scheduler must identify all clusters that meet the current policy before the update run begins.
	// All clusters involved in the update run are selected from the list of clusters scheduled by the CRP according
	// to the current policy.
	// +kubebuilder:validation:Optional
	PolicySnapshotIndexUsed string `json:"policySnapshotIndexUsed,omitempty"`

	// PolicyObservedClusterCount records the number of observed clusters in the policy snapshot.
	// It is recorded at the beginning of the update run from the policy snapshot object.
	// If the `ObservedClusterCount` value is updated during the update run, the update run is abandoned.
	// +kubebuilder:validation:Optional
	PolicyObservedClusterCount int `json:"policyObservedClusterCount,omitempty"`

	// ApplyStrategy is the apply strategy that the stagedUpdateRun is using.
	// It is the same as the apply strategy in the CRP when the staged update run starts.
	// The apply strategy is not updated during the update run even if it changes in the CRP.
	// +kubebuilder:validation:Optional
	ApplyStrategy *v1beta1.ApplyStrategy `json:"appliedStrategy,omitempty"`

	// StagedUpdateStrategySnapshot is the snapshot of the StagedUpdateStrategy used for the update run.
	// The snapshot is immutable during the update run.
	// The strategy is applied to the list of clusters scheduled by the CRP according to the current policy.
	// The update run fails to initialize if the strategy fails to produce a valid list of stages where each selected
	// cluster is included in exactly one stage.
	// +kubebuilder:validation:Optional
	StagedUpdateStrategySnapshot *StagedUpdateStrategySpec `json:"stagedUpdateStrategySnapshot,omitempty"`

	// StagesStatus lists the current updating status of each stage.
	// The list is empty if the update run is not started or failed to initialize.
	// +kubebuilder:validation:Optional
	StagesStatus []StageUpdatingStatus `json:"stagesStatus,omitempty"`

	// DeletionStageStatus lists the current status of the deletion stage. The deletion stage
	// removes all the resources from the clusters that are not selected by the
	// current policy after all the update stages are completed.
	// +kubebuilder:validation:Optional
	DeletionStageStatus *StageUpdatingStatus `json:"deletionStageStatus,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	//
	// Conditions is an array of current observed conditions for StagedUpdateRun.
	// Known conditions are "Initialized", "Progressing", "Succeeded".
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// StagedUpdateRunConditionType identifies a specific condition of the StagedUpdateRun.
// +enum
type StagedUpdateRunConditionType string

const (
	// StagedUpdateRunConditionInitialized indicates whether the staged update run is initialized, meaning it
	// has computed all the stages according to the referenced strategy and is ready to start the update.
	// Its condition status can be one of the following:
	// - "True": The staged update run is initialized successfully.
	// - "False": The staged update run encountered an error during initialization and aborted.
	// - "Unknown": The staged update run initialization has started.
	StagedUpdateRunConditionInitialized StagedUpdateRunConditionType = "Initialized"

	// StagedUpdateRunConditionProgressing indicates whether the staged update run is making progress.
	// Its condition status can be one of the following:
	// - "True": The staged update run is making progress.
	// - "False": The staged update run is waiting/paused.
	// - "Unknown" means it is unknown.
	StagedUpdateRunConditionProgressing StagedUpdateRunConditionType = "Progressing"

	// StagedUpdateRunConditionSucceeded indicates whether the staged update run is completed successfully.
	// Its condition status can be one of the following:
	// - "True": The staged update run is completed successfully.
	// - "False": The staged update run encountered an error and stopped.
	StagedUpdateRunConditionSucceeded StagedUpdateRunConditionType = "Succeeded"
)

// StageUpdatingStatus defines the status of the update run in a stage.
type StageUpdatingStatus struct {
	// The name of the stage.
	// +kubebuilder:validation:Required
	StageName string `json:"stageName"`

	// The list of each cluster's updating status in this stage.
	// +kubebuilder:validation:Required
	Clusters []ClusterUpdatingStatus `json:"clusters"`

	// The status of the post-update tasks associated with the current stage.
	// Empty if the stage has not finished updating all the clusters.
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:Optional
	AfterStageTaskStatus []AfterStageTaskStatus `json:"afterStageTaskStatus,omitempty"`

	// The time when the update started on the stage. Empty if the stage has not started updating.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// The time when the update finished on the stage. Empty if the stage has not started updating.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	EndTime *metav1.Time `json:"endTime,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	//
	// Conditions is an array of current observed updating conditions for the stage. Empty if the stage has not started updating.
	// Known conditions are "Progressing", "Succeeded".
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// StageUpdatingConditionType identifies a specific condition of the stage that is being updated.
// +enum
type StageUpdatingConditionType string

const (
	// StageUpdatingConditionProgressing indicates whether the stage updating is making progress.
	// Its condition status can be one of the following:
	// - "True": The stage updating is making progress.
	// - "False": The stage updating is waiting/pausing.
	StageUpdatingConditionProgressing StageUpdatingConditionType = "Progressing"

	// StageUpdatingConditionSucceeded indicates whether the stage updating is completed successfully.
	// Its condition status can be one of the following:
	// - "True": The stage updating is completed successfully.
	// - "False": The stage updating encountered an error and stopped.
	StageUpdatingConditionSucceeded StageUpdatingConditionType = "Succeeded"
)

// ClusterUpdatingStatus defines the status of the update run on a cluster.
type ClusterUpdatingStatus struct {
	// The name of the cluster.
	// +kubebuilder:validation:Required
	ClusterName string `json:"clusterName"`

	// ResourceOverrideSnapshots is a list of ResourceOverride snapshots associated with the cluster.
	// The list is computed at the beginning of the update run and not updated during the update run.
	// The list is empty if there are no resource overrides associated with the cluster.
	// +kubebuilder:validation:Optional
	ResourceOverrideSnapshots []v1beta1.NamespacedName `json:"resourceOverrideSnapshots,omitempty"`

	// ClusterResourceOverrides contains a list of applicable ClusterResourceOverride snapshot names
	// associated with the cluster.
	// The list is computed at the beginning of the update run and not updated during the update run.
	// The list is empty if there are no cluster overrides associated with the cluster.
	// +kubebuilder:validation:Optional
	ClusterResourceOverrideSnapshots []string `json:"clusterResourceOverrideSnapshots,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	//
	// Conditions is an array of current observed conditions for clusters. Empty if the cluster has not started updating.
	// Known conditions are "Started", "Succeeded".
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ClusterUpdatingStatusConditionType identifies a specific condition of the UpdatingStatus of the cluster.
// +enum
type ClusterUpdatingStatusConditionType string

const (
	// ClusterUpdatingConditionStarted indicates whether the cluster updating has started.
	// Its condition status can be one of the following:
	// - "True": The cluster updating has started.
	ClusterUpdatingConditionStarted ClusterUpdatingStatusConditionType = "Started"

	// ClusterUpdatingConditionSucceeded indicates whether the cluster updating is completed successfully.
	// Its condition status can be one of the following:
	// - "True": The cluster updating is completed successfully.
	// - "False": The cluster updating encountered an error and stopped.
	ClusterUpdatingConditionSucceeded ClusterUpdatingStatusConditionType = "Succeeded"
)

type AfterStageTaskStatus struct {
	// The type of the post-update task.
	// +kubebuilder:validation:Enum=TimedWait;Approval
	// +kubebuilder:validation:Required
	Type AfterStageTaskType `json:"type"`

	// The name of the approval request object that is created for this stage.
	// Only valid if the AfterStageTaskType is Approval.
	// +kubebuilder:validation:Optional
	ApprovalRequestName string `json:"approvalRequestName,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	//
	// Conditions is an array of current observed conditions for the specific type of post-update task.
	// Known conditions are "ApprovalRequestCreated", "WaitTimeElapsed", and "ApprovalRequestApproved".
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// AfterStageTaskType identifies a specific type of the AfterStageTask.
// +enum
type AfterStageTaskType string

const (
	// AfterStageTaskTypeTimedWait indicates the post-stage task is a timed wait.
	AfterStageTaskTypeTimedWait AfterStageTaskType = "TimedWait"

	// AfterStageTaskTypeApproval indicates the post-stage task is an approval.
	AfterStageTaskTypeApproval AfterStageTaskType = "Approval"
)

// AfterStageTaskConditionType identifies a specific condition of the AfterStageTask.
// +enum
type AfterStageTaskConditionType string

const (
	// AfterStageTaskConditionApprovalRequestCreated indicates if the approval request has been created.
	// Its condition status can be:
	// - "True": The approval request has been created.
	AfterStageTaskConditionApprovalRequestCreated AfterStageTaskConditionType = "ApprovalRequestCreated"

	// AfterStageTaskConditionApprovalRequestApproved indicates if the approval request has been approved.
	// Its condition status can be:
	// - "True": The approval request has been approved.
	AfterStageTaskConditionApprovalRequestApproved AfterStageTaskConditionType = "ApprovalRequestApproved"

	// AfterStageTaskConditionWaitTimeElapsed indicates if the wait time after each stage has elapsed.
	// If the status is "False", the condition message will include the remaining wait time.
	// Its condition status can be:
	// - "True": The wait time has elapsed.
	// - "False": The wait time has not elapsed.
	AfterStageTaskConditionWaitTimeElapsed AfterStageTaskConditionType = "WaitTimeElapsed"
)

// ClusterStagedUpdateRunList contains a list of ClusterStagedUpdateRun.
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterStagedUpdateRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterStagedUpdateRun `json:"items"`
}

// +genclient
// +genclient:Cluster
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-placement},shortName=careq
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:printcolumn:JSONPath=`.spec.parentStageRollout`,name="Update-Run",type=string
// +kubebuilder:printcolumn:JSONPath=`.spec.targetStage`,name="Stage",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Approved")].status`,name="Approved",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// ClusterApprovalRequest defines a request for user approval for cluster staged update run.
// The request object MUST have the following labels:
//   - `TargetUpdateRun`: Points to the cluster staged update run that this approval request is for.
//   - `TargetStage`: The name of the stage that this approval request is for.
//   - `IsLatestUpdateRunApproval`: Indicates whether this approval request is the latest one related to this update run.
type ClusterApprovalRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ClusterApprovalRequest.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="The spec field is immutable"
	// +kubebuilder:validation:Required
	Spec ApprovalRequestSpec `json:"spec"`

	// The observed state of ClusterApprovalRequest.
	// +kubebuilder:validation:Optional
	Status ApprovalRequestStatus `json:"status,omitempty"`
}

// ApprovalRequestSpec defines the desired state of the update run approval request.
// The entire spec is immutable.
type ApprovalRequestSpec struct {
	// The name of the staged update run that this approval request is for.
	// +kubebuilder:validation:Required
	TargetUpdateRun string `json:"parentStageRollout"`

	// The name of the update stage that this approval request is for.
	// +kubebuilder:validation:Required
	TargetStage string `json:"targetStage"`
}

// ApprovalRequestStatus defines the observed state of the ClusterApprovalRequest.
type ApprovalRequestStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	//
	// Conditions is an array of current observed conditions for the specific type of post-update task.
	// Known conditions are "Approved" and "ApprovalAccepted".
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ApprovalRequestConditionType identifies a specific condition of the ClusterApprovalRequest.
type ApprovalRequestConditionType string

const (
	// ApprovalRequestConditionApproved indicates if the approval request was approved.
	// Its condition status can be:
	// - "True": The request is approved.
	ApprovalRequestConditionApproved ApprovalRequestConditionType = "Approved"
)

// ClusterApprovalRequestList contains a list of ClusterApprovalRequest.
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterApprovalRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterApprovalRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&ClusterStagedUpdateRun{}, &ClusterStagedUpdateRunList{}, &ClusterStagedUpdateStrategy{}, &ClusterStagedUpdateStrategyList{}, &ClusterApprovalRequest{}, &ClusterApprovalRequestList{},
	)
}
