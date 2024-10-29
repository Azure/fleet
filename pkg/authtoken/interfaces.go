/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
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
