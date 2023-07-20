/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package topologyspreadconstraints

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

const (
	clusterName1 = "bravelion"
	clusterName2 = "smartfish"
	clusterName3 = "jumpingcat"
	clusterName4 = "singingbutterfly"
	clusterName5 = "sleepingwolf"

	topologyKey1   = "topology-key-1"
	topologyKey2   = "topology-key-2"
	topologyKey3   = "topology-key-3"
	topologyValue1 = "topology-value-1"
	topologyValue2 = "topology-value-2"
	topologyValue3 = "topology-value-3"

	bindingName1 = "binding-1"
	bindingName2 = "binding-2"
	bindingName3 = "binding-3"

	policyName = "policy-1"
)

// TestCountByDomain tests the countByDomain function.
func TestCountByDomain(t *testing.T) {
	clusterName6 := "dancingelephant"

	bindingName4 := "binding-4"
	bindingName5 := "binding-5"
	bindingName6 := "binding-6"

	testCases := []struct {
		name                       string
		clusters                   []fleetv1beta1.MemberCluster
		bindings                   []*fleetv1beta1.ClusterResourceBinding
		wantBindingCounterByDomain *bindingCounterByDomain
	}{
		{
			name:     "uninitialized (no clusters)",
			clusters: []fleetv1beta1.MemberCluster{},
			bindings: []*fleetv1beta1.ClusterResourceBinding{},
			wantBindingCounterByDomain: &bindingCounterByDomain{
				counter:        make(map[domainName]count),
				smallest:       -1,
				secondSmallest: -1,
				largest:        -1,
			},
		},
		{
			name: "uninitialized (no topology key present in clusters)",
			clusters: []fleetv1beta1.MemberCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName1,
					},
				},
			},
			bindings: []*fleetv1beta1.ClusterResourceBinding{},
			wantBindingCounterByDomain: &bindingCounterByDomain{
				counter:        make(map[domainName]count),
				smallest:       -1,
				secondSmallest: -1,
				largest:        -1,
			},
		},
		{
			name: "single domain",
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
							topologyKey1: topologyValue1,
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
			wantBindingCounterByDomain: &bindingCounterByDomain{
				counter: map[domainName]count{
					topologyValue1: 1,
				},
				smallest:       1,
				secondSmallest: 1,
				largest:        1,
			},
		},
		{
			name: "multiple domains, same count",
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
							topologyKey1: topologyValue3,
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName3,
					},
				},
			},
			wantBindingCounterByDomain: &bindingCounterByDomain{
				counter: map[domainName]count{
					topologyValue1: 1,
					topologyValue2: 1,
					topologyValue3: 1,
				},
				smallest:       1,
				secondSmallest: 1,
				largest:        1,
			},
		},
		{
			name: "multiple domains, different counts, separate special counts",
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
							topologyKey1: topologyValue1,
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName4,
						Labels: map[string]string{
							topologyKey1: topologyValue2,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName5,
						Labels: map[string]string{
							topologyKey1: topologyValue2,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName6,
						Labels: map[string]string{
							topologyKey1: topologyValue3,
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName3,
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName5,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName5,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName6,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName6,
					},
				},
			},
			wantBindingCounterByDomain: &bindingCounterByDomain{
				counter: map[domainName]count{
					topologyValue1: 2,
					topologyValue2: 3,
					topologyValue3: 1,
				},
				smallest:       1,
				secondSmallest: 2,
				largest:        3,
			},
		},
		{
			name: "multiple domains, different counts, multiple smallests",
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
							topologyKey1: topologyValue3,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName4,
						Labels: map[string]string{
							topologyKey1: topologyValue3,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName5,
						Labels: map[string]string{
							topologyKey1: topologyValue3,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName6,
						Labels: map[string]string{
							topologyKey1: topologyValue3,
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName3,
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName5,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName5,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName6,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName6,
					},
				},
			},
			wantBindingCounterByDomain: &bindingCounterByDomain{
				counter: map[domainName]count{
					topologyValue1: 1,
					topologyValue2: 1,
					topologyValue3: 4,
				},
				smallest:       1,
				secondSmallest: 1,
				largest:        4,
			},
		},
		{
			name: "multiple domains, different counts, multiple largests",
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName4,
						Labels: map[string]string{
							topologyKey1: topologyValue3,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName5,
						Labels: map[string]string{
							topologyKey1: topologyValue3,
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName3,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName3,
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingName5,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName5,
					},
				},
			},
			wantBindingCounterByDomain: &bindingCounterByDomain{
				counter: map[domainName]count{
					topologyValue1: 1,
					topologyValue2: 2,
					topologyValue3: 2,
				},
				smallest:       1,
				secondSmallest: 2,
				largest:        2,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			state := framework.NewCycleState(tc.clusters, tc.bindings)
			counter := countByDomain(tc.clusters, state, topologyKey1)
			if diff := cmp.Diff(counter, tc.wantBindingCounterByDomain, cmp.AllowUnexported(bindingCounterByDomain{})); diff != "" {
				t.Errorf("countByDomain() diff (-got, +want): %s", diff)
			}
		})
	}
}

// TestClassifyConstraints tests the classifyConstraints function.
func TestClassifyConstriants(t *testing.T) {
	maxSkew := int32(2)

	testCases := []struct {
		name               string
		policy             *fleetv1beta1.ClusterSchedulingPolicySnapshot
		wantDoNotSchedule  []*fleetv1beta1.TopologySpreadConstraint
		wantScheduleAnyway []*fleetv1beta1.TopologySpreadConstraint
	}{
		{
			name: "all doNotSchedule topology spread constraints",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						TopologySpreadConstraints: []fleetv1beta1.TopologySpreadConstraint{
							{
								MaxSkew:           &maxSkew,
								TopologyKey:       topologyKey1,
								WhenUnsatisfiable: fleetv1beta1.DoNotSchedule,
							},
							// Use the default value.
							{
								MaxSkew:     &maxSkew,
								TopologyKey: topologyKey2,
							},
						},
					},
				},
			},
			wantDoNotSchedule: []*fleetv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:           &maxSkew,
					TopologyKey:       topologyKey1,
					WhenUnsatisfiable: fleetv1beta1.DoNotSchedule,
				},
				{
					MaxSkew:     &maxSkew,
					TopologyKey: topologyKey2,
				},
			},
			wantScheduleAnyway: []*fleetv1beta1.TopologySpreadConstraint{},
		},
		{
			name: "all scheduleAnyway topology spread constraints",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						TopologySpreadConstraints: []fleetv1beta1.TopologySpreadConstraint{
							{
								MaxSkew:           &maxSkew,
								TopologyKey:       topologyKey1,
								WhenUnsatisfiable: fleetv1beta1.ScheduleAnyway,
							},
							{
								MaxSkew:           &maxSkew,
								TopologyKey:       topologyKey2,
								WhenUnsatisfiable: fleetv1beta1.ScheduleAnyway,
							},
						},
					},
				},
			},
			wantDoNotSchedule: []*fleetv1beta1.TopologySpreadConstraint{},
			wantScheduleAnyway: []*fleetv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:           &maxSkew,
					TopologyKey:       topologyKey1,
					WhenUnsatisfiable: fleetv1beta1.ScheduleAnyway,
				},
				{
					MaxSkew:           &maxSkew,
					TopologyKey:       topologyKey2,
					WhenUnsatisfiable: fleetv1beta1.ScheduleAnyway,
				},
			},
		},
		{
			name: "mixed",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						TopologySpreadConstraints: []fleetv1beta1.TopologySpreadConstraint{
							{
								MaxSkew:           &maxSkew,
								TopologyKey:       topologyKey1,
								WhenUnsatisfiable: fleetv1beta1.ScheduleAnyway,
							},
							{
								MaxSkew:           &maxSkew,
								TopologyKey:       topologyKey2,
								WhenUnsatisfiable: fleetv1beta1.ScheduleAnyway,
							},
							// Use the default value.
							{
								MaxSkew:     &maxSkew,
								TopologyKey: topologyKey3,
							},
						},
					},
				},
			},
			wantDoNotSchedule: []*fleetv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:     &maxSkew,
					TopologyKey: topologyKey3,
				},
			},
			wantScheduleAnyway: []*fleetv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:           &maxSkew,
					TopologyKey:       topologyKey1,
					WhenUnsatisfiable: fleetv1beta1.ScheduleAnyway,
				},
				{
					MaxSkew:           &maxSkew,
					TopologyKey:       topologyKey2,
					WhenUnsatisfiable: fleetv1beta1.ScheduleAnyway,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			doNotSchedule, scheduleAnyway := classifyConstraints(tc.policy)
			if diff := cmp.Diff(doNotSchedule, tc.wantDoNotSchedule); diff != "" {
				t.Errorf("classifyConstraints() doNotSchedule topology spread constraints diff (-got, +want): %s", diff)
			}
			if diff := cmp.Diff(scheduleAnyway, tc.wantScheduleAnyway); diff != "" {
				t.Errorf("classifyConstraints() scheduleAnyway topology spread constraints diff (-got, +want): %s", diff)
			}
		})
	}
}
