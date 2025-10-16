//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

package labels

const (
	// AzureFleetLabelPrefix is the prefix used to label fleet member cluster resources.
	AzureFleetLabelPrefix = "fleet.azure.com/"

	// AzureLocationLabel is the label key for Azure location.
	AzureLocationLabel = AzureFleetLabelPrefix + "location"

	// AzureSubscriptionIDLabel is the label key for Azure subscription ID.
	AzureSubscriptionIDLabel = AzureFleetLabelPrefix + "subscription-id"
)
