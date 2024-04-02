/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package validator provides utils to validate ClusterResourceOverride resources.
package validator

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/errors"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// ValidateClusterResourceOverride validates cluster resource override fields and returns error.
func ValidateClusterResourceOverride(cro fleetv1alpha1.ClusterResourceOverride, croList *fleetv1alpha1.ClusterResourceOverrideList) error {
	allErr := make([]error, 0)

	// Check if the resource is being selected by resource name
	if err := validateClusterResourceSelectors(cro); err != nil {
		// Skip other checks because the check is only valid if resource is selected by name
		return err
	}

	// Check if the override count limit for the resources has been reached
	if err := validateClusterResourceOverrideResourceLimit(cro, croList); err != nil {
		allErr = append(allErr, err)
	}

	// Check if override rule is using label selector
	if cro.Spec.Policy != nil {
		if err := validateClusterResourceOverridePolicy(cro); err != nil {
			allErr = append(allErr, err)
		}
	}

	return errors.NewAggregate(allErr)
}

// validateClusterResourceSelectors checks if override is selecting resource by name.
func validateClusterResourceSelectors(cro fleetv1alpha1.ClusterResourceOverride) error {
	selectorMap := make(map[fleetv1beta1.ClusterResourceSelector]bool)
	allErr := make([]error, 0)
	for _, selector := range cro.Spec.ClusterResourceSelectors {
		// Check if the resource is not being selected by label selector
		if selector.LabelSelector != nil {
			allErr = append(allErr, fmt.Errorf("label selector is not supported for resource selection %+v", selector))
			continue
		} else if selector.Name == "" {
			allErr = append(allErr, fmt.Errorf("resource name is required for resource selection %+v", selector))
			continue
		}

		// Check if there are any duplicate selectors
		if selectorMap[selector] {
			allErr = append(allErr, fmt.Errorf("resource selector %+v already exists, and must be unique", selector))
		}
		selectorMap[selector] = true
	}
	return errors.NewAggregate(allErr)
}

// validateClusterResourceOverrideResourceLimit checks if there is only 1 cluster resource override per resource,
// assuming the resource will be selected by the name only.
func validateClusterResourceOverrideResourceLimit(cro fleetv1alpha1.ClusterResourceOverride, croList *fleetv1alpha1.ClusterResourceOverrideList) error {
	// Check if croList is nil or empty, no need to check for resource limit
	if croList == nil || len(croList.Items) == 0 {
		return nil
	}
	overrideMap := make(map[fleetv1beta1.ClusterResourceSelector]string)
	// Add overrides and its selectors to the map
	for _, override := range croList.Items {
		selectors := override.Spec.ClusterResourceSelectors
		for _, selector := range selectors {
			overrideMap[selector] = override.GetName()
		}
	}

	allErr := make([]error, 0)
	// Check if any of the cro selectors exist in the override map
	for _, croSelector := range cro.Spec.ClusterResourceSelectors {
		if overrideMap[croSelector] != "" {
			// Ignore the same cluster resource override
			if cro.GetName() == overrideMap[croSelector] {
				continue
			}
			allErr = append(allErr, fmt.Errorf("invalid resource selector %+v: the resource has been selected by both %v and %v, which is not supported", croSelector, cro.GetName(), overrideMap[croSelector]))
		}
	}
	return errors.NewAggregate(allErr)
}

// validateClusterResourceOverridePolicy checks if override rule is selecting resource by name.
func validateClusterResourceOverridePolicy(cro fleetv1alpha1.ClusterResourceOverride) error {
	allErr := make([]error, 0)
	for _, rule := range cro.Spec.Policy.OverrideRules {
		if rule.ClusterSelector == nil {
			continue
		} else if len(rule.ClusterSelector.ClusterSelectorTerms) == 0 {
			allErr = append(allErr, fmt.Errorf("clusterSelector must have at least one term"))
		}
		for _, selector := range rule.ClusterSelector.ClusterSelectorTerms {
			// Check that only label selector is supported
			if selector.PropertySelector != nil || selector.PropertySorter != nil {
				allErr = append(allErr, fmt.Errorf("invalid clusterSelector %v: only labelSelector is supported", selector))
				continue
			}
			if selector.LabelSelector == nil {
				allErr = append(allErr, fmt.Errorf("invalid clusterSelector %v: labelSelector is required", selector))
			} else if err := validateLabelSelector(selector.LabelSelector, "cluster selector"); err != nil {
				allErr = append(allErr, err)
			}
		}
	}
	return errors.NewAggregate(allErr)
}
