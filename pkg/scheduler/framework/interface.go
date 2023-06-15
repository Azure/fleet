/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"context"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// Plugin is the interface which all scheduler plugins should implement.
type Plugin interface {
	Name() string
	// TO-DO （chenyu1): add a method to help plugin set up the framework as needed.
}

// PostBatchPlugin is the interface which all plugins that would like to run at the PostBatch
// extension point should implement.
type PostBatchPlugin interface {
	Plugin

	// PostBatch runs after the scheduler has determined the number of bindings to create;
	// a plugin which registers at this extension point must return one of the follows:
	// * A Success status with a new batch size; or
	// * A Skip status, if no changes in batch size is needed; or
	// * An InternalError status, if an expected error has occurred
	PostBatch(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (size int, status *Status)
}

// PreFilterPlugin is the interface which all plugins that would like to run at the PreFilter
// extension point should implement.
type PreFilterPlugin interface {
	Plugin

	// PreFilter runs before the scheduler enters the Filter stage; a plugin may perform
	// some setup at this extension point, such as caching the results that will be used in
	// following Filter calls, and/or run some checks to determine if it should be skipped in
	// the Filter stage.
	//
	// A plugin which registers at this extension point must return one of the follows:
	// * A Success status, if the plugin should run at the Filter stage; or
	// * A Skip status, if the plugin should be skipped at the Filter stage; or
	// * An InternalError status, if an expected error has occurred
	PreFilter(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (status *Status)
}

// FilterPlugin is the interface which all plugins that would like to run at the Filter
// extension point should implement.
type FilterPlugin interface {
	Plugin

	// Filter runs at the Filter stage, to check if a placement can be bound to a specific cluster.
	// A plugin which registers at this extension point must return one of the follows:
	// * A Success status, if the placement can be bound to the cluster; or
	// * A ClusterUnschedulable status, if the placement cannot be bound to the cluster; or
	// * An InternalError status, if an expected error has occurred
	Filter(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (status *Status)
}

// PreScorePlugin is the interface which all plugins that would like to run at the PreScore
// extension point should implement.
type PreScorePlugin interface {
	Plugin

	// PreScore runs before the scheduler enters the Score stage; a plugin may perform
	// some setup at this extension point, such as caching the results that will be used in
	// following Score calls, and/or run some checks to determine if it should be skipped in
	// the Filter stage.
	//
	// A plugin which registers at this extension point must return one of the follows:
	// * A Success status, if the plugin should run at the Score stage; or
	// * A Skip status, if the plugin should be skipped at the Score stage; or
	// * An InternalError status, if an expected error has occurred
	PreScore(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot) (status *Status)
}

// ScorePlugin is the interface which all plugins that would like to run at the Score
// extension point should implement.
type ScorePlugin interface {
	Plugin

	// Score runs at the Score stage, to score a cluster for a specific placement.
	// A plugin which registers at this extension point must return one of the follows:
	// * A Success status, with the score for the cluster; or
	// * An InternalError status, if an expected error has occurred
	Score(ctx context.Context, state CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterPolicySnapshot, cluster *fleetv1beta1.MemberCluster) (score *ClusterScore, status *Status)
}
