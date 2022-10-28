/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package validator

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiErrors "k8s.io/apimachinery/pkg/util/errors"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

// ValidateClusterResourcePlacement validate a ClusterResourcePlacement object
func ValidateClusterResourcePlacement(clusterResourcePlacement *fleetv1alpha1.ClusterResourcePlacement) error {
	allErr := make([]error, 0)

	for _, selector := range clusterResourcePlacement.Spec.ResourceSelectors {
		//TODO: make sure the selector's gvk is valid
		if selector.LabelSelector != nil {
			if len(selector.Name) != 0 {
				allErr = append(allErr, fmt.Errorf("the labelSelector and name fields are mutually exclusive in selector %+v", selector))
			}
			if _, err := metav1.LabelSelectorAsSelector(selector.LabelSelector); err != nil {
				allErr = append(allErr, fmt.Errorf("the labelSelector in resource selector %+v is invalid: %w", selector, err))
			}
		}
	}

	if clusterResourcePlacement.Spec.Policy != nil && clusterResourcePlacement.Spec.Policy.Affinity != nil &&
		clusterResourcePlacement.Spec.Policy.Affinity.ClusterAffinity != nil {
		for _, selector := range clusterResourcePlacement.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms {
			if _, err := metav1.LabelSelectorAsSelector(&selector.LabelSelector); err != nil {
				allErr = append(allErr, fmt.Errorf("the labelSelector in cluster selector %+v is invalid: %w", selector, err))
			}
		}
	}

	return apiErrors.NewAggregate(allErr)
}
