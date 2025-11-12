//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

// Package propertychecker features interfaces and other components that can be used to build
// a Fleet property checker for validating cluster requirements.
package propertychecker

import (
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// PropertyChecker is the interface that every property checker must implement.
//
// Property checkers validate whether clusters can meet specific property-based requirements
// defined in placement policies. This validation is performed in the fleet-hub-agent,
// which is responsible for scheduling and placement decisions across the fleet.
type PropertyChecker interface {
	// SupportsProperty returns true if this property checker can validate the given property name.
	// This allows the fleet-hub-agent scheduler to determine which property checker to use for a given requirement.
	SupportsProperty(propertyName string) bool

	// ValidatePropertyRequirement validates whether a member cluster can meet the specified
	// property selector requirement. It returns a bool indicating
	// whether the requirement can be met.
	//
	// The implementation should:
	// - Extract necessary information from the cluster (labels, properties, etc.)
	// - Validate the requirement against the cluster's capabilities
	// - Return boolean result and any error encountered during validation
	//
	// This method should complete promptly and handle errors gracefully.
	//
	// This validation is performed by the fleet-hub-agent as part of the placement and scheduling process.
	ValidatePropertyRequirement(
		cluster *clusterv1beta1.MemberCluster,
		req placementv1beta1.PropertySelectorRequirement,
	) (bool, error)
}
