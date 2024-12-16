/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"context"
	"fmt"

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
)

var builtInScheme = runtime.NewScheme()

func init() {
	// This is a trick that allows Fleet to check if a resource is a K8s built-in one.
	_ = clientgoscheme.AddToScheme(builtInScheme)
}

// applyInDryRunMode
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
	// Originally the manifest hash is kept only if three-way merge patch (client side apply esque
	// strategy) is used; with the new drift detection and takeover capabilities, the manifest hash
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
		// (client-side apply esque patch) should be used, and the last applied annotation
		// has been set.
		return r.threeWayMergePatch(ctx, gvr, manifestObjCopy, inMemberClusterObj, isOptimisticLockEnabled, false)
	case applyStrategy.Type == fleetv1beta1.ApplyStrategyTypeClientSideApply:
		// The apply strategy dictates that three-way merge patch
		// (client-side apply esque patch) should be used, but the last applied annotation
		// cannot be set. Fleet will fall back to server-side apply.
		return r.serverSideApply(
			ctx,
			gvr, manifestObjCopy, inMemberClusterObj,
			applyStrategy.ServerSideApplyConfig.ForceConflicts, isOptimisticLockEnabled, false,
		)
	case applyStrategy.Type == fleetv1beta1.ApplyStrategyTypeServerSideApply:
		// The apply strategy dictates that server-side apply should be used.
		return r.serverSideApply(
			ctx,
			gvr, manifestObjCopy, inMemberClusterObj,
			applyStrategy.ServerSideApplyConfig.ForceConflicts, isOptimisticLockEnabled, false,
		)
	default:
		// An unexpected apply strategy has been set.
		return nil, fmt.Errorf("unexpected apply strategy %s is found", applyStrategy.Type)
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
		return nil, fmt.Errorf("failed to create manifest object: %w", err)
	}
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
		return nil, fmt.Errorf("failed to get patch data: %w", err)
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
		return nil, fmt.Errorf("failed to patch the manifest object: %w", err)
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
		return nil, fmt.Errorf("failed to apply the manifest object: %w", err)
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
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		// use StrategicMergePatch for K8s built-in resources
		patchType = types.StrategicMergePatchType
		lookupPatchMeta, err = strategicpatch.NewPatchMetaFromStruct(versionedObject)
		if err != nil {
			return nil, err
		}
		patchData, err = strategicpatch.CreateThreeWayMergePatch(lastAppliedObjJSONBytes, manifestObjJSONBytes, liveObjJSONBytes, lookupPatchMeta, true)
		if err != nil {
			return nil, err
		}
	}
	return client.RawPatch(patchType, patchData), nil
}
