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

var (
	skippedCondTypesForCSASSAApplyStrategyTypes = map[condition.ResourceCondition]interface{}{
		condition.DiffReportedCondition: nil,
	}
	skippedCondTypesForReportDiffApplyStrategyType = map[condition.ResourceCondition]interface{}{
		condition.AppliedCondition:   nil,
		condition.AvailableCondition: nil,
	}
)

// Note (chenyu1): with the newly added DiffReported condition type, the status handling part
// probably needs some refactoring as it is triggering the code complexity linter (original
// cyclomatic complexity score 26, new score 31, threshold 30); the original
// assumption, where condition types can always be handled in sequential order, i.e., one failing
// condition type will exempt Fleet from processing all subsequent condition types, only partially
// stands now.

// setResourceConditions sets the resource related conditions by looking at the bindings and work, excluding the scheduled condition.
// It returns whether there is a cluster scheduled or not.
//
// nolint: gocyclo
func (r *Reconciler) setResourceConditions(ctx context.Context, crp *fleetv1beta1.ClusterResourcePlacement,
	latestSchedulingPolicySnapshot *fleetv1beta1.ClusterSchedulingPolicySnapshot, latestResourceSnapshot *fleetv1beta1.ClusterResourceSnapshot) (bool, error) {
	placementStatuses := make([]fleetv1beta1.ResourcePlacementStatus, 0, len(latestSchedulingPolicySnapshot.Status.ClusterDecisions))
	decisions := latestSchedulingPolicySnapshot.Status.ClusterDecisions
	selected, unselected := classifyClusterDecisions(decisions)

	// In the pickN case, if the placement cannot be satisfied. For example, pickN deployment requires 5 clusters and
	// scheduler schedules the resources on 3 clusters. We'll populate why the other two cannot be scheduled.
	// Here it is calculating how many unscheduled resources there are.
	unscheduledClusterCount := 0
	if crp.Spec.Policy != nil {
		if crp.Spec.Policy.PlacementType == fleetv1beta1.PickNPlacementType && crp.Spec.Policy.NumberOfClusters != nil {
			unscheduledClusterCount = int(*crp.Spec.Policy.NumberOfClusters) - len(selected)
		}
		if crp.Spec.Policy.PlacementType == fleetv1beta1.PickFixedPlacementType {
			unscheduledClusterCount = len(crp.Spec.Policy.ClusterNames) - len(selected)
		}
	}

	oldResourcePlacementStatusMap := buildResourcePlacementStatusMap(crp)
	resourceBindingMap, err := r.buildClusterResourceBindings(ctx, crp, latestSchedulingPolicySnapshot)
	if err != nil {
		return false, err
	}

	// record the total count per status for each condition
	var clusterConditionStatusRes [condition.TotalCondition][condition.TotalConditionStatus]int

	for _, c := range selected {
		var rps fleetv1beta1.ResourcePlacementStatus
		scheduledCondition := metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourceScheduledConditionType),
			Reason:             condition.ScheduleSucceededReason,
			Message:            c.Reason,
			ObservedGeneration: crp.Generation,
		}
		rps.ClusterName = c.ClusterName
		oldConditions, ok := oldResourcePlacementStatusMap[c.ClusterName]
		if ok {
			// update the lastTransitionTime considering the existing condition status instead of overwriting
			rps.Conditions = oldConditions
		}
		meta.SetStatusCondition(&rps.Conditions, scheduledCondition)
		res, err := r.setResourcePlacementStatusPerCluster(crp, latestResourceSnapshot, resourceBindingMap[c.ClusterName], &rps)
		if err != nil {
			return false, err
		}
		for condType, condStatus := range res {
			switch condStatus {
			case metav1.ConditionTrue:
				clusterConditionStatusRes[condType][condition.TrueConditionStatus]++
			case metav1.ConditionFalse:
				clusterConditionStatusRes[condType][condition.FalseConditionStatus]++
			case metav1.ConditionUnknown:
				clusterConditionStatusRes[condType][condition.UnknownConditionStatus]++
			}
		}
		// The resources can be changed without updating the crp spec.
		// To reflect the latest resource conditions, we reset conditions that are no longer relevant.
		for i := condition.RolloutStartedCondition; i < condition.TotalCondition; i++ {
			if _, ok := res[i]; !ok {
				meta.RemoveStatusCondition(&rps.Conditions, string(i.ResourcePlacementConditionType()))
			}
		}
		placementStatuses = append(placementStatuses, rps)
		klog.V(2).InfoS("Populated the resource placement status for the scheduled cluster", "clusterResourcePlacement", klog.KObj(crp), "cluster", c.ClusterName, "resourcePlacementStatus", rps)
	}
	isClusterScheduled := len(placementStatuses) > 0

	for i := 0; i < unscheduledClusterCount && i < len(unselected); i++ {
		// TODO: we could improve the message by summarizing the failure reasons from all of the unselected clusters.
		// For now, it starts from adding some sample failures of unselected clusters.
		var rp fleetv1beta1.ResourcePlacementStatus
		scheduledCondition := metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourceScheduledConditionType),
			Reason:             ResourceScheduleFailedReason,
			Message:            unselected[i].Reason,
			ObservedGeneration: crp.Generation,
		}

		meta.SetStatusCondition(&rp.Conditions, scheduledCondition)
		placementStatuses = append(placementStatuses, rp)
		klog.V(2).InfoS("Populated the resource placement status for the unscheduled cluster", "clusterResourcePlacement", klog.KObj(crp), "cluster", unselected[i].ClusterName)
	}
	crp.Status.PlacementStatuses = placementStatuses

	if !isClusterScheduled {
		// It covers one special case: CRP selects a cluster which joins (resource are applied) and then leaves.
		// In this case, CRP generation has not been changed.
		// And we cannot rely on the generation to filter out the stale conditions.
		// But the resource related conditions are set before. So that, we reset them.
		crp.Status.Conditions = []metav1.Condition{*crp.GetCondition(string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType))}
		return false, nil
	}

	// If the CRP has an apply strategy of the ClientSideApply or ServerSideApply type, skip
	// any updates to the DiffReported condition type; similarly, should the apply strategy be of
	// the ReportDiff type, Fleet will skip any updates to the Applied and Available condition
	// type.
	skippedCondTypes := skippedCondTypesForCSASSAApplyStrategyTypes
	if crp.Spec.Strategy.ApplyStrategy != nil && crp.Spec.Strategy.ApplyStrategy.Type == fleetv1beta1.ApplyStrategyTypeReportDiff {
		skippedCondTypes = skippedCondTypesForReportDiffApplyStrategyType
	}

	// Track all the condition types that have been set.
	setCondTypes := make(map[condition.ResourceCondition]interface{})
	for i := condition.RolloutStartedCondition; i < condition.TotalCondition; i++ {
		if _, ok := skippedCondTypes[i]; ok {
			// If the Applied and Available condition types are skipped (as the CRP uses
			// the ReportDiff apply strategy), proceed to evaluate the DiffReported condition
			// type.
			continue
		}

		setCondTypes[i] = nil
		if clusterConditionStatusRes[i][condition.UnknownConditionStatus] > 0 {
			crp.SetConditions(i.UnknownClusterResourcePlacementCondition(crp.Generation, clusterConditionStatusRes[i][condition.UnknownConditionStatus]))
			break
		} else if clusterConditionStatusRes[i][condition.FalseConditionStatus] > 0 {
			crp.SetConditions(i.FalseClusterResourcePlacementCondition(crp.Generation, clusterConditionStatusRes[i][condition.FalseConditionStatus]))
			break
		} else {
			cond := i.TrueClusterResourcePlacementCondition(crp.Generation, clusterConditionStatusRes[i][condition.TrueConditionStatus])
			if i == condition.OverriddenCondition {
				hasOverride := false
				for _, status := range placementStatuses {
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
	}
	// reset the remaining conditions, starting from the next one
	for i := condition.RolloutStartedCondition; i < condition.TotalCondition; i++ {
		// The resources can be changed without updating the crp spec.
		// To reflect the latest resource conditions, we reset the renaming conditions.
		if _, ok := setCondTypes[i]; !ok {
			meta.RemoveStatusCondition(&crp.Status.Conditions, string(i.ClusterResourcePlacementConditionType()))
		}
	}
	klog.V(2).InfoS("Populated the placement conditions", "clusterResourcePlacement", klog.KObj(crp))

	return true, nil
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
// It returns an array which records the status for each resource condition.
// The resource condition order (index) is defined as const:
// const (
//
//		RolloutStartedCondition resourceCondition = iota
//		OverriddenCondition
//		WorkSynchronizedCondition
//		AppliedCondition
//		AvailableCondition
//	    DiffReportedCondition
//		TotalCondition
//
// )
func (r *Reconciler) setResourcePlacementStatusPerCluster(crp *fleetv1beta1.ClusterResourcePlacement, latestResourceSnapshot *fleetv1beta1.ClusterResourceSnapshot,
	binding *fleetv1beta1.ClusterResourceBinding, status *fleetv1beta1.ResourcePlacementStatus) (map[condition.ResourceCondition]metav1.ConditionStatus, error) {
	res := make(map[condition.ResourceCondition]metav1.ConditionStatus)
	if binding == nil {
		meta.SetStatusCondition(&status.Conditions, condition.RolloutStartedCondition.UnknownResourceConditionPerCluster(crp.Generation))
		res[condition.RolloutStartedCondition] = metav1.ConditionUnknown
		return res, nil
	}

	// If the CRP has an apply strategy of the ClientSideApply or ServerSideApply type, skip
	// any updates to the DiffReported condition type; similarly, should the apply strategy be of
	// the ReportDiff type, Fleet will skip any updates to the Applied and Available condition
	// type.
	skippedCondTypes := skippedCondTypesForCSASSAApplyStrategyTypes
	if crp.Spec.Strategy.ApplyStrategy != nil && crp.Spec.Strategy.ApplyStrategy.Type == fleetv1beta1.ApplyStrategyTypeReportDiff {
		skippedCondTypes = skippedCondTypesForReportDiffApplyStrategyType
	}

	// There are few cases:
	// * if the resourceSnapshotName is not equal,
	//     1. the status is false, it means the rollout is stuck.
	//     2. otherwise, the rollout controller has not processed it yet.
	// * if the resourceSnapshotName is equal,
	//     just return the corresponding status.
	if binding.Spec.ResourceSnapshotName == latestResourceSnapshot.Name {
		for i := condition.RolloutStartedCondition; i < condition.TotalCondition; i++ {
			if _, ok := skippedCondTypes[i]; ok {
				// If the Applied and Available condition types are skipped (as the CRP uses
				// the ReportDiff apply strategy), proceed to evaluate the DiffReported condition
				// type.
				continue
			}

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
		return res, nil
	}
	// handling stale binding if binding.Spec.ResourceSnapshotName != latestResourceSnapshot.Name
	rolloutStartedCond := binding.GetCondition(string(condition.RolloutStartedCondition.ResourceBindingConditionType()))
	if condition.IsConditionStatusFalse(rolloutStartedCond, binding.Generation) {
		cond := metav1.Condition{
			Type:               string(condition.RolloutStartedCondition.ResourcePlacementConditionType()),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: crp.Generation,
			Reason:             condition.RolloutNotStartedYetReason,
			Message:            "The rollout is being blocked by the rollout strategy",
		}
		meta.SetStatusCondition(&status.Conditions, cond)
		res[condition.RolloutStartedCondition] = metav1.ConditionFalse
		return res, nil
	}
	// At this point, either the generation is not the one in the binding spec or the status is true/unknown.
	// It means the rollout controller has not handled the binding yet.
	meta.SetStatusCondition(&status.Conditions, condition.RolloutStartedCondition.UnknownResourceConditionPerCluster(crp.Generation))
	klog.V(5).InfoS("The staled binding rollout status is unknown", "clusterResourceBinding", klog.KObj(binding), "clusterResourcePlacement", klog.KObj(crp))
	res[condition.RolloutStartedCondition] = metav1.ConditionUnknown
	return res, nil
}
