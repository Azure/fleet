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
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

const (
	maxAllowedClusterProcessingConcurrency = 10
)

type allStagesProcessingResult string

const (
	allStagesProcessingResultSucceeded  allStagesProcessingResult = "Succeeded"
	allStagesProcessingResultFailed     allStagesProcessingResult = "Failed"
	allStagesProcessingResultInProgress allStagesProcessingResult = "InProgress"
	allStagesProcessingResultErred      allStagesProcessingResult = "Erred"
)

type stageProcessingResult string

const (
	stageProcessingResultSucceeded  stageProcessingResult = "Succeeded"
	stageProcessingResultFailed     stageProcessingResult = "Failed"
	stageProcessingResultInProgress stageProcessingResult = "InProgress"
	stageProcessingResultErred      stageProcessingResult = "Erred"
)

type clusterProcessingResult string

const (
	clusterProcessingResultSucceeded             clusterProcessingResult = "Succeeded"
	clusterProcessingResultSucceededYetSuspended clusterProcessingResult = "SucceededYetSuspended"
	clusterProcessingResultFailed                clusterProcessingResult = "Failed"
	clusterProcessingResultErred                 clusterProcessingResult = "Erred"
)

func (r *Reconciler) shouldSkipProcessing(ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor) (bool, error) {
	// Skip processing if the staged update run is in a terminal state or suspended after being initialized.
	//
	// To retry the staged update, one needs to create a new staged update run object.

	// Skip processing if the initialization has failed.
	initCond := meta.FindStatusCondition(stagedUpdateRun.GetStatus().Conditions, rolloutv1alpha1.StagedUpdateRunCondTypeInitialized)
	if initCond != nil && initCond.Status == metav1.ConditionFalse {
		// Note that for this branch no observed generation check is needed. Staged update run objects have immutable spec (except
		// for the suspended switch).
		klog.V(2).InfoS("The staged update run has failed to initialize; skip further processing", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
		return true, r.resyncStagedUpdateRunCondIfNeeded(ctx, stagedUpdateRun, rolloutv1alpha1.StagedUpdateRunCondTypeInitialized)
	}

	// Skip processing if the staged update run has been completed (successfully or not).
	completedCond := meta.FindStatusCondition(stagedUpdateRun.GetStatus().Conditions, rolloutv1alpha1.StagedUpdateRunCondTypeCompleted)
	if completedCond != nil {
		// Note that for this branch no observed generation check is needed. Staged update run objects have immutable spec (except
		// for the suspended switch).
		klog.V(2).InfoS("The staged update run has been completed; skip further processing",
			"stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
		// For simplicity reasons, the controller will not resync the initialized condition in this case.
		return true, r.resyncStagedUpdateRunCondIfNeeded(ctx, stagedUpdateRun, rolloutv1alpha1.StagedUpdateRunCondTypeCompleted)
	}

	// Skip processing if the staged update run has been initialized but is suspended.
	if initCond != nil && initCond.Status == metav1.ConditionTrue && stagedUpdateRun.GetSpec().Suspended {
		klog.V(2).InfoS("The staged update run is suspended; skip further processing", "stagedUpdateRun", klog.KObj(stagedUpdateRun), "controller", controllerName)
		if err := r.markStagedUpdateRunAsSuspended(ctx, stagedUpdateRun); err != nil {
			return false, err
		}
		return true, r.resyncStagedUpdateRunCondIfNeeded(ctx, stagedUpdateRun, rolloutv1alpha1.StagedUpdateRunCondTypeInitialized)
	}

	// The processing should continue; if the staged update run was previously suspended, mark it as resumed.
	if err := r.markStagedUpdateRunAsResumedIfApplicable(ctx, stagedUpdateRun); err != nil {
		return false, err
	}
	return false, nil
}

func (r *Reconciler) process(ctx context.Context, stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor) (
	allStagesProcessingRes allStagesProcessingResult, requeueAfter *time.Duration, err error) {
	// Verify if the staged update run has already been completed.
	completedCond := meta.FindStatusCondition(stagedUpdateRun.GetStatus().Conditions, rolloutv1alpha1.StagedUpdateRunCondTypeCompleted)
	switch {
	case completedCond == nil || completedCond.Status != metav1.ConditionTrue:
		// The staged update run has not yet completed; continue processing.
	case completedCond.Reason == rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded:
		// The staged update run has completed successfully; no further processing is needed.
		return allStagesProcessingResultSucceeded, nil, nil
	case completedCond.Reason == rolloutv1alpha1.StagedUpdateRunCompletedCondReasonPartiallySucceeded:
		// The staged update run has completed partially successfully; no further processing is needed.
		return allStagesProcessingResultSucceeded, nil, nil
	case completedCond.Reason == rolloutv1alpha1.StagedUpdateRunCompletedCondReasonFailed:
		// The staged update run has completed with failure; no further processing is needed.
		return allStagesProcessingResultFailed, nil, nil
	default:
		// The staged update run has completed with an unknown reason; report an unexpected error. Normally this
		// should never occur.
		return allStagesProcessingResultErred, nil, kferrors.NewUnexpectedError(nil, "staged update run completed with unknown reason", "observedCompletionReason", completedCond.Reason)
	}

	// Check if the staged update run has been marked as started; if not, add the Started condition.
	stagedUpdateRunStatus := stagedUpdateRun.GetStatus()
	startedCond := meta.FindStatusCondition(stagedUpdateRunStatus.Conditions, rolloutv1alpha1.StagedUpdateRunCondTypeStarted)
	if startedCond == nil || startedCond.ObservedGeneration != stagedUpdateRun.GetGeneration() {
		meta.SetStatusCondition(&stagedUpdateRunStatus.Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunCondTypeStarted,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.StagedUpdateRunStartedCondReasonUpdateStarted,
			Message:            "The staged update run has started",
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
	}

	// Start a child context.
	//
	// This context is set to time out after a specific period of time, and terminates the processing of the staged update
	// run, in order to:
	//
	// a) avoid one staged update run monopolizing the controller's processing capacity for too long; and
	// b) allow users to see timely updates on the staged update run's progress.
	childCtx, childCancel := context.WithTimeout(ctx, r.refreshPeriod)
	defer childCancel()

	// Prepare the failure policy.
	maxAllowedClusterFailures := int32(1)
	maxWaitTimePerCluster := 5 * time.Minute

	stagedUpdateRunSpec := stagedUpdateRun.GetSpec()
	if stagedUpdateRunSpec.FailurePolicy != nil {
		if stagedUpdateRunSpec.FailurePolicy.MaxAllowedClusterFailures != nil {
			maxAllowedClusterFailures = *stagedUpdateRunSpec.FailurePolicy.MaxAllowedClusterFailures
		}
		if stagedUpdateRunSpec.FailurePolicy.MaxWaitTimePerClusterMinutes > 0 {
			maxWaitTimePerCluster = time.Duration(stagedUpdateRunSpec.FailurePolicy.MaxWaitTimePerClusterMinutes) * time.Minute
		}
	}

	// Prepare the cluster update failure counter (shared across all stages).
	clusterUpdateTotalFailures := atomic.Int32{}
	clusterUpdateTotalFailures.Store(0)

	// Scan the stages and process them one by one.
	stages := stagedUpdateRun.GetStatus().Stages
	for idx := range stages {
		stage := &stages[idx]

		stageProcessingRes, requeueAfter, err := r.processOneStage(childCtx,
			stagedUpdateRun, stage, &clusterUpdateTotalFailures, maxAllowedClusterFailures, maxWaitTimePerCluster)
		if err != nil {
			if errors.Is(childCtx.Err(), context.DeadlineExceeded) {
				// The per-run processing time budget has been exhausted; this is expected (not a real
				// failure), so retry in the next reconciliation loop instead of reporting an error.

				// There might be another error that occurred independently; log the errors for reference.
				klog.V(2).InfoS("Per-run processing time budget exhausted",
					"stagedUpdateRun", klog.KObj(stagedUpdateRun), "stage", stage.StageName,
					"error", err)
				return allStagesProcessingResultInProgress, requeueAfter, nil
			}
			return allStagesProcessingResultErred, requeueAfter, err
		}

		switch stageProcessingRes {
		case stageProcessingResultSucceeded:
			// The stage has been successfully processed; move on to the next stage.
		case stageProcessingResultFailed:
			// The stage has failed; stop processing further stages.
			stagedUpdateRunStatus := stagedUpdateRun.GetStatus()
			meta.SetStatusCondition(&stagedUpdateRunStatus.Conditions, metav1.Condition{
				Type:               rolloutv1alpha1.StagedUpdateRunCondTypeCompleted,
				Status:             metav1.ConditionTrue,
				Reason:             rolloutv1alpha1.StagedUpdateRunCompletedCondReasonFailed,
				Message:            fmt.Sprintf("The staged update run has failed (stage %q has failed)", stage.StageName),
				ObservedGeneration: stagedUpdateRun.GetGeneration(),
			})
			return allStagesProcessingResultFailed, requeueAfter, nil
		case stageProcessingResultInProgress:
			// The stage is still being processed in progress; retry in the next reconciliation loop.
			return allStagesProcessingResultInProgress, requeueAfter, nil
		default:
			// The stage failed processing for an unknown reason; report an unexpected error. Normally this
			// should never occur.
			return allStagesProcessingResultErred, nil, kferrors.NewUnexpectedError(nil, "stage processing yields an unexpected result",
				"observedProcessingResult", stageProcessingRes)
		}
	}

	// All stages have completed successfully; mark the staged update run as completed (fully or partially).
	if clusterUpdateTotalFailures.Load() > 0 {
		meta.SetStatusCondition(&stagedUpdateRunStatus.Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunCondTypeCompleted,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.StagedUpdateRunCompletedCondReasonPartiallySucceeded,
			Message:            fmt.Sprintf("The staged update run has completed with %d failed clusters", clusterUpdateTotalFailures.Load()),
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
	} else {
		meta.SetStatusCondition(&stagedUpdateRunStatus.Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunCondTypeCompleted,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded,
			Message:            "The staged update run has completed successfully",
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
	}
	return allStagesProcessingResultSucceeded, nil, nil
}

func (r *Reconciler) processOneStage(
	ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	stage *rolloutv1alpha1.PerStageStatus,
	clusterUpdateTotalFailures *atomic.Int32,
	maxAllowedClusterFailures int32,
	maxWaitTimePerCluster time.Duration,
) (result stageProcessingResult, requeueAfter *time.Duration, err error) {
	// Skip the stage if it has already been completed.
	stageCompletedCond := meta.FindStatusCondition(stage.Conditions, rolloutv1alpha1.StagedUpdateRunPerStageCondTypeCompleted)
	switch {
	case stageCompletedCond == nil || stageCompletedCond.Status != metav1.ConditionTrue:
		// Move on to process this stage.
	case stageCompletedCond.Reason == rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonSucceeded:
		// The stage has already been completed successfully, so skip further processing.
		resyncPerStageCondWithNoCommit(stagedUpdateRun, stage, rolloutv1alpha1.StagedUpdateRunPerStageCondTypeCompleted)
		return stageProcessingResultSucceeded, nil, nil
	case stageCompletedCond.Reason == rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonFailed:
		// The stage has already been completed and failed, so skip further processing.
		resyncPerStageCondWithNoCommit(stagedUpdateRun, stage, rolloutv1alpha1.StagedUpdateRunPerStageCondTypeCompleted)
		return stageProcessingResultFailed, nil, nil
	case stageCompletedCond.Reason == rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonPartiallySucceeded:
		// The stage has already been completed with a partial success. Count the cluster update failures.
		resyncPerStageCondWithNoCommit(stagedUpdateRun, stage, rolloutv1alpha1.StagedUpdateRunPerStageCondTypeCompleted)
		if err := countClusterUpdateFailures(stage, clusterUpdateTotalFailures); err != nil {
			return stageProcessingResultErred, nil, kferrors.Wraps(err, "failed to count cluster update failures for partially succeeded stage")
		}
		return stageProcessingResultSucceeded, nil, nil
	}

	// Set the stage started timestamp.
	if stage.StartedTimestamp == nil {
		now := metav1.Now()
		stage.StartedTimestamp = &now
	}

	// Mark the stage as started.
	startedCond := meta.FindStatusCondition(stage.Conditions, rolloutv1alpha1.StagedUpdateRunPerStageCondTypeStarted)
	if startedCond == nil || startedCond.ObservedGeneration != stagedUpdateRun.GetGeneration() {
		meta.SetStatusCondition(&stage.Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunPerStageCondTypeStarted,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.StagedUpdateRunPerStageStartedCondReasonUpdateStarted,
			Message:            "Updating has started in the stage",
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
	}

	// Execute the before stage tasks.
	beforeStageTasksAllCleared, beforeStageTasksRequeueAfter, err := r.executeBeforeStageTasks(ctx, stagedUpdateRun, stage)
	if err != nil {
		return stageProcessingResultErred, beforeStageTasksRequeueAfter, err
	}
	if !beforeStageTasksAllCleared {
		return stageProcessingResultInProgress, beforeStageTasksRequeueAfter, nil
	}

	// Process updates to all clusters in the stage per given concurrency setting.
	allowedClusterProcessingConcurrency := 1
	if stage.MaxConcurrency != nil && *stage.MaxConcurrency > 0 {
		allowedClusterProcessingConcurrency = int(*stage.MaxConcurrency)
	}
	if allowedClusterProcessingConcurrency > maxAllowedClusterProcessingConcurrency {
		allowedClusterProcessingConcurrency = maxAllowedClusterProcessingConcurrency
	}

	var succeededClusterCnt, failedClusterCnt, erredClusterCnt = atomic.Int32{}, atomic.Int32{}, atomic.Int32{}

	// Spin up a child context.
	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	wg := &sync.WaitGroup{}
	nextClusterIdx := atomic.Int32{}
	clusterCnt := int32(len(stage.Clusters))
	errs := make([]error, clusterCnt)
	for workerIdx := 0; workerIdx < allowedClusterProcessingConcurrency; workerIdx++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				clusterIdx := nextClusterIdx.Add(1) - 1
				if clusterIdx >= clusterCnt {
					klog.V(2).InfoS("No more clusters to process in the stage", "workerIdx", workerIdx,
						"stage", stage.StageName, "stagedUpdateRun", klog.KObj(stagedUpdateRun))
					return
				}
				klog.V(2).InfoS("Processing cluster in the stage", "workerIdx", workerIdx,
					"clusterIdx", clusterIdx, "stage", stage.StageName, "stagedUpdateRun", klog.KObj(stagedUpdateRun))

				cluster := &stage.Clusters[clusterIdx]
				clusterProcessingRes, err := r.refreshOneCluster(childCtx, cluster, stagedUpdateRun, maxWaitTimePerCluster)
				if err != nil {
					klog.ErrorS(err, "Failed to update a cluster in the stage",
						append(kferrors.Args(err), "stagedUpdateRun", klog.KObj(stagedUpdateRun), "stage", stage.StageName, "cluster", cluster.ClusterName)...)
					errs[clusterIdx] = err
				}

				switch clusterProcessingRes {
				case clusterProcessingResultSucceeded:
					// The processing has completed successfully; the worker can move on to processing the next cluster.
					succeededClusterCnt.Add(1)
				case clusterProcessingResultFailed:
					failedClusterCnt.Add(1)
					totalFailures := clusterUpdateTotalFailures.Add(1)
					if totalFailures > maxAllowedClusterFailures {
						// The failure threshold has been exceeded. Cancel all ongoing cluster processing attempts.
						childCancel()
					}
					// The worker moves on to processing the next cluster.
				case clusterProcessingResultErred:
					// An error occurred while processing the cluster. It does not count as a cluster failure, however,
					// the worker will stop processing clusters.
					erredClusterCnt.Add(1)
					return
				}
			}
		}()
	}
	wg.Wait()

	// Tally the results.
	klog.InfoS("Processed all clusters in the stage",
		"stagedUpdateRun", klog.KObj(stagedUpdateRun),
		"stage", stage.StageName,
		"succeededClusterCnt", succeededClusterCnt.Load(),
		"failedClusterCnt", failedClusterCnt.Load(),
		"erredClusterCnt", erredClusterCnt.Load(),
	)
	switch {
	case clusterUpdateTotalFailures.Load() > maxAllowedClusterFailures:
		// The failure threshold has been exceeded. The stage has failed.
		meta.SetStatusCondition(&stage.Conditions, metav1.Condition{
			Type:   rolloutv1alpha1.StagedUpdateRunPerStageCondTypeCompleted,
			Status: metav1.ConditionTrue,
			Reason: rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonFailed,
			Message: fmt.Sprintf("The stage has failed (%d out of %d clusters failed to update; total failures across stages have exceeded the threshold)",
				failedClusterCnt.Load(), len(stage.Clusters)),
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
		stage.CompletedTimestamp = ptr.To(metav1.Now())
		return stageProcessingResultFailed, nil, nil
	case succeededClusterCnt.Load() == int32(len(stage.Clusters)):
		// All clusters in the stage have succeeded. Move on to the execution of the after stage tasks.
	case succeededClusterCnt.Load()+failedClusterCnt.Load() == int32(len(stage.Clusters)):
		// Some clusters have failed, but the failure threshold has not been exceeded. Move on to the execution
		// of the after stage tasks.
	default:
		// Not all clusters have been successfully updated, but the failure threshold has not been exceeded either.
		//
		// Requeue and continue processing in the next reconciliation loop.
		if aggregatedErrs := utilerrors.NewAggregate(errs); aggregatedErrs != nil {
			return stageProcessingResultErred, nil, kferrors.NewTransientError(nil, "failed to update some clusters", "errs", aggregatedErrs)
		}
		return stageProcessingResultInProgress, nil, nil
	}

	// Execute the after stage tasks.
	afterStageTasksAllCleared, afterStageTasksRequeueAfter, err := r.executeAfterStageTasks(ctx, stagedUpdateRun, stage)
	if err != nil {
		return stageProcessingResultErred, afterStageTasksRequeueAfter, err
	}
	if !afterStageTasksAllCleared {
		return stageProcessingResultInProgress, afterStageTasksRequeueAfter, nil
	}

	// Mark the stage as completed (fully or partially).
	if failedClusterCnt.Load() > 0 {
		meta.SetStatusCondition(&stage.Conditions, metav1.Condition{
			Type:   rolloutv1alpha1.StagedUpdateRunPerStageCondTypeCompleted,
			Status: metav1.ConditionTrue,
			Reason: rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonPartiallySucceeded,
			Message: fmt.Sprintf("The stage has completed with partial success (%d out of %d clusters failed to update)",
				failedClusterCnt.Load(), len(stage.Clusters)),
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
	} else {
		meta.SetStatusCondition(&stage.Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunPerStageCondTypeCompleted,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonSucceeded,
			Message:            "The stage has completed successfully",
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
	}
	stage.CompletedTimestamp = ptr.To(metav1.Now())
	return stageProcessingResultSucceeded, nil, nil
}

func countClusterUpdateFailures(stage *rolloutv1alpha1.PerStageStatus, clusterUpdateTotalFailures *atomic.Int32) error {
	for idx := range stage.Clusters {
		perClusterStatus := &stage.Clusters[idx]

		completedCond := meta.FindStatusCondition(perClusterStatus.Conditions, rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted)
		switch {
		case completedCond == nil || completedCond.Status != metav1.ConditionTrue:
			// The cluster has not completed its update, yet the stage has been marked as completed. Report
			// an unexpected error.
			return kferrors.NewUnexpectedError(nil, "a cluster has not completed its update, yet the stage has been marked as completed",
				"clusterName", perClusterStatus.ClusterName)
		case completedCond.Reason == rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed:
			// Register a cluster update failure.
			clusterUpdateTotalFailures.Add(1)
		default:
			// The cluster has completed its update successfully.
		}
	}
	return nil
}

func (r *Reconciler) refreshOneCluster(
	ctx context.Context,
	cluster *rolloutv1alpha1.PerClusterStatus,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	maxWaitTimePerCluster time.Duration,
) (clusterProcessingResult, error) {
	// Check if an update attempt has been completed before for the cluster.
	completedCond := meta.FindStatusCondition(cluster.Conditions, rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted)
	switch {
	case completedCond == nil || completedCond.Status != metav1.ConditionTrue:
		// There is no update attempt before or it has not been completed yet. Continue with the update process.
	case completedCond.Reason == rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded:
		// The update attempt has been completed successfully before.
		resyncPerClusterCondWithNoCommit(stagedUpdateRun, cluster, rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted)
		return clusterProcessingResultSucceeded, nil
	case completedCond.Reason == rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed:
		// The update attempt has failed before.
		resyncPerClusterCondWithNoCommit(stagedUpdateRun, cluster, rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted)
		return clusterProcessingResultFailed, nil
	default:
		// Found an unexpected completion reason. Consider this as an unexpected error.
		return clusterProcessingResultErred, kferrors.NewUnexpectedError(nil, "failed to determine the update status for a cluster", "unexpectedCompletionReason", completedCond.Reason)
	}

	maxWaitTime := maxWaitTimePerCluster

	// Note that at this position of the code, it is guaranteed that the staged update run has either never been
	// suspended or has been resumed.
	resumedCond := meta.FindStatusCondition(stagedUpdateRun.GetStatus().Conditions, rolloutv1alpha1.StagedUpdateRunCondTypeSuspended)

	// Check if an update attempt has started already for the cluster.
	startedCond := meta.FindStatusCondition(cluster.Conditions, rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeStarted)
	resyncPerClusterCondWithNoCommit(stagedUpdateRun, cluster, rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeStarted)
	if startedCond != nil && !clusterUpdateStartedBeforeRunResumption(startedCond, resumedCond) {
		// Check if the max wait time has elapsed.
		//
		// It is possible that the cluster has been updated successfully before this specific check is run (and
		// it could happen still within the time limit, but no one is checking at the time); here the controller
		// takes a more reserved stance and would fail a cluster as long as there is no clear signal found within
		// the wait time.
		//
		// Note that if the cluster update started before run is suspended, we reset the per cluster wait time.
		if elapsed := time.Since(startedCond.LastTransitionTime.Time); elapsed > maxWaitTime {
			markClusterUpdateAsFailedDueToTimeout(stagedUpdateRun, cluster)
			return clusterProcessingResultFailed, nil
		}
	}

	// Retrieve the placement binding.
	var binding placementv1alpha1.PlacementBindingAccessor
	bindingKey := types.NamespacedName{
		Namespace: stagedUpdateRun.GetNamespace(),
		Name:      cluster.PlacementBindingName,
	}
	if bindingKey.Namespace == "" {
		binding = &placementv1alpha1.ClusterPlacementBinding{}
	} else {
		binding = &placementv1alpha1.PlacementBinding{}
	}
	if err := r.hubClient.Get(ctx, bindingKey, binding); err != nil {
		if apierrors.IsNotFound(err) {
			// The placement binding cannot be found. Consider the update on the cluster as failed.
			//
			// Normally this would not occur as the controller has acquired the role of binding manager for
			// the placement policy.
			meta.SetStatusCondition(&cluster.Conditions, metav1.Condition{
				Type:               rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted,
				Status:             metav1.ConditionTrue,
				Reason:             rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed,
				Message:            "Failed to update the cluster as the binding cannot be found",
				ObservedGeneration: stagedUpdateRun.GetGeneration(),
			})
			wrappedErr := kferrors.NewUnexpectedError(err, "", "placementBinding", klog.KRef(bindingKey.Namespace, bindingKey.Name))
			klog.ErrorS(wrappedErr,
				"Failed to retrieve a placement binding for an initialized staged update run",
				append(kferrors.Args(wrappedErr), "stagedUpdateRun", klog.KObj(stagedUpdateRun))...)
			return clusterProcessingResultFailed, nil
		}
		return clusterProcessingResultErred, kferrors.NewAPIServerError(err, "failed to get the placement binding", true,
			"placementBinding", klog.KRef(bindingKey.Namespace, bindingKey.Name))
	}

	// Verify that the binding has not yet been marked for deletion and the cluster name matches, as a sanity check.
	//
	// Normally these branches should never run: the binding is only removed, and its target cluster only
	// changed, by re-running initialization, which also refreshes this very status.
	if !binding.GetDeletionTimestamp().IsZero() || binding.GetSpec().ClusterName != cluster.ClusterName {
		// Mark the cluster as failed to update.
		meta.SetStatusCondition(&cluster.Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed,
			Message:            "Failed to update the cluster as the binding has been marked for deletion or its cluster name does not match the tracked cluster name",
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
		wrappedErr := kferrors.NewUnexpectedError(nil,
			"the placement binding has been marked for deletion or its cluster name does not match the tracked cluster name",
			"placementBinding", klog.KObj(binding),
			"placementBindingDeletionTimestamp", binding.GetDeletionTimestamp(),
			"expectedClusterName", cluster.ClusterName, "observedClusterName", binding.GetSpec().ClusterName)
		klog.ErrorS(wrappedErr, "Failed to update the cluster due to placement binding issue", kferrors.Args(wrappedErr)...)
		return clusterProcessingResultFailed, nil
	}

	// Update the binding to use the expected resource snapshot name.
	//
	// Note (chenyu1): for simplicity reasons, here the control loop does not implement the rollback support
	// properly (i.e., no forward only constraint), and concurrent staged update runs are implemented in a
	// last writer wins manner (most likely).
	wantResourceSnapshotName := stagedUpdateRun.GetStatus().ResourceSnapshotNameToRollout
	if binding.GetSpec().ResourceSnapshotName != wantResourceSnapshotName {
		if startedCond != nil {
			// The resource snapshot name is updated after this specific staged update run starts updating the
			// cluster. There might be another staged update run processing the same cluster at the same time;
			// consider this as a failure.
			//
			// There exists a corner case where the controller here is simply reading a stale version of the binding.
			// Do a quorum read first to verify if the conflict does exist.
			snapshotNameChanged, latestBinding, err := r.placementBindingResourceSnapshotChanged(ctx, binding, wantResourceSnapshotName)
			if err != nil {
				return clusterProcessingResultErred, err
			}
			if snapshotNameChanged {
				markClusterUpdateAsFailedDueToConflict(stagedUpdateRun, cluster)
				klog.V(2).InfoS("Failed to update cluster due to resource snapshot name conflict",
					"placementBinding", klog.KObj(latestBinding), "cluster", cluster.ClusterName, "stagedUpdateRun", klog.KObj(stagedUpdateRun))
				return clusterProcessingResultFailed, nil
			}
			// The cached binding was stale; use the latest one from the API server.
			binding = latestBinding
		} else {
			binding.GetSpec().ResourceSnapshotName = wantResourceSnapshotName
			if err := r.hubClient.Update(ctx, binding); err != nil {
				return clusterProcessingResultErred, kferrors.NewAPIServerError(err,
					"failed to update the placement binding with the resource snapshot to roll out", false,
					"placementBinding", klog.KObj(binding), "resourceSnapshotName", wantResourceSnapshotName)
			}
		}
	}

	// Mark the cluster as started updating. Reset the condition (specifically the start time) if the cluster
	// started updating before the run was suspended.
	if startedCond == nil ||
		clusterUpdateStartedBeforeRunResumption(startedCond, resumedCond) {
		meta.RemoveStatusCondition(&cluster.Conditions, rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeStarted)
		meta.SetStatusCondition(&cluster.Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeStarted,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.StagedUpdateRunPerClusterStartedCondReasonUpdateStarted,
			Message:            "The update on the cluster has started",
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
		// Read the condition back so that its (assigned) LastTransitionTime is available for the
		// wait-time check in the polling loop below.
		startedCond = meta.FindStatusCondition(cluster.Conditions, rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeStarted)
	}

	// As a shortcut, if the binding has been suspended, there is no need to wait. Consider it updated right away.
	if binding.GetSpec().Suspended {
		klog.V(2).InfoS("Successfully updated cluster (binding suspended)",
			"cluster", cluster.ClusterName, "stagedUpdateRun", klog.KObj(stagedUpdateRun))
		meta.SetStatusCondition(&cluster.Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded,
			Message:            "The update on the cluster has completed successfully (binding has been suspended)",
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
		return clusterProcessingResultSucceeded, nil
	}

	// Capture the generation the binding is expected to have been bumped to once the resource snapshot
	// update above (if any) has been applied. This is used, rather than re-reading the generation off of
	// each poll below, because the cached client used by the polling loop may for a short while still
	// return the pre-update version of the binding (same generation and conditions together, both stale);
	// comparing a freshly re-fetched condition's ObservedGeneration against an equally stale generation
	// would otherwise spuriously look up-to-date.
	wantBindingGeneration := binding.GetGeneration()

	// Wait for the placement binding to report that the rollout has been synchronized to, and is available
	// on, the target cluster, polling periodically until either that happens or the context is cancelled
	// (e.g., the per-run processing time budget has been exhausted, in which case the caller will pick up
	// where this left off on the next reconciliation pass).
	timer := time.NewTimer(r.bindingAvailabilityCheckRateLimiter.When(bindingKey))
	defer timer.Stop()
	// Reset the availability check rate limiter for this binding after the refreshing completes.
	defer r.bindingAvailabilityCheckRateLimiter.Forget(bindingKey)
	clusterProcessingRes, err := r.waitForOneClusterToBeUpdatedAndAvailable(ctx, stagedUpdateRun, cluster,
		bindingKey, binding, wantResourceSnapshotName, wantBindingGeneration,
		startedCond.LastTransitionTime.Time, maxWaitTime, timer)
	switch {
	case err != nil || clusterProcessingRes == clusterProcessingResultErred:
		return clusterProcessingResultErred, kferrors.Wraps(err, "failed to wait for cluster to be updated and available",
			"placementBinding", klog.KObj(binding))
	case clusterProcessingRes == clusterProcessingResultFailed:
		return clusterProcessingResultFailed, nil
	case clusterProcessingRes == clusterProcessingResultSucceededYetSuspended:
		meta.SetStatusCondition(&cluster.Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded,
			Message:            "The update on the cluster has completed successfully (binding is suspended)",
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
	default: // clusterProcessingRes == clusterProcessingResultSucceeded
		meta.SetStatusCondition(&cluster.Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded,
			Message:            "The update on the cluster has completed successfully",
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
	}

	klog.V(2).InfoS("Successfully updated cluster",
		"cluster", cluster.ClusterName, "stagedUpdateRun", klog.KObj(stagedUpdateRun))
	return clusterProcessingResultSucceeded, nil
}

func clusterUpdateStartedBeforeRunResumption(startedCond, resumedCond *metav1.Condition) bool {
	if startedCond == nil || resumedCond == nil {
		return false
	}

	if startedCond.LastTransitionTime.Before(&resumedCond.LastTransitionTime) {
		return true
	}
	return false
}

func (r *Reconciler) waitForOneClusterToBeUpdatedAndAvailable(ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	cluster *rolloutv1alpha1.PerClusterStatus,
	bindingKey types.NamespacedName,
	binding placementv1alpha1.PlacementBindingAccessor,
	wantResourceSnapshotName string,
	wantBindingGeneration int64,
	updateStartedAt time.Time,
	maxWaitTime time.Duration,
	timer *time.Timer,
) (res clusterProcessingResult, err error) {
	for {
		select {
		case <-ctx.Done():
			return clusterProcessingResultErred, kferrors.NewTransientError(ctx.Err(),
				"the placement binding has not yet become synchronized and available", "placementBinding", klog.KObj(binding))
		case <-timer.C:
			// Time to check again; continue the loop.
		}

		// Check also on every loop run that the max wait time is still respected; the cluster might take
		// too long to become synchronized and available even though it did not time out earlier.
		//
		// Similarly, it is possible that the cluster has become synchronized and available just
		// before the max wait time is reached, yet the controller just wasn't able to observe it in time;
		// in this case, the controller takes a more reserved stance and would fail a cluster as long as there is
		// no clear signal found within the wait time.
		if elapsed := time.Since(updateStartedAt); elapsed > maxWaitTime {
			markClusterUpdateAsFailedDueToTimeout(stagedUpdateRun, cluster)
			klog.V(2).InfoS("Failed to update cluster as the max wait time has elapsed",
				"placementBinding", klog.KObj(binding), "cluster", cluster.ClusterName, "stagedUpdateRun", klog.KObj(stagedUpdateRun))
			return clusterProcessingResultFailed, nil
		}

		// Re-fetch the binding at the start of every iteration so that the check below always runs
		// against the latest observed state; without this, the loop would keep re-checking the same
		// (initial) snapshot of the binding and never notice it becoming synchronized and available.
		if err := r.hubClient.Get(ctx, bindingKey, binding); err != nil {
			return clusterProcessingResultErred, kferrors.NewAPIServerError(err, "failed to get the placement binding", true,
				"placementBinding", klog.KRef(bindingKey.Namespace, bindingKey.Name))
		}

		if binding.GetGeneration() < wantBindingGeneration {
			// The binding in cache is stale; check again later.
			timer.Reset(r.bindingAvailabilityCheckRateLimiter.When(bindingKey))
			continue
		}

		if binding.GetSpec().ResourceSnapshotName != wantResourceSnapshotName {
			// The resource snapshot name has been modified after the staged update run started processing the cluster;
			// mark the cluster as failed due to conflicts.
			//
			// Note that this is a best-effort check; KubeFleet at this moment does not block concurrent
			// staged update runs: when multiple runs target the same cluster (binding) at the same time,
			// either the last writer succeeded while the others failed, or they all failed due to timeouts (though
			// unlikely).
			markClusterUpdateAsFailedDueToConflict(stagedUpdateRun, cluster)
			klog.V(2).InfoS("Failed to update cluster due to resource snapshot name conflict",
				"placementBinding", klog.KObj(binding), "cluster", cluster.ClusterName, "stagedUpdateRun", klog.KObj(stagedUpdateRun))
			return clusterProcessingResultFailed, nil
		}

		if binding.GetSpec().Suspended {
			// The binding is suspended; consider the update succeeded yet suspended.
			klog.V(2).InfoS("The placement binding is suspended; considering the update succeeded yet suspended",
				"placementBinding", klog.KObj(binding), "cluster", cluster.ClusterName, "stagedUpdateRun", klog.KObj(stagedUpdateRun))
			return clusterProcessingResultSucceededYetSuspended, nil
		}

		// Note again that the controller here does a best-effort check only; for performance reasons, the check
		// always reads using the cached client, and as result we might observe stale data. This is alright
		// as concurrent staged update runs follow a last-writer-wins approach and the binding manager lock promises
		// that no other changes would be made to the binding in normal ops.
		bindingStatus := binding.GetStatus()
		synchronizedCond := meta.FindStatusCondition(bindingStatus.Conditions, placementv1alpha1.PlacementBindingCondTypeSynchronized)
		availableCond := meta.FindStatusCondition(bindingStatus.Conditions, placementv1alpha1.PlacementBindingCondTypeAvailable)
		if condition.IsConditionStatusTrue(synchronizedCond, binding.GetGeneration()) &&
			condition.IsConditionStatusTrue(availableCond, binding.GetGeneration()) {
			// The rollout has been synchronized to, and is available on, the target cluster.
			return clusterProcessingResultSucceeded, nil
		}

		// Reset the timer.
		timer.Reset(r.bindingAvailabilityCheckRateLimiter.When(bindingKey))
	}
}
