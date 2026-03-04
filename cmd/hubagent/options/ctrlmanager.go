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

// ControllerManagerOptions is a set of options the KubeFleet hub agent exposes for
// controlling the controller manager behavior.
type ControllerManagerOptions struct {
	// The TCP address that the hub agent controller manager would bind to
	// serve health probes. It can be set to "0" or the empty value to disable the health probe server.
	HealthProbeBindAddress string

	// The TCP address that the hub agent controller manager should bind to serve prometheus metrics.
	// It can be set to "0" or the empty value to disable the metrics server.
	MetricsBindAddress string

	// Enable the pprof server for profiling the hub agent controller manager or not.
	EnablePprof bool

	// The port in use by the pprof server for profiling the hub agent controller manager.
	PprofPort int

	// The QPS limit set to the rate limiter of the Kubernetes client in use by the controller manager
	// and all of its managed controller, for client-side throttling purposes.
	HubQPS float64

	// The burst limit set to the rate limiter of the Kubernetes client in use by the controller manager
	// and all of its managed controller, for client-side throttling purposes.
	HubBurst int

	// The duration for the informers in the controller manager to resync.
	ResyncPeriod metav1.Duration
}

// AddFlags adds flags for ControllerManagerOptions to the specified FlagSet.
func (o *ControllerManagerOptions) AddFlags(flags *flag.FlagSet) {
	// This input is sent to the controller manager for validation; no further check here.
	flags.StringVar(
		&o.HealthProbeBindAddress,
		"health-probe-bind-address",
		":8081",
		"The address (and port) on which the controller manager serves health probe requests. Set to '0' or empty value to disable the health probe server. Defaults to ':8081'.")

	// This input is sent to the controller manager for validation; no further check here.
	flags.StringVar(
		&o.MetricsBindAddress,
		"metrics-bind-address",
		":8080",
		"The address (and port) on which the controller manager serves prometheus metrics. Set to '0' or empty value to disable the metrics server. Defaults to ':8080'.")

	flags.BoolVar(
		&o.EnablePprof,
		"enable-pprof",
		false,
		"Enable the pprof server for profiling the hub agent controller manager or not.",
	)

	// This input is sent to the controller manager for validation; no further check here.
	flags.IntVar(
		&o.PprofPort,
		"pprof-port",
		6065,
		"The port that the agent will listen for serving pprof profiling requests. This option only applies if the pprof server is enabled. Defaults to 6065.",
	)

	flags.Var(newHubQPSValueWithValidation(250.0, &o.HubQPS), "hub-api-qps", "The QPS limit set to the rate limiter of the Kubernetes client in use by the controller manager and all of its managed controller, for client-side throttling purposes. Defaults to 250. Use a positive float64 value in the range [10.0, 10000.0], or set a less or equal to zero value to disable client-side throttling.")

	flags.Var(newHubBurstValueWithValidation(1000, &o.HubBurst), "hub-api-burst", "The burst limit set to the rate limiter of the Kubernetes client in use by the controller manager and all of its managed controller, for client-side throttling purposes. Defaults to 1000. Must be a positive value in the range [10, 20000], and it should be no less than the QPS limit.")

	flags.Var(newResyncPeriodValueWithValidation(6*time.Hour, &o.ResyncPeriod), "resync-period", "The duration for the informers in the controller manager to resync. Defaults to 6 hours. Must be a duration in the range [1h, 12h].")
}

// A list of flag variables that allow pluggable validation logic when parsing the input args.

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

	if qps < 10.0 || qps > 10000.0 {
		return fmt.Errorf("QPS limit is set to an invalid value (%f), must be a value in the range [10.0, 10000.0]", qps)
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

	if burst < 10 || burst > 20000 {
		return fmt.Errorf("burst limit is set to an invalid value (%d), must be a value in the range [10, 20000]", burst)
	}
	*v = HubBurstValueWithValidation(burst)
	return nil
}

func newHubBurstValueWithValidation(defaultVal int, p *int) *HubBurstValueWithValidation {
	*p = defaultVal
	return (*HubBurstValueWithValidation)(p)
}

type ResyncPeriodValueWithValidation metav1.Duration

func (v *ResyncPeriodValueWithValidation) String() string {
	return v.Duration.String()
}

func (v *ResyncPeriodValueWithValidation) Set(s string) error {
	// Some validation is also performed on the controller manager side. Just
	// to be on the safer side we also impose some limits here.
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("failed to parse duration value: %w", err)
	}
	if dur < time.Hour || dur > 12*time.Hour {
		return fmt.Errorf("resync period is set to an invalid value (%s), must be a value in the range [1h, 12h]", s)
	}
	v.Duration = dur
	return nil
}

func newResyncPeriodValueWithValidation(defaultVal time.Duration, p *metav1.Duration) *ResyncPeriodValueWithValidation {
	p.Duration = defaultVal
	return (*ResyncPeriodValueWithValidation)(p)
}
