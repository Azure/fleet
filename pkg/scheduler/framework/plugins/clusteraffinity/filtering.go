package clusteraffinity

import (
	"context"
	"fmt"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

const (
	requiredAffinityViolationReasonTemplate = "none of the nonempty required cluster affinity term (total number: %d) is matched"
)

// PreFilter allows the plugin to connect to the PreFilter extension point in the scheduling
// framework.
//
// Note that the scheduler will not run this extension point in parallel.
func (p *Plugin) PreFilter(
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
		// (Filter).
		return framework.NewNonErrorStatus(framework.Skip, p.Name(), "no cluster affinity term is present")
	}

	// Prepare some common states for future use. This helps avoid the cost of repeatedly
	// calculating the same states at each extension point.
	ps, err := preparePluginState(policy)
	if err != nil {
		return framework.FromError(err, p.Name(), "failed to prepare plugin state")
	}

	// Save the plugin state.
	state.Write(framework.StateKey(p.Name()), ps)

	if len(ps.requiredAffinityTerms) == 0 {
		// There are no required affinity terms to enforce; skip.
		//
		// Note that this will lead the scheduler to skip this plugin in the next stage
		// (Filter).
		return framework.NewNonErrorStatus(framework.Skip, p.Name(), "no required cluster affinity term is present or all of the terms are empty")
	}

	// All done.
	return nil
}

// Filter allows the plugin to connect to the Filter extension point in the scheduling framework.
func (p *Plugin) Filter(
	_ context.Context,
	state framework.CycleStatePluginReadWriter,
	_ *fleetv1beta1.ClusterSchedulingPolicySnapshot,
	cluster *fleetv1beta1.MemberCluster,
) (status *framework.Status) {
	// Read the plugin state.
	ps, err := p.readPluginState(state)
	if err != nil {
		// This branch should never be reached, as for any policy with present required cluster affinity terms, a common
		// plugin state has been set at the PreFilter extension point.
		return framework.FromError(err, p.Name(), "failed to read plugin state")
	}

	if ps.requiredAffinityTerms.Matches(cluster) {
		// all done.
		return nil
	}
	// If the requiredAffinityTerms is 0, the prefilter should skip the filter state.
	// When it reaches to this step, it means the cluster does not match any requiredAffinityTerm.
	reason := fmt.Sprintf(requiredAffinityViolationReasonTemplate, len(ps.requiredAffinityTerms))
	return framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), reason)
}
