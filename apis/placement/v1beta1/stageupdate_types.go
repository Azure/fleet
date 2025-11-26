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
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubefleet-dev/kubefleet/apis"
)

// make sure the UpdateRunObj and UpdateRunObjList interfaces are implemented by the
// ClusterStagedUpdateRun and StagedUpdateRun types.
var _ UpdateRunObj = &ClusterStagedUpdateRun{}
var _ UpdateRunObj = &StagedUpdateRun{}
var _ UpdateRunObjList = &ClusterStagedUpdateRunList{}
var _ UpdateRunObjList = &StagedUpdateRunList{}

// make sure the UpdateStrategyObj and UpdateStrategyObjList interfaces are implemented by the
// ClusterStagedUpdateStrategy and StagedUpdateStrategy types.
var _ UpdateStrategyObj = &ClusterStagedUpdateStrategy{}
var _ UpdateStrategyObj = &StagedUpdateStrategy{}
var _ UpdateStrategyObjList = &ClusterStagedUpdateStrategyList{}
var _ UpdateStrategyObjList = &StagedUpdateStrategyList{}

// make sure the ApprovalRequestObj and ApprovalRequestObjList interfaces are implemented by the
// ClusterApprovalRequest and ApprovalRequest types.
var _ ApprovalRequestObj = &ClusterApprovalRequest{}
var _ ApprovalRequestObj = &ApprovalRequest{}
var _ ApprovalRequestObjList = &ClusterApprovalRequestList{}
var _ ApprovalRequestObjList = &ApprovalRequestList{}

// UpdateRunSpecGetterSetter offers the functionality to work with UpdateRunSpec.
// +kubebuilder:object:generate=false
type UpdateRunSpecGetterSetter interface {
	GetUpdateRunSpec() *UpdateRunSpec
	SetUpdateRunSpec(UpdateRunSpec)
}

// UpdateRunStatusGetterSetter offers the functionality to work with UpdateRunStatus.
// +kubebuilder:object:generate=false
type UpdateRunStatusGetterSetter interface {
	GetUpdateRunStatus() *UpdateRunStatus
	SetUpdateRunStatus(UpdateRunStatus)
}

// UpdateRunObj offers the functionality to work with staged update run objects, including ClusterStagedUpdateRuns and StagedUpdateRuns.
// +kubebuilder:object:generate=false
type UpdateRunObj interface {
	apis.ConditionedObj
	UpdateRunSpecGetterSetter
	UpdateRunStatusGetterSetter
}

// UpdateRunListItemGetter offers the functionality to get a list of UpdateRunObj items.
// +kubebuilder:object:generate=false
type UpdateRunListItemGetter interface {
	GetUpdateRunObjs() []UpdateRunObj
}

// UpdateRunObjList offers the functionality to work with staged update run object list.
// +kubebuilder:object:generate=false
type UpdateRunObjList interface {
	client.ObjectList
	UpdateRunListItemGetter
}

// +genclient
// +genclient:Cluster
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-placement},shortName=csur
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.spec.placementName`,name="Placement",type=string
// +kubebuilder:printcolumn:JSONPath=`.spec.resourceSnapshotIndex`,name="Resource-Snapshot-Index",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.policySnapshotIndexUsed`,name="Policy-Snapshot-Index",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Initialized")].status`,name="Initialized",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Progressing")].status`,name="Progressing",type=string
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

	// The desired state of ClusterStagedUpdateRun.
	// +kubebuilder:validation:Required
	Spec UpdateRunSpec `json:"spec"`

	// The observed status of ClusterStagedUpdateRun.
	// +kubebuilder:validation:Optional
	Status UpdateRunStatus `json:"status,omitempty"`
}

// GetCondition returns the condition of the ClusterStagedUpdateRun.
func (c *ClusterStagedUpdateRun) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(c.Status.Conditions, conditionType)
}

// SetConditions sets the conditions of the ClusterStagedUpdateRun.
func (c *ClusterStagedUpdateRun) SetConditions(conditions ...metav1.Condition) {
	c.Status.Conditions = conditions
}

// GetUpdateRunSpec returns the staged update run spec.
func (c *ClusterStagedUpdateRun) GetUpdateRunSpec() *UpdateRunSpec {
	return &c.Spec
}

// SetUpdateRunSpec sets the staged update run spec.
func (c *ClusterStagedUpdateRun) SetUpdateRunSpec(spec UpdateRunSpec) {
	c.Spec = spec
}

// GetUpdateRunStatus returns the staged update run status.
func (c *ClusterStagedUpdateRun) GetUpdateRunStatus() *UpdateRunStatus {
	return &c.Status
}

// SetUpdateRunStatus sets the staged update run status.
func (c *ClusterStagedUpdateRun) SetUpdateRunStatus(status UpdateRunStatus) {
	c.Status = status
}

// State represents the desired state of an update run.
// +enum
type State string

const (
	// StateNotStarted describes user intent to initialize but not execute the update run.
	// This is the default state when an update run is created.
	StateNotStarted State = "Initialize"

	// StateStarted describes user intent to execute (or resume execution if paused).
	// Users can subsequently set the state to Pause or Abandon.
	StateStarted State = "Execute"

	// StateStopped describes user intent to pause the update run.
	// Users can subsequently set the state to Execute or Abandon.
	StateStopped State = "Pause"

	// StateAbandoned describes user intent to abandon the update run.
	// This is a terminal state; once set, it cannot be changed.
	StateAbandoned State = "Abandon"
)

// UpdateRunSpec defines the desired rollout strategy and the snapshot indices of the resources to be updated.
// It specifies a stage-by-stage update process across selected clusters for the given ResourcePlacement object.
// +kubebuilder:validation:XValidation:rule="!(has(oldSelf.state) && oldSelf.state == 'Initialize' && self.state == 'Pause')",message="invalid state transition: cannot transition from Initialize to Pause"
// +kubebuilder:validation:XValidation:rule="!(has(oldSelf.state) && oldSelf.state == 'Execute' && self.state == 'Initialize')",message="invalid state transition: cannot transition from Execute to Initialize"
// +kubebuilder:validation:XValidation:rule="!(has(oldSelf.state) && oldSelf.state == 'Pause' && self.state == 'Initialize')",message="invalid state transition: cannot transition from Pause to Initialize"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.state) || oldSelf.state != 'Abandon' || self.state == 'Abandon'",message="invalid state transition: Abandon is a terminal state and cannot transition to any other state"
type UpdateRunSpec struct {
	// PlacementName is the name of placement that this update run is applied to.
	// There can be multiple active update runs for each placement, but
	// it's up to the DevOps team to ensure they don't conflict with each other.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="placementName is immutable"
	PlacementName string `json:"placementName"`

	// The resource snapshot index of the selected resources to be updated across clusters.
	// The index represents a group of resource snapshots that includes all the resources a ResourcePlacement selected.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="resourceSnapshotIndex is immutable"
	// +kubebuilder:validation:Optional
	ResourceSnapshotIndex string `json:"resourceSnapshotIndex"`

	// The name of the update strategy that specifies the stages and the sequence
	// in which the selected resources will be updated on the member clusters. The stages
	// are computed according to the referenced strategy when the update run starts
	// and recorded in the status field.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="stagedRolloutStrategyName is immutable"
	StagedUpdateStrategyName string `json:"stagedRolloutStrategyName"`

	// State indicates the desired state of the update run.
	// Initialize: The update run should be initialized but execution should not start (default).
	// Execute: The update run should execute or resume execution.
	// Pause: The update run should pause execution.
	// Abandon: The update run should be abandoned and terminated.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=Initialize
	// +kubebuilder:validation:Enum=Initialize;Execute;Pause;Abandon
	State State `json:"state,omitempty"`
}

// UpdateStrategySpecGetterSetter offers the functionality to work with UpdateStrategySpec.
// +kubebuilder:object:generate=false
type UpdateStrategySpecGetterSetter interface {
	GetUpdateStrategySpec() *UpdateStrategySpec
	SetUpdateStrategySpec(UpdateStrategySpec)
}

// UpdateStrategyObj offers the functionality to work with staged update strategy objects, including ClusterStagedUpdateStrategies and StagedUpdateStrategies.
// +kubebuilder:object:generate=false
type UpdateStrategyObj interface {
	client.Object
	UpdateStrategySpecGetterSetter
}

// UpdateStrategyListItemGetter offers the functionality to get a list of UpdateStrategyObj items.
// +kubebuilder:object:generate=false
type UpdateStrategyListItemGetter interface {
	GetUpdateStrategyObjs() []UpdateStrategyObj
}

// UpdateStrategyObjList offers the functionality to work with staged update strategy object list.
// +kubebuilder:object:generate=false
type UpdateStrategyObjList interface {
	client.ObjectList
	UpdateStrategyListItemGetter
}

// +genclient
// +genclient:cluster
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-placement},shortName=csus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion

// ClusterStagedUpdateStrategy defines a reusable strategy that specifies the stages and the sequence
// in which the selected cluster resources will be updated on the member clusters.
type ClusterStagedUpdateStrategy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ClusterStagedUpdateStrategy.
	// +kubebuilder:validation:Required
	Spec UpdateStrategySpec `json:"spec"`
}

// GetUpdateStrategySpec returns the staged update strategy spec.
func (c *ClusterStagedUpdateStrategy) GetUpdateStrategySpec() *UpdateStrategySpec {
	return &c.Spec
}

// SetUpdateStrategySpec sets the staged update strategy spec.
func (c *ClusterStagedUpdateStrategy) SetUpdateStrategySpec(spec UpdateStrategySpec) {
	c.Spec = spec
}

// UpdateStrategySpec defines the desired state of the StagedUpdateStrategy.
type UpdateStrategySpec struct {
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

// GetUpdateStrategyObjs returns the update strategy objects in the list.
func (c *ClusterStagedUpdateStrategyList) GetUpdateStrategyObjs() []UpdateStrategyObj {
	objs := make([]UpdateStrategyObj, len(c.Items))
	for i := range c.Items {
		objs[i] = &c.Items[i]
	}
	return objs
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
	// If the label selector is empty, the stage includes all the selected clusters.
	// If the label selector is nil, the stage does not include any selected clusters.
	// +kubebuilder:validation:Optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// The label key used to sort the selected clusters.
	// The clusters within the stage are updated sequentially following the rule below:
	//   - primary: Ascending order based on the value of the label key, interpreted as integers if present.
	//   - secondary: Ascending order based on the name of the cluster if the label key is absent or the label value is the same.
	// +kubebuilder:validation:Optional
	SortingLabelKey *string `json:"sortingLabelKey,omitempty"`

	// MaxConcurrency specifies the maximum number of clusters that can be updated concurrently within this stage.
	// Value can be an absolute number (ex: 5) or a percentage of the total clusters in the stage (ex: 50%).
	// Fractional results are rounded down. A minimum of 1 update is enforced.
	// If not specified, all clusters in the stage are updated sequentially (effectively maxConcurrency = 1).
	// Defaults to 1.
	// +kubebuilder:default=1
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern="^(100|[1-9][0-9]?)%$"
	// +kubebuilder:validation:XValidation:rule="self == null || type(self) != int || self >= 1",message="maxConcurrency must be at least 1"
	// +kubebuilder:validation:Optional
	MaxConcurrency *intstr.IntOrString `json:"maxConcurrency,omitempty"`

	// The collection of tasks that each stage needs to complete successfully before moving to the next stage.
	// Each task is executed in parallel and there cannot be more than one task of the same type.
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:XValidation:rule="!self.exists(e, e.type == 'Approval' && has(e.waitTime))",message="AfterStageTaskType is Approval, waitTime is not allowed"
	// +kubebuilder:validation:XValidation:rule="!self.exists(e, e.type == 'TimedWait' && !has(e.waitTime))",message="AfterStageTaskType is TimedWait, waitTime is required"
	AfterStageTasks []StageTask `json:"afterStageTasks,omitempty"`

	// The collection of tasks that needs to completed successfully by each stage before starting the stage.
	// Each task is executed in parallel and there cannot be more than one task of the same type.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:validation:XValidation:rule="!self.exists(e, e.type == 'Approval' && has(e.waitTime))",message="AfterStageTaskType is Approval, waitTime is not allowed"
	// +kubebuilder:validation:XValidation:rule="!self.exists(e, e.type == 'TimedWait')",message="BeforeStageTaskType cannot be TimedWait"
	BeforeStageTasks []StageTask `json:"beforeStageTasks,omitempty"`
}

// StageTask is the pre or post stage task that needs to be completed before starting or moving to the next stage.
type StageTask struct {
	// The type of the before or after stage task.
	// +kubebuilder:validation:Enum=TimedWait;Approval
	// +kubebuilder:validation:Required
	Type StageTaskType `json:"type"`

	// The time to wait after all the clusters in the current stage complete the update before moving to the next stage.
	// +kubebuilder:validation:Pattern="^0|([0-9]+(\\.[0-9]+)?(s|m|h))+$"
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Optional
	WaitTime *metav1.Duration `json:"waitTime,omitempty"`
}

// UpdateRunStatus defines the observed state of the ClusterStagedUpdateRun.
type UpdateRunStatus struct {
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

	// ResourceSnapshotIndexUsed records the resource snapshot index that the update run is based on.
	// The index represents the same resource snapshots as specified in the spec field, or the latest.
	// +kubbebuilder:validation:Optional
	ResourceSnapshotIndexUsed string `json:"resourceSnapshotIndexUsed,omitempty"`

	// ApplyStrategy is the apply strategy that the stagedUpdateRun is using.
	// It is the same as the apply strategy in the CRP when the staged update run starts.
	// The apply strategy is not updated during the update run even if it changes in the CRP.
	// +kubebuilder:validation:Optional
	ApplyStrategy *ApplyStrategy `json:"appliedStrategy,omitempty"`

	// UpdateStrategySnapshot is the snapshot of the UpdateStrategy used for the update run.
	// The snapshot is immutable during the update run.
	// The strategy is applied to the list of clusters scheduled by the CRP according to the current policy.
	// The update run fails to initialize if the strategy fails to produce a valid list of stages where each selected
	// cluster is included in exactly one stage.
	// +kubebuilder:validation:Optional
	UpdateStrategySnapshot *UpdateStrategySpec `json:"stagedUpdateStrategySnapshot,omitempty"`

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
	// - "False": The staged update run is waiting/paused/abandoned.
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
	AfterStageTaskStatus []StageTaskStatus `json:"afterStageTaskStatus,omitempty"`

	// The status of the pre-update tasks associated with the current stage.
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:validation:Optional
	BeforeStageTaskStatus []StageTaskStatus `json:"beforeStageTaskStatus,omitempty"`

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
	ResourceOverrideSnapshots []NamespacedName `json:"resourceOverrideSnapshots,omitempty"`

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

type StageTaskStatus struct {
	// The type of the pre or post update task.
	// +kubebuilder:validation:Enum=TimedWait;Approval
	// +kubebuilder:validation:Required
	Type StageTaskType `json:"type"`

	// The name of the approval request object that is created for this stage.
	// Only valid if the AfterStageTaskType is Approval.
	// +kubebuilder:validation:Optional
	ApprovalRequestName string `json:"approvalRequestName,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	//
	// Conditions is an array of current observed conditions for the specific type of pre or post update task.
	// Known conditions are "ApprovalRequestCreated", "WaitTimeElapsed", and "ApprovalRequestApproved".
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// StageTaskType identifies a specific type of the AfterStageTask or BeforeStageTask.
// +enum
type StageTaskType string

const (
	// StageTaskTypeTimedWait indicates the stage task is a timed wait.
	StageTaskTypeTimedWait StageTaskType = "TimedWait"

	// StageTaskTypeApproval indicates the stage task is an approval.
	StageTaskTypeApproval StageTaskType = "Approval"
)

// StageTaskConditionType identifies a specific condition of the AfterStageTask or BeforeStageTask.
// +enum
type StageTaskConditionType string

const (
	// StageTaskConditionApprovalRequestCreated indicates if the approval request has been created.
	// Its condition status can be:
	// - "True": The approval request has been created.
	StageTaskConditionApprovalRequestCreated StageTaskConditionType = "ApprovalRequestCreated"

	// StageTaskConditionApprovalRequestApproved indicates if the approval request has been approved.
	// Its condition status can be:
	// - "True": The approval request has been approved.
	StageTaskConditionApprovalRequestApproved StageTaskConditionType = "ApprovalRequestApproved"

	// StageTaskConditionWaitTimeElapsed indicates if the wait time after each stage has elapsed.
	// If the status is "False", the condition message will include the remaining wait time.
	// Its condition status can be:
	// - "True": The wait time has elapsed.
	// - "False": The wait time has not elapsed.
	StageTaskConditionWaitTimeElapsed StageTaskConditionType = "WaitTimeElapsed"
)

// ClusterStagedUpdateRunList contains a list of ClusterStagedUpdateRun.
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterStagedUpdateRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterStagedUpdateRun `json:"items"`
}

// GetUpdateRunObjs returns the update run objects in the list.
func (c *ClusterStagedUpdateRunList) GetUpdateRunObjs() []UpdateRunObj {
	objs := make([]UpdateRunObj, len(c.Items))
	for i := range c.Items {
		objs[i] = &c.Items[i]
	}
	return objs
}

// ApprovalRequestSpecGetterSetter offers the functionality to work with ApprovalRequestSpec.
// +kubebuilder:object:generate=false
type ApprovalRequestSpecGetterSetter interface {
	GetApprovalRequestSpec() *ApprovalRequestSpec
	SetApprovalRequestSpec(ApprovalRequestSpec)
}

// ApprovalRequestStatusGetterSetter offers the functionality to work with ApprovalRequestStatus.
// +kubebuilder:object:generate=false
type ApprovalRequestStatusGetterSetter interface {
	GetApprovalRequestStatus() *ApprovalRequestStatus
	SetApprovalRequestStatus(ApprovalRequestStatus)
}

// ApprovalRequestObj offers the functionality to work with approval request objects, including ClusterApprovalRequests and ApprovalRequests.
// +kubebuilder:object:generate=false
type ApprovalRequestObj interface {
	apis.ConditionedObj
	ApprovalRequestSpecGetterSetter
	ApprovalRequestStatusGetterSetter
}

// ApprovalRequestListItemGetter offers the functionality to get a list of ApprovalRequestObj items.
// +kubebuilder:object:generate=false
type ApprovalRequestListItemGetter interface {
	GetApprovalRequestObjs() []ApprovalRequestObj
}

// ApprovalRequestObjList offers the functionality to work with approval request object list.
// +kubebuilder:object:generate=false
type ApprovalRequestObjList interface {
	client.ObjectList
	ApprovalRequestListItemGetter
}

// +genclient
// +genclient:Cluster
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-placement},shortName=careq
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion
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

// GetCondition returns the condition of the ClusterApprovalRequest.
func (c *ClusterApprovalRequest) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(c.Status.Conditions, conditionType)
}

// SetConditions sets the conditions of the ClusterApprovalRequest.
func (c *ClusterApprovalRequest) SetConditions(conditions ...metav1.Condition) {
	c.Status.Conditions = conditions
}

// GetApprovalRequestSpec returns the approval request spec.
func (c *ClusterApprovalRequest) GetApprovalRequestSpec() *ApprovalRequestSpec {
	return &c.Spec
}

// SetApprovalRequestSpec sets the approval request spec.
func (c *ClusterApprovalRequest) SetApprovalRequestSpec(spec ApprovalRequestSpec) {
	c.Spec = spec
}

// GetApprovalRequestStatus returns the approval request status.
func (c *ClusterApprovalRequest) GetApprovalRequestStatus() *ApprovalRequestStatus {
	return &c.Status
}

// SetApprovalRequestStatus sets the approval request status.
func (c *ClusterApprovalRequest) SetApprovalRequestStatus(status ApprovalRequestStatus) {
	c.Status = status
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

	// ApprovalRequestConditionApprovalAccepted indicates if the approved approval request was accepted.
	// Its condition status can be:
	// - "True": The request is approved.
	ApprovalRequestConditionApprovalAccepted ApprovalRequestConditionType = "ApprovalAccepted"
)

// ClusterApprovalRequestList contains a list of ClusterApprovalRequest.
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterApprovalRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterApprovalRequest `json:"items"`
}

// GetApprovalRequestObjs returns the approval request objects in the list.
func (c *ClusterApprovalRequestList) GetApprovalRequestObjs() []ApprovalRequestObj {
	objs := make([]ApprovalRequestObj, len(c.Items))
	for i := range c.Items {
		objs[i] = &c.Items[i]
	}
	return objs
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={fleet,fleet-placement},shortName=sur
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.spec.placementName`,name="Placement",type=string
// +kubebuilder:printcolumn:JSONPath=`.spec.resourceSnapshotIndex`,name="Resource-Snapshot-Index",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.policySnapshotIndexUsed`,name="Policy-Snapshot-Index",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Initialized")].status`,name="Initialized",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Progressing")].status`,name="Progressing",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Succeeded")].status`,name="Succeeded",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +kubebuilder:printcolumn:JSONPath=`.spec.stagedRolloutStrategyName`,name="Strategy",priority=1,type=string
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) < 128",message="metadata.name max length is 127"

// StagedUpdateRun represents a stage by stage update process that applies ResourcePlacement
// selected resources to specified clusters.
// Resources from unselected clusters are removed after all stages in the update strategy are completed.
// Each StagedUpdateRun object corresponds to a single release of a specific resource version.
// The release is abandoned if the StagedUpdateRun object is deleted or the scheduling decision changes.
// The name of the StagedUpdateRun must conform to RFC 1123.
type StagedUpdateRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of StagedUpdateRun.
	// +kubebuilder:validation:Required
	Spec UpdateRunSpec `json:"spec"`

	// The observed status of StagedUpdateRun.
	// +kubebuilder:validation:Optional
	Status UpdateRunStatus `json:"status,omitempty"`
}

// GetCondition returns the condition of the StagedUpdateRun.
func (s *StagedUpdateRun) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(s.Status.Conditions, conditionType)
}

// SetConditions sets the conditions of the StagedUpdateRun.
func (s *StagedUpdateRun) SetConditions(conditions ...metav1.Condition) {
	s.Status.Conditions = conditions
}

// GetUpdateRunSpec returns the staged update run spec.
func (s *StagedUpdateRun) GetUpdateRunSpec() *UpdateRunSpec {
	return &s.Spec
}

// SetUpdateRunSpec sets the staged update run spec.
func (s *StagedUpdateRun) SetUpdateRunSpec(spec UpdateRunSpec) {
	s.Spec = spec
}

// GetUpdateRunStatus returns the staged update run status.
func (s *StagedUpdateRun) GetUpdateRunStatus() *UpdateRunStatus {
	return &s.Status
}

// SetUpdateRunStatus sets the staged update run status.
func (s *StagedUpdateRun) SetUpdateRunStatus(status UpdateRunStatus) {
	s.Status = status
}

// StagedUpdateRunList contains a list of StagedUpdateRun.
// +kubebuilder:resource:scope=Namespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type StagedUpdateRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StagedUpdateRun `json:"items"`
}

// GetUpdateRunObjs returns the update run objects in the list.
func (s *StagedUpdateRunList) GetUpdateRunObjs() []UpdateRunObj {
	objs := make([]UpdateRunObj, len(s.Items))
	for i := range s.Items {
		objs[i] = &s.Items[i]
	}
	return objs
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet,fleet-placement},shortName=sus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion

// StagedUpdateStrategy defines a reusable strategy that specifies the stages and the sequence
// in which the selected cluster resources will be updated on the member clusters.
type StagedUpdateStrategy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of StagedUpdateStrategy.
	// +kubebuilder:validation:Required
	Spec UpdateStrategySpec `json:"spec"`
}

// GetUpdateStrategySpec returns the staged update strategy spec.
func (s *StagedUpdateStrategy) GetUpdateStrategySpec() *UpdateStrategySpec {
	return &s.Spec
}

// SetUpdateStrategySpec sets the staged update strategy spec.
func (s *StagedUpdateStrategy) SetUpdateStrategySpec(spec UpdateStrategySpec) {
	s.Spec = spec
}

// StagedUpdateStrategyList contains a list of StagedUpdateStrategy.
// +kubebuilder:resource:scope=Namespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type StagedUpdateStrategyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StagedUpdateStrategy `json:"items"`
}

// GetUpdateStrategyObjs returns the update strategy objects in the list.
func (s *StagedUpdateStrategyList) GetUpdateStrategyObjs() []UpdateStrategyObj {
	objs := make([]UpdateStrategyObj, len(s.Items))
	for i := range s.Items {
		objs[i] = &s.Items[i]
	}
	return objs
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={fleet,fleet-placement},shortName=areq
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.spec.parentStageRollout`,name="Update-Run",type=string
// +kubebuilder:printcolumn:JSONPath=`.spec.targetStage`,name="Stage",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="Approved")].status`,name="Approved",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// ApprovalRequest defines a request for user approval for staged update run.
// The request object MUST have the following labels:
//   - `TargetUpdateRun`: Points to the staged update run that this approval request is for.
//   - `TargetStage`: The name of the stage that this approval request is for.
//   - `IsLatestUpdateRunApproval`: Indicates whether this approval request is the latest one related to this update run.
type ApprovalRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of ApprovalRequest.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="The spec field is immutable"
	// +kubebuilder:validation:Required
	Spec ApprovalRequestSpec `json:"spec"`

	// The observed state of ApprovalRequest.
	// +kubebuilder:validation:Optional
	Status ApprovalRequestStatus `json:"status,omitempty"`
}

// GetCondition returns the condition of the ApprovalRequest.
func (a *ApprovalRequest) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(a.Status.Conditions, conditionType)
}

// SetConditions sets the conditions of the ApprovalRequest.
func (a *ApprovalRequest) SetConditions(conditions ...metav1.Condition) {
	a.Status.Conditions = conditions
}

// GetApprovalRequestSpec returns the approval request spec.
func (a *ApprovalRequest) GetApprovalRequestSpec() *ApprovalRequestSpec {
	return &a.Spec
}

// SetApprovalRequestSpec sets the approval request spec.
func (a *ApprovalRequest) SetApprovalRequestSpec(spec ApprovalRequestSpec) {
	a.Spec = spec
}

// GetApprovalRequestStatus returns the approval request status.
func (a *ApprovalRequest) GetApprovalRequestStatus() *ApprovalRequestStatus {
	return &a.Status
}

// SetApprovalRequestStatus sets the approval request status.
func (a *ApprovalRequest) SetApprovalRequestStatus(status ApprovalRequestStatus) {
	a.Status = status
}

// ApprovalRequestList contains a list of ApprovalRequest.
// +kubebuilder:resource:scope=Namespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ApprovalRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ApprovalRequest `json:"items"`
}

// GetApprovalRequestObjs returns the approval request objects in the list.
func (a *ApprovalRequestList) GetApprovalRequestObjs() []ApprovalRequestObj {
	objs := make([]ApprovalRequestObj, len(a.Items))
	for i := range a.Items {
		objs[i] = &a.Items[i]
	}
	return objs
}

func init() {
	SchemeBuilder.Register(
		&ClusterStagedUpdateRun{}, &ClusterStagedUpdateRunList{}, &ClusterStagedUpdateStrategy{}, &ClusterStagedUpdateStrategyList{}, &ClusterApprovalRequest{}, &ClusterApprovalRequestList{},
		&StagedUpdateRun{}, &StagedUpdateRunList{}, &StagedUpdateStrategy{}, &StagedUpdateStrategyList{}, &ApprovalRequest{}, &ApprovalRequestList{},
	)
}
