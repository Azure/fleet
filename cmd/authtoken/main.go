/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package main

import (
	"context"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"go.goms.io/fleet/pkg/authtoken"
	"go.goms.io/fleet/pkg/authtoken/providers/azure"
	"go.goms.io/fleet/pkg/authtoken/providers/secret"
	"go.goms.io/fleet/pkg/interfaces"
)

var (
	configPath string
	clientID   string
)

func parseArgs() (interfaces.AuthTokenProvider, error) {
	var tokenProvider interfaces.AuthTokenProvider
	rootCmd := &cobra.Command{Use: "refreshtoken", Args: cobra.NoArgs}
	rootCmd.PersistentFlags().StringVar(&configPath, "file-path", "/config/token", "token file path")

	var secretName string
	var secretNamespace string
	secretCmd := &cobra.Command{
		Use:  "secret",
		Args: cobra.NoArgs,
		Run: func(_ *cobra.Command, args []string) {
			tokenProvider = secret.New(secretName, secretNamespace)
		},
	}

	secretCmd.Flags().StringVar(&secretName, "name", "", "Secret name (required)")
	_ = secretCmd.MarkFlagRequired("name")

	secretCmd.Flags().StringVar(&secretNamespace, "namespace", "", "Secret namespace (required)")
	_ = secretCmd.MarkFlagRequired("namespace")

	azureCmd := &cobra.Command{
		Use:  "azure",
		Args: cobra.NoArgs,
		Run: func(_ *cobra.Command, args []string) {
			tokenProvider = azure.New(clientID)
		},
	}

	azureCmd.Flags().StringVar(&clientID, "clientid", "", "Azure AAD client ID (required)")
	_ = azureCmd.MarkFlagRequired("clientid")

	rootCmd.AddCommand(secretCmd, azureCmd)
	err := rootCmd.Execute()

	if err != nil {
		return nil, err
	}
	return tokenProvider, nil
}

func main() {
	tokenProvider, err := parseArgs()
	if err != nil {
		klog.Error(err)
		os.Exit(-1)
	}

	tokenRefresher := authtoken.NewAuthTokenRefresher(tokenProvider,
		authtoken.NewWriter(authtoken.NewFactory(configPath).Create),
		authtoken.DefaultRefreshDurationFunc, authtoken.DefaultCreateTicker)

	err = tokenRefresher.RefreshToken(context.Background())
	if err != nil {
		klog.Error(err)
		os.Exit(-1)
	}
}
