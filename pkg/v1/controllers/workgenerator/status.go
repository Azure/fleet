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

package workgenerator

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/errors"
)

func (r *Reconciler) refreshPlacementBindingStatus(
	ctx context.Context,
	placementBinding placementv1alpha1.PlacementBindingAccessor,
	works []placementv1alpha1.Work,
) (err error) {
	oldStatus := placementBinding.GetStatus().DeepCopy()

	if notReady := refreshPlacementBindingSyncCond(placementBinding, works); notReady {
		klog.V(2).InfoS("the synchronized condition is not yet ready to be refreshed; skipping status update for now")
		return nil
	}
	if notReady := refreshPlacementBindingAvailableCond(placementBinding, works); notReady {
		klog.V(2).InfoS("the available condition is not yet ready to be refreshed; skipping status update for now")
		return nil
	}

	total, synced, available, failed := countResourcesInWorksByProcessingResults(works)
	placementBinding.GetStatus().SelectedResources = ptr.To(total)
	placementBinding.GetStatus().SynchronizedResources = ptr.To(synced)
	placementBinding.GetStatus().AvailableResources = ptr.To(available)
	if len(failed) > 50 {
		klog.V(2).InfoS("Too many failed resources to report in placement binding status; truncating the list to 50",
			"placementBinding", klog.KObj(placementBinding), "totalFailedResources", len(failed))
		failed = failed[:50]
	}
	placementBinding.GetStatus().FailedResources = failed

	// Skip the update if the status has not changed.
	if equality.Semantic.DeepEqual(oldStatus, placementBinding.GetStatus()) {
		klog.V(2).InfoS("No need to update placement binding status as it has not changed",
			"placementBinding", klog.KObj(placementBinding),
			"selectedResources", total, "synchronizedResources", synced, "availableResources", available,
			"failedResources", len(failed))
		return nil
	}

	if err := r.hubClient.Status().Update(ctx, placementBinding); err != nil {
		return errors.NewAPIServerError(err, "failed to update placement binding status", true)
	}
	klog.V(2).InfoS("Updated placement binding status",
		"placementBinding", klog.KObj(placementBinding),
		"selectedResources", total, "synchronizedResources", synced, "availableResources", available,
		"failedResources", len(failed))
	return nil
}

func (r *Reconciler) reportPlacementBindingProcessingProgress(
	ctx context.Context,
	placementBinding placementv1alpha1.PlacementBindingAccessor,
	primaryPlacementResourceSnapshot placementv1alpha1.PlacementResourceSnapshotAccessor,
	worksToCreateOrUpdate []*placementv1alpha1.Work,
) error {
	placementBindingStatus := placementBinding.GetStatus()
	// Set the last processed placement resource snapshot name on the placement binding status.
	placementBindingStatus.LastProcessedResourceSnapshotName = ptr.To(primaryPlacementResourceSnapshot.GetName())

	// Set a false Synchronized condition with the WaitingForSynchronization reason on the placement binding.
	meta.SetStatusCondition(&placementBindingStatus.Conditions, metav1.Condition{
		Type:               placementv1alpha1.PlacementBindingCondTypeSynchronized,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: placementBinding.GetGeneration(),
		Reason:             placementv1alpha1.PlacementBindingSynchronizedCondReasonWaitingForSynchronization,
		Message:            "Waiting for the resources to be synchronized to the target cluster",
	})
	// Set an unknown Available condition with the WaitingForAvailabilityCheck reason on the placement binding.
	meta.SetStatusCondition(&placementBindingStatus.Conditions, metav1.Condition{
		Type:               placementv1alpha1.PlacementBindingCondTypeAvailable,
		Status:             metav1.ConditionUnknown,
		ObservedGeneration: placementBinding.GetGeneration(),
		Reason:             placementv1alpha1.PlacementBindingAvailableCondReasonWaitingForAvailabilityCheck,
		Message:            "Waiting for the resources to be checked for availability in the target cluster",
	})

	// Count the number of manifests in all created/updated work objects.
	total := int32(0)
	for idx := range worksToCreateOrUpdate {
		total += int32(len(worksToCreateOrUpdate[idx].Spec.Manifests)) //nolint:gosec // safe: there is no risk of overflowing due to API-level restrictions.
	}
	placementBindingStatus.SelectedResources = ptr.To(total)

	// Clear the other counters and failed resources as their previous values no longer apply.
	placementBindingStatus.SynchronizedResources = nil
	placementBindingStatus.AvailableResources = nil
	placementBindingStatus.FailedResources = nil

	if err := r.hubClient.Status().Update(ctx, placementBinding); err != nil {
		return errors.NewAPIServerError(err, "failed to update placement binding status", true)
	}
	klog.V(2).InfoS("Reported placement binding processing progress",
		"placementBinding", klog.KObj(placementBinding), "selectedResources", total)
	return nil
}

func refreshPlacementBindingSyncCond(placementBinding placementv1alpha1.PlacementBindingAccessor, works []placementv1alpha1.Work) (notReady bool) {
	// The binding is synchronized only if every work has been applied and its applied condition is up-to-date.
	synchronized := true
	for idx := range works {
		work := &works[idx]
		appliedCond := meta.FindStatusCondition(work.Status.Conditions, placementv1alpha1.WorkCondTypeApplied)
		if appliedCond == nil || appliedCond.ObservedGeneration != work.Generation {
			// The work object has not been applied or its applied condition is outdated. Instead of refreshing
			// the placement binding status conditions on stale data, return a transient error and wait for
			// the status to be updated.
			return true
		}
		if !condition.IsConditionStatusTrue(appliedCond, work.GetGeneration()) {
			synchronized = false
			break
		}
	}

	var syncCond metav1.Condition
	if synchronized {
		syncCond = metav1.Condition{
			Type:               placementv1alpha1.PlacementBindingCondTypeSynchronized,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: placementBinding.GetGeneration(),
			Reason:             placementv1alpha1.PlacementBindingSynchronizedCondReasonAllResourcesSynchronized,
			Message:            "All resources have been synchronized to the target cluster",
		}
	} else {
		syncCond = metav1.Condition{
			Type:               placementv1alpha1.PlacementBindingCondTypeSynchronized,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: placementBinding.GetGeneration(),
			Reason:             placementv1alpha1.PlacementBindingSynchronizedCondReasonFailedToSynchronizeSomeResources,
			Message:            "Some resources might be out of sync in the target cluster",
		}
	}
	meta.SetStatusCondition(&placementBinding.GetStatus().Conditions, syncCond)
	return false
}

func refreshPlacementBindingAvailableCond(placementBinding placementv1alpha1.PlacementBindingAccessor, works []placementv1alpha1.Work) (notReady bool) {
	// The binding is available only if every work is available and its available condition is up-to-date.
	available := true
	for idx := range works {
		work := &works[idx]
		availableCond := meta.FindStatusCondition(work.Status.Conditions, placementv1alpha1.WorkCondTypeAvailable)
		if availableCond == nil || availableCond.ObservedGeneration != work.Generation {
			// The work object has not been marked as available or its available condition is outdated.
			// Instead of refreshing the placement binding status conditions on stale data, return a transient error
			// and wait for the status to be updated.
			return true
		}
		if !condition.IsConditionStatusTrue(availableCond, work.GetGeneration()) {
			available = false
			break
		}
	}

	var availableCond metav1.Condition
	if available {
		availableCond = metav1.Condition{
			Type:               placementv1alpha1.PlacementBindingCondTypeAvailable,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: placementBinding.GetGeneration(),
			Reason:             placementv1alpha1.PlacementBindingAvailableCondReasonAllResourcesAvailable,
			Message:            "All resources are available in the target cluster",
		}
	} else {
		availableCond = metav1.Condition{
			Type:               placementv1alpha1.PlacementBindingCondTypeAvailable,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: placementBinding.GetGeneration(),
			Reason:             placementv1alpha1.PlacementBindingAvailableCondReasonSomeResourcesUnavailable,
			Message:            "Some resources might be unavailable in the target cluster",
		}
	}
	meta.SetStatusCondition(&placementBinding.GetStatus().Conditions, availableCond)
	return false
}

func countResourcesInWorksByProcessingResults(works []placementv1alpha1.Work) (
	total, synced, available int32,
	failed []placementv1alpha1.FailedResource,
) {
	for i := range works {
		work := &works[i]
		total += int32(len(work.Spec.Manifests)) //nolint:gosec // safe: there is no risk of overflowing due to API-level restrictions.
		for j := range work.Status.Manifests {
			manifest := &work.Status.Manifests[j]

			appliedCond := meta.FindStatusCondition(manifest.Conditions, placementv1alpha1.ManifestCondTypeApplied)
			// Note that the checks below do not take into account the condition's observed generation; this is
			// because for manifest conditions KubeFleet uses the generation of the actual manifest object
			// being applied, not the generation of the work object.
			switch {
			case appliedCond == nil:
				// The Applied condition has not been set yet; the manifest has not been processed.
				continue
			case appliedCond.Status != metav1.ConditionTrue:
				// The manifest has failed to be applied.
				failed = append(failed, failedResourceFromManifestStatus(manifest, appliedCond))
				continue
			default:
				// The manifest has been applied.
				synced++
			}

			availableCond := meta.FindStatusCondition(manifest.Conditions, placementv1alpha1.ManifestCondTypeAvailable)
			switch {
			case availableCond == nil:
				// The Available condition has not been set yet; the manifest has not been processed.
				continue
			case availableCond.Status != metav1.ConditionTrue:
				// The manifest is not yet available. We consider an applied manifest in a failed state if
				// it fails the availability check; see the work applier for the rules.
				failed = append(failed, failedResourceFromManifestStatus(manifest, availableCond))
				continue
			default:
				// The manifest is available.
				available++
			}
		}
	}
	return total, synced, available, failed
}

// failedResourceFromManifestStatus builds a FailedResource from a per-manifest status and the condition that
// is not true (nil if the condition is absent).
func failedResourceFromManifestStatus(manifest *placementv1alpha1.PerManifestStatus, falseCond *metav1.Condition) placementv1alpha1.FailedResource {
	failedResource := placementv1alpha1.FailedResource{
		ObjectRef: placementv1alpha1.ObjectReference{
			Namespace:  manifest.Identifier.Namespace,
			Name:       manifest.Identifier.Name,
			APIGroup:   manifest.Identifier.APIGroup,
			APIVersion: manifest.Identifier.APIVersion,
			Kind:       manifest.Identifier.Kind,
		},
		DiffDetails: manifest.DiffDetails,
	}
	if falseCond != nil {
		// Note that per KubeFleet API semantics, the observed generation set in the copied condition is the generation
		// of the actual manifest object being applied in the member cluster, not the generation of the work object
		// nor the placement binding object.
		failedResource.Conditions = []metav1.Condition{*falseCond}
	}
	return failedResource
}
