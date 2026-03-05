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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"go.goms.io/fleet/pkg/utils"
)

// PlacementManagementOptions is a set of options the KubeFleet hub agent exposes for
// managing resource placements.
type PlacementManagementOptions struct {
	// The period the KubeFleet hub agent will wait before marking a Work object as failed.
	//
	// This option is no longer in use and is only kept for compatibility reasons.
	WorkPendingGracePeriod metav1.Duration

	// A list of APIs that are block-listed for resource placement. Any resources under such APIs will be ignored
	// by the KubeFleet hub agent and will not be selected for resource placement.
	//
	// The list is a collection of GVKs separated by semicolons. A GVK can be of the format GROUP,
	// GROUP/VERSION, or GROUP/VERSION/KINDS, where KINDS is a comma separated array of Kind values. If you would
	// like to skip specific versions and/or kinds in the core API group, use the format VERSION, or
	// VERSION/KINDS instead. Below are some examples:
	//
	// * networking.k8s.io: skip all resources in the networking.k8s.io API group for placement;
	// * networking.k8s.io/v1beta1: skip all resources in the networking.k8s.io/v1beta1 group version for placement;
	// * networking.k8s.io/v1beta1/Ingress,IngressClass: skip the Ingress and IngressClass resources
	//   in the networking.k8s.io/v1beta1 group version for placement;
	// * v1beta1: skip all resources of version v1beta1 in the core API group for placement;
	// * v1/ConfigMap: skip ConfigMap resources of version v1 in the core API group for placement;
	// * networking.k8s.io/v1beta1/Ingress; v1beta1: skip the Ingress resource in the networking.k8s.io/v1beta1 group version
	//   and all resources of version v1beta1 in the core API group for placement.
	//
	// This option is mutually exclusive with the AllowedPropagatingAPIs option. KubeFleet comes with a built-in
	// block list of APIs for resource placement that covers most KubeFleet APIs and a select few of critical Kubernetes
	// system APIs; see the source code for more information.
	SkippedPropagatingAPIs string
	// A list of APIs that are allow-listed for resource placement. If specified, only resources under such APIs
	// will be selected for resource placement by the KubeFleet hub agent.
	//
	// The list is a collection of GVKs separated by semicolons. A GVK can be of the format GROUP,
	// GROUP/VERSION, or GROUP/VERSION/KINDS, where KINDS is a comma separated array of Kind values. If you would
	// like to skip specific versions and/or kinds in the core API group, use the format VERSION, or
	// VERSION/KINDS instead. Below are some examples:
	//
	// * networking.k8s.io: allow all resources in the networking.k8s.io API group for placement only;
	// * networking.k8s.io/v1beta1: allow all resources in the networking.k8s.io/v1beta1 group version for placement only;
	// * networking.k8s.io/v1beta1/Ingress,IngressClass: allow the Ingress and IngressClass resources
	//   in the networking.k8s.io/v1beta1 group version for placement only;
	// * v1beta1: allow all resources of version v1beta1 in the core API group for placement only;
	// * v1/ConfigMap: allow ConfigMap resources of version v1 in the core API group for placement only;
	// * networking.k8s.io/v1beta1/Ingress; v1beta1: only allow the Ingress resource in the networking.k8s.io/v1beta1
	//   group version and all resources of version v1beta1 in the core API group for placement.
	//
	// This option is mutually exclusive with the SkippedPropagatingAPIs option.
	AllowedPropagatingAPIs string

	// A list of namespace names that are block-listed for resource placement. The KubeFleet hub agent
	// will ignore the namespaces and any resources within them when selecting resources for placement.
	//
	// This list is a collection of names separated by commas, such as `internals,monitoring`. KubeFleet
	// also blocks a number of reserved namespace names for placement by default; such namespaces include
	// those that are prefixed with `kube-`, and `fleet-system`.
	SkippedPropagatingNamespaces string

	// The number of concurrent workers that help process resource changes for the placement APIs.
	ConcurrentResourceChangeSyncs int

	// The expected maximum number of member clusters in the fleet. This is used specifically for the purpose
	// of setting the number of concurrent workers for several key placement related controllers; setting the value
	// higher increases the concurrency of such controllers. KubeFleet will not enforce this limit on the actual
	// number of member clusters in the fleet.
	MaxFleetSize int

	// The expected maximum number of placements that are allowed to run concurrently. This is used specifically for
	// the purpose of setting the number of concurrent workers for several key placement related controllers; setting
	// the value higher increases the concurrency of such controllers.
	MaxConcurrentClusterPlacement int

	// The rate limiting options for work queues in use by several placement related controllers.
	PlacementControllerWorkQueueRateLimiterOpts RateLimitOptions

	// The minimum interval between resource snapshot creations.
	//
	// KubeFleet will collect resource changes periodically (as controlled by the ResourceChangesCollectionDuration parameter);
	// if new changes are found, KubeFleet will build a new resource snapshot if there has not been any
	// new snapshot built within the ResourceSnapshotCreationMinimumInterval.
	ResourceSnapshotCreationMinimumInterval time.Duration

	// The interval between resource change collection attempts.
	//
	// KubeFleet will collect resource changes periodically (as controlled by the ResourceChangesCollectionDuration parameter);
	// if new changes are found, KubeFleet will build a new resource snapshot if there has not been any
	// new snapshot built within the ResourceSnapshotCreationMinimumInterval.
	ResourceChangesCollectionDuration time.Duration
}

// AddFlags adds flags for PlacementManagementOptions to the specified FlagSet.
func (o *PlacementManagementOptions) AddFlags(flags *flag.FlagSet) {
	flags.Var(
		newWorkPendingGracePeriodValueWithValidation(15*time.Second, &o.WorkPendingGracePeriod),
		"work-pending-grace-period",
		"The period the KubeFleet hub agent will wait before marking a Work object as failed. This option is no longer in use and is only kept for compatibility reasons.",
	)

	flags.Var(
		newSkippedPropagatingAPIsValueWithValidation("", &o.SkippedPropagatingAPIs),
		"skipped-propagating-apis",
		"A list of APIs that are block-listed for resource placement. Any resources under such APIs will be ignored by the KubeFleet hub agent and will not be selected for resource placement. The list is a collection of GVKs separated by semicolons. A GVK can be of the format GROUP, GROUP/VERSION, or GROUP/VERSION/KINDS, where KINDS is a comma separated array of Kind values. If you would like to skip specific versions and/or kinds in the core API group, use the format VERSION, or VERSION/KINDS instead. For example, `networking.k8s.io/v1beta1/Ingress,IngressClass; v1/ConfigMap`. This option is mutually exclusive with the AllowedPropagatingAPIs option. KubeFleet comes with a built-in block list of APIs for resource placement that covers most KubeFleet APIs and a select few of critical Kubernetes system APIs; see the source code for more information.",
	)

	flags.Var(
		newAllowedPropagatingAPIsValueWithValidation("", &o.AllowedPropagatingAPIs),
		"allowed-propagating-apis",
		"A list of APIs that are allow-listed for resource placement. If specified, only resources under such APIs will be selected for resource placement by the KubeFleet hub agent. The list is a collection of GVKs separated by semicolons. A GVK can be of the format GROUP, GROUP/VERSION, or GROUP/VERSION/KINDS, where KINDS is a comma separated array of Kind values. If you would like to skip specific versions and/or kinds in the core API group, use the format VERSION, or VERSION/KINDS instead. For example, `networking.k8s.io/v1beta1/Ingress,IngressClass; v1/ConfigMap`. This option is mutually exclusive with the SkippedPropagatingAPIs option.",
	)

	flags.StringVar(
		&o.SkippedPropagatingNamespaces,
		"skipped-propagating-namespaces",
		"",
		"A list of comma-separated namespace names that are block-listed for resource placement. The KubeFleet hub agent will ignore the namespaces and any resources within them when selecting resources for placement.",
	)

	flags.Var(
		newConcurrentResourceChangeSyncsValueWithValidation(20, &o.ConcurrentResourceChangeSyncs),
		"concurrent-resource-change-syncs",
		"The number of concurrent workers that help process resource changes for the placement APIs. Default is 20. Must be a positive integer value in the range [1, 100].",
	)

	flags.Var(
		newMaxFleetSizeValueWithValidation(100, &o.MaxFleetSize),
		"max-fleet-size",
		"The expected maximum number of member clusters in the fleet. This is used specifically for setting the number of concurrent workers for several key placement related controllers. Default is 100. Must be a positive integer value in the range [30, 200].",
	)

	flags.Var(
		newMaxConcurrentClusterPlacementValueWithValidation(100, &o.MaxConcurrentClusterPlacement),
		"max-concurrent-cluster-placement",
		"The expected maximum number of placements that are allowed to run concurrently. This is used specifically for setting the number of concurrent workers for several key placement related controllers. Default is 100. Must be a positive integer value in the range [10, 200].",
	)

	o.PlacementControllerWorkQueueRateLimiterOpts.AddFlags(flags)

	flags.Var(
		newResourceSnapshotCreationMinimumIntervalValueWithValidation(30*time.Second, &o.ResourceSnapshotCreationMinimumInterval),
		"resource-snapshot-creation-minimum-interval",
		"The minimum interval between resource snapshot creations. Default is 30 seconds. Must be a duration in the range [0s, 5m].",
	)

	flags.Var(
		newResourceChangesCollectionDurationValueWithValidation(15*time.Second, &o.ResourceChangesCollectionDuration),
		"resource-changes-collection-duration",
		"The interval between resource change collection attempts. Default is 15 seconds. Must be a duration in the range [0s, 1m].",
	)
}

// A list of flag variables that allow pluggable validation logic when parsing the input args.

type WorkPendingGracePeriodValueWithValidation metav1.Duration

func (v *WorkPendingGracePeriodValueWithValidation) String() string {
	return v.Duration.String()
}

func (v *WorkPendingGracePeriodValueWithValidation) Set(s string) error {
	klog.Warningf("the work-pending-grace-period option is no longer in use and is only kept for compatibility reasons, it has no effect on the system behavior and should not be set")
	v.Duration = 15 * time.Second
	return nil
}

func newWorkPendingGracePeriodValueWithValidation(defaultVal time.Duration, p *metav1.Duration) *WorkPendingGracePeriodValueWithValidation {
	p.Duration = defaultVal
	return (*WorkPendingGracePeriodValueWithValidation)(p)
}

type SkippedPropagatingAPIsValueWithValidation string

func (v *SkippedPropagatingAPIsValueWithValidation) String() string {
	return string(*v)
}

func (v *SkippedPropagatingAPIsValueWithValidation) Set(s string) error {
	rc := utils.NewResourceConfig(false)
	if err := rc.Parse(s); err != nil {
		return fmt.Errorf("invalid list of skipped for propagation APIs: %w", err)
	}
	*v = SkippedPropagatingAPIsValueWithValidation(s)
	return nil
}

func newSkippedPropagatingAPIsValueWithValidation(defaultVal string, p *string) *SkippedPropagatingAPIsValueWithValidation {
	*p = defaultVal
	return (*SkippedPropagatingAPIsValueWithValidation)(p)
}

type AllowedPropagatingAPIsValueWithValidation string

func (v *AllowedPropagatingAPIsValueWithValidation) String() string {
	return string(*v)
}

func (v *AllowedPropagatingAPIsValueWithValidation) Set(s string) error {
	rc := utils.NewResourceConfig(true)
	if err := rc.Parse(s); err != nil {
		return fmt.Errorf("invalid list of allowed for propagation APIs: %w", err)
	}
	*v = AllowedPropagatingAPIsValueWithValidation(s)
	return nil
}

func newAllowedPropagatingAPIsValueWithValidation(defaultVal string, p *string) *AllowedPropagatingAPIsValueWithValidation {
	*p = defaultVal
	return (*AllowedPropagatingAPIsValueWithValidation)(p)
}

type ConcurrentResourceChangeSyncsValueWithValidation int

func (v *ConcurrentResourceChangeSyncsValueWithValidation) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *ConcurrentResourceChangeSyncsValueWithValidation) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse int value: %w", err)
	}
	if n < 1 || n > 100 {
		return fmt.Errorf("number of concurrent resource change syncs must be in the range [1, 100]")
	}
	*v = ConcurrentResourceChangeSyncsValueWithValidation(n)
	return nil
}

func newConcurrentResourceChangeSyncsValueWithValidation(defaultVal int, p *int) *ConcurrentResourceChangeSyncsValueWithValidation {
	*p = defaultVal
	return (*ConcurrentResourceChangeSyncsValueWithValidation)(p)
}

type MaxFleetSizeValueWithValidation int

func (v *MaxFleetSizeValueWithValidation) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *MaxFleetSizeValueWithValidation) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse int value: %w", err)
	}
	if n < 30 || n > 200 {
		return fmt.Errorf("number of max fleet size must be in the range [30, 200]")
	}
	*v = MaxFleetSizeValueWithValidation(n)
	return nil
}

func newMaxFleetSizeValueWithValidation(defaultVal int, p *int) *MaxFleetSizeValueWithValidation {
	*p = defaultVal
	return (*MaxFleetSizeValueWithValidation)(p)
}

type MaxConcurrentClusterPlacementValueWithValidation int

func (v *MaxConcurrentClusterPlacementValueWithValidation) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *MaxConcurrentClusterPlacementValueWithValidation) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse int value: %w", err)
	}
	if n < 10 || n > 200 {
		return fmt.Errorf("number of max concurrent cluster placements must be in the range [10, 200]")
	}
	*v = MaxConcurrentClusterPlacementValueWithValidation(n)
	return nil
}

func newMaxConcurrentClusterPlacementValueWithValidation(defaultVal int, p *int) *MaxConcurrentClusterPlacementValueWithValidation {
	*p = defaultVal
	return (*MaxConcurrentClusterPlacementValueWithValidation)(p)
}

type ResourceSnapshotCreationMinimumIntervalValueWithValidation time.Duration

func (v *ResourceSnapshotCreationMinimumIntervalValueWithValidation) String() string {
	return time.Duration(*v).String()
}

func (v *ResourceSnapshotCreationMinimumIntervalValueWithValidation) Set(s string) error {
	duration, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("failed to parse duration: %w", err)
	}
	if duration < 0 || duration > 5*time.Minute {
		return fmt.Errorf("duration must be in the range [0s, 5m]")
	}
	*v = ResourceSnapshotCreationMinimumIntervalValueWithValidation(duration)
	return nil
}

func newResourceSnapshotCreationMinimumIntervalValueWithValidation(defaultVal time.Duration, p *time.Duration) *ResourceSnapshotCreationMinimumIntervalValueWithValidation {
	*p = defaultVal
	return (*ResourceSnapshotCreationMinimumIntervalValueWithValidation)(p)
}

type ResourceChangesCollectionDurationValueWithValidation time.Duration

func (v *ResourceChangesCollectionDurationValueWithValidation) String() string {
	return time.Duration(*v).String()
}

func (v *ResourceChangesCollectionDurationValueWithValidation) Set(s string) error {
	duration, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("failed to parse duration: %w", err)
	}
	if duration < 0 || duration > time.Minute {
		return fmt.Errorf("duration must be in the range [0s, 1m]")
	}
	*v = ResourceChangesCollectionDurationValueWithValidation(duration)
	return nil
}

func newResourceChangesCollectionDurationValueWithValidation(defaultVal time.Duration, p *time.Duration) *ResourceChangesCollectionDurationValueWithValidation {
	*p = defaultVal
	return (*ResourceChangesCollectionDurationValueWithValidation)(p)
}
