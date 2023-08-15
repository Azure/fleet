/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package sameplacementaffinity

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

func TestScore(t *testing.T) {
	tests := []struct {
		name             string
		obsoleteBindings []*placementv1beta1.ClusterResourceBinding
		want             *framework.ClusterScore
	}{
		{
			name: "has an obsolete binding",
			obsoleteBindings: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: clusterName,
						State:         placementv1beta1.BindingStateBound,
					},
				},
			},
			want: &framework.ClusterScore{ObsoletePlacementAffinityScore: 1},
		},
		{
			name: "has an obsolete binding on other cluster",
			obsoleteBindings: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "another-cluster",
						State:         placementv1beta1.BindingStateBound,
					},
				},
			},
			want: &framework.ClusterScore{ObsoletePlacementAffinityScore: 0},
		},
		{
			name: "nil obsolete binding",
			want: &framework.ClusterScore{ObsoletePlacementAffinityScore: 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := New()
			state := framework.NewCycleState(nil, tc.obsoleteBindings)
			cluster := clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			}
			got, gotStatus := p.Score(context.Background(), state, nil, &cluster)
			if gotStatus != nil {
				t.Fatalf("Score() = status %v, want nil", gotStatus)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Score() clusterScore mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
