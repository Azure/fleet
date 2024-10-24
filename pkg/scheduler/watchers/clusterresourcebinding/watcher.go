/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourcebinding

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

// Reconciler reconciles the deletion of a ClusterResourceBinding.
type Reconciler struct {
	// Client is the client the controller uses to access the hub cluster.
	client.Client
	// SchedulerWorkQueue is the workqueue in use by the scheduler.
	SchedulerWorkQueue queue.ClusterResourcePlacementSchedulingQueueWriter
}

// Reconcile reconciles the CRB.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	crbRef := klog.KRef("", req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Scheduler source reconciliation starts", "clusterResourceBinding", crbRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Scheduler source reconciliation ends", "clusterResourceBinding", crbRef, "latency", latency)
	}()

	// Retrieve the CRB.
	crb := &fleetv1beta1.ClusterResourceBinding{}
	if err := r.Client.Get(ctx, req.NamespacedName, crb); err != nil {
		klog.ErrorS(err, "Failed to get cluster resource binding", "clusterResourceBinding", crbRef)
		return ctrl.Result{}, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
	}

	// Check if the CRB has been deleted and has the scheduler finalizer.
	if crb.DeletionTimestamp != nil && controllerutil.ContainsFinalizer(crb, fleetv1beta1.SchedulerCRBCleanupFinalizer) {
		// The CRB has been deleted and still has the scheduler finalizer; enqueue it's corresponding CRP
		// for the scheduler to process.
		crpName, exist := crb.GetLabels()[fleetv1beta1.CRPTrackingLabel]
		if !exist {
			err := controller.NewUnexpectedBehaviorError(fmt.Errorf("clusterResourceBinding %s doesn't have CRP tracking label", crb.Name))
			klog.ErrorS(err, "Failed to enqueue CRP name for CRB")
			// error cannot be retried.
			return ctrl.Result{}, nil
		}
		r.SchedulerWorkQueue.AddRateLimited(queue.ClusterResourcePlacementKey(crpName))
	}

	// No action is needed for the scheduler to take in other cases.
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
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
		For(&fleetv1beta1.ClusterResourceBinding{}).
		WithEventFilter(customPredicate).
		Complete(r)
}
