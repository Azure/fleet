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

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// TO-DO (chenyu1): move condition reasons in use by the work applier here.

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

	// DiffReportedStatusUnknownReason is the reason string of the DiffReported condition when the
	// diff reporting has just started and its status is not yet to be known.
	DiffReportedStatusUnknownReason = "DiffReportingPending"

	// DiffReportedStatusFalseReason is the reason string of the DiffReported condition when the
	// diff reporting has not been fully completed.
	DiffReportedStatusFalseReason = "DiffReportingIncompleteOrFailed"

	// DiffReportedStatusTrueReason is the reason string of the DiffReported condition when the
	// diff reporting has been fully completed.
	DiffReportedStatusTrueReason = "DiffReportingCompleted"

	// TODO: Add a user error reason
)

// A group of condition reason string which is used to populate the placement condition per cluster.
const (
	// ScheduleSucceededReason is the reason string of placement condition if scheduling succeeded.
	ScheduleSucceededReason = "Scheduled"

	// AllWorkSyncedReason is the reason string of placement condition if all works are synchronized.
	AllWorkSyncedReason = "AllWorkSynced"

	// SyncWorkFailedReason is the reason string of placement condition if some works failed to synchronize.
	SyncWorkFailedReason = "SyncWorkFailed"

	// WorkApplyInProcess is the reason string of placement condition if works are just synchronized and in the process of being applied.
	WorkApplyInProcess = "ApplyInProgress"

	// WorkDiffReportInProcess is the reason string of placement condition if works are just synchronized and diff reporting is in progress.
	WorkDiffReportInProcess = "DiffReportInProgress"

	// WorkNotAppliedReason is the reason string of placement condition if some works are not applied.
	WorkNotAppliedReason = "NotAllWorkHaveBeenApplied"

	// AllWorkAppliedReason is the reason string of placement condition if all works are applied.
	AllWorkAppliedReason = "AllWorkHaveBeenApplied"

	// WorkNotAvailableReason is the reason string of placement condition if some works are not available.
	WorkNotAvailableReason = "NotAllWorkAreAvailable"

	// WorkNotAvailabilityTrackableReason is the reason string of placement condition if some works are not trackable for availability.
	WorkNotAvailabilityTrackableReason = "NotAllWorkAreAvailabilityTrackable"

	// AllWorkAvailableReason is the reason string of placement condition if all works are available.
	AllWorkAvailableReason = "AllWorkAreAvailable"

	// AllWorkDiffReportedReason is the reason string of placement condition if all works have diff reported.
	AllWorkDiffReportedReason = "AllWorkHaveDiffReported"

	// WorkNotDiffReportedReason is the reason string of placement condition if some works failed to have diff reported.
	WorkNotDiffReportedReason = "NotAllWorkHaveDiffReported"
)

// A group of condition reason string which is used t populate the ClusterStagedUpdateRun condition.
const (
	// UpdateRunInitializeSucceededReason is the reason string of condition if the update run is initialized successfully.
	UpdateRunInitializeSucceededReason = "UpdateRunInitializedSuccessfully"

	// UpdateRunInitializeFailedReason is the reason string of condition if the update run is failed to initialize.
	UpdateRunInitializeFailedReason = "UpdateRunInitializedFailed"

	// UpdateRunProgressingReason is the reason string of condition if the staged update run is progressing.
	UpdateRunProgressingReason = "UpdateRunProgressing"

	// UpdateRunFailedReason is the reason string of condition if the staged update run failed.
	UpdateRunFailedReason = "UpdateRunFailed"

	// UpdateRunStuckReason is the reason string of condition if the staged update run is stuck waiting for a cluster to be updated.
	UpdateRunStuckReason = "UpdateRunStuck"

	// UpdateRunWaitingReason is the reason string of condition if the staged update run is waiting for an after-stage task to complete.
	UpdateRunWaitingReason = "UpdateRunWaiting"

	// UpdateRunSucceededReason is the reason string of condition if the staged update run succeeded.
	UpdateRunSucceededReason = "UpdateRunSucceeded"

	// StageUpdatingStartedReason is the reason string of condition if the stage updating has started.
	StageUpdatingStartedReason = "StageUpdatingStarted"

	// StageUpdatingWaitingReason is the reason string of condition if the stage updating is waiting.
	StageUpdatingWaitingReason = "StageUpdatingWaiting"

	// StageUpdatingFailedReason is the reason string of condition if the stage updating failed.
	StageUpdatingFailedReason = "StageUpdatingFailed"

	// StageUpdatingSucceededReason is the reason string of condition if the stage updating succeeded.
	StageUpdatingSucceededReason = "StageUpdatingSucceeded"

	// ClusterUpdatingStartedReason is the reason string of condition if the cluster updating has started.
	ClusterUpdatingStartedReason = "ClusterUpdatingStarted"

	// ClusterUpdatingFailedReason is the reason string of condition if the cluster updating failed.
	ClusterUpdatingFailedReason = "ClusterUpdatingFailed"

	// ClusterUpdatingSucceededReason is the reason string of condition if the cluster updating succeeded.
	ClusterUpdatingSucceededReason = "ClusterUpdatingSucceeded"

	// AfterStageTaskApprovalRequestApprovedReason is the reason string of condition if the approval request for after stage task has been approved.
	AfterStageTaskApprovalRequestApprovedReason = "AfterStageTaskApprovalRequestApproved"

	// AfterStageTaskApprovalRequestCreatedReason is the reason string of condition if the approval request for after stage task has been created.
	AfterStageTaskApprovalRequestCreatedReason = "AfterStageTaskApprovalRequestCreated"

	// AfterStageTaskWaitTimeElapsedReason is the reason string of condition if the wait time for after stage task has elapsed.
	AfterStageTaskWaitTimeElapsedReason = "AfterStageTaskWaitTimeElapsed"

	// ApprovalRequestApprovalAcceptedReason is the reason string of condition if the approval of the approval request has been accepted.
	ApprovalRequestApprovalAcceptedReason = "ApprovalRequestApprovalAccepted"
)

// A group of condition reason & message string which is used to populate the ClusterResourcePlacementEviction condition.
const (
	// ClusterResourcePlacementEvictionValidReason is the reason string of condition if the eviction is valid.
	ClusterResourcePlacementEvictionValidReason = "ClusterResourcePlacementEvictionValid"

	// ClusterResourcePlacementEvictionInvalidReason is the reason string of condition if the eviction is invalid.
	ClusterResourcePlacementEvictionInvalidReason = "ClusterResourcePlacementEvictionInvalid"

	// ClusterResourcePlacementEvictionExecutedReason is the reason string of condition if the eviction is executed.
	ClusterResourcePlacementEvictionExecutedReason = "ClusterResourcePlacementEvictionExecuted"

	// ClusterResourcePlacementEvictionNotExecutedReason is the reason string of condition if the eviction is not executed.
	ClusterResourcePlacementEvictionNotExecutedReason = "ClusterResourcePlacementEvictionNotExecuted"

	// EvictionInvalidMissingCRPMessage is the message string of invalid eviction condition when CRP is missing.
	EvictionInvalidMissingCRPMessage = "Failed to find ClusterResourcePlacement targeted by eviction"

	// EvictionInvalidDeletingCRPMessage is the message string of invalid eviction condition when CRP is deleting.
	EvictionInvalidDeletingCRPMessage = "Found deleting ClusterResourcePlacement targeted by eviction"

	// EvictionInvalidPickFixedCRPMessage is the message string of invalid eviction condition when CRP placement type is PickFixed.
	EvictionInvalidPickFixedCRPMessage = "Found ClusterResourcePlacement with PickFixed placement type targeted by eviction"

	// EvictionInvalidMissingCRBMessage is the message string of invalid eviction condition when CRB is missing.
	EvictionInvalidMissingCRBMessage = "Failed to find scheduler decision for placement in cluster targeted by eviction"

	// EvictionInvalidMultipleCRBMessage is the message string of invalid eviction condition when more than one CRB is present for cluster targeted by eviction.
	EvictionInvalidMultipleCRBMessage = "Found more than one scheduler decision for placement in cluster targeted by eviction"

	// EvictionValidMessage is the message string of valid eviction condition.
	EvictionValidMessage = "Eviction is valid"

	// EvictionAllowedNoPDBMessage is the message string for executed condition when no PDB is specified.
	EvictionAllowedNoPDBMessage = "Eviction is allowed, no ClusterResourcePlacementDisruptionBudget specified"

	// EvictionAllowedPlacementRemovedMessage is the message string for executed condition when CRB targeted by eviction is being deleted.
	EvictionAllowedPlacementRemovedMessage = "Eviction is allowed, resources propagated by placement is currently being removed from cluster targeted by eviction"

	// EvictionAllowedPlacementFailedMessage is the message string for executed condition when placed resources have failed to apply or the resources are not available.
	EvictionAllowedPlacementFailedMessage = "Eviction is allowed, placement has failed"

	// EvictionBlockedMisconfiguredPDBSpecifiedMessage is the message string for not executed condition when PDB specified is misconfigured for PickAll CRP.
	EvictionBlockedMisconfiguredPDBSpecifiedMessage = "Eviction is blocked by misconfigured ClusterResourcePlacementDisruptionBudget, either MaxUnavailable is specified or MinAvailable is specified as a percentage for PickAll ClusterResourcePlacement"

	// EvictionBlockedMissingPlacementMessage is the message string for not executed condition when resources are yet to be placed in targeted cluster by eviction.
	EvictionBlockedMissingPlacementMessage = "Eviction is blocked, placement has not propagated resources to target cluster yet"

	// EvictionAllowedPDBSpecifiedMessageFmt is the message format for executed condition when eviction is allowed by PDB specified.
	EvictionAllowedPDBSpecifiedMessageFmt = "Eviction is allowed by specified ClusterResourcePlacementDisruptionBudget, availablePlacements: %d, totalPlacements: %d"

	// EvictionBlockedPDBSpecifiedMessageFmt is the message format for not executed condition when eviction is blocked bt PDB specified.
	EvictionBlockedPDBSpecifiedMessageFmt = "Eviction is blocked by specified ClusterResourcePlacementDisruptionBudget, availablePlacements: %d, totalPlacements: %d"
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
	CondTypesForClientSideServerSideApplyStrategies = []ResourceCondition{
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

// ResourcePlacementConditionType returns the resource condition type per cluster used by cluster resource placement.
func (c ResourceCondition) ResourcePlacementConditionType() fleetv1beta1.ResourcePlacementConditionType {
	return []fleetv1beta1.ResourcePlacementConditionType{
		fleetv1beta1.ResourceRolloutStartedConditionType,
		fleetv1beta1.ResourceOverriddenConditionType,
		fleetv1beta1.ResourceWorkSynchronizedConditionType,
		fleetv1beta1.ResourcesAppliedConditionType,
		fleetv1beta1.ResourcesAvailableConditionType,
		fleetv1beta1.ResourcesDiffReportedConditionType,
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

// ClusterResourcePlacementConditionType returns the CRP condition type used by CRP.
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
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourcesDiffReportedConditionType),
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

type conditionStatus int // internal type to be used when populating the CRP status

const (
	UnknownConditionStatus conditionStatus = iota
	FalseConditionStatus
	TrueConditionStatus
	TotalConditionStatus
)
