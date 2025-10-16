package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// DefaultAzureCapacityService is the default implementation of AzureCapacityService.
type DefaultAzureCapacityService struct {
	endpoint string
	client   *http.Client
}

// Compile-time check to ensure DefaultAzureCapacityService implements AzureCapacityService
var _ AzureCapacityService = &DefaultAzureCapacityService{}

// NewAzureCapacityService creates a new default Azure capacity service with the given endpoint.
func NewAzureCapacityService(endpoint string) *DefaultAzureCapacityService {
	return &DefaultAzureCapacityService{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ValidateCapacityRequirement validates a capacity requirement against Azure APIs.
func (s *DefaultAzureCapacityService) ValidateCapacityRequirement(
	cluster *clusterv1beta1.MemberCluster,
	req placementv1beta1.PropertySelectorRequirement,
) (bool, error) {
	ctx := context.Background()
	subID, location := extractAzureInfoFromLabels(cluster)
	if subID == "" || location == "" {
		return false, fmt.Errorf("cluster %s does not have required Azure labels", cluster.Name)
	}
	capacity, sku, err := extractCapacityRequirements(req)
	if err != nil {
		return false, fmt.Errorf("failed to extract capacity requirement: %w", err)
	}

	url := fmt.Sprintf("%s/fleet/subscriptions/%s/providers/Microsoft.Compute/locations/%s/vmSizeRecommendations/vmAttributeBased/generate",
		s.endpoint, subID, location)
	payload := map[string]interface{}{
		"regular_priority_profile": map[string]interface{}{
			"capacity_unit_type": "CAPACITY_UNIT_TYPE_VM_INSTANCE_COUNT",
			"target_capacity":    capacity.Value(),
		},
		"recommendation_properties": map[string]interface{}{
			"restrictions_filter": "RESTRICTIONS_FILTER_QUOTA_AND_OFFER_RESTRICTIONS",
		},
		"resource_properties": map[string]interface{}{},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return false, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return false, fmt.Errorf("failed to make HTTP request to Azure service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("Azure service returned status %d", resp.StatusCode)
	}

	var respObj struct {
		//TODO: add other fields
		RecommendedVmSizes struct {
			RegularVmSizes []struct {
				Name string `json:"name"`
			} `json:"regularVmSizes"`
		} `json:"recommendedVmSizes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respObj); err != nil {
		return false, fmt.Errorf("failed to decode Azure service response: %w", err)
	}

	available := false
	for _, vm := range respObj.RecommendedVmSizes.RegularVmSizes {
		if vm.Name == sku {
			available = true
			break
		}
	}

	return available, nil
}

// extractAzureInfoFromLabels extracts subscription ID and location from MemberCluster labels.
// Returns the subscription ID and location if found, empty strings otherwise.
func extractAzureInfoFromLabels(cluster *clusterv1beta1.MemberCluster) (subscriptionID, location string) {
	if cluster == nil || cluster.Labels == nil {
		return "", ""
	}

	subscriptionID = cluster.Labels[AzureSubscriptionIDLabelKey]
	location = cluster.Labels[AzureLocationLabelKey]

	return subscriptionID, location
}

// extractCapacityRequirements extracts the capacity value from a PropertySelectorRequirement.
// This function is specifically designed for Azure SKU capacity properties that follow the pattern:
// "kubernetes.azure.com/vm-size/{sku}/capacity"
// Returns the capacity as a resource.Quantity and the SKU name if the requirement is valid,
// or an error if the requirement is invalid or not a capacity property.
func extractCapacityRequirements(req placementv1beta1.PropertySelectorRequirement) (*resource.Quantity, string, error) {
	// Extract SKU from the property name
	// Expected format: "kubernetes.azure.com/vm-size/{sku}/capacity"
	if !strings.HasSuffix(req.Name, "/capacity") {
		return nil, "", fmt.Errorf("invalid Azure SKU capacity property format: %q", req.Name)
	}

	// Remove prefix and suffix to get the SKU
	sku := strings.TrimSuffix(strings.TrimPrefix(req.Name, SkuCapacityPropertyPrefix+"/"), "/capacity")
	if sku == "" {
		return nil, "", fmt.Errorf("cannot extract SKU from property name: %q", req.Name)
	}

	// Validate that we have exactly one value
	if len(req.Values) != 1 {
		return nil, "", fmt.Errorf("Azure SKU capacity property must have exactly one value, got %d", len(req.Values))
	}

	// Parse the capacity value
	capacity, err := resource.ParseQuantity(req.Values[0])
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse capacity value %q: %w", req.Values[0], err)
	}

	return &capacity, sku, nil
}
