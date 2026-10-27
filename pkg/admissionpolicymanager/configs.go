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
	"reflect"

	"k8s.io/apimachinery/pkg/util/sets"

	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/errors"
)

// PolicyGeneratorConfigs holds the configurations for all available admission policy
// generators.
//
// This type is exposed so that users can provide a configuration object (in its serialized form)
// that specifies individual configurations for each generator.
type PolicyGeneratorConfigs struct {
	PodsAndReplicaSetsVAPGeneratorConfig          *PodsAndReplicaSetsValidatingAdmissionPolicyGenerator              `json:"denyPodsAndReplicaSetsOutsideReservedNamespaces,omitempty"`
	SvcAccountsAndTokenRequestsVAPGeneratorConfig *ServiceAccountsAndTokenRequestsValidatingAdmissionPolicyGenerator `json:"denyServiceAccountsAndTokenRequestsInReservedNamespaces,omitempty"`
	// The generator is not enabled in any of the default configurations and must be enabled explicitly. It should
	// only be enabled if the workload controllers of kube-controller-manager have been disabled in the hub cluster;
	// see AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator for more information.
	AnnotatedForPlacementObjectsVAPGeneratorConfig *AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator `json:"denyPlacementAnnotationIfNoPlacementPolicyAPIAccess,omitempty"`
}

// DefaultPolicyGeneratorConfigsForV0APIs is the default configuration of admission policy generators
// for placement with v0 APIs.
var DefaultPolicyGeneratorConfigsForV0APIs = &PolicyGeneratorConfigs{
	PodsAndReplicaSetsVAPGeneratorConfig: &PodsAndReplicaSetsValidatingAdmissionPolicyGenerator{
		ReservedNamespacePrefixes: []string{utils.FleetNSNamePrefix, utils.KubeNSNamePrefix},
	},
	SvcAccountsAndTokenRequestsVAPGeneratorConfig: &ServiceAccountsAndTokenRequestsValidatingAdmissionPolicyGenerator{
		ReservedNamespacePrefixes: []string{utils.FleetNSNamePrefix, utils.KubeNSNamePrefix},
		WhitelistedUsernames:      []string{aksSupportUser},
	},
}

// DefaultPolicyGeneratorConfigsForV1APIs is the default configuration of admission policy generators
// for placement with v1 APIs (placement policies). It covers all available admission policy generators
// except the placement annotation one (DenyPlacementAnnotationIfNoPlacementPolicyAPIAccess), which must be
// enabled explicitly.
var DefaultPolicyGeneratorConfigsForV1APIs = &PolicyGeneratorConfigs{
	PodsAndReplicaSetsVAPGeneratorConfig: &PodsAndReplicaSetsValidatingAdmissionPolicyGenerator{
		ReservedNamespacePrefixes: []string{utils.FleetNSNamePrefix, utils.KubeNSNamePrefix},
	},
	SvcAccountsAndTokenRequestsVAPGeneratorConfig: &ServiceAccountsAndTokenRequestsValidatingAdmissionPolicyGenerator{
		ReservedNamespacePrefixes: []string{utils.FleetNSNamePrefix, utils.KubeNSNamePrefix},
		WhitelistedUsernames:      []string{aksSupportUser},
	},
}

// Validate validates each generator configuration in the given PolicyGeneratorConfigs.
func (config *PolicyGeneratorConfigs) Validate() error {
	if config == nil {
		return nil
	}

	v := reflect.ValueOf(config).Elem()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.IsNil() {
			continue
		}
		gen, ok := field.Interface().(ValidatingAdmissionPolicyGenerator)
		if !ok {
			continue
		}
		if err := gen.Validate(); err != nil {
			return errors.Wraps(err, "one of the admission policy generators is invalid", "generator", gen.Name())
		}
	}
	return nil
}

// EnabledGenerators returns the set of names of generators that are enabled in the configuration.
func (config *PolicyGeneratorConfigs) EnabledGenerators() sets.Set[string] {
	enabled := sets.New[string]()
	if config == nil {
		return enabled
	}

	v := reflect.ValueOf(config).Elem()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.IsNil() {
			continue
		}
		gen, ok := field.Interface().(ValidatingAdmissionPolicyGenerator)
		if !ok {
			continue
		}
		enabled.Insert(gen.Name())
	}
	return enabled
}
