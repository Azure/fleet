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

// Package namespaceaffinity features a scheduler plugin that filters clusters based on namespace availability.
// This plugin ensures that ResourcePlacements are only scheduled to clusters where the target namespace exists.
package namespaceaffinity

import (
	"go.goms.io/fleet/pkg/scheduler/framework"
)

// Plugin is the scheduler plugin that filters clusters based on namespace availability for ResourcePlacements.
type Plugin struct {
	// The name of the plugin.
	name string

	// The framework handle.
	handle framework.Handle
}

var (
	// Verify that Plugin can connect to relevant extension points at compile time.
	//
	// This plugin leverages the following extension points:
	// * PreFilter
	// * Filter
	//
	// Note that successful connection to any of the extension points implies that the
	// plugin already implements the Plugin interface.
	_ framework.PreFilterPlugin = &Plugin{}
	_ framework.FilterPlugin    = &Plugin{}
)

type namespaceAffinityPluginOptions struct {
	// The name of the plugin.
	name string
}

type Option func(*namespaceAffinityPluginOptions)

var defaultPluginOptions = namespaceAffinityPluginOptions{
	name: "NamespaceAffinity",
}

// WithName sets the name of the plugin.
func WithName(name string) Option {
	return func(o *namespaceAffinityPluginOptions) {
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
