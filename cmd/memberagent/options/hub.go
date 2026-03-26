/*
Copyright 2026 The KubeFleet Authors.

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

package options

import (
	"flag"
)

// HubConnectivityOptions is a set of options that control how the KubeFleet
// member agent connects to the hub cluster.
type HubConnectivityOptions struct {
	// Enable certificate-based authentication or not when connecting to the hub cluster.
	//
	// If this is set to true, provide with the member agent the file paths to the key
	// and certificate to use for authentication via the `IDENTITY_KEY` and `IDENTITY_CERT`
	// environment variables respectively.
	//
	// Otherwise, the member agent will use token-based authentication when connecting
	// to the hub cluster. The agent will read the token from the file path specified
	// by the `CONFIG_PATH` environment variable.
	UseCertificateAuth bool

	// Use an insecure client or not when connecting to the hub cluster.
	//
	// If this is set to false, provide with the member agent a file path to the CA
	// bundle to use for verifying the hub cluster's identity via the `CA_BUNDLE` environment
	// variable; you can also give the member agent a file path to the CA data instead
	// via the `HUB_CERTIFICATE_AUTHORITY` environment variable.
	UseInsecureTLSClient bool
}

// AddFlags adds flags for HubConnectivityOptions to the specified FlagSet.
func (o *HubConnectivityOptions) AddFlags(flags *flag.FlagSet) {
	flags.BoolVar(
		&o.UseCertificateAuth,
		"use-ca-auth",
		false,
		"Enable certificate-based authentication or not when connecting to the hub cluster.")

	flags.BoolVar(
		&o.UseInsecureTLSClient,
		"tls-insecure",
		false,
		"Use an insecure client or not when connecting to the hub cluster.")
}
