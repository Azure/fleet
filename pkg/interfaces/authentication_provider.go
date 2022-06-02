/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package interfaces

import (
	"context"
)

type AuthenticationFactory interface {
	RefreshToken(ctx context.Context, hubServerAPIAddress string) error
}
