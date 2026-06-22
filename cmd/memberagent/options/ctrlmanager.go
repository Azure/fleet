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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CtrlManagerOptions struct {
	// This set of options apply to the controller manager instance that connects to the hub
	// cluster in the KubeFleet member agent.
	HubManagerOpts PerClusterCtrlManagerOptions
	// This set of options apply to the controller manager instance that connects to the member
	// cluster in the KubeFleet member agent.
	MemberManagerOpts PerClusterCtrlManagerOptions

	// Enable the pprof server for each controller manager instance or not.
	EnablePprof bool

	// The leader election related options that apply to both controller manager instances
	// in the KubeFleet member agent.
	LeaderElectionOpts LeaderElectionOptions
}

type PerClusterCtrlManagerOptions struct {
	// The TCP address that the controller manager would bind to
	// serve health probes. It can be set to "0" or the empty value to disable the health probe server.
	HealthProbeBindAddress string

	// The TCP address that the controller manager should bind to serve prometheus metrics.
	// It can be set to "0" or the empty value to disable the metrics server.
	MetricsBindAddress string

	// The port in use by the pprof server for profiling the controller manager. This option
	// only applies if pprof is enabled.
	PprofPort int

	// The QPS limit set to the rate limiter of the Kubernetes client in use by the controller manager
	// and all of its managed controller, for client-side throttling purposes.
	QPS float64

	// The burst limit set to the rate limiter of the Kubernetes client in use by the controller manager
	// and all of its managed controller, for client-side throttling purposes.
	Burst int
}

type LeaderElectionOptions struct {
	// Enable leader election or not.
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

func (o *CtrlManagerOptions) AddFlags(flags *flag.FlagSet) {
	flags.StringVar(
		&o.HubManagerOpts.HealthProbeBindAddress,
		"hub-health-probe-bind-address",
		":8081",
		"The TCP address that the controller manager would bind to serve health probes. It can be set to '0' or the empty value to disable the health probe server.")

	flags.StringVar(
		&o.HubManagerOpts.MetricsBindAddress,
		"hub-metrics-bind-address",
		":8080",
		"The TCP address that the controller manager should bind to serve prometheus metrics. It can be set to '0' or the empty value to disable the metrics server.")

	flags.IntVar(
		&o.HubManagerOpts.PprofPort,
		"hub-pprof-port",
		6066,
		"The port in use by the pprof server for profiling the controller manager that connects to the hub cluster.")

	flags.Var(
		newHubQPSValueWithValidation(50.0, &o.HubManagerOpts.QPS),
		"hub-api-qps",
		"The QPS limit set to the rate limiter of the Kubernetes client in use by the controller manager (and all of its managed controllers) that connects to the hub cluster, for client-side throttling purposes.")

	flags.StringVar(
		&o.MemberManagerOpts.HealthProbeBindAddress,
		"health-probe-bind-address",
		":8091",
		"The TCP address that the controller manager would bind to serve health probes. It can be set to '0' or the empty value to disable the health probe server.")

	flags.StringVar(
		&o.MemberManagerOpts.MetricsBindAddress,
		"metrics-bind-address",
		":8090",
		"The TCP address that the controller manager should bind to serve prometheus metrics. It can be set to '0' or the empty value to disable the metrics server.")

	flags.IntVar(
		&o.MemberManagerOpts.PprofPort,
		"pprof-port",
		6065,
		"The port in use by the pprof server for profiling the controller manager that connects to the member cluster.")

	flags.Var(
		newHubBurstValueWithValidation(500, &o.HubManagerOpts.Burst),
		"hub-api-burst",
		"The burst limit set to the rate limiter of the Kubernetes client in use by the controller manager (and all of its managed controllers) that connects to the hub cluster, for client-side throttling purposes.")

	flags.Var(
		newMemberQPSValueWithValidation(250.0, &o.MemberManagerOpts.QPS),
		"member-api-qps",
		"The QPS limit set to the rate limiter of the Kubernetes client in use by the controller manager (and all of its managed controllers) that connects to the member cluster, for client-side throttling purposes.")

	flags.Var(
		newMemberBurstValueWithValidation(1000, &o.MemberManagerOpts.Burst),
		"member-api-burst",
		"The burst limit set to the rate limiter of the Kubernetes client in use by the controller manager (and all of its managed controllers) that connects to the member cluster, for client-side throttling purposes.")

	flags.BoolVar(
		&o.EnablePprof,
		"enable-pprof",
		false,
		"Enable the pprof server for profiling the member agent controller managers or not.")

	flags.BoolVar(
		&o.LeaderElectionOpts.LeaderElect,
		"leader-elect",
		false,
		"Enable leader election on the controller managers in use by the KubeFleet member agent.")

	// This input is sent to the controller manager for validation; no further check here.
	flags.DurationVar(
		&o.LeaderElectionOpts.LeaseDuration.Duration,
		"leader-lease-duration",
		15*time.Second,
		"The duration of a leader election lease. This is the period where a non-leader candidate will wait after observing a leadership renewal before attempting to acquire leadership of the current leader. And it is also effectively the maximum duration that a leader can be stopped before it is replaced by another candidate. The option only applies if leader election is enabled.")

	// This input is sent to the controller manager for validation; no further check here.
	flags.DurationVar(
		&o.LeaderElectionOpts.RenewDeadline.Duration,
		"leader-renew-deadline",
		10*time.Second,
		"The interval between attempts by the acting master to renew a leadership slot before it stops leading. This must be less than or equal to the lease duration. The option only applies if leader election is enabled.")

	// This input is sent to the controller manager for validation; no further check here.
	flags.DurationVar(
		&o.LeaderElectionOpts.RetryPeriod.Duration,
		"leader-retry-period",
		2*time.Second,
		"The duration the clients should wait between attempting acquisition and renewal of a leadership. The option only applies if leader election is enabled.")

	flags.StringVar(
		&o.LeaderElectionOpts.ResourceNamespace,
		"leader-election-namespace",
		"kube-system",
		"The namespace in which the leader election resource will be created. The option only applies if leader election is enabled.")
}

type HubQPSValueWithValidation float64

func (v *HubQPSValueWithValidation) String() string {
	return fmt.Sprintf("%f", *v)
}

func (v *HubQPSValueWithValidation) Set(s string) error {
	// Some validation is also performed on the controller manager side and the client-go side. Just
	// to be on the safer side we also impose some limits here.
	qps, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("failed to parse float64 value: %w", err)
	}

	if qps < 0.0 {
		// Disable client-side throttling.
		*v = -1.0
		return nil
	}

	if qps < 10.0 || qps > 1000.0 {
		return fmt.Errorf("QPS limit is set to an invalid value (%f), must be a value in the range [10.0, 1000.0]", qps)
	}
	*v = HubQPSValueWithValidation(qps)
	return nil
}

func newHubQPSValueWithValidation(defaultVal float64, p *float64) *HubQPSValueWithValidation {
	*p = defaultVal
	return (*HubQPSValueWithValidation)(p)
}

type HubBurstValueWithValidation int

func (v *HubBurstValueWithValidation) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *HubBurstValueWithValidation) Set(s string) error {
	// Some validation is also performed on the controller manager side and the client-go side. Just
	// to be on the safer side we also impose some limits here.
	burst, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse int value: %w", err)
	}

	if burst < 10 || burst > 2000 {
		return fmt.Errorf("burst limit is set to an invalid value (%d), must be a value in the range [10, 2000]", burst)
	}
	*v = HubBurstValueWithValidation(burst)
	return nil
}

func newHubBurstValueWithValidation(defaultVal int, p *int) *HubBurstValueWithValidation {
	*p = defaultVal
	return (*HubBurstValueWithValidation)(p)
}

type MemberQPSValueWithValidation float64

func (v *MemberQPSValueWithValidation) String() string {
	return fmt.Sprintf("%f", *v)
}

func (v *MemberQPSValueWithValidation) Set(s string) error {
	// Some validation is also performed on the controller manager side and the client-go side. Just
	// to be on the safer side we also impose some limits here.
	qps, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("failed to parse float64 value: %w", err)
	}

	if qps < 0.0 {
		// Disable client-side throttling.
		*v = -1.0
		return nil
	}

	if qps < 10.0 || qps > 10000.0 {
		return fmt.Errorf("QPS limit is set to an invalid value (%f), must be a value in the range [10.0, 10000.0]", qps)
	}
	*v = MemberQPSValueWithValidation(qps)
	return nil
}

func newMemberQPSValueWithValidation(defaultVal float64, p *float64) *MemberQPSValueWithValidation {
	*p = defaultVal
	return (*MemberQPSValueWithValidation)(p)
}

type MemberBurstValueWithValidation int

func (v *MemberBurstValueWithValidation) String() string {
	return fmt.Sprintf("%d", *v)
}

func (v *MemberBurstValueWithValidation) Set(s string) error {
	// Some validation is also performed on the controller manager side and the client-go side. Just
	// to be on the safer side we also impose some limits here.
	burst, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to parse int value: %w", err)
	}

	if burst < 10 || burst > 20000 {
		return fmt.Errorf("burst limit is set to an invalid value (%d), must be a value in the range [10, 20000]", burst)
	}
	*v = MemberBurstValueWithValidation(burst)
	return nil
}

func newMemberBurstValueWithValidation(defaultVal int, p *int) *MemberBurstValueWithValidation {
	*p = defaultVal
	return (*MemberBurstValueWithValidation)(p)
}
