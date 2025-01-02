/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package updaterun

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/klog/v2"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

// validate validates the clusterStagedUpdateRun status and ensures the update can be continued.
// The function returns the index of the stage that is updating, and the list of clusters that are scheduled to be deleted.
// If the updating stage index is -1, it means all stages are finished, and the clusterStageUpdateRun should be marked as finished.
// If the updating stage index is 0, the next stage to be updated is the first stage.
// If the updating stage index is len(updateRun.Status.StagesStatus), the next stage to be updated will be the delete stage.
func (r *Reconciler) validate(
	ctx context.Context,
	updateRun *placementv1alpha1.ClusterStagedUpdateRun,
) (int, []*placementv1beta1.ClusterResourceBinding, []*placementv1beta1.ClusterResourceBinding, error) {
	// Some of the validating function changes the object, so we need to make a copy of the object.
	updateRunRef := klog.KObj(updateRun)
	updateRunCopy := updateRun.DeepCopy()
	klog.V(2).InfoS("Start to validate the clusterStagedUpdateRun", "clusterStagedUpdateRun", updateRunRef)

	// Validate the ClusterResourcePlacement object referenced by the ClusterStagedUpdateRun.
	placementName, err := r.validateCRP(ctx, updateRunCopy)
	if err != nil {
		return -1, nil, nil, err
	}
	// Validate the applyStrategy.
	if !reflect.DeepEqual(updateRun.Status.ApplyStrategy, updateRunCopy.Status.ApplyStrategy) {
		mismatchErr := controller.NewUserError(fmt.Errorf("the applyStrategy in the clusterStagedUpdateRun is outdated, latest: %v, recorded: %v", updateRunCopy.Status.ApplyStrategy, updateRun.Status.ApplyStrategy))
		klog.ErrorS(mismatchErr, "the applyStrategy in the clusterResourcePlacement has changed", "clusterResourcePlacement", placementName, "clusterStagedUpdateRun", updateRunRef)
		return -1, nil, nil, fmt.Errorf("%w: %s", errStagedUpdatedAborted, mismatchErr.Error())
	}

	// Retrieve the latest policy snapshot.
	latestPolicySnapshot, clusterCount, err := r.determinePolicySnapshot(ctx, placementName, updateRunCopy)
	if err != nil {
		return -1, nil, nil, err
	}
	// Make sure the latestPolicySnapshot has not changed.
	if updateRun.Status.PolicySnapshotIndexUsed != latestPolicySnapshot.Name {
		mismatchErr := fmt.Errorf("the policy snapshot index used in the clusterStagedUpdateRun is outdated, latest: %s, recorded: %s", latestPolicySnapshot.Name, updateRun.Status.PolicySnapshotIndexUsed)
		klog.ErrorS(mismatchErr, "there's a new latest policy snapshot", "clusterResourcePlacement", placementName, "clusterStagedUpdateRun", updateRunRef)
		return -1, nil, nil, fmt.Errorf("%w: %s", errStagedUpdatedAborted, mismatchErr.Error())
	}
	// Make sure the cluster count in the policy snapshot has not changed.
	if updateRun.Status.PolicyObservedClusterCount != clusterCount {
		mismatchErr := fmt.Errorf("the cluster count initialized in the clusterStagedUpdateRun is outdated, latest: %d, recorded: %d", clusterCount, updateRun.Status.PolicyObservedClusterCount)
		klog.ErrorS(mismatchErr, "the cluster count in the policy snapshot has changed", "clusterResourcePlacement", placementName, "clusterStagedUpdateRun", updateRunRef)
		return -1, nil, nil, fmt.Errorf("%w: %s", errStagedUpdatedAborted, mismatchErr.Error())
	}

	// Collect the clusters by the corresponding ClusterResourcePlacement with the latest policy snapshot.
	scheduledBindings, toBeDeletedBindings, err := r.collectScheduledClusters(ctx, placementName, latestPolicySnapshot, updateRunCopy)
	if err != nil {
		return -1, nil, nil, err
	}

	// Validate the stages and return the updating stage index.
	updatingStageIndex, err := r.validateStagesStatus(ctx, scheduledBindings, toBeDeletedBindings, updateRun, updateRunCopy)
	if err != nil {
		return -1, nil, nil, err
	}
	return updatingStageIndex, scheduledBindings, toBeDeletedBindings, nil
}

// validateStagesStatus validates both the update and delete stages of the ClusterStagedUpdateRun.
// The function returns the stage index that is updating, or any error encountered.
// If the updating stage index is -1, it means all stages are finished, and the clusterStageUpdateRun should be marked as finished.
// If the updating stage index is 0, the next stage to be updated will be the first stage.
// If the updating stage index is len(updateRun.Status.StagesStatus), the next stage to be updated will be the delete stage.
func (r *Reconciler) validateStagesStatus(
	ctx context.Context,
	scheduledBindings, toBeDeletedBindings []*placementv1beta1.ClusterResourceBinding,
	updateRun, updateRunCopy *placementv1alpha1.ClusterStagedUpdateRun,
) (int, error) {
	updateRunRef := klog.KObj(updateRun)

	// Recompute the stage status which does not include the delete stage.
	// Note that the compute process uses the StagedUpdateStrategySnapshot in status,
	// so it won't affect anything if the actual updateStrategy has changed.
	if updateRun.Status.StagedUpdateStrategySnapshot == nil {
		unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the clusterStagedUpdateRun has nil stagedUpdateStrategySnapshot"))
		klog.ErrorS(unexpectedErr, "Failed to find the stagedUpdateStrategySnapshot in the clusterStagedUpdateRun", "clusterStagedUpdateRun", updateRunRef)
		return -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
	}
	if err := r.computeRunStageStatus(ctx, scheduledBindings, updateRunCopy); err != nil {
		return -1, err
	}

	// Validate the stages in the updateRun and return the updating stage index.
	existingStageStatus := updateRun.Status.StagesStatus
	if existingStageStatus == nil {
		unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the clusterStagedUpdateRun has nil stagesStatus"))
		klog.ErrorS(unexpectedErr, "Failed to find the stagesStatus in the clusterStagedUpdateRun", "clusterStagedUpdateRun", updateRunRef)
		return -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
	}
	updatingStageIndex, lastFinishedStageIndex, validateErr := validateUpdateStagesStatus(existingStageStatus, updateRunCopy)
	if validateErr != nil {
		return -1, validateErr
	}

	return validateDeleteStageStatus(updatingStageIndex, lastFinishedStageIndex, len(existingStageStatus), toBeDeletedBindings, updateRunCopy)
}

// validateUpdateStagesStatus is a helper function to validate the updating stages in the clusterStagedUpdateRun.
// It compares the existing stage status with the latest list of clusters to be updated.
// It returns the index of the updating stage, the index of the last finished stage and any error encountered.
func validateUpdateStagesStatus(existingStageStatus []placementv1alpha1.StageUpdatingStatus, updateRun *placementv1alpha1.ClusterStagedUpdateRun) (int, int, error) {
	updatingStageIndex := -1
	lastFinishedStageIndex := -1
	// Remember the newly computed stage status.
	newStageStatus := updateRun.Status.StagesStatus
	// Make sure the number of stages in the clusterStagedUpdateRun are still the same.
	if len(existingStageStatus) != len(newStageStatus) {
		mismatchErr := fmt.Errorf("the number of stages in the clusterStagedUpdateRun has changed, new: %d, existing: %d", len(newStageStatus), len(existingStageStatus))
		klog.ErrorS(mismatchErr, "The number of stages in the clusterStagedUpdateRun has changed", "clusterStagedUpdateRun", klog.KObj(updateRun))
		return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, mismatchErr.Error())
	}
	// Make sure the stages in the updateRun are still the same.
	for curStage := range existingStageStatus {
		if existingStageStatus[curStage].StageName != newStageStatus[curStage].StageName {
			mismatchErr := fmt.Errorf("index `%d` stage name in the clusterStagedUpdateRun has changed, new: %s, existing: %s", curStage, newStageStatus[curStage].StageName, existingStageStatus[curStage].StageName)
			klog.ErrorS(mismatchErr, "The stage name in the clusterStagedUpdateRun has changed", "clusterStagedUpdateRun", klog.KObj(updateRun))
			return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, mismatchErr.Error())
		}
		if len(existingStageStatus[curStage].Clusters) != len(newStageStatus[curStage].Clusters) {
			mismatchErr := fmt.Errorf("the number of clusters in index `%d` stage has changed, new: %d, existing: %d", curStage, len(newStageStatus[curStage].Clusters), len(existingStageStatus[curStage].Clusters))
			klog.ErrorS(mismatchErr, "The number of clusters in the stage has changed", "clusterStagedUpdateRun", klog.KObj(updateRun))
			return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, mismatchErr.Error())
		}
		// Check that the clusters in the stage are still the same.
		for j := range existingStageStatus[curStage].Clusters {
			if existingStageStatus[curStage].Clusters[j].ClusterName != newStageStatus[curStage].Clusters[j].ClusterName {
				mismatchErr := fmt.Errorf("the `%d` cluster in the `%d` stage has changed, new: %s, existing: %s", j, curStage, newStageStatus[curStage].Clusters[j].ClusterName, existingStageStatus[curStage].Clusters[j].ClusterName)
				klog.ErrorS(mismatchErr, "The cluster in the stage has changed", "clusterStagedUpdateRun", klog.KObj(updateRun))
				return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, mismatchErr.Error())
			}
		}

		var err error
		updatingStageIndex, lastFinishedStageIndex, err = validateClusterUpdatingStatus(curStage, updatingStageIndex, lastFinishedStageIndex, &existingStageStatus[curStage], updateRun)
		if err != nil {
			return -1, -1, err
		}
	}
	return updatingStageIndex, lastFinishedStageIndex, nil
}

// validateClusterUpdatingStatus validates clusters updating status inside a single stage.
// It checks the cluster updating status according to the stage status and returns error if there's mismatch.
// It accepts current `updatingStageIndex` and `lastFinishedStageIndex` for cross-stage validation.
// It returns `curStage` as updatingStageIndex if the stage is updating or advances `lastFinishedStageIndex` if the stage has finished.
func validateClusterUpdatingStatus(
	curStage, updatingStageIndex, lastFinishedStageIndex int,
	stageStatus *placementv1alpha1.StageUpdatingStatus,
	updateRun *placementv1alpha1.ClusterStagedUpdateRun,
) (int, int, error) {
	stageSucceedCond := meta.FindStatusCondition(stageStatus.Conditions, string(placementv1alpha1.StageUpdatingConditionSucceeded))
	stageStartedCond := meta.FindStatusCondition(stageStatus.Conditions, string(placementv1alpha1.StageUpdatingConditionProgressing))
	if condition.IsConditionStatusTrue(stageSucceedCond, updateRun.Generation) {
		// The stage has finished.
		if updatingStageIndex != -1 && curStage > updatingStageIndex {
			// The finished stage is after the updating stage.
			unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the finished stage `%d` is after the updating stage `%d`", curStage, updatingStageIndex))
			klog.ErrorS(unexpectedErr, "The finished stage is after the updating stage", "clusterStagedUpdateRun", klog.KObj(updateRun))
			return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
		}
		// Make sure that all the clusters are updated.
		for curCluster := range stageStatus.Clusters {
			// Check if the cluster is still updating.
			if !condition.IsConditionStatusTrue(meta.FindStatusCondition(
				stageStatus.Clusters[curCluster].Conditions,
				string(placementv1alpha1.ClusterUpdatingConditionSucceeded)),
				updateRun.Generation) {
				// The clusters in the finished stage should all have finished too.
				unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("cluster `%s` in the finished stage `%s` has not succeeded", stageStatus.Clusters[curCluster].ClusterName, stageStatus.StageName))
				klog.ErrorS(unexpectedErr, "The cluster in a finished stage is still updating", "clusterStagedUpdateRun", klog.KObj(updateRun))
				return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
			}
		}
		if curStage != lastFinishedStageIndex+1 {
			// The current finished stage is not right after the last finished stage.
			unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the finished stage `%s` is not right after the last finished stage with index `%d`", stageStatus.StageName, lastFinishedStageIndex))
			klog.ErrorS(unexpectedErr, "There's not yet started stage before the finished stage", "clusterStagedUpdateRun", klog.KObj(updateRun))
			return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
		}
		// Record the last finished stage so we can continue from the next stage if no stage is updating.
		lastFinishedStageIndex = curStage
	} else if condition.IsConditionStatusFalse(stageSucceedCond, updateRun.Generation) {
		// The stage has failed.
		failedErr := fmt.Errorf("the stage `%s` has failed, err: %s", stageStatus.StageName, stageSucceedCond.Message)
		klog.ErrorS(failedErr, "The stage has failed", "stageCond", stageSucceedCond, "clusterStagedUpdateRun", klog.KObj(updateRun))
		return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, failedErr.Error())
	} else if stageStartedCond != nil {
		// The stage is still updating.
		if updatingStageIndex != -1 {
			// There should be only one stage updating at a time.
			unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the stage `%s` is updating, but there is already a stage with index `%d` updating", stageStatus.StageName, updatingStageIndex))
			klog.ErrorS(unexpectedErr, "Detected more than one updating stages", "clusterStagedUpdateRun", klog.KObj(updateRun))
			return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
		}
		if curStage != lastFinishedStageIndex+1 {
			// The current updating stage is not right after the last finished stage.
			unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the updating stage `%s` is not right after the last finished stage with index `%d`", stageStatus.StageName, lastFinishedStageIndex))
			klog.ErrorS(unexpectedErr, "There's not yet started stage before the updating stage", "clusterStagedUpdateRun", klog.KObj(updateRun))
			return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
		}
		updatingStageIndex = curStage
		// Collect the updating clusters.
		var updatingClusters []string
		for j := range stageStatus.Clusters {
			clusterStartedCond := meta.FindStatusCondition(stageStatus.Clusters[j].Conditions, string(placementv1alpha1.ClusterUpdatingConditionStarted))
			clusterFinishedCond := meta.FindStatusCondition(stageStatus.Clusters[j].Conditions, string(placementv1alpha1.ClusterUpdatingConditionSucceeded))
			if condition.IsConditionStatusTrue(clusterStartedCond, updateRun.Generation) &&
				!(condition.IsConditionStatusTrue(clusterFinishedCond, updateRun.Generation) || condition.IsConditionStatusFalse(clusterFinishedCond, updateRun.Generation)) {
				updatingClusters = append(updatingClusters, stageStatus.Clusters[j].ClusterName)
			}
		}
		// We don't allow more than one clusters to be updating at the same time.
		// TODO(wantjian): support multiple clusters updating at the same time.
		if len(updatingClusters) > 1 {
			unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("more than one cluster is updating in the stage `%s`, clusters: %v", stageStatus.StageName, updatingClusters))
			klog.ErrorS(unexpectedErr, "Detected more than one updating clusters in the stage", "clusterStagedUpdateRun", klog.KObj(updateRun))
			return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
		}
	}
	return updatingStageIndex, lastFinishedStageIndex, nil
}

// validateDeleteStageStatus validates the delete stage in the clusterStagedUpdateRun.
// It returns the updating stage index, or any error encountered.
func validateDeleteStageStatus(
	updatingStageIndex, lastFinishedStageIndex, totalStages int,
	toBeDeletedBindings []*placementv1beta1.ClusterResourceBinding,
	updateRun *placementv1alpha1.ClusterStagedUpdateRun,
) (int, error) {
	updateRunRef := klog.KObj(updateRun)
	existingDeleteStageStatus := updateRun.Status.DeletionStageStatus
	if existingDeleteStageStatus == nil {
		unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the clusterStagedUpdateRun has nil deletionStageStatus"))
		klog.ErrorS(unexpectedErr, "Failed to find the deletionStageStatus in the clusterStagedUpdateRun", "clusterStagedUpdateRun", updateRunRef)
		return -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
	}

	// Validate whether toBeDeletedBindings are a subnet of the clusters in the delete stage status.
	// We only validate if it's a subnet because we will delete the bindings during the deleteStage execution so they can disappear.
	// We only need to check the existence, not the order, because clusters are always sorted by name in the delete stage.
	deletingClusterMap := make(map[string]struct{}, len(existingDeleteStageStatus.Clusters))
	for _, cluster := range existingDeleteStageStatus.Clusters {
		deletingClusterMap[cluster.ClusterName] = struct{}{}
	}
	for _, binding := range toBeDeletedBindings {
		if _, ok := deletingClusterMap[binding.Spec.TargetCluster]; !ok {
			unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the cluster `%s` to be deleted is not in the delete stage", binding.Spec.TargetCluster))
			klog.ErrorS(unexpectedErr, "Detect new cluster to be unscheduled", "clusterResourceBinding", klog.KObj(binding), "clusterStagedUpdateRun", updateRunRef)
			return -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
		}
	}

	deleteStageFinishedCond := meta.FindStatusCondition(existingDeleteStageStatus.Conditions, string(placementv1alpha1.StagedUpdateRunConditionSucceeded))
	deleteStageProgressingCond := meta.FindStatusCondition(existingDeleteStageStatus.Conditions, string(placementv1alpha1.StagedUpdateRunConditionProgressing))
	// Check if there is any active updating stage
	if updatingStageIndex != -1 || lastFinishedStageIndex < totalStages-1 {
		// There are still stages updating before the delete stage, make sure the delete stage is not active/finished.
		if condition.IsConditionStatusTrue(deleteStageFinishedCond, updateRun.Generation) ||
			condition.IsConditionStatusFalse(deleteStageFinishedCond, updateRun.Generation) ||
			condition.IsConditionStatusTrue(deleteStageProgressingCond, updateRun.Generation) {
			unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the delete stage is active, but there are still stages updating, updatingStageIndex: %d, lastFinishedStageIndex: %d", updatingStageIndex, lastFinishedStageIndex))
			klog.ErrorS(unexpectedErr, "the delete stage is active, but there are still stages updating", "clusterStagedUpdateRun", updateRunRef)
			return -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
		}

		// If no stage is updating, continue from the last finished stage.
		// We initialized lastFinishedStageIndex to -1, so that from the very beginning, we start from 0, the first stage.
		if updatingStageIndex == -1 {
			updatingStageIndex = lastFinishedStageIndex + 1
		}
		return updatingStageIndex, nil
	}

	klog.InfoS("All stages are finished, continue from the delete stage", "clusterStagedUpdateRun", updateRunRef)
	// Check if the delete stage has finished successfully.
	if condition.IsConditionStatusTrue(deleteStageFinishedCond, updateRun.Generation) {
		klog.InfoS("The delete stage has finished successfully, no more stages to update", "clusterStagedUpdateRun", updateRunRef)
		return -1, nil
	}
	// Check if the delete stage has failed.
	if condition.IsConditionStatusFalse(deleteStageFinishedCond, updateRun.Generation) {
		failedErr := fmt.Errorf("the delete stage has failed, err: %s", deleteStageFinishedCond.Message)
		klog.ErrorS(failedErr, "The delete stage has failed", "stageCond", deleteStageFinishedCond, "clusterStagedUpdateRun", updateRunRef)
		return -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, failedErr.Error())
	}
	// The delete stage is still updating or just to start.
	return totalStages, nil
}
