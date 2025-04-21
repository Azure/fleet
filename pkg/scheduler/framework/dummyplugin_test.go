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

package framework

import (
	"context"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	dummyAllPurposePluginNameFormat = "dummyAllPurposePlugin-%d"
)

// A no-op, dummy plugin which connects to all extension points.
type DummyAllPurposePlugin struct {
	name            string
	postBatchRunner func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (size int, status *Status)
	preFilterRunner func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status)
	filterRunner    func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status)
	preScoreRunner  func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status)
	scoreRunner     func(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status)
}

// Check that the dummy plugin implements all the interfaces at compile time.
//
// Note that with the checks below the implementation of the Plugin interface is already implied.
var _ PostBatchPlugin = &DummyAllPurposePlugin{}
var _ PreFilterPlugin = &DummyAllPurposePlugin{}
var _ FilterPlugin = &DummyAllPurposePlugin{}
var _ PreScorePlugin = &DummyAllPurposePlugin{}
var _ ScorePlugin = &DummyAllPurposePlugin{}

// Name returns the name of the dummy plugin.
func (p *DummyAllPurposePlugin) Name() string {
	return p.name
}

// PostBatch implements the PostBatch interface for the dummy plugin.
func (p *DummyAllPurposePlugin) PostBatch(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (size int, status *Status) { //nolint:revive
	return p.postBatchRunner(ctx, state, policy)
}

// PreFilter implements the PreFilter interface for the dummy plugin.
func (p *DummyAllPurposePlugin) PreFilter(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) { //nolint:revive
	return p.preFilterRunner(ctx, state, policy)
}

// Filter implements the Filter interface for the dummy plugin.
func (p *DummyAllPurposePlugin) Filter(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (status *Status) { //nolint:revive
	return p.filterRunner(ctx, state, policy, cluster)
}

// PreScore implements the PreScore interface for the dummy plugin.
func (p *DummyAllPurposePlugin) PreScore(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (status *Status) { //nolint:revive
	return p.preScoreRunner(ctx, state, policy)
}

// Score implements the Score interface for the dummy plugin.
func (p *DummyAllPurposePlugin) Score(ctx context.Context, state CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot, cluster *clusterv1beta1.MemberCluster) (score *ClusterScore, status *Status) { //nolint:revive
	return p.scoreRunner(ctx, state, policy, cluster)
}

// SetUpWithFramework is a no-op to satisfy the Plugin interface.
func (p *DummyAllPurposePlugin) SetUpWithFramework(handle Handle) {} // nolint:revive
