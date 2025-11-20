//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

package clusteraffinity

import (
	"net/http"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/clients/azure/compute"
	"go.goms.io/fleet/pkg/propertychecker/azure"
	"go.goms.io/fleet/pkg/utils/labels"
	testcompute "go.goms.io/fleet/test/utils/azure/compute"
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
		name           string
		cluster        *clusterv1beta1.MemberCluster
		selector       placementv1beta1.PropertySelectorRequirement
		vmSize         string
		targetCapacity uint32
		wantHandled    bool
		wantAvailable  bool
		wantErr        bool
	}{
		{
			name:    "Azure SKU capacity property not handled",
			cluster: cluster,
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/count",
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"2"},
			},
		},
		{
			name:    "Azure SKU capacity property handled and available",
			cluster: cluster,
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"2"},
			},
			vmSize:         "Standard_D2s_v3",
			targetCapacity: 2,
			wantHandled:    true,
			wantAvailable:  true,
		},
		{
			name:    "Azure SKU capacity property handled but not available",
			cluster: cluster,
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/NonExistentSKU/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
				Values:   []string{"2"},
			},
			vmSize:         "NonExistentSKU",
			targetCapacity: 1,
			wantHandled:    true,
			wantAvailable:  false,
		},
		{
			name:    "Azure SKU capacity property with invalid operator",
			cluster: cluster,
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorEqualTo,
				Values:   []string{"2"},
			},
			wantErr: true,
		},
		{
			name:    "Azure SKU capacity property with non-integer value",
			cluster: cluster,
			selector: placementv1beta1.PropertySelectorRequirement{
				Name:     "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/capacity",
				Operator: placementv1beta1.PropertySelectorGreaterThan,
				Values:   []string{"two"},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set tenant ID environment variable to create client.
			t.Setenv("AZURE_TENANT_ID", testcompute.TestTenantID)
			// Create mock server.
			mockRequest := testcompute.GenerateAttributeBasedVMSizeRecommenderRequest(tt.cluster.Labels[labels.AzureSubscriptionIDLabel], tt.cluster.Labels[labels.AzureLocationLabel], tt.vmSize, tt.targetCapacity)
			server := testcompute.CreateMockAttributeBasedVMSizeRecommenderServer(t, mockRequest, testcompute.TestTenantID, testcompute.MockAttributeBasedVMSizeRecommenderResponse, http.StatusOK)
			defer server.Close()

			client, err := compute.NewAttributeBasedVMSizeRecommenderClient(server.URL, http.DefaultClient)
			if err != nil {
				t.Fatalf("failed to create VM size recommender client: %v", err)
			}

			req := &clusterRequirement{
				placementv1beta1.ClusterSelectorTerm{},
				azure.NewPropertyChecker(*client),
			}
			handled, available, err := req.MatchPropertiesInPropertyChecker(tt.cluster, tt.selector)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MatchPropertiesInPropertyChecker() error = %v, wantErr %v", err, tt.wantErr)
			}
			if handled != tt.wantHandled {
				t.Errorf("MatchPropertiesInPropertyChecker() handled = %v, want %v", handled, tt.wantHandled)
			}

			if available != tt.wantAvailable {
				t.Errorf("MatchPropertiesInPropertyChecker() available = %v, want %v", available, tt.wantAvailable)
			}
		})
	}
}
