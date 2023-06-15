/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
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

	dummyAllPurposePlugin := &DummyAllPurposePlugin{}
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

	if !cmp.Equal(profile, wantProfile, cmp.AllowUnexported(Profile{})) {
		t.Fatalf("NewProfile() = %v, want %v", profile, wantProfile)
	}
}
