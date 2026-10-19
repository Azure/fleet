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
	"fmt"
	"maps"
	"slices"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
)

// resourceStatus summarizes, for the policy status, how far the policy's latest resource revision
// has reached its target clusters.
type resourceStatus struct {
	// latestRevision names the policy's latest resource snapshot.
	latestRevision string
	// current reports whether that snapshot still matches the selected resources on the hub. When
	// it does not, the resources have changed since and wait for a rollout to take a new snapshot.
	current bool
	// targets counts the clusters that should hold a binding, including a bound cluster kept while
	// it is unschedulable: it still holds the resources, so it still counts against the status.
	targets int32
	// synchronized counts the target clusters whose binding has the latest revision applied.
	synchronized int32
	// outdated counts the target clusters whose binding still names an older revision; the other
	// unsynchronized ones have the latest revision yet to apply, or no binding yet.
	outdated int32
	// available counts the synchronized target clusters whose resources also passed the
	// availability check; it never exceeds synchronized.
	available int32
}

// summarizeResourceStatus counts, among the bindings of the clusters that should hold one, those
// that have the latest revision synchronized and, of those, the ones whose resources are
// available. A binding is synchronized only when it names the latest revision, the work generator
// has processed that revision, and its Synchronized condition is true for the binding's current
// generation; a binding still rolling out an older revision, or one that has not been processed
// yet, is not. Nor is a binding being deleted, whose conditions outlive the removal of its
// resources, or one still carrying an older sync strategy than the policy's, whose conditions
// describe the old one. Availability is judged only for the latest revision, as placements judge
// it only once their resources are synchronized.
func summarizeResourceStatus(latestRevision string, current bool, syncStrategy *kfplacementv1alpha1.SyncStrategy, targets map[string][]int, bindings []kfplacementv1alpha1.PlacementBindingAccessor) resourceStatus {
	status := resourceStatus{latestRevision: latestRevision, current: current, targets: int32Len(slices.Collect(maps.Keys(targets)))}
	for _, binding := range bindings {
		if _, isTarget := targets[binding.GetSpec().ClusterName]; !isTarget {
			continue
		}
		if !binding.GetDeletionTimestamp().IsZero() || !apiequality.Semantic.DeepEqual(binding.GetSpec().SyncStrategy, syncStrategy) {
			continue
		}
		if binding.GetSpec().ResourceSnapshotName != latestRevision {
			status.outdated++
			continue
		}
		bindingStatus := binding.GetStatus()
		gen := binding.GetGeneration()
		processed := bindingStatus.LastProcessedResourceSnapshotName
		if processed == nil || *processed != latestRevision ||
			!condition.IsConditionStatusTrue(meta.FindStatusCondition(bindingStatus.Conditions, kfplacementv1alpha1.PlacementBindingCondTypeSynchronized), gen) {
			continue
		}
		status.synchronized++
		if condition.IsConditionStatusTrue(meta.FindStatusCondition(bindingStatus.Conditions, kfplacementv1alpha1.PlacementBindingCondTypeAvailable), gen) {
			status.available++
		}
	}
	return status
}

// synchronizedCondition builds the policy's Synchronized condition: true when the latest revision
// still matches the selected resources and every target cluster has it synchronized. Per the FEP,
// a resource change alone takes no snapshot; it waits for a rollout, and until then the condition
// reports ResourcesChangedPendingRollout. Otherwise a false condition reports
// SomeClustersOnOlderResourceRevision while any target cluster is still on an older revision, and
// FailedToSynchronizeSomeClusters while the rest have yet to apply the latest one. With no target
// cluster there is nothing to tell apart from the hub, so the condition is true.
func (s resourceStatus) synchronizedCondition(generation int64) metav1.Condition {
	cond := metav1.Condition{
		Type:               kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized,
		Status:             metav1.ConditionFalse,
		Reason:             kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonFailedToSynchronizeSomeClusters,
		ObservedGeneration: generation,
	}
	switch {
	case s.targets > 0 && !s.current:
		cond.Reason = kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout
		cond.Message = fmt.Sprintf("The selected resources have changed since resource revision %s and are waiting for a rollout to reach the target clusters", s.latestRevision)
	case s.synchronized < s.targets:
		if s.outdated > 0 {
			cond.Reason = kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonSomeClustersOnOlderResourceRevision
		}
		cond.Message = fmt.Sprintf("%d of %d target clusters have the latest resource revision %s synchronized (still on an older revision: %d, yet to apply the latest one: %d)",
			s.synchronized, s.targets, s.latestRevision, s.outdated, s.targets-s.synchronized-s.outdated)
	case s.targets == 0:
		cond.Status = metav1.ConditionTrue
		cond.Reason = kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonAllClustersSynchronized
		cond.Message = fmt.Sprintf("No cluster is targeted; the latest resource revision is %s", s.latestRevision)
	default:
		cond.Status = metav1.ConditionTrue
		cond.Reason = kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonAllClustersSynchronized
		cond.Message = fmt.Sprintf("All %d target clusters have the latest resource revision %s synchronized", s.targets, s.latestRevision)
	}
	return cond
}

// resourceCollectedCondition builds the policy's ResourceCollected condition: true when the selected
// resources were collected and the latest snapshot taken or found, whether or not it still matches
// them; false when they cannot be collected for good (until the user or the resources change).
func resourceCollectedCondition(generation int64, selectionErr error) metav1.Condition {
	if selectionErr != nil {
		return metav1.Condition{
			Type:               kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected,
			Status:             metav1.ConditionFalse,
			Reason:             kfplacementv1alpha1.PlacementPolicyResourceCollectedCondReasonFailedToCollectSomeResources,
			Message:            "The selected resources cannot be collected: " + selectionErr.Error(),
			ObservedGeneration: generation,
		}
	}
	return metav1.Condition{
		Type:               kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected,
		Status:             metav1.ConditionTrue,
		Reason:             kfplacementv1alpha1.PlacementPolicyResourceCollectedCondReasonAllResourcesCollected,
		Message:            "All selected resources have been collected",
		ObservedGeneration: generation,
	}
}

// selectionFailedCondition builds the policy's Synchronized condition for a round whose selected
// resources cannot be collected for good (until the user or the resources change): the last
// round's verdict no longer describes them. It is reported as FailedToSynchronizeSomeClusters; the
// ResourceCollected condition carries the cause.
func selectionFailedCondition(generation int64, err error) metav1.Condition {
	return metav1.Condition{
		Type:               kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized,
		Status:             metav1.ConditionFalse,
		Reason:             kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonFailedToSynchronizeSomeClusters,
		Message:            "The selected resources cannot be snapshotted: " + err.Error(),
		ObservedGeneration: generation,
	}
}

// availableCondition builds the policy's Available condition: true when every target cluster has
// the latest revision synchronized and its resources have passed the availability check.
func (s resourceStatus) availableCondition(generation int64) metav1.Condition {
	if s.available < s.targets {
		return metav1.Condition{
			Type:               kfplacementv1alpha1.PlacementPolicyCondTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             kfplacementv1alpha1.PlacementPolicyAvailableCondReasonSomeClustersUnavailable,
			Message:            fmt.Sprintf("The resources are available on %d of %d target clusters", s.available, s.targets),
			ObservedGeneration: generation,
		}
	}
	message := fmt.Sprintf("The resources are available on all %d target clusters", s.targets)
	if s.targets == 0 {
		message = "No cluster is targeted"
	}
	return metav1.Condition{
		Type:               kfplacementv1alpha1.PlacementPolicyCondTypeAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             kfplacementv1alpha1.PlacementPolicyAvailableCondReasonAllClustersAvailable,
		Message:            message,
		ObservedGeneration: generation,
	}
}
