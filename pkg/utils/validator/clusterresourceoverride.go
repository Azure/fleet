package validator

import (
	"context"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
)

// ValidateClusterResourceOverride validates cluster resource override fields and returns error.
func ValidateClusterResourceOverride(ctx context.Context, client client.Client, operation admissionv1.Operation, cro fleetv1alpha1.ClusterResourceOverride) error {
	allErr := make([]error, 0)

	// Check if the resource is being selected by resource name
	if err := ValidateResourceSelectedByName(cro); err != nil {
		allErr = append(allErr, err)
	}
	// Get the list of cluster resource overrides
	croList, err := getClusterResourceOverrideList(ctx, client)
	if err != nil {
		allErr = append(allErr, err)
	}
	// Check if the override count limit has been reached
	if !ValidateClusterResourceOverrideLimit(operation, croList) {
		allErr = append(allErr, fmt.Errorf("at most 100 cluster resource overrides can be created"))
	}

	// Check if the override count limit has been reached
	if !ValidateClusterResourceOverrideResourceLimit(cro, croList) {
		allErr = append(allErr, fmt.Errorf("only 1 cluster resource override per resource can be created"))
	}
	return errors.NewAggregate(allErr)
}

// ValidateResourceSelectedByName checks if override is selecting resource by name.
func ValidateResourceSelectedByName(cro fleetv1alpha1.ClusterResourceOverride) error {
	for _, selector := range cro.Spec.ClusterResourceSelectors {
		// Check if the resource is being selected by resource name
		if selector.LabelSelector != nil {
			return fmt.Errorf("label selector is not supported for resource selection")
		} else if selector.Name == "" {
			return fmt.Errorf("resource name is required for resource selection")
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

// ValidateClusterResourceOverrideResourceLimit checks if there is only 1 cluster resource override per resource.
func ValidateClusterResourceOverrideResourceLimit(cro fleetv1alpha1.ClusterResourceOverride, croList *fleetv1alpha1.ClusterResourceOverrideList) bool {
	overrideMap := make(map[fleetv1beta1.ClusterResourceSelector]bool)
	// Add overrides and its selectors to the map
	for _, override := range croList.Items {
		selectors := override.Spec.ClusterResourceSelectors
		for _, selector := range selectors {
			overrideMap[selector] = true
		}
	}

	// Check if any of the cro selectors exist in the override map
	for _, croSelector := range cro.Spec.ClusterResourceSelectors {
		if ok := overrideMap[croSelector]; ok {
			return false
		}
	}
	return true
}

// getClusterResourceOverrideList returns a list of cluster resource overrides.
func getClusterResourceOverrideList(ctx context.Context, client client.Client) (*fleetv1alpha1.ClusterResourceOverrideList, error) {
	croList := &fleetv1alpha1.ClusterResourceOverrideList{}
	if err := client.List(ctx, croList); err != nil {
		klog.Errorf("Failed to list cluster resource overrides: %v", err)
		return nil, fmt.Errorf("failed to list cluster resource overrides, please retry the request: %w", err)
	}
	return croList, nil
}
