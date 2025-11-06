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
	// computeServiceAddressWithBasePath is the address of the Azure compute service with base path.
	computeServiceAddressWithBasePath string
}

// NewAzurePropertyCheckerOptions creates a new AzurePropertyCheckerOptions with default values.
func NewAzurePropertyCheckerOptions() *AzurePropertyCheckerOptions {
	return &AzurePropertyCheckerOptions{
		isEnabled:                         false,
		computeServiceAddressWithBasePath: "http://localhost:8421/compute",
	}
}

// AddFlags adds flags for Azure Property Checker options to the given FlagSet.
func (o *AzurePropertyCheckerOptions) AddFlags(flags *flag.FlagSet) {
	flags.BoolVar(&o.isEnabled, "azure-property-checker-enabled", o.isEnabled, "Enable Azure property checker for validating Azure-specific cluster properties.")
	flags.StringVar(&o.computeServiceAddressWithBasePath, "azure-compute-server-address", o.computeServiceAddressWithBasePath, "The address of the Azure compute service with base path.")
}

// IsEnabled returns whether the Azure property checker is enabled.
func (o *AzurePropertyCheckerOptions) IsEnabled() bool {
	return o.isEnabled
}

// ComputeServiceAddressWithBasePath returns the address of the Azure compute service with base path.
func (o *AzurePropertyCheckerOptions) ComputeServiceAddressWithBasePath() string {
	return o.computeServiceAddressWithBasePath
}
