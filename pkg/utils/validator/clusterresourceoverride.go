package validator

import (
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"

	"k8s.io/apimachinery/pkg/util/errors"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// ValidateClusterResourceOverride validates cluster resource override fields and returns error.
func ValidateClusterResourceOverride(cro fleetv1alpha1.ClusterResourceOverride, croList *fleetv1alpha1.ClusterResourceOverrideList) error {
	allErr := make([]error, 0)

	// Check if the resource is being selected by resource name
	if err := validateClusterResourceSelectors(cro); err != nil {
		return err
	}

	// Check if the override count limit for the resources has been reached
	if err := ValidateClusterResourceOverrideResourceLimit(cro, croList); err != nil {
		allErr = append(allErr, err)
	}
	return errors.NewAggregate(allErr)
}

// validateClusterResourceSelectors checks if override is selecting resource by name.
func validateClusterResourceSelectors(cro fleetv1alpha1.ClusterResourceOverride) error {
	selectorMap := make(map[fleetv1beta1.ClusterResourceSelector]bool)
	for _, selector := range cro.Spec.ClusterResourceSelectors {
		// Check if the resource is not being selected by label selector
		if selector.LabelSelector != nil {
			return fmt.Errorf("label selector is not supported for resource selection %+v", selector)
		} else if selector.Name == "" {
			return fmt.Errorf("resource name is required for resource selection %+v", selector)
		}

		// Check if there are any duplicate selectors
		if selectorMap[selector] {
			return fmt.Errorf("duplicate selector %+v", selector)
		}
		selectorMap[selector] = true
	}
	return nil
}

// ValidateClusterResourceOverrideLimit checks if there are at most 100 cluster resource overrides.
func ValidateClusterResourceOverrideLimit(operation admissionv1.Operation, croList *fleetv1alpha1.ClusterResourceOverrideList) bool {
	// Check if the override count limit has been reached
	if operation == admissionv1.Create {
		return len(croList.Items) < 100
	}
	return len(croList.Items) <= 100
}

// ValidateClusterResourceOverrideResourceLimit checks if there is only 1 cluster resource override per resource,
// assuming the resource will be selected by the name only.
func ValidateClusterResourceOverrideResourceLimit(cro fleetv1alpha1.ClusterResourceOverride, croList *fleetv1alpha1.ClusterResourceOverrideList) error {
	overrideMap := make(map[fleetv1beta1.ClusterResourceSelector]string)
	// Add overrides and its selectors to the map
	for _, override := range croList.Items {
		selectors := override.Spec.ClusterResourceSelectors
		for _, selector := range selectors {
			overrideMap[selector] = override.GetName()
		}
	}

	// Check if any of the cro selectors exist in the override map
	for _, croSelector := range cro.Spec.ClusterResourceSelectors {
		if overrideMap[croSelector] != "" {
			// Ignore the same cluster resource override
			if cro.GetName() == overrideMap[croSelector] {
				continue
			}
			return fmt.Errorf("the resource %v has been selected by both %v and %v, which is not supported", croSelector.Name, cro.GetName(), overrideMap[croSelector])
		}
	}
	return nil
}
