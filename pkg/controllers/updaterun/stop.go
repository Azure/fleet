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
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

// stop handles stopping the update run.
func (r *Reconciler) stop(
	updateRun placementv1beta1.UpdateRunObj,
	updatingStageIndex int,
	toBeUpdatedBindings, toBeDeletedBindings []placementv1beta1.BindingObj,
) (finished bool, waitTime time.Duration, stopErr error) {
	updateRunStatus := updateRun.GetUpdateRunStatus()
	var updatingStageStatus *placementv1beta1.StageUpdatingStatus

	// Set up defer function to handle errStagedUpdatedAborted.
	defer func() {
		checkIfErrorStagedUpdateAborted(stopErr, updateRun, updatingStageStatus)
	}()

	markUpdateRunStopping(updateRun)

	if updatingStageIndex < len(updateRunStatus.StagesStatus) {
		updatingStageStatus = &updateRunStatus.StagesStatus[updatingStageIndex]
		return r.stopUpdatingStage(updateRun, updatingStageStatus, toBeUpdatedBindings)
	}
	// All the stages have finished, stop the delete stage.
	finished, stopErr = r.stopDeleteStage(updateRun, toBeDeletedBindings)
	return finished, clusterUpdatingWaitTime, stopErr
}

// stopUpdatingStage stops the updating stage by letting the updating bindings finish and not starting new updates.
func (r *Reconciler) stopUpdatingStage(
	updateRun placementv1beta1.UpdateRunObj,
	updatingStageStatus *placementv1beta1.StageUpdatingStatus,
	toBeUpdatedBindings []placementv1beta1.BindingObj,
) (bool, time.Duration, error) {
	updateRunRef := klog.KObj(updateRun)
	// Create the map of the toBeUpdatedBindings.
	toBeUpdatedBindingsMap := make(map[string]placementv1beta1.BindingObj, len(toBeUpdatedBindings))
	for _, binding := range toBeUpdatedBindings {
		bindingSpec := binding.GetBindingSpec()
		toBeUpdatedBindingsMap[bindingSpec.TargetCluster] = binding
	}
	// Mark the stage as stopping in case it's not.
	markStageUpdatingStopping(updatingStageStatus, updateRun.GetGeneration())
	clusterUpdatingCount := 0
	var stuckClusterNames []string
	var clusterUpdateErrors []error
	// Go through each cluster in the stage and check if it's updating/succeeded/failed/not started.
	for i := 0; i < len(updatingStageStatus.Clusters); i++ {
		clusterStatus := &updatingStageStatus.Clusters[i]
		clusterStartedCond := meta.FindStatusCondition(clusterStatus.Conditions, string(placementv1beta1.ClusterUpdatingConditionStarted))
		if !condition.IsConditionStatusTrue(clusterStartedCond, updateRun.GetGeneration()) {
			// Cluster has not started updating therefore no need to do anything.
			continue
		}

		clusterUpdateSucceededCond := meta.FindStatusCondition(clusterStatus.Conditions, string(placementv1beta1.ClusterUpdatingConditionSucceeded))
		if condition.IsConditionStatusFalse(clusterUpdateSucceededCond, updateRun.GetGeneration()) || condition.IsConditionStatusTrue(clusterUpdateSucceededCond, updateRun.GetGeneration()) {
			// The cluster has already been updated or failed to update.
			continue
		}

		clusterUpdatingCount++

		binding, exists := toBeUpdatedBindingsMap[clusterStatus.ClusterName]
		if !exists || binding == nil {
			missingBindingErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the binding for cluster `%s` in stage `%s` is not found in the toBeUpdatedBindings map during stopping", clusterStatus.ClusterName, updatingStageStatus.StageName))
			klog.ErrorS(missingBindingErr, "Cannot find the binding for the cluster in the updating stage during stopping", "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "updateRun", updateRunRef)
			clusterUpdateErrors = append(clusterUpdateErrors, fmt.Errorf("%w: %s", errStagedUpdatedAborted, missingBindingErr.Error()))
			continue
		}
		finished, updateErr := checkClusterUpdateResult(binding, clusterStatus, updatingStageStatus, updateRun)
		if updateErr != nil {
			clusterUpdateErrors = append(clusterUpdateErrors, updateErr)
		}
		if finished {
			// The cluster has finished successfully, we can process another cluster in this round.
			clusterUpdatingCount--
		} else {
			// If cluster update has been running for more than "updateRunStuckThreshold", mark the update run as stuck.
			timeElapsed := time.Since(clusterStartedCond.LastTransitionTime.Time)
			if timeElapsed > updateRunStuckThreshold {
				klog.V(2).InfoS("Time waiting for cluster update to finish passes threshold, mark the update run as stuck", "timeElapsed", timeElapsed, "threshold", updateRunStuckThreshold, "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "updateRun", updateRunRef)
				stuckClusterNames = append(stuckClusterNames, clusterStatus.ClusterName)
			}
		}
	}

	// If there are stuck clusters, aggregate them into an error.
	aggregateUpdateRunStatus(updateRun, updatingStageStatus.StageName, stuckClusterNames)

	// Aggregate and return errors.
	if len(clusterUpdateErrors) > 0 {
		// Even though we aggregate errors, we can still check if one of the errors is a staged update aborted error by using errors.Is in the caller.
		return false, 0, utilerrors.NewAggregate(clusterUpdateErrors)
	}

	if clusterUpdatingCount == 0 {
		// All the clusters in the stage have finished updating or not started.
		markStageUpdatingStopped(updatingStageStatus, updateRun.GetGeneration())
		klog.InfoS("The stage has finished all clusters updating", "stage", updatingStageStatus.StageName, "updateRun", updateRunRef)
		return true, 0, nil
	}
	// Some clusters are still updating.
	klog.InfoS("The updating stage is waiting for updating clusters to finish before completely stopping", "numberOfUpdatingClusters", clusterUpdatingCount, "stage", updatingStageStatus.StageName, "updateRun", updateRunRef)
	return false, clusterUpdatingWaitTime, nil
}

// stopDeleteStage stops the delete stage by letting the deleting bindings finish.
func (r *Reconciler) stopDeleteStage(
	updateRun placementv1beta1.UpdateRunObj,
	toBeDeletedBindings []placementv1beta1.BindingObj,
) (bool, error) {
	updateRunRef := klog.KObj(updateRun)
	updateRunStatus := updateRun.GetUpdateRunStatus()
	// Mark the delete stage as stopping in case it's not.
	markStageUpdatingStopping(updateRunStatus.DeletionStageStatus, updateRun.GetGeneration())
	// The clusters whose deletion has not started are left as they are.
	if _, err := syncDeleteStageClusterStatuses(updateRun, toBeDeletedBindings); err != nil {
		return false, err
	}

	klog.V(2).InfoS("The delete stage is stopping", "numberOfDeletingClusters", len(toBeDeletedBindings), "updateRun", updateRunRef)
	allDeletingClustersDeleted := true
	for _, clusterStatus := range updateRunStatus.DeletionStageStatus.Clusters {
		if condition.IsConditionStatusTrue(meta.FindStatusCondition(clusterStatus.Conditions,
			string(placementv1beta1.ClusterUpdatingConditionStarted)), updateRun.GetGeneration()) && !condition.IsConditionStatusTrue(
			meta.FindStatusCondition(clusterStatus.Conditions, string(placementv1beta1.ClusterUpdatingConditionSucceeded)),
			updateRun.GetGeneration()) {
			allDeletingClustersDeleted = false
			break
		}
	}

	if allDeletingClustersDeleted {
		markStageUpdatingStopped(updateRunStatus.DeletionStageStatus, updateRun.GetGeneration())
	}
	return allDeletingClustersDeleted, nil
}

// markUpdateRunStopping marks the update run as stopping in memory.
func markUpdateRunStopping(updateRun placementv1beta1.UpdateRunObj) {
	klog.V(2).InfoS("Marking the update run as stopping", "updateRun", klog.KObj(updateRun))
	updateRunStatus := updateRun.GetUpdateRunStatus()
	meta.SetStatusCondition(&updateRunStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionProgressing),
		Status:             metav1.ConditionUnknown,
		ObservedGeneration: updateRun.GetGeneration(),
		Reason:             condition.UpdateRunStoppingReason,
		Message:            "The update run is in the process of stopping, waiting for all the updating/deleting clusters to finish updating before completing the stop process",
	})
}

// markStageUpdatingStopping marks the stage updating status as stopping in memory.
func markStageUpdatingStopping(stageUpdatingStatus *placementv1beta1.StageUpdatingStatus, generation int64) {
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
		Status:             metav1.ConditionUnknown,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingStoppingReason,
		Message:            "Waiting for all the updating clusters to finish updating before completing the stop process",
	})
}

// markStageUpdatingStopped marks the stage updating status as stopped in memory.
func markStageUpdatingStopped(stageUpdatingStatus *placementv1beta1.StageUpdatingStatus, generation int64) {
	meta.SetStatusCondition(&stageUpdatingStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: generation,
		Reason:             condition.StageUpdatingStoppedReason,
		Message:            "All the updating clusters have finished updating, the stage is now stopped, waiting to be resumed",
	})
}

// checkIfErrorStagedUpdateAborted marks the updating stage, or the delete stage if there is no updating stage,
// as failed in memory if the error aborts the update run.
func checkIfErrorStagedUpdateAborted(err error, updateRun placementv1beta1.UpdateRunObj, updatingStageStatus *placementv1beta1.StageUpdatingStatus) {
	if errors.Is(err, errStagedUpdatedAborted) {
		if updatingStageStatus != nil {
			klog.InfoS("The update run is aborted due to unrecoverable behavior in updating stage, marking the stage as failed", "stage", updatingStageStatus.StageName, "updateRun", klog.KObj(updateRun))
			markStageUpdatingFailed(updatingStageStatus, updateRun.GetGeneration(), err.Error())
		} else {
			// Handle deletion stage case.
			updateRunStatus := updateRun.GetUpdateRunStatus()
			markStageUpdatingFailed(updateRunStatus.DeletionStageStatus, updateRun.GetGeneration(), err.Error())
		}
	}
}
