/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package httputil

import (
	"net/http"
	"net/url"
	"testing"
)

func TestHTTPError(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Request: &http.Request{
			Method: http.MethodPost,
			URL: &url.URL{
				Scheme: "https",
				Host:   "example.com",
				Path:   "/api/v1/resource",
			},
		},
	}

	httpErr := NewHTTPError(resp)

	// Test fields are populated correctly.
	if httpErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("HTTPError.StatusCode = %d, want %d", httpErr.StatusCode, http.StatusServiceUnavailable)
	}
	if httpErr.Method != http.MethodPost {
		t.Errorf("HTTPError.Method = %q, want %q", httpErr.Method, http.MethodPost)
	}
	if httpErr.URL != "https://example.com/api/v1/resource" {
		t.Errorf("HTTPError.URL = %q, want %q", httpErr.URL, "https://example.com/api/v1/resource")
	}

	// Test Error() method.
	wantErrMsg := "request failed with status 503: POST https://example.com/api/v1/resource"
	if got := httpErr.Error(); got != wantErrMsg {
		t.Errorf("HTTPError.Error() = %q, want %q", got, wantErrMsg)
	}
}

func TestHTTPErrorIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  *HTTPError
		want bool
	}{
		{"400 Bad Request", &HTTPError{StatusCode: http.StatusBadRequest}, false},
		{"401 Unauthorized", &HTTPError{StatusCode: http.StatusUnauthorized}, false},
		{"403 Forbidden", &HTTPError{StatusCode: http.StatusForbidden}, false},
		{"404 Not Found", &HTTPError{StatusCode: http.StatusNotFound}, false},
		{"429 Too Many Requests", &HTTPError{StatusCode: http.StatusTooManyRequests}, true},
		{"500 Internal Server Error", &HTTPError{StatusCode: http.StatusInternalServerError}, true},
		{"502 Bad Gateway", &HTTPError{StatusCode: http.StatusBadGateway}, true},
		{"503 Service Unavailable", &HTTPError{StatusCode: http.StatusServiceUnavailable}, true},
		{"504 Gateway Timeout", &HTTPError{StatusCode: http.StatusGatewayTimeout}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.IsRetryable(); got != tt.want {
				t.Errorf("HTTPError.IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}
