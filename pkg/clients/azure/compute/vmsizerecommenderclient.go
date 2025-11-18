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
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/gofrs/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"k8s.io/klog/v2"

	computev1 "go.goms.io/fleet/apis/protos/azure/compute/v1"
	"go.goms.io/fleet/pkg/clients/httputil"
	"go.goms.io/fleet/pkg/utils/controller"
)

const (
	tenantIDEnvVarName = "AZURE_TENANT_ID"
	// recommendationsPathTemplate is the URL path template for VM size recommendations API.
	recommendationsPathTemplate = "/subscriptions/%s/providers/Microsoft.Compute/locations/%s/vmSizeRecommendations/vmAttributeBased/generate"
)

// AttributeBasedVMSizeRecommenderClient accesses Azure Attribute-Based VM Size Recommender API
// to provide VM size recommendations based on specified attributes.
type AttributeBasedVMSizeRecommenderClient struct {
	// tenantID is the ID of the Azure fleet's tenant.
	// At the moment, Azure fleet is single-tenant, the fleet and all its members must be in the same tenant.
	tenantID string
	// baseURL is the base URL of the http(s) requests to the attribute-based VM size recommender service endpoint.
	baseURL string
	// httpClient is the HTTP client used for making requests.
	httpClient *http.Client
}

// NewAttributeBasedVMSizeRecommenderClient creates a new AttributeBasedVMSizeRecommenderClient.
// The serverAddress is the remote Azure Attribute-Based VM Size Recommender service endpoint.
// Both serverAddress and httpClient must be provided.
func NewAttributeBasedVMSizeRecommenderClient(
	serverAddress string,
	httpClient *http.Client,
) (*AttributeBasedVMSizeRecommenderClient, error) {
	tenantID := os.Getenv(tenantIDEnvVarName)
	if tenantID == "" {
		return nil, fmt.Errorf("failed to get tenantID: environment variable %s is not set", tenantIDEnvVarName)
	}
	if len(serverAddress) == 0 {
		return nil, fmt.Errorf("serverAddress cannot be empty")
	}
	if httpClient == nil {
		return nil, fmt.Errorf("httpClient cannot be nil")
	}
	return &AttributeBasedVMSizeRecommenderClient{
		tenantID:   tenantID,
		baseURL:    serverAddress,
		httpClient: httpClient,
	}, nil
}

// GenerateAttributeBasedRecommendations generates VM size recommendations based on attributes.
func (c *AttributeBasedVMSizeRecommenderClient) GenerateAttributeBasedRecommendations(
	ctx context.Context,
	req *computev1.GenerateAttributeBasedRecommendationsRequest,
) (response *computev1.GenerateAttributeBasedRecommendationsResponse, err error) {
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
	clientRequestID := uuid.Must(uuid.NewV4()).String()
	httpReq.Header.Set(httputil.HeaderContentTypeKey, httputil.HeaderContentTypeJSON)
	httpReq.Header.Set(httputil.HeaderAcceptKey, httputil.HeaderContentTypeJSON)
	httpReq.Header.Set(httputil.HeaderAzureSubscriptionTenantIDKey, c.tenantID)
	httpReq.Header.Set(httputil.HeaderAzureClientRequestIDKey, clientRequestID)

	// Execute the request
	startTime := time.Now()
	klog.V(2).InfoS("Generating VM size recommendations", "subscriptionID", req.SubscriptionId, "location", req.Location, "clientRequestID", clientRequestID)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		if err != nil {
			klog.ErrorS(err, "Failed to generate VM size recommendations", "subscriptionID", req.SubscriptionId, "location", req.Location, "clientRequestID", clientRequestID, "latency", latency)
		}
		klog.V(2).InfoS("Generated VM size recommendations", "subscriptionID", req.SubscriptionId, "location", req.Location, "clientRequestID", clientRequestID, "latency", latency)
	}()

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
	response = &computev1.GenerateAttributeBasedRecommendationsResponse{}
	unmarshaler := protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}
	if err := unmarshaler.Unmarshal(respBody, response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return response, nil
}
