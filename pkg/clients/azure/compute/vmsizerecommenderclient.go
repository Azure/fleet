/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package compute provides clients for interacting with Azure Compute services.
package compute

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"google.golang.org/protobuf/encoding/protojson"

	computev1 "go.goms.io/fleet/apis/protos/azure/compute/v1"
	"go.goms.io/fleet/pkg/clients/httputil"
	"go.goms.io/fleet/pkg/utils/controller"
)

const (
	// recommendationsPathTemplate is the URL path template for VM size recommendations API.
	recommendationsPathTemplate = "/subscriptions/%s/providers/Microsoft.Compute/locations/%s/vmSizeRecommendations/vmAttributeBased/generate"
)

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
// Both serverAddress and httpClient must be provided.
func NewAttributeBasedVMSizeRecommenderClient(serverAddress string, httpClient *http.Client) (AttributeBasedVMSizeRecommenderClient, error) {
	if len(serverAddress) == 0 {
		return nil, fmt.Errorf("serverAddress cannot be empty")
	}
	if httpClient == nil {
		return nil, fmt.Errorf("httpClient cannot be nil")
	}
	return &attributeBasedVMSizeRecommenderClient{
		baseURL:    serverAddress,
		httpClient: httpClient,
	}, nil
}

// GenerateAttributeBasedRecommendations generates VM size recommendations based on attributes.
func (c *attributeBasedVMSizeRecommenderClient) GenerateAttributeBasedRecommendations(
	ctx context.Context,
	req *computev1.GenerateAttributeBasedRecommendationsRequest,
) (*computev1.GenerateAttributeBasedRecommendationsResponse, error) {
	if req == nil {
		return nil, controller.NewUnexpectedBehaviorError(errors.New("request cannot be nil"))
	}
	if req.SubscriptionId == "" {
		return nil, controller.NewUnexpectedBehaviorError(errors.New("subscription ID is required"))
	}
	if req.Location == "" {
		return nil, controller.NewUnexpectedBehaviorError(errors.New("location is required"))
	}
	if req.GetRegularPriorityProfile() == nil && req.GetSpotPriorityProfile() == nil {
		return nil, controller.NewUnexpectedBehaviorError(errors.New("either regular priority profile or spot priority profile must be provided"))
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
	httpReq.Header.Set(httputil.HeaderContentTypeKey, httputil.HeaderContentTypeJSON)
	httpReq.Header.Set(httputil.HeaderAcceptKey, httputil.HeaderContentTypeJSON)

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
