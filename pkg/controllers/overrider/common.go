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

// Package overrider features controllers to reconcile the override objects.
package overrider

import (
	"context"
	"sort"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/labels"
)

// Reconciler reconciles a clusterResourceOverride object.
type Reconciler struct {
	// Client is used to update objects which goes to the api server directly.
	client.Client
}

// handleOverrideDeleting handles the delete event of an override object. We need to delete all the related override Snapshot.
func (r *Reconciler) handleOverrideDeleting(ctx context.Context, overrideSnapshotObj, parentOverrideObj client.Object) error {
	overrideRef := klog.KObj(parentOverrideObj)
	if !controllerutil.ContainsFinalizer(parentOverrideObj, placementv1alpha1.OverrideFinalizer) {
		klog.V(2).InfoS("No need to do anything for the deleting override without a finalizer", "override", overrideRef)
		return nil
	}
	// delete all the associated snapshots
	if err := r.Client.DeleteAllOf(ctx, overrideSnapshotObj, client.InNamespace(parentOverrideObj.GetNamespace()), client.MatchingLabels{placementv1alpha1.OverrideTrackingLabel: parentOverrideObj.GetName()}); err != nil {
		klog.ErrorS(err, "Failed to delete all associated overrideSnapshot", "override", overrideRef)
		return controller.NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Deleted all overrideSnapshot associated with the override", "overrideSnapshot", klog.KObj(overrideSnapshotObj), "override", overrideRef)

	controllerutil.RemoveFinalizer(parentOverrideObj, placementv1alpha1.OverrideFinalizer)
	if err := r.Client.Update(ctx, parentOverrideObj); err != nil {
		klog.ErrorS(err, "Failed to remove crp finalizer", "override", overrideRef)
		return controller.NewUpdateIgnoreConflictError(err)
	}
	return nil
}

// ensureFinalizer ensures that the finalizer is added to the override object.
func (r *Reconciler) ensureFinalizer(ctx context.Context, parentOverrideObj client.Object) error {
	if !controllerutil.ContainsFinalizer(parentOverrideObj, placementv1alpha1.OverrideFinalizer) {
		klog.V(4).InfoS("add the override finalizer", "override", klog.KObj(parentOverrideObj))
		controllerutil.AddFinalizer(parentOverrideObj, placementv1alpha1.OverrideFinalizer)
		return controller.NewUpdateIgnoreConflictError(r.Update(ctx, parentOverrideObj, client.FieldOwner(utils.OverrideControllerFieldManagerName)))
	}
	return nil
}

// listSortedOverrideSnapshots returns the override snapshots sorted by the override index. This is only needed if we can't find any latest snapshot.
func (r *Reconciler) listSortedOverrideSnapshots(ctx context.Context, parentOverrideObj client.Object) (*unstructured.UnstructuredList, error) {
	parentOverrideRef := klog.KObj(parentOverrideObj)
	snapshotList := &unstructured.UnstructuredList{}
	var snapshotListGVK schema.GroupVersionKind
	if parentOverrideObj.GetObjectKind().GroupVersionKind().Kind == placementv1alpha1.ClusterResourceOverrideKind {
		snapshotListGVK = utils.ClusterResourceOverrideSnapshotKind
	} else {
		snapshotListGVK = utils.ResourceOverrideSnapshotKind
	}
	snapshotList.SetGroupVersionKind(snapshotListGVK)
	if err := r.Client.List(ctx, snapshotList, client.InNamespace(parentOverrideObj.GetNamespace()), client.MatchingLabels{placementv1alpha1.OverrideTrackingLabel: parentOverrideObj.GetName()}); err != nil {
		klog.ErrorS(err, "Failed to list all overrideSnapshot", "snapshotListGVK", snapshotListGVK, "parentOverride", parentOverrideRef)
		return nil, controller.NewAPIServerError(false, err)
	}
	var errs []error
	sort.Slice(snapshotList.Items, func(i, j int) bool {
		ii, err := labels.ExtractIndex(&snapshotList.Items[i], placementv1alpha1.OverrideIndexLabel)
		if err != nil {
			klog.ErrorS(err, "Failed to parse the override index label", "snapshotListGVK", snapshotListGVK, "parentOverride", parentOverrideRef, "overrideSnapshot", klog.KObj(&snapshotList.Items[i]))
			errs = append(errs, err)
		}
		ji, err := labels.ExtractIndex(&snapshotList.Items[j], placementv1alpha1.OverrideIndexLabel)
		if err != nil {
			klog.ErrorS(err, "Failed to parse the override index label", "snapshotListGVK", snapshotListGVK, "parentOverride", parentOverrideRef, "overrideSnapshot", klog.KObj(&snapshotList.Items[j]))
			errs = append(errs, err)
		}
		return ii < ji
	})

	if len(errs) > 0 {
		return nil, controller.NewUnexpectedBehaviorError(utilerrors.NewAggregate(errs))
	}

	return snapshotList, nil
}

func (r *Reconciler) removeExtraSnapshot(ctx context.Context, sortedSnapshotList *unstructured.UnstructuredList, limit int) error {
	// the list is sorted by the override index, so we can just remove from the beginning
	for i := 0; i <= len(sortedSnapshotList.Items)-limit; i++ {
		if err := r.Client.Delete(ctx, &sortedSnapshotList.Items[i]); err != nil {
			if !apierrors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to delete the extra override snapshot", "overrideSnapshot", klog.KObj(&sortedSnapshotList.Items[i]))
				return controller.NewAPIServerError(false, err)
			}
		}
		klog.V(2).InfoS("Deleted the extra override snapshot", "overrideSnapshot", klog.KObj(&sortedSnapshotList.Items[i]))
	}
	return nil
}

func (r *Reconciler) ensureSnapshotLatest(ctx context.Context, latestSnapshot client.Object) error {
	if latestSnapshot.GetLabels()[placementv1beta1.IsLatestSnapshotLabel] == strconv.FormatBool(true) {
		klog.V(2).InfoS("Policy has not changed", "overrideSnapshot", klog.KObj(latestSnapshot))
		return nil
	}
	// set the latest label to be true first to make sure there is only one or none active policy snapshot.
	labels := latestSnapshot.GetLabels()
	labels[placementv1beta1.IsLatestSnapshotLabel] = strconv.FormatBool(true)
	latestSnapshot.SetLabels(labels)
	if err := r.Client.Update(ctx, latestSnapshot); err != nil {
		klog.ErrorS(err, "Failed to set the isLatestSnapshot label to false", "overrideSnapshot", klog.KObj(latestSnapshot))
		return controller.NewUpdateIgnoreConflictError(err)
	}
	return nil
}
