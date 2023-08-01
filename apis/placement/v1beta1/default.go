/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"k8s.io/apimachinery/pkg/util/intstr"
)

// SetDefaultsClusterResourcePlacement sets the default values for ClusterResourcePlacement.
func SetDefaultsClusterResourcePlacement(obj *ClusterResourcePlacement) {
	if obj.Spec.Policy == nil {
		obj.Spec.Policy = &PlacementPolicy{
			PlacementType: PickAllPlacementType,
		}
	}
	strategy := &obj.Spec.Strategy
	// Set default DeploymentStrategyType as RollingUpdate.
	if strategy.Type == "" {
		strategy.Type = RollingUpdateRolloutStrategyType
	}
	if strategy.Type == RollingUpdateRolloutStrategyType {
		if strategy.RollingUpdate == nil {
			strategy.RollingUpdate = &RollingUpdateConfig{}
		}
		if strategy.RollingUpdate.MaxUnavailable == nil {
			maxUnavailable := intstr.FromString(DefaultMaxUnavailableValue)
			strategy.RollingUpdate.MaxUnavailable = &maxUnavailable
		}
		if strategy.RollingUpdate.MaxSurge == nil {
			maxSurge := intstr.FromString(DefaultMaxSurgeValue)
			strategy.RollingUpdate.MaxSurge = &maxSurge
		}
		if strategy.RollingUpdate.UnavailablePeriodSeconds == nil {
			unavailablePeriodSeconds := DefaultUnavailablePeriodSeconds
			strategy.RollingUpdate.UnavailablePeriodSeconds = &unavailablePeriodSeconds
		}
	}
	if obj.Spec.RevisionHistoryLimit == nil {
		obj.Spec.RevisionHistoryLimit = new(int32)
		*obj.Spec.RevisionHistoryLimit = RevisionHistoryLimitDefaultValue
	}
}
