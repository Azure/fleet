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
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

// progressedBinding builds a binding of generation 2 to the cluster, rolling out the revision, with
// the given processed revision and condition statuses; an empty status leaves the condition out, and
// staleGen reports the conditions for the previous generation.
func progressedBinding(cluster, revision string, processed *string, synchronized, available metav1.ConditionStatus, staleGen bool) kfplacementv1alpha1.PlacementBindingAccessor {
	binding := &kfplacementv1alpha1.PlacementBinding{
		ObjectMeta: metav1.ObjectMeta{Name: cluster, Generation: 2},
		Spec:       kfplacementv1alpha1.PlacementBindingSpec{ClusterName: cluster, ResourceSnapshotName: revision},
		Status:     kfplacementv1alpha1.PlacementBindingStatus{LastProcessedResourceSnapshotName: processed},
	}
	gen := int64(2)
	if staleGen {
		gen = 1
	}
	for condType, status := range map[string]metav1.ConditionStatus{
		kfplacementv1alpha1.PlacementBindingCondTypeSynchronized: synchronized,
		kfplacementv1alpha1.PlacementBindingCondTypeAvailable:    available,
	} {
		if status != "" {
			binding.Status.Conditions = append(binding.Status.Conditions, metav1.Condition{Type: condType, Status: status, ObservedGeneration: gen, Reason: "Test"})
		}
	}
	return binding
}

func TestSummarizeResourceStatus(t *testing.T) {
	const latest, older = "pp-snapshot-1", "pp-snapshot-0"
	targets := map[string][]int{"done": {0}, "rolling": {0}, "unprocessed": {0}, "stale-gen": {0}, "failing": {0}, "no-binding-yet": {0}, "terminating": {0}, "old-strategy": {0}}
	terminating := progressedBinding("terminating", latest, ptr.To(latest), metav1.ConditionTrue, metav1.ConditionTrue, false)
	terminating.SetDeletionTimestamp(ptr.To(metav1.Now()))
	oldStrategy := progressedBinding("old-strategy", latest, ptr.To(latest), metav1.ConditionTrue, metav1.ConditionTrue, false)
	oldStrategy.GetSpec().SyncStrategy = &kfplacementv1alpha1.SyncStrategy{ApplyMethod: kfplacementv1alpha1.ApplyMethodServerSideApply}
	bindings := []kfplacementv1alpha1.PlacementBindingAccessor{
		progressedBinding("done", latest, ptr.To(latest), metav1.ConditionTrue, metav1.ConditionTrue, false),
		// Still on the older revision: available there, but not synchronized with the latest one.
		progressedBinding("rolling", older, ptr.To(older), metav1.ConditionTrue, metav1.ConditionTrue, false),
		// Moved to the latest revision, which the work generator has yet to process.
		progressedBinding("unprocessed", latest, ptr.To(older), metav1.ConditionTrue, metav1.ConditionTrue, false),
		progressedBinding("stale-gen", latest, ptr.To(latest), metav1.ConditionTrue, metav1.ConditionTrue, true),
		progressedBinding("failing", latest, ptr.To(latest), metav1.ConditionFalse, metav1.ConditionUnknown, false),
		// Not a target anymore: on its way out, and not counted at all.
		progressedBinding("leaving", latest, ptr.To(latest), metav1.ConditionTrue, metav1.ConditionTrue, false),
		// Being deleted: its conditions outlive the removal of its resources.
		terminating,
		// Not yet updated to the policy's sync strategy: its conditions describe the old one.
		oldStrategy,
	}

	got := summarizeResourceStatus(latest, true, nil, targets, bindings)
	want := resourceStatus{latestRevision: latest, current: true, targets: 8, synchronized: 1, outdated: 1, available: 1}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(resourceStatus{})); diff != "" {
		t.Errorf("summarizeResourceStatus() mismatch (-want +got):\n%s", diff)
	}
}

func TestResourceStatusConditions(t *testing.T) {
	testCases := []struct {
		name          string
		status        resourceStatus
		wantSync      metav1.Condition
		wantAvailable metav1.Condition
	}{
		{
			name:   "all target clusters synchronized and available",
			status: resourceStatus{latestRevision: "r1", current: true, targets: 2, synchronized: 2, available: 2},
			wantSync: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized, Status: metav1.ConditionTrue, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonAllClustersSynchronized,
				Message: "All 2 target clusters have the latest resource revision r1 synchronized",
			},
			wantAvailable: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeAvailable, Status: metav1.ConditionTrue, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicyAvailableCondReasonAllClustersAvailable,
				Message: "The resources are available on all 2 target clusters",
			},
		},
		{
			name:   "rollout in progress",
			status: resourceStatus{latestRevision: "r1", current: true, targets: 4, synchronized: 1, outdated: 2, available: 2},
			wantSync: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized, Status: metav1.ConditionFalse, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonSomeClustersOnOlderResourceRevision,
				Message: "1 of 4 target clusters have the latest resource revision r1 synchronized (still on an older revision: 2, yet to apply the latest one: 1)",
			},
			wantAvailable: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeAvailable, Status: metav1.ConditionFalse, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicyAvailableCondReasonSomeClustersUnavailable,
				Message: "The resources are available on 2 of 4 target clusters",
			},
		},
		{
			name:   "latest revision yet to apply on some target clusters",
			status: resourceStatus{latestRevision: "r1", current: true, targets: 2, synchronized: 1, available: 1},
			wantSync: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized, Status: metav1.ConditionFalse, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonFailedToSynchronizeSomeClusters,
				Message: "1 of 2 target clusters have the latest resource revision r1 synchronized (still on an older revision: 0, yet to apply the latest one: 1)",
			},
			wantAvailable: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeAvailable, Status: metav1.ConditionFalse, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicyAvailableCondReasonSomeClustersUnavailable,
				Message: "The resources are available on 1 of 2 target clusters",
			},
		},
		{
			name:   "resources changed on the hub since the latest revision",
			status: resourceStatus{latestRevision: "r1", current: false, targets: 2, synchronized: 2, available: 2},
			wantSync: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized, Status: metav1.ConditionFalse, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout,
				Message: "The selected resources have changed since resource revision r1 and are waiting for a rollout to reach the target clusters",
			},
			wantAvailable: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeAvailable, Status: metav1.ConditionTrue, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicyAvailableCondReasonAllClustersAvailable,
				Message: "The resources are available on all 2 target clusters",
			},
		},
		{
			name:   "resources changed while some target clusters are on an older revision",
			status: resourceStatus{latestRevision: "r1", current: false, targets: 2, synchronized: 1, outdated: 1, available: 1},
			wantSync: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized, Status: metav1.ConditionFalse, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonResourcesChangedPendingRollout,
				Message: "The selected resources have changed since resource revision r1 and are waiting for a rollout to reach the target clusters",
			},
			wantAvailable: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeAvailable, Status: metav1.ConditionFalse, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicyAvailableCondReasonSomeClustersUnavailable,
				Message: "The resources are available on 1 of 2 target clusters",
			},
		},
		{
			name:   "resources changed with no target clusters",
			status: resourceStatus{latestRevision: "r0", current: false},
			wantSync: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized, Status: metav1.ConditionTrue, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonAllClustersSynchronized,
				Message: "No cluster is targeted; the latest resource revision is r0",
			},
			wantAvailable: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeAvailable, Status: metav1.ConditionTrue, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicyAvailableCondReasonAllClustersAvailable,
				Message: "No cluster is targeted",
			},
		},
		{
			name:   "no target clusters",
			status: resourceStatus{latestRevision: "r0", current: true},
			wantSync: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized, Status: metav1.ConditionTrue, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonAllClustersSynchronized,
				Message: "No cluster is targeted; the latest resource revision is r0",
			},
			wantAvailable: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeAvailable, Status: metav1.ConditionTrue, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicyAvailableCondReasonAllClustersAvailable,
				Message: "No cluster is targeted",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.wantSync, tc.status.synchronizedCondition(3)); diff != "" {
				t.Errorf("synchronizedCondition() mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantAvailable, tc.status.availableCondition(3)); diff != "" {
				t.Errorf("availableCondition() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResourceCollectedCondition(t *testing.T) {
	testCases := []struct {
		name         string
		selectionErr error
		want         metav1.Condition
	}{
		{
			name: "collected",
			want: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected, Status: metav1.ConditionTrue, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicyResourceCollectedCondReasonAllResourcesCollected,
				Message: "All selected resources have been collected",
			},
		},
		{
			name:         "cannot be collected",
			selectionErr: errors.New("configmaps \"app\" not found"),
			want: metav1.Condition{
				Type: kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected, Status: metav1.ConditionFalse, ObservedGeneration: 3,
				Reason:  kfplacementv1alpha1.PlacementPolicyResourceCollectedCondReasonFailedToCollectSomeResources,
				Message: "The selected resources cannot be collected: configmaps \"app\" not found",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, resourceCollectedCondition(3, tc.selectionErr)); diff != "" {
				t.Errorf("resourceCollectedCondition() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
