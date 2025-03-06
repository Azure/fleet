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

// Package clusterresourcebindingwatcher features a controller to watch the clusterResourceBinding changes.
package clusterresourcebindingwatcher

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

// Reconciler reconciles updates to clusterResourceBinding.
type Reconciler struct {
	// Client is the client the controller uses to access the hub cluster.
	client.Client
	// PlacementController maintains a rate limited queue which used to store
	// the name of the clusterResourcePlacement and a reconcile function to consume the items in queue.
	PlacementController controller.Controller
}

// Reconcile watches the clusterResourceBinding and enqueues the corresponding CRP name for those bindings whose
// status has changed. This is for the CRP controller to update the corresponding placementStatuses.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	bindingRef := klog.KRef("", req.Name)

	startTime := time.Now()
	klog.V(2).InfoS("ClusterResourceBindingWatcher Reconciliation starts", "clusterResourceBinding", bindingRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("ClusterResourceBindingWatcher Reconciliation ends", "clusterResourceBinding", bindingRef, "latency", latency)
	}()

	var binding fleetv1beta1.ClusterResourceBinding
	if err := r.Client.Get(ctx, req.NamespacedName, &binding); err != nil {
		klog.ErrorS(err, "Failed to get cluster resource binding", "clusterResourceBinding", bindingRef)
		return ctrl.Result{}, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
	}

	// Check if the cluster resource binding has been deleted.
	// Normally this would not happen as the event filter is set to filter out all deletion events.
	if binding.DeletionTimestamp != nil {
		// The cluster resource binding has been deleted; ignore it.
		return ctrl.Result{}, nil
	}

	// Fetch the CRP name from the CRPTrackingLabel on ClusterResourceBinding.
	crpName := binding.Labels[fleetv1beta1.CRPTrackingLabel]
	if len(crpName) == 0 {
		// The CRPTrackingLabel label is not present; normally this should never occur.
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("CRPTrackingLabel is missing or value is empty")),
			"CRPTrackingLabel is not present",
			"clusterResourceBinding", bindingRef)
		// This is not a situation that the controller can recover by itself. Should the label
		// value be corrected, the controller will be triggered again.
		return ctrl.Result{}, nil
	}

	// Enqueue the CRP name for reconciling.
	r.PlacementController.Enqueue(crpName)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	customPredicate := predicate.Funcs{
		// Ignoring creation and deletion events because the clusterSchedulingPolicySnapshot status is updated when bindings are create/deleted clusterSchedulingPolicySnapshot
		// controller enqueues the CRP name for reconciling whenever clusterSchedulingPolicySnapshot is updated.
		CreateFunc: func(e event.CreateEvent) bool {
			// Ignore creation events.
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Ignore deletion events.
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Check if the update event is valid.
			if e.ObjectOld == nil || e.ObjectNew == nil {
				err := controller.NewUnexpectedBehaviorError(fmt.Errorf("update event is invalid"))
				klog.ErrorS(err, "Failed to process update event")
				return false
			}
			oldBinding, oldOk := e.ObjectOld.(*fleetv1beta1.ClusterResourceBinding)
			newBinding, newOk := e.ObjectNew.(*fleetv1beta1.ClusterResourceBinding)
			if !oldOk || !newOk {
				err := controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to cast runtime objects in update event to cluster resource binding objects"))
				klog.ErrorS(err, "Failed to process update event")
				return false
			}
			return isBindingStatusUpdated(oldBinding, newBinding)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).Named("clusterresourcebinding-watcher").
		For(&fleetv1beta1.ClusterResourceBinding{}).
		WithEventFilter(customPredicate).
		Complete(r)
}

func isBindingStatusUpdated(oldBinding, newBinding *fleetv1beta1.ClusterResourceBinding) bool {
	for i := condition.RolloutStartedCondition; i < condition.TotalCondition; i++ {
		oldCond := oldBinding.GetCondition(string(i.ResourceBindingConditionType()))
		newCond := newBinding.GetCondition(string(i.ResourceBindingConditionType()))
		if !condition.EqualCondition(oldCond, newCond) {
			klog.V(2).InfoS("The binding status conditions have changed, need to refresh the CRP status", "binding", klog.KObj(oldBinding), "type", i.ResourceBindingConditionType())
			return true
		}
	}
	if !utils.IsFailedResourcePlacementsEqual(oldBinding.Status.FailedPlacements, newBinding.Status.FailedPlacements) {
		klog.V(2).InfoS("Failed placements reported on the binding status has changed, need to refresh the CRP status", "binding", klog.KObj(oldBinding))
		return true
	}
	if !utils.IsDriftedResourcePlacementsEqual(oldBinding.Status.DriftedPlacements, newBinding.Status.DriftedPlacements) {
		klog.V(2).InfoS("Drifted placements reported on the binding status has changed, need to refresh the CRP status", "binding", klog.KObj(oldBinding))
		return true
	}
	if !utils.IsDiffedResourcePlacementsEqual(oldBinding.Status.DiffedPlacements, newBinding.Status.DiffedPlacements) {
		klog.V(2).InfoS("Diffed placements reported on the binding status has changed, need to refresh the CRP status", "binding", klog.KObj(oldBinding))
		return true
	}

	klog.V(5).InfoS("The binding status has not changed, no need to refresh the CRP status", "binding", klog.KObj(oldBinding))
	return false
}
