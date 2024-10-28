/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package authtoken

import (
	"context"
	"time"
)

// AuthToken: Authorization Token containing token name as a string and its expiration time
type AuthToken struct {
	Token     string
	ExpiresOn time.Time
}

// AuthTokenProvider: Interface with a function that takes in a context input in order to fetch an AuthToken
type AuthTokenProvider interface {
	FetchToken(ctx context.Context) (AuthToken, error)
}

// AuthTokenWriter: Interface with a function to write an AuthToken
type AuthTokenWriter interface {
	WriteToken(token AuthToken) error
}
