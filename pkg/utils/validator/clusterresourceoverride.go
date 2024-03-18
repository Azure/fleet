package validator

import (
	"context"
	"fmt"
	"reflect"

	apiErrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
)

// ValidateClusterResourceOverride validates cluster resource override fields and returns error.
func ValidateClusterResourceOverride(ctx context.Context, client client.Client, cro fleetv1alpha1.ClusterResourceOverride) error {
	allErr := make([]error, 0)

	// Check if the resource is being selected by resource name
	if !ValidateResourceSelectedByName(cro) {
		allErr = append(allErr, fmt.Errorf("resource is not being selected by resource name"))
	}
	// Check if the override count limit has been reached
	if !ValidateCROLimit(ctx, client) {
		allErr = append(allErr, fmt.Errorf("at most 100 cluster resource overrides can be created"))
	}
	// Check if the override count limit has been reached
	if !ValidateCROResourceLimit(ctx, client, cro) {
		allErr = append(allErr, fmt.Errorf("only 1 cluster resource override per resource can be created"))
	}
	return apiErrors.NewAggregate(allErr)
}

// ValidateResourceSelectedByName checks if override is selecting resource by name
func ValidateResourceSelectedByName(cro fleetv1alpha1.ClusterResourceOverride) bool {
	for _, selector := range cro.Spec.ClusterResourceSelectors {
		// Check if the resource is being selected by resource name
		if selector.Name == "" || selector.LabelSelector != nil {
			return false
		}
	}
	return true
}

// ValidateCROLimit checks if there are at most 100 cluster resource overrides
func ValidateCROLimit(ctx context.Context, client client.Client) bool {
	// Create a list of cluster resource overrides with the given resource type
	croList := &fleetv1alpha1.ClusterResourceOverrideList{}
	if err := client.List(ctx, croList); err != nil {
		return false
	}
	// Check if the override count limit has been reached
	return len(croList.Items) < 100
}

// ValidateCROResourceLimit checks if there is only 1 cluster resource override per resource
func ValidateCROResourceLimit(ctx context.Context, client client.Client, cro fleetv1alpha1.ClusterResourceOverride) bool {
	// Create a list of cluster resource overrides with the given resource type
	croList := &fleetv1alpha1.ClusterResourceOverrideList{}
	if err := client.List(ctx, croList); err != nil {
		return false
	}
	var count int64
	for _, override := range croList.Items {
		for _, selector := range override.Spec.ClusterResourceSelectors {
			for _, croSelector := range cro.Spec.ClusterResourceSelectors {
				if reflect.DeepEqual(selector, croSelector) {
					count++
				}
			}
		}
	}
	return count < 1
}
