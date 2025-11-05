/* THIS FILE IS CREATED IN ANOTHER PROJECT AND COPIED HERE */
/* JUST FOR THE PURPOSE OF SEEDING THE COMPLETE FUNCTIONALITY OF THE SYSTEM */

//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

// Package azure provides property checkers for Azure-specific cluster requirements.
// It checks whether the cluster can meet the requirement defined by the property selector.
package azure

import (
	"go.goms.io/fleet/pkg/clients/azure/compute"
)

// PropertyChecker provides Azure-specific property validation for member clusters.
// It validates compute requirements to determine if clusters
// can meet the specified property selector requirements.
type PropertyChecker struct {
	// vmSizeRecommenderClient is the Azure compute client used to generate VM size recommendations
	// and validate SKU capacity requirements.
	vmSizeRecommenderClient compute.AttributeBasedVMSizeRecommenderClient
}

// NewPropertyChecker creates a new PropertyChecker with the given client.
// The vmSizeRecommenderClient is used to validate SKU capacity requirements.
func NewPropertyChecker(vmSizeRecommenderClient compute.AttributeBasedVMSizeRecommenderClient) *PropertyChecker {
	return &PropertyChecker{
		vmSizeRecommenderClient: vmSizeRecommenderClient,
	}
}

/* MORE CODE HERE BUT NOT RELEVANT TO THIS PR */
/* WILL FIX ONCE THE OTHER PR IS MERGED AND THIS IS REBASED */
