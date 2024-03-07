/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
)

// resourceCondition is all the resource related condition, for example, scheduled condition is not included.
type resourceCondition int

// The following conditions are in ordered.
// Once the placement is scheduled, it will be divided into following stages.
// Used to populate the CRP conditions.
const (
	RolloutStartedCondition resourceCondition = iota
	OverriddenCondition
	WorkCreatedCondition
	AppliedCondition
	AvailableCondition
	TotalCondition
)

// ResourcePlacementConditionType returns the resource condition type per cluster used by cluster resource placement.
func (c resourceCondition) ResourcePlacementConditionType() fleetv1beta1.ResourcePlacementConditionType {
	return []fleetv1beta1.ResourcePlacementConditionType{
		fleetv1beta1.ResourceRolloutStartedConditionType,
		fleetv1beta1.ResourceOverriddenConditionType,
		fleetv1beta1.ResourceWorkCreatedConditionType,
		fleetv1beta1.ResourcesAppliedConditionType,
		fleetv1beta1.ResourcesAvailableConditionType,
	}[c]
}

// ResourceBindingConditionType returns the binding condition type used by cluster resource binding.
func (c resourceCondition) ResourceBindingConditionType() fleetv1beta1.ResourceBindingConditionType {
	return []fleetv1beta1.ResourceBindingConditionType{
		fleetv1beta1.ResourceBindingRolloutStarted,
		fleetv1beta1.ResourceBindingOverridden,
		fleetv1beta1.ResourceBindingWorkCreated,
		fleetv1beta1.ResourceBindingApplied,
		fleetv1beta1.ResourceBindingAvailable,
	}[c]
}

// UnknownResourceConditionPerCluster returns the unknown resource condition.
func (c resourceCondition) UnknownResourceConditionPerCluster(generation int64) metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
			Reason:             condition.RolloutStartedUnknownReason,
			Message:            "In the process of deciding whether to rolling out the latest resources or not",
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
			Reason:             condition.OverriddenPendingReason,
			Message:            "In the process of overriding the selected resources if there is any override defined",
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourceWorkCreatedConditionType),
			Reason:             condition.WorkCreatedUnknownReason,
			Message:            "In the process of creating or updating the work object(s) in the hub cluster",
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
			Reason:             condition.ApplyPendingReason,
			Message:            "In the process of applying the resources on the member cluster",
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourcesAvailableConditionType),
			Reason:             condition.AvailableUnknownReason,
			Message:            "The availability of the selected resources is unknown yet ",
			ObservedGeneration: generation,
		},
	}[c]
}

// UnknownClusterResourcePlacementCondition returns the unknown cluster resource placement condition.
func (c resourceCondition) UnknownClusterResourcePlacementCondition(generation int64, clusterCount int) metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
			Reason:             condition.RolloutStartedUnknownReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of deciding whether to rolling out the latest resources or not", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
			Reason:             condition.OverriddenPendingReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of overriding the selected resources if there is any override defined", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ClusterResourcePlacementWorkCreatedConditionType),
			Reason:             condition.WorkCreatedUnknownReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of creating or updating the work object(s) in the hub cluster", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
			Reason:             condition.ApplyPendingReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of applying the resources on the member cluster", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
			Reason:             condition.AvailableUnknownReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) in the process of checking the availability of the selected resources", clusterCount),
			ObservedGeneration: generation,
		},
	}[c]
}

// FalseClusterResourcePlacementCondition returns the false cluster resource placement condition.
func (c resourceCondition) FalseClusterResourcePlacementCondition(generation int64, clusterCount int) metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
			Reason:             condition.RolloutNotStartedYetReason,
			Message:            fmt.Sprintf("The rollout is being blocked by the rollout strategy in %d cluster(s)", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
			Reason:             condition.OverriddenFailedReason,
			Message:            fmt.Sprintf("Failed to override resources in %d clusters, please check the `failedPlacements` status", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ClusterResourcePlacementWorkCreatedConditionType),
			Reason:             condition.WorkNotCreatedYetReason,
			Message:            fmt.Sprintf("There are %d cluster(s) which have not finished creating or updating work(s) yet", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
			Reason:             condition.ApplyFailedReason,
			Message:            fmt.Sprintf("Failed to apply resources to %d clusters, please check the `failedPlacements` status", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
			Reason:             condition.NotAvailableYetReason,
			Message:            fmt.Sprintf("The selected resources in %d cluster are still not available yet", clusterCount),
			ObservedGeneration: generation,
		},
	}[c]
}

// TrueClusterResourcePlacementCondition returns the true cluster resource placement condition.
func (c resourceCondition) TrueClusterResourcePlacementCondition(generation int64, clusterCount int) metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
			Reason:             condition.RolloutStartedReason,
			Message:            fmt.Sprintf("All %d cluster(s) start rolling out the latest resource", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
			Reason:             condition.OverriddenSucceededReason,
			Message:            fmt.Sprintf("The selected resources are successfully overridden in the %d clusters", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementWorkCreatedConditionType),
			Reason:             condition.WorkCreatedReason,
			Message:            fmt.Sprintf("Works(s) are succcesfully created or updated in the %d target clusters' namespaces", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
			Reason:             condition.ApplySucceededReason,
			Message:            fmt.Sprintf("The selected resources are successfully applied to %d clusters", clusterCount),
			ObservedGeneration: generation,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
			Reason:             condition.AvailableReason,
			Message:            fmt.Sprintf("The selected resources in %d cluster are available now", clusterCount),
			ObservedGeneration: generation,
		},
	}[c]
}

type conditionStatus int // internal type to be used when populating the CRP status

const (
	UnknownConditionStatus conditionStatus = iota
	FalseConditionStatus
	TrueConditionStatus
	TotalConditionStatus
)
