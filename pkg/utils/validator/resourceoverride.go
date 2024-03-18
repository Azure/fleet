package validator

import (
	"context"
	"fmt"
	"reflect"

	apiErrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
)

// ValidatrResourceOverride validates resource override fields and returns error.
func ValidateResourceOverride(ctx context.Context, client client.Client, ro fleetv1alpha1.ResourceOverride) error {
	allErr := make([]error, 0)

	// Check if the resource is being selected by resource name
	if !ValidateResourceSelectedByName(ro) {
		allErr = append(allErr, fmt.Errorf("resource is not being selected by resource name"))
	}
	// Check if the override count limit has been reached
	if !ValidateROLimit(ctx, client) {
		allErr = append(allErr, fmt.Errorf("at most 100 resource overrides can be created"))
	}
	// Check if the override count limit has been reached
	if !ValidateROResourceLimit(ctx, client, ro) {
		allErr = append(allErr, fmt.Errorf("only 1 resource override per resource can be created"))
	}
	return apiErrors.NewAggregate(allErr)
}

// ValidateResourceSelectedByName checks if override is selecting resource by name
func ValidateResourceSelectedByName(ro fleetv1alpha1.ResourceOverride) bool {
	for _, selector := range ro.Spec.ResourceSelectors {
		// Check if the resource is being selected by resource name
		if selector.Name == "" {
			return false
		}
	}
	return true
}

// ValidateROLimit checks if there are at most 100 resource overrides
func ValidateROLimit(ctx context.Context, client client.Client) bool {
	// Create a list of cluster resource overrides
	roList := &fleetv1alpha1.ResourceOverrideList{}
	if err := client.List(ctx, roList); err != nil {
		return false
	}
	// Check if the override count limit has been reached
	return len(roList.Items) < 100
}

// ValidateROResourceLimit checks if there is only 1 resource override per resource
func ValidateROResourceLimit(ctx context.Context, client client.Client, ro fleetv1alpha1.ResourceOverride) bool {
	// Create a list of cluster resource overrides
	roList := &fleetv1alpha1.ResourceOverrideList{}
	if err := client.List(ctx, roList); err != nil {
		return false
	}
	var count int64
	for _, override := range roList.Items {
		for _, selector := range override.Spec.ResourceSelectors {
			for _, roSelector := range ro.Spec.ResourceSelectors {
				if reflect.DeepEqual(selector, roSelector) {
					count++
				}
			}
		}
	}
	return count < 1
}
