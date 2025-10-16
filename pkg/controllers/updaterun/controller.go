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

// Package updaterun features a controller to reconcile the updateRun objects.
package updaterun

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	runtime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	hubmetrics "github.com/kubefleet-dev/kubefleet/pkg/metrics/hub"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
)

var (
	// errStagedUpdatedAborted is the error when the updateRun is aborted.
	errStagedUpdatedAborted = fmt.Errorf("cannot continue the updateRun")
	// errInitializedFailed is the error when the updateRun fails to initialize.
	// It is a wrapped error of errStagedUpdatedAborted, because some initialization functions are reused in the validation step.
	errInitializedFailed = fmt.Errorf("%w: failed to initialize the updateRun", errStagedUpdatedAborted)
)

// Reconciler reconciles an updateRun object.
type Reconciler struct {
	client.Client
	recorder record.EventRecorder
	// the informer contains the cache for all the resources we need to check the resource scope.
	InformerManager informer.Manager
}

func (r *Reconciler) Reconcile(ctx context.Context, req runtime.Request) (runtime.Result, error) {
	startTime := time.Now()
	klog.V(2).InfoS("UpdateRun reconciliation starts", "updateRun", req.NamespacedName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("UpdateRun reconciliation ends", "updateRun", req.NamespacedName, "latency", latency)
	}()

	updateRun, err := controller.FetchUpdateRunFromRequest(ctx, r.Client, req)
	if err != nil {
		klog.ErrorS(err, "Failed to get updateRun object", "updateRun", req.NamespacedName)
		return runtime.Result{}, client.IgnoreNotFound(err)
	}
	runObjRef := klog.KObj(updateRun)

	// Remove waitTime from the updateRun status for AfterStageTask for type Approval.
	removeWaitTimeFromUpdateRunStatus(updateRun)

	// Handle the deletion of the updateRun.
	if !updateRun.GetDeletionTimestamp().IsZero() {
		klog.V(2).InfoS("The updateRun is being deleted", "updateRun", runObjRef)
		deleted, waitTime, deleteErr := r.handleDelete(ctx, updateRun.DeepCopyObject().(placementv1beta1.UpdateRunObj))
		if deleteErr != nil {
			return runtime.Result{}, deleteErr
		}
		if deleted {
			return runtime.Result{}, nil
		}
		return runtime.Result{RequeueAfter: waitTime}, nil
	}

	// Add the finalizer to the updateRun.
	if err := r.ensureFinalizer(ctx, updateRun); err != nil {
		klog.ErrorS(err, "Failed to add the finalizer to the updateRun", "updateRun", runObjRef)
		return runtime.Result{}, err
	}

	// Emit the update run status metric based on status conditions in the updateRun.
	defer emitUpdateRunStatusMetric(updateRun)

	var updatingStageIndex int
	var toBeUpdatedBindings, toBeDeletedBindings []placementv1beta1.BindingObj
	updateRunStatus := updateRun.GetUpdateRunStatus()
	initCond := meta.FindStatusCondition(updateRunStatus.Conditions, string(placementv1beta1.StagedUpdateRunConditionInitialized))
	if !condition.IsConditionStatusTrue(initCond, updateRun.GetGeneration()) {
		if condition.IsConditionStatusFalse(initCond, updateRun.GetGeneration()) {
			klog.V(2).InfoS("The updateRun has failed to initialize", "errorMsg", initCond.Message, "updateRun", runObjRef)
			return runtime.Result{}, nil
		}
		var initErr error
		if toBeUpdatedBindings, toBeDeletedBindings, initErr = r.initialize(ctx, updateRun); initErr != nil {
			klog.ErrorS(initErr, "Failed to initialize the updateRun", "updateRun", runObjRef)
			// errInitializedFailed cannot be retried.
			if errors.Is(initErr, errInitializedFailed) {
				return runtime.Result{}, r.recordInitializationFailed(ctx, updateRun, initErr.Error())
			}
			return runtime.Result{}, initErr
		}
		updatingStageIndex = 0 // start from the first stage.
		klog.V(2).InfoS("Initialized the updateRun", "updateRun", runObjRef)
	} else {
		klog.V(2).InfoS("The updateRun is initialized", "updateRun", runObjRef)
		// Check if the updateRun is finished.
		finishedCond := meta.FindStatusCondition(updateRunStatus.Conditions, string(placementv1beta1.StagedUpdateRunConditionSucceeded))
		if condition.IsConditionStatusTrue(finishedCond, updateRun.GetGeneration()) || condition.IsConditionStatusFalse(finishedCond, updateRun.GetGeneration()) {
			klog.V(2).InfoS("The updateRun is finished", "finishedSuccessfully", finishedCond.Status, "updateRun", runObjRef)
			return runtime.Result{}, nil
		}
		var validateErr error
		// Validate the updateRun status to ensure the update can be continued and get the updating stage index and cluster indices.
		if updatingStageIndex, toBeUpdatedBindings, toBeDeletedBindings, validateErr = r.validate(ctx, updateRun); validateErr != nil {
			// errStagedUpdatedAborted cannot be retried.
			if errors.Is(validateErr, errStagedUpdatedAborted) {
				return runtime.Result{}, r.recordUpdateRunFailed(ctx, updateRun, validateErr.Error())
			}
			return runtime.Result{}, validateErr
		}
		klog.V(2).InfoS("The updateRun is validated", "updateRun", runObjRef)
	}

	// The previous run is completed but the update to the status failed.
	if updatingStageIndex == -1 {
		klog.V(2).InfoS("The updateRun is completed", "updateRun", runObjRef)
		return runtime.Result{}, r.recordUpdateRunSucceeded(ctx, updateRun)
	}

	// Execute the updateRun.
	klog.V(2).InfoS("Continue to execute the updateRun", "updatingStageIndex", updatingStageIndex, "updateRun", runObjRef)
	finished, waitTime, execErr := r.execute(ctx, updateRun, updatingStageIndex, toBeUpdatedBindings, toBeDeletedBindings)
	if errors.Is(execErr, errStagedUpdatedAborted) {
		// errStagedUpdatedAborted cannot be retried.
		return runtime.Result{}, r.recordUpdateRunFailed(ctx, updateRun, execErr.Error())
	}

	if finished {
		klog.V(2).InfoS("The updateRun is completed", "updateRun", runObjRef)
		return runtime.Result{}, r.recordUpdateRunSucceeded(ctx, updateRun)
	}

	// The execution is not finished yet or it encounters a retriable error.
	// We need to record the status and requeue.
	if updateErr := r.recordUpdateRunStatus(ctx, updateRun); updateErr != nil {
		return runtime.Result{}, updateErr
	}
	klog.V(2).InfoS("The updateRun is not finished yet", "requeueWaitTime", waitTime, "execErr", execErr, "updateRun", runObjRef)
	if execErr != nil {
		return runtime.Result{}, execErr
	}
	return runtime.Result{Requeue: true, RequeueAfter: waitTime}, nil
}

// handleDelete handles the deletion of the updateRun object.
// We delete all the dependent resources, including approvalRequest objects, of the updateRun object.
func (r *Reconciler) handleDelete(ctx context.Context, updateRun placementv1beta1.UpdateRunObj) (bool, time.Duration, error) {
	runObjRef := klog.KObj(updateRun)
	// Delete all the associated approvalRequests.
	var approvalRequest placementv1beta1.ApprovalRequestObj
	deleteOptions := []client.DeleteAllOfOption{client.MatchingLabels{placementv1beta1.TargetUpdateRunLabel: updateRun.GetName()}}
	if updateRun.GetNamespace() == "" {
		approvalRequest = &placementv1beta1.ClusterApprovalRequest{}
	} else {
		approvalRequest = &placementv1beta1.ApprovalRequest{}
		deleteOptions = append(deleteOptions, client.InNamespace(updateRun.GetNamespace()))
	}
	if err := r.Client.DeleteAllOf(ctx, approvalRequest, deleteOptions...); err != nil {
		klog.ErrorS(err, "Failed to delete all associated approvalRequests", "updateRun", runObjRef)
		return false, 0, controller.NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Deleted all approvalRequests associated with the updateRun", "updateRun", runObjRef)

	// Delete the update run status metric.
	hubmetrics.FleetUpdateRunStatusLastTimestampSeconds.DeletePartialMatch(prometheus.Labels{"namespace": updateRun.GetNamespace(), "name": updateRun.GetName()})

	controllerutil.RemoveFinalizer(updateRun, placementv1beta1.UpdateRunFinalizer)
	if err := r.Client.Update(ctx, updateRun); err != nil {
		klog.ErrorS(err, "Failed to remove updateRun finalizer", "updateRun", runObjRef)
		return false, 0, controller.NewUpdateIgnoreConflictError(err)
	}
	return true, 0, nil
}

// ensureFinalizer makes sure that the updateRun CR has a finalizer on it.
func (r *Reconciler) ensureFinalizer(ctx context.Context, updateRun placementv1beta1.UpdateRunObj) error {
	if controllerutil.ContainsFinalizer(updateRun, placementv1beta1.UpdateRunFinalizer) {
		return nil
	}
	klog.InfoS("Added the updateRun finalizer", "updateRun", klog.KObj(updateRun))
	controllerutil.AddFinalizer(updateRun, placementv1beta1.UpdateRunFinalizer)
	return r.Update(ctx, updateRun, client.FieldOwner(utils.UpdateRunControllerFieldManagerName))
}

// recordUpdateRunSucceeded records the succeeded condition in the updateRun status.
func (r *Reconciler) recordUpdateRunSucceeded(ctx context.Context, updateRun placementv1beta1.UpdateRunObj) error {
	updateRunStatus := updateRun.GetUpdateRunStatus()
	meta.SetStatusCondition(&updateRunStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionProgressing),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: updateRun.GetGeneration(),
		Reason:             condition.UpdateRunSucceededReason,
		Message:            "All stages are completed",
	})
	meta.SetStatusCondition(&updateRunStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionSucceeded),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: updateRun.GetGeneration(),
		Reason:             condition.UpdateRunSucceededReason,
		Message:            "All stages are completed successfully",
	})
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the updateRun status as succeeded", "updateRun", klog.KObj(updateRun))
		// updateErr can be retried.
		return controller.NewUpdateIgnoreConflictError(updateErr)
	}
	return nil
}

// recordUpdateRunFailed records the failed condition in the updateRun status.
func (r *Reconciler) recordUpdateRunFailed(ctx context.Context, updateRun placementv1beta1.UpdateRunObj, message string) error {
	updateRunStatus := updateRun.GetUpdateRunStatus()
	meta.SetStatusCondition(&updateRunStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionProgressing),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: updateRun.GetGeneration(),
		Reason:             condition.UpdateRunFailedReason,
		Message:            "The stages are aborted due to a non-recoverable error",
	})
	meta.SetStatusCondition(&updateRunStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionSucceeded),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: updateRun.GetGeneration(),
		Reason:             condition.UpdateRunFailedReason,
		Message:            message,
	})
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the updateRun status as failed", "updateRun", klog.KObj(updateRun))
		// updateErr can be retried.
		return controller.NewUpdateIgnoreConflictError(updateErr)
	}
	return nil
}

// recordUpdateRunStatus records the updateRun status.
func (r *Reconciler) recordUpdateRunStatus(ctx context.Context, updateRun placementv1beta1.UpdateRunObj) error {
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the updateRun status", "updateRun", klog.KObj(updateRun))
		return controller.NewUpdateIgnoreConflictError(updateErr)
	}
	return nil
}

// SetupWithManagerForClusterStagedUpdateRun sets up the controller with the Manager for ClusterStagedUpdateRun resources.
func (r *Reconciler) SetupWithManagerForClusterStagedUpdateRun(mgr runtime.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("clusterstagedupdaterun-controller")
	return runtime.NewControllerManagedBy(mgr).
		Named("clusterstagedupdaterun-controller").
		For(&placementv1beta1.ClusterStagedUpdateRun{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&placementv1beta1.ClusterApprovalRequest{}, &handler.Funcs{
			// We watch for ClusterApprovalRequest to be approved.
			UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling a clusterApprovalRequest update event", "clusterApprovalRequest", klog.KObj(e.ObjectNew))
				handleApprovalRequestUpdate(e.ObjectOld, e.ObjectNew, q, true)
			},
			// We watch for ClusterApprovalRequest deletion events to recreate it ASAP.
			DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling a clusterApprovalRequest delete event", "clusterApprovalRequest", klog.KObj(e.Object))
				handleApprovalRequestDelete(e.Object, q, true)
			},
		}).Complete(r)
}

// SetupWithManagerForStagedUpdateRun sets up the controller with the Manager for StagedUpdateRun resources.
func (r *Reconciler) SetupWithManagerForStagedUpdateRun(mgr runtime.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("stagedupdaterun-controller")
	return runtime.NewControllerManagedBy(mgr).
		Named("stagedupdaterun-controller").
		For(&placementv1beta1.StagedUpdateRun{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&placementv1beta1.ApprovalRequest{}, &handler.Funcs{
			// We watch for ApprovalRequest to be approved.
			UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling an approvalRequest update event", "approvalRequest", klog.KObj(e.ObjectNew))
				handleApprovalRequestUpdate(e.ObjectOld, e.ObjectNew, q, false)
			},
			// We watch for ApprovalRequest deletion events to recreate it ASAP.
			DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling an approvalRequest delete event", "approvalRequest", klog.KObj(e.Object))
				handleApprovalRequestDelete(e.Object, q, false)
			},
		}).Complete(r)
}

// handleApprovalRequestUpdate finds the UpdateRun creating the ApprovalRequest,
// and enqueues it to the UpdateRun controller queue only when the approved condition is changed.
// The isClusterScoped parameter determines whether to handle ClusterApprovalRequest (true) or ApprovalRequest (false).
func handleApprovalRequestUpdate(oldObj, newObj client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request], isClusterScoped bool) {
	var oldAppReq, newAppReq placementv1beta1.ApprovalRequestObj

	if isClusterScoped {
		oldClusterAppReq, ok := oldObj.(*placementv1beta1.ClusterApprovalRequest)
		if !ok {
			klog.V(2).ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot cast runtime object to ClusterApprovalRequest")),
				"Invalid object type", "object", klog.KObj(oldObj))
			return
		}
		newClusterAppReq, ok := newObj.(*placementv1beta1.ClusterApprovalRequest)
		if !ok {
			klog.V(2).ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot cast runtime object to ClusterApprovalRequest")),
				"Invalid object type", "object", klog.KObj(newObj))
			return
		}
		oldAppReq = oldClusterAppReq
		newAppReq = newClusterAppReq
	} else {
		oldNamespacedAppReq, ok := oldObj.(*placementv1beta1.ApprovalRequest)
		if !ok {
			klog.V(2).ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot cast runtime object to ApprovalRequest")),
				"Invalid object type", "object", klog.KObj(oldObj))
			return
		}
		newNamespacedAppReq, ok := newObj.(*placementv1beta1.ApprovalRequest)
		if !ok {
			klog.V(2).ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot cast runtime object to ApprovalRequest")),
				"Invalid object type", "object", klog.KObj(newObj))
			return
		}
		oldAppReq = oldNamespacedAppReq
		newAppReq = newNamespacedAppReq
	}

	approvedInOld := condition.IsConditionStatusTrue(meta.FindStatusCondition(oldAppReq.GetApprovalRequestStatus().Conditions, string(placementv1beta1.ApprovalRequestConditionApproved)), oldAppReq.GetGeneration())
	approvedInNew := condition.IsConditionStatusTrue(meta.FindStatusCondition(newAppReq.GetApprovalRequestStatus().Conditions, string(placementv1beta1.ApprovalRequestConditionApproved)), newAppReq.GetGeneration())

	if approvedInOld == approvedInNew {
		klog.V(2).InfoS("The approval status is not changed, ignore queueing", "approvalRequestObj", klog.KObj(newAppReq))
		return
	}

	updateRun := newAppReq.GetApprovalRequestSpec().TargetUpdateRun
	if len(updateRun) == 0 {
		klog.V(2).ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("TargetUpdateRun field in ApprovalRequest is empty")),
			"Invalid approval request", "approvalRequestObj", klog.KObj(newAppReq))
		return
	}

	// enqueue to the updaterun controller queue.
	q.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: newAppReq.GetNamespace(),
			Name:      updateRun,
		},
	})
}

// handleApprovalRequestDelete finds the UpdateRun creating the ApprovalRequest,
// and enqueues it to the UpdateRun controller queue when the ApprovalRequest is deleted.
// The isClusterScoped parameter determines whether to handle ClusterApprovalRequest (true) or ApprovalRequest (false).
func handleApprovalRequestDelete(obj client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request], isClusterScoped bool) {
	var appReq placementv1beta1.ApprovalRequestObj

	if isClusterScoped {
		clusterAppReq, ok := obj.(*placementv1beta1.ClusterApprovalRequest)
		if !ok {
			klog.V(2).ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot cast runtime object to ClusterApprovalRequest")),
				"Invalid object type", "object", klog.KObj(obj))
			return
		}
		appReq = clusterAppReq
	} else {
		namespacedAppReq, ok := obj.(*placementv1beta1.ApprovalRequest)
		if !ok {
			klog.V(2).ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot cast runtime object to ApprovalRequest")),
				"Invalid object type", "object", klog.KObj(obj))
			return
		}
		appReq = namespacedAppReq
	}

	isApproved := meta.FindStatusCondition(appReq.GetApprovalRequestStatus().Conditions, string(placementv1beta1.ApprovalRequestConditionApproved))
	approvalAccepted := condition.IsConditionStatusTrue(meta.FindStatusCondition(appReq.GetApprovalRequestStatus().Conditions, string(placementv1beta1.ApprovalRequestConditionApprovalAccepted)), appReq.GetGeneration())
	if isApproved != nil && approvalAccepted {
		klog.V(2).InfoS("The approval request has been approved and accepted, ignore queueing for delete event", "approvalRequestObj", klog.KObj(appReq))
		return
	}

	updateRun := appReq.GetApprovalRequestSpec().TargetUpdateRun
	if len(updateRun) == 0 {
		klog.V(2).ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("TargetUpdateRun field in ApprovalRequest is empty")),
			"Invalid approval request", "approvalRequestObj", klog.KObj(appReq))
		return
	}

	// enqueue to the updaterun controller queue.
	q.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: appReq.GetNamespace(),
			Name:      updateRun,
		},
	})
}

// emitUpdateRunStatusMetric emits the update run status metric based on status conditions in the updateRun.
func emitUpdateRunStatusMetric(updateRun placementv1beta1.UpdateRunObj) {
	generation := updateRun.GetGeneration()
	genStr := strconv.FormatInt(generation, 10)

	updateRunStatus := updateRun.GetUpdateRunStatus()
	succeedCond := meta.FindStatusCondition(updateRunStatus.Conditions, string(placementv1beta1.StagedUpdateRunConditionSucceeded))
	if succeedCond != nil && succeedCond.ObservedGeneration == generation {
		hubmetrics.FleetUpdateRunStatusLastTimestampSeconds.WithLabelValues(updateRun.GetNamespace(), updateRun.GetName(), genStr,
			string(placementv1beta1.StagedUpdateRunConditionSucceeded), string(succeedCond.Status), succeedCond.Reason).SetToCurrentTime()
		return
	}

	progressingCond := meta.FindStatusCondition(updateRunStatus.Conditions, string(placementv1beta1.StagedUpdateRunConditionProgressing))
	if progressingCond != nil && progressingCond.ObservedGeneration == generation {
		hubmetrics.FleetUpdateRunStatusLastTimestampSeconds.WithLabelValues(updateRun.GetNamespace(), updateRun.GetName(), genStr,
			string(placementv1beta1.StagedUpdateRunConditionProgressing), string(progressingCond.Status), progressingCond.Reason).SetToCurrentTime()
		return
	}

	initializedCond := meta.FindStatusCondition(updateRunStatus.Conditions, string(placementv1beta1.StagedUpdateRunConditionInitialized))
	if initializedCond != nil && initializedCond.ObservedGeneration == generation {
		hubmetrics.FleetUpdateRunStatusLastTimestampSeconds.WithLabelValues(updateRun.GetNamespace(), updateRun.GetName(), genStr,
			string(placementv1beta1.StagedUpdateRunConditionInitialized), string(initializedCond.Status), initializedCond.Reason).SetToCurrentTime()
		return
	}

	// We should rarely reach here, it can only happen when updating updateRun status fails.
	klog.V(2).InfoS("There's no valid status condition on updateRun, status updating failed possibly", "updateRun", klog.KObj(updateRun))
}

func removeWaitTimeFromUpdateRunStatus(updateRun placementv1beta1.UpdateRunObj) {
	// Remove waitTime from the updateRun status for AfterStageTask for type Approval.
	updateRunStatus := updateRun.GetUpdateRunStatus()
	if updateRunStatus.UpdateStrategySnapshot != nil {
		for i := range updateRunStatus.UpdateStrategySnapshot.Stages {
			for j := range updateRunStatus.UpdateStrategySnapshot.Stages[i].AfterStageTasks {
				if updateRunStatus.UpdateStrategySnapshot.Stages[i].AfterStageTasks[j].Type == placementv1beta1.AfterStageTaskTypeApproval {
					updateRunStatus.UpdateStrategySnapshot.Stages[i].AfterStageTasks[j].WaitTime = nil
				}
			}
		}
	}
}
