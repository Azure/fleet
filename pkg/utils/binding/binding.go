/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package binding

import (
	"k8s.io/klog/v2"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
)

// HasBindingFailed checks if ClusterResourceBinding has failed based on its conditions.
func HasBindingFailed(binding *placementv1beta1.ClusterResourceBinding) bool {
	for i := condition.OverriddenCondition; i <= condition.AvailableCondition; i++ {
		if condition.IsConditionStatusFalse(binding.GetCondition(string(i.ResourceBindingConditionType())), binding.Generation) {
			// TODO: parse the reason of the condition to see if the failure is recoverable/retriable or not
			klog.V(2).Infof("binding %s has condition %s with status false", binding.Name, string(i.ResourceBindingConditionType()))
			return true
		}
	}
	return false
}
