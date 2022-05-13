package main

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

const AKSScope = "6dae42f8-4368-4678-94ff-3960e28e3630"

func ProvideAzureToken(c *azidentity.ManagedIdentityCredential, ctx context.Context, policy policy.TokenRequestOptions) (*azcore.AccessToken, error) {
	return c.GetToken(ctx, policy)
}

func AzureMSIToken(azCredentialsFn func(options *azidentity.ManagedIdentityCredentialOptions) (*azidentity.ManagedIdentityCredential, error),
	clientID string, ctx context.Context,
	provideAZTokenFn func(c *azidentity.ManagedIdentityCredential, ctx context.Context, policy policy.TokenRequestOptions) (*azcore.AccessToken, error)) (*azcore.AccessToken, error) {

	opts := &azidentity.ManagedIdentityCredentialOptions{ID: azidentity.ClientID(clientID)}
	cred, err := azCredentialsFn(opts)
	if err != nil {
		return nil, err
	}

	token, err := provideAZTokenFn(cred, ctx, policy.TokenRequestOptions{
		Scopes: []string{AKSScope},
	})
	if err != nil {
		return nil, err
	}

	return token, err
}
