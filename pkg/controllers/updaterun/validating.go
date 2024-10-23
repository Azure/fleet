package updaterun

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

var errStagedUpdatedAborted = fmt.Errorf("can not continue the StagedUpdateRun")

// validateUpdateRunStatus validates the stagedUpdateRun status and ensures the update can be continued.
// The function returns the index of the stage that is updating, the list of clusters that are scheduled to be deleted.
// if the updating stage index is -1, it means all stages are finished, and the updateRun should be marked as finished.
// if the updating stage index is 0, the next stage to be updated will be the first stage.
// if the updating stage index is len(updateRun.Status.StagesStatus), the next stage to be updated will be the delete stage.
func (r *Reconciler) validateUpdateRunStatus(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun) (int, []*placementv1beta1.ClusterResourceBinding, []*placementv1beta1.ClusterResourceBinding, error) {
	// some of the validating function changes the object, so we need to make a copy of the object
	updateRunRef := klog.KObj(updateRun)
	updateRunCopy := updateRun.DeepCopy()
	klog.V(2).InfoS("start to validate the stage update run", "stagedUpdateRun", updateRunRef)
	// Validate the ClusterResourcePlacement object referenced by the ClusterStagedUpdateRun
	placementName, err := r.validateCRP(ctx, updateRunCopy)
	if err != nil {
		return -1, nil, nil, err
	}
	// Record the latest policy snapshot associated with the ClusterResourcePlacement
	latestPolicySnapshot, nodeCount, err := r.determinePolicySnapshot(ctx, placementName, updateRunCopy)
	if err != nil {
		return -1, nil, nil, err
	}
	// make sure the policy snapshot index used in the stagedUpdateRun is still valid
	if updateRun.Status.PolicySnapshotIndexUsed != latestPolicySnapshot.Name {
		misMatchErr := fmt.Errorf("the policy snapshot index used in the stagedUpdateRun is outdated, latest: %s, existing: %s", latestPolicySnapshot.Name, updateRun.Status.PolicySnapshotIndexUsed)
		klog.ErrorS(misMatchErr, "there is a new latest policy snapshot", "clusterResourcePlacement", placementName, "stagedUpdateRun", updateRunRef)
		return -1, nil, nil, fmt.Errorf("%w: %s", errStagedUpdatedAborted, misMatchErr.Error())
	}
	// make sure the node count used in the stagedUpdateRun has not changed
	if updateRun.Status.PolicyObservedClusterCount != nodeCount {
		misMatchErr := fmt.Errorf("the node count used in the stagedUpdateRun is outdated, latest: %d, existing: %d", nodeCount, updateRun.Status.PolicyObservedClusterCount)
		klog.ErrorS(misMatchErr, "The pick N node count has changed", "clusterResourcePlacement", placementName, "stagedUpdateRun", updateRunRef)
		return -1, nil, nil, fmt.Errorf("%w: %s", errStagedUpdatedAborted, misMatchErr.Error())
	}
	// Collect the scheduled clusters by the corresponding ClusterResourcePlacement with the latest policy snapshot
	scheduledBinding, tobeDeleted, err := r.collectScheduledClusters(ctx, placementName, latestPolicySnapshot, updateRunCopy)
	if err != nil {
		return -1, nil, nil, err
	}
	// validate the applyStrategy and stagedUpdateStrategySnapshot
	if updateRun.Status.ApplyStrategy == nil {
		missingErr := fmt.Errorf("the updateRun has no applyStrategy")
		klog.ErrorS(controller.NewUnexpectedBehaviorError(missingErr), "Failed to find the applyStrategy", "clusterResourcePlacement", placementName, "stagedUpdateRun", updateRunRef)
		return -1, nil, nil, fmt.Errorf("%w: %s", errStagedUpdatedAborted, missingErr.Error())
	}
	if updateRun.Status.StagedUpdateStrategySnapshot == nil {
		missingErr := fmt.Errorf("the updateRun has no stagedUpdateStrategySnapshot")
		klog.ErrorS(controller.NewUnexpectedBehaviorError(missingErr), "Failed to find the stagedUpdateStrategySnapshot", "clusterResourcePlacement", placementName, "stagedUpdateRun", updateRunRef)
		return -1, nil, nil, fmt.Errorf("%w: %s", errStagedUpdatedAborted, missingErr.Error())
	}
	if condition.IsConditionStatusFalse(meta.FindStatusCondition(updateRun.Status.Conditions, string(placementv1alpha1.StagedUpdateRunConditionProgressing)), updateRun.Generation) {
		// the updateRun has not started
		klog.V(2).InfoS("start the stage update run from the beginning", "stagedUpdateRun", updateRunRef)
		return 0, scheduledBinding, tobeDeleted, nil
	}
	// validate the stageStatus and deleteStageStatus in the updateRun
	updatingStageIndex, err := r.validateStagesStatus(ctx, scheduledBinding, updateRun)
	if err != nil {
		return -1, nil, nil, err
	}
	return updatingStageIndex, scheduledBinding, tobeDeleted, err
}

// validateStagesStatus validates the both the update and delete stage in the updateRun.
// The function returns the stage index that is updating, and any error that is encountered.
// if the updating stage index is -1, it means all stages are finished, and the updateRun should be marked as finished.
// if the updating stage index is 0, the next stage to be updated will be the first stage.
// if the updating stage index is len(updateRun.Status.StagesStatus), the next stage to be updated will be the delete stage.
func (r *Reconciler) validateStagesStatus(ctx context.Context, scheduledBinding []*placementv1beta1.ClusterResourceBinding, updateRun *placementv1alpha1.ClusterStagedUpdateRun) (int, error) {
	// take a copy of the existing updateRun
	existingStageStatus := updateRun.Status.StagesStatus
	existingDeleteStageStatus := updateRun.Status.DeletionStageStatus
	updateRunCopy := updateRun.DeepCopy()
	updateRunRef := klog.KObj(updateRun)
	// compute the stage status which does not include the delete stage
	if err := r.computeRunStageStatus(ctx, scheduledBinding, updateRunCopy); err != nil {
		return -1, err
	}
	// validate the stages in the updateRun and return the updating stage index
	updatingStageIndex, lastFinishedStageIndex, validateErr := validateUpdateStageStatus(existingStageStatus, updateRunCopy)
	if validateErr != nil {
		return -1, validateErr
	}
	deleteStageFinishedCond := meta.FindStatusCondition(existingDeleteStageStatus.Conditions, string(placementv1alpha1.StageUpdatingConditionSucceeded))
	deleteStageProgressingCond := meta.FindStatusCondition(existingDeleteStageStatus.Conditions, string(placementv1alpha1.StageUpdatingConditionProgressing))
	// check if the there is any active updating stage
	if updatingStageIndex != -1 || lastFinishedStageIndex < len(existingDeleteStageStatus.Clusters) {
		// there are still stages updating before the delete staging, make sure the delete stage is not active/finished
		if condition.IsConditionStatusTrue(deleteStageFinishedCond, updateRun.Generation) || condition.IsConditionStatusFalse(deleteStageFinishedCond, updateRun.Generation) ||
			condition.IsConditionStatusTrue(deleteStageProgressingCond, updateRun.Generation) {
			updateErr := fmt.Errorf("the delete stage is active, but there are still stages updating, updatingStageIndex: %d, lastFinishedStageIndex: %d", updatingStageIndex, lastFinishedStageIndex)
			klog.ErrorS(controller.NewUnexpectedBehaviorError(updateErr), "There are more than one stage active", "stagedUpdateRun", updateRunRef)
			return -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, updateErr.Error())
		}
		// if no stage is updating, continue from the last finished stage (which will result it start from 0)
		if updatingStageIndex == -1 {
			updatingStageIndex = lastFinishedStageIndex + 1
		}
		return updatingStageIndex, nil
	}
	klog.InfoS("All stages are finished, continue from the delete stage", "stagedUpdateRun", updateRunRef)
	// check if the delete stage has finished successfully
	if condition.IsConditionStatusTrue(deleteStageFinishedCond, updateRun.Generation) {
		klog.InfoS("The delete stage has finished successfully, no more stage to update", "stagedUpdateRun", updateRunRef)
		return -1, nil
	}
	// check if the delete stage has failed
	if condition.IsConditionStatusFalse(deleteStageFinishedCond, updateRun.Generation) {
		failedErr := fmt.Errorf("the delete stage has failed, err: %s", deleteStageFinishedCond.Message)
		klog.ErrorS(failedErr, "The delete stage has failed", "stageCond", deleteStageFinishedCond, "stagedUpdateRun", updateRunRef)
		return -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, failedErr.Error())
	}
	if condition.IsConditionStatusTrue(deleteStageProgressingCond, updateRun.Generation) {
		klog.InfoS("The delete stage is updating", "stagedUpdateRun", updateRunRef)
		return len(existingDeleteStageStatus.Clusters), nil
	}
	// all stages are finished but the delete stage is not active/or finished
	updateErr := fmt.Errorf("the delete stage is not active, but all stages finished, updatingStageIndex: %d, lastFinishedStageIndex: %d", updatingStageIndex, lastFinishedStageIndex)
	klog.ErrorS(controller.NewUnexpectedBehaviorError(updateErr), "There is no stage active", "stagedUpdateRun", updateRunRef)
	return -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, updateErr.Error())
}

// validateUpdateStageStatus is a helper function to validate the updating stages in the updateRun.
// it compares the existing stage status with the latest list of clusters to be updated.
// it returns the index of the updating stage, the index of the last finished stage and any error that is encountered.
func validateUpdateStageStatus(existingStageStatus []placementv1alpha1.StageUpdatingStatus, updateRun *placementv1alpha1.ClusterStagedUpdateRun) (int, int, error) {
	updatingStageIndex := -1
	lastFinishedStageIndex := -1
	// remember the newly computed stage status
	newStageStatus := updateRun.Status.StagesStatus
	// make sure number of stages in the updateRun are still the same
	if len(existingStageStatus) != len(newStageStatus) {
		misMatchErr := fmt.Errorf("the number of stages in the stagedUpdateRun has changed, latest: %d, existing: %d", len(newStageStatus), len(existingStageStatus))
		klog.ErrorS(misMatchErr, "The number of stages has changed", "stagedUpdateRun", klog.KObj(updateRun))
		return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, misMatchErr.Error())
	}
	// make sure the stages in the updateRun are still the same
	for curStage := range existingStageStatus {
		if existingStageStatus[curStage].StageName != newStageStatus[curStage].StageName {
			misMatchErr := fmt.Errorf("the `%d` stage in the stagedUpdateRun has changed, latest: %s, existing: %s", curStage, newStageStatus[curStage].StageName, existingStageStatus[curStage].StageName)
			klog.ErrorS(misMatchErr, "The stage  has changed", "stagedUpdateRun", klog.KObj(updateRun))
			return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, misMatchErr.Error())
		}
		if len(existingStageStatus[curStage].Clusters) != len(newStageStatus[curStage].Clusters) {
			misMatchErr := fmt.Errorf("the number of clusters in the stage `%s` has changed, latest: %d, existing: %d", existingStageStatus[curStage].StageName, len(newStageStatus), len(existingStageStatus))
			klog.ErrorS(misMatchErr, "The number of clusters in a stage has changed", "stagedUpdateRun", klog.KObj(updateRun))
			return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, misMatchErr.Error())
		}
		// check that the clusters in the stage are still the same
		for j := range existingStageStatus[curStage].Clusters {
			if existingStageStatus[curStage].Clusters[j].ClusterName != newStageStatus[curStage].Clusters[j].ClusterName {
				misMatchErr := fmt.Errorf("the `%d`th cluster in the stage `%s` has changed, latest: %s, existing: %s", j, existingStageStatus[curStage].StageName, newStageStatus[curStage].Clusters[j].ClusterName, existingStageStatus[curStage].Clusters[j].ClusterName)
				klog.ErrorS(misMatchErr, "The cluster in a stage has changed", "stagedUpdateRun", klog.KObj(updateRun))
				return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, misMatchErr.Error())
			}
		}
		stageSucceedCond := meta.FindStatusCondition(existingStageStatus[curStage].Conditions, string(placementv1alpha1.StageUpdatingConditionSucceeded))
		stageStartedCond := meta.FindStatusCondition(existingStageStatus[curStage].Conditions, string(placementv1alpha1.StageUpdatingConditionProgressing))
		if condition.IsConditionStatusTrue(stageSucceedCond, updateRun.Generation) { // the stage has finished
			if updatingStageIndex != -1 && curStage > updatingStageIndex {
				// the finished stage is after the updating stage
				unExpectedErr := fmt.Errorf("the finished stage `%d` is after the updating stage `%d`", curStage, updatingStageIndex)
				klog.ErrorS(controller.NewUnexpectedBehaviorError(unExpectedErr), "The finished stage is after the updating stage", "currentStage", existingStageStatus[curStage].StageName, "updatingStage", existingStageStatus[updatingStageIndex].StageName, "stagedUpdateRun", klog.KObj(updateRun))
				return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unExpectedErr.Error())
			}
			// record the last finished stage so we can continue from the next stage if no stage is updating
			lastFinishedStageIndex = curStage
			// make sure that all the clusters are upgraded
			for curCluster := range existingStageStatus[curStage].Clusters {
				// check if the cluster is updating
				if condition.IsConditionStatusFalse(meta.FindStatusCondition(existingStageStatus[curStage].Clusters[curCluster].Conditions, string(placementv1alpha1.ClusterUpdatingConditionSucceeded)), updateRun.Generation) {
					// the clusters in the finished stage should all be finished too
					unExpectedErr := fmt.Errorf("there is an updating cluster in finished stage `%s` , the updating clusrter: %s, the updating clusrter index: %d", existingStageStatus[curStage].StageName, existingStageStatus[curStage].Clusters[curCluster].ClusterName, curCluster)
					klog.ErrorS(controller.NewUnexpectedBehaviorError(unExpectedErr), "Detected updating clusters in finished stage", "stagedUpdateRun", klog.KObj(updateRun))
					return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unExpectedErr.Error())
				}
			}
		} else if condition.IsConditionStatusFalse(stageSucceedCond, updateRun.Generation) { // the stage is failed
			failedErr := fmt.Errorf("the stage `%s` has failed, err: %s", existingStageStatus[curStage].StageName, stageSucceedCond.Message)
			klog.ErrorS(failedErr, "The stage has failed", "stageCond", stageSucceedCond, "stagedUpdateRun", klog.KObj(updateRun))
			return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, failedErr.Error())
		} else if stageStartedCond != nil { // the stage is updating
			// check this is the only stage that is updating
			if updatingStageIndex != -1 {
				dupErr := fmt.Errorf("more than one updating stage, previous updating stage: %s, new updating stage: %s", existingStageStatus[updatingStageIndex].StageName, existingStageStatus[curStage].StageName)
				klog.ErrorS(controller.NewUnexpectedBehaviorError(dupErr), "Detected more than one updating stage", "stagedUpdateRun", klog.KObj(updateRun))
				return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, dupErr.Error())
			}
			if curStage != lastFinishedStageIndex+1 {
				// the previous stages are not all finished
				unexpectedErr := fmt.Errorf("the updating stage `%d` is not immediate after the last finished stage `%d`", curStage, lastFinishedStageIndex)
				klog.ErrorS(controller.NewUnexpectedBehaviorError(unexpectedErr), "There is not started stage before the updating stage", "currentStage", existingStageStatus[curStage].StageName, "stagedUpdateRun", klog.KObj(updateRun))
				return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
			}
			updatingStageIndex = curStage
			// collect the updating clusters
			var updatingClusters []string
			for j := range existingStageStatus[curStage].Clusters {
				// check if the cluster is updating
				if condition.IsConditionStatusTrue(meta.FindStatusCondition(existingStageStatus[curStage].Clusters[j].Conditions, string(placementv1alpha1.ClusterUpdatingConditionStarted)), updateRun.Generation) &&
					condition.IsConditionStatusFalse(meta.FindStatusCondition(existingStageStatus[curStage].Clusters[j].Conditions, string(placementv1alpha1.ClusterUpdatingConditionSucceeded)), updateRun.Generation) {
					updatingClusters = append(updatingClusters, existingStageStatus[curStage].Clusters[j].ClusterName)
				}
			}
			// We don't allow more than one cluster to be updating at the same time for now
			if len(updatingClusters) > 1 {
				dupErr := fmt.Errorf("more than one updating cluster in stage `%s`, updating clusrters: %v", existingStageStatus[curStage].StageName, updatingClusters)
				klog.ErrorS(controller.NewUnexpectedBehaviorError(dupErr), "Detected more than one updating cluster", "stagedUpdateRun", klog.KObj(updateRun))
				return -1, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, dupErr.Error())
			}
		}
	}
	return updatingStageIndex, lastFinishedStageIndex, nil
}

// recordUpdateRunFailed records the failed update run in the ClusterStagedUpdateRun status.
func (r *Reconciler) recordUpdateRunFailed(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun, message string) error {
	meta.SetStatusCondition(&updateRun.Status.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.StagedUpdateRunConditionSucceeded),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: updateRun.Generation,
		Reason:             condition.UpdateRunFailedReason,
		Message:            message,
	})
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the ClusterStagedUpdateRun status as failed", "stagedUpdateRun", klog.KObj(updateRun))
		return updateErr
	}
	return nil
}
