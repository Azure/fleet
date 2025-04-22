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

// IsBindingDiffReported checks if the binding is in diffReported state.
func IsBindingDiffReported(binding *placementv1beta1.ClusterResourceBinding) bool {
	diffReportCondition := binding.GetCondition(string(placementv1beta1.ResourceBindingDiffReported))
	return diffReportCondition != nil && diffReportCondition.ObservedGeneration == binding.Generation
}
