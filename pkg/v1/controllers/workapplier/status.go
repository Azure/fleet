/*
Copyright 2026 The KubeFleet Authors.

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
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

// refreshWorkStatus refreshes the status of a Work object based on the processing results of its manifests.
func (r *Reconciler) refreshWorkStatus(
	ctx context.Context,
	workObjProcessingStates []*workObjectProcessingState,
) error {
	// Note (chenyu1): it is possible to run this method in parallel; however, for simplicity reasons,
	// considering that in most of the time the count of work objects would be low, currently
	// KubeFleet still does the status refresh sequentially.
	for idx := range workObjProcessingStates {
		workObjProcessingState := workObjProcessingStates[idx]
		if err := r.refreshOneWorkStatus(ctx, workObjProcessingState); err != nil {
			wrappedErr := errors.Wraps(err, "", "work", klog.KObj(workObjProcessingState.work))
			return wrappedErr
		}
	}
	return nil
}

func (r *Reconciler) refreshOneWorkStatus(
	ctx context.Context,
	workObjProcessingState *workObjectProcessingState,
) error {
	work := workObjProcessingState.work
	statusCopy := work.Status.DeepCopy()

	// Note (chenyu1): it is possible to run this method in parallel; however, for simplicity reasons,
	// considering that in most of the time the count of manifests would be low, currently
	// KubeFleet still does the status refresh sequentially.

	// Set up the counters.
	manifestCount := len(workObjProcessingState.manifestProcessingStates)
	appliedManifestsCount := 0
	availableAppliedObjectsCount := 0
	untrackableAppliedObjectsCount := 0

	// Use the now timestamp as the observation time.
	now := metav1.Now()

	// Build an empty list of per-manifest statuses and port back existing per-manifest statuses to the list
	// as applicable.
	//
	// This step is necessary at the moment primarily because the manifest status features `metav1.Condition`,
	// the LastTransitionTime field of which requires that KubeFleet track the last known condition.
	//
	// Note that the refreshed manifest status array is pre-allocated and is of the same length as the number of
	// manifest processing states.
	refreshedManifestStatuses, refreshedManifestStatusesIndex := prepareRefreshedManifestStatusWithIndex(workObjProcessingState.manifestProcessingStates)
	portBackExistingManifestStatuses(statusCopy.Manifests, refreshedManifestStatuses, refreshedManifestStatusesIndex)

	// Refresh per-manifest statuses.
	for idx := range workObjProcessingState.manifestProcessingStates {
		manifestProcessingState := workObjProcessingState.manifestProcessingStates[idx]
		refreshOneManifestStatus(manifestProcessingState, &refreshedManifestStatuses[idx], now)

		// Tally the stats.
		if isManifestObjectApplied(manifestProcessingState.applyRes) {
			appliedManifestsCount++
		}
		if isAppliedObjectAvailable(manifestProcessingState.availabilityCheckRes) {
			availableAppliedObjectsCount++
		}
		if manifestProcessingState.availabilityCheckRes == AvailabilityResultTypeNotTrackable {
			untrackableAppliedObjectsCount++
		}
	}
	statusCopy.Manifests = refreshedManifestStatuses

	// Set work object status conditions.

	// Do a sanity check.
	if appliedManifestsCount > manifestCount ||
		availableAppliedObjectsCount > manifestCount ||
		untrackableAppliedObjectsCount > manifestCount {
		// Normally this should never happen.
		return errors.NewUnexpectedError(nil,
			"the number of applied manifests, available applied objects, or untrackable applied objects exceeds the total number of manifests",
			"appliedManifestsCount", appliedManifestsCount,
			"availableAppliedObjectsCount", availableAppliedObjectsCount,
			"untrackableAppliedObjectsCount", untrackableAppliedObjectsCount,
			"manifestCount", manifestCount)
	}
	if statusCopy.Conditions == nil {
		statusCopy.Conditions = []metav1.Condition{}
	}
	setWorkAppliedCondition(statusCopy, manifestCount, appliedManifestsCount, work.Generation)
	setWorkAvailableCondition(statusCopy, manifestCount, availableAppliedObjectsCount, untrackableAppliedObjectsCount, work.Generation)

	// TO-DO (chenyu1): if needed, trim the reported status so that the work object will not grow over Kubernetes'
	// object size limit.
	if shouldSkipStatusUpdate(&work.Status, statusCopy) {
		// No status change found; skip the update.
		klog.V(2).InfoS("No status change found for Work object; skip the status update", "work", klog.KObj(work))
	} else {
		klog.V(2).InfoS("Refreshing work object status", "work", klog.KObj(work))

		work.Status = *statusCopy
		if err := r.hubClient.Status().Update(ctx, work); err != nil {
			return errors.NewAPIServerError(err, "failed to update work object status", false, "work", klog.KObj(work))
		}
	}

	return nil
}

func prepareRefreshedManifestStatusWithIndex(manifestProcessingStates []*manifestProcessingState) (
	[]placementv1alpha1.PerManifestStatus, map[string]int) {
	// Pre-allocate the status slice.
	refreshedManifestStatuses := make([]placementv1alpha1.PerManifestStatus, len(manifestProcessingStates))

	refreshedManifestStatusesIndex := make(map[string]int, len(manifestProcessingStates))
	for idx := range manifestProcessingStates {
		manifestProcessingState := manifestProcessingStates[idx]
		idStr := manifestProcessingState.idStr

		if len(manifestProcessingState.idStr) == 0 {
			// There might be manifests without a valid identifier in the bundle set
			// (e.g., a decoding error has occurred when processing a bundle).
			// KubeFleet will skip these bundles, as there is no need to port back
			// information for such manifests any way for obvious reasons (manifest itself is not
			// identifiable). This is not considered as an error.
			continue
		}
		refreshedManifestStatusesIndex[idStr] = idx
	}
	return refreshedManifestStatuses, refreshedManifestStatusesIndex
}

func portBackExistingManifestStatuses(
	existingManifestStatuses []placementv1alpha1.PerManifestStatus,
	refreshedManifestStatuses []placementv1alpha1.PerManifestStatus,
	refreshedManifestStatusesIndex map[string]int,
) {
	for idx := range existingManifestStatuses {
		existingManifestStatus := existingManifestStatuses[idx]
		existingManifestId := &existingManifestStatus.Identifier
		if existingManifestId.Kind == "" ||
			existingManifestId.Resource == "" ||
			existingManifestId.Name == "" {
			// It is OK for an existing manifest status to not have a valid identifier; this
			// happens when the manifest status was previously associated with a manifest
			// that cannot be decoded. For obvious reasons KubeFleet does not need to port back
			// such manifest conditions any way.
			continue
		}
		existingManifestIdStr := formatManifestIdentifierStr(existingManifestId)

		if idx, found := refreshedManifestStatusesIndex[existingManifestIdStr]; found {
			// A match can be found via the index; port back the existing manifest status.
			refreshedManifestStatuses[idx] = *existingManifestStatus.DeepCopy()
		}
	}
}

func refreshOneManifestStatus(manifestProcessingState *manifestProcessingState,
	manifestStatusToRefresh *placementv1alpha1.PerManifestStatus,
	now metav1.Time,
) {
	// Populate the identifier in the manifest status.
	//
	// If the status has been ported back, the identifier written here is guaranteed to match the existing one.
	manifestStatusToRefresh.Identifier = *manifestProcessingState.id.DeepCopy()

	// Prepare the list of conditions in the manifest status.
	if manifestStatusToRefresh.Conditions == nil {
		manifestStatusToRefresh.Conditions = []metav1.Condition{}
	}

	// Set the conditions.
	//
	// Note that per API definition, the observed generation of a manifest condition is that
	// of the applied resource, not that of the Work object.
	var inMemberClusterObjGeneration *int64
	if manifestProcessingState.inMemberClusterObj != nil {
		inMemberClusterObjGeneration = ptr.To(manifestProcessingState.inMemberClusterObj.GetGeneration())
	}
	setManifestStatusAppliedCondition(manifestStatusToRefresh, manifestProcessingState.applyRes, manifestProcessingState.applyErr, inMemberClusterObjGeneration)
	setManifestStatusAvailableCondition(manifestStatusToRefresh, manifestProcessingState.availabilityCheckRes, manifestProcessingState.availabilityCheckErr, inMemberClusterObjGeneration)
	setManifestStatusDiffDetails(manifestStatusToRefresh, manifestProcessingState.diffs, inMemberClusterObjGeneration, now)
}

// setManifestStatusAppliedCondition sets the Applied condition on a manifest status.
func setManifestStatusAppliedCondition(
	manifestStatus *placementv1alpha1.PerManifestStatus,
	applyRes ApplyResultType,
	applyErr error,
	inMemberClusterObjGeneration *int64,
) {
	// If the in-cluster object generation is not available (i.e., there is no in-cluster object), use the default
	// value 0.
	observedGeneration := int64(0)
	if inMemberClusterObjGeneration != nil {
		observedGeneration = *inMemberClusterObjGeneration
	}

	var appliedCond metav1.Condition
	switch {
	case applyRes == ApplyResTypeApplied:
		// The manifest has been successfully applied.
		appliedCond = metav1.Condition{
			Type:               placementv1alpha1.ManifestCondTypeApplied,
			Status:             metav1.ConditionTrue,
			Reason:             string(ApplyResTypeApplied),
			Message:            ApplyResTypeAppliedDescription,
			ObservedGeneration: observedGeneration,
		}
	case applyRes == ApplyResTypeAppliedWithFailedDriftDetection:
		// The manifest has been successfully applied, but drift detection has failed.
		//
		// At this moment KubeFleet does not prepare a dedicated condition for drift detection outcomes.
		appliedCond = metav1.Condition{
			Type:               placementv1alpha1.ManifestCondTypeApplied,
			Status:             metav1.ConditionTrue,
			Reason:             string(ApplyResTypeAppliedWithFailedDriftDetection),
			Message:            ApplyResTypeAppliedWithFailedDriftDetectionDescription,
			ObservedGeneration: observedGeneration,
		}
	case !allApplyResTypes.Has(applyRes):
		// Do a sanity check; verify if the returned result type is a valid one.
		// Normally this branch should never run.
		wrappedErr := errors.NewUnexpectedError(applyErr, "found an unexpected apply result type",
			"manifestID", manifestStatus.Identifier, "applyRes", applyRes)
		klog.ErrorS(wrappedErr, "Failed to set the Applied condition", errors.Args(wrappedErr)...)
		// The work applier will consider this to be an apply failure.
		appliedCond = metav1.Condition{
			Type:               placementv1alpha1.ManifestCondTypeApplied,
			Status:             metav1.ConditionFalse,
			Reason:             string(ApplyResTypeFailedToApply),
			Message:            fmt.Sprintf("An unexpected apply result is returned (%s) with error (%s)", applyRes, applyErr.Error()),
			ObservedGeneration: observedGeneration,
		}
	default:
		// The apply op fails.
		appliedCond = metav1.Condition{
			Type:               placementv1alpha1.ManifestCondTypeApplied,
			Status:             metav1.ConditionFalse,
			Reason:             string(applyRes),
			Message:            fmt.Sprintf(ApplyResTypeFailedToApplyDescription, applyErr),
			ObservedGeneration: observedGeneration,
		}
	}

	meta.SetStatusCondition(&manifestStatus.Conditions, appliedCond)
}

// setManifestStatusAvailableCondition sets the Available condition on a manifest status.
func setManifestStatusAvailableCondition(
	manifestStatus *placementv1alpha1.PerManifestStatus,
	availabilityCheckRes AvailabilityCheckResultType,
	availabilityCheckErr error,
	inMemberClusterObjGeneration *int64,
) {
	// If the in-cluster object generation is not available (i.e., there is no in-cluster object), use the default
	// value 0.
	observedGeneration := int64(0)
	if inMemberClusterObjGeneration != nil {
		observedGeneration = *inMemberClusterObjGeneration
	}

	var availableCond *metav1.Condition
	switch availabilityCheckRes {
	case AvailabilityResultTypeSkipped:
		// The availability check has been skipped as the manifest has not been applied yet; in this
		// case no Available condition is set.
	case AvailabilityResultTypeFailed:
		// The availability check has failed.
		availableCond = &metav1.Condition{
			Type:               placementv1alpha1.ManifestCondTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             string(AvailabilityResultTypeFailed),
			Message:            fmt.Sprintf(AvailabilityResultTypeFailedDescription, availabilityCheckErr),
			ObservedGeneration: observedGeneration,
		}
	case AvailabilityResultTypeNotYetAvailable:
		// The manifest is not yet available.
		availableCond = &metav1.Condition{
			Type:               placementv1alpha1.ManifestCondTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             string(AvailabilityResultTypeNotYetAvailable),
			Message:            AvailabilityResultTypeNotYetAvailableDescription,
			ObservedGeneration: observedGeneration,
		}
	case AvailabilityResultTypeNotTrackable:
		// KubeFleet cannot track the availability of the manifest.
		availableCond = &metav1.Condition{
			Type:               placementv1alpha1.ManifestCondTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             string(AvailabilityResultTypeNotTrackable),
			Message:            AvailabilityResultTypeNotTrackableDescription,
			ObservedGeneration: observedGeneration,
		}
	default:
		// The manifest is available.
		availableCond = &metav1.Condition{
			Type:               placementv1alpha1.ManifestCondTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             string(AvailabilityResultTypeAvailable),
			Message:            AvailabilityResultTypeAvailableDescription,
			ObservedGeneration: observedGeneration,
		}
	}

	if availableCond == nil {
		// The conditions might be ported back from the previous run; a stale Available condition must be
		// removed when no new one is set.
		meta.RemoveStatusCondition(&manifestStatus.Conditions, placementv1alpha1.ManifestCondTypeAvailable)
		return
	}
	meta.SetStatusCondition(&manifestStatus.Conditions, *availableCond)
}

// setManifestStatusDiffDetails sets the diff details on a manifest status.
func setManifestStatusDiffDetails(
	manifestStatus *placementv1alpha1.PerManifestStatus,
	diffs []placementv1alpha1.PatchDetail,
	observedInMemberClusterGeneration *int64,
	now metav1.Time,
) {
	// Check if a first diffed timestamp has been set; if not, set it to the current time.
	firstDiffedTimestamp := now
	if manifestStatus.DiffDetails != nil && !manifestStatus.DiffDetails.FirstDiffedObservedTimestamp.IsZero() {
		firstDiffedTimestamp = manifestStatus.DiffDetails.FirstDiffedObservedTimestamp
	}

	// Reset the diff details (such details need no port-back).
	manifestStatus.DiffDetails = nil
	if len(diffs) == 0 {
		return
	}

	// Populate the diff details.
	manifestStatus.DiffDetails = &placementv1alpha1.DiffDetails{
		ObservedInMemberClusterGeneration: observedInMemberClusterGeneration,
		FirstDiffedObservedTimestamp:      firstDiffedTimestamp,
		ObservedDiffs:                     diffs,
	}
}

// isAppliedObjectAvailable returns whether an availability check result type indicates that an
// applied manifest object is available.
func isAppliedObjectAvailable(availabilityCheckRes AvailabilityCheckResultType) bool {
	return availabilityCheckRes == AvailabilityResultTypeAvailable || availabilityCheckRes == AvailabilityResultTypeNotTrackable
}

// setWorkAppliedCondition sets the Applied condition on a work object status.
//
// A work object is considered to be applied if all of its manifests have been successfully applied.
func setWorkAppliedCondition(
	workStatus *placementv1alpha1.WorkStatus,
	manifestCount, appliedManifestCount int,
	workGeneration int64,
) {
	var appliedCond metav1.Condition
	switch {
	case appliedManifestCount == manifestCount:
		// All manifests have been successfully applied.
		appliedCond = metav1.Condition{
			Type:   placementv1alpha1.WorkCondTypeApplied,
			Status: metav1.ConditionTrue,
			// Here KubeFleet reuses the same reason for individual manifests.
			Reason:             placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
			Message:            fmt.Sprintf("All %d manifest(s) have been successfully applied", manifestCount),
			ObservedGeneration: workGeneration,
		}
	default:
		// Not all manifests have been successfully applied.
		appliedCond = metav1.Condition{
			Type:               placementv1alpha1.WorkCondTypeApplied,
			Status:             metav1.ConditionFalse,
			Reason:             placementv1alpha1.WorkAppliedCondNotAllManifestsAppliedReason,
			Message:            fmt.Sprintf("%d out of %d manifest(s) have not yet been applied", manifestCount-appliedManifestCount, manifestCount),
			ObservedGeneration: workGeneration,
		}
	}

	meta.SetStatusCondition(&workStatus.Conditions, appliedCond)
	klog.V(2).InfoS("Applied condition set on the work object status",
		"appliedManifestCount", appliedManifestCount, "manifestCount", manifestCount)
}

// setWorkAvailableCondition sets the Available condition on a work object status.
//
// A work object is considered to be available if all of its applied manifests are available.
func setWorkAvailableCondition(
	workStatus *placementv1alpha1.WorkStatus,
	manifestCount, availableManifestCount, untrackableAppliedObjectsCount int,
	workGeneration int64,
) {
	var availableCond *metav1.Condition
	switch {
	case availableManifestCount == manifestCount && untrackableAppliedObjectsCount == 0:
		// All manifests are available.
		availableCond = &metav1.Condition{
			Type:               placementv1alpha1.WorkCondTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             placementv1alpha1.WorkAvailableCondAllManifestsAvailableReason,
			Message:            fmt.Sprintf("All %d manifest(s) are available", manifestCount),
			ObservedGeneration: workGeneration,
		}
	case availableManifestCount == manifestCount:
		// Some manifests are not trackable.
		availableCond = &metav1.Condition{
			Type:               placementv1alpha1.WorkCondTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             placementv1alpha1.WorkAvailableCondNotAllManifestsAvailableReason,
			Message:            fmt.Sprintf("All %d manifest(s) are available (with %d manifests untrackable)", manifestCount, untrackableAppliedObjectsCount),
			ObservedGeneration: workGeneration,
		}
	default:
		// Not all manifests are available.
		availableCond = &metav1.Condition{
			Type:               placementv1alpha1.WorkCondTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             placementv1alpha1.WorkAvailableCondNotAllManifestsAvailableReason,
			Message:            fmt.Sprintf("%d out of %d manifest(s) are not yet available", manifestCount-availableManifestCount, manifestCount),
			ObservedGeneration: workGeneration,
		}
	}

	meta.SetStatusCondition(&workStatus.Conditions, *availableCond)
	klog.V(2).InfoS("Available condition set on the work object status",
		"availableManifestCount", availableManifestCount,
		"untrackableAppliedObjectsCount", untrackableAppliedObjectsCount, "manifestCount", manifestCount)
}

func shouldSkipStatusUpdate(oldStatus, newStatus *placementv1alpha1.WorkStatus) bool {
	// Skip status update if there is no change in the status.
	return equality.Semantic.DeepEqual(oldStatus, newStatus)
}

// refreshAppliedWorkStatus refreshes the status of appliedWork objects.
func (r *Reconciler) refreshAppliedWorkStatus(
	ctx context.Context,
	workObjProcessingStates []*workObjectProcessingState,
) error {
	// Note (chenyu1): it is possible to run this method in parallel; however, for simplicity reasons,
	// considering that in most of the time the count of work objects would be low, currently
	// KubeFleet still does the status refresh sequentially.
	for idx := range workObjProcessingStates {
		workObjProcessingState := workObjProcessingStates[idx]
		if err := r.refreshOneAppliedWorkStatus(ctx, workObjProcessingState); err != nil {
			wrappedErr := errors.Wraps(err, "", "appliedWork", klog.KObj(workObjProcessingState.appliedWork))
			return wrappedErr
		}
	}
	return nil
}

func (r *Reconciler) refreshOneAppliedWorkStatus(
	ctx context.Context,
	workObjProcessingState *workObjectProcessingState,
) error {
	appliedWork := workObjProcessingState.appliedWork
	appliedWorkStatusCopy := appliedWork.Status.DeepCopy()

	manifestProcessingStates := workObjProcessingState.manifestProcessingStates

	// Note (chenyu1): It is possible to run this method in parallel; however, for simplicity reasons,
	// considering that in most of the time the count of manifests would be low, currently
	// KubeFleet still does the status refresh sequentially.

	// Pre-allocate the slice.
	//
	// Manifests that failed to get applied are not included in this list, hence
	// empty length.
	appliedResources := make([]placementv1alpha1.AppliedResource, 0, len(manifestProcessingStates))

	// Build the list of applied resources.
	for idx := range manifestProcessingStates {
		manifestProcessingState := manifestProcessingStates[idx]

		if isManifestObjectApplied(manifestProcessingState.applyRes) {
			appliedResources = append(appliedResources, placementv1alpha1.AppliedResource{
				ManifestIdentifier: *manifestProcessingState.id,
				UID:                manifestProcessingState.inMemberClusterObj.GetUID(),
			})
		}
	}

	// Update the AppliedWork object status.
	appliedWorkStatusCopy.AppliedResources = appliedResources

	// Skip the status update if no change found.
	if equality.Semantic.DeepEqual(&appliedWork.Status, appliedWorkStatusCopy) {
		klog.V(2).InfoS("No status change found for appliedWork object; skip the status update", "appliedWork", klog.KObj(appliedWork))
	} else {
		klog.V(2).InfoS("Refreshing appliedWork object status", "appliedWork", klog.KObj(appliedWork))

		appliedWork.Status = *appliedWorkStatusCopy
		if err := r.spokeClient.Status().Update(ctx, appliedWork); err != nil {
			klog.ErrorS(err, "Failed to update appliedWork status",
				"appliedWork", klog.KObj(appliedWork))
			return errors.NewAPIServerError(err, "failed to update appliedWork object status", false, "appliedWork", klog.KObj(appliedWork))
		}
	}
	return nil
}
