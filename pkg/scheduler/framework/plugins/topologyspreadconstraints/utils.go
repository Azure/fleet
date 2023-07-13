/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package topologyspreadconstraints

import (
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

// prepareTopologySpreadConstraintsPluginState initializes the state for the plugin to use
// in the scheduling cycle.
//
// TO-DO (chenyu1): assign variables when needed.
func prepareTopologySpreadConstraintsPluginState(_ framework.CycleStatePluginReadWriter, _ *fleetv1beta1.ClusterSchedulingPolicySnapshot) (*pluginState, error) {
	// Not yet implemented.
	return &pluginState{}, nil
}
