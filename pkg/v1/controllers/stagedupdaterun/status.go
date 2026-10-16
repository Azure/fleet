/*
Copyright 2026 The KubeFleet Authors.

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

package stagedupdaterun

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

func (r *Reconciler) markStagedUpdateRunAsInTerminalState(
	ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	message string,
) error {
	initCond := meta.FindStatusCondition(stagedUpdateRun.GetStatus().Conditions, rolloutv1alpha1.StagedUpdateRunCondTypeInitialized)
	completedCond := meta.FindStatusCondition(stagedUpdateRun.GetStatus().Conditions, rolloutv1alpha1.StagedUpdateRunCondTypeCompleted)
	stagedUpdateRunStatus := stagedUpdateRun.GetStatus()

	switch {
	case initCond == nil:
		// Add the failed to initialize condition if the staged update run has not been initialized yet; this alone
		// puts the staged update run in a terminal state, so the completed condition is not needed.
		meta.SetStatusCondition(&stagedUpdateRunStatus.Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunCondTypeInitialized,
			Status:             metav1.ConditionFalse,
			Reason:             rolloutv1alpha1.StagedUpdateRunInitializedCondReasonFailed,
			Message:            message,
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
	case initCond.Status == metav1.ConditionFalse:
		// The staged update run has already failed to initialize; there is nothing to add.
	case completedCond == nil:
		// The staged update run has been initialized but has not completed yet; add the failed to complete condition.
		meta.SetStatusCondition(&stagedUpdateRunStatus.Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunCondTypeCompleted,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.StagedUpdateRunCompletedCondReasonFailed,
			Message:            message,
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
	default:
		// The staged update run has already failed to complete; there is nothing to add.
	}

	if err := r.hubClient.Status().Update(ctx, stagedUpdateRun); err != nil {
		return errors.NewAPIServerError(err, "failed to update the staged update run status", false)
	}
	return nil
}

func (r *Reconciler) markStagedUpdateRunAsFailedToInitialize(
	ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	message string,
) error {
	stagedUpdateRunStatus := stagedUpdateRun.GetStatus()

	meta.SetStatusCondition(&stagedUpdateRunStatus.Conditions, metav1.Condition{
		Type:               rolloutv1alpha1.StagedUpdateRunCondTypeInitialized,
		Status:             metav1.ConditionFalse,
		Reason:             rolloutv1alpha1.StagedUpdateRunInitializedCondReasonFailed,
		Message:            message,
		ObservedGeneration: stagedUpdateRun.GetGeneration(),
	})

	if err := r.hubClient.Status().Update(ctx, stagedUpdateRun); err != nil {
		return errors.NewAPIServerError(err, "failed to update the staged update run status", false)
	}
	return nil
}

func (r *Reconciler) syncStagedUpdateRunInitializedStatus(
	ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	perStageStatuses []rolloutv1alpha1.PerStageStatus,
	resourceSnapshotName string,
) error {
	stagedUpdateRunStatus := stagedUpdateRun.GetStatus()

	// Set the resource snapshot name to roll out, the prepped stages, and the initialized condition.
	stagedUpdateRunStatus.ResourceSnapshotNameToRollout = resourceSnapshotName
	stagedUpdateRunStatus.Stages = perStageStatuses

	meta.SetStatusCondition(&stagedUpdateRunStatus.Conditions, metav1.Condition{
		Type:   rolloutv1alpha1.StagedUpdateRunCondTypeInitialized,
		Status: metav1.ConditionTrue,
		Reason: rolloutv1alpha1.StagedUpdateRunInitializedCondReasonPreppedResourceSnapshotAndAllStages,
		Message: fmt.Sprintf("the staged update run has been initialized (resource snapshot to roll out: %s; prepped %d stages)",
			resourceSnapshotName, len(perStageStatuses)),
		ObservedGeneration: stagedUpdateRun.GetGeneration(),
	})

	if err := r.hubClient.Status().Update(ctx, stagedUpdateRun); err != nil {
		return errors.NewAPIServerError(err, "failed to update the staged update run status", false)
	}
	return nil
}

func (r *Reconciler) markStagedUpdateRunAsSuspended(
	ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
) error {
	stagedUpdateRunStatus := stagedUpdateRun.GetStatus()
	suspendedCond := meta.FindStatusCondition(stagedUpdateRunStatus.Conditions, rolloutv1alpha1.StagedUpdateRunCondTypeSuspended)
	if condition.IsConditionStatusTrue(suspendedCond, stagedUpdateRun.GetGeneration()) {
		// The staged update run is already marked as suspended; no further action is needed.
		return nil
	}

	meta.SetStatusCondition(&stagedUpdateRunStatus.Conditions, metav1.Condition{
		Type:               rolloutv1alpha1.StagedUpdateRunCondTypeSuspended,
		Status:             metav1.ConditionTrue,
		Reason:             rolloutv1alpha1.StagedUpdateRunSuspendedCondReasonSuspended,
		Message:            "The staged update run has been suspended",
		ObservedGeneration: stagedUpdateRun.GetGeneration(),
	})

	if err := r.hubClient.Status().Update(ctx, stagedUpdateRun); err != nil {
		return errors.NewAPIServerError(err, "failed to update the staged update run status", false)
	}
	return nil
}

func (r *Reconciler) markStagedUpdateRunAsResumedIfApplicable(
	ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
) error {
	stagedUpdateRunStatus := stagedUpdateRun.GetStatus()
	suspendedCond := meta.FindStatusCondition(stagedUpdateRunStatus.Conditions, rolloutv1alpha1.StagedUpdateRunCondTypeSuspended)
	if suspendedCond == nil {
		// No need to add this condition if the staged update run has never been suspended.
		return nil
	}
	if condition.IsConditionStatusFalse(suspendedCond, stagedUpdateRun.GetGeneration()) &&
		suspendedCond.Reason == rolloutv1alpha1.StagedUpdateRunSuspendedCondReasonResumed {
		// The staged update run is already marked as resumed; no further action is needed.
		return nil
	}

	meta.SetStatusCondition(&stagedUpdateRunStatus.Conditions, metav1.Condition{
		Type:               rolloutv1alpha1.StagedUpdateRunCondTypeSuspended,
		Status:             metav1.ConditionFalse,
		Reason:             rolloutv1alpha1.StagedUpdateRunSuspendedCondReasonResumed,
		Message:            "The staged update run has been resumed",
		ObservedGeneration: stagedUpdateRun.GetGeneration(),
	})

	if err := r.hubClient.Status().Update(ctx, stagedUpdateRun); err != nil {
		return errors.NewAPIServerError(err, "failed to update the staged update run status", false)
	}
	return nil
}

// resyncStagedUpdateRunCondIfNeeded refreshes the observed generation of the given condition (e.g., Initialized or
// Completed) if it has fallen behind the generation of the staged update run (e.g., after the run is suspended or resumed).
func (r *Reconciler) resyncStagedUpdateRunCondIfNeeded(
	ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	condType string,
) error {
	stagedUpdateRunStatus := stagedUpdateRun.GetStatus()
	cond := meta.FindStatusCondition(stagedUpdateRunStatus.Conditions, condType)
	if cond == nil || cond.ObservedGeneration == stagedUpdateRun.GetGeneration() {
		// The condition has not been set yet, or it is up to date.
		return nil
	}

	// Re-set the condition with only the observed generation changed; as the status stays the same, the last
	// transition time is preserved.
	updatedCond := *cond
	updatedCond.ObservedGeneration = stagedUpdateRun.GetGeneration()
	meta.SetStatusCondition(&stagedUpdateRunStatus.Conditions, updatedCond)

	if err := r.hubClient.Status().Update(ctx, stagedUpdateRun); err != nil {
		return errors.NewAPIServerError(err, "failed to resync a condition of the staged update run", false,
			"conditionType", condType)
	}
	return nil
}

func resyncPerStageCondWithNoCommit(
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	stage *rolloutv1alpha1.PerStageStatus,
	condType string,
) {
	cond := meta.FindStatusCondition(stage.Conditions, condType)
	if cond == nil || cond.ObservedGeneration == stagedUpdateRun.GetGeneration() {
		return
	}
	updatedCond := *cond
	updatedCond.ObservedGeneration = stagedUpdateRun.GetGeneration()
	meta.SetStatusCondition(&stage.Conditions, updatedCond)
}

func resyncPerClusterCondWithNoCommit(
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	cluster *rolloutv1alpha1.PerClusterStatus,
	condType string,
) {
	cond := meta.FindStatusCondition(cluster.Conditions, condType)
	if cond == nil || cond.ObservedGeneration == stagedUpdateRun.GetGeneration() {
		return
	}
	updatedCond := *cond
	updatedCond.ObservedGeneration = stagedUpdateRun.GetGeneration()
	meta.SetStatusCondition(&cluster.Conditions, updatedCond)
}

func markClusterUpdateAsFailedDueToTimeout(
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	cluster *rolloutv1alpha1.PerClusterStatus,
) {
	meta.SetStatusCondition(&cluster.Conditions, metav1.Condition{
		Type:               rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted,
		Status:             metav1.ConditionTrue,
		Reason:             rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed,
		Message:            "The update on the cluster has failed due to timeout",
		ObservedGeneration: stagedUpdateRun.GetGeneration(),
	})
}

func markClusterUpdateAsFailedDueToConflict(
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	cluster *rolloutv1alpha1.PerClusterStatus,
) {
	meta.SetStatusCondition(&cluster.Conditions, metav1.Condition{
		Type:               rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted,
		Status:             metav1.ConditionTrue,
		Reason:             rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed,
		Message:            "The update on the cluster has failed due to resource snapshot name conflict",
		ObservedGeneration: stagedUpdateRun.GetGeneration(),
	})
}
