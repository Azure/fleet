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

package admissionpolicymanager

import (
	"go.goms.io/fleet/pkg/utils"
)

// PolicyGeneratorConfigs holds the configurations for all available admission policy
// generators.
//
// This type is exposed so that users can provide a configuration object (in its serialized form)
// that specifies individual configurations for each generator.
type PolicyGeneratorConfigs struct {
	PodsAndReplicaSetsVAPGeneratorConfig          *PodsAndReplicaSetsValidatingAdmissionPolicyGenerator              `json:"denyPodsAndReplicaSetsOutsideReservedNamespaces,omitempty"`
	SvcAccountsAndTokenRequestsVAPGeneratorConfig *ServiceAccountsAndTokenRequestsValidatingAdmissionPolicyGenerator `json:"denyServiceAccountsAndTokenRequestsInReservedNamespaces,omitempty"`
}

// DefaultPolicyGeneratorConfigs is the default configuration for all available admission policy generators.
var DefaultPolicyGeneratorConfigs = &PolicyGeneratorConfigs{
	PodsAndReplicaSetsVAPGeneratorConfig: &PodsAndReplicaSetsValidatingAdmissionPolicyGenerator{
		ReservedNamespacePrefixes: []string{utils.FleetNSNamePrefix, utils.KubeNSNamePrefix},
	},
	SvcAccountsAndTokenRequestsVAPGeneratorConfig: &ServiceAccountsAndTokenRequestsValidatingAdmissionPolicyGenerator{
		ReservedNamespacePrefixes: []string{utils.FleetNSNamePrefix, utils.KubeNSNamePrefix},
	},
}
