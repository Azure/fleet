/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package compute

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	computev1 "go.goms.io/fleet/apis/protos/azure/compute/v1"
)

const (
	testTenantID = "test-tenant-id"
)

func TestNewAttributeBasedVMSizeRecommenderClient(t *testing.T) {
	tests := []struct {
		name          string
		tenantID      string
		serverAddress string
		httpClient    *http.Client
		wantClient    *AttributeBasedVMSizeRecommenderClient
		wantErr       bool
	}{
		{
			name:          "with missing tenant ID environment variable",
			serverAddress: "https://example.com",
			httpClient:    http.DefaultClient,
			wantClient:    nil,
			wantErr:       true,
		},
		{
			name:          "with empty server address",
			tenantID:      testTenantID,
			serverAddress: "",
			httpClient:    http.DefaultClient,
			wantClient:    nil,
			wantErr:       true,
		},
		{
			name:          "with nil HTTP client",
			tenantID:      testTenantID,
			serverAddress: "http://localhost:8080",
			httpClient:    nil,
			wantClient:    nil,
			wantErr:       true,
		},
		{
			name:          "with all fields properly set",
			tenantID:      testTenantID,
			serverAddress: "https://example.com",
			httpClient:    http.DefaultClient,
			wantClient: &AttributeBasedVMSizeRecommenderClient{
				tenantID:   testTenantID,
				baseURL:    "https://example.com",
				httpClient: http.DefaultClient,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tenantIDEnvVarName, tt.tenantID)
			got, gotErr := NewAttributeBasedVMSizeRecommenderClient(tt.serverAddress, tt.httpClient)
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("NewAttributeBasedVMSizeRecommenderClient() error = %v, wantErr %v", gotErr, tt.wantErr)
			}
			if diff := cmp.Diff(tt.wantClient, got,
				cmp.AllowUnexported(AttributeBasedVMSizeRecommenderClient{})); diff != "" {
				t.Errorf("NewAttributeBasedVMSizeRecommenderClient() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestClient_GenerateAttributeBasedRecommendations(t *testing.T) {
	tests := []struct {
		name           string
		request        *computev1.GenerateAttributeBasedRecommendationsRequest
		mockStatusCode int
		mockResponse   string
		wantResponse   *computev1.GenerateAttributeBasedRecommendationsResponse
		wantErr        bool
		wantErrMsg     string
	}{
		{
			name: "successful request with regular priority profile",
			request: &computev1.GenerateAttributeBasedRecommendationsRequest{
				SubscriptionId: "sub-123",
				Location:       "eastus",
				RegularPriorityProfile: &computev1.RegularPriorityProfile{
					TargetCapacity: 5,
				},
				ResourceProperties: &computev1.ResourceProperties{},
			},
			mockStatusCode: http.StatusOK,
			mockResponse:   `{"recommended_vm_sizes":{"regular_vm_sizes": [{"name":"Standard_D2s_v3"}]}}`,
			wantResponse: &computev1.GenerateAttributeBasedRecommendationsResponse{
				RecommendedVmSizes: &computev1.RecommendedVMSizes{
					RegularVmSizes: []*computev1.RecommendedVMSizeProperties{
						{Name: "Standard_D2s_v3"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "successful request with spot priority profile",
			request: &computev1.GenerateAttributeBasedRecommendationsRequest{
				SubscriptionId: "sub-123",
				Location:       "westus2",
				RegularPriorityProfile: &computev1.RegularPriorityProfile{
					TargetCapacity: 5,
				},
				ResourceProperties: &computev1.ResourceProperties{},
			},
			mockStatusCode: http.StatusOK,
			mockResponse:   `{"recommended_vm_sizes":{"spot_vm_sizes": [{"name":"Standard_D4s_v3"}]}}`,
			wantResponse: &computev1.GenerateAttributeBasedRecommendationsResponse{
				RecommendedVmSizes: &computev1.RecommendedVMSizes{
					SpotVmSizes: []*computev1.RecommendedVMSizeProperties{
						{Name: "Standard_D4s_v3"},
					},
				},
			},
			wantErr: false,
		},
		{
			name:       "nil request",
			request:    nil,
			wantErr:    true,
			wantErrMsg: "request cannot be nil",
		},
		{
			name: "missing subscription ID",
			request: &computev1.GenerateAttributeBasedRecommendationsRequest{
				Location: "eastus",
				RegularPriorityProfile: &computev1.RegularPriorityProfile{
					TargetCapacity: 5,
				},
			},
			wantErr:    true,
			wantErrMsg: "subscription ID is required",
		},
		{
			name: "missing location",
			request: &computev1.GenerateAttributeBasedRecommendationsRequest{
				SubscriptionId: "sub-123",
				RegularPriorityProfile: &computev1.RegularPriorityProfile{
					TargetCapacity: 5,
				},
			},
			wantErr:    true,
			wantErrMsg: "location is required",
		},
		{
			name: "missing both priority profiles",
			request: &computev1.GenerateAttributeBasedRecommendationsRequest{
				SubscriptionId: "sub-123",
				Location:       "eastus",
			},
			wantErr:    true,
			wantErrMsg: "either regular priority profile or spot priority profile must be provided",
		},
		{
			name: "HTTP 400 error",
			request: &computev1.GenerateAttributeBasedRecommendationsRequest{
				SubscriptionId: "sub-123",
				Location:       "eastus",
				RegularPriorityProfile: &computev1.RegularPriorityProfile{
					TargetCapacity: 5,
				},
			},
			mockStatusCode: http.StatusBadRequest,
			mockResponse:   `{"error":"invalid request"}`,
			wantErr:        true,
			wantErrMsg:     "request failed with status 400",
		},
		{
			name: "HTTP 500 error",
			request: &computev1.GenerateAttributeBasedRecommendationsRequest{
				SubscriptionId: "sub-123",
				Location:       "eastus",
				RegularPriorityProfile: &computev1.RegularPriorityProfile{
					TargetCapacity: 5,
				},
			},
			mockStatusCode: http.StatusInternalServerError,
			mockResponse:   `{"error":"internal server error"}`,
			wantErr:        true,
			wantErrMsg:     "request failed with status 500",
		},
		{
			name: "invalid JSON response",
			request: &computev1.GenerateAttributeBasedRecommendationsRequest{
				SubscriptionId: "sub-123",
				Location:       "eastus",
				RegularPriorityProfile: &computev1.RegularPriorityProfile{
					TargetCapacity: 5,
				},
			},
			mockStatusCode: http.StatusOK,
			mockResponse:   `invalid json`,
			wantErr:        true,
			wantErrMsg:     "failed to unmarshal response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set tenant ID environment variable to create client.
			t.Setenv(tenantIDEnvVarName, testTenantID)
			// Create mock server.
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method.
				if r.Method != http.MethodPost {
					t.Errorf("got %s, want POST request", r.Method)
				}

				// Verify headers.
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("got %s, want Content-Type: application/json", r.Header.Get("Content-Type"))
				}
				if r.Header.Get("Accept") != "application/json" {
					t.Errorf("got %s, want Accept: application/json", r.Header.Get("Accept"))
				}
				if r.Header.Get("Grpc-Metadata-subscriptionTenantID") != testTenantID {
					t.Errorf("got %s, want Grpc-Metadata-subscriptionTenantID: %s",
						r.Header.Get("Grpc-Metadata-subscriptionTenantID"), testTenantID)
				}
				if r.Header.Get("Grpc-Metadata-clientRequestID") == "" {
					t.Error("Grpc-Metadata-clientRequestID header is missing")
				}

				// Verify URL path if request is not nil.
				if tt.request != nil && tt.request.SubscriptionId != "" && tt.request.Location != "" {
					wantPath := fmt.Sprintf(recommendationsPathTemplate, tt.request.SubscriptionId, tt.request.Location)
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
					if !proto.Equal(tt.request, &req) {
						t.Errorf("request body mismatch: got %+v, want %+v", &req, tt.request)
					}
				}

				// Write mock response.
				w.WriteHeader(tt.mockStatusCode)
				if _, err := w.Write([]byte(tt.mockResponse)); err != nil {
					t.Fatalf("failed to write response: %v", err)
				}
			}))
			defer server.Close()

			// Create client.
			client, err := NewAttributeBasedVMSizeRecommenderClient(server.URL, http.DefaultClient)
			if err != nil {
				t.Errorf("failed to create client: %v", err)
			}

			// Execute request.
			got, err := client.GenerateAttributeBasedRecommendations(context.Background(), tt.request)

			// Check error.
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateAttributeBasedRecommendations() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("GenerateAttributeBasedRecommendations() error = %v, want error containing %q", err, tt.wantErrMsg)
				return
			}

			// Compare response.
			if !proto.Equal(tt.wantResponse, got) {
				t.Errorf("GenerateAttributeBasedRecommendations() = %+v, want %+v", got, tt.wantResponse)
			}
		})
	}
}
