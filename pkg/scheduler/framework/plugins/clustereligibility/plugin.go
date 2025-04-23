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

// Package clustereligibility features a scheduler plugin that filters out clusters
// that are not eligible for resource placement.
package clustereligibility

import (
	"context"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
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
	p.handle = handle

	// This plugin does not need to set up any informer.
}

// Filter allows the plugin to connect to the Filter extension point in the scheduling framework.
func (p *Plugin) Filter(
	_ context.Context,
	_ framework.CycleStatePluginReadWriter,
	_ *placementv1beta1.ClusterSchedulingPolicySnapshot,
	cluster *clusterv1beta1.MemberCluster,
) (status *framework.Status) {
	if eligible, reason := p.handle.ClusterEligibilityChecker().IsEligible(cluster); !eligible {
		return framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), reason)
	}

	return nil
}
