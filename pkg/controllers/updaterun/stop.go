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

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
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
		return r.stopUpdatingStage(updateRun, updatingStageIndex, toBeUpdatedBindings)
	}
	// All the stages have finished, stop the delete stage.
	finished, stopErr = r.stopDeleteStage(updateRun, toBeDeletedBindings)
	return finished, clusterUpdatingWaitTime, stopErr
}

// stopUpdatingStage stops the updating stage by letting the updating bindings finish and not starting new updates.
func (r *Reconciler) stopUpdatingStage(
	updateRun placementv1beta1.UpdateRunObj,
	updatingStageIndex int,
	toBeUpdatedBindings []placementv1beta1.BindingObj,
) (bool, time.Duration, error) {
	updateRunStatus := updateRun.GetUpdateRunStatus()
	updatingStageStatus := &updateRunStatus.StagesStatus[updatingStageIndex]
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

		binding := toBeUpdatedBindingsMap[clusterStatus.ClusterName]
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
				klog.V(2).InfoS("Time waiting for cluster update to finish passes threshold, mark the update run as stuck", "time elapsed", timeElapsed, "threshold", updateRunStuckThreshold, "cluster", clusterStatus.ClusterName, "stage", updatingStageStatus.StageName, "updateRun", updateRunRef)
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
	existingDeleteStageStatus := updateRunStatus.DeletionStageStatus
	existingDeleteStageClusterMap := make(map[string]*placementv1beta1.ClusterUpdatingStatus, len(existingDeleteStageStatus.Clusters))
	for i := range existingDeleteStageStatus.Clusters {
		existingDeleteStageClusterMap[existingDeleteStageStatus.Clusters[i].ClusterName] = &existingDeleteStageStatus.Clusters[i]
	}
	// Mark the delete stage as stopping in case it's not.
	markStageUpdatingStopping(existingDeleteStageStatus, updateRun.GetGeneration())

	for _, binding := range toBeDeletedBindings {
		bindingSpec := binding.GetBindingSpec()
		curCluster, exist := existingDeleteStageClusterMap[bindingSpec.TargetCluster]
		if !exist {
			// This is unexpected because we already checked in validation.
			missingErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the to be deleted cluster `%s` is not in the deleting stage during stopping", bindingSpec.TargetCluster))
			klog.ErrorS(missingErr, "The cluster in the deleting stage does not include all the to be deleted binding", "updateRun", updateRunRef)
			return false, fmt.Errorf("%w: %s", errStagedUpdatedAborted, missingErr.Error())
		}
		// In validation, we already check the binding must exist in the status.
		delete(existingDeleteStageClusterMap, bindingSpec.TargetCluster)
		// Make sure the cluster is not marked as deleted as the binding is still there.
		if condition.IsConditionStatusTrue(meta.FindStatusCondition(curCluster.Conditions, string(placementv1beta1.ClusterUpdatingConditionSucceeded)), updateRun.GetGeneration()) {
			// The cluster status is marked as deleted.
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
	}

	// The rest of the clusters in the stage are not in the toBeDeletedBindings so it should be marked as delete succeeded.
	for _, clusterStatus := range existingDeleteStageClusterMap {
		// Make sure the cluster is marked as deleted.
		if !condition.IsConditionStatusTrue(meta.FindStatusCondition(clusterStatus.Conditions, string(placementv1beta1.ClusterUpdatingConditionStarted)), updateRun.GetGeneration()) {
			markClusterUpdatingStarted(clusterStatus, updateRun.GetGeneration())
		}
		markClusterUpdatingSucceeded(clusterStatus, updateRun.GetGeneration())
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
	return len(toBeDeletedBindings) == 0, nil
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
		Message:            "The update run is the process of stopping, waiting for all the updating/deleting clusters to finish updating before completing the stop process",
	})
}

// markStageUpdatingStopping marks the stage updating status as pausing in memory.
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
