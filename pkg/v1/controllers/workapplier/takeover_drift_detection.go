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
	"strings"

	"github.com/qri-io/jsonpointer"
	"github.com/wI2L/jsondiff"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kfioplacementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/ownerreferences"
)

const (
	patchDetailPerObjLimit = 100
)

const (
	k8sReservedLabelAnnotationFullDomain   = "kubernetes.io/"
	k8sReservedLabelAnnotationAbbrDomain   = "k8s.io/"
	kubefleetReservedLabelAnnotationDomain = "kubefleet.dev/"
)

// isOwnedByOlderKubeFleetAPIObjects checks if the object in the member cluster is owned by older KubeFleet API
// objects, specifically the appliedWork objects in the `placement.kubernetes-fleet.io` API group.
//
// This is added as a safeguard to avoid scenarios where a manifest is applied via both the newer PlacementPolicy
// APIs and the older ResourcePlacement APIs. And the safeguard always runs regardless of the sync strategy in use.
func isOwnedByOlderKubeFleetAPIObjects(inMemberClusterObj *unstructured.Unstructured) bool {
	if inMemberClusterObj == nil {
		return false
	}

	curOwners := inMemberClusterObj.GetOwnerReferences()
	for idx := range curOwners {
		owner := &curOwners[idx]
		if owner.APIVersion == kfioplacementv1beta1.GroupVersion.String() && owner.Kind == "AppliedWork" {
			return true
		}
	}
	return false
}

// shouldInitiateTakeOverAttempt checks if KubeFleet should initiate the takeover process for an object.
//
// A takeover process is initiated when:
//
//   - An object that matches with the given manifest has been created; but
//   - The object is not owned by KubeFleet (more specifically, the object is not owned by the
//     expected AppliedWork object).
func shouldInitiateTakeOverAttempt(manifestProcessingState *manifestProcessingState) bool {
	inMemberClusterObj := manifestProcessingState.inMemberClusterObj
	if inMemberClusterObj == nil {
		// Obviously, if the corresponding live object is not found, no takeover is
		// needed.
		return false
	}

	// Skip the takeover process if the sync strategy forbids so.
	if manifestProcessingState.fromPrimaryWorkObject.Spec.SyncStrategy.WhenAlreadyExists == placementv1alpha1.WhenAlreadyExistsOptionReportError {
		return false
	}

	// Check if the live object is owned by KubeFleet.
	curOwners := inMemberClusterObj.GetOwnerReferences()
	for idx := range curOwners {
		if ownerreferences.AreEqual(&curOwners[idx], manifestProcessingState.ownedBy) {
			// The live object is owned by KubeFleet; no takeover is needed.
			return false
		}
	}
	return true
}

// takeOverPreExistingObject takes over a pre-existing object in the member cluster.
func (r *Reconciler) takeOverPreExistingObject(
	ctx context.Context,
	manifestProcessingState *manifestProcessingState,
) (*unstructured.Unstructured, []placementv1alpha1.PatchDetail, bool, error) {
	gvr := manifestProcessingState.gvr
	syncStrategy := manifestProcessingState.fromPrimaryWorkObject.Spec.SyncStrategy
	expectedAppliedWorkOwnerRef := manifestProcessingState.ownedBy

	inMemberClusterObjCopy := manifestProcessingState.inMemberClusterObj.DeepCopy()
	existingOwnerRefs := inMemberClusterObjCopy.GetOwnerReferences()

	// At this moment KubeFleet is set to leave applied resources in the member cluster if the cluster
	// decides to leave its fleet; when this happens, the resources will be owned by an AppliedWork
	// object that might not have a corresponding Work object anymore as the cluster might have
	// been de-selected. If the cluster decides to re-join the fleet, and the same set of manifests
	// are being applied again, one may encounter unexpected errors due to the presence of the
	// left behind AppliedWork object. To address this issue, KubeFleet here will perform a cleanup,
	// removing any owner reference that points to an orphaned AppliedWork object.
	existingOwnerRefs, err := r.removeLeftBehindAppliedWorkOwnerRefs(ctx, existingOwnerRefs)
	if err != nil {
		return nil, nil, false, errors.Wraps(err, "failed to remove left-behind AppliedWork owner references")
	}

	// Check this object is already owned by another object (or controller); if so, KubeFleet will only
	// add itself as an additional owner if co-ownership is allowed.
	if len(existingOwnerRefs) >= 1 && syncStrategy.WhenOwnedByOthers != placementv1alpha1.WhenOwnedByOthersOptionShareOwnership {
		// The object is already owned by another object, and co-ownership is forbidden.
		// No takeover will be performed.
		//
		// Note that this will be registered as an (apply) error.
		return nil, nil, false, errors.NewUserError(nil,
			"the object is already owned by some other source(s) and co-ownership is disallowed",
			"existingOwnerRefs", existingOwnerRefs)
	}

	// Check if the object is already owned by KubeFleet, but the owner is a different AppliedWork
	// object, i.e., the object has been placed in duplicate.
	//
	// Originally KubeFleet does allow placing the same object multiple times; each placement
	// attempt might have its own object spec (envelopes might be used and in each envelope
	// the object looks different). This would lead to the object being constantly overwritten,
	// yet no error would be raised on the user-end. With the drift detection feature now added to KubeFleet, however,
	// this scenario would repeatedly trigger drift-related alarms, which could lead to user confusion.
	// To address this corner case, KubeFleet would now refuse to place the same object twice.
	if isPlacedByKubeFleetInDuplicate(existingOwnerRefs, expectedAppliedWorkOwnerRef) {
		return nil, nil, false, errors.NewUserError(nil, "the object is already owned by another KubeFleet AppliedWork object")
	}

	// Check if the takeover action requires additional steps (configuration difference inspection).
	if syncStrategy.WhenAlreadyExists == placementv1alpha1.WhenAlreadyExistsOptionTakeOverIfNoDiff {
		configDiffs, diffCalculatedInDegradedMode, err := r.diffBetweenManifestAndInMemberClusterObjects(ctx,
			gvr, manifestProcessingState.manifestObj, inMemberClusterObjCopy, syncStrategy.ComparisonOption)
		switch {
		case err != nil:
			return nil, nil, false, errors.Wraps(err, "failed to calculate configuration diffs between the manifest object and the object from the member cluster")
		case len(configDiffs) > 0:
			return nil, configDiffs, diffCalculatedInDegradedMode, nil
		}
	}

	// Take over the object.
	updatedOwnerRefs := append(existingOwnerRefs, *expectedAppliedWorkOwnerRef)
	inMemberClusterObjCopy.SetOwnerReferences(updatedOwnerRefs)
	takenOverInMemberClusterObj, err := r.spokeDynamicClient.
		Resource(*gvr).Namespace(inMemberClusterObjCopy.GetNamespace()).
		Update(ctx, inMemberClusterObjCopy, metav1.UpdateOptions{})
	if err != nil {
		// false as the dynamic client is non-caching.
		return nil, nil, false, errors.NewAPIServerError(err, "failed to take over the object", false)
	}

	return takenOverInMemberClusterObj, nil, false, nil
}

// removeLeftBehindAppliedWorkOwnerRefs removes owner references that point to orphaned AppliedWork objects.
func (r *Reconciler) removeLeftBehindAppliedWorkOwnerRefs(ctx context.Context, ownerRefs []metav1.OwnerReference) ([]metav1.OwnerReference, error) {
	updatedOwnerRefs := make([]metav1.OwnerReference, 0, len(ownerRefs))
	for idx := range ownerRefs {
		ownerRef := ownerRefs[idx]
		if ownerRef.APIVersion != placementv1alpha1.GroupVersion.String() || ownerRef.Kind != placementv1alpha1.AppliedWorkKind {
			// Skip non-appliedWork owner references.
			updatedOwnerRefs = append(updatedOwnerRefs, ownerRef)
			continue
		}

		// Check if the appliedWork object has a corresponding work object.
		workObj := &placementv1alpha1.Work{}
		err := r.hubClient.Get(ctx, types.NamespacedName{Namespace: r.workNSName, Name: ownerRef.Name}, workObj)
		switch {
		case err != nil && !apierrors.IsNotFound(err):
			// An unexpected error occurred.
			return nil, errors.NewAPIServerError(err, "failed to retrieve the work object", true,
				"workNamespace", r.workNSName, "workName", ownerRef.Name)
		case err == nil:
			// The appliedWork owner reference is valid; no need for removal.
			//
			// Note that no UID check is performed here; KubeFleet can (and will) reuse the same appliedWork
			// as long as it has the same name as a work object, even if the appliedWork object is not
			// originally derived from it. This is safe as the appliedWork object is in essence a delegate
			// and does not keep any additional information.
			updatedOwnerRefs = append(updatedOwnerRefs, ownerRef)
			continue
		default:
			// The appliedWork owner reference is invalid; the work object does not exist.
			//
			// Remove the owner reference.
			klog.V(2).InfoS("Found an owner reference that points to an orphaned AppliedWork object", "ownerRef", ownerRef)
			continue
		}
	}

	return updatedOwnerRefs, nil
}

// isPlacedByKubeFleetInDuplicate checks if the object has already been placed by KubeFleet via another
// placement policy.
func isPlacedByKubeFleetInDuplicate(ownerRefs []metav1.OwnerReference, expectedAppliedWorkOwnerRef *metav1.OwnerReference) bool {
	for idx := range ownerRefs {
		ownerRef := ownerRefs[idx]
		if ownerRef.APIVersion == placementv1alpha1.GroupVersion.String() &&
			ownerRef.Kind == placementv1alpha1.AppliedWorkKind &&
			ownerRef.UID != expectedAppliedWorkOwnerRef.UID {
			return true
		}
	}
	return false
}

// diffBetweenManifestAndInMemberClusterObjects calculates the differences between the manifest object
// and its corresponding object in the member cluster.
func (r *Reconciler) diffBetweenManifestAndInMemberClusterObjects(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObj, inMemberClusterObj *unstructured.Unstructured,
	cmpOption placementv1alpha1.ComparisonOption,
) ([]placementv1alpha1.PatchDetail, bool, error) {
	switch cmpOption {
	case placementv1alpha1.ComparisonOptionPartialComparison:
		return r.partialDiffBetweenManifestAndInMemberClusterObjects(ctx, gvr, manifestObj, inMemberClusterObj)
	case placementv1alpha1.ComparisonOptionFullComparison:
		// For the full comparison, KubeFleet compares directly the JSON representations of the
		// manifest object and the object in the member cluster.
		patchDetails, err := preparePatchDetails(manifestObj, inMemberClusterObj)
		return patchDetails, false, err
	default:
		return nil, false, errors.NewUnexpectedError(nil, "an invalid comparison option is specified", "comparisonOption", cmpOption)
	}
}

// partialDiffBetweenManifestAndInMemberClusterObjects calculates the differences between the
// manifest object and its corresponding object in the member cluster by performing a dry-run
// apply op; this would ignore differences in the unmanaged fields and report only those
// in the managed fields.
func (r *Reconciler) partialDiffBetweenManifestAndInMemberClusterObjects(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObj, inMemberClusterObj *unstructured.Unstructured,
) ([]placementv1alpha1.PatchDetail, bool, error) {
	// KubeFleet calculates the partial diff between two objects by running apply ops in the dry-run
	// mode.
	appliedObj, err := r.applyInDryRunMode(ctx, gvr, manifestObj, inMemberClusterObj)

	// After the dry-run apply op, all the managed fields should have been overwritten using the
	// values from the manifest object, while leaving all the unmanaged fields untouched. This
	// would allow KubeFleet to compare the object returned by the dry-run apply op with the object
	// that is currently in the member cluster; if all the fields are consistent, it is safe
	// for us to assume that there are no drifts, otherwise, any fields that are different
	// imply that running an actual apply op would lead to unexpected changes, which signifies
	// the presence of partial drifts (drifts in managed fields).

	switch {
	case err == nil:
		// The dry-run apply op has succeeded. All managed fields should have been overwritten using the
		// values from the manifest object, while leaving all the unmanaged fields untouched. This
		// would allow KubeFleet to compare the object returned by the dry-run apply op with the object
		// that is currently in the member cluster; if all the fields are consistent, it is safe
		// for us to assume that there are no drifts, otherwise, any fields that are different
		// imply that running an actual apply op would lead to unexpected changes, which signifies
		// the presence of partial drifts (drifts in managed fields).
		patchDetails, err := preparePatchDetails(appliedObj, inMemberClusterObj)
		return patchDetails, false, err
	case apierrors.IsInvalid(err):
		// The dry-run apply op has failed as the manifest object provided is not valid. This could
		// happen when the apply op involves fields that are immutable or the apply op attempts to
		// set invalid values. This error implies that the in-cluster object has already been modified,
		// and any change that the user supplies right now cannot be accepted.
		//
		// In this case, fall back to full comparison. Report that the diff is being calculated in a
		// degraded manner.
		//
		// This is not considered as a diff calculation error.
		klog.V(2).InfoS("Calculate diffs in degraded mode as the manifest object cannot be server-side applied in dry-run mode",
			"gvr", gvr, "manifestObj", klog.KObj(manifestObj), "serverErr", err)
		patchDetails, err := preparePatchDetails(manifestObj, inMemberClusterObj)
		return patchDetails, true, err
	default:
		// An unexpected error has occurred.
		return nil, false, errors.Wraps(err, "failed to apply the manifest in dry-run mode")
	}
}

// preparePatchDetails calculates the differences between two objects in the form
// of KubeFleet patch details.
func preparePatchDetails(srcObj, destObj *unstructured.Unstructured) ([]placementv1alpha1.PatchDetail, error) {
	// Discard certain fields from both objects before comparison.
	srcObjCopy := discardFieldsIrrelevantInComparisonFrom(srcObj)
	destObjCopy := discardFieldsIrrelevantInComparisonFrom(destObj)

	// Marshal the objects into JSON.
	srcObjJSONBytes, err := srcObjCopy.MarshalJSON()
	if err != nil {
		return nil, errors.NewUnexpectedError(err, "failed to marshal the source object into JSON")
	}

	destObjJSONBytes, err := destObjCopy.MarshalJSON()
	if err != nil {
		return nil, errors.NewUnexpectedError(err, "failed to marshal the destination object into JSON")
	}

	// Compare the JSON representations.
	patch, err := jsondiff.CompareJSON(srcObjJSONBytes, destObjJSONBytes)
	if err != nil {
		return nil, errors.NewUnexpectedError(err, "failed to compare the JSON representations of the source and destination objects")
	}

	// Prepare KubeFleet patch details from the JSON patches.
	details, err := organizeJSONPatchIntoPatchDetails(patch, srcObjCopy.Object)
	if err != nil {
		return nil, errors.Wraps(err, "failed to organize JSON patch operations into patch details")
	}

	// Obscure sensitive fields in the patch details.
	//
	// This currently only concerns Secret objects (core API group); all the drift/diff outputs regarding
	// a Secret object's data (`.data` or `.stringData` fields) are obscured.
	details = obscureSensitiveFieldsInPatchDetails(srcObj, details)
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

	// Clear out the object's name/namespace. For regular objects, the names/namespaces will
	// always be same between the manifest and the live objects, as guaranteed by the object
	// retrieval step earlier, so a comparison on these fields is unnecessary anyway.
	objCopy.SetName("")
	objCopy.SetNamespace("")

	// Clear out the object's generate name. This is a field that is irrelevant in comparison.
	objCopy.SetGenerateName("")

	// Remove certain labels and annotations.
	//
	// KubeFleet will remove labels/annotations that are reserved for KubeFleet own use cases, plus
	// well-known Kubernetes labels and annotations, as these cannot (should not) be set by users
	// directly.
	annotations := objCopy.GetAnnotations()
	cleanedAnnotations := map[string]string{}
	for k, v := range annotations {
		if strings.Contains(k, k8sReservedLabelAnnotationFullDomain) {
			// Skip Kubernetes reserved annotations.
			continue
		}

		if strings.Contains(k, k8sReservedLabelAnnotationAbbrDomain) {
			// Skip Kubernetes reserved annotations.
			continue
		}

		if strings.Contains(k, kubefleetReservedLabelAnnotationDomain) {
			// Skip KubeFleet reserved annotations.
			continue
		}
		cleanedAnnotations[k] = v
	}
	objCopy.SetAnnotations(cleanedAnnotations)

	labels := objCopy.GetLabels()
	cleanedLabels := map[string]string{}
	for k, v := range labels {
		if strings.Contains(k, k8sReservedLabelAnnotationFullDomain) {
			// Skip Kubernetes reserved labels.
			continue
		}

		if strings.Contains(k, k8sReservedLabelAnnotationAbbrDomain) {
			// Skip Kubernetes reserved labels.
			continue
		}

		if strings.Contains(k, kubefleetReservedLabelAnnotationDomain) {
			// Skip KubeFleet reserved labels.
			continue
		}
		cleanedLabels[k] = v
	}
	objCopy.SetLabels(cleanedLabels)

	// Fields below are system-reserved fields in object meta. Technically speaking they can be
	// set in the manifests, but this is a very uncommon practice, and currently KubeFleet will clear
	// these fields (except for the finalizers) before applying the manifests.
	// As a result, for now KubeFleet will ignore them in the comparison process as well.
	//
	// TO-DO (chenyu1): evaluate if this is a correct assumption for most (if not all) KubeFleet
	// users.
	objCopy.SetFinalizers([]string{})
	objCopy.SetManagedFields([]metav1.ManagedFieldsEntry{})
	objCopy.SetOwnerReferences([]metav1.OwnerReference{})

	// Fields below are read-only fields in object meta. KubeFleet will ignore them in the comparison
	// process.
	// Deleted objects are handled separately in the apply process; for comparison purposes,
	// KubeFleet will ignore the deletion timestamp and grace period seconds.
	objCopy.SetDeletionTimestamp(nil)
	objCopy.SetDeletionGracePeriodSeconds(nil)
	objCopy.SetGeneration(0)
	objCopy.SetResourceVersion("")
	objCopy.SetSelfLink("")
	objCopy.SetUID("")

	// Remove the status field.
	unstructured.RemoveNestedField(objCopy.Object, "status")

	// Remove all creationTimestamp fields.
	unstructured.RemoveNestedField(objCopy.Object, "metadata", "creationTimestamp")
	// Resources that have .spec.template.metadata include: Deployment, Job, StatefulSet,
	// DaemonSet, ReplicaSet, and CronJob.
	unstructured.RemoveNestedField(objCopy.Object, "spec", "template", "metadata", "creationTimestamp")
	// Also handle CronJob's .spec.jobTemplate.spec.template.metadata
	unstructured.RemoveNestedField(objCopy.Object, "spec", "jobTemplate", "spec", "template", "metadata", "creationTimestamp")

	return objCopy
}

// organizeJSONPatchIntoPatchDetails organizes the JSON patch operations into KubeFleet patch details.
func organizeJSONPatchIntoPatchDetails(patch jsondiff.Patch, manifestObjMap map[string]interface{}) ([]placementv1alpha1.PatchDetail, error) {
	// Pre-allocate the slice for the patch details. The organization procedure typically will yield
	// the same number of PatchDetail items as the JSON patch operations.
	details := make([]placementv1alpha1.PatchDetail, 0, len(patch))

	// A side note: here KubeFleet takes an expedient approach processing null JSON paths, by treating
	// null paths as empty strings.

	// Process only the first 100 ops.
	// TO-DO (chenyu1): Impose additional size limits.
	for idx := 0; idx < len(patch) && idx < patchDetailPerObjLimit; idx++ {
		op := patch[idx]
		pathPtr, err := jsonpointer.Parse(op.Path)
		if err != nil {
			// An invalid path is found; normally this should not happen.
			return nil, errors.NewUnexpectedError(err, "failed to parse the JSON path", "path", op.Path)
		}
		fromPtr, err := jsonpointer.Parse(op.From)
		if err != nil {
			// An invalid path is found; normally this should not happen.
			return nil, errors.NewUnexpectedError(err, "failed to parse the JSON path", "path", op.From)
		}

		switch op.Type {
		case jsondiff.OperationAdd:
			details = append(details, placementv1alpha1.PatchDetail{
				Path:          op.Path,
				ValueInMember: fmt.Sprint(op.Value),
			})
		case jsondiff.OperationRemove:
			// KubeFleet here skips validation as the JSON data is just marshalled.
			hubValue, err := pathPtr.Eval(manifestObjMap)
			if err != nil {
				return nil, errors.NewUnexpectedError(err, "failed to evaluate the JSON path in the manifest object", "path", op.Path)
			}
			details = append(details, placementv1alpha1.PatchDetail{
				Path:       op.Path,
				ValueInHub: fmt.Sprintf("%v", hubValue),
			})
		case jsondiff.OperationReplace:
			// KubeFleet here skips validation as the JSON data is just marshalled.
			hubValue, err := pathPtr.Eval(manifestObjMap)
			if err != nil {
				return nil, errors.NewUnexpectedError(err, "failed to evaluate the JSON path in the manifest object", "path", op.Path)
			}
			details = append(details, placementv1alpha1.PatchDetail{
				Path:          op.Path,
				ValueInMember: fmt.Sprint(op.Value),
				ValueInHub:    fmt.Sprintf("%v", hubValue),
			})
		case jsondiff.OperationMove:
			// Normally the Move operation will not be returned as factorization is disabled
			// for the JSON patch calculation process; however, KubeFleet here still processes them
			// just in case.
			//
			// Each Move operation will be parsed into two separate operations.
			hubValue, err := fromPtr.Eval(manifestObjMap)
			if err != nil {
				return nil, errors.NewUnexpectedError(err, "failed to evaluate the JSON path in the manifest object", "path", op.From)
			}
			details = append(details, placementv1alpha1.PatchDetail{
				Path:       op.From,
				ValueInHub: fmt.Sprintf("%v", hubValue),
			})
			details = append(details, placementv1alpha1.PatchDetail{
				Path:          op.Path,
				ValueInMember: fmt.Sprintf("%v", hubValue),
			})
		case jsondiff.OperationCopy:
			// Normally the Copy operation will not be returned as factorization is disabled
			// for the JSON patch calculation process; however, KubeFleet here still processes them
			// just in case.
			//
			// Each Copy operation will be parsed into an Add operation.
			hubValue, err := fromPtr.Eval(manifestObjMap)
			if err != nil {
				return nil, errors.NewUnexpectedError(err, "failed to evaluate the JSON path in the manifest object", "path", op.From)
			}
			details = append(details, placementv1alpha1.PatchDetail{
				Path:          op.Path,
				ValueInMember: fmt.Sprintf("%v", hubValue),
			})
		case jsondiff.OperationTest:
			// The Test op is a no-op in KubeFleet's use case. Normally it will not be returned, either.
		default:
			// An unexpected op is returned.
			return nil, errors.NewUnexpectedError(nil, "an unexpected JSON patch operation is returned", "op", op)
		}
	}

	return details, nil
}

// obscureSensitiveFieldsInPatchDetails obscures sensitive fields from the patch details so that
// such information will not be included in the drift/diff outputs.
//
// At this moment fields in the following API objects are discarded:
//
// * `.data` and `.stringData` field (and their children) in all Secret objects (`core` API group).
//
// Note (chenyu1): there are other Kubernetes API objects that also feature sensitive data,
// such as TokenRequest and CertificateSigningRequest; these objects are not included in the list
// as they have the sensitive information in the status, which are not accounted for in the
// drift/diff calculation in the very beginning.
func obscureSensitiveFieldsInPatchDetails(
	srcObj *unstructured.Unstructured, details []placementv1alpha1.PatchDetail,
) (sanitizedPatchDetails []placementv1alpha1.PatchDetail) {
	// Verify if the object is a Secret.
	if srcObj.GetAPIVersion() != "v1" || srcObj.GetKind() != "Secret" {
		return details
	}

	for idx := range details {
		pd := &details[idx]
		// Note (chenyu1): the string data field in the Secret object is provided by Kubernetes
		// as a write-only field for convenience reasons; all entries shall be merged into the
		// data field. Here the code still processes the field just for completeness reasons;
		// in practice it will never be included as part of the patch details.
		if strings.HasPrefix(pd.Path, "/data") || strings.HasPrefix(pd.Path, "/stringData") {
			// Obscure all patch details that concerns the Secret object's data.

			if len(pd.ValueInHub) > 0 {
				pd.ValueInHub = "(redacted for security reasons)"
			}
			if len(pd.ValueInMember) > 0 {
				pd.ValueInMember = "(redacted for security reasons)"
			}
		}
	}
	return details
}
