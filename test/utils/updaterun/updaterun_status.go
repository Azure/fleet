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

package updaterun

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
)

var (
	lessFuncCondition = func(a, b metav1.Condition) bool {
		return a.Type < b.Type
	}
	updateRunStatusCmpOption = cmp.Options{
		cmpopts.SortSlices(lessFuncCondition),
		utils.IgnoreConditionLTTAndMessageFields,
		cmpopts.IgnoreFields(placementv1beta1.StageUpdatingStatus{}, "StartTime", "EndTime"),
		cmpopts.EquateEmpty(),
	}
)

// ClusterStagedUpdateRunStatusSucceededActual verifies the status of the ClusterStagedUpdateRun.
func ClusterStagedUpdateRunStatusSucceededActual(
	ctx context.Context,
	hubClient client.Client,
	updateRunName string,
	wantResourceIndex string,
	wantPolicyIndex string,
	wantClusterCount int,
	wantApplyStrategy *placementv1beta1.ApplyStrategy,
	wantStrategySpec *placementv1beta1.UpdateStrategySpec,
	wantSelectedClusters [][]string,
	wantUnscheduledClusters []string,
	wantCROs map[string][]string,
	wantROs map[string][]placementv1beta1.NamespacedName,
	execute bool,
) func() error {
	return func() error {
		updateRun := &placementv1beta1.ClusterStagedUpdateRun{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: updateRunName}, updateRun); err != nil {
			return err
		}

		wantStatus := placementv1beta1.UpdateRunStatus{
			PolicySnapshotIndexUsed:    wantPolicyIndex,
			ResourceSnapshotIndexUsed:  wantResourceIndex,
			PolicyObservedClusterCount: wantClusterCount,
			ApplyStrategy:              wantApplyStrategy.DeepCopy(),
			UpdateStrategySnapshot:     wantStrategySpec,
		}

		if execute {
			wantStatus.StagesStatus = buildStageUpdatingStatuses(wantStrategySpec, wantSelectedClusters, wantCROs, wantROs, updateRun)
			wantStatus.DeletionStageStatus = buildDeletionStageStatus(wantStrategySpec, wantUnscheduledClusters, updateRun)
			wantStatus.Conditions = updateRunSucceedConditions(updateRun.Generation)
		} else {
			wantStatus.StagesStatus = buildStageUpdatingStatusesForInitialized(wantStrategySpec, wantSelectedClusters, wantCROs, wantROs, updateRun)
			wantStatus.DeletionStageStatus = buildDeletionStatusWithoutConditions(wantStrategySpec, wantUnscheduledClusters, updateRun)
			wantStatus.Conditions = updateRunInitializedConditions(updateRun.Generation)
		}
		if diff := cmp.Diff(updateRun.Status, wantStatus, updateRunStatusCmpOption...); diff != "" {
			return fmt.Errorf("UpdateRun status diff (-got, +want): %s", diff)
		}
		return nil
	}
}

// StagedUpdateRunStatusSucceededActual verifies the status of the StagedUpdateRun.
func StagedUpdateRunStatusSucceededActual(
	ctx context.Context,
	hubClient client.Client,
	updateRunName, namespace string,
	wantResourceIndex, wantPolicyIndex string,
	wantClusterCount int,
	wantApplyStrategy *placementv1beta1.ApplyStrategy,
	wantStrategySpec *placementv1beta1.UpdateStrategySpec,
	wantSelectedClusters [][]string,
	wantUnscheduledClusters []string,
	wantCROs map[string][]string,
	wantROs map[string][]placementv1beta1.NamespacedName,
	execute bool,
) func() error {
	return func() error {
		updateRun := &placementv1beta1.StagedUpdateRun{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: updateRunName, Namespace: namespace}, updateRun); err != nil {
			return err
		}

		wantStatus := placementv1beta1.UpdateRunStatus{
			PolicySnapshotIndexUsed:    wantPolicyIndex,
			ResourceSnapshotIndexUsed:  wantResourceIndex,
			PolicyObservedClusterCount: wantClusterCount,
			ApplyStrategy:              wantApplyStrategy.DeepCopy(),
			UpdateStrategySnapshot:     wantStrategySpec,
		}

		if execute {
			wantStatus.StagesStatus = buildStageUpdatingStatuses(wantStrategySpec, wantSelectedClusters, wantCROs, wantROs, updateRun)
			wantStatus.DeletionStageStatus = buildDeletionStageStatus(wantStrategySpec, wantUnscheduledClusters, updateRun)
			wantStatus.Conditions = updateRunSucceedConditions(updateRun.Generation)
		} else {
			wantStatus.StagesStatus = buildStageUpdatingStatusesForInitialized(wantStrategySpec, wantSelectedClusters, wantCROs, wantROs, updateRun)
			wantStatus.DeletionStageStatus = buildDeletionStatusWithoutConditions(wantStrategySpec, wantUnscheduledClusters, updateRun)
			wantStatus.Conditions = updateRunInitializedConditions(updateRun.Generation)
		}
		if diff := cmp.Diff(updateRun.Status, wantStatus, updateRunStatusCmpOption...); diff != "" {
			return fmt.Errorf("UpdateRun status diff (-got, +want): %s", diff)
		}
		return nil
	}
}

func buildStageUpdatingStatusesForInitialized(
	wantStrategySpec *placementv1beta1.UpdateStrategySpec,
	wantSelectedClusters [][]string,
	wantCROs map[string][]string,
	wantROs map[string][]placementv1beta1.NamespacedName,
	updateRun placementv1beta1.UpdateRunObj,
) []placementv1beta1.StageUpdatingStatus {
	stagesStatus := make([]placementv1beta1.StageUpdatingStatus, len(wantStrategySpec.Stages))
	for i, stage := range wantStrategySpec.Stages {
		stagesStatus[i].StageName = stage.Name
		stagesStatus[i].Clusters = make([]placementv1beta1.ClusterUpdatingStatus, len(wantSelectedClusters[i]))
		for j := range stagesStatus[i].Clusters {
			stagesStatus[i].Clusters[j].ClusterName = wantSelectedClusters[i][j]
			stagesStatus[i].Clusters[j].ClusterResourceOverrideSnapshots = wantCROs[wantSelectedClusters[i][j]]
			stagesStatus[i].Clusters[j].ResourceOverrideSnapshots = wantROs[wantSelectedClusters[i][j]]
		}
		stagesStatus[i].BeforeStageTaskStatus = buildStageTaskStatuses(stage.BeforeStageTasks, placementv1beta1.BeforeStageApprovalTaskNameFmt, stage.Name, updateRun, false)
		stagesStatus[i].AfterStageTaskStatus = buildStageTaskStatuses(stage.AfterStageTasks, placementv1beta1.AfterStageApprovalTaskNameFmt, stage.Name, updateRun, false)
	}
	return stagesStatus
}

func buildStageUpdatingStatuses(
	wantStrategySpec *placementv1beta1.UpdateStrategySpec,
	wantSelectedClusters [][]string,
	wantCROs map[string][]string,
	wantROs map[string][]placementv1beta1.NamespacedName,
	updateRun placementv1beta1.UpdateRunObj,
) []placementv1beta1.StageUpdatingStatus {
	stagesStatus := make([]placementv1beta1.StageUpdatingStatus, len(wantStrategySpec.Stages))
	for i, stage := range wantStrategySpec.Stages {
		stagesStatus[i].StageName = stage.Name
		stagesStatus[i].Clusters = make([]placementv1beta1.ClusterUpdatingStatus, len(wantSelectedClusters[i]))
		for j := range stagesStatus[i].Clusters {
			stagesStatus[i].Clusters[j].ClusterName = wantSelectedClusters[i][j]
			stagesStatus[i].Clusters[j].ClusterResourceOverrideSnapshots = wantCROs[wantSelectedClusters[i][j]]
			stagesStatus[i].Clusters[j].ResourceOverrideSnapshots = wantROs[wantSelectedClusters[i][j]]
			stagesStatus[i].Clusters[j].Conditions = updateRunClusterRolloutSucceedConditions(updateRun.GetGeneration())
		}
		// Skip populating task conditions if the stage has 0 clusters (stage is skipped).
		stageExecuted := len(wantSelectedClusters[i]) > 0
		stagesStatus[i].BeforeStageTaskStatus = buildStageTaskStatuses(stage.BeforeStageTasks, placementv1beta1.BeforeStageApprovalTaskNameFmt, stage.Name, updateRun, stageExecuted)
		stagesStatus[i].AfterStageTaskStatus = buildStageTaskStatuses(stage.AfterStageTasks, placementv1beta1.AfterStageApprovalTaskNameFmt, stage.Name, updateRun, stageExecuted)
		// Use skipped conditions if the stage has 0 clusters.
		if len(wantSelectedClusters[i]) == 0 {
			stagesStatus[i].Conditions = updateRunStageSkippedNoClustersConditions(updateRun.GetGeneration())
		} else {
			stagesStatus[i].Conditions = updateRunStageRolloutSucceedConditions(updateRun.GetGeneration())
		}
	}
	return stagesStatus
}

// buildStageTaskStatuses builds the statuses of the before or after stage tasks of a stage, which have their
// conditions set if the tasks are completed.
func buildStageTaskStatuses(
	tasks []placementv1beta1.StageTask,
	approvalRequestNameFmt, stageName string,
	updateRun placementv1beta1.UpdateRunObj,
	completed bool,
) []placementv1beta1.StageTaskStatus {
	taskStatuses := make([]placementv1beta1.StageTaskStatus, len(tasks))
	for i, task := range tasks {
		taskStatuses[i].Type = task.Type
		if task.Type == placementv1beta1.StageTaskTypeApproval {
			taskStatuses[i].ApprovalRequestName = fmt.Sprintf(approvalRequestNameFmt, updateRun.GetName(), stageName)
		}
		if completed {
			taskStatuses[i].Conditions = updateRunStageTaskSucceedConditions(updateRun.GetGeneration(), task.Type)
		}
	}
	return taskStatuses
}

func buildDeletionStageStatus(
	wantStrategySpec *placementv1beta1.UpdateStrategySpec,
	wantUnscheduledClusters []string,
	updateRun placementv1beta1.UpdateRunObj,
) *placementv1beta1.StageUpdatingStatus {
	deleteStageStatus := buildDeletionStatusWithoutConditions(wantStrategySpec, wantUnscheduledClusters, updateRun)
	deleteStageStatus.Conditions = updateRunStageRolloutSucceedConditions(updateRun.GetGeneration())
	// The tasks are skipped if the delete stage has 0 clusters.
	if len(wantUnscheduledClusters) > 0 {
		for i := range deleteStageStatus.BeforeStageTaskStatus {
			deleteStageStatus.BeforeStageTaskStatus[i].Conditions = updateRunStageTaskSucceedConditions(updateRun.GetGeneration(), deleteStageStatus.BeforeStageTaskStatus[i].Type)
		}
	}
	return deleteStageStatus
}

func buildDeletionStatusWithoutConditions(
	wantStrategySpec *placementv1beta1.UpdateStrategySpec,
	wantUnscheduledClusters []string,
	updateRun placementv1beta1.UpdateRunObj,
) *placementv1beta1.StageUpdatingStatus {
	deleteStageStatus := &placementv1beta1.StageUpdatingStatus{
		StageName: placementv1beta1.UpdateRunDeleteStageName,
	}
	if wantStrategySpec.DeleteStage != nil {
		deleteStageStatus.BeforeStageTaskStatus = buildStageTaskStatuses(wantStrategySpec.DeleteStage.BeforeStageTasks, placementv1beta1.BeforeStageApprovalTaskNameFmt, placementv1beta1.UpdateRunDeleteStageTaskName, updateRun, false)
	}
	deleteStageStatus.Clusters = make([]placementv1beta1.ClusterUpdatingStatus, len(wantUnscheduledClusters))
	for i := range deleteStageStatus.Clusters {
		deleteStageStatus.Clusters[i].ClusterName = wantUnscheduledClusters[i]
		deleteStageStatus.Clusters[i].Conditions = updateRunClusterRolloutSucceedConditions(updateRun.GetGeneration())
	}
	return deleteStageStatus
}

func updateRunClusterRolloutSucceedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.ClusterUpdatingConditionStarted),
			Status:             metav1.ConditionTrue,
			Reason:             condition.ClusterUpdatingStartedReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.ClusterUpdatingConditionSucceeded),
			Status:             metav1.ConditionTrue,
			Reason:             condition.ClusterUpdatingSucceededReason,
			ObservedGeneration: generation,
		},
	}
}

func updateRunStageRolloutSucceedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
			Status:             metav1.ConditionFalse,
			Reason:             condition.StageUpdatingSucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.StageUpdatingConditionSucceeded),
			Status:             metav1.ConditionTrue,
			Reason:             condition.StageUpdatingSucceededReason,
			ObservedGeneration: generation,
		},
	}
}

func updateRunStageSkippedNoClustersConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
			Status:             metav1.ConditionFalse,
			Reason:             condition.StageUpdatingSkippedNoClustersReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.StageUpdatingConditionSucceeded),
			Status:             metav1.ConditionTrue,
			Reason:             condition.StageUpdatingSkippedNoClustersReason,
			ObservedGeneration: generation,
		},
	}
}

func updateRunStageTaskSucceedConditions(generation int64, taskType placementv1beta1.StageTaskType) []metav1.Condition {
	if taskType == placementv1beta1.StageTaskTypeApproval {
		return []metav1.Condition{
			{
				Type:               string(placementv1beta1.StageTaskConditionApprovalRequestCreated),
				Status:             metav1.ConditionTrue,
				Reason:             condition.StageTaskApprovalRequestCreatedReason,
				ObservedGeneration: generation,
			},
			{
				Type:               string(placementv1beta1.StageTaskConditionApprovalRequestApproved),
				Status:             metav1.ConditionTrue,
				Reason:             condition.StageTaskApprovalRequestApprovedReason,
				ObservedGeneration: generation,
			},
		}
	}
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.StageTaskConditionWaitTimeElapsed),
			Status:             metav1.ConditionTrue,
			Reason:             condition.AfterStageTaskWaitTimeElapsedReason,
			ObservedGeneration: generation,
		},
	}
}

func updateRunSucceedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.StagedUpdateRunConditionInitialized),
			Status:             metav1.ConditionTrue,
			Reason:             condition.UpdateRunInitializeSucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.StagedUpdateRunConditionProgressing),
			Status:             metav1.ConditionFalse,
			Reason:             condition.UpdateRunSucceededReason,
			ObservedGeneration: generation,
		},
		{
			Type:               string(placementv1beta1.StagedUpdateRunConditionSucceeded),
			Status:             metav1.ConditionTrue,
			Reason:             condition.UpdateRunSucceededReason,
			ObservedGeneration: generation,
		},
	}
}

func updateRunInitializedConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(placementv1beta1.StagedUpdateRunConditionInitialized),
			Status:             metav1.ConditionTrue,
			Reason:             condition.UpdateRunInitializeSucceededReason,
			ObservedGeneration: generation,
		},
	}
}
