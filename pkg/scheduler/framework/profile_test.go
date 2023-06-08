/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"context"
	"testing"

	fleetv1 "go.goms.io/fleet/apis/v1"
)

const (
	dummyPluginName = "dummyAllPurposePlugin"
)

// A no-op, dummy plugin which connects to all extension points.
type DummyAllPurposePlugin struct{}

// Name returns the name of the dummy plugin.
func (p *DummyAllPurposePlugin) Name() string {
	return dummyPluginName
}

// PostBatch implements the PostBatch interface for the dummy plugin.
func (p *DummyAllPurposePlugin) PostBatch(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1.ClusterPolicySnapshot) (size int, status *Status) { //nolint:revive
	return 1, nil
}

// PreFilter implements the PreFilter interface for the dummy plugin.
func (p *DummyAllPurposePlugin) PreFilter(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1.ClusterPolicySnapshot) (status *Status) { //nolint:revive
	return nil
}

// Filter implements the Filter interface for the dummy plugin.
func (p *DummyAllPurposePlugin) Filter(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1.ClusterPolicySnapshot, cluster *fleetv1.MemberCluster) (status *Status) { //nolint:revive
	return nil
}

// PreScore implements the PreScore interface for the dummy plugin.
func (p *DummyAllPurposePlugin) PreScore(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1.ClusterPolicySnapshot) (status *Status) { //nolint:revive
	return nil
}

// Score implements the Score interface for the dummy plugin.
func (p *DummyAllPurposePlugin) Score(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1.ClusterPolicySnapshot, cluster *fleetv1.MemberCluster) (score *ClusterScore, status *Status) { //nolint:revive
	return &ClusterScore{}, nil
}

// TestProfile tests the basic ops of a Profile.
func TestProfile(t *testing.T) {
	profileName := "testProfile"
	profile := NewProfile(profileName)

	dummyAllPurposePlugin := &DummyAllPurposePlugin{}
	dummyPlugin := Plugin(dummyAllPurposePlugin)

	profile.WithPostBatchPlugin(dummyAllPurposePlugin)
	profile.WithPreFilterPlugin(dummyAllPurposePlugin)
	profile.WithFilterPlugin(dummyAllPurposePlugin)
	profile.WithPreScorePlugin(dummyAllPurposePlugin)
	profile.WithScorePlugin(dummyAllPurposePlugin)

	// Same plugin should be registered only once, even if it connects to multiple extension points.
	if len(profile.registeredPlugins) != 1 {
		t.Fatalf("registerPlugins len() = %d, want %d", len(profile.registeredPlugins), 1)
	}
	if pl, ok := profile.registeredPlugins[dummyPluginName]; !ok || pl != dummyPlugin {
		t.Fatalf("registeredPlugins[%s] = %v, %t, want %v, %t", dummyPluginName, pl, ok, dummyPlugin, true)
	}

	if len(profile.postBatchPlugins) != 1 {
		t.Fatalf("postBatchPlugins len() = %d, want %d", len(profile.postBatchPlugins), 1)
	}
	if pl := profile.postBatchPlugins[0]; pl != dummyPlugin {
		t.Fatalf("postBatchPlugins[0] = %v, want %v", pl, dummyPlugin)
	}

	if len(profile.preFilterPlugins) != 1 {
		t.Fatalf("preFilterPlugins len() = %d, want %d", len(profile.preFilterPlugins), 1)
	}
	if pl := profile.preFilterPlugins[0]; pl != dummyPlugin {
		t.Fatalf("preFilterPlugins[0] = %v, want %v", pl, dummyPlugin)
	}

	if len(profile.filterPlugins) != 1 {
		t.Fatalf("filterPlugins len() = %d, want %d", len(profile.filterPlugins), 1)
	}
	if pl := profile.filterPlugins[0]; pl != dummyPlugin {
		t.Fatalf("filterPlugins[0] = %v, want %v", pl, dummyPlugin)
	}

	if len(profile.preScorePlugins) != 1 {
		t.Fatalf("preScorePlugins len() = %d, want %d", len(profile.preScorePlugins), 1)
	}
	if pl := profile.preScorePlugins[0]; pl != dummyPlugin {
		t.Fatalf("preScorePlugins[0] = %v, want %v", pl, dummyPlugin)
	}

	if len(profile.scorePlugins) != 1 {
		t.Fatalf("scorePlugins len() = %d, want %d", len(profile.scorePlugins), 1)
	}
	if pl := profile.scorePlugins[0]; pl != dummyPlugin {
		t.Fatalf("scorePlugins[0] = %v, want %v", pl, dummyPlugin)
	}
}
