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
	"reflect"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
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

	// updateRunStuckThreshold is the time to wait on a single cluster update before marking update run as stuck.
	// TODO(wantjian): make this configurable
	updateRunStuckThreshold = 5 * time.Minute
)

// execute executes the update run by updating the clusters in the updating stage specified by updatingStageIndex.
// It returns a boolean indicating if the updateRun execution is completed,
// the time to wait before rechecking the cluster update status, and any error encountered.
func (r *Reconciler) execute(
	ctx context.Context,
	updateRun placementv1beta1.UpdateRunObj,
	updatingStageIndex int,
	toBeUpdatedBindings, toBeDeletedBindings []placementv1beta1.BindingObj,
) (finished bool, waitTime time.Duration, err error) {
	updateRunStatus := updateRun.GetUpdateRunStatus()
	var updatingStageStatus *placementv1beta1.StageUpdatingStatus

	// Set up defer function to handle errStagedUpdatedAborted.
	defer func() {
		checkIfErrorStagedUpdateAborted(err, updateRun, updatingStageStatus)
	}()

	// Mark updateRun as progressing if it's not already marked as waiting or stuck.
	// This avoids triggering an unnecessary in-memory transition from stuck (waiting) -> progressing -> stuck (waiting),
	// which would update the lastTransitionTime even though the status hasn't effectively changed.
	markUpdateRunProgressingIfNotWaitingOrStuck(updateRun)
	if updatingStageIndex < len(updateRunStatus.StagesStatus) {
		updatingStageStatus = &updateRunStatus.StagesStatus[updatingStageIndex]
		approved, err := r.checkBeforeStageTasksStatus(ctx, updatingStageIndex, updateRun)
		if err != nil {
			return false, 0, err
		}
		if !approved {
			markStageUpdatingWaiting(updatingStageStatus, updateRun.GetGeneration(), "Not all before-stage tasks are completed, waiting for approval")
			markUpdateRunWaiting(updateRun, fmt.Sprintf(condition.UpdateRunWaitingMessageFmt, "before-stage", updatingStageStatus.StageName))
			return false, stageUpdatingWaitTime, nil
		}
		maxConcurrency, err := calculateMaxConcurrencyValue(updateRunStatus, updatingStageIndex)
		if err != nil {
			return false, 0, fmt.Errorf("%w: %s", errStagedUpdatedAborted, err.Error())
		}
		waitTime, err = r.executeUpdatingStage(ctx, updateRun, updatingStageIndex, toBeUpdatedBindings, maxConcurrency)
		// The execution has not finished yet.
		return false, waitTime, err
	}
	// All the stages have finished, now start the delete stage.
	finished, err = r.executeDeleteStage(ctx, updateRun, toBeDeletedBindings)
	return finished, clusterUpdatingWaitTime, err
}

// checkBeforeStageTasksStatus checks if the before stage tasks have finished.
// It returns if the before stage tasks have finished or error if the before stage tasks failed.
func (r *Reconciler) checkBeforeStageTasksStatus(ctx context.Context, updatingStageIndex int, updateRun placementv1beta1.UpdateRunObj) (bool, error) {
	updateRunRef := klog.KObj(updateRun)
	updateRunStatus := updateRun.GetUpdateRunStatus()
	updatingStage := &updateRunStatus.UpdateStrategySnapshot.Stages[updatingStageIndex]
	if updatingStage.BeforeStageTasks == nil {
		klog.V(2).InfoS("There is no before stage task for this stage", "stage", updatingStage.Name, "updateRun", updateRunRef)
		return true, nil
	}

	updatingStageStatus := &updateRunStatus.StagesStatus[updatingStageIndex]
	for i, task := range updatingStage.BeforeStageTasks {
		switch task.Type {
		case placementv1beta1.StageTaskTypeApproval:
			approved, err := r.handleStageApprovalTask(ctx, &updatingStageStatus.BeforeStageTaskStatus[i], updatingStage, updateRun, placementv1beta1.BeforeStageTaskLabelValue)
			if err != nil {
				return false, err
			}
			return approved, nil // Ideally there should be only one approval task in before stage tasks.
		default:
			// Approval is the only supported before stage task.
			unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("found unsupported task type in before stage tasks: %s", task.Type))
			klog.ErrorS(unexpectedErr, "Task type is not supported in before stage tasks", "stage", updatingStage.Name, "updateRun", updateRunRef, "taskType", task.Type)
			return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
		}
	}
	return true, nil
}

// executeUpdatingStage executes a single updating stage by updating the bindings.
func (r *Reconciler) executeUpdatingStage(
	ctx context.Context,
	updateRun placementv1beta1.UpdateRunObj,
	updatingStageIndex int,
	toBeUpdatedBindings []placementv1beta1.BindingObj,
	maxConcurrency int,
) (time.Duration, error) {
	updateRunStatus := updateRun.GetUpdateRunStatus()
	updateRunSpec := updateRun.GetUpdateRunSpec()
	updatingStageStatus := &updateRunStatus.StagesStatus[updatingStageIndex]
	// The parse error is ignored because the initialization should have caught it.
	resourceIndex, _ := strconv.Atoi(updateRunStatus.ResourceSnapshotIndexUsed)
	resourceSnapshotName := fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, updateRunSpec.PlacementName, resourceIndex)
	updateRunRef := klog.KObj(updateRun)
	// Create the map of the toBeUpdatedBindings.
	toBeUpdatedBindingsMap := make(map[string]placementv1beta1.BindingObj, len(toBeUpdatedBindings))
	for _, binding := range toBeUpdatedBindings {
		bindingSpec := binding.GetBindingSpec()
		toBeUpdatedBindingsMap[bindingSpec.TargetCluster] = binding
	}

	finishedClusterCount := 0
	clusterUpdatingCount := 0
	var stuckClusterNames []string
	var clusterUpdateErrors []error
	// Go through each cluster in the stage and check if it's updating/succeeded/failed.
	for i := 0; i < len(updatingStageStatus.Clusters) && clusterUpdatingCount < maxConcurrency; i++ {
		clusterStatus := &updatingStageStatus.Clusters[i]
		clusterUpdateSucceededCond := meta.FindStatusCondition(clusterStatus.Conditions, string(placementv1beta1.ClusterUpdatingConditionSucceeded))
		if condition.IsConditionStatusTrue(clusterUpdateSucceededCond, updateRun.GetGeneration()) {
			// The cluster has been updated successfully.
			finishedClusterCount++
			continue
		}
		clusterUpdatingCount++
		if condition.IsConditionStatusFalse(clusterUpdateSucceededCond, updateRun.GetGeneration()) {
			// The cluster is marked as failed to update, this cluster is counted as updating cluster since it's not finished to avoid processing more clusters than maxConcurrency in this round.
			failedErr := fmt.Errorf("the cluster `%s` in the stage %s has failed", clusterStatus.ClusterName, updatingStageStatus.StageName)
			klog.ErrorS(failedErr, "The cluster has failed to be updated", "updateRun", updateRunRef)
			clusterUpdateErrors = append(clusterUpdateErrors, fmt.Errorf("%w: %s", errStagedUpdatedAborted, failedErr.Error()))
			continue
		}
		// The cluster needs to be processed.
		clusterStartedCond := meta.FindStatusCondition(clusterStatus.Conditions, string(placementv1beta1.ClusterUpdatingConditionStarted))
		binding := toBeUpdatedBindingsMap[clusterStatus.ClusterName]
		if !condition.IsConditionStatusTrue(clusterStartedCond, updateRun.GetGeneration()) {
			// The cluster has not started updating yet.
			if !isBindingSyncedWithClusterStatus(resourceSnapshotName, updateRun, binding, clusterStatus) {
				klog.V(2).InfoS("Found the first cluster that needs to be updated", "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "updateRun", updateRunRef)
				// The binding is not up-to-date with the cluster status.
				bindingSpec := binding.GetBindingSpec()
				bindingSpec.State = placementv1beta1.BindingStateBound
				bindingSpec.ResourceSnapshotName = resourceSnapshotName
				bindingSpec.ResourceOverrideSnapshots = clusterStatus.ResourceOverrideSnapshots
				bindingSpec.ClusterResourceOverrideSnapshots = clusterStatus.ClusterResourceOverrideSnapshots
				bindingSpec.ApplyStrategy = updateRunStatus.ApplyStrategy
				if err := r.Client.Update(ctx, binding); err != nil {
					klog.ErrorS(err, "Failed to update binding to be bound with the matching spec of the updateRun", "binding", klog.KObj(binding), "updateRun", updateRunRef)
					clusterUpdateErrors = append(clusterUpdateErrors, controller.NewUpdateIgnoreConflictError(err))
					continue
				}
				klog.V(2).InfoS("Updated the status of a binding to bound", "binding", klog.KObj(binding), "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "updateRun", updateRunRef)
				if err := r.updateBindingRolloutStarted(ctx, binding, updateRun); err != nil {
					clusterUpdateErrors = append(clusterUpdateErrors, err)
					continue
				}
			} else {
				klog.V(2).InfoS("Found the first binding that is updating but the cluster status has not been updated", "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "updateRun", updateRunRef)
				bindingSpec := binding.GetBindingSpec()
				if bindingSpec.State != placementv1beta1.BindingStateBound {
					bindingSpec.State = placementv1beta1.BindingStateBound
					if err := r.Client.Update(ctx, binding); err != nil {
						klog.ErrorS(err, "Failed to update a binding to be bound", "binding", klog.KObj(binding), "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "updateRun", updateRunRef)
						clusterUpdateErrors = append(clusterUpdateErrors, controller.NewUpdateIgnoreConflictError(err))
						continue
					}
					klog.V(2).InfoS("Updated the status of a binding to bound", "binding", klog.KObj(binding), "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "updateRun", updateRunRef)
					if err := r.updateBindingRolloutStarted(ctx, binding, updateRun); err != nil {
						clusterUpdateErrors = append(clusterUpdateErrors, err)
						continue
					}
				} else if !condition.IsConditionStatusTrue(meta.FindStatusCondition(binding.GetBindingStatus().Conditions, string(placementv1beta1.ResourceBindingRolloutStarted)), binding.GetGeneration()) {
					klog.V(2).InfoS("The binding is bound and up-to-date but the generation is updated by the scheduler, update rolloutStarted status again", "binding", klog.KObj(binding), "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "updateRun", updateRunRef)
					if err := r.updateBindingRolloutStarted(ctx, binding, updateRun); err != nil {
						clusterUpdateErrors = append(clusterUpdateErrors, err)
						continue
					}
				} else {
					if _, updateErr := checkClusterUpdateResult(binding, clusterStatus, updatingStageStatus, updateRun); updateErr != nil {
						clusterUpdateErrors = append(clusterUpdateErrors, updateErr)
						continue
					}
				}
			}
			markClusterUpdatingStarted(clusterStatus, updateRun.GetGeneration())
			markStageUpdatingProgressStarted(updatingStageStatus, updateRun.GetGeneration())
			// Need to continue as we need to process at most maxConcurrency number of clusters in parallel.
			continue
		}

		// Now the cluster has to be updating, the binding should point to the right resource snapshot and the binding should be bound.
		inSync := isBindingSyncedWithClusterStatus(resourceSnapshotName, updateRun, binding, clusterStatus)
		rolloutStarted := condition.IsConditionStatusTrue(meta.FindStatusCondition(binding.GetBindingStatus().Conditions, string(placementv1beta1.ResourceBindingRolloutStarted)), binding.GetGeneration())
		bindingSpec := binding.GetBindingSpec()
		if !inSync || !rolloutStarted || bindingSpec.State != placementv1beta1.BindingStateBound {
			// This issue mostly happens when there are concurrent updateRuns referencing the same placement but releasing different versions.
			// After the 1st updateRun updates the binding, and before the controller re-checks the binding status, the 2nd updateRun updates the same binding, and thus the 1st updateRun is preempted and observes the binding not matching the desired state.
			preemptedErr := controller.NewUserError(fmt.Errorf("the binding of the updating cluster `%s` in the stage `%s` is not up-to-date with the desired status, "+
				"please check the status of binding `%s` and see if there is a concurrent updateRun referencing the same placement and updating the same cluster",
				clusterStatus.ClusterName, updatingStageStatus.StageName, klog.KObj(binding)))
			klog.ErrorS(preemptedErr, "The binding has been changed during updating",
				"bindingSpecInSync", inSync, "bindingState", bindingSpec.State,
				"bindingRolloutStarted", rolloutStarted, "binding", klog.KObj(binding), "updateRun", updateRunRef)
			markClusterUpdatingFailed(clusterStatus, updateRun.GetGeneration(), preemptedErr.Error())
			clusterUpdateErrors = append(clusterUpdateErrors, fmt.Errorf("%w: %s", errStagedUpdatedAborted, preemptedErr.Error()))
			continue
		}

		finished, updateErr := checkClusterUpdateResult(binding, clusterStatus, updatingStageStatus, updateRun)
		if updateErr != nil {
			clusterUpdateErrors = append(clusterUpdateErrors, updateErr)
		}
		if finished {
			finishedClusterCount++
			// The cluster has finished successfully, we can process another cluster in this round.
			clusterUpdatingCount--
		} else {
			// If cluster update has been running for more than "updateRunStuckThreshold", mark the update run as stuck.
			timeElapsed := time.Since(clusterStartedCond.LastTransitionTime.Time)
			if timeElapsed > updateRunStuckThreshold {
				klog.V(2).InfoS("Time waiting for cluster update to finish passes threshold, mark the update run as stuck", "time elapsed", timeElapsed, "threshold", updateRunStuckThreshold, "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "updateRun", updateRunRef)
				stuckClusterNames = append(stuckClusterNames, clusterStatus.ClusterName)
			}
		}
	}

	// After processing maxConcurrency number of cluster, check if we need to mark the update run as stuck or progressing.
	aggregateUpdateRunStatus(updateRun, updatingStageStatus.StageName, stuckClusterNames)

	// Aggregate and return errors.
	if len(clusterUpdateErrors) > 0 {
		// Even though we aggregate errors, we can still check if one of the errors is a staged update aborted error by using errors.Is in the caller.
		return 0, utilerrors.NewAggregate(clusterUpdateErrors)
	}

	if finishedClusterCount == len(updatingStageStatus.Clusters) {
		return r.handleStageCompletion(ctx, updatingStageIndex, updateRun, updatingStageStatus)
	}

	// Some clusters are still updating.
	return clusterUpdatingWaitTime, nil
}

// handleStageCompletion handles the completion logic when all clusters in a stage are finished.
// Returns the wait time and any error encountered.
func (r *Reconciler) handleStageCompletion(
	ctx context.Context,
	updatingStageIndex int,
	updateRun placementv1beta1.UpdateRunObj,
	updatingStageStatus *placementv1beta1.StageUpdatingStatus,
) (time.Duration, error) {
	updateRunRef := klog.KObj(updateRun)

	// All the clusters in the stage have been updated.
	markUpdateRunWaiting(updateRun, fmt.Sprintf(condition.UpdateRunWaitingMessageFmt, "after-stage", updatingStageStatus.StageName))
	markStageUpdatingWaiting(updatingStageStatus, updateRun.GetGeneration(), "All clusters in the stage are updated, waiting for after-stage tasks to complete")
	klog.V(2).InfoS("The stage has finished all cluster updating", "stage", updatingStageStatus.StageName, "updateRun", updateRunRef)
	// Check if the after stage tasks are ready.
	approved, waitTime, err := r.checkAfterStageTasksStatus(ctx, updatingStageIndex, updateRun)
	if err != nil {
		return 0, err
	}
	if approved {
		markUpdateRunProgressing(updateRun)
		markStageUpdatingSucceeded(updatingStageStatus, updateRun.GetGeneration())
		// No need to wait to get to the next stage.
		return 0, nil
	}
	// The after stage tasks are not ready yet.
	if waitTime < 0 {
		waitTime = stageUpdatingWaitTime
	}
	return waitTime, nil
}

// executeDeleteStage executes the delete stage by deleting the bindings.
func (r *Reconciler) executeDeleteStage(
	ctx context.Context,
	updateRun placementv1beta1.UpdateRunObj,
	toBeDeletedBindings []placementv1beta1.BindingObj,
) (bool, error) {
	updateRunRef := klog.KObj(updateRun)
	updateRunStatus := updateRun.GetUpdateRunStatus()
	existingDeleteStageStatus := updateRunStatus.DeletionStageStatus
	existingDeleteStageClusterMap := make(map[string]*placementv1beta1.ClusterUpdatingStatus, len(existingDeleteStageStatus.Clusters))
	for i := range existingDeleteStageStatus.Clusters {
		existingDeleteStageClusterMap[existingDeleteStageStatus.Clusters[i].ClusterName] = &existingDeleteStageStatus.Clusters[i]
	}
	// Mark the delete stage as started in case it's not.
	markStageUpdatingProgressStarted(updateRunStatus.DeletionStageStatus, updateRun.GetGeneration())
	for _, binding := range toBeDeletedBindings {
		bindingSpec := binding.GetBindingSpec()
		curCluster, exist := existingDeleteStageClusterMap[bindingSpec.TargetCluster]
		if !exist {
			// This is unexpected because we already checked in validation.
			missingErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the to be deleted cluster `%s` is not in the deleting stage during execution", bindingSpec.TargetCluster))
			klog.ErrorS(missingErr, "The cluster in the deleting stage does not include all the to be deleted binding", "updateRun", updateRunRef)
			return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, missingErr.Error())
		}
		// In validation, we already check the binding must exist in the status.
		delete(existingDeleteStageClusterMap, bindingSpec.TargetCluster)
		// Make sure the cluster is not marked as deleted as the binding is still there.
		if condition.IsConditionStatusTrue(meta.FindStatusCondition(curCluster.Conditions, string(placementv1beta1.ClusterUpdatingConditionSucceeded)), updateRun.GetGeneration()) {
			unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the deleted cluster `%s` in the deleting stage still has a binding", bindingSpec.TargetCluster))
			klog.ErrorS(unexpectedErr, "The cluster in the deleting stage is not removed yet but marked as deleted", "cluster", curCluster.ClusterName, "updateRun", updateRunRef)
			return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
		}
		if condition.IsConditionStatusTrue(meta.FindStatusCondition(curCluster.Conditions, string(placementv1beta1.ClusterUpdatingConditionStarted)), updateRun.GetGeneration()) {
			// The cluster status is marked as being deleted.
			if binding.GetDeletionTimestamp().IsZero() {
				// The cluster is marked as deleting but the binding is not deleting.
				unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the cluster `%s` in the deleting stage is marked as deleting but its corresponding binding is not deleting", curCluster.ClusterName))
				klog.ErrorS(unexpectedErr, "The binding should be deleting before we mark a cluster deleting", "clusterStatus", curCluster, "updateRun", updateRunRef)
				return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
			}
			continue
		}
		// The cluster status is not deleting yet
		if err := r.Client.Delete(ctx, binding); err != nil {
			klog.ErrorS(err, "Failed to delete a binding in the update run", "binding", klog.KObj(binding), "cluster", curCluster.ClusterName, "updateRun", updateRunRef)
			return false, controller.NewAPIServerError(false, err)
		}
		klog.V(2).InfoS("Deleted a binding pointing to a to be deleted cluster", "binding", klog.KObj(binding), "cluster", curCluster.ClusterName, "updateRun", updateRunRef)
		markClusterUpdatingStarted(curCluster, updateRun.GetGeneration())
	}
	// The rest of the clusters in the stage are not in the toBeDeletedBindings so it should be marked as delete succeeded.
	for _, clusterStatus := range existingDeleteStageClusterMap {
		// Make sure the cluster is marked as deleted.
		if !condition.IsConditionStatusTrue(meta.FindStatusCondition(clusterStatus.Conditions, string(placementv1beta1.ClusterUpdatingConditionStarted)), updateRun.GetGeneration()) {
			markClusterUpdatingStarted(clusterStatus, updateRun.GetGeneration())
		}
		markClusterUpdatingSucceeded(clusterStatus, updateRun.GetGeneration())
	}
	klog.InfoS("The delete stage is progressing", "numberOfDeletingClusters", len(toBeDeletedBindings), "updateRun", updateRunRef)
	if len(toBeDeletedBindings) == 0 {
		markStageUpdatingSucceeded(updateRunStatus.DeletionStageStatus, updateRun.GetGeneration())
	}
	return len(toBeDeletedBindings) == 0, nil
}

// checkAfterStageTasksStatus checks if the after stage tasks have finished.
// It returns if the after stage tasks have finished or error if the after stage tasks failed.
// It also returns the time to wait before rechecking the wait type of task. It turns -1 if the task is not a wait type.
func (r *Reconciler) checkAfterStageTasksStatus(ctx context.Context, updatingStageIndex int, updateRun placementv1beta1.UpdateRunObj) (bool, time.Duration, error) {
	updateRunRef := klog.KObj(updateRun)
	updateRunStatus := updateRun.GetUpdateRunStatus()
	updatingStageStatus := &updateRunStatus.StagesStatus[updatingStageIndex]
	updatingStage := &updateRunStatus.UpdateStrategySnapshot.Stages[updatingStageIndex]
	if updatingStage.AfterStageTasks == nil {
		klog.V(2).InfoS("There is no after stage task for this stage", "stage", updatingStage.Name, "updateRun", updateRunRef)
		return true, 0, nil
	}
	passed := true
	afterStageWaitTime := time.Duration(-1)
	for i, task := range updatingStage.AfterStageTasks {
		switch task.Type {
		case placementv1beta1.StageTaskTypeTimedWait:
			waitStartTime := meta.FindStatusCondition(updatingStageStatus.Conditions, string(placementv1beta1.StageUpdatingConditionProgressing)).LastTransitionTime.Time
			// Check if the wait time has passed.
			waitTime := time.Until(waitStartTime.Add(task.WaitTime.Duration))
			if waitTime > 0 {
				klog.V(2).InfoS("The after stage task still need to wait", "waitStartTime", waitStartTime, "waitTime", task.WaitTime, "stage", updatingStage.Name, "updateRun", updateRunRef)
				passed = false
				afterStageWaitTime = waitTime
			} else {
				markAfterStageWaitTimeElapsed(&updatingStageStatus.AfterStageTaskStatus[i], updateRun.GetGeneration())
				klog.V(2).InfoS("The after stage wait task has completed", "stage", updatingStage.Name, "updateRun", updateRunRef)
			}
		case placementv1beta1.StageTaskTypeApproval:
			approved, err := r.handleStageApprovalTask(ctx, &updatingStageStatus.AfterStageTaskStatus[i], updatingStage, updateRun, placementv1beta1.AfterStageTaskLabelValue)
			if err != nil {
				return false, -1, err
			}
			if !approved {
				passed = false
			}
		}
	}
	if passed {
		afterStageWaitTime = 0
	}
	return passed, afterStageWaitTime, nil
}

// handleStageApprovalTask handles the approval task logic for before or after stage tasks.
// It returns true if the task is approved, false otherwise, and any error encountered.
func (r *Reconciler) handleStageApprovalTask(
	ctx context.Context,
	stageTaskStatus *placementv1beta1.StageTaskStatus,
	updatingStage *placementv1beta1.StageConfig,
	updateRun placementv1beta1.UpdateRunObj,
	stageTaskType string,
) (bool, error) {
	updateRunRef := klog.KObj(updateRun)

	stageTaskApproved := condition.IsConditionStatusTrue(meta.FindStatusCondition(stageTaskStatus.Conditions, string(placementv1beta1.StageTaskConditionApprovalRequestApproved)), updateRun.GetGeneration())
	if stageTaskApproved {
		// The stageTask has been approved.
		return true, nil
	}

	// Check if the approval request has been created.
	approvalRequest := buildApprovalRequestObject(types.NamespacedName{Name: stageTaskStatus.ApprovalRequestName, Namespace: updateRun.GetNamespace()}, updatingStage.Name, updateRun.GetName(), stageTaskType)
	requestRef := klog.KObj(approvalRequest)
	if err := r.Client.Create(ctx, approvalRequest); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// The approval task already exists.
			markStageTaskRequestCreated(stageTaskStatus, updateRun.GetGeneration())
			if err = r.Client.Get(ctx, client.ObjectKeyFromObject(approvalRequest), approvalRequest); err != nil {
				klog.ErrorS(err, "Failed to get the already existing approval request", "approvalRequest", requestRef, "stage", updatingStage.Name, "updateRun", updateRunRef)
				return false, controller.NewAPIServerError(true, err)
			}
			approvalRequestSpec := approvalRequest.GetApprovalRequestSpec()
			if approvalRequestSpec.TargetStage != updatingStage.Name || approvalRequestSpec.TargetUpdateRun != updateRun.GetName() {
				unexpectedErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the approval request task `%s/%s` is targeting update run `%s/%s` and stage `%s`, want target update run `%s/%s and stage `%s`", approvalRequest.GetNamespace(), approvalRequest.GetName(), approvalRequest.GetNamespace(), approvalRequestSpec.TargetUpdateRun, approvalRequestSpec.TargetStage, approvalRequest.GetNamespace(), updateRun.GetName(), updatingStage.Name))
				klog.ErrorS(unexpectedErr, "Found an approval request targeting wrong stage", "approvalRequestTask", requestRef, "stage", updatingStage.Name, "updateRun", updateRunRef)
				return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, unexpectedErr.Error())
			}
			approvalRequestStatus := approvalRequest.GetApprovalRequestStatus()
			approvalAccepted := condition.IsConditionStatusTrue(meta.FindStatusCondition(approvalRequestStatus.Conditions, string(placementv1beta1.ApprovalRequestConditionApprovalAccepted)), approvalRequest.GetGeneration())
			approved := condition.IsConditionStatusTrue(meta.FindStatusCondition(approvalRequestStatus.Conditions, string(placementv1beta1.ApprovalRequestConditionApproved)), approvalRequest.GetGeneration())
			if !approvalAccepted && !approved {
				klog.V(2).InfoS("The approval request has not been approved yet", "approvalRequestTask", requestRef, "stage", updatingStage.Name, "updateRun", updateRunRef)
				return false, nil
			}
			if approved {
				klog.V(2).InfoS("The approval request has been approved", "approvalRequestTask", requestRef, "stage", updatingStage.Name, "updateRun", updateRunRef)
				if !approvalAccepted {
					if err = r.updateApprovalRequestAccepted(ctx, approvalRequest); err != nil {
						klog.ErrorS(err, "Failed to accept the approved approval request", "approvalRequest", requestRef, "stage", updatingStage.Name, "updateRun", updateRunRef)
						// retriable err
						return false, err
					}
				}
			} else {
				// Approved state should not change once the approval is accepted.
				klog.V(2).InfoS("The approval request has been approval-accepted, ignoring changing back to unapproved", "approvalRequestTask", requestRef, "stage", updatingStage.Name, "updateRun", updateRunRef)
			}
			markStageTaskRequestApproved(stageTaskStatus, updateRun.GetGeneration())
		} else {
			// retriable error
			klog.ErrorS(err, "Failed to create the approval request", "approvalRequest", requestRef, "stage", updatingStage.Name, "updateRun", updateRunRef)
			return false, controller.NewAPIServerError(false, err)
		}
	} else {
		// The approval request has been created for the first time.
		klog.V(2).InfoS("The approval request has been created", "approvalRequestTask", requestRef, "stage", updatingStage.Name, "updateRun", updateRunRef)
		markStageTaskRequestCreated(stageTaskStatus, updateRun.GetGeneration())
		return false, nil
	}
	return true, nil
}

// updateBindingRolloutStarted updates the binding status to indicate the rollout has started.
func (r *Reconciler) updateBindingRolloutStarted(ctx context.Context, binding placementv1beta1.BindingObj, updateRun placementv1beta1.UpdateRunObj) error {
	// first reset the condition to reflect the latest lastTransitionTime
	binding.RemoveCondition(string(placementv1beta1.ResourceBindingRolloutStarted))
	cond := metav1.Condition{
		Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: binding.GetGeneration(),
		Reason:             condition.RolloutStartedReason,
		Message:            fmt.Sprintf("Detected the new changes on the resources and started the rollout process, resourceSnapshotIndex: %s, updateRun: %s", updateRun.GetUpdateRunSpec().ResourceSnapshotIndex, updateRun.GetName()),
	}
	binding.SetConditions(cond)
	if err := r.Client.Status().Update(ctx, binding); err != nil {
		klog.ErrorS(err, "Failed to update binding status", "binding", klog.KObj(binding), "condition", cond)
		return controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Updated binding as rolloutStarted", "binding", klog.KObj(binding), "condition", cond)
	return nil
}

// updateApprovalRequestAccepted updates the *approved* approval request status to indicate the approval accepted.
func (r *Reconciler) updateApprovalRequestAccepted(ctx context.Context, appReq placementv1beta1.ApprovalRequestObj) error {
	cond := metav1.Condition{
		Type:               string(placementv1beta1.ApprovalRequestConditionApprovalAccepted),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: appReq.GetGeneration(),
		Reason:             condition.ApprovalRequestApprovalAcceptedReason,
		Message:            "The approval request has been approved and cannot be reverted",
	}
	approvalRequestStatus := appReq.GetApprovalRequestStatus()
	meta.SetStatusCondition(&approvalRequestStatus.Conditions, cond)
	if err := r.Client.Status().Update(ctx, appReq); err != nil {
		klog.ErrorS(err, "Failed to update approval request status", "approvalRequest", klog.KObj(appReq), "condition", cond)
		return controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Updated approval request as approval accepted", "approvalRequest", klog.KObj(appReq), "condition", cond)
	return nil
}

// calculateMaxConcurrencyValue calculates the actual max concurrency value for a stage.
// It converts the IntOrString maxConcurrency (which can be an integer or percentage) to an integer value
// based on the total number of clusters in the stage. The value is rounded down with 1 at minimum.
func calculateMaxConcurrencyValue(status *placementv1beta1.UpdateRunStatus, stageIndex int) (int, error) {
	specifiedMaxConcurrency := status.UpdateStrategySnapshot.Stages[stageIndex].MaxConcurrency
	clusterCount := len(status.StagesStatus[stageIndex].Clusters)
	// Round down the maxConcurrency to the number of clusters in the stage.
	maxConcurrencyValue, err := intstr.GetScaledValueFromIntOrPercent(specifiedMaxConcurrency, clusterCount, false)
	if err != nil {
		return 0, err
	}
	// Handle the case where maxConcurrency is specified as percentage but results in 0 after scaling down.
	if maxConcurrencyValue == 0 {
		maxConcurrencyValue = 1
	}
	return maxConcurrencyValue, nil
}

// aggregateUpdateRunStatus aggregates the status of the update run based on the cluster update status.
// It marks the update run as stuck if any clusters are stuck, or as progressing if some clusters have finished updating.
func aggregateUpdateRunStatus(updateRun placementv1beta1.UpdateRunObj, stageName string, stuckClusterNames []string) {
	if len(stuckClusterNames) > 0 {
		markUpdateRunStuck(updateRun, stageName, stuckClusterNames)
	} else if updateRun.GetUpdateRunSpec().State == placementv1beta1.StateRun {
		// If there is no stuck cluster but some progress has been made, mark the update run as progressing.
		markUpdateRunProgressing(updateRun)
	}
}

// isBindingSyncedWithClusterStatus checks if the binding is up-to-date with the cluster status.
func isBindingSyncedWithClusterStatus(resourceSnapshotName string, updateRun placementv1beta1.UpdateRunObj, binding placementv1beta1.BindingObj, cluster *placementv1beta1.ClusterUpdatingStatus) bool {
	bindingSpec := binding.GetBindingSpec()
	if bindingSpec.ResourceSnapshotName != resourceSnapshotName {
		klog.ErrorS(fmt.Errorf("binding has different resourceSnapshotName, want: %s, got: %s", resourceSnapshotName, bindingSpec.ResourceSnapshotName), "binding is not up-to-date", "binding", klog.KObj(binding), "updateRun", klog.KObj(updateRun))
		return false
	}
	if !reflect.DeepEqual(cluster.ResourceOverrideSnapshots, bindingSpec.ResourceOverrideSnapshots) {
		klog.ErrorS(fmt.Errorf("binding has different resourceOverrideSnapshots, want: %v, got: %v", cluster.ResourceOverrideSnapshots, bindingSpec.ResourceOverrideSnapshots), "binding is not up-to-date", "binding", klog.KObj(binding), "updateRun", klog.KObj(updateRun))
		return false
	}
	if !reflect.DeepEqual(cluster.ClusterResourceOverrideSnapshots, bindingSpec.ClusterResourceOverrideSnapshots) {
		klog.ErrorS(fmt.Errorf("binding has different clusterResourceOverrideSnapshots, want: %v, got: %v", cluster.ClusterResourceOverrideSnapshots, bindingSpec.ClusterResourceOverrideSnapshots), "binding is not up-to-date", "binding", klog.KObj(binding), "updateRun", klog.KObj(updateRun))
		return false
	}
	if !reflect.DeepEqual(bindingSpec.ApplyStrategy, updateRun.GetUpdateRunStatus().ApplyStrategy) {
		klog.ErrorS(fmt.Errorf("binding has different applyStrategy, want: %v, got: %v", updateRun.GetUpdateRunStatus().ApplyStrategy, bindingSpec.ApplyStrategy), "binding is not up-to-date", "binding", klog.KObj(binding), "updateRun", klog.KObj(updateRun))
		return false
	}
	return true
}

// checkClusterUpdateResult checks if the resources have been updated successfully on a given cluster.
// It returns true if the resources have been updated successfully or any error if the update failed.
func checkClusterUpdateResult(
	binding placementv1beta1.BindingObj,
	clusterStatus *placementv1beta1.ClusterUpdatingStatus,
	updatingStage *placementv1beta1.StageUpdatingStatus,
	updateRun placementv1beta1.UpdateRunObj,
) (bool, error) {
	availCond := binding.GetCondition(string(placementv1beta1.ResourceBindingAvailable))
	diffReportCondition := binding.GetCondition(string(placementv1beta1.ResourceBindingDiffReported))
	if condition.IsConditionStatusTrue(availCond, binding.GetGeneration()) ||
		condition.IsConditionStatusTrue(diffReportCondition, binding.GetGeneration()) {
		// The resource updated on the cluster is available or diff is successfully reported.
		klog.InfoS("The cluster has been updated", "cluster", clusterStatus.ClusterName, "stage", updatingStage.StageName, "updateRun", klog.KObj(updateRun))
		markClusterUpdatingSucceeded(clusterStatus, updateRun.GetGeneration())
		return true, nil
	}
	if bindingutils.HasBindingFailed(binding) || condition.IsConditionStatusFalse(diffReportCondition, binding.GetGeneration()) {
		// We have no way to know if the failed condition is recoverable or not so we just let it run
		klog.InfoS("The cluster updating encountered an error", "cluster", clusterStatus.ClusterName, "stage", updatingStage.StageName, "updateRun", klog.KObj(updateRun))
		// TODO(wantjian): identify some non-recoverable error and mark the cluster updating as failed
		return false, fmt.Errorf("the cluster updating encountered an error at stage `%s`, updateRun := `%s`", updatingStage.StageName, updateRun.GetName())
	}
	klog.InfoS("The application on the cluster is in the mid of being updated", "cluster", clusterStatus.ClusterName, "stage", updatingStage.StageName, "updateRun", klog.KObj(updateRun))
	return false, nil
}

// buildApprovalRequestObject creates an approval request object for before-stage or after-stage tasks.
// It returns a ClusterApprovalRequest if namespace is empty, otherwise returns an ApprovalRequest.
func buildApprovalRequestObject(namespacedName types.NamespacedName, stageName, updateRunName, stageTaskType string) placementv1beta1.ApprovalRequestObj {
	var approvalRequest placementv1beta1.ApprovalRequestObj
	if namespacedName.Namespace == "" {
		approvalRequest = &placementv1beta1.ClusterApprovalRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespacedName.Name,
				Labels: map[string]string{
					placementv1beta1.TargetUpdatingStageNameLabel:   stageName,
					placementv1beta1.TargetUpdateRunLabel:           updateRunName,
					placementv1beta1.TaskTypeLabel:                  stageTaskType,
					placementv1beta1.IsLatestUpdateRunApprovalLabel: "true",
				},
			},
			Spec: placementv1beta1.ApprovalRequestSpec{
				TargetUpdateRun: updateRunName,
				TargetStage:     stageName,
			},
		}
	} else {
		approvalRequest = &placementv1beta1.ApprovalRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespacedName.Name,
				Namespace: namespacedName.Namespace,
				Labels: map[string]string{
					placementv1beta1.TargetUpdatingStageNameLabel:   stageName,
					placementv1beta1.TargetUpdateRunLabel:           updateRunName,
					placementv1beta1.TaskTypeLabel:                  stageTaskType,
					placementv1beta1.IsLatestUpdateRunApprovalLabel: "true",
				},
			},
			Spec: placementv1beta1.ApprovalRequestSpec{
				TargetUpdateRun: updateRunName,
				TargetStage:     stageName,
			},
		}
	}
	return approvalRequest
}

// markUpdateRunProgressing marks the update run as progressing in memory.
func markUpdateRunProgressing(updateRun placementv1beta1.UpdateRunObj) {
	updateRunStatus := updateRun.GetUpdateRunStatus()
	meta.SetStatusCondition(&updateRunStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionProgressing),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: updateRun.GetGeneration(),
		Reason:             condition.UpdateRunProgressingReason,
		Message:            "The update run is making progress",
	})
}

// markUpdateRunProgressingIfNotWaitingOrStuck marks the update run as progressing in memory if it's not marked as waiting or stuck already.
func markUpdateRunProgressingIfNotWaitingOrStuck(updateRun placementv1beta1.UpdateRunObj) {
	updateRunStatus := updateRun.GetUpdateRunStatus()
	progressingCond := meta.FindStatusCondition(updateRunStatus.Conditions, string(placementv1beta1.StagedUpdateRunConditionProgressing))
	if condition.IsConditionStatusFalse(progressingCond, updateRun.GetGeneration()) &&
		(progressingCond.Reason == condition.UpdateRunWaitingReason || progressingCond.Reason == condition.UpdateRunStuckReason) {
		// The updateRun is waiting or stuck, no need to mark it as started.
		return
	}
	markUpdateRunProgressing(updateRun)
}

// markUpdateRunStuck marks the updateRun as stuck in memory.
func markUpdateRunStuck(updateRun placementv1beta1.UpdateRunObj, stageName string, stuckClusterNames []string) {
	updateRunStatus := updateRun.GetUpdateRunStatus()
	meta.SetStatusCondition(&updateRunStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionProgressing),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: updateRun.GetGeneration(),
		Reason:             condition.UpdateRunStuckReason,
		Message:            fmt.Sprintf("The updateRun is stuck waiting for %s cluster(s) in stage %s to finish updating, please check placement status for potential errors", generateStuckClustersString(stuckClusterNames), stageName),
	})
}

func generateStuckClustersString(stuckClusterNames []string) string {
	if len(stuckClusterNames) == 0 {
		return ""
	}
	if len(stuckClusterNames) <= 3 {
		return strings.Join(stuckClusterNames, ", ")
	}
	// Print first 3 if more than 3 stuck clusters.
	clustersToShow := stuckClusterNames
	if len(stuckClusterNames) > 3 {
		clustersToShow = stuckClusterNames[:3]
	}
	return strings.Join(clustersToShow, ", ") + "..."
}

// markUpdateRunWaiting marks the updateRun as waiting in memory.
func markUpdateRunWaiting(updateRun placementv1beta1.UpdateRunObj, message string) {
	updateRunStatus := updateRun.GetUpdateRunStatus()
	meta.SetStatusCondition(&updateRunStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionProgressing),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: updateRun.GetGeneration(),
		Reason:             condition.UpdateRunWaitingReason,
		Message:            message,
	})
}

// markStageUpdatingProgressStarted marks the stage updating status as started in memory.
func markStageUpdatingProgressStarted(stageUpdatingStatus *placementv1beta1.StageUpdatingStatus, generation int64) {
	if stageUpdatingStatus.StartTime == nil {
		stageUpdatingStatus.StartTime = &metav1.Time{Time: time.Now()}
	}
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingStartedReason,
		Message:            "Clusters in the stage started updating",
	})
}

// markStageUpdatingWaiting marks the stage updating status as waiting in memory.
func markStageUpdatingWaiting(stageUpdatingStatus *placementv1beta1.StageUpdatingStatus, generation int64, message string) {
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingWaitingReason,
		Message:            message,
	})
}

// markStageUpdatingSucceeded marks the stage updating status as succeeded in memory.
func markStageUpdatingSucceeded(stageUpdatingStatus *placementv1beta1.StageUpdatingStatus, generation int64) {
	if stageUpdatingStatus.EndTime == nil {
		stageUpdatingStatus.EndTime = &metav1.Time{Time: time.Now()}
	}
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingSucceededReason,
		Message:            "All clusters in the stage are updated and after-stage tasks are completed",
	})
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageUpdatingConditionSucceeded),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingSucceededReason,
		Message:            "Stage update completed successfully",
	})
}

// markStageUpdatingFailed marks the stage updating status as failed in memory.
func markStageUpdatingFailed(stageUpdatingStatus *placementv1beta1.StageUpdatingStatus, generation int64, message string) {
	if stageUpdatingStatus.EndTime == nil {
		stageUpdatingStatus.EndTime = &metav1.Time{Time: time.Now()}
	}
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingFailedReason,
		Message:            "Stage update aborted due to a non-recoverable error",
	})
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
		Message:            "Cluster update started",
	})
}

// markClusterUpdatingSucceeded marks the cluster updating status as succeeded in memory.
func markClusterUpdatingSucceeded(clusterUpdatingStatus *placementv1beta1.ClusterUpdatingStatus, generation int64) {
	meta.SetStatusCondition(&clusterUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.ClusterUpdatingConditionSucceeded),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.ClusterUpdatingSucceededReason,
		Message:            "Cluster update completed successfully",
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

// markStageTaskRequestCreated marks the Approval for the before or after stage task as ApprovalRequestCreated in memory.
func markStageTaskRequestCreated(stageTaskStatus *placementv1beta1.StageTaskStatus, generation int64) {
	meta.SetStatusCondition(&stageTaskStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageTaskConditionApprovalRequestCreated),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.StageTaskApprovalRequestCreatedReason,
		Message:            "ApprovalRequest object is created",
	})
}

// markStageTaskRequestApproved marks the Approval for the before or after stage task as Approved in memory.
func markStageTaskRequestApproved(stageTaskStatus *placementv1beta1.StageTaskStatus, generation int64) {
	meta.SetStatusCondition(&stageTaskStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageTaskConditionApprovalRequestApproved),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.StageTaskApprovalRequestApprovedReason,
		Message:            "ApprovalRequest object is approved",
	})
}

// markAfterStageWaitTimeElapsed marks the TimeWait after stage task as TimeElapsed in memory.
func markAfterStageWaitTimeElapsed(afterStageTaskStatus *placementv1beta1.StageTaskStatus, generation int64) {
	meta.SetStatusCondition(&afterStageTaskStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageTaskConditionWaitTimeElapsed),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             condition.AfterStageTaskWaitTimeElapsedReason,
		Message:            "Wait time elapsed",
	})
}
