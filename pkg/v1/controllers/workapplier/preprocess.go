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
	"sync/atomic"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/ownerreferences"
)

func prepareWorkObjectAndManifestProcessingStates(works []*placementv1alpha1.Work,
	appliedWorks []*placementv1alpha1.AppliedWork) ([]*workObjectProcessingState, []*manifestProcessingState) {
	// Pre-allocate the slices.
	workObjectStates := make([]*workObjectProcessingState, 0, len(works))
	manifestProcessingStates := make([]*manifestProcessingState, 0, len(works[0].Spec.Manifests))

	primaryWork := works[0]
	primaryAppliedWorkOwnerRef := &metav1.OwnerReference{
		APIVersion:         placementv1alpha1.GroupVersion.String(),
		Kind:               "AppliedWork",
		Name:               appliedWorks[0].GetName(),
		UID:                appliedWorks[0].GetUID(),
		BlockOwnerDeletion: ptr.To(true),
	}

	for i := range works {
		work := works[i]

		perWorkManifestProcessingStates := make([]*manifestProcessingState, 0, len(work.Spec.Manifests))
		for j := range work.Spec.Manifests {
			manifest := &work.Spec.Manifests[j]
			manifestProcessingState := &manifestProcessingState{
				manifest:              manifest,
				fromWorkObj:           work,
				fromPrimaryWorkObject: primaryWork,
				// Use the appliedWork object that corresponds to the primary work object as the owner for
				// all manifests in this work object once they are applied.
				ownedBy: primaryAppliedWorkOwnerRef,
			}
			perWorkManifestProcessingStates = append(perWorkManifestProcessingStates, manifestProcessingState)
			manifestProcessingStates = append(manifestProcessingStates, manifestProcessingState)
		}
		workObjectState := &workObjectProcessingState{
			work:                     work,
			appliedWork:              appliedWorks[i],
			appliedWorkOwnerRef:      primaryAppliedWorkOwnerRef,
			manifestProcessingStates: perWorkManifestProcessingStates,
		}
		workObjectStates = append(workObjectStates, workObjectState)
	}

	return workObjectStates, manifestProcessingStates
}

func (r *Reconciler) preProcessWorkObjects(ctx context.Context, workObjProcessingStates []*workObjectProcessingState) (sets.Set[string], error) {
	for idx := range workObjProcessingStates {
		workObjProcessingState := workObjProcessingStates[idx]

		// Pre-process the manifests one work object at a time, so that the ordinals assigned to the
		// manifest identifiers stay relative to their own work object.
		if err := r.preProcessPerWorkObjManifests(ctx, workObjProcessingState.manifestProcessingStates); err != nil {
			return nil, kferrors.Wraps(err, "failed to pre-process work object", "work", klog.KObj(workObjProcessingState.work))
		}

		klog.V(2).InfoS("Pre-processed a work object",
			"work", klog.KObj(workObjProcessingState.work),
			"manifestCount", len(workObjProcessingState.manifestProcessingStates))
	}

	seenManifestIDs := markDuplicatedManifests(workObjProcessingStates)

	for idx := range workObjProcessingStates {
		workObjProcessingState := workObjProcessingStates[idx]

		// Write-ahead the manifest processing attempts in the work object status so that
		// KubeFleet can always track applied manifests, even upon untimely crashes. This method will
		// also check for any leftover apply attempts from previous runs and clean them up (if the
		// corresponding manifest object has been applied).
		if err := r.writeAheadPerWorkObjManifestProcessingAttempts(ctx, workObjProcessingState, seenManifestIDs); err != nil {
			return nil, kferrors.Wraps(err, "failed to write ahead manifest processing states", "work", klog.KObj(workObjProcessingState.work))
		}

		klog.V(2).InfoS("Wrote ahead manifest processing states for a work object",
			"work", klog.KObj(workObjProcessingState.work))
	}
	return seenManifestIDs, nil
}

func (r *Reconciler) preProcessPerWorkObjManifests(ctx context.Context, manifestProcessingStates []*manifestProcessingState) error {
	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	var innerErr atomic.Value
	preProcessOneManifest := func(pieces int) {
		// At this moment the processing state is just initialized.
		processingState := manifestProcessingStates[pieces]
		ownerWorkObj := processingState.fromWorkObj

		gvr, manifestObj, err := r.decodeManifest(processingState.manifest)
		// There exists a corner case where the decoding might have failed due to transient errors (e.g., the
		// API discovery endpoint is not responding); in this case, if we register the error as a user-end
		// decoding error and skip the manifest, and if the manifest has been applied before, the error might
		// trigger the later left-over cleanup step, which would delete the previously applied manifest. It should
		// be corrected in the subsequent reconciliation; however, it might trigger fluctuations. To be on the safer
		// side, the work applier is set to requeue upon transient decoding errors.
		//
		// Note that with aggregated API discovery enabled by default in most Kubernetes clusters now,
		// normally if the first decoding attempt succeeds, all subsequent decoding attempts will be resolved via
		// cache.
		if err != nil && kferrors.Category(err) == kferrors.ErrCategoryTransient {
			wrappedErr := kferrors.Wraps(err, "failed to decode the manifest", "ordinal", pieces)
			innerErr.Store(wrappedErr)
			childCancel()
			return
		}
		// Build the manifest identifier. Note that this would return an identifier even if the decoding fails.
		processingState.id = buildManifestIdentifier(pieces, gvr, manifestObj)
		if err != nil {
			wrappedErr := kferrors.Wraps(err, "failed to decode the manifest", "ordinal", pieces)
			processingState.applyErr = wrappedErr
			processingState.applyRes = ApplyResTypeDecodingErred
			return
		}

		// Reject objects with a generate name but no name.
		if len(manifestObj.GetGenerateName()) > 0 && len(manifestObj.GetName()) == 0 {
			wrappedErr := kferrors.NewUserError(nil, "rejected an object with only generate name",
				"ordinal", pieces, "manifestObj", klog.KObj(manifestObj), "work", klog.KObj(ownerWorkObj))
			processingState.applyErr = wrappedErr
			processingState.applyRes = ApplyResTypeFoundGenerateName
			return
		}

		processingState.manifestObj = manifestObj
		processingState.gvr = gvr

		// Add the string representation of the manifest identifier to the bundle.
		//
		// Note that the string representation ignores the ordinal number in the identifier.
		idStr := formatManifestIdentifierStr(processingState.id)
		processingState.idStr = idStr

		klog.V(2).InfoS("Decoded a manifest",
			"manifestObj", klog.KObj(manifestObj),
			"GVR", *gvr,
			"work", klog.KObj(ownerWorkObj))
	}
	r.parallelizer.ParallelizeUntil(childCtx, len(manifestProcessingStates), preProcessOneManifest, "preProcessManifests")

	if err := innerErr.Load(); err != nil {
		return kferrors.Wraps(err.(error), "failed to pre-process manifests")
	}
	// If the context has been cancelled, return an error immediately so that the reconciliation loop would exit now.
	if ctx.Err() != nil {
		return kferrors.NewTransientError(ctx.Err(), "context was canceled or timed out during manifest pre-processing")
	}
	return nil
}

// Decodes the manifest JSON into a Kubernetes unstructured object.
func (r *Reconciler) decodeManifest(manifest *placementv1alpha1.Manifest) (*schema.GroupVersionResource, *unstructured.Unstructured, error) {
	unstructuredObj := &unstructured.Unstructured{}
	if err := unstructuredObj.UnmarshalJSON(manifest.Raw); err != nil {
		return &schema.GroupVersionResource{}, nil, kferrors.NewUserError(err, "failed to unmarshal JSON")
	}

	mapping, err := r.restMapper.RESTMapping(unstructuredObj.GroupVersionKind().GroupKind(), unstructuredObj.GroupVersionKind().Version)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return &schema.GroupVersionResource{}, unstructuredObj,
				kferrors.NewUserError(err, "failed to find GVR from member cluster client REST mapping; the resource might not be available on the member cluster side")
		}
		return &schema.GroupVersionResource{}, unstructuredObj,
			kferrors.NewTransientError(err, "failed to complete REST mapping for the manifest")
	}

	return &mapping.Resource, unstructuredObj, nil
}

func buildManifestIdentifier(
	manifestIdx int,
	gvr *schema.GroupVersionResource,
	manifestObj *unstructured.Unstructured,
) *placementv1alpha1.ManifestIdentifier {
	// The ordinal field is always set.
	identifier := &placementv1alpha1.ManifestIdentifier{
		Ordinal: manifestIdx,
	}

	// Set the GVK, name, namespace, and generate name information if the manifest can be decoded
	// as a Kubernetes unstructured object.
	//
	// Note that for cluster-scoped objects, the namespace field will be empty.
	if manifestObj != nil {
		identifier.APIGroup = manifestObj.GroupVersionKind().Group
		identifier.APIVersion = manifestObj.GroupVersionKind().Version
		identifier.Kind = manifestObj.GetKind()
		identifier.Name = manifestObj.GetName()
		identifier.Namespace = manifestObj.GetNamespace()
	}

	// Set the GVR information if the manifest object can be REST mapped.
	if gvr != nil {
		identifier.Resource = gvr.Resource
	}

	return identifier
}

func formatManifestIdentifierStr(id *placementv1alpha1.ManifestIdentifier) string {
	// API version is omitted; KubeFleet will report an error if one attempts to place two manifests with the same
	// GK but different versions.
	return fmt.Sprintf("GK=%s/%s, Namespace=%s, Name=%s",
		id.APIGroup, id.Kind, id.Namespace, id.Name)
}

// markDuplicatedManifests rejects any manifest that shares an identifier with a manifest seen earlier, so that
// only its first occurrence is applied; duplicates would otherwise overwrite one another on the member cluster.
func markDuplicatedManifests(workObjProcessingStates []*workObjectProcessingState) sets.Set[string] {
	// The set spans all the work objects, as linked work objects apply to the same member cluster.
	seenManifestIDs := sets.New[string]()

	for idx := range workObjProcessingStates {
		workObjProcessingState := workObjProcessingStates[idx]

		for i := range workObjProcessingState.manifestProcessingStates {
			processingState := workObjProcessingState.manifestProcessingStates[i]
			if processingState.applyErr != nil {
				// The manifest has already failed pre-processing; it has no usable identifier string.
				klog.V(2).InfoS("Skipped a manifest in the duplicated manifest checking process as it has failed in the decoding process",
					"work", klog.KObj(workObjProcessingState.work), "ordinal", i,
					"applyErr", processingState.applyErr, "applyResTyp", processingState.applyRes)
				continue
			}

			if seenManifestIDs.Has(processingState.idStr) {
				processingState.applyErr = kferrors.NewUserError(nil, "rejected a duplicated manifest",
					"ordinal", processingState.id.Ordinal, "manifest", processingState.idStr,
					"work", klog.KObj(processingState.fromWorkObj))
				processingState.applyRes = ApplyResTypeDuplicated
				continue
			}
			seenManifestIDs.Insert(processingState.idStr)
		}

		klog.V(2).InfoS("Completed the check for duplicated manifests", "work", klog.KObj(workObjProcessingState.work))
	}
	return seenManifestIDs
}

func (r *Reconciler) writeAheadPerWorkObjManifestProcessingAttempts(ctx context.Context,
	workObjProcessingState *workObjectProcessingState, seenManifestIDs sets.Set[string]) error {
	work := workObjProcessingState.work
	manifestProcessingStates := workObjProcessingState.manifestProcessingStates

	// As a shortcut, if there's no spec change in the Work object and the status indicates that
	// a previous apply attempt has been recorded (**successful or not**), KubeFleet will skip the write-ahead
	// op.
	appliedCond := meta.FindStatusCondition(work.Status.Conditions, placementv1alpha1.WorkCondTypeApplied)
	if appliedCond != nil && appliedCond.ObservedGeneration == work.Generation {
		klog.V(2).InfoS("Attempt to apply the current set of manifests has been made before and the results have been recorded; will skip the write-ahead process",
			"work", klog.KObj(work))
		return nil
	}

	// Prepare the status update (the new manifest conditions) for the write-ahead process.
	perManifestStatusesToWriteAhead := make([]placementv1alpha1.PerManifestStatus, 0, len(manifestProcessingStates))

	// Build an index of existing manifest statuses for quicker lookups.
	existingManifestStatusIdx := prepareExistingManifestStatusIndex(work.Status.Manifests)

	for idx := range manifestProcessingStates {
		manifestProcessingState := manifestProcessingStates[idx]
		if manifestProcessingState.applyErr != nil {
			// Skip a manifest if it cannot be pre-processed, e.g., it cannot be decoded.
			//
			// Such manifests would still be reported in the status (see the later parts of the
			// reconciliation loop), it is just that they are not relevant in the write-ahead
			// process.
			klog.V(2).InfoS("Skipped a manifest in the write-ahead process as it has failed the decoding process or the duplicated manifest checking process",
				"work", klog.KObj(work), "ordinal", idx,
				"applyErr", manifestProcessingState.applyErr, "applyResTyp", manifestProcessingState.applyRes)
			continue
		}

		manifestStatusToWriteAhead := buildManifestStatusToWriteAhead(manifestProcessingState, work, existingManifestStatusIdx)
		perManifestStatusesToWriteAhead = append(perManifestStatusesToWriteAhead, manifestStatusToWriteAhead)

		klog.V(2).InfoS("Prepared write-ahead information for a manifest",
			"manifestObj", klog.KObj(manifestProcessingState.manifestObj), "manifestID", manifestProcessingState.id, "work", klog.KObj(work))
	}

	// Identify any manifests from previous runs that might have been applied and are now left
	// over in the member cluster.
	leftOverManifests := findLeftOverManifests(perManifestStatusesToWriteAhead, work, existingManifestStatusIdx, seenManifestIDs)
	if err := r.removeLeftOverManifests(ctx, leftOverManifests, workObjProcessingState); err != nil {
		return kferrors.Wraps(err, "failed to remove leftover manifests",
			"work", klog.KObj(work), "leftOverManifestCount", len(leftOverManifests), "removalFailedCount", len(err.Errors()))
	}
	klog.V(2).InfoS("Left-over manifests are found and removed",
		"leftOverManifestCount", len(leftOverManifests), "work", klog.KObj(work))

	// Update the status.
	//
	// Note that the Work object might have been refreshed by controllers on the hub cluster
	// before this step runs; in this case the current reconciliation loop must be abandoned.
	if work.Status.Conditions == nil {
		// As a sanity check, set an empty set of conditions. Currently the API definition does
		// not allow nil conditions.
		work.Status.Conditions = []metav1.Condition{}
	}
	work.Status.Manifests = perManifestStatusesToWriteAhead
	if err := r.hubClient.Status().Update(ctx, work); err != nil {
		return kferrors.NewAPIServerError(err, "failed to update work object status", false)
	}

	klog.V(2).InfoS("Write-ahead process completed", "work", klog.KObj(work))
	return nil
}

// prepareExistingManifestStatusIndex indexes the given manifest statuses by their identifier strings, so that
// the status of a specific manifest can be looked up without scanning the whole list.
func prepareExistingManifestStatusIndex(manifestStatuses []placementv1alpha1.PerManifestStatus) map[string]int {
	idx := make(map[string]int, len(manifestStatuses))
	for i := range manifestStatuses {
		idStr := formatManifestIdentifierStr(&manifestStatuses[i].Identifier)
		idx[idStr] = i
	}
	return idx
}

// buildManifestStatusToWriteAhead prepares the status of a manifest for the write-ahead process.
func buildManifestStatusToWriteAhead(manifestProcessingState *manifestProcessingState,
	work *placementv1alpha1.Work,
	existingManifestStatusIdx map[string]int,
) placementv1alpha1.PerManifestStatus {
	// If the manifest has an entry in the existing set of manifest statuses, port the previously
	// recorded processing results back.
	if idx, found := existingManifestStatusIdx[manifestProcessingState.idStr]; found {
		return work.Status.Manifests[idx]
	}

	// The manifest has not been processed before; report that KubeFleet is preparing to process it.
	return placementv1alpha1.PerManifestStatus{
		Identifier: *manifestProcessingState.id,
		Conditions: []metav1.Condition{
			{
				Type:               placementv1alpha1.WorkCondTypeApplied,
				Status:             metav1.ConditionFalse,
				Reason:             placementv1alpha1.WorkAppliedCondPreparingToProcessReason,
				Message:            "The manifest is being prepared for processing",
				LastTransitionTime: metav1.Now(),
			},
		},
	}
}

// findLeftOverManifests returns the manifests that have been left over on the member cluster side.
func findLeftOverManifests(
	perManifestStatusesToWriteAhead []placementv1alpha1.PerManifestStatus,
	work *placementv1alpha1.Work,
	existingManifestStatusIdx map[string]int,
	seenManifestIDs sets.Set[string],
) []placementv1alpha1.ManifestIdentifier {
	// Build an index for quicker lookup in the newly prepared write-ahead manifest conditions.
	// Here the work applier uses the string representations as map keys; ordinals are omitted from any lookup.
	//
	// Note that before this step, the work applier has already filtered out duplicate manifests.
	manifestStatusesToWriteAheadIdx := make(map[string]int, len(perManifestStatusesToWriteAhead))
	for idx := range perManifestStatusesToWriteAhead {
		idStr := formatManifestIdentifierStr(&perManifestStatusesToWriteAhead[idx].Identifier)
		manifestStatusesToWriteAheadIdx[idStr] = idx
	}

	// For each manifest condition in the existing set of manifest conditions, check if
	// there is a corresponding entry in the set of manifest conditions prepared for the write-ahead
	// process. If not, the work applier will consider that the manifest has been left over on the member
	// cluster side and should be removed.

	// An existing manifest status without a counterpart in the write-ahead set refers to a manifest that
	// is no longer part of the work object, and that might still be present on the member cluster side.
	leftOverManifests := []placementv1alpha1.ManifestIdentifier{}
	for existingManifestIDStr, existingManifestStatusIdx := range existingManifestStatusIdx {
		if _, found := manifestStatusesToWriteAheadIdx[existingManifestIDStr]; found {
			// The current manifest condition does not have a corresponding entry in the set of manifest
			// conditions prepared for the write-ahead process.
			continue
		}

		if seenManifestIDs.Has(existingManifestIDStr) {
			// There exists a corner case where a manifest might have changed its location, i.e., it was previously
			// seen on work object A but now lives on work object B. In this case the manifest should not be
			// categorized as a left-over manifest.
			klog.V(2).InfoS("A manifest has moved to another work object; it will not be considered a left-over manifest",
				"manifestID", existingManifestIDStr, "work", klog.KObj(work))
			continue
		}

		existingManifestStatus := work.Status.Manifests[existingManifestStatusIdx]

		// The work applier assumes that the manifest has been applied if:
		// a) it has an Applied condition set to the True status; or
		// b) it has an Applied condition which signals that the object is preparing to be processed.
		//
		// Note that the manifest status might not be up-to-date, so the work applier will not check on the
		// generation information.
		appliedCond := meta.FindStatusCondition(existingManifestStatus.Conditions, placementv1alpha1.WorkCondTypeApplied)
		if appliedCond == nil {
			continue
		}
		if appliedCond.Status == metav1.ConditionTrue || appliedCond.Reason == placementv1alpha1.WorkAppliedCondPreparingToProcessReason {
			leftOverManifests = append(leftOverManifests, existingManifestStatus.Identifier)
		}
	}
	return leftOverManifests
}

// removeLeftOverManifests removes applied left-over manifests from the member cluster.
func (r *Reconciler) removeLeftOverManifests(
	ctx context.Context,
	leftOverManifests []placementv1alpha1.ManifestIdentifier,
	workObjProcessingState *workObjectProcessingState,
) utilerrors.Aggregate {
	// Remove all the manifests in parallel.
	//
	// This is concurrency safe as each worker processes its own applied manifest and writes
	// to its own error slot.

	// Cancel the child context anyway to avoid leaks.
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Pre-allocate the slice.
	errs := make([]error, len(leftOverManifests))
	doWork := func(pieces int) {
		leftOverManifest := leftOverManifests[pieces]

		if err := r.removeOneLeftOverManifest(childCtx, leftOverManifest, workObjProcessingState.appliedWorkOwnerRef); err != nil {
			errs[pieces] = kferrors.Wraps(err, "failed to remove a left-over manifest", "manifestId", leftOverManifest)
		}
	}
	r.parallelizer.ParallelizeUntil(childCtx, len(leftOverManifests), doWork, "removeLeftOverManifests")

	return utilerrors.NewAggregate(errs)
}

// removeOneLeftOverManifest removes an applied manifest object that is left over in the member cluster.
func (r *Reconciler) removeOneLeftOverManifest(
	ctx context.Context,
	leftOverManifest placementv1alpha1.ManifestIdentifier,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) error {
	// Build the GVR.
	gvr := schema.GroupVersionResource{
		Group:    leftOverManifest.APIGroup,
		Version:  leftOverManifest.APIVersion,
		Resource: leftOverManifest.Resource,
	}
	manifestNamespace := leftOverManifest.Namespace
	manifestName := leftOverManifest.Name
	manifestRef := klog.KRef(manifestNamespace, manifestName)

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
		//
		// The cached flag is set to false as the dynamic client is non-caching.
		return kferrors.NewAPIServerError(err, "failed to retrieve the object from the member cluster", false,
			"gvr", gvr, "manifestObj", manifestRef)
	case inMemberClusterObj.GetDeletionTimestamp() != nil:
		// The object has been marked for deletion; no further action is needed.
		return nil
	}

	// There are occasions, though rare, where the object has the same GVR + namespace + name
	// combo but is not the applied manifest object the work applier tries to find. This could happen if the object
	// has been deleted and then re-created manually without the work applier's acknowledgement. In such cases
	// the work applier would ignore the object, and this is not registered as an error.
	if !isInMemberClusterObjectDerivedFromManifestObj(inMemberClusterObj, expectedAppliedWorkOwnerRef) {
		klog.V(2).InfoS("The object to remove is not derived from the manifest object; will not proceed with the removal",
			"gvr", gvr, "manifestObj", manifestRef,
			"inMemberClusterObj", klog.KObj(inMemberClusterObj),
			"expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		return nil
	}

	switch {
	case len(inMemberClusterObj.GetOwnerReferences()) > 1:
		// KubeFleet is not the sole owner of the object; in this case, KubeFleet will only drop the
		// ownership.
		klog.V(2).InfoS("The object to remove is co-owned by other sources; will drop the ownership",
			"gvr", gvr, "manifestObj", manifestRef,
			"inMemberClusterObj", klog.KObj(inMemberClusterObj),
			"expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		ownerreferences.Disown(inMemberClusterObj, expectedAppliedWorkOwnerRef)
		if _, err := r.spokeDynamicClient.Resource(gvr).Namespace(manifestNamespace).Update(ctx, inMemberClusterObj, metav1.UpdateOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return kferrors.NewAPIServerError(err, "failed to drop the ownership of the object", false,
				"gvr", gvr, "manifestObj", manifestRef,
				"inMemberClusterObj", klog.KObj(inMemberClusterObj),
				"expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		}
	default:
		// KubeFleet is the sole owner of the object; in this case, KubeFleet will delete the object.
		klog.V(2).InfoS("The object to remove is solely owned by KubeFleet; will delete the object",
			"gvr", gvr, "manifestObj", manifestRef,
			"inMemberClusterObj", klog.KObj(inMemberClusterObj),
			"expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		inMemberClusterObjUID := inMemberClusterObj.GetUID()
		deleteOpts := metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{
				// Add a UID pre-condition to guard against the case where the object has changed
				// right before the deletion request is sent.
				//
				// Technically speaking resource version based concurrency control should also be
				// enabled here; the work applier drops the check to avoid conflicts; this is safe as the KubeFleet
				// ownership is considered to be a reserved field and other changes on the object are
				// irrelevant to this step.
				UID: &inMemberClusterObjUID,
			},
		}
		if err := r.spokeDynamicClient.Resource(gvr).Namespace(manifestNamespace).Delete(ctx, manifestName, deleteOpts); err != nil && !apierrors.IsNotFound(err) {
			return kferrors.NewAPIServerError(err, "failed to delete the object", false,
				"gvr", gvr, "manifestObj", manifestRef,
				"inMemberClusterObj", klog.KObj(inMemberClusterObj),
				"expectedAppliedWorkOwnerRef", *expectedAppliedWorkOwnerRef)
		}
	}
	return nil
}

// isInMemberClusterObjectDerivedFromManifestObj checks if an object in the member cluster is derived
// from a specific manifest object.
func isInMemberClusterObjectDerivedFromManifestObj(inMemberClusterObj *unstructured.Unstructured, expectedAppliedWorkOwnerRef *metav1.OwnerReference) bool {
	// Do a sanity check.
	if inMemberClusterObj == nil {
		return false
	}

	// Verify if the owner reference still stands.
	curOwners := inMemberClusterObj.GetOwnerReferences()
	for idx := range curOwners {
		if ownerreferences.AreEqual(&curOwners[idx], expectedAppliedWorkOwnerRef) {
			return true
		}
	}
	return false
}
