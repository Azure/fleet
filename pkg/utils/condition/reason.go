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

// Package condition provides reason constants for status reporting across KubeFleet controllers and APIs.
package condition

// A group of condition reason string which is used to populate the placement condition.
const (
	// InvalidResourceSelectorsReason is the reason string of placement condition when the selected resources are invalid
	// or forbidden.
	InvalidResourceSelectorsReason = "InvalidResourceSelectors"

	// SchedulingUnknownReason is the reason string of placement condition when the schedule status is unknown.
	SchedulingUnknownReason = "SchedulePending"

	// ResourceScheduleSucceededReason is the reason string of placement condition when the selected resources are scheduled.
	ResourceScheduleSucceededReason = "ScheduleSucceeded"

	// ResourceScheduleFailedReason is the reason string of placement condition when the scheduler failed to schedule the selected resources.
	ResourceScheduleFailedReason = "ScheduleFailed"

	// RolloutStartedUnknownReason is the reason string of placement condition if rollout status is
	// unknown.
	RolloutStartedUnknownReason = "RolloutStartedUnknown"

	// RolloutControlledByExternalControllerReason is the reason string of the placement condition if
	// the placement rollout strategy type is set to External, and either rollout not started at all or
	// clusters observes different resource snapshot versions. This is a special case for unknown rolloutStarted condition.
	RolloutControlledByExternalControllerReason = "RolloutControlledByExternalController"

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

	// StatusSyncFailedReason is the reason string of placement condition when the status sync failed.
	StatusSyncFailedReason = "StatusSyncFailed"

	// StatusSyncSucceededReason is the reason string of placement condition when the status sync succeeded.
	StatusSyncSucceededReason = "StatusSyncSucceeded"

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

// A group of condition reason string which is used to populate the ClusterStagedUpdateRun condition.
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

// A group of condition reason string which is used for Work condition.
const (
	// WorkCondition condition reasons
	WorkAppliedFailedReason = "WorkAppliedFailed"

	// workAppliedCompletedReason is the reason string of condition when the all the manifests have been applied.
	WorkAppliedCompletedReason = "WorkAppliedCompleted"

	// workNotAvailableYetReason is the reason string of condition when the manifest has been applied but not yet available.
	WorkNotAvailableYetReason = "WorkNotAvailableYet"

	// workAvailabilityUnknownReason is the reason string of condition when the manifest availability is unknown.
	WorkAvailabilityUnknownReason = "WorkAvailabilityUnknown"

	// WorkAvailableReason is the reason string of condition when the manifest is available.
	WorkAvailableReason = "WorkAvailable"

	// WorkNotTrackableReason is the reason string of condition when the manifest is already up to date but we don't have
	// a way to track its availabilities.
	WorkNotTrackableReason = "WorkNotTrackable"

	// ManifestApplyFailedReason is the reason string of condition when it failed to apply manifest.
	ManifestApplyFailedReason = "ManifestApplyFailed"

	// ApplyConflictBetweenPlacementsReason is the reason string of condition when the manifest is owned by multiple placements,
	// and they have conflicts.
	ApplyConflictBetweenPlacementsReason = "ApplyConflictBetweenPlacements"

	// ManifestsAlreadyOwnedByOthersReason is the reason string of condition when the manifest is already owned by other
	// non-fleet appliers.
	ManifestsAlreadyOwnedByOthersReason = "ManifestsAlreadyOwnedByOthers"

	// ManifestAlreadyUpToDateReason is the reason string of condition when the manifest is already up to date.
	ManifestAlreadyUpToDateReason  = "ManifestAlreadyUpToDate"
	ManifestAlreadyUpToDateMessage = "Manifest is already up to date"

	// ManifestNeedsUpdateReason is the reason string of condition when the manifest needs to be updated.
	ManifestNeedsUpdateReason  = "ManifestNeedsUpdate"
	ManifestNeedsUpdateMessage = "Manifest has just been updated and in the processing of checking its availability"

	// ManifestAppliedCondPreparingToProcessReason is the reason string of condition when the manifest is being prepared for processing.
	ManifestAppliedCondPreparingToProcessReason  = "PreparingToProcess"
	ManifestAppliedCondPreparingToProcessMessage = "The manifest is being prepared for processing."

	// Some exported reasons for Work object conditions. Currently only the untrackable reason is being actively used.

	// This is a new reason for the Availability condition when the manifests are not
	// trackable for availability. This value is currently unused.
	//
	// TO-DO (chenyu1): switch to the new reason after proper rollout.
	WorkNotAllManifestsTrackableReasonNew = "SomeManifestsAreNotAvailabilityTrackable"
	// This reason uses the exact same value as the one kept in the old work applier for
	// compatibility reasons. It helps guard the case where the member agent is upgraded
	// before the hub agent.
	//
	// TO-DO (chenyu1): switch off the old reason after proper rollout.
	WorkNotAllManifestsTrackableReason    = WorkNotTrackableReason
	WorkAllManifestsAppliedReason         = "AllManifestsApplied"
	WorkAllManifestsAvailableReason       = "AllManifestsAvailable"
	WorkAllManifestsDiffReportedReason    = "AllManifestsDiffReported"
	WorkNotAllManifestsAppliedReason      = "SomeManifestsAreNotApplied"
	WorkNotAllManifestsAvailableReason    = "SomeManifestsAreNotAvailable"
	WorkNotAllManifestsDiffReportedReason = "SomeManifestsHaveNotReportedDiff"

	// Some condition messages for Work object conditions.
	AllManifestsAppliedMessage           = "All the specified manifests have been applied"
	AllManifestsHaveReportedDiffMessage  = "All the specified manifests have reported diff"
	AllAppliedObjectAvailableMessage     = "All of the applied manifests are available"
	SomeAppliedObjectUntrackableMessage  = "Some of the applied manifests cannot be tracked for availability"
	NotAllManifestsAppliedMessage        = "Failed to apply all manifests (%d of %d manifests are applied)"
	NotAllAppliedObjectsAvailableMessage = "Some manifests are not available (%d of %d manifests are available)"
	NotAllManifestsHaveReportedDiff      = "Failed to report diff on all manifests (%d of %d manifests have reported diff)"
)
