/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet/pkg/utils/controller"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

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

	// Use the now timestamp as the observation time.
	now := metav1.Now()

	// Rebuild the manifest conditions.

	// Pre-allocate the slice.
	rebuiltManifestConds := make([]fleetv1beta1.ManifestCondition, len(bundles))
	for idx := range bundles {
		bundle := bundles[idx]

		// Update the manifest condition based on the bundle processing results.
		manifestCond := &rebuiltManifestConds[idx]
		manifestCond.Identifier = *bundle.id
		manifestCond.Conditions = []metav1.Condition{}

		// Note that per API definition, the observed generation of a manifest condition is that
		// of the applied resource, not that of the Work object.
		inMemberClusterObjGeneration := int64(0)
		if bundle.inMemberClusterObj != nil {
			inMemberClusterObjGeneration = bundle.inMemberClusterObj.GetGeneration()
		}
		setManifestAppliedCondition(manifestCond, bundle.applyResTyp, bundle.applyErr, inMemberClusterObjGeneration)
		setManifestAvailableCondition(manifestCond, bundle.availabilityResTyp, bundle.availabilityErr, inMemberClusterObjGeneration)

		// Check if a first drifted timestamp has been set; if not, set it to the current time.
		firstDriftedTimestamp := bundle.firstDriftedTimestamp
		if firstDriftedTimestamp == nil {
			firstDriftedTimestamp = &now
		}
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
		firstDiffedTimestamp := bundle.firstDiffedTimestamp
		if firstDiffedTimestamp == nil {
			firstDiffedTimestamp = &now
		}
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
	}

	// Refresh the Work object status conditions.

	// Do a sanity check.
	if appliedManifestsCount > manifestCount || availableAppliedObjectsCount > manifestCount {
		// Normally this should never happen.
		return controller.NewUnexpectedBehaviorError(
			fmt.Errorf("the number of applied manifests (%d) or available applied objects (%d) exceeds the total number of manifests (%d)",
				appliedManifestsCount, availableAppliedObjectsCount, manifestCount))
	}

	setWorkAppliedCondition(&work.Status.Conditions, manifestCount, appliedManifestsCount, work.Generation)
	setWorkAvailableCondition(&work.Status.Conditions, manifestCount, availableAppliedObjectsCount, work.Generation)
	work.Status.ManifestConditions = rebuiltManifestConds

	// Update the Work object status.
	if err := r.hubClient.Status().Update(ctx, work); err != nil {
		return controller.NewAPIServerError(false, err)
	}
	return nil
}

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
		return controller.NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Refreshed AppliedWork object status",
		klog.KObj(appliedWork))
	return nil
}
