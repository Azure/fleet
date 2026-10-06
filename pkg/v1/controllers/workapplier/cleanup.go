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
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/ownerreferences"
)

// cleanupWhenBindingDeleted performs cleanup ops when the placement binding that spawns all the linked
// work objects has been deleted, based on the given SyncStrategy.
func (r *Reconciler) cleanupWhenBindingDeleted(ctx context.Context,
	primaryWork *placementv1alpha1.Work) (*time.Duration, error) {
	primaryWorkCopy := primaryWork.DeepCopy()
	setDefaultSyncStrategy(primaryWorkCopy)

	deletionPolicy := metav1.DeletePropagationForeground
	if primaryWorkCopy.Spec.SyncStrategy.WhenPlacementDeleted == placementv1alpha1.WhenPlacementDeletedOptionOrphanResources {
		deletionPolicy = metav1.DeletePropagationOrphan
	}

	// Retrieve all works that derive from the same placement policy.
	//
	// Note that this list comes from the cache; if the cache has become stale, and there exists a work object
	// that is owned by the placement policy but not yet reflected in the cache, it is guaranteed that the work applier
	// has never processed it before; as a result, the work object should have no finalizer and needs no cleanup.
	ownerNS := primaryWork.GetLabels()[placementv1alpha1.WorkOwnerNamespaceLabelKey]
	ownerName := primaryWork.GetLabels()[placementv1alpha1.WorkOwnedByPlacementBindingLabelKey]
	if ownerName == "" {
		return nil, errors.NewUnexpectedError(nil, "no owner placement binding found for the primary work object", "observedOwnerNS", ownerNS)
	}
	ownedByFieldVal := fmt.Sprintf(fieldindexers.WorkOwnedByPlacementBindingCustomFieldValFmt, ownerNS, ownerName)
	workList := &placementv1alpha1.WorkList{}
	if err := r.hubClient.List(ctx, workList,
		client.InNamespace(primaryWork.Namespace),
		client.MatchingFields{fieldindexers.WorkOwnedByPlacementBindingCustomFieldName: ownedByFieldVal},
	); err != nil {
		return nil, errors.NewAPIServerError(err, "failed to list the work objects owned by the placement policy", true,
			"ownerPlacementPolicy", ownedByFieldVal)
	}
	works := workList.Items

	// All the manifests across the linked work objects are owned by the appliedWork object of the primary work.
	appliedWork := &placementv1alpha1.AppliedWork{
		ObjectMeta: metav1.ObjectMeta{Name: primaryWork.Name},
	}
	err := r.spokeClient.Get(ctx, client.ObjectKey{Name: primaryWork.Name}, appliedWork)
	switch {
	case apierrors.IsNotFound(err):
		// The primary appliedWork object is absent; no manifest cleanup is needed.
		klog.V(2).InfoS("The primary appliedWork object is not found; no manifest cleanup is needed", "appliedWork", klog.KObj(appliedWork))
	case err != nil:
		// An unexpected error has occurred.
		return nil, errors.NewAPIServerError(err, "failed to retrieve the appliedWork object", true, "appliedWork", klog.KObj(appliedWork))
	case appliedWork.DeletionTimestamp.IsZero():
		// The appliedWork object exists and has not been marked for deletion yet; delete it.
		if err := r.spokeClient.Delete(ctx, appliedWork, &client.DeleteOptions{PropagationPolicy: &deletionPolicy}); err != nil && !apierrors.IsNotFound(err) {
			// An unexpected error has occurred.
			return nil, errors.NewAPIServerError(err, "failed to delete the appliedWork object", false,
				"appliedWork", klog.KObj(appliedWork), "propagationPolicy", deletionPolicy)
		}
		klog.V(2).InfoS("The primary appliedWork object has been marked for deletion; wait for the deletion to complete",
			"appliedWork", klog.KObj(appliedWork), "propagationPolicy", deletionPolicy)
		return &r.cleanupRequeueAfter, nil
	default:
		// The appliedWork object has been marked for deletion; wait for the deletion to complete.
		if time.Since(appliedWork.DeletionTimestamp.Time) < r.cleanupWaitTime {
			// Keep waiting.
			return &r.cleanupRequeueAfter, nil
		}

		if _, found := appliedWork.Annotations[appliedWorkForcedDeletedAnnotationKey]; !found {
			klog.V(2).InfoS("The primary appliedWork object has been stuck in the pending deletion state; force-remove KubeFleet owner reference from applied manifests as a last resort",
				"appliedWork", klog.KObj(appliedWork))
			// The appliedWork object has been stuck in the pending deletion state for longer than the cleanup wait time;
			// manually update owner references from all the applied manifests so that they no longer block the deletion
			// of the appliedWork object.
			if err := r.manuallyUpdateOwnerReferencesOnManifests(ctx, appliedWork, works); err != nil {
				return nil, errors.Wraps(err, "failed to manually update owner references from manifests to unblock cleanup")
			}

			// Add the force-deleted annotation to the appliedWork object.
			appliedWorkPatch := client.MergeFrom(appliedWork.DeepCopy())
			annotations := appliedWork.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations[appliedWorkForcedDeletedAnnotationKey] = "true"
			appliedWork.SetAnnotations(annotations)
			if err := r.spokeClient.Patch(ctx, appliedWork, appliedWorkPatch); err != nil && !apierrors.IsNotFound(err) {
				return nil, errors.NewAPIServerError(err, "failed to add the force-deleted annotation to the appliedWork object", false,
					"appliedWork", klog.KObj(appliedWork))
			}
		}

		if time.Since(appliedWork.DeletionTimestamp.Time) < 2*r.cleanupWaitTime {
			// Keep waiting.
			return &r.cleanupRequeueAfter, nil
		}

		// If the appliedWork object is still stuck in the pending deletion state after us manually removing the
		// owner references from all the applied manifests, the work applier will NOT keep
		// waiting for the appliedWork object to disappear. Users should keep an eye on the GC progress (the
		// API server/GC controller might not be fast enough) themselves, and manual intervention might be needed
		// if the situation persists.
		klog.Warning("The appliedWork object has been stuck in the pending deletion state for too long, and KubeFleet will proceed with the cleanup without further waiting; manual intervention might be needed.")
	}

	// Remove the cleanup finalizer from non-primary work objects first.
	for idx := range works {
		work := &works[idx]
		if work.Name == primaryWork.Name || !controllerutil.ContainsFinalizer(work, workApplierCleanupFinalizer) {
			continue
		}

		appliedWork := &placementv1alpha1.AppliedWork{
			ObjectMeta: metav1.ObjectMeta{Name: work.Name},
		}
		// No need to specify the propagation policy; these appliedWork objects do not own any child resources.
		if err := r.spokeClient.Delete(ctx, appliedWork); err != nil && !apierrors.IsNotFound(err) {
			return nil, errors.NewAPIServerError(err, "failed to delete the appliedWork object", false,
				"appliedWork", klog.KObj(appliedWork), "work", klog.KObj(work))
		}
		klog.V(2).InfoS("Cleaned up the appliedWork object", "appliedWork", klog.KObj(appliedWork), "work", klog.KObj(work))

		controllerutil.RemoveFinalizer(work, workApplierCleanupFinalizer)
		if err := r.hubClient.Update(ctx, work); err != nil {
			return nil, errors.NewAPIServerError(err, "failed to remove the cleanup finalizer from the work", false,
				"work", klog.KObj(work))
		}
		klog.V(2).InfoS("Removed the cleanup finalizer from the work", "work", klog.KObj(work))
	}

	// Remove the cleanup finalizer from the primary work object.
	if controllerutil.ContainsFinalizer(primaryWork, workApplierCleanupFinalizer) {
		controllerutil.RemoveFinalizer(primaryWork, workApplierCleanupFinalizer)
		if err := r.hubClient.Update(ctx, primaryWork); err != nil {
			return nil, errors.NewAPIServerError(err, "failed to remove the cleanup finalizer from the primary work", false,
				"work", klog.KObj(primaryWork))
		}
		klog.V(2).InfoS("Removed the cleanup finalizer from the primary work", "work", klog.KObj(primaryWork))
	}

	return nil, nil
}

func (r *Reconciler) manuallyUpdateOwnerReferencesOnManifests(ctx context.Context,
	appliedWork *placementv1alpha1.AppliedWork,
	works []placementv1alpha1.Work) error {
	appliedWorkOwnerRef := &metav1.OwnerReference{
		APIVersion: placementv1alpha1.GroupVersion.String(),
		Kind:       "AppliedWork",
		Name:       appliedWork.Name,
		UID:        appliedWork.UID,
	}

	// Loop through all work objects, identify all applied (or pre-processed) manifests
	// in the status, and add them as owner reference removal candidates.
	manifestsToRemoveOwnerRef := []placementv1alpha1.ManifestIdentifier{}
	for workIdx := range works {
		work := &works[workIdx]
		for manifestIdx := range work.Status.Manifests {
			manifestStatus := &work.Status.Manifests[manifestIdx]
			appliedCond := meta.FindStatusCondition(manifestStatus.Conditions, placementv1alpha1.WorkCondTypeApplied)
			if appliedCond == nil || (appliedCond.Status != metav1.ConditionTrue && appliedCond.Reason != placementv1alpha1.WorkAppliedCondPreparingToProcessReason) {
				continue
			}

			manifestsToRemoveOwnerRef = append(manifestsToRemoveOwnerRef, manifestStatus.Identifier)
		}
	}

	// Update the owner references of all the identified manifests so that they no longer block appliedWork object's
	// deletion.
	errs := make([]error, len(manifestsToRemoveOwnerRef))
	removeOwnerRefFromManifest := func(piece int) {
		identifier := manifestsToRemoveOwnerRef[piece]
		gvr := schema.GroupVersionResource{
			Group:    identifier.APIGroup,
			Version:  identifier.APIVersion,
			Resource: identifier.Resource,
		}
		manifestRef := klog.KRef(identifier.Namespace, identifier.Name)
		manifestObj, err := r.spokeDynamicClient.Resource(gvr).Namespace(identifier.Namespace).Get(ctx, identifier.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return
			}
			errs[piece] = errors.NewAPIServerError(err, "failed to retrieve a manifest while unblocking appliedWork deletion", false,
				"gvr", gvr, "manifestObj", manifestRef, "appliedWork", klog.KObj(appliedWork))
			return
		}

		ownerRefs := manifestObj.GetOwnerReferences()
		if len(ownerRefs) == 0 {
			// The applied manifest is no longer owned by KubeFleet; no further action is needed.
			return
		}
		updated := false
		for ownerIdx := range ownerRefs {
			if ownerreferences.AreEqual(&ownerRefs[ownerIdx], appliedWorkOwnerRef) && ptr.Deref(ownerRefs[ownerIdx].BlockOwnerDeletion, false) {
				// Set BlockOwnerDeletion to false to allow the appliedWork object to be deleted without being blocked
				// by this manifest.
				ownerRefs[ownerIdx].BlockOwnerDeletion = ptr.To(false)
				updated = true
			}
		}
		if !updated {
			return
		}

		manifestObj.SetOwnerReferences(ownerRefs)
		if _, err := r.spokeDynamicClient.Resource(gvr).Namespace(identifier.Namespace).Update(ctx, manifestObj, metav1.UpdateOptions{}); err != nil && !apierrors.IsNotFound(err) {
			errs[piece] = errors.NewAPIServerError(err, "failed to unblock a manifest owner reference", false,
				"gvr", gvr, "manifestObj", manifestRef, "appliedWork", klog.KObj(appliedWork))
		}
	}
	r.parallelizer.ParallelizeUntil(ctx, len(manifestsToRemoveOwnerRef), removeOwnerRefFromManifest, "manuallyRemoveOwnerReferencesFromManifests")

	if aggregatedErrs := utilerrors.NewAggregate(errs); aggregatedErrs != nil {
		return errors.Wraps(nil, "failed to update owner references on manifests",
			"appliedWork", klog.KObj(appliedWork), "errs", aggregatedErrs)
	}
	return nil
}

func (r *Reconciler) cleanupLeftOverWorks(ctx context.Context,
	leftOverWorks []*placementv1alpha1.Work, seenManifIDs sets.Set[string], appliedWorkOwnerRef *metav1.OwnerReference) error {
	for idx := range leftOverWorks {
		leftOverWork := leftOverWorks[idx]
		if !controllerutil.ContainsFinalizer(leftOverWork, workApplierCleanupFinalizer) {
			// The left-over work does not have the cleanup finalizer; no cleanup is needed.
			continue
		}

		// Identify manifests that are no longer seen and need to be removed.
		manifestsToDelete := make([]placementv1alpha1.ManifestIdentifier, 0)
		for manifestIdx := range leftOverWork.Status.Manifests {
			manifestStatus := &leftOverWork.Status.Manifests[manifestIdx]
			if seenManifIDs.Has(formatManifestIdentifierStr(&manifestStatus.Identifier)) {
				// There exists a corner case where a manifest, though present on a left-over work, is still present
				// on one of the linked work objects; in this case, such manifests would not be removed.
				continue
			}

			// Check if the manifest has been applied (or pre-processed) before; if so, attempt to remove it.
			appliedCond := meta.FindStatusCondition(manifestStatus.Conditions, placementv1alpha1.WorkCondTypeApplied)
			if appliedCond != nil && (appliedCond.Status == metav1.ConditionTrue || appliedCond.Reason == placementv1alpha1.WorkAppliedCondPreparingToProcessReason) {
				manifestsToDelete = append(manifestsToDelete, manifestStatus.Identifier)
			}
		}

		errs := make([]error, len(manifestsToDelete))
		removeManifest := func(piece int) {
			manifest := manifestsToDelete[piece]
			if err := r.removeOneLeftOverManifest(ctx, manifest, appliedWorkOwnerRef); err != nil {
				errs[piece] = errors.Wraps(err, "failed to remove a left-over manifest", "manifestId", manifest, "work", klog.KObj(leftOverWork))
			}
		}
		r.parallelizer.ParallelizeUntil(ctx, len(manifestsToDelete), removeManifest, "cleanupLeftOverWorks")
		aggregatedErrs := utilerrors.NewAggregate(errs)
		if aggregatedErrs != nil {
			return errors.Wraps(nil, "failed to remove manifests on a left-over work", "work", klog.KObj(leftOverWork), "errs", aggregatedErrs)
		}

		// Clean up the appliedWork object associated with the left-over work.
		appliedWork := &placementv1alpha1.AppliedWork{
			ObjectMeta: metav1.ObjectMeta{Name: leftOverWork.Name},
		}
		if err := r.spokeClient.Delete(ctx, appliedWork); err != nil && !apierrors.IsNotFound(err) {
			return errors.NewAPIServerError(err, "failed to delete the appliedWork object associated with the left-over work", false,
				"appliedWork", klog.KObj(appliedWork), "work", klog.KObj(leftOverWork))
		}
		klog.V(2).InfoS("Cleaned up the appliedWork object associated with the left-over work",
			"appliedWork", klog.KObj(appliedWork), "work", klog.KObj(leftOverWork))

		controllerutil.RemoveFinalizer(leftOverWork, workApplierCleanupFinalizer)
		if err := r.hubClient.Update(ctx, leftOverWork); err != nil {
			return errors.NewAPIServerError(err, "failed to remove the cleanup finalizer from the left-over work", false,
				"work", klog.KObj(leftOverWork))
		}
		klog.V(2).InfoS("Cleaned up a left-over work", "work", klog.KObj(leftOverWork))
	}
	return nil
}
