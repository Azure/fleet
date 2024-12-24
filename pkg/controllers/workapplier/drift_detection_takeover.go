/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"context"
	"fmt"

	"github.com/qri-io/jsonpointer"
	"github.com/wI2L/jsondiff"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func (r *Reconciler) takeOverPreExistingObject(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObj, inMemberClusterObj *unstructured.Unstructured,
	applyStrategy *fleetv1beta1.ApplyStrategy,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) (*unstructured.Unstructured, []fleetv1beta1.PatchDetail, error) {
	inMemberClusterObjCopy := inMemberClusterObj.DeepCopy()
	existingOwnerRefs := inMemberClusterObjCopy.GetOwnerReferences()

	// At this moment Fleet is set to leave applied resources in the member cluster if the cluster
	// decides to leave its fleet; when this happens, the resources will be owned by an AppliedWork
	// object that might not have a corresponding Work object anymore as the cluster might have
	// been de-selected. If the cluster decides to re-join the fleet, and the same set of manifests
	// are being applied again, one may encounter unexpected errors due to the presence of the
	// left behind AppliedWork object. To address this issue, Fleet here will perform a cleanup,
	// removing any owner reference that points to an orphaned AppliedWork object.
	existingOwnerRefs, err := r.removeLeftBehindAppliedWorkOwnerRefs(ctx, existingOwnerRefs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to remove left-behind AppliedWork owner references: %w", err)
	}

	// Check this object is already owned by another object (or controller); if so, Fleet will only
	// add itself as an additional owner if co-ownership is allowed.
	if len(existingOwnerRefs) >= 1 && !applyStrategy.AllowCoOwnership {
		// The object is already owned by another object, and co-ownership is forbidden.
		// No takeover will be performed.
		//
		// Note that This will be registered as an (apply) error.
		return nil, nil, fmt.Errorf("the object is already owned by some other sources(s) and co-ownership is disallowed")
	}

	// Check if the object is already owned by Fleet, but the owner is a different AppliedWork
	// object, i.e., the object has been placed in duplicate.
	//
	// Originally Fleet does allow placing the same object multiple times; each placement
	// attempt might have its own object spec (envelopes might be used and in each envelope
	// the object looks different). This would lead to the object being constantly overwritten,
	// but no error would be raised on the user-end. With the drift detection feature, however,
	// this scenario would lead to constant flipping of the drift reporting, which could lead to
	// user confusion. To address this corner case, Fleet would now deny placing the same object
	// twice.
	if isPlacedByFleetInDuplicate(existingOwnerRefs, expectedAppliedWorkOwnerRef) {
		return nil, nil, fmt.Errorf("the object is already owned by another Fleet AppliedWork object")
	}

	// Check if the takeover action requires additional steps (configuration difference inspection).
	//
	// Note that the default takeover action is AlwaysApply.
	if applyStrategy.WhenToTakeOver == fleetv1beta1.WhenToTakeOverTypeIfNoDiff {
		configDiffs, err := r.diffBetweenManifestAndInMemberClusterObjects(ctx, gvr, manifestObj, inMemberClusterObjCopy, applyStrategy.ComparisonOption)
		switch {
		case err != nil:
			return nil, nil, fmt.Errorf("failed to calculate configuration diffs between the manifest object and the object from the member cluster: %w", err)
		case len(configDiffs) > 0:
			return nil, configDiffs, nil
		}
	}

	// Take over the object.
	updatedOwnerRefs := append(existingOwnerRefs, *expectedAppliedWorkOwnerRef)
	inMemberClusterObjCopy.SetOwnerReferences(updatedOwnerRefs)
	takenOverInMemberClusterObj, err := r.spokeDynamicClient.
		Resource(*gvr).Namespace(inMemberClusterObjCopy.GetNamespace()).
		Update(ctx, inMemberClusterObjCopy, metav1.UpdateOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to take over the object: %w", err)
	}

	return takenOverInMemberClusterObj, nil, nil
}

// diffBetweenManifestAndInMemberClusterObjects calculates the differences between the manifest object
// and its corresponding object in the member cluster.
func (r *Reconciler) diffBetweenManifestAndInMemberClusterObjects(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObj, inMemberClusterObj *unstructured.Unstructured,
	cmpOption fleetv1beta1.ComparisonOptionType,
) ([]fleetv1beta1.PatchDetail, error) {
	switch cmpOption {
	case fleetv1beta1.ComparisonOptionTypePartialComparison:
		return r.partialDiffBetweenManifestAndInMemberClusterObjects(ctx, gvr, manifestObj, inMemberClusterObj)
	case fleetv1beta1.ComparisonOptionTypeFullComparison:
		// For the full comparison, Fleet compares directly the JSON representations of the
		// manifest object and the object in the member cluster.
		return preparePatchDetails(manifestObj, inMemberClusterObj)
	default:
		return nil, fmt.Errorf("an invalid comparison option is specified")
	}
}

func (r *Reconciler) partialDiffBetweenManifestAndInMemberClusterObjects(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObj, inMemberClusterObj *unstructured.Unstructured,
) ([]fleetv1beta1.PatchDetail, error) {
	// Fleet calculates the partial diff between two objects by running apply ops in the dry-run
	// mode.
	appliedObj, err := r.applyInDryRunMode(ctx, gvr, manifestObj, inMemberClusterObj)
	if err != nil {
		return nil, fmt.Errorf("failed to apply the manifest in dry-run mode: %w", err)
	}

	// After the dry-run apply op, all the managed fields should have been overwritten using the
	// values from the manifest object, while leaving all the unmanaged fields untouched. This
	// would allow Fleet to compare the object returned by the dry-run apply op with the object
	// that is currently in the member cluster; if all the fields are consistent, it is safe
	// for us to assume that there are no drifts, otherwise, any fields that are different
	// imply that running an actual apply op would lead to unexpected changes, which signifies
	// the presence of partial drifts (drifts in managed fields).

	// Prepare the patch details.
	return preparePatchDetails(appliedObj, inMemberClusterObj)
}

func organizeJSONPatchIntoFleetPatchDetails(patch jsondiff.Patch, manifestObjMap, inMemberClusterObjMap map[string]interface{}) ([]fleetv1beta1.PatchDetail, error) {
	// Pre-allocate the slice for the patch details. The organization procedure typically will yield
	// the same number of PatchDetail items as the JSON patch operations.
	details := make([]fleetv1beta1.PatchDetail, 0, len(patch))

	// A side note: here Fleet takes an expedient approach processing null JSON paths, by treating
	// null paths as empty strings.

	// Process only the first 100 ops.
	// TO-DO (chenyu1): Impose additional size limits.
	for idx := 0; idx < len(patch) && idx < patchDetailPerObjLimit; idx++ {
		op := patch[idx]
		pathPtr, err := jsonpointer.Parse(op.Path)
		if err != nil {
			// An invalid path is found; normally this should not happen.
			return nil, fmt.Errorf("failed to parse the JSON path: %w", err)
		}

		switch op.Type {
		case jsondiff.OperationAdd:
			details = append(details, fleetv1beta1.PatchDetail{
				Path:          op.Path,
				ValueInMember: fmt.Sprint(op.Value),
			})
		case jsondiff.OperationRemove:
			// Fleet here skips validation as the JSON data is just marshalled.
			hubValue, err := pathPtr.Eval(manifestObjMap)
			if err != nil {
				return nil, fmt.Errorf("failed to evaluate the JSON path %s in the manifest object: %w", op.Path, err)
			}
			details = append(details, fleetv1beta1.PatchDetail{
				Path:       op.Path,
				ValueInHub: fmt.Sprintf("%v", hubValue),
			})
		case jsondiff.OperationReplace:
			// Fleet here skips validation as the JSON data is just marshalled.
			hubValue, err := pathPtr.Eval(manifestObjMap)
			if err != nil {
				return nil, fmt.Errorf("failed to evaluate the JSON path %s in the manifest object: %w", op.Path, err)
			}
			details = append(details, fleetv1beta1.PatchDetail{
				Path:          op.Path,
				ValueInMember: fmt.Sprint(op.Value),
				ValueInHub:    fmt.Sprintf("%v", hubValue),
			})
		case jsondiff.OperationMove:
			// Normally the Move operation will not be returned as factorization is disabled
			// for the JSON patch calculation process; however, Fleet here still processes them
			// just in case.
			//
			// Each Move operation will be parsed into two separate operations.
			hubValue, err := pathPtr.Eval(manifestObjMap)
			if err != nil {
				return nil, fmt.Errorf("failed to evaluate the JSON path %s in the manifest object: %w", op.Path, err)
			}
			details = append(details, fleetv1beta1.PatchDetail{
				Path:       op.From,
				ValueInHub: fmt.Sprintf("%v", hubValue),
			})

			details = append(details, fleetv1beta1.PatchDetail{
				Path:          op.Path,
				ValueInMember: fmt.Sprintf("%v", hubValue),
			})
		case jsondiff.OperationCopy:
			// Normally the Copy operation will not be returned as factorization is disabled
			// for the JSON patch calculation process; however, Fleet here still processes them
			// just in case.
			//
			// Each Copy operation will be parsed into a Replace operation.
			memberValue, err := pathPtr.Eval(inMemberClusterObjMap)
			if err != nil {
				return nil, fmt.Errorf("failed to evaluate the JSON path %s in the applied object: %w", op.Path, err)
			}
			details = append(details, fleetv1beta1.PatchDetail{
				Path:          op.Path,
				ValueInMember: fmt.Sprintf("%v", memberValue),
			})
		case jsondiff.OperationTest:
			// The Test op is a no-op in Fleet's use case. Normally it will not be returned, either.
		default:
			// An unexpected op is returned.
			return nil, fmt.Errorf("an unexpected JSON patch operation is returned (%s)", op)
		}
	}

	return details, nil
}

func preparePatchDetails(srcObj, destObj *unstructured.Unstructured) ([]fleetv1beta1.PatchDetail, error) {
	// Discard certain fields from both objects before comparison.
	srcObjCopy := discardFieldsIrrelevantInComparisonFrom(srcObj)
	destObjCopy := discardFieldsIrrelevantInComparisonFrom(destObj)

	// Marshal the objects into JSON.
	srcObjJSONBytes, err := srcObjCopy.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the source object into JSON: %w", err)
	}

	destObjJSONBytes, err := destObjCopy.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the destination object into JSON: %w", err)
	}

	// Compare the JSON representations.
	patch, err := jsondiff.CompareJSON(srcObjJSONBytes, destObjJSONBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to compare the JSON representations of the source and destination objects: %w", err)
	}

	details, err := organizeJSONPatchIntoFleetPatchDetails(patch, srcObjCopy.Object, destObjCopy.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to organize JSON patch operations into Fleet patch details: %w", err)
	}
	return details, nil
}

func (r *Reconciler) removeLeftBehindAppliedWorkOwnerRefs(ctx context.Context, ownerRefs []metav1.OwnerReference) ([]metav1.OwnerReference, error) {
	updatedOwnerRefs := make([]metav1.OwnerReference, 0, len(ownerRefs))
	for idx := range ownerRefs {
		ownerRef := ownerRefs[idx]
		if ownerRef.APIVersion != fleetv1beta1.GroupVersion.String() || ownerRef.Kind != fleetv1beta1.AppliedWorkKind {
			// Skip non AppliedWork owner references.
			updatedOwnerRefs = append(updatedOwnerRefs, ownerRef)
			continue
		}

		// Check if the AppliedWork object has a corresponding Work object.
		workObj := &fleetv1beta1.Work{}
		err := r.hubClient.Get(ctx, types.NamespacedName{Namespace: r.workNameSpace, Name: ownerRef.Name}, workObj)
		switch {
		case err != nil && !errors.IsNotFound(err):
			// An unexpected error occurred.
			return nil, fmt.Errorf("failed to get the Work object: %w", err)
		case err == nil && workObj.UID == ownerRef.UID:
			// The AppliedWork owner reference is valid; no need for removal.
			updatedOwnerRefs = append(updatedOwnerRefs, ownerRef)
			continue
		default:
			// The AppliedWork owner reference is invalid; either the Work object does not exist or
			// the UIDs do not match. Remove the owner reference.
			klog.V(2).InfoS("Found an owner reference that points to an orphaned AppliedWork object", "ownerRef", ownerRef)
			continue
		}
	}

	return updatedOwnerRefs, nil
}
