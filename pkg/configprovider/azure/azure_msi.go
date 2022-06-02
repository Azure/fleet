/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package azure

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/pkg/errors"
	"k8s.io/klog"

	"go.goms.io/fleet/pkg/configprovider"
	"go.goms.io/fleet/pkg/interfaces"
)

const (
	aksScope = "6dae42f8-4368-4678-94ff-3960e28e3630"
)

type azureProvider struct {
	ClientID string
}

func NewFactory() interfaces.AuthenticationFactory {
	return &azureProvider{
		ClientID: os.Getenv("AZURE_CLIENT_ID"),
	}
}

// RefreshToken gets a new token to make request to the associated fleet' hub cluster, and writes it to the mounted file.
func (a *azureProvider) RefreshToken(ctx context.Context, tokenFile string) (*time.Time, error) {
	if a.ClientID == "" {
		return nil, errors.New("client ID is cannot be empty")
	}

	klog.Info("Calling managed identity API to get new token")

	opts := &azidentity.ManagedIdentityCredentialOptions{ID: azidentity.ClientID(a.ClientID)}
	credential, err := azidentity.NewManagedIdentityCredential(opts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create managed identity cred.")
	}

	token, err := credential.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{aksScope},
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get a token")
	}
	byteToken, err := json.Marshal(token)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse acr access token")
	}

	klog.Info("writing new token to the file")

	err = configprovider.WriteTokenToFile(tokenFile, byteToken)

	if err != nil {
		return nil, err
	}
	return &token.ExpiresOn, err
}
