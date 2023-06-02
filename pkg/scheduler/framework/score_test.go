/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"

	fleetv1 "go.goms.io/fleet/apis/v1"
)

func TestClusterScoreAdd(t *testing.T) {
	s1 := &ClusterScore{
		TopologySpreadScore: 0,
		AffinityScore:       0,
		PriorityScore:       0,
	}

	s2 := &ClusterScore{
		TopologySpreadScore: 1,
		AffinityScore:       5,
		PriorityScore:       80,
	}

	s1.Add(*s2)
	want := &ClusterScore{
		TopologySpreadScore: 1,
		AffinityScore:       5,
		PriorityScore:       80,
	}
	if !cmp.Equal(s1, want) {
		t.Fatalf("Add() = %v, want %v", s1, want)
	}
}

func TestClusterScoreLess(t *testing.T) {
	testCases := []struct {
		name string
		s1   *ClusterScore
		s2   *ClusterScore
		want bool
	}{
		{
			name: "s1 is less than s2 in topology spread score",
			s1: &ClusterScore{
				TopologySpreadScore: 0,
				AffinityScore:       10,
				PriorityScore:       20,
			},
			s2: &ClusterScore{
				TopologySpreadScore: 1,
				AffinityScore:       20,
				PriorityScore:       30,
			},
			want: true,
		},
		{
			name: "s1 is less than s2 in affinity score",
			s1: &ClusterScore{
				TopologySpreadScore: 1,
				AffinityScore:       10,
				PriorityScore:       20,
			},
			s2: &ClusterScore{
				TopologySpreadScore: 1,
				AffinityScore:       20,
				PriorityScore:       30,
			},
			want: true,
		},
		{
			name: "s1 is less than s2 in priority score",
			s1: &ClusterScore{
				TopologySpreadScore: 1,
				AffinityScore:       10,
				PriorityScore:       20,
			},
			s2: &ClusterScore{
				TopologySpreadScore: 1,
				AffinityScore:       10,
				PriorityScore:       30,
			},
			want: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.s1.Less(tc.s2) != tc.want {
				t.Fatalf("Less(%v, %v) = %t, want %t", tc.s1, tc.s2, !tc.want, tc.want)
			}

			if tc.s2.Less(tc.s1) != !tc.want {
				t.Fatalf("Less(%v, %v) = %t, want %t", tc.s2, tc.s1, tc.want, !tc.want)
			}
		})
	}
}

func TestClusterScoreEqual(t *testing.T) {
	s1 := &ClusterScore{
		TopologySpreadScore: 0,
		AffinityScore:       0,
		PriorityScore:       0,
	}

	s2 := &ClusterScore{
		TopologySpreadScore: 0,
		AffinityScore:       0,
		PriorityScore:       0,
	}

	if s1.Less(s2) || s2.Less(s1) {
		t.Fatalf("Less(%v, %v) = %v, Less(%v, %v) = %v, want both to be false", s1, s2, s1.Less(s2), s2, s1, s2.Less(s1))
	}
}

func TestScoredClustersSort(t *testing.T) {
	clusterA := &fleetv1.MemberCluster{}
	clusterB := &fleetv1.MemberCluster{}
	clusterC := &fleetv1.MemberCluster{}
	clusterD := &fleetv1.MemberCluster{}

	testCases := []struct {
		name string
		scs  ScoredClusters
		want ScoredClusters
	}{
		{
			name: "sort asc values",
			scs: ScoredClusters{
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore: 0,
						AffinityScore:       10,
						PriorityScore:       20,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore: 1,
						AffinityScore:       10,
						PriorityScore:       20,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore: 1,
						AffinityScore:       20,
						PriorityScore:       20,
					},
				},
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore: 2,
						AffinityScore:       30,
						PriorityScore:       40,
					},
				},
			},
			want: ScoredClusters{
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore: 2,
						AffinityScore:       30,
						PriorityScore:       40,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore: 1,
						AffinityScore:       20,
						PriorityScore:       20,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore: 1,
						AffinityScore:       10,
						PriorityScore:       20,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore: 0,
						AffinityScore:       10,
						PriorityScore:       20,
					},
				},
			},
		},
		{
			name: "sort desc values",
			scs: ScoredClusters{
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore: 2,
						AffinityScore:       30,
						PriorityScore:       40,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore: 1,
						AffinityScore:       20,
						PriorityScore:       20,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore: 1,
						AffinityScore:       10,
						PriorityScore:       20,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore: 0,
						AffinityScore:       10,
						PriorityScore:       20,
					},
				},
			},
			want: ScoredClusters{
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore: 2,
						AffinityScore:       30,
						PriorityScore:       40,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore: 1,
						AffinityScore:       20,
						PriorityScore:       20,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore: 1,
						AffinityScore:       10,
						PriorityScore:       20,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore: 0,
						AffinityScore:       10,
						PriorityScore:       20,
					},
				},
			},
		},
		{
			name: "sort values in random",
			scs: ScoredClusters{
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore: 1,
						AffinityScore:       20,
						PriorityScore:       20,
					},
				},
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore: 2,
						AffinityScore:       30,
						PriorityScore:       40,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore: 0,
						AffinityScore:       10,
						PriorityScore:       20,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore: 1,
						AffinityScore:       10,
						PriorityScore:       20,
					},
				},
			},
			want: ScoredClusters{
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore: 2,
						AffinityScore:       30,
						PriorityScore:       40,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore: 1,
						AffinityScore:       20,
						PriorityScore:       20,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore: 1,
						AffinityScore:       10,
						PriorityScore:       20,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore: 0,
						AffinityScore:       10,
						PriorityScore:       20,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sort.Sort(sort.Reverse(tc.scs))
			if !cmp.Equal(tc.scs, tc.want) {
				t.Fatalf("Sort() = %v, want %v", tc.scs, tc.want)
			}
		})
	}
}
