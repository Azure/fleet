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

package clusterresourceplacement

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// calculateFailedToScheduleClusterCount calculates the count of failed to schedule clusters based on the scheduling policy.
func calculateFailedToScheduleClusterCount(placementObj fleetv1beta1.PlacementObj, selected, unselected []*fleetv1beta1.ClusterDecision) (int, error) {
	failedToScheduleClusterCount := 0
	placementSpec := placementObj.GetPlacementSpec()
	switch {
	case placementSpec.Policy == nil:
		// No scheduling policy is set; Fleet assumes that a PickAll scheduling policy
		// is specified and in this case there is no need to calculate the count of
		// failed to schedule clusters as the scheduler will always set all eligible clusters.
	case placementSpec.Policy.PlacementType == fleetv1beta1.PickNPlacementType && placementSpec.Policy.NumberOfClusters != nil:
		// The PickN scheduling policy is used; in this case the count of failed to schedule
		// clusters is equal to the difference between the specified N number and the actual
		// number of selected clusters.
		failedToScheduleClusterCount = int(*placementSpec.Policy.NumberOfClusters) - len(selected)
	case placementSpec.Policy.PlacementType == fleetv1beta1.PickFixedPlacementType:
		// The PickFixed scheduling policy is used; in this case the count of failed to schedule
		// clusters is equal to the difference between the number of specified cluster names and
		// the actual number of selected clusters.
		failedToScheduleClusterCount = len(placementSpec.Policy.ClusterNames) - len(selected)
	default:
		// The PickAll scheduling policy is used; as explained earlier, in this case there is
		// no need to calculate the count of failed to schedule clusters.
	}

	if failedToScheduleClusterCount < 0 {
		// This should not happen in normal circumstances as the scheduler should not select
		// more clusters than requested. If it does happen, it indicates a bug.
		err := fmt.Errorf("calculated negative failed to schedule cluster count: %d", failedToScheduleClusterCount)
		klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "Invalid failed to schedule cluster count",
			"placement", klog.KObj(placementObj), "selectedCount", len(selected), "unselectedCount", len(unselected))
		return 0, controller.NewUnexpectedBehaviorError(err)
	}

	if failedToScheduleClusterCount > len(unselected) {
		// There exists a corner case where the failed to schedule cluster count exceeds the
		// total number of unselected clusters; this can occur when there does not exist
		// enough clusters in the fleet for the scheduler to pick. In this case, the count is
		// set to the total number of unselected clusters.
		failedToScheduleClusterCount = len(unselected)
	}

	return failedToScheduleClusterCount, nil
}

// buildFailedToSchedulePerClusterPlacementStatuses appends the resource placement statuses for
// the failed to schedule clusters to the list of all resource placement statuses.
func buildFailedToSchedulePerClusterPlacementStatuses(
	unselected []*fleetv1beta1.ClusterDecision,
	failedToScheduleClusterCount int,
	placementObj fleetv1beta1.PlacementObj,
) []fleetv1beta1.PerClusterPlacementStatus {
	perClusterStatuses := make([]fleetv1beta1.PerClusterPlacementStatus, 0, failedToScheduleClusterCount)
	// In the earlier step it has been guaranteed that failedToScheduleClusterCount is less than or equal to the
	// total number of unselected clusters; here Fleet still performs a sanity check.
	for i := 0; i < failedToScheduleClusterCount && i < len(unselected); i++ {
		perClusterStatus := fleetv1beta1.PerClusterPlacementStatus{}
		failedToScheduleCond := metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
			Reason:             condition.ResourceScheduleFailedReason,
			Message:            unselected[i].Reason,
			ObservedGeneration: placementObj.GetGeneration(),
		}
		meta.SetStatusCondition(&perClusterStatus.Conditions, failedToScheduleCond)
		perClusterStatuses = append(perClusterStatuses, perClusterStatus)
		klog.V(2).InfoS("Populated the resource placement status for the unscheduled cluster", "placement", klog.KObj(placementObj), "cluster", unselected[i].ClusterName)
	}

	return perClusterStatuses
}

// determineExpectedPlacementAndResourcePlacementStatusCondType determines the expected condition types for the CRP and resource placement statuses
// given the currently in-use apply strategy.
func determineExpectedPlacementAndResourcePlacementStatusCondType(placementObj fleetv1beta1.PlacementObj) []condition.ResourceCondition {
	placementSpec := placementObj.GetPlacementSpec()
	switch {
	case placementSpec.Strategy.ApplyStrategy == nil:
		return condition.CondTypesForApplyStrategies
	case placementSpec.Strategy.ApplyStrategy.Type == fleetv1beta1.ApplyStrategyTypeReportDiff:
		return condition.CondTypesForReportDiffApplyStrategy
	default:
		return condition.CondTypesForApplyStrategies
	}
}

// appendSelecteddPerClusterPlacementStatuses appends the per cluster placement statuses for the
// scheduled clusters to the list of all per cluster placement statuses.
// it returns the updated list of per cluster placement statuses.
func (r *Reconciler) buildSelectedPerClusterPlacementStatuses(
	ctx context.Context,
	selected []*fleetv1beta1.ClusterDecision,
	expectedCondTypes []condition.ResourceCondition,
	placementObj fleetv1beta1.PlacementObj,
	latestSchedulingPolicySnapshot fleetv1beta1.PolicySnapshotObj,
	latestResourceSnapshot fleetv1beta1.ResourceSnapshotObj,
) (
	[]fleetv1beta1.PerClusterPlacementStatus,
	[condition.TotalCondition][condition.TotalConditionStatus]int,
	error,
) {
	// Use a counter to track the number of each condition type set and their respective status.
	var perClusterCondTypeCounter [condition.TotalCondition][condition.TotalConditionStatus]int

	oldPerClusterStatusMap := buildPerClusterPlacementStatusMap(placementObj)
	clusterToBindingMap, err := r.buildClusterToBindingMap(ctx, placementObj, latestSchedulingPolicySnapshot)
	if err != nil {
		return nil, perClusterCondTypeCounter, err
	}

	resourceSnapshotIndexMap, err := r.findResourceSnapshotIndexForBindings(ctx, placementObj, clusterToBindingMap)
	if err != nil {
		return nil, perClusterCondTypeCounter, err
	}
	allPerClusterStatuses := make([]fleetv1beta1.PerClusterPlacementStatus, 0, len(latestSchedulingPolicySnapshot.GetPolicySnapshotStatus().ClusterDecisions))

	for idx := range selected {
		clusterDecision := selected[idx]
		perCluserStatus := fleetv1beta1.PerClusterPlacementStatus{}

		// Set the cluster name.
		perCluserStatus.ClusterName = clusterDecision.ClusterName

		// Port back the old conditions.
		// This is necessary for Fleet to track the last transition times correctly.
		if oldConds, ok := oldPerClusterStatusMap[clusterDecision.ClusterName]; ok {
			perCluserStatus.Conditions = oldConds
		}

		// Set the scheduled condition.
		scheduledCondition := metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
			Reason:             condition.ScheduleSucceededReason,
			Message:            clusterDecision.Reason,
			ObservedGeneration: placementObj.GetGeneration(),
		}
		meta.SetStatusCondition(&perCluserStatus.Conditions, scheduledCondition)

		// Prepare the new conditions.
		binding := clusterToBindingMap[clusterDecision.ClusterName]
		resourceSnapshotIndexOnBinding := resourceSnapshotIndexMap[clusterDecision.ClusterName]
		setStatusByCondType := r.setPerClusterPlacementStatus(placementObj, latestResourceSnapshot, resourceSnapshotIndexOnBinding, binding, &perCluserStatus, expectedCondTypes)

		// Update the counter.
		for condType, condStatus := range setStatusByCondType {
			switch condStatus {
			case metav1.ConditionTrue:
				perClusterCondTypeCounter[condType][condition.TrueConditionStatus]++
			case metav1.ConditionFalse:
				perClusterCondTypeCounter[condType][condition.FalseConditionStatus]++
			case metav1.ConditionUnknown:
				perClusterCondTypeCounter[condType][condition.UnknownConditionStatus]++
			}
		}

		// placement status will refresh even if the spec has not changed. To avoid confusion,
		// Fleet will reset unused conditions.
		for i := condition.RolloutStartedCondition; i < condition.TotalCondition; i++ {
			if _, ok := setStatusByCondType[i]; !ok {
				meta.RemoveStatusCondition(&perCluserStatus.Conditions, string(i.PerClusterPlacementConditionType()))
			}
		}
		// The allRPS slice has been pre-allocated, so the append call will never produce a new
		// slice; here, however, Fleet will still return the old slice just in case.
		allPerClusterStatuses = append(allPerClusterStatuses, perCluserStatus)
		klog.V(2).InfoS("Populated the resource placement status for the scheduled cluster", "placement", klog.KObj(placementObj), "cluster", clusterDecision.ClusterName, "resourcePlacementStatus", perCluserStatus)
	}

	return allPerClusterStatuses, perClusterCondTypeCounter, nil
}

// setPlacementConditions sets the CRP/RP conditions based on the resource placement statuses.
func setPlacementConditions(
	placementObj fleetv1beta1.PlacementObj,
	perClusterStatus []fleetv1beta1.PerClusterPlacementStatus,
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
			placementObj.SetConditions(generatePlacementConditionByStatus(placementObj, i, metav1.ConditionUnknown, placementObj.GetGeneration(), rpsSetCondTypeCounter[i][condition.UnknownConditionStatus]))
			shouldSkipRestCondTypes = true
		case rpsSetCondTypeCounter[i][condition.FalseConditionStatus] > 0:
			// There is at least one False condition of the given type being set on the per cluster placement statuses.
			placementObj.SetConditions(generatePlacementConditionByStatus(placementObj, i, metav1.ConditionFalse, placementObj.GetGeneration(), rpsSetCondTypeCounter[i][condition.FalseConditionStatus]))
			shouldSkipRestCondTypes = true
		default:
			// All the conditions of the given type are True.
			cond := generatePlacementConditionByStatus(placementObj, i, metav1.ConditionTrue, placementObj.GetGeneration(), rpsSetCondTypeCounter[i][condition.TrueConditionStatus])
			if i == condition.OverriddenCondition {
				hasOverride := false
				for _, status := range perClusterStatus {
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
			placementObj.SetConditions(cond)
		}

		if shouldSkipRestCondTypes {
			break
		}
	}

	// As CRP status will refresh even if the spec has not changed, Fleet will reset any unused conditions
	// to avoid confusion.
	for i := condition.RolloutStartedCondition; i < condition.TotalCondition; i++ {
		if _, ok := setCondTypes[i]; !ok {
			meta.RemoveStatusCondition(&placementObj.GetPlacementStatus().Conditions, getPlacementConditionType(placementObj, i))
		}
	}

	klog.V(2).InfoS("Populated the placement conditions", "placement", klog.KObj(placementObj))
}

// buildClusterToBindingMap builds a map of cluster to their corresponding binding map for CRP/RP placement objects.
func (r *Reconciler) buildClusterToBindingMap(ctx context.Context, placementObj fleetv1beta1.PlacementObj, latestSchedulingPolicySnapshot fleetv1beta1.PolicySnapshotObj) (map[string]fleetv1beta1.BindingObj, error) {
	placementKObj := klog.KObj(placementObj)
	// List all bindings for the placement object.
	bindings, err := controller.ListBindingsFromKey(ctx, r.Client, types.NamespacedName{Namespace: placementObj.GetNamespace(), Name: placementObj.GetName()})
	if err != nil {
		klog.ErrorS(err, "Failed to list bindings for placement", "placement", placementKObj)
		return nil, controller.NewAPIServerError(true, err)
	}
	res := make(map[string]fleetv1beta1.BindingObj, len(bindings))
	// filter out the latest resource bindings
	for i := range bindings {
		if !bindings[i].GetDeletionTimestamp().IsZero() {
			klog.V(2).InfoS("Filtering out the deleting clusterResourceBinding", "clusterResourceBinding", klog.KObj(bindings[i]))
			continue
		}

		if len(bindings[i].GetBindingSpec().TargetCluster) == 0 {
			err := fmt.Errorf("targetCluster is empty on clusterResourceBinding %s/%s", bindings[i].GetNamespace(), bindings[i].GetName())
			klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "Found an invalid clusterResourceBinding and skipping it when building placement status", "clusterResourceBinding", klog.KObj(bindings[i]), "placement", placementKObj)
			continue
		}

		// We don't check the bindings[i].Spec.ResourceSnapshotName != latestResourceSnapshot.Name here.
		// The existing conditions are needed when building the new ones.
		if bindings[i].GetBindingSpec().SchedulingPolicySnapshotName != latestSchedulingPolicySnapshot.GetName() {
			continue
		}
		res[bindings[i].GetBindingSpec().TargetCluster] = bindings[i]
	}

	return res, nil
}

// findResourceSnapshotIndexForBindings finds the resource snapshot index for each binding for CRP/RP.
// It returns a map which maps the target cluster name to the resource snapshot index string.
func (r *Reconciler) findResourceSnapshotIndexForBindings(
	ctx context.Context,
	placementObj fleetv1beta1.PlacementObj,
	bindingMap map[string]fleetv1beta1.BindingObj,
) (map[string]string, error) {
	placementKObj := klog.KObj(placementObj)
	res := make(map[string]string, len(bindingMap))
	for targetCluster, binding := range bindingMap {
		resourceSnapshotName := binding.GetBindingSpec().ResourceSnapshotName
		if resourceSnapshotName == "" {
			klog.InfoS("Empty resource snapshot name found in binding, controller might observe in-between state", "binding", klog.KObj(binding), "placement", placementKObj)
			res[targetCluster] = ""
			continue
		}
		var resourceSnapshotObj fleetv1beta1.ResourceSnapshotObj
		if isClusterScopedPlacement(placementObj) {
			resourceSnapshotObj = &fleetv1beta1.ClusterResourceSnapshot{}
		} else {
			resourceSnapshotObj = &fleetv1beta1.ResourceSnapshot{}
		}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: resourceSnapshotName, Namespace: placementObj.GetNamespace()}, resourceSnapshotObj); err != nil {
			if apierrors.IsNotFound(err) {
				klog.InfoS("The resource snapshot specified in binding is not found, probably deleted due to revision history limit",
					"resourceSnapshotName", resourceSnapshotName, "binding", klog.KObj(binding), "placement", placementKObj)
				res[targetCluster] = ""
				continue
			}
			klog.ErrorS(err, "Failed to get the resource snapshot specified in binding", "resourceSnapshotName", resourceSnapshotName, "binding", klog.KObj(binding), "placement", placementKObj)
			return res, controller.NewAPIServerError(true, err)
		}
		res[targetCluster] = resourceSnapshotObj.GetLabels()[fleetv1beta1.ResourceIndexLabel]
	}

	return res, nil
}

// setPerClusterPlacementStatus sets the resource related fields for each cluster.
// It returns a map which tracks the set status for each relevant condition type.
func (r *Reconciler) setPerClusterPlacementStatus(
	placementObj fleetv1beta1.PlacementObj,
	latestResourceSnapshot fleetv1beta1.ResourceSnapshotObj,
	resourceSnapshotIndexOnBinding string,
	binding fleetv1beta1.BindingObj,
	status *fleetv1beta1.PerClusterPlacementStatus,
	expectedCondTypes []condition.ResourceCondition,
) map[condition.ResourceCondition]metav1.ConditionStatus {
	res := make(map[condition.ResourceCondition]metav1.ConditionStatus)
	if binding == nil {
		// The binding cannot be found; Fleet might be observing an in-between state where
		// the cluster has been picked but the binding has not been created yet.
		meta.SetStatusCondition(&status.Conditions, condition.RolloutStartedCondition.UnknownPerClusterCondition(placementObj.GetGeneration()))
		res[condition.RolloutStartedCondition] = metav1.ConditionUnknown
		return res
	}

	// For External rollout strategy, the per-cluster status is set to whatever exists on the binding.
	if placementObj.GetPlacementSpec().Strategy.Type == fleetv1beta1.ExternalRolloutStrategyType {
		status.ObservedResourceIndex = resourceSnapshotIndexOnBinding
		setResourcePlacementStatusBasedOnBinding(placementObj, binding, status, expectedCondTypes, res)
		return res
	}

	// TODO (wantjian): we only change the per-cluster status for External rollout strategy for now, so set the ObservedResourceIndex as the latest.
	status.ObservedResourceIndex = latestResourceSnapshot.GetLabels()[fleetv1beta1.ResourceIndexLabel]
	rolloutStartedCond := binding.GetCondition(string(condition.RolloutStartedCondition.ResourceBindingConditionType()))
	switch {
	case binding.GetBindingSpec().ResourceSnapshotName != latestResourceSnapshot.GetName() && condition.IsConditionStatusFalse(rolloutStartedCond, binding.GetGeneration()):
		// The binding uses an out of date resource snapshot and rollout controller has reported
		// that the rollout is being blocked (the RolloutStarted condition is of the False status).
		cond := metav1.Condition{
			Type:               string(condition.RolloutStartedCondition.PerClusterPlacementConditionType()),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: placementObj.GetGeneration(),
			Reason:             condition.RolloutNotStartedYetReason,
			Message:            "The rollout is being blocked by the rollout strategy",
		}
		meta.SetStatusCondition(&status.Conditions, cond)
		res[condition.RolloutStartedCondition] = metav1.ConditionFalse
		return res
	case binding.GetBindingSpec().ResourceSnapshotName != latestResourceSnapshot.GetName():
		// The binding uses an out of date resource snapshot, and the RolloutStarted condition is
		// set to True, Unknown, or has become stale. Fleet might be observing an in-between state.
		meta.SetStatusCondition(&status.Conditions, condition.RolloutStartedCondition.UnknownPerClusterCondition(placementObj.GetGeneration()))
		klog.V(5).InfoS("The cluster resource binding has a stale RolloutStarted condition, or it links to an out of date resource snapshot yet has the RolloutStarted condition set to True or Unknown status", "clusterResourceBinding", klog.KObj(binding), "placement", klog.KObj(placementObj))
		res[condition.RolloutStartedCondition] = metav1.ConditionUnknown
		return res
	default:
		// The binding uses the latest resource snapshot.
		setResourcePlacementStatusBasedOnBinding(placementObj, binding, status, expectedCondTypes, res)
		return res
	}
}

// setResourcePlacementStatusBasedOnBinding sets the placement status based on its corresponding binding status.
// It updates the status object in place and tracks the set status for each relevant condition type in setStatusByCondType map provided.
func setResourcePlacementStatusBasedOnBinding(
	placementObj fleetv1beta1.PlacementObj,
	binding fleetv1beta1.BindingObj,
	status *fleetv1beta1.PerClusterPlacementStatus,
	expectedCondTypes []condition.ResourceCondition,
	setStatusByCondType map[condition.ResourceCondition]metav1.ConditionStatus,
) {
	for _, i := range expectedCondTypes {
		bindingCond := binding.GetCondition(string(i.ResourceBindingConditionType()))
		if !condition.IsConditionStatusTrue(bindingCond, binding.GetGeneration()) &&
			!condition.IsConditionStatusFalse(bindingCond, binding.GetGeneration()) {
			meta.SetStatusCondition(&status.Conditions, i.UnknownPerClusterCondition(placementObj.GetGeneration()))
			klog.V(5).InfoS("Find an unknown condition", "bindingCond", bindingCond, "clusterResourceBinding", klog.KObj(binding), "placement", klog.KObj(placementObj))
			setStatusByCondType[i] = metav1.ConditionUnknown
			break
		}

		switch i {
		case condition.RolloutStartedCondition:
			if bindingCond.Status == metav1.ConditionTrue {
				status.ApplicableResourceOverrides = binding.GetBindingSpec().ResourceOverrideSnapshots
				status.ApplicableClusterResourceOverrides = binding.GetBindingSpec().ClusterResourceOverrideSnapshots
			}
		case condition.AppliedCondition, condition.AvailableCondition:
			if bindingCond.Status == metav1.ConditionFalse {
				status.FailedPlacements = binding.GetBindingStatus().FailedPlacements
				status.DiffedPlacements = binding.GetBindingStatus().DiffedPlacements
			}
			// Note that configuration drifts can occur whether the manifests are applied
			// successfully or not.
			status.DriftedPlacements = binding.GetBindingStatus().DriftedPlacements
		case condition.DiffReportedCondition:
			if bindingCond.Status == metav1.ConditionTrue {
				status.DiffedPlacements = binding.GetBindingStatus().DiffedPlacements
			}
		}

		cond := metav1.Condition{
			Type:               string(i.PerClusterPlacementConditionType()),
			Status:             bindingCond.Status,
			ObservedGeneration: placementObj.GetGeneration(),
			Reason:             bindingCond.Reason,
			Message:            bindingCond.Message,
		}
		meta.SetStatusCondition(&status.Conditions, cond)
		setStatusByCondType[i] = bindingCond.Status

		if bindingCond.Status == metav1.ConditionFalse {
			break // if the current condition is false, no need to populate the rest conditions
		}
	}
}

// isClusterScopedPlacement returns true if the placement is cluster-scoped (namespace is empty), false otherwise.
func isClusterScopedPlacement(placementObj fleetv1beta1.PlacementObj) bool {
	return placementObj.GetNamespace() == ""
}

// getPlacementScheduledConditionType returns the appropriate scheduled condition type based on the placement type.
func getPlacementScheduledConditionType(placementObj fleetv1beta1.PlacementObj) string {
	if isClusterScopedPlacement(placementObj) {
		return string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType)
	}
	return string(fleetv1beta1.ResourcePlacementScheduledConditionType)
}

// getPlacementRolloutStartedConditionType returns the appropriate rollout started condition type based on the placement type.
func getPlacementRolloutStartedConditionType(placementObj fleetv1beta1.PlacementObj) string {
	if isClusterScopedPlacement(placementObj) {
		return string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType)
	}
	return string(fleetv1beta1.ResourcePlacementRolloutStartedConditionType)
}

// getPlacementConditionType returns the appropriate placement condition type based on whether the placement is cluster-scoped or namespace-scoped.
// If the placement namespace is empty (cluster-scoped), it returns the ClusterResourcePlacement condition type.
// Otherwise (namespace-scoped), it returns the ResourcePlacement condition type.
func getPlacementConditionType(placementObj fleetv1beta1.PlacementObj, condType condition.ResourceCondition) string {
	if isClusterScopedPlacement(placementObj) {
		// Cluster-scoped placement (ClusterResourcePlacement)
		return string(condType.ClusterResourcePlacementConditionType())
	}
	// Namespace-scoped placement (ResourcePlacement)
	return string(condType.ResourcePlacementConditionType())
}

// generatePlacementConditionByStatus returns the appropriate placement condition based on status and placement type.
// If the placement namespace is empty (cluster-scoped), it returns ClusterResourcePlacement conditions.
// Otherwise (namespace-scoped), it returns ResourcePlacement conditions.
func generatePlacementConditionByStatus(placementObj fleetv1beta1.PlacementObj, condType condition.ResourceCondition, status metav1.ConditionStatus, generation int64, clusterCount int) metav1.Condition {
	if isClusterScopedPlacement(placementObj) {
		// Cluster-scoped placement (ClusterResourcePlacement)
		switch status {
		case metav1.ConditionUnknown:
			return condType.UnknownClusterResourcePlacementCondition(generation, clusterCount)
		case metav1.ConditionFalse:
			return condType.FalseClusterResourcePlacementCondition(generation, clusterCount)
		default: // metav1.ConditionTrue is the only other case
			return condType.TrueClusterResourcePlacementCondition(generation, clusterCount)
		}
	}
	// Namespace-scoped placement (ResourcePlacement)
	switch status {
	case metav1.ConditionUnknown:
		return condType.UnknownResourcePlacementCondition(generation, clusterCount)
	case metav1.ConditionFalse:
		return condType.FalseResourcePlacementCondition(generation, clusterCount)
	default: // metav1.ConditionTrue is the only other case
		return condType.TrueResourcePlacementCondition(generation, clusterCount)
	}
}
