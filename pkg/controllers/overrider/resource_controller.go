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
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/labels"
	"go.goms.io/fleet/pkg/utils/resource"
)

// ResourceReconciler reconciles a ResourceOverride object.
type ResourceReconciler struct {
	Reconciler
}

// Reconcile triggers a single  reconcile round when scheduling policy has changed.
func (r *ResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	name := req.NamespacedName
	resourceOverride := placementv1alpha1.ResourceOverride{}
	overrideRef := klog.KRef(name.Namespace, name.Name)

	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "resourceOverride", overrideRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "resourceOverride", overrideRef, "latency", latency)
	}()

	if err := r.Client.Get(ctx, name, &resourceOverride); err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).InfoS("Ignoring notFound resourceOverride", "resourceOverride", overrideRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get resourceOverride", "resourceOverride", overrideRef)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}

	// Check if the resourceOverride is being deleted
	if resourceOverride.DeletionTimestamp != nil {
		klog.V(4).InfoS("The resourceOverride is being deleted", "resourceOverride", overrideRef)
		return ctrl.Result{}, r.handleOverrideDeleting(ctx, &placementv1alpha1.ResourceOverrideSnapshot{}, &resourceOverride)
	}

	// Ensure that we have the finalizer so we can delete all the related snapshots on cleanup
	err := r.ensureFinalizer(ctx, &resourceOverride)
	if err != nil {
		klog.ErrorS(err, "Failed to ensure the finalizer", "resourceOverride", overrideRef)
		return ctrl.Result{}, err
	}

	// create or update the overrideSnapshot
	return ctrl.Result{}, r.ensureResourceOverrideSnapshot(ctx, &resourceOverride, 10)
}

func (r *ResourceReconciler) ensureResourceOverrideSnapshot(ctx context.Context, ro *placementv1alpha1.ResourceOverride, revisionHistoryLimit int) error {
	croKObj := klog.KObj(ro)
	overridePolicy := ro.Spec
	overrideSpecHash, err := resource.HashOf(overridePolicy)
	if err != nil {
		klog.ErrorS(err, "Failed to generate policy hash of ResourceOverride", "ResourceOverride", croKObj)
		return controller.NewUnexpectedBehaviorError(err)
	}
	// we need to list the snapshots anyway since we need to remove the extra snapshots if there are too many of them.
	snapshotList, err := r.listSortedOverrideSnapshots(ctx, ro)
	if err != nil {
		return err
	}
	// delete redundant snapshot revisions before creating a new snapshot to guarantee that the number of snapshots won't exceed the limit.
	if err = r.removeExtraSnapshot(ctx, snapshotList, revisionHistoryLimit); err != nil {
		return err
	}

	latestSnapshotIndex := -1
	if len(snapshotList.Items) != 0 {
		// convert the last unstructured snapshot to the typed object
		latestSnapshot := &placementv1alpha1.ResourceOverrideSnapshot{}
		if err = runtime.DefaultUnstructuredConverter.FromUnstructured(snapshotList.Items[len(snapshotList.Items)-1].Object, latestSnapshot); err != nil {
			klog.ErrorS(err, "Invalid overrideSnapshot", "ResourceOverride", croKObj, "overrideSnapshot", klog.KObj(&snapshotList.Items[len(snapshotList.Items)-1]))
			return controller.NewUnexpectedBehaviorError(err)
		}
		if string(latestSnapshot.Spec.OverrideHash) == overrideSpecHash {
			// the content has not changed, so we don't need to create a new snapshot.
			return r.ensureSnapshotLatest(ctx, latestSnapshot)
		}
		// mark the last policy snapshot as inactive if it is different from what we have now.
		if latestSnapshot.Labels[placementv1beta1.IsLatestSnapshotLabel] == strconv.FormatBool(true) {
			// set the latest label to false first to make sure there is only one or none active policy snapshot
			latestSnapshot.Labels[placementv1beta1.IsLatestSnapshotLabel] = strconv.FormatBool(false)
			if err := r.Client.Update(ctx, latestSnapshot); err != nil {
				klog.ErrorS(err, "Failed to set the isLatestSnapshot label to false", "ResourceOverride", croKObj, "overrideSnapshot", klog.KObj(latestSnapshot))
				return controller.NewUpdateIgnoreConflictError(err)
			}
			klog.V(2).InfoS("Marked the latest overrideSnapshot as inactive", "ResourceOverride", croKObj, "overrideSnapshot", klog.KObj(latestSnapshot))
		}
		// we need to figure out the last snapshot index.
		latestSnapshotIndex, err = labels.ExtractIndex(latestSnapshot, placementv1alpha1.OverrideIndexLabel)
		if err != nil {
			klog.ErrorS(err, "Failed to parse the override index label", "ResourceOverride", croKObj, "overrideSnapshot", klog.KObj(latestSnapshot))
			return controller.NewUnexpectedBehaviorError(err)
		}
	}

	// Need to create new snapshot when 1) there is no snapshots or 2) the latest snapshot hash != current one.
	latestSnapshotIndex++
	newSnapshot := &placementv1alpha1.ResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, ro.Name, latestSnapshotIndex),
			Namespace: ro.Namespace,
			Labels: map[string]string{
				placementv1alpha1.OverrideTrackingLabel: ro.Name,
				placementv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
				placementv1alpha1.OverrideIndexLabel:    strconv.Itoa(latestSnapshotIndex),
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(ro, placementv1alpha1.GroupVersion.WithKind(placementv1alpha1.ResourceOverrideKind)),
			},
		},
		Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
			OverrideSpec: overridePolicy,
			OverrideHash: []byte(overrideSpecHash),
		},
	}
	if err = r.Client.Create(ctx, newSnapshot); err != nil {
		klog.ErrorS(err, "Failed to create new overrideSnapshot", "newOverrideSnapshot", klog.KObj(newSnapshot))
		return controller.NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Created a new overrideSnapshot", "ResourceOverride", croKObj, "newOverrideSnapshot", klog.KObj(newSnapshot))
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("resourceoverride-controller").
		For(&placementv1alpha1.ResourceOverride{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
