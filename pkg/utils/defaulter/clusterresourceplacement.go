/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package defaulter is an interface for setting default values for a resource.
package defaulter

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// SetDefaultsClusterResourcePlacement sets the default values for ClusterResourcePlacement.
func SetDefaultsClusterResourcePlacement(obj *fleetv1beta1.ClusterResourcePlacement) {
	if obj.Spec.Policy == nil {
		obj.Spec.Policy = &fleetv1beta1.PlacementPolicy{
			PlacementType: fleetv1beta1.PickAllPlacementType,
		}
	}
	if obj.Spec.Policy.TopologySpreadConstraints != nil {
		for _, constraint := range obj.Spec.Policy.TopologySpreadConstraints {
			if constraint.MaxSkew == nil {
				constraint.MaxSkew = new(int32)
				*constraint.MaxSkew = fleetv1beta1.DefaultMaxSkewValue
			}
			if constraint.WhenUnsatisfiable == "" {
				constraint.WhenUnsatisfiable = fleetv1beta1.DoNotSchedule
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
			maxUnavailable := intstr.FromString(fleetv1beta1.DefaultMaxUnavailableValue)
			strategy.RollingUpdate.MaxUnavailable = &maxUnavailable
		}
		if strategy.RollingUpdate.MaxSurge == nil {
			maxSurge := intstr.FromString(fleetv1beta1.DefaultMaxSurgeValue)
			strategy.RollingUpdate.MaxSurge = &maxSurge
		}
		if strategy.RollingUpdate.UnavailablePeriodSeconds == nil {
			unavailablePeriodSeconds := fleetv1beta1.DefaultUnavailablePeriodSeconds
			strategy.RollingUpdate.UnavailablePeriodSeconds = &unavailablePeriodSeconds
		}
	}
	if strategy.ApplyStrategy != nil {
		if strategy.ApplyStrategy.Type == "" {
			strategy.ApplyStrategy.Type = fleetv1beta1.ApplyStrategyTypeClientSideApply
		}
		if strategy.ApplyStrategy.Type == fleetv1beta1.ApplyStrategyTypeServerSideApply {
			if strategy.ApplyStrategy.ServerSideApplyConfig == nil {
				strategy.ApplyStrategy.ServerSideApplyConfig = &fleetv1beta1.ServerSideApplyConfig{}
			}
		}
	}

	if obj.Spec.RevisionHistoryLimit == nil {
		obj.Spec.RevisionHistoryLimit = new(int32)
		*obj.Spec.RevisionHistoryLimit = fleetv1beta1.RevisionHistoryLimitDefaultValue
	}
}
