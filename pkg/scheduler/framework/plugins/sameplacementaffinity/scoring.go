/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package sameplacementaffinity

import (
	"context"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

// Score allows the plugin to connect to the Score extension point in the scheduling framework.
func (p *Plugin) Score(
	_ context.Context,
	state framework.CycleStatePluginReadWriter,
	_ *placementv1beta1.ClusterSchedulingPolicySnapshot,
	cluster *clusterv1beta1.MemberCluster,
) (score *framework.ClusterScore, status *framework.Status) {
	if state.HasObsoleteBindingFor(cluster.Name) {
		return &framework.ClusterScore{ObsoletePlacementAffinityScore: 1}, nil
	}
	// All done.
	return &framework.ClusterScore{ObsoletePlacementAffinityScore: 0}, nil
}
