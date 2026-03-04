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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterManagementOptions is a set of options the KubeFleet hub agent exposes for
// managing member clusters.
type ClusterManagementOptions struct {
	// Expect that Fleet networking agents have been installed in the fleet or not. If set to true,
	// the hub agent will start to expect heartbeats from the networking agents on the member cluster related
	// resources.
	NetworkingAgentsEnabled bool

	// The duration the KubeFleet hub agent will wait for new heartbeats before marking a member cluster as unhealthy.
	UnhealthyThreshold metav1.Duration

	// The duration the KubeFleet hub agent will wait before force-deleting a member cluster resource after it has been
	// marked for deletion.
	ForceDeleteWaitTime metav1.Duration
}

// AddFlags adds flags for ClusterManagementOptions to the specified FlagSet.
func (o *ClusterManagementOptions) AddFlags(flags *flag.FlagSet) {
	flags.BoolVar(
		&o.NetworkingAgentsEnabled,
		"networking-agents-enabled",
		false,
		"Expect that Fleet networking agents have been installed in the fleet or not. If set to true, the hub agent will start to expect heartbeats from the networking agents on the member cluster related resources.",
	)

	flags.Var(
		newClusterUnhealthyThresholdValueWithValidation(60*time.Second, &o.UnhealthyThreshold),
		"cluster-unhealthy-threshold",
		"The duration the KubeFleet hub agent will wait for new heartbeats before marking a member cluster as unhealthy. Defaults to 60 seconds. Must be a duration in the range [30s, 1h].",
	)

	flags.Var(
		newForceDeleteWaitTimeValueWithValidation(15*time.Minute, &o.ForceDeleteWaitTime),
		"force-delete-wait-time",
		"The duration the KubeFleet hub agent will wait before force-deleting a member cluster resource after it has been marked for deletion. Defaults to 15 minutes. Must be a duration in the range [30s, 1h].",
	)
}

// A list of flag variables that allow pluggable validation logic when parsing the input args.

type ClusterUnhealthyThresholdValueWithValidation metav1.Duration

func (v *ClusterUnhealthyThresholdValueWithValidation) String() string {
	return v.Duration.String()
}

func (v *ClusterUnhealthyThresholdValueWithValidation) Set(s string) error {
	duration, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("failed to parse duration: %w", err)
	}
	if duration < 30*time.Second || duration > time.Hour {
		return fmt.Errorf("duration must be in the range [30s, 1h]")
	}
	v.Duration = duration
	return nil
}

func newClusterUnhealthyThresholdValueWithValidation(defaultVal time.Duration, p *metav1.Duration) *ClusterUnhealthyThresholdValueWithValidation {
	p.Duration = defaultVal
	return (*ClusterUnhealthyThresholdValueWithValidation)(p)
}

type ForceDeleteWaitTimeValueWithValidation metav1.Duration

func (v *ForceDeleteWaitTimeValueWithValidation) String() string {
	return v.Duration.String()
}

func (v *ForceDeleteWaitTimeValueWithValidation) Set(s string) error {
	duration, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("failed to parse duration: %w", err)
	}
	if duration < 30*time.Second || duration > time.Hour {
		return fmt.Errorf("duration must be in the range [30s, 1h]")
	}
	v.Duration = duration
	return nil
}

func newForceDeleteWaitTimeValueWithValidation(defaultVal time.Duration, p *metav1.Duration) *ForceDeleteWaitTimeValueWithValidation {
	p.Duration = defaultVal
	return (*ForceDeleteWaitTimeValueWithValidation)(p)
}
