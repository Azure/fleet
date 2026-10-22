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

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

var (
	// placementPolicyEventHandlerFuncs handles events for both PlacementPolicy and ClusterPlacementPolicy objects.
	//
	// Policies whose binding manager is this controller are always enqueued so outstanding claims can be
	// cleaned up. Otherwise, updates must affect whether an enabled auto-rollout is needed or when it can happen.
	placementPolicyEventHandlerFuncs = &handler.TypedFuncs[client.Object, ctrl.Request]{
		// Handle create events to recover enabled policies and outstanding binding manager claims after a restart.
		CreateFunc: func(
			ctx context.Context,
			e event.TypedCreateEvent[client.Object],
			q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
			obj := e.Object
			policy, err := castToPlacementPolicy(obj)
			if err != nil {
				klog.ErrorS(err, "Failed to enqueue a placement policy create event", errors.Args(err)...)
				return
			}

			if !isAutoRolloutEnabled(policy) && !holdsBindingManagerRole(policy) {
				return
			}
			q.Add(ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: policy.GetNamespace(), Name: policy.GetName()},
			})
		},
		// Handle updates for outstanding claims, or relevant changes when auto-rollout is or was enabled.
		UpdateFunc: func(
			ctx context.Context,
			e event.TypedUpdateEvent[client.Object],
			q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
			oldPolicy, err := castToPlacementPolicy(e.ObjectOld)
			if err != nil {
				klog.ErrorS(err, "Failed to cast old placement policy in update event", errors.Args(err)...)
				return
			}
			newPolicy, err := castToPlacementPolicy(e.ObjectNew)
			if err != nil {
				klog.ErrorS(err, "Failed to cast new placement policy in update event", errors.Args(err)...)
				return
			}

			if !holdsBindingManagerRole(newPolicy) {
				if !isAutoRolloutEnabled(oldPolicy) && !isAutoRolloutEnabled(newPolicy) {
					return
				}
				if !autoRolloutRelevantChange(oldPolicy, newPolicy) {
					return
				}
			}

			q.Add(ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: newPolicy.GetNamespace(), Name: newPolicy.GetName()},
			})
		},
		// Deletions need no handling; the controller does not add finalizers to placement policies.
	}
)

// autoRolloutRelevantChange returns true if the update to the placement policy might change the outcome of an
// auto-rollout evaluation.
func autoRolloutRelevantChange(oldPolicy, newPolicy placementv1alpha1.PlacementPolicyAccessor) bool {
	if !oldPolicy.GetDeletionTimestamp().Equal(newPolicy.GetDeletionTimestamp()) {
		return true
	}

	// Check the annotations that control auto-rollout.
	for _, key := range []string{
		rolloutv1alpha1.AutoRolloutEnabledAnnotationKey,
		rolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey,
	} {
		if oldPolicy.GetAnnotations()[key] != newPolicy.GetAnnotations()[key] {
			return true
		}
	}

	// Check the conditions that determine whether a rollout is needed: enqueue if the new object has them in
	// the states that call for a rollout, and at least one of them differs from its older version.
	if !isRolloutNeeded(newPolicy) {
		return false
	}
	for _, condType := range []string{
		placementv1alpha1.PlacementPolicyCondTypeResourceCollected,
		placementv1alpha1.PlacementPolicyCondTypeSynchronized,
	} {
		oldCond := meta.FindStatusCondition(oldPolicy.GetStatus().Conditions, condType)
		// The new condition is guaranteed to exist, as a rollout is needed on the new object.
		newCond := meta.FindStatusCondition(newPolicy.GetStatus().Conditions, condType)
		if oldCond == nil ||
			oldCond.Status != newCond.Status ||
			oldCond.Reason != newCond.Reason ||
			oldCond.ObservedGeneration != newCond.ObservedGeneration {
			return true
		}
	}
	return false
}

func castToPlacementPolicy(obj client.Object) (placementv1alpha1.PlacementPolicyAccessor, error) {
	if obj.GetNamespace() == "" {
		policy, ok := obj.(*placementv1alpha1.ClusterPlacementPolicy)
		if !ok {
			return nil, errors.NewUnexpectedError(nil, "failed to cast object as a cluster placement policy", "gvk", obj.GetObjectKind().GroupVersionKind(), "obj", klog.KObj(obj))
		}
		return policy, nil
	}
	policy, ok := obj.(*placementv1alpha1.PlacementPolicy)
	if !ok {
		return nil, errors.NewUnexpectedError(nil, "failed to cast object as a placement policy", "gvk", obj.GetObjectKind().GroupVersionKind(), "obj", klog.KObj(obj))
	}
	return policy, nil
}
