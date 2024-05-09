/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package condition provides condition related utils.
package condition

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// A group of condition reason string which is used to populate the placement condition.
const (
	// RolloutStartedUnknownReason is the reason string of placement condition if rollout status is
	// unknown.
	RolloutStartedUnknownReason = "RolloutStartedUnknown"

	// RolloutNotStartedYetReason is the reason string of placement condition if the rollout has not started yet.
	RolloutNotStartedYetReason = "RolloutNotStartedYet"

	// RolloutStartedReason is the reason string of placement condition if rollout status is started.
	RolloutStartedReason = "RolloutStarted"

	// OverriddenPendingReason is the reason string of placement condition when the selected resources are pending to override.
	OverriddenPendingReason = "OverriddenPending"

	// OverrideNotSpecifiedReason is the reason string of placement condition when no override is specified.
	OverrideNotSpecifiedReason = "NoOverrideSpecified"

	// OverriddenFailedReason is the reason string of placement condition when the selected resources fail to be overridden.
	OverriddenFailedReason = "OverriddenFailed"

	// OverriddenSucceededReason is the reason string of placement condition when the selected resources are overridden successfully.
	OverriddenSucceededReason = "OverriddenSucceeded"

	// WorkSynchronizedUnknownReason is the reason string of placement condition when the work is pending to be created
	// or updated.
	WorkSynchronizedUnknownReason = "WorkSynchronizedUnknown"

	// WorkNotSynchronizedYetReason is the reason string of placement condition when not all corresponding works are created
	// or updated in the target cluster's namespace yet.
	WorkNotSynchronizedYetReason = "WorkNotSynchronizedYet"

	// WorkSynchronizedReason is the reason string of placement condition when all corresponding works are created or updated
	// in the target cluster's namespace successfully.
	WorkSynchronizedReason = "WorkSynchronized"

	// ApplyPendingReason is the reason string of placement condition when the selected resources are pending to apply.
	ApplyPendingReason = "ApplyPending"

	// ApplyFailedReason is the reason string of placement condition when the selected resources fail to apply.
	ApplyFailedReason = "ApplyFailed"

	// ApplySucceededReason is the reason string of placement condition when the selected resources are applied successfully.
	ApplySucceededReason = "ApplySucceeded"

	// AvailableUnknownReason is the reason string of placement condition when the availability of selected resources
	// is unknown.
	AvailableUnknownReason = "ResourceAvailableUnknown"

	// NotAvailableYetReason is the reason string of placement condition if the selected resources are not available yet.
	NotAvailableYetReason = "ResourceNotAvailableYet"

	// AvailableReason is the reason string of placement condition if the selected resources are available.
	AvailableReason = "ResourceAvailable"
)

// A group of condition reason string which is used to populate the placement condition per cluster.
const (
	// ScheduleSucceededReason is the reason string of placement condition if scheduling succeeded.
	ScheduleSucceededReason = "Scheduled"

	// AllWorkSyncedReason is the reason string of placement condition if all works are synchronized.
	AllWorkSyncedReason = "AllWorkSynced"

	// SyncWorkFailedReason is the reason string of placement condition if some works failed to synchronize.
	SyncWorkFailedReason = "SyncWorkFailed"

	// WorkNeedSyncedReason is the reason string of placement condition if some works are in the processing of synchronizing.
	WorkNeedSyncedReason = "StillNeedToSyncWork"

	// WorkNotAppliedReason is the reason string of placement condition if some works are not applied.
	WorkNotAppliedReason = "NotAllWorkHaveBeenApplied"

	// AllWorkAppliedReason is the reason string of placement condition if all works are applied.
	AllWorkAppliedReason = "AllWorkHaveBeenApplied"

	// WorkNotAvailableReason is the reason string of placement condition if some works are not available.
	WorkNotAvailableReason = "NotAllWorkAreAvailable"

	// AllWorkAvailableReason is the reason string of placement condition if all works are available.
	AllWorkAvailableReason = "AllWorkAreAvailable"
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

// The following conditions are in ordered.
// Once the placement is scheduled, it will be divided into following stages.
// Used to populate the CRP conditions.
const (
	RolloutStartedCondition ResourceCondition = iota
	OverriddenCondition
	WorkSynchronizedCondition
	AppliedCondition
	AvailableCondition
	TotalCondition
)

func (c ResourceCondition) EventReasonForTrue() string {
	return []string{
		"PlacementRolloutStarted",
		"PlacementOverriddenSucceeded",
		"PlacementWorkSynchronized",
		"PlacementApplied",
		"PlacementAvailable",
	}[c]
}

func (c ResourceCondition) EventMessageForTrue() string {
	return []string{
		"Started rolling out the latest resources",
		"Placement has been successfully overridden",
		"Work(s) have been created or updated successfully for the selected cluster(s)",
		"Resources have been applied to the selected cluster(s)",
		"Resources are available on the selected cluster(s)",
	}[c]
}

// ResourcePlacementConditionType returns the resource condition type per cluster used by cluster resource placement.
func (c ResourceCondition) ResourcePlacementConditionType() fleetv1beta1.ResourcePlacementConditionType {
	return []fleetv1beta1.ResourcePlacementConditionType{
		fleetv1beta1.ResourceRolloutStartedConditionType,
		fleetv1beta1.ResourceOverriddenConditionType,
		fleetv1beta1.ResourceWorkSynchronizedConditionType,
		fleetv1beta1.ResourcesAppliedConditionType,
		fleetv1beta1.ResourcesAvailableConditionType,
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
	}[c]
}

// ClusterResourcePlacementConditionType returns the CRP condition type used by CRP.
func (c ResourceCondition) ClusterResourcePlacementConditionType() fleetv1beta1.ClusterResourcePlacementConditionType {
	return []fleetv1beta1.ClusterResourcePlacementConditionType{
		fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType,
		fleetv1beta1.ClusterResourcePlacementOverriddenConditionType,
		fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType,
		fleetv1beta1.ClusterResourcePlacementAppliedConditionType,
		fleetv1beta1.ClusterResourcePlacementAvailableConditionType,
	}[c]
}

// UnknownResourceConditionPerCluster returns the unknown resource condition.
func (c ResourceCondition) UnknownResourceConditionPerCluster(generation int64) metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
			Reason:             RolloutStartedUnknownReason,
			Message:            "In the process of deciding whether to roll out the latest resources or not",
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
			Reason:             OverriddenPendingReason,
			Message:            "In the process of overriding the selected resources if there is any override defined",
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
			Reason:             WorkSynchronizedUnknownReason,
			Message:            "In the process of creating or updating the work object(s) in the hub cluster",
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
			Reason:             ApplyPendingReason,
			Message:            "In the process of applying the resources on the member cluster",
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourcesAvailableConditionType),
			Reason:             AvailableUnknownReason,
			Message:            "The availability of the selected resources is unknown yet ",
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
			Message:            fmt.Sprintf("Works(s) are succcesfully created or updated in %d target clusters' namespaces", clusterCount),
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
	}[c]
}

type conditionStatus int // internal type to be used when populating the CRP status

const (
	UnknownConditionStatus conditionStatus = iota
	FalseConditionStatus
	TrueConditionStatus
	TotalConditionStatus
)
