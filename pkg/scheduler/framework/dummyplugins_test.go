/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"context"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	dummyAllPurposePluginName      = "dummyAllPurposePlugin"
	dummyPostBatchPluginNameFormat = "dummyPostBatchPlugin-%d"
	dummyPreFilterPluginNameFormat = "dummyPreFilterPlugin-%d"
	dummyFilterPluginNameFormat    = "dummyFilterPlugin-%d"
)

// A no-op, dummy plugin which connects to all extension points.
type DummyAllPurposePlugin struct{}

// Name returns the name of the dummy plugin.
func (p *DummyAllPurposePlugin) Name() string {
	return dummyAllPurposePluginName
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

// A dummy post batch plugin.
type dummyPostBatchPlugin struct {
	name   string
	runner func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (size int, status *Status)
}

// Name returns the name of the dummy plugin.
func (p *dummyPostBatchPlugin) Name() string {
	return p.name
}

// PostBatch implements the PostBatch interface for the dummy plugin.
func (p *dummyPostBatchPlugin) PostBatch(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (size int, status *Status) { //nolint:revive
	return p.runner(ctx, state, policy)
}

// SetUpWithFramework is a no-op to satisfy the Plugin interface.
func (p *dummyPostBatchPlugin) SetUpWithFramework(handle Handle) {} // nolint:revive

// A dummy pre-filter plugin.
type dummyPreFilterPlugin struct {
	name   string
	runner func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (status *Status)
}

// Name returns the name of the dummy plugin.
func (p *dummyPreFilterPlugin) Name() string {
	return p.name
}

// PreFilter implements the PreFilter interface for the dummy plugin.
func (p *dummyPreFilterPlugin) PreFilter(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (status *Status) { //nolint:revive
	return p.runner(ctx, state, policy)
}

// SetUpWithFramework is a no-op to satisfy the Plugin interface.
func (p *dummyPreFilterPlugin) SetUpWithFramework(handle Handle) {} // nolint:revive

// A dummy filter plugin.
type dummyFilterPlugin struct {
	name   string
	runner func(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status)
}

// Name returns the name of the dummy plugin.
func (d *dummyFilterPlugin) Name() string {
	return d.name
}

// Filter implements the Filter interface for the dummy plugin.
func (d *dummyFilterPlugin) Filter(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) { //nolint:revive
	return d.runner(ctx, state, policy, cluster)
}

// SetUpWithFramework is a no-op to satisfy the Plugin interface.
func (p *dummyFilterPlugin) SetUpWithFramework(handle Handle) {} // nolint: revive
