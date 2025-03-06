/*
Copyright 2025 The KubeFleet Authors.

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
