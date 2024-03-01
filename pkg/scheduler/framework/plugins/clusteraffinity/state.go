/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
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

func preparePluginState(state framework.CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (*pluginState, error) {
	ps := &pluginState{
		minMaxValuesByProperty: make(map[string]observedMinMaxValues),
	}

	// Verify if the policy features any property that requires sorting.
	noPreferredClusterAffinityTerms := (policy == nil ||
		policy.Spec.Policy == nil ||
		policy.Spec.Policy.Affinity == nil ||
		policy.Spec.Policy.Affinity.ClusterAffinity == nil ||
		len(policy.Spec.Policy.Affinity.ClusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution) == 0)
	if noPreferredClusterAffinityTerms {
		// There are no preferred cluster affinity terms specified in the scheduling policy;
		// Normally this should never occur as an inspection has been performed in the upper level.
		return ps, nil
	}

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

			for sidx := range cs {
				c := &cs[sidx]
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
