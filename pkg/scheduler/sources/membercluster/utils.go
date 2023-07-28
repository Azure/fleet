/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// isCRPFullyScheduled returns whether a CRP is fully scheduled.
func isCRPFullyScheduled(crp *fleetv1beta1.ClusterResourcePlacement) bool {
	// Check the scheduled condition on the CRP to determine if it is fully scheduled.
	//
	// Here the controller checks the status rather than listing all the bindings and verify
	// if the count matches with the CRP spec as the former approach has less overhead and
	// (more importantly) avoids leaking scheduler-specific logic into this controller. The
	// trade-off is that the controller may consider some fully scheduled CRPs as not fully
	// scheduled, if the CRP-side controller(s) cannot update the CRP status in a timely
	// manner.

	scheduledCondition := meta.FindStatusCondition(crp.Status.Conditions, string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType))
	if scheduledCondition == nil {
		// The scheduled condition is absent.
		return false
	}

	if scheduledCondition.Status != metav1.ConditionTrue || scheduledCondition.ObservedGeneration != crp.Generation {
		// The CRP is not fully scheduled, or its scheduled condition is out of date.
		return false
	}

	return true
}

// classifyCRPs returns a list of CRPs that are affected by cluster side changes in case 1a) and
// 1b).
func classifyCRPs(crps []fleetv1beta1.ClusterResourcePlacement) (toProcess []fleetv1beta1.ClusterResourcePlacement) {
	// Pre-allocate array.
	toProcess = make([]fleetv1beta1.ClusterResourcePlacement, 0, len(crps))

	for idx := range crps {
		crp := crps[idx]
		switch {
		case crp.Spec.Policy == nil:
			// CRPs with no placement policy specified are considered to be of the PickAll placement
			// type and are affected by cluster side changes in case 1a) and 1b).
			toProcess = append(toProcess, crp)
		case len(crp.Spec.Policy.ClusterNames) != 0:
			// Note that any CRP with a fixed set of target clusters will be automatically assigned
			// the PickAll placement type, as it is the default value.
			if !isCRPFullyScheduled(&crp) {
				// Any CRP with an non-empty list of target cluster names can be affected by cluster
				// side changes in case 1b), if it is not yet fully scheduled.
				toProcess = append(toProcess, crp)
			}
		case crp.Spec.Policy.PlacementType == fleetv1beta1.PickAllPlacementType:
			// CRPs of the PickAll placement type are affected by cluster side changes in case 1a)
			// and 1b).
			toProcess = append(toProcess, crp)
		case !isCRPFullyScheduled(&crp):
			// CRPs of the PickN placement type, which have not been fully scheduled, are affected
			// by cluster side changes in case 1a) and 1b).
			toProcess = append(toProcess, crp)
		}
	}

	return toProcess
}
