/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package httputil

import (
	"net/http"
	"testing"
)

func TestIsTransientStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{"400 Bad Request", http.StatusBadRequest, false},
		{"401 Unauthorized", http.StatusUnauthorized, false},
		{"403 Forbidden", http.StatusForbidden, false},
		{"404 Not Found", http.StatusNotFound, false},
		{"429 Too Many Requests", http.StatusTooManyRequests, true},
		{"500 Internal Server Error", http.StatusInternalServerError, true},
		{"502 Bad Gateway", http.StatusBadGateway, true},
		{"503 Service Unavailable", http.StatusServiceUnavailable, true},
		{"504 Gateway Timeout", http.StatusGatewayTimeout, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTransientStatusCode(tt.statusCode); got != tt.want {
				t.Errorf("IsTransientStatusCode(%d) = %v, want %v", tt.statusCode, got, tt.want)
			}
		})
	}
}
