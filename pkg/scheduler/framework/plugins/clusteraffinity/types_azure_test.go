//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

package clusteraffinity

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	computev1 "go.goms.io/fleet/apis/protos/azure/compute/v1"
	"go.goms.io/fleet/pkg/clients/azure/compute"
	"go.goms.io/fleet/pkg/clients/httputil"
	checker "go.goms.io/fleet/pkg/propertychecker/azure"
	"go.goms.io/fleet/pkg/utils/labels"
)

func TestIsAzureCapacityProperty(t *testing.T) {
	tests := []struct {
		name     string
		property string
		want     string
	}{
		{
			name:     "valid Azure capacity property",
			property: "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity",
			want:     "Standard_D2s_v3",
		},
		{
			name:     "invalid Azure capacity property - wrong prefix",
			property: "kubernetes-fleet.io/vm-sizes/Standard_D2s_v3/capacity",
			want:     "",
		},
		{
			name:     "invalid Azure capacity property - missing capacity suffix",
			property: "kubernetes.azure.com/vm-sizes/Standard_D2s_v3",
			want:     "",
		},
		{
			name:     "invalid Azure capacity property - empty string",
			property: "",
			want:     "",
		},
		{
			name:     "invalid Azure capacity property - random string",
			property: "random-string",
			want:     "",
		},
		{
			name:     "invalid Azure capacity property - only prefix",
			property: "kubernetes.azure.com/vm-sizes",
			want:     "",
		},
		{
			name:     "invalid Azure capacity property - no VM size",
			property: "kubernetes.azure.com/vm-sizes//capacity",
			want:     "",
		},
		{
			name:     "invalid Azure capacity property - extra segments",
			property: "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/extra/capacity",
			want:     "",
		},
		{
			name:     "invalid Azure capacity property -",
			property: "kubernetes.azure.com/vm-sizes.Standard_D2s_v3/capacity",
			want:     "",
		},
		{
			name:     "invalid capacity property - different suffix",
			property: "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity/count",
			want:     "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAzureSKUCapacityProperty(tt.property)
			if got != tt.want {
				t.Errorf("isAzureCapacityProperty(%q) = %q, want %q", tt.property, got, tt.want)
			}
		})
	}
}

func TestMatchPropertiesInPropertyChecker(t *testing.T) {
	cluster := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
			Labels: map[string]string{
				labels.AzureLocationLabel:       "eastus",
				labels.AzureSubscriptionIDLabel: "1234-5678-9012",
			},
		},
	}

	tests := []struct {
		name          string
		cluster       *clusterv1beta1.MemberCluster
		selector      placementv1beta1.PropertySelectorRequirement
		wantHandled   bool
		wantAvailable bool
		wantErr       bool
	}{
		{
			name:    "Azure SKU capacity property not handled",
			cluster: cluster,
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/count",
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"5"},
			},
			wantHandled: false,
		},
		{
			name:    "Azure SKU capacity property handled and available",
			cluster: cluster,
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"2"},
			},
			wantHandled:   true,
			wantAvailable: true,
		},
		{
			name:    "Azure SKU capacity property handled but not available",
			cluster: cluster,
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/NonExistentSKU/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"2"},
			},
			wantHandled:   true,
			wantAvailable: false,
		},
		{
			name:    "Azure SKU capacity property with invalid operator",
			cluster: cluster,
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorEqualTo,
				Values:   []string{"2"},
			},
			wantHandled: true,
			wantErr:     true,
		},
		{
			name:    "Azure SKU capacity property with non-integer value",
			cluster: cluster,
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"two"},
			},
			wantHandled: true,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := createMockAttributeBasedVMSizeRecommenderServer(t, http.StatusOK)
			defer server.Close()

			client, err := compute.NewAttributeBasedVMSizeRecommenderClient(server.URL, http.DefaultClient)
			if err != nil {
				t.Fatalf("failed to create VM size recommender client: %v", err)
			}

			req := &clusterRequirement{
				placementv1beta1.ClusterSelectorTerm{},
				checker.NewPropertyChecker(*client),
			}
			handled, available, err := req.MatchPropertiesInPropertyChecker(tt.cluster, tt.selector)
			if (err != nil) != tt.wantErr {
				t.Errorf("MatchPropertiesInPropertyChecker() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if handled != tt.wantHandled {
				t.Errorf("MatchPropertiesInPropertyChecker() handled = %v, want %v", handled, tt.wantHandled)
			}
			if handled {
				if available != tt.wantAvailable {
					t.Errorf("MatchPropertiesInPropertyChecker() available = %v, want %v", available, tt.wantAvailable)
				}
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
