package interfaces

import (
	"context"

	"k8s.io/client-go/rest"
)

type AuthenticationProvider interface {
	GetConfig(ctx context.Context, hubServerAPIAddress string) (rest.Config, error)
}
