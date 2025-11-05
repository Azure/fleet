//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

package options

import (
	"flag"
)

type AzurePropertyCheckerOptions struct {
	// isEnabled indicates whether the Azure property checker is enabled.
	isEnabled bool
	// configFilePath is the path to the Azure property checker configuration file.
	configFilePath string
}

// NewAzurePropertyCheckerOptions creates a new AzurePropertyCheckerOptions with default values.
func NewAzurePropertyCheckerOptions() *AzurePropertyCheckerOptions {
	return &AzurePropertyCheckerOptions{
		isEnabled:      false,
		configFilePath: "/etc/kubernetes/checker/config.json",
	}
}

// AddFlags adds flags for Azure Property Checker options to the given FlagSet.
func (o *AzurePropertyCheckerOptions) AddFlags(flags *flag.FlagSet) {
	flags.BoolVar(&o.isEnabled, "azure-property-checker-enabled", o.isEnabled, "Enable Azure property checker for validating Azure-specific cluster properties.")
	flags.StringVar(&o.configFilePath, "azure-property-checker-config-file", o.configFilePath, "Path to the Azure property checker configuration file.")
}

// IsEnabled returns whether the Azure property checker is enabled.
func (o *AzurePropertyCheckerOptions) IsEnabled() bool {
	return o.isEnabled
}

// ConfigFilePath returns the path to the Azure property checker configuration file.
func (o *AzurePropertyCheckerOptions) ConfigFilePath() string {
	return o.configFilePath
}
