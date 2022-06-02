/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package azure

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	"go.goms.io/fleet/pkg/configprovider"
	"go.goms.io/fleet/pkg/interfaces"
)

const (
	aksScope = "6dae42f8-4368-4678-94ff-3960e28e3630"
)

var (
	filePath      = "/config/token.json"
	refreshWithin = 4 * time.Hour
	refreshLock   sync.RWMutex
)

type azureToken struct {
}

// NewFactory
func NewFactory() interfaces.AuthenticationFactory {
	return &azureToken{}
}

// /refreshtoken
func CheckToken(rw http.ResponseWriter, req *http.Request) {
	currentFile, err := os.ReadFile(filePath)
	if err != nil {
		_, writeErr := rw.Write([]byte("cannot read the token file" + err.Error() + "\n"))
		if writeErr != nil {
			panic(1)
		}
	}
	if len(currentFile) == 0 {
		getTokenLoop(rw, req)
	}

	azToken := configprovider.EmptyToken()
	if err = json.Unmarshal(currentFile, azToken); err != nil {
		_, writeErr := rw.Write([]byte("cannot parse the token file" + err.Error() + "\n"))
		if writeErr != nil {
			panic(1)
		}
	}

	if azToken.WillExpireIn(refreshWithin) {
		getTokenLoop(rw, req)
	}
}

func getTokenLoop(rw http.ResponseWriter, req *http.Request) {
	az := new(azureToken)
	duration, err := time.ParseDuration("10s")
	if err != nil {
		_, writeErr := rw.Write([]byte("an error while parsing the duration time" + err.Error() + "\n"))
		if writeErr != nil {
			panic(1)
		}
	}
	refreshTokenBackoff := wait.Backoff{
		Duration: duration,
	}

	err = retry.OnError(refreshTokenBackoff,
		func(err error) bool {
			return true
		},
		func() error {
			err = az.RefreshToken(req.Context(), filePath)
			if err != nil {
				_, writeErr := rw.Write([]byte("cannot get the token" + err.Error() + "\n"))
				if writeErr != nil {
					panic(1)
				}
			}
			return err
		})
	if err != nil {
		_, writeErr := rw.Write([]byte("an error occurred while refreshing token" + err.Error() + "\n"))
		if writeErr != nil {
			panic(1)
		}
	}
}

// RefreshToken get a new token to make request to the associated fleet' hub cluster, and writes it to the mounted file.
func (a *azureToken) RefreshToken(ctx context.Context, tokenFile string) error {
	ClientID := os.Getenv("AZURE_CLIENT_ID")

	if ClientID == "" {
		return errors.New("client ID is cannot be empty")
	}

	currentFile, err := os.ReadFile(filePath)
	if err != nil {
		return errors.New("cannot read the token file" + err.Error() + "\n")
	}

	if len(currentFile) == 0 {
		return errors.New("cannot read the token file")
	}

	opts := &azidentity.ManagedIdentityCredentialOptions{ID: azidentity.ClientID(ClientID)}
	credential, err := azidentity.NewManagedIdentityCredential(opts)

	if err != nil {
		return errors.Wrap(err, "failed to create managed identity cred.")
	}

	refreshLock.Lock()
	defer refreshLock.Unlock()
	token, err := credential.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{aksScope},
	})
	if err != nil {
		return errors.Wrap(err, "failed to get a token")
	}

	file, err := os.OpenFile(filePath, os.O_WRONLY, os.ModePerm)
	if err != nil {
		return errors.Wrap(err, "cannot open the token file")
	}

	defer func() {
		err = file.Close()
	}()
	if err != nil {
		return errors.Wrap(err, "cannot close the token file")
	}

	expirationDate, err := configprovider.GetTokenExpiration(token.Token)
	if err != nil {
		return errors.Wrap(err, "failed to parse acr access token expiration")
	}

	azToken := configprovider.NewToken(token.Token, expirationDate)
	byteToken, err := json.Marshal(azToken)
	if err != nil {
		return errors.Wrap(err, "failed to parse acr access token")
	}

	_, err = file.Write(byteToken)
	if err != nil {
		return errors.Wrap(err, "cannot write the refresh token into the file")
	}

	return nil
}
