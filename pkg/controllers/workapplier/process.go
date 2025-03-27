/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/resource"
)

// processManifests processes all the manifests included in a Work object.
func (r *Reconciler) processManifests(
	ctx context.Context,
	bundles []*manifestProcessingBundle,
	work *fleetv1beta1.Work,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) {
	// TODO: We have to apply the namespace/crd/secret/configmap/pvc first
	// then we can process some of the manifests in parallel.
	for _, bundle := range bundles {
		if bundle.applyErr != nil {
			// Skip a manifest if it has failed pre-processing.
			continue
		}
		r.processOneManifest(ctx, bundle, work, expectedAppliedWorkOwnerRef)
		klog.V(2).InfoS("Processed a manifest", "manifestObj", klog.KObj(bundle.manifestObj), "work", klog.KObj(work))
	}
}

// processOneManifest processes a manifest (in the JSON format) embedded in the Work object.
func (r *Reconciler) processOneManifest(
	ctx context.Context,
	bundle *manifestProcessingBundle,
	work *fleetv1beta1.Work,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) {
	workRef := klog.KObj(work)
	manifestObjRef := klog.KObj(bundle.manifestObj)
	// Note (chenyu1): Fleet does not track references for objects in the member cluster as
	// the references should be the same as those of the manifest objects, provided that Fleet
	// does not support objects with generate names for now.

	// Firstly, attempt to find if an object has been created in the member cluster based on the manifest object.
	if shouldSkipProcessing := r.findInMemberClusterObjectFor(ctx, bundle, work, expectedAppliedWorkOwnerRef); shouldSkipProcessing {
		return
	}

	// Take over the object in the member cluster that corresponds to the manifest object
	// if applicable.
	//
	// Fleet will perform the takeover if:
	// a) Fleet can find an object in the member cluster that corresponds to the manifest object, and
	//    it is not currently owned by Fleet (specifically the expected AppliedWork object); and
	// b) takeover is allowed (i.e., the WhenToTakeOver option in the apply strategy is not set to Never).
	if shouldSkipProcessing := r.takeOverInMemberClusterObjectIfApplicable(ctx, bundle, work, expectedAppliedWorkOwnerRef); shouldSkipProcessing {
		return
	}

	// If the ApplyStrategy is of the ReportDiff mode, Fleet would
	// check for the configuration difference now; no drift detection nor apply op will be
	// executed.
	//
	// Note that this runs even if the object is not owned by Fleet.
	if shouldSkipProcessing := r.reportDiffOnlyIfApplicable(ctx, bundle, work, expectedAppliedWorkOwnerRef); shouldSkipProcessing {
		return
	}

	// For ClientSideApply and ServerSideApply apply strategies, ownership is a hard requirement.
	// Skip the rest of the processing step if the resource has been created in the member cluster
	// but Fleet is not listed as an owner of the resource in the member cluster yet (i.e., the
	// takeover process does not run). This check is necessary as Fleet now supports the
	// WhenToTakeOver option Never.
	if !canApplyWithOwnership(bundle.inMemberClusterObj, expectedAppliedWorkOwnerRef) {
		klog.V(2).InfoS("Ownership is not established yet; skip the apply op",
			"manifestObj", manifestObjRef, "GVR", *bundle.gvr, "work", workRef)
		bundle.applyErr = fmt.Errorf("no ownership of the object in the member cluster; takeover is needed")
		bundle.applyResTyp = ManifestProcessingApplyResultTypeNotTakenOver
		return
	}

	// Perform a round of drift detection before running the apply op, if the ApplyStrategy
	// dictates that an apply op can only be run when there are no drifts found.
	if shouldSkipProcessing := r.performPreApplyDriftDetectionIfApplicable(ctx, bundle, work, expectedAppliedWorkOwnerRef); shouldSkipProcessing {
		return
	}

	// Perform the apply op.
	appliedObj, err := r.apply(ctx, bundle.gvr, bundle.manifestObj, bundle.inMemberClusterObj, work.Spec.ApplyStrategy, expectedAppliedWorkOwnerRef)
	if err != nil {
		bundle.applyErr = fmt.Errorf("failed to apply the manifest: %w", err)
		bundle.applyResTyp = ManifestProcessingApplyResultTypeFailedToApply
		klog.ErrorS(err, "Failed to apply the manifest",
			"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
			"inMemberClusterObj", klog.KObj(bundle.inMemberClusterObj), "expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		return
	}
	if appliedObj != nil {
		// Update the bundle with the newly applied object, if an apply op has been run.
		bundle.inMemberClusterObj = appliedObj
	}
	klog.V(2).InfoS("Apply process completed",
		"manifestObj", manifestObjRef, "GVR", *bundle.gvr, "work", workRef)

	// Perform another round of drift detection after the apply op, if the ApplyStrategy dictates
	// that drift detection should be done in full comparison mode.
	//
	// Drift detection is always enabled currently in Fleet. At this stage of execution, it is
	// safe for us to assume that all the managed fields have been overwritten by the just
	// completed apply op (or no apply op is necessary); consequently, no further drift
	// detection is necessary if the partial comparison mode is used. However, for the full
	// comparison mode, the apply op might not to able to resolve all the drifts, should there
	// be any change made on the unmanaged fields; and Fleet would need to perform another
	// round of drift detection.
	if shouldSkipProcessing := r.performPostApplyDriftDetectionIfApplicable(ctx, bundle, work, expectedAppliedWorkOwnerRef); shouldSkipProcessing {
		return
	}

	// All done.
	bundle.applyResTyp = ManifestProcessingApplyResultTypeApplied
	klog.V(2).InfoS("Manifest processing completed",
		"manifestObj", manifestObjRef, "GVR", *bundle.gvr, "work", workRef)
}

// findInMemberClusterObjectFor attempts to find the corresponding object in the member cluster
// for a given manifest object.
//
// Note that it is possible that the object has not been created yet in the member cluster.
func (r *Reconciler) findInMemberClusterObjectFor(
	ctx context.Context,
	bundle *manifestProcessingBundle,
	work *fleetv1beta1.Work,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) (shouldSkipProcessing bool) {
	inMemberClusterObj, err := r.spokeDynamicClient.
		Resource(*bundle.gvr).
		Namespace(bundle.manifestObj.GetNamespace()).
		Get(ctx, bundle.manifestObj.GetName(), metav1.GetOptions{})
	switch {
	case err == nil:
		// An object derived from the manifest object has been found in the member cluster.
		klog.V(2).InfoS("Found the corresponding object for the manifest object in the member cluster",
			"manifestObj", klog.KObj(bundle.manifestObj), "GVR", *bundle.gvr, "work", klog.KObj(work))
		bundle.inMemberClusterObj = inMemberClusterObj
		return false
	case errors.IsNotFound(err):
		// The manifest object has never been applied before.
		klog.V(2).InfoS("The manifest object has not been created in the member cluster yet",
			"manifestObj", klog.KObj(bundle.manifestObj), "GVR", *bundle.gvr, "work", klog.KObj(work))
		return false
	default:
		// An unexpected error has occurred.
		wrappedErr := controller.NewAPIServerError(true, err)
		bundle.applyErr = fmt.Errorf("failed to find the corresponding object for the manifest object in the member cluster: %w", wrappedErr)
		bundle.applyResTyp = ManifestProcessingApplyResultTypeFailedToFindObjInMemberCluster
		klog.ErrorS(wrappedErr,
			"Failed to find the corresponding object for the manifest object in the member cluster",
			"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
			"expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		return true
	}
}

// takeOverInMemberClusterObjectIfApplicable attempts to take over an object in the member cluster
// as needed.
func (r *Reconciler) takeOverInMemberClusterObjectIfApplicable(
	ctx context.Context,
	bundle *manifestProcessingBundle,
	work *fleetv1beta1.Work,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) (shouldSkipProcessing bool) {
	if !shouldInitiateTakeOverAttempt(bundle.inMemberClusterObj, work.Spec.ApplyStrategy, expectedAppliedWorkOwnerRef) {
		// Takeover is not necessary; proceed with the processing.
		klog.V(2).InfoS("Takeover is not needed; skip the step")
		return false
	}

	// Take over the object. Note that this steps adds only the owner reference; no other
	// fields are modified (on the object from the member cluster).
	takenOverInMemberClusterObj, configDiffs, err := r.takeOverPreExistingObject(ctx,
		bundle.gvr, bundle.manifestObj, bundle.inMemberClusterObj,
		work.Spec.ApplyStrategy, expectedAppliedWorkOwnerRef)
	switch {
	case err != nil:
		// An unexpected error has occurred.
		bundle.applyErr = fmt.Errorf("failed to take over a pre-existing object: %w", err)
		bundle.applyResTyp = ManifestProcessingApplyResultTypeFailedToTakeOver
		klog.ErrorS(err, "Failed to take over a pre-existing object",
			"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
			"inMemberClusterObj", klog.KObj(bundle.inMemberClusterObj), "expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		return true
	case len(configDiffs) > 0:
		// Takeover cannot be performed as configuration differences are found between the manifest
		// object and the object in the member cluster.
		bundle.diffs = configDiffs
		bundle.applyErr = fmt.Errorf("cannot take over object: configuration differences are found between the manifest object and the corresponding object in the member cluster")
		bundle.applyResTyp = ManifestProcessingApplyResultTypeFailedToTakeOver
		klog.V(2).InfoS("Cannot take over object as configuration differences are found between the manifest object and the corresponding object in the member cluster",
			"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
			"expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		return true
	}

	// Takeover process is completed; update the bundle with the newly refreshed object from the member cluster.
	bundle.inMemberClusterObj = takenOverInMemberClusterObj
	klog.V(2).InfoS("The corresponding object has been taken over",
		"manifestObj", klog.KObj(bundle.manifestObj), "GVR", *bundle.gvr, "work", klog.KObj(work))
	return false
}

// shouldInitiateTakeOverAttempt checks if Fleet should initiate the takeover process for an object.
//
// A takeover process is initiated when:
//   - An object that matches with the given manifest has been created; but
//   - The object is not owned by Fleet (more specifically, the object is not owned by the
//     expected AppliedWork object).
func shouldInitiateTakeOverAttempt(inMemberClusterObj *unstructured.Unstructured,
	applyStrategy *fleetv1beta1.ApplyStrategy,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) bool {
	if inMemberClusterObj == nil {
		// Obviously, if the corresponding live object is not found, no takeover is
		// needed.
		return false
	}

	// Skip the takeover process if the apply strategy forbids so.
	if applyStrategy.WhenToTakeOver == fleetv1beta1.WhenToTakeOverTypeNever {
		return false
	}

	// Check if the live object is owned by Fleet.
	curOwners := inMemberClusterObj.GetOwnerReferences()
	for idx := range curOwners {
		if reflect.DeepEqual(curOwners[idx], *expectedAppliedWorkOwnerRef) {
			// The live object is owned by Fleet; no takeover is needed.
			return false
		}
	}
	return true
}

// reportDiffOnlyIfApplicable checks for configuration differences between the manifest object and
// the object in the member cluster, if the ReportDiff mode is enabled.
//
// Note that if the ReportDiff mode is on, manifest processing is completed as soon as diff
// reportings are done.
func (r *Reconciler) reportDiffOnlyIfApplicable(
	ctx context.Context,
	bundle *manifestProcessingBundle,
	work *fleetv1beta1.Work,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) (shouldSkipProcessing bool) {
	if work.Spec.ApplyStrategy.Type != fleetv1beta1.ApplyStrategyTypeReportDiff {
		// ReportDiff mode is not enabled; proceed with the processing.
		bundle.reportDiffResTyp = ManifestProcessingReportDiffResultTypeNotEnabled
		klog.V(2).InfoS("ReportDiff mode is not enabled; skip the step")
		return false
	}

	bundle.applyResTyp = ManifestProcessingApplyResultTypeNoApplyPerformed

	if bundle.inMemberClusterObj == nil {
		// The object has not created in the member cluster yet.
		//
		// In this case, the diff found would be the full object; for simplicity reasons,
		// Fleet will use a placeholder here rather than including the full JSON representation.
		bundle.reportDiffResTyp = ManifestProcessingReportDiffResultTypeFoundDiff
		bundle.diffs = []fleetv1beta1.PatchDetail{
			{
				// The root path.
				Path: "/",
				// For simplicity reason, Fleet reports a placeholder here rather than
				// including the full JSON representation.
				ValueInHub: "(the whole object)",
			},
		}
		klog.V(2).InfoS("Diff report completed; the object has not been created in the member cluster yet",
			"GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
			"work", klog.KObj(work))
		return true
	}

	// The object has been created in the member cluster; Fleet will calculate the configuration
	// diffs between the manifest object and the object from the member cluster.
	configDiffs, err := r.diffBetweenManifestAndInMemberClusterObjects(ctx,
		bundle.gvr,
		bundle.manifestObj, bundle.inMemberClusterObj,
		work.Spec.ApplyStrategy.ComparisonOption)
	switch {
	case err != nil:
		// Failed to calculate the configuration diffs.
		bundle.reportDiffErr = fmt.Errorf("failed to calculate configuration diffs between the manifest object and the object from the member cluster: %w", err)
		bundle.reportDiffResTyp = ManifestProcessingReportDiffResultTypeFailed
		klog.ErrorS(err,
			"Failed to calculate configuration diffs between the manifest object and the object from the member cluster",
			"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
			"inMemberClusterObj", klog.KObj(bundle.inMemberClusterObj), "expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
	case len(configDiffs) > 0:
		// Configuration diffs are found.
		bundle.diffs = configDiffs
		bundle.reportDiffResTyp = ManifestProcessingReportDiffResultTypeFoundDiff
		klog.V(2).InfoS("Diff report completed; configuration diffs are found",
			"GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
			"work", klog.KObj(work))
	default:
		// No configuration diffs are found.
		bundle.reportDiffResTyp = ManifestProcessingReportDiffResultTypeNoDiffFound
		klog.V(2).InfoS("Diff report completed; no configuration diffs are found",
			"GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
			"work", klog.KObj(work))
	}

	klog.V(2).InfoS("ReportDiff process completed",
		"manifestObj", klog.KObj(bundle.manifestObj), "GVR", *bundle.gvr, "work", klog.KObj(work))
	// If the ReportDiff mode is on, no further processing is performed, regardless of whether
	// diffs are found.
	return true
}

// canApplyWithOwnership checks if Fleet can perform an apply op, knowing that Fleet has
// acquired the ownership of the object, or that the object has not been created yet.
//
// Note that this function does not concern co-ownership; such checks are executed elsewhere.
func canApplyWithOwnership(inMemberClusterObj *unstructured.Unstructured, expectedAppliedWorkOwnerRef *metav1.OwnerReference) bool {
	if inMemberClusterObj == nil {
		// The object has not been created yet; Fleet can apply the object.
		return true
	}

	// Verify if the object is owned by Fleet.
	curOwners := inMemberClusterObj.GetOwnerReferences()
	for idx := range curOwners {
		if reflect.DeepEqual(curOwners[idx], *expectedAppliedWorkOwnerRef) {
			return true
		}
	}
	return false
}

// performPreApplyDriftDetectionIfApplicable checks if pre-apply drift detection is needed and
// runs the drift detection process if applicable.
func (r *Reconciler) performPreApplyDriftDetectionIfApplicable(
	ctx context.Context,
	bundle *manifestProcessingBundle,
	work *fleetv1beta1.Work,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) (shouldSkipProcessing bool) {
	isPreApplyDriftDetectionNeeded, err := shouldPerformPreApplyDriftDetection(bundle.manifestObj, bundle.inMemberClusterObj, work.Spec.ApplyStrategy)
	switch {
	case err != nil:
		// Fleet cannot determine if pre-apply drift detection is needed; this will only
		// happen if the hash calculation process fails, specifically when Fleet cannot
		// marshal the manifest object into its JSON representation. This should never happen,
		// especially considering that the manifest object itself has been
		// successfully decoded at this point of execution.
		//
		// For completion purposes, Fleet will still attempt to catch this and
		// report this as an unexpected error.
		_ = controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to determine if pre-apply drift detection is needed: %w", err))
		bundle.applyErr = fmt.Errorf("failed to determine if pre-apply drift detection is needed: %w", err)
		bundle.applyResTyp = ManifestProcessingApplyResultTypeFailedToRunDriftDetection
		return true
	case !isPreApplyDriftDetectionNeeded:
		// Drift detection is not needed; proceed with the processing.
		klog.V(2).InfoS("Pre-apply drift detection is not needed; skip the step")
		return false
	default:
		// Run the drift detection process.
		drifts, err := r.diffBetweenManifestAndInMemberClusterObjects(ctx,
			bundle.gvr,
			bundle.manifestObj, bundle.inMemberClusterObj,
			work.Spec.ApplyStrategy.ComparisonOption)
		switch {
		case err != nil:
			// An unexpected error has occurred.
			bundle.applyErr = fmt.Errorf("failed to calculate pre-apply drifts between the manifest and the object from the member cluster: %w", err)
			bundle.applyResTyp = ManifestProcessingApplyResultTypeFailedToRunDriftDetection
			klog.ErrorS(err,
				"Failed to calculate pre-apply drifts between the manifest and the object from the member cluster",
				"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
				"inMemberClusterObj", klog.KObj(bundle.inMemberClusterObj), "expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
			return true
		case len(drifts) > 0:
			// Drifts are found in the pre-apply drift detection process.
			bundle.drifts = drifts
			bundle.applyErr = fmt.Errorf("cannot apply manifest: drifts are found between the manifest and the object from the member cluster")
			bundle.applyResTyp = ManifestProcessingApplyResultTypeFoundDrifts
			klog.V(2).InfoS("Cannot apply manifest: drifts are found between the manifest and the object from the member cluster",
				"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
				"inMemberClusterObj", klog.KObj(bundle.inMemberClusterObj), "expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
			return true
		default:
			// No drifts are found in the pre-apply drift detection process; carry on with the apply op.
			klog.V(2).InfoS("Pre-apply drift detection completed; no drifts are found",
				"manifestObj", klog.KObj(bundle.manifestObj), "GVR", *bundle.gvr, "work", klog.KObj(work))
			return false
		}
	}
}

// shouldPerformPreApplyDriftDetection checks if pre-apply drift detection should be performed.
func shouldPerformPreApplyDriftDetection(manifestObj, inMemberClusterObj *unstructured.Unstructured, applyStrategy *fleetv1beta1.ApplyStrategy) (bool, error) {
	// Drift detection is performed before the apply op if (and only if):
	// * Fleet reports that the manifest has been applied before (i.e., inMemberClusterObj exists); and
	// * The apply strategy dictates that an apply op should only run if there is no
	//   detected drift; and
	// * The hash of the manifest object is consistent with the last applied manifest object hash
	//   annotation on the corresponding resource in the member cluster (i.e., the same manifest
	//   object has been applied before).
	if applyStrategy.WhenToApply != fleetv1beta1.WhenToApplyTypeIfNotDrifted || inMemberClusterObj == nil {
		// A shortcut to save some overhead.
		return false, nil
	}

	cleanedManifestObj := discardFieldsIrrelevantInComparisonFrom(manifestObj)
	manifestObjHash, err := resource.HashOf(cleanedManifestObj.Object)
	if err != nil {
		return false, err
	}

	inMemberClusterObjLastAppliedManifestObjHash := inMemberClusterObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation]
	return manifestObjHash == inMemberClusterObjLastAppliedManifestObjHash, nil
}

// performPostApplyDriftDetectionIfApplicable checks if post-apply drift detection is needed and
// runs the drift detection process if applicable.
func (r *Reconciler) performPostApplyDriftDetectionIfApplicable(
	ctx context.Context,
	bundle *manifestProcessingBundle,
	work *fleetv1beta1.Work,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) (shouldSkipProcessing bool) {
	if !shouldPerformPostApplyDriftDetection(work.Spec.ApplyStrategy) {
		// Post-apply drift detection is not needed; proceed with the processing.
		klog.V(2).InfoS("Post-apply drift detection is not needed; skip the step",
			"manifestObj", klog.KObj(bundle.manifestObj), "GVR", *bundle.gvr, "work", klog.KObj(work))
		return false
	}

	drifts, err := r.diffBetweenManifestAndInMemberClusterObjects(ctx,
		bundle.gvr,
		bundle.manifestObj, bundle.inMemberClusterObj,
		work.Spec.ApplyStrategy.ComparisonOption)
	switch {
	case err != nil:
		// An unexpected error has occurred.
		bundle.applyErr = fmt.Errorf("failed to calculate post-apply drifts between the manifest object and the object from the member cluster: %w", err)
		// This case counts as a partial error; the apply op has been completed, but Fleet
		// cannot determine if there are any drifts.
		bundle.applyResTyp = ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection
		klog.ErrorS(err,
			"Failed to calculate post-apply drifts between the manifest object and the object from the member cluster",
			"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
			"inMemberClusterObj", klog.KObj(bundle.inMemberClusterObj), "expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		return true
	case len(drifts) > 0:
		// Drifts are found in the post-apply drift detection process.
		bundle.drifts = drifts
		klog.V(2).InfoS("Post-apply drift detection completed; drifts are found",
			"manifestObj", klog.KObj(bundle.manifestObj), "GVR", *bundle.gvr, "work", klog.KObj(work))
		// The presence of such drifts are not considered as an error.
		return false
	default:
		// No drifts are found in the post-apply drift detection process.
		klog.V(2).InfoS("Post-apply drift detection completed; no drifts are found",
			"manifestObj", klog.KObj(bundle.manifestObj), "GVR", *bundle.gvr, "work", klog.KObj(work))
		return false
	}
}

// shouldPerformPostApplyDriftDetection checks if post-apply drift detection should be performed.
func shouldPerformPostApplyDriftDetection(applyStrategy *fleetv1beta1.ApplyStrategy) bool {
	// Post-apply drift detection is performed if (and only if):
	// * The apply strategy dictates that drift detection should run in full comparison mode.
	return applyStrategy.ComparisonOption == fleetv1beta1.ComparisonOptionTypeFullComparison
}
