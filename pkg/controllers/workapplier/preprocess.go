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
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/defaulter"
)

const (
	// A list of condition related values.
	ManifestAppliedCondPreparingToProcessReason  = "PreparingToProcess"
	ManifestAppliedCondPreparingToProcessMessage = "The manifest is being prepared for processing."
)

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
		// At this moment the bundles are just created.
		bundle := bundles[pieces]

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

		// Reject objects with a generate name but no name.
		if len(manifestObj.GetGenerateName()) > 0 && len(manifestObj.GetName()) == 0 {
			// The manifest object has a generate name but no name.
			klog.V(2).InfoS("Reject objects with only generate name", "manifestObj", klog.KObj(manifestObj), "work", klog.KObj(work))
			bundle.applyErr = fmt.Errorf("objects with only generate name are not supported")
			bundle.applyResTyp = ManifestProcessingApplyResultTypeFoundGenerateName
			return
		}

		bundle.manifestObj = manifestObj
		bundle.gvr = gvr
		klog.V(2).InfoS("Decoded a manifest",
			"manifestObj", klog.KObj(manifestObj),
			"GVR", *gvr,
			"work", klog.KObj(work))
	}
	r.parallelizer.ParallelizeUntil(childCtx, len(bundles), doWork, "decodingManifests")

	// Write ahead the manifest processing attempts in the Work object status. In the process
	// Fleet will also perform a cleanup to remove any left-over manifests that are applied
	// from previous runs.
	//
	// This is set up to address a corner case where the agent could crash right after manifests
	// are applied but before the status is properly updated, and upon the agent's restart, the
	// list of manifests has changed (some manifests have been removed). This would lead to a
	// situation where Fleet would lose track of the removed manifests.
	//
	// To avoid conflicts (or the hassle of preparing individual patches), the status update is
	// done in batch.
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
	workRef := klog.KObj(work)

	// As a shortcut, if there's no spec change in the Work object and the status indicates that
	// a previous apply attempt has been recorded (**successful or not**), Fleet will skip the write-ahead
	// op.
	workAppliedCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
	if workAppliedCond != nil && workAppliedCond.ObservedGeneration == work.Generation {
		klog.V(2).InfoS("Attempt to apply the current set of manifests has been made before and the results have been recorded; will skip the write-ahead process", "work", workRef)
		return nil
	}

	// As another shortcut, if the Work object has an apply strategy that has the ReportDiff
	// mode on, Fleet will skip the write-ahead op.
	//
	// Note that in this scenario Fleet will not attempt to remove any left over manifests;
	// such manifests will only get cleaned up when the ReportDiff mode is turned off, or the
	// CRP itself is deleted.
	if work.Spec.ApplyStrategy != nil && work.Spec.ApplyStrategy.Type == fleetv1beta1.ApplyStrategyTypeReportDiff {
		klog.V(2).InfoS("The apply strategy is set to report diff; will skip the write-ahead process", "work", workRef)
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
		// it. This is to address a corner case where users might have specified the same manifest
		// twice in resource envelopes; duplication will not occur if the manifests are directly
		// created in the hub cluster.
		//
		// A side note: Golang does support using structs as map keys; preparing the string
		// representations of structs as keys can help performance, though not by much. The reason
		// why string representations are used here is not for performance, though; instead, it
		// is to address the issue that for this comparison, ordinals should be ignored.
		wriStr, err := formatWRIString(bundle.id)
		if err != nil {
			// Normally this branch will never run as all manifests that cannot be decoded has been
			// skipped in the check above. Here Fleet simply skips the manifest.
			klog.ErrorS(err, "Failed to format the work resource identifier string",
				"ordinal", idx, "work", workRef)
			_ = controller.NewUnexpectedBehaviorError(err)
			continue
		}
		if _, found := checked[wriStr]; found {
			klog.V(2).InfoS("A duplicate manifest has been found",
				"ordinal", idx, "work", workRef, "WRI", wriStr)
			bundle.applyErr = fmt.Errorf("a duplicate manifest has been found")
			bundle.applyResTyp = ManifestProcessingApplyResultTypeDuplicated
			continue
		}
		checked[wriStr] = true

		// Prepare the manifest conditions for the write-ahead process.
		manifestCondForWA := prepareManifestCondForWriteAhead(wriStr, bundle.id, work.Generation, existingManifestCondQIdx, work.Status.ManifestConditions)
		manifestCondsForWA = append(manifestCondsForWA, manifestCondForWA)

		klog.V(2).InfoS("Prepared write-ahead information for a manifest",
			"manifestObj", klog.KObj(bundle.manifestObj), "WRI", wriStr, "work", workRef)
	}

	// Identify any manifests from previous runs that might have been applied and are now left
	// over in the member cluster.
	leftOverManifests := findLeftOverManifests(manifestCondsForWA, existingManifestCondQIdx, work.Status.ManifestConditions)
	if err := r.removeLeftOverManifests(ctx, leftOverManifests, expectedAppliedWorkOwnerRef); err != nil {
		klog.Errorf("Failed to remove left-over manifests (work=%+v, leftOverManifestCount=%d, removalFailureCount=%d)",
			workRef, len(leftOverManifests), len(err.Errors()))
		return fmt.Errorf("failed to remove left-over manifests: %w", err)
	}
	klog.V(2).InfoS("Left-over manifests are found and removed",
		"leftOverManifestCount", len(leftOverManifests), "work", workRef)

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
	klog.V(2).InfoS("Write-ahead process completed", "work", workRef)

	// Set the defaults again as the result yielded by the status update might have changed the object.
	defaulter.SetDefaultsWork(work)
	return nil
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

// buildWorkResourceIdentifier builds a work resource identifier for a manifest.
//
// Note that if the manifest cannot be decoded/applied, this function will return an identifier with
// the available information on hand.
func buildWorkResourceIdentifier(
	manifestIdx int,
	gvr *schema.GroupVersionResource,
	manifestObj *unstructured.Unstructured,
) *fleetv1beta1.WorkResourceIdentifier {
	// The ordinal field is always set.
	identifier := &fleetv1beta1.WorkResourceIdentifier{
		Ordinal: manifestIdx,
	}

	// Set the GVK, name, namespace, and generate name information if the manifest can be decoded
	// as a Kubernetes unstructured object.
	//
	// Note that:
	// * For cluster-scoped objects, the namespace field will be empty.
	// * For objects with generated names, the name field will be empty.
	// * For regular objects (i.e., objects with a pre-defined name), the generate name field will be empty.
	if manifestObj != nil {
		identifier.Group = manifestObj.GroupVersionKind().Group
		identifier.Version = manifestObj.GroupVersionKind().Version
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

// prepareExistingManifestCondQIdx returns a map that allows quicker look up of a manifest
// condition given a work resource identifier.
func prepareExistingManifestCondQIdx(existingManifestConditions []fleetv1beta1.ManifestCondition) map[string]int {
	existingManifestConditionQIdx := make(map[string]int)
	for idx := range existingManifestConditions {
		manifestCond := existingManifestConditions[idx]

		wriStr, err := formatWRIString(&manifestCond.Identifier)
		if err != nil {
			// There might be manifest conditions without a valid identifier in the existing set of
			// manifest conditions (e.g., decoding error has occurred in the previous run).
			// Fleet will skip these manifest conditions. This is not considered as an error.
			continue
		}

		existingManifestConditionQIdx[wriStr] = idx
	}
	return existingManifestConditionQIdx
}

// prepareManifestCondForWriteAhead prepares a manifest condition for the write-ahead process.
func prepareManifestCondForWriteAhead(
	wriStr string, wri *fleetv1beta1.WorkResourceIdentifier,
	workGeneration int64,
	existingManifestCondQIdx map[string]int,
	existingManifestConds []fleetv1beta1.ManifestCondition,
) fleetv1beta1.ManifestCondition {
	// For each manifest to process, check if there is a corresponding entry in the existing set
	// of manifest conditions. If so, Fleet will port information back to keep track of the
	// previous processing results; otherwise, Fleet will report that it is preparing to process
	// the manifest.
	existingManifestConditionIdx, found := existingManifestCondQIdx[wriStr]
	if found {
		// The current manifest condition has a corresponding entry in the existing set of manifest
		// conditions.
		//
		// Fleet simply ports the information back.
		return existingManifestConds[existingManifestConditionIdx]
	}

	// No corresponding entry is found in the existing set of manifest conditions.
	//
	// Prepare a manifest condition that indicates that Fleet is preparing to be process the manifest.
	return fleetv1beta1.ManifestCondition{
		Identifier: *wri,
		Conditions: []metav1.Condition{
			{
				Type:               fleetv1beta1.WorkConditionTypeApplied,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: workGeneration,
				Reason:             ManifestAppliedCondPreparingToProcessReason,
				Message:            ManifestAppliedCondPreparingToProcessMessage,
				LastTransitionTime: metav1.Now(),
			},
		},
	}
}

// findLeftOverManifests returns the manifests that have been left over on the member cluster side.
func findLeftOverManifests(
	manifestCondsForWA []fleetv1beta1.ManifestCondition,
	existingManifestCondQIdx map[string]int,
	existingManifestConditions []fleetv1beta1.ManifestCondition,
) []fleetv1beta1.AppliedResourceMeta {
	// Build an index for quicker lookup in the newly prepared write-ahead manifest conditions.
	// Here Fleet uses the string representations as map keys to omit ordinals from any lookup.
	//
	// Note that before this step, Fleet has already filtered out duplicate manifests.
	manifestCondsForWAQIdx := make(map[string]int)
	for idx := range manifestCondsForWA {
		manifestCond := manifestCondsForWA[idx]

		wriStr, err := formatWRIString(&manifestCond.Identifier)
		if err != nil {
			// Normally this branch will never run as all manifests that cannot be decoded has been
			// skipped before this function is called. Here Fleet simply skips the manifest as it
			// has no effect on the process.
			klog.ErrorS(err, "failed to format the work resource identifier string", "manifest", manifestCond.Identifier)
			continue
		}

		manifestCondsForWAQIdx[wriStr] = idx
	}

	// For each manifest condition in the existing set of manifest conditions, check if
	// there is a corresponding entry in the set of manifest conditions prepared for the write-ahead
	// process. If not, Fleet will consider that the manifest has been left over on the member
	// cluster side and should be removed.

	// Use an AppliedResourceMeta slice to allow code sharing.
	leftOverManifests := []fleetv1beta1.AppliedResourceMeta{}
	for existingManifestWRIStr, existingManifestCondIdx := range existingManifestCondQIdx {
		_, found := manifestCondsForWAQIdx[existingManifestWRIStr]
		if !found {
			existingManifestCond := existingManifestConditions[existingManifestCondIdx]
			// The current manifest condition does not have a corresponding entry in the set of manifest
			// conditions prepared for the write-ahead process.

			// Verify if the manifest condition indicates that the manifest could have been
			// applied.
			applied := meta.FindStatusCondition(existingManifestCond.Conditions, fleetv1beta1.WorkConditionTypeApplied)
			if applied.Status == metav1.ConditionTrue || applied.Reason == ManifestAppliedCondPreparingToProcessReason {
				// Fleet assumes that the manifest has been applied if:
				// a) it has an applied condition set to the True status; or
				// b) it has an applied condition which signals that the object is preparing to be processed.
				//
				// Note that the manifest condition might not be up-to-date, so Fleet will not
				// check on the generation information.
				leftOverManifests = append(leftOverManifests, fleetv1beta1.AppliedResourceMeta{
					WorkResourceIdentifier: existingManifestCond.Identifier,
					// UID information might not be available at this moment; the cleanup process
					// will perform additional checks anyway to guard against the
					// create-delete-recreate cases and/or same name but different setup cases.
					//
					// As a side note, it is true that the AppliedWork object status might have
					// the UID information; Fleet cannot rely on that though, as the AppliedWork
					// status is not guaranteed to be tracking the result of the last apply op.
					// Should the Fleet agent restarts multiple times before it gets a chance to
					// write the AppliedWork object statys, the UID information in the status
					// might be several generations behind.
				})
			}
		}
	}
	return leftOverManifests
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

		// Remove the left-over manifest.
		err := r.removeOneLeftOverManifest(ctx, appliedManifestMeta, expectedAppliedWorkOwnerRef)
		if err != nil {
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
		wrappedErr := controller.NewAPIServerError(true, err)
		return fmt.Errorf("failed to retrieve the object from the member cluster (gvr=%+v, manifestObj=%+v): %w", gvr, klog.KRef(manifestNamespace, manifestName), wrappedErr)
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
			wrappedErr := controller.NewAPIServerError(false, err)
			return fmt.Errorf("failed to drop the ownership of the object (gvr=%+v, manifestObj=%+v, inMemberClusterObj=%+v, expectedAppliedWorkOwnerRef=%+v): %w",
				gvr, klog.KRef(manifestNamespace, manifestName), klog.KObj(inMemberClusterObj), *expectedAppliedWorkOwnerRef, wrappedErr)
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
			wrappedErr := controller.NewAPIServerError(false, err)
			return fmt.Errorf("failed to delete the object (gvr=%+v, manifestObj=%+v, inMemberClusterObj=%+v, expectedAppliedWorkOwnerRef=%+v): %w",
				gvr, klog.KRef(manifestNamespace, manifestName), klog.KObj(inMemberClusterObj), *expectedAppliedWorkOwnerRef, wrappedErr)
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
		if reflect.DeepEqual(curOwners[idx], *expectedAppliedWorkOwnerRef) {
			return true
		}
	}
	return false
}

// removeOwnerRef removes the given owner reference from the object.
func removeOwnerRef(obj *unstructured.Unstructured, expectedAppliedWorkOwnerRef *metav1.OwnerReference) {
	ownerRefs := obj.GetOwnerReferences()
	updatedOwnerRefs := make([]metav1.OwnerReference, 0, len(ownerRefs))

	// Re-build the owner references; remove the given one from the list.
	for idx := range ownerRefs {
		if !reflect.DeepEqual(ownerRefs[idx], *expectedAppliedWorkOwnerRef) {
			updatedOwnerRefs = append(updatedOwnerRefs, ownerRefs[idx])
		}
	}
	obj.SetOwnerReferences(updatedOwnerRefs)
}
