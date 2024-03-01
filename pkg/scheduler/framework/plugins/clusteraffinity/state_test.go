/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusteraffinity

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

func TestPreparePluginState(t *testing.T) {
	testCases := []struct {
		name             string
		clusters         []clusterv1beta1.MemberCluster
		policy           *placementv1beta1.ClusterSchedulingPolicySnapshot
		wantPS           *pluginState
		wantErrStrPrefix string
	}{
		{
			name: "no preferred term which requires sorting",
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
							nodeCountPropertyName: {
								Value: "10",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName1,
					},
					Spec: clusterv1beta1.MemberClusterSpec{},
					Status: clusterv1beta1.MemberClusterStatus{
						Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
							nodeCountPropertyName: {
								Value: "2",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName1,
					},
					Spec: clusterv1beta1.MemberClusterSpec{},
					Status: clusterv1beta1.MemberClusterStatus{
						Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
							nodeCountPropertyName: {
								Value: "8",
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
											PropertySorter: &placementv1beta1.PropertySortPreference{
												Name:      nodeCountPropertyName,
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
			wantPS: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					nodeCountPropertyName: {
						min: ptr.To(resource.MustParse("2")),
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
							nodeCountPropertyName: {
								Value: "7",
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
						Name: clusterName1,
					},
					Spec: clusterv1beta1.MemberClusterSpec{},
					Status: clusterv1beta1.MemberClusterStatus{
						Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
							nodeCountPropertyName: {
								Value: "1",
							},
						},
						ResourceUsage: clusterv1beta1.ResourceUsage{
							Available: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("11"),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName1,
					},
					Spec: clusterv1beta1.MemberClusterSpec{},
					Status: clusterv1beta1.MemberClusterStatus{
						Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
							nodeCountPropertyName: {
								Value: "3",
							},
						},
						ResourceUsage: clusterv1beta1.ResourceUsage{
							Available: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("1"),
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
											PropertySorter: &placementv1beta1.PropertySortPreference{
												Name:      nodeCountPropertyName,
												SortOrder: placementv1beta1.Ascending,
											},
										},
									},
									{
										Weight: 100,
										Preference: placementv1beta1.ClusterSelectorTerm{
											PropertySorter: &placementv1beta1.PropertySortPreference{
												Name:      availableCPUPropertyName,
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
			wantPS: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					nodeCountPropertyName: {
						min: ptr.To(resource.MustParse("1")),
						max: ptr.To(resource.MustParse("7")),
					},
					availableCPUPropertyName: {
						min: ptr.To(resource.MustParse("1")),
						max: ptr.To(resource.MustParse("11")),
					},
				},
			},
		},
		{
			name: "property is not supported on any cluster",
			clusters: []clusterv1beta1.MemberCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName1,
					},
					Spec: clusterv1beta1.MemberClusterSpec{},
					Status: clusterv1beta1.MemberClusterStatus{
						Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
							nodeCountPropertyName: {
								Value: "10",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName1,
					},
					Spec: clusterv1beta1.MemberClusterSpec{},
					Status: clusterv1beta1.MemberClusterStatus{
						Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
							nodeCountPropertyName: {
								Value: "2",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName1,
					},
					Spec: clusterv1beta1.MemberClusterSpec{},
					Status: clusterv1beta1.MemberClusterStatus{
						Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
							nodeCountPropertyName: {
								Value: "8",
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
											PropertySorter: &placementv1beta1.PropertySortPreference{
												Name:      availableCPUPropertyName,
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
			wantPS: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					availableCPUPropertyName: {
						min: nil,
						max: nil,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			state := framework.NewCycleState(tc.clusters, nil, nil)
			ps, err := preparePluginState(state, tc.policy)
			if tc.wantErrStrPrefix != "" {
				if err == nil {
					t.Fatalf("preparePluginState(), got no error, want error with prefix %s", tc.wantErrStrPrefix)
				}

				if !strings.HasPrefix(err.Error(), tc.wantErrStrPrefix) {
					t.Fatalf("preparePluginState(), got error %v, expected error with prefix %s", err, tc.wantErrStrPrefix)
				}
				return
			}

			if diff := cmp.Diff(ps, tc.wantPS, cmp.AllowUnexported(pluginState{}, observedMinMaxValues{})); diff != "" {
				t.Fatalf("preparePluginState() diff (-got, +want):\n%s", diff)
			}
		})
	}
}
