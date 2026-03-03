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

package options

import (
	"flag"
)

// Options is the options to use for running the KubeFleet hub agent.
type Options struct {
	// Leader election related options.
	LeaderElectionOpts LeaderElectionOptions

	// Options that concern the setup of the controller manager instance in use by the KubeFleet hub agent.
	CtrlMgrOpts ControllerManagerOptions

	// KubeFleet webhook related options.
	WebhookOpts WebhookOptions

	// Feature flags that control the enabling of certain features in the hub agent.
	FeatureFlags FeatureFlags

	// Options that fine-tune how KubeFleet hub agent manages member clusters in the fleet.
	ClusterMgmtOpts ClusterManagementOptions

	// Options that fine-tune how KubeFleet hub agent manages resources placements in the fleet.
	PlacementMgmtOpts PlacementManagementOptions

	// AzurePropertyCheckerOpts contains options for Azure property checker
	AzurePropertyCheckerOpts AzurePropertyCheckerOptions
}

func NewOptions() *Options {
	return &Options{}
}

func (o *Options) AddFlags(flags *flag.FlagSet) {
	o.LeaderElectionOpts.AddFlags(flags)
	o.CtrlMgrOpts.AddFlags(flags)
	o.WebhookOpts.AddFlags(flags)
	o.FeatureFlags.AddFlags(flags)
	o.ClusterMgmtOpts.AddFlags(flags)
	o.PlacementMgmtOpts.AddFlags(flags)
	o.AzurePropertyCheckerOpts.AddFlags(flags)
}
