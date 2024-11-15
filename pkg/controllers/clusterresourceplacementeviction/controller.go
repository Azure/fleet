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
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

const (
	reasonClusterResourcePlacementEvictionValid       = "ClusterResourcePlacementEvictionValid"
	reasonClusterResourcePlacementEvictionInvalid     = "ClusterResourcePlacementEvictionInvalid"
	reasonClusterResourcePlacementEvictionExecuted    = "ClusterResourcePlacementEvictionExecuted"
	reasonClusterResourcePlacementEvictionNotExecuted = "ClusterResourcePlacementEvictionNotExecuted"

	evictionInvalidMissingCRP  = "Failed to find cluster resource placement targeted by eviction"
	evictionInvalidMissingCRB  = "Failed to find cluster resource binding for cluster targeted by eviction"
	evictionInvalidMultipleCRB = "Found more than one ClusterResourceBinding for cluster targeted by eviction"
	evictionValid              = "Eviction is valid"
	evictionAllowedNoPDB       = "Eviction Allowed, no ClusterResourcePlacementDisruptionBudget specified"

	evictionAllowedPDBSpecified = "Eviction is allowed by specified ClusterResourcePlacementDisruptionBudget, disruptionsAllowed: %d, availableBindings: %d, desiredBindings: %d, totalBindings: %d"
	evictionBlockedPDBSpecified = "Eviction is blocked by specified ClusterResourcePlacementDisruptionBudget, disruptionsAllowed: %d, availableBindings: %d, desiredBindings: %d, totalBindings: %d"
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
			return runtime.Result{}, err
		}
		isCRPPresent = false
	}
	if !isCRPPresent {
		klog.V(2).InfoS(evictionInvalidMissingCRP, "clusterResourcePlacementEviction", evictionName, "clusterResourcePlacement", eviction.Spec.PlacementName)
		markEvictionInvalid(&eviction, evictionInvalidMissingCRP)
		return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
	}

	var crbList placementv1beta1.ClusterResourceBindingList
	if err := r.Client.List(ctx, &crbList, client.MatchingLabels{placementv1beta1.CRPTrackingLabel: crp.Name}); err != nil {
		return runtime.Result{}, err
	}

	var evictionTargetBinding *placementv1beta1.ClusterResourceBinding
	for i := range crbList.Items {
		if crbList.Items[i].Spec.TargetCluster == eviction.Spec.ClusterName {
			if evictionTargetBinding == nil {
				evictionTargetBinding = &crbList.Items[i]
			} else {
				klog.V(2).InfoS(evictionInvalidMultipleCRB, "clusterResourcePlacementEviction", evictionName, "clusterResourcePlacement", eviction.Spec.PlacementName)
				markEvictionInvalid(&eviction, evictionInvalidMultipleCRB)
				return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
			}
		}
	}
	if evictionTargetBinding == nil {
		klog.V(2).InfoS(evictionInvalidMissingCRB, "clusterResourcePlacementEviction", evictionName, "clusterName", eviction.Spec.ClusterName)
		markEvictionInvalid(&eviction, evictionInvalidMissingCRB)
		return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
	}

	markEvictionValid(&eviction)
	isDBPresent := true
	var db placementv1alpha1.ClusterResourcePlacementDisruptionBudget
	if err := r.Client.Get(ctx, types.NamespacedName{Name: crp.Name}, &db); err != nil {
		if !errors.IsNotFound(err) {
			return runtime.Result{}, err
		}
		isDBPresent = false
	}

	if !isDBPresent {
		if err := r.deleteClusterResourceBinding(ctx, evictionTargetBinding); err != nil {
			return runtime.Result{}, err
		}
		markEvictionExecuted(&eviction, evictionAllowedNoPDB)
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
	allowed, disruptionsAllowed, availableBindings := isEvictionAllowed(desiredBindings, bindingList, db)
	if allowed {
		if err := r.deleteClusterResourceBinding(ctx, evictionTargetBinding); err != nil {
			return runtime.Result{}, err
		}
		markEvictionExecuted(&eviction, fmt.Sprintf(evictionAllowedPDBSpecified, disruptionsAllowed, availableBindings, desiredBindings, totalBindings))
	} else {
		markEvictionNotExecuted(&eviction, fmt.Sprintf(evictionBlockedPDBSpecified, disruptionsAllowed, availableBindings, desiredBindings, totalBindings))
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
func isEvictionAllowed(desiredBindings int, bindings []placementv1beta1.ClusterResourceBinding, db placementv1alpha1.ClusterResourcePlacementDisruptionBudget) (bool, int, int) {
	availableBindings := 0
	for i := range bindings {
		availableCondition := bindings[i].GetCondition(string(placementv1beta1.ResourceBindingAvailable))
		if condition.IsConditionStatusTrue(availableCondition, bindings[i].GetGeneration()) {
			availableBindings++
		}
	}
	var disruptionsAllowed int
	if db.Spec.MaxUnavailable != nil {
		maxUnavailable, _ := intstr.GetScaledValueFromIntOrPercent(db.Spec.MaxUnavailable, desiredBindings, true)
		unavailableBindings := len(bindings) - availableBindings
		disruptionsAllowed = maxUnavailable - unavailableBindings
	}
	if db.Spec.MinAvailable != nil {
		minAvailable, _ := intstr.GetScaledValueFromIntOrPercent(db.Spec.MinAvailable, desiredBindings, true)
		disruptionsAllowed = availableBindings - minAvailable
	}
	if disruptionsAllowed < 0 {
		disruptionsAllowed = 0
	}
	return disruptionsAllowed > 0, disruptionsAllowed, availableBindings
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
		Reason:             reasonClusterResourcePlacementEvictionValid,
		Message:            evictionValid,
	}
	eviction.SetConditions(cond)
}

// markEvictionInvalid sets the valid condition as false in eviction status.
func markEvictionInvalid(eviction *placementv1alpha1.ClusterResourcePlacementEviction, message string) {
	cond := metav1.Condition{
		Type:               string(placementv1alpha1.PlacementEvictionConditionTypeValid),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: eviction.Generation,
		Reason:             reasonClusterResourcePlacementEvictionInvalid,
		Message:            message,
	}
	eviction.SetConditions(cond)
}

// markEvictionExecuted sets the executed condition as true in eviction status.
func markEvictionExecuted(eviction *placementv1alpha1.ClusterResourcePlacementEviction, message string) {
	cond := metav1.Condition{
		Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: eviction.Generation,
		Reason:             reasonClusterResourcePlacementEvictionExecuted,
		Message:            message,
	}
	eviction.SetConditions(cond)
}

// markEvictionNotExecuted sets the executed condition as false in eviction status.
func markEvictionNotExecuted(eviction *placementv1alpha1.ClusterResourcePlacementEviction, message string) {
	cond := metav1.Condition{
		Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: eviction.Generation,
		Reason:             reasonClusterResourcePlacementEvictionNotExecuted,
		Message:            message,
	}
	eviction.SetConditions(cond)
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr runtime.Manager) error {
	return runtime.NewControllerManagedBy(mgr).
		WithOptions(ctrl.Options{MaxConcurrentReconciles: 1}). // set the max number of concurrent reconciles
		For(&placementv1alpha1.ClusterResourcePlacementEviction{}).
		Complete(r)
}
