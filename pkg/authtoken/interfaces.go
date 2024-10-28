/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package authtoken

import (
	"context"
	"time"
)

// An AuthToken is an authorization token for the fleet
type AuthToken struct {
	Token     string    // name of token
	ExpiresOn time.Time // expiration time of token
}

// AuthTokenProvider defines a method for fetching an AuthToken
type AuthTokenProvider interface {
	// FetchToken fetches an AuthToken
	// It returns an error if it is unable to fetch an AuthToken for the given input context
	FetchToken(ctx context.Context) (AuthToken, error)
}

// AuthTokenWriter defines a method for writing an AuthToken
type AuthTokenWriter interface {
	// WriteToken writes an AuthToken
	// It returns an error if it is unable to write the AuthToken
	WriteToken(token AuthToken) error
}
