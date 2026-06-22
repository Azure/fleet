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

type PropertyProviderOptions struct {
	// The region where the member cluster resides.
	Region string

	// The property provider to use in the KubeFleet member agent.
	Name string

	// The path to a configuration file that enables the KubeFleet member agent to
	// connect to a specific cloud platform to retrieve platform-specific cluster properties.
	CloudConfigFilePath string

	// Enable support for cost properties in the Azure property provider or not. This option
	// applies only when the Azure property provider is in use.
	EnableAzProviderCostProperties bool

	// Enable support for available resource properties in the Azure property provider or not.
	// This option applies only when the Azure property provider is in use.
	EnableAzProviderAvailableResourceProperties bool

	// Enable support for namespace collection in the Azure property provider or not. This option applies only when the Azure property provider is in use.
	EnableAzProviderNamespaceCollection bool
}

func (o *PropertyProviderOptions) AddFlags(flags *flag.FlagSet) {
	flags.StringVar(
		&o.Region,
		"region",
		"",
		"The region where the member cluster resides.")

	flags.StringVar(
		&o.Name,
		"property-provider",
		"none",
		"The property provider to use in the KubeFleet member agent.")

	flags.StringVar(
		&o.CloudConfigFilePath,
		"cloud-config",
		"/etc/kubernetes/provider/config.json",
		"The path to a configuration file that enables the KubeFleet member agent to connect to a specific cloud platform to retrieve platform-specific cluster properties.")

	flags.BoolVar(
		&o.EnableAzProviderCostProperties,
		"use-cost-properties-in-azure-provider",
		true,
		"Enable support for cost properties in the Azure property provider or not. This option applies only when the Azure property provider is in use.")

	flags.BoolVar(
		&o.EnableAzProviderAvailableResourceProperties,
		"use-available-res-properties-in-azure-provider",
		true,
		"Enable support for available resource properties in the Azure property provider or not. This option applies only when the Azure property provider is in use.")

	flags.BoolVar(
		&o.EnableAzProviderNamespaceCollection,
		"enable-namespace-collection-in-property-provider",
		false,
		"Enable support for namespace collection in the Azure property provider or not. This option applies only when the Azure property provider is in use.")
}
