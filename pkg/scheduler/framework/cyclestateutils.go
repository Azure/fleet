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

package framework

import (
	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
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
