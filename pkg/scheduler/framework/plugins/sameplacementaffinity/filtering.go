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

// Filter allows the plugin to connect to the Filter extension point in the scheduling framework.
func (p *Plugin) Filter(
	_ context.Context,
	state framework.CycleStatePluginReadWriter,
	_ *placementv1beta1.ClusterSchedulingPolicySnapshot,
	cluster *clusterv1beta1.MemberCluster,
) (status *framework.Status) {
	if !state.HasScheduledOrBoundBindingFor(cluster.Name) {
		// all done.
		return nil
	}

	reason := "resource placement has already been scheduled or bounded on the cluster"
	return framework.NewNonErrorStatus(framework.ClusterAlreadySelected, p.Name(), reason)
}
