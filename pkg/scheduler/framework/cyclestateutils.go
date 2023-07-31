/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// prepareScheduledOrBoundBindingsMap returns a map that allows quick lookup of whether a cluster
// already has a binding of the scheduled or bound state relevant to the current scheduling
// cycle.
func prepareScheduledOrBoundBindingsMap(scheduledOrBoundBindings ...[]*fleetv1beta1.ClusterResourceBinding) map[string]bool {
	bm := make(map[string]bool)

	for _, bindingSet := range scheduledOrBoundBindings {
		for _, binding := range bindingSet {
			bm[binding.Spec.TargetCluster] = true
		}
	}

	return bm
}

// prepareObsoleteBindingsMap returns a map that allows quick lookup of whether a cluster
// already has an obsolete binding relevant to the current scheduling cycle.
func prepareObsoleteBindingsMap(obsoleteBindings []*fleetv1beta1.ClusterResourceBinding) map[string]bool {
	bm := make(map[string]bool)

	for _, binding := range obsoleteBindings {
		bm[binding.Spec.TargetCluster] = true
	}

	return bm
}
