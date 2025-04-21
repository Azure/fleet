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

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1alpha1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/labels"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/resource"
)

// ClusterResourceReconciler reconciles a clusterResourceOverride object.
type ClusterResourceReconciler struct {
	Reconciler
}

// Reconcile triggers a single  reconcile round when scheduling policy has changed.
func (r *ClusterResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	name := req.NamespacedName
	clusterOverride := placementv1alpha1.ClusterResourceOverride{}
	overrideRef := klog.KRef(name.Namespace, name.Name)

	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "clusterResourceOverride", overrideRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "clusterResourceOverride", overrideRef, "latency", latency)
	}()

	if err := r.Client.Get(ctx, name, &clusterOverride); err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).InfoS("Ignoring notFound clusterResourceOverride", "clusterResourceOverride", overrideRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get clusterResourceOverride", "clusterResourceOverride", overrideRef)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}

	// Check if the clusterResourceOverride is being deleted
	if clusterOverride.DeletionTimestamp != nil {
		klog.V(4).InfoS("The clusterResourceOverride is being deleted", "clusterResourceOverride", overrideRef)
		return ctrl.Result{}, r.handleOverrideDeleting(ctx, &placementv1alpha1.ClusterResourceOverrideSnapshot{}, &clusterOverride)
	}

	// Ensure that we have the finalizer so we can delete all the related snapshots on cleanup
	if err := r.ensureFinalizer(ctx, &clusterOverride); err != nil {
		klog.ErrorS(err, "Failed to ensure the finalizer", "clusterResourceOverride", overrideRef)
		return ctrl.Result{}, err
	}

	// create or update the overrideSnapshot
	return ctrl.Result{}, r.ensureClusterResourceOverrideSnapshot(ctx, &clusterOverride, 10)
}

func (r *ClusterResourceReconciler) ensureClusterResourceOverrideSnapshot(ctx context.Context, cro *placementv1alpha1.ClusterResourceOverride, revisionHistoryLimit int) error {
	croKObj := klog.KObj(cro)
	overridePolicy := cro.Spec
	overrideSpecHash, err := resource.HashOf(overridePolicy)
	if err != nil {
		klog.ErrorS(err, "Failed to generate policy hash of clusterResourceOverride", "clusterResourceOverride", croKObj)
		return controller.NewUnexpectedBehaviorError(err)
	}
	// we need to list the snapshots anyway since we need to remove the extra snapshots if there are too many of them.
	snapshotList, err := r.listSortedOverrideSnapshots(ctx, cro)
	if err != nil {
		return err
	}
	// delete redundant snapshot revisions before creating a new snapshot to guarantee that the number of snapshots
	// won't exceed the limit.
	if err = r.removeExtraSnapshot(ctx, snapshotList, revisionHistoryLimit); err != nil {
		return err
	}

	latestSnapshotIndex := -1 //so index start at 0
	if len(snapshotList.Items) != 0 {
		// convert the last unstructured snapshot to the typed object
		latestSnapshot := &placementv1alpha1.ClusterResourceOverrideSnapshot{}
		if err = runtime.DefaultUnstructuredConverter.FromUnstructured(snapshotList.Items[len(snapshotList.Items)-1].Object, latestSnapshot); err != nil {
			klog.ErrorS(err, "Invalid overrideSnapshot", "clusterResourceOverride", croKObj, "overrideSnapshot", klog.KObj(&snapshotList.Items[len(snapshotList.Items)-1]))
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
				klog.ErrorS(err, "Failed to set the isLatestSnapshot label to false", "clusterResourceOverride", croKObj, "overrideSnapshot", klog.KObj(latestSnapshot))
				return controller.NewUpdateIgnoreConflictError(err)
			}
			klog.V(2).InfoS("Marked the latest overrideSnapshot as inactive", "clusterResourceOverride", croKObj, "overrideSnapshot", klog.KObj(latestSnapshot))
		}
		// we need to figure out the last snapshot index.
		latestSnapshotIndex, err = labels.ExtractIndex(latestSnapshot, placementv1alpha1.OverrideIndexLabel)
		if err != nil {
			klog.ErrorS(err, "Failed to parse the override index label", "clusterResourceOverride", croKObj, "overrideSnapshot", klog.KObj(latestSnapshot))
			return controller.NewUnexpectedBehaviorError(err)
		}
	}

	// Need to create new snapshot when 1) there is no snapshots or 2) the latest snapshot hash != current one.
	latestSnapshotIndex++
	newSnapshot := &placementv1alpha1.ClusterResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(placementv1alpha1.OverrideSnapshotNameFmt, cro.Name, latestSnapshotIndex),
			Labels: map[string]string{
				placementv1alpha1.OverrideTrackingLabel: cro.Name,
				placementv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
				placementv1alpha1.OverrideIndexLabel:    strconv.Itoa(latestSnapshotIndex),
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(cro, placementv1alpha1.GroupVersion.WithKind(placementv1alpha1.ClusterResourceOverrideKind)),
			},
		},
		Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
			OverrideSpec: overridePolicy,
			OverrideHash: []byte(overrideSpecHash),
		},
	}
	if err := r.Client.Create(ctx, newSnapshot); err != nil {
		klog.ErrorS(err, "Failed to create new overrideSnapshot", "newOverrideSnapshot", klog.KObj(newSnapshot))
		return controller.NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Created new overrideSnapshot", "clusterResourceOverride", croKObj, "newOverrideSnapshot", klog.KObj(newSnapshot))
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("clusterresourceoverride-controller").
		For(&placementv1alpha1.ClusterResourceOverride{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
