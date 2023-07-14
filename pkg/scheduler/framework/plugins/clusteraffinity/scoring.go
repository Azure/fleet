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
	_ *fleetv1beta1.ClusterSchedulingPolicySnapshot,
) (status *framework.Status) {
	// TODO not implemented.
	_, _ = p.readPluginState(state)
	return nil
}

// Score allows the plugin to connect to the Score extension point in the scheduling framework.
func (p *Plugin) Score(
	_ context.Context,
	_ framework.CycleStatePluginReadWriter,
	_ *fleetv1beta1.ClusterSchedulingPolicySnapshot,
	_ *fleetv1beta1.MemberCluster,
) (score *framework.ClusterScore, status *framework.Status) {
	// TODO not implemented.
	return nil, nil
}
