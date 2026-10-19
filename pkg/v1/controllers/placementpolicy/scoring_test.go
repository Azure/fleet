/*
Copyright 2026 The KubeFleet Authors.

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

package placementpolicy

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
)

// rankedCluster builds a member cluster reporting the three ranking metrics; an empty string
// leaves the metric unreported.
func rankedCluster(name, nodes, cpu, memory string) *clusterv1beta1.MemberCluster {
	cluster := &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if nodes != "" {
		cluster.Status.Properties = map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
			propertyprovider.NodeCountProperty: {Value: nodes},
		}
	}
	available := corev1.ResourceList{}
	if cpu != "" {
		available[corev1.ResourceCPU] = resource.MustParse(cpu)
	}
	if memory != "" {
		available[corev1.ResourceMemory] = resource.MustParse(memory)
	}
	cluster.Status.ResourceUsage.Available = available
	return cluster
}

func TestRankClusters(t *testing.T) {
	testCases := []struct {
		name       string
		candidates []*clusterv1beta1.MemberCluster
		want       []string
	}{
		{
			name: "fewer nodes rank first",
			candidates: []*clusterv1beta1.MemberCluster{
				rankedCluster("big", "10", "4", "8Gi"),
				rankedCluster("small", "2", "4", "8Gi"),
			},
			want: []string{"small", "big"},
		},
		{
			name: "more available CPU ranks first",
			candidates: []*clusterv1beta1.MemberCluster{
				rankedCluster("busy", "3", "1", "8Gi"),
				rankedCluster("idle", "3", "7", "8Gi"),
			},
			want: []string{"idle", "busy"},
		},
		{
			name: "more available memory ranks first",
			candidates: []*clusterv1beta1.MemberCluster{
				rankedCluster("full", "3", "4", "1Gi"),
				rankedCluster("roomy", "3", "4", "32Gi"),
			},
			want: []string{"roomy", "full"},
		},
		{
			name: "criteria add up with equal weight",
			candidates: []*clusterv1beta1.MemberCluster{
				// Best on nodes and CPU (200), worst on memory (0): 200.
				rankedCluster("two-wins", "1", "8", "1Gi"),
				// Worst on nodes and CPU (0), best on memory (100): 100.
				rankedCluster("one-win", "9", "1", "16Gi"),
				// Middle on every criterion: 150.
				rankedCluster("middling", "5", "4.5", "8.5Gi"),
			},
			want: []string{"two-wins", "middling", "one-win"},
		},
		{
			name: "all tied falls back to name order",
			candidates: []*clusterv1beta1.MemberCluster{
				rankedCluster("c", "3", "4", "8Gi"),
				rankedCluster("a", "3", "4", "8Gi"),
				rankedCluster("b", "3", "4", "8Gi"),
			},
			want: []string{"a", "b", "c"},
		},
		{
			name: "unreported metrics rank last on that criterion",
			candidates: []*clusterv1beta1.MemberCluster{
				rankedCluster("silent", "", "", ""),
				rankedCluster("reporting", "10", "1", "1Gi"),
			},
			// The reporting cluster ties with itself (min == max) and scores 0 everywhere, just
			// like the silent one; the name decides.
			want: []string{"reporting", "silent"},
		},
		{
			name: "a partially reporting cluster loses the criteria it skips",
			candidates: []*clusterv1beta1.MemberCluster{
				// Best on nodes (100), unreported elsewhere (0): 100.
				rankedCluster("nodes-only", "1", "", ""),
				// Worst on nodes (0), only reporter on CPU and memory so those tie at 0: 0.
				rankedCluster("full-report", "5", "4", "8Gi"),
			},
			want: []string{"nodes-only", "full-report"},
		},
		{
			name: "malformed node count is treated as unreported",
			candidates: []*clusterv1beta1.MemberCluster{
				rankedCluster("garbage", "lots", "4", "8Gi"),
				rankedCluster("sane", "3", "4", "8Gi"),
				rankedCluster("big", "9", "4", "8Gi"),
			},
			// The cluster with the garbage value scores 0 on node count like the biggest one,
			// and the name settles that tie.
			want: []string{"sane", "big", "garbage"},
		},
		{
			name:       "no candidates",
			candidates: nil,
			want:       []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := rankClusters(tc.candidates)
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("rankClusters() mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}
