/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusteraffinity

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

func TestPreScore(t *testing.T) {
	tests := []struct {
		name              string
		policy            *fleetv1beta1.PlacementPolicy
		ps                *pluginState
		notSetPluginState bool
		want              *framework.Status
	}{
		{
			name: "nil policy",
			want: framework.NewNonErrorStatus(framework.Skip, defaultPluginName),
		},
		{
			name:   "nil affinity",
			policy: &fleetv1beta1.PlacementPolicy{},
			want:   framework.NewNonErrorStatus(framework.Skip, defaultPluginName),
		},
		{
			name: "nil cluster affinity",
			policy: &fleetv1beta1.PlacementPolicy{
				Affinity: &fleetv1beta1.Affinity{},
			},
			want: framework.NewNonErrorStatus(framework.Skip, defaultPluginName),
		},
		{
			name: "pluginState is not set",
			policy: &fleetv1beta1.PlacementPolicy{
				Affinity: &fleetv1beta1.Affinity{
					ClusterAffinity: &fleetv1beta1.ClusterAffinity{},
				},
			},
			notSetPluginState: true,
			want:              framework.FromError(errors.New("invalid state"), defaultPluginName),
		},
		{
			name: "nil pluginState",
			policy: &fleetv1beta1.PlacementPolicy{
				Affinity: &fleetv1beta1.Affinity{
					ClusterAffinity: &fleetv1beta1.ClusterAffinity{},
				},
			},
			want: framework.FromError(errors.New("invalid state"), defaultPluginName),
		},
		{
			name: "no preferred affinity terms",
			policy: &fleetv1beta1.PlacementPolicy{
				Affinity: &fleetv1beta1.Affinity{
					ClusterAffinity: &fleetv1beta1.ClusterAffinity{},
				},
			},
			ps:   &pluginState{},
			want: framework.NewNonErrorStatus(framework.Skip, defaultPluginName),
		},
		{
			name: "have preferred affinity terms",
			policy: &fleetv1beta1.PlacementPolicy{
				Affinity: &fleetv1beta1.Affinity{
					ClusterAffinity: &fleetv1beta1.ClusterAffinity{},
				},
			},
			ps: &pluginState{
				preferredAffinityTerms: []preferredAffinityTerm{
					{
						weight: 5,
						affinityTerm: affinityTerm{
							selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
						},
					},
				},
			},
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := New()
			state := framework.NewCycleState(nil)
			if !tc.notSetPluginState {
				state.Write(framework.StateKey(p.Name()), tc.ps)
			}
			snapshot := &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: tc.policy,
				},
			}
			got := p.PreScore(context.Background(), state, snapshot)
			if diff := cmp.Diff(tc.want, got, cmpStatusOptions); diff != "" {
				t.Errorf("PreScore() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestPluginScore(t *testing.T) {
	tests := []struct {
		name              string
		ps                *pluginState
		notSetPluginState bool
		cluster           *fleetv1beta1.MemberCluster
		wantScore         *framework.ClusterScore
		wantStatus        *framework.Status
	}{
		{
			name:              "pluginState is not set",
			notSetPluginState: true,
			wantStatus:        framework.FromError(errors.New("invalid state"), defaultPluginName),
		},
		{
			name:       "nil pluginState",
			wantStatus: framework.FromError(errors.New("invalid state"), defaultPluginName),
		},
		{
			name: "no preferred affinity terms",
			ps: &pluginState{
				preferredAffinityTerms: []preferredAffinityTerm{},
			},
			wantScore: &framework.ClusterScore{AffinityScore: 0},
		},
		{
			name: "have preferred affinity terms",
			ps: &pluginState{
				preferredAffinityTerms: []preferredAffinityTerm{
					{
						affinityTerm: affinityTerm{
							selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
						},
						weight: 5,
					},
					{
						affinityTerm: affinityTerm{
							selector: labels.SelectorFromSet(map[string]string{}),
						},
						weight: -8,
					},
				},
			},
			cluster: &fleetv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"region": "us-west",
						"zone":   "zone2",
					},
				},
			},
			wantScore: &framework.ClusterScore{AffinityScore: -3},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := New()
			state := framework.NewCycleState(nil)
			if !tc.notSetPluginState {
				state.Write(framework.StateKey(p.Name()), tc.ps)
			}
			got, gotStatus := p.Score(context.Background(), state, nil, tc.cluster)
			if diff := cmp.Diff(tc.wantStatus, gotStatus, cmpStatusOptions); diff != "" {
				t.Fatalf("Score() status mismatch (-want, +got):\n%s", diff)
			}

			if gotStatus == nil && !cmp.Equal(got, tc.wantScore) {
				t.Errorf("Score()=%v, want score %v", got, tc.wantScore)
			}
		})
	}
}
