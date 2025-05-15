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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/mergepatch"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/resource"
)

var builtInScheme = runtime.NewScheme()

func init() {
	// This is a trick that allows Fleet to check if a resource is a K8s built-in one.
	_ = clientgoscheme.AddToScheme(builtInScheme)
}

// applyInDryRunMode dry-runs an apply op.
func (r *Reconciler) applyInDryRunMode(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObj, inMemberClusterObj *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	// In this method, Fleet will always use forced server-side apply
	// w/o optimistic lock for diff calculation.
	//
	// This is OK as partial comparison concerns only fields that are currently present
	// in the manifest object, and Fleet will clear out system managed and read-only fields
	// before the comparison.
	//
	// Note that full comparison can be carried out directly without involving the apply op.
	return r.serverSideApply(ctx, gvr, manifestObj, inMemberClusterObj, true, false, true)
}

func (r *Reconciler) apply(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObj, inMemberClusterObj *unstructured.Unstructured,
	applyStrategy *fleetv1beta1.ApplyStrategy,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) (*unstructured.Unstructured, error) {
	// Create a sanitized copy of the manifest object.
	//
	// Note (chenyu1): for objects directly created on the hub, the Fleet hub agent has already
	// performed a round of sanitization (i.e., clearing fields such as resource version,
	// UID, etc.); this, however, does not apply to enveloped objects, which are currently
	// placed as-is. To accommodate for this, the work applier here will perform an additional
	// round of sanitization.
	//
	// TO-DO (chenyu1): this processing step can be removed if the enveloped objects are
	// pre-processed by the Fleet hub agent as well, provided that there are no further
	// backwards compatibility concerns.
	manifestObjCopy := sanitizeManifestObject(manifestObj)

	// Compute the hash of the manifest object.
	//
	// Originally the manifest hash is kept only if three-way merge patch (client side apply)
	// is used; with the new drift detection and takeover capabilities, the manifest hash
	// will always be kept regardless of the apply strategy in use, as it is needed for
	// drift detection purposes.
	//
	// Note that certain fields have been removed from the manifest object in the hash computation
	// process.
	if err := setManifestHashAnnotation(manifestObjCopy); err != nil {
		return nil, fmt.Errorf("failed to set manifest hash annotation: %w", err)
	}

	// Validate owner references.
	//
	// As previously mentioned, with the new capabilities, at this point of the workflow,
	// Fleet has been added as an owner for the object. Still, to guard against cases where
	// the allow co-ownership switch is turned on then off or the addition of new owner references
	// in the manifest, Fleet will still perform a validation round.
	if err := validateOwnerReferences(manifestObj, inMemberClusterObj, applyStrategy, expectedAppliedWorkOwnerRef); err != nil {
		return nil, fmt.Errorf("failed to validate owner references: %w", err)
	}

	// Add the owner reference information.
	setOwnerRef(manifestObjCopy, expectedAppliedWorkOwnerRef)

	// If three-way merge patch is used, set the Fleet-specific last applied annotation.
	// Note that this op might not complete due to the last applied annotation being too large;
	// this is not recognized as an error and Fleet will switch to server-side apply instead.
	isLastAppliedAnnotationSet := false
	if applyStrategy.Type == fleetv1beta1.ApplyStrategyTypeClientSideApply {
		var err error
		isLastAppliedAnnotationSet, err = setFleetLastAppliedAnnotation(manifestObjCopy)
		if err != nil {
			return nil, fmt.Errorf("failed to set last applied annotation: %w", err)
		}

		// Note that Fleet might choose to skip the last applied annotation due to size limits
		// even if no error has occurred.
		klog.V(2).InfoS("Completed the last applied annotation setting process", "isSet", isLastAppliedAnnotationSet,
			"GVR", *gvr, "manifestObj", klog.KObj(manifestObjCopy))
	}

	// Create the object if it does not exist in the member cluster.
	if inMemberClusterObj == nil {
		return r.createManifestObject(ctx, gvr, manifestObjCopy)
	}

	// Note: originally Fleet will add its owner reference and
	// retrieve existing (previously applied) object during the apply op; with the addition
	// of drift detection and takeover capabilities, such steps have been completed before
	// the apply op actually runs.

	// Run the apply op. Note that Fleet will always attempt to apply the manifest, even if
	// the manifest object hash does not change.

	// Optimistic lock is enabled when the apply strategy dictates that an apply op should
	// not be carries through if a drift has been found (i.e., the WhenToApply field is set
	// to IfNotDrifted); this helps Fleet guard against
	// cases where inadvertent changes are being made in an untimely manner (i.e., changes
	// are made when the Fleet agent is preparing an apply op).
	//
	// Note that if the apply strategy dictates that apply ops are always executed (i.e.,
	// the WhenToApply field is set to Always), Fleet will not enable optimistic lock. This
	// is consistent with the behavior before the drift detection and takeover experience
	// is added.
	isOptimisticLockEnabled := shouldEnableOptimisticLock(applyStrategy)

	switch {
	case applyStrategy.Type == fleetv1beta1.ApplyStrategyTypeClientSideApply && isLastAppliedAnnotationSet:
		// The apply strategy dictates that three-way merge patch
		// (client-side apply) should be used, and the last applied annotation
		// has been set.
		klog.V(2).InfoS("Using three-way merge patch to apply the manifest object",
			"GVR", *gvr, "manifestObj", klog.KObj(manifestObjCopy))
		return r.threeWayMergePatch(ctx, gvr, manifestObjCopy, inMemberClusterObj, isOptimisticLockEnabled, false)
	case applyStrategy.Type == fleetv1beta1.ApplyStrategyTypeClientSideApply:
		// The apply strategy dictates that three-way merge patch
		// (client-side apply) should be used, but the last applied annotation
		// cannot be set. Fleet will fall back to server-side apply.
		klog.V(2).InfoS("Falling back to server-side apply as the last applied annotation cannot be set",
			"GVR", *gvr, "manifestObj", klog.KObj(manifestObjCopy))
		return r.serverSideApply(
			ctx,
			gvr, manifestObjCopy, inMemberClusterObj,
			applyStrategy.ServerSideApplyConfig.ForceConflicts, isOptimisticLockEnabled, false,
		)
	case applyStrategy.Type == fleetv1beta1.ApplyStrategyTypeServerSideApply:
		// The apply strategy dictates that server-side apply should be used.
		klog.V(2).InfoS("Using server-side apply to apply the manifest object",
			"GVR", *gvr, "manifestObj", klog.KObj(manifestObjCopy))
		return r.serverSideApply(
			ctx,
			gvr, manifestObjCopy, inMemberClusterObj,
			applyStrategy.ServerSideApplyConfig.ForceConflicts, isOptimisticLockEnabled, false,
		)
	default:
		// An unexpected apply strategy has been set. Normally this will never run as the built-in
		// validation would block invalid values.
		wrappedErr := fmt.Errorf("unexpected apply strategy %s is found", applyStrategy.Type)
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		return nil, wrappedErr
	}
}

// createManifestObject creates the manifest object in the member cluster.
func (r *Reconciler) createManifestObject(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObject *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	createOpts := metav1.CreateOptions{
		FieldManager: workFieldManagerName,
	}
	createdObj, err := r.spokeDynamicClient.Resource(*gvr).Namespace(manifestObject.GetNamespace()).Create(ctx, manifestObject, createOpts)
	if err != nil {
		wrappedErr := controller.NewAPIServerError(false, err)
		return nil, fmt.Errorf("failed to create manifest object: %w", wrappedErr)
	}
	klog.V(2).InfoS("Created the manifest object", "GVR", *gvr, "manifestObj", klog.KObj(createdObj))
	return createdObj, nil
}

// threeWayMergePatch uses three-way merge patch to apply the manifest object.
func (r *Reconciler) threeWayMergePatch(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObj, inMemberClusterObj *unstructured.Unstructured,
	optimisticLock, dryRun bool,
) (*unstructured.Unstructured, error) {
	// Enable optimistic lock by forcing the resource version field to be added to the
	// JSON merge patch. Optimistic lock is always enabled in the dry run mode.
	if optimisticLock || dryRun {
		curResourceVer := inMemberClusterObj.GetResourceVersion()
		if len(curResourceVer) == 0 {
			return nil, fmt.Errorf("failed to enable optimistic lock: resource version is empty on the object %s/%s from the member cluster", inMemberClusterObj.GroupVersionKind(), klog.KObj(inMemberClusterObj))
		}

		// Add the resource version to the manifest object.
		manifestObj.SetResourceVersion(curResourceVer)

		// Remove the resource version from the object in the member cluster.
		inMemberClusterObj.SetResourceVersion("")
	}

	// Create a three-way merge patch.
	patch, err := buildThreeWayMergePatch(manifestObj, inMemberClusterObj)
	if err != nil {
		return nil, fmt.Errorf("failed to create three-way merge patch: %w", err)
	}
	data, err := patch.Data(manifestObj)
	if err != nil {
		// Fleet uses raw patch; this branch should never run.
		wrappedErr := fmt.Errorf("failed to get patch data: %w", err)
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		return nil, wrappedErr
	}

	// Use three-way merge (similar to kubectl client side apply) to patch the object in the
	// member cluster.
	//
	// This will:
	// * Remove fields that are present in the last applied configuration but not in the
	//   manifest object.
	// * Create fields that are present in the manifest object but not in the object from the member cluster.
	// * Update fields that are present in both the manifest object and the object from the member cluster.
	patchOpts := metav1.PatchOptions{
		FieldManager: workFieldManagerName,
	}
	if dryRun {
		patchOpts.DryRun = []string{metav1.DryRunAll}
	}
	patchedObj, err := r.spokeDynamicClient.
		Resource(*gvr).Namespace(manifestObj.GetNamespace()).
		Patch(ctx, manifestObj.GetName(), patch.Type(), data, patchOpts)
	if err != nil {
		wrappedErr := controller.NewAPIServerError(false, err)
		return nil, fmt.Errorf("failed to patch the manifest object: %w", wrappedErr)
	}
	return patchedObj, nil
}

// serverSideApply uses server-side apply to apply the manifest object.
func (r *Reconciler) serverSideApply(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObj, inMemberClusterObj *unstructured.Unstructured,
	force, optimisticLock, dryRun bool,
) (*unstructured.Unstructured, error) {
	// Enable optimistic lock by forcing the resource version field to be added to the
	// JSON merge patch. Optimistic lock is always disabled in the dry run mode.
	if optimisticLock && !dryRun {
		curResourceVer := inMemberClusterObj.GetResourceVersion()
		if len(curResourceVer) == 0 {
			return nil, fmt.Errorf("failed to enable optimistic lock: resource version is empty on the object %s/%s from the member cluster", inMemberClusterObj.GroupVersionKind(), klog.KObj(inMemberClusterObj))
		}

		// Add the resource version to the manifest object.
		manifestObj.SetResourceVersion(curResourceVer)

		// Remove the resource version from the object in the member cluster.
		inMemberClusterObj.SetResourceVersion("")
	}

	// Check if forced server-side apply is needed even if it is not turned on by the user.
	//
	// Note (chenyu1): This is added to addresses cases where Kubernetes might register
	// Fleet (the member agent) as an Update typed field manager for the object, which blocks
	// the same agent itself from performing a server-side apply due to conflicts,
	// as Kubernetes considers Update typed and Apply typed field managers to be different
	// entities, despite having the same identifier. In these cases, users will see their
	// first apply attempt being successful, yet any subsequent update would fail due to
	// conflicts. There are also a few other similar cases that are solved by this check;
	// see the inner comments for specifics.
	if shouldUseForcedServerSideApply(inMemberClusterObj) {
		force = true
	}

	// Use server-side apply to apply the manifest object.
	//
	// See the Kubernetes documentation on structured merged diff for the exact behaviors.
	applyOpts := metav1.ApplyOptions{
		FieldManager: workFieldManagerName,
		Force:        force,
	}
	if dryRun {
		applyOpts.DryRun = []string{metav1.DryRunAll}
	}
	appliedObj, err := r.spokeDynamicClient.
		Resource(*gvr).Namespace(manifestObj.GetNamespace()).
		Apply(ctx, manifestObj.GetName(), manifestObj, applyOpts)
	if err != nil {
		wrappedErr := controller.NewAPIServerError(false, err)
		return nil, fmt.Errorf("failed to apply the manifest object: %w", wrappedErr)
	}
	return appliedObj, nil
}

// threeWayMergePatch creates a patch by computing a three-way diff based on
// the manifest object, the live object, and the last applied configuration as kept in
// the annotations.
func buildThreeWayMergePatch(manifestObj, liveObj *unstructured.Unstructured) (client.Patch, error) {
	// Marshal the manifest object into JSON bytes.
	manifestObjJSONBytes, err := manifestObj.MarshalJSON()
	if err != nil {
		return nil, err
	}
	// Marshal the live object into JSON bytes.
	liveObjJSONBytes, err := liveObj.MarshalJSON()
	if err != nil {
		return nil, err
	}
	// Retrieve the last applied configuration from the annotations. This can be an empty string.
	lastAppliedObjJSONBytes := getFleetLastAppliedAnnotation(liveObj)

	var patchType types.PatchType
	var patchData []byte
	var lookupPatchMeta strategicpatch.LookupPatchMeta

	versionedObject, err := builtInScheme.New(liveObj.GetObjectKind().GroupVersionKind())
	switch {
	case runtime.IsNotRegisteredError(err):
		// use JSONMergePatch for custom resources
		// because StrategicMergePatch doesn't support custom resources
		patchType = types.MergePatchType
		preconditions := []mergepatch.PreconditionFunc{
			mergepatch.RequireKeyUnchanged("apiVersion"),
			mergepatch.RequireKeyUnchanged("kind"),
			mergepatch.RequireMetadataKeyUnchanged("name"),
		}
		patchData, err = jsonmergepatch.CreateThreeWayJSONMergePatch(
			lastAppliedObjJSONBytes, manifestObjJSONBytes, liveObjJSONBytes, preconditions...)
		if err != nil {
			wrappedErr := fmt.Errorf("failed to create three-way JSON merge patch: %w", err)
			_ = controller.NewUnexpectedBehaviorError(wrappedErr)
			return nil, wrappedErr
		}
	case err != nil:
		return nil, err
	default:
		// use StrategicMergePatch for K8s built-in resources
		patchType = types.StrategicMergePatchType
		lookupPatchMeta, err = strategicpatch.NewPatchMetaFromStruct(versionedObject)
		if err != nil {
			wrappedErr := fmt.Errorf("failed to create patch meta from struct (strategic merge patch): %w", err)
			_ = controller.NewUnexpectedBehaviorError(wrappedErr)
			return nil, wrappedErr
		}
		patchData, err = strategicpatch.CreateThreeWayMergePatch(lastAppliedObjJSONBytes, manifestObjJSONBytes, liveObjJSONBytes, lookupPatchMeta, true)
		if err != nil {
			wrappedErr := fmt.Errorf("failed to create three-way strategic merge patch: %w", err)
			_ = controller.NewUnexpectedBehaviorError(wrappedErr)
			return nil, wrappedErr
		}
	}
	return client.RawPatch(patchType, patchData), nil
}

// setFleetLastAppliedAnnotation sets the last applied annotation on the provided manifest object.
func setFleetLastAppliedAnnotation(manifestObj *unstructured.Unstructured) (bool, error) {
	annotations := manifestObj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Remove the last applied annotation just in case.
	delete(annotations, fleetv1beta1.LastAppliedConfigAnnotation)
	manifestObj.SetAnnotations(annotations)

	lastAppliedManifestJSONBytes, err := manifestObj.MarshalJSON()
	if err != nil {
		wrappedErr := fmt.Errorf("failed to marshal the manifest object into JSON: %w", err)
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		return false, wrappedErr
	}
	annotations[fleetv1beta1.LastAppliedConfigAnnotation] = string(lastAppliedManifestJSONBytes)
	isLastAppliedAnnotationSet := true

	if err := validation.ValidateAnnotationsSize(annotations); err != nil {
		// If the annotation size exceeds the limit, Fleet will set the annotation to an empty string.
		annotations[fleetv1beta1.LastAppliedConfigAnnotation] = ""
		isLastAppliedAnnotationSet = false
	}

	manifestObj.SetAnnotations(annotations)
	return isLastAppliedAnnotationSet, nil
}

// getFleetLastAppliedAnnotation returns the last applied annotation of a manifest object.
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

// setManifestHashAnnotation computes the hash of the provided manifest and sets an annotation of the
// hash on the provided unstructured object.
func setManifestHashAnnotation(manifestObj *unstructured.Unstructured) error {
	cleanedManifestObj := discardFieldsIrrelevantInComparisonFrom(manifestObj)
	manifestObjHash, err := resource.HashOf(cleanedManifestObj.Object)
	if err != nil {
		wrappedErr := fmt.Errorf("failed to compute the hash of the manifest object: %w", err)
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		return wrappedErr
	}

	annotations := manifestObj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[fleetv1beta1.ManifestHashAnnotation] = manifestObjHash
	manifestObj.SetAnnotations(annotations)
	return nil
}

// setOwnerRef sets the expected owner reference (reference to an AppliedWork object)
// on a manifest to be applied.
func setOwnerRef(obj *unstructured.Unstructured, expectedAppliedWorkOwnerRef *metav1.OwnerReference) {
	ownerRefs := obj.GetOwnerReferences()

	// Typically owner references is a system-managed field, and at this moment Fleet will
	// clear owner references (if any) set in the manifest object. However, for consistency
	// reasons, here Fleet will still assume that there might be some owner references set
	// in the manifest object.
	//
	// TO-DO (chenyu1): evaluate if user-set owner references should be kept.
	ownerRefs = append(ownerRefs, *expectedAppliedWorkOwnerRef)
	obj.SetOwnerReferences(ownerRefs)
}

// validateOwnerReferences validates the owner references of an applied manifest, checking
// if an apply op can be performed on the object.
func validateOwnerReferences(
	manifestObj, inMemberClusterObj *unstructured.Unstructured,
	applyStrategy *fleetv1beta1.ApplyStrategy,
	expectedAppliedWorkOwnerRef *metav1.OwnerReference,
) error {
	manifestObjOwnerRefs := manifestObj.GetOwnerReferences()

	// If the manifest object already features some owner reference(s), but co-ownership is
	// disallowed, the validation fails.
	//
	// This is just a sanity check; normally the branch will never get triggered as Fleet would
	// perform sanitization on the manifest object before applying it, which removes all owner
	// references.
	if len(manifestObjOwnerRefs) > 0 && !applyStrategy.AllowCoOwnership {
		wrappedErr := fmt.Errorf("manifest is set to have owner references but co-ownership is disallowed")
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		return wrappedErr
	}

	// Do a sanity check to verify that no AppliedWork object is directly added as an owner
	// in the manifest object. Normally the branch will never get triggered as Fleet would
	// perform sanitization on the manifest object before applying it, which removes all owner
	// references.
	for _, ownerRef := range manifestObjOwnerRefs {
		if ownerRef.APIVersion == fleetv1beta1.GroupVersion.String() && ownerRef.Kind == fleetv1beta1.AppliedWorkKind {
			wrappedErr := fmt.Errorf("an AppliedWork object is unexpectedly added as an owner in the manifest object")
			_ = controller.NewUnexpectedBehaviorError(wrappedErr)
			return wrappedErr
		}
	}

	if inMemberClusterObj == nil {
		// The manifest object has never been applied yet; no need to do further validation.
		return nil
	}
	inMemberClusterObjOwnerRefs := inMemberClusterObj.GetOwnerReferences()

	// If the live object is co-owned but co-ownership is no longer allowed, the validation fails.
	if len(inMemberClusterObjOwnerRefs) > 1 && !applyStrategy.AllowCoOwnership {
		wrappedErr := fmt.Errorf("object is co-owned by multiple objects but co-ownership has been disallowed")
		_ = controller.NewUserError(wrappedErr)
		return wrappedErr
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
		wrappedErr := fmt.Errorf("object is not owned by the expected AppliedWork object")
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		return wrappedErr
	}

	// If the object is already owned by another AppliedWork object, the validation fails.
	//
	// Normally this branch will never get executed as Fleet would refuse to take over an object
	// that has been owned by another AppliedWork object.
	if isPlacedByFleetInDuplicate(inMemberClusterObjOwnerRefs, expectedAppliedWorkOwnerRef) {
		wrappedErr := fmt.Errorf("object is already owned by another AppliedWork object")
		_ = controller.NewUnexpectedBehaviorError(wrappedErr)
		return wrappedErr
	}

	return nil
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

// shouldUseForcedServerSideApply checks if forced server-side apply should be used even if
// the force option is not turned on.
func shouldUseForcedServerSideApply(inMemberClusterObj *unstructured.Unstructured) bool {
	managedFields := inMemberClusterObj.GetManagedFields()
	for idx := range managedFields {
		mf := &managedFields[idx]
		// workFieldManagerName is the field manager name used by Fleet; its presence
		// suggests that some (not necessarily all) fields are managed by Fleet.
		//
		// `before-first-apply` is a field manager name used by Kubernetes to "properly"
		// track field managers between non-apply and apply ops. Specifically, this
		// manager is added when an object is being applied, but Kubernetes finds
		// that the object does not have any managed field specified.
		//
		// Note (chenyu1): unfortunately this name is not exposed as a public variable. See
		// the Kubernetes source code for more information.
		if mf.Manager != workFieldManagerName && mf.Manager != "before-first-apply" {
			// There exists a field manager this is neither Fleet nor the `before-first-apply`
			// field manager, which suggests that the object (or at least some of its fields)
			// is managed by another entity. Fleet will not enable forced server-side apply in
			// this case and let user decide if forced apply is needed.
			klog.V(2).InfoS("Found a field manager that is neither Fleet nor the `before-first-apply` field manager; Fleet will not enable forced server-side apply unless explicitly requested",
				"fieldManager", mf.Manager,
				"GVK", inMemberClusterObj.GroupVersionKind(), "inMemberClusterObj", klog.KObj(inMemberClusterObj))
			return false
		}
	}

	// All field managers are either Fleet or the `before-first-apply` field manager;
	// use forced server-side apply to avoid confusing self-conflicts. This would
	// allow Fleet to (correctly) assume ownership of managed fields.
	klog.V(2).InfoS("All field managers are either Fleet or the `before-first-apply` field manager; Fleet will enable forced server-side apply",
		"GVK", inMemberClusterObj.GroupVersionKind(), "inMemberClusterObj", klog.KObj(inMemberClusterObj))
	return true
}
