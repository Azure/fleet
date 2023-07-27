/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package clustereligibility features a scheduler plugin that filters out clusters
// that are not eligible for resource placement.
package clustereligibility

import (
	"context"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/clustereligibilitychecker"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

const (
	// defaultPluginName is the default name of the plugin.
	defaultPluginName = "ClusterEligibility"
)

// Plugin is the scheduler plugin that performs the cluster eligibility check.
type Plugin struct {
	// The name of the plugin.
	name string

	// The cluster eligibility checker in use by the scheduler framework.
	clusterEligibilityChecker *clustereligibilitychecker.ClusterEligibilityChecker

	// The framework handle.
	handle framework.Handle
}

var (
	// Verify that Plugin can connect to relevant extension points
	// at compile time.
	//
	// This plugin leverages the following the extension points:
	// * Filter
	//
	// Note that successful connection to any of the extension points implies that the
	// plugin already implements the Plugin interface.
	_ framework.FilterPlugin = &Plugin{}
)

// pluginOptions is the options for this plugin.
type pluginOptions struct {
	// The name of the plugin.
	name string
}

// Option helps set up the plugin.
type Option func(*pluginOptions)

// defaultPluginOptions is the default options for this plugin.
var defaultPluginOptions = pluginOptions{
	name: defaultPluginName,
}

// WithName sets the name of the plugin.
func WithName(name string) Option {
	return func(o *pluginOptions) {
		o.name = name
	}
}

// New returns a new Plugin.
func New(opts ...Option) Plugin {
	options := defaultPluginOptions
	for _, opt := range opts {
		opt(&options)
	}

	return Plugin{
		name: options.name,
	}
}

// Name returns the name of the plugin.
func (p *Plugin) Name() string {
	return p.name
}

// SetUpWithFramework sets up this plugin with a scheduler framework.
func (p *Plugin) SetUpWithFramework(handle framework.Handle) {
	p.clusterEligibilityChecker = handle.ClusterEligibilityChecker()
	p.handle = handle

	// This plugin does not need to set up any informer.
}

// Filter allows the plugin to connect to the Filter extension point in the scheduling framework.
func (p *Plugin) Filter(
	_ context.Context,
	_ framework.CycleStatePluginReadWriter,
	_ *fleetv1beta1.ClusterSchedulingPolicySnapshot,
	cluster *fleetv1beta1.MemberCluster,
) (status *framework.Status) {
	if eligible, reason := p.clusterEligibilityChecker.IsEligible(cluster); !eligible {
		return framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), reason)
	}

	return nil
}
