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

package framework

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
)

// TestClusterScoreToAdd tests the Add() method of ClusterScore.
func TestClusterScoreAdd(t *testing.T) {
	s1 := &ClusterScore{
		TopologySpreadScore:            0,
		AffinityScore:                  0,
		ObsoletePlacementAffinityScore: 0,
	}

	s2 := &ClusterScore{
		TopologySpreadScore:            1,
		AffinityScore:                  5,
		ObsoletePlacementAffinityScore: 1,
	}

	s1.Add(s2)
	want := &ClusterScore{
		TopologySpreadScore:            1,
		AffinityScore:                  5,
		ObsoletePlacementAffinityScore: 1,
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
				TopologySpreadScore:            1,
				AffinityScore:                  5,
				ObsoletePlacementAffinityScore: 1,
			},
			s2: &ClusterScore{
				TopologySpreadScore:            1,
				AffinityScore:                  5,
				ObsoletePlacementAffinityScore: 1,
			},
			want: true,
		},
		{
			name: "s1 is not equal to s2",
			s1: &ClusterScore{
				TopologySpreadScore:            2,
				AffinityScore:                  5,
				ObsoletePlacementAffinityScore: 1,
			},
			s2: &ClusterScore{
				TopologySpreadScore:            1,
				AffinityScore:                  7,
				ObsoletePlacementAffinityScore: 0,
			},
		},
		{
			name: "s1 is nil",
			s2: &ClusterScore{
				TopologySpreadScore:            1,
				AffinityScore:                  7,
				ObsoletePlacementAffinityScore: 0,
			},
		},
		{
			name: "s2 is nil",
			s1: &ClusterScore{
				TopologySpreadScore:            2,
				AffinityScore:                  5,
				ObsoletePlacementAffinityScore: 1,
			},
		},
		{
			name: "both s1 and s2 are nil",
			want: true,
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
				TopologySpreadScore:            1,
				AffinityScore:                  10,
				ObsoletePlacementAffinityScore: 0,
			},
			s2: &ClusterScore{
				TopologySpreadScore:            1,
				AffinityScore:                  10,
				ObsoletePlacementAffinityScore: 1,
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

func TestClusterScoreLessWhenEqual(t *testing.T) {
	s1 := &ClusterScore{
		TopologySpreadScore:            0,
		AffinityScore:                  0,
		ObsoletePlacementAffinityScore: 0,
	}

	s2 := &ClusterScore{
		TopologySpreadScore:            0,
		AffinityScore:                  0,
		ObsoletePlacementAffinityScore: 0,
	}

	if s1.Less(s2) || s2.Less(s1) {
		t.Fatalf("Less(%v, %v) = %v, Less(%v, %v) = %v, want both to be false", s1, s2, s1.Less(s2), s2, s1, s2.Less(s1))
	}
}

func TestScoredClustersSort(t *testing.T) {
	clusterA := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-1",
		},
	}
	clusterB := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-2",
		},
	}
	clusterC := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-3",
		},
	}
	clusterD := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-4",
		},
	}
	clusterE := &clusterv1beta1.MemberCluster{
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
						TopologySpreadScore:            0,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  20,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterE,
					Score: &ClusterScore{
						TopologySpreadScore:            2,
						AffinityScore:                  30,
						ObsoletePlacementAffinityScore: 0,
					},
				},
			},
			want: ScoredClusters{
				{
					Cluster: clusterE,
					Score: &ClusterScore{
						TopologySpreadScore:            2,
						AffinityScore:                  30,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  20,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 0,
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
						TopologySpreadScore:            2,
						AffinityScore:                  30,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterE,
					Score: &ClusterScore{
						TopologySpreadScore:            2,
						AffinityScore:                  30,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  20,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 1,
					},
				},
			},
			want: ScoredClusters{
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:            2,
						AffinityScore:                  30,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterE,
					Score: &ClusterScore{
						TopologySpreadScore:            2,
						AffinityScore:                  30,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  20,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 1,
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
						TopologySpreadScore:            1,
						AffinityScore:                  20,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:            2,
						AffinityScore:                  30,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterE,
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 1,
					},
				},
			},
			want: ScoredClusters{
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:            2,
						AffinityScore:                  30,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  20,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterE,
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  10,
						ObsoletePlacementAffinityScore: 0,
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
						TopologySpreadScore:            0,
						AffinityScore:                  0,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  0,
						ObsoletePlacementAffinityScore: 0,
					},
				},
			},
			want: ScoredClusters{
				{
					Cluster: clusterD,
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  0,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  0,
						ObsoletePlacementAffinityScore: 0,
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

func TestScoredClustersString(t *testing.T) {
	clusterA := &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}}
	clusterB := &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"}}
	clusterC := &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-c"}}

	testCases := []struct {
		name     string
		scs      ScoredClusters
		expected string
	}{
		{
			name:     "empty slice",
			scs:      ScoredClusters{},
			expected: "ScoredClusters{}",
		},
		{
			name: "single cluster",
			scs: ScoredClusters{
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:            1,
						AffinityScore:                  2,
						ObsoletePlacementAffinityScore: 0,
					},
				},
			},
			expected: "ScoredClusters{Cluster{Name: cluster-a, Score: &{1 2 0}}}",
		},
		{
			name: "multiple clusters",
			scs: ScoredClusters{
				{
					Cluster: clusterA,
					Score: &ClusterScore{
						TopologySpreadScore:            100,
						AffinityScore:                  50,
						ObsoletePlacementAffinityScore: 1,
					},
				},
				{
					Cluster: clusterB,
					Score: &ClusterScore{
						TopologySpreadScore:            0,
						AffinityScore:                  0,
						ObsoletePlacementAffinityScore: 0,
					},
				},
				{
					Cluster: clusterC,
					Score: &ClusterScore{
						TopologySpreadScore:            -10,
						AffinityScore:                  -5,
						ObsoletePlacementAffinityScore: 0,
					},
				},
			},
			expected: "ScoredClusters{Cluster{Name: cluster-a, Score: &{100 50 1}}, Cluster{Name: cluster-b, Score: &{0 0 0}}, Cluster{Name: cluster-c, Score: &{-10 -5 0}}}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.scs.String()
			if got != tc.expected {
				t.Fatalf("String() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestScoredClustersStringTruncation(t *testing.T) {
	var scs ScoredClusters
	var i int32
	for i = 0; i < maxClusterInfoForDebugging+5; i++ {
		cluster := &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("cluster-%02d", i)}}
		scs = append(scs, &ScoredCluster{
			Cluster: cluster,
			Score: &ClusterScore{
				TopologySpreadScore:            i,
				AffinityScore:                  i * 2,
				ObsoletePlacementAffinityScore: int(i) % 2,
			},
		})
	}

	output := scs.String()

	if got, want := strings.Count(output, "Cluster{Name:"), maxClusterInfoForDebugging; got != want {
		t.Fatalf("String() truncated cluster count = %d, want %d", got, want)
	}

	if !strings.HasPrefix(output, "ScoredClusters{") {
		t.Fatalf("String() prefix mismatch: %q", output)
	}

	if !strings.HasSuffix(output, "}") {
		t.Fatalf("String() suffix mismatch: %q", output)
	}

	if !strings.Contains(output, "cluster-00") {
		t.Fatalf("String() missing first cluster: %q", output)
	}

	lastIncluded := fmt.Sprintf("cluster-%02d", maxClusterInfoForDebugging-1)
	if !strings.Contains(output, lastIncluded) {
		t.Fatalf("String() missing last retained cluster %q: %q", lastIncluded, output)
	}

	if strings.Contains(output, fmt.Sprintf("cluster-%02d", maxClusterInfoForDebugging)) {
		t.Fatalf("String() should not contain overflow cluster: %q", output)
	}
}
