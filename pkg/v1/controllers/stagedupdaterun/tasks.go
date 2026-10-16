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
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/ownerreferences"
)

// executeBeforeStageTasks executes all the before stage tasks of a stage. It reports whether all the tasks have
// been cleared; if not, the returned requeue period is the longest one requested by the tasks.
func (r *Reconciler) executeBeforeStageTasks(
	ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	stage *rolloutv1alpha1.PerStageStatus,
) (allCleared bool, requeueAfter *time.Duration, err error) {
	allCleared = true
	longestRequeueAfter := time.Duration(0)
	for idx := range stage.BeforeStageTasks {
		task := &stage.BeforeStageTasks[idx]
		shouldContinue, taskRequeueAfter, err := r.executeOneStageTask(ctx, stagedUpdateRun, stage, task, true)
		if err != nil {
			return false, taskRequeueAfter, kferrors.Wraps(err, "failed to complete before stage task", "task", task.Type)
		}
		if !shouldContinue {
			allCleared = false
			if taskRequeueAfter != nil && *taskRequeueAfter > longestRequeueAfter {
				longestRequeueAfter = *taskRequeueAfter
			}
		}
	}
	return allCleared, &longestRequeueAfter, nil
}

// executeAfterStageTasks executes all the after stage tasks of a stage. It reports whether all the tasks have
// been cleared; if not, the returned requeue period is the longest one requested by the tasks.
func (r *Reconciler) executeAfterStageTasks(
	ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	stage *rolloutv1alpha1.PerStageStatus,
) (allCleared bool, requeueAfter *time.Duration, err error) {
	allCleared = true
	longestRequeueAfter := time.Duration(0)
	for idx := range stage.AfterStageTasks {
		task := &stage.AfterStageTasks[idx]
		shouldContinue, taskRequeueAfter, err := r.executeOneStageTask(ctx, stagedUpdateRun, stage, task, false)
		if err != nil {
			return false, taskRequeueAfter, kferrors.Wraps(err, "failed to complete after stage task", "task", task.Type)
		}
		if !shouldContinue {
			allCleared = false
			if taskRequeueAfter != nil && *taskRequeueAfter > longestRequeueAfter {
				longestRequeueAfter = *taskRequeueAfter
			}
		}
	}
	return allCleared, &longestRequeueAfter, nil
}

func (r *Reconciler) executeOneStageTask(
	ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	stage *rolloutv1alpha1.PerStageStatus,
	task *rolloutv1alpha1.PerStageTaskStatus,
	isBeforeStageTask bool,
) (shouldContinue bool, requeueAfter *time.Duration, err error) {
	switch task.Type {
	case rolloutv1alpha1.StageTaskTypeTimedWait:
		return r.executeTimedWaitStageTask(stagedUpdateRun, task)
	case rolloutv1alpha1.StageTaskTypeApproval:
		return r.executeApprovalStageTask(ctx, stagedUpdateRun, stage, task, isBeforeStageTask)
	default:
		// Normally this will never happen.
		return false, nil, kferrors.NewUnexpectedError(nil, "an unknown stage task type was found", "taskType", task.Type)
	}
}

func (r *Reconciler) executeApprovalStageTask(
	ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	stage *rolloutv1alpha1.PerStageStatus,
	task *rolloutv1alpha1.PerStageTaskStatus,
	isBeforeStageTask bool,
) (shouldContinue bool, requeueAfter *time.Duration, err error) {
	// Skip the task if it has already been completed (the approval has been received).
	approvalAcceptedCond := meta.FindStatusCondition(task.Conditions, rolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestApproved)
	if approvalAcceptedCond != nil && approvalAcceptedCond.Status == metav1.ConditionTrue {
		return true, nil, nil
	}

	// Check if an approval request has been created. If not, create one, and add the ApprovalRequestCreated condition.
	requestCreatedCond := meta.FindStatusCondition(task.Conditions, rolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestCreated)
	if requestCreatedCond == nil {
		// Create an approval request.
		if err := r.createApprovalRequest(ctx, stagedUpdateRun, stage, task, isBeforeStageTask); err != nil {
			return false, nil, err
		}
		// No need to wait for the approval to be granted; the reconciliation loop will be triggered again
		// when the approval request is approved.
		return false, nil, nil
	}

	// The approval request has been created. Check if it has been approved. If not, wait for the next reconciliation attempt.
	approvalRequestName := task.ApprovalRequestName
	if approvalRequestName == "" {
		// Do a sanity check; normally this branch will never run.
		return false, nil, kferrors.NewUnexpectedError(nil, "approval request name is empty in the task status")
	}

	var approvalRequest rolloutv1alpha1.ApprovalRequestAccessor
	var approvalRequestNamespacedName types.NamespacedName
	if stagedUpdateRun.GetNamespace() == "" {
		approvalRequest = &rolloutv1alpha1.ClusterApprovalRequest{}
		approvalRequestNamespacedName = types.NamespacedName{Name: approvalRequestName}
	} else {
		approvalRequest = &rolloutv1alpha1.ApprovalRequest{}
		approvalRequestNamespacedName = types.NamespacedName{Namespace: stagedUpdateRun.GetNamespace(), Name: approvalRequestName}
	}

	if err := r.hubClient.Get(ctx, approvalRequestNamespacedName, approvalRequest); err != nil {
		if apierrors.IsNotFound(err) {
			// The approval request cannot be found. It might have been deleted by the user; re-create it.
			if err := r.createApprovalRequest(ctx, stagedUpdateRun, stage, task, isBeforeStageTask); err != nil {
				return false, nil, err
			}
			// No need to wait for the approval to be granted; the reconciliation loop will be triggered again
			// when the approval request is approved.
			return false, nil, nil
		}
		return false, nil, kferrors.NewAPIServerError(err, "failed to get approval request", true,
			"approvalRequest", approvalRequestName)
	}

	// Check if the approval request is owned by the staged update run. If not, delete it and return an error.
	//
	// This is primarily added as a safeguard against situations where staged update runs are re-created yet
	// the GC process hasn't caught up and old approval requests from same-named staged update runs might still exist.
	//
	// Note that approval requests are typically considered to be objects always created by the staged update run
	// controller; users are not expected to create or manage them directly.
	if !metav1.IsControlledBy(approvalRequest, stagedUpdateRun) {
		isOwnedBySameNamed, err := ownerreferences.IsOwnedByWithNoUIDCheck(r.hubClient, approvalRequest, stagedUpdateRun)
		if err != nil {
			return false, nil, kferrors.Wraps(err, "failed to check ownership of approval request",
				"approvalRequest", klog.KObj(approvalRequest))
		}
		if isOwnedBySameNamed {
			// The approval request is owned by a same-named staged update run but not the current one; it should be
			// deleted so that it can be re-created.
			klog.V(2).InfoS("Approval request is not owned by the staged update run; deleting it",
				"approvalRequest", klog.KObj(approvalRequest), "stage", stage.StageName, "stagedUpdateRun", klog.KObj(stagedUpdateRun))
			if err := r.hubClient.Delete(ctx, approvalRequest); err != nil && !apierrors.IsNotFound(err) {
				return false, nil, kferrors.NewAPIServerError(err, "failed to delete approval request not owned by the staged update run", false,
					"approvalRequest", klog.KObj(approvalRequest))
			}
			// The approval request will be re-created in the next reconciliation attempt.
			return false, nil, kferrors.NewTransientError(nil, "the approval request is not owned by the staged update run; deleted it",
				"approvalRequest", klog.KObj(approvalRequest))
		}
		// The approval request is owned by a differently named staged update run or no staged update run at all.
		klog.V(2).InfoS("Approval request is owned by a differently named staged update run or no staged update run at all",
			"approvalRequest", klog.KObj(approvalRequest), "stage", stage.StageName, "stagedUpdateRun", klog.KObj(stagedUpdateRun))

		// Flip the ApprovalRequestCreated condition to False so that users are informed of the ownership conflict.
		meta.SetStatusCondition(&task.Conditions, metav1.Condition{
			Type:   rolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestCreated,
			Status: metav1.ConditionFalse,
			Reason: rolloutv1alpha1.StagedUpdateRunTaskApprovalRequestCreatedCondReasonOwnershipMismatch,
			Message: fmt.Sprintf("An approval request %q is found but it is not owned by the staged update run; "+
				"if the request is manually created, remove it to proceed", approvalRequestName),
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
		return false, nil, kferrors.NewUserError(nil, "an approval request is found but its ownership does not align; if the request is manually created, remove it to proceed",
			"approvalRequest", klog.KObj(approvalRequest))
	}

	// Check if the approval request has been approved.
	requestApprovedCond := meta.FindStatusCondition(approvalRequest.GetStatus().Conditions, rolloutv1alpha1.ApprovalRequestCondTypeApproved)
	if requestApprovedCond == nil || requestApprovedCond.Status != metav1.ConditionTrue {
		// The approval request has not been approved yet.
		return false, nil, nil
	}

	// Add an approval accepted condition to the approval request.
	if !meta.IsStatusConditionTrue(approvalRequest.GetStatus().Conditions, rolloutv1alpha1.ApprovalRequestCondTypeApprovalAccepted) {
		meta.SetStatusCondition(&approvalRequest.GetStatus().Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.ApprovalRequestCondTypeApprovalAccepted,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.ApprovalRequestApprovalAcceptedCondReasonAccepted,
			Message:            "The approval has been accepted by the staged update run",
			ObservedGeneration: approvalRequest.GetGeneration(),
		})
		// Use an update (rather than a patch) so that the write fails with a conflict if the approval request has
		// changed (e.g., the approval has been revoked) since it was read.
		if err := r.hubClient.Status().Update(ctx, approvalRequest); err != nil {
			return false, nil, kferrors.NewAPIServerError(err, "failed to add the approval accepted condition to the approval request", false,
				"approvalRequest", klog.KObj(approvalRequest))
		}
		klog.V(2).InfoS("Accepted the approval of the approval request",
			"approvalRequest", klog.KObj(approvalRequest), "stage", stage.StageName, "stagedUpdateRun", klog.KObj(stagedUpdateRun))
	}

	// Mark the approval task as completed in the staged update run status.
	meta.SetStatusCondition(&task.Conditions, metav1.Condition{
		Type:               rolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestApproved,
		Status:             metav1.ConditionTrue,
		Reason:             rolloutv1alpha1.StagedUpdateRunTaskApprovalRequestApprovedCondReasonApproved,
		Message:            "The approval request has been approved",
		ObservedGeneration: stagedUpdateRun.GetGeneration(),
	})

	// The approval request has been approved.
	return true, nil, nil
}

// createApprovalRequest creates an approval request for the given stage of the staged update run. The
// request is owned by the staged update run; it is not an error if the request already exists.
func (r *Reconciler) createApprovalRequest(
	ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	stage *rolloutv1alpha1.PerStageStatus,
	task *rolloutv1alpha1.PerStageTaskStatus,
	isBeforeStageTask bool,
) error {
	var approvalRequestName string
	if isBeforeStageTask {
		approvalRequestName = formatBeforeStageApprovalRequestName(stagedUpdateRun.GetName(), stage.StageName)
	} else {
		approvalRequestName = formatAfterStageApprovalRequestName(stagedUpdateRun.GetName(), stage.StageName)
	}
	// Set the approval request name in the task status.
	task.ApprovalRequestName = approvalRequestName

	var approvalRequest rolloutv1alpha1.ApprovalRequestAccessor
	var ownerGVK schema.GroupVersionKind
	if stagedUpdateRun.GetNamespace() == "" {
		ownerGVK = rolloutv1alpha1.GroupVersion.WithKind(rolloutv1alpha1.ClusterStagedUpdateRunKind)
		approvalRequest = &rolloutv1alpha1.ClusterApprovalRequest{}
	} else {
		ownerGVK = rolloutv1alpha1.GroupVersion.WithKind(rolloutv1alpha1.StagedUpdateRunKind)
		approvalRequest = &rolloutv1alpha1.ApprovalRequest{}
	}

	approvalRequest.SetName(approvalRequestName)
	approvalRequest.SetNamespace(stagedUpdateRun.GetNamespace())
	approvalRequest.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(stagedUpdateRun, ownerGVK),
	})
	approvalRequest.SetSpec(rolloutv1alpha1.ApprovalRequestSpec{
		StagedUpdateRunName: stagedUpdateRun.GetName(),
		StageName:           stage.StageName,
	})

	err := r.hubClient.Create(ctx, approvalRequest)
	if err == nil || apierrors.IsAlreadyExists(err) {
		// Set the request created condition.
		meta.SetStatusCondition(&task.Conditions, metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestCreated,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.StagedUpdateRunTaskApprovalRequestCreatedCondReasonCreated,
			Message:            "The approval request has been created",
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		})
	}
	// The controller must return the AlreadyExists error instead of ignoring it, as the approval request found right now
	// might have already been processed.
	if err != nil {
		return kferrors.NewAPIServerError(err, "failed to create approval request", false, "approvalRequest", klog.KObj(approvalRequest))
	}
	return nil
}

func (r *Reconciler) executeTimedWaitStageTask(
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
	task *rolloutv1alpha1.PerStageTaskStatus,
) (shouldContinue bool, requeueAfter *time.Duration, err error) {
	// Skip the task if it has already been completed (the wait time has elapsed).
	waitTimeElapsedCond := meta.FindStatusCondition(task.Conditions, rolloutv1alpha1.StagedUpdateRunTaskCondTypeWaitTimeElapsed)
	if waitTimeElapsedCond != nil && waitTimeElapsedCond.Status == metav1.ConditionTrue {
		return true, nil, nil
	}

	// Execute a TimedWait task.
	timedWaitStartedCond := meta.FindStatusCondition(task.Conditions, rolloutv1alpha1.StagedUpdateRunTaskCondTypeTimedWaitStarted)
	if timedWaitStartedCond == nil {
		// The task is being run for the first time. Add the TimedWaitStarted condition to track the start time.
		timedWaitStartedCond = &metav1.Condition{
			Type:               rolloutv1alpha1.StagedUpdateRunTaskCondTypeTimedWaitStarted,
			Status:             metav1.ConditionTrue,
			Reason:             rolloutv1alpha1.StagedUpdateRunTaskTimedWaitStartedCondReasonTimerStarted,
			Message:            "The TimedWait task has started",
			ObservedGeneration: stagedUpdateRun.GetGeneration(),
		}
		meta.SetStatusCondition(&task.Conditions, *timedWaitStartedCond)
		// Read the TimedWaitStarted condition again to ensure that the last transition timestamp value is populated.
		timedWaitStartedCond = meta.FindStatusCondition(task.Conditions, rolloutv1alpha1.StagedUpdateRunTaskCondTypeTimedWaitStarted)
	}

	// Calculate how long the task has been waiting based on the last transition time of the TimedWaitStarted condition.
	elapsed := time.Since(timedWaitStartedCond.LastTransitionTime.Time)
	remaining := task.WaitTime.Duration - elapsed
	if remaining > 0 {
		// The wait time has not yet elapsed. Return the remaining wait time.
		return false, &remaining, nil
	}

	// The wait time has elapsed. Mark the task as completed.
	meta.SetStatusCondition(&task.Conditions, metav1.Condition{
		Type:               rolloutv1alpha1.StagedUpdateRunTaskCondTypeWaitTimeElapsed,
		Status:             metav1.ConditionTrue,
		Reason:             rolloutv1alpha1.StagedUpdateRunTaskWaitTimeElapsedCondReasonTimerElapsed,
		Message:            "The TimedWait task is completed (the wait time has elapsed)",
		ObservedGeneration: stagedUpdateRun.GetGeneration(),
	})
	return true, nil, nil
}
