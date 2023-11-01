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
	"k8s.io/apimachinery/pkg/util/validation"

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

	if len(clusterResourcePlacement.Name) > validation.DNS1035LabelMaxLength {
		allErr = append(allErr, fmt.Errorf("the name field cannot have length exceeding %d", validation.DNS1035LabelMaxLength))
	}

	for _, selector := range clusterResourcePlacement.Spec.ResourceSelectors {
		//TODO: make sure the selector's gvk is valid
		if selector.LabelSelector != nil {
			if len(selector.Name) != 0 {
				allErr = append(allErr, fmt.Errorf("the labelSelector and name fields are mutually exclusive in selector %+v", selector))
			}
			allErr = append(allErr, validateLabelSelector(selector.LabelSelector, "resource selector"))
		}
	}

	if clusterResourcePlacement.Spec.Policy != nil {
		if err := validatePlacementPolicy(clusterResourcePlacement.Spec.Policy); err != nil {
			allErr = append(allErr, fmt.Errorf("the placement policy field is invalid: %w", err))
		}
	}

	if err := validateRolloutStrategy(clusterResourcePlacement.Spec.Strategy); err != nil {
		allErr = append(allErr, fmt.Errorf("the rollout Strategy field  is invalid: %w", err))
	}

	return apiErrors.NewAggregate(allErr)
}

func IsPlacementTypeUpdated(oldCRP, currentCRP placementv1beta1.ClusterResourcePlacement) bool {
	oldPolicy := oldCRP.Spec.Policy
	currentPolicy := currentCRP.Spec.Policy
	if oldPolicy != nil && currentPolicy != nil {
		return oldPolicy.PlacementType != currentPolicy.PlacementType
	}
	return false
}

func validatePlacementPolicy(policy *placementv1beta1.PlacementPolicy) error {
	switch policy.PlacementType {
	case placementv1beta1.PickFixedPlacementType:
		if err := validatePolicyForPickFixedPlacementType(policy); err != nil {
			return err
		}
	case placementv1beta1.PickAllPlacementType:
		if err := validatePolicyForPickAllPlacementType(policy); err != nil {
			return err
		}
	case placementv1beta1.PickNPlacementType:
		if err := validatePolicyForPickNPolicyType(policy); err != nil {
			return err
		}
	}

	return nil
}

func validatePolicyForPickFixedPlacementType(policy *placementv1beta1.PlacementPolicy) error {
	allErr := make([]error, 0)
	if len(policy.ClusterNames) == 0 {
		allErr = append(allErr, fmt.Errorf("cluster names cannot be empty for policy type %s", placementv1beta1.PickFixedPlacementType))
	}
	if policy.NumberOfClusters != nil {
		allErr = append(allErr, fmt.Errorf("number of clusters must be nil for policy type %s, only valid for PickN placement policy type", placementv1beta1.PickFixedPlacementType))
	}
	if policy.Affinity != nil {
		allErr = append(allErr, fmt.Errorf("affinity must be nil for policy type %s, only valid for PickAll/PickN placement poliy types", placementv1beta1.PickFixedPlacementType))
	}
	if len(policy.TopologySpreadConstraints) > 0 {
		allErr = append(allErr, fmt.Errorf("topology spread constraints needs to be empty for policy type %s, only valid for PickN policy type", placementv1beta1.PickFixedPlacementType))
	}

	return apiErrors.NewAggregate(allErr)
}

func validatePolicyForPickAllPlacementType(policy *placementv1beta1.PlacementPolicy) error {
	allErr := make([]error, 0)
	if len(policy.ClusterNames) > 0 {
		allErr = append(allErr, fmt.Errorf("cluster names needs to be empty for policy type %s, only valid for PickFixed policy type", placementv1beta1.PickAllPlacementType))
	}
	if policy.NumberOfClusters != nil {
		allErr = append(allErr, fmt.Errorf("number of clusters must be nil for policy type %s, only valid for PickN placement policy type", placementv1beta1.PickAllPlacementType))
	}
	// Allowing user to supply empty cluster affinity, only validating cluster affinity if non-nil
	if policy.Affinity != nil && policy.Affinity.ClusterAffinity != nil {
		allErr = append(allErr, validateClusterAffinity(policy.Affinity.ClusterAffinity, policy.PlacementType))
	}
	if len(policy.TopologySpreadConstraints) > 0 {
		allErr = append(allErr, fmt.Errorf("topology spread constraints needs to be empty for policy type %s, only valid for PickN policy type", placementv1beta1.PickAllPlacementType))
	}

	return apiErrors.NewAggregate(allErr)
}

func validatePolicyForPickNPolicyType(policy *placementv1beta1.PlacementPolicy) error {
	allErr := make([]error, 0)
	if len(policy.ClusterNames) > 0 {
		allErr = append(allErr, fmt.Errorf("cluster names needs to be empty for policy type %s, only valid for PickFixed policy type", placementv1beta1.PickNPlacementType))
	}
	if policy.NumberOfClusters != nil {
		if *policy.NumberOfClusters < 0 {
			allErr = append(allErr, fmt.Errorf("number of clusters cannot be %d for policy type %s", *policy.NumberOfClusters, placementv1beta1.PickNPlacementType))
		}
	} else {
		allErr = append(allErr, fmt.Errorf("number of cluster cannot be nil for policy type %s", placementv1beta1.PickNPlacementType))
	}
	// Allowing user to supply empty cluster affinity, only validating cluster affinity if non-nil
	if policy.Affinity != nil && policy.Affinity.ClusterAffinity != nil {
		allErr = append(allErr, validateClusterAffinity(policy.Affinity.ClusterAffinity, policy.PlacementType))
	}
	if len(policy.TopologySpreadConstraints) > 0 {
		allErr = append(allErr, validateTopologySpreadConstraints(policy.TopologySpreadConstraints))
	}

	return apiErrors.NewAggregate(allErr)
}

func validateClusterAffinity(clusterAffinity *placementv1beta1.ClusterAffinity, placementType placementv1beta1.PlacementType) error {
	allErr := make([]error, 0)
	// Both RequiredDuringSchedulingIgnoredDuringExecution and PreferredDuringSchedulingIgnoredDuringExecution are optional fields, so validating only if non-nil/length is greater than zero
	switch placementType {
	case placementv1beta1.PickAllPlacementType:
		if clusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			allErr = append(allErr, validateClusterSelector(clusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution))
		}
		if len(clusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution) > 0 {
			allErr = append(allErr, fmt.Errorf("PreferredDuringSchedulingIgnoredDuringExecution will be ignored for placement policy type %s", placementType))
		}
	case placementv1beta1.PickNPlacementType:
		if clusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			allErr = append(allErr, validateClusterSelector(clusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution))
		}
		if len(clusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution) > 0 {
			allErr = append(allErr, validatePreferredClusterSelectors(clusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution))
		}
	}
	return apiErrors.NewAggregate(allErr)
}

func validateTopologySpreadConstraints(topologyConstraints []placementv1beta1.TopologySpreadConstraint) error {
	allErr := make([]error, 0)
	for _, tc := range topologyConstraints {
		if len(tc.WhenUnsatisfiable) > 0 && tc.WhenUnsatisfiable != placementv1beta1.DoNotSchedule && tc.WhenUnsatisfiable != placementv1beta1.ScheduleAnyway {
			allErr = append(allErr, fmt.Errorf("unknown when unsatisfiable type %s", tc.WhenUnsatisfiable))
		}
	}
	return apiErrors.NewAggregate(allErr)
}

func validateClusterSelector(clusterSelector *placementv1beta1.ClusterSelector) error {
	allErr := make([]error, 0)
	for _, clusterSelectorTerm := range clusterSelector.ClusterSelectorTerms {
		// Since label selector is a required field in ClusterSelectorTerm, not checking to see if it's an empty object.
		allErr = append(allErr, validateLabelSelector(&clusterSelectorTerm.LabelSelector, "cluster selector"))
	}
	return apiErrors.NewAggregate(allErr)
}

func validatePreferredClusterSelectors(preferredClusterSelectors []placementv1beta1.PreferredClusterSelector) error {
	allErr := make([]error, 0)
	for _, preferredClusterSelector := range preferredClusterSelectors {
		// API server validation on object occurs before webhook is triggered hence not validating weight.
		allErr = append(allErr, validateLabelSelector(&preferredClusterSelector.Preference.LabelSelector, "preferred cluster selector"))
	}
	return apiErrors.NewAggregate(allErr)
}

func validateLabelSelector(labelSelector *metav1.LabelSelector, parent string) error {
	if _, err := metav1.LabelSelectorAsSelector(labelSelector); err != nil {
		return fmt.Errorf("the labelSelector in %s %+v is invalid: %w", parent, labelSelector, err)
	}
	return nil
}

func validateRolloutStrategy(rolloutStrategy placementv1beta1.RolloutStrategy) error {
	allErr := make([]error, 0)

	if rolloutStrategy.Type != "" && rolloutStrategy.Type != placementv1beta1.RollingUpdateRolloutStrategyType {
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
