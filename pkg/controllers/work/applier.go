/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package work

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/defaulter"
)

// Applier is the interface to apply the resources on the member clusters.
type Applier interface {
	ApplyUnstructured(ctx context.Context, applyStrategy *fleetv1beta1.ApplyStrategy, gvr schema.GroupVersionResource, manifestObj *unstructured.Unstructured) (*unstructured.Unstructured, ApplyAction, error)
}

// serverSideApply uses server side apply to apply the manifest.
func serverSideApply(ctx context.Context, client dynamic.Interface, force bool, gvr schema.GroupVersionResource,
	manifestObj *unstructured.Unstructured) (*unstructured.Unstructured, ApplyAction, error) {
	manifestRef := klog.KObj(manifestObj)
	options := metav1.ApplyOptions{
		FieldManager: workFieldManagerName,
		Force:        force,
	}
	manifestRes, err := client.Resource(gvr).Namespace(manifestObj.GetNamespace()).Apply(ctx, manifestObj.GetName(), manifestObj, options)
	if err != nil {
		klog.ErrorS(err, "Failed to apply object", "gvr", gvr, "manifest", manifestRef)
		return nil, errorApplyAction, controller.NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Manifest apply succeeded", "gvr", gvr, "manifest", manifestRef)
	return manifestRes, manifestServerSideAppliedAction, nil
}

// findConflictedWork checks if the manifest is owned by other placements which have configured different strategy.
// It returns the first conflicted work it finds.
func findConflictedWork(ctx context.Context, hubClient client.Client, namespace string, strategy *fleetv1beta1.ApplyStrategy, ownerRefs []metav1.OwnerReference) (*fleetv1beta1.Work, error) {
	for _, ownerRef := range ownerRefs {
		if ownerRef.APIVersion != fleetv1beta1.GroupVersion.String() || ownerRef.Kind != fleetv1beta1.AppliedWorkKind {
			continue
		}

		// we need to check if the appliedWork is using the same strategy as the work
		// Note, the resource could be already owned by the current work and apply strategy of the work may be changed
		// too during the check.
		// We'll return the user error to retry again.
		work := &fleetv1beta1.Work{}
		name := types.NamespacedName{Namespace: namespace, Name: ownerRef.Name}
		if err := hubClient.Get(ctx, name, work); err != nil {
			klog.ErrorS(err, "Failed to retrieve the work", "work", name)
			if errors.IsNotFound(err) {
				// The work may be just deleted by the time we are checking it and the ownerRef is already stale.
				// Or it could be manually deleted by the user.
				// Return the error to retry.
				return nil, controller.NewExpectedBehaviorError(err)
			}
			return nil, controller.NewAPIServerError(true, err)
		}
		// TODO, could be removed once we have the defaulting webhook with fail policy.
		// Make sure these conditions are met before moving
		// * the defaulting webhook failure policy is configured as "fail".
		// * user cannot update/delete the webhook.
		defaulter.SetDefaultsWork(work)
		if !equality.Semantic.DeepEqual(strategy, work.Spec.ApplyStrategy) {
			return work, nil
		}
	}
	return nil, nil
}

func validateOwnerReference(ctx context.Context, hubClient client.Client, namespace string, strategy *fleetv1beta1.ApplyStrategy, ownerRefs []metav1.OwnerReference) (ApplyAction, error) {
	// If no owner reference is found, the resource could be managed by the work.
	// There is a corner case that the resource is already managed by the work but the owner reference could be removed
	// by other controllers later.
	if len(ownerRefs) == 0 {
		return "", nil
	}

	conflictedWork, err := findConflictedWork(ctx, hubClient, namespace, strategy, ownerRefs)
	if err != nil {
		return errorApplyAction, err
	}
	if conflictedWork != nil {
		placement := conflictedWork.Labels[fleetv1beta1.CRPTrackingLabel]
		err := fmt.Errorf("manifest is already managed by placement %s but with different apply strategy or the placement strategy is changed", placement)
		klog.ErrorS(err, "Skip applying a manifest managed by another placement but with different apply strategy",
			"conflictedWork", conflictedWork.Name, "conflictedPlacement", placement, "conflictedWorkApplyStrategy", conflictedWork.Spec.ApplyStrategy)
		return applyConflictBetweenPlacements, controller.NewUserError(err)
	}

	// skip if the current resource is not derived from the work and co-ownership is disallowed
	// Note: if the co-ownership is added afterwards, e.g., by a controller using on the user-side, all resource changes
	// will fail to be updated.
	if !strategy.AllowCoOwnership && !isManifestManagedByWork(ownerRefs) {
		err := fmt.Errorf("resource exists and is not managed by the fleet controller and co-ownernship is disallowed")
		klog.ErrorS(err, "Skip applying a manifest managed by non-fleet applier", "ownerRefs", ownerRefs)
		return manifestAlreadyOwnedByOthers, controller.NewUserError(err)
	}
	return "", nil
}
