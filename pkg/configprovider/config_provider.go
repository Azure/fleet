/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package configprovider

import (
	"context"
	"errors"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"go.goms.io/fleet/pkg/interfaces"
	"k8s.io/client-go/rest"
)

const (
	aksScope = "6dae42f8-4368-4678-94ff-3960e28e3630"
)

type azureAuthProvider struct{}

func NewAzureAuthProvider() interfaces.AuthenticationProvider {
	return &azureAuthProvider{}
}

//GetConfig returns a RESTConfig which could be used to make request to the associated fleet' hub cluster.
func (obj *azureAuthProvider) GetConfig(ctx context.Context, hubServerAPIAddress string) (rest.Config, error) {
	clusterIdentity := os.Getenv("cluster_identity")

	if clusterIdentity == "" {
		return rest.Config{}, errors.New("cluster identity empty")
	}

	if hubServerAPIAddress == "" {
		return rest.Config{}, errors.New("hub server api address empty")
	}

	opts := &azidentity.ManagedIdentityCredentialOptions{ID: azidentity.ClientID(clusterIdentity)}
	credential, err := azidentity.NewManagedIdentityCredential(opts)

	if err != nil {
		return rest.Config{}, err
	}

	token, err := credential.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{aksScope},
	})

	if err != nil {
		return rest.Config{}, err
	}

	emptyToken := azcore.AccessToken{}
	if *(token) == emptyToken {
		return rest.Config{}, errors.New("access token empty")
	}

	return rest.Config{
		BearerToken: token.Token,
		Host:        hubServerAPIAddress,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true, // TODO (mng): Remove this line once server CA can be passed in as flag.
		},
	}, nil
}
