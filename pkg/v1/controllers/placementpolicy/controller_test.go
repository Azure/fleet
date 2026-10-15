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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
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
	c := fake.NewClientBuilder().
		WithScheme(scheme).
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

	r := NewReconciler(c, c)
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
	r := NewReconciler(c, c)

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
