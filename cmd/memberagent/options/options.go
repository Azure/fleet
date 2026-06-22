/*
Copyright 2026 The KubeFleet Authors.

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

// Options is the options to use for running the KubeFleet member agent.
type Options struct {
	// Options that control how the KubeFleet member agent connects to the hub cluster.
	HubConnectivityOpts HubConnectivityOptions

	// Options that concern the setup of the controller manager instance in use by the
	// KubeFleet member agent.
	CtrlManagerOptions CtrlManagerOptions

	// Options that control how the KubeFleet member agent applies resources from the hub
	// cluster to the member cluster.
	ApplierOpts ApplierOptions

	// KubeFleet cluster property provider related options.
	PropertyProviderOpts PropertyProviderOptions

	// The fields below are added only for backwards compatibility reasons.
	// Their values are never read.
	UseV1Beta1APIs bool
}

func NewOptions() *Options {
	return &Options{}
}

func (o *Options) AddFlags(flags *flag.FlagSet) {
	o.HubConnectivityOpts.AddFlags(flags)
	o.CtrlManagerOptions.AddFlags(flags)
	o.ApplierOpts.AddFlags(flags)
	o.PropertyProviderOpts.AddFlags(flags)

	// The flags set up below are added only for backwards compatibility reasons.
	// They are no-op flags and their values are never read.
	flags.BoolVar(
		&o.UseV1Beta1APIs,
		"enable-v1beta1-apis",
		true,
		"Use KubeFleet v1beta1 APIs or not. This flag is obsolete; its value has no effect.")
}
