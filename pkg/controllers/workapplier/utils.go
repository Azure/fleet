/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	// A list of condition related values.
	ManifestAppliedCondPreparingToProcessReason  = "PreparingToProcess"
	ManifestAppliedCondPreparingToProcessMessage = "The manifest is being prepared for processing."
)

func prepareManifestProcessingBundles(work *fleetv1beta1.Work) []*manifestProcessingBundle {
	// Pre-allocate the bundles.
	bundles := make([]*manifestProcessingBundle, 0, len(work.Spec.Workload.Manifests))
	for idx := range work.Spec.Workload.Manifests {
		manifest := work.Spec.Workload.Manifests[idx]
		bundles = append(bundles, &manifestProcessingBundle{
			manifest: &manifest,
		})
	}
	return bundles
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

// formatWRIString returns a string representation of a work resource identifier.
func formatWRIString(wri *fleetv1beta1.WorkResourceIdentifier) (string, error) {
	switch {
	case wri.Group == "" && wri.Version == "":
		// The manifest object cannot be decoded, i.e., it can only be identified by its ordinal.
		//
		// This branch is added solely for completeness reasons; normally such objects would not
		// be included in any cases that would require a WRI string formatting.
		return "", fmt.Errorf("the manifest object can only be identified by its ordinal")
	default:
		// For a regular object, the string representation includes the actual name.
		return fmt.Sprintf("GV=%s/%s, Kind=%s, Namespace=%s, Name=%s",
			wri.Group, wri.Version, wri.Kind, wri.Namespace, wri.Name), nil
	}
}

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

func prepareManifestCondForWA(
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
