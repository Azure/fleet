package azure

import (
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	// SkuCapacityPropertyPrefix is the prefix that identifies the capacity as a property.
	SkuCapacityPropertyPrefix = "kubernetes.azure.com/vm-size"

	// AzureLocationLabelKey is the label key for Azure location
	AzureLocationLabelKey = "fleet.azure.com/location"

	// AzureSubscriptionIDLabelKey is the label key for Azure subscription ID
	AzureSubscriptionIDLabelKey = "fleet.azure.com/subscription-id"
)

// AzureCapacityService defines the interface for validating Azure capacity requirements.
type AzureCapacityService interface {

	// ValidateCapacityRequirement validates a capacity requirement against Azure APIs.
	ValidateCapacityRequirement(
		cluster *clusterv1beta1.MemberCluster,
		req placementv1beta1.PropertySelectorRequirement,
	) (bool, error)
}
