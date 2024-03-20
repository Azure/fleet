package validator

import (
	"context"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
)

// ValidateResourceOverride validates resource override fields and returns error.
func ValidateResourceOverride(ctx context.Context, client client.Client, operation admissionv1.Operation, ro fleetv1alpha1.ResourceOverride) error {
	allErr := make([]error, 0)

	// Check if the resource is being selected by resource name
	if err := validateResourceSelected(ro); err != nil {
		return err
	}
	// List of resource overrides
	roList, err := listResourceOverrideList(ctx, client)
	if err != nil {
		return err
	}
	// Check if the override count limit has been reached
	if !ValidateResourceOverrideLimit(operation, roList) {
		allErr = append(allErr, fmt.Errorf("at most 100 resource overrides can be created"))
	}

	// Check if the override count limit for the resources has been reached
	if err = ValidateResourceOverrideResourceLimit(operation, ro, roList); err != nil {
		allErr = append(allErr, err)
	}
	return errors.NewAggregate(allErr)
}

// validateResourceSelected checks if override is selecting resource by name.
func validateResourceSelected(ro fleetv1alpha1.ResourceOverride) error {
	for _, selector := range ro.Spec.ResourceSelectors {
		if selector.Name == "" {
			return fmt.Errorf("resource name is required for resource selection %+v", selector)
		}
	}
	return nil
}

// ValidateResourceOverrideLimit checks if there are at most 100 cluster resource overrides.
func ValidateResourceOverrideLimit(operation admissionv1.Operation, croList *fleetv1alpha1.ResourceOverrideList) bool {
	// Check if the override count limit has been reached
	if operation == admissionv1.Create {
		return len(croList.Items) < 100
	}
	return len(croList.Items) <= 100
}

// ValidateResourceOverrideResourceLimit checks if there is only 1 cluster resource override per resource,
// assuming the resource will be selected by the name only.
func ValidateResourceOverrideResourceLimit(operation admissionv1.Operation, cro fleetv1alpha1.ResourceOverride, roList *fleetv1alpha1.ResourceOverrideList) error {
	overrideMap := make(map[fleetv1alpha1.ResourceSelector]string)
	// Add overrides and its selectors to the map
	for _, override := range roList.Items {
		selectors := override.Spec.ResourceSelectors
		for _, selector := range selectors {
			overrideMap[selector] = override.GetName()
		}
	}

	// Check if any of the cro selectors exist in the override map
	for _, croSelector := range cro.Spec.ResourceSelectors {
		if overrideMap[croSelector] != "" {
			// If update operation, ignore the same cluster resource override
			if cro.GetName() == overrideMap[croSelector] && operation == admissionv1.Update {
				continue
			}
			return fmt.Errorf("the resource %v has been selected by both %v and %v, which are not supported", croSelector.Name, cro.GetName(), overrideMap[croSelector])
		}
	}
	return nil
}

// listResourceOverrideList returns a list of resource overrides.
func listResourceOverrideList(ctx context.Context, client client.Client) (*fleetv1alpha1.ResourceOverrideList, error) {
	roList := &fleetv1alpha1.ResourceOverrideList{}
	if err := client.List(ctx, roList); err != nil {
		klog.ErrorS(err, "Failed to list resource overrides when validating")
		return nil, fmt.Errorf("failed to list resource overrides, please retry the request: %w", err)
	}
	return roList, nil
}
