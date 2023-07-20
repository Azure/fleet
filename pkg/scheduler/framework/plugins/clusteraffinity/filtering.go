package clusteraffinity

import (
	"context"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

// PreFilter allows the plugin to connect to the PreFilter extension point in the scheduling
// framework.
//
// Note that the scheduler will not run this extension point in parallel.
func (p *Plugin) PreFilter(
	_ context.Context,
	_ framework.CycleStatePluginReadWriter,
	policy *fleetv1beta1.ClusterSchedulingPolicySnapshot,
) (status *framework.Status) {
	// TODO not implemented yet
	_, _ = preparePluginState(policy)
	return nil
}

// Filter allows the plugin to connect to the Filter extension point in the scheduling framework.
func (p *Plugin) Filter(
	_ context.Context,
	_ framework.CycleStatePluginReadWriter,
	_ *fleetv1beta1.ClusterSchedulingPolicySnapshot,
	_ *fleetv1beta1.MemberCluster,
) (status *framework.Status) {
	// TODO not implemented yet
	return nil
}
