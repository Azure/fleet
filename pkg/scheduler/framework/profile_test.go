/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	dummyProfileName = "dummyProfile"
	dummyPluginName  = "dummyAllPurposePlugin"
)

// A no-op, dummy plugin which connects to all extension points.
type DummyAllPurposePlugin struct{}

// Name returns the name of the dummy plugin.
func (p *DummyAllPurposePlugin) Name() string {
	return dummyPluginName
}

// PostBatch implements the PostBatch interface for the dummy plugin.
func (p *DummyAllPurposePlugin) PostBatch(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.SchedulingPolicySnapshot) (size int, status *Status) { //nolint:revive
	return 1, nil
}

// PreFilter implements the PreFilter interface for the dummy plugin.
func (p *DummyAllPurposePlugin) PreFilter(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.SchedulingPolicySnapshot) (status *Status) { //nolint:revive
	return nil
}

// Filter implements the Filter interface for the dummy plugin.
func (p *DummyAllPurposePlugin) Filter(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.SchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status) { //nolint:revive
	return nil
}

// PreScore implements the PreScore interface for the dummy plugin.
func (p *DummyAllPurposePlugin) PreScore(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.SchedulingPolicySnapshot) (status *Status) { //nolint:revive
	return nil
}

// Score implements the Score interface for the dummy plugin.
func (p *DummyAllPurposePlugin) Score(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.SchedulingPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (score *ClusterScore, status *Status) { //nolint:revive
	return &ClusterScore{}, nil
}

// SetUpWithFramework is a no-op to satisfy the Plugin interface.
func (p *DummyAllPurposePlugin) SetUpWithFramework(handle Handle) {} // nolint:revive

// TestProfile tests the basic ops of a Profile.
func TestProfile(t *testing.T) {
	profile := NewProfile(dummyProfileName)

	dummyAllPurposePlugin := &DummyAllPurposePlugin{}
	dummyPlugin := Plugin(dummyAllPurposePlugin)

	profile.WithPostBatchPlugin(dummyAllPurposePlugin)
	profile.WithPreFilterPlugin(dummyAllPurposePlugin)
	profile.WithFilterPlugin(dummyAllPurposePlugin)
	profile.WithPreScorePlugin(dummyAllPurposePlugin)
	profile.WithScorePlugin(dummyAllPurposePlugin)

	wantProfile := &Profile{
		name:             dummyProfileName,
		postBatchPlugins: []PostBatchPlugin{dummyAllPurposePlugin},
		preFilterPlugins: []PreFilterPlugin{dummyAllPurposePlugin},
		filterPlugins:    []FilterPlugin{dummyAllPurposePlugin},
		preScorePlugins:  []PreScorePlugin{dummyAllPurposePlugin},
		scorePlugins:     []ScorePlugin{dummyAllPurposePlugin},
		registeredPlugins: map[string]Plugin{
			dummyPluginName: dummyPlugin,
		},
	}

	if !cmp.Equal(profile, wantProfile, cmp.AllowUnexported(Profile{})) {
		t.Fatalf("NewProfile() = %v, want %v", profile, wantProfile)
	}
}
