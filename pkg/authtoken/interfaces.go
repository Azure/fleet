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
package authtoken

import (
	"context"
	"time"
)

// An AuthToken is an authentication token used to communicate with the hub API server.
type AuthToken struct {
	Token     string    // The authentication token string.
	ExpiresOn time.Time // The expiration time of the token.
}

// Provider defines a method for fetching an authentication token.
type Provider interface {
	// FetchToken fetches an authentication token to make requests to its associated fleet's hub cluster.
	// It returns the token for a given input context, or an error if the retrieval fails.
	FetchToken(ctx context.Context) (AuthToken, error)
}

// Writer defines a method for writing an authentication token to a specified location.
type Writer interface {
	// WriteToken writes the provided authentication token to a filepath location specified in a TokenWriter.
	// It returns an error if the writing process fails.
	WriteToken(token AuthToken) error
}
