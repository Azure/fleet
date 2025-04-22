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

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
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
	// Check if the CRP is fully scheduled, or its scheduled condition is out of date.
	return condition.IsConditionStatusTrue(scheduledCondition, crp.Generation)
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
		case crp.Spec.Policy.PlacementType == fleetv1beta1.PickFixedPlacementType:
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
			// by cluster side changes in case 1a) and 1b) listed in the Reconcile func.
			toProcess = append(toProcess, crp)
		}
	}

	return toProcess
}
