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
)

const (
	topologyKey1   = "topology-key-1"
	topologyKey2   = "topology-key-2"
	topologyKey3   = "topology-key-3"
	topologyValue1 = "topology-value-1"
	topologyValue2 = "topology-value-2"
	topologyValue3 = "topology-value-3"

	policyName = "policy-1"
)

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
