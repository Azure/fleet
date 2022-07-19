/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package main

import (
	"context"
	"flag"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/klog/v2"

	"go.goms.io/fleet/pkg/authtoken"
	"go.goms.io/fleet/pkg/authtoken/providers/azure"
	"go.goms.io/fleet/pkg/authtoken/providers/secret"
	"go.goms.io/fleet/pkg/interfaces"
)

var (
	configPath string
)

func parseArgs() (interfaces.AuthTokenProvider, error) {
	var tokenProvider interfaces.AuthTokenProvider
	rootCmd := &cobra.Command{Use: "refreshtoken", Args: cobra.NoArgs}
	rootCmd.PersistentFlags().StringVar(&configPath, "file-path", "/config/token", "token file path")

	var secretName string
	var secretNamespace string
	var err error
	secretCmd := &cobra.Command{
		Use:  "secret",
		Args: cobra.NoArgs,
		Run: func(_ *cobra.Command, args []string) {
			tokenProvider, err = secret.New(secretName, secretNamespace)
			if err != nil {
				klog.ErrorS(err, "error while creating new secret provider")
				klog.FlushAndExit(klog.ExitFlushTimeout, 1)
			}
		},
	}

	secretCmd.Flags().StringVar(&secretName, "name", "", "Secret name (required)")
	_ = secretCmd.MarkFlagRequired("name")

	secretCmd.Flags().StringVar(&secretNamespace, "namespace", "", "Secret namespace (required)")
	_ = secretCmd.MarkFlagRequired("namespace")

	var clientID string
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
	err = rootCmd.Execute()
	if err != nil {
		return nil, err
	}
	return tokenProvider, nil
}

func main() {
	klog.InitFlags(nil)

	// Add go flags (e.g., --v) to pflag.
	// Reference: https://github.com/spf13/pflag#supporting-go-flags-when-using-pflag
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	tokenProvider, err := parseArgs()
	if err != nil {
		klog.ErrorS(err, "error has occurred while parsing refresh token flags")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	defer klog.Flush()

	klog.V(3).InfoS("creating token refresher")
	tokenRefresher := authtoken.NewAuthTokenRefresher(tokenProvider,
		authtoken.NewWriter(authtoken.NewFactory(configPath).Create),
		authtoken.DefaultRefreshDurationFunc, authtoken.DefaultCreateTicker)

	err = tokenRefresher.RefreshToken(context.Background())
	if err != nil {
		klog.ErrorS(err, "error has occurred while refreshing the token")
		os.Exit(1)
	}
}
