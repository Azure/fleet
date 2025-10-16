//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

package clusteraffinity

import (
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/propertyprovider/azure"
)

// MatchPropertiesInPropertyChecker checks if the given property selector requirement
// matches any azure-specific properties that can be handled by the azure property checker.
func (c *clusterRequirement) MatchPropertiesInPropertyChecker(cluster *clusterv1beta1.MemberCluster, req placementv1beta1.PropertySelectorRequirement) (handled bool, available bool, err error) {
	// Check if the property is an Azure SKU capacity property.
	if sku := isAzureSKUCapacityProperty(req.Name); sku != "" {
		// Use the Azure property checker to validate SKU capacity requirement.
		available, err := c.PropertyChecker.CheckIfMeetSKUCapacityRequirement(cluster, req, sku)
		if err != nil {
			return false, false, err
		}
		return true, available, err
	}

	// Property is not handled by this property checker, fallback to standard validation.
	return false, false, nil
}

// isAzureSKUCapacityProperty checks if a property name matches the Azure SKU capacity pattern.
// If it matches, it returns the SKU name; otherwise, it returns an empty string.
func isAzureSKUCapacityProperty(propertyName string) string {
	// Validate if the requirement is an Azure SKU capacity property.
	// Extract SKU from the property name using regex.
	// Expected format: "kubernetes.azure.com/vm-sizes/{sku}/capacity"
	matches := azure.CapacityPerSKUPropertyRegex.FindStringSubmatch(propertyName)
	if len(matches) == 2 {
		return matches[1]
	}
	return ""
}
