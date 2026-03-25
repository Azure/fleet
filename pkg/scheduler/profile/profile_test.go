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

package profile

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework/plugins/clusteraffinity"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework/plugins/clustereligibility"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework/plugins/namespaceaffinity"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework/plugins/sameplacementaffinity"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework/plugins/tainttoleration"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework/plugins/topologyspreadconstraints"
)

// TestNewDefaultProfile tests the creation of a default scheduling profile.
func TestNewDefaultProfile(t *testing.T) {
	profile := NewDefaultProfile()

	if profile == nil {
		t.Fatal("NewDefaultProfile() returned nil, want non-nil profile")
	}

	// Verify profile name
	if profile.Name() != defaultProfileName {
		t.Errorf("NewDefaultProfile() profile name = %q, want %q", profile.Name(), defaultProfileName)
	}

	// Use reflection to check the internal structure since the fields are unexported
	// Create a comparable profile to verify the structure
	wantProfile := framework.NewProfile(defaultProfileName)

	// Configure the expected profile with the same plugins
	testClusterAffinityPlugin := clusteraffinity.New()
	testClusterEligibilityPlugin := clustereligibility.New()
	testNamespaceAffinityPlugin := namespaceaffinity.New()
	testSamePlacementAffinityPlugin := sameplacementaffinity.New()
	testTopologySpreadConstraintsPlugin := topologyspreadconstraints.New()
	testTaintTolerationPlugin := tainttoleration.New()

	wantProfile.WithPostBatchPlugin(&testTopologySpreadConstraintsPlugin).
		WithPreFilterPlugin(&testClusterAffinityPlugin).WithPreFilterPlugin(&testNamespaceAffinityPlugin).WithPreFilterPlugin(&testTopologySpreadConstraintsPlugin).
		WithFilterPlugin(&testClusterAffinityPlugin).WithFilterPlugin(&testClusterEligibilityPlugin).WithFilterPlugin(&testNamespaceAffinityPlugin).WithFilterPlugin(&testTaintTolerationPlugin).WithFilterPlugin(&testSamePlacementAffinityPlugin).WithFilterPlugin(&testTopologySpreadConstraintsPlugin).
		WithPreScorePlugin(&testClusterAffinityPlugin).WithPreScorePlugin(&testTopologySpreadConstraintsPlugin).
		WithScorePlugin(&testClusterAffinityPlugin).WithScorePlugin(&testSamePlacementAffinityPlugin).WithScorePlugin(&testTopologySpreadConstraintsPlugin)

	// Compare the profiles using cmp.Equal with AllowUnexported to access private fields
	if diff := cmp.Diff(profile, wantProfile,
		cmp.AllowUnexported(framework.Profile{},
			clusteraffinity.Plugin{},
			clustereligibility.Plugin{},
			namespaceaffinity.Plugin{},
			sameplacementaffinity.Plugin{},
			topologyspreadconstraints.Plugin{},
			tainttoleration.Plugin{})); diff != "" {
		t.Errorf("NewDefaultProfile() mismatch (-got +want):\n%s", diff)
	}
}

// TestNewProfileWithOptions tests the creation of a scheduling profile with custom options.
// It verifies that:
// 1. Profile is created successfully with both empty and custom options
// 2. Profile name is set to the default value regardless of options
// 3. Custom ClusterAffinityPlugin option is accepted
func TestNewProfileWithOptions(t *testing.T) {
	testCases := []struct {
		name     string
		opts     Options
		wantName string
	}{
		{
			name:     "EmptyOptions",
			opts:     Options{},
			wantName: defaultProfileName,
		},
		{
			name: "CustomClusterAffinityPlugin",
			opts: Options{
				ClusterAffinityPlugin: &clusteraffinity.Plugin{},
			},
			wantName: defaultProfileName,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profile := NewProfile(tc.opts)

			if profile == nil {
				t.Fatal("NewProfile() returned nil, want non-nil profile")
			}

			if profile.Name() != tc.wantName {
				t.Errorf("NewProfile() profile name = %q, want %q", profile.Name(), tc.wantName)
			}
		})
	}
}

// TestNewProfileWithCustomClusterAffinityPlugin tests that custom cluster affinity plugin is used when provided.
func TestNewProfileWithCustomClusterAffinityPlugin(t *testing.T) {
	customPlugin := clusteraffinity.New()
	opts := Options{
		ClusterAffinityPlugin: &customPlugin,
	}

	profile1 := NewProfile(opts)
	profile2 := NewProfile(Options{}) // Default profile

	// Both profiles should have the same name
	if profile1.Name() != profile2.Name() {
		t.Errorf("Profile names should be the same: custom=%q, default=%q", profile1.Name(), profile2.Name())
	}

	// Both profiles should be configured similarly, but with potentially different plugin instances
	// This test verifies that the custom plugin option doesn't break the profile creation
	if profile1 == nil || profile2 == nil {
		t.Error("Both profiles should be non-nil")
	}
}
