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

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
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
			wantStatus.DeletionStageStatus = buildDeletionStageStatus(wantUnscheduledClusters, updateRun)
			wantStatus.Conditions = updateRunSucceedConditions(updateRun.Generation)
		} else {
			wantStatus.StagesStatus = buildStageUpdatingStatusesForInitialized(wantStrategySpec, wantSelectedClusters, wantCROs, wantROs, updateRun)
			wantStatus.DeletionStageStatus = buildDeletionStatusWithoutConditions(wantUnscheduledClusters, updateRun)
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
			wantStatus.DeletionStageStatus = buildDeletionStageStatus(wantUnscheduledClusters, updateRun)
			wantStatus.Conditions = updateRunSucceedConditions(updateRun.Generation)
		} else {
			wantStatus.StagesStatus = buildStageUpdatingStatusesForInitialized(wantStrategySpec, wantSelectedClusters, wantCROs, wantROs, updateRun)
			wantStatus.DeletionStageStatus = buildDeletionStatusWithoutConditions(wantUnscheduledClusters, updateRun)
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
		stagesStatus[i].BeforeStageTaskStatus = make([]placementv1beta1.StageTaskStatus, len(stage.BeforeStageTasks))
		for j, task := range stage.BeforeStageTasks {
			stagesStatus[i].BeforeStageTaskStatus[j].Type = task.Type
			if task.Type == placementv1beta1.StageTaskTypeApproval {
				stagesStatus[i].BeforeStageTaskStatus[j].ApprovalRequestName = fmt.Sprintf(placementv1beta1.BeforeStageApprovalTaskNameFmt, updateRun.GetName(), stage.Name)
			}
		}
		stagesStatus[i].AfterStageTaskStatus = make([]placementv1beta1.StageTaskStatus, len(stage.AfterStageTasks))
		for j, task := range stage.AfterStageTasks {
			stagesStatus[i].AfterStageTaskStatus[j].Type = task.Type
			if task.Type == placementv1beta1.StageTaskTypeApproval {
				stagesStatus[i].AfterStageTaskStatus[j].ApprovalRequestName = fmt.Sprintf(placementv1beta1.AfterStageApprovalTaskNameFmt, updateRun.GetName(), stage.Name)
			}
		}
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
		stagesStatus[i].BeforeStageTaskStatus = make([]placementv1beta1.StageTaskStatus, len(stage.BeforeStageTasks))
		for j, task := range stage.BeforeStageTasks {
			stagesStatus[i].BeforeStageTaskStatus[j].Type = task.Type
			if task.Type == placementv1beta1.StageTaskTypeApproval {
				stagesStatus[i].BeforeStageTaskStatus[j].ApprovalRequestName = fmt.Sprintf(placementv1beta1.BeforeStageApprovalTaskNameFmt, updateRun.GetName(), stage.Name)
			}
			stagesStatus[i].BeforeStageTaskStatus[j].Conditions = updateRunStageTaskSucceedConditions(updateRun.GetGeneration(), task.Type)
		}
		stagesStatus[i].AfterStageTaskStatus = make([]placementv1beta1.StageTaskStatus, len(stage.AfterStageTasks))
		for j, task := range stage.AfterStageTasks {
			stagesStatus[i].AfterStageTaskStatus[j].Type = task.Type
			if task.Type == placementv1beta1.StageTaskTypeApproval {
				stagesStatus[i].AfterStageTaskStatus[j].ApprovalRequestName = fmt.Sprintf(placementv1beta1.AfterStageApprovalTaskNameFmt, updateRun.GetName(), stage.Name)
			}
			stagesStatus[i].AfterStageTaskStatus[j].Conditions = updateRunStageTaskSucceedConditions(updateRun.GetGeneration(), task.Type)
		}
		stagesStatus[i].Conditions = updateRunStageRolloutSucceedConditions(updateRun.GetGeneration())
	}
	return stagesStatus
}

func buildDeletionStageStatus(
	wantUnscheduledClusters []string,
	updateRun placementv1beta1.UpdateRunObj,
) *placementv1beta1.StageUpdatingStatus {
	deleteStageStatus := buildDeletionStatusWithoutConditions(wantUnscheduledClusters, updateRun)
	deleteStageStatus.Conditions = updateRunStageRolloutSucceedConditions(updateRun.GetGeneration())
	return deleteStageStatus
}

func buildDeletionStatusWithoutConditions(
	wantUnscheduledClusters []string,
	updateRun placementv1beta1.UpdateRunObj,
) *placementv1beta1.StageUpdatingStatus {
	deleteStageStatus := &placementv1beta1.StageUpdatingStatus{
		StageName: "kubernetes-fleet.io/deleteStage",
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
