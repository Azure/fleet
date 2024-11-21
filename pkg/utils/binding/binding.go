package binding

import (
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
)

// HasBindingFailed checks if ClusterResourceBinding has failed based on its applied and available conditions.
func HasBindingFailed(binding *placementv1beta1.ClusterResourceBinding) bool {
	appliedCondition := binding.GetCondition(string(placementv1beta1.ResourceBindingApplied))
	availableCondition := binding.GetCondition(string(placementv1beta1.ResourceBindingAvailable))
	if condition.IsConditionStatusFalse(appliedCondition, binding.Generation) || condition.IsConditionStatusFalse(availableCondition, binding.Generation) {
		return true
	}
	return false
}
