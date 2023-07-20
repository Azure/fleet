/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusteraffinity

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	clusterName = "cluster-1"
)

func TestMatches(t *testing.T) {
	tests := []struct {
		name    string
		term    *affinityTerm
		cluster *fleetv1beta1.MemberCluster
		want    bool
	}{
		{
			name: "matched cluster",
			term: &affinityTerm{
				selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
			},
			cluster: &fleetv1beta1.MemberCluster{
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
			cluster: &fleetv1beta1.MemberCluster{
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
			cluster: &fleetv1beta1.MemberCluster{
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
			cluster: &fleetv1beta1.MemberCluster{
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
		terms   *AffinityTerms
		cluster *fleetv1beta1.MemberCluster
		want    bool
	}{
		{
			name: "matched cluster with single term",
			terms: &AffinityTerms{
				terms: []affinityTerm{
					{
						selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
					},
				},
			},
			cluster: &fleetv1beta1.MemberCluster{
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
			terms: &AffinityTerms{
				terms: []affinityTerm{
					{
						selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
					},
					{
						selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
					},
				},
			},
			cluster: &fleetv1beta1.MemberCluster{
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
			terms: &AffinityTerms{
				terms: []affinityTerm{
					{
						selector: labels.SelectorFromSet(map[string]string{}),
					},
					{
						selector: labels.SelectorFromSet(map[string]string{"region": "us-east"}),
					},
				},
			},
			cluster: &fleetv1beta1.MemberCluster{
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
			terms: &AffinityTerms{
				terms: []affinityTerm{
					{
						selector: labels.SelectorFromSet(map[string]string{"region": "us-east"}),
					},
				},
			},
			cluster: &fleetv1beta1.MemberCluster{
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
			name: "empty terms",
			terms: &AffinityTerms{
				terms: []affinityTerm{},
			},
			cluster: &fleetv1beta1.MemberCluster{
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
		terms   *PreferredAffinityTerms
		cluster *fleetv1beta1.MemberCluster
		want    int32
	}{
		{
			name: "empty terms",
			terms: &PreferredAffinityTerms{
				terms: []preferredAffinityTerm{},
			},
			cluster: &fleetv1beta1.MemberCluster{
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
			terms: &PreferredAffinityTerms{
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
			},
			cluster: &fleetv1beta1.MemberCluster{
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
			terms: &PreferredAffinityTerms{
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
			},
			cluster: &fleetv1beta1.MemberCluster{
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
			terms: &PreferredAffinityTerms{
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
			},
			cluster: &fleetv1beta1.MemberCluster{
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
		terms []fleetv1beta1.ClusterSelectorTerm
		want  *AffinityTerms
	}{
		{
			name: "nil terms",
			want: nil,
		},
		{
			name:  "empty terms",
			terms: []fleetv1beta1.ClusterSelectorTerm{},
			want:  nil,
		},
		{
			name: "nonempty terms have empty term",
			terms: []fleetv1beta1.ClusterSelectorTerm{
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
			want: nil,
		},
		{
			name: "nonempty terms",
			terms: []fleetv1beta1.ClusterSelectorTerm{
				{
					LabelSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"region": "us-west",
						},
					},
				},
			},
			want: &AffinityTerms{
				terms: []affinityTerm{
					{
						selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
					},
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
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(AffinityTerms{}, affinityTerm{})); diff != "" {
				t.Errorf("NewAffinityTerms() affinityTerms mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestNewPreferredAffinityTerms(t *testing.T) {
	tests := []struct {
		name  string
		terms []fleetv1beta1.PreferredClusterSelector
		want  *PreferredAffinityTerms
	}{
		{
			name: "nil terms",
			want: nil,
		},
		{
			name:  "empty terms",
			terms: []fleetv1beta1.PreferredClusterSelector{},
			want:  nil,
		},
		{
			name: "nonempty terms have empty term",
			terms: []fleetv1beta1.PreferredClusterSelector{
				{
					Preference: fleetv1beta1.ClusterSelectorTerm{
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{},
						},
					},
					Weight: 5,
				},
				{
					Preference: fleetv1beta1.ClusterSelectorTerm{
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"region": "us-west",
							},
						},
					},
					Weight: 0,
				},
			},
			want: nil,
		},
		{
			name: "nonempty terms",
			terms: []fleetv1beta1.PreferredClusterSelector{
				{
					Preference: fleetv1beta1.ClusterSelectorTerm{
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"region": "us-west",
							},
						},
					},
					Weight: 5,
				},
			},
			want: &PreferredAffinityTerms{
				terms: []preferredAffinityTerm{
					{
						weight: 5,
						affinityTerm: affinityTerm{
							selector: labels.SelectorFromSet(map[string]string{"region": "us-west"}),
						},
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
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(PreferredAffinityTerms{}, preferredAffinityTerm{}, affinityTerm{})); diff != "" {
				t.Errorf("NewPreferredAffinityTerms() preferredAffinityTerm mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
