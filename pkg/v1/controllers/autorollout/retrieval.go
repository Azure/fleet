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

package autorollout

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

// retrievePlacementPolicy retrieves the PlacementPolicy or ClusterPlacementPolicy resource that
// the key points to and returns it as a PlacementPolicyAccessor interface.
//
// An empty namespace in the key means that the resource is a ClusterPlacementPolicy.
func (r *Reconciler) retrievePlacementPolicy(ctx context.Context, key types.NamespacedName) (placementv1alpha1.PlacementPolicyAccessor, error) {
	var placementPolicyAccessor placementv1alpha1.PlacementPolicyAccessor
	var gvk schema.GroupVersionKind
	if key.Namespace == "" {
		// The object to retrieve is a ClusterPlacementPolicy.
		placementPolicyAccessor = &placementv1alpha1.ClusterPlacementPolicy{}
		gvk = placementv1alpha1.GroupVersion.WithKind(placementv1alpha1.ClusterPlacementPolicyKind)
	} else {
		// The object to retrieve is a PlacementPolicy.
		placementPolicyAccessor = &placementv1alpha1.PlacementPolicy{}
		gvk = placementv1alpha1.GroupVersion.WithKind(placementv1alpha1.PlacementPolicyKind)
	}

	if err := r.hubClient.Get(ctx, key, placementPolicyAccessor); err != nil {
		return nil, errors.NewAPIServerError(err, "failed to get placement policy object", true)
	}
	// The controller-runtime client (and the informer cache backing it) does not populate TypeMeta
	// (apiVersion/kind) on typed Get calls, so the GVK is set explicitly here.
	placementPolicyAccessor.GetObjectKind().SetGroupVersionKind(gvk)
	return placementPolicyAccessor, nil
}

// listAllPlacementBindings lists all the placement bindings (or cluster placement bindings) owned by the
// placement policy and returns them as PlacementBindingAccessor interfaces. Bindings that have been marked for
// deletion are excluded.
func (r *Reconciler) listAllPlacementBindings(
	ctx context.Context,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
) ([]placementv1alpha1.PlacementBindingAccessor, error) {
	// List candidates by the policy name, then verify controller ownership to exclude bindings from an older policy instance.
	ownerFieldSelector := client.MatchingFields{
		fieldindexers.PlacementBindingOwnedByCustomFieldName: placementPolicy.GetName(),
	}

	var placementBindings []placementv1alpha1.PlacementBindingAccessor
	if placementPolicy.GetNamespace() == "" {
		// The placement policy is cluster-scoped; list all cluster placement bindings owned by it.
		cpbs := &placementv1alpha1.ClusterPlacementBindingList{}
		if err := r.hubClient.List(ctx, cpbs, ownerFieldSelector); err != nil {
			return nil, errors.NewAPIServerError(err, "failed to list cluster placement bindings", true,
				"placementPolicy", klog.KObj(placementPolicy))
		}
		for i := range cpbs.Items {
			cpb := &cpbs.Items[i]
			if cpb.DeletionTimestamp.IsZero() && metav1.IsControlledBy(cpb, placementPolicy) {
				placementBindings = append(placementBindings, cpb)
			}
		}
	} else {
		// The placement policy is namespaced; list all placement bindings owned by it in its namespace.
		namespaceSelector := client.InNamespace(placementPolicy.GetNamespace())
		pbs := &placementv1alpha1.PlacementBindingList{}
		if err := r.hubClient.List(ctx, pbs, namespaceSelector, ownerFieldSelector); err != nil {
			return nil, errors.NewAPIServerError(err, "failed to list placement bindings", true,
				"placementPolicy", klog.KObj(placementPolicy))
		}
		for i := range pbs.Items {
			pb := &pbs.Items[i]
			if pb.DeletionTimestamp.IsZero() && metav1.IsControlledBy(pb, placementPolicy) {
				placementBindings = append(placementBindings, pb)
			}
		}
	}
	return placementBindings, nil
}
