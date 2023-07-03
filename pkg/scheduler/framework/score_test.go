/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// TestClusterScoreToAdd tests the Add() method of ClusterScore.
func TestClusterScoreAdd(t *testing.T) {
	s1 := &ClusterScore{
		TopologySpreadScore:          0,
		AffinityScore:                0,
		ActiveOrCreatingBindingScore: 0,
	}

	s2 := &ClusterScore{
		TopologySpreadScore:          1,
		AffinityScore:                5,
		ActiveOrCreatingBindingScore: 1,
	}

	s1.Add(s2)
	want := &ClusterScore{
		TopologySpreadScore:          1,
		AffinityScore:                5,
		ActiveOrCreatingBindingScore: 1,
	}
	if diff := cmp.Diff(s1, want); diff != "" {
		t.Fatalf("Add() diff (-got, +want): %s", diff)
	}
}

// TestClusterScoreEqual tests the Equal() method of ClusterScore.
func TestClusterScoreEqual(t *testing.T) {
	testCases := []struct {
		name string
		s1   *ClusterScore
		s2   *ClusterScore
		want bool
	}{
		{
			name: "s1 is equal to s2",
			s1: &ClusterScore{
				TopologySpreadScore:          1,
				AffinityScore:                5,
				ActiveOrCreatingBindingScore: 1,
			},
			s2: &ClusterScore{
				TopologySpreadScore:          1,
				AffinityScore:                5,
				ActiveOrCreatingBindingScore: 1,
			},
			want: true,
		},
		{
			name: "s1 is not equal to s2",
			s1: &ClusterScore{
				TopologySpreadScore:          2,
				AffinityScore:                5,
				ActiveOrCreatingBindingScore: 1,
			},
			s2: &ClusterScore{
				TopologySpreadScore:          1,
				AffinityScore:                7,
				ActiveOrCreatingBindingScore: 0,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s1.Equal(tc.s2); got != tc.want {
				t.Fatalf("Equal() = %v, want %v", got, tc.want)
			}
		})
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
			},
			s2: &ClusterScore{
				TopologySpreadScore: 1,
				AffinityScore:       20,
			},
			want: true,
		},
		{
			name: "s1 is less than s2 in affinity score",
			s1: &ClusterScore{
				TopologySpreadScore: 1,
				AffinityScore:       10,
			},
			s2: &ClusterScore{
				TopologySpreadScore: 1,
				AffinityScore:       20,
			},
			want: true,
		},
		{
			name: "s1 is less than s2 in active or creating binding score",
			s1: &ClusterScore{
				TopologySpreadScore:          1,
				AffinityScore:                10,
				ActiveOrCreatingBindingScore: 0,
			},
			s2: &ClusterScore{
				TopologySpreadScore:          1,
				AffinityScore:                10,
				ActiveOrCreatingBindingScore: 1,
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

func TestClusterScoreEqualWhenLess(t *testing.T) {
	s1 := &ClusterScore{
		TopologySpreadScore:          0,
		AffinityScore:                0,
		ActiveOrCreatingBindingScore: 0,
	}

	s2 := &ClusterScore{
		TopologySpreadScore:          0,
		AffinityScore:                0,
		ActiveOrCreatingBindingScore: 0,
	}

	if s1.Less(s2) || s2.Less(s1) {
		t.Fatalf("Less(%v, %v) = %v, Less(%v, %v) = %v, want both to be false", s1, s2, s1.Less(s2), s2, s1, s2.Less(s1))
	}
}

func TestScoredClustersSort(t *testing.T) {
	clusterA := &fleetv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-1",
		},
	}
	clusterB := &fleetv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-2",
		},
	}
	clusterC := &fleetv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-3",
		},
	}
	clusterD := &fleetv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-4",
		},
	}
	clusterE := &fleetv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-5",
		},
	}

	testCases := []struct {
		name string
		scs  ScoredClusters
		want ScoredClusters
	}{
		{
			name: "sort asc values",
			scs: ScoredClusters{
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 0,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 1,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:          1,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 1,
					},
				},
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:          1,
						AffinityScore:                20,
						ActiveOrCreatingBindingScore: 0,
					},
				},
				{
					Cluster: clusterE,
					Score: &ClusterScore{
						TopologySpreadScore:          2,
						AffinityScore:                30,
						ActiveOrCreatingBindingScore: 0,
					},
				},
			},
			want: ScoredClusters{
				{
					Cluster: clusterE,
					Score: &ClusterScore{
						TopologySpreadScore:          2,
						AffinityScore:                30,
						ActiveOrCreatingBindingScore: 0,
					},
				},
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:          1,
						AffinityScore:                20,
						ActiveOrCreatingBindingScore: 0,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:          1,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 1,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 1,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 0,
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
						TopologySpreadScore:          2,
						AffinityScore:                30,
						ActiveOrCreatingBindingScore: 1,
					},
				},
				{
					Cluster: clusterE,
					Score: &ClusterScore{
						TopologySpreadScore:          2,
						AffinityScore:                30,
						ActiveOrCreatingBindingScore: 0,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:          1,
						AffinityScore:                20,
						ActiveOrCreatingBindingScore: 1,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore:          1,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 0,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 1,
					},
				},
			},
			want: ScoredClusters{
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:          2,
						AffinityScore:                30,
						ActiveOrCreatingBindingScore: 1,
					},
				},
				{
					Cluster: clusterE,
					Score: &ClusterScore{
						TopologySpreadScore:          2,
						AffinityScore:                30,
						ActiveOrCreatingBindingScore: 0,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:          1,
						AffinityScore:                20,
						ActiveOrCreatingBindingScore: 1,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore:          1,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 0,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 1,
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
						TopologySpreadScore:          1,
						AffinityScore:                20,
						ActiveOrCreatingBindingScore: 0,
					},
				},
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:          2,
						AffinityScore:                30,
						ActiveOrCreatingBindingScore: 1,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 0,
					},
				},
				{
					Cluster: clusterE,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 1,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore:          1,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 1,
					},
				},
			},
			want: ScoredClusters{
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:          2,
						AffinityScore:                30,
						ActiveOrCreatingBindingScore: 1,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:          1,
						AffinityScore:                20,
						ActiveOrCreatingBindingScore: 0,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore:          1,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 1,
					},
				},
				{
					Cluster: clusterE,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 1,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                10,
						ActiveOrCreatingBindingScore: 0,
					},
				},
			},
		},
		{
			name: "sort by name when scores are the same",
			scs: ScoredClusters{
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                0,
						ActiveOrCreatingBindingScore: 0,
					},
				},
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                0,
						ActiveOrCreatingBindingScore: 0,
					},
				},
			},
			want: ScoredClusters{
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                0,
						ActiveOrCreatingBindingScore: 0,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:          0,
						AffinityScore:                0,
						ActiveOrCreatingBindingScore: 0,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sort.Sort(sort.Reverse(tc.scs))
			if diff := cmp.Diff(tc.scs, tc.want); diff != "" {
				t.Fatalf("Sort() sorted diff (-got, +want): %s", diff)
			}
		})
	}
}
