/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package azure

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"go.goms.io/fleet/pkg/interfaces"
)

const (
	aksScope = "6dae42f8-4368-4678-94ff-3960e28e3630"
)

type AzureAuthTokenProvider struct {
	ClientID string
	Scope    string
}

func New(clientID, scope string) interfaces.AuthTokenProvider {
	if scope == "" {
		scope = aksScope
	}
	return &AzureAuthTokenProvider{
		ClientID: clientID,
		Scope:    scope,
	}
}

// FetchToken gets a new token to make request to the associated fleet' hub cluster.
func (a *AzureAuthTokenProvider) FetchToken(ctx context.Context) (interfaces.AuthToken, error) {
	token := interfaces.AuthToken{}
	opts := &azidentity.ManagedIdentityCredentialOptions{ID: azidentity.ClientID(a.ClientID)}

	klog.V(2).InfoS("FetchToken", "client ID", a.ClientID)
	credential, err := azidentity.NewManagedIdentityCredential(opts)
	if err != nil {
		return token, fmt.Errorf("failed to create managed identity cred: %w", err)
	}
	var azToken *azcore.AccessToken
	err = retry.OnError(retry.DefaultBackoff,
		func(err error) bool {
			return ctx.Err() == nil
		}, func() error {
			klog.V(2).InfoS("GetToken start", "credential", credential)
			azToken, err = credential.GetToken(ctx, policy.TokenRequestOptions{
				Scopes: []string{a.Scope},
			})
			if err != nil {
				klog.ErrorS(err, "Failed to GetToken")
			}
			return err
		})
	if err != nil {
		return token, fmt.Errorf("failed to get a token: %w", err)
	}

	token.Token = azToken.Token
	token.ExpiresOn = azToken.ExpiresOn

	return token, nil
}
