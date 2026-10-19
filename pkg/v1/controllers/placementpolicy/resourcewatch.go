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

package placementpolicy

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/keys"
)

// EnqueuePoliciesSelecting re-queues the policies that may select the hub resource the key names,
// so that a change to it shows on their status (the latest resource revision going stale) without
// waiting for the periodic requeue. The resource watcher calls it for every resource change it
// sees, deletions included, which is why only the key is needed.
//
// A namespaced policy selects only from its own namespace; a cluster-scoped one selects from the
// namespace its selector names, or from every namespace (and the cluster scope) when it names none.
//
// ponytail: label selectors are not evaluated, so a change to any resource of a selected kind in
// scope re-queues the policy; the re-queue only re-checks the snapshot's hash, so this costs a
// reconcile, never a wrong result. Match the latest snapshot's resource identifiers if the
// fan-out shows up in profiles.
func (r *Reconciler) EnqueuePoliciesSelecting(ctx context.Context, key keys.ClusterWideKey) error {
	if key.Namespace != "" {
		policies := &kfplacementv1alpha1.PlacementPolicyList{}
		if err := r.List(ctx, policies, client.InNamespace(key.Namespace)); err != nil {
			return kferrors.NewAPIServerError(err, "failed to list the placement policies", true, "namespace", key.Namespace)
		}
		for i := range policies.Items {
			policy := &policies.Items[i]
			if maySelect(policy.Namespace, policy.Spec.ResourceSelectors, key) {
				klog.V(2).InfoS("A selected resource changed; re-queuing the placement policy", "placementPolicy", klog.KObj(policy), "resource", key)
				r.policySource.Add(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: policy.Namespace, Name: policy.Name}})
			}
		}
	}

	policies := &kfplacementv1alpha1.ClusterPlacementPolicyList{}
	if err := r.List(ctx, policies); err != nil {
		return kferrors.NewAPIServerError(err, "failed to list the cluster placement policies", true)
	}
	for i := range policies.Items {
		policy := &policies.Items[i]
		if maySelect("", policy.Spec.ResourceSelectors, key) {
			klog.V(2).InfoS("A selected resource changed; re-queuing the cluster placement policy", "clusterPlacementPolicy", klog.KObj(policy), "resource", key)
			r.clusterPolicySource.Add(reconcile.Request{NamespacedName: types.NamespacedName{Name: policy.Name}})
		}
	}
	return nil
}

// maySelect reports whether a policy in the given namespace ("" for a cluster-scoped policy) may
// select the resource the key names. The version is not compared: the resource watcher reports
// the preferred version, while a selector may name any served one.
func maySelect(policyNamespace string, selectors []kfplacementv1alpha1.ResourceSelector, key keys.ClusterWideKey) bool {
	for i := range selectors {
		selector := &selectors[i]
		if selector.APIGroup != key.Group || selector.Kind != key.Kind || (selector.Name != "" && selector.Name != key.Name) {
			continue
		}
		namespace := selector.Namespace
		if policyNamespace != "" {
			// A namespaced policy always selects from its own namespace.
			namespace = policyNamespace
		}
		if namespace == "" || namespace == key.Namespace {
			return true
		}
	}
	return false
}
