/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package updaterun features a controller to reconcile the clusterStagedUpdateRun objects.
package updaterun

import (
	"context"
	"fmt"
	"time"

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

	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
)

// Reconciler reconciles a ClusterStagedUpdateRun object.
type Reconciler struct {
	client.Client
	recorder record.EventRecorder
}

func (r *Reconciler) Reconcile(ctx context.Context, req runtime.Request) (runtime.Result, error) {
	startTime := time.Now()
	klog.V(2).InfoS("ClusterStagedUpdateRun reconciliation starts", "clusterStagedUpdateRun", req.NamespacedName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("ClusterStagedUpdateRun reconciliation ends", "clusterStagedUpdateRun", req.NamespacedName, "latency", latency)
	}()

	var updateRun placementv1alpha1.ClusterStagedUpdateRun
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

	// TODO(wantjian): reconcile the clusterStagedUpdateRun.
	return runtime.Result{}, nil
}

// handleDelete handles the deletion of the clusterStagedUpdateRun object.
// We delete all the dependent resources, including approvalRequest objects, of the clusterStagedUpdateRun object.
func (r *Reconciler) handleDelete(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun) (bool, time.Duration, error) {
	runObjRef := klog.KObj(updateRun)
	// delete all the associated approvalRequests.
	approvalRequest := &placementv1alpha1.ClusterApprovalRequest{}
	if err := r.Client.DeleteAllOf(ctx, approvalRequest, client.MatchingLabels{placementv1alpha1.TargetUpdateRunLabel: updateRun.GetName()}); err != nil {
		klog.ErrorS(err, "Failed to delete all associated approvalRequests", "clusterStagedUpdateRun", runObjRef)
		return false, 0, controller.NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Deleted all approvalRequests associated with the clusterStagedUpdateRun", "clusterStagedUpdateRun", runObjRef)
	controllerutil.RemoveFinalizer(updateRun, placementv1alpha1.ClusterStagedUpdateRunFinalizer)
	if err := r.Client.Update(ctx, updateRun); err != nil {
		klog.ErrorS(err, "Failed to remove updateRun finalizer", "clusterStagedUpdateRun", runObjRef)
		return false, 0, controller.NewUpdateIgnoreConflictError(err)
	}
	return true, 0, nil
}

// ensureFinalizer makes sure that the ClusterStagedUpdateRun CR has a finalizer on it.
func (r *Reconciler) ensureFinalizer(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun) error {
	if controllerutil.ContainsFinalizer(updateRun, placementv1alpha1.ClusterStagedUpdateRunFinalizer) {
		return nil
	}
	klog.InfoS("Added the staged update run finalizer", "stagedUpdateRun", klog.KObj(updateRun))
	controllerutil.AddFinalizer(updateRun, placementv1alpha1.ClusterStagedUpdateRunFinalizer)
	return r.Update(ctx, updateRun, client.FieldOwner(utils.UpdateRunControllerFieldManagerName))
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr runtime.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("clusterresource-stagedupdaterun-controller")
	return runtime.NewControllerManagedBy(mgr).
		Named("clusterresource-stagedupdaterun-controller").
		For(&placementv1alpha1.ClusterStagedUpdateRun{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&placementv1alpha1.ClusterApprovalRequest{}, &handler.Funcs{
			// We only care about when an approval request is approved.
			UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.RateLimitingInterface) {
				klog.V(2).InfoS("Handling a clusterApprovalRequest update event", "clusterApprovalRequest", klog.KObj(e.ObjectNew))
				handleClusterApprovalRequest(e.ObjectNew, q)
			},
			GenericFunc: func(ctx context.Context, e event.GenericEvent, q workqueue.RateLimitingInterface) {
				klog.V(2).InfoS("Handling a clusterApprovalRequest generic event", "clusterApprovalRequest", klog.KObj(e.Object))
				handleClusterApprovalRequest(e.Object, q)
			},
		}).Complete(r)
}

// handleClusterApprovalRequest finds the ClusterStagedUpdateRun creating the ClusterApprovalRequest,
// and enqueues it to the ClusterStagedUpdateRun controller queue.
func handleClusterApprovalRequest(obj client.Object, q workqueue.RateLimitingInterface) {
	approvalRequest, ok := obj.(*placementv1alpha1.ClusterApprovalRequest)
	if !ok {
		klog.V(2).ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot cast runtime object to ClusterApprovalRequest")),
			"Invalid object type", "object", klog.KObj(obj))
		return
	}
	updateRun := approvalRequest.Spec.TargetUpdateRun
	if len(updateRun) == 0 {
		klog.V(2).ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("TargetUpdateRun field in ClusterApprovalRequest is empty")),
			"Invalid clusterApprovalRequest", "clusterApprovalRequest", klog.KObj(approvalRequest))
		return
	}
	// enqueue to the updaterun controller queue.
	q.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{Name: updateRun},
	})
}
