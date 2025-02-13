/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package binding

import (
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
)

// HasBindingFailed checks if ClusterResourceBinding has failed based on its applied and available conditions.
func HasBindingFailed(binding *placementv1beta1.ClusterResourceBinding) bool {
	appliedCondition := binding.GetCondition(string(placementv1beta1.ResourceBindingApplied))
	availableCondition := binding.GetCondition(string(placementv1beta1.ResourceBindingAvailable))
	// TODO: parse the reason of the condition to see if the failure is recoverable/retriable or not
	if condition.IsConditionStatusFalse(appliedCondition, binding.Generation) || condition.IsConditionStatusFalse(availableCondition, binding.Generation) {
		return true
	}
	return false
}

// IsBindingDiffReported checks if the binding is in diffReported state.
func IsBindingDiffReported(binding *placementv1beta1.ClusterResourceBinding) bool {
	diffReportCondition := binding.GetCondition(string(placementv1beta1.ResourceBindingDiffReported))
	return diffReportCondition != nil && diffReportCondition.ObservedGeneration == binding.Generation
}
