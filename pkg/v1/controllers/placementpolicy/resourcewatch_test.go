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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/keys"
)

func resourceKey(group, version, kind, namespace, name string) keys.ClusterWideKey {
	return keys.ClusterWideKey{ResourceIdentifier: placementv1beta1.ResourceIdentifier{
		Group: group, Version: version, Kind: kind, Namespace: namespace, Name: name,
	}}
}

func TestMaySelect(t *testing.T) {
	configMap := resourceKey("", "v1", "ConfigMap", "team-a", "app")
	testCases := []struct {
		name            string
		policyNamespace string
		selector        kfplacementv1alpha1.ResourceSelector
		key             keys.ClusterWideKey
		want            bool
	}{
		{name: "by name", policyNamespace: "team-a", selector: kfplacementv1alpha1.ResourceSelector{Kind: "ConfigMap", Name: "app"}, key: configMap, want: true},
		{name: "another name", policyNamespace: "team-a", selector: kfplacementv1alpha1.ResourceSelector{Kind: "ConfigMap", Name: "other"}, key: configMap},
		{name: "by label selector, not evaluated", policyNamespace: "team-a", selector: kfplacementv1alpha1.ResourceSelector{
			Kind: "ConfigMap", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}},
		}, key: configMap, want: true},
		{name: "another kind", policyNamespace: "team-a", selector: kfplacementv1alpha1.ResourceSelector{Kind: "Secret", Name: "app"}, key: configMap},
		{name: "another group", policyNamespace: "team-a", selector: kfplacementv1alpha1.ResourceSelector{APIGroup: "apps", Kind: "ConfigMap", Name: "app"}, key: configMap},
		{name: "version is not compared", selector: kfplacementv1alpha1.ResourceSelector{
			APIGroup: "autoscaling", APIVersion: "v1", Kind: "HorizontalPodAutoscaler", Name: "hpa",
		}, key: resourceKey("autoscaling", "v2", "HorizontalPodAutoscaler", "", "hpa"), want: true},
		{name: "namespaced policy ignores the selector namespace", policyNamespace: "team-a", selector: kfplacementv1alpha1.ResourceSelector{
			Kind: "ConfigMap", Name: "app", Namespace: "team-b",
		}, key: configMap, want: true},
		{name: "namespaced policy, another namespace", policyNamespace: "team-b", selector: kfplacementv1alpha1.ResourceSelector{Kind: "ConfigMap", Name: "app"}, key: configMap},
		{name: "namespaced policy, cluster-scoped resource", policyNamespace: "team-a", selector: kfplacementv1alpha1.ResourceSelector{Kind: "Namespace", Name: "team-a"},
			key: resourceKey("", "v1", "Namespace", "", "team-a")},
		{name: "cluster policy, selector namespace", selector: kfplacementv1alpha1.ResourceSelector{Kind: "ConfigMap", Name: "app", Namespace: "team-a"}, key: configMap, want: true},
		{name: "cluster policy, another selector namespace", selector: kfplacementv1alpha1.ResourceSelector{Kind: "ConfigMap", Name: "app", Namespace: "team-b"}, key: configMap},
		{name: "cluster policy, every namespace", selector: kfplacementv1alpha1.ResourceSelector{Kind: "ConfigMap", Name: "app"}, key: configMap, want: true},
		{name: "cluster policy, cluster-scoped resource", selector: kfplacementv1alpha1.ResourceSelector{Kind: "Namespace", Name: "team-a"},
			key: resourceKey("", "v1", "Namespace", "", "team-a"), want: true},
		{name: "a namespace selector does not cover its contents", selector: kfplacementv1alpha1.ResourceSelector{Kind: "Namespace", Name: "team-a"}, key: configMap},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := maySelect(tc.policyNamespace, []kfplacementv1alpha1.ResourceSelector{tc.selector}, tc.key); got != tc.want {
				t.Errorf("maySelect(%q, %+v, %v) = %t, want %t", tc.policyNamespace, tc.selector, tc.key, got, tc.want)
			}
		})
	}
}

// drain starts the source on a fresh queue and returns, sorted, the requests it hands over.
func drain(t *testing.T, start func(context.Context, workqueue.TypedRateLimitingInterface[reconcile.Request]) error) []reconcile.Request {
	t.Helper()
	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()
	if err := start(context.Background(), q); err != nil {
		t.Fatalf("Start() = %v, want no error", err)
	}
	var reqs []reconcile.Request
	for q.Len() > 0 {
		req, _ := q.Get()
		reqs = append(reqs, req)
		q.Done(req)
	}
	return reqs
}

func TestEnqueuePoliciesSelecting(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	selecting := []kfplacementv1alpha1.ResourceSelector{{Kind: "ConfigMap", Name: "app"}}
	other := []kfplacementv1alpha1.ResourceSelector{{Kind: "Secret", Name: "app"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&kfplacementv1alpha1.PlacementPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "selects"}, Spec: kfplacementv1alpha1.PlacementPolicySpec{ResourceSelectors: selecting}},
		&kfplacementv1alpha1.PlacementPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "other-kind"}, Spec: kfplacementv1alpha1.PlacementPolicySpec{ResourceSelectors: other}},
		&kfplacementv1alpha1.PlacementPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team-b", Name: "other-namespace"}, Spec: kfplacementv1alpha1.PlacementPolicySpec{ResourceSelectors: selecting}},
		&kfplacementv1alpha1.ClusterPlacementPolicy{ObjectMeta: metav1.ObjectMeta{Name: "fleet-wide"}, Spec: kfplacementv1alpha1.PlacementPolicySpec{ResourceSelectors: selecting}},
		&kfplacementv1alpha1.ClusterPlacementPolicy{ObjectMeta: metav1.ObjectMeta{Name: "other-kind"}, Spec: kfplacementv1alpha1.PlacementPolicySpec{ResourceSelectors: other}},
	).Build()
	r := NewReconciler(c, c, snapshotStub{})

	if err := r.EnqueuePoliciesSelecting(context.Background(), resourceKey("", "v1", "ConfigMap", "team-a", "app")); err != nil {
		t.Fatalf("EnqueuePoliciesSelecting() = %v, want no error", err)
	}
	sortReqs := cmpopts.SortSlices(func(a, b reconcile.Request) bool { return a.String() < b.String() })
	wantPolicies := []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "selects"}}}
	if diff := cmp.Diff(wantPolicies, drain(t, r.policySource.Start), sortReqs); diff != "" {
		t.Errorf("EnqueuePoliciesSelecting() re-queued placement policies mismatch (-want +got):\n%s", diff)
	}
	wantClusterPolicies := []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "fleet-wide"}}}
	if diff := cmp.Diff(wantClusterPolicies, drain(t, r.clusterPolicySource.Start), sortReqs); diff != "" {
		t.Errorf("EnqueuePoliciesSelecting() re-queued cluster placement policies mismatch (-want +got):\n%s", diff)
	}
}
