package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/stretchr/testify/assert"
)

func TestGetMSIToken(t *testing.T) {
	errCreatingCredentials := errors.New("err creating credentials")
	errGettingToken := errors.New("err getting token")
	tokenHappyPath := azcore.AccessToken{ExpiresOn: time.Now()}

	testCases := map[string]struct {
		provideAzCredentialsFn func(options *azidentity.ManagedIdentityCredentialOptions) (*azidentity.ManagedIdentityCredential, error)
		provideTokenFn         func(c *azidentity.ManagedIdentityCredential, ctx context.Context, policy policy.TokenRequestOptions) (*azcore.AccessToken, error)
		wantError              error
		wantToken              *azcore.AccessToken
	}{
		"happy path": {
			provideAzCredentialsFn: azidentity.NewManagedIdentityCredential,
			provideTokenFn: func(c *azidentity.ManagedIdentityCredential, ctx context.Context, policy policy.TokenRequestOptions) (*azcore.AccessToken, error) {
				return &tokenHappyPath, nil
			},
			wantError: nil,
			wantToken: &tokenHappyPath,
		},
		"error creating credentials": {
			provideAzCredentialsFn: func(options *azidentity.ManagedIdentityCredentialOptions) (*azidentity.ManagedIdentityCredential, error) {
				return nil, errCreatingCredentials
			},
			provideTokenFn: ProvideAzureToken,
			wantError:      errCreatingCredentials,
			wantToken:      nil,
		},
		"error getting token": {
			provideAzCredentialsFn: azidentity.NewManagedIdentityCredential,
			provideTokenFn: func(c *azidentity.ManagedIdentityCredential, ctx context.Context, policy policy.TokenRequestOptions) (*azcore.AccessToken, error) {
				return nil, errGettingToken
			},
			wantError: errGettingToken,
			wantToken: nil,
		},
	}
	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			token, err := AzureMSIToken(testCase.provideAzCredentialsFn, "", context.Background(), testCase.provideTokenFn)
			assert.Equalf(t, testCase.wantToken, token, "Test case %s", testName)
			assert.Equalf(t, testCase.wantError, err, "Test case %s", testName)
		})
	}
}
