/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package eviction

import (
	"k8s.io/klog/v2"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
)

// IsEvictionInTerminalState checks to see if eviction is in a terminal state.
func IsEvictionInTerminalState(eviction *placementv1beta1.ClusterResourcePlacementEviction) bool {
	if validCondition := eviction.GetCondition(string(placementv1beta1.PlacementEvictionConditionTypeValid)); condition.IsConditionStatusFalse(validCondition, eviction.GetGeneration()) {
		klog.V(2).InfoS("Invalid eviction, no need to reconcile", "clusterResourcePlacementEviction", eviction.Name)
		return true
	}

	if executedCondition := eviction.GetCondition(string(placementv1beta1.PlacementEvictionConditionTypeExecuted)); executedCondition != nil {
		klog.V(2).InfoS("Eviction has executed condition specified, no need to reconcile", "clusterResourcePlacementEviction", eviction.Name)
		return true
	}
	return false
}

// IsPlacementPresent checks to see if placement on target cluster could be present.
func IsPlacementPresent(binding *placementv1beta1.ClusterResourceBinding) bool {
	if binding.Spec.State == placementv1beta1.BindingStateBound {
		return true
	}
	if binding.Spec.State == placementv1beta1.BindingStateUnscheduled {
		currentAnnotation := binding.GetAnnotations()
		previousState, exist := currentAnnotation[placementv1beta1.PreviousBindingStateAnnotation]
		if exist && placementv1beta1.BindingState(previousState) == placementv1beta1.BindingStateBound {
			return true
		}
	}
	return false
}
