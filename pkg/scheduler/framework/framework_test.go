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
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/clustereligibilitychecker"
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

	bindingNameTemplate = "binding-%d"
	clusterNameTemplate = "cluster-%d"
)

var (
	ignoreObjectMetaResourceVersionField = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")
	ignoreObjectAnnotationField          = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "Annotations")
	ignoreObjectMetaNameField            = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "Name")
	ignoreTypeMetaAPIVersionKindFields   = cmpopts.IgnoreFields(metav1.TypeMeta{}, "APIVersion", "Kind")
	ignoredStatusFields                  = cmpopts.IgnoreFields(Status{}, "reasons", "err")
	ignoredBindingWithPatchFields        = cmpopts.IgnoreFields(bindingWithPatch{}, "patch")
	ignoredCondFields                    = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
	ignoreCycleStateFields               = cmpopts.IgnoreFields(CycleState{}, "store", "clusters", "scheduledOrBoundBindings", "obsoleteBindings")
	ignoreClusterDecisionScoreAndReasonFields = cmpopts.IgnoreFields(placementv1beta1.ClusterDecision{}, "ClusterScore", "Reason")

	lessFuncCluster = func(cluster1, cluster2 *clusterv1beta1.MemberCluster) bool {
		return cluster1.Name < cluster2.Name
	}
	lessFuncScoredCluster = func(scored1, scored2 *ScoredCluster) bool {
		return scored1.Cluster.Name < scored2.Cluster.Name
	}
	lessFuncFilteredCluster = func(filtered1, filtered2 *filteredClusterWithStatus) bool {
		return filtered1.cluster.Name < filtered2.cluster.Name
	}
)

// A few utilities for generating a large number of objects.
var (
	defaultFilteredStatus = NewNonErrorStatus(ClusterUnschedulable, dummyPluginName)

	generateResourceBindings = func(count int, startIdx int) []*placementv1beta1.ClusterResourceBinding {
		bindings := make([]*placementv1beta1.ClusterResourceBinding, 0, count)

		for i := 0; i < count; i++ {
			bindings = append(bindings, &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(bindingNameTemplate, i+startIdx),
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					ClusterDecision: placementv1beta1.ClusterDecision{
						ClusterName: fmt.Sprintf(clusterNameTemplate, i+startIdx),
						Selected:    true,
					},
				},
			})
		}
		return bindings
	}

	generateClusterDecisions = func(count int, startIdx int, selected bool) []placementv1beta1.ClusterDecision {
		decisions := make([]placementv1beta1.ClusterDecision, 0, count)

		for i := 0; i < count; i++ {
			newDecision := placementv1beta1.ClusterDecision{
				ClusterName: fmt.Sprintf(clusterNameTemplate, i+startIdx),
				Selected:    selected,
			}

			decisions = append(decisions, newDecision)
		}
		return decisions
	}

	generateNotPickedScoredClusters = func(count int, startIdx int) ScoredClusters {
		notPicked := make(ScoredClusters, 0, count)

		for i := 0; i < count; i++ {
			notPicked = append(notPicked, &ScoredCluster{
				Cluster: &clusterv1beta1.MemberCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(clusterNameTemplate, i+startIdx),
					},
				},
				Score: &ClusterScore{},
			})
		}
		return notPicked
	}

	generatedFilterdClusterWithStatus = func(count int, startIdx int) []*filteredClusterWithStatus {
		filtered := make([]*filteredClusterWithStatus, 0, count)

		for i := 0; i < count; i++ {
			filtered = append(filtered, &filteredClusterWithStatus{
				cluster: &clusterv1beta1.MemberCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(clusterNameTemplate, i+startIdx),
					},
				},
				status: defaultFilteredStatus,
			})
		}
		return filtered
	}
)

// TO-DO (chenyu1): expand the test cases as development stablizes.

// TestMain sets up the test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme.
	if err := placementv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

// TestCollectClusters tests the collectClusters method.
func TestCollectClusters(t *testing.T) {
	cluster := clusterv1beta1.MemberCluster{
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

	want := []clusterv1beta1.MemberCluster{cluster}
	if diff := cmp.Diff(clusters, want); diff != "" {
		t.Fatalf("collectClusters() diff (-got, +want) = %s", diff)
	}
}

// TestCollectBindings tests the collectBindings method.
func TestCollectBindings(t *testing.T) {
	binding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
			Labels: map[string]string{
				placementv1beta1.CRPTrackingLabel: crpName,
			},
		},
	}
	altCRPName := "another-test-placement"

	testCases := []struct {
		name    string
		binding *placementv1beta1.ClusterResourceBinding
		crpName string
		want    []placementv1beta1.ClusterResourceBinding
	}{
		{
			name:    "found matching bindings",
			binding: binding,
			crpName: crpName,
			want:    []placementv1beta1.ClusterResourceBinding{*binding},
		},
		{
			name:    "no matching bindings",
			binding: binding,
			crpName: altCRPName,
			want:    []placementv1beta1.ClusterResourceBinding{},
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
	policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
		},
	}
	deleteTime := metav1.Now()

	clusterName1 := fmt.Sprintf(clusterNameTemplate, 1)
	clusterName2 := fmt.Sprintf(clusterNameTemplate, 2)
	clusterName3 := fmt.Sprintf(clusterNameTemplate, 3)
	clusterName4 := fmt.Sprintf(clusterNameTemplate, 4)
	clusters := []clusterv1beta1.MemberCluster{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName1,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName2,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              clusterName3,
				DeletionTimestamp: &deleteTime,
			},
		},
	}

	deletingBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "binding-2",
			DeletionTimestamp: &deleteTime,
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State: placementv1beta1.BindingStateUnscheduled,
		},
	}

	unscheduledBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-3",
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State: placementv1beta1.BindingStateUnscheduled,
		},
	}
	associatedWithLeavingClusterBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-4",
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:                        placementv1beta1.BindingStateBound,
			TargetCluster:                clusterName3,
			SchedulingPolicySnapshotName: altPolicyName,
		},
	}
	assocaitedWithDisappearedClusterBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-5",
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:                        placementv1beta1.BindingStateScheduled,
			TargetCluster:                clusterName4,
			SchedulingPolicySnapshotName: policyName,
		},
	}
	obsoleteBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-6",
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:                        placementv1beta1.BindingStateBound,
			TargetCluster:                clusterName1,
			SchedulingPolicySnapshotName: altPolicyName,
		},
	}
	boundBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-7",
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:                        placementv1beta1.BindingStateBound,
			TargetCluster:                clusterName1,
			SchedulingPolicySnapshotName: policyName,
		},
	}
	scheduledBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-8",
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:                        placementv1beta1.BindingStateScheduled,
			TargetCluster:                clusterName2,
			SchedulingPolicySnapshotName: policyName,
		},
	}

	bindings := []placementv1beta1.ClusterResourceBinding{
		deletingBinding,
		unscheduledBinding,
		associatedWithLeavingClusterBinding,
		assocaitedWithDisappearedClusterBinding,
		obsoleteBinding,
		boundBinding,
		scheduledBinding,
	}
	wantBound := []*placementv1beta1.ClusterResourceBinding{&boundBinding}
	wantScheduled := []*placementv1beta1.ClusterResourceBinding{&scheduledBinding}
	wantObsolete := []*placementv1beta1.ClusterResourceBinding{&obsoleteBinding}
	wantUnscheduled := []*placementv1beta1.ClusterResourceBinding{&unscheduledBinding}
	wantDangling := []*placementv1beta1.ClusterResourceBinding{&associatedWithLeavingClusterBinding, &assocaitedWithDisappearedClusterBinding}

	bound, scheduled, obsolete, unscheduled, dangling := classifyBindings(policy, bindings, clusters)
	if diff := cmp.Diff(bound, wantBound); diff != "" {
		t.Errorf("classifyBindings() bound diff (-got, +want): %s", diff)
	}

	if diff := cmp.Diff(scheduled, wantScheduled); diff != "" {
		t.Errorf("classifyBindings() scheduled diff (-got, +want) = %s", diff)
	}

	if diff := cmp.Diff(obsolete, wantObsolete); diff != "" {
		t.Errorf("classifyBindings() obsolete diff (-got, +want) = %s", diff)
	}

	if diff := cmp.Diff(unscheduled, wantUnscheduled); diff != "" {
		t.Errorf("classifyBindings() unscheduled diff (-got, +want) = %s", diff)
	}

	if diff := cmp.Diff(dangling, wantDangling); diff != "" {
		t.Errorf("classifyBindings() dangling diff (-got, +want) = %s", diff)
	}
}

// TestMarkAsUnscheduledFor tests the markAsUnscheduledFor method.
func TestMarkAsUnscheduledFor(t *testing.T) {
	boundBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State: placementv1beta1.BindingStateBound,
		},
	}

	scheduledBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: altBindingName,
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State: placementv1beta1.BindingStateScheduled,
		},
	}
	// setup fake client with bindings
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(&boundBinding, &scheduledBinding).
		Build()
	// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
	f := &framework{
		client: fakeClient,
	}
	// call markAsUnscheduledFor
	ctx := context.Background()
	if err := f.markAsUnscheduledFor(ctx, []*placementv1beta1.ClusterResourceBinding{&boundBinding, &scheduledBinding}); err != nil {
		t.Fatalf("markAsUnscheduledFor() = %v, want no error", err)
	}
	// check if the boundBinding has been updated
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: bindingName}, &boundBinding); err != nil {
		t.Fatalf("Get cluster resource boundBinding %s = %v, want no error", bindingName, err)
	}
	want := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
			Annotations: map[string]string{
				placementv1beta1.PreviousBindingStateAnnotation: string(placementv1beta1.BindingStateBound),
			},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State: placementv1beta1.BindingStateUnscheduled,
		},
	}
	if diff := cmp.Diff(boundBinding, want, ignoreTypeMetaAPIVersionKindFields, ignoreObjectMetaResourceVersionField); diff != "" {
		t.Errorf("boundBinding diff (-got, +want): %s", diff)
	}
	// check if the scheduledBinding has been updated with the correct annotation
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: altBindingName}, &scheduledBinding); err != nil {
		t.Fatalf("Get cluster resource boundBinding %s = %v, want no error", altBindingName, err)
	}
	want = placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: altBindingName,
			Annotations: map[string]string{
				placementv1beta1.PreviousBindingStateAnnotation: string(placementv1beta1.BindingStateScheduled),
			},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State: placementv1beta1.BindingStateUnscheduled,
		},
	}
	if diff := cmp.Diff(scheduledBinding, want, ignoreTypeMetaAPIVersionKindFields, ignoreObjectMetaResourceVersionField); diff != "" {
		t.Errorf("scheduledBinding diff (-got, +want): %s", diff)
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
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) *Status {
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
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameB,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
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
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) *Status {
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
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) *Status {
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
			state := NewCycleState([]clusterv1beta1.MemberCluster{}, []*placementv1beta1.ClusterResourceBinding{})
			policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
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
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
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
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
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
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
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
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA)
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
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
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						return NewNonErrorStatus(Skip, dummyFilterPluginNameA)
					},
				},
			},
			wantStatus: FromError(fmt.Errorf("internal error"), dummyFilterPluginNameA),
		},
		{
			name: "multiple plugins, one success, one already selected",
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						return NewNonErrorStatus(ClusterAlreadySelected, dummyFilterPluginNameA)
					},
				},
			},
			wantStatus: NewNonErrorStatus(ClusterAlreadySelected, dummyFilterPluginNameA),
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
			state := NewCycleState([]clusterv1beta1.MemberCluster{}, []*placementv1beta1.ClusterResourceBinding{})
			for _, name := range tc.skippedPluginNames {
				state.skippedFilterPlugins.Insert(name)
			}
			policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			}
			cluster := &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			}

			status := f.runFilterPluginsFor(ctx, state, policy, cluster)
			if diff := cmp.Diff(status, tc.wantStatus, cmp.AllowUnexported(Status{}), ignoredStatusFields); diff != "" {
				t.Errorf("runFilterPluginsFor() returned status diff (-got, +want) = %s", diff)
			}
		})
	}
}

// TestRunFilterPlugins tests the runFilterPlugins method.
func TestRunFilterPlugins(t *testing.T) {
	dummyFilterPluginNameA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 0)
	dummyFilterPluginNameB := fmt.Sprintf(dummyAllPurposePluginNameFormat, 1)

	clusters := []clusterv1beta1.MemberCluster{
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
		wantClusters   []*clusterv1beta1.MemberCluster
		wantFiltered   []*filteredClusterWithStatus
		expectedToFail bool
	}{
		{
			name: "three clusters, two filter plugins, all passed",
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
			},
			wantClusters: []*clusterv1beta1.MemberCluster{
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
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == clusterName {
							return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA)
						}
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == anotherClusterName {
							return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameB)
						}
						return nil
					},
				},
			},
			wantClusters: []*clusterv1beta1.MemberCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: altClusterName,
					},
				},
			},
			wantFiltered: []*filteredClusterWithStatus{
				{
					cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: clusterName,
						},
					},
					status: NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA),
				},
				{
					cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: anotherClusterName,
						},
					},
					status: NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameB),
				},
			},
		},
		{
			name: "three clusters, two filter plugins, two already selected",
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == clusterName {
							return NewNonErrorStatus(ClusterAlreadySelected, dummyFilterPluginNameA)
						}
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == anotherClusterName {
							return NewNonErrorStatus(ClusterAlreadySelected, dummyFilterPluginNameB)
						}
						return nil
					},
				},
			},
			wantClusters: []*clusterv1beta1.MemberCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: altClusterName,
					},
				},
			},
			wantFiltered: []*filteredClusterWithStatus{},
		},
		{
			name: "three clusters, two filter plugins, one success, one internal error on specific cluster",
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
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
			state := NewCycleState([]clusterv1beta1.MemberCluster{}, []*placementv1beta1.ClusterResourceBinding{})
			policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
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

	clusters := []clusterv1beta1.MemberCluster{
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

	policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
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
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameB,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return FromError(fmt.Errorf("internal error"), dummyPreFilterPluginNameB)
					},
				},
			},
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
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
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameB,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
			},
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
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
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameB,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
			},
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
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
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameB,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
			},
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == clusterName {
							return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA)
						}
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
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
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameB,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
			},
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == altClusterName {
							return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA)
						}
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
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
			state := NewCycleState([]clusterv1beta1.MemberCluster{}, []*placementv1beta1.ClusterResourceBinding{})
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

// TestCrossReferencePickedClustersAndDeDupBindings tests the crossReferencePickedClustersAndDeDupBindings function.
func TestCrossReferencePickedClustersAndDeDupBindings(t *testing.T) {
	policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
		},
	}

	clusterName1 := fmt.Sprintf(clusterNameTemplate, 1)
	clusterName2 := fmt.Sprintf(clusterNameTemplate, 2)
	clusterName3 := fmt.Sprintf(clusterNameTemplate, 3)
	clusterName4 := fmt.Sprintf(clusterNameTemplate, 4)

	affinityScore1 := int32(10)
	topologySpreadScore1 := int32(2)
	affinityScore2 := int32(20)
	topologySpreadScore2 := int32(1)
	affinityScore3 := int32(30)
	topologySpreadScore3 := int32(0)

	sorted := ScoredClusters{
		{
			Cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
			},
			Score: &ClusterScore{
				TopologySpreadScore:            int(topologySpreadScore1),
				AffinityScore:                  int(affinityScore1),
				ObsoletePlacementAffinityScore: 1,
			},
		},
		{
			Cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName2,
				},
			},
			Score: &ClusterScore{
				TopologySpreadScore:            int(topologySpreadScore2),
				AffinityScore:                  int(affinityScore2),
				ObsoletePlacementAffinityScore: 0,
			},
		},
		{
			Cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName3,
				},
			},
			Score: &ClusterScore{
				TopologySpreadScore:            int(topologySpreadScore3),
				AffinityScore:                  int(affinityScore3),
				ObsoletePlacementAffinityScore: 1,
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
		unscheduled  []*placementv1beta1.ClusterResourceBinding
		obsolete     []*placementv1beta1.ClusterResourceBinding
		wantToCreate []*placementv1beta1.ClusterResourceBinding
		wantToPatch  []*bindingWithPatch
		wantToDelete []*placementv1beta1.ClusterResourceBinding
	}{
		{
			name:   "no matching obsolete bindings",
			picked: sorted,
			obsolete: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName4,
					},
				},
			},
			wantToCreate: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName1,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName1,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
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
							placementv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName2,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName2,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
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
							placementv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName3,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								AffinityScore:       &affinityScore3,
								TopologySpreadScore: &topologySpreadScore3,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
			wantToPatch: []*bindingWithPatch{},
			wantToDelete: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName4,
					},
				},
			},
		},
		{
			name:   "all matching obsolete bindings",
			picked: sorted,
			obsolete: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateBound,
						TargetCluster: clusterName1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName2,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateBound,
						TargetCluster: clusterName3,
					},
				},
			},
			wantToCreate: []*placementv1beta1.ClusterResourceBinding{},
			wantToPatch: []*bindingWithPatch{
				{
					updated: &placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName1,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster:                clusterName1,
							SchedulingPolicySnapshotName: policyName,
							State:                        placementv1beta1.BindingStateBound,
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName1,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore1,
									TopologySpreadScore: &topologySpreadScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					patch: client.MergeFrom(&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName1,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: clusterName1,
						},
					}),
				},
				{
					updated: &placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName2,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster:                clusterName2,
							SchedulingPolicySnapshotName: policyName,
							State:                        placementv1beta1.BindingStateScheduled,
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName2,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore2,
									TopologySpreadScore: &topologySpreadScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					patch: client.MergeFrom(&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName2,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: clusterName2,
						},
					}),
				},
				{
					updated: &placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName3,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster:                clusterName3,
							SchedulingPolicySnapshotName: policyName,
							State:                        placementv1beta1.BindingStateBound,
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName3,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore3,
									TopologySpreadScore: &topologySpreadScore3,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					patch: client.MergeFrom(&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName3,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: clusterName3,
						},
					}),
				},
			},
			wantToDelete: []*placementv1beta1.ClusterResourceBinding{},
		},
		{
			name:   "mixed obsolete bindings",
			picked: sorted,
			obsolete: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateBound,
						TargetCluster: clusterName1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName2,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateBound,
						TargetCluster: clusterName4,
					},
				},
			},
			wantToCreate: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName3,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
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
					updated: &placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName1,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster:                clusterName1,
							State:                        placementv1beta1.BindingStateBound,
							SchedulingPolicySnapshotName: policyName,
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName1,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore1,
									TopologySpreadScore: &topologySpreadScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					patch: client.MergeFrom(&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName1,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: clusterName1,
						},
					}),
				},
				{
					updated: &placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName2,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster:                clusterName2,
							SchedulingPolicySnapshotName: policyName,
							State:                        placementv1beta1.BindingStateScheduled,
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName2,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore2,
									TopologySpreadScore: &topologySpreadScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					patch: client.MergeFrom(&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName2,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: clusterName2,
						},
					}),
				},
			},
			wantToDelete: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateBound,
						TargetCluster: clusterName4,
					},
				},
			},
		},
		{
			name:   "no matching unscheduled bindings",
			picked: sorted,
			unscheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName4,
					},
				},
			},
			wantToCreate: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName1,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName1,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
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
							placementv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName2,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName2,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
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
							placementv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName3,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								AffinityScore:       &affinityScore3,
								TopologySpreadScore: &topologySpreadScore3,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
			wantToPatch:  []*bindingWithPatch{},
			wantToDelete: []*placementv1beta1.ClusterResourceBinding{},
		},
		{
			name:   "matching 1 unscheduled bindings",
			picked: sorted,
			unscheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
						Annotations: map[string]string{
							placementv1beta1.PreviousBindingStateAnnotation: string(placementv1beta1.BindingStateBound),
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName2,
					},
				},
			},
			wantToCreate: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName1,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName1,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								AffinityScore:       &affinityScore1,
								TopologySpreadScore: &topologySpreadScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName3,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
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
					updated: &placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name:        bindingName2,
							Annotations: map[string]string{},
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster:                clusterName2,
							SchedulingPolicySnapshotName: policyName,
							State:                        placementv1beta1.BindingStateBound,
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName2,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore2,
									TopologySpreadScore: &topologySpreadScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					patch: client.MergeFrom(&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName2,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: clusterName2,
						},
					}),
				},
			},
			wantToDelete: []*placementv1beta1.ClusterResourceBinding{},
		},
		{
			name:   "matching 1 unscheduled with previous state scheduled and 1 obsolete bindings",
			picked: sorted,
			unscheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
						Annotations: map[string]string{
							placementv1beta1.PreviousBindingStateAnnotation: string(placementv1beta1.BindingStateScheduled),
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName2,
					},
				},
			},
			obsolete: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateBound,
						TargetCluster: clusterName1,
					},
				},
			},
			wantToCreate: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: crpName,
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policyName,
						TargetCluster:                clusterName3,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
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
					updated: &placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name:        bindingName1,
							Annotations: map[string]string{},
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster:                clusterName1,
							SchedulingPolicySnapshotName: policyName,
							State:                        placementv1beta1.BindingStateBound,
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName1,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore1,
									TopologySpreadScore: &topologySpreadScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					patch: client.MergeFrom(&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName1,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: clusterName1,
						},
					}),
				},
				{
					updated: &placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name:        bindingName2,
							Annotations: map[string]string{},
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster:                clusterName2,
							SchedulingPolicySnapshotName: policyName,
							State:                        placementv1beta1.BindingStateScheduled,
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName2,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore2,
									TopologySpreadScore: &topologySpreadScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					patch: client.MergeFromWithOptions(&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName2,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: clusterName2,
						},
					}, client.MergeFromWithOptimisticLock{}),
				},
			},
			wantToDelete: []*placementv1beta1.ClusterResourceBinding{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			toCreate, toDelete, toPatch, err := crossReferencePickedClustersAndDeDupBindings(crpName, policy, tc.picked, tc.unscheduled, tc.obsolete)
			if err != nil {
				t.Errorf("crossReferencePickedClustersAndDeDupBindings test `%s`, err = %v, want no error", tc.name, err)
				return
			}

			if diff := cmp.Diff(toCreate, tc.wantToCreate, ignoreObjectMetaNameField); diff != "" {
				t.Errorf("crossReferencePickedClustersAndDeDupBindings test `%s` toCreate diff (-got, +want) = %s", tc.name, diff)
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
				t.Errorf("crossReferencePickedClustersAndDeDupBindings test `%s` toPatch diff (-got, +want): %s", tc.name, diff)
			}

			if diff := cmp.Diff(toDelete, tc.wantToDelete); diff != "" {
				t.Errorf("crossReferencePickedClustersAndDeDupBindings test `%s` toDelete diff (-got, +want): %s", tc.name, diff)
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

	toCreate := []*placementv1beta1.ClusterResourceBinding{
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

	binding := &placementv1beta1.ClusterResourceBinding{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: bindingName}, binding); err != nil {
		t.Fatalf("Get binding (%s) = %v, want no error", bindingName, err)
	}

	if diff := cmp.Diff(binding, toCreate[0], ignoreTypeMetaAPIVersionKindFields); diff != "" {
		t.Fatalf("created binding diff (-got, +want) = %s", diff)
	}
}

// TestUpdateBindings tests the updateBindings method.
func TestPatchBindings(t *testing.T) {
	binding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
			// Set the resource version; this is needed so that the calculated patch will not
			// include the resource version field.
			ResourceVersion: resourceVersion,
		},
		Spec: placementv1beta1.ResourceBindingSpec{
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
	updated := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
			// Set the resource version; this is needed so that the calculated patch will not
			// include the resource version field.
			ResourceVersion: resourceVersion,
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			TargetCluster:                clusterName,
			SchedulingPolicySnapshotName: policyName,
			ClusterDecision: placementv1beta1.ClusterDecision{
				ClusterName: clusterName,
				Selected:    true,
				ClusterScore: &placementv1beta1.ClusterScore{
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

	current := &placementv1beta1.ClusterResourceBinding{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: bindingName}, current); err != nil {
		t.Fatalf("Get binding (%s) = %v, want no error", bindingName, err)
	}

	if diff := cmp.Diff(current, updated, ignoreTypeMetaAPIVersionKindFields); diff != "" {
		t.Fatalf("patched binding diff (-got, +want) = %s", diff)
	}
}

// TestManipulateBindings tests the manipulateBindings method.
func TestManipulateBindings(t *testing.T) {
	toCreateBinding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
		},
	}
	toPatchBinding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            altBindingName,
			ResourceVersion: resourceVersion,
		},
	}
	toDeleteBinding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: anotherBindingName,
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State: placementv1beta1.BindingStateBound,
		},
	}

	topologySpreadScore := int32(0)
	affinityScore := int32(1)
	updatedBinding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: altBindingName,
			// Set the resource version; this is needed so that the calculated patch will not
			// include the resource version field.
			ResourceVersion: resourceVersion,
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			TargetCluster:                clusterName,
			SchedulingPolicySnapshotName: policyName,
			ClusterDecision: placementv1beta1.ClusterDecision{
				ClusterName: clusterName,
				Selected:    true,
				ClusterScore: &placementv1beta1.ClusterScore{
					TopologySpreadScore: &topologySpreadScore,
					AffinityScore:       &affinityScore,
				},
				Reason: pickedByPolicyReason,
			},
		},
	}

	wantDeleteddBinding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: anotherBindingName,
			Annotations: map[string]string{
				placementv1beta1.PreviousBindingStateAnnotation: string(placementv1beta1.BindingStateBound),
			},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State: placementv1beta1.BindingStateUnscheduled,
		},
	}

	policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
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

	toCreate := []*placementv1beta1.ClusterResourceBinding{toCreateBinding}
	toPatch := []*bindingWithPatch{
		{
			updated: updatedBinding,
			patch:   client.MergeFrom(toPatchBinding),
		},
	}
	toDelete := []*placementv1beta1.ClusterResourceBinding{toDeleteBinding}
	if err := f.manipulateBindings(ctx, policy, toCreate, toDelete, toPatch); err != nil {
		t.Fatalf("manipulateBindings() = %v, want no error", err)
	}

	// Check if the requested binding has been created.
	createdBinding := &placementv1beta1.ClusterResourceBinding{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: bindingName}, createdBinding); err != nil {
		t.Errorf("Get() binding %s = %v, want no error", bindingName, err)
	}
	if diff := cmp.Diff(createdBinding, toCreateBinding, ignoreTypeMetaAPIVersionKindFields); diff != "" {
		t.Errorf("created binding %s diff (-got, +want): %s", bindingName, diff)
	}

	// Check if the requested binding has been patched.
	patchedBinding := &placementv1beta1.ClusterResourceBinding{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: altBindingName}, patchedBinding); err != nil {
		t.Errorf("Get() binding %s = %v, want no error", altBindingName, err)
	}
	if diff := cmp.Diff(patchedBinding, updatedBinding, ignoreTypeMetaAPIVersionKindFields); diff != "" {
		t.Errorf("patched binding %s diff (-got, +want): %s", altBindingName, diff)
	}

	// Check if the requested binding has been deleted.
	deletedBinding := &placementv1beta1.ClusterResourceBinding{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: anotherBindingName}, deletedBinding); err != nil {
		t.Errorf("Get() binding %s = %v, want no error", anotherBindingName, err)
	}
	if diff := cmp.Diff(deletedBinding, wantDeleteddBinding, ignoreTypeMetaAPIVersionKindFields, ignoreObjectMetaResourceVersionField); diff != "" {
		t.Errorf("unscheduled binding %s diff (-got, +want): %s", anotherBindingName, diff)
	}
}

// TestUpdatePolicySnapshotStatusFromBindings tests the updatePolicySnapshotStatusFromBindings method.
func TestUpdatePolicySnapshotStatusFromBindings(t *testing.T) {
	defaultMaxUnselectedClusterDecisionCount := 20

	affinityScore1 := int32(1)
	topologySpreadScore1 := int32(10)
	affinityScore2 := int32(0)
	topologySpreadScore2 := int32(20)
	affinityScore3 := int32(-1)
	topologySpreadScore3 := int32(0)

	filteredStatus := NewNonErrorStatus(ClusterUnschedulable, dummyPluginName, "filtered")

	crpGeneration := 1
	policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
			Annotations: map[string]string{
				placementv1beta1.CRPGenerationAnnotation: fmt.Sprintf("%d", crpGeneration),
			},
		},
	}

	testCases := []struct {
		name                              string
		maxUnselectedClusterDecisionCount int
		notPicked                         ScoredClusters
		filtered                          []*filteredClusterWithStatus
		existing                          [][]*placementv1beta1.ClusterResourceBinding
		wantDecisions                     []placementv1beta1.ClusterDecision
		wantCondition                     metav1.Condition
	}{
		{
			name:                              "no filtered/not picked clusters",
			maxUnselectedClusterDecisionCount: defaultMaxUnselectedClusterDecisionCount,
			existing: [][]*placementv1beta1.ClusterResourceBinding{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore1,
									TopologySpreadScore: &topologySpreadScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: altBindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: altClusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore2,
									TopologySpreadScore: &topologySpreadScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
				},
			},
			wantDecisions: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						AffinityScore:       &affinityScore1,
						TopologySpreadScore: &topologySpreadScore1,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: altClusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						AffinityScore:       &affinityScore2,
						TopologySpreadScore: &topologySpreadScore2,
					},
					Reason: pickedByPolicyReason,
				},
			},
			wantCondition: newScheduledCondition(policy, metav1.ConditionTrue, fullyScheduledReason, fullyScheduledMessage),
		},
		{
			name:                              "with filtered clusters and existing bindings",
			maxUnselectedClusterDecisionCount: defaultMaxUnselectedClusterDecisionCount,
			existing: [][]*placementv1beta1.ClusterResourceBinding{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore1,
									TopologySpreadScore: &topologySpreadScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: altBindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: altClusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore2,
									TopologySpreadScore: &topologySpreadScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
				},
			},
			filtered: []*filteredClusterWithStatus{
				{
					cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: anotherClusterName,
						},
					},
					status: filteredStatus,
				},
			},
			wantDecisions: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						AffinityScore:       &affinityScore1,
						TopologySpreadScore: &topologySpreadScore1,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: altClusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						AffinityScore:       &affinityScore2,
						TopologySpreadScore: &topologySpreadScore2,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: anotherClusterName,
					Selected:    false,
					Reason:      filteredStatus.String(),
				},
			},
			wantCondition: newScheduledCondition(policy, metav1.ConditionTrue, fullyScheduledReason, fullyScheduledMessage),
		},
		{
			name:                              "with not picked clusters and existing bindings",
			maxUnselectedClusterDecisionCount: defaultMaxUnselectedClusterDecisionCount,
			existing: [][]*placementv1beta1.ClusterResourceBinding{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore1,
									TopologySpreadScore: &topologySpreadScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: altBindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: altClusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore2,
									TopologySpreadScore: &topologySpreadScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
				},
			},
			notPicked: ScoredClusters{
				{
					Cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: anotherClusterName,
						},
					},
					Score: &ClusterScore{
						TopologySpreadScore: int(topologySpreadScore3),
						AffinityScore:       int(affinityScore3),
					},
				},
			},
			wantDecisions: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						AffinityScore:       &affinityScore1,
						TopologySpreadScore: &topologySpreadScore1,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: altClusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						AffinityScore:       &affinityScore2,
						TopologySpreadScore: &topologySpreadScore2,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: anotherClusterName,
					Selected:    false,
					Reason:      notPickedByScoreReason,
					ClusterScore: &placementv1beta1.ClusterScore{
						AffinityScore:       &affinityScore3,
						TopologySpreadScore: &topologySpreadScore3,
					},
				},
			},
			wantCondition: newScheduledCondition(policy, metav1.ConditionTrue, fullyScheduledReason, fullyScheduledMessage),
		},
		{
			name:                              "with both filtered/not picked clusters and existing bindings",
			maxUnselectedClusterDecisionCount: defaultMaxUnselectedClusterDecisionCount,
			existing: [][]*placementv1beta1.ClusterResourceBinding{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore1,
									TopologySpreadScore: &topologySpreadScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
				},
			},
			notPicked: ScoredClusters{
				{
					Cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: altClusterName,
						},
					},
					Score: &ClusterScore{
						TopologySpreadScore: int(topologySpreadScore3),
						AffinityScore:       int(affinityScore3),
					},
				},
			},
			filtered: []*filteredClusterWithStatus{
				{
					cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: anotherClusterName,
						},
					},
					status: filteredStatus,
				},
			},
			wantDecisions: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						AffinityScore:       &affinityScore1,
						TopologySpreadScore: &topologySpreadScore1,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: altClusterName,
					Selected:    false,
					ClusterScore: &placementv1beta1.ClusterScore{
						AffinityScore:       &affinityScore3,
						TopologySpreadScore: &topologySpreadScore3,
					},
					Reason: notPickedByScoreReason,
				},
				{
					ClusterName: anotherClusterName,
					Selected:    false,
					Reason:      filteredStatus.String(),
				},
			},
			wantCondition: newScheduledCondition(policy, metav1.ConditionTrue, fullyScheduledReason, fullyScheduledMessage),
		},
		{
			name:                              "none",
			maxUnselectedClusterDecisionCount: defaultMaxUnselectedClusterDecisionCount,
			wantCondition:                     newScheduledCondition(policy, metav1.ConditionTrue, fullyScheduledReason, fullyScheduledMessage),
		},
		{
			name:                              "too many filtered",
			maxUnselectedClusterDecisionCount: 1,
			existing: [][]*placementv1beta1.ClusterResourceBinding{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore1,
									TopologySpreadScore: &topologySpreadScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
				},
			},
			filtered: []*filteredClusterWithStatus{
				{
					cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: altClusterName,
						},
					},
					status: filteredStatus,
				},
				{
					cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: anotherClusterName,
						},
					},
					status: filteredStatus,
				},
			},
			wantDecisions: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						AffinityScore:       &affinityScore1,
						TopologySpreadScore: &topologySpreadScore1,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: altClusterName,
					Selected:    false,
					Reason:      filteredStatus.String(),
				},
			},
			wantCondition: newScheduledCondition(policy, metav1.ConditionTrue, fullyScheduledReason, fullyScheduledMessage),
		},
		{
			name:                              "too many not picked",
			maxUnselectedClusterDecisionCount: 1,
			existing: [][]*placementv1beta1.ClusterResourceBinding{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									AffinityScore:       &affinityScore1,
									TopologySpreadScore: &topologySpreadScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
				},
			},
			notPicked: ScoredClusters{
				{
					Cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: altClusterName,
						},
					},
					Score: &ClusterScore{
						AffinityScore:       int(affinityScore2),
						TopologySpreadScore: int(topologySpreadScore2),
					},
				},
				{
					Cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: anotherClusterName,
						},
					},
					Score: &ClusterScore{
						AffinityScore:       int(affinityScore3),
						TopologySpreadScore: int(topologySpreadScore3),
					},
				},
			},
			wantDecisions: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						AffinityScore:       &affinityScore1,
						TopologySpreadScore: &topologySpreadScore1,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: altClusterName,
					Selected:    false,
					Reason:      notPickedByScoreReason,
					ClusterScore: &placementv1beta1.ClusterScore{
						AffinityScore:       &affinityScore2,
						TopologySpreadScore: &topologySpreadScore2,
					},
				},
			},
			wantCondition: newScheduledCondition(policy, metav1.ConditionTrue, fullyScheduledReason, fullyScheduledMessage),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(policy).
				Build()
			// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
			f := &framework{
				client:                            fakeClient,
				maxUnselectedClusterDecisionCount: tc.maxUnselectedClusterDecisionCount,
			}

			ctx := context.Background()
			numOfClusters := 0
			for _, bindingSet := range tc.existing {
				numOfClusters += len(bindingSet)
			}
			if err := f.updatePolicySnapshotStatusFromBindings(ctx, policy, numOfClusters, tc.notPicked, tc.filtered, tc.existing...); err != nil {
				t.Fatalf("updatePolicySnapshotStatusFrom() = %v, want no error", err)
			}

			updatedPolicy := &placementv1beta1.ClusterSchedulingPolicySnapshot{}
			if err := f.client.Get(ctx, types.NamespacedName{Name: policyName}, updatedPolicy); err != nil {
				t.Fatalf("Get policy snapshot, got %v, want no error", err)
			}

			if diff := cmp.Diff(updatedPolicy.Status.ClusterDecisions, tc.wantDecisions); diff != "" {
				t.Errorf("policy snapshot status cluster decisions not equal (-got, +want): %s", diff)
			}

			updatedCondition := meta.FindStatusCondition(updatedPolicy.Status.Conditions, string(placementv1beta1.PolicySnapshotScheduled))
			if diff := cmp.Diff(updatedCondition, &tc.wantCondition, ignoredCondFields); diff != "" {
				t.Errorf("policy snapshot scheduled condition not equal (-got, +want): %s", diff)
			}

			if policy.Status.ObservedCRPGeneration != int64(crpGeneration) {
				t.Errorf("policy snapshot observed CRP generation: got %d, want %d", policy.Status.ObservedCRPGeneration, crpGeneration)
			}
		})
	}
}

// TestShouldDownscale tests the shouldDownscale function.
func TestShouldDownscale(t *testing.T) {
	testCases := []struct {
		name      string
		policy    *placementv1beta1.ClusterSchedulingPolicySnapshot
		desired   int
		present   int
		obsolete  int
		wantAct   bool
		wantCount int
	}{
		{
			name: "should not downscale (pick all)",
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
				},
			},
		},
		{
			name: "should not downscale (enough bindings)",
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickNPlacementType,
					},
				},
			},
			desired: 1,
			present: 1,
		},
		{
			name: "should downscale (not enough bindings)",
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickNPlacementType,
					},
				},
			},
			desired:   0,
			present:   1,
			wantAct:   true,
			wantCount: 1,
		},
		{
			name: "should downscale (obsolete bindings)",
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickNPlacementType,
					},
				},
			},
			desired:   1,
			present:   1,
			obsolete:  1,
			wantAct:   true,
			wantCount: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			act, count := shouldDownscale(tc.policy, tc.desired, tc.present, tc.obsolete)
			if act != tc.wantAct || count != tc.wantCount {
				t.Fatalf("shouldDownscale() = %v, %v, want %v, %v", act, count, tc.wantAct, tc.wantCount)
			}
		})
	}
}

// TestSortByClusterScoreAndName tests the sortByClusterScoreAndName function.
func TestSortByClusterScoreAndName(t *testing.T) {
	topologySpreadScore1 := int32(0)
	affinityScore1 := int32(10)
	topologySpreadScore2 := int32(1)
	affinityScore2 := int32(20)
	topologySpreadScore3 := int32(0)
	affinityScore3 := int32(5)

	testCases := []struct {
		name     string
		bindings []*placementv1beta1.ClusterResourceBinding
		want     []*placementv1beta1.ClusterResourceBinding
	}{
		{
			name: "no scores assigned to any cluster",
			bindings: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: altBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: altClusterName,
					},
				},
			},
			want: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: altBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: altClusterName,
					},
				},
			},
		},
		{
			name: "no scores assigned to one cluster",
			bindings: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: altBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: altClusterName,
					},
				},
			},
			want: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: altBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: altClusterName,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
						},
					},
				},
			},
		},
		{
			name: "different scores",
			bindings: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: altBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: altClusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore2,
								AffinityScore:       &affinityScore2,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: anotherBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: anotherClusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore3,
								AffinityScore:       &affinityScore3,
							},
						},
					},
				},
			},
			want: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: anotherBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: anotherClusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore3,
								AffinityScore:       &affinityScore3,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: altBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: altClusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore2,
								AffinityScore:       &affinityScore2,
							},
						},
					},
				},
			},
		},
		{
			name: "same score, different names",
			bindings: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: altBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: altClusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: anotherBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: anotherClusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
						},
					},
				},
			},
			want: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: anotherBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: anotherClusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: altBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: altClusterName,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sorted := sortByClusterScoreAndName(tc.bindings)
			if diff := cmp.Diff(sorted, tc.want); diff != "" {
				t.Errorf("sortByClusterScoreAndName() diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestNewSchedulingDecisionsFromBindings tests the newSchedulingDecisionsFromBindings function.
func TestNewSchedulingDecisionsFromBindings(t *testing.T) {
	topologySpreadScore1 := int32(1)
	affinityScore1 := int32(10)
	topologySpreadScore2 := int32(0)
	affinityScore2 := int32(20)
	topologySpreadScore3 := int32(2)
	affinityScore3 := int32(5)

	filteredStatus := NewNonErrorStatus(ClusterUnschedulable, dummyPlugin, dummyReasons...)

	testCases := []struct {
		name                              string
		maxUnselectedClusterDecisionCount int
		notPicked                         ScoredClusters
		filtered                          []*filteredClusterWithStatus
		existing                          [][]*placementv1beta1.ClusterResourceBinding
		want                              []placementv1beta1.ClusterDecision
	}{
		{
			name:                              "no not picked/filtered clusters, small number of existing bindings",
			maxUnselectedClusterDecisionCount: 20,
			existing: [][]*placementv1beta1.ClusterResourceBinding{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									TopologySpreadScore: &topologySpreadScore1,
									AffinityScore:       &affinityScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: altBindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: altClusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									TopologySpreadScore: &topologySpreadScore2,
									AffinityScore:       &affinityScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
				},
			},
			want: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore1,
						AffinityScore:       &affinityScore1,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: altClusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore2,
						AffinityScore:       &affinityScore2,
					},
					Reason: pickedByPolicyReason,
				},
			},
		},
		{
			name:                              "with not picked clusters, small number of existing bindings",
			maxUnselectedClusterDecisionCount: 20,
			notPicked: ScoredClusters{
				{
					Cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: anotherClusterName,
						},
					},
					Score: &ClusterScore{
						AffinityScore:       int(affinityScore3),
						TopologySpreadScore: int(topologySpreadScore3),
					},
				},
			},
			existing: [][]*placementv1beta1.ClusterResourceBinding{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									TopologySpreadScore: &topologySpreadScore1,
									AffinityScore:       &affinityScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: altBindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: altClusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									TopologySpreadScore: &topologySpreadScore2,
									AffinityScore:       &affinityScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
				},
			},
			want: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore1,
						AffinityScore:       &affinityScore1,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: altClusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore2,
						AffinityScore:       &affinityScore2,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: anotherClusterName,
					Selected:    false,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore3,
						AffinityScore:       &affinityScore3,
					},
					Reason: notPickedByScoreReason,
				},
			},
		},
		{
			name:                              "with both not picked and filtered clusters, small number of existing bindings",
			maxUnselectedClusterDecisionCount: 20,
			notPicked: ScoredClusters{
				{
					Cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: anotherClusterName,
						},
					},
					Score: &ClusterScore{
						AffinityScore:       int(affinityScore3),
						TopologySpreadScore: int(topologySpreadScore3),
					},
				},
			},
			filtered: []*filteredClusterWithStatus{
				{
					cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: altClusterName,
						},
					},
					status: filteredStatus,
				},
			},
			existing: [][]*placementv1beta1.ClusterResourceBinding{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									TopologySpreadScore: &topologySpreadScore1,
									AffinityScore:       &affinityScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
				},
			},
			want: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore1,
						AffinityScore:       &affinityScore1,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: anotherClusterName,
					Selected:    false,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore3,
						AffinityScore:       &affinityScore3,
					},
					Reason: notPickedByScoreReason,
				},
				{
					ClusterName: altClusterName,
					Selected:    false,
					Reason:      filteredStatus.String(),
				},
			},
		},
		{
			name:                              "with filtered clusters, small number of existing bindings",
			maxUnselectedClusterDecisionCount: 20,
			filtered: []*filteredClusterWithStatus{
				{
					cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: anotherClusterName,
						},
					},
					status: filteredStatus,
				},
			},
			existing: [][]*placementv1beta1.ClusterResourceBinding{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									TopologySpreadScore: &topologySpreadScore1,
									AffinityScore:       &affinityScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: altBindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: altClusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									TopologySpreadScore: &topologySpreadScore2,
									AffinityScore:       &affinityScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
				},
			},
			want: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore1,
						AffinityScore:       &affinityScore1,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: altClusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore2,
						AffinityScore:       &affinityScore2,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: anotherClusterName,
					Selected:    false,
					Reason:      filteredStatus.String(),
				},
			},
		},
		{
			name:                              "with filtered clusters (count exceeding limit), small number of existing bindings",
			maxUnselectedClusterDecisionCount: 0,
			filtered: []*filteredClusterWithStatus{
				{
					cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: anotherClusterName,
						},
					},
					status: filteredStatus,
				},
			},
			existing: [][]*placementv1beta1.ClusterResourceBinding{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									TopologySpreadScore: &topologySpreadScore1,
									AffinityScore:       &affinityScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: altBindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: altClusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									TopologySpreadScore: &topologySpreadScore2,
									AffinityScore:       &affinityScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
				},
			},
			want: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore1,
						AffinityScore:       &affinityScore1,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: altClusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore2,
						AffinityScore:       &affinityScore2,
					},
					Reason: pickedByPolicyReason,
				},
			},
		},
		{
			name:                              "with not picked clusters (count exceeding limit), small number of existing bindings",
			maxUnselectedClusterDecisionCount: 0,
			notPicked: ScoredClusters{
				{
					Cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: anotherClusterName,
						},
					},
					Score: &ClusterScore{
						AffinityScore:       int(affinityScore3),
						TopologySpreadScore: int(topologySpreadScore3),
					},
				},
			},
			existing: [][]*placementv1beta1.ClusterResourceBinding{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: bindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: clusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									TopologySpreadScore: &topologySpreadScore1,
									AffinityScore:       &affinityScore1,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: altBindingName,
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							ClusterDecision: placementv1beta1.ClusterDecision{
								ClusterName: altClusterName,
								Selected:    true,
								ClusterScore: &placementv1beta1.ClusterScore{
									TopologySpreadScore: &topologySpreadScore2,
									AffinityScore:       &affinityScore2,
								},
								Reason: pickedByPolicyReason,
							},
						},
					},
				},
			},
			want: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore1,
						AffinityScore:       &affinityScore1,
					},
					Reason: pickedByPolicyReason,
				},
				{
					ClusterName: altClusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore2,
						AffinityScore:       &affinityScore2,
					},
					Reason: pickedByPolicyReason,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			decisions := newSchedulingDecisionsFromBindings(tc.maxUnselectedClusterDecisionCount, tc.notPicked, tc.filtered, tc.existing...)
			if diff := cmp.Diff(tc.want, decisions); diff != "" {
				t.Errorf("newSchedulingDecisionsFrom() decisions diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestNewSchedulingDecisionsFrom tests a special case in the newSchedulingDecisionsFrom function,
// specifically the case where the number of new decisions exceeds the API limit.
func TestNewSchedulingDecisionsFromOversized(t *testing.T) {
	wantDecisions1 := generateClusterDecisions(1000, 0, true)

	wantDecisions2 := generateClusterDecisions(980, 0, true)
	wantDecisions2 = append(wantDecisions2, generateClusterDecisions(20, 980, false)...)

	wantDecisions3 := generateClusterDecisions(10, 0, true)
	wantDecisions3 = append(wantDecisions3, generateClusterDecisions(20, 10, false)...)

	testCases := []struct {
		name                              string
		maxUnselectedClusterDecisionCount int
		notPicked                         ScoredClusters
		filtered                          []*filteredClusterWithStatus
		bindingSets                       [][]*placementv1beta1.ClusterResourceBinding
		wantDecisions                     []placementv1beta1.ClusterDecision
	}{
		{
			name:                              "too many selected clusters",
			maxUnselectedClusterDecisionCount: 20,
			bindingSets: [][]*placementv1beta1.ClusterResourceBinding{
				generateResourceBindings(550, 0),
				generateResourceBindings(550, 550),
			},
			wantDecisions: wantDecisions1,
		},
		{
			name:                              "count of not picked clusters exceeding API limit",
			maxUnselectedClusterDecisionCount: 50,
			bindingSets: [][]*placementv1beta1.ClusterResourceBinding{
				generateResourceBindings(490, 0),
				generateResourceBindings(490, 490),
			},
			notPicked:     generateNotPickedScoredClusters(50, 980),
			wantDecisions: wantDecisions2,
		},
		{
			name:                              "count of not picked clusters exceeding custom limit",
			maxUnselectedClusterDecisionCount: 20,
			bindingSets: [][]*placementv1beta1.ClusterResourceBinding{
				generateResourceBindings(10, 0),
			},
			notPicked:     generateNotPickedScoredClusters(50, 10),
			wantDecisions: wantDecisions3,
		},
		{
			name:                              "count of filtered clusters exceeding API limit",
			maxUnselectedClusterDecisionCount: 50,
			bindingSets: [][]*placementv1beta1.ClusterResourceBinding{
				generateResourceBindings(490, 0),
				generateResourceBindings(490, 490),
			},
			notPicked:     nil,
			filtered:      generatedFilterdClusterWithStatus(50, 980),
			wantDecisions: wantDecisions2,
		},
		{
			name:                              "count of filtered clusters exceeding custom limit",
			maxUnselectedClusterDecisionCount: 20,
			bindingSets: [][]*placementv1beta1.ClusterResourceBinding{
				generateResourceBindings(10, 0),
			},
			notPicked:     nil,
			filtered:      generatedFilterdClusterWithStatus(50, 10),
			wantDecisions: wantDecisions3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			decisions := newSchedulingDecisionsFromBindings(tc.maxUnselectedClusterDecisionCount, tc.notPicked, tc.filtered, tc.bindingSets...)
			if diff := cmp.Diff(decisions, tc.wantDecisions, ignoreClusterDecisionScoreAndReasonFields); diff != "" {
				t.Errorf("newSchedulingDecisionsFrom() decisions diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestEqualDecisions tests the equalDecisions function.
func TestEqualDecisions(t *testing.T) {
	topologySpreadScore1 := int32(1)
	affinityScore1 := int32(10)
	topologySpreadScore2 := int32(0)
	affinityScore2 := int32(20)
	topologySpreadScore3 := int32(1)
	affinityScore3 := int32(10)

	testCases := []struct {
		name    string
		current []placementv1beta1.ClusterDecision
		desired []placementv1beta1.ClusterDecision
		want    bool
	}{
		{
			name:    "not equal (different lengths)",
			current: []placementv1beta1.ClusterDecision{},
			desired: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore1,
						AffinityScore:       &affinityScore1,
					},
					Reason: pickedByPolicyReason,
				},
			},
		},
		{
			name: "not equal (same length, different contents)",
			current: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore2,
						AffinityScore:       &affinityScore2,
					},
					Reason: pickedByPolicyReason,
				},
			},
			desired: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore1,
						AffinityScore:       &affinityScore1,
					},
					Reason: pickedByPolicyReason,
				},
			},
		},
		{
			name: "equal",
			current: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore3,
						AffinityScore:       &affinityScore3,
					},
					Reason: pickedByPolicyReason,
				},
			},
			desired: []placementv1beta1.ClusterDecision{
				{
					ClusterName: clusterName,
					Selected:    true,
					ClusterScore: &placementv1beta1.ClusterScore{
						TopologySpreadScore: &topologySpreadScore1,
						AffinityScore:       &affinityScore1,
					},
					Reason: pickedByPolicyReason,
				},
			},
			want: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if isEqual := equalDecisions(tc.current, tc.desired); isEqual != tc.want {
				t.Errorf("equalDecisions() = %v, want %v", isEqual, tc.want)
			}
		})
	}
}

// TestRunPostBatchPlugins tests the runPostBatchPlugins method.
func TestRunPostBatchPlugins(t *testing.T) {
	dummyPostBatchPluginNameA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 0)
	dummyPostBatchPluginNameB := fmt.Sprintf(dummyAllPurposePluginNameFormat, 1)

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
				&DummyAllPurposePlugin{
					name: dummyPostBatchPluginNameA,
					postBatchRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (size int, status *Status) {
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
				&DummyAllPurposePlugin{
					name: dummyPostBatchPluginNameA,
					postBatchRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (size int, status *Status) {
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
				&DummyAllPurposePlugin{
					name: dummyPostBatchPluginNameA,
					postBatchRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (size int, status *Status) {
						return 2, nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyPostBatchPluginNameB,
					postBatchRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (size int, status *Status) {
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
				&DummyAllPurposePlugin{
					name: dummyPostBatchPluginNameA,
					postBatchRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (size int, status *Status) {
						return 0, FromError(fmt.Errorf("internal error"), dummyPostBatchPluginNameA)
					},
				},
				&DummyAllPurposePlugin{
					name: dummyPostBatchPluginNameB,
					postBatchRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (size int, status *Status) {
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
				&DummyAllPurposePlugin{
					name: dummyPostBatchPluginNameA,
					postBatchRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (size int, status *Status) {
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
				&DummyAllPurposePlugin{
					name: dummyPostBatchPluginNameA,
					postBatchRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (size int, status *Status) {
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
			state := NewCycleState([]clusterv1beta1.MemberCluster{}, []*placementv1beta1.ClusterResourceBinding{})
			state.desiredBatchSize = tc.desiredBatchSize
			policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			}
			batchLimit, status := f.runPostBatchPlugins(ctx, state, policy)
			if batchLimit != tc.wantBatchLimit {
				t.Errorf("runPostBatchPlugins() batch limit = %d, want %d", batchLimit, tc.wantBatchLimit)
			}
			if diff := cmp.Diff(status, tc.wantStatus, cmpopts.IgnoreUnexported(Status{}), ignoredStatusFields); diff != "" {
				t.Errorf("runPostBatchPlugins() status diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestRunPreScorePlugins tests the runPreScorePlugins method.
func TestRunPreScorePlugins(t *testing.T) {
	dummyPreScorePluginNameA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 0)
	dummyPreScorePluginNameB := fmt.Sprintf(dummyAllPurposePluginNameFormat, 1)

	testCases := []struct {
		name                   string
		preScorePlugins        []PreScorePlugin
		wantSkippedPluginNames []string
		wantStatus             *Status
	}{
		{
			name: "single plugin, success",
			preScorePlugins: []PreScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyPreScorePluginNameA,
					preScoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) *Status {
						return nil
					},
				},
			},
		},
		{
			name: "multiple plugins, one success, one skip",
			preScorePlugins: []PreScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyPreScorePluginNameA,
					preScoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) *Status {
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyPreScorePluginNameB,
					preScoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return NewNonErrorStatus(Skip, dummyPreScorePluginNameB)
					},
				},
			},
			wantSkippedPluginNames: []string{dummyPreScorePluginNameB},
		},
		{
			name: "single plugin, internal error",
			preScorePlugins: []PreScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyPreScorePluginNameA,
					preScoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return FromError(fmt.Errorf("internal error"), dummyPreScorePluginNameA)
					},
				},
			},
			wantStatus: FromError(fmt.Errorf("internal error"), dummyPreScorePluginNameA),
		},
		{
			name: "single plugin, unschedulable",
			preScorePlugins: []PreScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyPreScorePluginNameA,
					preScoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return NewNonErrorStatus(ClusterUnschedulable, dummyPreScorePluginNameA)
					},
				},
			},
			wantStatus: FromError(fmt.Errorf("cluster is unschedulable"), dummyPreScorePluginNameA),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profile := NewProfile(dummyProfileName)
			for _, p := range tc.preScorePlugins {
				profile.WithPreScorePlugin(p)
			}
			f := &framework{
				profile: profile,
			}

			ctx := context.Background()
			state := NewCycleState([]clusterv1beta1.MemberCluster{}, []*placementv1beta1.ClusterResourceBinding{})
			policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			}

			status := f.runPreScorePlugins(ctx, state, policy)
			if diff := cmp.Diff(status, tc.wantStatus, cmp.AllowUnexported(Status{}), ignoredStatusFields); diff != "" {
				t.Errorf("runPreScorePlugins() status diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestDownScale tests the downscale method.
func TestDownscale(t *testing.T) {
	bindingName1 := fmt.Sprintf(bindingNameTemplate, 1)
	bindingName2 := fmt.Sprintf(bindingNameTemplate, 2)
	bindingName3 := fmt.Sprintf(bindingNameTemplate, 3)
	bindingName4 := fmt.Sprintf(bindingNameTemplate, 4)
	bindingName5 := fmt.Sprintf(bindingNameTemplate, 5)
	bindingName6 := fmt.Sprintf(bindingNameTemplate, 6)

	clusterName1 := fmt.Sprintf(clusterNameTemplate, 1)
	clusterName2 := fmt.Sprintf(clusterNameTemplate, 2)
	clusterName3 := fmt.Sprintf(clusterNameTemplate, 3)
	clusterName4 := fmt.Sprintf(clusterNameTemplate, 4)
	clusterName5 := fmt.Sprintf(clusterNameTemplate, 5)
	clusterName6 := fmt.Sprintf(clusterNameTemplate, 6)

	topologySpreadScore1 := int32(1)
	affinityScore1 := int32(10)
	topologySpreadScore2 := int32(0)
	affinityScore2 := int32(20)

	testCases := []struct {
		name                 string
		scheduled            []*placementv1beta1.ClusterResourceBinding
		bound                []*placementv1beta1.ClusterResourceBinding
		count                int
		wantUpdatedScheduled []*placementv1beta1.ClusterResourceBinding
		wantUpdatedBound     []*placementv1beta1.ClusterResourceBinding
		wantUnscheduled      []*placementv1beta1.ClusterResourceBinding
		expectedToFail       bool
	}{
		{
			name: "downscale count is zero",
			scheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateScheduled,
					},
				},
			},
			bound: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateBound,
					},
				},
			},
			count: 0,
			wantUpdatedScheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateScheduled,
					},
				},
			},
			wantUpdatedBound: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateBound,
					},
				},
			},
			wantUnscheduled: []*placementv1beta1.ClusterResourceBinding{},
		},
		{
			name: "invalid downscale count",
			scheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateScheduled,
					},
				},
			},
			bound: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateBound,
					},
				},
			},
			count:          3,
			expectedToFail: true,
		},
		{
			name: "trim part of scheduled bindings only",
			scheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName3,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName1,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName1,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName2,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName2,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore2,
								AffinityScore:       &affinityScore2,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
			bound: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateBound,
					},
				},
			},
			count: 2,
			wantUpdatedScheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName3,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
			wantUpdatedBound: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateBound,
					},
				},
			},
			wantUnscheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateUnscheduled,
						TargetCluster: clusterName1,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName1,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateUnscheduled,
						TargetCluster: clusterName2,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName2,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore2,
								AffinityScore:       &affinityScore2,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
		},
		{
			name: "trim all scheduled bindings",
			scheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName3,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName1,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName1,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName2,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName2,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore2,
								AffinityScore:       &affinityScore2,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
			bound: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateBound,
					},
				},
			},
			count:                3,
			wantUpdatedScheduled: nil,
			wantUpdatedBound: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateBound,
					},
				},
			},
			wantUnscheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateUnscheduled,
						TargetCluster: clusterName3,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateUnscheduled,
						TargetCluster: clusterName1,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName1,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateUnscheduled,
						TargetCluster: clusterName2,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName2,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore2,
								AffinityScore:       &affinityScore2,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
		},
		{
			name: "trim all scheduled bindings + part of bound bindings",
			scheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName3,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName1,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName1,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName2,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName2,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore2,
								AffinityScore:       &affinityScore2,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
			bound: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateBound,
						TargetCluster: clusterName5,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName5,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName5,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateBound,
						TargetCluster: clusterName6,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName6,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore2,
								AffinityScore:       &affinityScore2,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName6,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateBound,
						TargetCluster: clusterName4,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName4,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
			count:                4,
			wantUpdatedScheduled: nil,
			wantUpdatedBound: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName6,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateBound,
						TargetCluster: clusterName4,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName4,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName4,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateBound,
						TargetCluster: clusterName5,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName5,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
			wantUnscheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateUnscheduled,
						TargetCluster: clusterName3,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateUnscheduled,
						TargetCluster: clusterName1,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName1,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateUnscheduled,
						TargetCluster: clusterName2,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName2,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore2,
								AffinityScore:       &affinityScore2,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName5,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateUnscheduled,
						TargetCluster: clusterName6,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName6,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore2,
								AffinityScore:       &affinityScore2,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
		},
		{
			name: "trim all bindings",
			scheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName1,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName1,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateScheduled,
						TargetCluster: clusterName2,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName2,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
			bound: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateBound,
						TargetCluster: clusterName3,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
			count:                3,
			wantUpdatedScheduled: nil,
			wantUpdatedBound:     nil,
			wantUnscheduled: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateUnscheduled,
						TargetCluster: clusterName1,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName1,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName2,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateUnscheduled,
						TargetCluster: clusterName2,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName2,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         placementv1beta1.BindingStateUnscheduled,
						TargetCluster: clusterName3,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName: clusterName3,
							Selected:    true,
							ClusterScore: &placementv1beta1.ClusterScore{
								TopologySpreadScore: &topologySpreadScore1,
								AffinityScore:       &affinityScore1,
							},
							Reason: pickedByPolicyReason,
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			for _, binding := range tc.scheduled {
				fakeClientBuilder.WithObjects(binding)
			}
			for _, binding := range tc.bound {
				fakeClientBuilder.WithObjects(binding)
			}
			fakeClient := fakeClientBuilder.Build()
			// Construct framework manually instead of using NewFramework to avoid mocking the
			// controller manager.
			f := &framework{
				client: fakeClient,
			}

			ctx := context.Background()
			scheduled, bound, err := f.downscale(ctx, tc.scheduled, tc.bound, tc.count)
			if tc.expectedToFail {
				if err == nil {
					t.Fatalf("downscaled() = nil, want error")
				}

				return
			}

			if diff := cmp.Diff(scheduled, tc.wantUpdatedScheduled, ignoreObjectMetaResourceVersionField, ignoreTypeMetaAPIVersionKindFields); diff != "" {
				t.Errorf("downscale() updated scheduled diff (-got, +want) = %s", diff)
			}

			if diff := cmp.Diff(bound, tc.wantUpdatedBound, ignoreObjectMetaResourceVersionField, ignoreTypeMetaAPIVersionKindFields); diff != "" {
				t.Errorf("downscale() updated bound diff (-got, +want) = %s", diff)
			}

			// Verify that some bindings have been set to the unscheduled state.
			for _, wantUnscheduledBinding := range tc.wantUnscheduled {
				unscheduledBinding := &placementv1beta1.ClusterResourceBinding{}
				if err := fakeClient.Get(ctx, types.NamespacedName{Name: wantUnscheduledBinding.Name}, unscheduledBinding); err != nil {
					t.Errorf("Get() binding %s = %v, want no error", wantUnscheduledBinding.Name, err)
				}

				if diff := cmp.Diff(unscheduledBinding, wantUnscheduledBinding, ignoreObjectMetaResourceVersionField, ignoreObjectAnnotationField, ignoreTypeMetaAPIVersionKindFields); diff != "" {
					t.Errorf("unscheduled binding %s diff (-got, +want): %s", wantUnscheduledBinding.Name, diff)
				}
			}
		})
	}
}

// TestRunScorePluginsFor tests the runScorePluginsFor method.
func TestRunScorePluginsFor(t *testing.T) {
	dummyScorePluginA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 0)
	dummyScorePluginB := fmt.Sprintf(dummyAllPurposePluginNameFormat, 1)

	testCases := []struct {
		name               string
		scorePlugins       []ScorePlugin
		skippedPluginNames []string
		wantStatus         *Status
		wantScoreList      map[string]*ClusterScore
	}{
		{
			name: "single plugin, success",
			scorePlugins: []ScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyScorePluginA,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						return &ClusterScore{
							TopologySpreadScore: 1,
							AffinityScore:       20,
						}, nil
					},
				},
			},
			wantScoreList: map[string]*ClusterScore{
				dummyScorePluginA: {
					TopologySpreadScore: 1,
					AffinityScore:       20,
				},
			},
		},
		{
			name: "multiple plugins, all success",
			scorePlugins: []ScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyScorePluginA,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						return &ClusterScore{
							TopologySpreadScore: 1,
							AffinityScore:       20,
						}, nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyScorePluginB,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						return &ClusterScore{
							TopologySpreadScore: 0,
							AffinityScore:       10,
						}, nil
					},
				},
			},
			wantScoreList: map[string]*ClusterScore{
				dummyScorePluginA: {
					TopologySpreadScore: 1,
					AffinityScore:       20,
				},
				dummyScorePluginB: {
					TopologySpreadScore: 0,
					AffinityScore:       10,
				},
			},
		},
		{
			name: "multiple plugin, one success, one skipped",
			scorePlugins: []ScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyScorePluginA,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						return &ClusterScore{
							TopologySpreadScore: 1,
							AffinityScore:       20,
						}, nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyScorePluginB,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						return &ClusterScore{
							TopologySpreadScore: 0,
							AffinityScore:       10,
						}, nil
					},
				},
			},
			skippedPluginNames: []string{dummyScorePluginB},
			wantScoreList: map[string]*ClusterScore{
				dummyScorePluginA: {
					TopologySpreadScore: 1,
					AffinityScore:       20,
				},
			},
		},
		{
			name: "single plugin, internal error",
			scorePlugins: []ScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyScorePluginA,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						return nil, FromError(fmt.Errorf("internal error"), dummyScorePluginA)
					},
				},
			},
			wantStatus: FromError(fmt.Errorf("internal error"), dummyScorePluginA),
		},
		{
			name: "single plugin, skip",
			scorePlugins: []ScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyScorePluginA,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						return nil, NewNonErrorStatus(Skip, dummyScorePluginA)
					},
				},
			},
			wantStatus: FromError(fmt.Errorf("unexpected status"), dummyScorePluginA),
		},
		{
			name: "single plugin, unschedulable",
			scorePlugins: []ScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyScorePluginA,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						return nil, NewNonErrorStatus(ClusterUnschedulable, dummyScorePluginA)
					},
				},
			},
			wantStatus: FromError(fmt.Errorf("unexpected status"), dummyScorePluginA),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profile := NewProfile(dummyProfileName)
			for _, p := range tc.scorePlugins {
				profile.WithScorePlugin(p)
			}
			f := &framework{
				profile: profile,
			}

			ctx := context.Background()
			state := NewCycleState([]clusterv1beta1.MemberCluster{}, []*placementv1beta1.ClusterResourceBinding{})
			for _, name := range tc.skippedPluginNames {
				state.skippedScorePlugins.Insert(name)
			}
			policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			}
			cluster := &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			}

			scoreList, status := f.runScorePluginsFor(ctx, state, policy, cluster)
			if diff := cmp.Diff(status, tc.wantStatus, cmp.AllowUnexported(Status{}), ignoredStatusFields); diff != "" {
				t.Errorf("runScorePluginsFor() status diff (-got, +want): %s", diff)
			}

			if diff := cmp.Diff(scoreList, tc.wantScoreList); diff != "" {
				t.Errorf("runScorePluginsFor() scoreList diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestRunScorePlugins tests the runScorePlugins method.
func TestRunScorePlugins(t *testing.T) {
	dummyScorePluginNameA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 0)
	dummyScorePluginNameB := fmt.Sprintf(dummyAllPurposePluginNameFormat, 1)

	clusters := []*clusterv1beta1.MemberCluster{
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
		name               string
		scorePlugins       []ScorePlugin
		clusters           []*clusterv1beta1.MemberCluster
		wantScoredClusters ScoredClusters
		expectedToFail     bool
	}{
		{
			name: "three clusters, two score plugins, all scored",
			scorePlugins: []ScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyScorePluginNameA,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						switch cluster.Name {
						case clusterName:
							return &ClusterScore{
								TopologySpreadScore: 1,
							}, nil
						case altClusterName:
							return &ClusterScore{
								TopologySpreadScore: 0,
							}, nil
						case anotherClusterName:
							return &ClusterScore{
								TopologySpreadScore: 2,
							}, nil
						}
						return &ClusterScore{}, nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyScorePluginNameB,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						switch cluster.Name {
						case clusterName:
							return &ClusterScore{
								AffinityScore: 10,
							}, nil
						case altClusterName:
							return &ClusterScore{
								AffinityScore: 20,
							}, nil
						case anotherClusterName:
							return &ClusterScore{
								AffinityScore: 15,
							}, nil
						}
						return &ClusterScore{}, nil
					},
				},
			},
			clusters: clusters,
			wantScoredClusters: ScoredClusters{
				{
					Cluster: clusters[0],
					Score: &ClusterScore{
						TopologySpreadScore: 1,
						AffinityScore:       10,
					},
				},
				{
					Cluster: clusters[1],
					Score: &ClusterScore{
						TopologySpreadScore: 0,
						AffinityScore:       20,
					},
				},
				{
					Cluster: clusters[2],
					Score: &ClusterScore{
						TopologySpreadScore: 2,
						AffinityScore:       15,
					},
				},
			},
		},
		{
			name: "three clusters, two score plugins, one internal error on specific cluster",
			scorePlugins: []ScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyScorePluginNameA,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						switch cluster.Name {
						case clusterName:
							return &ClusterScore{
								TopologySpreadScore: 1,
							}, nil
						case altClusterName:
							return &ClusterScore{
								TopologySpreadScore: 0,
							}, nil
						case anotherClusterName:
							return &ClusterScore{
								TopologySpreadScore: 2,
							}, nil
						}
						return &ClusterScore{}, nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyScorePluginNameB,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						switch cluster.Name {
						case clusterName:
							return &ClusterScore{
								AffinityScore: 10,
							}, nil
						case altClusterName:
							return &ClusterScore{}, FromError(fmt.Errorf("internal error"), dummyScorePluginNameB)
						case anotherClusterName:
							return &ClusterScore{
								AffinityScore: 15,
							}, nil
						}
						return &ClusterScore{}, nil
					},
				},
			},
			clusters:       clusters,
			expectedToFail: true,
		},
		{
			name: "no cluster to score",
			scorePlugins: []ScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyScorePluginNameA,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						switch cluster.Name {
						case clusterName:
							return &ClusterScore{
								TopologySpreadScore: 1,
							}, nil
						case altClusterName:
							return &ClusterScore{
								TopologySpreadScore: 0,
							}, nil
						case anotherClusterName:
							return &ClusterScore{
								TopologySpreadScore: 2,
							}, nil
						}
						return &ClusterScore{}, nil
					},
				},
			},
			clusters:           []*clusterv1beta1.MemberCluster{},
			wantScoredClusters: ScoredClusters{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profile := NewProfile(dummyProfileName)
			for _, p := range tc.scorePlugins {
				profile.WithScorePlugin(p)
			}
			f := &framework{
				profile:      profile,
				parallelizer: parallelizer.NewParallelizer(parallelizer.DefaultNumOfWorkers),
			}

			ctx := context.Background()
			state := NewCycleState([]clusterv1beta1.MemberCluster{}, []*placementv1beta1.ClusterResourceBinding{})
			policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			}

			scoredClusters, err := f.runScorePlugins(ctx, state, policy, tc.clusters)
			if tc.expectedToFail {
				if err == nil {
					t.Errorf("runScorePlugins(), got no error, want error")
				}
				return
			}

			if err != nil {
				t.Fatalf("runScorePlugins() = %v, want no error", err)
			}

			// The method runs in parallel; as a result the order cannot be guaranteed.
			// Sort them by cluster name for easier comparison.
			if diff := cmp.Diff(scoredClusters, tc.wantScoredClusters, cmpopts.SortSlices(lessFuncScoredCluster), cmp.AllowUnexported(ScoredCluster{})); diff != "" {
				t.Errorf("runScorePlugins() scored clusters diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestCalcNumOfClustersToSelect tests the calcNumOfClustersToSelect function.
func TestCalcNumOfClustersToSelect(t *testing.T) {
	testCases := []struct {
		name    string
		desired int
		limit   int
		scored  int
		want    int
	}{
		{
			name:    "no limit, enough bindings to pick",
			desired: 3,
			limit:   3,
			scored:  10,
			want:    3,
		},
		{
			name:    "limit imposed, enough bindings to pick",
			desired: 3,
			limit:   2,
			scored:  10,
			want:    2,
		},
		{
			name:    "limit imposed, not enough bindings to pick",
			desired: 3,
			limit:   2,
			scored:  1,
			want:    1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			toSelect := calcNumOfClustersToSelect(tc.desired, tc.limit, tc.scored)
			if toSelect != tc.want {
				t.Errorf("calcNumOfClustersToSelect(), got %d, want %d", toSelect, tc.want)
			}
		})
	}
}

// TestPickTopNScoredClusters tests the pickTopNScoredClusters function.
func TestPickTopNScoredClusters(t *testing.T) {
	scs := ScoredClusters{
		{
			Cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			},
			Score: &ClusterScore{
				TopologySpreadScore:            1,
				AffinityScore:                  20,
				ObsoletePlacementAffinityScore: 0,
			},
		},
		{
			Cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: altClusterName,
				},
			},
			Score: &ClusterScore{
				TopologySpreadScore:            2,
				AffinityScore:                  10,
				ObsoletePlacementAffinityScore: 1,
			},
		},
	}

	testCases := []struct {
		name           string
		scoredClusters ScoredClusters
		picks          int
		wantPicked     ScoredClusters
		wantNotPicked  ScoredClusters
	}{
		{
			name:           "no scored clusters",
			scoredClusters: ScoredClusters{},
			picks:          1,
			wantPicked:     ScoredClusters{},
			wantNotPicked:  ScoredClusters{},
		},
		{
			name:           "zero to pick",
			scoredClusters: scs,
			picks:          0,
			wantPicked:     ScoredClusters{},
			wantNotPicked:  scs,
		},
		{
			name:           "not enough to pick",
			scoredClusters: scs,
			picks:          10,
			wantPicked: ScoredClusters{
				{
					Cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: altClusterName,
						},
					},
					Score: &ClusterScore{
						TopologySpreadScore:            2,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: clusterName,
						},
					},
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  20,
						ObsoletePlacementAffinityScore: 0,
					},
				},
			},
			wantNotPicked: ScoredClusters{},
		},
		{
			name:           "enough to pick",
			scoredClusters: scs,
			picks:          1,
			wantPicked: ScoredClusters{
				{
					Cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: altClusterName,
						},
					},
					Score: &ClusterScore{
						TopologySpreadScore:            2,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 1,
					},
				},
			},
			wantNotPicked: ScoredClusters{
				{
					Cluster: &clusterv1beta1.MemberCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name: clusterName,
						},
					},
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  20,
						ObsoletePlacementAffinityScore: 0,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			picked, notPicked := pickTopNScoredClusters(tc.scoredClusters, tc.picks)
			if diff := cmp.Diff(picked, tc.wantPicked); diff != "" {
				t.Errorf("pickTopNScoredClusters() picked diff (-got, +want): %s", diff)
			}

			if diff := cmp.Diff(notPicked, tc.wantNotPicked); diff != "" {
				t.Errorf("pickTopNScoredClusters() not picked diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestShouldRequeue tests the shouldRequeue function.
func TestShouldRequeue(t *testing.T) {
	testCases := []struct {
		name             string
		desiredBatchSize int
		batchLimit       int
		bindingCount     int
		want             bool
	}{
		{
			name:             "no batch limit set, enough bindings",
			desiredBatchSize: 3,
			batchLimit:       3,
			bindingCount:     3,
		},
		{
			name:             "no batch limit set, not enough bindings",
			desiredBatchSize: 3,
			batchLimit:       3,
			bindingCount:     2,
		},
		{
			name:             "batch limit set, enough bindings",
			desiredBatchSize: 5,
			batchLimit:       1,
			bindingCount:     1,
			want:             true,
		},
		{
			name:             "batch limit set, not enough bindings",
			desiredBatchSize: 5,
			batchLimit:       1,
			bindingCount:     0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requeue := shouldRequeue(tc.desiredBatchSize, tc.batchLimit, tc.bindingCount)
			if requeue != tc.want {
				t.Errorf("shouldRequeue(), got %t, want %t", requeue, tc.want)
			}
		})
	}
}

// TestCrossReferenceClustersWithTargetNames tests the crossReferenceClustersWithTargetNames method.
func TestCrossReferenceClustersWithTargetNames(t *testing.T) {
	deleteTime := metav1.Now()

	clusterName1 := fmt.Sprintf(clusterNameTemplate, 1)
	clusterName2 := fmt.Sprintf(clusterNameTemplate, 2)
	clusterName3 := fmt.Sprintf(clusterNameTemplate, 3)
	clusterName4 := fmt.Sprintf(clusterNameTemplate, 4)
	clusterName5 := fmt.Sprintf(clusterNameTemplate, 5)
	clusterName6 := fmt.Sprintf(clusterNameTemplate, 6)

	current := []clusterv1beta1.MemberCluster{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName1,
			},
			Status: clusterv1beta1.MemberClusterStatus{
				AgentStatus: []clusterv1beta1.AgentStatus{
					{
						Type: clusterv1beta1.MemberAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(clusterv1beta1.AgentJoined),
								Status: metav1.ConditionTrue,
							},
							{
								Type:               string(clusterv1beta1.AgentHealthy),
								Status:             metav1.ConditionFalse,
								LastTransitionTime: metav1.NewTime(time.Now()),
							},
						},
						LastReceivedHeartbeat: metav1.NewTime(time.Now()),
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName2,
			},
			Status: clusterv1beta1.MemberClusterStatus{
				AgentStatus: []clusterv1beta1.AgentStatus{
					{
						Type: clusterv1beta1.MemberAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(clusterv1beta1.AgentJoined),
								Status: metav1.ConditionTrue,
							},
							{
								Type:               string(clusterv1beta1.AgentHealthy),
								Status:             metav1.ConditionFalse,
								LastTransitionTime: metav1.NewTime(time.Now()),
							},
						},
						LastReceivedHeartbeat: metav1.NewTime(time.Now()),
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName3,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              clusterName4,
				DeletionTimestamp: &deleteTime,
			},
		},
	}

	testCases := []struct {
		name         string
		target       []string
		wantValid    []*clusterv1beta1.MemberCluster
		wantInvalid  []*invalidClusterWithReason
		wantNotFound []string
	}{
		{
			// This case normally should never occur.
			name:         "no target",
			target:       []string{},
			wantValid:    []*clusterv1beta1.MemberCluster{},
			wantInvalid:  []*invalidClusterWithReason{},
			wantNotFound: []string{},
		},
		{
			name: "all valid",
			target: []string{
				clusterName1,
				clusterName2,
			},
			wantValid: []*clusterv1beta1.MemberCluster{
				&current[0],
				&current[1],
			},
			wantInvalid:  []*invalidClusterWithReason{},
			wantNotFound: []string{},
		},
		{
			name: "all invalid",
			target: []string{
				clusterName3,
				clusterName4,
			},
			wantValid: []*clusterv1beta1.MemberCluster{},
			wantInvalid: []*invalidClusterWithReason{
				{
					cluster: &current[2],
					reason:  "cluster is not connected to the fleet: member agent not online yet",
				},
				{
					cluster: &current[3],
					reason:  "cluster has left the fleet",
				},
			},
			wantNotFound: []string{},
		},
		{
			name: "all not found",
			target: []string{
				clusterName5,
				clusterName6,
			},
			wantValid:   []*clusterv1beta1.MemberCluster{},
			wantInvalid: []*invalidClusterWithReason{},
			wantNotFound: []string{
				clusterName5,
				clusterName6,
			},
		},
		{
			name: "mixed",
			target: []string{
				clusterName1,
				clusterName3,
				clusterName5,
			},
			wantValid: []*clusterv1beta1.MemberCluster{
				&current[0],
			},
			wantInvalid: []*invalidClusterWithReason{
				{
					cluster: &current[2],
					reason:  "cluster is not connected to the fleet: member agent not online yet",
				},
			},
			wantNotFound: []string{
				clusterName5,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Construct framework manually instead of using NewFramework() to avoid mocking the controller manager.
			f := &framework{
				clusterEligibilityChecker: clustereligibilitychecker.New(),
			}

			valid, invalid, notFound := f.crossReferenceClustersWithTargetNames(current, tc.target)

			if diff := cmp.Diff(valid, tc.wantValid, cmp.AllowUnexported(invalidClusterWithReason{})); diff != "" {
				t.Errorf("crossReferenceClustersWithTargetNames() valid diff (-got, +want): %s", diff)
			}
			if diff := cmp.Diff(invalid, tc.wantInvalid, cmp.AllowUnexported(invalidClusterWithReason{})); diff != "" {
				t.Errorf("crossReferenceClustersWithTargetNames() invalid diff (-got, +want): %s", diff)
			}
			if diff := cmp.Diff(notFound, tc.wantNotFound, cmp.AllowUnexported(invalidClusterWithReason{})); diff != "" {
				t.Errorf("crossReferenceClustersWithTargetNames() notFound diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestRunAllPluginsForPickNPlacementType tests the runAllPluginsForPickNPlacementType method.
func TestRunAllPluginsForPickNPlacementType(t *testing.T) {
	dummyPostBatchPluginNameA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 0)
	dummyPostBatchPluginNameB := fmt.Sprintf(dummyAllPurposePluginNameFormat, 1)

	dummyPreFilterPluginNameA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 2)

	dummyFilterPluginNameA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 3)
	dummyFilterPluginNameB := fmt.Sprintf(dummyAllPurposePluginNameFormat, 4)

	dummyPreScorePluginNameA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 5)

	dummyScorePluginNameA := fmt.Sprintf(dummyAllPurposePluginNameFormat, 6)
	dummyScorePluginNameB := fmt.Sprintf(dummyAllPurposePluginNameFormat, 7)

	clusters := []clusterv1beta1.MemberCluster{
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

	policy := &placementv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
		},
	}

	testCases := []struct {
		name                          string
		numOfClusters                 int
		numOfBoundOrScheduledBindings int
		postBatchPlugins              []PostBatchPlugin
		preFilterPlugins              []PreFilterPlugin
		filterPlugins                 []FilterPlugin
		preScorePlugins               []PreScorePlugin
		scorePlugins                  []ScorePlugin
		wantState                     *CycleState
		wantScoredClusters            ScoredClusters
		wantFiltered                  []*filteredClusterWithStatus
		expectedToFail                bool
	}{
		{
			name:                          "a postbatch plugin returns error",
			numOfClusters:                 2,
			numOfBoundOrScheduledBindings: 0,
			postBatchPlugins: []PostBatchPlugin{
				&DummyAllPurposePlugin{
					name: dummyPostBatchPluginNameA,
					postBatchRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (size int, status *Status) {
						return 0, FromError(fmt.Errorf("internal error"), dummyFilterPluginNameA)
					},
				},
			},
			expectedToFail: true,
		},
		{
			name:                          "a prefilter plugin returns error",
			numOfClusters:                 2,
			numOfBoundOrScheduledBindings: 0,
			preFilterPlugins: []PreFilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameA,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return FromError(fmt.Errorf("internal error"), dummyPreFilterPluginNameA)
					},
				},
			},
			expectedToFail: true,
		},
		{
			name:                          "a filter plugin returns error",
			numOfClusters:                 2,
			numOfBoundOrScheduledBindings: 0,
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						return FromError(fmt.Errorf("internal error"), dummyFilterPluginNameA)
					},
				},
			},
			expectedToFail: true,
		},
		{
			name:                          "a prescore plugin returns error",
			numOfClusters:                 2,
			numOfBoundOrScheduledBindings: 0,
			preScorePlugins: []PreScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyPreScorePluginNameA,
					preScoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return FromError(fmt.Errorf("internal error"), dummyPreScorePluginNameA)
					},
				},
			},
			expectedToFail: true,
		},
		{
			name:                          "a score plugin returns error",
			numOfClusters:                 2,
			numOfBoundOrScheduledBindings: 0,
			scorePlugins: []ScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyScorePluginNameA,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						return nil, FromError(fmt.Errorf("internal error"), dummyScorePluginNameA)
					},
				},
			},
			expectedToFail: true,
		},
		{
			name:                          "no batch limit set, all clusters scored",
			numOfClusters:                 3,
			numOfBoundOrScheduledBindings: 1,
			postBatchPlugins:              []PostBatchPlugin{},
			preFilterPlugins:              []PreFilterPlugin{},
			filterPlugins:                 []FilterPlugin{},
			preScorePlugins: []PreScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyPreScorePluginNameA,
					preScoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
			},
			scorePlugins: []ScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyScorePluginNameA,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						switch cluster.Name {
						case clusterName:
							return &ClusterScore{
								TopologySpreadScore: 1,
							}, nil
						case altClusterName:
							return &ClusterScore{
								TopologySpreadScore: 0,
							}, nil
						case anotherClusterName:
							return &ClusterScore{
								TopologySpreadScore: -1,
							}, nil
						default:
							return nil, FromError(fmt.Errorf("internal error"), dummyScorePluginNameA)
						}
					},
				},
				&DummyAllPurposePlugin{
					name: dummyScorePluginNameB,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						switch cluster.Name {
						case clusterName:
							return &ClusterScore{
								AffinityScore: 10,
							}, nil
						case altClusterName:
							return &ClusterScore{
								AffinityScore: 0,
							}, nil
						case anotherClusterName:
							return &ClusterScore{
								AffinityScore: 50,
							}, nil
						default:
							return nil, FromError(fmt.Errorf("internal error"), dummyScorePluginNameB)
						}
					},
				},
			},
			wantState: &CycleState{
				desiredBatchSize: 2,
				batchSizeLimit:   2,
			},
			wantScoredClusters: ScoredClusters{
				{
					Cluster: &clusters[0],
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: &clusters[1],
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  0,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: &clusters[2],
					Score: &ClusterScore{
						TopologySpreadScore:            -1,
						AffinityScore:                  50,
						ObsoletePlacementAffinityScore: 0,
					},
				},
			},
			wantFiltered: []*filteredClusterWithStatus{},
		},
		{
			name:                          "no batch limit set, all clusters filtered",
			numOfClusters:                 3,
			numOfBoundOrScheduledBindings: 1,
			postBatchPlugins:              []PostBatchPlugin{},
			preFilterPlugins: []PreFilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameA,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
			},
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						switch cluster.Name {
						case clusterName:
							return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA)
						case anotherClusterName:
							return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA)
						default:
							return nil
						}
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == altClusterName {
							return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameB)
						}
						return nil
					},
				},
			},
			preScorePlugins: []PreScorePlugin{},
			scorePlugins: []ScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyScorePluginNameA,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						return nil, FromError(fmt.Errorf("internal error"), dummyScorePluginNameA)
					},
				},
			},
			wantState: &CycleState{
				desiredBatchSize: 2,
				batchSizeLimit:   2,
			},
			wantScoredClusters: ScoredClusters{},
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
					status:  NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA),
				},
			},
		},
		{
			name:                          "batch limit set, mixed",
			numOfClusters:                 3,
			numOfBoundOrScheduledBindings: 1,
			postBatchPlugins: []PostBatchPlugin{
				&DummyAllPurposePlugin{
					name: dummyPostBatchPluginNameA,
					postBatchRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (size int, status *Status) {
						return 1, nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyPostBatchPluginNameB,
					postBatchRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (size int, status *Status) {
						return 2, nil
					},
				},
			},
			preFilterPlugins: []PreFilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyPreFilterPluginNameA,
					preFilterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
			},
			filterPlugins: []FilterPlugin{
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameA,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						if cluster.Name == clusterName {
							return NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA)
						}
						return nil
					},
				},
				&DummyAllPurposePlugin{
					name: dummyFilterPluginNameB,
					filterRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) {
						return nil
					},
				},
			},
			preScorePlugins: []PreScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyPreScorePluginNameA,
					preScoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) {
						return nil
					},
				},
			},
			scorePlugins: []ScorePlugin{
				&DummyAllPurposePlugin{
					name: dummyScorePluginNameA,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						switch cluster.Name {
						case clusterName:
							return nil, FromError(fmt.Errorf("internal error"), dummyScorePluginNameA)
						case altClusterName:
							return &ClusterScore{
								TopologySpreadScore: 0,
							}, nil
						case anotherClusterName:
							return &ClusterScore{
								TopologySpreadScore: -1,
							}, nil
						default:
							return nil, FromError(fmt.Errorf("internal error"), dummyScorePluginNameA)
						}
					},
				},
				&DummyAllPurposePlugin{
					name: dummyScorePluginNameB,
					scoreRunner: func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) {
						switch cluster.Name {
						case clusterName:
							return nil, FromError(fmt.Errorf("internal error"), dummyScorePluginNameB)
						case altClusterName:
							return &ClusterScore{
								AffinityScore: 0,
							}, nil
						case anotherClusterName:
							return &ClusterScore{
								AffinityScore: 50,
							}, nil
						default:
							return nil, FromError(fmt.Errorf("internal error"), dummyScorePluginNameB)
						}
					},
				},
			},
			wantState: &CycleState{
				desiredBatchSize: 2,
				batchSizeLimit:   1,
			},
			wantScoredClusters: ScoredClusters{
				{
					Cluster: &clusters[1],
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  0,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: &clusters[2],
					Score: &ClusterScore{
						TopologySpreadScore:            -1,
						AffinityScore:                  50,
						ObsoletePlacementAffinityScore: 0,
					},
				},
			},
			wantFiltered: []*filteredClusterWithStatus{
				{
					cluster: &clusters[0],
					status:  NewNonErrorStatus(ClusterUnschedulable, dummyFilterPluginNameA),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profile := NewProfile(dummyProfileName)
			for _, p := range tc.postBatchPlugins {
				profile.WithPostBatchPlugin(p)
			}
			for _, p := range tc.preFilterPlugins {
				profile.WithPreFilterPlugin(p)
			}
			for _, p := range tc.filterPlugins {
				profile.WithFilterPlugin(p)
			}
			for _, p := range tc.preScorePlugins {
				profile.WithPreScorePlugin(p)
			}
			for _, p := range tc.scorePlugins {
				profile.WithScorePlugin(p)
			}

			f := &framework{
				profile:      profile,
				parallelizer: parallelizer.NewParallelizer(parallelizer.DefaultNumOfWorkers),
			}

			ctx := context.Background()
			state := NewCycleState(clusters, nil)
			scored, filtered, err := f.runAllPluginsForPickNPlacementType(ctx, state, policy, tc.numOfClusters, tc.numOfBoundOrScheduledBindings, clusters)
			if tc.expectedToFail {
				if err == nil {
					t.Errorf("runAllPluginsForPickNPlacementType() returned no error, want error")
				}

				return
			}

			if diff := cmp.Diff(state, tc.wantState, cmp.AllowUnexported(CycleState{}), ignoreCycleStateFields); diff != "" {
				t.Errorf("runAllPluginsForPickNPlacementType() state diff (-got, +want): %s", diff)
			}

			// The method runs in parallel; as a result the order cannot be guaranteed.
			// Sort the results by cluster name for comparison.
			if diff := cmp.Diff(scored, tc.wantScoredClusters, cmpopts.SortSlices(lessFuncScoredCluster)); diff != "" {
				t.Errorf("runAllPluginsForPickNPlacementType() scored diff (-got, +want): %s", diff)
			}
			if diff := cmp.Diff(filtered, tc.wantFiltered, cmpopts.SortSlices(lessFuncFilteredCluster), cmp.AllowUnexported(filteredClusterWithStatus{}, Status{})); diff != "" {
				t.Errorf("runAllPluginsForPickNPlacementType() filtered diff (-got, +want): %s", diff)
			}
		})
	}
}
