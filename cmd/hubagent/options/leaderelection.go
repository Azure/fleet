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
	"fmt"
	"strconv"
	"time"

	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LeaderElectionOptions is a set of options the KubeFleet hub agent exposes for controlling
// the leader election behaviors.
//
// Only a subset of leader election options supported by the controller manager is added here,
// for simplicity reasons.
type LeaderElectionOptions struct {
	// Enable leader election or not. This helps ensure that there is only one active
	// hub agent controller manager when multiple instances of the hub agent are running.
	LeaderElect bool

	// The duration of a leader election lease. This is the period where a non-leader candidate
	// will wait after observing a leadership renewal before attempting to acquire leadership of the
	// current leader. And it is also effectively the maximum duration that a leader can be stopped
	// before it is replaced by another candidate. The option only applies if leader election is enabled.
	LeaseDuration metav1.Duration

	// The interval between attempts by the acting master to renew a leadership slot
	// before it stops leading. This must be less than or equal to the lease duration.
	// The option only applies if leader election is enabled.
	RenewDeadline metav1.Duration

	// The duration the clients should wait between attempting acquisition and renewal of a
	// leadership. The option only applies if leader election is enabled.
	RetryPeriod metav1.Duration

	// The namespace of the resource object that will be used to lock during leader election cycles.
	// This option only applies if leader election is enabled.
	ResourceNamespace string

	// The QPS limit set to the rate limiter of the Kubernetes client in use by the controller manager
	// for leader election purposes. This sets up a separate client-side throttling mechanism specifically
	// for lease related operations, mostly to avoid an adverse situation where normal operations
	// in the controller manager starve the lease related operations, and thus cause unexpected leadership losses.
	LeaderElectionQPS float64

	// The burst limit set to the rate limiter of the Kubernetes client in use by the controller manager
	// for leader election purposes. This sets up a separate client-side throttling mechanism specifically
	// for lease related operations, mostly to avoid an adverse situation where normal operations
	// in the controller manager starve the lease related operations, and thus cause unexpected leadership losses.
	LeaderElectionBurst int
}

// AddFlags adds flags for LeaderElectionOptions to the specified FlagSet.
func (o *LeaderElectionOptions) AddFlags(flags *flag.FlagSet) {
	flags.BoolVar(
		&o.LeaderElect,
		"leader-elect",
		// Note: this should be overridden to true even in cases where the hub agent deployment runs with only
		// one replica, as this can help ensure system correctness during rolling updates.
		false,
		"Enable a leader election client to gain leadership before the hub agent controller manager starts to run or not.")

	// This input is sent to the controller manager for validation; no further check here.
	flags.DurationVar(
		&o.LeaseDuration.Duration,
		"leader-lease-duration",
		60*time.Second,
		"The duration of a leader election lease. This is the period where a non-leader candidate will wait after observing a leadership renewal before attempting to acquire leadership of the current leader. And it is also effectively the maximum duration that a leader can be stopped before it is replaced by another candidate. The option only applies if leader election is enabled.",
	)

	// This input is sent to the controller manager for validation; no further check here.
	flags.DurationVar(
		&o.RenewDeadline.Duration,
		"leader-renew-deadline",
		45*time.Second,
		"The interval between attempts by the acting master to renew a leadership slot before it stops leading. This must be less than or equal to the lease duration. The option only applies if leader election is enabled",
	)

	// This input is sent to the controller manager for validation; no further check here.
	flags.DurationVar(
		&o.RetryPeriod.Duration,
		"leader-retry-period",
		5*time.Second,
		"The duration the clients should wait between attempting acquisition and renewal of a leadership. The option only applies if leader election is enabled",
	)

	// This input is sent to the controller manager for validation; no further check here.
	flags.StringVar(
		&o.ResourceNamespace,
		"leader-election-namespace",
		utils.FleetSystemNamespace,
		"The namespace of the resource object that will be used to lock during leader election cycles. The option only applies if leader election is enabled.",
	)

	flags.Var(
		newLeaderElectionQPSValueWithValidation(250, &o.LeaderElectionQPS),
		"leader-election-qps",
		"The QPS limit set to the rate limiter of the Kubernetes client in use by the controller manager for leader election purposes. This sets up a separate client-side throttling mechanism specifically for lease related operations, mostly to avoid an adverse situation where normal operations in the controller manager starve the lease related operations, and thus cause unexpected leadership losses. Defaults to 250. Use a positive float64 value in the range [10.0, 1000.0], or set a less or equal to zero value to disable client-side throttling.")

	flags.Var(
		newLeaderElectionBurstValueWithValidation(1000, &o.LeaderElectionBurst),
		"leader-election-burst",
		"The burst limit set to the rate limiter of the Kubernetes client in use by the controller manager for leader election purposes. This sets up a separate client-side throttling mechanism specifically for lease related operations, mostly to avoid an adverse situation where normal operations in the controller manager starve the lease related operations, and thus cause unexpected leadership losses. Defaults to 1000. Use a positive int value in the range [10, 2000].")
}

// A list of flag variables that allow pluggable validation logic when parsing the input args.

type LeaderElectionQPSValueWithValidation float64

func (v *LeaderElectionQPSValueWithValidation) String() string {
	return fmt.Sprintf("%f", *v)
}

func (v *LeaderElectionQPSValueWithValidation) Set(s string) error {
	// Some validation is also performed on the controller manager side and the client-go side. Just
	// to be on the safer side we also impose some limits here.
	qps, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("failed to parse float64 value: %w", err)
	}

	if qps <= 0.0 {
		// Disable client-side throttling.
		*v = -1.0
		return nil
	}

	if qps < 10.0 || qps > 1000.0 {
		return fmt.Errorf("QPS limit is set to an invalid value (%f), must be a value in the range [10.0, 1000.0]", qps)
	}
	*v = LeaderElectionQPSValueWithValidation(qps)
	return nil
}

func newLeaderElectionQPSValueWithValidation(defaultVal float64, p *float64) *LeaderElectionQPSValueWithValidation {
	*p = defaultVal
	return (*LeaderElectionQPSValueWithValidation)(p)
}

type LeaderElectionBurstValueWithValidation int

func (v *LeaderElectionBurstValueWithValidation) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *LeaderElectionBurstValueWithValidation) Set(s string) error {
	// Some validation is also performed on the controller manager side and the client-go side. Just
	// to be on the safer side we also impose some limits here.
	burst, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse int value: %w", err)
	}

	if burst < 10 || burst > 2000 {
		return fmt.Errorf("burst limit is set to an invalid value (%d), must be a value in the range [10, 2000]", burst)
	}
	*v = LeaderElectionBurstValueWithValidation(burst)
	return nil
}

func newLeaderElectionBurstValueWithValidation(defaultVal int, p *int) *LeaderElectionBurstValueWithValidation {
	*p = defaultVal
	return (*LeaderElectionBurstValueWithValidation)(p)
}
