package updaterun

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

const (
	clusterUpdatingWaitTime = 15 * time.Second
	//
	stageUpdatingWaitTime = 60 * time.Second
)

// executeUpdateRun executes the update run by updating the clusters in the updatingClusterIndices with the update run.
func (r *Reconciler) executeUpdateRun(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun, updatingStageIndex int,
	tobeUpdatedBinding, tobeDeletedBinding []*placementv1beta1.ClusterResourceBinding) (bool, time.Duration, error) {
	// mark the update run as started regardless if it is already marked
	markUpdateRunStarted(updateRun)
	if updatingStageIndex < len(updateRun.Status.StagesStatus) {
		updatingStage := &updateRun.Status.StagesStatus[updatingStageIndex]
		waitTime, executingErr := r.executeUpdatingStage(ctx, updateRun, updatingStageIndex, tobeUpdatedBinding)
		if errors.Is(executingErr, errStagedUpdatedAborted) {
			markStageUpdatingFailed(updatingStage, updateRun.Generation, executingErr.Error())
			return true, waitTime, executingErr
		}
		// the stage has not finished yet
		if updatingErr := r.recordUpdateRunStatus(ctx, updateRun); updatingErr != nil {
			klog.ErrorS(updatingErr, "Failed to update the stagedUpdateRun status", "stagedUpdateRun", klog.KObj(updateRun))
		}
		return false, waitTime, executingErr
	}
	// all the stages have been finished, now start the delete stage
	finished, executingErr := r.executeDeleteStage(ctx, tobeDeletedBinding, updateRun)
	if !finished {
		if updatingErr := r.recordUpdateRunStatus(ctx, updateRun); updatingErr != nil {
			klog.ErrorS(updatingErr, "Failed to update the stagedUpdateRun status", "stagedUpdateRun", klog.KObj(updateRun))
		}
	}
	return finished, clusterUpdatingWaitTime, executingErr
}

// executeUpdatingStage executes the updating stage by updating the clusters in the updatingStage with the update run.
func (r *Reconciler) executeUpdatingStage(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun, updatingStageIndex int, tobeUpdatedBinding []*placementv1beta1.ClusterResourceBinding) (time.Duration, error) {
	updatingStageStatus := &updateRun.Status.StagesStatus[updatingStageIndex]
	resourceSnapshotName := updateRun.Spec.ResourceSnapshotIndex
	// create the map of the tobeUpdatedBinding
	tobeUpdatedBindingMap := make(map[string]*placementv1beta1.ClusterResourceBinding, len(tobeUpdatedBinding))
	for _, binding := range tobeUpdatedBinding {
		tobeUpdatedBindingMap[binding.Spec.TargetCluster] = binding
	}
	finishedClusterCount := 0
	// go through each cluster in the stage and check if it is updated
	for i := range updatingStageStatus.Clusters {
		clusterStatus := &updatingStageStatus.Clusters[i]
		clusterStartedCond := meta.FindStatusCondition(clusterStatus.Conditions, string(placementv1alpha1.ClusterUpdatingConditionStarted))
		clusterUpdateSucceededCond := meta.FindStatusCondition(clusterStatus.Conditions, string(placementv1alpha1.ClusterUpdatingConditionSucceeded))
		if condition.IsConditionStatusFalse(clusterUpdateSucceededCond, updateRun.Generation) {
			// the cluster is marked as failed to update
			failedErr := fmt.Errorf("the to be updated cluster `%s` in the stage %s has failed", clusterStatus.ClusterName, updatingStageStatus.StageName)
			klog.ErrorS(failedErr, "The cluster has failed to be updated", "stagedUpdateRun", klog.KObj(updateRun))
			return 0, fmt.Errorf("%w: %s", errStagedUpdatedAborted, failedErr.Error())
		}
		if condition.IsConditionStatusTrue(clusterUpdateSucceededCond, updateRun.Generation) {
			// the cluster is marked as finished updating successfully
			finishedClusterCount++
			continue
		}
		// the cluster is either updating or not started updating yet
		binding := tobeUpdatedBindingMap[clusterStatus.ClusterName]
		availCond := binding.GetCondition(string(placementv1beta1.ResourceBindingAvailable))
		if !condition.IsConditionStatusTrue(clusterStartedCond, updateRun.Generation) {
			// the cluster has not started updating yet
			markClusterUpdatingStarted(clusterStatus, updateRun.Generation)
			if finishedClusterCount == 0 {
				markStageUpdatingStarted(updatingStageStatus, updateRun.Generation)
			}
			if !isBindingSyncedWithClusterStatus(updateRun, binding, clusterStatus) {
				klog.V(2).InfoS("Find the first cluster that needs to be updated", "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "stagedUpdateRun", klog.KObj(updateRun))
				// The binding is not up to date with the cluster status.
				binding.Spec.State = placementv1beta1.BindingStateBound
				binding.Spec.ResourceSnapshotName = resourceSnapshotName
				binding.Spec.ResourceOverrideSnapshots = clusterStatus.ResourceOverrideSnapshots
				binding.Spec.ClusterResourceOverrideSnapshots = clusterStatus.ClusterResourceOverrideSnapshots
				binding.Spec.ApplyStrategy = updateRun.Status.ApplyStrategy
				if err := r.Client.Update(ctx, binding); err != nil {
					klog.ErrorS(err, "Failed to update binding to be bound with matching spec with the update run", "clusterResourceBinding", klog.KObj(binding), "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "stagedUpdateRun", klog.KObj(updateRun))
					return 0, controller.NewUpdateIgnoreConflictError(err)
				}
				klog.V(2).InfoS("Updated the status of a binding to bound", "clusterResourceBinding", klog.KObj(binding), "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "stagedUpdateRun", klog.KObj(updateRun))
			} else {
				klog.V(2).InfoS("Find the first binding that is updating but the cluster status has not been updated", "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "stagedUpdateRun", klog.KObj(updateRun))
				if binding.Spec.State != placementv1beta1.BindingStateBound {
					if err := r.Client.Update(ctx, binding); err != nil {
						klog.ErrorS(err, "Failed to update binding to be bound", "clusterResourceBinding", klog.KObj(binding), "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "stagedUpdateRun", klog.KObj(updateRun))
						return 0, controller.NewUpdateIgnoreConflictError(err)
					}
					klog.V(2).InfoS("Updated the status of a binding to bound", "clusterResourceBinding", klog.KObj(binding), "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "stagedUpdateRun", klog.KObj(updateRun))
				} else {
					if _, updateErr := checkClusterUpgradeResult(availCond, binding, clusterStatus, updatingStageStatus, updateRun); updateErr != nil {
						return clusterUpdatingWaitTime, updateErr
					}
				}
			}
			// no need to continue as we only support one cluster updating at a time for now
			return clusterUpdatingWaitTime, nil
		}
		// now the cluster has to be updating, the binding should point to the right resource snapshot and the binding should be bound.
		if !isBindingSyncedWithClusterStatus(updateRun, binding, clusterStatus) || binding.Spec.State != placementv1beta1.BindingStateBound {
			unexpectedErr := fmt.Errorf("the updating cluster `%s` in the stage %s does not match the cluster status: %+v, binding := %+v", clusterStatus.ClusterName, updatingStageStatus.StageName, clusterStatus, binding.Spec)
			klog.ErrorS(controller.NewUnexpectedBehaviorError(unexpectedErr), "The binding has been changed after the updating, please check if there is con-current update run", "stagedUpdateRun", klog.KObj(updateRun))
			markClusterUpdatingFailed(clusterStatus, updateRun.Generation, unexpectedErr.Error())
			return 0, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
		}
		if finishedClusterCount == 0 {
			markStageUpdatingStarted(updatingStageStatus, updateRun.Generation)
		}
		finished, resultErr := checkClusterUpgradeResult(availCond, binding, clusterStatus, updatingStageStatus, updateRun)
		if finished {
			finishedClusterCount++
			continue
		}
		// no need continue as we only support one cluster updating at a time for now
		return clusterUpdatingWaitTime, resultErr
	}
	if finishedClusterCount == len(updatingStageStatus.Clusters) {
		// all the clusters in the stage have been updated
		markStageUpdatingWaiting(updatingStageStatus, updateRun.Generation)
		klog.V(2).InfoS("The stage has finished all cluster updating", "stage", updatingStageStatus.StageName, "stagedUpdateRun", klog.KObj(updateRun))
		// Check if the after stage tasks are ready.
		approved, err := r.checkAfterStageTasksStatus(ctx, updatingStageIndex, updateRun)
		if err != nil {
			return 0, err
		}
		if approved {
			markStageUpdatingSucceeded(updatingStageStatus, updateRun.Generation)
			return 0, nil //no need to wait to get to the next stage
		}
		return stageUpdatingWaitTime, nil
	}
	return clusterUpdatingWaitTime, nil
}

// checkClusterUpgradeResult checks if the cluster has been updated successfully.
// it returns if the cluster has been updated successfully and the error if the cluster upgrade failed.
func checkClusterUpgradeResult(availCond *metav1.Condition, binding *placementv1beta1.ClusterResourceBinding, clusterStatus *placementv1alpha1.ClusterUpdatingStatus,
	updatingStage *placementv1alpha1.StageUpdatingStatus, updateRun *placementv1alpha1.ClusterStagedUpdateRun) (bool, error) {
	if condition.IsConditionStatusTrue(availCond, binding.Generation) {
		// the resource updated on the cluster is available
		klog.InfoS("The cluster has been updated", "cluster", clusterStatus.ClusterName, "stage", updatingStage.StageName, "stagedUpdateRun", klog.KObj(updateRun))
		markClusterUpdatingSucceeded(clusterStatus, updateRun.Generation)
		return true, nil
	}
	for i := condition.OverriddenCondition; i < condition.AppliedCondition; i++ {
		bindingCond := binding.GetCondition(string(i.ResourceBindingConditionType()))
		if condition.IsConditionStatusFalse(bindingCond, binding.Generation) {
			// we have no awy to know if the failed condition is recoverable or not so we just let it run
			klog.InfoS("The cluster upgrading encountered an error", "failedCondition", bindingCond, "cluster", clusterStatus.ClusterName, "stage", updatingStage.StageName, "stagedUpdateRun", klog.KObj(updateRun))
			//TODO: identify some none recoverable error and mark the cluster upgrading as failed
			return false, fmt.Errorf("the cluster upgrading encountered an error at stage `%s`, err := `%s`", string(i.ResourceBindingConditionType()), bindingCond.Message)
		}
	}
	return false, nil
}

// isBindingSyncedWithClusterStatus checks if the binding is updated with the cluster status.
func isBindingSyncedWithClusterStatus(updateRun *placementv1alpha1.ClusterStagedUpdateRun, binding *placementv1beta1.ClusterResourceBinding, cluster *placementv1alpha1.ClusterUpdatingStatus) bool {
	return binding.Spec.ResourceSnapshotName == updateRun.Spec.ResourceSnapshotIndex &&
		reflect.DeepEqual(cluster.ResourceOverrideSnapshots, binding.Spec.ResourceOverrideSnapshots) &&
		reflect.DeepEqual(cluster.ClusterResourceOverrideSnapshots, binding.Spec.ClusterResourceOverrideSnapshots) &&
		reflect.DeepEqual(binding.Spec.ApplyStrategy, updateRun.Status.ApplyStrategy)
}

// checkAfterStageTasksStatus checks if the after stage tasks have finished.
// it returns if the after stage tasks have finished and the error if the after stage tasks failed.
func (r *Reconciler) checkAfterStageTasksStatus(ctx context.Context, updatingStageIndex int, updateRun *placementv1alpha1.ClusterStagedUpdateRun) (bool, error) {
	updatingStageStatus := &updateRun.Status.StagesStatus[updatingStageIndex]
	updatingStage := &updateRun.Status.StagedUpdateStrategySnapshot.Stages[updatingStageIndex]
	if updatingStage.AfterStageTasks == nil {
		klog.V(2).InfoS("There is no after stage task for this stage", "stage", updatingStage.Name, "stagedUpdateRun", klog.KObj(updateRun))
		return true, nil
	}
	for i, task := range updatingStage.AfterStageTasks {
		switch task.Type {
		case placementv1alpha1.AfterStageTaskTypeTimedWait:
			waitStartTime := meta.FindStatusCondition(updatingStageStatus.Conditions, string(placementv1alpha1.StageUpdatingConditionProgressing)).LastTransitionTime.Time
			// check if the wait time has passed
			if waitStartTime.Add(task.WaitTime.Duration).After(time.Now()) {
				klog.V(2).InfoS("The after stage task still need to wait", "waitStartTime", waitStartTime, "waitTime", task.WaitTime, "stage", updatingStage.Name, "stagedUpdateRun", klog.KObj(updateRun))
				return false, nil
			}
			markAfterStageWaitTimeElapsed(&updatingStageStatus.AfterStageTaskStatus[i], updateRun.Generation)
			klog.V(2).InfoS("The after stage wait task has completed", "stage", updatingStage.Name, "stagedUpdateRun", klog.KObj(updateRun))

		case placementv1alpha1.AfterStageTaskTypeApproval:
			// check if the approval request has been created
			approvalRequest := placementv1alpha1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      updatingStageStatus.AfterStageTaskStatus[i].ApprovalRequestName,
					Namespace: updateRun.Namespace,
					Labels: map[string]string{
						placementv1alpha1.TargetUpdatingStageNameLabel:   updatingStage.Name,
						placementv1alpha1.TargetUpdateRunLabel:           updateRun.Name,
						placementv1alpha1.IsLatestUpdateRunApprovalLabel: "true",
					},
				},
				Spec: placementv1alpha1.ApprovalRequestSpec{
					TargetUpdateRun: updateRun.Name,
					TargetStage:     updatingStage.Name,
				},
			}
			requestRef := klog.KObj(&approvalRequest)
			if err := r.Create(ctx, &approvalRequest); err != nil {
				if apierrors.IsAlreadyExists(err) {
					// the approval task already exists
					markAfterStageRequestCreated(&updatingStageStatus.AfterStageTaskStatus[i], updateRun.Generation)
					if err = r.Get(ctx, client.ObjectKeyFromObject(&approvalRequest), &approvalRequest); err != nil {
						klog.ErrorS(err, "Failed to get the already existing approval request", "approvalRequest", requestRef, "stage", updatingStage.Name, "stagedUpdateRun", klog.KObj(updateRun))
						return false, err
					}
					if approvalRequest.Spec.TargetStage != updatingStage.Name || approvalRequest.Spec.TargetUpdateRun != updateRun.Name {
						unexpectedErr := fmt.Errorf("the approval request task `%s` is targeting update run `%s` and stage `%s` ", approvalRequest.Name, approvalRequest.Spec.TargetStage, approvalRequest.Spec.TargetUpdateRun)
						klog.ErrorS(controller.NewUnexpectedBehaviorError(unexpectedErr), "Found an approval request targeting wrong stage", "approvalRequestTask", requestRef, "stage", updatingStage.Name, "stagedUpdateRun", klog.KObj(updateRun))
						return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
					}
					if !condition.IsConditionStatusTrue(meta.FindStatusCondition(approvalRequest.Status.Conditions, string(placementv1alpha1.ApprovalRequestConditionApproved)), approvalRequest.Generation) {
						klog.V(2).InfoS("The approval request has not been approved yet", "approvalRequestTask", requestRef, "stage", updatingStage.Name, "stagedUpdateRun", klog.KObj(updateRun))
						return false, nil
					}
					klog.V(2).InfoS("The approval request has been approved", "approvalRequestTask", requestRef, "stage", updatingStage.Name, "stagedUpdateRun", klog.KObj(updateRun))
					markAfterStageRequestApproved(&updatingStageStatus.AfterStageTaskStatus[i], updateRun.Generation)
				} else {
					// retryable error
					klog.ErrorS(err, "Failed to create the approval request", "approvalRequest", requestRef, "stage", updatingStage.Name, "stagedUpdateRun", klog.KObj(updateRun))
					return false, err
				}
			} else {
				// the approval request has been created for the first time
				klog.V(2).InfoS("The approval request has been created", "approvalRequestTask", requestRef, "stage", updatingStage.Name, "stagedUpdateRun", klog.KObj(updateRun))
				markAfterStageRequestCreated(&updatingStageStatus.AfterStageTaskStatus[i], updateRun.Generation)
				return false, nil
			}
		}
	}
	// all the after stage tasks have been finished or the for loop will return before this line
	return true, nil
}

// executeDeleteStage executes the delete stage by updating the clusters in the deleteStage with the update run.
func (r *Reconciler) executeDeleteStage(ctx context.Context, tobeDeletedBindings []*placementv1beta1.ClusterResourceBinding, updateRun *placementv1alpha1.ClusterStagedUpdateRun) (bool, error) {
	existingDeleteStageStatus := updateRun.Status.DeletionStageStatus
	existingDeleteStageClustersMap := make(map[string]*placementv1alpha1.ClusterUpdatingStatus, len(existingDeleteStageStatus.Clusters))
	for i, clusterStatus := range existingDeleteStageStatus.Clusters {
		existingDeleteStageClustersMap[clusterStatus.ClusterName] = &existingDeleteStageStatus.Clusters[i]
	}
	deletingBinding := 0
	// check that the clusters in the stage are part of the tobeDeletedBindings
	for _, binding := range tobeDeletedBindings {
		curCluster, exist := existingDeleteStageClustersMap[binding.Spec.TargetCluster]
		if !exist {
			missingErr := fmt.Errorf("the to be deleted cluster `%s` is not in the deleting stage", binding.Spec.TargetCluster)
			klog.ErrorS(missingErr, "The cluster in the deleting stage does not include all the to be deleted binding", "stagedUpdateRun", klog.KObj(updateRun))
			return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, missingErr.Error())
		}
		delete(existingDeleteStageClustersMap, binding.Spec.TargetCluster)
		// make sure the cluster is not marked as deleted as the binding is still there
		if condition.IsConditionStatusTrue(meta.FindStatusCondition(curCluster.Conditions, string(placementv1alpha1.ClusterUpdatingConditionSucceeded)), updateRun.Generation) {
			unexpectedErr := fmt.Errorf("the deleted cluster `%s` in the deleting stage is not deleted yet", binding.Spec.TargetCluster)
			klog.ErrorS(controller.NewUnexpectedBehaviorError(unexpectedErr), "The cluster in the deleting stage is not removed yet but marked as deleted", "cluster", curCluster.ClusterName, "stagedUpdateRun", klog.KObj(updateRun))
			return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
		}
		if condition.IsConditionStatusTrue(meta.FindStatusCondition(curCluster.Conditions, string(placementv1alpha1.ClusterUpdatingConditionStarted)), updateRun.Generation) {
			// the cluster status is marked as being deleted
			if binding.DeletionTimestamp.IsZero() {
				// the cluster is marked as deleting but the binding is not deleting
				unexpectedErr := fmt.Errorf("the cluster `%s` in the deleting stage is marked as deleting but its corresponding binding is not deleting", curCluster.ClusterName)
				klog.ErrorS(controller.NewUnexpectedBehaviorError(unexpectedErr), "The binding should be deleting before we mark a cluster deleting", "clusterStatus", curCluster, "stagedUpdateRun", klog.KObj(updateRun))
				return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
			}
			deletingBinding++
			continue
		}
		// the cluster status is not deleting yet
		if isBindingSyncedWithClusterStatus(updateRun, binding, curCluster) {
			// delete the binding that is up to date with the cluster status.
			if err := r.Client.Delete(ctx, binding); err != nil {
				klog.ErrorS(err, "Failed to delete a binding in the update run", "clusterResourceBinding", klog.KObj(binding), "cluster", curCluster.ClusterName, "stagedUpdateRun", klog.KObj(updateRun))
				return false, controller.NewAPIServerError(false, err)
			}
			klog.V(2).InfoS("Deleted a binding pointing to a to be deleted cluster", "clusterResourceBinding", klog.KObj(binding), "cluster", curCluster.ClusterName, "stagedUpdateRun", klog.KObj(updateRun))
			markClusterUpdatingStarted(curCluster, updateRun.Generation)
			if deletingBinding == 0 {
				markStageUpdatingStarted(updateRun.Status.DeletionStageStatus, updateRun.Generation)
			}
		} else {
			// the binding is not up to date with the cluster status, it could happen if there are multiple update stage running concurrently.
			delErr := fmt.Errorf("the deleted cluster `%s` in the deleting stage does not match the cluster status: %+v, binding := %+v", curCluster.ClusterName, curCluster, binding.Spec)
			klog.ErrorS(delErr, "The binding has been changed after the updating, please check if there is con-current update run", "stagedUpdateRun", klog.KObj(updateRun))
			return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, delErr.Error())
		}
	}
	// the rest of the clusters in the stage are not in the tobeDeletedBindings so it should be marked as delete succeeded
	for _, clusterStatus := range existingDeleteStageClustersMap {
		// make sure the cluster is marked as deleting
		if !condition.IsConditionStatusTrue(meta.FindStatusCondition(clusterStatus.Conditions, string(placementv1alpha1.ClusterUpdatingConditionStarted)), updateRun.Generation) {
			markClusterUpdatingStarted(clusterStatus, updateRun.Generation)
		}
		markClusterUpdatingSucceeded(clusterStatus, updateRun.Generation)
	}
	klog.InfoS("The delete stage is progressing", "numberOfDeletingClusters", len(tobeDeletedBindings), "stagedUpdateRun", klog.KObj(updateRun))
	if len(tobeDeletedBindings) == 0 {
		markStageUpdatingSucceeded(updateRun.Status.DeletionStageStatus, updateRun.Generation)
	}
	return len(tobeDeletedBindings) == 0, nil
}

// recordUpdateRunSucceeded marks the update run as succeeded in memory.
func markAfterStageRequestCreated(afterStageTaskStatus *placementv1alpha1.AfterStageTaskStatus, generation int64) {
	meta.SetStatusCondition(&afterStageTaskStatus.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.AfterStageTaskConditionApprovalRequestCreated),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.AfterStageTaskApprovalRequestCreatedReason,
	})
}

// recordUpdateRunSucceeded marks the update run as succeeded in memory.
func markAfterStageRequestApproved(afterStageTaskStatus *placementv1alpha1.AfterStageTaskStatus, generation int64) {
	meta.SetStatusCondition(&afterStageTaskStatus.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.AfterStageTaskConditionApprovalRequestApproved),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.AfterStageTaskApprovalRequestApprovedReason,
	})
}

// recordUpdateRunSucceeded marks the update run as succeeded in memory.
func markAfterStageWaitTimeElapsed(afterStageTaskStatus *placementv1alpha1.AfterStageTaskStatus, generation int64) {
	meta.SetStatusCondition(&afterStageTaskStatus.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.AfterStageTaskConditionWaitTimeElapsed),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.AfterStageTaskWaitTimeElapsedReason,
	})
}

// recordUpdateRunSucceeded mark the update run as succeeded in memory.
func markClusterUpdatingStarted(clusterUpdatingStatus *placementv1alpha1.ClusterUpdatingStatus, generation int64) {
	meta.SetStatusCondition(&clusterUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.ClusterUpdatingConditionStarted),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.ClusterUpdatingStartedReason,
	})
}

// markClusterUpdatingFailed mark the cluster updating failed in memory.
func markClusterUpdatingFailed(clusterUpdatingStatus *placementv1alpha1.ClusterUpdatingStatus, generation int64, message string) {
	meta.SetStatusCondition(&clusterUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.ClusterUpdatingConditionSucceeded),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: generation,
		Reason:             condition.ClusterUpdatingFailedReason,
		Message:            message,
	})
}

// markClusterUpdatingSucceeded mark the cluster updating succeeded in memory.
func markClusterUpdatingSucceeded(clusterUpdatingStatus *placementv1alpha1.ClusterUpdatingStatus, generation int64) {
	meta.SetStatusCondition(&clusterUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.ClusterUpdatingConditionSucceeded),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.ClusterUpdatingSucceededReason,
	})
}

// markStageUpdatingStarted mark the stage updating started in memory.
func markStageUpdatingStarted(stageUpdatingStatus *placementv1alpha1.StageUpdatingStatus, generation int64) {
	if stageUpdatingStatus.StartTime == nil {
		stageUpdatingStatus.StartTime = &metav1.Time{Time: time.Now()}
	}
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.StageUpdatingConditionProgressing),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingStartedReason,
	})
}

// markStageUpdatingWaiting mark the stage updating as waiting in memory.
func markStageUpdatingWaiting(stageUpdatingStatus *placementv1alpha1.StageUpdatingStatus, generation int64) {
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.StageUpdatingConditionProgressing),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingWaitingReason,
	})
}

// markStageUpdatingFailed mark the stage updating failed in memory.
func markStageUpdatingFailed(stageUpdatingStatus *placementv1alpha1.StageUpdatingStatus, generation int64, message string) {
	if stageUpdatingStatus.StartTime == nil {
		stageUpdatingStatus.StartTime = &metav1.Time{Time: time.Now()}
	}
	if stageUpdatingStatus.EndTime == nil {
		stageUpdatingStatus.EndTime = &metav1.Time{Time: time.Now()}
	}
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.StageUpdatingConditionSucceeded),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingFailedReason,
		Message:            message,
	})
}

// markStageUpdatingSucceeded mark the stage updating as succeeded in memory.
func markStageUpdatingSucceeded(stageUpdatingStatus *placementv1alpha1.StageUpdatingStatus, generation int64) {
	if stageUpdatingStatus.StartTime == nil {
		stageUpdatingStatus.StartTime = &metav1.Time{Time: time.Now()}
	}
	if stageUpdatingStatus.EndTime == nil {
		stageUpdatingStatus.EndTime = &metav1.Time{Time: time.Now()}
	}
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.StageUpdatingConditionSucceeded),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingSucceededReason,
	})
}

// markUpdateRunStarted mark the update run as succeeded in memory.
func markUpdateRunStarted(updateRun *placementv1alpha1.ClusterStagedUpdateRun) {
	meta.SetStatusCondition(&updateRun.Status.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.StagedUpdateRunConditionProgressing),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: updateRun.Generation,
		Reason:             condition.UpdateRunStartedReason,
	})
}

// recordUpdateRunFailed records the ClusterStagedUpdateRun status.
func (r *Reconciler) recordUpdateRunStatus(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun) error {
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the ClusterStagedUpdateRun status as failed", "stagedUpdateRun", klog.KObj(updateRun))
		return updateErr
	}
	return nil
}

// recordUpdateRunSucceeded records the update run as succeeded.
func (r *Reconciler) recordUpdateRunSucceeded(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun) error {
	meta.SetStatusCondition(&updateRun.Status.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.StagedUpdateRunConditionSucceeded),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: updateRun.Generation,
		Reason:             condition.UpdateRunSucceededReason,
	})
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the ClusterStagedUpdateRun status as completed successfully", "stagedUpdateRun", klog.KObj(updateRun))
		return updateErr
	}
	return nil
}
