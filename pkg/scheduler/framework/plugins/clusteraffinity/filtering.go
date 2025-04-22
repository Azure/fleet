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
	"context"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

// PreFilter allows the plugin to connect to the PreFilter extension point in the scheduling framework.
func (p *Plugin) PreFilter(
	_ context.Context,
	_ framework.CycleStatePluginReadWriter,
	ps *placementv1beta1.ClusterSchedulingPolicySnapshot,
) (status *framework.Status) {
	noRequiredClusterAffinityTerms := ps.Spec.Policy == nil ||
		ps.Spec.Policy.Affinity == nil ||
		ps.Spec.Policy.Affinity.ClusterAffinity == nil ||
		ps.Spec.Policy.Affinity.ClusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil ||
		len(ps.Spec.Policy.Affinity.ClusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution.ClusterSelectorTerms) == 0
	if noRequiredClusterAffinityTerms {
		// There are no required cluster affinity terms to enforce; consider all clusters
		// eligible for resource placement in the scope of this plugin.
		//
		// Note that this will set the cluster to skip the Filter stage for all clusters.
		return framework.NewNonErrorStatus(framework.Skip, p.Name(), "no required cluster affinity terms to enforce")
	}

	return nil
}

// Filter allows the plugin to connect to the Filter extension point in the scheduling framework.
func (p *Plugin) Filter(
	_ context.Context,
	_ framework.CycleStatePluginReadWriter,
	ps *placementv1beta1.ClusterSchedulingPolicySnapshot,
	cluster *clusterv1beta1.MemberCluster,
) (status *framework.Status) {
	// Note that this extension point assumes that previous extension point (PreFilter) has
	// guaranteed that if scheduling policy reaches this stage, it must have at least one
	// required cluster affinity term to enforce.

	for idx := range ps.Spec.Policy.Affinity.ClusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution.ClusterSelectorTerms {
		t := &ps.Spec.Policy.Affinity.ClusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution.ClusterSelectorTerms[idx]
		r := clusterRequirement(*t)
		isMatched, err := r.Matches(cluster)
		if err != nil {
			// An error has occurred when matching the cluster against a required affinity term.
			return framework.FromError(err, p.Name(), "failed to match the cluster against a required affinity term")
		}
		if isMatched {
			// The cluster matches with the required affinity term; mark it as eligible for
			// resource placement.
			//
			// Note that when there are multiple cluster selector terms, the results are OR'd.
			return nil
		}
	}

	// The cluster does not match any of the required affinity terms; consider it ineligible for resource
	// placement in the scope of this plugin.
	return framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), "cluster does not match with any of the required cluster affinity terms")
}
