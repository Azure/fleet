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
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	workapi "go.goms.io/fleet/pkg/controllers/work"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/annotations"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/labels"
)

var (
	// We only include 100 failed resource placements even if there are more than 100.
	maxFailedResourcePlacementLimit = 100
)

const (
	// ClusterResourcePlacementStatus condition reasons
	invalidResourceSelectorsReason = "InvalidResourceSelectors"

	schedulingUnknownReason = "SchedulePending"

	synchronizePendingReason   = "SynchronizePending"
	synchronizeSucceededReason = "SynchronizeSucceeded"

	// ApplyFailedReason is the reason string of placement condition when the selected resources fail to apply.
	ApplyFailedReason = "ApplyFailed"
	// ApplyPendingReason is the reason string of placement condition when the selected resources are pending to apply.
	ApplyPendingReason = "ApplyPending"
	// ApplySucceededReason is the reason string of placement condition when the selected resources are applied successfully.
	ApplySucceededReason = "ApplySucceeded"

	// ResourcePlacementStatus condition reasons
	// resourceApplyFailedReason is the reason string of placement condition when the selected resources fail to apply.
	resourceApplyFailedReason = "ApplyFailed"
	// resourceApplyPendingReason is the reason string of placement condition when the selected resources are pending to apply.
	resourceApplyPendingReason = "ApplyPending"
	// resourceApplySucceededReason is the reason string of placement condition when the selected resources are applied successfully.
	resourceApplySucceededReason = "ApplySucceeded"

	// workSynchronizePendingReason is the reason string of placement condition when the work(s) are pending to synchronize.
	workSynchronizePendingReason = "WorkSynchronizePending"
	// workSynchronizeSucceededReason is the reason string of placement condition when the work(s) are synchronized successfully.
	workSynchronizeSucceededReason = "WorkSynchronizeSucceeded"

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
			Reason:             synchronizePendingReason,
			Message:            fmt.Sprintf("There are still %d cluster(s) pending to be sychronized on the hub cluster", pendingCount),
			ObservedGeneration: crp.Generation,
		}
	}
	return metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
		Reason:             synchronizeSucceededReason,
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
			Message:            fmt.Sprintf("Failed to apply manifests to %d clusters, please check the `failedResourcePlacements` status", failedCount),
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
func (r *Reconciler) setWorkStatusForResourcePlacementStatus(ctx context.Context, crp *fleetv1beta1.ClusterResourcePlacement, latestResourceSnapshot *fleetv1beta1.ClusterResourceSnapshot, status *fleetv1beta1.ResourcePlacementStatus) (*metav1.Condition, *metav1.Condition, error) {
	crpKObj := klog.KObj(crp)
	namespaceMatcher := client.InNamespace(fmt.Sprintf(utils.NamespaceNameFormat, status.ClusterName))
	workLabelMatcher := client.MatchingLabels{
		fleetv1beta1.CRPTrackingLabel: crp.Name,
	}
	workList := &workv1alpha1.WorkList{}

	if err := r.Client.List(ctx, workList, workLabelMatcher, namespaceMatcher); err != nil {
		klog.ErrorS(err, "Failed to list all the work associated with the clusterResourcePlacement", "clusterResourcePlacement", crpKObj, "clusterName", status.ClusterName)
		return nil, nil, controller.NewAPIServerError(true, err)
	}

	resourceIndex, err := labels.ExtractResourceIndexFromClusterResourceSnapshot(latestResourceSnapshot)
	if err != nil {
		klog.ErrorS(err, "Failed to parse the resource snapshot index label from latest clusterResourceSnapshot", "clusterResourcePlacement", crpKObj, "clusterResourceSnapshot", klog.KObj(latestResourceSnapshot))
		return nil, nil, controller.NewUnexpectedBehaviorError(err)
	}
	// Used to build the work synchronized condition
	oldWorkCounter := 0 // The work is pointing to the old resourceSnapshot.
	newWorkCounter := 0 // The work is pointing to the latest resourceSnapshot.
	// Used to build the work applied condition
	pendingWorkCounter := 0 // The work has not been applied yet.

	failedResourcePlacements := make([]fleetv1beta1.FailedResourcePlacement, 0, maxFailedResourcePlacementLimit) // preallocate the memory
	for i := range workList.Items {
		if workList.Items[i].DeletionTimestamp != nil {
			continue // ignore the deleting work
		}

		workKObj := klog.KObj(&workList.Items[i])
		indexFromWork, err := labels.ExtractResourceSnapshotIndexFromWork(&workList.Items[i])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the resource snapshot index label from work", "clusterResourcePlacement", crpKObj, "work", workKObj)
			return nil, nil, controller.NewUnexpectedBehaviorError(err)
		}
		if indexFromWork > resourceIndex {
			err := fmt.Errorf("invalid work %s: resource snapshot index %d on the work is greater than resource index %d on the latest clusterResourceSnapshot", workKObj, indexFromWork, resourceIndex)
			klog.ErrorS(err, "Invalid work", "clusterResourcePlacement", crpKObj, "clusterResourceSnapshot", klog.KObj(latestResourceSnapshot), "work", workKObj)
			return nil, nil, controller.NewUnexpectedBehaviorError(err)
		} else if indexFromWork < resourceIndex {
			// The work is pointing to the old resourceSnapshot.
			// it means the rollout controller has not updated the binding yet or work generator has not handled this work yet.
			oldWorkCounter++
		} else { // indexFromWork = resourceIndex
			// The work is pointing to the latest resourceSnapshot.
			newWorkCounter++
			// We only build the work applied status on the new works.
			isPending, failedManifests := buildFailedResourcePlacements(&workList.Items[i])
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
		"numberOfOldWorks", oldWorkCounter, "numberOfNewWorks", newWorkCounter,
		"numberOfPendingWorks", pendingWorkCounter, "numberOfFailedResources", len(failedResourcePlacements))

	status.FailedResourcePlacements = failedResourcePlacements

	desiredWorkCounter, err := annotations.ExtractNumberOfResourceSnapshotsFromResourceSnapshot(latestResourceSnapshot)
	if err != nil {
		klog.ErrorS(err, "Master resource snapshot has invalid numberOfResourceSnapshots annotation", "clusterResourcePlacement", crpKObj, "clusterResourceSnapshot", klog.KObj(latestResourceSnapshot))
		return nil, nil, controller.NewUnexpectedBehaviorError(err)
	}
	isSync, workSynchronizedCondition, err := buildWorkSynchronizedCondition(crp, desiredWorkCounter, newWorkCounter, oldWorkCounter)
	if err != nil {
		return nil, nil, err
	}
	meta.SetStatusCondition(&status.Conditions, workSynchronizedCondition)

	workAppliedCondition := buildWorkAppliedCondition(crp, !isSync || pendingWorkCounter > 0, len(failedResourcePlacements) > 0)
	meta.SetStatusCondition(&status.Conditions, workAppliedCondition)
	return &workSynchronizedCondition, &workAppliedCondition, nil
}

func buildWorkSynchronizedCondition(crp *fleetv1beta1.ClusterResourcePlacement, desiredWorkCounter, newWorkCounter, oldWorkCounter int) (bool, metav1.Condition, error) {
	if desiredWorkCounter == newWorkCounter && oldWorkCounter == 0 {
		// We have created all the works according to the latest resource snapshot.
		return true, metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
			Reason:             workSynchronizeSucceededReason,
			Message:            "Successfully Synchronized work(s) for placement",
			ObservedGeneration: crp.Generation,
		}, nil
	}
	return false, metav1.Condition{
		Status:             metav1.ConditionFalse,
		Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
		Reason:             workSynchronizePendingReason,
		Message:            "In the process of synchronizing or operation is blocked by the rollout strategy ",
		ObservedGeneration: crp.Generation,
	}, nil
}

func buildWorkAppliedCondition(crp *fleetv1beta1.ClusterResourcePlacement, hasPendingWork, hasFailedResource bool) metav1.Condition {
	// hasPendingWork could be true when
	// - the works have not been created in the hub cluster.
	// - the works have not been processed by the member cluster.
	if hasPendingWork {
		return metav1.Condition{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
			Reason:             resourceApplyPendingReason,
			Message:            "Works need to be synchronized on the hub cluster or there are still manifests pending to be processed by the member cluster",
			ObservedGeneration: crp.Generation,
		}
	}

	if hasFailedResource {
		return metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
			Reason:             resourceApplyFailedReason,
			Message:            "Failed to apply manifests, please check the `failedResourcePlacements` status",
			ObservedGeneration: crp.Generation,
		}
	}

	return metav1.Condition{ // if !hasFailedResource && !hasPendingWork
		Status:             metav1.ConditionTrue,
		Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
		Reason:             resourceApplySucceededReason,
		Message:            "Successfully applied resources",
		ObservedGeneration: crp.Generation,
	}
}

// buildFailedResourcePlacements returns if work is pending or not.
// If the work has been applied, it returns the list of failed resources.
func buildFailedResourcePlacements(work *workv1alpha1.Work) (isPending bool, res []fleetv1beta1.FailedResourcePlacement) {
	// check the overall condition
	workKObj := klog.KObj(work)
	appliedCond := meta.FindStatusCondition(work.Status.Conditions, workapi.ConditionTypeApplied)
	if appliedCond == nil {
		klog.V(3).InfoS("The work is never picked up by the member cluster", "work", workKObj)
		return true, nil
	}
	if appliedCond.ObservedGeneration < work.GetGeneration() || appliedCond.Status == metav1.ConditionUnknown {
		klog.V(3).InfoS("The update of the work is not picked up by the member cluster yet", "work", workKObj, "workGeneration", work.GetGeneration(), "appliedGeneration", appliedCond.ObservedGeneration, "status", appliedCond.Status)
		return true, nil
	}

	if appliedCond.Status == metav1.ConditionTrue {
		klog.V(3).InfoS("The work is applied successfully by the member cluster", "work", workKObj, "workGeneration")
		return false, nil
	}

	res = make([]fleetv1beta1.FailedResourcePlacement, 0, len(work.Status.ManifestConditions))
	for _, manifestCondition := range work.Status.ManifestConditions {
		appliedCond = meta.FindStatusCondition(manifestCondition.Conditions, workapi.ConditionTypeApplied)
		// collect if there is an explicit fail
		if appliedCond != nil && appliedCond.Status != metav1.ConditionTrue {
			klog.V(2).InfoS("Find a failed to apply manifest",
				"manifestName", manifestCondition.Identifier.Name, "group", manifestCondition.Identifier.Group,
				"version", manifestCondition.Identifier.Version, "kind", manifestCondition.Identifier.Kind)

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
			res = append(res, failedManifest)
		}
	}
	return false, res
}
