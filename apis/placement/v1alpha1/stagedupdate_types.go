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
// +genclient:namespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",categories={fleet,fleet-placement}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StagedUpdateRun defines a stage by stage update run that applies the selected resources by the
// corresponding ClusterResourcePlacement to its selected clusters. We remove the resources from the clusters that are
// unselected after all the stages explicitly defined in the updateStrategy complete.
// Each StagedUpdateRun object corresponds to a single "release" of a certain version of the resources.
// The release is abandoned if the StagedUpdateRun object is deleted or the scheduling decision (i.e., the selected clusters) changes.
// The name of the StagedUpdateRun needs to conform to [RFC 1123](https://tools.ietf.org/html/rfc1123).
type StagedUpdateRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of StagedUpdateRun. The spec is immutable.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="The spec field is immutable"
	Spec StagedUpdateRunSpec `json:"spec"`

	// The observed status of StagedUpdateRun.
	// +kubebuilder:validation:Optional
	Status StagedUpdateRunStatus `json:"status,omitempty"`
}

// StagedUpdateRunSpec defines the desired rollout strategy and the snapshot indices of the resources to be updated.
// It specifies a stage-by-stage update process across selected clusters for the given ResourcePlacement object.
type StagedUpdateRunSpec struct {
	// A reference to the placement that this update run is applied to.
	// There can be multiple active update runs for each placement but
	// it's up to the devOps to make sure they don't conflict with each other.
	// +kubebuilder:validation:Required
	PlacementRef PlacementReference `json:"placementRef"`

	// The resource snapshot index of the selected resources to be updated across clusters.
	// The index represents a group of resourceSnapshots that includes all the resources a ResourcePlacement selected.
	// +kubebuilder:validation:Required
	ResourceSnapshotIndex string `json:"resourceSnapshotIndex"`

	// The reference to the update strategy that specifies the stages and the sequence
	// in which the selected resources will be updated on the member clusters. We will compute
	// the stages according to the referenced strategy when we first start the update run
	// and record the computed stages in the status field.
	// +kubebuilder:validation:Required
	StagedUpdateStrategyRef v1beta1.NamespacedName `json:"stagedRolloutStrategyRef"`
}

// PlacementReference is a reference to a placement object.
type PlacementReference struct {
	// Name is the name of the referenced placement.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// +genclient
// +genclient:namespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",categories={fleet,fleet-placement}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StagedUpdateStrategy defines a reusable strategy that specifies the stages and the sequence
// in which the selected resources will be updated on the member clusters.
type StagedUpdateStrategy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of StagedUpdateStrategy.
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

// StagedUpdateStrategyList contains a list of StagedUpdateStrategy.
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type StagedUpdateStrategyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StagedUpdateStrategy `json:"items"`
}

// StageConfig describes a single update stage.
// The clusters in each stage are updated sequentially for now.
// We will stop the update if any of the updates fail.
type StageConfig struct {
	// The name of the stage. This MUST be unique within the same StagedUpdateStrategy.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="[A-Za-z0-9]+$"
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
	// Each task is executed in parallel and there can not be more than one task of the same type.
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:Optional
	AfterStageTasks []AfterStageTask `json:"afterStageTasks,omitempty"`
}

// AfterStageTask is the collection of post stage tasks that ALL need to be completed before we can move to the next stage.
type AfterStageTask struct {
	// The type of the after stage task.
	// +kubebuilder:validation:Enum=TimedWait;Approval
	// +kubebuilder:validation:Required
	Type AfterStageTaskType `json:"type"`

	// The time to wait after all the clusters in the current stage complete the update before we move to the next stage.
	// +kubebuilder:default="1h"
	// +kubebuilder:validation:Pattern="^0|([0-9]+(\\.[0-9]+)?(s|m|h))+$"
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Optional
	WaitTime metav1.Duration `json:"waitTime,omitempty"`
}

// StagedUpdateRunStatus defines the observed state of the StagedUpdateRun.
type StagedUpdateRunStatus struct {
	// PolicySnapShotIndexUsed records the policy snapshot index of the ClusterResourcePlacement (CRP) that
	// the update run is based on. The index represents the latest policy snapshot at the start of the update run.
	// If a newer policy snapshot is detected after the run starts, the staged update run is abandoned.
	// The scheduler must identify all clusters that meet the current policy before the update run begins.
	// All clusters involved in the update run are selected from the list of clusters scheduled by the CRP according
	// to the current policy.
	// +kubebuilder:validation:Optional
	PolicySnapshotIndexUsed string `json:"policySnapshotIndexUsed,omitempty"`

	// ApplyStrategy is the apply strategy that the stagedUpdateRun is using.
	// It is the same as the apply strategy in the CRP when we first start the staged update run.
	// We will NOT update the apply strategy during the update run even if the apply strategy changes in the CRP.
	// +kubebuilder:validation:Optional
	ApplyStrategy v1beta1.ApplyStrategy `json:"appliedStrategy,omitempty"`

	// StagedUpdateStrategySnapshot is the snapshot of the StagedUpdateStrategy that we are going to use for the update run.
	// The snapshot is immutable during the update run.
	// We will apply the strategy to the the list of clusters scheduled by the CRP according to the current policy.
	// The update run will fail to initialize if the strategy fails to produce a valid list of stages in which each selected
	// cluster is included in exactly one stage.
	// +kubebuilder:validation:Optional
	StagedUpdateStrategySnapshot StagedUpdateStrategySpec `json:"stagedUpdateStrategySnapshot,omitempty"`

	// StagesStatus list the current updating status of each stage.
	// The list is empty if the update run is not started or failed to initialize.
	// +kubebuilder:validation:Optional
	StagesStatus []StageUpdatingStatus `json:"stagesStatus,omitempty"`

	// DeletionStageStatus list the current status of the deletion stage. The deletion stage is
	// the stage that removes all the resources from the clusters that are not selected by the
	// current policy after all the update stages are completed.
	// +kubebuilder:validation:Optional
	DeletionStageStatus StageUpdatingStatus `json:"deletionStageStatus,omitempty"`

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
	// StagedUpdateRunConditionInitialized indicates whether the staged update run is initialized which means it
	// has computed all the stages according to the referenced strategy and is ready to start the update.
	// Its condition status can be one of the following:
	// - "True" means the staged update run is initialized.
	// - "False" means the staged update run encountered an error during initialization.
	StagedUpdateRunConditionInitialized StagedUpdateRunConditionType = "Initialized"

	// StagedUpdateRunConditionProgressing indicates whether the staged update run is making progress.
	// Its condition status can be one of the following:
	// - "True" means the staged update run is making progress.
	// - "False" means the staged update run is waiting/paused.
	// - "Unknown" means it is unknown.
	StagedUpdateRunConditionProgressing StagedUpdateRunConditionType = "Progressing"

	// StagedUpdateRunConditionSucceeded indicates whether the staged update run is completed successfully.
	// Its condition status can be one of the following:
	// - "True" means the staged update run is completed successfully.
	// - "False" means the staged update run encountered an error and stopped.
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

	// The status of the post update tasks that are associated with current stage.
	// Empty if the stage has not finished updating all the clusters.
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:Optional
	AfterStageTaskStatus []AfterStageTaskStatus `json:"afterStageTaskStatus ,omitempty"`

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
	// - "True" means the stage updating is making progress.
	// - "False" means the stage updating is waiting/pausing.
	StageUpdatingConditionProgressing StageUpdatingConditionType = "Progressing"

	// ClusterUpdatingStatusConditionSucceeded indicates whether the stage updating is completed successfully.
	// Its condition status can be one of the following:
	// - "True" means the stage updating is completed successfully.
	// - "False" means the stage updating encountered an error and stopped.
	ClusterUpdatingStatusConditionSucceeded StageUpdatingConditionType = "Succeeded"
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
	// Known conditions are "Started,"Succeeded".
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ClusterUpdatingStatusConditionType identifies a specific condition of the UpdatingStatus of the cluster.
// +enum
type ClusterUpdatingStatusConditionType string

const (
	// UpdatingStatusConditionTypeStarted indicates whether the cluster updating has started.
	// Its condition status can be one of the following:
	// - "True" means the cluster updating has started.
	// - "False" means the stage updating has not started.
	UpdatingStatusConditionTypeStarted ClusterUpdatingStatusConditionType = "Started"

	// UpdatingStatusConditionTypeSucceeded indicates whether the cluster updating is completed successfully.
	// Its condition status can be one of the following:
	// - "True" means the cluster updating is completed successfully.
	// - "False" means the  cluster updating encountered an error and stopped.
	UpdatingStatusConditionTypeSucceeded ClusterUpdatingStatusConditionType = "Succeeded"
)

type AfterStageTaskStatus struct {
	// The type of the post update task.
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
	// Conditions is an array of current observed conditions for the specific type of post update task.
	// Known conditions are "ApprovalRequestCreated", "WaitTimeElapsed", and "ApprovalRequestApproved".
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// AfterStageTaskType identifies a specific type of the AfterStageTask.
// +enum
type AfterStageTaskType string

const (
	// AfterStageTaskTypeTimedWait indicates the post stage task is a timed wait.
	AfterStageTaskTypeTimedWait AfterStageTaskType = "TimedWait"

	// AfterStageTaskTypeApproval indicates the post stage task is an approval.
	AfterStageTaskTypeApproval AfterStageTaskType = "Approval"
)

// AfterStageTaskConditionType identifies a specific condition of the AfterStageTask.
// +enum
type AfterStageTaskConditionType string

const (
	// AfterStageTaskConditionApprovalRequestCreated indicates whether the approval request is created.
	// Its condition status can be one of the following:
	// - "True" means the approval request is created.
	// - "False" means the approval request is not created.
	AfterStageTaskConditionApprovalRequestCreated AfterStageTaskConditionType = "ApprovalRequestCreated"

	// AfterStageTaskConditionApprovalRequestApproved indicates whether the approval request is approved.
	// Its condition status can be one of the following:
	// - "True" means the approval request is approved.
	// - "False" means the approval request is not approved.
	AfterStageTaskConditionApprovalRequestApproved AfterStageTaskConditionType = "ApprovalRequestApproved"

	// AfterStageTaskConditionApprovalWaitTimeElapsed indicates whether the wait time after each stage is elapsed.
	// We will fill the message of the condition of the remaining wait time if the status is "False".
	// Its condition status can be one of the following:
	// - "True" means the  wait time is elapsed.
	// - "False" means the  wait time is not elapsed.
	AfterStageTaskConditionApprovalWaitTimeElapsed AfterStageTaskConditionType = "WaitTimeElapsed"
)

// StagedUpdateRunList contains a list of StagedUpdateRun.
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type StagedUpdateRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StagedUpdateRun `json:"items"`
}

// +genclient
// +genclient:namespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",categories={fleet,fleet-placement}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ApprovalRequest defines a request for the approval from the user.
// The request object MUST have the following labels:
//   - `TargetUpdateRun` which points to the update run that this approval request is for.
//   - `TargetStage` which is the name of the stage that this approval request is for.
//   - `IsLatestUpdateRunApproval` which indicates whether this approval request is the latest one related to this update run.
type ApprovalRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ApprovalRequest.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="The spec field is immutable"
	// +kubebuilder:validation:Required
	Spec ApprovalRequestSpec `json:"spec"`

	// The desired state of ApprovalRequest.
	// +kubebuilder:validation:Optional
	Status ApprovalRequestStatus `json:"status,omitempty"`
}

// ApprovalRequestSpec defines the desired the update run approval request state.
// The entire spec is immutable.
type ApprovalRequestSpec struct {
	// The name of the staged update run that this approval request is for.
	// +kubebuilder:validation:Required
	TargetUpdateRun string `json:"parentStageRollout"`

	// The name of the update stage that this approval request is for.
	// +kubebuilder:validation:Required
	TargetStage string `json:"targetStage"`
}

// ApprovalRequestStatus defines the observed state of the ApprovalRequest.
type ApprovalRequestStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	//
	// Conditions is an array of current observed conditions for the specific type of post update task.
	// Known conditions are "Approved", and "ApprovalAccepted".
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ApprovalRequestConditionType identifies a specific condition of the ApprovalRequest.
type ApprovalRequestConditionType string

const (
	// ApprovalRequestConditionApproved indicates if the approval request was approved.
	// Its condition status can be one of the following:
	// - "True" means the request is approved.
	// - "False" means the request not approved.
	ApprovalRequestConditionApproved ApprovalRequestConditionType = "Approved"

	// ApprovalRequestConditionApprovalAccepted indicates whether the approval request is accepted by the update process.
	// Its condition status can be one of the following:
	// - "True" means the approval request is accepted.
	// - "False" means the approval request is not accepted.
	// - "Unknown" means it is not approved yet.
	ApprovalRequestConditionApprovalAccepted ApprovalRequestConditionType = "ApprovalAccepted"
)

// ApprovalRequestList contains a list of ApprovalRequest.
// +kubebuilder:resource:scope="Namespaced"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ApprovalRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ApprovalRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&StagedUpdateRun{}, &StagedUpdateRunList{}, &StagedUpdateStrategy{}, &StagedUpdateStrategyList{}, &ApprovalRequest{}, &ApprovalRequestList{},
	)
}
