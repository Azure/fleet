/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package azure

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/pkg/errors"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"go.goms.io/fleet/pkg/interfaces"
)

const (
	aksScope = "6dae42f8-4368-4678-94ff-3960e28e3630"
)

type azureAuthTokenProvider struct {
	clientID string
}

func New(clientID string) interfaces.AuthTokenProvider {
	return &azureAuthTokenProvider{
		clientID: clientID,
	}
}

// FetchToken gets a new token to make request to the associated fleet' hub cluster.
func (a *azureAuthTokenProvider) FetchToken(ctx context.Context) (interfaces.AuthToken, error) {
	token := interfaces.AuthToken{}
	opts := &azidentity.ManagedIdentityCredentialOptions{ID: azidentity.ClientID(a.clientID)}

	klog.V(5).InfoS("FetchToken", "client ID", a.clientID)
	credential, err := azidentity.NewManagedIdentityCredential(opts)
	if err != nil {
		return token, errors.Wrap(err, "failed to create managed identity cred.")
	}
	var azToken *azcore.AccessToken
	err = retry.OnError(retry.DefaultBackoff,
		func(err error) bool {
			return ctx.Err() == nil
		}, func() error {
			klog.V(5).InfoS("GetToken start", "credential", credential)
			azToken, err = credential.GetToken(ctx, policy.TokenRequestOptions{
				Scopes: []string{aksScope},
			})
			if err != nil {
				klog.ErrorS(err, "Failed to GetToken")
			}
			return err
		})
	if err != nil {
		return token, errors.Wrap(err, "failed to get a token")
	}

	token.Token = azToken.Token
	token.ExpiresOn = azToken.ExpiresOn

	return token, nil
}
