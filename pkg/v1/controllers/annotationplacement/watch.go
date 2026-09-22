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

package annotationplacement

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

const controllerName = "annotation-placement-controller"

// SourceQueue is how the resource watcher hands annotated resources to this controller.
//
// The annotated resources are of any kind the hub agent watches, which the manager's cache does not
// know ahead of time, so they cannot be watched through the manager and must be fed in instead.
// The feed is a source rather than a channel so that the producer is never blocked and repeated
// reports of one resource collapse: Add writes straight into the controller's own workqueue, which
// never blocks a writer and drops a request already queued for the same resource. A channel would
// push both problems onto the resource watcher, which has a whole fleet's events to deliver and no
// business waiting on this controller.
//
// Resources reported before the manager starts are held until it does, so the watcher does not have
// to know when the controller became ready. That buffer is unbounded and is only ever drained by
// the controller calling Start, so a producer must not outlive the manager it was created for: in
// the hub agent both are started together under the same leader election.
//
// The zero value is ready to use.
type SourceQueue struct {
	mu      sync.Mutex
	queue   workqueue.TypedRateLimitingInterface[Request]
	pending map[Request]struct{}
}

// Add reports an annotated resource. It never blocks, and it is safe to call from any goroutine
// and before the manager has started.
func (q *SourceQueue) Add(object client.Object) {
	req := RequestFor(object)
	if req.Kind == "" {
		klog.ErrorS(nil, "Skipped a resource that names no kind", "obj", klog.KObj(object))
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.queue == nil {
		if q.pending == nil {
			q.pending = make(map[Request]struct{})
		}
		// Collapsed the same way the controller's queue collapses, so a resource reported many
		// times before the manager started still costs one reconcile once it has.
		q.pending[req] = struct{}{}
		return
	}
	q.queue.Add(req)
}

// Start hands the queue to the controller. It is called by the controller, never directly.
func (q *SourceQueue) Start(_ context.Context, queue workqueue.TypedRateLimitingInterface[Request]) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.queue = queue
	for req := range q.pending {
		queue.Add(req)
	}
	q.pending = nil
	return nil
}

// SetupWithManager registers the controller with the manager.
//
// Annotated resources arrive through the given SourceQueue -- fed by the resource watcher, in the
// hub agent -- rather than from a watch of their own. The generated policies are watched through
// the manager, and an event on one enqueues the resource it was generated from: without that, an
// edit or a deletion of a generated policy produces no event on its resource, and for a resource
// that never changes again the policy would stay missing or wrong forever.
//
// The caller owns what the controller cannot decide for itself: the placement.kubefleet.dev types
// must be registered in the manager's scheme, and the hub agent must hold RBAC to read, create,
// update, and delete the two policy kinds.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, sources *SourceQueue) error {
	if sources == nil {
		// Reported here rather than left to panic once the manager starts the source: the caller
		// already checks this error, and a controller with nothing feeding it places nothing.
		return kferrors.NewUnexpectedError(nil, "the annotated resource source is nil", "controller", controllerName)
	}
	return builder.TypedControllerManagedBy[Request](mgr).
		Named(controllerName).
		WatchesRawSource(sources).
		Watches(
			&kfplacementv1alpha1.PlacementPolicy{},
			handler.TypedEnqueueRequestsFromMapFunc(mapGeneratedPolicyToSource),
			builder.WithPredicates(generatedPolicyDrift()),
		).
		Watches(
			&kfplacementv1alpha1.ClusterPlacementPolicy{},
			handler.TypedEnqueueRequestsFromMapFunc(mapGeneratedPolicyToSource),
			builder.WithPredicates(generatedPolicyDrift()),
		).
		Complete(r)
}

// generatedPolicyDrift admits the policy events that can mean a generated policy needs repair: a
// creation or deletion, and an update to the spec (which bumps the generation), the labels, or the
// owner references -- exactly what applyDesiredPolicy writes. Status updates are filtered out: the
// controllers that schedule a policy write its status on every change of the fleet, and each event
// let through here costs an uncached read of the annotated resource to learn that nothing drifted.
func generatedPolicyDrift() predicate.Predicate {
	ownerReferencesChanged := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return !equality.Semantic.DeepEqual(e.ObjectOld.GetOwnerReferences(), e.ObjectNew.GetOwnerReferences())
		},
	}
	return predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}, ownerReferencesChanged)
}

// mapGeneratedPolicyToSource enqueues the resource that generated a policy, identified from the
// policy's owner references.
//
// Only the owner whose identity reproduces this policy's own generated name is enqueued. A policy
// may carry more than one owner reference -- a foreign one that applyDesiredPolicy deliberately
// preserves, or any owner on a hand-authored policy that shares these watches -- and following
// those would enqueue a request for a kind the resource watcher does not track. Matching the
// generated name is exact, so only the true source passes.
//
// The owner reference is used rather than the parent labels because the labels are lossy (a long
// name is shortened to a prefix and a hash) and, being labels, can be stripped -- which is itself
// drift this watch exists to repair. On an update both the old and the new object are mapped, so an
// owner reference present only on the old side still enqueues the resource whose policy the update
// just took away from it.
func mapGeneratedPolicyToSource(_ context.Context, policy client.Object) []Request {
	// A generated policy always lives in the namespace of the resource it came from, and a
	// cluster-scoped one has none, matching a cluster-scoped owner.
	policyName, policyNamespace := policy.GetName(), policy.GetNamespace()
	for _, owner := range policy.GetOwnerReferences() {
		gv, err := schema.ParseGroupVersion(owner.APIVersion)
		if err != nil {
			klog.ErrorS(err, "Skipped an owner with an unparsable API version", "policy", klog.KObj(policy), "apiVersion", owner.APIVersion)
			continue
		}
		gvk := gv.WithKind(owner.Kind)
		if generatedPolicyName(gvk, policyNamespace, owner.Name) != policyName {
			// Not the owner this policy was generated from; following it would reach a resource
			// the watcher never selected.
			continue
		}
		return []Request{{
			GroupVersionKind: gvk,
			NamespacedName:   client.ObjectKey{Namespace: policyNamespace, Name: owner.Name},
		}}
	}
	return nil
}

// SourceNamespace returns the namespace whose skip-status governs whether a source resource is
// placed. For an ordinary namespaced resource that is its metadata.namespace; a core Namespace
// object carries none and is itself the namespace, so its own name is returned. The skip check
// would otherwise read an empty namespace for a Namespace source and place one KubeFleet excludes.
func SourceNamespace(source *unstructured.Unstructured) string {
	if gvk := source.GroupVersionKind(); gvk.Group == "" && gvk.Kind == "Namespace" {
		return source.GetName()
	}
	return source.GetNamespace()
}
