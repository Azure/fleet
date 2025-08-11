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

// Package condition provides condition related utils.
package condition

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// EqualCondition compares one condition with another; it ignores the LastTransitionTime and Message fields,
// and will consider the ObservedGeneration values from the two conditions a match if the current
// condition is newer.
func EqualCondition(current, desired *metav1.Condition) bool {
	if current == nil && desired == nil {
		return true
	}
	return current != nil &&
		desired != nil &&
		current.Type == desired.Type &&
		current.Status == desired.Status &&
		current.Reason == desired.Reason &&
		current.ObservedGeneration >= desired.ObservedGeneration
}

// EqualConditionIgnoreReason compares one condition with another; it ignores the Reason, LastTransitionTime, and
// Message fields, and will consider the ObservedGeneration values from the two conditions a match if the current
// condition is newer.
func EqualConditionIgnoreReason(current, desired *metav1.Condition) bool {
	if current == nil && desired == nil {
		return true
	}

	return current != nil &&
		desired != nil &&
		current.Type == desired.Type &&
		current.Status == desired.Status &&
		current.ObservedGeneration >= desired.ObservedGeneration
}

// IsConditionStatusTrue returns true if the condition is true and the observed generation matches the latest generation.
func IsConditionStatusTrue(cond *metav1.Condition, latestGeneration int64) bool {
	return cond != nil && cond.Status == metav1.ConditionTrue && cond.ObservedGeneration == latestGeneration
}

// IsConditionStatusFalse returns true if the condition is false and the observed generation matches the latest generation.
func IsConditionStatusFalse(cond *metav1.Condition, latestGeneration int64) bool {
	return cond != nil && cond.Status == metav1.ConditionFalse && cond.ObservedGeneration == latestGeneration
}

// ResourceCondition is all the resource related condition, for example, scheduled condition is not included.
type ResourceCondition int

// The full set of condition types that Fleet will populate on CRPs (the CRP itself and the
// resource placement status per cluster) and cluster resource bindings.
//
//   - RolloutStarted, Overridden and WorkSynchronized apply to all objects;
//   - Applied and Available apply only when the apply strategy in use is of the ClientSideApply
//     and ServerSideApply type;
//   - DiffReported applies only the apply strategy in use is of the ReportDiff type.
//   - Total is a end marker (not used).
const (
	RolloutStartedCondition ResourceCondition = iota
	OverriddenCondition
	WorkSynchronizedCondition
	AppliedCondition
	AvailableCondition
	DiffReportedCondition
	TotalCondition
)

var (
	// Different set of condition types that Fleet will populate in sequential order based on the
	// apply strategy in use.
	CondTypesForApplyStrategies = []ResourceCondition{
		RolloutStartedCondition,
		OverriddenCondition,
		WorkSynchronizedCondition,
		AppliedCondition,
		AvailableCondition,
	}

	CondTypesForReportDiffApplyStrategy = []ResourceCondition{
		RolloutStartedCondition,
		OverriddenCondition,
		WorkSynchronizedCondition,
		DiffReportedCondition,
	}
)

func (c ResourceCondition) EventReasonForTrue() string {
	return []string{
		"PlacementRolloutStarted",
		"PlacementOverriddenSucceeded",
		"PlacementWorkSynchronized",
		"PlacementApplied",
		"PlacementAvailable",
		"PlacementDiffReported",
	}[c]
}

func (c ResourceCondition) EventMessageForTrue() string {
	return []string{
		"Started rolling out the latest resources",
		"Placement has been successfully overridden",
		"Work(s) have been created or updated successfully for the selected cluster(s)",
		"Resources have been applied to the selected cluster(s)",
		"Resources are available on the selected cluster(s)",
		"Configuration differences have been reported on the selected cluster(s)",
	}[c]
}

// PerClusterPlacementConditionType returns the resource condition type per cluster used by a placement.
func (c ResourceCondition) PerClusterPlacementConditionType() fleetv1beta1.PerClusterPlacementConditionType {
	return []fleetv1beta1.PerClusterPlacementConditionType{
		fleetv1beta1.PerClusterRolloutStartedConditionType,
		fleetv1beta1.PerClusterOverriddenConditionType,
		fleetv1beta1.PerClusterWorkSynchronizedConditionType,
		fleetv1beta1.PerClusterAppliedConditionType,
		fleetv1beta1.PerClusterAvailableConditionType,
		fleetv1beta1.PerClusterDiffReportedConditionType,
	}[c]
}

// ResourceBindingConditionType returns the binding condition type used by cluster resource binding.
func (c ResourceCondition) ResourceBindingConditionType() fleetv1beta1.ResourceBindingConditionType {
	return []fleetv1beta1.ResourceBindingConditionType{
		fleetv1beta1.ResourceBindingRolloutStarted,
		fleetv1beta1.ResourceBindingOverridden,
		fleetv1beta1.ResourceBindingWorkSynchronized,
		fleetv1beta1.ResourceBindingApplied,
		fleetv1beta1.ResourceBindingAvailable,
		fleetv1beta1.ResourceBindingDiffReported,
	}[c]
}

// ClusterResourcePlacementConditionType returns the CRP condition type used by CRP controller.
func (c ResourceCondition) ClusterResourcePlacementConditionType() fleetv1beta1.ClusterResourcePlacementConditionType {
	return []fleetv1beta1.ClusterResourcePlacementConditionType{
		fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType,
		fleetv1beta1.ClusterResourcePlacementOverriddenConditionType,
		fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType,
		fleetv1beta1.ClusterResourcePlacementAppliedConditionType,
		fleetv1beta1.ClusterResourcePlacementAvailableConditionType,
		fleetv1beta1.ClusterResourcePlacementDiffReportedConditionType,
	}[c]
}

// ResourcePlacementConditionType returns the RP condition type used by RP controller.
func (c ResourceCondition) ResourcePlacementConditionType() fleetv1beta1.ResourcePlacementConditionType {
	return []fleetv1beta1.ResourcePlacementConditionType{
		fleetv1beta1.ResourcePlacementRolloutStartedConditionType,
		fleetv1beta1.ResourcePlacementOverriddenConditionType,
		fleetv1beta1.ResourcePlacementWorkSynchronizedConditionType,
		fleetv1beta1.ResourcePlacementAppliedConditionType,
		fleetv1beta1.ResourcePlacementAvailableConditionType,
		fleetv1beta1.ResourcePlacementDiffReportedConditionType,
	}[c]
}

// UnknownPerClusterCondition returns the unknown resource condition.
func (c ResourceCondition) UnknownPerClusterCondition(generation int64) metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
			Reason:             RolloutStartedUnknownReason,
			Message:            "In the process of deciding whether to roll out some version of the resources or not",
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
			Reason:             OverriddenPendingReason,
			Message:            "In the process of overriding the selected resources if there is any override defined",
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
			Reason:             WorkSynchronizedUnknownReason,
			Message:            "In the process of creating or updating the work object(s) in the hub cluster",
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
			Reason:             ApplyPendingReason,
			Message:            "In the process of applying the resources on the member cluster",
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
			Reason:             AvailableUnknownReason,
			Message:            "The availability of the selected resources is unknown yet ",
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.PerClusterDiffReportedConditionType),
			Reason:             DiffReportedStatusUnknownReason,
			Message:            "Diff reporting has just started; its status is not yet to be known",
			ObservedGeneration: generation,
		},
	}[c]
}

// UnknownClusterResourcePlacementCondition returns the unknown cluster resource placement condition.
func (c ResourceCondition) UnknownClusterResourcePlacementCondition(generation int64, clusterCount int) metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
			Reason:             RolloutStartedUnknownReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of deciding whether to roll out the latest resources or not", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
			Reason:             OverriddenPendingReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of overriding the selected resources if there is any override defined", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
			Reason:             WorkSynchronizedUnknownReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of creating or updating the work object(s) in the hub cluster", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
			Reason:             ApplyPendingReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of applying the resources on the member cluster", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
			Reason:             AvailableUnknownReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of checking the availability of the selected resources", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ClusterResourcePlacementDiffReportedConditionType),
			Reason:             DiffReportedStatusUnknownReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of checking for configuration differences", clusterCount),
			ObservedGeneration: generation,
		},
	}[c]
}

// FalseClusterResourcePlacementCondition returns the false cluster resource placement condition.
func (c ResourceCondition) FalseClusterResourcePlacementCondition(generation int64, clusterCount int) metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
			Reason:             RolloutNotStartedYetReason,
			Message:            fmt.Sprintf("The rollout is being blocked by the rollout strategy in %d cluster(s)", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
			Reason:             OverriddenFailedReason,
			Message:            fmt.Sprintf("Failed to override resources in %d cluster(s)", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
			Reason:             WorkNotSynchronizedYetReason,
			Message:            fmt.Sprintf("There are %d cluster(s) which have not finished creating or updating work(s) yet", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
			Reason:             ApplyFailedReason,
			Message:            fmt.Sprintf("Failed to apply resources to %d cluster(s), please check the `failedPlacements` status", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
			Reason:             NotAvailableYetReason,
			Message:            fmt.Sprintf("The selected resources in %d cluster(s) are still not available yet", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ClusterResourcePlacementDiffReportedConditionType),
			Reason:             DiffReportedStatusFalseReason,
			Message:            fmt.Sprintf("Diff reporting in %d clusters is still in progress or has failed", clusterCount),
			ObservedGeneration: generation,
		},
	}[c]
}

// TrueClusterResourcePlacementCondition returns the true cluster resource placement condition.
func (c ResourceCondition) TrueClusterResourcePlacementCondition(generation int64, clusterCount int) metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
			Reason:             RolloutStartedReason,
			Message:            fmt.Sprintf("All %d cluster(s) start rolling out the latest resource", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
			Reason:             OverriddenSucceededReason,
			Message:            fmt.Sprintf("The selected resources are successfully overridden in %d cluster(s)", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
			Reason:             WorkSynchronizedReason,
			Message:            fmt.Sprintf("Works(s) are succcesfully created or updated in %d target cluster(s)' namespaces", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
			Reason:             ApplySucceededReason,
			Message:            fmt.Sprintf("The selected resources are successfully applied to %d cluster(s)", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
			Reason:             AvailableReason,
			Message:            fmt.Sprintf("The selected resources in %d cluster(s) are available now", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementDiffReportedConditionType),
			Reason:             DiffReportedStatusTrueReason,
			Message:            fmt.Sprintf("Diff reporting in %d cluster(s) has been completed", clusterCount),
			ObservedGeneration: generation,
		},
	}[c]
}

// UnknownResourcePlacementCondition returns the unknown resource placement condition.
func (c ResourceCondition) UnknownResourcePlacementCondition(generation int64, clusterCount int) metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourcePlacementRolloutStartedConditionType),
			Reason:             RolloutStartedUnknownReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of deciding whether to roll out the latest resources or not", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourcePlacementOverriddenConditionType),
			Reason:             OverriddenPendingReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of overriding the selected resources if there is any override defined", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourcePlacementWorkSynchronizedConditionType),
			Reason:             WorkSynchronizedUnknownReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of creating or updating the work object(s) in the hub cluster", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourcePlacementAppliedConditionType),
			Reason:             ApplyPendingReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of applying the resources on the member cluster", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourcePlacementAvailableConditionType),
			Reason:             AvailableUnknownReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of checking the availability of the selected resources", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourcePlacementDiffReportedConditionType),
			Reason:             DiffReportedStatusUnknownReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of checking for configuration differences", clusterCount),
			ObservedGeneration: generation,
		},
	}[c]
}

// FalseResourcePlacementCondition returns the false resource placement condition.
func (c ResourceCondition) FalseResourcePlacementCondition(generation int64, clusterCount int) metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourcePlacementRolloutStartedConditionType),
			Reason:             RolloutNotStartedYetReason,
			Message:            fmt.Sprintf("The rollout is being blocked by the rollout strategy in %d cluster(s)", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourcePlacementOverriddenConditionType),
			Reason:             OverriddenFailedReason,
			Message:            fmt.Sprintf("Failed to override resources in %d cluster(s)", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourcePlacementWorkSynchronizedConditionType),
			Reason:             WorkNotSynchronizedYetReason,
			Message:            fmt.Sprintf("There are %d cluster(s) which have not finished creating or updating work(s) yet", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourcePlacementAppliedConditionType),
			Reason:             ApplyFailedReason,
			Message:            fmt.Sprintf("Failed to apply resources to %d cluster(s), please check the `failedPlacements` status", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourcePlacementAvailableConditionType),
			Reason:             NotAvailableYetReason,
			Message:            fmt.Sprintf("The selected resources in %d cluster(s) are still not available yet", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourcePlacementDiffReportedConditionType),
			Reason:             DiffReportedStatusFalseReason,
			Message:            fmt.Sprintf("Diff reporting in %d clusters is still in progress or has failed", clusterCount),
			ObservedGeneration: generation,
		},
	}[c]
}

// TrueResourcePlacementCondition returns the true resource placement condition.
func (c ResourceCondition) TrueResourcePlacementCondition(generation int64, clusterCount int) metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourcePlacementRolloutStartedConditionType),
			Reason:             RolloutStartedReason,
			Message:            fmt.Sprintf("All %d cluster(s) start rolling out the latest resource", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourcePlacementOverriddenConditionType),
			Reason:             OverriddenSucceededReason,
			Message:            fmt.Sprintf("The selected resources are successfully overridden in %d cluster(s)", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourcePlacementWorkSynchronizedConditionType),
			Reason:             WorkSynchronizedReason,
			Message:            fmt.Sprintf("Works(s) are succcesfully created or updated in %d target cluster(s)' namespaces", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourcePlacementAppliedConditionType),
			Reason:             ApplySucceededReason,
			Message:            fmt.Sprintf("The selected resources are successfully applied to %d cluster(s)", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourcePlacementAvailableConditionType),
			Reason:             AvailableReason,
			Message:            fmt.Sprintf("The selected resources in %d cluster(s) are available now", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourcePlacementDiffReportedConditionType),
			Reason:             DiffReportedStatusTrueReason,
			Message:            fmt.Sprintf("Diff reporting in %d cluster(s) has been completed", clusterCount),
			ObservedGeneration: generation,
		},
	}[c]
}

type conditionStatus int // internal type to be used when populating the CRP status

const (
	UnknownConditionStatus conditionStatus = iota
	FalseConditionStatus
	TrueConditionStatus
	TotalConditionStatus
)
