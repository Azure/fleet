//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

package azure

import (
	"fmt"
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
	"go.goms.io/fleet/pkg/clients/httputil"
	"go.goms.io/fleet/pkg/propertyprovider/azure"
	"go.goms.io/fleet/pkg/utils/labels"
)

func TestSupportsProperty(t *testing.T) {
	propertyChecker := &PropertyChecker{}

	tests := []struct {
		name         string
		propertyName string
		want         bool
	}{
		{
			name:         "supported Azure SKU capacity property",
			propertyName: fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D2s_v3"),
			want:         true,
		},
		{
			name:         "unsupported property name",
			propertyName: "fleet.azure.com/unsupported-property",
			want:         false,
		},
		{
			name:         "similar but unsupported property name with extra suffix",
			propertyName: fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D2s_v3") + "-extra-suffix",
			want:         false,
		},
		{
			name:         "unsupported property name with missing SKU",
			propertyName: fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, ""),
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := propertyChecker.SupportsProperty(tt.propertyName)
			if got != tt.want {
				t.Errorf("SupportsProperty() = %v, want %v", got, tt.want)
			}
		})
	}

}
func TestValidateCapacity(t *testing.T) {
	tests := []struct {
		name           string
		req            placementv1beta1.PropertySelectorRequirement
		wantValue      uint32
		wantError      bool
		errorSubstring string
	}{
		{
			name: "valid capacity value for GreaterThan operator",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D2s_v3"),
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"10"},
			},
			wantValue: 10,
			wantError: false,
		},
		{
			name: "valid capacity value for GreaterThanOrEqualTo operator",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D2s_v3"),
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"50"},
			},
			wantValue: 49,
			wantError: false,
		},
		{
			name: "capacity value exceeds maximum limit",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D2s_v3"),
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"201"},
			},
			wantError:      true,
			errorSubstring: "exceeds maximum allowed value of 200",
		},
		{
			name: "unsupported operator for capacity of zero",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D2s_v3"),
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"0"},
			},
			wantError:      true,
			errorSubstring: "capacity value cannot be zero for operator",
		},
		{
			name: "capacity equal to max limit with GreaterThan operator",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D2s_v3"),
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"200"},
			},
			wantError:      true,
			errorSubstring: "exceeds maximum allowed value",
		},
		{
			name: "supported operator with capacity of zero",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D2s_v3"),
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"0"},
			},
			wantValue: 0,
			wantError: false,
		},
		{
			name: "capacity equal to max limit with supported operator",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D2s_v3"),
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"200"},
			},
			wantValue: 199,
			wantError: false,
		},
		{
			name: "capacity above maximum limit",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D2s_v3"),
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"300"},
			},
			wantError:      true,
			errorSubstring: "exceeds maximum allowed value",
		},
		{
			name: "capacity at minimum limit",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D2s_v3"),
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"1"},
			},
			wantValue: 1,
			wantError: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := validateCapacity(tt.req)

			if tt.wantError {
				if err == nil {
					t.Fatalf("validateCapacity() = nil, want err")
				} else if tt.errorSubstring != "" && !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Fatalf("validateCapacity() error = %v, want %v", err, tt.errorSubstring)
				}
				return
			}

			if err != nil {
				t.Fatalf("validateCapacity() = %v, want no err", err)
				return
			}

			if value != tt.wantValue {
				t.Errorf("validateCapacity() = %v, want %v", value, tt.wantValue)
			}
		})
	}
}

func TestExtractCapacityRequirements(t *testing.T) {
	validProperty := fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D2s_v3")

	tests := []struct {
		name              string
		req               placementv1beta1.PropertySelectorRequirement
		wantSKU           string
		wantCapacityValue uint32
		wantError         bool
		errorSubstring    string
	}{
		{
			name: "valid Azure SKU capacity property",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     validProperty,
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"4"},
			},
			wantSKU:           "Standard_D4s_v3",
			wantCapacityValue: 4,
			wantError:         false,
		},
		{
			name: "invalid Azure SKU capacity property exceeding max limit",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_B2ms"),
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"201"},
			},
			wantSKU:        "Standard_B2ms",
			wantError:      true,
			errorSubstring: "exceeds maximum allowed value of 200",
		},
		{
			name: "invalid Azure SKU capacity property with decimal",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_F4s_v2"),
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"3.5"},
			},
			wantSKU:        "Standard_F4s_v2",
			wantError:      true,
			errorSubstring: "failed to validate capacity value",
		},
		{
			name: "no values",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     validProperty,
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{},
			},
			wantError:      true,
			errorSubstring: "must have exactly 1 value(s), got 0",
		},
		{
			name: "multiple values",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D4s_v3"),
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"4", "8"},
			},
			wantError:      true,
			errorSubstring: "must have exactly 1 value(s), got 2",
		},
		{
			name: "invalid capacity value",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     validProperty,
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"invalid-quantity"},
			},
			wantError:      true,
			errorSubstring: "failed to validate capacity value",
		},
		{
			name: "unsupported operator EqualTo",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     validProperty,
				Operator: placementv1beta1.PropertySelectorEqualTo,
				Values:   []string{"4"},
			},
			wantError:      true,
			errorSubstring: "unsupported operator \"Eq\" for property \"kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity\", only GreaterThan (Gt) and GreaterThanOrEqualTo (Ge) are supported",
		},
		{
			name: "unsupported operator LessThanOrEqualTo",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     validProperty,
				Operator: placementv1beta1.PropertySelectorLessThanOrEqualTo,
				Values:   []string{"4"},
			},
			wantError:      true,
			errorSubstring: "unsupported operator \"Le\" for property \"kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity\", only GreaterThan (Gt) and GreaterThanOrEqualTo (Ge) are supported",
		},
		{
			name: "unsupported operator LessThan",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     validProperty,
				Operator: placementv1beta1.PropertySelectorLessThan,
				Values:   []string{"4"},
			},
			wantError:      true,
			errorSubstring: "unsupported operator \"Lt\" for property \"kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity\", only GreaterThan (Gt) and GreaterThanOrEqualTo (Ge) are supported",
		},
		{
			name: "unsupported operator NotEqualTo",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     validProperty,
				Operator: placementv1beta1.PropertySelectorNotEqualTo,
				Values:   []string{"4"},
			},
			wantError:      true,
			errorSubstring: "unsupported operator \"Ne\" for property \"kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity\", only GreaterThan (Gt) and GreaterThanOrEqualTo (Ge) are supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capacity, err := extractCapacityRequirements(tt.req)

			if tt.wantError {
				if err == nil {
					t.Fatalf("extractCapacityRequirements() = nil, want err")
				} else if tt.errorSubstring != "" && !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Fatalf("extractCapacityRequirements() error = %v, want %v", err, tt.errorSubstring)
				}
				return
			}

			if err != nil {
				t.Fatalf("extractCapacityRequirements() = %v, want no err", err)
				return
			}

			if capacity != tt.wantCapacityValue {
				t.Errorf("extractCapacityRequirements() capacity = %v, wantt %v", capacity, tt.wantCapacityValue)
			}
		})
	}
}

func TestCheckIfMeetSKUCapacityRequirement(t *testing.T) {
	// Prepare test data
	validSKU := "Standard_D2s_v3"
	validPropertySelectorRequirement := placementv1beta1.PropertySelectorRequirement{
		Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, validSKU),
		Operator: placementv1beta1.PropertySelectorGreaterThan,
		Values:   []string{"3"},
	}
	cluster := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				labels.AzureLocationLabel:       "centraluseuap",
				labels.AzureSubscriptionIDLabel: "8ecadfc9-d1a3-4ea4-b844-0d9f87e4d7c8",
			},
		},
	}

	tests := []struct {
		name           string
		cluster        *clusterv1beta1.MemberCluster
		sku            string
		req            placementv1beta1.PropertySelectorRequirement
		mockStatusCode int
		wantAvailable  bool
		wantError      bool
		errorSubstring string
	}{
		{
			name:           "valid capacity request",
			cluster:        cluster,
			sku:            validSKU,
			req:            validPropertySelectorRequirement,
			mockStatusCode: http.StatusOK,
			wantAvailable:  true,
			wantError:      false,
		},
		{
			name:    "unavailable SKU request",
			cluster: cluster,
			sku:     "Standard_D2s_v4",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "Standard_D2s_v4"),
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"2"},
			},
			mockStatusCode: http.StatusOK,
			wantAvailable:  false,
			wantError:      false,
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
			sku:            validSKU,
			req:            validPropertySelectorRequirement,
			wantError:      true,
			errorSubstring: "failed to extract Azure location label from cluster : label \"fleet.azure.com/location\" not found in cluster",
		},
		{
			name: "missing Azure location label",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labels.AzureSubscriptionIDLabel: "8ecadfc9-d1a3-4ea4-b844-0d9f87e4d7c8",
					},
				},
			},
			sku:            validSKU,
			req:            validPropertySelectorRequirement,
			wantError:      true,
			errorSubstring: "failed to extract Azure location label from cluster",
		},
		{
			name: "missing Azure subscription ID label",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labels.AzureLocationLabel: "centraluseuap",
					},
				},
			},
			sku:            validSKU,
			req:            validPropertySelectorRequirement,
			wantError:      true,
			errorSubstring: "failed to extract Azure subscription ID label from cluster",
		},
		{
			name:           "Azure API returns error",
			cluster:        cluster,
			sku:            validSKU,
			req:            validPropertySelectorRequirement,
			mockStatusCode: http.StatusInternalServerError,
			wantError:      true,
			errorSubstring: "failed to generate VM size recommendations from Azure",
		},
		{
			name:    "invalid operator in requirement",
			cluster: cluster,
			sku:     validSKU,
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, validSKU),
				Operator: placementv1beta1.PropertySelectorEqualTo,
				Values:   []string{"3"},
			},
			mockStatusCode: http.StatusOK,
			wantError:      true,
			errorSubstring: "unsupported operator \"Eq\" for property \"kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity\", only GreaterThan (Gt) and GreaterThanOrEqualTo (Ge) are supported",
		},
		{
			name:    "unsupported operator in requirement",
			cluster: cluster,
			sku:     validSKU,
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, validSKU),
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"0"},
			},
			mockStatusCode: http.StatusOK,
			wantError:      true,
			errorSubstring: "capacity value cannot be zero for operator",
		},
		{
			name:    "cases-insensitive request - unavailable SKU",
			cluster: cluster,
			sku:     "STANDARD_D2S_V3",
			req: placementv1beta1.PropertySelectorRequirement{
				Name:     fmt.Sprintf(azure.CapacityPerSKUPropertyTmpl, "STANDARD_D2S_V3"),
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"2"},
			},
			mockStatusCode: http.StatusOK,
			wantAvailable:  true,
			wantError:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := createMockAttributeBasedVMSizeRecommenderServer(t, tt.mockStatusCode)
			defer server.Close()

			client, err := compute.NewAttributeBasedVMSizeRecommenderClient(server.URL, http.DefaultClient)
			if err != nil {
				t.Fatalf("failed to create VM size recommender client: %v", err)
			}
			propertyChecker := NewPropertyChecker(*client)

			result, err := propertyChecker.CheckIfMeetSKUCapacityRequirement(tt.cluster, tt.req, tt.sku)
			if tt.wantError {
				if err == nil {
					t.Fatalf("CheckIfMeetSKUCapacityRequirement error () = nil, want error")
				} else if tt.errorSubstring != "" && !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("CheckIfMeetSKUCapacityRequirement error () = %s, want %v", err, tt.errorSubstring)
				}
				return
			}

			if err != nil {
				t.Fatalf("CheckIfMeetSKUCapacityRequirement error () = %v, want nil", err)
			}

			if result != tt.wantAvailable {
				t.Errorf("CheckIfMeetSKUCapacityRequirement () = %v, want %v", result, tt.wantAvailable)
			}
		})
	}
}

// createMockAttributeBasedVMSizeRecommenderServer creates a mock HTTP server for testing AttributeBasedVMSizeRecommenderClient.
func createMockAttributeBasedVMSizeRecommenderServer(t *testing.T, httpStatusCode int) *httptest.Server {
	// Create mock server
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method
		if r.Method != http.MethodPost {
			t.Errorf("Mock PropertyChecker method () = %s, want POST request", r.Method)
		}

		// Verify headers
		if r.Header.Get(httputil.HeaderContentTypeKey) != httputil.HeaderContentTypeJSON {
			t.Errorf("Mock PropertyChecker content () = %s, want %s", r.Header.Get(httputil.HeaderContentTypeKey), httputil.HeaderContentTypeJSON)
		}
		if r.Header.Get(httputil.HeaderAcceptKey) != httputil.HeaderContentTypeJSON {
			t.Errorf("Mock PropertyChecker accept () = %s, want %s", r.Header.Get(httputil.HeaderAcceptKey), httputil.HeaderContentTypeJSON)
		}

		// Verify request body using proto json for proper proto3 one of support
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
		if httpStatusCode == 0 {
			httpStatusCode = http.StatusOK
		}
		w.Header().Set(httputil.HeaderContentTypeKey, httputil.HeaderContentTypeJSON)
		w.WriteHeader(httpStatusCode)

		// Mock the expected response from the Azure API.
		mockAzureResponse := `{
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

		if _, err := w.Write([]byte(mockAzureResponse)); err != nil {
			t.Fatalf("failed to write mock response: %v", err)
		}
	}))
}
