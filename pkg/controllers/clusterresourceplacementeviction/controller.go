/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacementeviction

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	runtime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/controller"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	bindingutils "go.goms.io/fleet/pkg/utils/binding"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

const (
	clusterResourcePlacementEvictionValidReason       = "ClusterResourcePlacementEvictionValid"
	clusterResourcePlacementEvictionInvalidReason     = "ClusterResourcePlacementEvictionInvalid"
	clusterResourcePlacementEvictionExecutedReason    = "ClusterResourcePlacementEvictionExecuted"
	clusterResourcePlacementEvictionNotExecutedReason = "ClusterResourcePlacementEvictionNotExecuted"

	evictionInvalidMissingCRPMessage       = "Failed to find ClusterResourcePlacement targeted by eviction"
	evictionValidMessage                   = "Eviction is valid"
	evictionAllowedNoPDBMessage            = "Eviction Allowed, no ClusterResourcePlacementDisruptionBudget specified"
	evictionAllowedPlacementRemovedMessage = "Eviction Allowed, placement is currently being removed from cluster targeted bu eviction"
	evictionAllowedPlacementFailedMessage  = "Eviction Allowed, placement has failed"

	evictionAllowedPDBSpecifiedFmt = "Eviction is allowed by specified ClusterResourcePlacementDisruptionBudget, availablePlacements: %d, desiredPlacements: %d, totalPlacements: %d"
	evictionBlockedPDBSpecifiedFmt = "Eviction is blocked by specified ClusterResourcePlacementDisruptionBudget, availablePlacements: %d, desiredPlacements: %d, totalPlacements: %d"
)

// Reconciler reconciles a ClusterResourcePlacementEviction object.
type Reconciler struct {
	client.Client
}

// Reconcile triggers a single eviction reconcile round.
func (r *Reconciler) Reconcile(ctx context.Context, req runtime.Request) (runtime.Result, error) {
	startTime := time.Now()
	evictionName := req.NamespacedName.Name
	klog.V(2).InfoS("ClusterResourcePlacementEviction reconciliation starts", "clusterResourcePlacementEviction", evictionName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("ClusterResourcePlacementEviction reconciliation ends", "clusterResourcePlacementEviction", evictionName, "latency", latency)
	}()

	var eviction placementv1alpha1.ClusterResourcePlacementEviction
	if err := r.Client.Get(ctx, req.NamespacedName, &eviction); err != nil {
		klog.ErrorS(err, "Failed to get cluster resource placement eviction", "clusterResourcePlacementEviction", evictionName)
		return runtime.Result{}, client.IgnoreNotFound(err)
	}

	validCondition := eviction.GetCondition(string(placementv1alpha1.PlacementEvictionConditionTypeValid))
	if condition.IsConditionStatusFalse(validCondition, eviction.GetGeneration()) {
		klog.V(2).InfoS("Invalid eviction, no need to reconcile", "clusterResourcePlacementEviction", evictionName)
		return runtime.Result{}, nil
	}

	executedCondition := eviction.GetCondition(string(placementv1alpha1.PlacementEvictionConditionTypeExecuted))
	if executedCondition != nil {
		klog.V(2).InfoS("Eviction has executed condition specified, no need to reconcile", "clusterResourcePlacementEviction", evictionName)
		return runtime.Result{}, nil
	}

	isCRPPresent := true
	var crp placementv1beta1.ClusterResourcePlacement
	if err := r.Client.Get(ctx, types.NamespacedName{Name: eviction.Spec.PlacementName}, &crp); err != nil {
		if !errors.IsNotFound(err) {
			return runtime.Result{}, controller.NewAPIServerError(true, err)
		}
		isCRPPresent = false
	}
	if !isCRPPresent {
		klog.V(2).InfoS(evictionInvalidMissingCRPMessage, "clusterResourcePlacementEviction", evictionName, "clusterResourcePlacement", eviction.Spec.PlacementName)
		markEvictionInvalid(&eviction, evictionInvalidMissingCRPMessage)
		return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
	}

	var crbList placementv1beta1.ClusterResourceBindingList
	if err := r.Client.List(ctx, &crbList, client.MatchingLabels{placementv1beta1.CRPTrackingLabel: crp.Name}); err != nil {
		return runtime.Result{}, controller.NewAPIServerError(true, err)
	}

	var evictionTargetBinding *placementv1beta1.ClusterResourceBinding
	for i := range crbList.Items {
		if crbList.Items[i].Spec.TargetCluster == eviction.Spec.ClusterName {
			if evictionTargetBinding == nil {
				evictionTargetBinding = &crbList.Items[i]
			} else {
				klog.V(2).InfoS("Found more than one cluster resource binding for cluster targeted by eviction", "clusterResourcePlacementEviction", evictionName, "clusterResourcePlacement", eviction.Spec.PlacementName)
				markEvictionInvalid(&eviction, "Found more than one scheduler decision for placement in cluster targeted by eviction")
				return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
			}
		}
	}
	if evictionTargetBinding == nil {
		klog.V(2).InfoS("Failed to find cluster resource binding for cluster targeted by eviction", "clusterResourcePlacementEviction", evictionName, "clusterName", eviction.Spec.ClusterName)
		markEvictionInvalid(&eviction, "Failed to find scheduler decision for placement in cluster targeted by eviction")
		return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
	}

	markEvictionValid(&eviction)

	// Check to see if binding is being deleted.
	if evictionTargetBinding.GetDeletionTimestamp() != nil {
		markEvictionExecuted(&eviction, evictionAllowedPlacementRemovedMessage)
		return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
	}

	// Check to see if binding has failed. If so no need to check disruption budget we can evict.
	if bindingutils.HasBindingFailed(evictionTargetBinding) {
		if err := r.deleteClusterResourceBinding(ctx, evictionTargetBinding); err != nil {
			return runtime.Result{}, err
		}
		markEvictionExecuted(&eviction, evictionAllowedPlacementFailedMessage)
		return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
	}

	isDBPresent := true
	var db placementv1alpha1.ClusterResourcePlacementDisruptionBudget
	if err := r.Client.Get(ctx, types.NamespacedName{Name: crp.Name}, &db); err != nil {
		if !errors.IsNotFound(err) {
			return runtime.Result{}, controller.NewAPIServerError(true, err)
		}
		isDBPresent = false
	}

	if !isDBPresent {
		if err := r.deleteClusterResourceBinding(ctx, evictionTargetBinding); err != nil {
			return runtime.Result{}, err
		}
		markEvictionExecuted(&eviction, evictionAllowedNoPDBMessage)
		return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
	}

	bindingList := crbList.Items
	var desiredBindings int
	switch crp.Spec.Policy.PlacementType {
	case placementv1beta1.PickAllPlacementType:
		desiredBindings = calculateSchedulerTargetedBindings(bindingList)
	case placementv1beta1.PickNPlacementType:
		desiredBindings = int(*crp.Spec.Policy.NumberOfClusters)
	case placementv1beta1.PickFixedPlacementType:
		desiredBindings = len(crp.Spec.Policy.ClusterNames)
	}

	totalBindings := len(bindingList)
	allowed, availableBindings := isEvictionAllowed(desiredBindings, bindingList, db)
	if allowed {
		if err := r.deleteClusterResourceBinding(ctx, evictionTargetBinding); err != nil {
			return runtime.Result{}, err
		}
		markEvictionExecuted(&eviction, fmt.Sprintf(evictionAllowedPDBSpecifiedFmt, availableBindings, desiredBindings, totalBindings))
	} else {
		markEvictionNotExecuted(&eviction, fmt.Sprintf(evictionBlockedPDBSpecifiedFmt, availableBindings, desiredBindings, totalBindings))
	}

	return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
}

// updateEvictionStatus updates eviction status.
func (r *Reconciler) updateEvictionStatus(ctx context.Context, eviction *placementv1alpha1.ClusterResourcePlacementEviction) error {
	evictionRef := klog.KObj(eviction)
	if err := r.Client.Status().Update(ctx, eviction); err != nil {
		klog.ErrorS(err, "Failed to update eviction status", "clusterResourcePlacementEviction", evictionRef)
		return controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Updated the status of a eviction", "clusterResourcePlacementEviction", evictionRef)
	return nil
}

// deleteClusterResourceBinding deletes the specified cluster resource binding.
func (r *Reconciler) deleteClusterResourceBinding(ctx context.Context, binding *placementv1beta1.ClusterResourceBinding) error {
	bindingRef := klog.KObj(binding)
	if err := r.Client.Delete(ctx, binding); err != nil {
		klog.ErrorS(err, "Failed to delete cluster resource binding", "clusterResourceBinding", bindingRef)
		return controller.NewDeleteIgnoreNotFoundError(err)
	}
	klog.V(2).InfoS("Issued delete on cluster resource binding, eviction succeeded", "clusterResourceBinding", bindingRef)
	return nil
}

// isEvictionAllowed calculates if eviction allowed based on available bindings and spec specified in placement disruption budget.
func isEvictionAllowed(desiredBindings int, bindings []placementv1beta1.ClusterResourceBinding, db placementv1alpha1.ClusterResourcePlacementDisruptionBudget) (bool, int) {
	availableBindings := 0
	for i := range bindings {
		availableCondition := bindings[i].GetCondition(string(placementv1beta1.ResourceBindingAvailable))
		if condition.IsConditionStatusTrue(availableCondition, bindings[i].GetGeneration()) {
			availableBindings++
		}
	}
	var disruptionsAllowed int
	switch {
	case db.Spec.MaxUnavailable != nil:
		maxUnavailable, _ := intstr.GetScaledValueFromIntOrPercent(db.Spec.MaxUnavailable, desiredBindings, true)
		unavailableBindings := len(bindings) - availableBindings
		disruptionsAllowed = maxUnavailable - unavailableBindings
	case db.Spec.MinAvailable != nil:
		minAvailable, _ := intstr.GetScaledValueFromIntOrPercent(db.Spec.MinAvailable, desiredBindings, true)
		disruptionsAllowed = availableBindings - minAvailable
	}
	if disruptionsAllowed < 0 {
		disruptionsAllowed = 0
	}
	return disruptionsAllowed > 0, availableBindings
}

// calculateSchedulerTargetedBindings returns the count of bindings targeted by the scheduler.
func calculateSchedulerTargetedBindings(bindings []placementv1beta1.ClusterResourceBinding) int {
	schedulerTargetedBindings := 0
	for i := range bindings {
		if bindings[i].Spec.State == placementv1beta1.BindingStateBound || bindings[i].Spec.State == placementv1beta1.BindingStateScheduled {
			schedulerTargetedBindings++
		}
	}
	return schedulerTargetedBindings
}

// markEvictionValid sets the valid condition as true in eviction status.
func markEvictionValid(eviction *placementv1alpha1.ClusterResourcePlacementEviction) {
	cond := metav1.Condition{
		Type:               string(placementv1alpha1.PlacementEvictionConditionTypeValid),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: eviction.Generation,
		Reason:             clusterResourcePlacementEvictionValidReason,
		Message:            evictionValidMessage,
	}
	eviction.SetConditions(cond)

	klog.V(2).InfoS("Marked eviction as valid", "clusterResourcePlacementEviction", klog.KObj(eviction))
}

// markEvictionInvalid sets the valid condition as false in eviction status.
func markEvictionInvalid(eviction *placementv1alpha1.ClusterResourcePlacementEviction, message string) {
	cond := metav1.Condition{
		Type:               string(placementv1alpha1.PlacementEvictionConditionTypeValid),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: eviction.Generation,
		Reason:             clusterResourcePlacementEvictionInvalidReason,
		Message:            message,
	}
	eviction.SetConditions(cond)
	klog.V(2).InfoS("Marked eviction as invalid", "clusterResourcePlacementEviction", klog.KObj(eviction))
}

// markEvictionExecuted sets the executed condition as true in eviction status.
func markEvictionExecuted(eviction *placementv1alpha1.ClusterResourcePlacementEviction, message string) {
	cond := metav1.Condition{
		Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: eviction.Generation,
		Reason:             clusterResourcePlacementEvictionExecutedReason,
		Message:            message,
	}
	eviction.SetConditions(cond)
	klog.V(2).InfoS("Marked eviction as executed", "clusterResourcePlacementEviction", klog.KObj(eviction))
}

// markEvictionNotExecuted sets the executed condition as false in eviction status.
func markEvictionNotExecuted(eviction *placementv1alpha1.ClusterResourcePlacementEviction, message string) {
	cond := metav1.Condition{
		Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: eviction.Generation,
		Reason:             clusterResourcePlacementEvictionNotExecutedReason,
		Message:            message,
	}
	eviction.SetConditions(cond)
	klog.V(2).InfoS("Marked eviction as not executed", "clusterResourcePlacementEviction", klog.KObj(eviction))
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr runtime.Manager) error {
	// TODO(arvind): Increase max concurrent reconcile count, use map in PDB status to keep track of ongoing evictions.
	return runtime.NewControllerManagedBy(mgr).
		WithOptions(ctrl.Options{MaxConcurrentReconciles: 1}). // max concurrent reconciles is currently set to 1 for concurrency control.
		For(&placementv1alpha1.ClusterResourcePlacementEviction{}).
		Complete(r)
}
