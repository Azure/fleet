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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	"k8s.io/kubectl/pkg/util/deployment"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/resource"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/ownerreferences"
)

var builtInScheme = runtime.NewScheme()

func init() {
	// This is a trick that allows KubeFleet to check if a resource is a K8s built-in one.
	_ = clientgoscheme.AddToScheme(builtInScheme)
}

// applyInDryRunMode dry-runs an apply op.
func (r *Reconciler) applyInDryRunMode(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObj, inMemberClusterObj *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	// In this method, KubeFleet will always use forced server-side apply
	// w/o optimistic lock for diff calculation.
	//
	// This is OK as partial comparison concerns only fields that are currently present
	// in the manifest object, and KubeFleet will clear out system managed and read-only fields
	// before the comparison.
	//
	// Note that full comparison can be carried out directly without involving the apply op.
	return r.serverSideApply(ctx, gvr, manifestObj, inMemberClusterObj, true, false, true)
}

func (r *Reconciler) apply(
	ctx context.Context,
	manifestProcessingState *manifestProcessingState,
) (*unstructured.Unstructured, error) {
	gvr := manifestProcessingState.gvr
	inMemberClusterObj := manifestProcessingState.inMemberClusterObj
	syncStrategy := manifestProcessingState.fromPrimaryWorkObject.Spec.SyncStrategy
	expectedAppliedWorkOwnerRef := manifestProcessingState.ownedBy

	// Create a sanitized copy of the manifest object.
	//
	// TO-DO (chenyu1): this processing step can be dropped if the KubeFleet hub agent can guarantee that all manifest
	// objects have already been sanitized.
	manifestObjCopy := sanitizeManifestObject(manifestProcessingState.manifestObj)

	// Compute the hash of the manifest object.
	//
	// Originally the manifest hash is kept only if three-way merge patch (client side apply)
	// is used; with the new drift detection and takeover capabilities, the manifest hash
	// will always be kept regardless of the apply method in use, as it is needed for
	// drift detection purposes.
	//
	// Note that certain fields have been removed from the manifest object in the hash computation
	// process.
	if err := setLastAppliedManifestHashAnnotation(manifestObjCopy); err != nil {
		return nil, errors.Wraps(err, "failed to set manifest hash annotation")
	}

	// Validate owner references.
	//
	// As previously mentioned, with the new capabilities, at this point of the workflow,
	// KubeFleet has been added as an owner for the object. Still, to guard against cases where
	// co-ownership is turned on then off or the addition of new owner references
	// in the manifest, KubeFleet will still perform a validation round.
	if err := validateOwnerRefs(manifestProcessingState); err != nil {
		return nil, errors.Wraps(err, "failed to validate owner references")
	}

	// Add the owner reference information.
	ownerreferences.Own(manifestObjCopy, expectedAppliedWorkOwnerRef)

	// If three-way merge patch is used, set the KubeFleet-specific last applied annotation.
	// Note that this op might not complete due to the last applied annotation being too large;
	// this is not recognized as an error and KubeFleet will switch to server-side apply instead.
	isLastAppliedAnnotationSet := false
	if syncStrategy.ApplyMethod == placementv1alpha1.ApplyMethodClientSideApply {
		var err error
		isLastAppliedAnnotationSet, err = setLastAppliedConfigAnnotation(manifestObjCopy)
		if err != nil {
			return nil, errors.Wraps(err, "failed to set last applied configuration annotation")
		}

		// Note that KubeFleet might choose to skip the last applied annotation due to size limits
		// even if no error has occurred.
		klog.V(2).InfoS("Completed the last applied annotation setting process", "isSet", isLastAppliedAnnotationSet,
			"GVR", *gvr, "manifestObj", klog.KObj(manifestObjCopy))
	}

	// Create the object if it does not exist in the member cluster.
	if inMemberClusterObj == nil {
		shouldCreateNS, err := r.shouldCreateNamespaceFor(ctx, manifestObjCopy, syncStrategy)
		if err != nil {
			return nil, errors.Wraps(err, "failed to determine if the namespace should be created")
		}
		if shouldCreateNS {
			if err := r.createNamespaceFor(ctx, manifestObjCopy); err != nil {
				return nil, errors.Wraps(err, "failed to create namespace for the manifest object")
			}
		}
		return r.createManifestObject(ctx, gvr, manifestObjCopy)
	}

	// Run the apply op. Note that KubeFleet will always attempt to apply the manifest, even if
	// the manifest object hash does not change.

	// Optimistic lock is enabled when the sync strategy dictates that a drift should be reported
	// rather than overwritten (i.e., the WhenDrifted field is set to ReportError); this helps
	// KubeFleet guard against cases where inadvertent changes are being made in an untimely manner
	// (i.e., changes are made when the KubeFleet agent is preparing an apply op).
	//
	// Note that if the sync strategy dictates that drifts should be overwritten (i.e.,
	// the WhenDrifted field is set to ApplyAnyway), KubeFleet will not enable optimistic lock. This
	// is consistent with the behavior before the drift detection and takeover experience
	// is added.
	isOptimisticLockEnabled := shouldEnableOptimisticLock(syncStrategy)

	switch {
	case syncStrategy.ApplyMethod == placementv1alpha1.ApplyMethodClientSideApply && isLastAppliedAnnotationSet:
		// The sync strategy dictates that three-way merge patch
		// (client-side apply) should be used, and the last applied annotation
		// has been set.
		klog.V(2).InfoS("Using three-way merge patch to apply the manifest object",
			"GVR", *gvr, "manifestObj", klog.KObj(manifestObjCopy))
		return r.threeWayMergePatch(ctx, gvr, manifestObjCopy, inMemberClusterObj, isOptimisticLockEnabled, false)
	case syncStrategy.ApplyMethod == placementv1alpha1.ApplyMethodClientSideApply:
		// The sync strategy dictates that three-way merge patch
		// (client-side apply) should be used, but the last applied annotation
		// cannot be set. KubeFleet will fall back to server-side apply.
		klog.V(2).InfoS("Falling back to server-side apply as the last applied annotation cannot be set",
			"GVR", *gvr, "manifestObj", klog.KObj(manifestObjCopy))
		return r.serverSideApply(
			ctx,
			gvr, manifestObjCopy, inMemberClusterObj,
			// When falling back to SSA, always disable force apply ops (this is also the default
			// behavior).
			//
			// Note that the work applier might still enable force apply ops if it finds that
			// self-conflicts might occur.
			false, isOptimisticLockEnabled, false,
		)
	case syncStrategy.ApplyMethod == placementv1alpha1.ApplyMethodServerSideApply:
		// The sync strategy dictates that server-side apply should be used.
		//
		// The server-side apply options are not defaulted by the API server when absent.
		forceConflicts := syncStrategy.ServerSideApplyOptions != nil && syncStrategy.ServerSideApplyOptions.ForceConflicts
		klog.V(2).InfoS("Using server-side apply to apply the manifest object",
			"GVR", *gvr, "manifestObj", klog.KObj(manifestObjCopy))
		return r.serverSideApply(
			ctx,
			gvr, manifestObjCopy, inMemberClusterObj,
			forceConflicts, isOptimisticLockEnabled, false,
		)
	default:
		// An unexpected apply method has been set. Normally this will never run as the built-in
		// validation would block invalid values.
		return nil, errors.NewUnexpectedError(nil, "an unexpected apply method is found", "applyMethod", syncStrategy.ApplyMethod)
	}
}

// shouldEnableOptimisticLock checks if optimistic lock should be enabled given a sync strategy.
func shouldEnableOptimisticLock(syncStrategy *placementv1alpha1.SyncStrategy) bool {
	// Optimistic lock is enabled if drifts are to be reported rather than overwritten.
	return syncStrategy.WhenDrifted == placementv1alpha1.WhenDriftedOptionReportError
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
		// Remove the two KubeFleet reserved annotations. This is normally not set by users.
		delete(annotations, placementv1alpha1.LastAppliedManifestHashAnnotationKey)
		delete(annotations, placementv1alpha1.LastAppliedConfigAnnotationKey)

		// Remove the last applied configuration set by kubectl.
		delete(annotations, corev1.LastAppliedConfigAnnotation)

		// Remove the revision annotation set by deployment controller.
		delete(annotations, deployment.RevisionAnnotation)

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

	// Note: in the KubeFleet hub agent logic, the system also handles the Service and Job objects
	// in a special way, so as to remove certain fields that are set by the hub cluster API
	// server automatically; for the KubeFleet member agent logic here, however, KubeFleet assumes
	// that if these fields are set, users must have set them on purpose, and they should not
	// be removed. The difference comes to the fact that the KubeFleet member agent sanitization
	// logic concerns only the enveloped objects, which are free from any hub cluster API
	// server manipulation anyway.

	return manifestObjCopy
}

// setLastAppliedManifestHashAnnotation computes the hash of the provided manifest and sets an annotation of the
// hash on the provided unstructured object.
func setLastAppliedManifestHashAnnotation(manifestObj *unstructured.Unstructured) error {
	cleanedManifestObj := discardFieldsIrrelevantInComparisonFrom(manifestObj)
	manifestObjHash, err := resource.HashOf(cleanedManifestObj.Object)
	if err != nil {
		return errors.NewUnexpectedError(err, "failed to compute the hash of the manifest object")
	}

	annotations := manifestObj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[placementv1alpha1.LastAppliedManifestHashAnnotationKey] = manifestObjHash
	manifestObj.SetAnnotations(annotations)
	return nil
}

// validateOwnerRefs validates the owner references of an applied manifest, checking
// if an apply op can be performed on the object.
func validateOwnerRefs(
	manifestProcessingState *manifestProcessingState,
) error {
	inMemberClusterObj := manifestProcessingState.inMemberClusterObj
	syncStrategy := manifestProcessingState.fromPrimaryWorkObject.Spec.SyncStrategy
	expectedAppliedWorkOwnerRef := manifestProcessingState.ownedBy
	isCoOwnershipAllowed := syncStrategy.WhenOwnedByOthers == placementv1alpha1.WhenOwnedByOthersOptionShareOwnership

	manifestObjOwnerRefs := manifestProcessingState.manifestObj.GetOwnerReferences()

	// If the manifest object already features some owner reference(s), but co-ownership is
	// disallowed, the validation fails.
	//
	// This is just a sanity check; normally the branch will never get triggered as KubeFleet would
	// perform sanitization on the manifest object before applying it, which removes all owner
	// references.
	if len(manifestObjOwnerRefs) > 0 && !isCoOwnershipAllowed {
		return errors.NewUnexpectedError(nil, "manifest is set to have owner references but co-ownership is disallowed")
	}

	// Do a sanity check to verify that no AppliedWork object is directly added as an owner
	// in the manifest object. Normally the branch will never get triggered as KubeFleet would
	// perform sanitization on the manifest object before applying it, which removes all owner
	// references.
	for idx := range manifestObjOwnerRefs {
		ownerRef := &manifestObjOwnerRefs[idx]
		if ownerRef.APIVersion == placementv1alpha1.GroupVersion.String() && ownerRef.Kind == placementv1alpha1.AppliedWorkKind {
			return errors.NewUnexpectedError(nil, "an appliedWork object is unexpectedly added as an owner in the manifest object")
		}
	}

	if inMemberClusterObj == nil {
		// The manifest object has never been applied yet; no need to do further validation.
		return nil
	}
	inMemberClusterObjOwnerRefs := inMemberClusterObj.GetOwnerReferences()

	// If the live object is co-owned but co-ownership is no longer allowed, the validation fails.
	if len(inMemberClusterObjOwnerRefs) > 1 && !isCoOwnershipAllowed {
		return errors.NewUserError(nil, "object is co-owned by multiple objects but co-ownership has been disallowed",
			"observedOwners", inMemberClusterObjOwnerRefs)
	}

	// Note that at this point of execution, one of the owner references is guaranteed to be the
	// expected AppliedWork object. For safety reasons, KubeFleet will still do a sanity check.
	found := false
	for idx := range inMemberClusterObjOwnerRefs {
		if ownerreferences.AreEqual(&inMemberClusterObjOwnerRefs[idx], expectedAppliedWorkOwnerRef) {
			found = true
			break
		}
	}
	if !found {
		return errors.NewUnexpectedError(nil, "object is not owned by the expected appliedWork object")
	}

	// If the object is already owned by another appliedWork object, the validation fails.
	//
	// Normally this branch will never get executed as KubeFleet would refuse to take over an object
	// that has been owned by another appliedWork object.
	if isPlacedByKubeFleetInDuplicate(inMemberClusterObjOwnerRefs, expectedAppliedWorkOwnerRef) {
		return errors.NewUnexpectedError(nil, "object is already owned by another appliedWork object")
	}

	return nil
}

// setLastAppliedConfigAnnotation sets the last applied annotation on the provided manifest object.
func setLastAppliedConfigAnnotation(manifestObj *unstructured.Unstructured) (bool, error) {
	annotations := manifestObj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Remove the last applied annotation just in case.
	delete(annotations, placementv1alpha1.LastAppliedConfigAnnotationKey)
	manifestObj.SetAnnotations(annotations)

	lastAppliedManifestJSONBytes, err := manifestObj.MarshalJSON()
	if err != nil {
		return false, errors.NewUnexpectedError(err, "failed to marshal the manifest object into JSON")
	}
	annotations[placementv1alpha1.LastAppliedConfigAnnotationKey] = string(lastAppliedManifestJSONBytes)
	isLastAppliedAnnotationSet := true

	if err := validation.ValidateAnnotationsSize(annotations); err != nil {
		// If the annotation size exceeds the limit, KubeFleet will set the annotation to an empty string.
		annotations[placementv1alpha1.LastAppliedConfigAnnotationKey] = ""
		isLastAppliedAnnotationSet = false
	}

	manifestObj.SetAnnotations(annotations)
	return isLastAppliedAnnotationSet, nil
}

// shouldCreateNamespaceFor checks if a namespace must be created in the member cluster before the manifest
// object itself can be applied.
func (r *Reconciler) shouldCreateNamespaceFor(ctx context.Context, manifestObj *unstructured.Unstructured, syncStrategy *placementv1alpha1.SyncStrategy) (bool, error) {
	if syncStrategy.WhenNamespaceDoesNotExist != placementv1alpha1.WhenNamespaceDoesNotExistOptionCreateNamespace {
		// The sync strategy forbids KubeFleet from creating the namespace.
		return false, nil
	}

	ns := manifestObj.GetNamespace()
	if len(ns) == 0 {
		// The manifest object is cluster-scoped; no namespace is needed.
		return false, nil
	}

	err := r.spokeClient.Get(ctx, types.NamespacedName{Name: ns}, &corev1.Namespace{})
	switch {
	case err == nil:
		return false, nil
	case apierrors.IsNotFound(err):
		klog.V(2).InfoS("The namespace of the manifest object does not exist in the member cluster", "namespace", ns)
		return true, nil
	default:
		// An unexpected error has occurred.
		return false, errors.NewAPIServerError(err,
			"failed to check if the namespace of the manifest object exists in the member cluster", true, "namespace", ns)
	}
}

// createNamespaceFor creates the namespace that a manifest object resides in.
//
// The namespace itself is not managed by KubeFleet; it is left behind when the placement is removed.
func (r *Reconciler) createNamespaceFor(ctx context.Context, manifestObj *unstructured.Unstructured) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: manifestObj.GetNamespace(),
		},
	}

	err := r.spokeClient.Create(ctx, ns, client.FieldOwner(fieldManagerName))
	switch {
	case err == nil:
		klog.V(2).InfoS("Created the namespace for the manifest object",
			"namespace", ns.Name, "manifestObj", klog.KObj(manifestObj))
		return nil
	case apierrors.IsAlreadyExists(err):
		// The namespace has been created; this is not considered an error.
		klog.V(2).InfoS("The namespace for the manifest object already exists in the member cluster",
			"namespace", ns.Name, "manifestObj", klog.KObj(manifestObj))
		return nil
	default:
		// false as this is a write op.
		return errors.NewAPIServerError(err, "failed to create the namespace for the manifest object", false,
			"namespace", ns.Name)
	}
}

// createManifestObject creates the manifest object in the member cluster.
func (r *Reconciler) createManifestObject(
	ctx context.Context,
	gvr *schema.GroupVersionResource,
	manifestObject *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	createOpts := metav1.CreateOptions{
		FieldManager: fieldManagerName,
	}
	createdObj, err := r.spokeDynamicClient.Resource(*gvr).Namespace(manifestObject.GetNamespace()).Create(ctx, manifestObject, createOpts)
	if err != nil {
		// false as the dynamic client is non-caching.
		return nil, errors.NewAPIServerError(err, "failed to create the manifest object", false)
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
			return nil, errors.NewUnexpectedError(nil, "failed to enable optimistic lock: resource version is empty on the object from the member cluster",
				"GVK", inMemberClusterObj.GroupVersionKind(), "inMemberClusterObj", klog.KObj(inMemberClusterObj))
		}

		// Add the resource version to the manifest object.
		manifestObj.SetResourceVersion(curResourceVer)

		// Remove the resource version from the object in the member cluster.
		inMemberClusterObj.SetResourceVersion("")
	}

	// Create a three-way merge patch.
	patch, err := buildThreeWayMergePatch(manifestObj, inMemberClusterObj)
	if err != nil {
		return nil, errors.Wraps(err, "failed to create three-way merge patch")
	}
	data, err := patch.Data(manifestObj)
	if err != nil {
		// KubeFleet uses raw patch; this branch should never run.
		return nil, errors.NewUnexpectedError(err, "failed to get patch data")
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
		FieldManager: fieldManagerName,
	}
	if dryRun {
		patchOpts.DryRun = []string{metav1.DryRunAll}
	}
	patchedObj, err := r.spokeDynamicClient.
		Resource(*gvr).Namespace(manifestObj.GetNamespace()).
		Patch(ctx, manifestObj.GetName(), patch.Type(), data, patchOpts)
	if err != nil {
		// false as the dynamic client is non-caching.
		return nil, errors.NewAPIServerError(err, "failed to patch the manifest object", false)
	}
	return patchedObj, nil
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
	lastAppliedObjJSONBytes := getLastAppliedConfigAnnotation(liveObj)

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
			return nil, errors.NewUnexpectedError(err, "failed to create three-way JSON merge patch")
		}
	case err != nil:
		return nil, err
	default:
		// use StrategicMergePatch for K8s built-in resources
		patchType = types.StrategicMergePatchType
		lookupPatchMeta, err = strategicpatch.NewPatchMetaFromStruct(versionedObject)
		if err != nil {
			return nil, errors.NewUnexpectedError(err, "failed to create patch meta from struct (strategic merge patch)")
		}
		patchData, err = strategicpatch.CreateThreeWayMergePatch(lastAppliedObjJSONBytes, manifestObjJSONBytes, liveObjJSONBytes, lookupPatchMeta, true)
		if err != nil {
			return nil, errors.NewUnexpectedError(err, "failed to create three-way strategic merge patch")
		}
	}
	return client.RawPatch(patchType, patchData), nil
}

// getLastAppliedConfigAnnotation returns the last applied annotation of a manifest object.
func getLastAppliedConfigAnnotation(inMemberClusterObj *unstructured.Unstructured) []byte {
	annotations := inMemberClusterObj.GetAnnotations()
	if annotations == nil {
		// The last applied annotation is not found in the live object; normally this should not
		// happen, but KubeFleet can still handle this situation.
		klog.V(2).InfoS("No annotations in the live object",
			"GVK", inMemberClusterObj.GroupVersionKind(), "inMemberClusterObj", klog.KObj(inMemberClusterObj))
		return nil
	}

	lastAppliedManifestJSONStr, found := annotations[placementv1alpha1.LastAppliedConfigAnnotationKey]
	if !found {
		// The last applied annotation is not found in the live object; normally this should not
		// happen, but KubeFleet can still handle this situation.
		klog.V(2).InfoS("The last applied annotation is not found in the live object",
			"GVK", inMemberClusterObj.GroupVersionKind(), "inMemberClusterObj", klog.KObj(inMemberClusterObj))
		return nil
	}

	return []byte(lastAppliedManifestJSONStr)
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
			return nil, errors.NewUnexpectedError(nil, "failed to enable optimistic lock: resource version is empty on the object from the member cluster",
				"GVK", inMemberClusterObj.GroupVersionKind(), "inMemberClusterObj", klog.KObj(inMemberClusterObj))
		}

		// Add the resource version to the manifest object.
		manifestObj.SetResourceVersion(curResourceVer)

		// Remove the resource version from the object in the member cluster.
		inMemberClusterObj.SetResourceVersion("")
	}

	// Check if forced server-side apply is needed even if it is not turned on by the user.
	//
	// Note (chenyu1): This is added to addresses cases where Kubernetes might register
	// KubeFleet (the member agent) as an Update typed field manager for the object, which blocks
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
		FieldManager: fieldManagerName,
		Force:        force,
	}
	if dryRun {
		applyOpts.DryRun = []string{metav1.DryRunAll}
	}
	appliedObj, err := r.spokeDynamicClient.
		Resource(*gvr).Namespace(manifestObj.GetNamespace()).
		Apply(ctx, manifestObj.GetName(), manifestObj, applyOpts)
	if err != nil {
		return nil, errors.NewAPIServerError(err, "failed to apply the manifest object: an error is returned by the API server", false)
	}
	return appliedObj, nil
}

// shouldUseForcedServerSideApply checks if forced server-side apply should be used even if
// the force option is not turned on.
func shouldUseForcedServerSideApply(inMemberClusterObj *unstructured.Unstructured) bool {
	managedFields := inMemberClusterObj.GetManagedFields()
	for idx := range managedFields {
		mf := &managedFields[idx]
		// fieldManagerName is the field manager name used by KubeFleet; its presence
		// suggests that some (not necessarily all) fields are managed by KubeFleet.
		//
		// `before-first-apply` is a field manager name used by Kubernetes to "properly"
		// track field managers between non-apply and apply ops. Specifically, this
		// manager is added when an object is being applied, but Kubernetes finds
		// that the object does not have any managed field specified.
		//
		// Note (chenyu1): unfortunately this name is not exposed as a public variable. See
		// the Kubernetes source code for more information.
		if mf.Manager != fieldManagerName && mf.Manager != "before-first-apply" {
			// There exists a field manager this is neither KubeFleet nor the `before-first-apply`
			// field manager, which suggests that the object (or at least some of its fields)
			// is managed by another entity. KubeFleet will not enable forced server-side apply in
			// this case and let user decide if forced apply is needed.
			klog.V(2).InfoS("Found a field manager that is neither KubeFleet nor the `before-first-apply` field manager; KubeFleet will not enable forced server-side apply unless explicitly requested",
				"fieldManager", mf.Manager,
				"GVK", inMemberClusterObj.GroupVersionKind(), "inMemberClusterObj", klog.KObj(inMemberClusterObj))
			return false
		}
	}

	// All field managers are either KubeFleet or the `before-first-apply` field manager;
	// use forced server-side apply to avoid confusing self-conflicts. This would
	// allow KubeFleet to (correctly) assume ownership of managed fields.
	klog.V(2).InfoS("All field managers are either KubeFleet or the `before-first-apply` field manager; KubeFleet will enable forced server-side apply",
		"GVK", inMemberClusterObj.GroupVersionKind(), "inMemberClusterObj", klog.KObj(inMemberClusterObj))
	return true
}
