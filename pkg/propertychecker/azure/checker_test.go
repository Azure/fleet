//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

package azure

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	computev1 "go.goms.io/fleet/apis/protos/azure/compute/v1"
	"go.goms.io/fleet/pkg/clients/azure/compute"
	"go.goms.io/fleet/pkg/utils"
)

func TestExtractCapacityRequirements(t *testing.T) {
	tests := []struct {
		name           string
		req            placementv1beta1.PropertySelectorRequirement
		wantSKU        string
		wantQty        uint32
		wantError      bool
		errorSubstring string
	}{
		{
			name: "valid Azure SKU capacity property",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D4s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"4"},
			},
			wantSKU:   "Standard_D4s_v3",
			wantQty:   4,
			wantError: false,
		},
		{
			name: "invalid Azure SKU capacity property exceeding max limit",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_B2ms/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"8Gi"},
			},
			wantSKU:        "Standard_B2ms",
			wantError:      true,
			errorSubstring: "exceeds maximum allowed value of 200",
		},
		{
			name: "invalid Azure SKU capacity property with decimal",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_F4s_v2/capacity",
				Operator: placementv1beta1.PropertySelectorLessThan,
				Values:   []string{"3.5"},
			},
			wantSKU:        "Standard_F4s_v2",
			wantError:      true,
			errorSubstring: "must be a whole number, decimal values are not allowed for VM instance count",
		},
		{
			name: "missing SKU in property name",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes//capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"4"},
			},
			wantError:      true,
			errorSubstring: "property name \"kubernetes.azure.com/vm-sizes//capacity\" does not match expected SKU capacity format \"kubernetes.azure.com/vm-sizes/%s/capacity\"",
		},
		{
			name: "no values",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D4s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{},
			},
			wantError:      true,
			errorSubstring: "must have exactly one value, got 0",
		},
		{
			name: "multiple values",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D4s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"4", "8"},
			},
			wantError:      true,
			errorSubstring: "must have exactly one value, got 2",
		},
		{
			name: "invalid capacity value",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D4s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"invalid-quantity"},
			},
			wantError:      true,
			errorSubstring: "failed to parse capacity value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capacity, sku, err := extractCapacityRequirements(tt.req)

			if tt.wantError {
				if err == nil {
					t.Errorf("extractCapacityRequirements() expected error but got none")
				} else if tt.errorSubstring != "" && !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("extractCapacityRequirements() error = %v, got %v", tt.errorSubstring, err)
				}
				return
			}

			if err != nil {
				t.Errorf("extractCapacityRequirements() unexpected error = %v", err)
				return
			}

			if sku != tt.wantSKU {
				t.Errorf("extractCapacityRequirements() sku = %v, got %v", tt.wantSKU, sku)
			}

			if capacity != tt.wantQty {
				t.Errorf("extractCapacityRequirements() capacity = %v, got %v", tt.wantQty, capacity)
			}
		})
	}
}

// Mock the expected response from the Azure API.
var mockAzureResponse = `{
    "recommendedVmSizes": {
        "regularVmSizes": [
            {
                "family": "Dsv3",
                "name": "Standard_D2s_v3",
                "size": "D2"
            },
            {
                "family": "Standard",
                "name": "Standard_B1s",
                "size": "Standard_B1s"
            }
        ]
    }
}`

func TestCheckIfMeetSKUCapacityRequirement(t *testing.T) {
	// Prepare test data
	cluster := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				utils.AzureLocationLabelKey:       "centraluseuap",
				utils.AzureSubscriptionIDLabelKey: "8ecadfc9-d1a3-4ea4-b844-0d9f87e4d7c8",
			},
		},
	}

	tests := []struct {
		name           string
		cluster        *clusterv1beta1.MemberCluster
		selector       placementv1beta1.PropertySelectorRequirement
		mockStatusCode int
		wantAvailable  bool
		expectError    bool
		errorSubstring string
	}{
		{
			name:    "valid request",
			cluster: cluster,
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"1"},
			},
			mockStatusCode: http.StatusOK,
			wantAvailable:  true,
			expectError:    false,
		},
		{
			name:    "invalid capacity request",
			cluster: cluster,
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D3s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"2"},
			},
			mockStatusCode: http.StatusOK,
			wantAvailable:  false,
			expectError:    false,
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
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"10"},
			},
			expectError:    true,
			errorSubstring: "failed to extract Azure location label from cluster : label \"fleet.azure.com/location\" not found in cluster",
		},
		{
			name:    "unavailable SKU",
			cluster: cluster,
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D2s_v4/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"10"},
			},
			mockStatusCode: http.StatusOK,
			wantAvailable:  false,
			expectError:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method
				if r.Method != http.MethodPost {
					t.Errorf("got %s, want POST request", r.Method)
				}

				// Verify headers
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("got %s, want Content-Type: application/json", r.Header.Get("Content-Type"))
				}
				if r.Header.Get("Accept") != "application/json" {
					t.Errorf("got %s, want Accept: application/json", r.Header.Get("Accept"))
				}

				// Verify request body using protojson for proper proto3 oneof support
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("failed to read request body: %v", err)
				}
				var req computev1.GenerateAttributeBasedRecommendationsRequest
				unmarshaler := protojson.UnmarshalOptions{
					DiscardUnknown: true,
				}
				if err := unmarshaler.Unmarshal(body, &req); err != nil {
					t.Fatalf("failed to unmarshal request body: %v", err)
				}

				// Write mock response with status code from test case
				if tt.mockStatusCode == 0 {
					tt.mockStatusCode = http.StatusOK
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.mockStatusCode)

				if _, err := w.Write([]byte(mockAzureResponse)); err != nil {
					t.Fatalf("failed to write response: %v", err)
				}
			}))
			defer server.Close()

			client, err := compute.NewAttributeBasedVMSizeRecommenderClient(server.URL, http.DefaultClient)
			if err != nil {
				t.Fatalf("failed to create VM size recommender client: %v", err)
			}
			svc := NewPropertyChecker(*client)
			result, err := svc.CheckIfMeetSKUCapacityRequirement(tt.cluster, tt.selector)
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
