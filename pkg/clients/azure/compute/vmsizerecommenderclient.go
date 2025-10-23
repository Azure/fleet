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

package compute

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"google.golang.org/protobuf/encoding/protojson"

	computev1 "go.goms.io/fleet/apis/protos/azure/compute/v1"
	"go.goms.io/fleet/pkg/clients/consts"
)

const (
	// recommendationsPathTemplate is the URL path template for VM size recommendations API.
	recommendationsPathTemplate = "/subscriptions/%s/providers/Microsoft.Compute/locations/%s/vmSizeRecommendations/vmAttributeBased/generate"
)

// AttributeBasedVMSizeRecommenderClientFactory is a function type for creating AttributeBasedVMSizeRecommenderClient instances.
type AttributeBasedVMSizeRecommenderClientFactory func(endpoint string, httpClient *http.Client) AttributeBasedVMSizeRecommenderClient

// AttributeBasedVMSizeRecommenderClient is an interface for interacting with the Azure Attribute-Based VM Size Recommender API.
type AttributeBasedVMSizeRecommenderClient interface {
	// GenerateAttributeBasedRecommendations generates VM size recommendations based on attributes.
	GenerateAttributeBasedRecommendations(ctx context.Context, req *computev1.GenerateAttributeBasedRecommendationsRequest) (*computev1.GenerateAttributeBasedRecommendationsResponse, error)
}

var _ AttributeBasedVMSizeRecommenderClient = &attributeBasedVMSizeRecommenderClient{}

// attributeBasedVMSizeRecommenderClient implements the AttributeBasedVMSizeRecommenderClient interface
// for interacting with Azure Attribute-Based VM Size Recommender API.
type attributeBasedVMSizeRecommenderClient struct {
	// baseURL is the base URL of the http(s) requests to the attribute-based VM size recommender service endpoint.
	baseURL string
	// httpClient is the HTTP client used for making requests.
	httpClient *http.Client
}

// NewAttributeBasedVMSizeRecommenderClient creates a new attribute-based VM size recommender client.
// The serverAddress is the remote Azure Attribute-Based VM Size Recommender service endpoint.
// If httpClient is nil, a default client with 60s timeout will be used.
// If the serverAddress does not have a scheme (http:// or https://), like localhost:8080, http:// will be added.
func NewAttributeBasedVMSizeRecommenderClient(serverAddress string, httpClient *http.Client) AttributeBasedVMSizeRecommenderClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: consts.HTTPTimeoutAzure} // Client with default transport and 60s timeout.
	}
	// Add http:// scheme if no scheme is present
	if !strings.HasPrefix(serverAddress, "http://") && !strings.HasPrefix(serverAddress, "https://") {
		serverAddress = "http://" + serverAddress
	}
	// Ensure serverAddress doesn't have trailing slash
	serverAddress = strings.TrimSuffix(serverAddress, "/")
	return &attributeBasedVMSizeRecommenderClient{
		baseURL:    serverAddress,
		httpClient: httpClient,
	}
}

// GenerateAttributeBasedRecommendations generates VM size recommendations based on attributes.
func (c *attributeBasedVMSizeRecommenderClient) GenerateAttributeBasedRecommendations(
	ctx context.Context,
	req *computev1.GenerateAttributeBasedRecommendationsRequest,
) (*computev1.GenerateAttributeBasedRecommendationsResponse, error) {
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
	httpReq.Header.Set(consts.HeaderContentTypeKey, consts.HeaderContentTypeJSON)
	httpReq.Header.Set(consts.HeaderAcceptKey, consts.HeaderContentTypeJSON)

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
		return nil, fmt.Errorf("request failed with status %d: %w", resp.StatusCode, runtime.NewResponseError(resp))
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
