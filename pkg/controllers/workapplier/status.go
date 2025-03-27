/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

// refreshWorkStatus refreshes the status of a Work object based on the processing results of its manifests.
func (r *Reconciler) refreshWorkStatus(
	ctx context.Context,
	work *fleetv1beta1.Work,
	bundles []*manifestProcessingBundle,
) error {
	// Note (chenyu1): this method can run in parallel; however, for simplicity reasons,
	// considering that in most of the time the count of manifests would be low, currently
	// Fleet still does the status refresh sequentially.

	manifestCount := len(bundles)
	appliedManifestsCount := 0
	availableAppliedObjectsCount := 0
	untrackableAppliedObjectsCount := 0
	diffReportedObjectsCount := 0

	// Use the now timestamp as the observation time.
	now := metav1.Now()

	// Rebuild the manifest conditions.

	// Pre-allocate the slice.
	rebuiltManifestConds := make([]fleetv1beta1.ManifestCondition, len(bundles))

	// Port back existing manifest conditions to the pre-allocated slice.
	//
	// This step is necessary at the moment primarily for two reasons:
	// a) manifest condition uses metav1.Condition, the LastTransitionTime field of which requires
	//    that Fleet track the last known condition;
	// b) part of the Fleet rollout process uses the LastTransitionTime of the Available condition
	//    to calculate the minimum wait period for an untrackable Work object (a Work object with
	//    one or more untrackable manifests).

	// Prepare an index for quicker lookup.
	rebuiltManifestCondQIdx := prepareRebuiltManifestCondQIdx(bundles)

	// Port back existing manifest conditions using the index.
	for idx := range work.Status.ManifestConditions {
		existingManifestCond := work.Status.ManifestConditions[idx]

		existingManifestCondWRIStr, err := formatWRIString(&existingManifestCond.Identifier)
		if err != nil {
			// It is OK for an existing manifest condition to not have a valid identifier; this
			// happens when the manifest condition was previously associated with a manifest
			// that cannot be decoded. For obvious reasons Fleet does not need to port back
			// such manifest conditions any way.
			continue
		}

		// Check if the WRI string has a match in the index.
		if rebuiltManifestCondIdx, ok := rebuiltManifestCondQIdx[existingManifestCondWRIStr]; ok {
			// Port back the existing manifest condition.
			rebuiltManifestConds[rebuiltManifestCondIdx] = *existingManifestCond.DeepCopy()
		}
	}

	for idx := range bundles {
		bundle := bundles[idx]

		// Update the manifest condition based on the bundle processing results.
		manifestCond := &rebuiltManifestConds[idx]
		manifestCond.Identifier = *bundle.id
		if manifestCond.Conditions == nil {
			manifestCond.Conditions = []metav1.Condition{}
		}

		// Note that per API definition, the observed generation of a manifest condition is that
		// of the applied resource, not that of the Work object.
		inMemberClusterObjGeneration := int64(0)
		if bundle.inMemberClusterObj != nil {
			inMemberClusterObjGeneration = bundle.inMemberClusterObj.GetGeneration()
		}
		setManifestAppliedCondition(manifestCond, bundle.applyResTyp, bundle.applyErr, inMemberClusterObjGeneration)
		setManifestAvailableCondition(manifestCond, bundle.availabilityResTyp, bundle.availabilityErr, inMemberClusterObjGeneration)
		setManifestDiffReportedCondition(manifestCond, bundle.reportDiffResTyp, bundle.reportDiffErr, inMemberClusterObjGeneration)

		// Check if a first drifted timestamp has been set; if not, set it to the current time.
		firstDriftedTimestamp := &now
		if manifestCond.DriftDetails != nil && !manifestCond.DriftDetails.FirstDriftedObservedTime.IsZero() {
			firstDriftedTimestamp = &manifestCond.DriftDetails.FirstDriftedObservedTime
		}
		// Reset the drift details (such details need no port-back).
		manifestCond.DriftDetails = nil
		if len(bundle.drifts) > 0 {
			// Populate drift details if there are drifts found.
			var observedInMemberClusterGen int64
			if bundle.inMemberClusterObj != nil {
				observedInMemberClusterGen = bundle.inMemberClusterObj.GetGeneration()
			}

			manifestCond.DriftDetails = &fleetv1beta1.DriftDetails{
				ObservationTime:                   now,
				ObservedInMemberClusterGeneration: observedInMemberClusterGen,
				FirstDriftedObservedTime:          *firstDriftedTimestamp,
				ObservedDrifts:                    bundle.drifts,
			}
		}

		// Check if a first diffed timestamp has been set; if not, set it to the current time.
		firstDiffedTimestamp := &now
		if manifestCond.DiffDetails != nil && !manifestCond.DiffDetails.FirstDiffedObservedTime.IsZero() {
			firstDiffedTimestamp = &manifestCond.DiffDetails.FirstDiffedObservedTime
		}
		// Reset the diff details (such details need no port-back).
		manifestCond.DiffDetails = nil
		if len(bundle.diffs) > 0 {
			// Populate diff details if there are diffs found.
			var observedInMemberClusterGen *int64
			if bundle.inMemberClusterObj != nil {
				observedInMemberClusterGen = ptr.To(bundle.inMemberClusterObj.GetGeneration())
			}

			manifestCond.DiffDetails = &fleetv1beta1.DiffDetails{
				ObservationTime:                   now,
				ObservedInMemberClusterGeneration: observedInMemberClusterGen,
				FirstDiffedObservedTime:           *firstDiffedTimestamp,
				ObservedDiffs:                     bundle.diffs,
			}
		}

		// Tally the stats.
		if isManifestObjectApplied(bundle.applyResTyp) {
			appliedManifestsCount++
		}
		if isAppliedObjectAvailable(bundle.availabilityResTyp) {
			availableAppliedObjectsCount++
		}
		if bundle.availabilityResTyp == ManifestProcessingAvailabilityResultTypeNotTrackable {
			untrackableAppliedObjectsCount++
		}
		if isManifestObjectDiffReported(bundle.reportDiffResTyp) {
			diffReportedObjectsCount++
		}
	}

	// Refresh the Work object status conditions.

	// Do a sanity check.
	if appliedManifestsCount > manifestCount || availableAppliedObjectsCount > manifestCount || untrackableAppliedObjectsCount > manifestCount || diffReportedObjectsCount > manifestCount {
		// Normally this should never happen.
		return controller.NewUnexpectedBehaviorError(
			fmt.Errorf("the number of applied manifests (%d), available applied objects (%d), untrackable applied objects (%d), or diff reported objects (%d) exceeds the total number of manifests (%d)",
				appliedManifestsCount, availableAppliedObjectsCount, untrackableAppliedObjectsCount, diffReportedObjectsCount, manifestCount))
	}

	if work.Status.Conditions == nil {
		work.Status.Conditions = []metav1.Condition{}
	}
	setWorkAppliedCondition(work, manifestCount, appliedManifestsCount)
	setWorkAvailableCondition(work, manifestCount, availableAppliedObjectsCount, untrackableAppliedObjectsCount)
	setWorkDiffReportedCondition(work, manifestCount, diffReportedObjectsCount)
	work.Status.ManifestConditions = rebuiltManifestConds

	// Update the Work object status.
	if err := r.hubClient.Status().Update(ctx, work); err != nil {
		return controller.NewAPIServerError(false, err)
	}
	return nil
}

// refreshAppliedWorkStatus refreshes the status of an AppliedWork object based on the processing results of its manifests.
func (r *Reconciler) refreshAppliedWorkStatus(
	ctx context.Context,
	appliedWork *fleetv1beta1.AppliedWork,
	bundles []*manifestProcessingBundle,
) error {
	// Note (chenyu1): this method can run in parallel; however, for simplicity reasons,
	// considering that in most of the time the count of manifests would be low, currently
	// Fleet still does the status refresh sequentially.

	// Pre-allocate the slice.
	//
	// Manifests that failed to get applied are not included in this list, hence
	// empty length.
	appliedResources := make([]fleetv1beta1.AppliedResourceMeta, 0, len(bundles))

	// Build the list of applied resources.
	for idx := range bundles {
		bundle := bundles[idx]

		if isManifestObjectApplied(bundle.applyResTyp) {
			appliedResources = append(appliedResources, fleetv1beta1.AppliedResourceMeta{
				WorkResourceIdentifier: *bundle.id,
				UID:                    bundle.inMemberClusterObj.GetUID(),
			})
		}
	}

	// Update the AppliedWork object status.
	appliedWork.Status.AppliedResources = appliedResources
	if err := r.spokeClient.Status().Update(ctx, appliedWork); err != nil {
		klog.ErrorS(err, "Failed to update AppliedWork status",
			"appliedWork", klog.KObj(appliedWork))
		return controller.NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Refreshed AppliedWork object status",
		klog.KObj(appliedWork))
	return nil
}

// isManifestObjectAvailable returns if an availability result type indicates that a manifest
// object in a bundle is available.
func isAppliedObjectAvailable(availabilityResTyp ManifestProcessingAvailabilityResultType) bool {
	return availabilityResTyp == ManifestProcessingAvailabilityResultTypeAvailable || availabilityResTyp == ManifestProcessingAvailabilityResultTypeNotTrackable
}

// isManifestObjectDiffReported returns if a diff report result type indicates that a manifest
// object has been checked for configuration differences.
func isManifestObjectDiffReported(reportDiffResTyp ManifestProcessingReportDiffResultType) bool {
	return reportDiffResTyp == ManifestProcessingReportDiffResultTypeFoundDiff || reportDiffResTyp == ManifestProcessingReportDiffResultTypeNoDiffFound
}

// setManifestAppliedCondition sets the Applied condition on an applied manifest.
func setManifestAppliedCondition(
	manifestCond *fleetv1beta1.ManifestCondition,
	appliedResTyp manifestProcessingAppliedResultType,
	applyError error,
	inMemberClusterObjGeneration int64,
) {
	var appliedCond *metav1.Condition
	switch appliedResTyp {
	case ManifestProcessingApplyResultTypeApplied:
		// The manifest has been successfully applied.
		appliedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Status:             metav1.ConditionTrue,
			Reason:             string(ManifestProcessingApplyResultTypeApplied),
			Message:            ManifestProcessingApplyResultTypeAppliedDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	case ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection:
		// The manifest has been successfully applied, but drift detection has failed.
		//
		// At this moment Fleet does not prepare a dedicated condition for drift detection
		// outcomes.
		appliedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Status:             metav1.ConditionTrue,
			Reason:             string(ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection),
			Message:            ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetectionDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	case ManifestProcessingApplyResultTypeNoApplyPerformed:
		// ReportDiff mode is on and no apply op has been performed. In this case, Fleet
		// will reset the Applied condition.
	default:
		// The apply op fails.
		appliedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Status:             metav1.ConditionFalse,
			Reason:             string(appliedResTyp),
			Message:            fmt.Sprintf("Failed to applied the manifest (error: %s)", applyError),
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	}

	if appliedCond != nil {
		meta.SetStatusCondition(&manifestCond.Conditions, *appliedCond)
		klog.V(2).InfoS("Applied condition set in ManifestCondition",
			"WRI", manifestCond.Identifier,
			"applyResTyp", appliedResTyp, "applyError", applyError,
			"inMemberClusterObjGeneration", inMemberClusterObjGeneration)
	} else {
		// As the conditions are ported back; removal must be performed if the Applied
		// condition is not set.
		meta.RemoveStatusCondition(&manifestCond.Conditions, fleetv1beta1.WorkConditionTypeApplied)
		klog.V(2).InfoS("Applied condition removed from ManifestCondition",
			"WRI", manifestCond.Identifier,
			"applyResTyp", appliedResTyp, "applyError", applyError,
			"inMemberClusterObjGeneration", inMemberClusterObjGeneration)
	}
}

// setManifestAvailableCondition sets the Available condition on an applied manifest.
func setManifestAvailableCondition(
	manifestCond *fleetv1beta1.ManifestCondition,
	availabilityResTyp ManifestProcessingAvailabilityResultType,
	availabilityError error,
	inMemberClusterObjGeneration int64,
) {
	var availableCond *metav1.Condition
	switch availabilityResTyp {
	case ManifestProcessingAvailabilityResultTypeSkipped:
		// Availability check has been skipped for the manifest as it has not been applied yet.
		//
		// In this case, no availability condition is set.
	case ManifestProcessingAvailabilityResultTypeFailed:
		// Availability check has failed.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             string(ManifestProcessingAvailabilityResultTypeFailed),
			Message:            fmt.Sprintf(ManifestProcessingAvailabilityResultTypeFailedDescription, availabilityError),
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	case ManifestProcessingAvailabilityResultTypeNotYetAvailable:
		// The manifest is not yet available.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             string(ManifestProcessingAvailabilityResultTypeNotYetAvailable),
			Message:            ManifestProcessingAvailabilityResultTypeNotYetAvailableDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	case ManifestProcessingAvailabilityResultTypeNotTrackable:
		// Fleet cannot track the availability of the manifest.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             string(ManifestProcessingAvailabilityResultTypeNotTrackable),
			Message:            ManifestProcessingAvailabilityResultTypeNotTrackableDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	default:
		// The manifest is available.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             string(ManifestProcessingAvailabilityResultTypeAvailable),
			Message:            ManifestProcessingAvailabilityResultTypeAvailableDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	}

	if availableCond != nil {
		meta.SetStatusCondition(&manifestCond.Conditions, *availableCond)
		klog.V(2).InfoS("Available condition set in ManifestCondition",
			"WRI", manifestCond.Identifier,
			"availabilityResTyp", availabilityResTyp, "availabilityError", availabilityError,
			"inMemberClusterObjGeneration", inMemberClusterObjGeneration)
	} else {
		// As the conditions are ported back; removal must be performed if the Available
		// condition is not set.
		meta.RemoveStatusCondition(&manifestCond.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
		klog.V(2).InfoS("Available condition removed from ManifestCondition",
			"WRI", manifestCond.Identifier,
			"availabilityResTyp", availabilityResTyp, "availabilityError", availabilityError,
			"inMemberClusterObjGeneration", inMemberClusterObjGeneration)
	}
}

// setManifestDiffReportedCondition sets the DiffReported condition on a manifest.
func setManifestDiffReportedCondition(
	manifestCond *fleetv1beta1.ManifestCondition,
	reportDiffResTyp ManifestProcessingReportDiffResultType,
	reportDiffError error,
	inMemberClusterObjGeneration int64,
) {
	var diffReportedCond *metav1.Condition
	switch reportDiffResTyp {
	case ManifestProcessingReportDiffResultTypeFailed:
		// Diff reporting has failed.
		diffReportedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeDiffReported,
			Status:             metav1.ConditionFalse,
			Reason:             string(ManifestProcessingReportDiffResultTypeFailed),
			Message:            fmt.Sprintf(ManifestProcessingReportDiffResultTypeFailedDescription, reportDiffError),
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	case ManifestProcessingReportDiffResultTypeNotEnabled:
		// Diff reporting is not enabled.
		//
		// For simplicity reasons, the DiffReported condition will only appear when
		// the ReportDiff mode is on; in other configurations, the condition will be
		// removed.
	case ManifestProcessingReportDiffResultTypeNoDiffFound:
		// No diff has been found.
		diffReportedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeDiffReported,
			Status:             metav1.ConditionTrue,
			Reason:             string(ManifestProcessingReportDiffResultTypeNoDiffFound),
			Message:            ManifestProcessingReportDiffResultTypeNoDiffFoundDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	case ManifestProcessingReportDiffResultTypeFoundDiff:
		// Found diffs.
		diffReportedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeDiffReported,
			Status:             metav1.ConditionTrue,
			Reason:             string(ManifestProcessingReportDiffResultTypeFoundDiff),
			Message:            ManifestProcessingReportDiffResultTypeFoundDiffDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	}

	if diffReportedCond != nil {
		meta.SetStatusCondition(&manifestCond.Conditions, *diffReportedCond)
		klog.V(2).InfoS("DiffReported condition set in ManifestCondition",
			"WRI", manifestCond.Identifier,
			"reportDiffResTyp", reportDiffResTyp, "reportDiffError", reportDiffError,
			"inMemberClusterObjGeneration", inMemberClusterObjGeneration)
	} else {
		// As the conditions are ported back; removal must be performed if the DiffReported
		// condition is not set.
		meta.RemoveStatusCondition(&manifestCond.Conditions, fleetv1beta1.WorkConditionTypeDiffReported)
		klog.V(2).InfoS("DiffReported condition removed from ManifestCondition",
			"WRI", manifestCond.Identifier,
			"reportDiffResTyp", reportDiffResTyp, "reportDiffError", reportDiffError,
			"inMemberClusterObjGeneration", inMemberClusterObjGeneration)
	}
}

// setWorkAppliedCondition sets the Applied condition on a Work object.
//
// A Work object is considered to be applied if all of its manifests have been successfully applied.
func setWorkAppliedCondition(
	work *fleetv1beta1.Work,
	manifestCount, appliedManifestCount int,
) {
	var appliedCond *metav1.Condition
	switch {
	case work.Spec.ApplyStrategy != nil && work.Spec.ApplyStrategy.Type == fleetv1beta1.ApplyStrategyTypeReportDiff:
		// ReportDiff mode is on; no apply op has been performed, and consequently
		// Fleet will not update the Applied condition.
	case appliedManifestCount == manifestCount:
		// All manifests have been successfully applied.
		appliedCond = &metav1.Condition{
			Type:   fleetv1beta1.WorkConditionTypeApplied,
			Status: metav1.ConditionTrue,
			// Here Fleet reuses the same reason for individual manifests.
			Reason:             WorkAllManifestsAppliedReason,
			Message:            allManifestsAppliedMessage,
			ObservedGeneration: work.Generation,
		}
	default:
		// Not all manifests have been successfully applied.
		appliedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Status:             metav1.ConditionFalse,
			Reason:             WorkNotAllManifestsAppliedReason,
			Message:            fmt.Sprintf(notAllManifestsAppliedMessage, appliedManifestCount, manifestCount),
			ObservedGeneration: work.Generation,
		}
	}

	if appliedCond != nil {
		meta.SetStatusCondition(&work.Status.Conditions, *appliedCond)
		klog.V(2).InfoS("Applied condition set on Work object",
			"appliedManifestCount", appliedManifestCount, "manifestCount", manifestCount,
			"work", klog.KObj(work))
	} else {
		// For consistency reasons, Fleet will remove the Applied condition if the
		// it is not set in the current run (i.e., ReportDiff mode is on).
		meta.RemoveStatusCondition(&work.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
		klog.V(2).InfoS("Applied condition removed on Work object",
			"appliedManifestCount", appliedManifestCount, "manifestCount", manifestCount,
			"work", klog.KObj(work))
	}
}

// setWorkAvailableCondition sets the Available condition on a Work object.
//
// A Work object is considered to be available if all of its applied manifests are available.
func setWorkAvailableCondition(
	work *fleetv1beta1.Work,
	manifestCount, availableManifestCount, untrackableAppliedObjectsCount int,
) {
	appliedCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
	var availableCond *metav1.Condition
	switch {
	case work.Spec.ApplyStrategy != nil && work.Spec.ApplyStrategy.Type == fleetv1beta1.ApplyStrategyTypeReportDiff:
		// ReportDiff mode is on; no apply op has been performed, and consequently
		// Fleet will not update the Available condition.
	case !condition.IsConditionStatusTrue(appliedCond, work.Generation):
		// Not all manifests have been applied; skip updating the Available condition.
	case availableManifestCount == manifestCount && untrackableAppliedObjectsCount == 0:
		// All manifests are available.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             WorkAllManifestsAvailableReason,
			Message:            allAppliedObjectAvailableMessage,
			ObservedGeneration: work.Generation,
		}
	case availableManifestCount == manifestCount:
		// Some manifests are not trackable.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             WorkNotAllManifestsTrackableReason,
			Message:            someAppliedObjectUntrackableMessage,
			ObservedGeneration: work.Generation,
		}
	default:
		// Not all manifests are available.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             WorkNotAllManifestsAvailableReason,
			Message:            fmt.Sprintf(notAllAppliedObjectsAvailableMessage, availableManifestCount, manifestCount),
			ObservedGeneration: work.Generation,
		}
	}

	if availableCond != nil {
		meta.SetStatusCondition(&work.Status.Conditions, *availableCond)
		klog.V(2).InfoS("Available condition set on Work object",
			"availableManifestCount", availableManifestCount, "untrackableAppliedObjectsCount", untrackableAppliedObjectsCount,
			"manifestCount", manifestCount,
			"work", klog.KObj(work))
	} else {
		// Fleet will remove the Available condition if it is not set in the current run
		// as it can change without Work object generation bumps.
		meta.RemoveStatusCondition(&work.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
		klog.V(2).InfoS("Available condition removed on Work object",
			"availableManifestCount", availableManifestCount, "untrackableAppliedObjectsCount", untrackableAppliedObjectsCount,
			"manifestCount", manifestCount,
			"work", klog.KObj(work))
	}
}

// setWorkDiffReportedCondition sets the DiffReported condition on a Work object.
func setWorkDiffReportedCondition(
	work *fleetv1beta1.Work,
	manifestCount, diffReportedObjectsCount int,
) {
	var diffReportedCond *metav1.Condition
	switch {
	case work.Spec.ApplyStrategy == nil || work.Spec.ApplyStrategy.Type != fleetv1beta1.ApplyStrategyTypeReportDiff:
		// ReportDiff mode is not on; Fleet will remove DiffReported condition.
	case manifestCount == diffReportedObjectsCount:
		// All objects have completed diff reporting.
		diffReportedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeDiffReported,
			Status:             metav1.ConditionTrue,
			Reason:             WorkAllManifestsDiffReportedReason,
			Message:            allManifestsHaveReportedDiffMessage,
			ObservedGeneration: work.Generation,
		}
	default:
		// Not all objects have completed diff reporting.
		diffReportedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeDiffReported,
			Status:             metav1.ConditionFalse,
			Reason:             WorkNotAllManifestsDiffReportedReason,
			Message:            fmt.Sprintf(notAllManifestsHaveReportedDiff, diffReportedObjectsCount, manifestCount),
			ObservedGeneration: work.Generation,
		}
	}

	if diffReportedCond != nil {
		meta.SetStatusCondition(&work.Status.Conditions, *diffReportedCond)
		klog.V(2).InfoS("DiffReported condition set on Work object",
			"diffReportedObjectsCount", diffReportedObjectsCount, "manifestCount", manifestCount,
			"work", klog.KObj(work))
	} else {
		// For consistency reasons, Fleet will remove the DiffReported condition if the
		// ReportDiff mode is not being used.
		meta.RemoveStatusCondition(&work.Status.Conditions, fleetv1beta1.WorkConditionTypeDiffReported)
		klog.V(2).InfoS("DiffReported condition removed on Work object",
			"diffReportedObjectsCount", diffReportedObjectsCount, "manifestCount", manifestCount,
			"work", klog.KObj(work))
	}
}

// prepareRebuiltManifestCondQIdx returns a map that allows quicker look up of a manifest
// condition given a work resource identifier.
func prepareRebuiltManifestCondQIdx(bundles []*manifestProcessingBundle) map[string]int {
	rebuiltManifestCondQIdx := make(map[string]int)
	for idx := range bundles {
		bundle := bundles[idx]

		wriStr, err := formatWRIString(bundle.id)
		if err != nil {
			// There might be manifest conditions without a valid identifier in the bundle set
			// (e.g., decoding error has occurred when processing a bundle).
			// Fleet will skip these bundles, as there is no need to port back
			// information for such manifests any way for obvious reasons (manifest itself is not
			// identifiable). This is not considered as an error.
			continue
		}

		rebuiltManifestCondQIdx[wriStr] = idx
	}
	return rebuiltManifestCondQIdx
}
