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
	// Otherwise, unless UseKubeConfig is set, the member agent will use token-based
	// authentication when connecting to the hub cluster. The agent will read the token
	// from the file path specified by the `CONFIG_PATH` environment variable.
	UseCertificateAuth bool

	// Use an insecure client or not when connecting to the hub cluster.
	//
	// If this is set to false, provide with the member agent a file path to the CA
	// bundle to use for verifying the hub cluster's identity via the `CA_BUNDLE` environment
	// variable; you can also give the member agent a file path to the CA data instead
	// via the `HUB_CERTIFICATE_AUTHORITY` environment variable.
	UseInsecureTLSClient bool

	// Use a kubeconfig file for the hub cluster connection instead of the refresh-token
	// sidecar or UseCertificateAuth. When set, provide the member agent the path to a
	// kubeconfig file via the `KUBE_CONFIG_PATH` environment variable; it's loaded via
	// client-go's clientcmd (the same mechanism kubectl uses). This takes over the hub
	// connection entirely: HUB_SERVER_URL, UseCertificateAuth, UseInsecureTLSClient,
	// CA_BUNDLE/HUB_CERTIFICATE_AUTHORITY, and CONFIG_PATH/IDENTITY_KEY/IDENTITY_CERT are all
	// ignored, since the kubeconfig already carries the server URL, TLS configuration, and
	// authentication (a bearer token, a client certificate, or a standard Kubernetes exec
	// credential plugin — https://kubernetes.io/docs/reference/access-authn-authz/authentication/#client-go-credential-plugins).
	//
	// This is the vendor-neutral way to authenticate to the hub via a federated identity
	// (AWS IRSA, GCP Workload Identity, Azure AD workload identity, SPIFFE, etc.): point this
	// at a kubeconfig whose exec stanza invokes whatever credential plugin your platform
	// provides. KubeFleet itself has no vendor-specific knowledge of any of these; the exec
	// plugin binary must be present in the member agent image (e.g. via a custom image
	// layered on top of the official one, or via the Helm chart's extraInitContainers).
	//
	// Mutually exclusive with UseCertificateAuth.
	UseKubeConfig bool
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

	flags.BoolVar(
		&o.UseKubeConfig,
		"use-kubeconfig",
		false,
		"Use a kubeconfig file for the hub cluster connection instead of the refresh-token sidecar or --use-ca-auth. Requires the KUBE_CONFIG_PATH environment variable to also be set. Mutually exclusive with --use-ca-auth.")
}
