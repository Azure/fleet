/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// AffinityTerm is a processed version of ClusterSelectorTerm.
type AffinityTerm struct {
	selector labels.Selector
}

// Matches returns true if the cluster matches the label selector.
func (at *AffinityTerm) Matches(cluster *fleetv1beta1.MemberCluster) bool {
	return at.selector.Matches(labels.Set(cluster.Labels))
}

// preferredAffinityTerm is a "processed" representation of PreferredClusterSelector.
type preferredAffinityTerm struct {
	AffinityTerm
	weight int32
}

// PreferredAffinityTerms is a "processed" representation of []PreferredClusterSelector.
type PreferredAffinityTerms struct {
	terms []preferredAffinityTerm
}

// Score returns a score for a cluster: the sum of the weights of the terms that match the cluster.
func (t *PreferredAffinityTerms) Score(cluster *fleetv1beta1.MemberCluster) int32 {
	var score int32
	for _, term := range t.terms {
		if term.AffinityTerm.Matches(cluster) {
			score += term.weight
		}
	}
	return score
}

func newAffinityTerm(term *fleetv1beta1.ClusterSelectorTerm) (*AffinityTerm, error) {
	selector, err := metav1.LabelSelectorAsSelector(&term.LabelSelector)
	if err != nil {
		return nil, err
	}
	return &AffinityTerm{selector: selector}, nil
}

// NewAffinityTerms returns the list of processed affinity terms.
func NewAffinityTerms(terms []fleetv1beta1.ClusterSelectorTerm) ([]AffinityTerm, error) {
	res := make([]AffinityTerm, 0, len(terms))
	for i := range terms {
		// skipping for empty terms
		if isEmptyClusterSelectorTerm(terms[i]) {
			continue
		}
		t, err := newAffinityTerm(&terms[i])
		if err != nil {
			// We get here if the label selector failed to process
			return nil, err
		}
		res = append(res, *t)
	}
	if len(res) == 0 {
		return nil, nil
	}
	return res, nil
}

// NewPreferredAffinityTerms returns the list of processed preferred affinity terms.
func NewPreferredAffinityTerms(terms []fleetv1beta1.PreferredClusterSelector) (*PreferredAffinityTerms, error) {
	res := make([]preferredAffinityTerm, 0, len(terms))
	for i, term := range terms {
		// skipping for weight == 0 or empty terms
		if term.Weight == 0 || isEmptyClusterSelectorTerm(term.Preference) {
			continue
		}
		t, err := newAffinityTerm(&term.Preference)
		if err != nil {
			// We get here if the label selector failed to process
			return nil, err
		}
		res = append(res, preferredAffinityTerm{AffinityTerm: *t, weight: terms[i].Weight})
	}
	if len(res) == 0 {
		return nil, nil
	}
	return &PreferredAffinityTerms{terms: res}, nil
}

func isEmptyClusterSelectorTerm(term fleetv1beta1.ClusterSelectorTerm) bool {
	return len(term.LabelSelector.MatchLabels) == 0 && len(term.LabelSelector.MatchExpressions) == 0
}
