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

package stagedupdaterun

import (
	"context"
	"sync/atomic"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

func (r *Reconciler) retrieveStagedUpdateRun(ctx context.Context, key types.NamespacedName) (rolloutv1alpha1.StagedUpdateRunAccessor, error) {
	var stagedUpdateRunAccessor rolloutv1alpha1.StagedUpdateRunAccessor
	var gvk schema.GroupVersionKind
	if key.Namespace == "" {
		// The object to retrieve is a ClusterStagedUpdateRun.
		stagedUpdateRunAccessor = &rolloutv1alpha1.ClusterStagedUpdateRun{}
		gvk = rolloutv1alpha1.GroupVersion.WithKind(rolloutv1alpha1.ClusterStagedUpdateRunKind)
	} else {
		// The object to retrieve is a StagedUpdateRun.
		stagedUpdateRunAccessor = &rolloutv1alpha1.StagedUpdateRun{}
		gvk = rolloutv1alpha1.GroupVersion.WithKind(rolloutv1alpha1.StagedUpdateRunKind)
	}

	if err := r.hubClient.Get(ctx, key, stagedUpdateRunAccessor); err != nil {
		return nil, errors.NewAPIServerError(err, "failed to get staged update run object", true)
	}
	// The controller-runtime client (and the informer cache backing it) does not populate TypeMeta
	// (apiVersion/kind) on typed Get calls, so the controller sets the GVK explicitly here. This ensures
	// that any code relying on stagedUpdateRunAccessor.GetObjectKind() afterwards (e.g., when building an
	// ObjectReference for the binding manager) sees a correctly populated Kind/APIVersion.
	stagedUpdateRunAccessor.GetObjectKind().SetGroupVersionKind(gvk)
	return stagedUpdateRunAccessor, nil
}

func (r *Reconciler) retrieveLinkedPlacementPolicy(
	ctx context.Context, stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor) (placementv1alpha1.PlacementPolicyAccessor, error) {
	placementPolicyName := stagedUpdateRun.GetSpec().PlacementPolicyName
	placementPolicyKey := types.NamespacedName{
		Namespace: stagedUpdateRun.GetNamespace(),
		Name:      placementPolicyName,
	}

	var placementPolicyAccessor placementv1alpha1.PlacementPolicyAccessor
	if placementPolicyKey.Namespace == "" {
		placementPolicyAccessor = &placementv1alpha1.ClusterPlacementPolicy{}
	} else {
		placementPolicyAccessor = &placementv1alpha1.PlacementPolicy{}
	}

	if err := r.hubClient.Get(ctx, placementPolicyKey, placementPolicyAccessor); err != nil {
		return nil, errors.NewAPIServerError(err, "failed to get placement policy object", true)
	}
	return placementPolicyAccessor, nil
}

func (r *Reconciler) retrieveStagedUpdateStrategy(
	ctx context.Context,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
) (rolloutv1alpha1.StagedUpdateStrategyAccessor, error) {
	strategyKey := types.NamespacedName{
		Namespace: stagedUpdateRun.GetNamespace(),
		Name:      stagedUpdateRun.GetSpec().StagedUpdateStrategyName,
	}

	var stagedUpdateStrategy rolloutv1alpha1.StagedUpdateStrategyAccessor
	if strategyKey.Namespace == "" {
		// The staged update run is cluster-scoped; retrieve the ClusterStagedUpdateStrategy.
		stagedUpdateStrategy = &rolloutv1alpha1.ClusterStagedUpdateStrategy{}
	} else {
		// The staged update run is namespaced; retrieve the StagedUpdateStrategy in its namespace.
		stagedUpdateStrategy = &rolloutv1alpha1.StagedUpdateStrategy{}
	}

	if err := r.hubClient.Get(ctx, strategyKey, stagedUpdateStrategy); err != nil {
		return nil, errors.NewAPIServerError(err, "failed to get staged update strategy object", true,
			"stagedUpdateStrategy", klog.KRef(strategyKey.Namespace, strategyKey.Name))
	}
	return stagedUpdateStrategy, nil
}

func (r *Reconciler) listAllPlacementBindings(
	ctx context.Context,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
) ([]placementv1alpha1.PlacementBindingAccessor, error) {
	// Bindings are matched by the placement policy name in their specs, as the owner label value might be truncated.
	ownerFieldSelector := client.MatchingFields{
		fieldindexers.PlacementBindingOwnedByCustomFieldName: placementPolicy.GetName(),
	}

	var placementBindings []placementv1alpha1.PlacementBindingAccessor
	if placementPolicy.GetNamespace() == "" {
		// The placement policy is cluster-scoped; list all cluster placement bindings owned by it.
		cpbs := &placementv1alpha1.ClusterPlacementBindingList{}
		if err := r.hubClient.List(ctx, cpbs, ownerFieldSelector); err != nil {
			return nil, errors.NewAPIServerError(err, "failed to list cluster placement bindings", true,
				"placementPolicy", klog.KRef(placementPolicy.GetNamespace(), placementPolicy.GetName()))
		}
		for i := range cpbs.Items {
			cpb := &cpbs.Items[i]
			if cpb.DeletionTimestamp.IsZero() {
				// Count only placement bindings that have not been marked for deletion.
				placementBindings = append(placementBindings, cpb)
			}
		}
	} else {
		// The placement policy is namespaced; list all placement bindings owned by it in its namespace.
		namespaceSelector := client.InNamespace(placementPolicy.GetNamespace())
		pbs := &placementv1alpha1.PlacementBindingList{}
		if err := r.hubClient.List(ctx, pbs, namespaceSelector, ownerFieldSelector); err != nil {
			return nil, errors.NewAPIServerError(err, "failed to list placement bindings", true,
				"placementPolicy", klog.KRef(placementPolicy.GetNamespace(), placementPolicy.GetName()))
		}
		for i := range pbs.Items {
			pb := &pbs.Items[i]
			if pb.DeletionTimestamp.IsZero() {
				// Count only placement bindings that have not been marked for deletion.
				placementBindings = append(placementBindings, pb)
			}
		}
	}
	return placementBindings, nil
}

func (r *Reconciler) retrieveReferencedClusters(
	ctx context.Context,
	placementBindings []placementv1alpha1.PlacementBindingAccessor,
) ([]*clusterv1beta1.MemberCluster, bool, error) {
	// Prepare a child context.
	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	// Prepare a flag that signals whether some of the member clusters could not be retrieved.
	someClustersNotRetrieved := atomic.Bool{}

	// Pre-allocate the slice to avoid contention.
	memberClusters := make([]*clusterv1beta1.MemberCluster, len(placementBindings))
	errs := make([]error, len(placementBindings))
	// Retrieve member cluster objects in parallel.
	doWork := func(pieces int) {
		clusterName := placementBindings[pieces].GetSpec().ClusterName
		memberCluster := &clusterv1beta1.MemberCluster{}
		if err := r.hubClient.Get(childCtx, types.NamespacedName{Name: clusterName}, memberCluster); err != nil {
			if apierrors.IsNotFound(err) {
				// The member cluster does not exist; skip it.
				klog.V(2).InfoS("Cannot find the member cluster referenced by a placement binding",
					"memberClusterName", clusterName, "placementBinding", klog.KObj(placementBindings[pieces]))
				someClustersNotRetrieved.Store(true)
				return
			}
			errs[pieces] = errors.NewAPIServerError(err, "failed to get member cluster object", true,
				"memberCluster", klog.KRef("", clusterName), "placementBinding", klog.KObj(placementBindings[pieces]))
			// Fail fast.
			childCancel()
			return
		}
		if !memberCluster.DeletionTimestamp.IsZero() {
			// The member cluster has been marked for deletion; skip it.
			klog.V(2).InfoS("The member cluster referenced by a placement binding has been marked for deletion; skip it",
				"memberCluster", klog.KObj(memberCluster), "placementBinding", klog.KObj(placementBindings[pieces]))
			someClustersNotRetrieved.Store(true)
			return
		}
		memberClusters[pieces] = memberCluster
	}
	r.parallelizer.ParallelizeUntil(childCtx, len(placementBindings), doWork, "retrieveReferencedClusters")

	if err := utilerrors.NewAggregate(errs); err != nil {
		return nil, someClustersNotRetrieved.Load(), errors.Wraps(err, "aggregated errors occurred when retrieving referenced clusters")
	}
	// Return the context error (nil if the context has not been cancelled) here to avoid nil cluster entries
	// being returned without errors.
	return memberClusters, someClustersNotRetrieved.Load(), ctx.Err()
}
