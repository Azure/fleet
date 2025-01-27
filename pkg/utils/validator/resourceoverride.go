/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package validator provides utils to validate ResourceOverride resources.
package validator

import (
	"errors"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/util/errors"

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

	if ro.Spec.Policy != nil {
		if err := validateOverridePolicy(ro.Spec.Policy); err != nil {
			allErr = append(allErr, err)
		}
	}

	return apierrors.NewAggregate(allErr)
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
	return apierrors.NewAggregate(allErr)
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
	return apierrors.NewAggregate(allErr)
}

// validateOverridePolicy checks if override rule is selecting resource by name.
func validateOverridePolicy(policy *fleetv1alpha1.OverridePolicy) error {
	allErr := make([]error, 0)
	for _, rule := range policy.OverrideRules {
		if rule.ClusterSelector != nil {
			for _, selector := range rule.ClusterSelector.ClusterSelectorTerms {
				// Check that only label selector is supported
				if selector.PropertySelector != nil || selector.PropertySorter != nil {
					allErr = append(allErr, fmt.Errorf("invalid clusterSelector %+v: only labelSelector is supported", selector))
					continue
				}
				if selector.LabelSelector == nil {
					allErr = append(allErr, fmt.Errorf("invalid clusterSelector %+v: labelSelector is required", selector))
				} else if err := validateLabelSelector(selector.LabelSelector, "cluster selector"); err != nil {
					allErr = append(allErr, err)
				}
			}
		}
		switch rule.OverrideType {
		case fleetv1alpha1.DeleteOverrideType:
			if len(rule.JSONPatchOverrides) != 0 {
				return errors.New("invalid JSONPatchOverrides: JSONPatchOverrides cannot be set when the override type is Delete")
			}

		case fleetv1alpha1.JSONPatchOverrideType:
			if err := validateJSONPatchOverride(rule.JSONPatchOverrides); err != nil {
				allErr = append(allErr, err)
			}
		}
	}
	return apierrors.NewAggregate(allErr)
}

// validateJSONPatchOverride checks if JSON patch override is valid.
func validateJSONPatchOverride(jsonPatchOverrides []fleetv1alpha1.JSONPatchOverride) error {
	if len(jsonPatchOverrides) == 0 {
		return errors.New("invalid JSONPatchOverrides: JSONPatchOverrides cannot be empty")
	}

	allErr := make([]error, 0)
	for _, patch := range jsonPatchOverrides {
		if err := validateJSONPatchOverridePath(patch.Path); err != nil {
			allErr = append(allErr, fmt.Errorf("invalid JSONPatchOverride %s: %w", patch, err))
		}

		if patch.Operator == fleetv1alpha1.JSONPatchOverrideOpRemove && len(patch.Value.Raw) != 0 {
			allErr = append(allErr, fmt.Errorf("invalid JSONPatchOverride %s: remove operation cannot have value", patch))
		}
	}
	return apierrors.NewAggregate(allErr)
}

func validateJSONPatchOverridePath(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must start with /")
	}

	// The path begins with a slash, and at least there will be two elements.
	parts := strings.Split(path, "/")[1:]
	switch parts[0] {
	case "kind", "apiVersion":
		return fmt.Errorf("cannot override typeMeta fields")
	case "metadata":
		if len(parts) == 1 {
			return fmt.Errorf("cannot override field metadata")
		} else if parts[1] != "annotations" && parts[1] != "labels" {
			return fmt.Errorf("cannot override metadata fields except annotations and labels")
		}
	case "status":
		return fmt.Errorf("cannot override status fields")
	}

	for i := range parts {
		if len(strings.TrimSpace(parts[i])) == 0 {
			return fmt.Errorf("path cannot contain empty string")
		}
	}
	return nil
}
