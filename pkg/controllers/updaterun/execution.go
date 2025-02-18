/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package updaterun

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	bindingutils "go.goms.io/fleet/pkg/utils/binding"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

var (
	// clusterUpdatingWaitTime is the time to wait before rechecking the cluster update status.
	// Put it as a variable for convenient testing.
	clusterUpdatingWaitTime = 15 * time.Second

	// stageUpdatingWaitTime is the time to wait before rechecking the stage update status.
	// Put it as a variable for convenient testing.
	stageUpdatingWaitTime = 60 * time.Second
)

// execute executes the update run by updating the clusters in the updating stage specified by updatingStageIndex.
// It returns a boolean indicating if the clusterStageUpdateRun execution is completed,
// the time to wait before rechecking the cluster update status, and any error encountered.
func (r *Reconciler) execute(
	ctx context.Context,
	updateRun *placementv1beta1.ClusterStagedUpdateRun,
	updatingStageIndex int,
	toBeUpdatedBindings, toBeDeletedBindings []*placementv1beta1.ClusterResourceBinding,
) (bool, time.Duration, error) {
	// Mark the update run as started regardless of whether it's already marked.
	markUpdateRunStarted(updateRun)

	if updatingStageIndex < len(updateRun.Status.StagesStatus) {
		updatingStage := &updateRun.Status.StagesStatus[updatingStageIndex]
		waitTime, execErr := r.executeUpdatingStage(ctx, updateRun, updatingStageIndex, toBeUpdatedBindings)
		if errors.Is(execErr, errStagedUpdatedAborted) {
			markStageUpdatingFailed(updatingStage, updateRun.Generation, execErr.Error())
			return true, waitTime, execErr
		}
		// The execution has not finished yet.
		return false, waitTime, execErr
	}
	// All the stages have finished, now start the delete stage.
	finished, execErr := r.executeDeleteStage(ctx, updateRun, toBeDeletedBindings)
	if errors.Is(execErr, errStagedUpdatedAborted) {
		markStageUpdatingFailed(updateRun.Status.DeletionStageStatus, updateRun.Generation, execErr.Error())
		return true, 0, execErr
	}
	return finished, clusterUpdatingWaitTime, execErr
}

// executeUpdatingStage executes a single updating stage by updating the clusterResourceBindings.
func (r *Reconciler) executeUpdatingStage(
	ctx context.Context,
	updateRun *placementv1beta1.ClusterStagedUpdateRun,
	updatingStageIndex int,
	toBeUpdatedBindings []*placementv1beta1.ClusterResourceBinding,
) (time.Duration, error) {
	updatingStageStatus := &updateRun.Status.StagesStatus[updatingStageIndex]
	// The parse error is ignored because the initialization should have caught it.
	resourceIndex, _ := strconv.Atoi(updateRun.Spec.ResourceSnapshotIndex)
	resourceSnapshotName := fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, updateRun.Spec.PlacementName, resourceIndex)
	updateRunRef := klog.KObj(updateRun)
	// Create the map of the toBeUpdatedBindings.
	toBeUpdatedBindingsMap := make(map[string]*placementv1beta1.ClusterResourceBinding, len(toBeUpdatedBindings))
	for _, binding := range toBeUpdatedBindings {
		toBeUpdatedBindingsMap[binding.Spec.TargetCluster] = binding
	}
	finishedClusterCount := 0

	// Go through each cluster in the stage and check if it's updated.
	for i := range updatingStageStatus.Clusters {
		clusterStatus := &updatingStageStatus.Clusters[i]
		clusterStartedCond := meta.FindStatusCondition(clusterStatus.Conditions, string(placementv1beta1.ClusterUpdatingConditionStarted))
		clusterUpdateSucceededCond := meta.FindStatusCondition(clusterStatus.Conditions, string(placementv1beta1.ClusterUpdatingConditionSucceeded))
		if condition.IsConditionStatusFalse(clusterUpdateSucceededCond, updateRun.Generation) {
			// The cluster is marked as failed to update.
			failedErr := fmt.Errorf("the cluster `%s` in the stage %s has failed", clusterStatus.ClusterName, updatingStageStatus.StageName)
			klog.ErrorS(failedErr, "The cluster has failed to be updated", "clusterStagedUpdateRun", updateRunRef)
			return 0, fmt.Errorf("%w: %s", errStagedUpdatedAborted, failedErr.Error())
		}
		if condition.IsConditionStatusTrue(clusterUpdateSucceededCond, updateRun.Generation) {
			// The cluster has been updated successfully.
			finishedClusterCount++
			continue
		}
		// The cluster is either updating or not started yet.
		binding := toBeUpdatedBindingsMap[clusterStatus.ClusterName]
		if !condition.IsConditionStatusTrue(clusterStartedCond, updateRun.Generation) {
			// The cluster has not started updating yet.
			if !isBindingSyncedWithClusterStatus(resourceSnapshotName, updateRun, binding, clusterStatus) {
				klog.V(2).InfoS("Found the first cluster that needs to be updated", "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "clusterStagedUpdateRun", updateRunRef)
				// The binding is not up-to-date with the cluster status.
				binding.Spec.State = placementv1beta1.BindingStateBound
				binding.Spec.ResourceSnapshotName = resourceSnapshotName
				binding.Spec.ResourceOverrideSnapshots = clusterStatus.ResourceOverrideSnapshots
				binding.Spec.ClusterResourceOverrideSnapshots = clusterStatus.ClusterResourceOverrideSnapshots
				binding.Spec.ApplyStrategy = updateRun.Status.ApplyStrategy
				if err := r.Client.Update(ctx, binding); err != nil {
					klog.ErrorS(err, "Failed to update binding to be bound with the matching spec of the updateRun", "binding", klog.KObj(binding), "clusterStagedUpdateRun", updateRunRef)
					return 0, controller.NewUpdateIgnoreConflictError(err)
				}
				klog.V(2).InfoS("Updated the status of a binding to bound", "binding", klog.KObj(binding), "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "clusterStagedUpdateRun", updateRunRef)
				if err := r.updateBindingRolloutStarted(ctx, binding, updateRun); err != nil {
					return 0, err
				}
			} else {
				klog.V(2).InfoS("Found the first binding that is updating but the cluster status has not been updated", "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "clusterStagedUpdateRun", updateRunRef)
				if binding.Spec.State != placementv1beta1.BindingStateBound {
					binding.Spec.State = placementv1beta1.BindingStateBound
					if err := r.Client.Update(ctx, binding); err != nil {
						klog.ErrorS(err, "Failed to update a binding to be bound", "binding", klog.KObj(binding), "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "clusterStagedUpdateRun", updateRunRef)
						return 0, controller.NewUpdateIgnoreConflictError(err)
					}
					klog.V(2).InfoS("Updated the status of a binding to bound", "binding", klog.KObj(binding), "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "clusterStagedUpdateRun", updateRunRef)
					if err := r.updateBindingRolloutStarted(ctx, binding, updateRun); err != nil {
						return 0, err
					}
				} else if !condition.IsConditionStatusTrue(meta.FindStatusCondition(binding.Status.Conditions, string(placementv1beta1.ResourceBindingRolloutStarted)), binding.Generation) {
					klog.V(2).InfoS("The binding is bound and up-to-date but the generation is updated by the scheduler, update rolloutStarted status again", "binding", klog.KObj(binding), "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "clusterStagedUpdateRun", updateRunRef)
					if err := r.updateBindingRolloutStarted(ctx, binding, updateRun); err != nil {
						return 0, err
					}
				} else {
					if _, updateErr := checkClusterUpdateResult(binding, clusterStatus, updatingStageStatus, updateRun); updateErr != nil {
						return clusterUpdatingWaitTime, updateErr
					}
				}
			}
			markClusterUpdatingStarted(clusterStatus, updateRun.Generation)
			if finishedClusterCount == 0 {
				markStageUpdatingStarted(updatingStageStatus, updateRun.Generation)
			}
			// No need to continue as we only support one cluster updating at a time for now.
			return clusterUpdatingWaitTime, nil
		}

		// Now the cluster has to be updating, the binding should point to the right resource snapshot and the binding should be bound.
		if !isBindingSyncedWithClusterStatus(resourceSnapshotName, updateRun, binding, clusterStatus) || binding.Spec.State != placementv1beta1.BindingStateBound ||
			!condition.IsConditionStatusTrue(meta.FindStatusCondition(binding.Status.Conditions, string(placementv1beta1.ResourceBindingRolloutStarted)), binding.Generation) {
			unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the updating cluster `%s` in the stage %s does not match the cluster status: %+v, binding: %+v, condition: %+v",
				clusterStatus.ClusterName, updatingStageStatus.StageName, clusterStatus, binding.Spec, binding.GetCondition(string(placementv1beta1.ResourceBindingRolloutStarted))))
			klog.ErrorS(unexpectedErr, "The binding has been changed during updating, please check if there's concurrent clusterStagedUpdateRun", "clusterStagedUpdateRun", updateRunRef)
			markClusterUpdatingFailed(clusterStatus, updateRun.Generation, unexpectedErr.Error())
			return 0, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
		}
		finished, updateErr := checkClusterUpdateResult(binding, clusterStatus, updatingStageStatus, updateRun)
		if finished {
			finishedClusterCount++
			continue
		}
		// No need to continue as we only support one cluster updating at a time for now.
		return clusterUpdatingWaitTime, updateErr
	}

	if finishedClusterCount == len(updatingStageStatus.Clusters) {
		// All the clusters in the stage have been updated.
		markStageUpdatingWaiting(updatingStageStatus, updateRun.Generation)
		klog.V(2).InfoS("The stage has finished all cluster updating", "stage", updatingStageStatus.StageName, "clusterStagedUpdateRun", updateRunRef)
		// Check if the after stage tasks are ready.
		approved, waitTime, err := r.checkAfterStageTasksStatus(ctx, updatingStageIndex, updateRun)
		if err != nil {
			return 0, err
		}
		if approved {
			markStageUpdatingSucceeded(updatingStageStatus, updateRun.Generation)
			// No need to wait to get to the next stage.
			return 0, nil
		}
		// The after stage tasks are not ready yet.
		if waitTime < 0 {
			waitTime = stageUpdatingWaitTime
		}
		return waitTime, nil
	}
	return clusterUpdatingWaitTime, nil
}

// executeDeleteStage executes the delete stage by deleting the clusterResourceBindings.
func (r *Reconciler) executeDeleteStage(
	ctx context.Context,
	updateRun *placementv1beta1.ClusterStagedUpdateRun,
	toBeDeletedBindings []*placementv1beta1.ClusterResourceBinding,
) (bool, error) {
	updateRunRef := klog.KObj(updateRun)
	existingDeleteStageStatus := updateRun.Status.DeletionStageStatus
	existingDeleteStageClusterMap := make(map[string]*placementv1beta1.ClusterUpdatingStatus, len(existingDeleteStageStatus.Clusters))
	for i := range existingDeleteStageStatus.Clusters {
		existingDeleteStageClusterMap[existingDeleteStageStatus.Clusters[i].ClusterName] = &existingDeleteStageStatus.Clusters[i]
	}
	// Mark the delete stage as started in case it's not.
	markStageUpdatingStarted(updateRun.Status.DeletionStageStatus, updateRun.Generation)
	for _, binding := range toBeDeletedBindings {
		curCluster, exist := existingDeleteStageClusterMap[binding.Spec.TargetCluster]
		if !exist {
			// This is unexpected because we already checked in validation.
			missingErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the to be deleted cluster `%s` is not in the deleting stage during execution", binding.Spec.TargetCluster))
			klog.ErrorS(missingErr, "The cluster in the deleting stage does not include all the to be deleted binding", "clusterStagedUpdateRun", updateRunRef)
			return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, missingErr.Error())
		}
		// In validation, we already check the binding must exist in the status.
		delete(existingDeleteStageClusterMap, binding.Spec.TargetCluster)
		// Make sure the cluster is not marked as deleted as the binding is still there.
		if condition.IsConditionStatusTrue(meta.FindStatusCondition(curCluster.Conditions, string(placementv1beta1.ClusterUpdatingConditionSucceeded)), updateRun.Generation) {
			unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the deleted cluster `%s` in the deleting stage still has a clusterResourceBinding", binding.Spec.TargetCluster))
			klog.ErrorS(unexpectedErr, "The cluster in the deleting stage is not removed yet but marked as deleted", "cluster", curCluster.ClusterName, "clusterStagedUpdateRun", updateRunRef)
			return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
		}
		if condition.IsConditionStatusTrue(meta.FindStatusCondition(curCluster.Conditions, string(placementv1beta1.ClusterUpdatingConditionStarted)), updateRun.Generation) {
			// The cluster status is marked as being deleted.
			if binding.DeletionTimestamp.IsZero() {
				// The cluster is marked as deleting but the binding is not deleting.
				unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the cluster `%s` in the deleting stage is marked as deleting but its corresponding binding is not deleting", curCluster.ClusterName))
				klog.ErrorS(unexpectedErr, "The binding should be deleting before we mark a cluster deleting", "clusterStatus", curCluster, "clusterStagedUpdateRun", updateRunRef)
				return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
			}
			continue
		}
		// The cluster status is not deleting yet
		if err := r.Client.Delete(ctx, binding); err != nil {
			klog.ErrorS(err, "Failed to delete a binding in the update run", "binding", klog.KObj(binding), "cluster", curCluster.ClusterName, "clusterStagedUpdateRun", updateRunRef)
			return false, controller.NewAPIServerError(false, err)
		}
		klog.V(2).InfoS("Deleted a binding pointing to a to be deleted cluster", "binding", klog.KObj(binding), "cluster", curCluster.ClusterName, "clusterStagedUpdateRun", updateRunRef)
		markClusterUpdatingStarted(curCluster, updateRun.Generation)
	}
	// The rest of the clusters in the stage are not in the toBeDeletedBindings so it should be marked as delete succeeded.
	for _, clusterStatus := range existingDeleteStageClusterMap {
		// Make sure the cluster is marked as deleted.
		if !condition.IsConditionStatusTrue(meta.FindStatusCondition(clusterStatus.Conditions, string(placementv1beta1.ClusterUpdatingConditionStarted)), updateRun.Generation) {
			markClusterUpdatingStarted(clusterStatus, updateRun.Generation)
		}
		markClusterUpdatingSucceeded(clusterStatus, updateRun.Generation)
	}
	klog.InfoS("The delete stage is progressing", "numberOfDeletingClusters", len(toBeDeletedBindings), "clusterStagedUpdateRun", updateRunRef)
	if len(toBeDeletedBindings) == 0 {
		markStageUpdatingSucceeded(updateRun.Status.DeletionStageStatus, updateRun.Generation)
	}
	return len(toBeDeletedBindings) == 0, nil
}

// checkAfterStageTasksStatus checks if the after stage tasks have finished.
// Tt returns if the after stage tasks have finished or error if the after stage tasks failed.
// It also returns the time to wait before rechecking the wait type of task. It turns -1 if the task is not a wait type.
func (r *Reconciler) checkAfterStageTasksStatus(ctx context.Context, updatingStageIndex int, updateRun *placementv1beta1.ClusterStagedUpdateRun) (bool, time.Duration, error) {
	updateRunRef := klog.KObj(updateRun)
	updatingStageStatus := &updateRun.Status.StagesStatus[updatingStageIndex]
	updatingStage := &updateRun.Status.StagedUpdateStrategySnapshot.Stages[updatingStageIndex]
	if updatingStage.AfterStageTasks == nil {
		klog.V(2).InfoS("There is no after stage task for this stage", "stage", updatingStage.Name, "clusterStagedUpdateRun", updateRunRef)
		return true, 0, nil
	}
	for i, task := range updatingStage.AfterStageTasks {
		switch task.Type {
		case placementv1beta1.AfterStageTaskTypeTimedWait:
			waitStartTime := meta.FindStatusCondition(updatingStageStatus.Conditions, string(placementv1beta1.StageUpdatingConditionProgressing)).LastTransitionTime.Time
			// Check if the wait time has passed.
			waitTime := time.Until(waitStartTime.Add(task.WaitTime.Duration))
			if waitTime > 0 {
				klog.V(2).InfoS("The after stage task still need to wait", "waitStartTime", waitStartTime, "waitTime", task.WaitTime, "stage", updatingStage.Name, "clusterStagedUpdateRun", updateRunRef)
				return false, waitTime, nil
			}
			markAfterStageWaitTimeElapsed(&updatingStageStatus.AfterStageTaskStatus[i], updateRun.Generation)
			klog.V(2).InfoS("The after stage wait task has completed", "stage", updatingStage.Name, "clusterStagedUpdateRun", updateRunRef)
		case placementv1beta1.AfterStageTaskTypeApproval:
			// Check if the approval request has been created.
			approvalRequest := placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: updatingStageStatus.AfterStageTaskStatus[i].ApprovalRequestName,
					Labels: map[string]string{
						placementv1beta1.TargetUpdatingStageNameLabel:   updatingStage.Name,
						placementv1beta1.TargetUpdateRunLabel:           updateRun.Name,
						placementv1beta1.IsLatestUpdateRunApprovalLabel: "true",
					},
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: updateRun.Name,
					TargetStage:     updatingStage.Name,
				},
			}
			requestRef := klog.KObj(&approvalRequest)
			if err := r.Client.Create(ctx, &approvalRequest); err != nil {
				if apierrors.IsAlreadyExists(err) {
					// The approval task already exists.
					markAfterStageRequestCreated(&updatingStageStatus.AfterStageTaskStatus[i], updateRun.Generation)
					if err = r.Client.Get(ctx, client.ObjectKeyFromObject(&approvalRequest), &approvalRequest); err != nil {
						klog.ErrorS(err, "Failed to get the already existing approval request", "approvalRequest", requestRef, "stage", updatingStage.Name, "clusterStagedUpdateRun", updateRunRef)
						return false, -1, controller.NewAPIServerError(true, err)
					}
					if approvalRequest.Spec.TargetStage != updatingStage.Name || approvalRequest.Spec.TargetUpdateRun != updateRun.Name {
						unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the approval request task `%s` is targeting update run `%s` and stage `%s` ", approvalRequest.Name, approvalRequest.Spec.TargetStage, approvalRequest.Spec.TargetUpdateRun))
						klog.ErrorS(unexpectedErr, "Found an approval request targeting wrong stage", "approvalRequestTask", requestRef, "stage", updatingStage.Name, "clusterStagedUpdateRun", updateRunRef)
						return false, -1, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
					}
					approvalAccepted := condition.IsConditionStatusTrue(meta.FindStatusCondition(approvalRequest.Status.Conditions, string(placementv1beta1.ApprovalRequestConditionApprovalAccepted)), approvalRequest.Generation)
					// Approved state should not change once the approval is accepted.
					if !approvalAccepted && !condition.IsConditionStatusTrue(meta.FindStatusCondition(approvalRequest.Status.Conditions, string(placementv1beta1.ApprovalRequestConditionApproved)), approvalRequest.Generation) {
						klog.V(2).InfoS("The approval request has not been approved yet", "approvalRequestTask", requestRef, "stage", updatingStage.Name, "clusterStagedUpdateRun", updateRunRef)
						return false, -1, nil
					}
					klog.V(2).InfoS("The approval request has been approved", "approvalRequestTask", requestRef, "stage", updatingStage.Name, "clusterStagedUpdateRun", updateRunRef)
					if !approvalAccepted {
						if err = r.updateApprovalRequestAccepted(ctx, &approvalRequest); err != nil {
							klog.ErrorS(err, "Failed to accept the approved approval request", "approvalRequest", requestRef, "stage", updatingStage.Name, "clusterStagedUpdateRun", updateRunRef)
							// retriable err
							return false, 0, err
						}
					}
					markAfterStageRequestApproved(&updatingStageStatus.AfterStageTaskStatus[i], updateRun.Generation)
				} else {
					// retriable error
					klog.ErrorS(err, "Failed to create the approval request", "approvalRequest", requestRef, "stage", updatingStage.Name, "clusterStagedUpdateRun", updateRunRef)
					return false, -1, controller.NewAPIServerError(false, err)
				}
			} else {
				// The approval request has been created for the first time.
				klog.V(2).InfoS("The approval request has been created", "approvalRequestTask", requestRef, "stage", updatingStage.Name, "clusterStagedUpdateRun", updateRunRef)
				markAfterStageRequestCreated(&updatingStageStatus.AfterStageTaskStatus[i], updateRun.Generation)
				return false, -1, nil
			}
		}
	}
	// All the after stage tasks have been finished or the for loop will return before this line.
	return true, 0, nil
}

// updateBindingRolloutStarted updates the binding status to indicate the rollout has started.
func (r *Reconciler) updateBindingRolloutStarted(ctx context.Context, binding *placementv1beta1.ClusterResourceBinding, updateRun *placementv1beta1.ClusterStagedUpdateRun) error {
	// first reset the condition to reflect the latest lastTransitionTime
	binding.RemoveCondition(string(placementv1beta1.ResourceBindingRolloutStarted))
	cond := metav1.Condition{
		Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: binding.Generation,
		Reason:             condition.RolloutStartedReason,
		Message:            fmt.Sprintf("Detected the new changes on the resources and started the rollout process, resourceSnapshotIndex: %s, clusterStagedUpdateRun: %s", updateRun.Spec.ResourceSnapshotIndex, updateRun.Name),
	}
	binding.SetConditions(cond)
	if err := r.Client.Status().Update(ctx, binding); err != nil {
		klog.ErrorS(err, "Failed to update binding status", "clusterResourceBinding", klog.KObj(binding), "condition", cond)
		return controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Updated binding as rolloutStarted", "clusterResourceBinding", klog.KObj(binding), "condition", cond)
	return nil
}

// updateApprovalRequestAccepted updates the *approved* clusterApprovalRequest status to indicate the approval accepted.
func (r *Reconciler) updateApprovalRequestAccepted(ctx context.Context, appReq *placementv1beta1.ClusterApprovalRequest) error {
	cond := metav1.Condition{
		Type:               string(placementv1beta1.ApprovalRequestConditionApprovalAccepted),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: appReq.Generation,
		Reason:             condition.ApprovalRequestApprovalAcceptedReason,
	}
	meta.SetStatusCondition(&appReq.Status.Conditions, cond)
	if err := r.Client.Status().Update(ctx, appReq); err != nil {
		klog.ErrorS(err, "Failed to update approval request status", "clusterApprovalRequest", klog.KObj(appReq), "condition", cond)
		return controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Updated approval request as approval accepted", "clusterApprovalRequest", klog.KObj(appReq), "condition", cond)
	return nil
}

// isBindingSyncedWithClusterStatus checks if the binding is up-to-date with the cluster status.
func isBindingSyncedWithClusterStatus(resourceSnapshotName string, updateRun *placementv1beta1.ClusterStagedUpdateRun, binding *placementv1beta1.ClusterResourceBinding, cluster *placementv1beta1.ClusterUpdatingStatus) bool {
	if binding.Spec.ResourceSnapshotName != resourceSnapshotName {
		klog.ErrorS(fmt.Errorf("binding has different resourceSnapshotName, want: %s, got: %s", resourceSnapshotName, binding.Spec.ResourceSnapshotName), "ClusterResourceBinding is not up-to-date", "clusterResourceBinding", klog.KObj(binding), "clusterStagedUpdateRun", klog.KObj(updateRun))
		return false
	}
	if !reflect.DeepEqual(cluster.ResourceOverrideSnapshots, binding.Spec.ResourceOverrideSnapshots) {
		klog.ErrorS(fmt.Errorf("binding has different resourceOverrideSnapshots, want: %v, got: %v", cluster.ResourceOverrideSnapshots, binding.Spec.ResourceOverrideSnapshots), "ClusterResourceBinding is not up-to-date", "clusterResourceBinding", klog.KObj(binding), "clusterStagedUpdateRun", klog.KObj(updateRun))
		return false
	}
	if !reflect.DeepEqual(cluster.ClusterResourceOverrideSnapshots, binding.Spec.ClusterResourceOverrideSnapshots) {
		klog.ErrorS(fmt.Errorf("binding has different clusterResourceOverrideSnapshots, want: %v, got: %v", cluster.ClusterResourceOverrideSnapshots, binding.Spec.ClusterResourceOverrideSnapshots), "ClusterResourceBinding is not up-to-date", "clusterResourceBinding", klog.KObj(binding), "clusterStagedUpdateRun", klog.KObj(updateRun))
		return false
	}
	if !reflect.DeepEqual(binding.Spec.ApplyStrategy, updateRun.Status.ApplyStrategy) {
		klog.ErrorS(fmt.Errorf("binding has different applyStrategy, want: %v, got: %v", updateRun.Status.ApplyStrategy, binding.Spec.ApplyStrategy), "ClusterResourceBinding is not up-to-date", "clusterResourceBinding", klog.KObj(binding), "clusterStagedUpdateRun", klog.KObj(updateRun))
		return false
	}
	return true
}

// checkClusterUpdateResult checks if the resources have been updated successfully on a given cluster.
// It returns true if the resources have been updated successfully or any error if the update failed.
func checkClusterUpdateResult(
	binding *placementv1beta1.ClusterResourceBinding,
	clusterStatus *placementv1beta1.ClusterUpdatingStatus,
	updatingStage *placementv1beta1.StageUpdatingStatus,
	updateRun *placementv1beta1.ClusterStagedUpdateRun,
) (bool, error) {
	availCond := binding.GetCondition(string(placementv1beta1.ResourceBindingAvailable))
	if condition.IsConditionStatusTrue(availCond, binding.Generation) {
		// The resource updated on the cluster is available.
		klog.InfoS("The cluster has been updated", "cluster", clusterStatus.ClusterName, "stage", updatingStage.StageName, "clusterStagedUpdateRun", klog.KObj(updateRun))
		markClusterUpdatingSucceeded(clusterStatus, updateRun.Generation)
		return true, nil
	}
	if bindingutils.HasBindingFailed(binding) {
		// We have no way to know if the failed condition is recoverable or not so we just let it run
		klog.InfoS("The cluster updating encountered an error", "cluster", clusterStatus.ClusterName, "stage", updatingStage.StageName, "clusterStagedUpdateRun", klog.KObj(updateRun))
		// TODO(wantjian): identify some non-recoverable error and mark the cluster updating as failed
		return false, fmt.Errorf("the cluster updating encountered an error at stage `%s`, updateRun := `%s`", updatingStage.StageName, updateRun.Name)
	}
	klog.InfoS("The application on the cluster is in the mid of being updated", "cluster", clusterStatus.ClusterName, "stage", updatingStage.StageName, "clusterStagedUpdateRun", klog.KObj(updateRun))
	return false, nil
}

// markUpdateRunStarted marks the update run as started in memory.
func markUpdateRunStarted(updateRun *placementv1beta1.ClusterStagedUpdateRun) {
	meta.SetStatusCondition(&updateRun.Status.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionProgressing),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: updateRun.Generation,
		Reason:             condition.UpdateRunStartedReason,
	})
}

// markStageUpdatingStarted marks the stage updating status as started in memory.
func markStageUpdatingStarted(stageUpdatingStatus *placementv1beta1.StageUpdatingStatus, generation int64) {
	if stageUpdatingStatus.StartTime == nil {
		stageUpdatingStatus.StartTime = &metav1.Time{Time: time.Now()}
	}
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingStartedReason,
	})
}

// markStageUpdatingWaiting marks the stage updating status as waiting in memory.
func markStageUpdatingWaiting(stageUpdatingStatus *placementv1beta1.StageUpdatingStatus, generation int64) {
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingWaitingReason,
	})
}

// markStageUpdatingSucceeded marks the stage updating status as succeeded in memory.
func markStageUpdatingSucceeded(stageUpdatingStatus *placementv1beta1.StageUpdatingStatus, generation int64) {
	if stageUpdatingStatus.EndTime == nil {
		stageUpdatingStatus.EndTime = &metav1.Time{Time: time.Now()}
	}
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageUpdatingConditionSucceeded),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingSucceededReason,
	})
}

// markStageUpdatingFailed marks the stage updating status as failed in memory.
func markStageUpdatingFailed(stageUpdatingStatus *placementv1beta1.StageUpdatingStatus, generation int64, message string) {
	if stageUpdatingStatus.EndTime == nil {
		stageUpdatingStatus.EndTime = &metav1.Time{Time: time.Now()}
	}
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageUpdatingConditionSucceeded),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingFailedReason,
		Message:            message,
	})
}

// markClusterUpdatingStarted marks the cluster updating status as started in memory.
func markClusterUpdatingStarted(clusterUpdatingStatus *placementv1beta1.ClusterUpdatingStatus, generation int64) {
	meta.SetStatusCondition(&clusterUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.ClusterUpdatingConditionStarted),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.ClusterUpdatingStartedReason,
	})
}

// markClusterUpdatingSucceeded marks the cluster updating status as succeeded in memory.
func markClusterUpdatingSucceeded(clusterUpdatingStatus *placementv1beta1.ClusterUpdatingStatus, generation int64) {
	meta.SetStatusCondition(&clusterUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.ClusterUpdatingConditionSucceeded),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.ClusterUpdatingSucceededReason,
	})
}

// markClusterUpdatingFailed marks the cluster updating status as failed in memory.
func markClusterUpdatingFailed(clusterUpdatingStatus *placementv1beta1.ClusterUpdatingStatus, generation int64, message string) {
	meta.SetStatusCondition(&clusterUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.ClusterUpdatingConditionSucceeded),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: generation,
		Reason:             condition.ClusterUpdatingFailedReason,
		Message:            message,
	})
}

// markAfterStageRequestCreated marks the Approval after stage task as ApprovalRequestCreated in memory.
func markAfterStageRequestCreated(afterStageTaskStatus *placementv1beta1.AfterStageTaskStatus, generation int64) {
	meta.SetStatusCondition(&afterStageTaskStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.AfterStageTaskConditionApprovalRequestCreated),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.AfterStageTaskApprovalRequestCreatedReason,
	})
}

// markAfterStageRequestApproved marks the Approval after stage task as Approved in memory.
func markAfterStageRequestApproved(afterStageTaskStatus *placementv1beta1.AfterStageTaskStatus, generation int64) {
	meta.SetStatusCondition(&afterStageTaskStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.AfterStageTaskConditionApprovalRequestApproved),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.AfterStageTaskApprovalRequestApprovedReason,
	})
}

// markAfterStageWaitTimeElapsed marks the TimeWait after stage task as TimeElapsed in memory.
func markAfterStageWaitTimeElapsed(afterStageTaskStatus *placementv1beta1.AfterStageTaskStatus, generation int64) {
	meta.SetStatusCondition(&afterStageTaskStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.AfterStageTaskConditionWaitTimeElapsed),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.AfterStageTaskWaitTimeElapsedReason,
	})
}
