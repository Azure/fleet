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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/resource"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/ownerreferences"
)

func (r *Reconciler) processManifests(ctx context.Context, manifestProcessingStates []*manifestProcessingState) error {
	// Process all manifests in parallel.
	//
	// There are cases where certain groups of manifests should not be processed in parallel with
	// each other (e.g., a config map must be applied after its owner namespace is applied);
	// to address this situation, manifests are processed in waves: manifests in the same wave are
	// processed in parallel, while different waves are processed sequentially.

	// Organize the manifests into different waves for parallel processing based on their
	// GVR information.
	processingWaves := organizeManifestsIntoProcessingWaves(manifestProcessingStates)
	for idx := range processingWaves {
		statesInWave := processingWaves[idx].states

		// TO-DO (chenyu1): evaluate if there is a need to avoid repeated closure
		// assignment just for capturing variables.
		doWork := func(piece int) {
			state := statesInWave[piece]
			if state.applyErr != nil {
				// Skip a manifest if it has failed pre-processing.
				//
				// This added as a sanity check as the organization step normally
				// would have already skipped all the manifests with processing failures.
				return
			}

			r.processOneManifest(ctx, state)
			klog.V(2).InfoS("Processed a manifest",
				"manifestObj", klog.KObj(state.manifestObj), "GVR", *state.gvr,
				"work", klog.KObj(state.fromWorkObj), "primaryWork", klog.KObj(state.fromPrimaryWorkObject))
		}

		r.parallelizer.ParallelizeUntil(ctx, len(statesInWave), doWork, fmt.Sprintf("processingManifestsInWave%d", idx))

		// Unlike some other steps in the reconciliation loop, the manifest processing step does not end
		// with a contextual API call; consequently, if the context has been cancelled during this step,
		// some manifest might not get processed at all, and passing such states to the next step may trigger
		// unexpected behaviors. To address this, at the end of this step the work applier checks for context
		// cancellation directly.
		if err := ctx.Err(); err != nil {
			klog.V(2).InfoS("Manifest processing has been interrupted as the main context has been cancelled")
			return errors.NewTransientError(err, "manifest processing has been interrupted")
		}
	}
	return nil
}

func (r *Reconciler) processOneManifest(ctx context.Context, manifestProcessingState *manifestProcessingState) {
	manifestObj := manifestProcessingState.manifestObj
	ownerWorkObj := manifestProcessingState.fromWorkObj
	gvr := manifestProcessingState.gvr

	// Firstly, attempt to find if an object has been created in the member cluster based on the manifest object.
	if shouldSkipProcessing := r.findInMemberClusterObjectFor(ctx, manifestProcessingState); shouldSkipProcessing {
		return
	}

	// Take over the object in the member cluster that corresponds to the manifest object
	// if applicable.
	//
	// KubeFleet will perform the takeover if:
	// a) KubeFleet can find an object in the member cluster that corresponds to the manifest object, and
	//    it is not currently owned by KubeFleet (specifically the expected appliedWork object); and
	// b) takeover is allowed (i.e., the WhenAlreadyExists option in the sync strategy is not set to ReportError).
	if shouldSkipProcessing := r.takeOverInMemberClusterObjectIfApplicable(ctx, manifestProcessingState); shouldSkipProcessing {
		return
	}

	// For both the ClientSideApply and ServerSideApply methods, ownership is a hard requirement.
	// Skip the rest of the processing step if the resource has been created in the member cluster
	// but KubeFleet is not listed as an owner of the resource in the member cluster yet (i.e., the
	// takeover process does not run). This check is necessary as KubeFleet supports the
	// WhenAlreadyExists option ReportError.
	if !canApplyWithOwnership(manifestProcessingState) {
		wrappedErr := errors.NewUserError(nil, "no ownership of the object in the member cluster; takeover is needed",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		manifestProcessingState.applyErr = wrappedErr
		manifestProcessingState.applyRes = ApplyResTypeNotTakenOver
		klog.V(2).InfoS("Ownership is not established yet; skip the apply op",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		return
	}

	// Perform a round of drift detection before running the apply op, if the ApplyStrategy
	// dictates that an apply op can only be run when there are no drifts found.
	if shouldSkipProcessing := r.performPreApplyDriftDetectionIfApplicable(ctx, manifestProcessingState); shouldSkipProcessing {
		return
	}

	// Perform the apply op.
	appliedObj, err := r.apply(ctx, manifestProcessingState)
	if err != nil {
		wrappedErr := errors.Wraps(err, "failed to apply the manifest",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		manifestProcessingState.applyErr = wrappedErr
		manifestProcessingState.applyRes = ApplyResTypeFailedToApply
		klog.ErrorS(err, "Failed to apply the manifest", errors.Args(wrappedErr)...)
		return
	}
	if appliedObj != nil {
		// Track the newly applied object, if an apply op has been run.
		manifestProcessingState.inMemberClusterObj = appliedObj
	}
	klog.V(2).InfoS("Apply process completed",
		"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
		"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))

	// Perform another round of drift detection after the apply op, if the ApplyStrategy dictates
	// that drift detection should be done in full comparison mode.
	//
	// Drift detection is currently always enabled in KubeFleet. At this stage of execution, it is
	// safe for us to assume that all the managed fields have been overwritten by the just
	// completed apply op (or no apply op is necessary); consequently, no further drift
	// detection is necessary if the partial comparison mode is used. However, for the full
	// comparison mode, the apply op might not to able to resolve all the drifts, should there
	// be any change made on the unmanaged fields; and KubeFleet would need to perform another
	// round of drift detection.
	if shouldSkipProcessing := r.performPostApplyDriftDetectionIfApplicable(ctx, manifestProcessingState); shouldSkipProcessing {
		return
	}

	manifestProcessingState.applyRes = ApplyResTypeApplied
	klog.V(2).InfoS("Manifest processing completed",
		"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
		"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
}

// findInMemberClusterObjectFor attempts to find the corresponding object in the member cluster
// for a given manifest object.
//
// Note that it is possible that the object has not been created yet in the member cluster.
func (r *Reconciler) findInMemberClusterObjectFor(
	ctx context.Context,
	manifestProcessingState *manifestProcessingState,
) (shouldSkipProcessing bool) {
	manifestObj := manifestProcessingState.manifestObj
	ownerWorkObj := manifestProcessingState.fromWorkObj
	gvr := manifestProcessingState.gvr

	inMemberClusterObj, err := r.spokeDynamicClient.
		Resource(*gvr).
		Namespace(manifestObj.GetNamespace()).
		Get(ctx, manifestObj.GetName(), metav1.GetOptions{})
	switch {
	case err == nil:
		// An object derived from the manifest object has been found in the member cluster.
		klog.V(2).InfoS("Found the corresponding object for the manifest object in the member cluster",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		manifestProcessingState.inMemberClusterObj = inMemberClusterObj
		return false
	case apierrors.IsNotFound(err):
		// The manifest object has never been applied before.
		klog.V(2).InfoS("The manifest object has not been created in the member cluster yet",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		return false
	default:
		// An unexpected error has occurred.
		wrappedErr := errors.NewAPIServerError(err,
			"failed to find the corresponding object for the manifest object in the member cluster",
			false, // false as the dynamic client is non-caching.
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		manifestProcessingState.applyErr = wrappedErr
		manifestProcessingState.applyRes = ApplyResTypeFailedToFindObjInMemberCluster
		klog.ErrorS(err,
			"Failed to find the corresponding object for the manifest object in the member cluster",
			errors.Args(wrappedErr)...)
		return true
	}
}

// takeOverInMemberClusterObjectIfApplicable attempts to take over an object in the member cluster
// as needed.
func (r *Reconciler) takeOverInMemberClusterObjectIfApplicable(
	ctx context.Context,
	manifestProcessingState *manifestProcessingState,
) (shouldSkipProcessing bool) {
	manifestObj := manifestProcessingState.manifestObj
	inMemberClusterObj := manifestProcessingState.inMemberClusterObj
	ownerWorkObj := manifestProcessingState.fromWorkObj
	gvr := manifestProcessingState.gvr

	if isOwnedByOlderKubeFleetAPIObjects(inMemberClusterObj) {
		wrappedErr := errors.NewUserError(nil,
			"the object to process is already owned by another KubeFleet placement API object",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		manifestProcessingState.applyErr = wrappedErr
		manifestProcessingState.applyRes = ApplyResTypeOwnedByOtherKubeFleetAPIObjects
		klog.ErrorS(wrappedErr, "Failed to check if takeover is needed", errors.Args(wrappedErr)...)
		return true
	}

	if !shouldInitiateTakeOverAttempt(manifestProcessingState) {
		// Takeover is not necessary; proceed with the processing.
		klog.V(2).InfoS("Takeover is not needed; skip the step",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		return false
	}

	// Take over the object. Note that this steps adds only the owner reference; no other
	// fields are modified (on the object from the member cluster).
	takenOverInMemberClusterObj, configDiffs, diffCalculatedInDegradedMode, err := r.takeOverPreExistingObject(ctx, manifestProcessingState)
	switch {
	case err != nil:
		// An unexpected error has occurred.
		wrappedErr := errors.Wraps(err, "failed to take over a pre-existing object",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		manifestProcessingState.applyErr = wrappedErr
		manifestProcessingState.applyRes = ApplyResTypeFailedToTakeOver
		klog.ErrorS(err, "Failed to complete the takeover process", errors.Args(wrappedErr)...)
		return true
	case len(configDiffs) > 0 && diffCalculatedInDegradedMode:
		// Takeover cannot be performed as configuration differences are found between the manifest
		// object and the object in the member cluster in degraded mode.
		//
		// Note that though degraded diff calculation itself is not considered as an error, the
		// presence of diffs is indeed an error.
		manifestProcessingState.diffs = configDiffs
		manifestProcessingState.applyErr = errors.NewUserError(nil,
			"cannot take over object: configuration differences are found between the manifest object and the corresponding object in the member cluster in degraded mode (full comparison is performed instead of partial comparison, as the manifest object is considered to be invalid by the member cluster API server)",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		manifestProcessingState.applyRes = ApplyResTypeFailedToTakeOver
		klog.V(2).InfoS("Cannot take over object as configuration differences are found between the manifest object and the corresponding object in the member cluster in degraded mode",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		return true
	case len(configDiffs) > 0:
		// Takeover cannot be performed as configuration differences are found between the manifest
		// object and the object in the member cluster.
		manifestProcessingState.diffs = configDiffs
		manifestProcessingState.applyErr = errors.NewUserError(nil,
			"cannot take over object: configuration differences are found between the manifest object and the corresponding object in the member cluster",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		manifestProcessingState.applyRes = ApplyResTypeFailedToTakeOver
		klog.V(2).InfoS("Cannot take over object as configuration differences are found between the manifest object and the corresponding object in the member cluster",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		return true
	}

	// Takeover process is completed; track the newly refreshed object from the member cluster.
	manifestProcessingState.inMemberClusterObj = takenOverInMemberClusterObj
	klog.V(2).InfoS("The corresponding object has been taken over",
		"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
		"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
	return false
}

// canApplyWithOwnership checks if KubeFleet can perform an apply op, knowing that KubeFleet has
// acquired the ownership of the object, or that the object has not been created yet.
//
// Note that this function does not concern co-ownership; such checks are executed elsewhere.
func canApplyWithOwnership(manifestProcessingState *manifestProcessingState) bool {
	inMemberClusterObj := manifestProcessingState.inMemberClusterObj
	if inMemberClusterObj == nil {
		// The object has not been created yet; KubeFleet can apply the object.
		return true
	}

	// Verify if the object is owned by KubeFleet.
	curOwners := inMemberClusterObj.GetOwnerReferences()
	for idx := range curOwners {
		if ownerreferences.AreEqual(&curOwners[idx], manifestProcessingState.ownedBy) {
			return true
		}
	}
	return false
}

// performPreApplyDriftDetectionIfApplicable checks if pre-apply drift detection is needed and
// runs the drift detection process if applicable.
func (r *Reconciler) performPreApplyDriftDetectionIfApplicable(
	ctx context.Context,
	manifestProcessingState *manifestProcessingState,
) (shouldSkipProcessing bool) {
	manifestObj := manifestProcessingState.manifestObj
	inMemberClusterObj := manifestProcessingState.inMemberClusterObj
	ownerWorkObj := manifestProcessingState.fromWorkObj
	gvr := manifestProcessingState.gvr

	isPreApplyDriftDetectionNeeded, err := shouldPerformPreApplyDriftDetection(manifestProcessingState)
	switch {
	case err != nil:
		// KubeFleet cannot determine if pre-apply drift detection is needed; this will only
		// happen if the hash calculation process fails, specifically when KubeFleet cannot
		// marshal the manifest object into its JSON representation. This should never happen,
		// especially considering that the manifest object itself has been
		// successfully decoded at this point of execution.
		wrappedErr := errors.NewUnexpectedError(err, "failed to determine if pre-apply drift detection is needed",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		manifestProcessingState.applyErr = wrappedErr
		manifestProcessingState.applyRes = ApplyResTypeFailedToRunDriftDetection
		klog.ErrorS(err, "Failed to determine if pre-apply drift detection is needed", errors.Args(wrappedErr)...)
		return true
	case !isPreApplyDriftDetectionNeeded:
		// Drift detection is not needed; proceed with the processing.
		klog.V(2).InfoS("Pre-apply drift detection is not needed; skip the step",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		return false
	}

	// Run the drift detection process.
	diffs, driftsCalculatedInDegradedMode, err := r.diffBetweenManifestAndInMemberClusterObjects(ctx,
		gvr, manifestObj, inMemberClusterObj,
		manifestProcessingState.fromPrimaryWorkObject.Spec.SyncStrategy.ComparisonOption)
	switch {
	case err != nil:
		// An unexpected error has occurred.
		wrappedErr := errors.Wraps(err, "failed to calculate pre-apply drifts between the manifest and the object from the member cluster",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		manifestProcessingState.applyErr = wrappedErr
		manifestProcessingState.applyRes = ApplyResTypeFailedToRunDriftDetection
		klog.ErrorS(err, "Failed to complete pre-apply drift detection", errors.Args(wrappedErr)...)
		return true
	case len(diffs) > 0 && driftsCalculatedInDegradedMode:
		// Configuration drifts are found in degraded mode.
		//
		// Note that though degraded drift calculation itself is not considered as an error, the
		// presence of drifts in the pre-apply drift detection step is indeed an error.
		manifestProcessingState.diffs = diffs
		manifestProcessingState.applyErr = errors.NewUserError(nil,
			"cannot apply manifest: drifts are found between the manifest and the object from the member cluster in degraded mode (full comparison is performed instead of partial comparison, as the manifest object is considered to be invalid by the member cluster API server)",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		manifestProcessingState.applyRes = ApplyResTypeFoundDriftsInDegradedMode
		klog.V(2).InfoS("Cannot apply manifest: drifts are found between the manifest and the object from the member cluster in degraded mode",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		return true
	case len(diffs) > 0:
		// Drifts are found in the pre-apply drift detection process.
		manifestProcessingState.diffs = diffs
		manifestProcessingState.applyErr = errors.NewUserError(nil,
			"cannot apply manifest: drifts are found between the manifest and the object from the member cluster",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		manifestProcessingState.applyRes = ApplyResTypeFoundDrifts
		klog.V(2).InfoS("Cannot apply manifest: drifts are found between the manifest and the object from the member cluster",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		return true
	default:
		// No drifts are found in the pre-apply drift detection process; carry on with the apply op.
		klog.V(2).InfoS("Pre-apply drift detection completed; no drifts are found",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		return false
	}
}

// shouldPerformPreApplyDriftDetection checks if pre-apply drift detection should be performed.
func shouldPerformPreApplyDriftDetection(manifestProcessingState *manifestProcessingState) (bool, error) {
	// Drift detection is performed before the apply op if (and only if):
	// * KubeFleet reports that the manifest has been applied before (i.e., inMemberClusterObj exists); and
	// * The sync strategy dictates that a drift should be reported rather than overwritten; and
	// * The hash of the manifest object is consistent with the last applied manifest object hash
	//   annotation on the corresponding resource in the member cluster (i.e., the same manifest
	//   object has been applied before).
	inMemberClusterObj := manifestProcessingState.inMemberClusterObj
	syncStrategy := manifestProcessingState.fromPrimaryWorkObject.Spec.SyncStrategy
	if syncStrategy.WhenDrifted != placementv1alpha1.WhenDriftedOptionReportError || inMemberClusterObj == nil {
		// A shortcut to save some overhead.
		return false, nil
	}

	cleanedManifestObj := discardFieldsIrrelevantInComparisonFrom(manifestProcessingState.manifestObj)
	manifestObjHash, err := resource.HashOf(cleanedManifestObj.Object)
	if err != nil {
		return false, err
	}

	inMemberClusterObjLastAppliedManifestObjHash := inMemberClusterObj.GetAnnotations()[placementv1alpha1.LastAppliedManifestHashAnnotationKey]
	return manifestObjHash == inMemberClusterObjLastAppliedManifestObjHash, nil
}

// performPostApplyDriftDetectionIfApplicable checks if post-apply drift detection is needed and
// runs the drift detection process if applicable.
func (r *Reconciler) performPostApplyDriftDetectionIfApplicable(
	ctx context.Context,
	manifestProcessingState *manifestProcessingState,
) (shouldSkipProcessing bool) {
	manifestObj := manifestProcessingState.manifestObj
	inMemberClusterObj := manifestProcessingState.inMemberClusterObj
	ownerWorkObj := manifestProcessingState.fromWorkObj
	gvr := manifestProcessingState.gvr
	syncStrategy := manifestProcessingState.fromPrimaryWorkObject.Spec.SyncStrategy

	if !shouldPerformPostApplyDriftDetection(syncStrategy) {
		// Post-apply drift detection is not needed; proceed with the processing.
		klog.V(2).InfoS("Post-apply drift detection is not needed; skip the step",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		return false
	}

	diffs, driftsCalculatedInDegradedMode, err := r.diffBetweenManifestAndInMemberClusterObjects(ctx,
		gvr, manifestObj, inMemberClusterObj, syncStrategy.ComparisonOption)
	switch {
	case err != nil:
		// An unexpected error has occurred.
		//
		// This case counts as a partial error; the apply op has been completed, but KubeFleet
		// cannot determine if there are any drifts.
		wrappedErr := errors.Wraps(err, "failed to calculate post-apply drifts between the manifest object and the object from the member cluster",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		manifestProcessingState.applyErr = wrappedErr
		manifestProcessingState.applyRes = ApplyResTypeAppliedWithFailedDriftDetection
		klog.ErrorS(err,
			"Failed to complete post-apply drift detection",
			errors.Args(wrappedErr)...)
		return true
	case len(diffs) > 0 && driftsCalculatedInDegradedMode:
		// Configuration drifts are found, but they were calculated in degraded mode.
		//
		// Normally this should never happen, as post-apply drift detection will only run if the full
		// comparison mode is enabled, but degraded mode only applies to partial comparison mode.
		manifestProcessingState.diffs = diffs
		klog.V(2).InfoS("Post-apply drift detection completed; drifts are found in degraded mode",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		// The presence of such drifts is not considered as an error.
		return false
	case len(diffs) > 0:
		// Drifts are found in the post-apply drift detection process.
		manifestProcessingState.diffs = diffs
		klog.V(2).InfoS("Post-apply drift detection completed; drifts are found",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		// The presence of such drifts is not considered as an error.
		return false
	default:
		// No drifts are found in the post-apply drift detection process.
		klog.V(2).InfoS("Post-apply drift detection completed; no drifts are found",
			"manifestObj", klog.KObj(manifestObj), "GVR", *gvr,
			"work", klog.KObj(ownerWorkObj), "primaryWork", klog.KObj(manifestProcessingState.fromPrimaryWorkObject))
		return false
	}
}

// shouldPerformPostApplyDriftDetection checks if post-apply drift detection should be performed.
func shouldPerformPostApplyDriftDetection(syncStrategy *placementv1alpha1.SyncStrategy) bool {
	// Post-apply drift detection is performed if (and only if):
	// * The sync strategy dictates that drift detection should run in full comparison mode.
	return syncStrategy.ComparisonOption == placementv1alpha1.ComparisonOptionFullComparison
}
