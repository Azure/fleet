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

package clusteraffinity

import (
	"k8s.io/apimachinery/pkg/api/resource"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

type observedMinMaxValues struct {
	min *resource.Quantity
	max *resource.Quantity
}

type pluginState struct {
	minMaxValuesByProperty map[string]observedMinMaxValues
}

// preparePluginState prepares a common state for easier queries of min. and max.
// observed values of properties (if applicable).
func preparePluginState(state framework.CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (*pluginState, error) {
	ps := &pluginState{
		minMaxValuesByProperty: make(map[string]observedMinMaxValues),
	}

	// Note that this function assumes that the scheduling policy must have at least one
	// enforceable preferred cluster affinity term, as guaranteed by its caller.

	var cs []clusterv1beta1.MemberCluster
	for tidx := range policy.Spec.Policy.Affinity.ClusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
		t := &policy.Spec.Policy.Affinity.ClusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution[tidx]
		if t.Preference.PropertySorter != nil {
			if cs == nil {
				// Do a lazy retrieval of the cluster list; as the copy has some overhead.
				cs = state.ListClusters()
			}

			n := t.Preference.PropertySorter.Name
			// Use pointers so that zero values can also be compared.
			var minQ, maxQ *resource.Quantity

			for cidx := range cs {
				c := &cs[cidx]
				q, err := retrievePropertyValueFrom(c, n)
				if err != nil {
					// An error has occurred when retrieving the property value from the cluster.
					//
					// Note that not having a specific property (i.e., a resource type is not supported
					// or a property is not available on a cluster) is not considered an error.
					return nil, err
				}
				if q == nil {
					// The property is not supported yet on the cluster.
					continue
				}

				if minQ == nil || q.Cmp(*minQ) < 0 {
					minQ = q
				}
				if maxQ == nil || q.Cmp(*maxQ) > 0 {
					maxQ = q
				}
			}

			ps.minMaxValuesByProperty[n] = observedMinMaxValues{
				// Note that there exists a special case where both extremums are nil; this can occur
				// when a property is specified in the preferred terms, yet none of the clusters supports
				// it.
				min: minQ,
				max: maxQ,
			}
		}
	}

	return ps, nil
}
