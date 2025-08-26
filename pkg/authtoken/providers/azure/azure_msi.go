/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package azure

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"github.com/kubefleet-dev/kubefleet/pkg/authtoken"
)

const (
	aksScope = "6dae42f8-4368-4678-94ff-3960e28e3630"
)

type AuthTokenProvider struct {
	ClientID string
	Scope    string
}

func New(clientID, scope string) authtoken.Provider {
	if scope == "" {
		scope = aksScope
	}
	return &AuthTokenProvider{
		ClientID: clientID,
		Scope:    scope,
	}
}

// FetchToken gets a new token to make request to the associated fleet' hub cluster.
func (a *AuthTokenProvider) FetchToken(ctx context.Context) (authtoken.AuthToken, error) {
	token := authtoken.AuthToken{}

	httpClient := &http.Client{}
	opts := &azidentity.ManagedIdentityCredentialOptions{
		ClientOptions: azcore.ClientOptions{
			Transport: httpClient,
		},
		ID: azidentity.ClientID(a.ClientID),
	}

	klog.V(2).InfoS("FetchToken", "client ID", a.ClientID)
	credential, err := azidentity.NewManagedIdentityCredential(opts)
	if err != nil {
		return token, fmt.Errorf("failed to create managed identity cred: %w", err)
	}
	var azToken azcore.AccessToken
	err = retry.OnError(retry.DefaultBackoff,
		func(err error) bool {
			return ctx.Err() == nil
		}, func() error {
			klog.V(2).InfoS("GetToken start", "credential", credential)
			azToken, err = credential.GetToken(ctx, policy.TokenRequestOptions{
				Scopes: []string{a.Scope},
			})
			if err != nil {
				klog.ErrorS(err, "Failed to GetToken", "scope", a.Scope)
				// We may race at startup with a sidecar which inserts an iptables rule
				// to intercept IMDS calls.  If we get here before such an iptables rule
				// is inserted, we will inadvertently connect to real IMDS, which won't
				// be able to service our request.  IMDS does not set 'Connection:
				// close' on 400 errors.  Default Go HTTP client behavior will keep the
				// underlying TCP connection open for reuse, unaffected by iptables,
				// causing all further requests to continue to be sent to real IMDS and
				// fail.  If an error is returned from the IMDS call, explicitly close the
				// connection used by the HTTP client.
				httpClient.CloseIdleConnections()
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
