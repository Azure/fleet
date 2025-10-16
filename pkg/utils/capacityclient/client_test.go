/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package capacityclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	computev1 "go.goms.io/fleet/pkg/protos/azure/compute/v1"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name           string
		endpoint       string
		httpClient     *http.Client
		wantBaseURL    string
		wantHTTPClient *http.Client
	}{
		{
			name:           "with custom HTTP client",
			endpoint:       "https://example.com",
			httpClient:     &http.Client{},
			wantBaseURL:    "https://example.com",
			wantHTTPClient: &http.Client{},
		},
		{
			name:           "with nil HTTP client uses default",
			endpoint:       "https://example.com",
			httpClient:     nil,
			wantBaseURL:    "https://example.com",
			wantHTTPClient: http.DefaultClient,
		},
		{
			name:           "removes trailing slash from endpoint",
			endpoint:       "https://example.com/",
			httpClient:     nil,
			wantBaseURL:    "https://example.com",
			wantHTTPClient: http.DefaultClient,
		},
		{
			name:           "adds http scheme to endpoint without scheme",
			endpoint:       "localhost:8080",
			httpClient:     nil,
			wantBaseURL:    "http://localhost:8080",
			wantHTTPClient: http.DefaultClient,
		},
		{
			name:           "adds http scheme and removes trailing slash",
			endpoint:       "example.com:8080/",
			httpClient:     nil,
			wantBaseURL:    "http://example.com:8080",
			wantHTTPClient: http.DefaultClient,
		},
		{
			name:           "preserves existing http scheme",
			endpoint:       "http://localhost:8080",
			httpClient:     nil,
			wantBaseURL:    "http://localhost:8080",
			wantHTTPClient: http.DefaultClient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewClient(tt.endpoint, tt.httpClient).(*client)
			if got.baseURL != tt.wantBaseURL {
				t.Errorf("NewClient() baseURL = %v, want %v", got.baseURL, tt.wantBaseURL)
			}
			// For custom HTTP client, just verify it's not nil
			if tt.httpClient != nil && got.httpClient == nil {
				t.Errorf("NewClient() httpClient is nil, want non-nil")
			}
			// For nil HTTP client, verify it uses default client
			if tt.httpClient == nil && got.httpClient != http.DefaultClient {
				t.Errorf("NewClient() httpClient = %v, want http.DefaultClient", got.httpClient)
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
				PriorityProfile: &computev1.GenerateAttributeBasedRecommendationsRequest_RegularPriorityProfile{
					RegularPriorityProfile: &computev1.RegularPriorityProfile{},
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
				PriorityProfile: &computev1.GenerateAttributeBasedRecommendationsRequest_SpotPriorityProfile{
					SpotPriorityProfile: &computev1.SpotPriorityProfile{},
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
				PriorityProfile: &computev1.GenerateAttributeBasedRecommendationsRequest_RegularPriorityProfile{
					RegularPriorityProfile: &computev1.RegularPriorityProfile{},
				},
			},
			wantErr:    true,
			wantErrMsg: "subscription ID is required",
		},
		{
			name: "missing location",
			request: &computev1.GenerateAttributeBasedRecommendationsRequest{
				SubscriptionId: "sub-123",
				PriorityProfile: &computev1.GenerateAttributeBasedRecommendationsRequest_RegularPriorityProfile{
					RegularPriorityProfile: &computev1.RegularPriorityProfile{},
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
				PriorityProfile: &computev1.GenerateAttributeBasedRecommendationsRequest_RegularPriorityProfile{
					RegularPriorityProfile: &computev1.RegularPriorityProfile{},
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
				PriorityProfile: &computev1.GenerateAttributeBasedRecommendationsRequest_RegularPriorityProfile{
					RegularPriorityProfile: &computev1.RegularPriorityProfile{},
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
				PriorityProfile: &computev1.GenerateAttributeBasedRecommendationsRequest_RegularPriorityProfile{
					RegularPriorityProfile: &computev1.RegularPriorityProfile{},
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
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method
				if r.Method != http.MethodPost {
					t.Errorf("expected POST request, got %s", r.Method)
				}

				// Verify headers
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("expected Content-Type: application/json, got %s", r.Header.Get("Content-Type"))
				}
				if r.Header.Get("Accept") != "application/json" {
					t.Errorf("expected Accept: application/json, got %s", r.Header.Get("Accept"))
				}

				// Verify URL path if request is not nil
				if tt.request != nil && tt.request.SubscriptionId != "" && tt.request.Location != "" {
					expectedPath := fmt.Sprintf(recommendationsPathTemplate, tt.request.SubscriptionId, tt.request.Location)
					if r.URL.Path != expectedPath {
						t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
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
					if !proto.Equal(tt.request, &req) {
						t.Errorf("request body mismatch: got %+v, want %+v", &req, tt.request)
					}
				}

				// Write mock response
				w.WriteHeader(tt.mockStatusCode)
				if _, err := w.Write([]byte(tt.mockResponse)); err != nil {
					t.Fatalf("failed to write response: %v", err)
				}
			}))
			defer server.Close()

			// Create client
			client := NewClient(server.URL, nil)

			// Execute request
			got, err := client.GenerateAttributeBasedRecommendations(context.Background(), tt.request)

			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateAttributeBasedRecommendations() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("GenerateAttributeBasedRecommendations() error = %v, want error containing %q", err, tt.wantErrMsg)
				return
			}

			// Compare response
			if !proto.Equal(tt.wantResponse, got) {
				t.Errorf("GenerateAttributeBasedRecommendations() = %+v, want %+v", got, tt.wantResponse)
			}
		})
	}
}

func TestClient_GenerateAttributeBasedRecommendations_ContextCancellation(t *testing.T) {
	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This handler will never respond in time
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(server.URL, nil)

	// Create a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := &computev1.GenerateAttributeBasedRecommendationsRequest{
		SubscriptionId: "sub-123",
		Location:       "eastus",
		PriorityProfile: &computev1.GenerateAttributeBasedRecommendationsRequest_RegularPriorityProfile{
			RegularPriorityProfile: &computev1.RegularPriorityProfile{},
		},
	}

	_, err := client.GenerateAttributeBasedRecommendations(ctx, req)
	if err == nil {
		t.Error("GenerateAttributeBasedRecommendations() expected error for cancelled context, got nil")
	}
}
