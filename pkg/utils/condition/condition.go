/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package condition provides condition related utils.
package condition

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A group of condition reason string which is used to populate the placement condition.
const (
	// ScheduleSucceededReason is the reason string of placement condition if scheduling succeeded.
	ScheduleSucceededReason = "Scheduled"

	// RolloutStartedUnknownReason is the reason string of placement condition if rollout status is
	// unknown.
	RolloutStartedUnknownReason = "RolloutStartedUnknown"

	// RolloutNotStartedYetReason is the reason string of placement condition if the rollout has not started yet.
	RolloutNotStartedYetReason = "RolloutNotStartedYet"

	// RolloutStartedReason is the reason string of placement condition if rollout status is started.
	RolloutStartedReason = "RolloutStarted"

	// OverriddenPendingReason is the reason string of placement condition when the selected resources are pending to override.
	OverriddenPendingReason = "OverriddenPending"

	// OverriddenFailedReason is the reason string of placement condition when the selected resources fail to be overridden.
	OverriddenFailedReason = "OverriddenFailed"

	// OverriddenSucceededReason is the reason string of placement condition when the selected resources are overridden successfully.
	OverriddenSucceededReason = "OverriddenSucceeded"

	// WorkCreatedUnknownReason is the reason string of placement condition when the work is pending to be created.
	WorkCreatedUnknownReason = "WorkCreatedUnknown"

	// WorkNotCreatedYetReason is the reason string of placement condition when not all corresponding works are created
	// in the target cluster's namespace yet.
	WorkNotCreatedYetReason = "WorkNotCreatedYet"

	// WorkCreatedReason is the reason string of placement condition when all corresponding works are created in the target
	// cluster's namespace successfully.
	WorkCreatedReason = "OverriddenSucceeded"

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
