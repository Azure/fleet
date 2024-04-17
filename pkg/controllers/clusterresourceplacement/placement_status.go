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
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/labels"
)

var (
	// We only include 100 failed resource placements even if there are more than 100.
	maxFailedResourcePlacementLimit = 100
)

// ClusterResourcePlacementStatus condition reasons
const (
	// InvalidResourceSelectorsReason is the reason string of placement condition when the selected resources are invalid
	// or forbidden.
	InvalidResourceSelectorsReason = "InvalidResourceSelectors"
	// SchedulingUnknownReason is the reason string of placement condition when the schedule status is unknown.
	SchedulingUnknownReason = "SchedulePending"

	// SynchronizePendingReason is the reason string of placement condition when the selected resources have not synchronized
	// under the per-cluster namespaces (i.e., fleet-member-<member-name>) on the hub cluster yet.
	SynchronizePendingReason = "SynchronizePending"
	// SynchronizeSucceededReason is the reason string of placement condition when the selected resources are
	// successfully synchronized under the per-cluster namespaces (i.e., fleet-member-<member-name>) on the hub cluster.
	SynchronizeSucceededReason = "SynchronizeSucceeded"

	// ApplyFailedReason is the reason string of placement condition when the selected resources fail to apply.
	ApplyFailedReason = "ApplyFailed"
	// ApplyPendingReason is the reason string of placement condition when the selected resources are pending to apply.
	ApplyPendingReason = "ApplyPending"
	// ApplySucceededReason is the reason string of placement condition when the selected resources are applied successfully.
	ApplySucceededReason = "ApplySucceeded"
)

// ResourcePlacementStatus condition reasons and message formats
const (
	// ResourceApplyFailedReason is the reason string of placement condition when the selected resources fail to apply.
	ResourceApplyFailedReason = "ApplyFailed"
	// ResourceApplyPendingReason is the reason string of placement condition when the selected resources are pending to apply.
	ResourceApplyPendingReason = "ApplyPending"
	// ResourceApplySucceededReason is the reason string of placement condition when the selected resources are applied successfully.
	ResourceApplySucceededReason = "ApplySucceeded"

	// WorkSynchronizePendingReason is the reason string of placement condition when the work(s) are pending to synchronize.
	WorkSynchronizePendingReason = "WorkSynchronizePending"
	// WorkSynchronizeFailedReason is the reason string of placement condition when the work(s) failed to synchronize.
	WorkSynchronizeFailedReason = "WorkSynchronizeFailed"
	// WorkSynchronizeSucceededReason is the reason string of placement condition when the work(s) are synchronized successfully.
	WorkSynchronizeSucceededReason = "WorkSynchronizeSucceeded"

	// ResourceScheduleSucceededReason is the reason string of placement condition when the selected resources are scheduled.
	ResourceScheduleSucceededReason = "ScheduleSucceeded"
	// ResourceScheduleFailedReason is the reason string of placement condition when the scheduler failed to schedule the selected resources.
	ResourceScheduleFailedReason = "ScheduleFailed"

	// ResourcePlacementStatus schedule condition message formats
	resourcePlacementConditionScheduleFailedMessageFormat             = "%s is not selected: %s"
	resourcePlacementConditionScheduleFailedWithScoreMessageFormat    = "%s is not selected with clusterScore %+v: %s"
	resourcePlacementConditionScheduleSucceededMessageFormat          = "Successfully scheduled resources for placement in %s: %s"
	resourcePlacementConditionScheduleSucceededWithScoreMessageFormat = "Successfully scheduled resources for placement in %s (affinity score: %d, topology spread score: %d): %s"
)

func buildClusterResourcePlacementSyncCondition(crp *fleetv1beta1.ClusterResourcePlacement, pendingCount, succeededCount int) metav1.Condition {
	klog.V(3).InfoS("Building the clusterResourcePlacement synchronized condition", "clusterResourcePlacement", klog.KObj(crp),
		"numberOfSynchronizedPendingCluster", pendingCount, "numberOfSynchronizedSucceededCluster", succeededCount)
	if pendingCount > 0 {
		return metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
			Reason:             SynchronizePendingReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) pending to be sychronized on the hub cluster", pendingCount),
			ObservedGeneration: crp.Generation,
		}
	}
	return metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
		Reason:             SynchronizeSucceededReason,
		Message:            fmt.Sprintf("All %d cluster(s) are synchronized to the latest resources on the hub cluster", succeededCount),
		ObservedGeneration: crp.Generation,
	}
}

func buildClusterResourcePlacementApplyCondition(crp *fleetv1beta1.ClusterResourcePlacement, isSync bool, pendingCount, succeededCount, failedCount int) metav1.Condition {
	klog.V(3).InfoS("Building the clusterResourcePlacement applied condition", "clusterResourcePlacement", klog.KObj(crp),
		"isSynced", isSync, "numberOfAppliedPendingCluster", pendingCount, "numberOfAppliedSucceededCluster", succeededCount, "numberOfAppliedFailedCluster", failedCount)

	if pendingCount+succeededCount+failedCount == 0 {
		// If the cluster is selected, it should be in one of the state.
		return metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
			Reason:             ApplySucceededReason,
			Message:            "There are no clusters selected to place the resources",
			ObservedGeneration: crp.Generation,
		}
	}

	if !isSync || pendingCount > 0 {
		return metav1.Condition{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
			Reason:             ApplyPendingReason,
			Message:            fmt.Sprintf("Works need to be synchronized on the hub cluster or there are still manifests pending to be processed by the %d member clusters ", pendingCount),
			ObservedGeneration: crp.Generation,
		}
	}

	if failedCount > 0 {
		return metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
			Reason:             ApplyFailedReason,
			Message:            fmt.Sprintf("Failed to apply manifests to %d clusters, please check the `failedPlacements` status", failedCount),
			ObservedGeneration: crp.Generation,
		}
	}

	return metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
		Reason:             ApplySucceededReason,
		Message:            fmt.Sprintf("Successfully applied resources to %d member clusters", succeededCount),
		ObservedGeneration: crp.Generation,
	}
}

// setWorkStatusForResourcePlacementStatus will list all the associated works with latest resourceSnapshots and build the work conditions.
// Returns workSynchronizedCondition & workAppliedCondition.
func (r *Reconciler) setWorkStatusForResourcePlacementStatus(ctx context.Context,
	crp *fleetv1beta1.ClusterResourcePlacement, latestResourceSnapshot *fleetv1beta1.ClusterResourceSnapshot, clusterResourceBinding *fleetv1beta1.ClusterResourceBinding,
	status *fleetv1beta1.ResourcePlacementStatus) (*metav1.Condition, *metav1.Condition, error) {
	crpKObj := klog.KObj(crp)
	namespaceMatcher := client.InNamespace(fmt.Sprintf(utils.NamespaceNameFormat, status.ClusterName))
	workLabelMatcher := client.MatchingLabels{
		fleetv1beta1.CRPTrackingLabel: crp.Name,
	}
	workList := &fleetv1beta1.WorkList{}

	if err := r.Client.List(ctx, workList, workLabelMatcher, namespaceMatcher); err != nil {
		klog.ErrorS(err, "Failed to list all the work associated with the clusterResourcePlacement", "clusterResourcePlacement", crpKObj, "clusterName", status.ClusterName)
		return nil, nil, controller.NewAPIServerError(true, err)
	}

	latestResourceIndex, err := labels.ExtractResourceIndexFromClusterResourceSnapshot(latestResourceSnapshot)
	if err != nil {
		klog.ErrorS(err, "Failed to parse the resource snapshot index label from latest clusterResourceSnapshot", "clusterResourcePlacement", crpKObj, "clusterResourceSnapshot", klog.KObj(latestResourceSnapshot))
		return nil, nil, controller.NewUnexpectedBehaviorError(err)
	}
	// Used to build the work applied condition
	pendingWorkCounter := 0 // The work has not been applied yet.

	failedResourcePlacements := make([]fleetv1beta1.FailedResourcePlacement, 0, maxFailedResourcePlacementLimit) // preallocate the memory
	for i := range workList.Items {
		work := workList.Items[i]
		if work.DeletionTimestamp != nil {
			continue // ignore the deleting work
		}
		workKObj := klog.KObj(&work)
		resourceIndexFromWork, err := labels.ExtractResourceSnapshotIndexFromWork(&work)
		if err != nil {
			klog.ErrorS(err, "Failed to parse the resource snapshot index label from work", "clusterResourcePlacement", crpKObj, "work", workKObj)
			return nil, nil, controller.NewUnexpectedBehaviorError(err)
		}
		if resourceIndexFromWork > latestResourceIndex {
			err := fmt.Errorf("invalid work %s: resource snapshot index %d on the work is greater than resource index %d on the latest clusterResourceSnapshot", workKObj, resourceIndexFromWork, latestResourceIndex)
			klog.ErrorS(err, "Invalid work", "clusterResourcePlacement", crpKObj, "clusterResourceSnapshot", klog.KObj(latestResourceSnapshot), "work", workKObj)
			return nil, nil, controller.NewUnexpectedBehaviorError(err)
		} else if resourceIndexFromWork == latestResourceIndex {
			// We only build the work applied status on the new works.
			isPending, failedManifests := buildFailedResourcePlacements(&work)
			if isPending {
				pendingWorkCounter++
			}
			if len(failedManifests) != 0 && len(failedResourcePlacements) < maxFailedResourcePlacementLimit {
				failedResourcePlacements = append(failedResourcePlacements, failedManifests...)
			}
		}
	}

	if len(failedResourcePlacements) > maxFailedResourcePlacementLimit {
		failedResourcePlacements = failedResourcePlacements[0:maxFailedResourcePlacementLimit]
	}

	klog.V(2).InfoS("Building the resourcePlacementStatus", "clusterResourcePlacement", crpKObj, "clusterName", status.ClusterName,
		"numberOfPendingWorks", pendingWorkCounter, "numberOfFailedResources", len(failedResourcePlacements))

	status.FailedPlacements = failedResourcePlacements

	isSync, workSynchronizedCondition := buildWorkSynchronizedCondition(crp, clusterResourceBinding)
	meta.SetStatusCondition(&status.Conditions, workSynchronizedCondition)

	workAppliedCondition := buildWorkAppliedCondition(crp, !isSync || pendingWorkCounter > 0, len(failedResourcePlacements) > 0)
	meta.SetStatusCondition(&status.Conditions, workAppliedCondition)
	return &workSynchronizedCondition, &workAppliedCondition, nil
}

func buildWorkSynchronizedCondition(crp *fleetv1beta1.ClusterResourcePlacement, binding *fleetv1beta1.ClusterResourceBinding) (bool, metav1.Condition) {
	if binding != nil {
		boundCondition := binding.GetCondition(string(fleetv1beta1.ResourceBindingBound))
		if condition.IsConditionStatusTrue(boundCondition, binding.Generation) {
			// This condition is set to true only if the work generator have created all the works according to the latest resource snapshot.
			return true, metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
				Reason:             WorkSynchronizeSucceededReason,
				Message:            "Successfully Synchronized work(s) for placement",
				ObservedGeneration: crp.Generation,
			}
		} else if condition.IsConditionStatusFalse(boundCondition, binding.Generation) {
			return false, metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
				Reason:             WorkSynchronizeFailedReason,
				Message:            boundCondition.Message,
				ObservedGeneration: crp.Generation,
			}
		}
	}
	return false, metav1.Condition{
		Status:             metav1.ConditionFalse,
		Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
		Reason:             WorkSynchronizePendingReason,
		Message:            "In the process of synchronizing or operation is blocked by the rollout strategy ",
		ObservedGeneration: crp.Generation,
	}
}

func buildWorkAppliedCondition(crp *fleetv1beta1.ClusterResourcePlacement, hasPendingWork, hasFailedResource bool) metav1.Condition {
	// hasPendingWork could be true when
	// - the works have not been created in the hub cluster.
	// - the works have not been processed by the member cluster.
	if hasPendingWork {
		return metav1.Condition{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
			Reason:             ResourceApplyPendingReason,
			Message:            "Works need to be synchronized on the hub cluster or there are still manifests pending to be processed by the member cluster",
			ObservedGeneration: crp.Generation,
		}
	}

	if hasFailedResource {
		return metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
			Reason:             ResourceApplyFailedReason,
			Message:            "Failed to apply manifests, please check the `failedPlacements` status",
			ObservedGeneration: crp.Generation,
		}
	}

	return metav1.Condition{ // if !hasFailedResource && !hasPendingWork
		Status:             metav1.ConditionTrue,
		Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
		Reason:             ResourceApplySucceededReason,
		Message:            "Successfully applied resources",
		ObservedGeneration: crp.Generation,
	}
}

// buildFailedResourcePlacements returns if work is pending or not.
// If the work has been applied, it returns the list of failed resources.
func buildFailedResourcePlacements(work *fleetv1beta1.Work) (isPending bool, res []fleetv1beta1.FailedResourcePlacement) {
	// check the overall condition
	workKObj := klog.KObj(work)
	appliedCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
	if appliedCond == nil {
		klog.V(3).InfoS("The work is never picked up by the member cluster", "work", workKObj)
		return true, nil
	}
	if appliedCond.ObservedGeneration < work.GetGeneration() || appliedCond.Status == metav1.ConditionUnknown {
		klog.V(3).InfoS("The update of the work is not picked up by the member cluster yet", "work", workKObj, "workGeneration", work.GetGeneration(), "appliedGeneration", appliedCond.ObservedGeneration, "status", appliedCond.Status)
		return true, nil
	}

	if appliedCond.Status == metav1.ConditionTrue {
		klog.V(3).InfoS("The work is applied successfully by the member cluster", "work", workKObj, "workGeneration", work.GetGeneration())
		return false, nil
	}

	// check if the work is generated by an enveloped object
	envelopeType, isEnveloped := work.GetLabels()[fleetv1beta1.EnvelopeTypeLabel]
	var envelopObjName, envelopObjNamespace string
	if isEnveloped {
		// If the work  generated by an enveloped object, it must contain those labels.
		envelopObjName = work.GetLabels()[fleetv1beta1.EnvelopeNameLabel]
		envelopObjNamespace = work.GetLabels()[fleetv1beta1.EnvelopeNamespaceLabel]
	}
	res = make([]fleetv1beta1.FailedResourcePlacement, 0, len(work.Status.ManifestConditions))
	for _, manifestCondition := range work.Status.ManifestConditions {
		appliedCond = meta.FindStatusCondition(manifestCondition.Conditions, fleetv1beta1.WorkConditionTypeApplied)
		// collect if there is an explicit fail
		if appliedCond != nil && appliedCond.Status != metav1.ConditionTrue {
			if isEnveloped {
				klog.V(2).InfoS("Find a failed to apply enveloped manifest",
					"manifestName", manifestCondition.Identifier.Name,
					"group", manifestCondition.Identifier.Group,
					"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind,
					"envelopeType", envelopeType, "envelopObjName", envelopObjName, "envelopObjNamespace", envelopObjNamespace)
			} else {
				klog.V(2).InfoS("Find a failed to apply manifest",
					"manifestName", manifestCondition.Identifier.Name, "group", manifestCondition.Identifier.Group,
					"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind)
			}
			failedManifest := fleetv1beta1.FailedResourcePlacement{
				ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
					Group:     manifestCondition.Identifier.Group,
					Version:   manifestCondition.Identifier.Version,
					Kind:      manifestCondition.Identifier.Kind,
					Name:      manifestCondition.Identifier.Name,
					Namespace: manifestCondition.Identifier.Namespace,
				},
				Condition: *appliedCond,
			}
			if isEnveloped {
				failedManifest.ResourceIdentifier.Envelope = &fleetv1beta1.EnvelopeIdentifier{
					Name:      envelopObjName,
					Namespace: envelopObjNamespace,
					Type:      fleetv1beta1.EnvelopeType(envelopeType),
				}
			}
			res = append(res, failedManifest)
		}
	}
	return false, res
}

// --------------------------------- populating the new conditions------------

// setResourceConditions sets the resource related conditions by looking at the bindings and work, excluding the scheduled
// condition.
// It returns whether there is a cluster scheduled or not.
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
			Message:            fmt.Sprintf(resourcePlacementConditionScheduleSucceededMessageFormat, c.ClusterName, c.Reason),
			ObservedGeneration: crp.Generation,
		}
		rps.ClusterName = c.ClusterName
		if c.ClusterScore != nil {
			scheduledCondition.Message = fmt.Sprintf(resourcePlacementConditionScheduleSucceededWithScoreMessageFormat, c.ClusterName, *c.ClusterScore.AffinityScore, *c.ClusterScore.TopologySpreadScore, c.Reason)
		}
		oldConditions, ok := oldResourcePlacementStatusMap[c.ClusterName]
		if ok {
			// update the lastTransitionTime considering the existing condition status instead of overwriting
			rps.Conditions = oldConditions
		}
		meta.SetStatusCondition(&rps.Conditions, scheduledCondition)
		res, err := r.setResourcePlacementStatusPerCluster(ctx, crp, latestResourceSnapshot, resourceBindingMap[c.ClusterName], &rps)
		if err != nil {
			return false, err
		}
		for i := range res {
			switch res[i] {
			case metav1.ConditionTrue:
				clusterConditionStatusRes[i][condition.TrueConditionStatus]++
			case metav1.ConditionFalse:
				clusterConditionStatusRes[i][condition.FalseConditionStatus]++
			case metav1.ConditionUnknown:
				clusterConditionStatusRes[i][condition.UnknownConditionStatus]++
			}
		}
		placementStatuses = append(placementStatuses, rps)
		klog.V(2).InfoS("Populated the resource placement status for the scheduled cluster", "clusterResourcePlacement", klog.KObj(crp), "cluster", c.ClusterName)
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
			Message:            fmt.Sprintf(resourcePlacementConditionScheduleFailedMessageFormat, unselected[i].ClusterName, unselected[i].Reason),
			ObservedGeneration: crp.Generation,
		}
		if unselected[i].ClusterScore != nil {
			scheduledCondition.Message = fmt.Sprintf(resourcePlacementConditionScheduleFailedWithScoreMessageFormat, unselected[i].ClusterName, unselected[i].ClusterScore, unselected[i].Reason)
		}
		meta.SetStatusCondition(&rp.Conditions, scheduledCondition)
		placementStatuses = append(placementStatuses, rp)
		klog.V(2).InfoS("Populated the resource placement status for the unscheduled cluster", "clusterResourcePlacement", klog.KObj(crp), "cluster", unselected[i].ClusterName)
	}
	crp.Status.PlacementStatuses = placementStatuses

	if !isClusterScheduled {
		// TODO special handling when isClusterScheduled is false
		// Currently it will stop updating the resource related conditions and leave the existing conditions there.
		return false, nil
	}

	for i := condition.RolloutStartedCondition; i < condition.TotalCondition; i++ {
		if clusterConditionStatusRes[i][condition.UnknownConditionStatus] > 0 {
			crp.SetConditions(i.UnknownClusterResourcePlacementCondition(crp.Generation, clusterConditionStatusRes[i][condition.UnknownConditionStatus]))
			break
		} else if clusterConditionStatusRes[i][condition.FalseConditionStatus] > 0 {
			crp.SetConditions(i.FalseClusterResourcePlacementCondition(crp.Generation, clusterConditionStatusRes[i][condition.FalseConditionStatus]))
			break
		} else {
			crp.SetConditions(i.TrueClusterResourcePlacementCondition(crp.Generation, clusterConditionStatusRes[i][condition.TrueConditionStatus]))
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
//	RolloutStartedCondition resourceCondition = iota
//	OverriddenCondition
//	WorkSynchronizedCondition
//	AppliedCondition
//	AvailableCondition
//	TotalCondition
//
// )
func (r *Reconciler) setResourcePlacementStatusPerCluster(ctx context.Context,
	crp *fleetv1beta1.ClusterResourcePlacement, latestResourceSnapshot *fleetv1beta1.ClusterResourceSnapshot, binding *fleetv1beta1.ClusterResourceBinding, status *fleetv1beta1.ResourcePlacementStatus) ([]metav1.ConditionStatus, error) {
	if binding == nil {
		meta.SetStatusCondition(&status.Conditions, condition.RolloutStartedCondition.UnknownResourceConditionPerCluster(crp.Generation))
		return []metav1.ConditionStatus{metav1.ConditionUnknown}, nil
	}

	res := make([]metav1.ConditionStatus, 0, condition.TotalCondition)
	// There are few cases:
	// * if the resourceSnapshotName is not equal,
	//     1. the status is false, it means the rollout is stuck.
	//     2. otherwise, the rollout controller has not processed it yet.
	// * if the resourceSnapshotName is equal,
	//     just return the corresponding status.
	if binding.Spec.ResourceSnapshotName == latestResourceSnapshot.Name {
		for i := condition.RolloutStartedCondition; i < condition.TotalCondition; i++ {
			bindingCond := binding.GetCondition(string(i.ResourceBindingConditionType()))
			if !condition.IsConditionStatusTrue(bindingCond, binding.Generation) &&
				!condition.IsConditionStatusFalse(bindingCond, binding.Generation) {
				meta.SetStatusCondition(&status.Conditions, i.UnknownResourceConditionPerCluster(crp.Generation))
				res = append(res, metav1.ConditionUnknown)
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
					if err := r.setFailedPlacementsPerCluster(ctx, crp, binding, status); err != nil {
						return nil, err
					}
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
			res = append(res, bindingCond.Status)

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
		res = append(res, metav1.ConditionFalse)
		return res, nil
	}
	// At this point, either the generation is not the one in the binding spec or the status is true/unknown.
	// It means the rollout controller has not handled the binding yet.
	meta.SetStatusCondition(&status.Conditions, condition.RolloutStartedCondition.UnknownResourceConditionPerCluster(crp.Generation))
	return []metav1.ConditionStatus{metav1.ConditionUnknown}, nil
}

// TODO, instead of crp looking for the failed manifests from the works, the work generator will populate in the binding
// in addition to the conditions to solve the inconsistency data between bindings and works, which is also more efficient.
// Note, today there is no data about the mapping between the binding generation and work generation.
func (r *Reconciler) setFailedPlacementsPerCluster(ctx context.Context, crp *fleetv1beta1.ClusterResourcePlacement, binding *fleetv1beta1.ClusterResourceBinding, status *fleetv1beta1.ResourcePlacementStatus) error {
	namespaceMatcher := client.InNamespace(fmt.Sprintf(utils.NamespaceNameFormat, status.ClusterName))
	workLabelMatcher := client.MatchingLabels{
		fleetv1beta1.CRPTrackingLabel:   crp.Name,
		fleetv1beta1.ParentBindingLabel: binding.Name,
	}
	workList := &fleetv1beta1.WorkList{}
	crpKObj := klog.KObj(crp)
	bindingKObj := klog.KObj(binding)
	if err := r.Client.List(ctx, workList, workLabelMatcher, namespaceMatcher); err != nil {
		klog.ErrorS(err, "Failed to list all the work associated with the clusterResourcePlacement", "clusterResourcePlacement", crpKObj, "clusterResourceBinding", bindingKObj, "clusterName", status.ClusterName)
		return controller.NewAPIServerError(true, err)
	}
	klog.V(2).InfoS("Listed works to find the failed placements", "clusterResourcePlacement", crpKObj, "clusterResourceBinding", bindingKObj, "clusterName", status.ClusterName, "numberOfWorks", len(workList.Items))

	failedResourcePlacements := make([]fleetv1beta1.FailedResourcePlacement, 0, maxFailedResourcePlacementLimit) // preallocate the memory
	for i := range workList.Items {
		work := workList.Items[i]
		if work.DeletionTimestamp != nil {
			klog.V(2).InfoS("Ignoring the deleting work", "clusterResourcePlacement", crpKObj, "clusterResourceBinding", bindingKObj, "work", klog.KObj(&work))
			continue // ignore the deleting work
		}
		failedManifests := extractFailedResourcePlacementsFromWork(&work)
		if len(failedManifests) != 0 && len(failedResourcePlacements) < maxFailedResourcePlacementLimit {
			failedResourcePlacements = append(failedResourcePlacements, failedManifests...)
		}
	}

	if len(failedResourcePlacements) == 0 {
		err := fmt.Errorf("there are no works (total number %v) with failed manifest condition which is not matched with the binding status: %v", len(workList.Items), binding.Status.Conditions)
		klog.ErrorS(err, "No failed manifests are found for the resource", "clusterResourcePlacement", crpKObj, "clusterResourceBinding", bindingKObj, "clusterName", status.ClusterName)
		// There will be a case that, the binding is just updated when we query the works.
		// So that the works have been updated and the cached binding condition is out of date.
		// We requeue the request to try again.
		return controller.NewExpectedBehaviorError(err)
	}

	if len(failedResourcePlacements) > maxFailedResourcePlacementLimit {
		failedResourcePlacements = failedResourcePlacements[0:maxFailedResourcePlacementLimit]
	}
	status.FailedPlacements = failedResourcePlacements
	klog.V(2).InfoS("Populated failed manifests", "clusterResourcePlacement", crpKObj, "clusterName", status.ClusterName, "numberOfFailedPlacements", len(failedResourcePlacements))
	return nil
}

func extractFailedResourcePlacementsFromWork(work *fleetv1beta1.Work) []fleetv1beta1.FailedResourcePlacement {
	appliedCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
	availableCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)

	// The applied condition and available condition are always updated in one call.
	// It means the observedGeneration of these two are always the same.
	// If IsConditionStatusFalse is true, means both are observing the latest work.
	if !condition.IsConditionStatusFalse(appliedCond, work.Generation) &&
		!condition.IsConditionStatusFalse(availableCond, work.Generation) {
		return nil
	}

	// check if the work is generated by an enveloped object
	envelopeType, isEnveloped := work.GetLabels()[fleetv1beta1.EnvelopeTypeLabel]
	var envelopObjName, envelopObjNamespace string
	if isEnveloped {
		// If the work  generated by an enveloped object, it must contain those labels.
		envelopObjName = work.GetLabels()[fleetv1beta1.EnvelopeNameLabel]
		envelopObjNamespace = work.GetLabels()[fleetv1beta1.EnvelopeNamespaceLabel]
	}
	res := make([]fleetv1beta1.FailedResourcePlacement, 0, len(work.Status.ManifestConditions))
	for _, manifestCondition := range work.Status.ManifestConditions {
		failedManifest := fleetv1beta1.FailedResourcePlacement{
			ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
				Group:     manifestCondition.Identifier.Group,
				Version:   manifestCondition.Identifier.Version,
				Kind:      manifestCondition.Identifier.Kind,
				Name:      manifestCondition.Identifier.Name,
				Namespace: manifestCondition.Identifier.Namespace,
			},
		}
		if isEnveloped {
			failedManifest.ResourceIdentifier.Envelope = &fleetv1beta1.EnvelopeIdentifier{
				Name:      envelopObjName,
				Namespace: envelopObjNamespace,
				Type:      fleetv1beta1.EnvelopeType(envelopeType),
			}
		}

		appliedCond = meta.FindStatusCondition(manifestCondition.Conditions, fleetv1beta1.WorkConditionTypeApplied)
		// collect if there is an explicit fail
		// The observedGeneration of the manifest condition is the generation of the applied manifest.
		// The overall applied and available conditions are observing the latest work generation.
		// So that the manifest condition should be latest, assuming they're populated by the work agent in one update call.
		if appliedCond != nil && appliedCond.Status == metav1.ConditionFalse {
			if isEnveloped {
				klog.V(2).InfoS("Find a failed to apply enveloped manifest",
					"manifestName", manifestCondition.Identifier.Name,
					"group", manifestCondition.Identifier.Group,
					"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind,
					"envelopeType", envelopeType, "envelopObjName", envelopObjName, "envelopObjNamespace", envelopObjNamespace)
			} else {
				klog.V(2).InfoS("Find a failed to apply manifest",
					"manifestName", manifestCondition.Identifier.Name, "group", manifestCondition.Identifier.Group,
					"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind)
			}
			failedManifest.Condition = *appliedCond
			res = append(res, failedManifest)
			break
		}
		availableCond = meta.FindStatusCondition(manifestCondition.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
		if availableCond != nil && availableCond.Status == metav1.ConditionFalse {
			if isEnveloped {
				klog.V(2).InfoS("Find an unavailable enveloped manifest",
					"manifestName", manifestCondition.Identifier.Name,
					"group", manifestCondition.Identifier.Group,
					"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind,
					"envelopeType", envelopeType, "envelopObjName", envelopObjName, "envelopObjNamespace", envelopObjNamespace)
			} else {
				klog.V(2).InfoS("Find an unavailable enveloped manifest",
					"manifestName", manifestCondition.Identifier.Name, "group", manifestCondition.Identifier.Group,
					"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind)
			}
			failedManifest.Condition = *availableCond
			res = append(res, failedManifest)
		}
	}
	return res
}
