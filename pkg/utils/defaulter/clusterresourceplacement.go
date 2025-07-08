/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package defaulter is an interface for setting default values for a resource.
package defaulter

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
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

// SetPlacementDefaults sets the default values for placement.
func SetPlacementDefaults(obj fleetv1beta1.PlacementObj) {
	spec := obj.GetPlacementSpec()
	if spec.Policy == nil {
		spec.Policy = &fleetv1beta1.PlacementPolicy{
			PlacementType: fleetv1beta1.PickAllPlacementType,
		}
	}

	if spec.Policy.TopologySpreadConstraints != nil {
		for i := range spec.Policy.TopologySpreadConstraints {
			if spec.Policy.TopologySpreadConstraints[i].MaxSkew == nil {
				spec.Policy.TopologySpreadConstraints[i].MaxSkew = ptr.To(int32(DefaultMaxSkewValue))
			}
			if spec.Policy.TopologySpreadConstraints[i].WhenUnsatisfiable == "" {
				spec.Policy.TopologySpreadConstraints[i].WhenUnsatisfiable = fleetv1beta1.DoNotSchedule
			}
		}
	}

	if spec.Policy.Tolerations != nil {
		for i := range spec.Policy.Tolerations {
			if spec.Policy.Tolerations[i].Operator == "" {
				spec.Policy.Tolerations[i].Operator = corev1.TolerationOpEqual
			}
		}
	}

	strategy := &spec.Strategy
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

	if spec.Strategy.ApplyStrategy == nil {
		spec.Strategy.ApplyStrategy = &fleetv1beta1.ApplyStrategy{}
	}
	SetDefaultsApplyStrategy(spec.Strategy.ApplyStrategy)

	if spec.RevisionHistoryLimit == nil {
		spec.RevisionHistoryLimit = ptr.To(int32(DefaultRevisionHistoryLimitValue))
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
