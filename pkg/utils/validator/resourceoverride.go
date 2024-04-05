/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package validator provides utils to validate ResourceOverride resources.
package validator

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/errors"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
)

// ValidateResourceOverride validates resource override fields and returns error.
func ValidateResourceOverride(ro fleetv1alpha1.ResourceOverride, roList *fleetv1alpha1.ResourceOverrideList) error {
	allErr := make([]error, 0)

	// Check if the resource is being selected by resource name.
	if err := validateResourceSelectors(ro); err != nil {
		// Skip the resource limit check because the check is only valid if resource selectors are valid.
		return err
	}

	// Check if the override count limit for the resources has been reached.
	if err := validateResourceOverrideResourceLimit(ro, roList); err != nil {
		allErr = append(allErr, err)
	}

	// Check if override rule is using label selector
	if ro.Spec.Policy != nil {
		if err := validateOverridePolicy(ro.Spec.Policy); err != nil {
			allErr = append(allErr, err)
		}
	}

	return errors.NewAggregate(allErr)
}

// validateResourceSelectors checks if override is selecting a unique resource.
func validateResourceSelectors(ro fleetv1alpha1.ResourceOverride) error {
	selectorMap := make(map[fleetv1alpha1.ResourceSelector]bool)
	allErr := make([]error, 0)
	for _, selector := range ro.Spec.ResourceSelectors {
		// Check if there are any duplicate selectors.
		if selectorMap[selector] {
			allErr = append(allErr, fmt.Errorf("resource selector %+v already exists, and must be unique", selector))
		}
		selectorMap[selector] = true
	}
	return errors.NewAggregate(allErr)
}

// validateResourceOverrideResourceLimit checks if there is only 1 resource override per resource,
// assuming the resource will be selected by the name only.
func validateResourceOverrideResourceLimit(ro fleetv1alpha1.ResourceOverride, roList *fleetv1alpha1.ResourceOverrideList) error {
	// Check if roList is nil or empty, no need to check for resource limit.
	if roList == nil || len(roList.Items) == 0 {
		return nil
	}
	overrideMap := make(map[fleetv1alpha1.ResourceSelector]string)
	// Add overrides and its selectors to the map.
	for _, override := range roList.Items {
		selectors := override.Spec.ResourceSelectors
		for _, selector := range selectors {
			overrideMap[selector] = override.GetName()
		}
	}

	allErr := make([]error, 0)
	// Check if any of the ro selectors exist in the override map.
	for _, roSelector := range ro.Spec.ResourceSelectors {
		if overrideMap[roSelector] != "" {
			// Ignore the same resource override.
			if ro.GetName() == overrideMap[roSelector] {
				continue
			}
			allErr = append(allErr, fmt.Errorf("invalid resource selector %+v: the resource has been selected by both %v and %v, which is not supported", roSelector, ro.GetName(), overrideMap[roSelector]))
		}
	}
	return errors.NewAggregate(allErr)
}

// validateOverridePolicy checks if override rule is selecting resource by name.
func validateOverridePolicy(policy *fleetv1alpha1.OverridePolicy) error {
	allErr := make([]error, 0)
	for _, rule := range policy.OverrideRules {
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
