/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"time"

	"github.com/Azure/go-autorest/autorest/date"
	"github.com/pkg/errors"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"go.goms.io/fleet/pkg/configprovider/azure"
)

var (
	configPath    = os.Getenv("CONFIG_PATH")
	providerName  = flag.String("provider-name", "azure", "Name of the provider used to refresh auth token. available options: azure, secret")
	AzureProvider = "azure"
	//SecretProvider = "secret"
)

func getTokenFilePath() (string, error) {
	homeDir := getHomeDir()

	tokenFilePath, err := filepath.Abs(filepath.Join(homeDir, configPath))
	if err != nil {
		return "", err
	}
	return tokenFilePath, nil
}

// getHomeDir attempts to get the home dir from env
func getHomeDir() string {
	// handle different runtime int the future
	return os.Getenv("HOME")
}

func refreshTokenRetry(ctx context.Context, filePath string) (*time.Time, error) {
	expiryDate := &time.Time{}

	var err error

	err = retry.OnError(retry.DefaultBackoff,
		func(err error) bool {
			return true
		}, func() error {
			switch providerName {
			case &AzureProvider:
				expiryDate, err = azure.NewFactory().RefreshToken(ctx, filePath)

				if err != nil {
					err = errors.Wrap(err, "cannot get the token")
				}
			}
			return err
		})
	if err != nil {
		return nil, errors.Wrap(err, "an error occurred while refreshing token")
	}
	return expiryDate, nil
}

func fetchTokenLoop(ctx context.Context, tokenFilePath string) {
	timeNow := time.Now()
	expiry, err := refreshTokenRetry(ctx, tokenFilePath)
	if err != nil {
		klog.Fatalf(err.Error(), " an error occurred while refreshing token")
	}
	if expiry.IsZero() {
		klog.Error(" Expiry date cannot be zero or nil. Setting expiry date to now")
		expiry = &timeNow
	}
	klog.Infof("new token refresh expiry date is %d", expiry)

	refreshPeriod := expiry.Hour() / 2
	refreshDate := time.Time(date.NewUnixTimeFromSeconds(float64(refreshPeriod))).UTC()
	klog.Infof("token will be refreshed in %d hours", timeNow.Hour()-refreshDate.Hour())

	// Run fetchToken after the updated expiry date
	time.AfterFunc(expiry.Sub(refreshDate), func() {
		fetchTokenLoop(ctx, tokenFilePath)
	})
}

func main() {
	flag.Parse()
	// secret will be a env var

	tokenFilePath, err := getTokenFilePath()
	if err != nil {
		klog.Error(err, " cannot retrieve the token file path")
	}

	fetchTokenLoop(context.Background(), tokenFilePath)

	// keep the main goroutine from exiting
	select {}
}
