/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package clusteraffinity features a scheduler plugin that enforces cluster affinity (if any) defined on a CRP.
package clusteraffinity

import (
	"errors"
	"fmt"

	"go.goms.io/fleet/pkg/scheduler/framework"
)

const (
	// Fleet supports two types of metrics: resource metrics and non-resource metrics.
	//
	// Non-resource metrics are key/value pairs, where the key is a Kubernetes label name
	// and the value a sortable numeric. Resource metrics, however, are stored in a structured
	// manner, as a Kubernetes cluster might have different capacities for the same type
	// of resource.
	//
	// For scheduling, Fleet allows users to query both types of metrics to inform
	// scheduling decisions. For non-resource metrics, users could refer to them directly
	// using their keys (the label names); and for resource metrics, Fleet reserves a special
	// format so that resource metrics can also be referenced using some label names.
	//
	// Such label names belong to a special domain, `resources.kubernetes-fleet.io`, and
	// its name should be separated by a dash (`-`), where the part before is the capacity
	// type, and the part after the name of the resource.
	//
	// TO-DO (chenyu1): uncomment with future PRs.
	//
	//resourceMetricLabelPrefix  = "resources.kubernetes-fleet.io/"
	resourceMetricLabelNameSep = "-"
)

// Plugin is the scheduler plugin that enforces the cluster affinity (if any) defined on a CRP.
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
	// * PreFilter
	// * Filter
	// * PreScore
	// * Score
	//
	// Note that successful connection to any of the extension points implies that the
	// plugin already implements the Plugin interface.
	_ framework.PreFilterPlugin = &Plugin{}
	_ framework.FilterPlugin    = &Plugin{}
	_ framework.PreScorePlugin  = &Plugin{}
	_ framework.ScorePlugin     = &Plugin{}
)

type clusterAffinityPluginOptions struct {
	// The name of the plugin.
	name string
}

type Option func(*clusterAffinityPluginOptions)

var defaultPluginOptions = clusterAffinityPluginOptions{
	name: "ClusterAffinity",
}

// WithName sets the name of the plugin.
func WithName(name string) Option {
	return func(o *clusterAffinityPluginOptions) {
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

// readPluginState reads the plugin state from the cycle state.
func (p *Plugin) readPluginState(state framework.CycleStatePluginReadWriter) (*pluginState, error) {
	// Read from the cycle state.
	val, err := state.Read(framework.StateKey(p.Name()))
	if err != nil {
		return nil, fmt.Errorf("failed to read value from the cycle state: %w", err)
	}

	// Cast the value to the right type.
	ps, ok := val.(*pluginState)
	if !ok {
		return nil, fmt.Errorf("failed to cast value %v to the right type", val)
	}
	if ps == nil {
		return nil, errors.New("plugin state is nil")
	}
	return ps, nil
}
