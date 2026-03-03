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
	"strconv"
)

// FeatureFlags is a set of feature flags the KubeFleet hub agent exposes.
type FeatureFlags struct {
	// Enable the hub agent to watch the KubeFleet v1beta1 APIs or not. This flag is kept only for
	// compatibility reasons; it has no effect at this moment, as KubeFleet v1alpha1 APIs have
	// been removed and the v1beta1 APIs are the storage version in use.
	EnableV1Beta1APIs bool

	// Enable the ClusterInventory API support in the KubeFleet hub agent or not.
	//
	// ClusterInventory APIs are a set of Kubernetes Multi-Cluster SIG standard APIs for discovering
	// currently registered member clusters in a multi-cluster management platform.
	EnableClusterInventoryAPIs bool

	// Enable the StagedUpdateRun API support in the KubeFleet hub agent or not.
	//
	// StagedUpdateRun APIs are a set of KubeFleet APIs for progressively updating resource placements.
	EnableStagedUpdateRunAPIs bool

	// Enable the Eviction API support in the KubeFleet hub agent or not.
	//
	// Eviction APIs are a set of KubeFleet APIs for evicting resource placements from member clusters
	// with minimal disruptions.
	EnableEvictionAPIs bool

	// Enable the ResourcePlacement API support in the KubeFleet hub agent or not.
	//
	// ResourcePlacement APIs are a set of KubeFleet APIs for processing namespace scoped resource placements.
	// This flag does not concern the cluster-scoped placement APIs (`ClusterResourcePlacement` and its related APIs).
	EnableResourcePlacementAPIs bool
}

// AddFlags adds flags for FeatureFlags to the specified FlagSet.
func (o *FeatureFlags) AddFlags(flags *flag.FlagSet) {
	flags.Var(
		newEnableV1Beta1APIsValueWithValidation(true, &o.EnableV1Beta1APIs),
		"enable-v1beta1-apis",
		"Enable the hub agent to watch the KubeFleet v1beta1 APIs or not.",
	)

	flags.BoolVar(
		&o.EnableClusterInventoryAPIs,
		"enable-cluster-inventory-apis",
		true,
		"Enable the ClusterInventory API support in the KubeFleet hub agent or not.",
	)

	flags.BoolVar(
		&o.EnableStagedUpdateRunAPIs,
		"enable-staged-update-run-apis",
		true,
		"Enable the StagedUpdateRun API support in the KubeFleet hub agent or not.",
	)

	flags.BoolVar(
		&o.EnableEvictionAPIs,
		"enable-eviction-apis",
		true,
		"Enable the Eviction API support in the KubeFleet hub agent or not.",
	)

	flags.BoolVar(
		&o.EnableResourcePlacementAPIs,
		"enable-resource-placement",
		true,
		"Enable the ResourcePlacement API support (for namespace-scoped placements) in the KubeFleet hub agent or not.",
	)
}

// A list of flag variables that allow pluggable validation logic when parsing the input args.

type EnableV1Beta1APIsValueWithValidation bool

func (v *EnableV1Beta1APIsValueWithValidation) String() string {
	return fmt.Sprintf("%t", *v)
}

func (v *EnableV1Beta1APIsValueWithValidation) Set(s string) error {
	enabled, err := strconv.ParseBool(s)
	if err != nil {
		return fmt.Errorf("failed to parse bool value: %w", err)
	}
	if !enabled {
		return fmt.Errorf("the KubeFleet v1beta1 APIs are the storage version and must be enabled")
	}
	*v = EnableV1Beta1APIsValueWithValidation(enabled)
	return nil
}

func newEnableV1Beta1APIsValueWithValidation(defaultVal bool, p *bool) *EnableV1Beta1APIsValueWithValidation {
	*p = defaultVal
	return (*EnableV1Beta1APIsValueWithValidation)(p)
}
