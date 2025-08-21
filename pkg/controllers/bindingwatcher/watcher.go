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

// Package bindingwatcher features a controller to watch the clusterResourceBinding and resourceBinding changes.
package bindingwatcher

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// Reconciler reconciles updates to clusterResourceBinding and resourceBinding.
type Reconciler struct {
	// Client is the client the controller uses to access the hub cluster.
	client.Client
	// PlacementController maintains a rate limited queue which used to store
	// the name of the clusterResourcePlacement/resourcePlacement and a reconcile function to consume the items in queue.
	PlacementController controller.Controller
}

// Reconcile watches the binding resources and enqueues the corresponding placement name for those bindings whose
// status has changed. This is for the placement controller to update the corresponding placementStatuses.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	bindingRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("BindingWatcher Reconciliation starts", "binding", bindingRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("BindingWatcher Reconciliation ends", "binding", bindingRef, "latency", latency)
	}()

	var binding fleetv1beta1.BindingObj
	var err error
	if req.Namespace == "" {
		// ClusterResourceBinding (cluster-scoped)
		var clusterResourceBinding fleetv1beta1.ClusterResourceBinding
		if err = r.Client.Get(ctx, req.NamespacedName, &clusterResourceBinding); err != nil {
			klog.ErrorS(err, "Failed to get cluster resource binding", "binding", bindingRef)
			return ctrl.Result{}, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
		}
		binding = &clusterResourceBinding
	} else {
		// ResourceBinding (namespaced)
		var resourceBinding fleetv1beta1.ResourceBinding
		if err = r.Client.Get(ctx, req.NamespacedName, &resourceBinding); err != nil {
			klog.ErrorS(err, "Failed to get resource binding", "binding", bindingRef)
			return ctrl.Result{}, controller.NewAPIServerError(true, client.IgnoreNotFound(err))
		}
		binding = &resourceBinding
	}

	// Check if the binding has been deleted.
	// Normally this would not happen as the event filter is set to filter out all deletion events.
	if binding.GetDeletionTimestamp() != nil {
		// The binding has been deleted; ignore it.
		return ctrl.Result{}, nil
	}

	// Fetch the placement name from the PlacementTrackingLabel on the binding.
	placementName := binding.GetLabels()[fleetv1beta1.PlacementTrackingLabel]
	if len(placementName) == 0 {
		// The PlacementTrackingLabel label is not present; normally this should never occur.
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("PlacementTrackingLabel is missing or value is empty")),
			"PlacementTrackingLabel is not present",
			"binding", bindingRef)
		// This is not a situation that the controller can recover by itself. Should the label
		// value be corrected, the controller will be triggered again.
		return ctrl.Result{}, nil
	}

	// Enqueue the placement key for reconciling.
	r.PlacementController.Enqueue(controller.GetObjectKeyFromNamespaceName(req.Namespace, placementName))
	return ctrl.Result{}, nil
}

// SetupWithManagerForClusterResourceBinding sets up the controller with the manager for ClusterResourceBinding.
func (r *Reconciler) SetupWithManagerForClusterResourceBinding(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("cluster-resource-binding-watcher").
		For(&fleetv1beta1.ClusterResourceBinding{}).
		WithEventFilter(buildCustomPredicate(true)).
		Complete(r)
}

// SetupWithManagerForResourceBinding sets up the controller with the manager for ResourceBinding.
func (r *Reconciler) SetupWithManagerForResourceBinding(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("resource-binding-watcher").
		For(&fleetv1beta1.ResourceBinding{}).
		WithEventFilter(buildCustomPredicate(false)).
		Complete(r)
}

func buildCustomPredicate(isClusterScoped bool) predicate.Predicate {
	return predicate.Funcs{
		// Ignoring creation and deletion events because the policySnapshot status is updated when bindings are created/deleted
		// controller enqueues the placement key for reconciling whenever policySnapshot is updated.
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

			var oldBinding, newBinding fleetv1beta1.BindingObj
			var oldOK, newOK bool
			if isClusterScoped {
				oldBinding, oldOK = e.ObjectOld.(*fleetv1beta1.ClusterResourceBinding)
				newBinding, newOK = e.ObjectNew.(*fleetv1beta1.ClusterResourceBinding)
			} else {
				oldBinding, oldOK = e.ObjectOld.(*fleetv1beta1.ResourceBinding)
				newBinding, newOK = e.ObjectNew.(*fleetv1beta1.ResourceBinding)
			}

			if !oldOK || !newOK {
				err := controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to cast runtime objects in update event to binding objects"))
				klog.ErrorS(err, "Failed to process update event", "isClusterScoped", isClusterScoped)
				return false
			}
			return isBindingStatusUpdated(oldBinding, newBinding)
		},
	}
}

func isBindingStatusUpdated(oldBinding, newBinding fleetv1beta1.BindingObj) bool {
	for i := condition.RolloutStartedCondition; i < condition.TotalCondition; i++ {
		oldCond := oldBinding.GetCondition(string(i.ResourceBindingConditionType()))
		newCond := newBinding.GetCondition(string(i.ResourceBindingConditionType()))
		if !condition.EqualCondition(oldCond, newCond) {
			klog.V(2).InfoS("The binding status conditions have changed, need to refresh the placement status", "binding", klog.KObj(oldBinding), "type", i.ResourceBindingConditionType())
			return true
		}
	}

	oldStatus := oldBinding.GetBindingStatus()
	newStatus := newBinding.GetBindingStatus()

	if !utils.IsFailedResourcePlacementsEqual(oldStatus.FailedPlacements, newStatus.FailedPlacements) {
		klog.V(2).InfoS("Failed placements reported on the binding status has changed, need to refresh the placement status", "binding", klog.KObj(oldBinding))
		return true
	}
	if !utils.IsDriftedResourcePlacementsEqual(oldStatus.DriftedPlacements, newStatus.DriftedPlacements) {
		klog.V(2).InfoS("Drifted placements reported on the binding status has changed, need to refresh the placement status", "binding", klog.KObj(oldBinding))
		return true
	}
	if !utils.IsDiffedResourcePlacementsEqual(oldStatus.DiffedPlacements, newStatus.DiffedPlacements) {
		klog.V(2).InfoS("Diffed placements reported on the binding status has changed, need to refresh the placement status", "binding", klog.KObj(oldBinding))
		return true
	}

	klog.V(5).InfoS("The binding status has not changed, no need to refresh the placement status", "binding", klog.KObj(oldBinding))
	return false
}
