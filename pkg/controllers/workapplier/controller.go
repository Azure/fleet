/*
Copyright 2021 The Kubernetes Authors.

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

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/atomic"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrloption "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/defaulter"
	"go.goms.io/fleet/pkg/utils/parallelizer"
	"go.goms.io/fleet/pkg/utils/resource"
)

const (
	patchDetailPerObjLimit = 100

	availabilityCheckRequeueAfter = time.Second * 5
	driftCheckRequeueAfter        = time.Minute * 3
)

const (
	workFieldManagerName = "work-api-agent"
)

// Reconciler reconciles a Work object.
type Reconciler struct {
	hubClient            client.Client
	workNameSpace        string
	spokeDynamicClient   dynamic.Interface
	spokeClient          client.Client
	restMapper           meta.RESTMapper
	recorder             record.EventRecorder
	concurrentReconciles int
	joined               *atomic.Bool
	parallelizer         *parallelizer.Parallerlizer
}

func NewReconciler(
	hubClient client.Client, workNameSpace string,
	spokeDynamicClient dynamic.Interface, spokeClient client.Client, restMapper meta.RESTMapper,
	recorder record.EventRecorder,
	concurrentReconciles int,
	workerCount int,
) *Reconciler {
	return &Reconciler{
		hubClient:            hubClient,
		spokeDynamicClient:   spokeDynamicClient,
		spokeClient:          spokeClient,
		restMapper:           restMapper,
		recorder:             recorder,
		concurrentReconciles: concurrentReconciles,
		parallelizer:         parallelizer.NewParallelizer(workerCount),
		workNameSpace:        workNameSpace,
		joined:               atomic.NewBool(false),
	}
}

const (
	allManifestsAppliedMessage       = "All the specified manifests have been applied"
	allAppliedObjectAvailableMessage = "All of the applied manifests are available"

	notAllManifestsAppliedReason         = "FailedToApplyAllManifests"
	notAllManifestsAppliedMessage        = "Failed to apply all the specified manifests (%d of %d manifests are applied)"
	notAllAppliedObjectsAvailableReason  = "NotAllAppliedObjectAreAvailable"
	notAllAppliedObjectsAvailableMessage = "Not all of the applied manifests are available (%d of %d manifests are available)"
)

type manifestProcessingAppliedResultType string

const (
	// The result types and descriptions for processing failures.
	ManifestProcessingApplyResultTypeDecodingErred                  manifestProcessingAppliedResultType = "DecodingErred"
	ManifestProcessingApplyResultTypeDuplicated                     manifestProcessingAppliedResultType = "Duplicated"
	ManifestProcessingApplyResultTypeFailedToFindObjInMemberCluster manifestProcessingAppliedResultType = "FailedToFindObjInMemberCluster"
	ManifestProcessingApplyResultTypeFailedToTakeOver               manifestProcessingAppliedResultType = "FailedToTakeOver"
	ManifestProcessingApplyResultTypeFailedToReportDiff             manifestProcessingAppliedResultType = "FailedToReportDiff"
	ManifestProcessingApplyResultTypeFailedToRunDriftDetection      manifestProcessingAppliedResultType = "FailedToRunDriftDetection"
	ManifestProcessingApplyResultTypeFoundDrifts                    manifestProcessingAppliedResultType = "FoundDrifts"
	ManifestProcessingApplyResultTypeFailedToApply                  manifestProcessingAppliedResultType = "FailedToApply"

	// The result type and description for partially successfully processing attempts.
	ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection manifestProcessingAppliedResultType = "AppliedWithFailedDriftDetection"

	ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetectionDescription = "Manifest has been applied successfully, but drift detection has failed"

	// The result type and description for successful processing attempts.
	ManifestProcessingApplyResultTypeApplied manifestProcessingAppliedResultType = "Applied"

	ManifestProcessingApplyResultTypeAppliedDescription = "Manifest has been applied successfully"
)

type ManifestProcessingAvailabilityResultType string

const (
	// The result type for availability check being skipped.
	ManifestProcessingAvailabilityResultTypeSkipped ManifestProcessingAvailabilityResultType = "Skipped"

	// The result type for availability check failures.
	ManifestProcessingAvailabilityResultTypeFailed ManifestProcessingAvailabilityResultType = "Failed"

	ManifestProcessingAvailabilityResultTypeFailedDescription = "Failed to track the availability of the applied manifest (error = %s)"

	// The result types for completed availability checks.
	ManifestProcessingAvailabilityResultTypeAvailable       ManifestProcessingAvailabilityResultType = "Available"
	ManifestProcessingAvailabilityResultTypeNotYetAvailable ManifestProcessingAvailabilityResultType = "NotYetAvailable"
	ManifestProcessingAvailabilityResultTypeNotTrackable    ManifestProcessingAvailabilityResultType = "NotTrackable"

	ManifestProcessingAvailabilityResultTypeAvailableDescription       = "Manifest is available"
	ManifestProcessingAvailabilityResultTypeNotYetAvailableDescription = "Manifest is not yet available; Fleet will check again later"
	ManifestProcessingAvailabilityResultTypeNotTrackableDescription    = "Manifest's availability is not trackable; Fleet assumes that the applied manifest is available"
)

type manifestProcessingBundle struct {
	manifest              *fleetv1beta1.Manifest
	id                    *fleetv1beta1.WorkResourceIdentifier
	manifestObj           *unstructured.Unstructured
	inMemberClusterObj    *unstructured.Unstructured
	manifestCondIdx       *int
	gvr                   *schema.GroupVersionResource
	applyResTyp           manifestProcessingAppliedResultType
	availabilityResTyp    ManifestProcessingAvailabilityResultType
	applyErr              error
	availabilityErr       error
	drifts                []fleetv1beta1.PatchDetail
	firstDriftedTimestamp *metav1.Time
	diffs                 []fleetv1beta1.PatchDetail
	firstDiffedTimestamp  *metav1.Time
}

// Reconcile implement the control loop logic for Work object.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.joined.Load() {
		klog.V(2).InfoS("Work controller is not started yet, requeue the request", "work", req.NamespacedName)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	startTime := time.Now()
	klog.V(2).InfoS("ApplyWork reconciliation starts", "work", req.NamespacedName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("ApplyWork reconciliation ends", "work", req.NamespacedName, "latency", latency)
	}()

	// Retrieve the Work object.
	work := &fleetv1beta1.Work{}
	err := r.hubClient.Get(ctx, req.NamespacedName, work)
	switch {
	case apierrors.IsNotFound(err):
		klog.V(2).InfoS("The work resource is deleted", "work", req.NamespacedName)
		return ctrl.Result{}, nil
	case err != nil:
		klog.ErrorS(err, "Failed to retrieve the work", "work", req.NamespacedName)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}

	workRef := klog.KObj(work)

	// Garbage collect the AppliedWork object if the Work object has been deleted.
	if !work.DeletionTimestamp.IsZero() {
		klog.V(2).InfoS("Resource is in the process of being deleted", work.Kind, workRef)
		return r.garbageCollectAppliedWork(ctx, work)
	}

	// set default value so that the following call can skip checking nil
	// TODO, could be removed once we have the defaulting webhook with fail policy.
	// Make sure these conditions are met before moving
	// * the defaulting webhook failure policy is configured as "fail".
	// * user cannot update/delete the webhook.
	defaulter.SetDefaultsWork(work)

	// ensure that the appliedWork and the finalizer exist
	appliedWork, err := r.ensureAppliedWork(ctx, work)
	if err != nil {
		return ctrl.Result{}, err
	}
	expectedAppliedWorkOwnerRef := &metav1.OwnerReference{
		APIVersion:         fleetv1beta1.GroupVersion.String(),
		Kind:               fleetv1beta1.AppliedWorkKind,
		Name:               appliedWork.GetName(),
		UID:                appliedWork.GetUID(),
		BlockOwnerDeletion: ptr.To(false),
	}

	// Note (chenyu1): as of Nov 8, 2024, Fleet has a bug which would assign an identifier with empty
	// name to an object with generated name; since in earlier versions the identifier struct
	// itself does not bookkeep generate name information, this would effectively lead to the loss
	// of track of such objects. When migrating to this version, any status information impacted
	// by the bug would have an index with invalid identifier strings; Fleet is aware of this
	// situation and it is still safe to handle manifests with such an erred index as in later
	// steps the code will attempt to rebuild the association between the manifest object and
	// the object from the member cluster.

	// Prepare the bundles.
	bundles := prepareManifestProcessingBundles(work)

	// Pre-process the manifests to apply.
	//
	// In this step, Fleet will:
	// a) decode the manifests; and
	// b) write ahead the manifest processing attempts; and
	// c) remove any applied manifests left over from previous runs.
	if err := r.preProcessManifests(ctx, bundles, work, expectedAppliedWorkOwnerRef); err != nil {
		klog.ErrorS(err, "Failed to pre-process the manifests", "work", workRef)
		return ctrl.Result{}, err
	}

	// Process the manifests.
	r.processManifests(ctx, bundles, work, expectedAppliedWorkOwnerRef)
	// Track the availability information.
	r.trackInMemberClusterObjAvailability(ctx, bundles, &workRef)

	trackWorkApplyLatencyMetric(work)

	// Refresh the status of the Work object.
	if err := r.refreshWorkStatus(ctx, work, bundles); err != nil {
		return ctrl.Result{}, err
	}

	// Refresh the status of the AppliedWork object.
	if err := r.refreshAppliedWorkStatus(ctx, appliedWork, bundles); err != nil {
		return ctrl.Result{}, err
	}

	// If the Work object is not yet available, reconcile again in 5 seconds.
	if !isWorkObjectAvailable(work) {
		return ctrl.Result{RequeueAfter: availabilityCheckRequeueAfter}, nil
	}
	// Otherwise, reconcile again in 3 minutes for drift detection purposes.
	return ctrl.Result{RequeueAfter: driftCheckRequeueAfter}, nil
}

// preProcessManifests pre-processes manifests for the later ops.
func (r *Reconciler) preProcessManifests(
	ctx context.Context,
	bundles []*manifestProcessingBundle,
	work *fleetv1beta1.Work,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) error {
	// Decode the manifests.
	// Run the decoding in parallel to boost performance.
	//
	// This is concurrency safe as the bundles slice has been pre-allocated.

	// Prepare a child context.
	// Cancel the child context anyway to avoid leaks.
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	doWork := func(pieces int) {
		bundle := bundles[pieces]
		if bundle.applyErr != nil {
			// Skip a manifest if it cannot be processed.
			return
		}

		gvr, manifestObj, err := r.decodeManifest(bundle.manifest)
		// Build the identifier. Note that this would return an identifier even if the decoding
		// fails.
		bundle.id = buildWorkResourceIdentifier(pieces, gvr, manifestObj)
		if err != nil {
			klog.ErrorS(err, "Failed to decode the manifest", "ordinal", pieces, "work", klog.KObj(work))
			bundle.applyErr = fmt.Errorf("failed to decode manifest: %w", err)
			bundle.applyResTyp = ManifestProcessingApplyResultTypeDecodingErred
			return
		}

		bundle.manifestObj = manifestObj
		bundle.gvr = gvr
	}
	r.parallelizer.ParallelizeUntil(childCtx, len(bundles), doWork, "decodingManifests")

	// Write ahead the manifest processing attempts. In the process Fleet will also perform a
	// cleanup to remove any left-over manifests that are applied from previous runs.
	//
	// This is necessary primarily for the reason that there exists a corner case where the agent
	// could crash right after manifests are applied but before the status is properly updated,
	// and upon the agent's restart, the list of manifests has changed (some manifests have been
	// removed). This would lead to a situation where Fleet would lose track of the removed
	// manifests.
	//
	// To address this corner case, Fleet writes ahead the manifest processing attempts to Work
	// object status, and through cross-reference, Fleet will be able to determine if there exists
	// left-over manifests and perform clean-up as appropriate.
	//
	// To avoid conflicts (or the hassle of preparing individual patches), the status update is
	// done in batch.
	//
	// Note that during the write-ahead process, Fleet will also perform a duplication check, which
	// guarantees that
	// * for regular objects, no object with the same GVK + namespace/name combo would be processed
	//   twice;
	// * for objects with generated names, no object with the same GVK + namespace/generate name
	//   combo would be processed twice.
	//
	// This check is done on the Work object scope, and is primarily added to address the case
	// where duplicate objects might appear in a Fleet resource envelope and lead to unexpected
	// behaviors. Duplication is a non-issue without Fleet resource envelopes, as the Fleet hub
	// cluster Kubernetes API server already promises uniqueness when resources are first created.
	return r.writeAheadManifestProcessingAttempts(ctx, bundles, work, expectedAppliedWorkOwnerRef)
}

// writeAheadManifestProcessingAttempts helps write ahead manifest processing attempts so that
// Fleet can always track applied manifests, even upon untimely crashes. This method will
// also check for any leftover apply attempts from previous runs and clean them up (if the
// correspond object has been applied).
func (r *Reconciler) writeAheadManifestProcessingAttempts(
	ctx context.Context,
	bundles []*manifestProcessingBundle,
	work *fleetv1beta1.Work,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) error {
	// As a shortcut, if there's no spec change in the Work object and the status indicates that
	// a previous apply attempt has been recorded (**successful or not**), Fleet will skip the write-ahead
	// op.
	workAppliedCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
	if workAppliedCond != nil && workAppliedCond.ObservedGeneration == work.Generation {
		klog.V(2).InfoS("Fleet has attempted to apply the current set of manifests before and recorded the results; will skip the write-ahead process", "work", klog.KObj(work))
		return nil
	}

	// Prepare the status update (the new manifest conditions) for the write-ahead process.
	//
	// Note that even though we pre-allocate the slice, the length is set to 0. This is to
	// accommodate the case where there might manifests that have failed pre-processing;
	// such manifests will not be included in this round's status update.
	manifestCondsForWA := make([]fleetv1beta1.ManifestCondition, 0, len(bundles))

	// Prepare an query index of existing manifest conditions on the Work object for quicker
	// lookups.
	existingManifestCondQIdx := prepareExistingManifestCondQIdx(work.Status.ManifestConditions)

	// For each manifest, verify if it has been tracked in the newly prepared manifest conditions.
	// This helps signal duplicated resources in the Work object.
	checked := make(map[string]bool, len(bundles))
	for idx := range bundles {
		bundle := bundles[idx]
		if bundle.applyErr != nil {
			// Skip a manifest if it cannot be pre-processed, i.e., it can only be identified by
			// its ordinal.
			//
			// Such manifests would still be reported in the status (see the later parts of the
			// reconciliation loop), it is just that they are not relevant in the write-ahead
			// process.
			continue
		}

		// Register the manifest in the checked map; if another manifest with the same identifier
		// has been checked before, Fleet would mark the current manifest as a duplicate and skip
		// it.
		//
		// A side note: Golang does support using structs as map keys; preparing the string
		// representations of structs as keys can help performance, though not by much. The reason
		// why string representations are used here is not for performance, though; instead, it
		// is to address the issue that for this comparison, ordinals should be ignored.
		wriStr, err := formatWRIString(bundle.id)
		if err != nil {
			// Normally this branch will never run as all manifests that cannot be decoded has been
			// skipped in the check above. Here Fleet simply skips the manifest.
			klog.ErrorS(err, "Failed to format the work resource identifier string", "ordinal", idx, "work", klog.KObj(work))
			continue
		}
		if _, found := checked[wriStr]; found {
			klog.V(2).InfoS("A duplicate manifest has been found", "ordinal", idx, "work", klog.KObj(work), "workResourceId", wriStr)
			bundle.applyErr = fmt.Errorf("a duplicate manifest has been found")
			bundle.applyResTyp = ManifestProcessingApplyResultTypeDuplicated
			continue
		}
		checked[wriStr] = true

		// Prepare the manifest conditions for the write-ahead process.
		manifestCondForWA := prepareManifestCondForWA(wriStr, bundle.id, work.Generation, existingManifestCondQIdx, work.Status.ManifestConditions)
		manifestCondsForWA = append(manifestCondsForWA, manifestCondForWA)

		// Keep track of the last drift/diff observed timestamp.
		if manifestCondForWA.DriftDetails != nil && !manifestCondForWA.DriftDetails.FirstDriftedObservedTime.IsZero() {
			bundle.firstDriftedTimestamp = manifestCondForWA.DriftDetails.FirstDriftedObservedTime.DeepCopy()
		}
		if manifestCondForWA.DiffDetails != nil && !manifestCondForWA.DiffDetails.FirstDiffedObservedTime.IsZero() {
			bundle.firstDiffedTimestamp = manifestCondForWA.DiffDetails.FirstDiffedObservedTime.DeepCopy()
		}
	}

	// Identify any manifests from previous runs that might have been applied and are now left
	// over in the member cluster.
	leftOverManifests := findLeftOverManifests(manifestCondsForWA, existingManifestCondQIdx, work.Status.ManifestConditions)
	if err := r.removeLeftOverManifests(ctx, leftOverManifests, expectedAppliedWorkOwnerRef); err != nil {
		klog.Errorf("Failed to remove left-over manifests (work=%+v, leftOverManifestCount=%d, removalFailureCount=%d)",
			klog.KObj(work), len(leftOverManifests), len(err.Errors()))
		return fmt.Errorf("failed to remove left-over manifests: %w", err)
	}

	// Update the status.
	//
	// Note that the Work object might have been refreshed by controllers on the hub cluster
	// before this step runs; in this case the current reconciliation loop must be abandoned.
	if work.Status.Conditions == nil {
		// As a sanity check, set an empty set of conditions. Currently the API definition does
		// not allow nil conditions.
		work.Status.Conditions = []metav1.Condition{}
	}
	work.Status.ManifestConditions = manifestCondsForWA
	if err := r.hubClient.Status().Update(ctx, work); err != nil {
		return controller.NewAPIServerError(false, fmt.Errorf("failed to write ahead manifest processing attempts: %w", err))
	}
	return nil
}

func (r *Reconciler) processManifests(
	ctx context.Context,
	bundles []*manifestProcessingBundle,
	work *fleetv1beta1.Work,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) {
	// Process all the manifests in parallel.
	//
	// This is concurrency safe as the bundles slice has been pre-allocated.

	// Prepare a child context.
	// Cancel the child context anyway to avoid leaks.
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	doWork := func(pieces int) {
		bundle := bundles[pieces]
		if bundle.applyErr != nil {
			// Skip a manifest if it has failed pre-processing.
			return
		}

		r.processOneManifest(childCtx, bundle, work, expectedAppliedWorkOwnerRef)
	}
	r.parallelizer.ParallelizeUntil(childCtx, len(bundles), doWork, "processingManifests")
}

// processOneManifest processes a manifest (in the JSON format) embedded in the Work object.
func (r *Reconciler) processOneManifest(
	ctx context.Context,
	bundle *manifestProcessingBundle,
	work *fleetv1beta1.Work,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) {
	// Firstly, attempt to find if an object has been created in the member cluster based on the manifest object.
	inMemberClusterObj, err := r.findInMemberClusterObjectFor(ctx,
		bundle.gvr, bundle.manifestObj, bundle.manifestCondIdx,
		work, expectedAppliedWorkOwnerRef)
	if err != nil {
		bundle.applyErr = fmt.Errorf("failed to find the corresponding object for the manifest object in the member cluster: %w", err)
		bundle.applyResTyp = ManifestProcessingApplyResultTypeFailedToFindObjInMemberCluster
		klog.ErrorS(err,
			"Failed to find the corresponding object for the manifest object in the member cluster",
			"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
			"expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		return
	}
	bundle.inMemberClusterObj = inMemberClusterObj

	// Verify if takeover is needed.
	if shouldInitiateTakeOverAttempt(bundle.manifestObj, bundle.inMemberClusterObj, expectedAppliedWorkOwnerRef) {
		// Take over the object. Note that this steps adds only the owner reference; no other
		// fields are modified (on the object from the member cluster).
		takenOverInMemberClusterObj, configDiffs, err := r.takeOverPreExistingObject(ctx,
			bundle.gvr, bundle.manifestObj, bundle.inMemberClusterObj,
			work.Spec.ApplyStrategy, expectedAppliedWorkOwnerRef)
		switch {
		case err != nil:
			bundle.applyErr = fmt.Errorf("failed to take over a pre-existing object: %w", err)
			bundle.applyResTyp = ManifestProcessingApplyResultTypeFailedToTakeOver
			klog.ErrorS(err, "Failed to take over a pre-existing object",
				"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
				"inMemberClusterObj", klog.KObj(bundle.inMemberClusterObj), "expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
			return
		case len(configDiffs) > 0:
			bundle.diffs = configDiffs
			bundle.applyErr = fmt.Errorf("cannot take over object: configuration differences are found between the manifest object and the corresponding object in the member cluster")
			bundle.applyResTyp = ManifestProcessingApplyResultTypeFailedToTakeOver
			klog.V(2).InfoS("Cannot take over object as configuration differences are found between the manifest object and the corresponding object in the member cluster",
				"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
				"inMemberClusterObj", klog.KObj(bundle.inMemberClusterObj), "expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
			return
		}

		// Update the bundle with the newly refreshed object from the member cluster.
		bundle.inMemberClusterObj = takenOverInMemberClusterObj
	}

	applyStrategy := work.Spec.ApplyStrategy
	// If the ApplyStrategy has been set to the use the ReportDiff apply mode, Fleet would
	// check for the configuration difference now; no drift detection nor apply op will be
	// executed.
	if applyStrategy.Type == fleetv1beta1.ApplyStrategyTypeReportDiff {
		configDiffs, err := r.diffBetweenManifestAndInMemberClusterObjects(ctx,
			bundle.gvr,
			bundle.manifestObj, bundle.inMemberClusterObj,
			applyStrategy.ComparisonOption)
		if err != nil {
			bundle.applyErr = fmt.Errorf("failed to calculate configuration diffs between the manifest object and the object from the member cluster: %w", err)
			bundle.applyResTyp = ManifestProcessingApplyResultTypeFailedToReportDiff
			klog.ErrorS(err,
				"Failed to calculate configuration diffs between the manifest object and the object from the member cluster",
				"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
				"inMemberClusterObj", klog.KObj(bundle.inMemberClusterObj), "expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
			return
		}
		bundle.diffs = configDiffs
		return
	}

	// Perform a round of drift detection before running the apply op, if the ApplyStrategy
	// dictates that an apply op can only be run when there are no drifts found.
	isPreApplyDriftDetectionNeeded, err := shouldPerformPreApplyDriftDetection(bundle.manifestObj, bundle.inMemberClusterObj, work.Spec.ApplyStrategy)
	switch {
	case err != nil:
		bundle.applyErr = fmt.Errorf("failed to determine if pre-apply drift detection is needed: %w", err)
		bundle.applyResTyp = ManifestProcessingApplyResultTypeFailedToRunDriftDetection
		klog.ErrorS(err, "Failed to determine if pre-apply drift detection is needed",
			"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
			"inMemberClusterObj", klog.KObj(bundle.inMemberClusterObj), "expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		return
	case isPreApplyDriftDetectionNeeded:
		drifts, err := r.diffBetweenManifestAndInMemberClusterObjects(ctx,
			bundle.gvr,
			bundle.manifestObj, bundle.inMemberClusterObj,
			applyStrategy.ComparisonOption)
		switch {
		case err != nil:
			bundle.applyErr = fmt.Errorf("failed to calculate pre-apply drifts between the manifest and the object from the member cluster: %w", err)
			bundle.applyResTyp = ManifestProcessingApplyResultTypeFailedToRunDriftDetection
			klog.ErrorS(err,
				"Failed to calculate pre-apply drifts between the manifest and the object from the member cluster",
				"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
				"inMemberClusterObj", klog.KObj(bundle.inMemberClusterObj), "expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
			return
		case len(drifts) > 0:
			bundle.drifts = drifts
			bundle.applyErr = fmt.Errorf("cannot apply manifest: drifts are found between the manifest and the object from the member cluster")
			bundle.applyResTyp = ManifestProcessingApplyResultTypeFoundDrifts
			klog.V(2).InfoS("Cannot apply manifest: drifts are found between the manifest and the object from the member cluster",
				"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
				"inMemberClusterObj", klog.KObj(bundle.inMemberClusterObj), "expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
			return
		}

		// No drifts are found; carry on with the apply op.
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
	bundle.inMemberClusterObj = appliedObj
	// For objects with generated names, Fleet would need to update the bundle identifier to include
	// the actual name of the applied object.
	if bundle.id.GenerateName != "" && bundle.id.Name == "" {
		bundle.id.Name = appliedObj.GetName()
	}

	// Perform another round of drift detection after the apply op, if the ApplyStrategy dictates
	// that drift detection should be done in full comparison mode.
	//
	// Drift detection is always enabled currently in Fleet. At this stage of execution, it is
	// safe for us to assume that all the managed fields have been overwritten by the just
	// completed apply op; consequently, no further drift detection is necessary if the partial
	// comparison mode is used. However, for the full comparison mode, the apply op might not to
	// able to resolve all the drifts, should there be any change made on the unmanaged fields;
	// and Fleet would need to perform another round of drift detection.
	if shouldPerformPostApplyDriftDetection(work.Spec.ApplyStrategy) {
		drifts, err := r.diffBetweenManifestAndInMemberClusterObjects(ctx,
			bundle.gvr,
			bundle.manifestObj, appliedObj,
			applyStrategy.ComparisonOption)
		if err != nil {
			bundle.applyErr = fmt.Errorf("failed to calculate post-apply drifts between the manifest object and the object from the member cluster: %w", err)
			// This case counts as a partial error; the apply op has been completed, but Fleet
			// cannot determine if there are any drifts.
			bundle.applyResTyp = ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection
			klog.ErrorS(err,
				"Failed to calculate post-apply drifts between the manifest object and the object from the member cluster",
				"work", klog.KObj(work), "GVR", *bundle.gvr, "manifestObj", klog.KObj(bundle.manifestObj),
				"inMemberClusterObj", klog.KObj(bundle.inMemberClusterObj), "expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
			return
		}
		bundle.drifts = drifts
		// The presence of such drifts are not considered as an error.
	}

	// All done.
	bundle.applyResTyp = ManifestProcessingApplyResultTypeApplied
}

// findInMemberClusterObjectFor attempts to find the corresponding object in the member cluster
// for a given manifest object.
//
// Note that it is possible that the object has not been created yet in the member cluster;
// this method will not return an error in such a case.
func (r *Reconciler) findInMemberClusterObjectFor(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObj *unstructured.Unstructured,
	manifestCondIdx *int,
	work *fleetv1beta1.Work,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) (*unstructured.Unstructured, error) {
	// If the manifest object is an object with generated name, Fleet would need to handle the
	// object searching process in the member cluster differently as the object's name might
	// not have been tracked yet.
	if manifestObj.GetGenerateName() != "" {
		return r.findInMemberClusterObjectForObjWithGenerateName(ctx, gvr, manifestObj, manifestCondIdx, work, expectedAppliedWorkOwnerRef)
	}

	// For regular objects, i.e., objects with no generate names, Fleet will look up the object
	// in the member cluster directly.
	inMemberClusterObj, err := r.spokeDynamicClient.
		Resource(*gvr).
		Namespace(manifestObj.GetNamespace()).
		Get(ctx, manifestObj.GetName(), metav1.GetOptions{})
	if err == nil {
		return inMemberClusterObj, nil
	}
	return nil, client.IgnoreNotFound(err)
}

// findInMemberClusterObjectForObjWithGenerateName attempts to find the object in the member cluster
// for a given manifest object with a generated name.
func (r *Reconciler) findInMemberClusterObjectForObjWithGenerateName(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObj *unstructured.Unstructured,
	manifestCondIdx *int,
	work *fleetv1beta1.Work,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) (*unstructured.Unstructured, error) {
	// As a shortcut, if the Work object already keeps a manifest condition for the object, and
	// the manifest condition features a valid name, Fleet would use the association directly.
	if manifestCondIdx != nil {
		manifestCondIdentifier := work.Status.ManifestConditions[*manifestCondIdx].Identifier
		if manifestCondIdentifier.Name != "" {
			inMemberClusterObj, err := r.spokeDynamicClient.
				Resource(*gvr).
				Namespace(manifestObj.GetNamespace()).
				Get(ctx, manifestCondIdentifier.Name, metav1.GetOptions{})
			switch {
			case err == nil && isInMemberClusterObjectDerivedFromManifestObjWithGenerateName(inMemberClusterObj, manifestObj.GetGenerateName(), expectedAppliedWorkOwnerRef):
				// The object has been found; Fleet also performs a sanity check to ensure
				// that object found in the member cluster can be associated with the manifest object.
				return inMemberClusterObj, nil
			case err == nil:
				// The object has been found in the member cluster but the sanity check fails.
				// Normally this would not occur; Fleet would assume that the record in the status
				// is stale/inconsistent and create a new object in the member cluster.
				klog.V(2).InfoS("A corresponding object has been found in the member cluster given the name in the status records but it is not derived from the manifest object",
					"manifestObj", klog.KObj(manifestObj), "inMemberClusterObj", klog.KObj(inMemberClusterObj), "work", klog.KObj(work))
				return nil, nil
			case apierrors.IsNotFound(err):
				// The object cannot be found in the member cluster; this normally would not
				// occur either, as a name has been written in the status, unless the user has
				// deleted the object manually. Fleet would assume that the record in the status
				// is stale/inconsistent and create a new object in the member cluster.
				klog.V(2).InfoS("Failed to find the corresponding object in the member cluster despite that a name has been recorded in the status",
					"manifestObj", klog.KObj(manifestObj), "work", klog.KObj(work))
				return nil, nil
			default:
				// An unexpected error has occurred.
				return nil, err
			}
		}
	}

	// If the Work object does not keep a manifest condition for the object, Fleet would need
	// to do the lookup the hardway, listing all objects and check if any fits the description
	// in the manifest object.
	//
	// This is a relatively expensive op, but it should only happen once for each object with
	// generated name.
	inMemberClusterObjCandidates, err := r.spokeDynamicClient.
		Resource(*gvr).
		Namespace(manifestObj.GetNamespace()).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for idx := range inMemberClusterObjCandidates.Items {
		inMemberClusterObjCandidate := inMemberClusterObjCandidates.Items[idx]
		if isInMemberClusterObjectDerivedFromManifestObjWithGenerateName(&inMemberClusterObjCandidate, manifestObj.GetGenerateName(), expectedAppliedWorkOwnerRef) {
			// A matching object has been found in the member cluster. Normally there should be
			// at most one matching object, so here in the code flow Fleet takes the first one that matched and
			// skips the rest. Should there be multiple matches, to avoid unexpected interruptions,
			// no cleanup should be performed.
			klog.V(2).InfoS("Found an object derived from the manifest object",
				"manifestObj", klog.KObj(manifestObj), "inMemberClusterObj", klog.KObj(&inMemberClusterObjCandidate), "work", klog.KObj(work))
			return &inMemberClusterObjCandidate, nil
		}
	}
	// No matching object has been found in the member cluster; Fleet will need to apply the
	// manifest to create one.
	return nil, nil
}

// garbageCollectAppliedWork deletes the appliedWork and all the manifests associated with it from the cluster.
func (r *Reconciler) garbageCollectAppliedWork(ctx context.Context, work *fleetv1beta1.Work) (ctrl.Result, error) {
	deletePolicy := metav1.DeletePropagationBackground
	if !controllerutil.ContainsFinalizer(work, fleetv1beta1.WorkFinalizer) {
		return ctrl.Result{}, nil
	}
	// delete the appliedWork which will remove all the manifests associated with it
	// TODO: allow orphaned manifest
	appliedWork := fleetv1beta1.AppliedWork{
		ObjectMeta: metav1.ObjectMeta{Name: work.Name},
	}
	err := r.spokeClient.Delete(ctx, &appliedWork, &client.DeleteOptions{PropagationPolicy: &deletePolicy})
	switch {
	case apierrors.IsNotFound(err):
		klog.V(2).InfoS("The appliedWork is already deleted", "appliedWork", work.Name)
	case err != nil:
		klog.ErrorS(err, "Failed to delete the appliedWork", "appliedWork", work.Name)
		return ctrl.Result{}, err
	default:
		klog.InfoS("Successfully deleted the appliedWork", "appliedWork", work.Name)
	}
	controllerutil.RemoveFinalizer(work, fleetv1beta1.WorkFinalizer)
	return ctrl.Result{}, r.hubClient.Update(ctx, work, &client.UpdateOptions{})
}

// ensureAppliedWork makes sure that an associated appliedWork and a finalizer on the work resource exists on the cluster.
func (r *Reconciler) ensureAppliedWork(ctx context.Context, work *fleetv1beta1.Work) (*fleetv1beta1.AppliedWork, error) {
	workRef := klog.KObj(work)
	appliedWork := &fleetv1beta1.AppliedWork{}
	hasFinalizer := false
	if controllerutil.ContainsFinalizer(work, fleetv1beta1.WorkFinalizer) {
		hasFinalizer = true
		err := r.spokeClient.Get(ctx, types.NamespacedName{Name: work.Name}, appliedWork)
		switch {
		case apierrors.IsNotFound(err):
			klog.ErrorS(err, "AppliedWork finalizer resource does not exist even with the finalizer, it will be recreated", "appliedWork", workRef.Name)
		case err != nil:
			klog.ErrorS(err, "Failed to retrieve the appliedWork ", "appliedWork", workRef.Name)
			return nil, controller.NewAPIServerError(true, err)
		default:
			return appliedWork, nil
		}
	}

	// we create the appliedWork before setting the finalizer, so it should always exist unless it's deleted behind our back
	appliedWork = &fleetv1beta1.AppliedWork{
		ObjectMeta: metav1.ObjectMeta{
			Name: work.Name,
		},
		Spec: fleetv1beta1.AppliedWorkSpec{
			WorkName:      work.Name,
			WorkNamespace: work.Namespace,
		},
	}
	if err := r.spokeClient.Create(ctx, appliedWork); err != nil && !apierrors.IsAlreadyExists(err) {
		klog.ErrorS(err, "AppliedWork create failed", "appliedWork", workRef.Name)
		return nil, err
	}
	if !hasFinalizer {
		klog.InfoS("Add the finalizer to the work", "work", workRef)
		work.Finalizers = append(work.Finalizers, fleetv1beta1.WorkFinalizer)
		return appliedWork, r.hubClient.Update(ctx, work, &client.UpdateOptions{})
	}
	klog.InfoS("Recreated the appliedWork resource", "appliedWork", workRef.Name)
	return appliedWork, nil
}

// Decodes the manifest JSON into a Kubernetes unstructured object.
func (r *Reconciler) decodeManifest(manifest *fleetv1beta1.Manifest) (*schema.GroupVersionResource, *unstructured.Unstructured, error) {
	unstructuredObj := &unstructured.Unstructured{}
	if err := unstructuredObj.UnmarshalJSON(manifest.Raw); err != nil {
		return &schema.GroupVersionResource{}, nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	mapping, err := r.restMapper.RESTMapping(unstructuredObj.GroupVersionKind().GroupKind(), unstructuredObj.GroupVersionKind().Version)
	if err != nil {
		return &schema.GroupVersionResource{}, unstructuredObj, fmt.Errorf("failed to find GVR from member cluster client REST mapping: %w", err)
	}

	return &mapping.Resource, unstructuredObj, nil
}

// removeLeftOverManifests removes applied left-over manifests from the member cluster.
func (r *Reconciler) removeLeftOverManifests(
	ctx context.Context,
	leftOverManifests []fleetv1beta1.AppliedResourceMeta,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) utilerrors.Aggregate {
	// Remove all the manifests in parallel.
	//
	// This is concurrency safe as each worker processes its own applied manifest and writes
	// to its own error slot.

	// Prepare a child context.
	// Cancel the child context anyway to avoid leaks.
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Pre-allocate the slice.
	errs := make([]error, len(leftOverManifests))
	doWork := func(pieces int) {
		appliedManifestMeta := leftOverManifests[pieces]

		var err error
		switch {
		case appliedManifestMeta.GenerateName != "" && appliedManifestMeta.Name == "":
			// The object has a generated name but the name is not set; this can happen in cases
			// where the manifest condition entry describes a write-ahead attempt. In this case
			// Fleet would need to look up matching objects to complete the cleanup.
			//
			// This is a relatively expensive op, but it happens only in cases where the Fleet
			// agent is interrupted untimely during a reconciliation loop, which should be
			// fairly rare.
			err = r.removeOneLeftOverManifestWithGenerateName(ctx, appliedManifestMeta, expectedAppliedWorkOwnerRef)
			errs[pieces] = fmt.Errorf("failed to remove the left-over manifest (object with generated name): %w", err)
		default:
			// The object has a regular name; Fleet can look up the object directly.
			err = r.removeOneLeftOverManifest(ctx, appliedManifestMeta, expectedAppliedWorkOwnerRef)
			errs[pieces] = fmt.Errorf("failed to remove the left-over manifest (regular object): %w", err)
		}
	}
	r.parallelizer.ParallelizeUntil(childCtx, len(leftOverManifests), doWork, "removeLeftOverManifests")

	return utilerrors.NewAggregate(errs)
}

// removeOneLeftOverManifestWithGenerateName removes an applied manifest object that is left over
// in the member cluster.
func (r *Reconciler) removeOneLeftOverManifest(
	ctx context.Context,
	leftOverManifest fleetv1beta1.AppliedResourceMeta,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) error {
	// Build the GVR.
	gvr := schema.GroupVersionResource{
		Group:    leftOverManifest.Group,
		Version:  leftOverManifest.Version,
		Resource: leftOverManifest.Resource,
	}
	manifestNamespace := leftOverManifest.Namespace
	manifestName := leftOverManifest.Name

	inMemberClusterObj, err := r.spokeDynamicClient.
		Resource(gvr).
		Namespace(manifestNamespace).
		Get(ctx, manifestName, metav1.GetOptions{})
	switch {
	case err != nil && apierrors.IsNotFound(err):
		// The object has been deleted from the member cluster; no further action is needed.
		return nil
	case err != nil:
		// Failed to retrieve the object from the member cluster.
		return fmt.Errorf("failed to retrieve the object from the member cluster (gvr=%+v, manifestObj=%+v): %w", gvr, klog.KRef(manifestNamespace, manifestName), err)
	case inMemberClusterObj.GetDeletionTimestamp() != nil:
		// The object has been marked for deletion; no further action is needed.
		return nil
	}

	// There are occasions, though rare, where the object has the same GVR + namespace + name
	// combo but is not the applied object Fleet tries to find. This could happen if the object
	// has been deleted and then re-created manually without Fleet's acknowledgement. In such cases
	// Fleet would ignore the object, and this is not registered as an error.
	if !isInMemberClusterObjectDerivedFromManifestObj(inMemberClusterObj, expectedAppliedWorkOwnerRef) {
		// The object is not derived from the manifest object.
		klog.V(2).InfoS("The object to remove is not derived from the manifest object; will not proceed with the removal",
			"gvr", gvr, "manifestObj",
			klog.KRef(manifestNamespace, manifestName), "inMemberClusterObj", klog.KObj(inMemberClusterObj),
			"expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		return nil
	}

	switch {
	case len(inMemberClusterObj.GetOwnerReferences()) > 1:
		// Fleet is not the sole owner of the object; in this case, Fleet will only drop the
		// ownership.
		klog.V(2).InfoS("The object to remove is co-owned by other sources; Fleet will drop the ownership",
			"gvr", gvr, "manifestObj",
			klog.KRef(manifestNamespace, manifestName), "inMemberClusterObj", klog.KObj(inMemberClusterObj),
			"expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		removeOwnerRef(inMemberClusterObj, expectedAppliedWorkOwnerRef)
		if _, err := r.spokeDynamicClient.Resource(gvr).Namespace(manifestNamespace).Update(ctx, inMemberClusterObj, metav1.UpdateOptions{}); err != nil && !apierrors.IsNotFound(err) {
			// Failed to drop the ownership.
			return fmt.Errorf("failed to drop the ownership of the object (gvr=%+v, manifestObj=%+v, inMemberClusterObj=%+v, expectedAppliedWorkOwnerRef=%+v): %w",
				gvr, klog.KRef(manifestNamespace, manifestName), klog.KObj(inMemberClusterObj), *expectedAppliedWorkOwnerRef, err)
		}
	default:
		// Fleet is the sole owner of the object; in this case, Fleet will delete the object.
		klog.V(2).InfoS("The object to remove is solely owned by Fleet; Fleet will delete the object",
			"gvr", gvr, "manifestObj",
			klog.KRef(manifestNamespace, manifestName), "inMemberClusterObj", klog.KObj(inMemberClusterObj),
			"expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		inMemberClusterObjUID := inMemberClusterObj.GetUID()
		deleteOpts := metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{
				// Add a UID pre-condition to guard against the case where the object has changed
				// right before the deletion request is sent.
				//
				// Technically speaking resource version based concurrency control should also be
				// enabled here; Fleet drops the check to avoid conflicts; this is safe as the Fleet
				// ownership is considered to be a reserved field and other changes on the object are
				// irrelevant to this step.
				UID: &inMemberClusterObjUID,
			},
		}
		if err := r.spokeDynamicClient.Resource(gvr).Namespace(manifestNamespace).Delete(ctx, manifestName, deleteOpts); err != nil && !apierrors.IsNotFound(err) {
			// Failed to delete the object from the member cluster.
			return fmt.Errorf("failed to delete the object (gvr=%+v, manifestObj=%+v, inMemberClusterObj=%+v, expectedAppliedWorkOwnerRef=%+v): %w",
				gvr, klog.KRef(manifestNamespace, manifestName), klog.KObj(inMemberClusterObj), *expectedAppliedWorkOwnerRef, err)
		}
	}
	return nil
}

// removeOneLeftOverManifestWithGenerateName removes an applied manifest object with a generated
// name only that might be left over in the member cluster.
func (r *Reconciler) removeOneLeftOverManifestWithGenerateName(
	ctx context.Context,
	leftOverManifest fleetv1beta1.AppliedResourceMeta,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) error {
	// Build the GVR.
	gvr := schema.GroupVersionResource{
		Group:    leftOverManifest.Group,
		Version:  leftOverManifest.Version,
		Resource: leftOverManifest.Resource,
	}
	manifestNamespace := leftOverManifest.Namespace

	// The object has only a generate name; Fleet would need to filter out matching objects by
	// an list op.
	inMemberClusterObjCandidates, err := r.spokeDynamicClient.
		Resource(gvr).
		Namespace(manifestNamespace).
		List(ctx, metav1.ListOptions{})
	switch {
	case err != nil:
		// Failed to list objects in the member cluster.
		return fmt.Errorf("failed to list objects (gvr=%+v, manifestObjNamespace=%+v): %w", gvr, manifestNamespace, err)
	case len(inMemberClusterObjCandidates.Items) == 0:
		// No objects found in the member cluster.
		return nil
	}

	// Pre-allocation a slice to hold matching objects; use a smaller size as normally there should
	// be at most one matching object.
	inMemberClusterObjs := make([]*unstructured.Unstructured, 0, 1)
	for idx := range inMemberClusterObjCandidates.Items {
		inMemberClusterObjCandidate := inMemberClusterObjCandidates.Items[idx]
		if isInMemberClusterObjectDerivedFromManifestObjWithGenerateName(&inMemberClusterObjCandidate, leftOverManifest.GenerateName, expectedAppliedWorkOwnerRef) {
			inMemberClusterObjs = append(inMemberClusterObjs, &inMemberClusterObjCandidate)
		}
	}
	if len(inMemberClusterObjs) == 0 {
		// No matching objects found in the member cluster.
		return nil
	}

	// Remove all the matching objects.
	for idx := range inMemberClusterObjs {
		inMemberClusterObj := inMemberClusterObjs[idx]

		switch {
		case inMemberClusterObj.GetDeletionTimestamp() != nil:
			// The object has been marked for deletion; no further action is needed.
			continue
		case len(inMemberClusterObj.GetOwnerReferences()) > 1:
			// Fleet is not the sole owner of the object; in this case, Fleet will only drop the
			// ownership.
			klog.V(2).InfoS("The object to remove is co-owned by other sources; Fleet will drop the ownership",
				"gvr", gvr, "inMemberClusterObj", klog.KObj(inMemberClusterObj),
				"expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
			removeOwnerRef(inMemberClusterObj, expectedAppliedWorkOwnerRef)
			if _, err := r.spokeDynamicClient.Resource(gvr).Namespace(manifestNamespace).Update(ctx, inMemberClusterObj, metav1.UpdateOptions{}); err != nil && !apierrors.IsNotFound(err) {
				// Failed to drop the ownership.
				return fmt.Errorf("failed to drop the ownership of the object (gvr=%+v, inMemberClusterObj=%+v, expectedAppliedWorkOwnerRef=%+v): %w",
					gvr, klog.KObj(inMemberClusterObj), *expectedAppliedWorkOwnerRef, err)
			}
		default:
			// Fleet is the sole owner of the object; in this case, Fleet will delete the object.
			klog.V(2).InfoS("The object to remove is solely owned by Fleet; Fleet will delete the object",
				"gvr", gvr, "inMemberClusterObj", klog.KObj(inMemberClusterObj),
				"expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
			inMemberClusterObjUID := inMemberClusterObj.GetUID()
			deleteOpts := metav1.DeleteOptions{
				Preconditions: &metav1.Preconditions{
					// Add a UID pre-condition to guard against the case where the object has changed
					// right before the deletion request is sent.
					//
					// Technically speaking resource version based concurrency control should also be
					// enabled here; Fleet drops the check to avoid conflicts; this is safe as the Fleet
					// ownership is considered to be a reserved field and other changes on the object are
					// irrelevant to this step.
					UID: &inMemberClusterObjUID,
				},
			}
			if err := r.spokeDynamicClient.Resource(gvr).Namespace(manifestNamespace).Delete(ctx, inMemberClusterObj.GetName(), deleteOpts); err != nil && !apierrors.IsNotFound(err) {
				// Failed to delete the object from the member cluster.
				return fmt.Errorf("failed to delete the object (gvr=%+v, inMemberClusterObj=%+v, expectedAppliedWorkOwnerRef=%+v): %w",
					gvr, klog.KObj(inMemberClusterObj), *expectedAppliedWorkOwnerRef, err)
			}
		}
	}
	return nil
}

// Join starts to reconcile
func (r *Reconciler) Join(_ context.Context) error {
	if !r.joined.Load() {
		klog.InfoS("Mark the apply work reconciler joined")
	}
	r.joined.Store(true)
	return nil
}

// Leave start
func (r *Reconciler) Leave(ctx context.Context) error {
	var works fleetv1beta1.WorkList
	if r.joined.Load() {
		klog.InfoS("Mark the apply work reconciler left")
	}
	r.joined.Store(false)
	// list all the work object we created in the member cluster namespace
	listOpts := []client.ListOption{
		client.InNamespace(r.workNameSpace),
	}
	if err := r.hubClient.List(ctx, &works, listOpts...); err != nil {
		klog.ErrorS(err, "Failed to list all the work object", "clusterNS", r.workNameSpace)
		return client.IgnoreNotFound(err)
	}
	// we leave the resources on the member cluster for now
	for _, work := range works.Items {
		staleWork := work.DeepCopy()
		if controllerutil.ContainsFinalizer(staleWork, fleetv1beta1.WorkFinalizer) {
			controllerutil.RemoveFinalizer(staleWork, fleetv1beta1.WorkFinalizer)
			if updateErr := r.hubClient.Update(ctx, staleWork, &client.UpdateOptions{}); updateErr != nil {
				klog.ErrorS(updateErr, "Failed to remove the work finalizer from the work",
					"clusterNS", r.workNameSpace, "work", klog.KObj(staleWork))
				return updateErr
			}
		}
	}
	klog.V(2).InfoS("Successfully removed all the work finalizers in the cluster namespace",
		"clusterNS", r.workNameSpace, "number of work", len(works.Items))
	return nil
}

// SetupWithManager wires up the controller.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(ctrloption.Options{
			MaxConcurrentReconciles: r.concurrentReconciles,
		}).
		For(&fleetv1beta1.Work{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

// setManifestHashAnnotation computes the hash of the provided manifest and sets an annotation of the
// hash on the provided unstructured object.
func setManifestHashAnnotation(manifestObj *unstructured.Unstructured) error {
	cleanedManifestObj := discardFieldsIrrelevantInComparisonFrom(manifestObj)
	manifestObjHash, err := resource.HashOf(cleanedManifestObj.Object)
	if err != nil {
		return err
	}

	annotations := manifestObj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[fleetv1beta1.ManifestHashAnnotation] = manifestObjHash
	manifestObj.SetAnnotations(annotations)
	return nil
}
