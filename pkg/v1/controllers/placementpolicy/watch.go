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

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

// memberClusterSchedulingView is a projection of a MemberCluster onto exactly the attributes
// that can change a placement policy's scheduling outcome. Attributes that churn on every
// member agent heartbeat without affecting scheduling — the heartbeat timestamp itself,
// property observation times, condition transition times — are deliberately excluded, so that
// heartbeat traffic does not fan out into policy reconciliations across the fleet.
type memberClusterSchedulingView struct {
	// The fields are exported so that the semantic deep-equality helper, which reflects over
	// field values, can access them; the type itself stays package-private.
	Labels      map[string]string
	Taints      []clusterv1beta1.Taint
	Terminating bool

	MemberAgentJoined  metav1.ConditionStatus
	MemberAgentHealthy metav1.ConditionStatus

	PropertyValues map[clusterv1beta1.PropertyName]string
	Capacity       corev1.ResourceList
	Allocatable    corev1.ResourceList
	Available      corev1.ResourceList
}

func schedulingViewOf(cluster *clusterv1beta1.MemberCluster) memberClusterSchedulingView {
	view := memberClusterSchedulingView{
		Labels:      cluster.Labels,
		Taints:      cluster.Spec.Taints,
		Terminating: !cluster.DeletionTimestamp.IsZero(),
		Capacity:    cluster.Status.ResourceUsage.Capacity,
		Allocatable: cluster.Status.ResourceUsage.Allocatable,
		Available:   cluster.Status.ResourceUsage.Available,
	}

	if joinedCond := cluster.GetAgentCondition(clusterv1beta1.MemberAgent, clusterv1beta1.AgentJoined); joinedCond != nil {
		view.MemberAgentJoined = joinedCond.Status
	}
	if healthyCond := cluster.GetAgentCondition(clusterv1beta1.MemberAgent, clusterv1beta1.AgentHealthy); healthyCond != nil {
		view.MemberAgentHealthy = healthyCond.Status
	}

	if len(cluster.Status.Properties) > 0 {
		view.PropertyValues = make(map[clusterv1beta1.PropertyName]string, len(cluster.Status.Properties))
		for name, value := range cluster.Status.Properties {
			view.PropertyValues[name] = value.Value
		}
	}
	return view
}

// memberClusterSchedulingRelevantChanges is a predicate that admits member cluster events only
// when they can affect scheduling outcomes; in particular, updates that only advance heartbeat
// or observation timestamps are filtered out.
func memberClusterSchedulingRelevantChanges() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		DeleteFunc: func(event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldCluster, oldOK := e.ObjectOld.(*clusterv1beta1.MemberCluster)
			newCluster, newOK := e.ObjectNew.(*clusterv1beta1.MemberCluster)
			if !oldOK || !newOK {
				// This should never happen as the predicate is registered on MemberCluster
				// watches only; admit the event out of caution.
				return true
			}
			return !apiequality.Semantic.DeepEqual(schedulingViewOf(oldCluster), schedulingViewOf(newCluster))
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// mapMemberClusterToPlacementPolicies enqueues every PlacementPolicy on a member cluster event.
//
// Cluster changes are not attributable to individual policies without evaluating every selector
// anyway, so all policies are re-enqueued; the scheduling-relevant-changes predicate keeps the
// event volume low.
func (r *Reconciler) mapMemberClusterToPlacementPolicies(ctx context.Context, _ client.Object) []reconcile.Request {
	policies := &kfplacementv1alpha1.PlacementPolicyList{}
	if err := r.List(ctx, policies); err != nil {
		klog.ErrorS(err, "Failed to list placement policies for member cluster mapping")
		return nil
	}
	requests := make([]reconcile.Request, 0, len(policies.Items))
	for i := range policies.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&policies.Items[i]),
		})
	}
	return requests
}

// mapClaimToPlacementPolicy resolves a cluster claim to its owning namespaced PlacementPolicy
// via its spec reference; claims of cluster-scoped policies are left to the
// ClusterPlacementPolicy controller's mapper.
func (r *Reconciler) mapClaimToPlacementPolicy(_ context.Context, obj client.Object) []reconcile.Request {
	// The spec reference, not the ownership labels, is the policy's authoritative identity: a
	// label value cannot hold a name longer than 63 bytes.
	ref := policyRefOf(obj)
	if ref == nil || ref.Name == "" || ref.Namespace == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}}}
}

// mapClaimToClusterPlacementPolicy resolves a cluster claim to its owning ClusterPlacementPolicy
// via its spec reference.
func (r *Reconciler) mapClaimToClusterPlacementPolicy(_ context.Context, obj client.Object) []reconcile.Request {
	ref := policyRefOf(obj)
	if ref == nil || ref.Name == "" || ref.Namespace != "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: ref.Name}}}
}

// policyRefOf returns the placement policy reference of a cluster claim, or nil for anything
// else; the claim watches this maps for are registered on ClusterClaim objects only.
func policyRefOf(obj client.Object) *kfplacementv1alpha1.ObjectReference {
	claim, ok := obj.(*kfplacementv1alpha1.ClusterClaim)
	if !ok {
		return nil
	}
	return claim.Spec.PlacementPolicyRef
}

// mapMemberClusterToClusterPlacementPolicies enqueues every ClusterPlacementPolicy on a member
// cluster event.
func (r *Reconciler) mapMemberClusterToClusterPlacementPolicies(ctx context.Context, _ client.Object) []reconcile.Request {
	policies := &kfplacementv1alpha1.ClusterPlacementPolicyList{}
	if err := r.List(ctx, policies); err != nil {
		klog.ErrorS(err, "Failed to list cluster placement policies for member cluster mapping")
		return nil
	}
	requests := make([]reconcile.Request, 0, len(policies.Items))
	for i := range policies.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&policies.Items[i]),
		})
	}
	return requests
}
