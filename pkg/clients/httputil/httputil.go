/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package httputil provides common utilities for HTTP clients.
package httputil

import (
	"net/http"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

// Common HTTP constants.
const (
	// HeaderContentTypeKey is the HTTP header key for Content-Type.
	HeaderContentTypeKey = "Content-Type"
	// HeaderAcceptKey is the HTTP header key for Accept.
	HeaderAcceptKey = "Accept"
	// HeaderContentTypeJSON is the Content-Type header value for JSON payloads.
	HeaderContentTypeJSON = "application/json"
)

const (
	// HTTPTimeoutAzure is the timeout for HTTP requests to Azure services.
	// Setting to 60 seconds, following ARM client request timeout conventions:
	// https://github.com/Azure/azure-resource-manager-rpc/blob/master/v1.0/common-api-details.md#client-request-timeout.
	HTTPTimeoutAzure = 60 * time.Second

	// HeaderAzureSubscriptionTenantIDKey is the HTTP header key for the tenantID of the requested Azure Subscription.
	// grpc-gateway maps headers with Grpc-Metadata- prefix to grpc metadata after removing it.
	// See: https://github.com/grpc-ecosystem/grpc-gateway.
	HeaderAzureSubscriptionTenantIDKey = runtime.MetadataHeaderPrefix + "subscriptionTenantID"
	// HeaderAzureClientRequestIDKey is the HTTP header key for Azure Client Request ID.
	HeaderAzureClientRequestIDKey = runtime.MetadataHeaderPrefix + "clientRequestID"
)

var (
	// DefaultClientForAzure is the default HTTP client to access Azure services.
	DefaultClientForAzure = &http.Client{Timeout: HTTPTimeoutAzure}
)

// transientHTTPStatusCodes defines HTTP status codes that indicate transient errors
// which may succeed on retry.
var transientHTTPStatusCodes = map[int]bool{
	http.StatusTooManyRequests:     true, // 429 - Rate limiting
	http.StatusInternalServerError: true, // 500 - Server error
	http.StatusBadGateway:          true, // 502 - Bad gateway
	http.StatusServiceUnavailable:  true, // 503 - Service unavailable
	http.StatusGatewayTimeout:      true, // 504 - Gateway timeout
}

// IsTransientStatusCode returns true if the HTTP status code indicates a transient
// error that may succeed on retry (429, 5xx).
func IsTransientStatusCode(statusCode int) bool {
	return transientHTTPStatusCodes[statusCode]
}
