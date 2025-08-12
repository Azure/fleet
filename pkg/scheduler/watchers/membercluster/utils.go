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

package membercluster

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
)

// isPlacementFullyScheduled returns whether a placement is fully scheduled.
func isPlacementFullyScheduled(placement fleetv1beta1.PlacementObj) bool {
	// Check the scheduled condition on the placement to determine if it is fully scheduled.
	//
	// Here the controller checks the status rather than listing all the bindings and verify
	// if the count matches with the placement spec as the former approach has less overhead and
	// (more importantly) avoids leaking scheduler-specific logic into this controller. The
	// trade-off is that the controller may consider some fully scheduled placements as not fully
	// scheduled, if the placement-side controller(s) cannot update the placement status in a timely
	// manner.

	var scheduledCondition *metav1.Condition
	if placement.GetNamespace() == "" {
		// Find CRP scheduled condition.
		scheduledCondition = meta.FindStatusCondition(placement.GetPlacementStatus().Conditions, string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType))
	} else {
		// Find RP scheduled condition.
		scheduledCondition = meta.FindStatusCondition(placement.GetPlacementStatus().Conditions, string(fleetv1beta1.ResourcePlacementScheduledConditionType))
	}
	// Check if the placement is fully scheduled, or its scheduled condition is out of date.
	return condition.IsConditionStatusTrue(scheduledCondition, placement.GetGeneration())
}

// classifyPlacements returns a list of placements that are affected by cluster side changes in case 1a) and
// 1b).
func classifyPlacements(placements []fleetv1beta1.PlacementObj) (toProcess []fleetv1beta1.PlacementObj) {
	// Pre-allocate array.
	toProcess = make([]fleetv1beta1.PlacementObj, 0, len(placements))

	for idx := range placements {
		placement := placements[idx]
		switch {
		case placement.GetPlacementSpec().Policy == nil:
			// Placements with no placement policy specified are considered to be of the PickAll placement
			// type and are affected by cluster side changes in case 1a) and 1b).
			toProcess = append(toProcess, placement)
		case placement.GetPlacementSpec().Policy.PlacementType == fleetv1beta1.PickFixedPlacementType:
			if !isPlacementFullyScheduled(placement) {
				// Any Placement with an non-empty list of target cluster names can be affected by cluster
				// side changes in case 1b), if it is not yet fully scheduled.
				toProcess = append(toProcess, placement)
			}
		case placement.GetPlacementSpec().Policy.PlacementType == fleetv1beta1.PickAllPlacementType:
			// Placements of the PickAll placement type are affected by cluster side changes in case 1a)
			// and 1b).
			toProcess = append(toProcess, placement)
		case !isPlacementFullyScheduled(placement):
			// Placements of the PickN placement type, which have not been fully scheduled, are affected
			// by cluster side changes in case 1a) and 1b) listed in the Reconcile func.
			toProcess = append(toProcess, placement)
		}
	}

	return toProcess
}

// convertCRPArrayToPlacementObjs converts a slice of ClusterResourcePlacement items to PlacementObj array.
func convertCRPArrayToPlacementObjs(crps []fleetv1beta1.ClusterResourcePlacement) []fleetv1beta1.PlacementObj {
	placements := make([]fleetv1beta1.PlacementObj, len(crps))
	for i := range crps {
		placements[i] = &crps[i]
	}
	return placements
}

// convertRPArrayToPlacementObjs converts a slice of ResourcePlacement items to PlacementObj array.
func convertRPArrayToPlacementObjs(rps []fleetv1beta1.ResourcePlacement) []fleetv1beta1.PlacementObj {
	placements := make([]fleetv1beta1.PlacementObj, len(rps))
	for i := range rps {
		placements[i] = &rps[i]
	}
	return placements
}
