/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package httputil provides common utilities for HTTP clients.
package httputil

import (
	"net/http"
	"time"
)

// Common HTTP constants.
const (
	// HTTPTimeoutAzure is the timeout for HTTP requests to Azure services.
	// Setting to 60 seconds, following ARM client request timeout conventions:
	// https://github.com/Azure/azure-resource-manager-rpc/blob/master/v1.0/common-api-details.md#client-request-timeout.
	HTTPTimeoutAzure = 60 * time.Second

	// HeaderContentTypeKey is the HTTP header key for Content-Type.
	HeaderContentTypeKey = "Content-Type"
	// HeaderAcceptKey is the HTTP header key for Accept.
	HeaderAcceptKey = "Accept"
	// HeaderContentTypeJSON is the Content-Type header value for JSON payloads.
	HeaderContentTypeJSON = "application/json"
)

var (
	// DefaultClientForAzure is the default HTTP client to access Azure services.
	DefaultClientForAzure = &http.Client{Timeout: HTTPTimeoutAzure}
)
