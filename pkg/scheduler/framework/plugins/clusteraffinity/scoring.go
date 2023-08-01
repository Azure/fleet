/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusteraffinity

import (
	"context"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

// PreScore allows the plugin to connect to the PreScore extension point in the scheduling
// framework.
func (p *Plugin) PreScore(
	_ context.Context,
	state framework.CycleStatePluginReadWriter,
	policy *fleetv1beta1.ClusterSchedulingPolicySnapshot,
) (status *framework.Status) {
	noClusterAffinity := policy.Spec.Policy == nil ||
		policy.Spec.Policy.Affinity == nil ||
		policy.Spec.Policy.Affinity.ClusterAffinity == nil

	if noClusterAffinity {
		// There are no clusterAffinity filter to enforce; skip.
		//
		// Note that this will lead the scheduler to skip this plugin in the next stage
		// (Score).
		return framework.NewNonErrorStatus(framework.Skip, p.Name(), "no cluster affinity term is present")
	}

	// Read the plugin state.
	ps, err := p.readPluginState(state)
	if err != nil {
		// This branch should never be reached, as for any policy with present topology spread
		// constraints, a common plugin state has been set at the PreFilter extension point.
		return framework.FromError(err, p.Name(), "failed to read plugin state")
	}

	if len(ps.preferredAffinityTerms) == 0 {
		// There are no preferred affinity terms to enforce; skip.
		//
		// Note that this will lead the scheduler to skip this plugin in the next stage
		// (Score).
		return framework.NewNonErrorStatus(framework.Skip, p.Name(), "no preferred cluster affinity term is present or all of the terms are empty")
	}

	// All done.
	return nil
}

// Score allows the plugin to connect to the Score extension point in the scheduling framework.
func (p *Plugin) Score(
	_ context.Context,
	state framework.CycleStatePluginReadWriter,
	_ *fleetv1beta1.ClusterSchedulingPolicySnapshot,
	cluster *fleetv1beta1.MemberCluster,
) (score *framework.ClusterScore, status *framework.Status) {
	// Read the plugin state.
	ps, err := p.readPluginState(state)
	if err != nil {
		// This branch should never be reached, as for any policy with present cluster affinity terms, a common plugin
		// state has been set at the PreFilter extension point.
		return nil, framework.FromError(err, p.Name(), "failed to read plugin state")
	}
	score = &framework.ClusterScore{
		AffinityScore: int(ps.preferredAffinityTerms.Score(cluster)),
	}
	// All done.
	return score, nil
}
