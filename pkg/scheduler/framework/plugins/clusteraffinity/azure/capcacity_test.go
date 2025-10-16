package azure

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestExtractAzureInfoFromLabels(t *testing.T) {
	tests := []struct {
		name                   string
		cluster                *clusterv1beta1.MemberCluster
		expectedSubscriptionID string
		expectedLocation       string
	}{
		{
			name: "cluster with both subscription ID and location labels",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
					Labels: map[string]string{
						AzureSubscriptionIDLabelKey: "12345678-1234-1234-1234-123456789012",
						AzureLocationLabelKey:       "eastus",
						"other-label":               "other-value",
					},
				},
			},
			expectedSubscriptionID: "12345678-1234-1234-1234-123456789012",
			expectedLocation:       "eastus",
		},
		{
			name: "cluster with only subscription ID label",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
					Labels: map[string]string{
						AzureSubscriptionIDLabelKey: "12345678-1234-1234-1234-123456789012",
						"other-label":               "other-value",
					},
				},
			},
			expectedSubscriptionID: "12345678-1234-1234-1234-123456789012",
			expectedLocation:       "",
		},
		{
			name: "cluster with only location label",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
					Labels: map[string]string{
						AzureLocationLabelKey: "westus",
						"other-label":         "other-value",
					},
				},
			},
			expectedSubscriptionID: "",
			expectedLocation:       "westus",
		},
		{
			name: "cluster with no Azure labels",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
					Labels: map[string]string{
						"other-label": "other-value",
					},
				},
			},
			expectedSubscriptionID: "",
			expectedLocation:       "",
		},
		{
			name: "cluster with no labels",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
			},
			expectedSubscriptionID: "",
			expectedLocation:       "",
		},
		{
			name:                   "nil cluster",
			cluster:                nil,
			expectedSubscriptionID: "",
			expectedLocation:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subscriptionID, location := extractAzureInfoFromLabels(tt.cluster)

			if subscriptionID != tt.expectedSubscriptionID {
				t.Errorf("extractAzureInfoFromLabels() subscriptionID = %v, expected %v", subscriptionID, tt.expectedSubscriptionID)
			}

			if location != tt.expectedLocation {
				t.Errorf("extractAzureInfoFromLabels() location = %v, expected %v", location, tt.expectedLocation)
			}
		})
	}
}

func TestExtractCapacityRequirements(t *testing.T) {
	tests := []struct {
		name           string
		req            placementv1beta1.PropertySelectorRequirement
		expectedSKU    string
		expectedQty    string
		expectError    bool
		errorSubstring string
	}{
		{
			name: "valid Azure SKU capacity property",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-size/Standard_D4s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"4"},
			},
			expectedSKU: "Standard_D4s_v3",
			expectedQty: "4",
			expectError: false,
		},
		{
			name: "valid Azure SKU capacity property with memory",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-size/Standard_B2ms/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"8Gi"},
			},
			expectedSKU: "Standard_B2ms",
			expectedQty: "8Gi",
			expectError: false,
		},
		{
			name: "valid Azure SKU capacity property with decimal",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-size/Standard_F4s_v2/capacity",
				Operator: placementv1beta1.PropertySelectorLessThan,
				Values:   []string{"3.5"},
			},
			expectedSKU: "Standard_F4s_v2",
			expectedQty: "3.5",
			expectError: false,
		},
		{
			name: "Azure SKU property but wrong suffix",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-size/Standard_D4s_v3/usage",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"4"},
			},
			expectError:    true,
			errorSubstring: "invalid Azure SKU capacity property format",
		},
		{
			name: "missing SKU in property name",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-size//capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"4"},
			},
			expectError:    true,
			errorSubstring: "cannot extract SKU from property name",
		},
		{
			name: "no values",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-size/Standard_D4s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{},
			},
			expectError:    true,
			errorSubstring: "must have exactly one value, got 0",
		},
		{
			name: "multiple values",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-size/Standard_D4s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"4", "8"},
			},
			expectError:    true,
			errorSubstring: "must have exactly one value, got 2",
		},
		{
			name: "invalid capacity value",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-size/Standard_D4s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"invalid-quantity"},
			},
			expectError:    true,
			errorSubstring: "failed to parse capacity value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capacity, sku, err := extractCapacityRequirements(tt.req)

			if tt.expectError {
				if err == nil {
					t.Errorf("extractCapacityRequirements() expected error but got none")
				} else if tt.errorSubstring != "" && !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("extractCapacityRequirements() error = %v, expected to contain %v", err, tt.errorSubstring)
				}
				return
			}

			if err != nil {
				t.Errorf("extractCapacityRequirements() unexpected error = %v", err)
				return
			}

			if sku != tt.expectedSKU {
				t.Errorf("extractCapacityRequirements() sku = %v, expected %v", sku, tt.expectedSKU)
			}

			if capacity == nil {
				t.Errorf("extractCapacityRequirements() capacity is nil")
				return
			}

			expectedCapacity := resource.MustParse(tt.expectedQty)
			if !capacity.Equal(expectedCapacity) {
				t.Errorf("extractCapacityRequirements() capacity = %v, expected %v", capacity, expectedCapacity)
			}
		})
	}
}

func TestValidateCapacityRequirement(t *testing.T) {
	// Mock response JSON for recommendedVmSizes
	mockResponse := map[string]interface{}{
		"recommendedVmSizes": map[string]interface{}{
			"regularVmSizes": []map[string]interface{}{
				{"name": "Standard_D2s_v3"},
				{"name": "Standard_A1_v2"},
			},
		},
	}
	respBytes, _ := json.Marshal(mockResponse)

	// Start a test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(respBytes)
	}))
	defer ts.Close()

	// Create the service pointing to the test server
	svc := &DefaultAzureCapacityService{
		endpoint: ts.URL,
		client:   ts.Client(),
	}

	// Prepare test data
	cluster := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"fleet.azure.com/location":        "centraluseuap",
				"fleet.azure.com/subscription-id": "8ecadfc9-d1a3-4ea4-b844-0d9f87e4d7c8",
			},
		},
	}

	tests := []struct {
		name           string
		cluster        *clusterv1beta1.MemberCluster
		req            placementv1beta1.PropertySelectorRequirement
		wantAvailable  bool
		expectError    bool
		errorSubstring string
	}{
		{
			name:    "valid request",
			cluster: cluster,
			req: placementv1beta1.PropertySelectorRequirement{
				Name:   "kubernetes.azure.com/vm-size/Standard_D2s_v3/capacity",
				Values: []string{"10"},
			},
			wantAvailable: true,
			expectError:   false,
		},
		{
			name:    "invalid capacity request",
			cluster: cluster,
			req: placementv1beta1.PropertySelectorRequirement{
				Name:   "kubernetes.azure.com/vm-size/Standard_D3s_v3/capacity",
				Values: []string{"2"},
			},
			wantAvailable: false,
			expectError:   false,
		},
		{
			name: "missing Azure labels",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"other-label": "other-value",
					},
				},
			},
			req: placementv1beta1.PropertySelectorRequirement{
				Name:   "kubernetes.azure.com/vm-size/Standard_D2s_v3/capacity",
				Values: []string{"10"},
			},
			expectError:    true,
			errorSubstring: "does not have required Azure labels",
		},
		{
			name:    "invalid capacity property",
			cluster: cluster,
			req: placementv1beta1.PropertySelectorRequirement{
				Name:   "kubernetes.azure.com/vm-size/Standard_D2s_v3/usage",
				Values: []string{"10"},
			},
			expectError:    true,
			errorSubstring: "invalid Azure SKU capacity property format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.ValidateCapacityRequirement(tt.cluster, tt.req)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error but got none")
				} else if tt.errorSubstring != "" && !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("error = %v, expected to contain %v", err, tt.errorSubstring)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.wantAvailable {
				t.Errorf("expected Available=%v, got %v", tt.wantAvailable, result)
			}
		})
	}
}
