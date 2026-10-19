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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

func policyFor(name, namespace string) kfplacementv1alpha1.PlacementPolicyAccessor {
	if namespace == "" {
		return &kfplacementv1alpha1.ClusterPlacementPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: name},
		}
	}
	return &kfplacementv1alpha1.PlacementPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
}

func TestClaimName(t *testing.T) {
	policyA := policyFor("app", "tenant-a")
	policyB := policyFor("app", "tenant-b")
	clusterScoped := policyFor("app", "")

	nameA := claimName(policyA, 0)
	nameB := claimName(policyB, 0)
	nameCluster := claimName(clusterScoped, 0)

	if nameA == nameB {
		t.Errorf("claimName(tenant-a/app, 0) = claimName(tenant-b/app, 0) = %s, want distinct names", nameA)
	}
	if nameA == nameCluster {
		t.Errorf("claimName(tenant-a/app, 0) = claimName(cluster-scoped app, 0) = %s, want distinct names", nameA)
	}
	if got := claimName(policyA, 0); got != nameA {
		t.Errorf("claimName(tenant-a/app, 0) = %s on the second call, want the deterministic %s", got, nameA)
	}
	if nameIdx1 := claimName(policyA, 1); nameIdx1 == nameA {
		t.Errorf("claimName(tenant-a/app, 1) = claimName(tenant-a/app, 0) = %s, want distinct names per selector", nameA)
	}

	longPolicy := policyFor(strings.Repeat("x", 250), "tenant-a")
	if got := claimName(longPolicy, 0); len(got) > 253 {
		t.Errorf("claimName(long policy, 0) has length %d, want at most 253", len(got))
	}
}

// TestClaimNameValidity guards the generated claim name against the 253-character object name
// limit and DNS-1123 subdomain rules, including names whose truncation point lands on a
// separator.
func TestClaimNameValidity(t *testing.T) {
	testCases := []struct {
		name       string
		policyName string
	}{
		{
			name:       "short name",
			policyName: "app",
		},
		{
			name:       "long name",
			policyName: strings.Repeat("x", 250),
		},
		{
			name:       "dot at the truncation boundary",
			policyName: strings.Repeat("a", claimNameBaseMaxLength-1) + "." + strings.Repeat("a", 50),
		},
		{
			name:       "dash at the truncation boundary",
			policyName: strings.Repeat("a", claimNameBaseMaxLength-1) + "-" + strings.Repeat("a", 50),
		},
		{
			name:       "run of separators at the truncation boundary",
			policyName: strings.Repeat("a", claimNameBaseMaxLength-3) + "-.-" + strings.Repeat("a", 50),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := claimName(policyFor(tc.policyName, "tenant-a"), 0)
			if errs := validation.IsDNS1123Subdomain(got); len(errs) > 0 {
				t.Errorf("claimName(%q, 0) = %q, want a valid DNS-1123 subdomain, got errors %v", tc.policyName, got, errs)
			}
		})
	}
}

func TestDesiredClaims(t *testing.T) {
	policy := policyFor("app", "tenant-a")
	regionTerms := func(region string) []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm {
		return []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{regionLabel: region}},
		}
	}

	testCases := []struct {
		name     string
		outcomes []selectorOutcome
		want     []desiredClaim
	}{
		{
			name: "one claim for the first unfulfilled selector",
			outcomes: []selectorOutcome{
				{
					counts:          resolvedCounts{desired: 1, minimum: 1},
					terms:           regionTerms("eastus"),
					whenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim,
				},
			},
			want: []desiredClaim{{name: claimName(policyFor("app", "tenant-a"), 0), terms: regionTerms("eastus")}},
		},
		{
			name: "budget caps at one claim across multiple unfulfilled selectors",
			outcomes: []selectorOutcome{
				{
					counts:          resolvedCounts{desired: 1, minimum: 1},
					terms:           regionTerms("eastus"),
					whenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim,
				},
				{
					counts:          resolvedCounts{desired: 1, minimum: 1},
					terms:           regionTerms("westus"),
					whenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim,
				},
			},
			want: []desiredClaim{{name: claimName(policyFor("app", "tenant-a"), 0), terms: regionTerms("eastus")}},
		},
		{
			name: "fulfilled selectors and KeepSearching selectors yield no claims",
			outcomes: []selectorOutcome{
				{
					counts:          resolvedCounts{desired: 1, minimum: 1},
					matched:         []string{"a"},
					terms:           regionTerms("eastus"),
					whenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim,
				},
				{
					counts:          resolvedCounts{desired: 1, minimum: 1},
					terms:           regionTerms("westus"),
					whenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionKeepSearching,
				},
			},
			want: []desiredClaim{},
		},
		{
			name: "a fulfilled earlier selector passes the budget to a later one",
			outcomes: []selectorOutcome{
				{
					counts:          resolvedCounts{desired: 1, minimum: 1},
					matched:         []string{"a"},
					terms:           regionTerms("eastus"),
					whenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim,
				},
				{
					counts:          resolvedCounts{desired: 2, minimum: 2},
					matched:         []string{"a"},
					terms:           regionTerms("westus"),
					whenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim,
				},
			},
			want: []desiredClaim{{name: claimName(policyFor("app", "tenant-a"), 1), terms: regionTerms("westus")}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := desiredClaims(policy, tc.outcomes)
			// The outcome field is a pointer back into tc.outcomes that carries no identity of its
			// own to assert here; the name/terms selection is what this test pins. Its wiring is
			// exercised by TestClaimReadyToRotate and the integration tests.
			if diff := cmp.Diff(got, tc.want, cmp.AllowUnexported(desiredClaim{}), cmpopts.IgnoreFields(desiredClaim{}, "outcome")); diff != "" {
				t.Errorf("desiredClaims(%v) mismatch (-got, +want):\n%s", tc.outcomes, diff)
			}
		})
	}
}

func TestClaimReadyToRotate(t *testing.T) {
	completed := func() []metav1.Condition {
		return []metav1.Condition{{Type: kfplacementv1alpha1.ClusterClaimCondTypeCompleted, Status: metav1.ConditionTrue, Reason: "Fulfilled"}}
	}
	claim := func(conds []metav1.Condition, provisioned *string) *kfplacementv1alpha1.ClusterClaim {
		return &kfplacementv1alpha1.ClusterClaim{
			Status: kfplacementv1alpha1.ClusterClaimStatus{Conditions: conds, ProvisionedClusterName: provisioned},
		}
	}
	// A count-of-3 selector with one eligible cluster: a deficit remains.
	deficit := &selectorOutcome{counts: resolvedCounts{desired: 3, minimum: 3}, matched: []string{"c1"}}

	testCases := []struct {
		name    string
		claim   *kfplacementv1alpha1.ClusterClaim
		outcome *selectorOutcome
		want    bool
	}{
		{
			name:    "completed with an eligible cluster while a deficit remains rotates",
			claim:   claim(completed(), ptr.To("c1")),
			outcome: deficit,
			want:    true,
		},
		{
			name:    "not yet completed does not rotate",
			claim:   claim(nil, nil),
			outcome: deficit,
			want:    false,
		},
		{
			// The provisioner completed but its cluster has not joined/become eligible yet: waiting
			// here is what keeps a second claim from being issued mid-join (no double-provisioning).
			name:    "completed but the provisioned cluster is not eligible yet does not rotate",
			claim:   claim(completed(), ptr.To("c2")),
			outcome: deficit,
			want:    false,
		},
		{
			name:    "completed with no provisioned cluster recorded does not rotate",
			claim:   claim(completed(), nil),
			outcome: deficit,
			want:    false,
		},
		{
			// The selector is already at its desired count; the claim is withdrawn (not rotated) by
			// the fulfilled path, so rotation must not fire.
			name:    "satisfied in full does not rotate",
			claim:   claim(completed(), ptr.To("c1")),
			outcome: &selectorOutcome{counts: resolvedCounts{desired: 1, minimum: 1}, matched: []string{"c1"}},
			want:    false,
		},
		{
			// count: All with a minCount floor is satisfied in full at the floor, so a completed
			// claim below the floor still has a deficit and rotates, mirroring the integer case.
			name:    "select-all below its minCount floor rotates",
			claim:   claim(completed(), ptr.To("c1")),
			outcome: &selectorOutcome{counts: resolvedCounts{selectAll: true, minimum: 2}, matched: []string{"c1"}},
			want:    true,
		},
		{
			name:    "a nil outcome does not rotate",
			claim:   claim(completed(), ptr.To("c1")),
			outcome: nil,
			want:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimReadyToRotate(tc.claim, tc.outcome); got != tc.want {
				t.Errorf("claimReadyToRotate() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestClaimBelongsTo checks that ownership survives a version change but not a group change.
func TestClaimBelongsTo(t *testing.T) {
	ref := &kfplacementv1alpha1.ObjectReference{
		APIGroup: kfplacementv1alpha1.GroupVersion.Group, APIVersion: "v1alpha1",
		Kind: kfplacementv1alpha1.PlacementPolicyKind, Namespace: "work", Name: "app",
	}
	withRef := func(mutate func(r *kfplacementv1alpha1.ObjectReference)) *kfplacementv1alpha1.ClusterClaim {
		got := ref.DeepCopy()
		mutate(got)
		return &kfplacementv1alpha1.ClusterClaim{Spec: kfplacementv1alpha1.ClusterClaimSpec{PlacementPolicyRef: got}}
	}
	testCases := []struct {
		name  string
		claim *kfplacementv1alpha1.ClusterClaim
		want  bool
	}{
		{name: "same reference", claim: withRef(func(*kfplacementv1alpha1.ObjectReference) {}), want: true},
		{name: "another version", claim: withRef(func(r *kfplacementv1alpha1.ObjectReference) { r.APIVersion = "v1" }), want: true},
		{name: "another group", claim: withRef(func(r *kfplacementv1alpha1.ObjectReference) { r.APIGroup = "other.example.dev" }), want: false},
		{name: "another name", claim: withRef(func(r *kfplacementv1alpha1.ObjectReference) { r.Name = "other" }), want: false},
		{name: "no reference", claim: &kfplacementv1alpha1.ClusterClaim{}, want: false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimBelongsTo(tc.claim, ref); got != tc.want {
				t.Errorf("claimBelongsTo() = %t, want %t", got, tc.want)
			}
		})
	}
}

// TestReconcileClaimsNameOccupied pins how a create that finds its deterministic name taken is
// handled: a claim of the policy the cache has yet to show holds a budget slot, while a claim of
// another policy under the name is reported instead of leaving the selector silently unfulfilled.
func TestReconcileClaimsNameOccupied(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	terms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{MatchLabels: map[string]string{regionLabel: "eastus"}}}
	outcomes := []selectorOutcome{{
		counts:          resolvedCounts{desired: 1, minimum: 1},
		terms:           terms,
		whenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim,
	}}

	testCases := []struct {
		name            string
		occupantRef     *kfplacementv1alpha1.ObjectReference
		wantOutstanding int32
		wantErr         bool
	}{
		{
			name:            "a claim of the policy that the cache has yet to show",
			occupantRef:     policyReference(policyFor("pp", "default")),
			wantOutstanding: 1,
		},
		{
			name:        "a claim of another policy",
			occupantRef: policyReference(policyFor("other", "default")),
			wantErr:     true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			policy := &kfplacementv1alpha1.PlacementPolicy{ObjectMeta: metav1.ObjectMeta{Name: "pp", Namespace: "default"}}
			occupant := &kfplacementv1alpha1.ClusterClaim{
				ObjectMeta: metav1.ObjectMeta{Name: claimName(policy, 0)},
				Spec:       kfplacementv1alpha1.ClusterClaimSpec{PlacementPolicyRef: tc.occupantRef, ClusterSelectorTerms: terms},
			}
			// The API server holds the occupant; the cache has yet to see it, so both its list and
			// a cached read come back without it.
			apiServer := fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy, occupant).Build()
			cache := interceptor.NewClient(apiServer, interceptor.Funcs{
				List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, isClaims := list.(*kfplacementv1alpha1.ClusterClaimList); isClaims {
						return nil
					}
					return cl.List(ctx, list, opts...)
				},
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, isClaim := obj.(*kfplacementv1alpha1.ClusterClaim); isClaim {
						return apierrors.NewNotFound(kfplacementv1alpha1.GroupVersion.WithResource("clusterclaims").GroupResource(), key.Name)
					}
					return cl.Get(ctx, key, obj, opts...)
				},
			})

			r := NewReconciler(cache, apiServer, snapshotStub{})
			outstanding, err := r.reconcileClaims(context.Background(), policy, outcomes, metav1.Time{})
			if (err != nil) != tc.wantErr {
				t.Fatalf("reconcileClaims() = %v, want error %t", err, tc.wantErr)
			}
			if tc.wantErr {
				if got := kferrors.Category(err); got != kferrors.ErrCategoryUser {
					t.Errorf("reconcileClaims() error category = %q, want %q", got, kferrors.ErrCategoryUser)
				}
			}
			if outstanding != tc.wantOutstanding {
				t.Errorf("reconcileClaims() = %d outstanding, want %d", outstanding, tc.wantOutstanding)
			}

			got := &kfplacementv1alpha1.ClusterClaim{}
			if err := apiServer.Get(context.Background(), client.ObjectKeyFromObject(occupant), got); err != nil {
				t.Fatalf("Get(occupant) = %v, want the occupant left in place", err)
			}
			if diff := cmp.Diff(tc.occupantRef, got.Spec.PlacementPolicyRef); diff != "" {
				t.Errorf("occupant placementPolicyRef mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestReconcileClaimsCountsClaimsTheCacheHasYetToShow pins the one-claim budget under cache lag: a
// claim issued last round for selector 0, which the cache has yet to show, still holds the budget
// when selector 1 is the next one to want a claim, so no second claim is issued beside it.
func TestReconcileClaimsCountsClaimsTheCacheHasYetToShow(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	policy := &kfplacementv1alpha1.PlacementPolicy{ObjectMeta: metav1.ObjectMeta{Name: "pp", Namespace: "default"}}
	lagging := &kfplacementv1alpha1.ClusterClaim{
		ObjectMeta: metav1.ObjectMeta{Name: claimName(policy, 0), UID: "lagging-uid"},
		Spec: kfplacementv1alpha1.ClusterClaimSpec{
			PlacementPolicyRef:   policyReference(policy),
			ClusterSelectorTerms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{MatchLabels: map[string]string{regionLabel: "eastus"}}},
		},
	}
	apiServer := fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy, lagging).Build()
	cache := interceptor.NewClient(apiServer, interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, isClaims := list.(*kfplacementv1alpha1.ClusterClaimList); isClaims {
				return nil
			}
			return cl.List(ctx, list, opts...)
		},
	})
	// Selector 0 has been satisfied since; selector 1 now wants a claim.
	outcomes := []selectorOutcome{
		{counts: resolvedCounts{desired: 1, minimum: 1}, matched: []string{"east-1"}, terms: lagging.Spec.ClusterSelectorTerms, whenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim},
		{counts: resolvedCounts{desired: 1, minimum: 1}, terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{MatchLabels: map[string]string{regionLabel: "westus"}}}, whenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim},
	}

	r := NewReconciler(cache, apiServer, snapshotStub{})
	outstanding, err := r.reconcileClaims(context.Background(), policy, outcomes, metav1.Time{})
	if err != nil {
		t.Fatalf("reconcileClaims() = %v, want no error", err)
	}
	if outstanding != 1 {
		t.Errorf("reconcileClaims() = %d outstanding, want the 1 claim the cache has yet to show", outstanding)
	}
	if err := apiServer.Get(context.Background(), client.ObjectKey{Name: claimName(policy, 1)}, &kfplacementv1alpha1.ClusterClaim{}); !apierrors.IsNotFound(err) {
		t.Errorf("Get(selector 1's claim) = %v, want NotFound: a second claim would exceed the budget of one", err)
	}
}

// TestCountUnseenClaims pins that claims are told apart by UID: a claim the cache shows is not
// counted again, while a successor under a name the cache still shows an old claim for is.
func TestCountUnseenClaims(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kfplacementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	policy := &kfplacementv1alpha1.PlacementPolicy{ObjectMeta: metav1.ObjectMeta{Name: "pp", Namespace: "default"}}
	claim := func(name, uid string, ref *kfplacementv1alpha1.ObjectReference) *kfplacementv1alpha1.ClusterClaim {
		return &kfplacementv1alpha1.ClusterClaim{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(uid)}, Spec: kfplacementv1alpha1.ClusterClaimSpec{PlacementPolicyRef: ref}}
	}
	own := policyReference(policy)
	seen := claim("pp-0", "seen", own)
	unseen := claim("pp-1", "unseen", own)
	foreign := claim("other-0", "foreign", policyReference(policyFor("other", "default")))
	apiServer := fake.NewClientBuilder().WithScheme(scheme).WithObjects(seen, unseen, foreign).Build()
	r := NewReconciler(apiServer, apiServer, snapshotStub{})

	// The cache shows the seen claim and a predecessor of the unseen one under its name.
	cached := []kfplacementv1alpha1.ClusterClaim{*seen, *claim("pp-1", "predecessor", own)}
	got, err := r.countUnseenClaims(context.Background(), policy, cached)
	if err != nil {
		t.Fatalf("countUnseenClaims() = %v, want no error", err)
	}
	if got != 1 {
		t.Errorf("countUnseenClaims() = %d, want 1: only the successor the cache has yet to show", got)
	}
}
