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

package options

import (
	"flag"
	"fmt"
)

// WebhookOptions is a set of options the KubeFleet hub agent exposes for
// controlling webhook behavior.
type WebhookOptions struct {
	// Enable the KubeFleet webhooks or not.
	EnableWebhooks bool

	// The connection type used by the webhook client. Valid values are `url` and `service`.
	// NOTE: at this moment this setting seems to be superficial, as even with the value set to `url`,
	// the system would just compose a Kubernetes service URL based on the provided service name.
	// This option only applies if webhooks are enabled.
	ClientConnectionType string

	// The Kubernetes service name for hosting the webhooks. This option applies only if
	// webhooks are enabled.
	ServiceName string

	// Enable the KubeFleet guard rail webhook or not. The guard rail webhook helps guard against
	// inadvertent modifications to Fleet resources. This option only applies if webhooks are enabled.
	EnableGuardRail bool

	// A list of comma-separated usernames who are whitelisted in the guard rail webhook and
	// thus allowed to modify KubeFleet resources. This option only applies if the guard rail
	// webhook is enabled.
	GuardRailWhitelistedUsers string

	// Set the guard rail webhook to block users (with certain exceptions) from modifying the labels
	// on the MemberCluster resources. This option only applies if the guard rail webhook is enabled.
	GuardRailDenyModifyMemberClusterLabels bool

	// Enable workload resources (pods and replicaSets) to be created in the hub cluster or not.
	// If set to false, the KubeFleet pod and replicaset validating webhooks, which blocks the creation
	// of pods and replicaSets outside KubeFleet reserved namespaces for most users, will be disabled.
	// This option only applies if webhooks are enabled.
	EnableWorkload bool

	// Use the cert-manager project for managing KubeFleet webhook server certificates or not.
	// If set to false, the system will use self-signed certificates.
	// This option only applies if webhooks are enabled.
	UseCertManager bool
}

// AddFlags adds flags for WebhookOptions to the specified FlagSet.
func (o *WebhookOptions) AddFlags(flags *flag.FlagSet) {
	flags.BoolVar(
		&o.EnableWebhooks,
		"enable-webhook",
		true,
		"Enable the KubeFleet webhooks or not.",
	)

	flags.Func(
		"webhook-client-connection-type",
		"The connection type used by the webhook client. Valid values are `url` and `service`. Defaults to `url`. This option only applies if webhooks are enabled.",
		func(s string) error {
			if len(s) == 0 {
				o.ClientConnectionType = "url"
				return nil
			}

			parsedStr, err := parseWebhookClientConnectionString(s)
			if err != nil {
				return fmt.Errorf("invalid webhook client connection type: %w", err)
			}
			o.ClientConnectionType = string(parsedStr)
			return nil
		},
	)

	flags.StringVar(
		&o.ServiceName,
		"webhook-service-name",
		"fleetwebhook",
		"The Kubernetes service name for hosting the webhooks. This option only applies if webhooks are enabled.",
	)

	flags.BoolVar(
		&o.EnableGuardRail,
		"enable-guard-rail",
		false,
		"Enable the KubeFleet guard rail webhook or not. The guard rail webhook helps guard against inadvertent modifications to Fleet resources. This option only applies if webhooks are enabled.",
	)

	flags.StringVar(
		&o.GuardRailWhitelistedUsers,
		"whitelisted-users",
		"",
		"A list of comma-separated usernames who are whitelisted in the guard rail webhook and thus allowed to modify KubeFleet resources. This option only applies if the guard rail webhook is enabled.",
	)

	flags.BoolVar(
		&o.GuardRailDenyModifyMemberClusterLabels,
		"deny-modify-member-cluster-labels",
		false,
		"Set the guard rail webhook to block users (with certain exceptions) from modifying the labels on the MemberCluster resources. This option only applies if the guard rail webhook is enabled.",
	)

	flags.BoolVar(
		&o.EnableWorkload,
		"enable-workload",
		false,
		"Enable workload resources (pods and replicaSets) to be created in the hub cluster or not. If set to false, the KubeFleet pod and replicaset validating webhooks, which blocks the creation of pods and replicaSets outside KubeFleet reserved namespaces for most users, will be disabled. This option only applies if webhooks are enabled.",
	)

	flags.BoolVar(
		&o.UseCertManager,
		"use-cert-manager",
		false,
		"Use the cert-manager project for managing KubeFleet webhook server certificates or not. If set to false, the system will use self-signed certificates. If set to true, the EnableWorkload option must be set to true as well. This option only applies if webhooks are enabled.",
	)
}
