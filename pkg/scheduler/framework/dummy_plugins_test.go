/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"context"
	"fmt"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	// dummyPluginTriggerTag is a label on policy snapshots, which triggers specific plugins when set,
	// to simulate different scenarios.
	dummyPluginTriggerTag = "trigger-tag"
)

// A no-op, dummy plugin which connects to all extension points.
type DummyAllPurposePlugin struct{}

// Name returns the name of the dummy plugin.
func (p *DummyAllPurposePlugin) Name() string {
	return dummyPluginName
}

// PostBatch implements the PostBatch interface for the dummy plugin.
func (p *DummyAllPurposePlugin) PostBatch(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (size int, status *Status) { //nolint:revive
	return 1, nil
}

// PreFilter implements the PreFilter interface for the dummy plugin.
func (p *DummyAllPurposePlugin) PreFilter(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (status *Status) { //nolint:revive
	return nil
}

// Filter implements the Filter interface for the dummy plugin.
func (p *DummyAllPurposePlugin) Filter(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) { //nolint:revive
	return nil
}

// PreScore implements the PreScore interface for the dummy plugin.
func (p *DummyAllPurposePlugin) PreScore(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (status *Status) { //nolint:revive
	return nil
}

// Score implements the Score interface for the dummy plugin.
func (p *DummyAllPurposePlugin) Score(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (score *ClusterScore, status *Status) { //nolint:revive
	return &ClusterScore{}, nil
}

// SetUpWithFramework is a no-op to satisfy the Plugin interface.
func (p *DummyAllPurposePlugin) SetUpWithFramework(handle Handle) {} // nolint:revive

// A dummy post batch plugin, which adjusts batch size limit by cluster names.
type dummyPostBatchPlugin struct{}

// Name returns the name of the dummy plugin.
func (p *dummyPostBatchPlugin) Name() string {
	return dummyPluginName
}

// PostBatch implements the PostBatch interface for the dummy plugin.
//
// It returns a status in accordance with the tag added to the policy.
func (p *dummyPostBatchPlugin) PostBatch(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (size int, status *Status) { //nolint:revive
	tag, ok := policy.Labels[dummyPluginTriggerTag]
	if !ok {
		return 0, FromError(fmt.Errorf("no %s label found", dummyPluginTriggerTag), p.Name())
	}

	switch tag {
	case "post-batch-success":
		return 1, nil
	case "post-batch-internal-error":
		return 0, FromError(fmt.Errorf("internal error"), p.Name())
	case "post-batch-skip":
		return 0, NewNonErrorStatus(Skip, p.Name())
	case "post-batch-cluster-unschedulable":
		// For testing purposes only; normally a post batch plugin will never yield such a status.
		return 0, NewNonErrorStatus(ClusterUnschedulable, p.Name())
	default:
		return 1, nil
	}
}
