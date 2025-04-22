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
package main

import (
	"context"
	"flag"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/klog/v2"

	"github.com/kubefleet-dev/kubefleet/pkg/authtoken"
	"github.com/kubefleet-dev/kubefleet/pkg/authtoken/providers/azure"
	"github.com/kubefleet-dev/kubefleet/pkg/authtoken/providers/secret"
)

var (
	configPath string
)

func parseArgs() (authtoken.Provider, error) {
	var tokenProvider authtoken.Provider
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

	secretCmd.Flags().StringVar(&secretNamespace, "namespace", "default", "Secret namespace (required)")
	_ = secretCmd.MarkFlagRequired("namespace")

	var clientID string
	var scope string
	azureCmd := &cobra.Command{
		Use:  "azure",
		Args: cobra.NoArgs,
		Run: func(_ *cobra.Command, args []string) {
			tokenProvider = azure.New(clientID, scope)
		},
	}

	azureCmd.Flags().StringVar(&clientID, "clientid", "", "Azure AAD client ID (required)")
	// TODO: this scope argument is specific for Azure provider. We should allow registering and parsing provider specific argument
	// in provider level, instead of global level.
	azureCmd.Flags().StringVar(&scope, "scope", "", "Azure AAD token scope (optional)")
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
