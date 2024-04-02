/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package overrider defines common utils for working with override.
package overrider

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
)

// IsClusterMatched checks if the cluster is matched with the override rules.
func IsClusterMatched(cluster clusterv1beta1.MemberCluster, rule placementv1alpha1.OverrideRule) (bool, error) {
	if rule.ClusterSelector == nil { // it means matching all the member clusters
		return true, nil
	}

	for _, term := range rule.ClusterSelector.ClusterSelectorTerms {
		selector, err := metav1.LabelSelectorAsSelector(term.LabelSelector)
		if err != nil {
			return false, fmt.Errorf("invalid cluster label selector %v: %w", term.LabelSelector, err)
		}
		if selector.Matches(labels.Set(cluster.Labels)) {
			return true, nil
		}
	}
	return false, nil
}
