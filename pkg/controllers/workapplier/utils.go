/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package work

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/wI2L/jsondiff"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/resource"
)

const (
	// A list of condition related values.
	ManifestAppliedCondPreparingToProcessReason  = "PreparingToProcess"
	ManifestAppliedCondPreparingToProcessMessage = "The manifest is being prepared for processing."

	ManifestAvailableCondNotYetAppliedReason  = "NotYetApplied"
	ManifestAvailableCondNotYetAppliedMessage = "The manifest has not been applied yet."
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
	inMemberClusterObj *unstructured.Unstructured,
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
		identifier.GenerateName = manifestObj.GetGenerateName()
	}

	// Set the GVR information if the manifest object can be REST mapped.
	if gvr != nil {
		identifier.Resource = gvr.Resource
	}

	// Normally, the names between the manifest and the live object would always match. However, for
	// manifest objects with generated names, its name can only be resolved after the object is
	// applied; in this case, Fleet will set the name field using the information from the live
	// object.
	if inMemberClusterObj != nil {
		identifier.Name = inMemberClusterObj.GetName()
	}

	return identifier
}

// formatWRIString returns a string representation of a work resource identifier.
func formatWRIString(wri *fleetv1beta1.WorkResourceIdentifier) (string, error) {
	switch {
	case wri.Group == "":
		// The manifest object cannot be decoded, i.e., it can only be identfied by its ordinal.
		//
		// This branch is added solely for completeness reasons; normally such objects would not
		// be included in any cases that would require a WRI string formatting.
		return "", fmt.Errorf("the manifest object can only be identified by its ordinal")
	case wri.GenerateName != "":
		// For a resource with generated name, the string representation includes the generate name
		// instead of the actual name.
		return fmt.Sprintf("GV=%s/%s, Kind=%s, Namespace=%s, GenerateName=%s",
			wri.Group, wri.Version, wri.Kind, wri.Namespace, wri.GenerateName), nil
	default:
		// For a regular object, the string representation includes the actual name.
		return fmt.Sprintf("Ordinal=%d, GV=%s/%s, Kind=%s, Namespace=%s, Name=%s",
			wri.Ordinal, wri.Group, wri.Version, wri.Kind, wri.Namespace, wri.Name), nil
	}
}

// appendManifestConditionPreparingToProcess appends a manifest's Applied condition
// to the set of manifest conditions, and marks it as preparing to process.
func appendManifestConditionPreparingToProcess(
	manifestConds []fleetv1beta1.ManifestCondition,
	wri *fleetv1beta1.WorkResourceIdentifier,
	workGeneration int64,
) []fleetv1beta1.ManifestCondition {
	appliedCond := metav1.Condition{
		Type:               fleetv1beta1.WorkConditionTypeApplied,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: workGeneration,
		Reason:             ManifestAppliedCondPreparingToProcessReason,
		Message:            ManifestAppliedCondPreparingToProcessMessage,
	}

	return append(manifestConds, fleetv1beta1.ManifestCondition{
		Identifier: *wri,
		Conditions: []metav1.Condition{appliedCond},
	})
}

// isLiveObjectDerivedFromManifestObjWithGenerateName checks if a live object is connected to a
// manifest object with a generated name.
//
// This is a sanity check that make sures:
// * the live object's name is derived from the manifest object's generate name; and
// * the live object is owned by Fleet (specifically the expected AppliedWork object).
func isInMemberClusterObjectDerivedFromManifestObjWithGenerateName(inMemberClusterObj *unstructured.Unstructured, expectedGenerateName string, expectedAppliedWorkOwnerRef *metav1.OwnerReference) bool {
	// Do a sanity check.
	if inMemberClusterObj == nil {
		return false
	}

	if !strings.HasPrefix(inMemberClusterObj.GetName(), expectedGenerateName) {
		// The live object's name is not derived from the manifest object's generate name.
		return false
	}

	// Verify if the owner reference still stands.
	return isInMemberClusterObjectDerivedFromManifestObj(inMemberClusterObj, expectedAppliedWorkOwnerRef)
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

// shouldInitiateTakeOverAttempt checks if Fleet should initate the takeover process for an object.
//
// A takeover process is initiated when:
//   - An object that matches with the given manifest has been created; but
//   - The object is not owned by Fleet (more specifically, the object is not owned by the
//     expected AppliedWork object).
func shouldInitiateTakeOverAttempt(manifestObj, inMemberClusterObj *unstructured.Unstructured, expectedAppliedWorkOwnerRef *metav1.OwnerReference) bool {
	if inMemberClusterObj == nil {
		// Perform a sanity check. If the corresponding live object is not found, no takeover is
		// needed.
		return false
	}

	if manifestObj.GetGenerateName() != "" {
		// For obvious reasons, no takeover is ever needed for objects with generated names.
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

func isPlacedByFleetInDuplicate(ownerRefs []metav1.OwnerReference, expectedAppliedWorkOwnerRef *metav1.OwnerReference) bool {
	for idx := range ownerRefs {
		ownerRef := ownerRefs[idx]
		if ownerRef.APIVersion == fleetv1beta1.GroupVersion.String() && ownerRef.Kind == fleetv1beta1.AppliedWorkKind && string(ownerRef.UID) != string(expectedAppliedWorkOwnerRef.UID) {
			return true
		}
	}
	return false
}

func fullDiffBetweenManifestAndInMemberClusterObjects(
	manifestObj, inMemberClusterObj *unstructured.Unstructured,
) ([]fleetv1beta1.PatchDetail, error) {
	// Fleet calculates the full diff between two objects by directly comparing their JSON
	// representations.

	// Discard certain fields from both objects before comparison.
	manifestObjCopy := discardFieldsIrrelevantInComparisonFrom(manifestObj)
	inMemberClusterObjCopy := discardFieldsIrrelevantInComparisonFrom(inMemberClusterObj)

	// Marshal the objects into JSON.
	manifestObjJSONBytes, err := manifestObjCopy.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the manifest object into JSON: %w", err)
	}
	inMemberClusterObjJSONBytes, err := inMemberClusterObjCopy.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the object from the member cluster into JSON: %w", err)
	}

	// Compare the JSON representations.
	patch, err := jsondiff.CompareJSON(manifestObjJSONBytes, inMemberClusterObjJSONBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to compare the JSON representations of the manifest object and the object from the member cluster: %w", err)
	}

	details, err := organizeJSONPatchIntoFleetPatchDetails(patch, manifestObjJSONBytes, inMemberClusterObjJSONBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to organize JSON patch operations into Fleet patch details: %w", err)
	}
	return details, nil
}

// discardFieldsIrrelevantInComparisonFrom discards fields that are irrelevant when comparing
// the manifest and live objects (or two manifest objects).
//
// Note that this method will return an object copy; the original object will be left untouched.
func discardFieldsIrrelevantInComparisonFrom(obj *unstructured.Unstructured) *unstructured.Unstructured {
	// Create a deep copy of the object.
	objCopy := obj.DeepCopy()

	// Remove object meta fields that are irrelevant in comparison.

	// Clear out the object's name. This is necessary because an object with a generated name will
	// have a blank name in the manifest object, but a non-blank name in the live object. In such
	// cases, the 1:1 link between the two objects is established in the object retrieval
	// step. For regular objects, the names will always be same between the manifest and
	// the live object, as guaranteed by the object retrieval step earlier as well, so a comparison
	// on the name field is unnecessary anyway.
	objCopy.SetName("")

	// Clear out the object's generate name. This is also a field that is irrelevant in comparison.
	objCopy.SetGenerateName("")

	// Remove certain labels and annoations. At this moment Fleet only removes annotations that
	// concerns itself.
	//
	// TO-DO (chenyu1): evaluate if there are additional Kubernetes well-known labels and annotations
	// that needs to be removed in this process.
	if annotations := objCopy.GetAnnotations(); annotations != nil {
		delete(annotations, fleetv1beta1.ManifestHashAnnotation)
		delete(annotations, fleetv1beta1.LastAppliedConfigAnnotation)
		if len(annotations) == 0 {
			objCopy.SetAnnotations(nil)
		} else {
			objCopy.SetAnnotations(annotations)
		}
	}

	// Fields below are system-reserved fields in object meta. Technically speaking they can be
	// set in the manifests, but this is a very uncommon practice, and currently Fleet will clear
	// these fields (except for the finalizers) before applying the manifests].
	// As a result, for now Fleet will ignore them in the comparison process as well.
	//
	// TO-DO (chenyu1): evaluate if this is a correct assumption for most (if not all) Fleet
	// users.
	objCopy.SetFinalizers([]string{})
	objCopy.SetManagedFields([]metav1.ManagedFieldsEntry{})
	objCopy.SetOwnerReferences([]metav1.OwnerReference{})

	// Fields below are read-only fields in object meta. Fleet will ignore them in the comparison
	// process.
	objCopy.SetCreationTimestamp(metav1.Time{})
	// Deleted objects are handled separately in the apply process; for comparison purposes,
	// Fleet will ignore the deletion timestamp and grace period seconds.
	objCopy.SetDeletionTimestamp(nil)
	objCopy.SetDeletionGracePeriodSeconds(nil)
	objCopy.SetGeneration(0)
	objCopy.SetResourceVersion("")
	objCopy.SetSelfLink("")
	objCopy.SetUID("")

	// Remove the status field.
	unstructured.RemoveNestedField(objCopy.Object, "status")

	return objCopy
}

func organizeJSONPatchIntoFleetPatchDetails(patch jsondiff.Patch, manifestObjJSONBytes, inMemberClusterObjJSONBytes []byte) ([]fleetv1beta1.PatchDetail, error) {
	// Pre-allocate the slice for the patch details. The organization procedure typically will yield
	// the same number of PatchDetail items as the JSON patch operations.
	details := make([]fleetv1beta1.PatchDetail, len(patch))

	// Prepare the string form to save some overhead.
	manifestObjJSONStr := string(manifestObjJSONBytes)
	inMemberClusterObjJSONStr := string(inMemberClusterObjJSONBytes)

	// A side note: here Fleet takes an expedient approach processing null JSON paths, by treating
	// null paths as empty strings.

	// Process only the first 100 ops.
	// TO-DO (chenyu1): Impose additional size limits.
	for idx := 0; idx < len(patch) && idx < patchDetailPerObjLimit; idx++ {
		op := patch[idx]
		switch op.Type {
		case jsondiff.OperationAdd:
			details = append(details, fleetv1beta1.PatchDetail{
				Path:          op.Path,
				ValueInMember: fmt.Sprint(op.Value),
			})
		case jsondiff.OperationRemove:
			// Fleet here skips validation as the JSON data is just marshalled.
			hubValue := gjson.Get(manifestObjJSONStr, op.Path)
			details = append(details, fleetv1beta1.PatchDetail{
				Path:       op.Path,
				ValueInHub: hubValue.Str,
			})
		case jsondiff.OperationReplace:
			// Fleet here skips validation as the JSON data is just marshalled.
			hubValue := gjson.Get(manifestObjJSONStr, op.Path)
			details = append(details, fleetv1beta1.PatchDetail{
				Path:          op.Path,
				ValueInMember: fmt.Sprint(op.Value),
				ValueInHub:    hubValue.Str,
			})
		case jsondiff.OperationMove:
			// Normally the Move operation will not be returned as factorization is disabled
			// for the JSON patch calculation process; however, Fleet here still processes them
			// just in case.
			//
			// Each Move operation will be parsed into two separate operations.
			hubValue := gjson.Get(manifestObjJSONStr, op.Path)
			details = append(details, fleetv1beta1.PatchDetail{
				Path:       op.From,
				ValueInHub: hubValue.Str,
			})

			details = append(details, fleetv1beta1.PatchDetail{
				Path:          op.Path,
				ValueInMember: hubValue.Str,
			})
		case jsondiff.OperationCopy:
			// Normally the Copy operation will not be returned as factorization is disabled
			// for the JSON patch calculation process; however, Fleet here still processes them
			// just in case.
			//
			// Each Copy operation will be parsed into a Replace operation.
			memberValue := gjson.Get(inMemberClusterObjJSONStr, op.Path)
			details = append(details, fleetv1beta1.PatchDetail{
				Path:          op.Path,
				ValueInMember: memberValue.Str,
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

func setFleetLastAppliedAnnotation(manifestObj *unstructured.Unstructured) (bool, error) {
	annotations := manifestObj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Remove the last applied annotation just in case.
	delete(annotations, fleetv1beta1.LastAppliedConfigAnnotation)
	lastAppliedManifestJSONBytes, err := manifestObj.MarshalJSON()
	if err != nil {
		return false, fmt.Errorf("failed to marshal the manifest object into JSON: %w", err)
	}
	annotations[fleetv1beta1.LastAppliedConfigAnnotation] = string(lastAppliedManifestJSONBytes)
	isLastAppliedAnnoationSet := true

	if err := validation.ValidateAnnotationsSize(annotations); err != nil {
		// If the annotation size exceeds the limit, Fleet will set the annotation to an empty string.
		annotations[fleetv1beta1.LastAppliedConfigAnnotation] = ""
		isLastAppliedAnnoationSet = false
	}

	manifestObj.SetAnnotations(annotations)
	return isLastAppliedAnnoationSet, nil
}

func getFleetLastAppliedAnnotation(inMemberClusterObj *unstructured.Unstructured) []byte {
	annotations := inMemberClusterObj.GetAnnotations()
	if annotations == nil {
		// The last applied annotation is not found in the live object; normally this should not
		// happen, but Fleet can still handle this situation.
		klog.Warningf("no annotations in the live object %s/%s", inMemberClusterObj.GroupVersionKind(), klog.KObj(inMemberClusterObj))
		return nil
	}

	lastAppliedManifestJSONStr, found := annotations[fleetv1beta1.LastAppliedConfigAnnotation]
	if !found {
		// The last applied annotation is not found in the live object; normally this should not
		// happen, but Fleet can still handle this situation.
		klog.Warningf("the last applied annotation is not found in the live object %s/%s", inMemberClusterObj.GroupVersionKind(), klog.KObj(inMemberClusterObj))
		return nil
	}

	return []byte(lastAppliedManifestJSONStr)
}

func setOwnerRef(obj *unstructured.Unstructured, expectedAppliedWorkOwnerRef *metav1.OwnerReference) {
	ownerRefs := obj.GetOwnerReferences()
	if ownerRefs == nil {
		ownerRefs = []metav1.OwnerReference{}
	}
	// Typically owner references is a system-managed field, and at this moment Fleet will
	// clear owner references (if any) set in the manifest object. However, for consistency
	// reasons, here Fleet will still assume that there might be some owner references set
	// in the manifest object.
	ownerRefs = append(ownerRefs, *expectedAppliedWorkOwnerRef)
	obj.SetOwnerReferences(ownerRefs)
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

func validateOwnerReferences(
	manifestObj, inMemberClusterObj *unstructured.Unstructured,
	applyStrategy *fleetv1beta1.ApplyStrategy,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) error {
	manifestObjOwnerRefs := manifestObj.GetOwnerReferences()
	inMemberClusterObjOwnerRefs := inMemberClusterObj.GetOwnerReferences()

	// If the manifest object already features some owner reference(s), but co-ownership is
	// disallowed, the validation fails.
	if len(manifestObjOwnerRefs) > 0 && !applyStrategy.AllowCoOwnership {
		return fmt.Errorf("manifest is set to have multiple owner references but co-ownership is disallowed")
	}

	// Do a sanity check to verify that no AppliedWork object is directly added as an owner
	// in the manifest object.
	for _, ownerRef := range manifestObjOwnerRefs {
		if ownerRef.APIVersion == fleetv1beta1.GroupVersion.String() && ownerRef.Kind == fleetv1beta1.AppliedWorkKind {
			return fmt.Errorf("an AppliedWork object is unexpectedly added as an owner in the manifest object")
		}
	}

	// If the live object is co-owned but co-ownership is no longer allowed, the validation fails.
	if len(inMemberClusterObjOwnerRefs) > 1 && !applyStrategy.AllowCoOwnership {
		return fmt.Errorf("object is co-owned by multiple objects but co-ownership has been disallowed")
	}

	// Note that at this point of execution, one of the owner references is guaranteed to be the
	// expected AppliedWork object. For safety reasons, Fleet will still do a sanity check.
	found := false
	for _, ownerRef := range inMemberClusterObjOwnerRefs {
		if reflect.DeepEqual(ownerRef, *expectedAppliedWorkOwnerRef) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("object is not owned by the expected AppliedWork object")
	}

	// If the object is already owned by another AppliedWork object, the validation fails.
	//
	// Normally this branch will never get executed as Fleet would refuse to take over an object
	// that has been owned by another AppliedWork object.
	if isPlacedByFleetInDuplicate(inMemberClusterObjOwnerRefs, expectedAppliedWorkOwnerRef) {
		return fmt.Errorf("object is already owned by another AppliedWork object")
	}

	return nil
}

func shouldApply(manifestObj, inMemberClusterObj *unstructured.Unstructured) bool {
	// Fleet will apply the manifest if (and only if) a newer version of the manifest
	// is available.
	return manifestObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation] != inMemberClusterObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation]
}

// prepareObjectWithGenerateName prepares an object with a generated name for an apply op.
//
// Specifically, Fleet will set the manifest object to have the same name as the live object. For
// regular objects, this is a no-op.
func prepareObjectWithGenerateName(manifestObj, inMemberClusterObj *unstructured.Unstructured) {
	if manifestObj.GetGenerateName() != "" {
		manifestObj.SetGenerateName("")
		manifestObj.SetName(inMemberClusterObj.GetName())
	}
}

// sanitizeManifestObject sanitizes the manifest object before applying it.
//
// The sanitization logic here is consistent with that of the CRP controller, sans the API server
// specific parts; see also the generateRawContent function in the respective controller.
//
// Note that this function returns a copy of the manifest object; the original object will be left
// untouched.
func sanitizeManifestObject(manifestObj *unstructured.Unstructured) *unstructured.Unstructured {
	// Create a deep copy of the object.
	manifestObjCopy := manifestObj.DeepCopy()

	// Remove certain labels and annotations.
	if annotations := manifestObjCopy.GetAnnotations(); annotations != nil {
		// Remove the two Fleet reserved annotations. This is normally not set by users.
		delete(annotations, fleetv1beta1.ManifestHashAnnotation)
		delete(annotations, fleetv1beta1.LastAppliedConfigAnnotation)

		// Remove the last applied configuration set by kubectl.
		delete(annotations, corev1.LastAppliedConfigAnnotation)
		if len(annotations) == 0 {
			manifestObjCopy.SetAnnotations(nil)
		} else {
			manifestObjCopy.SetAnnotations(annotations)
		}
	}

	// Remove certain system-managed fields.
	manifestObjCopy.SetOwnerReferences(nil)
	manifestObjCopy.SetManagedFields(nil)

	// Remove the read-only fields.
	manifestObjCopy.SetCreationTimestamp(metav1.Time{})
	manifestObjCopy.SetDeletionTimestamp(nil)
	manifestObjCopy.SetDeletionGracePeriodSeconds(nil)
	manifestObjCopy.SetGeneration(0)
	manifestObjCopy.SetResourceVersion("")
	manifestObjCopy.SetSelfLink("")
	manifestObjCopy.SetUID("")

	// Remove the status field.
	unstructured.RemoveNestedField(manifestObjCopy.Object, "status")

	// Note: in the Fleet hub agent logic, the system also handles the Service and Job objects
	// in a special way, so as to remove certain fields that are set by the hub cluster API
	// server automatically; for the Fleet member agent logic here, however, Fleet assumes
	// that if these fields are set, users must have set them on purpose, and they should not
	// be removed. The difference comes to the fact that the Fleet member agent sanitization
	// logic concerns only the enveloped objects, which are free from any hub cluster API
	// server manipulation anyway.

	return manifestObjCopy
}

// shouldEnableOptimisticLock checks if optimistic lock should be enabled given an apply strategy.
func shouldEnableOptimisticLock(applyStrategy *fleetv1beta1.ApplyStrategy) bool {
	// Optimistic lock is enabled if the apply strategy is set to IfNotDrifted.
	return applyStrategy.WhenToApply == fleetv1beta1.WhenToApplyTypeIfNotDrifted
}

// shouldPerformPreApplyDriftDetection checks if pre-apply drift detection should be performed.
func shouldPerformPreApplyDriftDetection(manifestObj, inMemberClusterObj *unstructured.Unstructured, applyStrategy *fleetv1beta1.ApplyStrategy) (bool, error) {
	// Drift detection is performed before the apply op if (and only if):
	// * The apply strategy dictates that an apply op should only run if there is no
	//   detected drift; and
	// * The hash of the manifest object is consistently with that bookkept in the live object
	//   annotations (i.e., the manifest object has been applied before).
	if applyStrategy.WhenToApply != fleetv1beta1.WhenToApplyTypeIfNotDrifted {
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

// shouldPerformPostApplyDriftDetection checks if post-apply drift detection should be performed.
func shouldPerformPostApplyDriftDetection(applyStrategy *fleetv1beta1.ApplyStrategy) bool {
	// Post-apply drift detection is performed if (and only if):
	// * The apply strategy dictates that drift detection should run in full comparison mode.
	return applyStrategy.ComparisonOption == fleetv1beta1.ComparisonOptionTypeFullComparison
}

// isManifestObjectApplied returns if an applied result type indicates that a manifest
// object in a bundle has been successfully applied.
func isManifestObjectApplied(appliedResTyp manifestProcessingAppliedResultType) bool {
	return appliedResTyp == ManifestProcessingApplyResultTypeApplied || appliedResTyp == ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection
}

// isManifestObjectAvailable returns if an availability result type indicates that a manifest
// object in a bundle is available.
func isAppliedObjectAvailable(availabilityResTyp ManifestProcessingAvailabilityResultType) bool {
	return availabilityResTyp == ManifestProcessingAvailabilityResultTypeAvailable || availabilityResTyp == ManifestProcessingAvailabilityResultTypeNotTrackable
}

// setManifestAppliedCondition sets the applied condition for a specific manifest.
func setManifestAppliedCondition(
	manifestCond *fleetv1beta1.ManifestCondition,
	appliedResTyp manifestProcessingAppliedResultType,
	applyError error,
	workGeneration int64,
) {
	var appliedCond *metav1.Condition
	switch {
	case appliedResTyp == ManifestProcessingApplyResultTypeApplied:
		// The manifest has been successfully applied.
		appliedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Status:             metav1.ConditionTrue,
			Reason:             string(ManifestProcessingApplyResultTypeApplied),
			Message:            ManifestProcessingApplyResultTypeAppliedDescription,
			ObservedGeneration: workGeneration,
		}
	case appliedResTyp == ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection:
		// The manifest has been successfully applied, but drift detection has failed.
		//
		// At this moment Fleet does not prepare a dedicated condition for drift detection
		// outcomes.
		appliedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Status:             metav1.ConditionTrue,
			Reason:             string(ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection),
			Message:            ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetectionDescription,
			ObservedGeneration: workGeneration,
		}
	default:
		// The apply op fails.
		appliedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Status:             metav1.ConditionFalse,
			Reason:             string(appliedResTyp),
			Message:            fmt.Sprintf("Failed to applied the manifest (error: %s)", applyError),
			ObservedGeneration: workGeneration,
		}
	}

	meta.SetStatusCondition(&manifestCond.Conditions, *appliedCond)
}

func setManifestAvailableCondition(
	manifestCond *fleetv1beta1.ManifestCondition,
	availabilityResTyp ManifestProcessingAvailabilityResultType,
	availabilityError error,
	workGeneration int64,
) {
	var availableCond *metav1.Condition
	switch {
	case availabilityResTyp == ManifestProcessingAvailabilityResultTypeSkipped:
		// Availability check has been skipped for the manifest as it has not been applied yet.
		//
		// In this case, no availability condition is set.
	case availabilityResTyp == ManifestProcessingAvailabilityResultTypeFailed:
		// Availability check has failed.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             string(ManifestProcessingAvailabilityResultTypeFailed),
			Message:            fmt.Sprintf(ManifestProcessingAvailabilityResultTypeFailedDescription, availabilityError),
			ObservedGeneration: workGeneration,
		}
	case availabilityResTyp == ManifestProcessingAvailabilityResultTypeNotYetAvailable:
		// The manifest is not yet available.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             string(ManifestProcessingAvailabilityResultTypeNotYetAvailable),
			Message:            ManifestProcessingAvailabilityResultTypeNotYetAvailableDescription,
			ObservedGeneration: workGeneration,
		}
	case availabilityResTyp == ManifestProcessingAvailabilityResultTypeNotTrackable:
		// Fleet cannot track the availability of the manifest.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             string(ManifestProcessingAvailabilityResultTypeNotTrackable),
			Message:            ManifestProcessingAvailabilityResultTypeNotTrackableDescription,
			ObservedGeneration: workGeneration,
		}
	default:
		// The manifest is available.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             string(ManifestProcessingAvailabilityResultTypeAvailable),
			Message:            ManifestProcessingAvailabilityResultTypeAvailableDescription,
			ObservedGeneration: workGeneration,
		}
	}

	if availableCond != nil {
		meta.SetStatusCondition(&manifestCond.Conditions, *availableCond)
	}
}

// setWorkAppliedCondition sets the applied condition for a Work object.
//
// A work object is considered to be applied if all of its manifests have been successfully applied.
func setWorkAppliedCondition(
	workStatusConditions *[]metav1.Condition,
	manifestCount, appliedManifestCount int,
	workGeneration int64,
) {
	var appliedCond *metav1.Condition
	switch {
	case appliedManifestCount == manifestCount:
		// All manifests have been successfully applied.
		appliedCond = &metav1.Condition{
			Type:   fleetv1beta1.WorkConditionTypeApplied,
			Status: metav1.ConditionTrue,
			// Here Fleet re-uses the same reason for individual manifests.
			Reason:             string(ManifestProcessingApplyResultTypeApplied),
			Message:            allManifestsAppliedMessage,
			ObservedGeneration: workGeneration,
		}
	default:
		// Not all manifests have been successfully applied.
		appliedCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeApplied,
			Status:             metav1.ConditionFalse,
			Reason:             notAllManifestsAppliedReason,
			Message:            fmt.Sprintf(notAllManifestsAppliedMessage, appliedManifestCount, manifestCount),
			ObservedGeneration: workGeneration,
		}
	}
	meta.SetStatusCondition(workStatusConditions, *appliedCond)
}

// setWorkAvailableCondition sets the available condition for a Work object.
func setWorkAvailableCondition(
	workStatusConditions *[]metav1.Condition,
	manifestCount, availableManifestCount int,
	workGeneration int64,
) {
	var availableCond *metav1.Condition
	switch {
	case availableManifestCount == manifestCount:
		// All manifests are available.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             string(ManifestProcessingAvailabilityResultTypeAvailable),
			Message:            allAppliedObjectAvailableMessage,
			ObservedGeneration: workGeneration,
		}
	default:
		// Not all manifests are available.
		availableCond = &metav1.Condition{
			Type:               fleetv1beta1.WorkConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             notAllAppliedObjectsAvailableReason,
			Message:            fmt.Sprintf(notAllAppliedObjectsAvailableMessage, availableManifestCount, manifestCount),
			ObservedGeneration: workGeneration,
		}
	}
	meta.SetStatusCondition(workStatusConditions, *availableCond)
}

func crossReferenceManifestConditions(
	manifestCondsForWA []fleetv1beta1.ManifestCondition,
	existingManifestConditions []fleetv1beta1.ManifestCondition,
) []fleetv1beta1.AppliedResourceMeta {
	// Build an index for quicker lookup.
	//
	// Here Fleet also uses the string representations as map keys to omit ordinals from any lookup.
	manifestCondsForWAIdx := make(map[string]bool)
	for idx := range manifestCondsForWA {
		manifestCond := manifestCondsForWA[idx]

		wriStr, err := formatWRIString(&manifestCond.Identifier)
		if err != nil {
			// Normally this branch will never run as all manifests that cannot be decoded has been
			// skipped before this function is called. Here Fleet simply skips the manifest.
			klog.ErrorS(err, "failed to format the work resource identifier string", "manifest", manifestCond.Identifier)
			continue
		}

		manifestCondsForWAIdx[wriStr] = true
	}

	// Cross-reference the two set of manifest conditions. Filter out any manifest condition
	// that does not have a corresponding entry in the set of manifest conditions prepared
	// for the write-ahead process.

	// Use an AppliedResourceMeta slice to allow code sharing.
	leftOverManifests := []fleetv1beta1.AppliedResourceMeta{}
	for idx := range existingManifestConditions {
		manifestCond := existingManifestConditions[idx]

		wriStr, err := formatWRIString(&manifestCond.Identifier)
		if err != nil {
			// No special handling is needed for manifest conditions without a valid identifier
			// in the existing set of manifest conditions. They will be overwritten shortly.
			continue
		}

		if _, found := manifestCondsForWAIdx[wriStr]; !found {
			// The manifest condition does not have a corresponding entry in the set of manifest
			// conditions prepared for the write-ahead process.

			// Verify if the manifest condition indicates that the manifest could have been
			// applied.
			applied := meta.FindStatusCondition(manifestCond.Conditions, fleetv1beta1.WorkConditionTypeApplied)
			if applied.Status == metav1.ConditionTrue || applied.Reason == ManifestAppliedCondPreparingToProcessReason {
				// Fleet assumes that the manifest has been applied if:
				// a) it has an applied condition set to the True status; or
				// b) it has an applied condition which signals that the object is preparing to be processed.
				//
				// Note that the manifest condition might not be up-to-date.
				leftOverManifests = append(leftOverManifests, fleetv1beta1.AppliedResourceMeta{
					WorkResourceIdentifier: manifestCond.Identifier,
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

// isWorkObjectAvailable checks if a Work object is available.
func isWorkObjectAvailable(work *fleetv1beta1.Work) bool {
	availableCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
	return availableCond != nil && availableCond.Status == metav1.ConditionTrue
}
