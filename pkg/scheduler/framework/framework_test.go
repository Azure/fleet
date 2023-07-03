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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	anotherBindingName = "yet-another-test-binding"
	clusterName        = "bravelion"
	altClusterName     = "smartcat"
	anotherClusterName = "singingbutterfly"
	resourceVersion    = "1"
)

var (
	ignoreObjectMetaResourceVersionField = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")
	ignoreObjectMetaNameField            = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "Name")
	ignoreTypeMetaAPIVersionKindFields   = cmpopts.IgnoreFields(metav1.TypeMeta{}, "APIVersion", "Kind")
	ignoredStatusFields                  = cmpopts.IgnoreFields(Status{}, "reasons", "err")
<<<<<<< HEAD
	ignoredBindingWithPatchFields        = cmpopts.IgnoreFields(bindingWithPatch{}, "patch")
=======
	ignoredCondFields                    = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
>>>>>>> 5ba64b2 (Added more scheduler logic)

	lessFuncCluster = func(cluster1, cluster2 *fleetv1beta1.MemberCluster) bool {
		return cluster1.Name < cluster2.Name
	}
	lessFuncScoredCluster = func(scored1, scored2 ScoredCluster) bool {
		return scored1.Cluster.Name < scored2.Cluster.Name
	}
	lessFuncFilteredCluster = func(filtered1, filtered2 *filteredClusterWithStatus) bool {
		return filtered1.cluster.Name < filtered2.cluster.Name
	}
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

// TestMarkAsUnscheduledFor tests the markAsUnscheduledFor method.
func TestMarkAsUnscheduledFor(t *testing.T) {
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
			if diff := cmp.Diff(passed, tc.wantClusters, cmpopts.SortSlices(lessFuncCluster)); diff != "" {
				t.Errorf("passed clusters diff (-got, +want): %s", diff)
			}

			if diff := cmp.Diff(filtered, tc.wantFiltered, cmpopts.SortSlices(lessFuncFilteredCluster), cmp.AllowUnexported(filteredClusterWithStatus{}, Status{})); diff != "" {
				t.Errorf("filtered clusters diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestRunAllPluginsForPickAllPlacementType tests the runAllPluginsForPickAllPlacementType method.
func TestRunAllPluginsForPickAllPlacementType(t *testing.T) {
	dummyPreFilterPluginNameA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 0)
	dummyPreFilterPluginNameB := fmt.Sprintf(dummyAllPurposePluginNameFormat, 1)

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

	policy := &fleetv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
		},
	}

	testCases := []struct {
		name             string
		preFilterPlugins []PreFilterPlugin
		filterPlugins    []FilterPlugin
		wantScored       ScoredClusters
		wantFiltered     []*filteredClusterWithStatus
		expectedToFail   bool
	}{
		{
			name: "a prefilter plugin returns error",
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
						return FromError(fmt.Errorf("internal error"), dummyPreFilterPluginNameB)
					},
				},
			},
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
			expectedToFail: true,
		},
		{
			name: "a filter plugin returns error",
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
						return nil
					},
				},
			},
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
						if cluster.Name == altClusterName {
							return FromError(fmt.Errorf("internal error"), dummyFilterPluginNameB)
						}
						return nil
					},
				},
			},
			expectedToFail: true,
		},
		{
			name: "all clusters scored",
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
						return nil
					},
				},
			},
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
			wantScored: ScoredClusters{
				{
					Cluster: &clusters[0],
					Score:   &ClusterScore{},
				},
				{
					Cluster: &clusters[1],
					Score:   &ClusterScore{},
				},
				{
					Cluster: &clusters[2],
					Score:   &ClusterScore{},
				},
			},
			wantFiltered: []*filteredClusterWithStatus{},
		},
		{
			name: "all clusters filtered out",
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
						return nil
					},
				},
			},
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
						if cluster.Name != clusterName {
							return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameB)
						}
						return nil
					},
				},
			},
			wantScored: ScoredClusters{},
			wantFiltered: []*filteredClusterWithStatus{
				{
					cluster: &clusters[0],
					status:  NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA),
				},
				{
					cluster: &clusters[1],
					status:  NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameB),
				},
				{
					cluster: &clusters[2],
					status:  NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameB),
				},
			},
		},
		{
			name: "mixed",
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
						return nil
					},
				},
			},
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == altClusterName {
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
			wantScored: ScoredClusters{
				{
					Cluster: &clusters[0],
					Score:   &ClusterScore{},
				},
			},
			wantFiltered: []*filteredClusterWithStatus{
				{
					cluster: &clusters[1],
					status:  NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA),
				},
				{
					cluster: &clusters[2],
					status:  NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameB),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profile := NewProfile(dummyProfileName)
			for _, p := range tc.preFilterPlugins {
				profile.WithPreFilterPlugin(p)
			}
			for _, p := range tc.filterPlugins {
				profile.WithFilterPlugin(p)
			}
			f := &framework{
				profile:      profile,
				parallelizer: parallelizer.NewParallelizer(parallelizer.DefaultNumOfWorkers),
			}

			ctx := context.Background()
			state := NewCycleState()
			scored, filtered, err := f.runAllPluginsForPickAllPlacementType(ctx, state, policy, clusters)
			if tc.expectedToFail {
				if err == nil {
					t.Errorf("runAllPluginsForPickAllPlacementType(), want error")
				}
				return
			}

			// The method runs in parallel; as a result the order cannot be guaranteed.
			// Sort the results by cluster name for comparison.
			if diff := cmp.Diff(scored, tc.wantScored, cmpopts.SortSlices(lessFuncScoredCluster), cmp.AllowUnexported(ScoredCluster{})); diff != "" {
				t.Errorf("runAllPluginsForPickAllPlacementType() scored (-got, +want): %s", diff)
			}

			if diff := cmp.Diff(filtered, tc.wantFiltered, cmpopts.SortSlices(lessFuncFilteredCluster), cmp.AllowUnexported(filteredClusterWithStatus{}, Status{})); diff != "" {
				t.Errorf("runAllPluginsForPickAllPlacementType() filtered (-got, +want): %s", diff)
			}
		})
	}
}

// TestCrossReferencePickedClustersAndObsoleteBindings tests the crossReferencePickedClustersAndObsoleteBindings function.
func TestCrossReferencePickedCustersAndObsoleteBindings(t *testing.T) {
	policy := &fleetv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
		},
	}

	clusterName1 := "cluster-1"
	clusterName2 := "cluster-2"
	clusterName3 := "cluster-3"
	clusterName4 := "cluster-4"

	affinityScore1 := int32(10)
	topologySpreadScore1 := int32(2)
	affinityScore2 := int32(20)
	topologySpreadScore2 := int32(1)
	affinityScore3 := int32(30)
	topologySpreadScore3 := int32(0)

	sorted := ScoredClusters{
		{
			Cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
			},
			Score: &ClusterScore{
				TopologySpreadScore:   int(topologySpreadScore1),
				AffinityScore:         int(affinityScore1),
				BoundOrScheduledScore: 1,
			},
		},
		{
			Cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName2,
				},
			},
			Score: &ClusterScore{
				TopologySpreadScore:   int(topologySpreadScore2),
				AffinityScore:         int(affinityScore2),
				BoundOrScheduledScore: 0,
			},
		},
		{
			Cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName3,
				},
			},
			Score: &ClusterScore{
				TopologySpreadScore:   int(topologySpreadScore3),
				AffinityScore:         int(affinityScore3),
				BoundOrScheduledScore: 1,
			},
		},
	}

	// Note that these names are placeholders only; actual names should be generated one.
	bindingName1 := "binding-1"
	bindingName2 := "binding-2"
	bindingName3 := "binding-3"
	bindingName4 := "binding-4"

	testCases := []struct {
		name         string
		picked       ScoredClusters
		obsolete     []*fleetv1beta1.ClusterResourceBinding
		wantToCreate []*fleetv1beta1.ClusterResourceBinding
		wantToPatch  []*bindingWithPatch
		wantToDelete []*fleetv1beta1.ClusterResourceBinding
	}{
		{
			name:   "no matching obsolete bindings",
			picked: sorted,
			obsolete: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName4,
					},
				},
			},
			wantToCreate: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                        fleetv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName1,
						ClusterDecision: fleetv1beta1.ClusterDecision{
							ClusterName: clusterName1,
							Selected:    true,
							ClusterScore: &fleetv1beta1.ClusterScore{
								AffinityScore:       &affinityScore1,
								TopologySpreadScore: &topologySpreadScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                        fleetv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName2,
						ClusterDecision: fleetv1beta1.ClusterDecision{
							ClusterName: clusterName2,
							Selected:    true,
							ClusterScore: &fleetv1beta1.ClusterScore{
								AffinityScore:       &affinityScore2,
								TopologySpreadScore: &topologySpreadScore2,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                        fleetv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName3,
						ClusterDecision: fleetv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &fleetv1beta1.ClusterScore{
								AffinityScore:       &affinityScore3,
								TopologySpreadScore: &topologySpreadScore3,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
			wantToPatch: []*bindingWithPatch{},
			wantToDelete: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName4,
					},
				},
			},
		},
		{
			name:   "all matching obsolete bindings",
			picked: sorted,
			obsolete: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName2,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName3,
					},
				},
			},
			wantToCreate: []*fleetv1beta1.ClusterResourceBinding{},
			wantToPatch: []*bindingWithPatch{
				{
					updated: &fleetv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName1,
						},
						Spec: fleetv1beta1.ResourceBindingSpec{
							TargetCluster:                clusterName1,
							SchedulingPolicySnapshotName: policyName,
							ClusterDecision: fleetv1beta1.ClusterDecision{
								ClusterName: clusterName1,
								Selected:    true,
								ClusterScore: &fleetv1beta1.ClusterScore{
									AffinityScore:       &affinityScore1,
									TopologySpreadScore: &topologySpreadScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					patch: client.MergeFrom(&fleetv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName1,
						},
						Spec: fleetv1beta1.ResourceBindingSpec{
							TargetCluster: clusterName1,
						},
					}),
				},
				{
					updated: &fleetv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName2,
						},
						Spec: fleetv1beta1.ResourceBindingSpec{
							TargetCluster:                clusterName2,
							SchedulingPolicySnapshotName: policyName,
							ClusterDecision: fleetv1beta1.ClusterDecision{
								ClusterName: clusterName2,
								Selected:    true,
								ClusterScore: &fleetv1beta1.ClusterScore{
									AffinityScore:       &affinityScore2,
									TopologySpreadScore: &topologySpreadScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					patch: client.MergeFrom(&fleetv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName2,
						},
						Spec: fleetv1beta1.ResourceBindingSpec{
							TargetCluster: clusterName2,
						},
					}),
				},
				{
					updated: &fleetv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName3,
						},
						Spec: fleetv1beta1.ResourceBindingSpec{
							TargetCluster:                clusterName3,
							SchedulingPolicySnapshotName: policyName,
							ClusterDecision: fleetv1beta1.ClusterDecision{
								ClusterName: clusterName3,
								Selected:    true,
								ClusterScore: &fleetv1beta1.ClusterScore{
									AffinityScore:       &affinityScore3,
									TopologySpreadScore: &topologySpreadScore3,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					patch: client.MergeFrom(&fleetv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName3,
						},
						Spec: fleetv1beta1.ResourceBindingSpec{
							TargetCluster: clusterName3,
						},
					}),
				},
			},
			wantToDelete: []*fleetv1beta1.ClusterResourceBinding{},
		},
		{
			name:   "mixed",
			picked: sorted,
			obsolete: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName2,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName4,
					},
				},
			},
			wantToCreate: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:                        fleetv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName3,
						ClusterDecision: fleetv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &fleetv1beta1.ClusterScore{
								AffinityScore:       &affinityScore3,
								TopologySpreadScore: &topologySpreadScore3,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
			wantToPatch: []*bindingWithPatch{
				{
					updated: &fleetv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName1,
						},
						Spec: fleetv1beta1.ResourceBindingSpec{
							TargetCluster:                clusterName1,
							SchedulingPolicySnapshotName: policyName,
							ClusterDecision: fleetv1beta1.ClusterDecision{
								ClusterName: clusterName1,
								Selected:    true,
								ClusterScore: &fleetv1beta1.ClusterScore{
									AffinityScore:       &affinityScore1,
									TopologySpreadScore: &topologySpreadScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					patch: client.MergeFrom(&fleetv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName1,
						},
						Spec: fleetv1beta1.ResourceBindingSpec{
							TargetCluster: clusterName1,
						},
					}),
				},
				{
					updated: &fleetv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName2,
						},
						Spec: fleetv1beta1.ResourceBindingSpec{
							TargetCluster:                clusterName2,
							SchedulingPolicySnapshotName: policyName,
							ClusterDecision: fleetv1beta1.ClusterDecision{
								ClusterName: clusterName2,
								Selected:    true,
								ClusterScore: &fleetv1beta1.ClusterScore{
									AffinityScore:       &affinityScore2,
									TopologySpreadScore: &topologySpreadScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					patch: client.MergeFrom(&fleetv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName2,
						},
						Spec: fleetv1beta1.ResourceBindingSpec{
							TargetCluster: clusterName2,
						},
					}),
				},
			},
			wantToDelete: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName4,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			toCreate, toDelete, toPatch, err := crossReferencePickedCustersAndObsoleteBindings(crpName, policy, tc.picked, tc.obsolete)
			if err != nil {
				t.Errorf("crossReferencePickedClustersAndObsoleteBindings() = %v, want no error", err)
				return
			}

			if diff := cmp.Diff(toCreate, tc.wantToCreate, ignoreObjectMetaNameField); diff != "" {
				t.Errorf("crossReferencePickedClustersAndObsoleteBindings() toCreate diff (-got, +want) = %s", diff)
			}

			// Verify names separately.
			for _, binding := range toCreate {
				prefix := fmt.Sprintf("%s-%s-", crpName, binding.Spec.TargetCluster)
				if !strings.HasPrefix(binding.Name, prefix) {
					t.Errorf("toCreate binding name, got %s, want prefix %s", binding.Name, prefix)
				}
			}

			// Ignore the patch field (not exported in local package).
			if diff := cmp.Diff(toPatch, tc.wantToPatch, cmp.AllowUnexported(bindingWithPatch{}), ignoredBindingWithPatchFields); diff != "" {
				t.Errorf("crossReferencePickedClustersAndObsoleteBindings() toPatch diff (-got, +want): %s", diff)
			}

			if diff := cmp.Diff(toDelete, tc.wantToDelete); diff != "" {
				t.Errorf("crossReferencePickedClustersAndObsoleteBindings() toDelete diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestCreateBindings tests the createBindings method.
func TestCreateBindings(t *testing.T) {
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		Build()
	// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
	f := &framework{
		client: fakeClient,
	}

	toCreate := []*fleetv1beta1.ClusterResourceBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: bindingName,
			},
		},
	}

	ctx := context.Background()
	if err := f.createBindings(ctx, toCreate); err != nil {
		t.Fatalf("createBindings() = %v, want no error", err)
	}

	binding := &fleetv1beta1.ClusterResourceBinding{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: bindingName}, binding); err != nil {
		t.Fatalf("Get binding (%s) = %v, want no error", bindingName, err)
	}

	if diff := cmp.Diff(binding, toCreate[0], ignoreTypeMetaAPIVersionKindFields); diff != "" {
		t.Fatalf("created binding diff (-got, +want) = %s", diff)
	}
}

// TestUpdateBindings tests the updateBindings method.
func TestPatchBindings(t *testing.T) {
	binding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
			// Set the resource version; this is needed so that the calculated patch will not
			// include the resource version field.
			ResourceVersion: resourceVersion,
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			TargetCluster: clusterName,
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

	topologySpreadScore := int32(0)
	affinityScore := int32(1)
	updated := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
			// Set the resource version; this is needed so that the calculated patch will not
			// include the resource version field.
			ResourceVersion: resourceVersion,
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			TargetCluster:                clusterName,
			SchedulingPolicySnapshotName: policyName,
			ClusterDecision: fleetv1beta1.ClusterDecision{
				ClusterName: clusterName,
				Selected:    true,
				ClusterScore: &fleetv1beta1.ClusterScore{
					TopologySpreadScore: &topologySpreadScore,
					AffinityScore:       &affinityScore,
				},
				Reason: pickedByPolicyReason,
			},
		},
	}

	toPatch := []*bindingWithPatch{
		{
			updated: updated,
			patch:   client.MergeFrom(binding),
		},
	}

	ctx := context.Background()
	if err := f.patchBindings(ctx, toPatch); err != nil {
		t.Fatalf("patchBindings() = %v, want no error", err)
	}

	current := &fleetv1beta1.ClusterResourceBinding{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: bindingName}, current); err != nil {
		t.Fatalf("Get binding (%s) = %v, want no error", bindingName, err)
	}

	if diff := cmp.Diff(current, updated, ignoreTypeMetaAPIVersionKindFields); diff != "" {
		t.Fatalf("patched binding diff (-got, +want) = %s", diff)
	}
}

// TestManipulateBindings tests the manipulateBindings method.
func TestManipulateBindings(t *testing.T) {
	toCreateBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
		},
	}
	toPatchBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            altBindingName,
			ResourceVersion: resourceVersion,
		},
	}
	toDeleteBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: anotherBindingName,
		},
	}

	topologySpreadScore := int32(0)
	affinityScore := int32(1)
	updatedBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: altBindingName,
			// Set the resource version; this is needed so that the calculated patch will not
			// include the resource version field.
			ResourceVersion: resourceVersion,
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			TargetCluster:                clusterName,
			SchedulingPolicySnapshotName: policyName,
			ClusterDecision: fleetv1beta1.ClusterDecision{
				ClusterName: clusterName,
				Selected:    true,
				ClusterScore: &fleetv1beta1.ClusterScore{
					TopologySpreadScore: &topologySpreadScore,
					AffinityScore:       &affinityScore,
				},
				Reason: pickedByPolicyReason,
			},
		},
	}

	unscheduledBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: anotherBindingName,
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			State: fleetv1beta1.BindingStateUnscheduled,
		},
	}

	policy := &fleetv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(toPatchBinding, toDeleteBinding).
		Build()
	// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
	f := &framework{
		client: fakeClient,
	}

	ctx := context.Background()

	toCreate := []*fleetv1beta1.ClusterResourceBinding{toCreateBinding}
	toPatch := []*bindingWithPatch{
		{
			updated: updatedBinding,
			patch:   client.MergeFrom(toPatchBinding),
		},
	}
	toDelete := []*fleetv1beta1.ClusterResourceBinding{toDeleteBinding}
	if err := f.manipulateBindings(ctx, policy, toCreate, toDelete, toPatch); err != nil {
		t.Fatalf("manipulateBindings() = %v, want no error", err)
	}

	// Check if the requested binding has been created.
	createdBinding := &fleetv1beta1.ClusterResourceBinding{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: bindingName}, createdBinding); err != nil {
		t.Errorf("Get() binding %s = %v, want no error", bindingName, err)
	}
	if diff := cmp.Diff(createdBinding, toCreateBinding, ignoreTypeMetaAPIVersionKindFields); diff != "" {
		t.Errorf("created binding %s diff (-got, +want): %s", bindingName, diff)
	}

	// Check if the requested binding has been patched.
	patchedBinding := &fleetv1beta1.ClusterResourceBinding{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: altBindingName}, patchedBinding); err != nil {
		t.Errorf("Get() binding %s = %v, want no error", altBindingName, err)
	}
	if diff := cmp.Diff(patchedBinding, updatedBinding, ignoreTypeMetaAPIVersionKindFields); diff != "" {
		t.Errorf("patched binding %s diff (-got, +want): %s", altBindingName, diff)
	}

	// Check if the requested binding has been deleted.
	deletedBinding := &fleetv1beta1.ClusterResourceBinding{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: anotherBindingName}, deletedBinding); err != nil {
		t.Errorf("Get() binding %s = %v, want no error", anotherBindingName, err)
	}
	if diff := cmp.Diff(deletedBinding, unscheduledBinding, ignoreTypeMetaAPIVersionKindFields, ignoreObjectMetaResourceVersionField); diff != "" {
		t.Errorf("unscheduled binding %s diff (-got, +want): %s", anotherBindingName, diff)
	}
}
