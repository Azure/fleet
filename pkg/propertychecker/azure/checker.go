//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

// Package azure provides property checkers for Azure-specific cluster requirements.
// It checks whether the cluster can meet the requirement defined by the property selector.
package azure

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/klog/v2"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	computev1 "go.goms.io/fleet/apis/protos/azure/compute/v1"
	"go.goms.io/fleet/pkg/clients/azure/compute"
	"go.goms.io/fleet/pkg/utils/labels"
)

const (
	// maxVMInstanceCapacity defines the maximum allowed VM instance capacity for SKU capacity requirements.
	// This limit is set to prevent excessive resource requests and potential quota issues.
	// The value is constrained to a reasonable upper bound of 200 for most production workloads.
	// Upper bound is enforced after adjusting for operator semantics.
	maxVMInstanceCapacity = 200

	// minVMInstanceCapacity defines the minimum allowed VM instance capacity for SKU capacity requirements.
	// The Azure Capacity API requires capacity values greater than 0, so minimum is set to 1.
	// Lower bound is enforced after adjusting for operator semantics.
	minVMInstanceCapacity = 1
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
// requirement and checks whether the cluster's Azure subscription and location can provision
// the requested VM instances.
//
// The cluster must have both Azure location and subscription ID labels configured.
// Returns true if the SKU capacity requirement can be met, false otherwise.
func (s *PropertyChecker) CheckIfMeetSKUCapacityRequirement(
	cluster *clusterv1beta1.MemberCluster,
	req placementv1beta1.PropertySelectorRequirement,
	sku string,
) (bool, error) {
	location, err := labels.ExtractLabelFromMemberCluster(cluster, labels.AzureLocationLabel)
	if err != nil {
		return false, fmt.Errorf("failed to extract Azure location label from cluster %s: %w", cluster.Name, err)
	}

	subID, err := labels.ExtractLabelFromMemberCluster(cluster, labels.AzureSubscriptionIDLabel)
	if err != nil {
		return false, fmt.Errorf("failed to extract Azure subscription ID label from cluster %s: %w", cluster.Name, err)
	}

	// Extract capacity requirements from the property selector requirement.
	capacity, err := extractCapacityRequirements(req)
	if err != nil {
		return false, fmt.Errorf("failed to extract capacity requirements from property selector requirement: %w", err)
	}

	// Request VM size recommendations to validate SKU availability and capacity.
	// The capacity is checked by ensuring the current allocatable capacity is greater than the requested capacity.
	klog.V(2).Infof("Checking SKU %s with capacity %d in cluster %s", sku, capacity, cluster.Name)
	request := &computev1.GenerateAttributeBasedRecommendationsRequest{
		SubscriptionId: subID,
		Location:       location,
		RegularPriorityProfile: &computev1.RegularPriorityProfile{
			CapacityUnitType: computev1.CapacityUnitType_CAPACITY_UNIT_TYPE_VM_INSTANCE_COUNT,
			TargetCapacity:   capacity, // CurrentAllocatableCapacity > RequestedCapacity
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

	// This check is a defense mechanism; vmSizeRecommenderClient should return a VM size recommendation
	// if the SKU is available in the specified location and subscription.
	available := false
	for _, vm := range respObj.RecommendedVmSizes.RegularVmSizes {
		if strings.EqualFold(vm.Name, sku) {
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
// Returns the capacity value adjusted for the operator semantics, as the VMSizeRecommender API
// checks whether current allocatable capacity is greater than the requested capacity.
func extractCapacityRequirements(req placementv1beta1.PropertySelectorRequirement) (uint32, error) {
	if req.Operator != placementv1beta1.PropertySelectorGreaterThan && req.Operator != placementv1beta1.PropertySelectorGreaterThanOrEqualTo {
		return 0, fmt.Errorf("unsupported operator %q for SKU capacity property, only GreaterThan (Gt) and GreaterThanOrEqualTo (Ge) are supported", req.Operator)
	}

	// Validate that we have exactly one value.
	if len(req.Values) != 1 {
		return 0, fmt.Errorf("azure SKU capacity property must have exactly one value, got %d", len(req.Values))
	}

	capacity, err := validateCapacity(req.Values[0], req.Operator)
	if err != nil {
		return 0, fmt.Errorf("failed to validate capacity value %q: %w", req.Values[0], err)
	}

	// Safe conversion to uint32 - all validations passed
	return capacity, nil
}

// validateCapacity checks if the provided capacity value is valid.
// Returns the capacity as uint32 if valid, or a zero and an error if invalid.
func validateCapacity(value string, operator placementv1beta1.PropertySelectorOperator) (uint32, error) {
	// Parse directly as uint32 to avoid integer overflow issues.
	valueUint, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid capacity value %q: %w", value, err)
	}

	capacity := uint32(valueUint) // capacity is >= 0 since it's parsed as uint.

	// Adjust capacity based on operator semantics for VMSizeRecommender API.
	// If operator is GreaterThanOrEqualTo, decrement capacity by 1 as the API checks for strictly greater than.
	if operator == placementv1beta1.PropertySelectorGreaterThanOrEqualTo && capacity > minVMInstanceCapacity {
		capacity -= 1
	}

	// Validate against maximum allowed capacity (exceed maxVMInstanceCapacity).
	if capacity >= maxVMInstanceCapacity {
		return 0, fmt.Errorf("capacity value %d exceeds maximum allowed value of %d", capacity, maxVMInstanceCapacity)
	}

	// Ensure capacity meets minimum requirements (minVMInstanceCapacity) after operator adjustment.
	// The VMSizeRecommender API requires capacity > 0.
	if capacity < minVMInstanceCapacity {
		capacity = minVMInstanceCapacity
	}

	return capacity, nil
}
