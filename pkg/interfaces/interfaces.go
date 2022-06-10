/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package interfaces

import (
	"context"
	"time"
)

type AuthToken struct {
	Token     string
	ExpiresOn time.Time
}

type AuthTokenProvider interface {
	FetchToken(ctx context.Context) (AuthToken, error)
}

type AuthTokenWriter interface {
	WriteToken(token AuthToken) error
}
