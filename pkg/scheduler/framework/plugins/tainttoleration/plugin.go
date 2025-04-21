package tainttoleration

import "github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework"

// Plugin is the scheduler plugin that checks to see if taints on the MemberCluster
// can be tolerated by tolerations on ClusterResourcePlacement
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
	//
	// Note that successful connection to any of the extension points implies that the
	// plugin already implements the Plugin interface.
	_ framework.FilterPlugin = &Plugin{}
)

type taintTolerationPluginOptions struct {
	// The name of the plugin.
	name string
}

type Option func(*taintTolerationPluginOptions)

var defaultPluginOptions = taintTolerationPluginOptions{
	name: "TaintToleration",
}

// WithName sets the name of the plugin.
func WithName(name string) Option {
	return func(o *taintTolerationPluginOptions) {
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
