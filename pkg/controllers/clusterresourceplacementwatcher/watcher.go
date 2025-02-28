/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package clusterresourceplacementwatcher features a controller to watch the clusterResourcePlacement changes.
package clusterresourceplacementwatcher

import (
	"context"
	"time"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
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
	startTime := time.Now()
	klog.V(2).InfoS("ClusterResourcePlacementWatcher reconciliation starts", "clusterResourcePlacement", req.Name)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("ClusterResourcePlacementWatcher reconciliation ends", "clusterResourcePlacement", req.Name, "latency", latency)
	}()

	r.PlacementController.Enqueue(req.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("clusterresourceplacementv1beta1-watcher").
		For(&fleetv1beta1.ClusterResourcePlacement{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}
