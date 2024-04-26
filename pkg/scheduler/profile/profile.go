/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
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
