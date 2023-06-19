/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework/parallelizer"
	"go.goms.io/fleet/pkg/utils"
)

const (
	CRPName        = "test-placement"
	policyName     = "test-policy"
	bindingName    = "test-binding"
	altBindingName = "another-test-binding"
	clusterName    = "bravelion"
	altClusterName = "smartcat"
)

var (
	ignoredCondFields   = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
	ignoredStatusFields = cmpopts.IgnoreFields(Status{}, "reasons", "err")
)

// TO-DO (chenyu1): expand the test cases as development stablizes.

// TestMain sets up the test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme.
	if err := fleetv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

// TestExtractNumOfClustersFromPolicySnapshot tests the extractNumOfClustersFromPolicySnapshot function.
func TestExtractNumOfClustersFromPolicySnapshot(t *testing.T) {
	testCases := []struct {
		name              string
		policy            *fleetv1beta1.ClusterPolicySnapshot
		wantNumOfClusters int
		expectedToFail    bool
	}{
		{
			name: "valid annotation",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					Annotations: map[string]string{
						fleetv1beta1.NumOfClustersAnnotation: "1",
					},
				},
			},
			wantNumOfClusters: 1,
		},
		{
			name: "no annotation",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			},
			expectedToFail: true,
		},
		{
			name: "invalid annotation: not an integer",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					Annotations: map[string]string{
						fleetv1beta1.NumOfClustersAnnotation: "abc",
					},
				},
			},
			expectedToFail: true,
		},
		{
			name: "invalid annotation: negative integer",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					Annotations: map[string]string{
						fleetv1beta1.NumOfClustersAnnotation: "-1",
					},
				},
			},
			expectedToFail: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			numOfClusters, err := extractNumOfClustersFromPolicySnapshot(tc.policy)
			if tc.expectedToFail {
				if err == nil {
					t.Fatalf("extractNumOfClustersFromPolicySnapshot() = %v, %v, want error", numOfClusters, err)
				}
				return
			}

			if numOfClusters != tc.wantNumOfClusters {
				t.Fatalf("extractNumOfClustersFromPolicySnapshot() = %v, %v, want %v, nil", numOfClusters, err, tc.wantNumOfClusters)
			}
		})
	}
}

// TestCollectClusters tests the collectClusters method.
func TestCollectClusters(t *testing.T) {
	cluster := fleetv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-1",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(&cluster).
		Build()
	// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
	f := &framework{
		client: fakeClient,
	}

	ctx := context.Background()
	clusters, err := f.collectClusters(ctx)
	if err != nil {
		t.Fatalf("collectClusters() = %v, %v, want no error", clusters, err)
	}

	wantClusters := []fleetv1beta1.MemberCluster{cluster}
	if !cmp.Equal(clusters, wantClusters) {
		t.Fatalf("collectClusters() = %v, %v, want %v, nil", clusters, err, wantClusters)
	}
}

// TestExtractOwnerCRPNameFromPolicySnapshot tests the extractOwnerCRPNameFromPolicySnapshot method.
func TestExtractOwnerCRPNameFromPolicySnapshot(t *testing.T) {
	testCases := []struct {
		name         string
		policy       *fleetv1beta1.ClusterPolicySnapshot
		expectToFail bool
	}{
		{
			name: "policy with CRP owner reference",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: utils.CRPV1Beta1GVK.Kind,
							Name: CRPName,
						},
					},
				},
			},
		},
		{
			name: "policy without CRP owner reference",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:            policyName,
					OwnerReferences: []metav1.OwnerReference{},
				},
			},
			expectToFail: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			owner, err := extractOwnerCRPNameFromPolicySnapshot(tc.policy)
			if tc.expectToFail {
				if err == nil {
					t.Fatalf("extractOwnerCRPNameFromPolicySnapshot() = %v, %v, want cannot find owner ref error", owner, err)
				}
				return
			}

			if err != nil || owner != CRPName {
				t.Fatalf("extractOwnerCRPNameFromPolicySnapshot() = %v, %v, want %v, no error", owner, err, CRPName)
			}
		})
	}
}

// TestCollectBindings tests the collectBindings method.
func TestCollectBindings(t *testing.T) {
	binding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
			Labels: map[string]string{
				fleetv1beta1.CRPTrackingLabel: CRPName,
			},
		},
	}
	altCRPName := "another-test-placement"

	testCases := []struct {
		name                   string
		binding                *fleetv1beta1.ClusterResourceBinding
		policy                 *fleetv1beta1.ClusterPolicySnapshot
		expectToFail           bool
		expectToFindNoBindings bool
	}{
		{
			name:    "found matching bindings",
			binding: binding,
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: utils.CRPV1Beta1GVK.Kind,
							Name: CRPName,
						},
					},
				},
			},
		},
		{
			name: "no owner reference in policy",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			},
			expectToFail: true,
		},
		{
			name:    "no matching bindings",
			binding: binding,
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: utils.CRPV1Beta1GVK.Kind,
							Name: altCRPName,
						},
					},
				},
			},
			expectToFindNoBindings: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tc.binding != nil {
				fakeClientBuilder.WithObjects(tc.binding)
			}
			fakeClient := fakeClientBuilder.Build()
			// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
			f := &framework{
				uncachedReader: fakeClient,
			}

			ctx := context.Background()
			bindings, err := f.collectBindings(ctx, tc.policy)
			if tc.expectToFail {
				if err == nil {
					t.Fatalf("collectBindings() = %v, %v, want failed to collect bindings error", bindings, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("collectBindings() = %v, %v, want %v, no error", bindings, err, binding)
			}
			wantBindings := []fleetv1beta1.ClusterResourceBinding{}
			if !tc.expectToFindNoBindings {
				wantBindings = append(wantBindings, *binding)
			}
			if !cmp.Equal(bindings, wantBindings) {
				t.Fatalf("collectBindings() = %v, %v, want %v, no error", bindings, err, wantBindings)
			}
		})
	}
}

// TestClassifyBindings tests the classifyBindings function.
func TestClassifyBindings(t *testing.T) {
	timestamp := metav1.Now()
	activeBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "active-binding",
		},
	}
	deletedBindingWithDispatcherFinalizer := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "deleted-binding-with-dispatcher-finalizer",
			DeletionTimestamp: &timestamp,
			Finalizers: []string{
				utils.DispatcherFinalizer,
			},
		},
	}
	deletedBindingWithoutDispatcherFinalizer := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "deleted-binding-without-dispatcher-finalizer",
			DeletionTimestamp: &timestamp,
		},
	}

	active, deletedWith, deletedWithout := classifyBindings([]fleetv1beta1.ClusterResourceBinding{*activeBinding, *deletedBindingWithDispatcherFinalizer, *deletedBindingWithoutDispatcherFinalizer})
	if diff := cmp.Diff(active, []*fleetv1beta1.ClusterResourceBinding{activeBinding}); diff != "" {
		t.Errorf("classifyBindings() active = %v, want %v", active, []*fleetv1beta1.ClusterResourceBinding{activeBinding})
	}
	if !cmp.Equal(deletedWith, []*fleetv1beta1.ClusterResourceBinding{deletedBindingWithDispatcherFinalizer}) {
		t.Errorf("classifyBindings() deletedWithDispatcherFinalizer = %v, want %v", deletedWith, []*fleetv1beta1.ClusterResourceBinding{deletedBindingWithDispatcherFinalizer})
	}
	if !cmp.Equal(deletedWithout, []*fleetv1beta1.ClusterResourceBinding{deletedBindingWithoutDispatcherFinalizer}) {
		t.Errorf("classifyBindings() deletedWithoutDispatcherFinalizer = %v, want %v", deletedWithout, []*fleetv1beta1.ClusterResourceBinding{deletedBindingWithoutDispatcherFinalizer})
	}
}

// TestRemoveSchedulerFinalizerFromBindings tests the removeSchedulerFinalizerFromBindings method.
func TestRemoveSchedulerFinalizerFromBindings(t *testing.T) {
	binding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:       bindingName,
			Finalizers: []string{utils.SchedulerFinalizer},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(binding).
		Build()
	// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
	f := &framework{
		client: fakeClient,
	}

	ctx := context.Background()
	if err := f.removeSchedulerFinalizerFromBindings(ctx, []*fleetv1beta1.ClusterResourceBinding{binding}); err != nil {
		t.Fatalf("removeSchedulerFinalizerFromBindings() = %v, want no error", err)
	}

	// Verify that the finalizer has been removed.
	updatedBinding := &fleetv1beta1.ClusterResourceBinding{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: bindingName}, updatedBinding); err != nil {
		t.Fatalf("Binding Get(%v) = %v, want no error", bindingName, err)
	}

	if controllerutil.ContainsFinalizer(updatedBinding, utils.SchedulerFinalizer) {
		t.Fatalf("Binding %s finalizers = %v, want no scheduler finalizer", bindingName, updatedBinding.Finalizers)
	}
}

// TestShouldDownscale tests the shouldDownscale function.
func TestShouldDownscale(t *testing.T) {
	testCases := []struct {
		name          string
		policy        *fleetv1beta1.ClusterPolicySnapshot
		numOfClusters int
		active        []*fleetv1beta1.ClusterResourceBinding
		wantAct       bool
		wantCount     int
	}{
		{
			name: "should not downscale (pick all)",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.PolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickAllPlacementType,
					},
				},
			},
		},
		{
			name: "should not downscale (enough bindings)",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.PolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
					},
				},
			},
			numOfClusters: 1,
			active: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName,
					},
				},
			},
		},
		{
			name: "should downscale (not enough bindings)",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.PolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
					},
				},
			},
			numOfClusters: 0,
			active: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName,
					},
				},
			},
			wantAct:   true,
			wantCount: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			act, count := shouldDownscale(tc.policy, tc.numOfClusters, tc.active)
			if act != tc.wantAct || count != tc.wantCount {
				t.Fatalf("shouldDownscale() = %v, %v, want %v, %v", act, count, tc.wantAct, tc.wantCount)
			}
		})
	}
}

// TestSortByCreationTimestampBindingsWrapper checks if the sortByCreationTimestampBindings wrapper implements
// sort.Interface correctly, i.e, if it is sortable by CreationTimestamp.
func TestSortByCreationTimestampBindingsWrapper(t *testing.T) {
	timestampA := metav1.Now()
	timestampB := metav1.NewTime(time.Now().Add(time.Second))

	sorted := sortByCreationTimestampBindings([]*fleetv1beta1.ClusterResourceBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              bindingName,
				CreationTimestamp: timestampB,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              altBindingName,
				CreationTimestamp: timestampA,
			},
		},
	})
	sort.Sort(sorted)

	wantSorted := sortByCreationTimestampBindings([]*fleetv1beta1.ClusterResourceBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              altBindingName,
				CreationTimestamp: timestampA,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              bindingName,
				CreationTimestamp: timestampB,
			},
		},
	})
	if !cmp.Equal(sorted, wantSorted) {
		t.Fatalf("sortByCreationTimestamp, got %v, want %v", sorted, wantSorted)
	}
}

// TestDownscale tests the downscale method.
func TestDownscale(t *testing.T) {
	timestampA := metav1.Now()
	timestampB := metav1.NewTime(time.Now().Add(time.Second))

	bindingA := fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:              bindingName,
			CreationTimestamp: timestampA,
		},
	}
	bindingB := fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:              altBindingName,
			CreationTimestamp: timestampB,
		},
	}

	active := []*fleetv1beta1.ClusterResourceBinding{&bindingA, &bindingB}
	count := 1

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(&bindingA, &bindingB).
		Build()
	// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
	f := &framework{
		client: fakeClient,
	}

	ctx := context.Background()
	remains, err := f.downscale(ctx, active, count)
	wantRemains := []*fleetv1beta1.ClusterResourceBinding{&bindingB}
	if err != nil || !cmp.Equal(remains, wantRemains) {
		t.Fatalf("downscale(%v, %v) = %v, %v, want %v, no error", active, count, remains, err, wantRemains)
	}
}

// TestPrepareNewSchedulingDecisions tests the prepareNewSchedulingDecisions function.
func TestPrepareNewSchedulingDecisions(t *testing.T) {
	policy := &fleetv1beta1.ClusterPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
		},
		Status: fleetv1beta1.PolicySnapshotStatus{
			ClusterDecisions: []fleetv1beta1.ClusterDecision{
				{
					ClusterName: altClusterName,
					Selected:    false,
				},
			},
		},
	}
	binding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			TargetCluster: clusterName,
			ClusterDecision: fleetv1beta1.ClusterDecision{
				ClusterName: clusterName,
				Selected:    true,
			},
		},
	}

	decisions := prepareNewSchedulingDecisions(policy, []*fleetv1beta1.ClusterResourceBinding{binding})
	wantDecisions := []fleetv1beta1.ClusterDecision{
		binding.Spec.ClusterDecision,
		policy.Status.ClusterDecisions[0],
	}

	if !cmp.Equal(decisions, wantDecisions) {
		t.Fatalf("prepareNewSchedulingDecisions(%v, %v) = %v, want %v", policy, binding, decisions, wantDecisions)
	}
}

// TestShouldSchedule tests the shouldSchedule function.
func TestShouldSchedule(t *testing.T) {
	testCases := []struct {
		name                  string
		policy                *fleetv1beta1.ClusterPolicySnapshot
		numOfClusters         int
		existingBindingsCount int
		want                  bool
	}{
		{
			name: "should schedule (pick all)",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.PolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickAllPlacementType,
					},
				},
			},
			want: true,
		},
		{
			name: "should not schedule (enough bindings)",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.PolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
					},
				},
			},
			numOfClusters:         1,
			existingBindingsCount: 1,
		},
		{
			name: "should schedule (not enough bindings)",
			policy: &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.PolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
					},
				},
			},
			numOfClusters:         2,
			existingBindingsCount: 1,
			want:                  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if shouldSchedule(tc.policy, tc.numOfClusters, tc.existingBindingsCount) != tc.want {
				t.Fatalf("shouldSchedule(%v, %v, %v) = %v, want %v", tc.policy, tc.numOfClusters, tc.existingBindingsCount, !tc.want, tc.want)
			}
		})
	}
}

// TestEqualDecisions tests the equalDecisions function.
func TestEqualDecisions(t *testing.T) {
	testCases := []struct {
		name    string
		current []fleetv1beta1.ClusterDecision
		desired []fleetv1beta1.ClusterDecision
		want    bool
	}{
		{
			name: "equal decisions",
			current: []fleetv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
				},
			},
			desired: []fleetv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
				},
			},
			want: true,
		},
		{
			name: "not equal decisions (different value)",
			current: []fleetv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
				},
			},
			desired: []fleetv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    false,
				},
			},
		},
		{
			name: "not equal decisions (empty current)",
			current: []fleetv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
				},
			},
			desired: []fleetv1beta1.ClusterDecision{},
		},
		{
			name: "not equal decisions (empty desired)",
			current: []fleetv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
				},
			},
			desired: []fleetv1beta1.ClusterDecision{},
		},
		{
			name: "not equal decisions (no match)",
			current: []fleetv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
				},
			},
			desired: []fleetv1beta1.ClusterDecision{
				{
					ClusterName: altClusterName,
					Selected:    true,
				},
			},
		},
		{
			name: "not equal decisions (different lengths)",
			current: []fleetv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
				},
				{
					ClusterName: altClusterName,
					Selected:    true,
				},
			},
			desired: []fleetv1beta1.ClusterDecision{
				{
					ClusterName: altClusterName,
					Selected:    true,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if equalDecisions(tc.current, tc.desired) != tc.want {
				t.Fatalf("equalDecisions(%v, %v) = %v, want %v", tc.current, tc.desired, !tc.want, tc.want)
			}
		})
	}
}

// TestUpdatePolicySnapshotStatus tests the updatePolicySnapshotStatus method.
func TestUpdatePolicySnapshotStatus(t *testing.T) {
	policy := &fleetv1beta1.ClusterPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
		},
	}
	decisions := []fleetv1beta1.ClusterDecision{
		{
			ClusterName: clusterName,
			Selected:    true,
		},
	}
	condition := fullyScheduledCondition(policy)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(policy).
		Build()
	// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
	f := &framework{
		client: fakeClient,
	}

	ctx := context.Background()
	if err := f.updatePolicySnapshotStatus(ctx, policy, decisions, condition); err != nil {
		t.Fatalf("updatePolicySnapshotStatus(%v, %v, %v) = %v, want no error", policy, decisions, condition, err)
	}

	// Verify that the policy was updated.
	updatedPolicy := &fleetv1beta1.ClusterPolicySnapshot{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: policy.Name}, updatedPolicy); err != nil {
		t.Fatalf("clusterPolicySnapshot Get(%v) = %v, want no error", policy.Name, err)
	}

	if !cmp.Equal(updatedPolicy.Status.ClusterDecisions, decisions) {
		t.Errorf("cluster decisions, got %v, want %v", updatedPolicy.Status.ClusterDecisions, decisions)
	}

	updatedCondition := meta.FindStatusCondition(updatedPolicy.Status.Conditions, string(fleetv1beta1.PolicySnapshotScheduled))
	if !cmp.Equal(updatedCondition, &condition, ignoredCondFields) {
		t.Errorf("scheduled condition, got %v, want %v", updatedCondition, condition)
	}
}

// TestRunPostBatchPlugins tests the runPostBatchPlugins method.
func TestRunPostBatchPlugins(t *testing.T) {
	dummyPostBatchPluginNameA := fmt.Sprintf(dummyPostBatchPluginNameFormat, 0)
	dummyPostBatchPluginNameB := fmt.Sprintf(dummyPostBatchPluginNameFormat, 1)

	testCases := []struct {
		name             string
		postBatchPlugins []PostBatchPlugin
		desiredBatchSize int
		wantBatchLimit   int
		wantStatus       *Status
	}{
		{
			name: "single plugin, success",
			postBatchPlugins: []PostBatchPlugin{
				&dummyPostBatchPlugin{
					name: dummyPostBatchPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (size int, status *Status) {
						return 1, nil
					},
				},
			},
			desiredBatchSize: 10,
			wantBatchLimit:   1,
		},
		{
			name: "single plugin, success, oversized",
			postBatchPlugins: []PostBatchPlugin{
				&dummyPostBatchPlugin{
					name: dummyPostBatchPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (size int, status *Status) {
						return 15, nil
					},
				},
			},
			desiredBatchSize: 10,
			wantBatchLimit:   10,
		},
		{
			name: "multiple plugins, all success",
			postBatchPlugins: []PostBatchPlugin{
				&dummyPostBatchPlugin{
					name: dummyPostBatchPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (size int, status *Status) {
						return 2, nil
					},
				},
				&dummyPostBatchPlugin{
					name: dummyPostBatchPluginNameB,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (size int, status *Status) {
						return 1, nil
					},
				},
			},
			desiredBatchSize: 10,
			wantBatchLimit:   1,
		},
		{
			name: "multple plugins, one success, one error",
			postBatchPlugins: []PostBatchPlugin{
				&dummyPostBatchPlugin{
					name: dummyPostBatchPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (size int, status *Status) {
						return 0, FromError(fmt.Errorf("internal error"), dummyPostBatchPluginNameA)
					},
				},
				&dummyPostBatchPlugin{
					name: dummyPostBatchPluginNameB,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (size int, status *Status) {
						return 1, nil
					},
				},
			},
			desiredBatchSize: 10,
			wantStatus:       FromError(fmt.Errorf("internal error"), dummyPostBatchPluginNameA),
		},
		{
			name: "single plugin, skip",
			postBatchPlugins: []PostBatchPlugin{
				&dummyPostBatchPlugin{
					name: dummyPostBatchPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (size int, status *Status) {
						return 0, NewNonErrorStatus(Skip, dummyPostBatchPluginNameA)
					},
				},
			},
			desiredBatchSize: 10,
			wantBatchLimit:   10,
		},
		{
			name: "single plugin, unschedulable",
			postBatchPlugins: []PostBatchPlugin{
				&dummyPostBatchPlugin{
					name: dummyPostBatchPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (size int, status *Status) {
						return 1, NewNonErrorStatus(ClusterUnschedulable, dummyPostBatchPluginNameA)
					},
				},
			},
			desiredBatchSize: 10,
			wantStatus:       FromError(fmt.Errorf("internal error"), dummyPostBatchPluginNameA),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profile := NewProfile(dummyProfileName)
			for _, p := range tc.postBatchPlugins {
				profile.WithPostBatchPlugin(p)
			}
			f := &framework{
				profile: profile,
			}

			ctx := context.Background()
			state := NewCycleState()
			state.desiredBatchSize = tc.desiredBatchSize
			policy := &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			}
			batchLimit, status := f.runPostBatchPlugins(ctx, state, policy)
			if batchLimit != tc.wantBatchLimit || !cmp.Equal(status, tc.wantStatus, cmpopts.IgnoreUnexported(Status{}), ignoredStatusFields) {
				t.Errorf("runPostBatchPlugins(%v, %v) = %v %v, want %v, %v", state, policy, batchLimit, status, tc.wantBatchLimit, tc.wantStatus)
			}
		})
	}
}

// TestRunPreFilterPlugins tests the runPreFilterPlugins method.
func TestRunPreFilterPlugins(t *testing.T) {
	dummyPreFilterPluginNameA := fmt.Sprintf(dummyPreFilterPluginNameFormat, 0)
	dummyPreFilterPluginNameB := fmt.Sprintf(dummyPreFilterPluginNameFormat, 1)

	testCases := []struct {
		name                   string
		preFilterPlugins       []PreFilterPlugin
		wantSkippedPluginNames []string
		wantStatus             *Status
	}{
		{
			name: "single plugin, success",
			preFilterPlugins: []PreFilterPlugin{
				&dummyPreFilterPlugin{
					name: dummyPreFilterPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) *Status {
						return nil
					},
				},
			},
		},
		{
			name: "multiple plugins, one success, one skip",
			preFilterPlugins: []PreFilterPlugin{
				&dummyPreFilterPlugin{
					name: dummyPreFilterPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (status *Status) {
						return nil
					},
				},
				&dummyPreFilterPlugin{
					name: dummyPreFilterPluginNameB,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (status *Status) {
						return NewNonErrorStatus(Skip, dummyPreFilterPluginNameB)
					},
				},
			},
			wantSkippedPluginNames: []string{dummyPreFilterPluginNameB},
		},
		{
			name: "single plugin, internal error",
			preFilterPlugins: []PreFilterPlugin{
				&dummyPreFilterPlugin{
					name: dummyPreFilterPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) *Status {
						return FromError(fmt.Errorf("internal error"), dummyPreFilterPluginNameA)
					},
				},
			},
			wantStatus: FromError(fmt.Errorf("internal error"), dummyPreFilterPluginNameA),
		},
		{
			name: "single plugin, unschedulable",
			preFilterPlugins: []PreFilterPlugin{
				&dummyPreFilterPlugin{
					name: dummyPreFilterPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) *Status {
						return NewNonErrorStatus(ClusterUnschedulable, dummyPreFilterPluginNameA)
					},
				},
			},
			wantStatus: FromError(fmt.Errorf("cluster is unschedulable"), dummyPreFilterPluginNameA),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profile := NewProfile(dummyProfileName)
			for _, p := range tc.preFilterPlugins {
				profile.WithPreFilterPlugin(p)
			}
			f := &framework{
				profile: profile,
			}

			ctx := context.Background()
			state := NewCycleState()
			policy := &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			}

			status := f.runPreFilterPlugins(ctx, state, policy)
			if !cmp.Equal(status, tc.wantStatus, cmp.AllowUnexported(Status{}), ignoredStatusFields) {
				t.Errorf("runPreFilterPlugins(%v, %v) = %v, want %v", state, policy, status, tc.wantStatus)
			}
		})
	}
}

// TestRunFilterPluginsFor tests the runFilterPluginsFor method.
func TestRunFilterPluginsFor(t *testing.T) {
	dummyFilterPluginNameA := fmt.Sprintf(dummyFilterPluginNameFormat, 0)
	dummyFilterPluginNameB := fmt.Sprintf(dummyFilterPluginNameFormat, 1)

	testCases := []struct {
		name               string
		filterPlugins      []FilterPlugin
		skippedPluginNames []string
		wantStatus         *Status
	}{
		{
			name: "single plugin, success",
			filterPlugins: []FilterPlugin{
				&dummyFilterPlugin{
					name: dummyFilterPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
			},
		},
		{
			name: "multiple plugins, one success, one skipped",
			filterPlugins: []FilterPlugin{
				&dummyFilterPlugin{
					name: dummyFilterPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
				&dummyFilterPlugin{
					name: dummyFilterPluginNameB,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameB)
					},
				},
			},
			skippedPluginNames: []string{dummyFilterPluginNameB},
		},
		{
			name: "single plugin, internal error",
			filterPlugins: []FilterPlugin{
				&dummyFilterPlugin{
					name: dummyFilterPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return FromError(fmt.Errorf("internal error"), dummyFilterPluginNameA)
					},
				},
			},
			wantStatus: FromError(fmt.Errorf("internal error"), dummyFilterPluginNameA),
		},
		{
			name: "multiple plugins, one unschedulable, one success",
			filterPlugins: []FilterPlugin{
				&dummyFilterPlugin{
					name: dummyFilterPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA)
					},
				},
				&dummyFilterPlugin{
					name: dummyFilterPluginNameB,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
			},
			wantStatus: NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA),
		},
		{
			name: "single plugin, skip",
			filterPlugins: []FilterPlugin{
				&dummyFilterPlugin{
					name: dummyFilterPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return NewNonErrorStatus(Skip, dummyFilterPluginNameA)
					},
				},
			},
			wantStatus: FromError(fmt.Errorf("internal error"), dummyFilterPluginNameA),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profile := NewProfile(dummyProfileName)
			for _, p := range tc.filterPlugins {
				profile.WithFilterPlugin(p)
			}
			f := &framework{
				profile: profile,
			}

			ctx := context.Background()
			state := NewCycleState()
			for _, name := range tc.skippedPluginNames {
				state.skippedFilterPlugins.Insert(name)
			}
			policy := &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			}
			cluster := &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			}

			status := f.runFilterPluginsFor(ctx, state, policy, cluster)
			if !cmp.Equal(status, tc.wantStatus, cmpopts.IgnoreUnexported(Status{}), ignoredStatusFields) {
				t.Errorf("runFilterPluginsFor(%v, %v, %v) = %v, want %v", state, policy, cluster, status, tc.wantStatus)
			}
		})
	}
}

// TestRunFilterPlugins tests the runFilterPlugins method.
func TestRunFilterPlugins(t *testing.T) {
	dummyFilterPluginNameA := fmt.Sprintf(dummyFilterPluginNameFormat, 0)
	dummyFilterPluginNameB := fmt.Sprintf(dummyFilterPluginNameFormat, 1)

	anotherClusterName := "singingbutterfly"
	clusters := []fleetv1beta1.MemberCluster{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: altClusterName,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: anotherClusterName,
			},
		},
	}

	testCases := []struct {
		name                     string
		filterPlugins            []FilterPlugin
		wantPassedClusterNames   []string
		wantFilteredClusterNames []string
		expectedToFail           bool
	}{
		{
			name: "three clusters, two filter plugins, all passed",
			filterPlugins: []FilterPlugin{
				&dummyFilterPlugin{
					name: dummyFilterPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
				&dummyFilterPlugin{
					name: dummyFilterPluginNameB,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
			},
			wantPassedClusterNames: []string{clusterName, altClusterName, anotherClusterName},
		},
		{
			name: "three clusters, two filter plugins, two filtered",
			filterPlugins: []FilterPlugin{
				&dummyFilterPlugin{
					name: dummyFilterPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == clusterName {
							return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA)
						}
						return nil
					},
				},
				&dummyFilterPlugin{
					name: dummyFilterPluginNameB,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == anotherClusterName {
							return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameB)
						}
						return nil
					},
				},
			},
			wantPassedClusterNames:   []string{altClusterName},
			wantFilteredClusterNames: []string{clusterName, anotherClusterName},
		},
		{
			name: "three clusters, internal error",
			filterPlugins: []FilterPlugin{
				&dummyFilterPlugin{
					name: dummyFilterPluginNameA,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
				&dummyFilterPlugin{
					name: dummyFilterPluginNameB,
					runner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == anotherClusterName {
							return FromError(fmt.Errorf("internal error"), dummyFilterPluginNameB)
						}
						return nil
					},
				},
			},
			expectedToFail: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profile := NewProfile(dummyProfileName)
			for _, p := range tc.filterPlugins {
				profile.WithFilterPlugin(p)
			}
			f := &framework{
				profile:      profile,
				parallelizer: parallelizer.NewParallelizer(parallelizer.DefaultNumOfWorkers),
			}

			ctx := context.Background()
			state := NewCycleState()
			policy := &fleetv1beta1.ClusterPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			}

			passed, filtered, err := f.runFilterPlugins(ctx, state, policy, clusters)
			if tc.expectedToFail {
				if err == nil {
					t.Errorf("runFilterPlugins(%v, %v, %v) = %v %v %v, want error", state, policy, clusters, passed, filtered, err)
				}
				return
			}

			// The method runs in parallel; as a result the order cannot be guaranteed.
			// Organize the results into maps for easier comparison.
			passedMap := make(map[string]bool)
			for _, cluster := range passed {
				passedMap[cluster.Name] = true
			}
			wantPassedMap := make(map[string]bool)
			for _, name := range tc.wantPassedClusterNames {
				wantPassedMap[name] = true
			}

			if !cmp.Equal(passedMap, wantPassedMap) {
				t.Errorf("passed clusters, got %v, want %v", passedMap, wantPassedMap)
			}

			filteredMap := make(map[string]bool)
			for _, item := range filtered {
				filteredMap[item.cluster.Name] = true
				// As a sanity check, verify if all status are of the ClusterUnschedulable status code.
				if !item.status.IsClusterUnschedulable() {
					t.Errorf("filtered cluster %s status, got %v, want status code ClusterUnschedulable", item.cluster.Name, item.status)
				}
			}
			wantFilteredMap := make(map[string]bool)
			for _, name := range tc.wantFilteredClusterNames {
				wantFilteredMap[name] = true
			}

			if !cmp.Equal(filteredMap, wantFilteredMap) {
				t.Errorf("filtered clusters, got %v, want %v", filteredMap, wantFilteredMap)
			}
		})
	}
}
