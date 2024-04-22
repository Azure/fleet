/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package overrider

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestIsClusterMatched(t *testing.T) {
	tests := []struct {
		name    string
		cluster clusterv1beta1.MemberCluster
		rule    placementv1alpha1.OverrideRule
		want    bool
	}{
		{
			name: "matched overrides with nil cluster selector",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
			},
			rule: placementv1alpha1.OverrideRule{}, // nil cluster selector selects no clusters
			want: false,
		},
		{
			name: "rule with empty cluster selector",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
			},
			rule: placementv1alpha1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{}, // empty cluster label selects all clusters
			},
			want: true,
		},
		{
			name: "rule with empty cluster selector terms",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
			},
			rule: placementv1alpha1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{}, // empty cluster label terms selects all clusters
				},
			},
			want: true,
		},
		{
			name: "rule with nil cluster label selector",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
			},
			rule: placementv1alpha1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: nil,
						},
					},
				},
			},
			want: false,
		},
		{
			name: "rule with empty cluster label selector",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
			},
			rule: placementv1alpha1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{}, // empty label selector selects all clusters
						},
					},
				},
			},
			want: true,
		},
		{
			name: "matched overrides with non-empty cluster label",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
					Labels: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			rule: placementv1alpha1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value1",
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "matched overrides with multiple cluster terms",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
					Labels: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			rule: placementv1alpha1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value2",
								},
							},
						},
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value1",
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "no matched overrides with non-empty cluster label",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
					Labels: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			rule: placementv1alpha1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value2",
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IsClusterMatched(tc.cluster, tc.rule)
			if err != nil {
				t.Fatalf("IsClusterMatched() got error %v, want nil", err)
			}

			if got != tc.want {
				t.Errorf("IsClusterMatched() = %v, want %v", got, tc.want)
			}
		})
	}
}
