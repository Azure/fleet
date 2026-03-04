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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.goms.io/fleet/pkg/utils"
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
		15*time.Second,
		"The duration of a leader election lease. This is the period where a non-leader candidate will wait after observing a leadership renewal before attempting to acquire leadership of the current leader. And it is also effectively the maximum duration that a leader can be stopped before it is replaced by another candidate. The option only applies if leader election is enabled.",
	)

	// This input is sent to the controller manager for validation; no further check here.
	flags.DurationVar(
		&o.RenewDeadline.Duration,
		"leader-renew-deadline",
		10*time.Second,
		"The interval between attempts by the acting master to renew a leadership slot before it stops leading. This must be less than or equal to the lease duration. The option only applies if leader election is enabled",
	)

	// This input is sent to the controller manager for validation; no further check here.
	flags.DurationVar(
		&o.RetryPeriod.Duration,
		"leader-retry-period",
		2*time.Second,
		"The duration the clients should wait between attempting acquisition and renewal of a leadership. The option only applies if leader election is enabled",
	)

	// This input is sent to the controller manager for validation; no further check here.
	flags.StringVar(
		&o.ResourceNamespace,
		"leader-election-namespace",
		utils.FleetSystemNamespace,
		"The namespace of the resource object that will be used to lock during leader election cycles. The option only applies if leader election is enabled.",
	)
}
