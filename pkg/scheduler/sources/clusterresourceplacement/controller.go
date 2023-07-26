/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package clusterresourceplacement features a controller that enqueues CRPs for the
// scheduler to process where the CRP is marked for deletion.
package clusterresourceplacement

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/queue"
	"go.goms.io/fleet/pkg/utils/controller"
)

// Reconciler reconciles the deletion of a CRP.
type Reconciler struct {
	// client is the client the controller uses to access the hub cluster.
	client client.Client
	// schedulerWorkqueue is the workqueue in use by the scheduler.
	schedulerWorkqueue queue.ClusterResourcePlacementSchedulingQueueWriter
}

// Reconcile reconciles the CRP.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	crpRef := klog.KRef("", req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Scheduler source reconciliation starts", "clusterResourcePlacement", crpRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Scheduler source reconciliation ends", "clusterResourcePlacement", crpRef, "latency", latency)
	}()

	// Retrieve the CRP.
	crp := &fleetv1beta1.ClusterResourcePlacement{}
	if err := r.client.Get(ctx, req.NamespacedName, crp); err != nil {
		klog.ErrorS(err, "Failed to get cluster resource placement", "clusterResourcePlacement", crpRef)
		return ctrl.Result{}, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
	}

	// Check if the policy snapshot has been deleted and has the scheduler finalizer.
	//
	// Normally this would not happen as the event filter is set to filter out all deletion events.
	if crp.DeletionTimestamp != nil && controllerutil.ContainsFinalizer(crp, fleetv1beta1.SchedulerCRPCleanupFinalizer) {
		// The policy snapshot has been deleted and still has the scheduler finalizer;
		// enqueue it for the scheduler to process.
		r.schedulerWorkqueue.AddRateLimited(queue.ClusterResourcePlacementKey(crp.Name))
	}

	// No action is needed for the scheduler to take in other cases.
	return ctrl.Result{}, nil
}

// SetupWithManger sets up the controller with the manager.
func (r *Reconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	customPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Ignore creation events.
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Ignore deletion events (events emitted when the object is actually removed
			// from storage).
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Check if the update event is valid.
			if e.ObjectOld == nil || e.ObjectNew == nil {
				err := controller.NewUnexpectedBehaviorError(fmt.Errorf("update event is invalid"))
				klog.ErrorS(err, "Failed to process update event")
				return false
			}

			// Check if the deletion timestamp has been set.
			oldDeletionTimestamp := e.ObjectOld.GetDeletionTimestamp()
			newDeletionTimestamp := e.ObjectNew.GetDeletionTimestamp()
			if oldDeletionTimestamp == nil && newDeletionTimestamp != nil {
				return true
			}

			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1beta1.ClusterResourcePlacement{}).
		WithEventFilter(customPredicate).
		Complete(r)
}
