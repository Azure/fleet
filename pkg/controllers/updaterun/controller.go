/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package updaterun features controllers to reconcile the stagedUpdateRun objects.
package updaterun

import (
	"context"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
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

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/informer"
)

var errStagedUpdatedFailed = fmt.Errorf("failed to continue the StagedUpdateRun")

// Reconciler reconciles a StagedUpdateRun object
type Reconciler struct {
	client.Client
	recorder record.EventRecorder
	// the informer contains the cache for all the resources we need.
	// to check the resource scope
	InformerManager informer.Manager
}

func (r *Reconciler) Reconcile(ctx context.Context, req runtime.Request) (runtime.Result, error) {
	startTime := time.Now()
	klog.V(2).InfoS("StagedUpdateRun reconciliation starts", "stagedUpdateRun", req.NamespacedName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("StagedUpdateRun reconciliation ends", "stagedUpdateRun", req.NamespacedName, "latency", latency)
	}()

	var updateRun placementv1alpha1.StagedUpdateRun
	if err := r.Client.Get(ctx, req.NamespacedName, &updateRun); err != nil {
		klog.ErrorS(err, "Failed to get stagedUpdateRun object", "stagedUpdateRun", req.Name)
		return runtime.Result{}, client.IgnoreNotFound(err)
	}
	runObjRef := klog.KObj(&updateRun)

	// Handle the deletion of the stagedUpdateRun
	if !updateRun.DeletionTimestamp.IsZero() {
		klog.V(2).InfoS("The stagedUpdateRun is being deleted", "stagedUpdateRun", runObjRef)
		return runtime.Result{}, r.handleDelete(ctx, updateRun.DeepCopy())
	}

	// Add the finalizer to the stagedUpdateRun
	if err := r.ensureFinalizer(ctx, &updateRun); err != nil {
		klog.ErrorS(err, "Failed to add the finalizer to the stagedUpdateRun", "stagedUpdateRun", runObjRef)
		return runtime.Result{}, err
	}

	if !condition.IsConditionStatusTrue(meta.FindStatusCondition(updateRun.Status.Conditions, string(placementv1alpha1.StagedUpdateRunConditionInitialized)), updateRun.Generation) {
		klog.V(2).InfoS("The stagedUpdateRun is not initialized", "stagedUpdateRun", runObjRef)
		if err := r.initialize(ctx, &updateRun); err != nil {
			klog.ErrorS(err, "Failed to initialize the stagedUpdateRun", "stagedUpdateRun", runObjRef)
			// errInitializedFailed cannot be retried
			if errors.Is(err, errInitializedFailed) {
				return runtime.Result{}, r.recordInitializationFailed(ctx, &updateRun, err.Error())
			}
			return runtime.Result{}, err
		}
	} else {
		klog.V(2).InfoS("The stagedUpdateRun is initialized,", "stagedUpdateRun", runObjRef)

	}

	// TODO: Implement stage by stage update logic
	return runtime.Result{}, nil
}

// handleDelete handles the deletion of the stagedUpdateRun object
// We need to wait for the update run to stop before deleting the stagedUpdateRun object
// We will delete all the dependent resources, such as approvalRequest objects, of the stagedUpdateRun object.
func (r *Reconciler) handleDelete(ctx context.Context, updateRun *placementv1alpha1.StagedUpdateRun) error {
	runObjRef := klog.KObj(updateRun)
	// delete all the associated snapshots
	approvalRequest := &placementv1alpha1.ApprovalRequest{}
	if err := r.Client.DeleteAllOf(ctx, approvalRequest, client.InNamespace(updateRun.GetNamespace()), client.MatchingLabels{placementv1alpha1.TargetUpdateRunLabel: updateRun.GetName()}); err != nil {
		klog.ErrorS(err, "Failed to delete all associated approvalRequests", "stagedUpdateRun", runObjRef)
		return controller.NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Deleted all approvalRequests associated with the stagedUpdateRun", "stagedUpdateRun", runObjRef)
	controllerutil.RemoveFinalizer(updateRun, placementv1alpha1.StagedUpdateRunFinalizer)
	if err := r.Client.Update(ctx, updateRun); err != nil {
		klog.ErrorS(err, "Failed to remove updateRun finalizer", "stagedUpdateRun", runObjRef)
		return controller.NewUpdateIgnoreConflictError(err)
	}
	return nil
}

// ensureFinalizer makes sure that the member cluster CR has a finalizer on it
func (r *Reconciler) ensureFinalizer(ctx context.Context, updateRun *placementv1alpha1.StagedUpdateRun) error {
	if controllerutil.ContainsFinalizer(updateRun, placementv1alpha1.StagedUpdateRunFinalizer) {
		return nil
	}
	klog.InfoS("Added the staged update run finalizer", "stagedUpdateRun", klog.KObj(updateRun))
	controllerutil.AddFinalizer(updateRun, placementv1alpha1.StagedUpdateRunFinalizer)
	return r.Update(ctx, updateRun, client.FieldOwner(utils.UpdateRunControllerFieldManagerName))
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr runtime.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("stagedupdaterun-controller")
	return runtime.NewControllerManagedBy(mgr).
		Named("stagedupdaterun-controller").
		For(&placementv1alpha1.StagedUpdateRun{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&placementv1alpha1.ApprovalRequest{}, &handler.Funcs{
			// We only care about when an approval request is approved.
			UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.RateLimitingInterface) {
				klog.V(2).InfoS("Handling an approvalRequest update event", "approvalRequest", klog.KObj(e.ObjectNew))
				handleApprovalRequest(e.ObjectNew, q)
			},
			GenericFunc: func(ctx context.Context, e event.GenericEvent, q workqueue.RateLimitingInterface) {
				klog.V(2).InfoS("Handling a approvalRequest generic event", "approvalRequest", klog.KObj(e.Object))
				handleApprovalRequest(e.Object, q)
			},
		}).Complete(r)
}

// handleApprovalRequest find the CRP name from the approval request and enqueue the CRP to the updaterun controller queue
func handleApprovalRequest(approvalRequest client.Object, q workqueue.RateLimitingInterface) {
	// get the CRP name from the label
	crp := approvalRequest.GetLabels()[placementv1beta1.CRPTrackingLabel]
	if len(crp) == 0 {
		// should never happen, we might be able to alert on this error
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot find CRPTrackingLabel label value")),
			"Invalid clusterResourceSnapshot", "clusterResourceSnapshot", klog.KObj(approvalRequest))
		return
	}
	// enqueue the CRP to the updaterun controller queue
	q.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{Name: crp},
	})
}
