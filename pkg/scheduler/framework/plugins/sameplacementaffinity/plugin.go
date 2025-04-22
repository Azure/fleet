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

// Package sameplacementaffinity features a scheduler plugin that filters out any cluster that has been already scheduled/bounded
// to the resource placement and prefers the same cluster which has an obsolete binding.
package sameplacementaffinity

import "go.goms.io/fleet/pkg/scheduler/framework"

// Plugin is the scheduler plugin that enforces the same placement affinity and anti-affinity.
// "Affinity" means a scheduler prefers the cluster which has an obsolete binding of the same placement in order to
// minimize interruption between different scheduling runs.
// "Anti-Affinity" means a scheduler filters out the cluster which has been already scheduled/bounded as two placements
// cannot be scheduled on a cluster.
type Plugin struct {
	// The name of the plugin.
	name string

	// The framework handle.
	handle framework.Handle
}

var (
	// Verify that Plugin can connect to relevant extension points at compile time.
	//
	// This plugin leverages the following the extension points:
	// * Filter
	// * Score
	//
	// Note that successful connection to any of the extension points implies that the
	// plugin already implements the Plugin interface.
	_ framework.FilterPlugin = &Plugin{}
	_ framework.ScorePlugin  = &Plugin{}
)

type samePlacementAntiAffinityPluginOptions struct {
	// The name of the plugin.
	name string
}

type Option func(*samePlacementAntiAffinityPluginOptions)

var defaultPluginOptions = samePlacementAntiAffinityPluginOptions{
	name: "SamePlacementAntiAffinity",
}

// WithName sets the name of the plugin.
func WithName(name string) Option {
	return func(o *samePlacementAntiAffinityPluginOptions) {
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
}
