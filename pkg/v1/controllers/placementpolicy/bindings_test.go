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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

// uniformClusters builds a cluster inventory in which every cluster ranks the same, so that the
// ranking falls back to name order and the tests can reason about it.
func uniformClusters(names ...string) map[string]*clusterv1beta1.MemberCluster {
	clusters := make(map[string]*clusterv1beta1.MemberCluster, len(names))
	for _, name := range names {
		clusters[name] = rankedCluster(name, "3", "4", "8Gi")
	}
	return clusters
}

// boundSince builds the bound-cluster map chooseClusters takes, with each cluster bound the
// given number of minutes ago.
func boundSince(minutesAgo map[string]int) map[string]metav1.Time {
	now := time.Now()
	bound := make(map[string]metav1.Time, len(minutesAgo))
	for name, minutes := range minutesAgo {
		bound[name] = metav1.NewTime(now.Add(-time.Duration(minutes) * time.Minute))
	}
	return bound
}

// TestChooseClusters pins the sticky selection: what a fresh policy picks, what a bound cluster
// keeps through ranking changes and outages, how a lowered count trims, and how one cluster
// serves several selectors.
func TestChooseClusters(t *testing.T) {
	testCases := []struct {
		name        string
		outcomes    []selectorOutcome
		bound       map[string]metav1.Time
		clusters    map[string]*clusterv1beta1.MemberCluster
		wantDesired map[string][]int
		wantChosen  [][]string
	}{
		{
			name: "fresh policy picks the best-ranked clusters up to the count",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 1}, matched: []string{"fat", "slim"}},
			},
			clusters: map[string]*clusterv1beta1.MemberCluster{
				"fat":  rankedCluster("fat", "10", "4", "8Gi"),
				"slim": rankedCluster("slim", "2", "4", "8Gi"),
			},
			wantDesired: map[string][]int{"slim": {0}},
			wantChosen:  [][]string{{"slim"}},
		},
		{
			name: "a bound cluster sticks ahead of a better-ranked one",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 1}, matched: []string{"fat", "slim"}},
			},
			bound: boundSince(map[string]int{"fat": 5}),
			clusters: map[string]*clusterv1beta1.MemberCluster{
				"fat":  rankedCluster("fat", "10", "4", "8Gi"),
				"slim": rankedCluster("slim", "2", "4", "8Gi"),
			},
			wantDesired: map[string][]int{"fat": {0}},
			wantChosen:  [][]string{{"fat"}},
		},
		{
			name: "a bound cluster that went unschedulable sticks but does not count",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 2}, matched: []string{"a", "b"}, sticky: []string{"dark"}},
			},
			bound:       boundSince(map[string]int{"dark": 5}),
			clusters:    uniformClusters("a", "b", "dark"),
			wantDesired: map[string][]int{"a": {0}, "b": {0}, "dark": {0}},
			wantChosen:  [][]string{{"a", "b"}},
		},
		{
			name: "a bound cluster whose data cannot be evaluated sticks the same way",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 1}, matched: []string{"a"}, sticky: []string{"garbled"}},
			},
			bound:       boundSince(map[string]int{"garbled": 5}),
			clusters:    uniformClusters("a", "garbled"),
			wantDesired: map[string][]int{"a": {0}, "garbled": {0}},
			wantChosen:  [][]string{{"a"}},
		},
		{
			name: "an unbound unschedulable cluster is never picked",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 2}, matched: []string{"a"}, sticky: []string{"dark"}},
			},
			clusters:    uniformClusters("a", "dark"),
			wantDesired: map[string][]int{"a": {0}},
			wantChosen:  [][]string{{"a"}},
		},
		{
			name: "a bound cluster that matches nothing anymore is dropped",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 1}, matched: []string{"a"}},
			},
			bound:       boundSince(map[string]int{"gone": 5, "relabeled": 5}),
			clusters:    uniformClusters("a", "relabeled"),
			wantDesired: map[string][]int{"a": {0}},
			wantChosen:  [][]string{{"a"}},
		},
		{
			name: "a lowered count drops the newest bindings first",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 1}, matched: []string{"newest", "old", "older"}},
			},
			bound:       boundSince(map[string]int{"older": 30, "old": 20, "newest": 10}),
			clusters:    uniformClusters("newest", "old", "older"),
			wantDesired: map[string][]int{"older": {0}},
			wantChosen:  [][]string{{"older"}},
		},
		{
			name: "a lowered count drops a cluster that no longer matches before the newer matching ones",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 3}, matched: []string{"a", "b", "c", "d", "drifted"}, stale: []string{"drifted"}},
			},
			bound:       boundSince(map[string]int{"drifted": 50, "a": 40, "b": 30, "c": 20, "d": 10}),
			clusters:    uniformClusters("a", "b", "c", "d", "drifted"),
			wantDesired: map[string][]int{"a": {0}, "b": {0}, "c": {0}},
			wantChosen:  [][]string{{"a", "b", "c"}},
		},
		{
			name: "a cluster that no longer matches and went unschedulable sticks but does not count",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 1}, matched: []string{"a", "b"}, sticky: []string{"drifted-dark"}, stale: []string{"drifted-dark"}},
			},
			bound:       boundSince(map[string]int{"drifted-dark": 50, "a": 40, "b": 30}),
			clusters:    uniformClusters("a", "b", "drifted-dark"),
			wantDesired: map[string][]int{"a": {0}, "drifted-dark": {0}},
			wantChosen:  [][]string{{"a"}},
		},
		{
			name: "a cluster back from an outage keeps its binding and the replacement is trimmed",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 2}, matched: []string{"back", "kept", "replacement"}},
			},
			bound:       boundSince(map[string]int{"back": 30, "kept": 30, "replacement": 5}),
			clusters:    uniformClusters("back", "kept", "replacement"),
			wantDesired: map[string][]int{"back": {0}, "kept": {0}},
			wantChosen:  [][]string{{"back", "kept"}},
		},
		{
			name: "one cluster satisfies two selectors",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 1}, matched: []string{"a", "b"}},
				{counts: resolvedCounts{desired: 1}, matched: []string{"a", "c"}},
			},
			clusters:    uniformClusters("a", "b", "c"),
			wantDesired: map[string][]int{"a": {0, 1}},
			wantChosen:  [][]string{{"a"}, {"a"}},
		},
		{
			name: "a cluster trimmed by one selector may still serve another",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 1}, matched: []string{"a", "b"}},
				{counts: resolvedCounts{desired: 1}, matched: []string{"b"}},
			},
			bound:       boundSince(map[string]int{"a": 30, "b": 10}),
			clusters:    uniformClusters("a", "b"),
			wantDesired: map[string][]int{"a": {0}, "b": {1}},
			wantChosen:  [][]string{{"a"}, {"b"}},
		},
		{
			name: "a pick of this round is the newest when a later selector trims",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 1}, matched: []string{"fresh"}},
				{counts: resolvedCounts{desired: 1}, matched: []string{"fresh", "veteran"}},
			},
			bound:       boundSince(map[string]int{"veteran": 30}),
			clusters:    uniformClusters("fresh", "veteran"),
			wantDesired: map[string][]int{"fresh": {0}, "veteran": {1}},
			wantChosen:  [][]string{{"fresh"}, {"veteran"}},
		},
		{
			name: "selectAll binds every schedulable match and keeps the unschedulable bound one",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{selectAll: true}, matched: []string{"a", "b"}, sticky: []string{"dark"}},
			},
			bound:       boundSince(map[string]int{"dark": 5}),
			clusters:    uniformClusters("a", "b", "dark"),
			wantDesired: map[string][]int{"a": {0}, "b": {0}, "dark": {0}},
			wantChosen:  [][]string{{"a", "b"}},
		},
		{
			name: "fewer matches than the count binds what there is",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 3}, matched: []string{"a"}},
			},
			clusters:    uniformClusters("a"),
			wantDesired: map[string][]int{"a": {0}},
			wantChosen:  [][]string{{"a"}},
		},
		{
			name:        "no selectors",
			wantDesired: map[string][]int{},
			wantChosen:  [][]string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := chooseClusters(tc.outcomes, tc.bound, tc.clusters)
			if diff := cmp.Diff(got, tc.wantDesired); diff != "" {
				t.Errorf("chooseClusters() desired mismatch (-got, +want):\n%s", diff)
			}
			gotChosen := make([][]string, 0, len(tc.outcomes))
			for i := range tc.outcomes {
				gotChosen = append(gotChosen, tc.outcomes[i].chosen)
			}
			if diff := cmp.Diff(gotChosen, tc.wantChosen); diff != "" {
				t.Errorf("chooseClusters() chosen mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestBindingName guards the generated binding name against the object name limit and DNS-1123
// rules, including a truncation point that lands on a separator, and pins that bindings of
// different clusters, policies, namespaces, and scopes never share a name.
func TestBindingName(t *testing.T) {
	long := strings.Repeat("a", bindingNameSegmentMaxLength-1) + "." + strings.Repeat("a", 150)
	testCases := []struct {
		name       string
		policyName string
		namespace  string
		cluster    string
	}{
		{name: "short names", policyName: "app", namespace: "tenant-a", cluster: "east-1"},
		{name: "another cluster", policyName: "app", namespace: "tenant-a", cluster: "east-2"},
		{name: "another policy", policyName: "app2", namespace: "tenant-a", cluster: "east-1"},
		{name: "another namespace", policyName: "app", namespace: "tenant-b", cluster: "east-1"},
		{name: "cluster scope", policyName: "app", cluster: "east-1"},
		{name: "long policy name with a separator at the truncation point", policyName: long, namespace: "tenant-a", cluster: "east-1"},
		{name: "long cluster name with a separator at the truncation point", policyName: "app", namespace: "tenant-a", cluster: long},
		{name: "both long", policyName: long, namespace: "tenant-a", cluster: long},
	}

	seen := sets.New[string]()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := bindingName(policyFor(tc.policyName, tc.namespace), tc.cluster)
			if errs := validation.IsDNS1123Subdomain(got); len(errs) > 0 {
				t.Errorf("bindingName() = %q, want a valid DNS-1123 subdomain, got errors %v", got, errs)
			}
			if again := bindingName(policyFor(tc.policyName, tc.namespace), tc.cluster); again != got {
				t.Errorf("bindingName() = %q on the second call, want the deterministic %q", again, got)
			}
			if seen.Has(got) {
				t.Errorf("bindingName() = %q, want a name distinct from every other case's", got)
			}
			seen.Insert(got)
		})
	}
}

// withIndexes registers, on a fake client builder that already has its scheme, the binding and
// claim indexes the controller lists through, as fieldindexers.SetupWithHubAgentControllerManager
// does on the real cache.
func withIndexes(b *fake.ClientBuilder) *fake.ClientBuilder {
	byPolicyName := func(obj client.Object) []string {
		return []string{obj.(kfplacementv1alpha1.PlacementBindingAccessor).GetSpec().PlacementPolicyName}
	}
	byPolicyRef := func(obj client.Object) []string {
		return []string{fieldindexers.ClusterClaimOwnedByPlacementPolicyCustomFieldVal(obj.(*kfplacementv1alpha1.ClusterClaim).Spec.PlacementPolicyRef)}
	}
	return b.
		WithIndex(&kfplacementv1alpha1.PlacementBinding{}, fieldindexers.PlacementBindingOwnedByCustomFieldName, byPolicyName).
		WithIndex(&kfplacementv1alpha1.ClusterPlacementBinding{}, fieldindexers.PlacementBindingOwnedByCustomFieldName, byPolicyName).
		WithIndex(&kfplacementv1alpha1.ClusterClaim{}, fieldindexers.ClusterClaimOwnedByPlacementPolicyCustomFieldName, byPolicyRef)
}

// bindingManagerHeldBy builds a policy whose binding manager role is held by the given
// controller, as a previous round would have left it.
func bindingManagerHeldBy(controller string) *kfplacementv1alpha1.PlacementPolicy {
	policy := &kfplacementv1alpha1.PlacementPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "pp", UID: types.UID("pp-uid")},
		Spec: kfplacementv1alpha1.PlacementPolicySpec{
			ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{{APIVersion: "v1", Kind: "ConfigMap", Name: "demo"}},
		},
	}
	policy.Status.BindingManager = &kfplacementv1alpha1.BindingManager{
		ControllerName: controller,
		ObjectRefs:     []kfplacementv1alpha1.ObjectReference{*policyReference(policy)},
	}
	return policy
}

// TestReconcileBindingsReleasesLeftOverRole pins that a round with nothing to do still releases a
// binding manager role a previous round left held, e.g. after a failed write whose change became
// moot; otherwise the policy would stay locked for every other binding manipulator forever.
func TestReconcileBindingsReleasesLeftOverRole(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	policy := bindingManagerHeldBy(controllerName)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy).WithStatusSubresource(policy).Build()
	r := NewReconciler(c, c, snapshotStub{})

	roleHeld, err := r.reconcileBindings(context.Background(), policy, nil, map[string][]int{}, nil)
	if err != nil || roleHeld {
		t.Fatalf("reconcileBindings() = %t, %v, want false, nil", roleHeld, err)
	}
	fetched := &kfplacementv1alpha1.PlacementPolicy{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pp"}, fetched); err != nil {
		t.Fatalf("Get() = %v, want no error", err)
	}
	if fetched.Status.BindingManager != nil {
		t.Errorf("reconcileBindings() left the binding manager role as %v, want it released", fetched.Status.BindingManager)
	}
}

// TestReconcileBindingsBacksOffWhenRoleIsHeld pins that a role held by another process leaves
// the bindings untouched and reports the hold, rather than writing over a rollout in progress.
func TestReconcileBindingsBacksOffWhenRoleIsHeld(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	policy := bindingManagerHeldBy("rollout-controller")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy).WithStatusSubresource(policy).Build()
	r := NewReconciler(c, c, snapshotStub{})

	outcomes := []selectorOutcome{{counts: resolvedCounts{desired: 1}, matched: []string{"east-1"}, chosen: []string{"east-1"}}}
	roleHeld, err := r.reconcileBindings(context.Background(), policy, outcomes, map[string][]int{"east-1": {0}}, nil)
	if err != nil || !roleHeld {
		t.Fatalf("reconcileBindings() = %t, %v, want true, nil", roleHeld, err)
	}
	bindings := &kfplacementv1alpha1.PlacementBindingList{}
	if err := c.List(context.Background(), bindings); err != nil {
		t.Fatalf("List() = %v, want no error", err)
	}
	if len(bindings.Items) != 0 {
		t.Errorf("reconcileBindings() created %d bindings while the role was held elsewhere, want 0", len(bindings.Items))
	}
}

// TestReconcileBindingsUpdatesSyncStrategy pins that a change to a policy's sync strategy reaches
// the bindings it already has, not only the ones it creates afterwards.
func TestReconcileBindingsUpdatesSyncStrategy(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	policy := bindingManagerHeldBy(controllerName)
	c := withIndexes(fake.NewClientBuilder().WithScheme(scheme)).WithObjects(policy).WithStatusSubresource(policy).Build()
	r := NewReconciler(c, c, snapshotStub{})
	outcomes := []selectorOutcome{{counts: resolvedCounts{desired: 1}, matched: []string{"east-1"}, chosen: []string{"east-1"}}}
	chosen := map[string][]int{"east-1": {0}}
	ctx := context.Background()

	if _, err := r.reconcileBindings(ctx, policy, outcomes, chosen, nil); err != nil {
		t.Fatalf("reconcileBindings() = %v, want no error", err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "pp"}, policy); err != nil {
		t.Fatalf("Get() = %v, want no error", err)
	}
	policy.Spec.SyncStrategy = &kfplacementv1alpha1.SyncStrategy{ApplyMethod: kfplacementv1alpha1.ApplyMethodServerSideApply}
	want := policy.Spec.SyncStrategy.DeepCopy()
	// The change is written, not just set in memory: claiming the binding manager role re-reads
	// the policy, as a real round would.
	if err := c.Update(ctx, policy); err != nil {
		t.Fatalf("Update() = %v, want no error", err)
	}

	// The second round sees the binding the first created, so the change has to go through an
	// update of the existing binding.
	existing, err := r.listBindings(ctx, policy)
	if err != nil || len(existing) != 1 {
		t.Fatalf("listBindings() = %d bindings, %v, want 1 binding, no error", len(existing), err)
	}
	if _, err := r.reconcileBindings(ctx, policy, outcomes, chosen, existing); err != nil {
		t.Fatalf("reconcileBindings() = %v, want no error", err)
	}

	bindings := &kfplacementv1alpha1.PlacementBindingList{}
	if err := c.List(ctx, bindings); err != nil {
		t.Fatalf("List() = %v, want no error", err)
	}
	if len(bindings.Items) != 1 {
		t.Fatalf("reconcileBindings() left %d bindings, want 1", len(bindings.Items))
	}
	if diff := cmp.Diff(want, bindings.Items[0].Spec.SyncStrategy); diff != "" {
		t.Errorf("binding sync strategy mismatch (-want +got):\n%s", diff)
	}
}

// TestReconcileBindingsSettlesATakenName pins what happens when a binding's deterministic name is
// already taken: an orphan of the policy's name for the same cluster is adopted and brought up to
// date, while any other occupant is reported -- as transient when an earlier policy of the same
// name still controls it -- without holding up the policy's other clusters or its role.
func TestReconcileBindingsSettlesATakenName(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	testCases := []struct {
		name            string
		owner           *metav1.OwnerReference
		occupantCluster string
		wantErrCategory kferrors.ErrCategory
		wantAdopted     bool
	}{
		{name: "an orphan of the policy's name", occupantCluster: "east-1", wantAdopted: true},
		{name: "an orphan of the policy's name for another cluster", occupantCluster: "west-9", wantErrCategory: kferrors.ErrCategoryUser},
		{
			name:            "a binding of an earlier policy of the same name",
			owner:           &metav1.OwnerReference{APIVersion: kfplacementv1alpha1.GroupVersion.String(), Kind: kfplacementv1alpha1.PlacementPolicyKind, Name: "pp", UID: "earlier-uid", Controller: ptr.To(true)},
			occupantCluster: "east-1",
			wantErrCategory: kferrors.ErrCategoryTransient,
		},
		{
			name:            "a binding another object controls",
			owner:           &metav1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: "squatter", UID: "squatter-uid", Controller: ptr.To(true)},
			occupantCluster: "east-1",
			wantErrCategory: kferrors.ErrCategoryUser,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			policy := bindingManagerHeldBy(controllerName)
			occupant := &kfplacementv1alpha1.PlacementBinding{
				ObjectMeta: metav1.ObjectMeta{Name: bindingName(policy, "east-1"), Namespace: "default"},
				Spec:       kfplacementv1alpha1.PlacementBindingSpec{PlacementPolicyName: "pp", ClusterName: tc.occupantCluster, ResourceSnapshotName: "pp-snapshot-from-before"},
			}
			if tc.owner != nil {
				occupant.OwnerReferences = []metav1.OwnerReference{*tc.owner}
			}
			c := withIndexes(fake.NewClientBuilder().WithScheme(scheme)).WithObjects(policy, occupant).WithStatusSubresource(policy).Build()
			r := NewReconciler(c, c, snapshotStub{})
			ctx := context.Background()

			outcomes := []selectorOutcome{{counts: resolvedCounts{desired: 2}, matched: []string{"east-1", "west-2"}, chosen: []string{"east-1", "west-2"}}}
			_, err := r.reconcileBindings(ctx, policy, outcomes, map[string][]int{"east-1": {0}, "west-2": {0}}, nil)
			if got := kferrors.Category(err); (err != nil) != (tc.wantErrCategory != "") || (err != nil && got != tc.wantErrCategory) {
				t.Fatalf("reconcileBindings() = %v (category %q), want error category %q", err, got, tc.wantErrCategory)
			}

			got := &kfplacementv1alpha1.PlacementBinding{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(occupant), got); err != nil {
				t.Fatalf("Get(occupant) = %v, want no error", err)
			}
			if adopted := metav1.IsControlledBy(got, policy); adopted != tc.wantAdopted {
				t.Errorf("occupant controlled by the policy = %t, want %t", adopted, tc.wantAdopted)
			}
			if tc.wantAdopted && got.Spec.ResourceSnapshotName != stubSnapshotName(policy) {
				t.Errorf("adopted binding rolls out %q, want the policy's latest snapshot %q", got.Spec.ResourceSnapshotName, stubSnapshotName(policy))
			}
			if !tc.wantAdopted && got.Spec.ClusterName != tc.occupantCluster {
				t.Errorf("occupant retargeted to cluster %q, want it left on %q", got.Spec.ClusterName, tc.occupantCluster)
			}
			// The other cluster is bound regardless, and the role is not left held.
			if err := c.Get(ctx, types.NamespacedName{Namespace: "default", Name: bindingName(policy, "west-2")}, &kfplacementv1alpha1.PlacementBinding{}); err != nil {
				t.Errorf("Get(west-2 binding) = %v, want the other cluster bound", err)
			}
			fetched := &kfplacementv1alpha1.PlacementPolicy{}
			if err := c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "pp"}, fetched); err != nil {
				t.Fatalf("Get(policy) = %v, want no error", err)
			}
			if fetched.Status.BindingManager != nil {
				t.Errorf("reconcileBindings() left the binding manager role as %v, want it released", fetched.Status.BindingManager)
			}
		})
	}
}
