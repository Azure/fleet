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
	Token     string    // name of token
	ExpiresOn time.Time // expiration time of token
}

// Provider defines a method for fetching an AuthToken.
type Provider interface {
	// FetchToken fetches an AuthToken.
	// It returns an error if it is unable to fetch an AuthToken for the given input context.
	FetchToken(ctx context.Context) (AuthToken, error)
}

// Writer defines a method for writing an AuthToken.
type Writer interface {
	// WriteToken writes an AuthToken.
	// It returns an error if it is unable to write the AuthToken.
	WriteToken(token AuthToken) error
}
