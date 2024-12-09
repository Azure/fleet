/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package defaulter is an interface for setting default values for a resource.
package defaulter

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	// DefaultMaxUnavailableValue is the default value of MaxUnavailable in the rolling update config.
	DefaultMaxUnavailableValue = "25%"

	// DefaultMaxSurgeValue is the default value of MaxSurge in the rolling update config.
	DefaultMaxSurgeValue = "25%"

	// DefaultUnavailablePeriodSeconds is the default period of time we consider a newly applied workload as unavailable.
	DefaultUnavailablePeriodSeconds = 60

	// DefaultMaxSkewValue is the default degree to which resources may be unevenly distributed.
	DefaultMaxSkewValue = 1

	// DefaultRevisionHistoryLimitValue is the default value of RevisionHistoryLimit.
	DefaultRevisionHistoryLimitValue = 10
)

// SetDefaultsClusterResourcePlacement sets the default values for ClusterResourcePlacement.
func SetDefaultsClusterResourcePlacement(obj *fleetv1beta1.ClusterResourcePlacement) {
	if obj.Spec.Policy == nil {
		obj.Spec.Policy = &fleetv1beta1.PlacementPolicy{
			PlacementType: fleetv1beta1.PickAllPlacementType,
		}
	}

	if obj.Spec.Policy.TopologySpreadConstraints != nil {
		for i := range obj.Spec.Policy.TopologySpreadConstraints {
			if obj.Spec.Policy.TopologySpreadConstraints[i].MaxSkew == nil {
				obj.Spec.Policy.TopologySpreadConstraints[i].MaxSkew = ptr.To(int32(DefaultMaxSkewValue))
			}
			if obj.Spec.Policy.TopologySpreadConstraints[i].WhenUnsatisfiable == "" {
				obj.Spec.Policy.TopologySpreadConstraints[i].WhenUnsatisfiable = fleetv1beta1.DoNotSchedule
			}
		}
	}

	if obj.Spec.Policy.Tolerations != nil {
		for i := range obj.Spec.Policy.Tolerations {
			if obj.Spec.Policy.Tolerations[i].Operator == "" {
				obj.Spec.Policy.Tolerations[i].Operator = corev1.TolerationOpEqual
			}
		}
	}

	strategy := &obj.Spec.Strategy
	if strategy.Type == "" {
		strategy.Type = fleetv1beta1.RollingUpdateRolloutStrategyType
	}
	if strategy.Type == fleetv1beta1.RollingUpdateRolloutStrategyType {
		if strategy.RollingUpdate == nil {
			strategy.RollingUpdate = &fleetv1beta1.RollingUpdateConfig{}
		}
		if strategy.RollingUpdate.MaxUnavailable == nil {
			strategy.RollingUpdate.MaxUnavailable = ptr.To(intstr.FromString(DefaultMaxUnavailableValue))
		}
		if strategy.RollingUpdate.MaxSurge == nil {
			strategy.RollingUpdate.MaxSurge = ptr.To(intstr.FromString(DefaultMaxSurgeValue))
		}
		if strategy.RollingUpdate.UnavailablePeriodSeconds == nil {
			strategy.RollingUpdate.UnavailablePeriodSeconds = ptr.To(DefaultUnavailablePeriodSeconds)
		}
	}

	if obj.Spec.Strategy.ApplyStrategy == nil {
		obj.Spec.Strategy.ApplyStrategy = &fleetv1beta1.ApplyStrategy{}
	}
	SetDefaultsApplyStrategy(obj.Spec.Strategy.ApplyStrategy)

	if obj.Spec.RevisionHistoryLimit == nil {
		obj.Spec.RevisionHistoryLimit = ptr.To(int32(DefaultRevisionHistoryLimitValue))
	}
}

// SetDefaultsApplyStrategy sets the default values for an ApplyStrategy object.
func SetDefaultsApplyStrategy(obj *fleetv1beta1.ApplyStrategy) {
	if obj.Type == "" {
		obj.Type = fleetv1beta1.ApplyStrategyTypeClientSideApply
	}
	if obj.Type == fleetv1beta1.ApplyStrategyTypeServerSideApply && obj.ServerSideApplyConfig == nil {
		obj.ServerSideApplyConfig = &fleetv1beta1.ServerSideApplyConfig{
			ForceConflicts: false,
		}
	}

	if obj.ComparisonOption == "" {
		obj.ComparisonOption = fleetv1beta1.ComparisonOptionTypePartialComparison
	}
	if obj.WhenToApply == "" {
		obj.WhenToApply = fleetv1beta1.WhenToApplyTypeAlways
	}
	if obj.WhenToTakeOver == "" {
		obj.WhenToTakeOver = fleetv1beta1.WhenToTakeOverTypeAlways
	}
}
