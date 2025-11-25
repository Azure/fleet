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

// AddFlags adds flags for Azure Property Checker options to the given FlagSet.
func (o *AzurePropertyCheckerOptions) AddFlags(flags *flag.FlagSet) {
	flags.BoolVar(&o.IsEnabled, "azure-property-checker-enabled", false, "Enable Azure property checker for validating Azure-specific cluster properties.")
	flags.StringVar(&o.ComputeServiceAddressWithBasePath, "azure-compute-server-address", "http://localhost:8421/compute", "The address of the Azure compute service with base path.")
}
