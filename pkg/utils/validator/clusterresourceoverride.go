package validator

import (
	"context"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// ValidateClusterResourceOverride validates cluster resource override fields and returns error.
func ValidateClusterResourceOverride(ctx context.Context, client client.Client, operation admissionv1.Operation, cro fleetv1alpha1.ClusterResourceOverride) error {
	allErr := make([]error, 0)

	// Check if the resource is being selected by resource name
	if err := validateResourceSelected(cro); err != nil {
		return err
	}
	// List of cluster resource overrides
	croList, err := listClusterResourceOverrideList(ctx, client)
	if err != nil {
		return err
	}
	// Check if the override count limit has been reached
	if !ValidateClusterResourceOverrideLimit(operation, croList) {
		allErr = append(allErr, fmt.Errorf("at most 100 cluster resource overrides can be created"))
	}

	// Check if the override count limit for the resources has been reached
	if err = ValidateClusterResourceOverrideResourceLimit(operation, cro, croList); err != nil {
		allErr = append(allErr, err)
	}
	return errors.NewAggregate(allErr)
}

// validateResourceSelected checks if override is selecting resource by name.
func validateResourceSelected(cro fleetv1alpha1.ClusterResourceOverride) error {
	for _, selector := range cro.Spec.ClusterResourceSelectors {
		// Check if the resource is not being selected by label selector
		if selector.LabelSelector != nil {
			if selector.Name != "" {
				return fmt.Errorf("the labelSelector and name fields are mutually exclusive in selector %+v", selector)
			}
			return fmt.Errorf("label selector is not supported for resource selection %+v", selector)
		} else if selector.Name == "" {
			return fmt.Errorf("resource name is required for resource selection %+v", selector)
		}
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
func ValidateClusterResourceOverrideResourceLimit(operation admissionv1.Operation, cro fleetv1alpha1.ClusterResourceOverride, croList *fleetv1alpha1.ClusterResourceOverrideList) error {
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
			// If update operation, ignore the same cluster resource override
			if cro.GetName() == overrideMap[croSelector] && operation == admissionv1.Update {
				continue
			}
			return fmt.Errorf("the resource %v has been selected by both %v and %v, which are not supported", croSelector.Name, cro.GetName(), overrideMap[croSelector])
		}
	}
	return nil
}

// listClusterResourceOverrideList returns a list of cluster resource overrides.
func listClusterResourceOverrideList(ctx context.Context, client client.Client) (*fleetv1alpha1.ClusterResourceOverrideList, error) {
	croList := &fleetv1alpha1.ClusterResourceOverrideList{}
	if err := client.List(ctx, croList); err != nil {
		klog.ErrorS(err, "Failed to list cluster resource overrides when validating")
		return nil, fmt.Errorf("failed to list cluster resource overrides, please retry the request: %w", err)
	}
	return croList, nil
}
