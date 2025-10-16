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
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"

	computev1 "go.goms.io/fleet/pkg/protos/azure/compute/v1"
)

const (
	// recommendationsPathTemplate is the URL path template for VM size recommendations API.
	recommendationsPathTemplate = "/subscriptions/%s/providers/Microsoft.Compute/locations/%s/vmSizeRecommendations/vmAttributeBased/generate"
)

// CapacityClientFactory is a function type for creating CapacityClient instances.
type CapacityClientFactory func(endpoint string, httpClient *http.Client) CapacityClient

// CapacityClient is an interface for interacting with the Azure Capacity API.
type CapacityClient interface {
	// GenerateAttributeBasedRecommendations generates VM size recommendations based on attributes.
	GenerateAttributeBasedRecommendations(ctx context.Context, req *computev1.GenerateAttributeBasedRecommendationsRequest) (*computev1.GenerateAttributeBasedRecommendationsResponse, error)
}

var _ CapacityClient = &client{}

// client implements the CapacityClient interface for interacting with Azure Capacity API.
type client struct {
	// baseURL is the base URL of the capacity service endpoint.
	baseURL string
	// httpClient is the HTTP client used for making requests.
	httpClient *http.Client
}

// NewClient creates a new capacity client with the given endpoint and HTTP client.
// If httpClient is nil, http.DefaultClient will be used.
// If the endpoint does not have a scheme (http:// or https://), like localhost:8080, http:// will be added.
func NewClient(endpoint string, httpClient *http.Client) CapacityClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	// Add http:// scheme if no scheme is present
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "http://" + endpoint
	}
	// Ensure endpoint doesn't have trailing slash
	endpoint = strings.TrimSuffix(endpoint, "/")
	return &client{
		baseURL:    endpoint,
		httpClient: httpClient,
	}
}

// GenerateAttributeBasedRecommendations generates VM size recommendations based on attributes.
func (c *client) GenerateAttributeBasedRecommendations(ctx context.Context, req *computev1.GenerateAttributeBasedRecommendationsRequest) (*computev1.GenerateAttributeBasedRecommendationsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}
	if req.SubscriptionId == "" {
		return nil, fmt.Errorf("subscription ID is required")
	}
	if req.Location == "" {
		return nil, fmt.Errorf("location is required")
	}
	if req.GetRegularPriorityProfile() == nil && req.GetSpotPriorityProfile() == nil {
		return nil, fmt.Errorf("either regular priority profile or spot priority profile must be provided")
	}

	// Build the URL
	path := fmt.Sprintf(recommendationsPathTemplate, req.SubscriptionId, req.Location)
	url := c.baseURL + path

	// Marshal request body using protojson for proper proto3 oneof support
	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}
	body, err := marshaler.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	// Execute the request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Unmarshal response using protojson for proper proto3 support
	var response computev1.GenerateAttributeBasedRecommendationsResponse
	unmarshaler := protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}
	if err := unmarshaler.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &response, nil
}
