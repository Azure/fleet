/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package updaterun features a controller to reconcile the clusterStagedUpdateRun objects.
package updaterun

import (
	"context"
	"errors"
	"fmt"
	"time"

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

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/informer"
)

var (
	// errStagedUpdatedAborted is the error when the ClusterStagedUpdateRun is aborted.
	errStagedUpdatedAborted = fmt.Errorf("cannot continue the ClusterStagedUpdateRun")
	// errInitializedFailed is the error when the ClusterStagedUpdateRun fails to initialize.
	// It is a wrapped error of errStagedUpdatedAborted, because some initialization functions are reused in the validation step.
	errInitializedFailed = fmt.Errorf("%w: failed to initialize the clusterStagedUpdateRun", errStagedUpdatedAborted)
)

// Reconciler reconciles a ClusterStagedUpdateRun object.
type Reconciler struct {
	client.Client
	recorder record.EventRecorder
	// the informer contains the cache for all the resources we need to check the resource scope.
	InformerManager informer.Manager
}

func (r *Reconciler) Reconcile(ctx context.Context, req runtime.Request) (runtime.Result, error) {
	startTime := time.Now()
	klog.V(2).InfoS("ClusterStagedUpdateRun reconciliation starts", "clusterStagedUpdateRun", req.NamespacedName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("ClusterStagedUpdateRun reconciliation ends", "clusterStagedUpdateRun", req.NamespacedName, "latency", latency)
	}()

	var updateRun placementv1beta1.ClusterStagedUpdateRun
	if err := r.Client.Get(ctx, req.NamespacedName, &updateRun); err != nil {
		klog.ErrorS(err, "Failed to get clusterStagedUpdateRun object", "clusterStagedUpdateRun", req.Name)
		return runtime.Result{}, client.IgnoreNotFound(err)
	}
	runObjRef := klog.KObj(&updateRun)

	// Handle the deletion of the clusterStagedUpdateRun.
	if !updateRun.DeletionTimestamp.IsZero() {
		klog.V(2).InfoS("The clusterStagedUpdateRun is being deleted", "clusterStagedUpdateRun", runObjRef)
		deleted, waitTime, err := r.handleDelete(ctx, updateRun.DeepCopy())
		if err != nil {
			return runtime.Result{}, err
		}
		if deleted {
			return runtime.Result{}, nil
		}
		return runtime.Result{RequeueAfter: waitTime}, nil
	}

	// Add the finalizer to the clusterStagedUpdateRun.
	if err := r.ensureFinalizer(ctx, &updateRun); err != nil {
		klog.ErrorS(err, "Failed to add the finalizer to the clusterStagedUpdateRun", "clusterStagedUpdateRun", runObjRef)
		return runtime.Result{}, err
	}

	var updatingStageIndex int
	var toBeUpdatedBindings, toBeDeletedBindings []*placementv1beta1.ClusterResourceBinding
	var err error
	initCond := meta.FindStatusCondition(updateRun.Status.Conditions, string(placementv1beta1.StagedUpdateRunConditionInitialized))
	if !condition.IsConditionStatusTrue(initCond, updateRun.Generation) {
		if condition.IsConditionStatusFalse(initCond, updateRun.Generation) {
			klog.V(2).InfoS("The clusterStagedUpdateRun has failed to initialize", "errorMsg", initCond.Message, "clusterStagedUpdateRun", runObjRef)
			return runtime.Result{}, nil
		}
		if toBeUpdatedBindings, toBeDeletedBindings, err = r.initialize(ctx, &updateRun); err != nil {
			klog.ErrorS(err, "Failed to initialize the clusterStagedUpdateRun", "clusterStagedUpdateRun", runObjRef)
			// errInitializedFailed cannot be retried.
			if errors.Is(err, errInitializedFailed) {
				return runtime.Result{}, r.recordInitializationFailed(ctx, &updateRun, err.Error())
			}
			return runtime.Result{}, err
		}
		updatingStageIndex = 0 // start from the first stage.
		klog.V(2).InfoS("Initialized the clusterStagedUpdateRun", "clusterStagedUpdateRun", runObjRef)
	} else {
		klog.V(2).InfoS("The clusterStagedUpdateRun is initialized", "clusterStagedUpdateRun", runObjRef)
		// Check if the clusterStagedUpdateRun is finished.
		finishedCond := meta.FindStatusCondition(updateRun.Status.Conditions, string(placementv1beta1.StagedUpdateRunConditionSucceeded))
		if condition.IsConditionStatusTrue(finishedCond, updateRun.Generation) || condition.IsConditionStatusFalse(finishedCond, updateRun.Generation) {
			klog.V(2).InfoS("The clusterStagedUpdateRun is finished", "finishedSuccessfully", finishedCond.Status, "clusterStagedUpdateRun", runObjRef)
			return runtime.Result{}, nil
		}

		// Validate the clusterStagedUpdateRun status to ensure the update can be continued and get the updating stage index and cluster indices.
		if updatingStageIndex, toBeUpdatedBindings, toBeDeletedBindings, err = r.validate(ctx, &updateRun); err != nil {
			// errStagedUpdatedAborted cannot be retried.
			if errors.Is(err, errStagedUpdatedAborted) {
				return runtime.Result{}, r.recordUpdateRunFailed(ctx, &updateRun, err.Error())
			}
			return runtime.Result{}, err
		}
		klog.V(2).InfoS("The clusterStagedUpdateRun is validated", "clusterStagedUpdateRun", runObjRef)
	}

	// The previous run is completed but the update to the status failed.
	if updatingStageIndex == -1 {
		klog.V(2).InfoS("The clusterStagedUpdateRun is completed", "clusterStagedUpdateRun", runObjRef)
		return runtime.Result{}, r.recordUpdateRunSucceeded(ctx, &updateRun)
	}

	// Execute the updateRun.
	klog.V(2).InfoS("Continue to execute the clusterStagedUpdateRun", "updatingStageIndex", updatingStageIndex, "clusterStagedUpdateRun", runObjRef)
	finished, waitTime, execErr := r.execute(ctx, &updateRun, updatingStageIndex, toBeUpdatedBindings, toBeDeletedBindings)
	if errors.Is(execErr, errStagedUpdatedAborted) {
		// errStagedUpdatedAborted cannot be retried.
		return runtime.Result{}, r.recordUpdateRunFailed(ctx, &updateRun, execErr.Error())
	}

	if finished {
		klog.V(2).InfoS("The clusterStagedUpdateRun is completed", "clusterStagedUpdateRun", runObjRef)
		return runtime.Result{}, r.recordUpdateRunSucceeded(ctx, &updateRun)
	}

	// The execution is not finished yet or it encounters a retriable error.
	// We need to record the status and requeue.
	if updateErr := r.recordUpdateRunStatus(ctx, &updateRun); updateErr != nil {
		return runtime.Result{}, updateErr
	}
	klog.V(2).InfoS("The clusterStagedUpdateRun is not finished yet", "requeueWaitTime", waitTime, "execErr", execErr, "clusterStagedUpdateRun", runObjRef)
	if execErr != nil {
		return runtime.Result{}, execErr
	}
	return runtime.Result{Requeue: true, RequeueAfter: waitTime}, nil
}

// handleDelete handles the deletion of the clusterStagedUpdateRun object.
// We delete all the dependent resources, including approvalRequest objects, of the clusterStagedUpdateRun object.
func (r *Reconciler) handleDelete(ctx context.Context, updateRun *placementv1beta1.ClusterStagedUpdateRun) (bool, time.Duration, error) {
	runObjRef := klog.KObj(updateRun)
	// delete all the associated approvalRequests.
	approvalRequest := &placementv1beta1.ClusterApprovalRequest{}
	if err := r.Client.DeleteAllOf(ctx, approvalRequest, client.MatchingLabels{placementv1beta1.TargetUpdateRunLabel: updateRun.GetName()}); err != nil {
		klog.ErrorS(err, "Failed to delete all associated approvalRequests", "clusterStagedUpdateRun", runObjRef)
		return false, 0, controller.NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Deleted all approvalRequests associated with the clusterStagedUpdateRun", "clusterStagedUpdateRun", runObjRef)
	controllerutil.RemoveFinalizer(updateRun, placementv1beta1.ClusterStagedUpdateRunFinalizer)
	if err := r.Client.Update(ctx, updateRun); err != nil {
		klog.ErrorS(err, "Failed to remove updateRun finalizer", "clusterStagedUpdateRun", runObjRef)
		return false, 0, controller.NewUpdateIgnoreConflictError(err)
	}
	return true, 0, nil
}

// ensureFinalizer makes sure that the ClusterStagedUpdateRun CR has a finalizer on it.
func (r *Reconciler) ensureFinalizer(ctx context.Context, updateRun *placementv1beta1.ClusterStagedUpdateRun) error {
	if controllerutil.ContainsFinalizer(updateRun, placementv1beta1.ClusterStagedUpdateRunFinalizer) {
		return nil
	}
	klog.InfoS("Added the staged update run finalizer", "stagedUpdateRun", klog.KObj(updateRun))
	controllerutil.AddFinalizer(updateRun, placementv1beta1.ClusterStagedUpdateRunFinalizer)
	return r.Update(ctx, updateRun, client.FieldOwner(utils.UpdateRunControllerFieldManagerName))
}

// recordUpdateRunSucceeded records the succeeded condition in the ClusterStagedUpdateRun status.
func (r *Reconciler) recordUpdateRunSucceeded(ctx context.Context, updateRun *placementv1beta1.ClusterStagedUpdateRun) error {
	meta.SetStatusCondition(&updateRun.Status.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionSucceeded),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: updateRun.Generation,
		Reason:             condition.UpdateRunSucceededReason,
	})
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the ClusterStagedUpdateRun status as succeeded", "clusterStagedUpdateRun", klog.KObj(updateRun))
		// updateErr can be retried.
		return controller.NewUpdateIgnoreConflictError(updateErr)
	}
	return nil
}

// recordUpdateRunFailed records the failed condition in the ClusterStagedUpdateRun status.
func (r *Reconciler) recordUpdateRunFailed(ctx context.Context, updateRun *placementv1beta1.ClusterStagedUpdateRun, message string) error {
	meta.SetStatusCondition(&updateRun.Status.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionSucceeded),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: updateRun.Generation,
		Reason:             condition.UpdateRunFailedReason,
		Message:            message,
	})
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the ClusterStagedUpdateRun status as failed", "clusterStagedUpdateRun", klog.KObj(updateRun))
		// updateErr can be retried.
		return controller.NewUpdateIgnoreConflictError(updateErr)
	}
	return nil
}

// recordUpdateRunStatus records the ClusterStagedUpdateRun status.
func (r *Reconciler) recordUpdateRunStatus(ctx context.Context, updateRun *placementv1beta1.ClusterStagedUpdateRun) error {
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the ClusterStagedUpdateRun status", "clusterStagedUpdateRun", klog.KObj(updateRun))
		return controller.NewUpdateIgnoreConflictError(updateErr)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr runtime.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("clusterresource-stagedupdaterun-controller")
	return runtime.NewControllerManagedBy(mgr).
		Named("clusterresource-stagedupdaterun-controller").
		For(&placementv1beta1.ClusterStagedUpdateRun{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&placementv1beta1.ClusterApprovalRequest{}, &handler.Funcs{
			// We only care about when an approval request is approved.
			UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling a clusterApprovalRequest update event", "clusterApprovalRequest", klog.KObj(e.ObjectNew))
				handleClusterApprovalRequest(e.ObjectOld, e.ObjectNew, q)
			},
		}).Complete(r)
}

// handleClusterApprovalRequest finds the ClusterStagedUpdateRun creating the ClusterApprovalRequest,
// and enqueues it to the ClusterStagedUpdateRun controller queue only when the approved condition is changed.
func handleClusterApprovalRequest(oldObj, newObj client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	oldAppReq, ok := oldObj.(*placementv1beta1.ClusterApprovalRequest)
	if !ok {
		klog.V(2).ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot cast runtime object to ClusterApprovalRequest")),
			"Invalid object type", "object", klog.KObj(oldObj))
		return
	}
	newAppReq, ok := newObj.(*placementv1beta1.ClusterApprovalRequest)
	if !ok {
		klog.V(2).ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot cast runtime object to ClusterApprovalRequest")),
			"Invalid object type", "object", klog.KObj(newObj))
		return
	}

	approvedInOld := condition.IsConditionStatusTrue(meta.FindStatusCondition(oldAppReq.Status.Conditions, string(placementv1beta1.ApprovalRequestConditionApproved)), oldAppReq.Generation)
	approvedInNew := condition.IsConditionStatusTrue(meta.FindStatusCondition(newAppReq.Status.Conditions, string(placementv1beta1.ApprovalRequestConditionApproved)), newAppReq.Generation)

	if approvedInOld == approvedInNew {
		klog.V(2).InfoS("The approval status is not changed, ignore queueing", "clusterApprovalRequest", klog.KObj(newAppReq))
		return
	}

	updateRun := newAppReq.Spec.TargetUpdateRun
	if len(updateRun) == 0 {
		klog.V(2).ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("TargetUpdateRun field in ClusterApprovalRequest is empty")),
			"Invalid clusterApprovalRequest", "clusterApprovalRequest", klog.KObj(newAppReq))
		return
	}
	// enqueue to the updaterun controller queue.
	q.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{Name: updateRun},
	})
}
