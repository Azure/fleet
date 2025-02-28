/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

// ClusterResourcePlacementStatus condition reasons
const (
	// InvalidResourceSelectorsReason is the reason string of placement condition when the selected resources are invalid
	// or forbidden.
	InvalidResourceSelectorsReason = "InvalidResourceSelectors"
	// SchedulingUnknownReason is the reason string of placement condition when the schedule status is unknown.
	SchedulingUnknownReason = "SchedulePending"

	// ApplyFailedReason is the reason string of placement condition when the selected resources fail to apply.
	ApplyFailedReason = "ApplyFailed"
	// ApplyPendingReason is the reason string of placement condition when the selected resources are pending to apply.
	ApplyPendingReason = "ApplyPending"
	// ApplySucceededReason is the reason string of placement condition when the selected resources are applied successfully.
	ApplySucceededReason = "ApplySucceeded"
)

// ResourcePlacementStatus condition reasons and message formats
const (
	// ResourceScheduleSucceededReason is the reason string of placement condition when the selected resources are scheduled.
	ResourceScheduleSucceededReason = "ScheduleSucceeded"
	// ResourceScheduleFailedReason is the reason string of placement condition when the scheduler failed to schedule the selected resources.
	ResourceScheduleFailedReason = "ScheduleFailed"
)

// calculateFailedToScheduleClusterCount calculates the count of failed to schedule clusters based on the scheduling policy.
func calculateFailedToScheduleClusterCount(crp *fleetv1beta1.ClusterResourcePlacement, selected, unselected []*fleetv1beta1.ClusterDecision) int {
	failedToScheduleClusterCount := 0
	switch {
	case crp.Spec.Policy == nil:
		// No scheduling policy is set; Fleet assumes that a PickAll scheduling policy
		// is specified and in this case there is no need to calculate the count of
		// failed to schedule clusters as the scheduler will always set all eligible clusters.
	case crp.Spec.Policy.PlacementType == fleetv1beta1.PickNPlacementType && crp.Spec.Policy.NumberOfClusters != nil:
		// The PickN scheduling policy is used; in this case the count of failed to schedule
		// clusters is equal to the difference between the specified N number and the actual
		// number of selected clusters.
		failedToScheduleClusterCount = int(*crp.Spec.Policy.NumberOfClusters) - len(selected)
	case crp.Spec.Policy.PlacementType == fleetv1beta1.PickFixedPlacementType:
		// The PickFixed scheduling policy is used; in this case the count of failed to schedule
		// clusters is equal to the difference between the number of specified cluster names and
		// the actual number of selected clusters.
		failedToScheduleClusterCount = len(crp.Spec.Policy.ClusterNames) - len(selected)
	default:
		// The PickAll scheduling policy is used; as explained earlier, in this case there is
		// no need to calculate the count of failed to schedule clusters.
	}

	if failedToScheduleClusterCount > len(unselected) {
		// There exists a corner case where the failed to schedule cluster count exceeds the
		// total number of unselected clusters; this can occur when there does not exist
		// enough clusters in the fleet for the scheduler to pick. In this case, the count is
		// set to the total number of unselected clusters.
		failedToScheduleClusterCount = len(unselected)
	}

	return failedToScheduleClusterCount
}

// appendFailedToScheduleResourcePlacementStatuses appends the resource placement statuses for
// the failed to schedule clusters to the list of all resource placement statuses.
func appendFailedToScheduleResourcePlacementStatuses(
	allRPS []fleetv1beta1.ResourcePlacementStatus,
	unselected []*fleetv1beta1.ClusterDecision,
	failedToScheduleClusterCount int,
	crp *fleetv1beta1.ClusterResourcePlacement,
) []fleetv1beta1.ResourcePlacementStatus {
	// In the earlier step it has been guaranteed that failedToScheduleClusterCount is less than or equal to the
	// total number of unselected clusters; here Fleet still performs a sanity check.
	for i := 0; i < failedToScheduleClusterCount && i < len(unselected); i++ {
		rps := &fleetv1beta1.ResourcePlacementStatus{}

		failedToScheduleCond := metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourceScheduledConditionType),
			Reason:             ResourceScheduleFailedReason,
			Message:            unselected[i].Reason,
			ObservedGeneration: crp.Generation,
		}
		meta.SetStatusCondition(&rps.Conditions, failedToScheduleCond)
		// The allRPS slice has been pre-allocated, so the append call will never produce a new
		// slice; here, however, Fleet will still return the old slice just in case.
		allRPS = append(allRPS, *rps)
		klog.V(2).InfoS("Populated the resource placement status for the unscheduled cluster", "clusterResourcePlacement", klog.KObj(crp), "cluster", unselected[i].ClusterName)
	}

	return allRPS
}

// determineExpectedCRPAndResourcePlacementStatusCondType determines the expected condition types for the CRP and resource placement statuses
// given the currently in-use apply strategy.
func determineExpectedCRPAndResourcePlacementStatusCondType(crp *fleetv1beta1.ClusterResourcePlacement) []condition.ResourceCondition {
	switch {
	case crp.Spec.Strategy.ApplyStrategy == nil:
		return condition.CondTypesForClientSideServerSideApplyStrategies
	case crp.Spec.Strategy.ApplyStrategy.Type == fleetv1beta1.ApplyStrategyTypeReportDiff:
		return condition.CondTypesForReportDiffApplyStrategy
	default:
		return condition.CondTypesForClientSideServerSideApplyStrategies
	}
}

// appendScheduledResourcePlacementStatuses appends the resource placement statuses for the
// scheduled clusters to the list of all resource placement statuses.
func (r *Reconciler) appendScheduledResourcePlacementStatuses(
	ctx context.Context,
	allRPS []fleetv1beta1.ResourcePlacementStatus,
	selected []*fleetv1beta1.ClusterDecision,
	expectedCondTypes []condition.ResourceCondition,
	crp *fleetv1beta1.ClusterResourcePlacement,
	latestSchedulingPolicySnapshot *fleetv1beta1.ClusterSchedulingPolicySnapshot,
	latestClusterResourceSnapshot *fleetv1beta1.ClusterResourceSnapshot,
) (
	[]fleetv1beta1.ResourcePlacementStatus,
	[condition.TotalCondition][condition.TotalConditionStatus]int,
	error,
) {
	// Use a counter to track the number of each condition type set and their respective status.
	var rpsSetCondTypeCounter [condition.TotalCondition][condition.TotalConditionStatus]int

	oldRPSMap := buildResourcePlacementStatusMap(crp)
	bindingMap, err := r.buildClusterResourceBindings(ctx, crp, latestSchedulingPolicySnapshot)
	if err != nil {
		return allRPS, rpsSetCondTypeCounter, err
	}

	for idx := range selected {
		clusterDecision := selected[idx]
		rps := &fleetv1beta1.ResourcePlacementStatus{}

		// Port back the old conditions.
		// This is necessary for Fleet to track the last transition times correctly.
		if oldConds, ok := oldRPSMap[clusterDecision.ClusterName]; ok {
			rps.Conditions = oldConds
		}

		// Set the scheduled condition.
		scheduledCondition := metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourceScheduledConditionType),
			Reason:             condition.ScheduleSucceededReason,
			Message:            clusterDecision.Reason,
			ObservedGeneration: crp.Generation,
		}
		meta.SetStatusCondition(&rps.Conditions, scheduledCondition)

		// Set the cluster name.
		rps.ClusterName = clusterDecision.ClusterName

		// Prepare the new conditions.
		binding := bindingMap[clusterDecision.ClusterName]
		setStatusByCondType := r.setResourcePlacementStatusPerCluster(crp, latestClusterResourceSnapshot, binding, rps, expectedCondTypes)

		// Update the counter.
		for condType, condStatus := range setStatusByCondType {
			switch condStatus {
			case metav1.ConditionTrue:
				rpsSetCondTypeCounter[condType][condition.TrueConditionStatus]++
			case metav1.ConditionFalse:
				rpsSetCondTypeCounter[condType][condition.FalseConditionStatus]++
			case metav1.ConditionUnknown:
				rpsSetCondTypeCounter[condType][condition.UnknownConditionStatus]++
			}
		}

		// CRP status will refresh even if the spec has not changed. To avoid confusion,
		// Fleet will reset unused conditions.
		for i := condition.RolloutStartedCondition; i < condition.TotalCondition; i++ {
			if _, ok := setStatusByCondType[i]; !ok {
				meta.RemoveStatusCondition(&rps.Conditions, string(i.ResourcePlacementConditionType()))
			}
		}
		// The allRPS slice has been pre-allocated, so the append call will never produce a new
		// slice; here, however, Fleet will still return the old slice just in case.
		allRPS = append(allRPS, *rps)
		klog.V(2).InfoS("Populated the resource placement status for the scheduled cluster", "clusterResourcePlacement", klog.KObj(crp), "cluster", clusterDecision.ClusterName, "resourcePlacementStatus", rps)
	}

	return allRPS, rpsSetCondTypeCounter, nil
}

// setCRPConditions sets the CRP conditions based on the resource placement statuses.
func setCRPConditions(
	crp *fleetv1beta1.ClusterResourcePlacement,
	allRPS []fleetv1beta1.ResourcePlacementStatus,
	rpsSetCondTypeCounter [condition.TotalCondition][condition.TotalConditionStatus]int,
	expectedCondTypes []condition.ResourceCondition,
) {
	// Track all the condition types that have been set.
	setCondTypes := make(map[condition.ResourceCondition]interface{})

	for _, i := range expectedCondTypes {
		setCondTypes[i] = nil
		// If any given condition type is set to False or Unknown, Fleet will skip evaluation of the
		// rest conditions.
		shouldSkipRestCondTypes := false
		switch {
		case rpsSetCondTypeCounter[i][condition.UnknownConditionStatus] > 0:
			// There is at least one Unknown condition of the given type being set on the per cluster placement statuses.
			crp.SetConditions(i.UnknownClusterResourcePlacementCondition(crp.Generation, rpsSetCondTypeCounter[i][condition.UnknownConditionStatus]))
			shouldSkipRestCondTypes = true
		case rpsSetCondTypeCounter[i][condition.FalseConditionStatus] > 0:
			// There is at least one False condition of the given type being set on the per cluster placement statuses.
			crp.SetConditions(i.FalseClusterResourcePlacementCondition(crp.Generation, rpsSetCondTypeCounter[i][condition.FalseConditionStatus]))
			shouldSkipRestCondTypes = true
		default:
			// All the conditions of the given type are True.
			cond := i.TrueClusterResourcePlacementCondition(crp.Generation, rpsSetCondTypeCounter[i][condition.TrueConditionStatus])
			if i == condition.OverriddenCondition {
				hasOverride := false
				for _, status := range allRPS {
					if len(status.ApplicableResourceOverrides) > 0 || len(status.ApplicableClusterResourceOverrides) > 0 {
						hasOverride = true
						break
					}
				}
				if !hasOverride {
					cond.Reason = condition.OverrideNotSpecifiedReason
					cond.Message = "No override rules are configured for the selected resources"
				}
			}
			crp.SetConditions(cond)
		}

		if shouldSkipRestCondTypes {
			break
		}
	}

	// As CRP status will refresh even if the spec has not changed, Fleet will reset any unused conditions
	// to avoid confusion.
	for i := condition.RolloutStartedCondition; i < condition.TotalCondition; i++ {
		if _, ok := setCondTypes[i]; !ok {
			meta.RemoveStatusCondition(&crp.Status.Conditions, string(i.ClusterResourcePlacementConditionType()))
		}
	}

	klog.V(2).InfoS("Populated the placement conditions", "clusterResourcePlacement", klog.KObj(crp))
}

func (r *Reconciler) buildClusterResourceBindings(ctx context.Context, crp *fleetv1beta1.ClusterResourcePlacement, latestSchedulingPolicySnapshot *fleetv1beta1.ClusterSchedulingPolicySnapshot) (map[string]*fleetv1beta1.ClusterResourceBinding, error) {
	// List all bindings derived from the CRP.
	bindingList := &fleetv1beta1.ClusterResourceBindingList{}
	listOptions := client.MatchingLabels{
		fleetv1beta1.CRPTrackingLabel: crp.Name,
	}
	crpKObj := klog.KObj(crp)
	if err := r.Client.List(ctx, bindingList, listOptions); err != nil {
		klog.ErrorS(err, "Failed to list all bindings", "clusterResourcePlacement", crpKObj)
		return nil, controller.NewAPIServerError(true, err)
	}

	res := make(map[string]*fleetv1beta1.ClusterResourceBinding, len(bindingList.Items))
	bindings := bindingList.Items
	// filter out the latest resource bindings
	for i := range bindings {
		if !bindings[i].DeletionTimestamp.IsZero() {
			klog.V(2).InfoS("Filtering out the deleting clusterResourceBinding", "clusterResourceBinding", klog.KObj(&bindings[i]))
			continue
		}

		if len(bindings[i].Spec.TargetCluster) == 0 {
			err := fmt.Errorf("targetCluster is empty on clusterResourceBinding %s", bindings[i].Name)
			klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "Found an invalid clusterResourceBinding and skipping it when building placement status", "clusterResourceBinding", klog.KObj(&bindings[i]), "clusterResourcePlacement", crpKObj)
			continue
		}

		// We don't check the bindings[i].Spec.ResourceSnapshotName != latestResourceSnapshot.Name here.
		// The existing conditions are needed when building the new ones.
		if bindings[i].Spec.SchedulingPolicySnapshotName != latestSchedulingPolicySnapshot.Name {
			continue
		}
		res[bindings[i].Spec.TargetCluster] = &bindings[i]
	}
	return res, nil
}

// setResourcePlacementStatusPerCluster sets the resource related fields for each cluster.
// It returns a map which tracks the set status for each relevant condition type.
func (r *Reconciler) setResourcePlacementStatusPerCluster(
	crp *fleetv1beta1.ClusterResourcePlacement,
	latestResourceSnapshot *fleetv1beta1.ClusterResourceSnapshot,
	binding *fleetv1beta1.ClusterResourceBinding,
	status *fleetv1beta1.ResourcePlacementStatus,
	expectedCondTypes []condition.ResourceCondition,
) map[condition.ResourceCondition]metav1.ConditionStatus {
	res := make(map[condition.ResourceCondition]metav1.ConditionStatus)
	if binding == nil {
		// The binding cannot be found; Fleet might be observing an in-between state where
		// the cluster has been picked but the binding has not been created yet.
		meta.SetStatusCondition(&status.Conditions, condition.RolloutStartedCondition.UnknownResourceConditionPerCluster(crp.Generation))
		res[condition.RolloutStartedCondition] = metav1.ConditionUnknown
		return res
	}

	rolloutStartedCond := binding.GetCondition(string(condition.RolloutStartedCondition.ResourceBindingConditionType()))
	switch {
	case binding.Spec.ResourceSnapshotName != latestResourceSnapshot.Name && condition.IsConditionStatusFalse(rolloutStartedCond, binding.Generation):
		// The binding uses an out of date resource snapshot and rollout controller has reported
		// that the rollout is being blocked (the RolloutStarted condition is of the False status).
		cond := metav1.Condition{
			Type:               string(condition.RolloutStartedCondition.ResourcePlacementConditionType()),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: crp.Generation,
			Reason:             condition.RolloutNotStartedYetReason,
			Message:            "The rollout is being blocked by the rollout strategy",
		}
		meta.SetStatusCondition(&status.Conditions, cond)
		res[condition.RolloutStartedCondition] = metav1.ConditionFalse
		return res
	case binding.Spec.ResourceSnapshotName != latestResourceSnapshot.Name:
		// The binding uses an out of date resource snapshot, and the RolloutStarted condition is
		// set to True, Unknown, or has become stale. Fleet might be observing an in-between state.
		meta.SetStatusCondition(&status.Conditions, condition.RolloutStartedCondition.UnknownResourceConditionPerCluster(crp.Generation))
		klog.V(5).InfoS("The cluster resource binding has a stale RolloutStarted condition, or it links to an out of date resource snapshot yet has the RolloutStarted condition set to True or Unknown status", "clusterResourceBinding", klog.KObj(binding), "clusterResourcePlacement", klog.KObj(crp))
		res[condition.RolloutStartedCondition] = metav1.ConditionUnknown
		return res
	default:
		// The binding uses the latest resource snapshot.
		for _, i := range expectedCondTypes {
			bindingCond := binding.GetCondition(string(i.ResourceBindingConditionType()))
			if !condition.IsConditionStatusTrue(bindingCond, binding.Generation) &&
				!condition.IsConditionStatusFalse(bindingCond, binding.Generation) {
				meta.SetStatusCondition(&status.Conditions, i.UnknownResourceConditionPerCluster(crp.Generation))
				klog.V(5).InfoS("Find an unknown condition", "bindingCond", bindingCond, "clusterResourceBinding", klog.KObj(binding), "clusterResourcePlacement", klog.KObj(crp))
				res[i] = metav1.ConditionUnknown
				break
			}

			switch i {
			case condition.RolloutStartedCondition:
				if bindingCond.Status == metav1.ConditionTrue {
					status.ApplicableResourceOverrides = binding.Spec.ResourceOverrideSnapshots
					status.ApplicableClusterResourceOverrides = binding.Spec.ClusterResourceOverrideSnapshots
				}
			case condition.AppliedCondition, condition.AvailableCondition:
				if bindingCond.Status == metav1.ConditionFalse {
					status.FailedPlacements = binding.Status.FailedPlacements
					status.DiffedPlacements = binding.Status.DiffedPlacements
				}
				// Note that configuration drifts can occur whether the manifests are applied
				// successfully or not.
				status.DriftedPlacements = binding.Status.DriftedPlacements
			case condition.DiffReportedCondition:
				if bindingCond.Status == metav1.ConditionTrue {
					status.DiffedPlacements = binding.Status.DiffedPlacements
				}
			}

			cond := metav1.Condition{
				Type:               string(i.ResourcePlacementConditionType()),
				Status:             bindingCond.Status,
				ObservedGeneration: crp.Generation,
				Reason:             bindingCond.Reason,
				Message:            bindingCond.Message,
			}
			meta.SetStatusCondition(&status.Conditions, cond)
			res[i] = bindingCond.Status

			if bindingCond.Status == metav1.ConditionFalse {
				break // if the current condition is false, no need to populate the rest conditions
			}
		}
		return res
	}
}
