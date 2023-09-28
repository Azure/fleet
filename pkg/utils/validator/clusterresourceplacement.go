/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package validator provides utils to validate cluster resource placement resource.
package validator

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apiErrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils/informer"
)

var ResourceInformer informer.Manager

// ValidateClusterResourcePlacementAlpha validates a ClusterResourcePlacement v1alpha1 object.
func ValidateClusterResourcePlacementAlpha(clusterResourcePlacement *fleetv1alpha1.ClusterResourcePlacement) error {
	allErr := make([]error, 0)

	// we leverage the informer manager to do the resource scope validation
	if ResourceInformer == nil {
		allErr = append(allErr, fmt.Errorf("cannot perform resource scope check for now, please retry"))
	}

	for _, selector := range clusterResourcePlacement.Spec.ResourceSelectors {
		if selector.LabelSelector != nil {
			if len(selector.Name) != 0 {
				allErr = append(allErr, fmt.Errorf("the labelSelector and name fields are mutually exclusive in selector %+v", selector))
			}
			if _, err := metav1.LabelSelectorAsSelector(selector.LabelSelector); err != nil {
				allErr = append(allErr, fmt.Errorf("the labelSelector in resource selector %+v is invalid: %w", selector, err))
			}
		}
		if ResourceInformer != nil {
			gvk := schema.GroupVersionKind{
				Group:   selector.Group,
				Version: selector.Version,
				Kind:    selector.Kind,
			}
			// TODO: Ensure gvk created from resource selector is valid.
			if !ResourceInformer.IsClusterScopedResources(gvk) {
				allErr = append(allErr, fmt.Errorf("the resource is not found in schema (please retry) or it is not a cluster scoped resource: %v", gvk))
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

// ValidateClusterResourcePlacement validates a ClusterResourcePlacement object.
func ValidateClusterResourcePlacement(clusterResourcePlacement *placementv1beta1.ClusterResourcePlacement) error {
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
	if err := validatePlacementPolicy(clusterResourcePlacement.Spec.Policy); err != nil {
		allErr = append(allErr, fmt.Errorf("the placement policy field is invalid: %w", err))
	}

	if clusterResourcePlacement.Spec.Policy != nil && clusterResourcePlacement.Spec.Policy.Affinity != nil &&
		clusterResourcePlacement.Spec.Policy.Affinity.ClusterAffinity != nil {
		if err := validateClusterAffinity(clusterResourcePlacement.Spec.Policy.Affinity.ClusterAffinity); err != nil {
			allErr = append(allErr, fmt.Errorf("the clusterAffinity field is invalid: %w", err))
		}
	}

	if err := validateRolloutStrategy(clusterResourcePlacement.Spec.Strategy); err != nil {
		allErr = append(allErr, fmt.Errorf("the rollout Strategy field  is invalid: %w", err))
	}

	return apiErrors.NewAggregate(allErr)
}

func validateClusterAffinity(_ *placementv1beta1.ClusterAffinity) error {
	// TODO: implement this
	return nil
}

func validatePlacementPolicy(policy *placementv1beta1.PlacementPolicy) error {
	allErr := make([]error, 0)
	switch policy.PlacementType {
	case placementv1beta1.PickFixedPlacementType:
		if err := validatePolicyForPickFixedPlacementType(policy); err != nil {
			allErr = append(allErr, err)
		}
	case placementv1beta1.PickAllPlacementType:
		if err := validatePolicyForPickAllPlacementType(policy); err != nil {
			allErr = append(allErr, err)
		}
	case placementv1beta1.PickNPlacementType:
		if err := validatePolicyForPickNPolicyType(policy); err != nil {
			allErr = append(allErr, err)
		}
	}

	return apiErrors.NewAggregate(allErr)
}

func validatePolicyForPickFixedPlacementType(policy *placementv1beta1.PlacementPolicy) error {
	allErr := make([]error, 0)
	if len(policy.ClusterNames) == 0 {
		allErr = append(allErr, fmt.Errorf("cluster names cannot be empty for policy type %s", policy.PlacementType))
	}
	if policy.NumberOfClusters != nil {
		allErr = append(allErr, fmt.Errorf("NumberOfClusters must be nil for policy type %s", policy.PlacementType))
	}
	if policy.Affinity != nil {
		allErr = append(allErr, fmt.Errorf("affinity must be nil for policy type %s", policy.PlacementType))
	}
	if len(policy.TopologySpreadConstraints) > 0 {
		allErr = append(allErr, fmt.Errorf("topology spread constraints needs to be empty for policy type %s", policy.PlacementType))
	}
	return apiErrors.NewAggregate(allErr)
}

func validatePolicyForPickAllPlacementType(policy *placementv1beta1.PlacementPolicy) error {
	allErr := make([]error, 0)
	if len(policy.ClusterNames) > 0 {
		allErr = append(allErr, fmt.Errorf("cluster names needs to be empty for policy type %s", policy.PlacementType))
	}
	if policy.NumberOfClusters != nil {
		allErr = append(allErr, fmt.Errorf("NumberOfClusters must be nil for policy type %s", policy.PlacementType))
	}
	if policy.Affinity != nil && policy.Affinity.ClusterAffinity != nil {
		if err := validateClusterAffinity(policy.Affinity.ClusterAffinity); err != nil {
			allErr = append(allErr, fmt.Errorf("the clusterAffinity field is invalid: %w", err))
		}
	}
	if len(policy.TopologySpreadConstraints) > 0 {
		allErr = append(allErr, fmt.Errorf("topology spread constraints needs to be empty for policy type %s", policy.PlacementType))
	}
	return apiErrors.NewAggregate(allErr)
}

func validatePolicyForPickNPolicyType(policy *placementv1beta1.PlacementPolicy) error {
	allErr := make([]error, 0)
	if len(policy.ClusterNames) > 0 {
		allErr = append(allErr, fmt.Errorf("cluster names needs to be empty for policy type %s", policy.PlacementType))
	}
	return apiErrors.NewAggregate(allErr)
}

func validateRolloutStrategy(rolloutStrategy placementv1beta1.RolloutStrategy) error {
	allErr := make([]error, 0)
	if rolloutStrategy.Type != placementv1beta1.RollingUpdateRolloutStrategyType {
		allErr = append(allErr, fmt.Errorf("unsupported rollout strategy type `%s`", rolloutStrategy.Type))
	}

	if rolloutStrategy.RollingUpdate != nil {
		if rolloutStrategy.RollingUpdate.UnavailablePeriodSeconds != nil && *rolloutStrategy.RollingUpdate.UnavailablePeriodSeconds < 0 {
			allErr = append(allErr, fmt.Errorf("unavailablePeriodSeconds must be greater than or equal to 0, got %d", *rolloutStrategy.RollingUpdate.UnavailablePeriodSeconds))
		}
		if rolloutStrategy.RollingUpdate.MaxUnavailable != nil {
			value, err := intstr.GetScaledValueFromIntOrPercent(rolloutStrategy.RollingUpdate.MaxUnavailable, 10, true)
			if err != nil {
				allErr = append(allErr, fmt.Errorf("maxUnavailable `%+v` is invalid: %w", rolloutStrategy.RollingUpdate.MaxUnavailable, err))
			}
			if value < 0 {
				allErr = append(allErr, fmt.Errorf("maxUnavailable must be greater than or equal to 0, got `%+v`", rolloutStrategy.RollingUpdate.MaxUnavailable))
			}
		}
		if rolloutStrategy.RollingUpdate.MaxSurge != nil {
			value, err := intstr.GetScaledValueFromIntOrPercent(rolloutStrategy.RollingUpdate.MaxSurge, 10, true)
			if err != nil {
				allErr = append(allErr, fmt.Errorf("maxSurge `%+v` is invalid: %w", rolloutStrategy.RollingUpdate.MaxSurge, err))
			}
			if value < 0 {
				allErr = append(allErr, fmt.Errorf("maxSurge must be greater than or equal to 0, got `%+v`", rolloutStrategy.RollingUpdate.MaxSurge))
			}
		}
	}

	return apiErrors.NewAggregate(allErr)
}
