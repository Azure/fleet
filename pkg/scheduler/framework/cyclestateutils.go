/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// prepareScheduledOrBoundMap returns a map that allows quick lookup of whether a cluster
// already has a binding of the scheduled or bound state relevant to the current scheduling
// cycle.
func prepareScheduledOrBoundMap(scheduledOrBoundBindings ...[]*fleetv1beta1.ClusterResourceBinding) map[string]bool {
	scheduledOrBound := make(map[string]bool)

	for _, bindingSet := range scheduledOrBoundBindings {
		for _, binding := range bindingSet {
			scheduledOrBound[binding.Spec.TargetCluster] = true
		}
	}

	return scheduledOrBound
}
