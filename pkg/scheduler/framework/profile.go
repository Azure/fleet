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

package framework

// Profile specifies the scheduling profile a framework uses; it includes the plugins in use
// by the framework at each extension point in order.
//
// At this moment, since Fleet does not support runtime profiles, all plugins are registered
// directly to one universal profile, in their instantiated forms, rather than decoupled using
// a factory registry and instantiated along with the profile's associated framework.
type Profile struct {
	name string

	postBatchPlugins []PostBatchPlugin
	preFilterPlugins []PreFilterPlugin
	filterPlugins    []FilterPlugin
	preScorePlugins  []PreScorePlugin
	scorePlugins     []ScorePlugin

	// RegisteredPlugins is a map of all plugins registered to the profile, keyed by their names.
	// This helps to avoid setting up same plugin multiple times with the framework if the plugin
	// registers at multiple extension points.
	registeredPlugins map[string]Plugin
}

// WithPostBatchPlugin registers a PostBatchPlugin to the profile.
func (profile *Profile) WithPostBatchPlugin(plugin PostBatchPlugin) *Profile {
	profile.postBatchPlugins = append(profile.postBatchPlugins, plugin)
	profile.registeredPlugins[plugin.Name()] = plugin
	return profile
}

// WithPreFilterPlugin registers a PreFilterPlugin to the profile.
func (profile *Profile) WithPreFilterPlugin(plugin PreFilterPlugin) *Profile {
	profile.preFilterPlugins = append(profile.preFilterPlugins, plugin)
	profile.registeredPlugins[plugin.Name()] = plugin
	return profile
}

// WithFilterPlugin registers a FilterPlugin to the profile.
func (profile *Profile) WithFilterPlugin(plugin FilterPlugin) *Profile {
	profile.filterPlugins = append(profile.filterPlugins, plugin)
	profile.registeredPlugins[plugin.Name()] = plugin
	return profile
}

// WithPreScorePlugin registers a PreScorePlugin to the profile.
func (profile *Profile) WithPreScorePlugin(plugin PreScorePlugin) *Profile {
	profile.preScorePlugins = append(profile.preScorePlugins, plugin)
	profile.registeredPlugins[plugin.Name()] = plugin
	return profile
}

// WithScorePlugin registers a ScorePlugin to the profile.
func (profile *Profile) WithScorePlugin(plugin ScorePlugin) *Profile {
	profile.scorePlugins = append(profile.scorePlugins, plugin)
	profile.registeredPlugins[plugin.Name()] = plugin
	return profile
}

// Name returns the name of the profile.
func (profile *Profile) Name() string {
	return profile.name
}

// NewProfile creates scheduling profile.
func NewProfile(name string) *Profile {
	return &Profile{
		name:              name,
		registeredPlugins: map[string]Plugin{},
	}
}
