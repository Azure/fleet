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

// Package profile holds the definition of a scheduling Profile.
package profile

import (
	"go.goms.io/fleet/pkg/scheduler/framework"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/clusteraffinity"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/clustereligibility"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/sameplacementaffinity"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/tainttoleration"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/topologyspreadconstraints"
)

const (
	// defaultProfileName is the default name for the scheduling profile.
	defaultProfileName = "DefaultProfile"
)

// NewDefaultProfile creates a default scheduling profile.
func NewDefaultProfile() *framework.Profile {
	p := framework.NewProfile(defaultProfileName)

	// default plugin list
	clusterAffinityPlugin := clusteraffinity.New()
	clusterEligibilityPlugin := clustereligibility.New()
	samePlacementAffinityPlugin := sameplacementaffinity.New()
	topologySpreadConstraintsPlugin := topologyspreadconstraints.New()
	taintTolerationPlugin := tainttoleration.New()

	p.WithPostBatchPlugin(&topologySpreadConstraintsPlugin).
		WithPreFilterPlugin(&clusterAffinityPlugin).WithPreFilterPlugin(&topologySpreadConstraintsPlugin).
		WithFilterPlugin(&clusterAffinityPlugin).WithFilterPlugin(&clusterEligibilityPlugin).WithFilterPlugin(&taintTolerationPlugin).WithFilterPlugin(&samePlacementAffinityPlugin).WithFilterPlugin(&topologySpreadConstraintsPlugin).
		WithPreScorePlugin(&clusterAffinityPlugin).WithPreScorePlugin(&topologySpreadConstraintsPlugin).
		WithScorePlugin(&clusterAffinityPlugin).WithScorePlugin(&samePlacementAffinityPlugin).WithScorePlugin(&topologySpreadConstraintsPlugin)
	return p
}
