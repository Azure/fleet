//
//Copyright (c) Microsoft Corporation.
//Licensed under the MIT license.

// compute package provides utilities for testing Azure Compute services.
package compute

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	computev1 "go.goms.io/fleet/apis/protos/azure/compute/v1"
	"go.goms.io/fleet/pkg/clients/httputil"
)

const (
	TestTenantID = "test-tenant-id"

	MockAttributeBasedVMSizeRecommenderResponse = `{
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
)

// GenerateAttributeBasedVMSizeRecommenderRequest is a helper function to create a mock request.
func GenerateAttributeBasedVMSizeRecommenderRequest(subscriptionID, location, sku string, targetCapacity uint32) *computev1.GenerateAttributeBasedRecommendationsRequest {
	return &computev1.GenerateAttributeBasedRecommendationsRequest{
		SubscriptionId: subscriptionID,
		Location:       location,
		RegularPriorityProfile: &computev1.RegularPriorityProfile{
			CapacityUnitType: computev1.CapacityUnitType_CAPACITY_UNIT_TYPE_VM_INSTANCE_COUNT,
			TargetCapacity:   targetCapacity,
		},
		RecommendationProperties: &computev1.RecommendationProperties{
			RestrictionsFilter: computev1.RecommendationProperties_RESTRICTIONS_FILTER_QUOTA_AND_OFFER_RESTRICTIONS,
		},
		ResourceProperties: &computev1.ResourceProperties{
			VmAttributes: &computev1.VMAttributes{
				AllowedVmSizes: []string{sku},
			},
		},
	}
}

// CreateMockAttributeBasedVMSizeRecommenderServer is a helper function to create a mock server with a generated response.
func CreateMockAttributeBasedVMSizeRecommenderServer(t *testing.T, request *computev1.GenerateAttributeBasedRecommendationsRequest, testTenantID, response string, httpStatusCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method.
		if r.Method != http.MethodPost {
			t.Errorf("got %s, want POST request", r.Method)
		}

		// Verify headers.
		if r.Header.Get(httputil.HeaderContentTypeKey) != httputil.HeaderContentTypeJSON {
			t.Errorf("got %s, want Content-Type: %s", r.Header.Get(httputil.HeaderContentTypeKey), httputil.HeaderContentTypeJSON)
		}
		if r.Header.Get(httputil.HeaderAcceptKey) != httputil.HeaderContentTypeJSON {
			t.Errorf("got %s, want Accept: %s", r.Header.Get(httputil.HeaderAcceptKey), httputil.HeaderContentTypeJSON)
		}
		if r.Header.Get(httputil.HeaderAzureSubscriptionTenantIDKey) != testTenantID {
			t.Errorf("got %s, want Grpc-Metadata-subscriptionTenantID: %s",
				r.Header.Get(httputil.HeaderAzureSubscriptionTenantIDKey), testTenantID)
		}
		if r.Header.Get(httputil.HeaderAzureClientRequestIDKey) == "" {
			t.Error("Grpc-Metadata-clientRequestID header is missing")
		}

		// Verify URL path if request is not nil.
		if request != nil && request.SubscriptionId != "" && request.Location != "" {
			wantPath := fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Compute/locations/%s/vmSizeRecommendations/vmAttributeBased/generate", request.SubscriptionId, request.Location)
			if r.URL.Path != wantPath {
				t.Errorf("got %s, want path %s", r.URL.Path, wantPath)
			}

			// Verify request body using protojson for proper proto3 oneof support.
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
			if !proto.Equal(request, &req) {
				t.Errorf("request body mismatch: got %+v, want %+v", &req, request)
			}
		}

		// Write mock response.
		w.WriteHeader(httpStatusCode)
		if _, err := w.Write([]byte(response)); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
}
