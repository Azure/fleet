/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacementeviction

import (
	"context"
	"fmt"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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

	evictionInvalidMissingCRPMessage                = "Failed to find ClusterResourcePlacement targeted by eviction"
	evictionInvalidDeletingCRPMessage               = "Found deleting ClusterResourcePlacement targeted by eviction"
	evictionInvalidMissingCRBMessage                = "Failed to find scheduler decision for placement in cluster targeted by eviction"
	evictionInvalidMultipleCRBMessage               = "Found more than one scheduler decision for placement in cluster targeted by eviction"
	evictionValidMessage                            = "Eviction is valid"
	evictionAllowedNoPDBMessage                     = "Eviction Allowed, no ClusterResourcePlacementDisruptionBudget specified"
	evictionAllowedPlacementRemovedMessage          = "Eviction Allowed, placement is currently being removed from cluster targeted by eviction"
	evictionAllowedPlacementFailedMessage           = "Eviction Allowed, placement has failed"
	evictionBlockedMisconfiguredPDBSpecifiedMessage = "Eviction is blocked by Misconfigured ClusterResourcePlacementDisruptionBudget, either MaxUnavailable is specified or MinAvailable is specified as a percentage for PickAll CRP"

	evictionAllowedPDBSpecifiedFmt = "Eviction is allowed by specified ClusterResourcePlacementDisruptionBudget, availablePlacements: %d, totalPlacements: %d"
	evictionBlockedPDBSpecifiedFmt = "Eviction is blocked by specified ClusterResourcePlacementDisruptionBudget, availablePlacements: %d, totalPlacements: %d"
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

	if isEvictionInTerminalState(&eviction) {
		return runtime.Result{}, nil
	}

	// Validated eviction spec.
	validationResult, err := r.validateEviction(ctx, &eviction)
	if err != nil {
		return runtime.Result{}, controller.NewAPIServerError(true, err)
	}
	if !validationResult.isValid {
		return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
	}

	// Eviction is valid.
	markEvictionValid(&eviction)

	// Unwrap validation result for processing.
	crp, evictionTargetBinding, bindingList := validationResult.crp, validationResult.crb, validationResult.bindings

	// Check to see if binding is being deleted.
	if evictionTargetBinding.GetDeletionTimestamp() != nil {
		klog.V(2).InfoS("ClusterResourceBinding targeted by eviction is being deleted",
			"clusterResourcePlacementEviction", evictionName, "clusterResourceBinding", evictionTargetBinding.Name, "targetCluster", eviction.Spec.ClusterName)
		markEvictionExecuted(&eviction, evictionAllowedPlacementRemovedMessage)
		return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
	}

	// Check to see if binding has failed. If so no need to check disruption budget we can evict.
	if bindingutils.HasBindingFailed(evictionTargetBinding) {
		klog.V(2).InfoS("ClusterResourceBinding targeted by eviction is in failed state",
			"clusterResourcePlacementEviction", evictionName, "clusterResourceBinding", evictionTargetBinding.Name, "targetCluster", eviction.Spec.ClusterName)
		if err := r.deleteClusterResourceBinding(ctx, evictionTargetBinding); err != nil {
			return runtime.Result{}, err
		}
		markEvictionExecuted(&eviction, evictionAllowedPlacementFailedMessage)
		return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
	}

	var db placementv1alpha1.ClusterResourcePlacementDisruptionBudget
	if err := r.Client.Get(ctx, types.NamespacedName{Name: crp.Name}, &db); err != nil {
		if k8serrors.IsNotFound(err) {
			if err = r.deleteClusterResourceBinding(ctx, evictionTargetBinding); err != nil {
				return runtime.Result{}, err
			}
			markEvictionExecuted(&eviction, evictionAllowedNoPDBMessage)
			return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
		}
		return runtime.Result{}, controller.NewAPIServerError(true, err)
	}

	// handle special case for PickAll CRP.
	if crp.Spec.Policy.PlacementType == placementv1beta1.PickAllPlacementType {
		if db.Spec.MaxUnavailable != nil || db.Spec.MinAvailable.Type == intstr.String {
			markEvictionNotExecuted(&eviction, evictionBlockedMisconfiguredPDBSpecifiedMessage)
			return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
		}
	}

	totalBindings := len(bindingList)
	allowed, availableBindings := isEvictionAllowed(bindingList, *crp, db)
	if allowed {
		if err := r.deleteClusterResourceBinding(ctx, evictionTargetBinding); err != nil {
			return runtime.Result{}, err
		}
		markEvictionExecuted(&eviction, fmt.Sprintf(evictionAllowedPDBSpecifiedFmt, availableBindings, totalBindings))
	} else {
		markEvictionNotExecuted(&eviction, fmt.Sprintf(evictionBlockedPDBSpecifiedFmt, availableBindings, totalBindings))
	}

	return runtime.Result{}, r.updateEvictionStatus(ctx, &eviction)
}

// validateEviction performs validation for eviction object's spec and returns a wrapped validation result.
func (r *Reconciler) validateEviction(ctx context.Context, eviction *placementv1alpha1.ClusterResourcePlacementEviction) (*evictionValidationResult, error) {
	validationResult := &evictionValidationResult{isValid: false}
	var crp placementv1beta1.ClusterResourcePlacement
	if err := r.Client.Get(ctx, types.NamespacedName{Name: eviction.Spec.PlacementName}, &crp); err != nil {
		if k8serrors.IsNotFound(err) {
			klog.V(2).InfoS(evictionInvalidMissingCRPMessage, "clusterResourcePlacementEviction", eviction.Name, "clusterResourcePlacement", eviction.Spec.PlacementName)
			markEvictionInvalid(eviction, evictionInvalidMissingCRPMessage)
			return validationResult, nil
		}
		return nil, err
	}
	if crp.DeletionTimestamp != nil {
		klog.V(2).InfoS(evictionInvalidDeletingCRPMessage, "clusterResourcePlacementEviction", eviction.Name, "clusterResourcePlacement", eviction.Spec.PlacementName)
		markEvictionInvalid(eviction, evictionInvalidDeletingCRPMessage)
		return validationResult, nil
	}
	validationResult.crp = &crp

	var crbList placementv1beta1.ClusterResourceBindingList
	if err := r.Client.List(ctx, &crbList, client.MatchingLabels{placementv1beta1.CRPTrackingLabel: crp.Name}); err != nil {
		return nil, err
	}
	validationResult.bindings = crbList.Items

	var evictionTargetBinding *placementv1beta1.ClusterResourceBinding
	for i := range crbList.Items {
		if crbList.Items[i].Spec.TargetCluster == eviction.Spec.ClusterName {
			if evictionTargetBinding == nil {
				evictionTargetBinding = &crbList.Items[i]
			} else {
				klog.V(2).InfoS(evictionInvalidMultipleCRBMessage, "clusterResourcePlacementEviction", eviction.Name, "clusterResourcePlacement", eviction.Spec.PlacementName)
				markEvictionInvalid(eviction, evictionInvalidMultipleCRBMessage)
				return validationResult, nil
			}
		}
	}
	if evictionTargetBinding == nil {
		klog.V(2).InfoS("Failed to find cluster resource binding for cluster targeted by eviction", "clusterResourcePlacementEviction", eviction.Name, "targetCluster", eviction.Spec.ClusterName)
		markEvictionInvalid(eviction, evictionInvalidMissingCRBMessage)
		return validationResult, nil
	}
	validationResult.crb = evictionTargetBinding

	validationResult.isValid = true
	return validationResult, nil
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

// isEvictionInTerminalState checks to see if eviction is in a terminal state.
func isEvictionInTerminalState(eviction *placementv1alpha1.ClusterResourcePlacementEviction) bool {
	validCondition := eviction.GetCondition(string(placementv1alpha1.PlacementEvictionConditionTypeValid))
	if condition.IsConditionStatusFalse(validCondition, eviction.GetGeneration()) {
		klog.V(2).InfoS("Invalid eviction, no need to reconcile", "clusterResourcePlacementEviction", eviction.Name)
		return true
	}

	executedCondition := eviction.GetCondition(string(placementv1alpha1.PlacementEvictionConditionTypeExecuted))
	if executedCondition != nil {
		klog.V(2).InfoS("Eviction has executed condition specified, no need to reconcile", "clusterResourcePlacementEviction", eviction.Name)
		return true
	}
	return false
}

// isEvictionAllowed calculates if eviction allowed based on available bindings and spec specified in placement disruption budget.
func isEvictionAllowed(bindings []placementv1beta1.ClusterResourceBinding, crp placementv1beta1.ClusterResourcePlacement, db placementv1alpha1.ClusterResourcePlacementDisruptionBudget) (bool, int) {
	availableBindings := 0
	for i := range bindings {
		availableCondition := bindings[i].GetCondition(string(placementv1beta1.ResourceBindingAvailable))
		if condition.IsConditionStatusTrue(availableCondition, bindings[i].GetGeneration()) {
			availableBindings++
		}
	}

	var desiredBindings int
	placementType := crp.Spec.Policy.PlacementType
	switch placementType {
	case placementv1beta1.PickNPlacementType:
		desiredBindings = int(*crp.Spec.Policy.NumberOfClusters)
	case placementv1beta1.PickFixedPlacementType:
		desiredBindings = len(crp.Spec.Policy.ClusterNames)
	case placementv1beta1.PickAllPlacementType:
		// we don't know the desired bindings for PickAll.
	}

	var disruptionsAllowed int
	switch {
	// For PickAll CRPs, MaxUnavailable won't be specified in DB.
	case db.Spec.MaxUnavailable != nil:
		maxUnavailable, _ := intstr.GetScaledValueFromIntOrPercent(db.Spec.MaxUnavailable, desiredBindings, true)
		unavailableBindings := len(bindings) - availableBindings
		disruptionsAllowed = maxUnavailable - unavailableBindings
	case db.Spec.MinAvailable != nil:
		var minAvailable int
		if placementType == placementv1beta1.PickAllPlacementType {
			// MinAvailable will be an Integer value for PickAll CRP.
			minAvailable = db.Spec.MinAvailable.IntValue()
		} else {
			minAvailable, _ = intstr.GetScaledValueFromIntOrPercent(db.Spec.MinAvailable, desiredBindings, true)
		}
		disruptionsAllowed = availableBindings - minAvailable
	}
	if disruptionsAllowed < 0 {
		disruptionsAllowed = 0
	}
	return disruptionsAllowed > 0, availableBindings
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

type evictionValidationResult struct {
	crp      *placementv1beta1.ClusterResourcePlacement
	crb      *placementv1beta1.ClusterResourceBinding
	bindings []placementv1beta1.ClusterResourceBinding
	isValid  bool
}
