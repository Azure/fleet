//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

package options

import (
	"flag"
)

// AzurePropertyCheckerOptions holds the options for the Azure property checker.
type AzurePropertyCheckerOptions struct {
	// IsEnabled indicates whether the Azure property checker is enabled.
	IsEnabled bool
	// ComputeServiceAddressWithBasePath is the address of the Azure compute service with base path.
	ComputeServiceAddressWithBasePath string
}

// NewAzurePropertyCheckerOptions creates a new AzurePropertyCheckerOptions with default values.
func NewAzurePropertyCheckerOptions() AzurePropertyCheckerOptions {
	return AzurePropertyCheckerOptions{
		IsEnabled:                         false,
		ComputeServiceAddressWithBasePath: "http://localhost:8421/compute",
	}
}

// AddFlags adds flags for Azure Property Checker options to the given FlagSet.
func (o AzurePropertyCheckerOptions) AddFlags(flags *flag.FlagSet) {
	flags.BoolVar(&o.IsEnabled, "azure-property-checker-enabled", o.IsEnabled, "Enable Azure property checker for validating Azure-specific cluster properties.")
	flags.StringVar(&o.ComputeServiceAddressWithBasePath, "azure-compute-server-address", o.ComputeServiceAddressWithBasePath, "The address of the Azure compute service with base path.")
}
