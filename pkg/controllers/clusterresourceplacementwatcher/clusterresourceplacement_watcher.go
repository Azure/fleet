/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package clusterresourceplacementwatcher features a controller to watch the clusterResourcePlacement changes.
package clusterresourceplacementwatcher

import (
	"context"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
)

// Reconciler reconciles a clusterResourcePlacement object.
type Reconciler struct {
	// PlacementController maintains a rate limited queue which used to store
	// the name of the clusterResourcePlacement and a reconcile function to consume the items in queue.
	PlacementController controller.Controller
}

// Reconcile triggers a single CRP reconcile round if CRP has changed.
func (r *Reconciler) Reconcile(_ context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.V(2).InfoS("Reconciliation starts", "clusterResourcePlacement", req.Name)
	defer klog.V(2).InfoS("Reconciliation ends", "clusterResourcePlacement", req.Name)

	r.PlacementController.Enqueue(req.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1beta1.ClusterResourcePlacement{}).
		WithEventFilter(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				if e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration() {
					klog.V(4).InfoS("Ignore a clusterResourcePlacement update event with no spec change", "clusterResourcePlacement", e.ObjectNew.GetName())
					return false
				}
				return true
			},
		}).
		Complete(r)
}
