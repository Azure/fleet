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

package memberclusterplacement

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	. "github.com/kubefleet-dev/kubefleet/apis/v1alpha1"
)

func TestMatchPlacement(t *testing.T) {
	var tests = map[string]struct {
		placement     *ClusterResourcePlacement
		memberCluster *MemberCluster
		match         bool
	}{
		"no policy matches all": {
			placement: &ClusterResourcePlacement{
				Spec: ClusterResourcePlacementSpec{},
			},
			memberCluster: &MemberCluster{
				Spec: MemberClusterSpec{},
			},
			match: true,
		},
		"empty policy matches all": {
			placement: &ClusterResourcePlacement{
				Spec: ClusterResourcePlacementSpec{
					Policy: &PlacementPolicy{},
				},
			},
			memberCluster: &MemberCluster{
				Spec: MemberClusterSpec{},
			},
			match: true,
		},
		"empty affinity matches all": {
			placement: &ClusterResourcePlacement{
				Spec: ClusterResourcePlacementSpec{
					Policy: &PlacementPolicy{
						Affinity: &Affinity{},
					},
				},
			},
			memberCluster: &MemberCluster{
				Spec: MemberClusterSpec{},
			},
			match: true,
		},
		"named cluster matches specific": {
			placement: &ClusterResourcePlacement{
				Spec: ClusterResourcePlacementSpec{
					Policy: &PlacementPolicy{
						ClusterNames: []string{"clusterA", "clusterB"},
					},
				},
			},
			memberCluster: &MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "clusterA", // one of the named cluster
				},
			},
			match: true,
		},
		"named cluster only matches specific": {
			placement: &ClusterResourcePlacement{
				Spec: ClusterResourcePlacementSpec{
					Policy: &PlacementPolicy{
						ClusterNames: []string{"clusterA", "clusterB"},
					},
				},
			},
			memberCluster: &MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "clusterC", // not one of the named cluster
				},
			},
			match: false,
		},
		"named cluster does not matches but was selected is a match": {
			placement: &ClusterResourcePlacement{
				Spec: ClusterResourcePlacementSpec{
					Policy: &PlacementPolicy{
						ClusterNames: []string{"clusterA", "clusterB"},
					},
				},
				Status: ClusterResourcePlacementStatus{
					TargetClusters: []string{"clusterC"},
				},
			},
			memberCluster: &MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "clusterC", // not one of the named cluster
				},
			},
			match: true,
		},
		"empty cluster affinity matches all": {
			placement: &ClusterResourcePlacement{
				Spec: ClusterResourcePlacementSpec{
					Policy: &PlacementPolicy{
						Affinity: &Affinity{
							ClusterAffinity: &ClusterAffinity{},
						},
					},
				},
			},
			memberCluster: &MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "clusterA", // one of the named cluster
				},
			},
			match: true,
		},
		"cluster label selector matches": {
			placement: &ClusterResourcePlacement{
				Spec: ClusterResourcePlacementSpec{
					Policy: &PlacementPolicy{
						Affinity: &Affinity{
							ClusterAffinity: &ClusterAffinity{
								ClusterSelectorTerms: []ClusterSelectorTerm{
									{
										LabelSelector: metav1.LabelSelector{
											MatchLabels: map[string]string{"match": "label"},
										},
									},
								},
							},
						},
					},
				},
			},
			memberCluster: &MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"match": "label"}, //exact match
				},
			},
			match: true,
		},
		"cluster label selector no in match": {
			placement: &ClusterResourcePlacement{
				Spec: ClusterResourcePlacementSpec{
					Policy: &PlacementPolicy{
						Affinity: &Affinity{
							ClusterAffinity: &ClusterAffinity{
								ClusterSelectorTerms: []ClusterSelectorTerm{
									{
										LabelSelector: metav1.LabelSelector{
											MatchExpressions: []metav1.LabelSelectorRequirement{
												{
													Key:      "match",
													Operator: metav1.LabelSelectorOpNotIn,
													Values:   []string{"notLabel1", "notLabel2"},
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
			memberCluster: &MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"match": "label"}, //label is not in
				},
			},
			match: true,
		},
		"cluster label selector matches just one term is a match": {
			placement: &ClusterResourcePlacement{
				Spec: ClusterResourcePlacementSpec{
					Policy: &PlacementPolicy{
						Affinity: &Affinity{
							ClusterAffinity: &ClusterAffinity{
								ClusterSelectorTerms: []ClusterSelectorTerm{
									{
										LabelSelector: metav1.LabelSelector{
											MatchExpressions: []metav1.LabelSelectorRequirement{
												{
													Key:      "match",
													Operator: metav1.LabelSelectorOpNotIn,
													Values:   []string{"notLabel1", "notLabel2"},
												},
											},
										},
									},
									{
										LabelSelector: metav1.LabelSelector{
											MatchLabels: map[string]string{"match": "label", "match2": "label2"},
										},
									},
								},
							},
						},
					},
				},
			},
			memberCluster: &MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"match": "label"}, //label matches only one ClusterSelectorTerm
				},
			},
			match: true,
		},
		"cluster label selector not exact match is no match": {
			placement: &ClusterResourcePlacement{
				Spec: ClusterResourcePlacementSpec{
					Policy: &PlacementPolicy{
						Affinity: &Affinity{
							ClusterAffinity: &ClusterAffinity{
								ClusterSelectorTerms: []ClusterSelectorTerm{
									{
										LabelSelector: metav1.LabelSelector{
											MatchLabels: map[string]string{"match": "label", "match2": "label2"},
										},
									},
								},
							},
						},
					},
				},
			},
			memberCluster: &MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"match": "label"}, //didn't match all
				},
			},
			match: false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var uObj unstructured.Unstructured
			u, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(tt.memberCluster)
			uObj.Object = u
			if got := matchPlacement(tt.placement, &uObj); got != tt.match {
				t.Errorf("test case `%s` test match got %t, want %t", name, got, tt.match)
			}
		})
	}
}
