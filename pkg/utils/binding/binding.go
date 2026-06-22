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

// nonTerminalBindingFailureReasons is the closed set of `Reason` strings that the per-cluster
// binding conditions (Overridden, WorkSynchronized, Applied, Available) can carry while still
// `Status=False` *without* representing a terminal failure. Each one means the binding is still
// progressing and the controller should keep waiting rather than treat the binding as failed.
//
// The set is intentionally an allowlist: any new `Status=False` reason added to the codebase is
// treated as terminal until it is explicitly added here. This is the safer default because the
// alternative (treat unknown reasons as transient) would stall the rollout controller forever
// on a genuinely-failing binding it does not yet know how to classify.
//
// When you add a new in-progress / pending / "not yet" reason to pkg/utils/condition/reason.go
// for any of the above conditions, also add it here.
var nonTerminalBindingFailureReasons = map[string]struct{}{
	condition.OverriddenPendingReason:      {},
	condition.WorkNotSynchronizedYetReason: {},
	condition.ApplyPendingReason:           {},
	condition.NotAvailableYetReason:        {},
}

// HasBindingFailed reports whether a binding has reached a terminal failure on any of its
// per-cluster conditions (Overridden, WorkSynchronized, Applied, Available).
//
// A condition counts as a terminal failure when its `Status` is `False` *and* its `Reason` is
// not in `nonTerminalBindingFailureReasons`. The Reason check is necessary because several of
// the in-progress states (e.g. `WorkNotSynchronizedYetReason`, `NotAvailableYetReason`) are
// expressed as `Status=False` rather than `Status=Unknown`. Treating those as failures was the
// previous bug — it caused the rollout controller to drop bindings that were still progressing
// out of `canBeReadyBindings`, stalling rollout decisions.
//
// Unknown reasons fall through to "terminal" by design; see the comment on
// `nonTerminalBindingFailureReasons`.
func HasBindingFailed(binding placementv1beta1.BindingObj) bool {
	for i := condition.OverriddenCondition; i <= condition.AvailableCondition; i++ {
		c := binding.GetCondition(string(i.ResourceBindingConditionType()))
		if !condition.IsConditionStatusFalse(c, binding.GetGeneration()) {
			continue
		}
		if _, transient := nonTerminalBindingFailureReasons[c.Reason]; transient {
			klog.V(3).Infof("binding %s has condition %s status false with non-terminal reason %q; treating as in-progress",
				klog.KObj(binding), string(i.ResourceBindingConditionType()), c.Reason)
			continue
		}
		klog.V(2).Infof("binding %s has terminal failure on condition %s (reason %q)",
			klog.KObj(binding), string(i.ResourceBindingConditionType()), c.Reason)
		return true
	}
	return false
}

// IsBindingDiffReported checks if the binding is in diffReported state.
func IsBindingDiffReported(binding placementv1beta1.BindingObj) bool {
	diffReportCondition := binding.GetCondition(string(placementv1beta1.ResourceBindingDiffReported))
	return diffReportCondition != nil && diffReportCondition.ObservedGeneration == binding.GetGeneration()
}
