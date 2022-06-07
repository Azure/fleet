/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package interfaces

import (
	"context"
	"time"
)

type AuthenticationFactory interface {
	//get a token and write it to a file and return its expiration date
	RefreshToken(ctx context.Context, filePath string) (*time.Time, error)
}
