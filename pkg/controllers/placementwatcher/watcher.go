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

// Package placementwatcher features a controller to watch the clusterResourcePlacement and resourcePlacement changes.
package placementwatcher

import (
	"context"
	"time"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// Reconciler reconciles both ClusterResourcePlacement and ResourcePlacement objects.
type Reconciler struct {
	// PlacementController maintains a rate limited queue which used to store
	// the name of the placement objects and a reconcile function to consume the items in queue.
	PlacementController controller.Controller
}

// Reconcile triggers a single placement reconcile round if placement has changed.
func (r *Reconciler) Reconcile(_ context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	klog.V(2).InfoS("PlacementWatcher reconciliation starts", "placement", req.NamespacedName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("PlacementWatcher reconciliation ends", "placement", req.NamespacedName, "latency", latency)
	}()

	r.PlacementController.Enqueue(string(controller.GetObjectKeyFromRequest(req)))
	return ctrl.Result{}, nil
}

// SetupWithManagerForClusterResourcePlacement sets up the controller with the Manager.
func (r *Reconciler) SetupWithManagerForClusterResourcePlacement(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("clusterresourceplacement-watcher").
		For(&fleetv1beta1.ClusterResourcePlacement{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

// SetupWithManagerForResourcePlacement sets up the controller with the Manager.
func (r *Reconciler) SetupWithManagerForResourcePlacement(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("resourceplacement-watcher").
		For(&fleetv1beta1.ResourcePlacement{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}
