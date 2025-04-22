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

package clusteraffinity

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/propertyprovider"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

// TestPreScore tests the PreScore extension point of this plugin.
func TestPreScore(t *testing.T) {
	testCases := []struct {
		name       string
		clusters   []clusterv1beta1.MemberCluster
		policy     *placementv1beta1.ClusterSchedulingPolicySnapshot
		wantStatus *framework.Status
		wantPS     *pluginState
	}{
		{
			name: "no scheduling policy",
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: nil,
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.Skip, p.Name(), "no preferred cluster affinity terms specified"),
		},
		{
			name: "no affinity terms",
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: nil,
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.Skip, p.Name(), "no preferred cluster affinity terms specified"),
		},
		{
			name: "no cluster affinity terms",
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: nil,
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.Skip, p.Name(), "no preferred cluster affinity terms specified"),
		},
		{
			name: "no preferred cluster affinity terms",
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: nil,
							},
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.Skip, p.Name(), "no preferred cluster affinity terms specified"),
		},
		{
			name: "single preferred term which does not require sorting",
			clusters: []clusterv1beta1.MemberCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName1,
					},
				},
			},
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 100,
										Preference: placementv1beta1.ClusterSelectorTerm{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantPS: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{},
			},
		},
		{
			name: "single preferred term which requires sorting",
			clusters: []clusterv1beta1.MemberCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName1,
					},
					Spec: clusterv1beta1.MemberClusterSpec{},
					Status: clusterv1beta1.MemberClusterStatus{
						Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
							propertyprovider.NodeCountProperty: {
								Value: "10",
							},
						},
					},
				},
			},
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 100,
										Preference: placementv1beta1.ClusterSelectorTerm{
											PropertySorter: &placementv1beta1.PropertySorter{
												Name: propertyprovider.NodeCountProperty,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantPS: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("10")),
						max: ptr.To(resource.MustParse("10")),
					},
				},
			},
		},
		{
			name: "multiple preferred terms which require sorting",
			clusters: []clusterv1beta1.MemberCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName1,
					},
					Spec: clusterv1beta1.MemberClusterSpec{},
					Status: clusterv1beta1.MemberClusterStatus{
						Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
							propertyprovider.NodeCountProperty: {
								Value: "10",
							},
						},
						ResourceUsage: clusterv1beta1.ResourceUsage{
							Available: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("10"),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName2,
					},
					Spec: clusterv1beta1.MemberClusterSpec{},
					Status: clusterv1beta1.MemberClusterStatus{
						Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
							propertyprovider.NodeCountProperty: {
								Value: "20",
							},
						},
						ResourceUsage: clusterv1beta1.ResourceUsage{
							Available: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("12"),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName2,
					},
					Spec: clusterv1beta1.MemberClusterSpec{},
					Status: clusterv1beta1.MemberClusterStatus{
						Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
							propertyprovider.NodeCountProperty: {
								Value: "15",
							},
						},
						ResourceUsage: clusterv1beta1.ResourceUsage{
							Available: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("7"),
							},
						},
					},
				},
			},
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 100,
										Preference: placementv1beta1.ClusterSelectorTerm{
											PropertySorter: &placementv1beta1.PropertySorter{
												Name: propertyprovider.NodeCountProperty,
											},
										},
									},
									{
										Weight: 100,
										Preference: placementv1beta1.ClusterSelectorTerm{
											PropertySorter: &placementv1beta1.PropertySorter{
												Name: propertyprovider.AvailableCPUCapacityProperty,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantPS: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("10")),
						max: ptr.To(resource.MustParse("20")),
					},
					propertyprovider.AvailableCPUCapacityProperty: {
						min: ptr.To(resource.MustParse("7")),
						max: ptr.To(resource.MustParse("12")),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			state := framework.NewCycleState(tc.clusters, nil, nil)
			status := p.PreScore(ctx, state, tc.policy)

			if diff := cmp.Diff(
				status, tc.wantStatus,
				cmp.AllowUnexported(framework.Status{}),
				ignoreStatusErrorField,
			); diff != "" {
				t.Errorf("PreScore() unexpected status (-got, +want):\n%s", diff)
			}

			if tc.wantPS != nil {
				ps, err := p.readPluginState(state)
				if err != nil {
					t.Fatalf("failed to read plugin state: %v", err)
				}

				if diff := cmp.Diff(
					ps, tc.wantPS,
					cmp.AllowUnexported(pluginState{}, observedMinMaxValues{}),
				); diff != "" {
					t.Errorf("PreScore() unexpected plugin state (-got, +want):\n%s", diff)
				}
			}
		})
	}
}

// TestPluginScore tests the Score extension point of this plugin.
func TestPluginScore(t *testing.T) {
	testCases := []struct {
		name       string
		ps         *pluginState
		policy     *placementv1beta1.ClusterSchedulingPolicySnapshot
		cluster    *clusterv1beta1.MemberCluster
		wantStatus *framework.Status
		wantScore  *framework.ClusterScore
	}{
		{
			name: "single preferred term which features only label selector, matched",
			ps:   &pluginState{},
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 50,
										Preference: placementv1beta1.ClusterSelectorTerm{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue1,
					},
				},
			},
			wantScore: &framework.ClusterScore{
				AffinityScore: 50,
			},
		},
		{
			name: "single preferred term which features only label selector, not matched",
			ps:   &pluginState{},
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 50,
										Preference: placementv1beta1.ClusterSelectorTerm{
											LabelSelector: &metav1.LabelSelector{
												MatchExpressions: []metav1.LabelSelectorRequirement{
													{
														Key:      regionLabelName,
														Operator: metav1.LabelSelectorOpNotIn,
														Values: []string{
															regionLabelValue2,
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue2,
					},
				},
			},
			wantScore: &framework.ClusterScore{},
		},
		{
			name: "single preferred term which requires sorting",
			ps: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("10")),
						max: ptr.To(resource.MustParse("20")),
					},
				},
			},
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 50,
										Preference: placementv1beta1.ClusterSelectorTerm{
											PropertySorter: &placementv1beta1.PropertySorter{
												Name:      propertyprovider.NodeCountProperty,
												SortOrder: placementv1beta1.Ascending,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.NodeCountProperty: {
							Value: "15",
						},
					},
				},
			},
			wantScore: &framework.ClusterScore{
				AffinityScore: 25,
			},
		},
		{
			name: "single preferred term which features label selector and requires sorting",
			ps: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("10")),
						max: ptr.To(resource.MustParse("20")),
					},
				},
			},
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 50,
										Preference: placementv1beta1.ClusterSelectorTerm{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue2,
												},
											},
											PropertySorter: &placementv1beta1.PropertySorter{
												Name:      propertyprovider.NodeCountProperty,
												SortOrder: placementv1beta1.Descending,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue2,
					},
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.NodeCountProperty: {
							Value: "12",
						},
					},
				},
			},
			wantScore: &framework.ClusterScore{
				AffinityScore: 10,
			},
		},
		{
			name: "single preferred term which features label selector and requires sorting (cannot be sorted, no data available)",
			ps: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("10")),
						max: ptr.To(resource.MustParse("20")),
					},
				},
			},
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 50,
										Preference: placementv1beta1.ClusterSelectorTerm{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue2,
												},
											},
											PropertySorter: &placementv1beta1.PropertySorter{
												Name:      propertyprovider.NodeCountProperty,
												SortOrder: placementv1beta1.Descending,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue2,
					},
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{},
				},
			},
			wantScore: &framework.ClusterScore{
				AffinityScore: 0,
			},
		},
		{
			name: "multiple preferred terms",
			ps: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("10")),
						max: ptr.To(resource.MustParse("20")),
					},
					propertyprovider.AvailableCPUCapacityProperty: {
						min: ptr.To(resource.MustParse("7")),
						max: ptr.To(resource.MustParse("25")),
					},
				},
			},
			policy: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 50,
										Preference: placementv1beta1.ClusterSelectorTerm{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue2,
												},
											},
											PropertySorter: &placementv1beta1.PropertySorter{
												Name:      propertyprovider.NodeCountProperty,
												SortOrder: placementv1beta1.Descending,
											},
										},
									},
									{
										Weight: 20,
										Preference: placementv1beta1.ClusterSelectorTerm{
											LabelSelector: &metav1.LabelSelector{
												MatchExpressions: []metav1.LabelSelectorRequirement{
													{
														Key:      envLabelName,
														Operator: metav1.LabelSelectorOpIn,
														Values: []string{
															envLabelValue1,
														},
													},
												},
											},
											PropertySorter: &placementv1beta1.PropertySorter{
												Name:      propertyprovider.AvailableCPUCapacityProperty,
												SortOrder: placementv1beta1.Ascending,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue2,
						envLabelName:    envLabelValue1,
					},
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.NodeCountProperty: {
							Value: "12",
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Available: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("10"),
						},
					},
				},
			},
			wantScore: &framework.ClusterScore{
				AffinityScore: 27,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			state := framework.NewCycleState(nil, nil, nil)
			state.Write(framework.StateKey(p.Name()), tc.ps)

			score, status := p.Score(ctx, state, tc.policy, tc.cluster)
			if diff := cmp.Diff(
				status, tc.wantStatus,
				cmp.AllowUnexported(framework.Status{}),
				ignoreStatusErrorField,
			); diff != "" {
				t.Fatalf("Score() unexpected status (-got, +want):\n%s", diff)
			}

			if diff := cmp.Diff(score, tc.wantScore); diff != "" {
				t.Fatalf("Score() unexpected score (-got, +want):\n%s", diff)
			}
		})
	}
}
