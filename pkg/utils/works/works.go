/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package works features work related utility functions which could be shared between controllers.
package works

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/annotations"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/labels"
)

var (
	// We only include 100 failed resource placements even if there are more than 100.
	maxFailedResourcePlacementLimit = 100
)

// BuildStatusForCluster will list all the associated works with latest resourceSnapshots and build the work conditions.
func BuildStatusForCluster(ctx context.Context, cachedClient client.Client, crp *fleetv1beta1.ClusterResourcePlacement, latestResourceSnapshot *fleetv1beta1.ClusterResourceSnapshot, cluster string) ([]metav1.Condition, []fleetv1beta1.FailedResourcePlacement, error) {
	crpKObj := klog.KObj(crp)
	namespaceMatcher := client.InNamespace(fmt.Sprintf(utils.NamespaceNameFormat, cluster))
	workLabelMatcher := client.MatchingLabels{
		fleetv1beta1.CRPTrackingLabel: crp.Name,
	}
	workList := &workv1alpha1.WorkList{}

	if err := cachedClient.List(ctx, workList, workLabelMatcher, namespaceMatcher); err != nil {
		klog.ErrorS(err, "Failed to list all the work associated with the clusterResourcePlacement", "clusterResourcePlacement", crpKObj, "clusterName", cluster)
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

	failedResourcePlacements := make([]fleetv1beta1.FailedResourcePlacement, 0, 10) // preallocate the memory
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
		}
		isPending, failedManifests := buildFailedResourcePlacements(&workList.Items[i])
		if isPending {
			pendingWorkCounter++
		}
		if len(failedManifests) != 0 && len(failedResourcePlacements) <= maxFailedResourcePlacementLimit {
			failedResourcePlacements = append(failedResourcePlacements, failedManifests...)
		}
	}

	if len(failedResourcePlacements) > maxFailedResourcePlacementLimit {
		failedResourcePlacements = failedResourcePlacements[0:maxFailedResourcePlacementLimit]
	}
	klog.V(2).InfoS("Building the resourcePlacementStatus", "clusterResourcePlacement", crpKObj, "clusterName", cluster,
		"numberOfOldWorks", oldWorkCounter, "numberOfNewWorks", newWorkCounter,
		"numberOfPendingWorks", pendingWorkCounter, "numberOfFailedResources", len(failedResourcePlacements))

	desiredWorkCounter, err := annotations.ExtractNumberOfResourceSnapshotsFromResourceSnapshot(latestResourceSnapshot)
	if err != nil {
		klog.ErrorS(err, "Master resource snapshot has invalid numberOfResourceSnapshots annotation", "clusterResourcePlacement", crpKObj, "clusterResourceSnapshot", klog.KObj(latestResourceSnapshot))
		return nil, nil, controller.NewUnexpectedBehaviorError(err)
	}
	workSynchronizedCondition, err := buildWorkSynchronizedCondition(crp, desiredWorkCounter, newWorkCounter, oldWorkCounter)
	if err != nil {
		return nil, nil, err
	}
	if newWorkCounter+oldWorkCounter == 0 {
		// skip setting the applied work condition
		return []metav1.Condition{workSynchronizedCondition}, failedResourcePlacements, nil
	}
	workAppliedCondition := buildWorkAppliedCondition(crp, pendingWorkCounter > 0, len(failedResourcePlacements) > 0)
	return []metav1.Condition{workSynchronizedCondition, workAppliedCondition}, failedResourcePlacements, nil
}

func buildWorkSynchronizedCondition(_ *fleetv1beta1.ClusterResourcePlacement, _, _, _ int) (metav1.Condition, error) {
	return metav1.Condition{}, nil
}

func buildWorkAppliedCondition(_ *fleetv1beta1.ClusterResourcePlacement, _, _ bool) metav1.Condition {
	return metav1.Condition{}
}

// buildFailedResourcePlacements returns if work is pending or not.
// If the work has been applied, it returns the list of failed resources.
func buildFailedResourcePlacements(_ *workv1alpha1.Work) (isPending bool, res []fleetv1beta1.FailedResourcePlacement) {
	return false, nil
}
