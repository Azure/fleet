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

package workapplier

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/resource"
)

const (
	WorkStatusTrimmedDueToOversizedStatusReason  = "Oversized"
	WorkStatusTrimmedDueToOversizedStatusMsgTmpl = "The status data (drift/diff details and back-reported status) has been trimmed due to size constraints (%d bytes over limit %d)"
)

// refreshWorkStatus refreshes the status of a Work object based on the processing results of its manifests.
//
// TO-DO (chenyu1): refactor this method a bit to reduce its complexity and enable parallelization.
func (r *Reconciler) refreshWorkStatus( //nolint:gocyclo
	ctx context.Context,
	work *fleetv1beta1.Work,
	bundles []*manifestProcessingBundle,
) error {
	originalStatus := work.Status.DeepCopy()

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

	// Set the two flags here as they are per-work-object settings.
	isReportDiffModeOn := work.Spec.ApplyStrategy != nil && work.Spec.ApplyStrategy.Type == fleetv1beta1.ApplyStrategyTypeReportDiff
	isStatusBackReportingOn := work.Spec.ReportBackStrategy != nil && work.Spec.ReportBackStrategy.Type == fleetv1beta1.ReportBackStrategyTypeMirror
	isDriftedOrDiffed := false
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
		setManifestAppliedCondition(manifestCond, isReportDiffModeOn, bundle.applyOrReportDiffResTyp, bundle.applyOrReportDiffErr, inMemberClusterObjGeneration)
		setManifestAvailableCondition(manifestCond, bundle.availabilityResTyp, bundle.availabilityErr, inMemberClusterObjGeneration)
		setManifestDiffReportedCondition(manifestCond, isReportDiffModeOn, bundle.applyOrReportDiffResTyp, bundle.applyOrReportDiffErr, inMemberClusterObjGeneration)

		// Check if a first drifted timestamp has been set; if not, set it to the current time.
		firstDriftedTimestamp := &now
		if manifestCond.DriftDetails != nil && !manifestCond.DriftDetails.FirstDriftedObservedTime.IsZero() {
			firstDriftedTimestamp = &manifestCond.DriftDetails.FirstDriftedObservedTime
		}
		// Reset the drift details (such details need no port-back).
		manifestCond.DriftDetails = nil
		if len(bundle.drifts) > 0 {
			isDriftedOrDiffed = true

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
			isDriftedOrDiffed = true

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

		// Tally the stats, and perform status back-reporting if applicable.
		if isManifestObjectApplied(bundle.applyOrReportDiffResTyp) {
			appliedManifestsCount++

			if isStatusBackReportingOn {
				// Back-report the status from the member cluster side, if applicable.
				//
				// Back-reporting is only performed when:
				// a) the ReportBackStrategy is of the type Mirror; and
				// b) the manifest object has been applied successfully.
				backReportStatus(bundle.inMemberClusterObj, manifestCond, now, klog.KObj(work))
			}
		}
		if isAppliedObjectAvailable(bundle.availabilityResTyp) {
			availableAppliedObjectsCount++
		}
		if bundle.availabilityResTyp == AvailabilityResultTypeNotTrackable {
			untrackableAppliedObjectsCount++
		}
		if isManifestObjectDiffReported(bundle.applyOrReportDiffResTyp) {
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

	// Perform a size check before the status update. If the Work object goes over the size limit, trim
	// some data from its status to ensure that update ops can go through.
	//
	// Note (chenyu1): at this moment, for simplicity reasons, the trimming op follows a very simple logic:
	// if the size limit is breached, the work applier will summarize all drift/diff details in the status,
	// and drop all back-reported status data. More sophisticated trimming logic does obviously exist; here
	// the controller prefers the simple version primarily for two reasons:
	//
	// a) in most of the time, it is rare to reach the size limit: KubeFleet's snapshotting mechanism
	//    tries to keep the total manifest size in a Work object below 800KB (exceptions do exist), which leaves ~600KB
	//    space for the status data. The work applier reports for each manifest two conditions at most in the
	//    status (which are all quite small in size), plus the drift/diff details and the back-reported status
	//    (if applicable); considering the observation that drifts/diffs are not common and their details are usually small
	//    (just a JSON path plus the before/after values), and the observation that most Kubernetes objects
	//    only have a few KBs of status data and not all API types need status back-reporting, most of the time
	//    the Work object should have enough space for status data without trimming;
	// b) performing more fine-grained, selective trimming can be a very CPU and memory intensive (e.g.
	//    various serialization calls) and complex process, and it is difficult to yield optimal results
	//    even with best efforts.
	//
	// TO-DO (chenyu1): re-visit this part of the code and evaluate the need for more fine-grained sharding
	// if we have users that do use placements of a large collection of manifests and/or very large objects
	// with drift/diff detection and status back-reporting on.
	//
	// TO-DO (chenyu1): evaluate if we need to impose more strict size limits on the manifests to ensure that
	// Work objects (almost) always have enough space for status data.
	sizeDeltaBytes, err := resource.CalculateSizeDeltaOverLimitFor(work, resource.DefaultObjSizeLimitWithPaddingBytes)
	if err != nil {
		// Normally this should never occur.
		klog.ErrorS(err, "Failed to check Work object size before status update", "work", klog.KObj(work))
		wrappedErr := fmt.Errorf("failed to check work object size before status update: %w", err)
		return controller.NewUnexpectedBehaviorError(wrappedErr)
	}
	if sizeDeltaBytes > 0 {
		klog.V(2).InfoS("Must trim status data as the work object has grown over its size limit",
			"work", klog.KObj(work),
			"sizeDeltaBytes", sizeDeltaBytes, "sizeLimitBytes", resource.DefaultObjSizeLimitWithPaddingBytes)
		trimWorkStatusDataWhenOversized(work)
	}
	setWorkStatusTrimmedCondition(work, sizeDeltaBytes, resource.DefaultObjSizeLimitWithPaddingBytes)

	// Update the Work object status.
	if shouldSkipStatusUpdate(isDriftedOrDiffed, isStatusBackReportingOn, originalStatus, &work.Status) {
		// No status change found; skip the update.
		klog.V(2).InfoS("No status change found for Work object; skip the status update", "work", klog.KObj(work))
	} else {
		klog.V(2).InfoS("Refreshing work object status", "work", klog.KObj(work), "isDriftedOrDiffed", isDriftedOrDiffed, "isStatusBackReportingOn", isStatusBackReportingOn)
		if err := r.hubClient.Status().Update(ctx, work); err != nil {
			return controller.NewAPIServerError(false, err)
		}
	}
	return nil
}

// refreshAppliedWorkStatus refreshes the status of an AppliedWork object based on the processing results of its manifests.
func (r *Reconciler) refreshAppliedWorkStatus(
	ctx context.Context,
	appliedWork *fleetv1beta1.AppliedWork,
	bundles []*manifestProcessingBundle,
) error {
	originalStatus := appliedWork.Status.DeepCopy()

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

		if isManifestObjectApplied(bundle.applyOrReportDiffResTyp) {
			appliedResources = append(appliedResources, fleetv1beta1.AppliedResourceMeta{
				WorkResourceIdentifier: *bundle.id,
				UID:                    bundle.inMemberClusterObj.GetUID(),
			})
		}
	}

	// Update the AppliedWork object status.
	appliedWork.Status.AppliedResources = appliedResources

	// Skip the status update if no change found.
	if equality.Semantic.DeepEqual(originalStatus, &appliedWork.Status) {
		klog.V(2).InfoS("No status change found for AppliedWork object; skip the status update", "appliedWork", klog.KObj(appliedWork))
	} else {
		klog.V(2).InfoS("Refreshing AppliedWork object status", "appliedWork", klog.KObj(appliedWork))
		if err := r.spokeClient.Status().Update(ctx, appliedWork); err != nil {
			klog.ErrorS(err, "Failed to update AppliedWork status",
				"appliedWork", klog.KObj(appliedWork))
			return controller.NewAPIServerError(false, err)
		}
	}
	return nil
}

// isManifestObjectAvailable returns if an availability result type indicates that a manifest
// object in a bundle is available.
func isAppliedObjectAvailable(availabilityResTyp ManifestProcessingAvailabilityResultType) bool {
	return availabilityResTyp == AvailabilityResultTypeAvailable || availabilityResTyp == AvailabilityResultTypeNotTrackable
}

// isManifestObjectDiffReported returns if a diff report result type indicates that a manifest
// object has been checked for configuration differences.
func isManifestObjectDiffReported(reportDiffResTyp ManifestProcessingApplyOrReportDiffResultType) bool {
	return reportDiffResTyp == ApplyOrReportDiffResTypeFoundDiff || reportDiffResTyp == ApplyOrReportDiffResTypeNoDiffFound
}

// setManifestAppliedCondition sets the Applied condition on an applied manifest.
func setManifestAppliedCondition(
	manifestCond *fleetv1beta1.ManifestCondition,
	isReportDiffModeOn bool,
	applyOrReportDiffResTyp ManifestProcessingApplyOrReportDiffResultType,
	applyOrReportDiffError error,
	inMemberClusterObjGeneration int64,
) {
	var appliedCond *metav1.Condition
	switch {
	case isReportDiffModeOn:
		// ReportDiff mode is on and no apply op has been performed. In this case, Fleet
		// will reset the Applied condition.
	case applyOrReportDiffResTyp == ApplyOrReportDiffResTypeApplied:
		// The manifest has been successfully applied.
		appliedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Status:             metav1.ConditionTrue,
			Reason:             string(ApplyOrReportDiffResTypeApplied),
			Message:            ApplyOrReportDiffResTypeAppliedDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	case applyOrReportDiffResTyp == ApplyOrReportDiffResTypeAppliedWithFailedDriftDetection:
		// The manifest has been successfully applied, but drift detection has failed.
		//
		// At this moment Fleet does not prepare a dedicated condition for drift detection
		// outcomes.
		appliedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Status:             metav1.ConditionTrue,
			Reason:             string(ApplyOrReportDiffResTypeAppliedWithFailedDriftDetection),
			Message:            string(ApplyOrReportDiffResTypeAppliedWithFailedDriftDetection),
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	case !manifestProcessingApplyResTypSet.Has(applyOrReportDiffResTyp):
		// Do a sanity check; verify if the returned result type is a valid one.
		// Normally this branch should never run.
		wrappedErr := fmt.Errorf("found an unexpected apply result type %s", applyOrReportDiffResTyp)
		klog.ErrorS(wrappedErr, "Failed to set Applied condition",
			"workResourceID", manifestCond.Identifier,
			"applyOrReportDiffResTyp", applyOrReportDiffResTyp,
			"applyOrReportDiffError", applyOrReportDiffError)
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		// The work applier will consider this to be an apply failure.
		appliedCond = &metav1.Condition{
			Type:   fleetv1beta1.WorkConditionTypeApplied,
			Status: metav1.ConditionFalse,
			Reason: string(ApplyOrReportDiffResTypeFailedToApply),
			Message: fmt.Sprintf("An unexpected apply result is yielded (%s, error: %s)",
				applyOrReportDiffResTyp, applyOrReportDiffError),
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	default:
		// The apply op fails.
		appliedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Status:             metav1.ConditionFalse,
			Reason:             string(applyOrReportDiffResTyp),
			Message:            fmt.Sprintf("Failed to apply the manifest (error: %s)", applyOrReportDiffError),
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	}

	if appliedCond != nil {
		meta.SetStatusCondition(&manifestCond.Conditions, *appliedCond)
		klog.V(2).InfoS("Applied condition set in ManifestCondition",
			"workResourceID", manifestCond.Identifier,
			"applyOrReportDiffResTyp", applyOrReportDiffResTyp, "applyOrReportDiffError", applyOrReportDiffError,
			"inMemberClusterObjGeneration", inMemberClusterObjGeneration)
	} else {
		// As the conditions are ported back; removal must be performed if the Applied
		// condition is not set.
		meta.RemoveStatusCondition(&manifestCond.Conditions, fleetv1beta1.WorkConditionTypeApplied)
		klog.V(2).InfoS("Applied condition removed from ManifestCondition",
			"workResourceID", manifestCond.Identifier,
			"applyOrReportDiffResTyp", applyOrReportDiffResTyp, "applyOrReportDiffError", applyOrReportDiffError,
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
	case AvailabilityResultTypeSkipped:
		// Availability check has been skipped for the manifest as it has not been applied yet.
		//
		// In this case, no availability condition is set.
	case AvailabilityResultTypeFailed:
		// Availability check has failed.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             string(AvailabilityResultTypeFailed),
			Message:            fmt.Sprintf(AvailabilityResultTypeFailedDescription, availabilityError),
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	case AvailabilityResultTypeNotYetAvailable:
		// The manifest is not yet available.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             string(AvailabilityResultTypeNotYetAvailable),
			Message:            AvailabilityResultTypeNotYetAvailableDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	case AvailabilityResultTypeNotTrackable:
		// Fleet cannot track the availability of the manifest.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             string(AvailabilityResultTypeNotTrackable),
			Message:            AvailabilityResultTypeNotTrackableDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	default:
		// The manifest is available.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             string(AvailabilityResultTypeAvailable),
			Message:            AvailabilityResultTypeAvailableDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	}

	if availableCond != nil {
		meta.SetStatusCondition(&manifestCond.Conditions, *availableCond)
		klog.V(2).InfoS("Available condition set in ManifestCondition",
			"workResourceID", manifestCond.Identifier,
			"availabilityResTyp", availabilityResTyp, "availabilityError", availabilityError,
			"inMemberClusterObjGeneration", inMemberClusterObjGeneration)
	} else {
		// As the conditions are ported back; removal must be performed if the Available
		// condition is not set.
		meta.RemoveStatusCondition(&manifestCond.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
		klog.V(2).InfoS("Available condition removed from ManifestCondition",
			"workResourceID", manifestCond.Identifier,
			"availabilityResTyp", availabilityResTyp, "availabilityError", availabilityError,
			"inMemberClusterObjGeneration", inMemberClusterObjGeneration)
	}
}

// setManifestDiffReportedCondition sets the DiffReported condition on a manifest.
func setManifestDiffReportedCondition(
	manifestCond *fleetv1beta1.ManifestCondition,
	isReportDiffModeOn bool,
	applyOrReportDiffResTyp ManifestProcessingApplyOrReportDiffResultType,
	applyOrReportDiffErr error,
	inMemberClusterObjGeneration int64,
) {
	var diffReportedCond *metav1.Condition
	switch {
	case !isReportDiffModeOn:
		// ReportDiff mode is not on; Fleet will remove DiffReported condition.
	case applyOrReportDiffResTyp == ApplyOrReportDiffResTypeFailedToReportDiff:
		// Diff reporting has failed.
		diffReportedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeDiffReported,
			Status:             metav1.ConditionFalse,
			Reason:             string(ApplyOrReportDiffResTypeFailedToReportDiff),
			Message:            fmt.Sprintf(ApplyOrReportDiffResTypeFailedToReportDiffDescription, applyOrReportDiffErr),
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	case applyOrReportDiffResTyp == ApplyOrReportDiffResTypeNoDiffFound:
		// No diff has been found.
		diffReportedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeDiffReported,
			Status:             metav1.ConditionTrue,
			Reason:             string(ApplyOrReportDiffResTypeNoDiffFound),
			Message:            ApplyOrReportDiffResTypeNoDiffFoundDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	case applyOrReportDiffResTyp == ApplyOrReportDiffResTypeFoundDiff:
		// Found diffs.
		diffReportedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeDiffReported,
			Status:             metav1.ConditionTrue,
			Reason:             string(ApplyOrReportDiffResTypeFoundDiff),
			Message:            ApplyOrReportDiffResTypeFoundDiffDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	case applyOrReportDiffResTyp == ApplyOrReportDiffResTypeFoundDiffInDegradedMode:
		// Found diffs in degraded mode.
		//
		// This is not considered as a system error.
		diffReportedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeDiffReported,
			Status:             metav1.ConditionTrue,
			Reason:             string(ApplyOrReportDiffResTypeFoundDiffInDegradedMode),
			Message:            ApplyOrReportDiffResTypeFoundDiffInDegradedModeDescription,
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	default:
		// There are cases where the work applier might not be able to complete the diff reporting
		// due to failures in the pre-processing or processing stage (e.g., the manifest cannot be decoded,
		// or the user sets up a takeover strategy that cannot be completed). This is not considered
		// as a system error.
		diffReportedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeDiffReported,
			Status:             metav1.ConditionFalse,
			Reason:             string(ApplyOrReportDiffResTypeFailedToReportDiff),
			Message:            fmt.Sprintf("An error blocks the diff reporting process (%s, error: %s)", applyOrReportDiffResTyp, applyOrReportDiffErr),
			ObservedGeneration: inMemberClusterObjGeneration,
		}
	}

	if diffReportedCond != nil {
		meta.SetStatusCondition(&manifestCond.Conditions, *diffReportedCond)
		klog.V(2).InfoS("DiffReported condition set in ManifestCondition",
			"workResourceID", manifestCond.Identifier,
			"applyOrReportDiffResTyp", applyOrReportDiffResTyp, "applyOrReportDiffErr", applyOrReportDiffErr,
			"inMemberClusterObjGeneration", inMemberClusterObjGeneration)
	} else {
		// As the conditions are ported back; removal must be performed if the DiffReported
		// condition is not set.
		meta.RemoveStatusCondition(&manifestCond.Conditions, fleetv1beta1.WorkConditionTypeDiffReported)
		klog.V(2).InfoS("DiffReported condition removed from ManifestCondition",
			"workResourceID", manifestCond.Identifier,
			"applyOrReportDiffResTyp", applyOrReportDiffResTyp, "applyOrReportDiffErr", applyOrReportDiffErr,
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
			Reason:             condition.WorkAllManifestsAppliedReason,
			Message:            condition.AllManifestsAppliedMessage,
			ObservedGeneration: work.Generation,
		}
	default:
		// Not all manifests have been successfully applied.
		appliedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Status:             metav1.ConditionFalse,
			Reason:             condition.WorkNotAllManifestsAppliedReason,
			Message:            fmt.Sprintf(condition.NotAllManifestsAppliedMessage, appliedManifestCount, manifestCount),
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
			Reason:             condition.WorkAllManifestsAvailableReason,
			Message:            condition.AllAppliedObjectAvailableMessage,
			ObservedGeneration: work.Generation,
		}
	case availableManifestCount == manifestCount:
		// Some manifests are not trackable.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             condition.WorkNotAllManifestsTrackableReason,
			Message:            condition.SomeAppliedObjectUntrackableMessage,
			ObservedGeneration: work.Generation,
		}
	default:
		// Not all manifests are available.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             condition.WorkNotAllManifestsAvailableReason,
			Message:            fmt.Sprintf(condition.NotAllAppliedObjectsAvailableMessage, availableManifestCount, manifestCount),
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
			Reason:             condition.WorkAllManifestsDiffReportedReason,
			Message:            condition.AllManifestsHaveReportedDiffMessage,
			ObservedGeneration: work.Generation,
		}
	default:
		// Not all objects have completed diff reporting.
		diffReportedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeDiffReported,
			Status:             metav1.ConditionFalse,
			Reason:             condition.WorkNotAllManifestsDiffReportedReason,
			Message:            fmt.Sprintf(condition.NotAllManifestsHaveReportedDiff, diffReportedObjectsCount, manifestCount),
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

		if len(bundle.workResourceIdentifierStr) == 0 {
			// There might be manifest conditions without a valid identifier in the bundle set
			// (e.g., decoding error has occurred when processing a bundle).
			// Fleet will skip these bundles, as there is no need to port back
			// information for such manifests any way for obvious reasons (manifest itself is not
			// identifiable). This is not considered as an error.
			continue
		}
		rebuiltManifestCondQIdx[bundle.workResourceIdentifierStr] = idx
	}
	return rebuiltManifestCondQIdx
}

// backReportStatus writes the status field of an object applied on the member cluster side in
// the status of the Work object.
func backReportStatus(
	inMemberClusterObj *unstructured.Unstructured,
	manifestCond *fleetv1beta1.ManifestCondition,
	now metav1.Time,
	workRef klog.ObjectRef,
) {
	if inMemberClusterObj == nil || inMemberClusterObj.Object == nil {
		// Do a sanity check; normally this will never occur (as status back-reporting
		// only applies to objects that have been successfully applied).
		//
		// Should this unexpected situation occurs, the work applier does not register
		// it as an error; the object shall be ignored for the status back-reporting
		// part of the reconciliation loop.
		wrapperErr := fmt.Errorf("attempted to back-report status for a manifest that has not been applied yet or cannot be found on the member cluster side")
		_ = controller.NewUnexpectedBehaviorError(wrapperErr)
		klog.ErrorS(wrapperErr, "Failed to back-report status", "work", workRef, "resourceIdentifier", manifestCond.Identifier)
		return
	}
	if _, ok := inMemberClusterObj.Object["status"]; !ok {
		// The object from the member cluster side does not have a status subresource; this
		// is not considered as an error.
		klog.V(2).InfoS("cannot back-report status as the applied resource on the member cluster side does not have a status subresource", "work", workRef, "resourceIdentifier", manifestCond.Identifier)
		return
	}

	statusBackReportingWrapper := make(map[string]interface{})
	// The TypeMeta fields must be added in the wrapper, otherwise the client libraries would
	// have trouble serializing/deserializing the wrapper object when it's written/read to/from
	// the API server.
	statusBackReportingWrapper["apiVersion"] = inMemberClusterObj.GetAPIVersion()
	statusBackReportingWrapper["kind"] = inMemberClusterObj.GetKind()
	statusBackReportingWrapper["status"] = inMemberClusterObj.Object["status"]
	statusData, err := json.Marshal(statusBackReportingWrapper)
	if err != nil {
		// This normally should never occur.
		wrappedErr := fmt.Errorf("failed to marshal wrapped back-reported status: %w", err)
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		klog.ErrorS(wrappedErr, "Failed to prepare status wrapper", "work", workRef, "resourceIdentifier", manifestCond.Identifier)
		return
	}

	manifestCond.BackReportedStatus = &fleetv1beta1.BackReportedStatus{
		ObservedStatus: runtime.RawExtension{
			Raw: statusData,
		},
		ObservationTime: now,
	}
}

// trimWorkStatusDataWhenOversized trims some data from the Work object status when the object
// reaches its size limit.
func trimWorkStatusDataWhenOversized(work *fleetv1beta1.Work) {
	// Trim drift/diff details + back-reported status from the Work object status.
	// Replace detailed reportings with a summary if applicable.
	for idx := range work.Status.ManifestConditions {
		manifestCond := &work.Status.ManifestConditions[idx]

		// Note (chenyu1): check for the second term will always pass; it is added as a sanity check.
		if manifestCond.DriftDetails != nil && len(manifestCond.DriftDetails.ObservedDrifts) > 0 {
			driftCount := len(manifestCond.DriftDetails.ObservedDrifts)
			firstDriftPath := manifestCond.DriftDetails.ObservedDrifts[0].Path
			// If there are multiple drifts, report only the path of the first drift plus the count of
			// other paths. Also, leave out the specific value differences.
			pathSummary := firstDriftPath
			if len(manifestCond.DriftDetails.ObservedDrifts) > 1 {
				pathSummary = fmt.Sprintf("%s and %d more path(s)", firstDriftPath, driftCount-1)
			}
			manifestCond.DriftDetails.ObservedDrifts = []fleetv1beta1.PatchDetail{
				{
					Path:          pathSummary,
					ValueInMember: "(omitted)",
					ValueInHub:    "(omitted)",
				},
			}
		}

		// Note (chenyu1): check for the second term will always pass; it is added as a sanity check.
		if manifestCond.DiffDetails != nil && len(manifestCond.DiffDetails.ObservedDiffs) > 0 {
			diffCount := len(manifestCond.DiffDetails.ObservedDiffs)
			firstDiffPath := manifestCond.DiffDetails.ObservedDiffs[0].Path
			// If there are multiple drifts, report only the path of the first drift plus the count of
			// other paths. Also, leave out the specific value differences.
			pathSummary := firstDiffPath
			if len(manifestCond.DiffDetails.ObservedDiffs) > 1 {
				pathSummary = fmt.Sprintf("%s and %d more path(s)", firstDiffPath, diffCount-1)
			}
			manifestCond.DiffDetails.ObservedDiffs = []fleetv1beta1.PatchDetail{
				{
					Path:          pathSummary,
					ValueInMember: "(omitted)",
					ValueInHub:    "(omitted)",
				},
			}
		}

		manifestCond.BackReportedStatus = nil
	}
}

// setWorkStatusTrimmedCondition sets or removes the StatusTrimmed condition on a Work object
// based on whether the status has been trimmed due to it being oversized.
//
// Note (chenyu1): at this moment, due to limitations on the hub agent controller side (some
// controllers assume that placement related conditions are always set in a specific sequence),
// this StatusTrimmed condition might not be exposed on the placement status properly yet.
func setWorkStatusTrimmedCondition(work *fleetv1beta1.Work, sizeDeltaBytes, sizeLimitBytes int) {
	if sizeDeltaBytes <= 0 {
		// Drop the StatusTrimmed condition if it exists.
		if isCondRemoved := meta.RemoveStatusCondition(&work.Status.Conditions, fleetv1beta1.WorkConditionTypeStatusTrimmed); isCondRemoved {
			klog.V(2).InfoS("StatusTrimmed condition removed from Work object status", "work", klog.KObj(work))
		}
		return
	}

	// Set or update the StatusTrimmed condition.
	meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{
		Type:               fleetv1beta1.WorkConditionTypeStatusTrimmed,
		Status:             metav1.ConditionTrue,
		Reason:             WorkStatusTrimmedDueToOversizedStatusReason,
		Message:            fmt.Sprintf(WorkStatusTrimmedDueToOversizedStatusMsgTmpl, sizeDeltaBytes, sizeLimitBytes),
		ObservedGeneration: work.Generation,
	})
}

func shouldSkipStatusUpdate(isDriftedOrDiffed, isStatusBackReportingOn bool, originalStatus, currentStatus *fleetv1beta1.WorkStatus) bool {
	if isDriftedOrDiffed || isStatusBackReportingOn {
		// Always proceed with status update if there are drifts/diffs detected or if status back-reporting is on.
		// This is necessary as the drift/diff details and back-reported status data are timestamped and the timestamps are
		// always refreshed per reconciliation loop.
		return false
	}

	// Skip status update if there is no change in the status.
	return equality.Semantic.DeepEqual(originalStatus, currentStatus)
}
