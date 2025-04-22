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

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	dummyProfileName = "dummyProfile"
	dummyPluginName  = "dummyAllPurposePlugin"
)

// TestProfile tests the basic ops of a Profile.
func TestProfile(t *testing.T) {
	profile := NewProfile(dummyProfileName)

	dummyAllPurposePlugin := &DummyAllPurposePlugin{
		name: dummyPluginName,
	}
	dummyPlugin := Plugin(dummyAllPurposePlugin)

	profile.WithPostBatchPlugin(dummyAllPurposePlugin)
	profile.WithPreFilterPlugin(dummyAllPurposePlugin)
	profile.WithFilterPlugin(dummyAllPurposePlugin)
	profile.WithPreScorePlugin(dummyAllPurposePlugin)
	profile.WithScorePlugin(dummyAllPurposePlugin)

	wantProfile := &Profile{
		name:             dummyProfileName,
		postBatchPlugins: []PostBatchPlugin{dummyAllPurposePlugin},
		preFilterPlugins: []PreFilterPlugin{dummyAllPurposePlugin},
		filterPlugins:    []FilterPlugin{dummyAllPurposePlugin},
		preScorePlugins:  []PreScorePlugin{dummyAllPurposePlugin},
		scorePlugins:     []ScorePlugin{dummyAllPurposePlugin},
		registeredPlugins: map[string]Plugin{
			dummyPluginName: dummyPlugin,
		},
	}

	if !cmp.Equal(profile, wantProfile, cmp.AllowUnexported(Profile{}, DummyAllPurposePlugin{})) {
		t.Fatalf("NewProfile() = %v, want %v", profile, wantProfile)
	}
}
