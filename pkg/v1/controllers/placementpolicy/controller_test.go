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
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

// TestReconcilePreservesClaimCountOnFailedRound pins that a claim round failing before it could
// count anything does not publish its partial count: a zero from a failed list would falsely
// report the policy's claims gone, on the status and on the metric derived from it.
func TestReconcilePreservesClaimCountOnFailedRound(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}

	policy := &kfplacementv1alpha1.PlacementPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "pp", Namespace: "default", Generation: 1},
		Spec: kfplacementv1alpha1.PlacementPolicySpec{
			ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{{
				Count:           ptr.To(intstr.FromInt32(1)),
				WhenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim,
			}},
			ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{{APIVersion: "v1", Kind: "ConfigMap", Name: "app"}},
		},
		Status: kfplacementv1alpha1.PlacementPolicyStatus{
			// The last completed round counted one active claim.
			ActiveClusterClaims: ptr.To(int32(1)),
		},
	}

	listErr := errors.New("the claim list is unwell")
	c := withIndexes(fake.NewClientBuilder().WithScheme(scheme)).
		WithObjects(policy).
		WithStatusSubresource(policy).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, isClaims := list.(*kfplacementv1alpha1.ClusterClaimList); isClaims {
					return listErr
				}
				return cl.List(ctx, list, opts...)
			},
		}).
		Build()

	r := NewReconciler(c, c, snapshotStub{})
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "pp"}})
	if !errors.Is(err, listErr) {
		t.Fatalf("Reconcile() = %v, want the claim list error %v so the round is retried", err, listErr)
	}

	fetched := &kfplacementv1alpha1.PlacementPolicy{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pp"}, fetched); err != nil {
		t.Fatalf("Get() = %v, want no error", err)
	}
	if fetched.Status.ActiveClusterClaims == nil || *fetched.Status.ActiveClusterClaims != 1 {
		t.Errorf("Reconcile() left activeClusterClaims = %v, want the last completed round's 1 preserved", fetched.Status.ActiveClusterClaims)
	}
}

// TestReconcileIgnoresDeletedPolicy pins that a policy that is already gone ends the round quietly.
// The read is wrapped as a categorized API server error, and IsNotFound has to keep resolving
// through that wrap: an error utility that flattened the cause instead would turn every deleted
// policy into an endless retry, with its metric series never forgotten.
func TestReconcileIgnoresDeletedPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := NewReconciler(c, c, snapshotStub{})

	for _, name := range []types.NamespacedName{{Namespace: "default", Name: "gone"}, {Name: "gone"}} {
		res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: name})
		if err != nil {
			t.Errorf("Reconcile(%v) = %v, want no error for a policy that no longer exists", name, err)
		}
		if res.RequeueAfter != 0 {
			t.Errorf("Reconcile(%v) requeued after %v, want no requeue for a policy that no longer exists", name, res.RequeueAfter)
		}
	}
}

// TestReconcileBacksOffWhileRoleIsHeld pins that a policy whose binding manager role another
// process holds is retried with a growing delay rather than at a fixed pace, and that a round
// that completes resets the delay.
func TestReconcileBacksOffWhileRoleIsHeld(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	policy := bindingManagerHeldBy("rollout-controller")
	cluster := testCluster("east-1", nil, nil, nil)
	c := withIndexes(fake.NewClientBuilder().WithScheme(scheme)).
		WithObjects(policy, cluster).
		WithStatusSubresource(policy).
		Build()
	r := NewReconciler(c, c, snapshotStub{})
	r.eligibility = allEligible{}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "pp"}}

	var delays []time.Duration
	for range 3 {
		res, err := r.Reconcile(context.Background(), req)
		if err != nil {
			t.Fatalf("Reconcile() = %v, want no error while the role is held elsewhere", err)
		}
		delays = append(delays, res.RequeueAfter)
	}
	wantDelays := []time.Duration{bindingManagerRetryBase, 2 * bindingManagerRetryBase, 4 * bindingManagerRetryBase}
	if diff := cmp.Diff(wantDelays, delays); diff != "" {
		t.Errorf("Reconcile() requeue delays mismatch (-want +got):\n%s", diff)
	}

	// The rollout finishes and releases the role; the round completes and the backoff resets.
	fetched := &kfplacementv1alpha1.PlacementPolicy{}
	if err := c.Get(context.Background(), req.NamespacedName, fetched); err != nil {
		t.Fatalf("Get() = %v, want no error", err)
	}
	fetched.Status.BindingManager = nil
	if err := c.Status().Update(context.Background(), fetched); err != nil {
		t.Fatalf("Status().Update() = %v, want no error", err)
	}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile() = %v, want no error once the role is free", err)
	}
	if got := r.roleBackoff.NumRequeues(req); got != 0 {
		t.Errorf("roleBackoff.NumRequeues() = %d after a completed round, want 0", got)
	}
}

// failingSnapshotter is a snapshot manager that cannot take or read any snapshot.
type failingSnapshotter struct{ err error }

func (f failingSnapshotter) SnapshotResourcesIfNoSnapshotExists(context.Context, kfplacementv1alpha1.PlacementPolicyAccessor) ([]kfplacementv1alpha1.PlacementResourceSnapshotAccessor, bool, error) {
	return nil, false, f.err
}

// TestReconcileSchedulesWithoutSnapshot pins that scheduling does not wait on resources: a round
// whose snapshot fails still unbinds a cluster that has left and writes the Scheduled status, and
// only then reports the snapshot error for a retry. A transient failure leaves the Synchronized
// and ResourceCollected conditions and the cluster counts as they were; a selected resource that
// is gone reports both conditions false, no cluster synchronized, and the availability the
// bindings report.
func TestReconcileSchedulesWithoutSnapshot(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	testCases := []struct {
		name              string
		snapshotErr       error
		wantSyncCond      metav1.ConditionStatus
		wantCollectedCond metav1.ConditionStatus
		wantCounts        *int32
	}{
		{
			name:              "transient failure",
			snapshotErr:       errors.New("the snapshot manager is unwell"),
			wantSyncCond:      metav1.ConditionTrue,
			wantCollectedCond: metav1.ConditionTrue,
		},
		{
			name: "selected resource gone",
			// Wrapped as the snapshot manager wraps a failed read of a resource selected by name.
			snapshotErr: kferrors.Wraps(kferrors.NewAPIServerError(apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "app"), "failed to get selected resource", true),
				"failed to retrieve selected resources (name-based selector)"),
			wantSyncCond:      metav1.ConditionFalse,
			wantCollectedCond: metav1.ConditionFalse,
			wantCounts:        ptr.To[int32](0),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			policy := bindingManagerHeldBy(controllerName)
			policy.Status.BindingManager = nil
			policy.Status.Conditions = []metav1.Condition{{
				Type:               kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized,
				Status:             metav1.ConditionTrue,
				Reason:             kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonAllClustersSynchronized,
				ObservedGeneration: policy.Generation,
			}, {
				Type:               kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected,
				Status:             metav1.ConditionTrue,
				Reason:             kfplacementv1alpha1.PlacementPolicyResourceCollectedCondReasonAllResourcesCollected,
				ObservedGeneration: policy.Generation,
			}}
			gone := &kfplacementv1alpha1.PlacementBinding{
				ObjectMeta: metav1.ObjectMeta{Name: bindingName(policy, "gone"), Namespace: "default", OwnerReferences: []metav1.OwnerReference{
					{APIVersion: kfplacementv1alpha1.GroupVersion.String(), Kind: kfplacementv1alpha1.PlacementPolicyKind, Name: "pp", UID: policy.UID, Controller: ptr.To(true)},
				}},
				Spec: kfplacementv1alpha1.PlacementBindingSpec{PlacementPolicyName: "pp", ClusterName: "gone", ResourceSnapshotName: "pp-snapshot-0"},
			}
			c := withIndexes(fake.NewClientBuilder().WithScheme(scheme)).
				WithObjects(policy, gone).
				WithStatusSubresource(policy).
				Build()
			r := NewReconciler(c, c, failingSnapshotter{err: tc.snapshotErr})
			r.eligibility = allEligible{}
			req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "pp"}}

			if _, err := r.Reconcile(context.Background(), req); !errors.Is(err, tc.snapshotErr) {
				t.Fatalf("Reconcile() = %v, want the snapshot error %v so the round is retried", err, tc.snapshotErr)
			}
			bindings := &kfplacementv1alpha1.PlacementBindingList{}
			if err := c.List(context.Background(), bindings); err != nil {
				t.Fatalf("List() = %v, want no error", err)
			}
			if len(bindings.Items) != 0 {
				t.Errorf("Reconcile() left %d bindings, want the binding of the cluster that left deleted", len(bindings.Items))
			}
			fetched := &kfplacementv1alpha1.PlacementPolicy{}
			if err := c.Get(context.Background(), req.NamespacedName, fetched); err != nil {
				t.Fatalf("Get() = %v, want no error", err)
			}
			if meta.FindStatusCondition(fetched.Status.Conditions, kfplacementv1alpha1.PlacementPolicyCondTypeScheduled) == nil {
				t.Errorf("Reconcile() wrote no Scheduled condition, want the scheduling status written")
			}
			if fetched.Status.LatestResourceRevisionName != nil {
				t.Errorf("Reconcile() set latestResourceRevisionName = %q, want it left unset without a snapshot", *fetched.Status.LatestResourceRevisionName)
			}
			if cond := meta.FindStatusCondition(fetched.Status.Conditions, kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized); cond == nil || cond.Status != tc.wantSyncCond {
				t.Errorf("Reconcile() left Synchronized condition %+v, want status %s", cond, tc.wantSyncCond)
			}
			var gotCollected metav1.ConditionStatus
			if cond := meta.FindStatusCondition(fetched.Status.Conditions, kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected); cond != nil {
				gotCollected = cond.Status
			}
			if gotCollected != tc.wantCollectedCond {
				t.Errorf("Reconcile() left ResourceCollected condition status %q, want %q", gotCollected, tc.wantCollectedCond)
			}
			if diff := cmp.Diff(tc.wantCounts, fetched.Status.SynchronizedClusters); diff != "" {
				t.Errorf("Reconcile() synchronizedClusters mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantCounts, fetched.Status.ResourcesAvailableClusters); diff != "" {
				t.Errorf("Reconcile() resourcesAvailableClusters mismatch (-want +got):\n%s", diff)
			}
			if gotAvailable := meta.FindStatusCondition(fetched.Status.Conditions, kfplacementv1alpha1.PlacementPolicyCondTypeAvailable) != nil; gotAvailable != (tc.wantCounts != nil) {
				t.Errorf("Reconcile() wrote an Available condition = %t, want %t", gotAvailable, tc.wantCounts != nil)
			}
		})
	}
}

// spec is fixed and the role would otherwise block the policy's rollouts.
func TestReconcileReleasesRoleOnInvalidSelectors(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	policy := bindingManagerHeldBy(controllerName)
	// Admitted by the API, but Gt cannot apply to a label.
	policy.Spec.ClusterSelectors = []kfplacementv1alpha1.ClusterSelector{{
		Terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
			MatchLabelExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
				{Key: "tier", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"1"}},
			},
		}},
	}}
	c := withIndexes(fake.NewClientBuilder().WithScheme(scheme)).WithObjects(policy).WithStatusSubresource(policy).Build()
	r := NewReconciler(c, c, snapshotStub{})
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "pp"}}

	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile() = %v, want no error", err)
	}
	fetched := &kfplacementv1alpha1.PlacementPolicy{}
	if err := c.Get(context.Background(), req.NamespacedName, fetched); err != nil {
		t.Fatalf("Get() = %v, want no error", err)
	}
	if fetched.Status.BindingManager != nil {
		t.Errorf("Reconcile() left the binding manager role as %v, want it released", fetched.Status.BindingManager)
	}
	if cond := meta.FindStatusCondition(fetched.Status.Conditions, kfplacementv1alpha1.PlacementPolicyCondTypeScheduled); cond == nil || cond.Reason != reasonInvalidClusterSelectors {
		t.Errorf("Reconcile() Scheduled condition = %v, want reason %q", cond, reasonInvalidClusterSelectors)
	}
}
