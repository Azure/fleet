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
	"k8s.io/apimachinery/pkg/labels"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	clusterName = "cluster-1"
)

func TestMatches(t *testing.T) {
	tests := []struct {
		name    string
		term    *affinityTerm
		cluster *clusterv1beta1.MemberCluster
		want    bool
	}{
		{
			name: "matched cluster",
			term: &affinityTerm{
				selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			want: true,
		},
		{
			name: "label value mismatched cluster",
			term: &affinityTerm{
				selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"region": "us-east",
					},
				},
			},
			want: false,
		},
		{
			name: "empty terms which does not restrict the selection space",
			term: &affinityTerm{
				selector: labels.SelectorFromSet(map[string]string{}),
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			want: true,
		},
		{
			name: "label does not exist in cluster",
			term: &affinityTerm{
				selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"regions": "us-west",
					},
				},
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.term.Matches(tc.cluster)
			if got != tc.want {
				t.Fatalf("Matches()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestAffinityTermsMatches(t *testing.T) {
	tests := []struct {
		name    string
		terms   AffinityTerms
		cluster *clusterv1beta1.MemberCluster
		want    bool
	}{
		{
			name: "matched cluster with single term",
			terms: []affinityTerm{
				{
					selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			want: true,
		},
		{
			name: "matched cluster with multiple terms",
			terms: []affinityTerm{
				{
					selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
				},
				{
					selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"region": "us-east",
					},
				},
			},
			want: false,
		},
		{
			name: "matched cluster with empty terms which does not restrict the selection space",
			terms: []affinityTerm{
				{
					selector: labels.SelectorFromSet(map[string]string{}),
				},
				{
					selector: labels.SelectorFromSet(map[string]string{"region": "us-east"}),
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			want: true,
		},
		{
			name: "not matched cluster",
			terms: []affinityTerm{
				{
					selector: labels.SelectorFromSet(map[string]string{"region": "us-east"}),
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"regions": "us-west",
					},
				},
			},
			want: false,
		},
		{
			name:  "empty terms",
			terms: []affinityTerm{},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"regions": "us-west",
					},
				},
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.terms.Matches(tc.cluster)
			if got != tc.want {
				t.Fatalf("Matches()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestScore(t *testing.T) {
	tests := []struct {
		name    string
		terms   PreferredAffinityTerms
		cluster *clusterv1beta1.MemberCluster
		want    int32
	}{
		{
			name:  "empty terms",
			terms: []preferredAffinityTerm{},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			want: 0,
		},
		{
			name: "multiple terms (all matched)",
			terms: []preferredAffinityTerm{
				{
					affinityTerm: affinityTerm{
						selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
					},
					weight: 5,
				},
				{
					affinityTerm: affinityTerm{
						selector: labels.SelectorFromSet(map[string]string{}),
					},
					weight: -8,
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			want: -3,
		},
		{
			name: "multiple terms (partial matched)",
			terms: []preferredAffinityTerm{
				{
					affinityTerm: affinityTerm{
						selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
					},
					weight: 5,
				},
				{
					affinityTerm: affinityTerm{
						selector: labels.SelectorFromSet(map[string]string{"zone": "zone1"}),
					},
					weight: -8,
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"region": "us-west",
						"zone":   "zone2",
					},
				},
			},
			want: 5,
		},
		{
			name: "multiple terms (all mismatched)",
			terms: []preferredAffinityTerm{
				{
					affinityTerm: affinityTerm{
						selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
					},
					weight: 5,
				},
				{
					affinityTerm: affinityTerm{
						selector: labels.SelectorFromSet(map[string]string{"zone": "zone1"}),
					},
					weight: -8,
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"region": "us-east",
						"zone":   "zone2",
					},
				},
			},
			want: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.terms.Score(tc.cluster)
			if got != tc.want {
				t.Fatalf("Score()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewAffinityTerms(t *testing.T) {
	tests := []struct {
		name  string
		terms []placementv1beta1.ClusterSelectorTerm
		want  AffinityTerms
	}{
		{
			name: "nil terms",
			want: []affinityTerm{},
		},
		{
			name:  "empty terms",
			terms: []placementv1beta1.ClusterSelectorTerm{},
			want:  []affinityTerm{},
		},
		{
			name: "nonempty terms have empty term",
			terms: []placementv1beta1.ClusterSelectorTerm{
				{
					LabelSelector: metav1.LabelSelector{},
				},
				{
					LabelSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{},
					},
				},
				{
					LabelSelector: metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{},
					},
				},
			},
			want: []affinityTerm{},
		},
		{
			name: "nonempty terms",
			terms: []placementv1beta1.ClusterSelectorTerm{
				{
					LabelSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"region": "us-west",
						},
					},
				},
			},
			want: []affinityTerm{
				{
					selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewAffinityTerms(tc.terms)
			if err != nil {
				t.Fatalf("NewAffinityTerms() got error %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(affinityTerm{})); diff != "" {
				t.Errorf("NewAffinityTerms() affinityTerms mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestNewPreferredAffinityTerms(t *testing.T) {
	tests := []struct {
		name  string
		terms []placementv1beta1.PreferredClusterSelector
		want  PreferredAffinityTerms
	}{
		{
			name: "nil terms",
			want: []preferredAffinityTerm{},
		},
		{
			name:  "empty terms",
			terms: []placementv1beta1.PreferredClusterSelector{},
			want:  []preferredAffinityTerm{},
		},
		{
			name: "nonempty terms have empty term",
			terms: []placementv1beta1.PreferredClusterSelector{
				{
					Preference: placementv1beta1.ClusterSelectorTerm{
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{},
						},
					},
					Weight: 5,
				},
				{
					Preference: placementv1beta1.ClusterSelectorTerm{
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"region": "us-west",
							},
						},
					},
					Weight: 0,
				},
			},
			want: []preferredAffinityTerm{},
		},
		{
			name: "nonempty terms",
			terms: []placementv1beta1.PreferredClusterSelector{
				{
					Preference: placementv1beta1.ClusterSelectorTerm{
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"region": "us-west",
							},
						},
					},
					Weight: 5,
				},
			},
			want: []preferredAffinityTerm{
				{
					weight: 5,
					affinityTerm: affinityTerm{
						selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewPreferredAffinityTerms(tc.terms)
			if err != nil {
				t.Fatalf("NewPreferredAffinityTerms() got error %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(preferredAffinityTerm{}, affinityTerm{})); diff != "" {
				t.Errorf("NewPreferredAffinityTerms() preferredAffinityTerm mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

// TestExtractResourceMetricDataFrom tests the extractResourceMetricDataFrom function.
func TestExtractResourceMetricDataFrom(t *testing.T) {
	ru := &clusterv1beta1.ResourceUsage{
		Capacity: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse("12"),
			corev1.ResourceMemory:           resource.MustParse("24Gi"),
			corev1.ResourceStorage:          resource.MustParse("100Gi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("100Gi"),
		},
		Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse("10"),
			corev1.ResourceMemory:           resource.MustParse("20Gi"),
			corev1.ResourceStorage:          resource.MustParse("90Gi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("90Gi"),
		},
		Available: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse("4"),
			corev1.ResourceMemory:           resource.MustParse("4Gi"),
			corev1.ResourceStorage:          resource.MustParse("40Gi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("24Gi"),
		},
	}

	testCases := []struct {
		name                 string
		usage                *clusterv1beta1.ResourceUsage
		label                string
		wantResourceQuantity resource.Quantity
		wantErrMsg           string
	}{
		{
			name:                 "cpu resource metric (allocatable)",
			usage:                ru,
			label:                "allocatable-cpu",
			wantResourceQuantity: resource.MustParse("10"),
		},
		{
			name:                 "cpu resource metric (available)",
			usage:                ru,
			label:                "available-cpu",
			wantResourceQuantity: resource.MustParse("4"),
		},
		{
			name:                 "memory resource metric (allocatable)",
			usage:                ru,
			label:                "allocatable-memory",
			wantResourceQuantity: resource.MustParse("20Gi"),
		},
		{
			name:                 "memory resource metric (available)",
			usage:                ru,
			label:                "available-memory",
			wantResourceQuantity: resource.MustParse("4Gi"),
		},
		{
			name:       "unavailable capacity type",
			usage:      ru,
			label:      "mincapacity-cpu",
			wantErrMsg: "failed to look up resource metric name mincapacity-cpu: the capacity type does not exist",
		},
		{
			name:       "unavailable resource type",
			usage:      ru,
			label:      "allocatable-megagpu",
			wantErrMsg: "failed to look up resource metric name allocatable-megagpu: the resource name does not exist",
		},
		{
			name:       "invalid metric name (no separator)",
			usage:      ru,
			label:      "allocatablecpu",
			wantErrMsg: "failed to parse resource metric name allocatablecpu: the name is not of the correct format",
		},
		{
			name:       "invalid metric name (no capacity type)",
			usage:      ru,
			label:      "-cpu",
			wantErrMsg: "failed to parse resource metric name -cpu: the name is not of the correct format",
		},
		{
			name:       "invalid metric name (no resource type)",
			usage:      ru,
			label:      "allocatable-",
			wantErrMsg: "failed to parse resource metric name allocatable-: the name is not of the correct format",
		},
		{
			name:       "no resource usage",
			label:      "allocatable-cpu",
			wantErrMsg: "failed to look up resource metric: the resource usage is nil",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotResourceQuantity, err := ExtractResourceMetricDataFrom(tc.usage, tc.label)
			if err != nil {
				if tc.wantErrMsg == "" {
					t.Fatalf("extractResourceMetricDataFrom(), got error %v, want no error", err)
				}

				if !strings.HasPrefix(err.Error(), tc.wantErrMsg) {
					t.Fatalf("extractResourceMetricDataFrom(), got error %v, want %v", err.Error(), tc.wantErrMsg)
				}
				return
			}

			if !tc.wantResourceQuantity.Equal(gotResourceQuantity) {
				t.Fatalf("extractResourceMetricDataFrom() = %v, want %v", gotResourceQuantity, tc.wantResourceQuantity)
			}
		})
	}
}
