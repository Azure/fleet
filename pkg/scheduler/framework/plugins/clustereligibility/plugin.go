/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package clustereligibility features a scheduler plugin that filters out clusters
// that are not eligible for resource placement.
package clustereligibility

import (
	"context"
	"time"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/clustereligibilitycheck"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

const (
	// defaultPluginName is the default name of the plugin.
	defaultPluginName = "ClusterEligibility"

	// defaultClusterHeartbeatTimeout is the default timeout value this plugin uses for checking
	// if a cluster has been disconnected from the fleet for a prolonged period of time.
	defaultClusterHeartbeatTimeout = time.Minute * 5

	// defaultClusterHealthCheckTimeout is the default timeout value this plugin uses for checking
	// if a cluster is still in a healthy state.
	defaultClusterHealthCheckTimeout = time.Minute * 5
)

// Plugin is the scheduler plugin that performs the cluster eligibility check.
type Plugin struct {
	// The name of the plugin.
	name string

	// The timeout value this plugin uses for checking if a cluster has been disconnected
	// from the fleet for a prolonged period of time.
	clusterHeartbeatTimeout time.Duration

	// The timeout value this plugin uses for checking if a cluster is still in a healthy state.
	clusterHealthCheckTimeout time.Duration

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

	// The timeout value this plugin uses for checking if a cluster has been disconnected
	// from the fleet for a prolonged period of time.
	clusterHeartbeatTimeout time.Duration

	// The timeout value this plugin uses for checking if a cluster is still in a healthy state.
	clusterHealthCheckTimeout time.Duration
}

// Option helps set up the plugin.
type Option func(*pluginOptions)

// defaultPluginOptions is the default options for this plugin.
var defaultPluginOptions = pluginOptions{
	name:                      defaultPluginName,
	clusterHeartbeatTimeout:   defaultClusterHeartbeatTimeout,
	clusterHealthCheckTimeout: defaultClusterHealthCheckTimeout,
}

// WithName sets the name of the plugin.
func WithName(name string) Option {
	return func(o *pluginOptions) {
		o.name = name
	}
}

// WithClusterHeartbeatTimeout sets the timeout value this plugin uses for checking
// if a cluster has been disconnected from the fleet for a prolonged period of time.
func WithClusterHeartbeatTimeout(timeout time.Duration) Option {
	return func(o *pluginOptions) {
		o.clusterHeartbeatTimeout = timeout
	}
}

// WithClusterHealthCheckTimeout sets the timeout value this plugin uses for checking
// if a cluster is still in a healthy state.
func WithClusterHealthCheckTimeout(timeout time.Duration) Option {
	return func(o *pluginOptions) {
		o.clusterHealthCheckTimeout = timeout
	}
}

// New returns a new Plugin.
func New(opts ...Option) Plugin {
	options := defaultPluginOptions
	for _, opt := range opts {
		opt(&options)
	}

	return Plugin{
		name:                      options.name,
		clusterHeartbeatTimeout:   options.clusterHeartbeatTimeout,
		clusterHealthCheckTimeout: options.clusterHealthCheckTimeout,
	}
}

// Name returns the name of the plugin.
func (p *Plugin) Name() string {
	return p.name
}

// SetUpWithFramework sets up this plugin with a scheduler framework.
func (p *Plugin) SetUpWithFramework(handle framework.Handle) {
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
	if eligible, reason := clustereligibilitycheck.IsClusterEligible(cluster, p.clusterHeartbeatTimeout, p.clusterHealthCheckTimeout); !eligible {
		return framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), reason)
	}

	return nil
}
