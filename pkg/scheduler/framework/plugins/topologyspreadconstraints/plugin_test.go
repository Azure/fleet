/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package topologyspreadconstraints

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

var (
	plugin = New()

	ignoreStatusErrorField = cmpopts.IgnoreFields(framework.Status{}, "err")
)

// TestPostBatch tests how this plugin connects to the post batch extension point.
func TestPostBatch(t *testing.T) {
	numOfClusters := int32(10)
	maxSkew := int32(1)

	testCases := []struct {
		name       string
		policy     *fleetv1beta1.ClusterSchedulingPolicySnapshot
		wantLimit  int
		wantStatus *framework.Status
	}{
		{
			name: "no policy specified",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			},
			wantLimit:  0,
			wantStatus: framework.FromError(fmt.Errorf("policy does not exist"), defaultPluginName, "failed to get policy"),
		},
		{
			name: "no topology spread constraints specified",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
			},
			wantLimit:  0,
			wantStatus: framework.NewNonErrorStatus(framework.Skip, defaultPluginName, "no topology spread constraint is present"),
		},
		{
			name: "topology spread constraints specified",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
						TopologySpreadConstraints: []fleetv1beta1.TopologySpreadConstraint{
							{
								MaxSkew:           &maxSkew,
								TopologyKey:       topologyKey1,
								WhenUnsatisfiable: fleetv1beta1.DoNotSchedule,
							},
						},
					},
				},
			},
			wantLimit:  1,
			wantStatus: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			state := framework.NewCycleState([]fleetv1beta1.MemberCluster{}, []*fleetv1beta1.ClusterResourceBinding{})

			limit, status := plugin.PostBatch(ctx, state, tc.policy)
			if limit != tc.wantLimit {
				t.Errorf("PostBatch() limit = %d, want %d", limit, tc.wantLimit)
			}
			// It is safe to compare unexported fields here as the struct is owned by the project.
			if diff := cmp.Diff(status, tc.wantStatus, cmp.AllowUnexported(framework.Status{}), ignoreStatusErrorField); diff != "" {
				t.Errorf("PostBatch() status diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestPreFilter tests how this plugin connects to the pre filter extension point.
func TestPreFilter(t *testing.T) {
	numOfClusters := int32(10)
	maxSkew1 := int32(2)
	maxSkew2 := int32(1)

	testCases := []struct {
		name            string
		policy          *fleetv1beta1.ClusterSchedulingPolicySnapshot
		clusters        []fleetv1beta1.MemberCluster
		bindings        []*fleetv1beta1.ClusterResourceBinding
		wantStatus      *framework.Status
		wantPluginState *pluginState
	}{
		{
			name:     "no policy specified",
			clusters: []fleetv1beta1.MemberCluster{},
			bindings: []*fleetv1beta1.ClusterResourceBinding{},
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.Skip, defaultPluginName, "policy does not exist"),
		},
		{
			name:     "no topology spread constraints specified",
			clusters: []fleetv1beta1.MemberCluster{},
			bindings: []*fleetv1beta1.ClusterResourceBinding{},
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.Skip, defaultPluginName, "no topology spread constraint is present"),
		},
		{
			name: "no doNotSchedule topology spread constraints",
			// Topology key 1:
			// * Domain 1 (topology value 1): 1 binding
			// * Domain 2 (topology value 2): 1 binding
			clusters: []fleetv1beta1.MemberCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName1,
						Labels: map[string]string{
							topologyKey1: topologyValue1,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName2,
						Labels: map[string]string{
							topologyKey1: topologyValue2,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName3,
						Labels: map[string]string{
							topologyKey1: topologyValue2,
						},
					},
				},
			},
			bindings: []*fleetv1beta1.ClusterResourceBinding{
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
			},
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						TopologySpreadConstraints: []fleetv1beta1.TopologySpreadConstraint{
							{
								MaxSkew:           &maxSkew1,
								TopologyKey:       topologyKey1,
								WhenUnsatisfiable: fleetv1beta1.ScheduleAnyway,
							},
						},
					},
				},
			},
			wantPluginState: &pluginState{
				doNotScheduleConstraints: []*fleetv1beta1.TopologySpreadConstraint{},
				scheduleAnywayConstraints: []*fleetv1beta1.TopologySpreadConstraint{
					{
						MaxSkew:           &maxSkew1,
						TopologyKey:       topologyKey1,
						WhenUnsatisfiable: fleetv1beta1.ScheduleAnyway,
					},
				},
				violations: doNotScheduleViolations{},
				scores: topologySpreadScores{
					clusterName1: 1 * skewChangeScoreFactor,
					clusterName2: 1 * skewChangeScoreFactor,
					clusterName3: 1 * skewChangeScoreFactor,
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.Skip, defaultPluginName, "no DoNotSchedule topology spread constraint is present"),
		},
		{
			name: "with doNotSchedule topology spread constraints",
			// Topology key 1:
			// * Domain 1 (topology value 1): 1 binding
			// * Domain 2 (topology value 2): 0 binding
			clusters: []fleetv1beta1.MemberCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName1,
						Labels: map[string]string{
							topologyKey1: topologyValue1,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName2,
						Labels: map[string]string{
							topologyKey1: topologyValue2,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName3,
						Labels: map[string]string{
							topologyKey1: topologyValue2,
						},
					},
				},
			},
			bindings: []*fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName1,
					},
				},
			},
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						TopologySpreadConstraints: []fleetv1beta1.TopologySpreadConstraint{
							{
								MaxSkew:           &maxSkew2,
								TopologyKey:       topologyKey1,
								WhenUnsatisfiable: fleetv1beta1.DoNotSchedule,
							},
						},
					},
				},
			},
			wantPluginState: &pluginState{
				doNotScheduleConstraints: []*fleetv1beta1.TopologySpreadConstraint{
					{
						MaxSkew:           &maxSkew2,
						TopologyKey:       topologyKey1,
						WhenUnsatisfiable: fleetv1beta1.DoNotSchedule,
					},
				},
				scheduleAnywayConstraints: []*fleetv1beta1.TopologySpreadConstraint{},
				violations: doNotScheduleViolations{
					clusterName1: violationReasons{
						fmt.Sprintf(doNotScheduleConstraintViolationReasonTemplate, topologyKey1, maxSkew2),
					},
				},
				scores: topologySpreadScores{
					clusterName2: -1 * skewChangeScoreFactor,
					clusterName3: -1 * skewChangeScoreFactor,
				},
			},
			wantStatus: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			state := framework.NewCycleState(tc.clusters, tc.bindings)
			status := plugin.PreFilter(ctx, state, tc.policy)
			// It is safe to compare unexported fields here as the struct is owned by the project.
			if diff := cmp.Diff(status, tc.wantStatus, cmp.AllowUnexported(framework.Status{}), ignoreStatusErrorField); diff != "" {
				t.Fatalf("PreFilter() status diff (-got, +want): %s", diff)
			}

			if tc.wantPluginState == nil {
				// Skip the case if there is no expected plugin state.
				return
			}

			ps, err := plugin.readPluginState(state)
			if err != nil {
				t.Fatalf("Get plugin state = %v, want no error", err)
			}
			if diff := cmp.Diff(ps, tc.wantPluginState, cmp.AllowUnexported(pluginState{})); diff != "" {
				t.Fatalf("PreFilter() plugin state diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestFilter tests how this plugin connects to the filter extension point.
func TestFilter(t *testing.T) {
	policy := fleetv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
		},
	}

	maxSkew := 1

	testCases := []struct {
		name    string
		ps      *pluginState
		cluster *fleetv1beta1.MemberCluster
		want    *framework.Status
	}{
		{
			name: "no violations",
			ps: &pluginState{
				violations: doNotScheduleViolations{},
			},
			cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
			},
			want: nil,
		},
		{
			name: "with violations, match found",
			ps: &pluginState{
				violations: doNotScheduleViolations{
					clusterName1: violationReasons{
						fmt.Sprintf(doNotScheduleConstraintViolationReasonTemplate, topologyKey1, maxSkew),
					},
					clusterName2: violationReasons{
						fmt.Sprintf(doNotScheduleConstraintViolationReasonTemplate, topologyKey2, maxSkew),
					},
				},
			},
			cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
			},
			want: framework.NewNonErrorStatus(framework.ClusterUnschedulable, defaultPluginName, fmt.Sprintf(doNotScheduleConstraintViolationReasonTemplate, topologyKey1, maxSkew)),
		},
		{
			name: "with violations, match not found",
			ps: &pluginState{
				violations: doNotScheduleViolations{
					clusterName1: violationReasons{
						fmt.Sprintf(doNotScheduleConstraintViolationReasonTemplate, topologyKey1, maxSkew),
					},
					clusterName2: violationReasons{
						fmt.Sprintf(doNotScheduleConstraintViolationReasonTemplate, topologyKey2, maxSkew),
					},
				},
			},
			cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName3,
				},
			},
			want: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			state := framework.NewCycleState(nil, nil)
			state.Write(defaultPluginName, tc.ps)

			status := plugin.Filter(ctx, state, &policy, tc.cluster)
			// It is safe to compare unexported fields here as the struct is owned by the project.
			if diff := cmp.Diff(status, tc.want, cmp.AllowUnexported(framework.Status{}), ignoreStatusErrorField); diff != "" {
				t.Fatalf("Filter() status diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestPreScore tests how this plugin connects to the pre score extension point.
func TestPreScore(t *testing.T) {
	numOfClusters := int32(10)

	maxSkew := int32(1)

	testCases := []struct {
		name   string
		policy *fleetv1beta1.ClusterSchedulingPolicySnapshot
		ps     *pluginState
		want   *framework.Status
	}{
		{
			name: "no policy",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			},
			want: framework.FromError(fmt.Errorf("policy does not exist"), defaultPluginName, "failed to get policy"),
		},
		{
			name: "no topology spread constraints",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
			},
			want: framework.NewNonErrorStatus(framework.Skip, defaultPluginName, "no topology spread constraint is present"),
		},
		{
			name: "no plugin state",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
						TopologySpreadConstraints: []fleetv1beta1.TopologySpreadConstraint{
							{
								MaxSkew:           &maxSkew,
								TopologyKey:       topologyKey1,
								WhenUnsatisfiable: fleetv1beta1.DoNotSchedule,
							},
						},
					},
				},
			},
			want: framework.FromError(fmt.Errorf("internal error"), defaultPluginName, "failed to read plugin state"),
		},
		{
			name: "with topology spread scores",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
						TopologySpreadConstraints: []fleetv1beta1.TopologySpreadConstraint{
							{
								MaxSkew:           &maxSkew,
								TopologyKey:       topologyKey1,
								WhenUnsatisfiable: fleetv1beta1.DoNotSchedule,
							},
						},
					},
				},
			},
			ps: &pluginState{
				doNotScheduleConstraints: []*fleetv1beta1.TopologySpreadConstraint{
					{
						MaxSkew:           &maxSkew,
						TopologyKey:       topologyKey1,
						WhenUnsatisfiable: fleetv1beta1.DoNotSchedule,
					},
				},
				scheduleAnywayConstraints: []*fleetv1beta1.TopologySpreadConstraint{},
				violations:                doNotScheduleViolations{},
				scores: topologySpreadScores{
					clusterName1: 1,
				},
			},
			want: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			state := framework.NewCycleState(nil, nil)
			if tc.ps != nil {
				state.Write(defaultPluginName, tc.ps)
			}

			status := plugin.PreScore(ctx, state, tc.policy)
			// It is safe to compare unexported fields here as the struct is owned by the project.
			if diff := cmp.Diff(status, tc.want, cmp.AllowUnexported(framework.Status{}), ignoreStatusErrorField); diff != "" {
				t.Errorf("PreScore() status diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestScore tests how this plugin connects to the score extension point.
func TestScore(t *testing.T) {
	clusterScore := 1

	policy := &fleetv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: policyName,
		},
	}

	cluster := &fleetv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName1,
		},
	}

	testCases := []struct {
		name       string
		ps         *pluginState
		wantScore  *framework.ClusterScore
		wantStatus *framework.Status
	}{
		{
			name:       "no plugin state",
			wantStatus: framework.FromError(fmt.Errorf("internal error"), defaultPluginName, "failed to read plugin state"),
		},
		{
			name: "with scores, match found",
			ps: &pluginState{
				scores: topologySpreadScores{
					clusterName1: clusterScore,
				},
			},
			wantScore: &framework.ClusterScore{
				TopologySpreadScore: clusterScore,
			},
		},
		{
			name: "with scores, match not found",
			ps: &pluginState{
				scores: topologySpreadScores{
					clusterName2: clusterScore,
				},
			},
			wantStatus: framework.FromError(fmt.Errorf("internal error"), defaultPluginName),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			state := framework.NewCycleState(nil, nil)
			if tc.ps != nil {
				state.Write(defaultPluginName, tc.ps)
			}

			score, status := plugin.Score(ctx, state, policy, cluster)
			// Note that cmp package will attempt to call the custom Equal() method for cluster scores.
			if diff := cmp.Diff(score, tc.wantScore); diff != "" {
				t.Errorf("Score() score diff (-got, +want): %s", diff)
			}
			// It is safe to compare unexported fields here as the struct is owned by the project.
			if diff := cmp.Diff(status, tc.wantStatus, cmp.AllowUnexported(framework.Status{}), ignoreStatusErrorField); diff != "" {
				t.Errorf("Score() status diff (-got, +want): %s", diff)
			}
		})
	}
}
