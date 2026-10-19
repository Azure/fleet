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
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

func claimWithRef(ref *kfplacementv1alpha1.ObjectReference) *kfplacementv1alpha1.ClusterClaim {
	return &kfplacementv1alpha1.ClusterClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim"},
		Spec:       kfplacementv1alpha1.ClusterClaimSpec{PlacementPolicyRef: ref},
	}
}

// TestMapClaimToPolicies pins the claim-to-policy mapping to spec.placementPolicyRef, which is
// the only identity that survives policy names longer than a label value can hold.
func TestMapClaimToPolicies(t *testing.T) {
	longName := strings.Repeat("x", 250)

	testCases := []struct {
		name              string
		obj               client.Object
		wantNamespaced    []reconcile.Request
		wantClusterScoped []reconcile.Request
	}{
		{
			name:           "namespaced policy reference",
			obj:            claimWithRef(&kfplacementv1alpha1.ObjectReference{Name: "app", Namespace: "tenant-a"}),
			wantNamespaced: []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "app", Namespace: "tenant-a"}}},
		},
		{
			name:              "cluster-scoped policy reference",
			obj:               claimWithRef(&kfplacementv1alpha1.ObjectReference{Name: "app"}),
			wantClusterScoped: []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "app"}}},
		},
		{
			name:           "name longer than a label value still maps",
			obj:            claimWithRef(&kfplacementv1alpha1.ObjectReference{Name: longName, Namespace: "tenant-a"}),
			wantNamespaced: []reconcile.Request{{NamespacedName: types.NamespacedName{Name: longName, Namespace: "tenant-a"}}},
		},
		{
			name: "claim without a policy reference maps to nothing",
			obj:  claimWithRef(nil),
		},
		{
			name: "reference without a name maps to nothing",
			obj:  claimWithRef(&kfplacementv1alpha1.ObjectReference{Namespace: "tenant-a"}),
		},
		{
			name: "object of another kind maps to nothing",
			obj:  &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "tenant-a"}},
		},
	}

	r := &Reconciler{}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotNamespaced := r.mapClaimToPlacementPolicy(context.Background(), tc.obj)
			if diff := cmp.Diff(gotNamespaced, tc.wantNamespaced); diff != "" {
				t.Errorf("mapClaimToPlacementPolicy() mismatch (-got, +want):\n%s", diff)
			}
			gotClusterScoped := r.mapClaimToClusterPlacementPolicy(context.Background(), tc.obj)
			if diff := cmp.Diff(gotClusterScoped, tc.wantClusterScoped); diff != "" {
				t.Errorf("mapClaimToClusterPlacementPolicy() mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestMemberClusterSchedulingRelevantChanges pins which member cluster updates reach the policy
// reconciler: heartbeat and observation timestamps must not, anything scheduling reads must.
func TestMemberClusterSchedulingRelevantChanges(t *testing.T) {
	now := metav1.Now()
	base := func() *clusterv1beta1.MemberCluster {
		return &clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "member", Labels: map[string]string{"region": "eastus"}},
			Spec:       clusterv1beta1.MemberClusterSpec{Taints: []clusterv1beta1.Taint{{Key: "k", Value: "v", Effect: corev1.TaintEffectNoSchedule}}},
			Status: clusterv1beta1.MemberClusterStatus{
				AgentStatus: []clusterv1beta1.AgentStatus{{
					Type:                  clusterv1beta1.MemberAgent,
					Conditions:            []metav1.Condition{{Type: string(clusterv1beta1.AgentJoined), Status: metav1.ConditionTrue, LastTransitionTime: now}},
					LastReceivedHeartbeat: now,
				}},
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{"node-count": {Value: "3", ObservationTime: now}},
			},
		}
	}

	testCases := []struct {
		name   string
		mutate func(*clusterv1beta1.MemberCluster)
		want   bool
	}{
		{name: "heartbeat only", mutate: func(mc *clusterv1beta1.MemberCluster) {
			mc.Status.AgentStatus[0].LastReceivedHeartbeat = metav1.NewTime(now.Add(time.Minute))
			mc.Status.Properties["node-count"] = clusterv1beta1.PropertyValue{Value: "3", ObservationTime: metav1.NewTime(now.Add(time.Minute))}
		}},
		{name: "label change", mutate: func(mc *clusterv1beta1.MemberCluster) { mc.Labels["region"] = "westus" }, want: true},
		{name: "taint change", mutate: func(mc *clusterv1beta1.MemberCluster) { mc.Spec.Taints = nil }, want: true},
		{name: "joined condition flips", mutate: func(mc *clusterv1beta1.MemberCluster) {
			mc.Status.AgentStatus[0].Conditions[0].Status = metav1.ConditionFalse
		}, want: true},
		{name: "property value change", mutate: func(mc *clusterv1beta1.MemberCluster) {
			mc.Status.Properties["node-count"] = clusterv1beta1.PropertyValue{Value: "4", ObservationTime: now}
		}, want: true},
		{name: "deletion starts", mutate: func(mc *clusterv1beta1.MemberCluster) { mc.DeletionTimestamp = &now }, want: true},
	}

	pred := memberClusterSchedulingRelevantChanges()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			updated := base()
			tc.mutate(updated)
			if got := pred.Update(event.UpdateEvent{ObjectOld: base(), ObjectNew: updated}); got != tc.want {
				t.Errorf("memberClusterSchedulingRelevantChanges().Update() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBindingRelevantChanges pins that a binding update wakes its policy when the spec or the
// rollout progress changes, and not when only a condition's message or transition time does.
func TestBindingRelevantChanges(t *testing.T) {
	base := func() *kfplacementv1alpha1.PlacementBinding {
		return &kfplacementv1alpha1.PlacementBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "b", Generation: 1},
			Status: kfplacementv1alpha1.PlacementBindingStatus{
				LastProcessedResourceSnapshotName: ptr.To("pp-snapshot-0"),
				Conditions: []metav1.Condition{
					{Type: kfplacementv1alpha1.PlacementBindingCondTypeSynchronized, Status: metav1.ConditionFalse, ObservedGeneration: 1, Reason: "Waiting", Message: "a"},
				},
			},
		}
	}
	testCases := []struct {
		name   string
		mutate func(*kfplacementv1alpha1.PlacementBinding)
		want   bool
	}{
		{name: "spec change", mutate: func(b *kfplacementv1alpha1.PlacementBinding) { b.Generation = 2 }, want: true},
		{name: "synchronized", mutate: func(b *kfplacementv1alpha1.PlacementBinding) { b.Status.Conditions[0].Status = metav1.ConditionTrue }, want: true},
		{name: "available reported", mutate: func(b *kfplacementv1alpha1.PlacementBinding) {
			b.Status.Conditions = append(b.Status.Conditions, metav1.Condition{Type: kfplacementv1alpha1.PlacementBindingCondTypeAvailable, Status: metav1.ConditionTrue, ObservedGeneration: 1})
		}, want: true},
		{name: "new revision processed", mutate: func(b *kfplacementv1alpha1.PlacementBinding) {
			b.Status.LastProcessedResourceSnapshotName = ptr.To("pp-snapshot-1")
		}, want: true},
		{name: "message only", mutate: func(b *kfplacementv1alpha1.PlacementBinding) { b.Status.Conditions[0].Message = "b" }, want: false},
		{name: "resource counts only", mutate: func(b *kfplacementv1alpha1.PlacementBinding) { b.Status.SelectedResources = ptr.To(int32(3)) }, want: false},
	}
	// Creates and deletes still wake the owner, so that a binding deleted out of band is recreated.
	if !bindingRelevantChanges().Create(event.CreateEvent{Object: base()}) || !bindingRelevantChanges().Delete(event.DeleteEvent{Object: base()}) {
		t.Errorf("bindingRelevantChanges() filtered out a create or a delete, want both admitted")
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			oldObj, newObj := base(), base()
			tc.mutate(newObj)
			if got := bindingRelevantChanges().Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}); got != tc.want {
				t.Errorf("bindingRelevantChanges().Update() = %t, want %t", got, tc.want)
			}
		})
	}
}
