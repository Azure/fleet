//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

// Package azure provides property checkers for Azure-specific cluster requirements.
// It checks whether the cluster can meet the requirement defined by the property selector.
package azure

import (
	"context"
	"fmt"
	"regexp"

	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/labels"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	computev1 "go.goms.io/fleet/apis/protos/azure/compute/v1"
	"go.goms.io/fleet/pkg/clients/azure/compute"
	"go.goms.io/fleet/pkg/propertyprovider/azure"
)

const (
	// MaxVMInstanceCapacity defines the maximum allowed VM instance capacity for SKU capacity requirements.
	// This limit is set to prevent excessive resource requests and potential quota issues.
	// The value is constrained by uint32 maximum (4,294,967,295) but set to a reasonable upper bound
	// of 200 for most production workloads.
	MaxVMInstanceCapacity = 200

	// MinVMInstanceCapacity defines the minimum allowed VM instance capacity for SKU capacity requirements.
	// Capacity must be at least 1 to be meaningful.
	MinVMInstanceCapacity = 1

	// uint32MaxValue represents the maximum value that can be stored in a uint32
	uint32MaxValue = int64(^uint32(0))
)

var (
	// skuCapacityRegex extracts SKU name from capacity property names.
	// Based on SKUCapacityPropertyTmpl = "kubernetes.azure.com/vm-sizes/%s/capacity"
	skuCapacityRegex = regexp.MustCompile(`^` + regexp.QuoteMeta(azure.SkuPropertyPrefix) + `/([^/]+)/capacity$`)
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

// CheckIfMeetSKUCapacityRequirement validates whether a member cluster can meet the specified
// SKU capacity requirement. It extracts the required SKU and capacity from the property selector
// requirement and checks to determine if the cluster's Azure subscription
// and location can provision the requested VM instances.
//
// The cluster must have both Azure location and subscription ID labels configured.
// Returns true if the SKU capacity requirement can be met, false otherwise.
func (s *PropertyChecker) CheckIfMeetSKUCapacityRequirement(
	cluster *clusterv1beta1.MemberCluster,
	req placementv1beta1.PropertySelectorRequirement,
) (bool, error) {
	location, err := labels.ExtractLabelFromMemberCluster(cluster, utils.AzureLocationLabelKey)
	if err != nil {
		return false, fmt.Errorf("failed to extract Azure location label from cluster %s: %w", cluster.Name, err)
	}

	subID, err := labels.ExtractLabelFromMemberCluster(cluster, utils.AzureSubscriptionIDLabelKey)
	if err != nil {
		return false, fmt.Errorf("failed to extract Azure subscription ID label from cluster %s: %w", cluster.Name, err)
	}

	capacity, sku, err := extractCapacityRequirements(req)
	if err != nil {
		return false, fmt.Errorf("failed to extract capacity requirement: %w", err)
	}

	request := &computev1.GenerateAttributeBasedRecommendationsRequest{
		SubscriptionId: subID,
		Location:       location,
		RegularPriorityProfile: &computev1.RegularPriorityProfile{
			CapacityUnitType: computev1.CapacityUnitType_CAPACITY_UNIT_TYPE_VM_INSTANCE_COUNT,
			TargetCapacity:   capacity,
		},
		ResourceProperties: &computev1.ResourceProperties{
			VmAttributes: &computev1.VMAttributes{
				AllowedVmSizes: []string{sku},
			},
		},
		RecommendationProperties: &computev1.RecommendationProperties{
			RestrictionsFilter: computev1.RecommendationProperties_RESTRICTIONS_FILTER_QUOTA_AND_OFFER_RESTRICTIONS,
		},
	}

	respObj, err := s.vmSizeRecommenderClient.GenerateAttributeBasedRecommendations(context.Background(), request)
	if err != nil {
		return false, fmt.Errorf("failed to generate VM size recommendations from Azure: %w", err)
	}

	available := false
	for _, vm := range respObj.RecommendedVmSizes.RegularVmSizes {
		if vm.Name == sku {
			available = true
			klog.V(2).Infof("SKU %s is available in cluster %s", sku, cluster.Name)
			break
		}
	}

	return available, nil
}

// extractCapacityRequirements extracts the capacity value from a PropertySelectorRequirement.
// This function is specifically designed for Azure SKU capacity properties that follow the pattern:
// "kubernetes.azure.com/vm-sizes/{sku}/capacity"
// Returns the capacity as a resource.Quantity and the SKU name if the requirement is valid,
// or an error if the requirement is invalid or not a capacity property.
func extractCapacityRequirements(req placementv1beta1.PropertySelectorRequirement) (uint32, string, error) {
	// Extract SKU from the property name using regex
	// Expected format: "kubernetes.azure.com/vm-sizes/{sku}/capacity"
	matches := skuCapacityRegex.FindStringSubmatch(req.Name)
	if len(matches) != 2 {
		return 0, "", fmt.Errorf("property name %q does not match expected SKU capacity format %q", req.Name, azure.SKUCapacityPropertyTmpl)
	}
	sku := matches[1]

	// Validate that we have exactly one value
	if len(req.Values) != 1 {
		return 0, "", fmt.Errorf("azure SKU capacity property must have exactly one value, got %d", len(req.Values))
	}

	// Parse the capacity value
	capacity, err := resource.ParseQuantity(req.Values[0])
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse capacity value %q: %w", req.Values[0], err)
	}

	// Ensure capacity is a whole number (integer) since VM instance count cannot be fractional
	milliValue := capacity.MilliValue()
	if milliValue%1000 != 0 {
		return 0, "", fmt.Errorf("capacity value %q must be a whole number, decimal values are not allowed for VM instance count", req.Values[0])
	}

	// Get the integer value for validation
	capacityValue := capacity.Value()

	// Validate capacity is non-negative
	if capacityValue < 0 {
		return 0, "", fmt.Errorf("capacity value %d cannot be negative", capacityValue)
	}

	// Validate capacity bounds
	if capacityValue < MinVMInstanceCapacity {
		return 0, "", fmt.Errorf("capacity value %d is below minimum allowed value of %d", capacityValue, MinVMInstanceCapacity)
	}
	if capacityValue > MaxVMInstanceCapacity {
		return 0, "", fmt.Errorf("capacity value %d exceeds maximum allowed value of %d", capacityValue, MaxVMInstanceCapacity)
	}

	// Ensure capacity can be safely converted to uint32
	if capacityValue > uint32MaxValue {
		return 0, "", fmt.Errorf("capacity value %d exceeds uint32 maximum value %d", capacityValue, uint32MaxValue)
	}

	// Safe conversion to uint32 - all validations passed
	return uint32(capacityValue), sku, nil
}
