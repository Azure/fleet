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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework/parallelizer"
)

const (
	crpName            = "test-placement"
	policyName         = "test-policy"
	altPolicyName      = "another-test-policy"
	bindingName        = "test-binding"
	altBindingName     = "another-test-binding"
	clusterName        = "bravelion"
	altClusterName     = "smartcat"
	anotherClusterName = "singingbutterfly"
)

var (
	ignoreObjectMetaResourceVersionField = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")
	ignoreTypeMetaAPIVersionKindFields   = cmpopts.IgnoreFields(metav1.TypeMeta{}, "APIVersion", "Kind")
	ignoredStatusFields                  = cmpopts.IgnoreFields(Status{}, "reasons", "err")
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
		t.Fatalf("collectClusters() = %v, want no error", err)
	}

	want := []fleetv1beta1.MemberCluster{cluster}
	if diff := cmp.Diff(clusters, want); diff != "" {
		t.Fatalf("collectClusters() diff (-got, +want) = %s", diff)
	}
}

// TestCollectBindings tests the collectBindings method.
func TestCollectBindings(t *testing.T) {
	binding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
			Labels: map[string]string{
				fleetv1beta1.CRPTrackingLabel: crpName,
			},
		},
	}
	altCRPName := "another-test-placement"

	testCases := []struct {
		name    string
		binding *fleetv1beta1.ClusterResourceBinding
		crpName string
		want    []fleetv1beta1.ClusterResourceBinding
	}{
		{
			name:    "found matching bindings",
			binding: binding,
			crpName: crpName,
			want:    []fleetv1beta1.ClusterResourceBinding{*binding},
		},
		{
			name:    "no matching bindings",
			binding: binding,
			crpName: altCRPName,
			want:    []fleetv1beta1.ClusterResourceBinding{},
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
			bindings, err := f.collectBindings(ctx, tc.crpName)
			if err != nil {
				t.Fatalf("collectBindings() = %v, want no error", err)
			}
			if diff := cmp.Diff(bindings, tc.want, ignoreObjectMetaResourceVersionField); diff != "" {
				t.Fatalf("collectBindings() diff (-got, +want) = %s", diff)
			}
		})
	}
}

// TestClassifyBindings tests the classifyBindings function.
func TestClassifyBindings(t *testing.T) {
	policy := &fleetv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
		},
	}

	clusterName1 := "cluster-1"
	clusterName2 := "cluster-2"
	clusterName3 := "cluster-3"
	clusterName4 := "cluster-4"
	clusters := []fleetv1beta1.MemberCluster{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName1,
			},
			Spec: fleetv1beta1.MemberClusterSpec{
				State: fleetv1beta1.ClusterStateJoin,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName2,
			},
			Spec: fleetv1beta1.MemberClusterSpec{
				State: fleetv1beta1.ClusterStateJoin,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName3,
			},
			Spec: fleetv1beta1.MemberClusterSpec{
				State: fleetv1beta1.ClusterStateLeave,
			},
		},
	}

	unscheduledBinding := fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-3",
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			State: fleetv1beta1.BindingStateUnscheduled,
		},
	}
	associatedWithLeavingClusterBinding := fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-4",
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			State:                        fleetv1beta1.BindingStateBound,
			TargetCluster:                clusterName3,
			SchedulingPolicySnapshotName: altPolicyName,
		},
	}
	assocaitedWithDisappearedClusterBinding := fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-5",
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			State:                        fleetv1beta1.BindingStateScheduled,
			TargetCluster:                clusterName4,
			SchedulingPolicySnapshotName: policyName,
		},
	}
	obsoleteBinding := fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-6",
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			State:                        fleetv1beta1.BindingStateBound,
			TargetCluster:                clusterName1,
			SchedulingPolicySnapshotName: altPolicyName,
		},
	}
	boundBinding := fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-7",
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			State:                        fleetv1beta1.BindingStateBound,
			TargetCluster:                clusterName1,
			SchedulingPolicySnapshotName: policyName,
		},
	}
	scheduledBinding := fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-8",
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			State:                        fleetv1beta1.BindingStateScheduled,
			TargetCluster:                clusterName2,
			SchedulingPolicySnapshotName: policyName,
		},
	}

	bindings := []fleetv1beta1.ClusterResourceBinding{
		unscheduledBinding,
		associatedWithLeavingClusterBinding,
		assocaitedWithDisappearedClusterBinding,
		obsoleteBinding,
		boundBinding,
		scheduledBinding,
	}
	wantBound := []*fleetv1beta1.ClusterResourceBinding{&boundBinding}
	wantScheduled := []*fleetv1beta1.ClusterResourceBinding{&scheduledBinding}
	wantObsolete := []*fleetv1beta1.ClusterResourceBinding{&obsoleteBinding}
	wantDangling := []*fleetv1beta1.ClusterResourceBinding{&associatedWithLeavingClusterBinding, &assocaitedWithDisappearedClusterBinding}

	bound, scheduled, obsolete, dangling := classifyBindings(policy, bindings, clusters)
	if diff := cmp.Diff(bound, wantBound); diff != "" {
		t.Errorf("classifyBindings() bound diff (-got, +want): %s", diff)
	}

	if diff := cmp.Diff(scheduled, wantScheduled); diff != "" {
		t.Errorf("classifyBindings() scheduled diff (-got, +want) = %s", diff)
	}

	if diff := cmp.Diff(obsolete, wantObsolete); diff != "" {
		t.Errorf("classifyBindings() obsolete diff (-got, +want) = %s", diff)
	}

	if diff := cmp.Diff(dangling, wantDangling); diff != "" {
		t.Errorf("classifyBindings() dangling diff (-got, +want) = %s", diff)
	}
}

// TestMarkAsUnscheduled tests the markAsUnscheduled method.
func TestMarkAsUnscheduled(t *testing.T) {
	binding := fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			State: fleetv1beta1.BindingStateBound,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(&binding).
		Build()
	// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
	f := &framework{
		client: fakeClient,
	}

	ctx := context.Background()
	if err := f.markAsUnscheduledFor(ctx, []*fleetv1beta1.ClusterResourceBinding{&binding}); err != nil {
		t.Fatalf("markAsUnscheduledFor() = %v, want no error", err)
	}

	if err := fakeClient.Get(ctx, types.NamespacedName{Name: bindingName}, &binding); err != nil {
		t.Fatalf("Get cluster resource binding %s = %v, want no error", bindingName, err)
	}

	want := fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			State: fleetv1beta1.BindingStateUnscheduled,
		},
	}
	if diff := cmp.Diff(binding, want, ignoreTypeMetaAPIVersionKindFields, ignoreObjectMetaResourceVersionField); diff != "" {
		t.Errorf("binding diff (-got, +want): %s", diff)
	}
}

// TestRunPreFilterPlugins tests the runPreFilterPlugins method.
func TestRunPreFilterPlugins(t *testing.T) {
	dummyPreFilterPluginNameA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 0)
	dummyPreFilterPluginNameB := fmt.Sprintf(dummyAllPurposePluginNameFormat, 1)

	testCases := []struct {
		name                   string
		preFilterPlugins       []PreFilterPlugin
		wantSkippedPluginNames []string
		wantStatus             *Status
	}{
		{
			name: "single plugin, success",
			preFilterPlugins: []PreFilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameA,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) *Status {
						return nil
					},
				},
			},
		},
		{
			name: "multiple plugins, one success, one skip",
			preFilterPlugins: []PreFilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameA,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameB,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return NewNonErrorStatus(Skip, dummyPreFilterPluginNameB)
					},
				},
			},
			wantSkippedPluginNames: []string{dummyPreFilterPluginNameB},
		},
		{
			name: "single plugin, internal error",
			preFilterPlugins: []PreFilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameA,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) *Status {
						return FromError(fmt.Errorf("internal error"), dummyPreFilterPluginNameA)
					},
				},
			},
			wantStatus: FromError(fmt.Errorf("internal error"), dummyPreFilterPluginNameA),
		},
		{
			name: "single plugin, unschedulable",
			preFilterPlugins: []PreFilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameA,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) *Status {
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
			// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
			f := &framework{
				profile: profile,
			}

			ctx := context.Background()
			state := NewCycleState()
			policy := &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			}

			status := f.runPreFilterPlugins(ctx, state, policy)
			if diff := cmp.Diff(status, tc.wantStatus, cmp.AllowUnexported(Status{}), ignoredStatusFields); diff != "" {
				t.Errorf("runPreFilterPlugins() returned status diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestRunFilterPluginsFor tests the runFilterPluginsFor method.
func TestRunFilterPluginsFor(t *testing.T) {
	dummyFilterPluginNameA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 0)
	dummyFilterPluginNameB := fmt.Sprintf(dummyAllPurposePluginNameFormat, 1)

	testCases := []struct {
		name               string
		filterPlugins      []FilterPlugin
		skippedPluginNames []string
		wantStatus         *Status
	}{
		{
			name: "single plugin, success",
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
			},
		},
		{
			name: "multiple plugins, one success, one skipped",
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameB)
					},
				},
			},
			skippedPluginNames: []string{dummyFilterPluginNameB},
		},
		{
			name: "single plugin, internal error",
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return FromError(fmt.Errorf("internal error"), dummyFilterPluginNameA)
					},
				},
			},
			wantStatus: FromError(fmt.Errorf("internal error"), dummyFilterPluginNameA),
		},
		{
			name: "multiple plugins, one unschedulable, one success",
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA)
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
			},
			wantStatus: NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA),
		},
		{
			name: "single plugin, skip",
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
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
			// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
			f := &framework{
				profile: profile,
			}

			ctx := context.Background()
			state := NewCycleState()
			for _, name := range tc.skippedPluginNames {
				state.skippedFilterPlugins.Insert(name)
			}
			policy := &fleetv1beta1.ClusterSchedulingPolicySnapshot{
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
			if diff := cmp.Diff(status, tc.wantStatus, cmpopts.IgnoreUnexported(Status{}), ignoredStatusFields); diff != "" {
				t.Errorf("runFilterPluginsFor() returned status diff (-got, +want) = %s", diff)
			}
		})
	}
}

// TestRunFilterPlugins tests the runFilterPlugins method.
func TestRunFilterPlugins(t *testing.T) {
	dummyFilterPluginNameA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 0)
	dummyFilterPluginNameB := fmt.Sprintf(dummyAllPurposePluginNameFormat, 1)

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
		name           string
		filterPlugins  []FilterPlugin
		wantClusters   []*fleetv1beta1.MemberCluster
		wantFiltered   []*filteredClusterWithStatus
		expectedToFail bool
	}{
		{
			name: "three clusters, two filter plugins, all passed",
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
			},
			wantClusters: []*fleetv1beta1.MemberCluster{
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
			},
			wantFiltered: []*filteredClusterWithStatus{},
		},
		{
			name: "three clusters, two filter plugins, two filtered",
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == clusterName {
							return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA)
						}
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == anotherClusterName {
							return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameB)
						}
						return nil
					},
				},
			},
			wantClusters: []*fleetv1beta1.MemberCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: altClusterName,
					},
				},
			},
			wantFiltered: []*filteredClusterWithStatus{
				{
					cluster: &fleetv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: clusterName,
						},
					},
					status: NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA),
				},
				{
					cluster: &fleetv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: anotherClusterName,
						},
					},
					status: NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameB),
				},
			},
		},
		{
			name: "three clusters, two filter plugins, one success, one internal error on specific cluster",
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
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
			policy := &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			}

			passed, filtered, err := f.runFilterPlugins(ctx, state, policy, clusters)
			if tc.expectedToFail {
				if err == nil {
					t.Fatalf("runFilterPlugins(%v, %v, %v) = %v %v %v, want error", state, policy, clusters, passed, filtered, err)
				}
				return
			}

			// The method runs in parallel; as a result the order cannot be guaranteed.
			// Sort the results by cluster name for comparison.
			lessFuncCluster := func(cluster1, cluster2 *fleetv1beta1.MemberCluster) bool {
				return cluster1.Name < cluster2.Name
			}
			lessFuncFilteredCluster := func(filtered1, filtered2 *filteredClusterWithStatus) bool {
				return filtered1.cluster.Name < filtered2.cluster.Name
			}

			if diff := cmp.Diff(passed, tc.wantClusters, cmpopts.SortSlices(lessFuncCluster)); diff != "" {
				t.Errorf("passed clusters diff (-got, +want): %s", diff)
			}

			if diff := cmp.Diff(filtered, tc.wantFiltered, cmpopts.SortSlices(lessFuncFilteredCluster), cmp.AllowUnexported(filteredClusterWithStatus{}, Status{})); diff != "" {
				t.Errorf("filtered clusters diff (-got, +want): %s", diff)
			}
		})
	}
}
