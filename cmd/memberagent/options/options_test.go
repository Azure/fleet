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
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestHubConnectivityOptions tests the parsing of the hub connectivity options defined in HubConnectivityOptions.
func TestHubConnectivityOptions(t *testing.T) {
	testCases := []struct {
		name               string
		flagSetName        string
		args               []string
		wantHubConnectOpts HubConnectivityOptions
		wantErred          bool
		wantErrMsgSubStr   string
	}{
		{
			name:        "all default",
			flagSetName: "allDefault",
			args:        []string{},
			wantHubConnectOpts: HubConnectivityOptions{
				UseCertificateAuth:   false,
				UseInsecureTLSClient: false,
			},
		},
		{
			name:        "all specified",
			flagSetName: "allSpecified",
			args: []string{
				"--use-ca-auth=true",
				"--tls-insecure=true",
			},
			wantHubConnectOpts: HubConnectivityOptions{
				UseCertificateAuth:   true,
				UseInsecureTLSClient: true,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet(tc.flagSetName, flag.ContinueOnError)
			hubConnectOpts := HubConnectivityOptions{}
			hubConnectOpts.AddFlags(flags)

			err := flags.Parse(tc.args)
			if tc.wantErred {
				if err == nil {
					t.Fatalf("flag Parse() = nil, want erred")
				}

				if !strings.Contains(err.Error(), tc.wantErrMsgSubStr) {
					t.Fatalf("flag Parse() error = %v, want error msg with sub-string %s", err, tc.wantErrMsgSubStr)
				}
				return
			}

			if err != nil {
				t.Fatalf("flag Parse() = %v, want nil", err)
			}

			if diff := cmp.Diff(hubConnectOpts, tc.wantHubConnectOpts); diff != "" {
				t.Errorf("hub connectivity options diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestCtrlManagerOptions tests the parsing and validation logic of the controller manager options
// defined in CtrlManagerOptions, including the hub and member per-cluster options and the leader
// election options.
func TestCtrlManagerOptions(t *testing.T) {
	testCases := []struct {
		name             string
		flagSetName      string
		args             []string
		wantCtrlMgrOpts  CtrlManagerOptions
		wantErred        bool
		wantErrMsgSubStr string
	}{
		{
			name:        "all default",
			flagSetName: "allDefault",
			args:        []string{},
			wantCtrlMgrOpts: CtrlManagerOptions{
				HubManagerOpts: PerClusterCtrlManagerOptions{
					HealthProbeBindAddress: ":8081",
					MetricsBindAddress:     ":8080",
					PprofPort:              6066,
					QPS:                    50.0,
					Burst:                  500,
				},
				MemberManagerOpts: PerClusterCtrlManagerOptions{
					HealthProbeBindAddress: ":8091",
					MetricsBindAddress:     ":8090",
					PprofPort:              6065,
					QPS:                    250.0,
					Burst:                  1000,
				},
				EnablePprof: false,
				LeaderElectionOpts: LeaderElectionOptions{
					LeaderElect:       false,
					LeaseDuration:     metav1.Duration{Duration: 15 * time.Second},
					RenewDeadline:     metav1.Duration{Duration: 10 * time.Second},
					RetryPeriod:       metav1.Duration{Duration: 2 * time.Second},
					ResourceNamespace: "kube-system",
				},
			},
		},
		{
			name:        "all specified",
			flagSetName: "allSpecified",
			args: []string{
				"--hub-health-probe-bind-address=:18081",
				"--hub-metrics-bind-address=:18080",
				"--hub-pprof-port=16066",
				"--hub-api-qps=100",
				"--hub-api-burst=1000",
				"--health-probe-bind-address=:18091",
				"--metrics-bind-address=:18090",
				"--pprof-port=16065",
				"--member-api-qps=500",
				"--member-api-burst=2000",
				"--enable-pprof=true",
				"--leader-elect=true",
				"--leader-lease-duration=30s",
				"--leader-renew-deadline=20s",
				"--leader-retry-period=5s",
				"--leader-election-namespace=test-namespace",
			},
			wantCtrlMgrOpts: CtrlManagerOptions{
				HubManagerOpts: PerClusterCtrlManagerOptions{
					HealthProbeBindAddress: ":18081",
					MetricsBindAddress:     ":18080",
					PprofPort:              16066,
					QPS:                    100.0,
					Burst:                  1000,
				},
				MemberManagerOpts: PerClusterCtrlManagerOptions{
					HealthProbeBindAddress: ":18091",
					MetricsBindAddress:     ":18090",
					PprofPort:              16065,
					QPS:                    500.0,
					Burst:                  2000,
				},
				EnablePprof: true,
				LeaderElectionOpts: LeaderElectionOptions{
					LeaderElect:       true,
					LeaseDuration:     metav1.Duration{Duration: 30 * time.Second},
					RenewDeadline:     metav1.Duration{Duration: 20 * time.Second},
					RetryPeriod:       metav1.Duration{Duration: 5 * time.Second},
					ResourceNamespace: "test-namespace",
				},
			},
		},
		{
			name:        "negative hub client QPS value",
			flagSetName: "hubQPSNegative",
			args:        []string{"--hub-api-qps=-5"},
			wantCtrlMgrOpts: CtrlManagerOptions{
				HubManagerOpts: PerClusterCtrlManagerOptions{
					HealthProbeBindAddress: ":8081",
					MetricsBindAddress:     ":8080",
					PprofPort:              6066,
					QPS:                    -1,
					Burst:                  500,
				},
				MemberManagerOpts: PerClusterCtrlManagerOptions{
					HealthProbeBindAddress: ":8091",
					MetricsBindAddress:     ":8090",
					PprofPort:              6065,
					QPS:                    250.0,
					Burst:                  1000,
				},
				EnablePprof: false,
				LeaderElectionOpts: LeaderElectionOptions{
					LeaderElect:       false,
					LeaseDuration:     metav1.Duration{Duration: 15 * time.Second},
					RenewDeadline:     metav1.Duration{Duration: 10 * time.Second},
					RetryPeriod:       metav1.Duration{Duration: 2 * time.Second},
					ResourceNamespace: "kube-system",
				},
			},
		},
		{
			name:             "hub client QPS parse error",
			flagSetName:      "hubQPSParseError",
			args:             []string{"--hub-api-qps=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse float64 value",
		},
		{
			name:             "hub client QPS out of range (too small)",
			flagSetName:      "hubQPSOutOfRangeTooSmall",
			args:             []string{"--hub-api-qps=9.9"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("QPS limit is set to an invalid value (%f), must be a value in the range [10.0, 1000.0]", 9.9),
		},
		{
			name:             "hub client QPS out of range (too large)",
			flagSetName:      "hubQPSOutOfRangeTooLarge",
			args:             []string{"--hub-api-qps=1000.1"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("QPS limit is set to an invalid value (%f), must be a value in the range [10.0, 1000.0]", 1000.1),
		},
		{
			name:             "hub client burst parse error",
			flagSetName:      "hubBurstParseError",
			args:             []string{"--hub-api-burst=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse int value",
		},
		{
			name:             "hub client burst out of range (too small)",
			flagSetName:      "hubBurstOutOfRangeTooSmall",
			args:             []string{"--hub-api-burst=9"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("burst limit is set to an invalid value (%d), must be a value in the range [10, 2000]", 9),
		},
		{
			name:             "hub client burst out of range (too large)",
			flagSetName:      "hubBurstOutOfRangeTooLarge",
			args:             []string{"--hub-api-burst=2001"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("burst limit is set to an invalid value (%d), must be a value in the range [10, 2000]", 2001),
		},
		{
			name:        "negative member client QPS value",
			flagSetName: "memberQPSNegative",
			args:        []string{"--member-api-qps=-5"},
			wantCtrlMgrOpts: CtrlManagerOptions{
				HubManagerOpts: PerClusterCtrlManagerOptions{
					HealthProbeBindAddress: ":8081",
					MetricsBindAddress:     ":8080",
					PprofPort:              6066,
					QPS:                    50.0,
					Burst:                  500,
				},
				MemberManagerOpts: PerClusterCtrlManagerOptions{
					HealthProbeBindAddress: ":8091",
					MetricsBindAddress:     ":8090",
					PprofPort:              6065,
					QPS:                    -1,
					Burst:                  1000,
				},
				EnablePprof: false,
				LeaderElectionOpts: LeaderElectionOptions{
					LeaderElect:       false,
					LeaseDuration:     metav1.Duration{Duration: 15 * time.Second},
					RenewDeadline:     metav1.Duration{Duration: 10 * time.Second},
					RetryPeriod:       metav1.Duration{Duration: 2 * time.Second},
					ResourceNamespace: "kube-system",
				},
			},
		},
		{
			name:             "member client QPS parse error",
			flagSetName:      "memberQPSParseError",
			args:             []string{"--member-api-qps=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse float64 value",
		},
		{
			name:             "member client QPS out of range (too small)",
			flagSetName:      "memberQPSOutOfRangeTooSmall",
			args:             []string{"--member-api-qps=9.9"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("QPS limit is set to an invalid value (%f), must be a value in the range [10.0, 10000.0]", 9.9),
		},
		{
			name:             "member client QPS out of range (too large)",
			flagSetName:      "memberQPSOutOfRangeTooLarge",
			args:             []string{"--member-api-qps=10000.1"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("QPS limit is set to an invalid value (%f), must be a value in the range [10.0, 10000.0]", 10000.1),
		},
		{
			name:             "member client burst parse error",
			flagSetName:      "memberBurstParseError",
			args:             []string{"--member-api-burst=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse int value",
		},
		{
			name:             "member client burst out of range (too small)",
			flagSetName:      "memberBurstOutOfRangeTooSmall",
			args:             []string{"--member-api-burst=9"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("burst limit is set to an invalid value (%d), must be a value in the range [10, 20000]", 9),
		},
		{
			name:             "member client burst out of range (too large)",
			flagSetName:      "memberBurstOutOfRangeTooLarge",
			args:             []string{"--member-api-burst=20001"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("burst limit is set to an invalid value (%d), must be a value in the range [10, 20000]", 20001),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet(tc.flagSetName, flag.ContinueOnError)
			ctrlMgrOpts := CtrlManagerOptions{}
			ctrlMgrOpts.AddFlags(flags)

			err := flags.Parse(tc.args)
			if tc.wantErred {
				if err == nil {
					t.Fatalf("flag Parse() = nil, want erred")
				}

				if !strings.Contains(err.Error(), tc.wantErrMsgSubStr) {
					t.Fatalf("flag Parse() error = %v, want error msg with sub-string %s", err, tc.wantErrMsgSubStr)
				}
				return
			}

			if err != nil {
				t.Fatalf("flag Parse() = %v, want nil", err)
			}

			if diff := cmp.Diff(ctrlMgrOpts, tc.wantCtrlMgrOpts); diff != "" {
				t.Errorf("controller manager options diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestApplierOptions tests the parsing and validation logic of the applier options defined in ApplierOptions.
func TestApplierOptions(t *testing.T) {
	testCases := []struct {
		name             string
		flagSetName      string
		args             []string
		wantApplierOpts  ApplierOptions
		wantErred        bool
		wantErrMsgSubStr string
	}{
		{
			name:        "all default",
			flagSetName: "allDefault",
			args:        []string{},
			wantApplierOpts: ApplierOptions{
				ResourceForceDeletionWaitTimeMinutes:                                  5,
				EnablePriorityQueue:                                                   false,
				RequeueRateLimiterAttemptsWithFixedDelay:                              1,
				RequeueRateLimiterFixedDelaySeconds:                                   5,
				RequeueRateLimiterExponentialBaseForSlowBackoff:                       1.2,
				RequeueRateLimiterInitialSlowBackoffDelaySeconds:                      2,
				RequeueRateLimiterMaxSlowBackoffDelaySeconds:                          15,
				RequeueRateLimiterExponentialBaseForFastBackoff:                       1.5,
				RequeueRateLimiterMaxFastBackoffDelaySeconds:                          900,
				RequeueRateLimiterSkipToFastBackoffForAvailableOrDiffReportedWorkObjs: true,
				PriorityLinearEquationCoEffA:                                          -3,
				PriorityLinearEquationCoEffB:                                          100,
			},
		},
		{
			name:        "all specified",
			flagSetName: "allSpecified",
			args: []string{
				"--deletion-wait-time=10",
				"--enable-work-applier-priority-queue=true",
				"--work-applier-requeue-rate-limiter-attempts-with-fixed-delay=5",
				"--work-applier-requeue-rate-limiter-fixed-delay-seconds=10",
				"--work-applier-requeue-rate-limiter-exponential-base-for-slow-backoff=1.5",
				"--work-applier-requeue-rate-limiter-initial-slow-backoff-delay-seconds=5",
				"--work-applier-requeue-rate-limiter-max-slow-backoff-delay-seconds=30",
				"--work-applier-requeue-rate-limiter-exponential-base-for-fast-backoff=2.0",
				"--work-applier-requeue-rate-limiter-max-fast-backoff-delay-seconds=1800",
				"--work-applier-requeue-rate-limiter-skip-to-fast-backoff-for-available-or-diff-reported-work-objs=false",
				"--work-applier-priority-linear-equation-coeff-a=-10",
				"--work-applier-priority-linear-equation-coeff-b=500",
			},
			wantApplierOpts: ApplierOptions{
				ResourceForceDeletionWaitTimeMinutes:                                  10,
				EnablePriorityQueue:                                                   true,
				RequeueRateLimiterAttemptsWithFixedDelay:                              5,
				RequeueRateLimiterFixedDelaySeconds:                                   10,
				RequeueRateLimiterExponentialBaseForSlowBackoff:                       1.5,
				RequeueRateLimiterInitialSlowBackoffDelaySeconds:                      5,
				RequeueRateLimiterMaxSlowBackoffDelaySeconds:                          30,
				RequeueRateLimiterExponentialBaseForFastBackoff:                       2.0,
				RequeueRateLimiterMaxFastBackoffDelaySeconds:                          1800,
				RequeueRateLimiterSkipToFastBackoffForAvailableOrDiffReportedWorkObjs: false,
				PriorityLinearEquationCoEffA:                                          -10,
				PriorityLinearEquationCoEffB:                                          500,
			},
		},
		{
			name:             "deletion wait time parse error",
			flagSetName:      "deletionWaitTimeParseError",
			args:             []string{"--deletion-wait-time=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse integer value",
		},
		{
			name:             "deletion wait time out of range (too small)",
			flagSetName:      "deletionWaitTimeOutOfRangeTooSmall",
			args:             []string{"--deletion-wait-time=0"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("resource force deletion wait time in minutes is set to an invalid value (%d), must be a value in the range [1, 60]", 0),
		},
		{
			name:             "deletion wait time out of range (too large)",
			flagSetName:      "deletionWaitTimeOutOfRangeTooLarge",
			args:             []string{"--deletion-wait-time=61"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("resource force deletion wait time in minutes is set to an invalid value (%d), must be a value in the range [1, 60]", 61),
		},
		{
			name:             "requeue attempts with fixed delay parse error",
			flagSetName:      "requeueAttemptsWithFixedDelayParseError",
			args:             []string{"--work-applier-requeue-rate-limiter-attempts-with-fixed-delay=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse integer value",
		},
		{
			name:             "requeue attempts with fixed delay out of range (too small)",
			flagSetName:      "requeueAttemptsWithFixedDelayOutOfRangeTooSmall",
			args:             []string{"--work-applier-requeue-rate-limiter-attempts-with-fixed-delay=0"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("requeue rate limiter attempts with fixed delay is set to an invalid value (%d), must be a value in the range [1, 40]", 0),
		},
		{
			name:             "requeue attempts with fixed delay out of range (too large)",
			flagSetName:      "requeueAttemptsWithFixedDelayOutOfRangeTooLarge",
			args:             []string{"--work-applier-requeue-rate-limiter-attempts-with-fixed-delay=41"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("requeue rate limiter attempts with fixed delay is set to an invalid value (%d), must be a value in the range [1, 40]", 41),
		},
		{
			name:             "requeue fixed delay seconds parse error",
			flagSetName:      "requeueFixedDelaySecondsParseError",
			args:             []string{"--work-applier-requeue-rate-limiter-fixed-delay-seconds=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse integer value",
		},
		{
			name:             "requeue fixed delay seconds out of range (too small)",
			flagSetName:      "requeueFixedDelaySecondsOutOfRangeTooSmall",
			args:             []string{"--work-applier-requeue-rate-limiter-fixed-delay-seconds=1"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("requeue rate limiter fixed delay seconds is set to an invalid value (%d), must be a value in the range [2, 300]", 1),
		},
		{
			name:             "requeue fixed delay seconds out of range (too large)",
			flagSetName:      "requeueFixedDelaySecondsOutOfRangeTooLarge",
			args:             []string{"--work-applier-requeue-rate-limiter-fixed-delay-seconds=301"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("requeue rate limiter fixed delay seconds is set to an invalid value (%d), must be a value in the range [2, 300]", 301),
		},
		{
			name:             "requeue exponential base for slow backoff parse error",
			flagSetName:      "requeueExpBaseForSlowBackoffParseError",
			args:             []string{"--work-applier-requeue-rate-limiter-exponential-base-for-slow-backoff=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse float value",
		},
		{
			name:             "requeue exponential base for slow backoff out of range (too small)",
			flagSetName:      "requeueExpBaseForSlowBackoffOutOfRangeTooSmall",
			args:             []string{"--work-applier-requeue-rate-limiter-exponential-base-for-slow-backoff=1.04"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("requeue rate limiter exponential base for slow backoff is set to an invalid value (%g), must be a value in the range [1.05, 2]", 1.04),
		},
		{
			name:             "requeue exponential base for slow backoff out of range (too large)",
			flagSetName:      "requeueExpBaseForSlowBackoffOutOfRangeTooLarge",
			args:             []string{"--work-applier-requeue-rate-limiter-exponential-base-for-slow-backoff=2.01"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("requeue rate limiter exponential base for slow backoff is set to an invalid value (%g), must be a value in the range [1.05, 2]", 2.01),
		},
		{
			name:             "requeue initial slow backoff delay seconds parse error",
			flagSetName:      "requeueInitSlowBackoffDelaySecondsParseError",
			args:             []string{"--work-applier-requeue-rate-limiter-initial-slow-backoff-delay-seconds=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse integer value",
		},
		{
			name:             "requeue initial slow backoff delay seconds out of range (too small)",
			flagSetName:      "requeueInitSlowBackoffDelaySecondsOutOfRangeTooSmall",
			args:             []string{"--work-applier-requeue-rate-limiter-initial-slow-backoff-delay-seconds=1"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("requeue rate limiter initial slow backoff delay seconds is set to an invalid value (%d), must be a value no less than 2", 1),
		},
		{
			name:             "requeue max slow backoff delay seconds parse error",
			flagSetName:      "requeueMaxSlowBackoffDelaySecondsParseError",
			args:             []string{"--work-applier-requeue-rate-limiter-max-slow-backoff-delay-seconds=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse integer value",
		},
		{
			name:             "requeue max slow backoff delay seconds out of range (too small)",
			flagSetName:      "requeueMaxSlowBackoffDelaySecondsOutOfRangeTooSmall",
			args:             []string{"--work-applier-requeue-rate-limiter-max-slow-backoff-delay-seconds=1"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("requeue rate limiter max slow backoff delay seconds is set to an invalid value (%d), must be a value no less than 2", 1),
		},
		{
			name:             "requeue exponential base for fast backoff parse error",
			flagSetName:      "requeueExpBaseForFastBackoffParseError",
			args:             []string{"--work-applier-requeue-rate-limiter-exponential-base-for-fast-backoff=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse float value",
		},
		{
			name:             "requeue exponential base for fast backoff out of range (at lower boundary)",
			flagSetName:      "requeueExpBaseForFastBackoffOutOfRangeAtLowerBoundary",
			args:             []string{"--work-applier-requeue-rate-limiter-exponential-base-for-fast-backoff=1.0"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("requeue rate limiter exponential base for fast backoff is set to an invalid value (%g), must be a value in the range (1, 2]", 1.0),
		},
		{
			name:             "requeue exponential base for fast backoff out of range (too large)",
			flagSetName:      "requeueExpBaseForFastBackoffOutOfRangeTooLarge",
			args:             []string{"--work-applier-requeue-rate-limiter-exponential-base-for-fast-backoff=2.01"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("requeue rate limiter exponential base for fast backoff is set to an invalid value (%g), must be a value in the range (1, 2]", 2.01),
		},
		{
			name:             "requeue max fast backoff delay seconds parse error",
			flagSetName:      "requeueMaxFastBackoffDelaySecondsParseError",
			args:             []string{"--work-applier-requeue-rate-limiter-max-fast-backoff-delay-seconds=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse integer value",
		},
		{
			name:             "requeue max fast backoff delay seconds out of range (at lower boundary)",
			flagSetName:      "requeueMaxFastBackoffDelaySecondsOutOfRangeAtLowerBoundary",
			args:             []string{"--work-applier-requeue-rate-limiter-max-fast-backoff-delay-seconds=0"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("requeue rate limiter max fast backoff delay seconds is set to an invalid value (%d), must be a value in the range (0, 3600]", 0),
		},
		{
			name:             "requeue max fast backoff delay seconds out of range (too large)",
			flagSetName:      "requeueMaxFastBackoffDelaySecondsOutOfRangeTooLarge",
			args:             []string{"--work-applier-requeue-rate-limiter-max-fast-backoff-delay-seconds=3601"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("requeue rate limiter max fast backoff delay seconds is set to an invalid value (%d), must be a value in the range (0, 3600]", 3601),
		},
		{
			name:             "priority linear equation coefficient A parse error",
			flagSetName:      "priCoEffAParseError",
			args:             []string{"--work-applier-priority-linear-equation-coeff-a=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse integer value",
		},
		{
			name:             "priority linear equation coefficient A out of range (non-negative)",
			flagSetName:      "priCoEffAOutOfRangeNonNegative",
			args:             []string{"--work-applier-priority-linear-equation-coeff-a=0"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("priority linear equation coefficient A is set to an invalid value (%d), must be a negative integer no less than -100", 0),
		},
		{
			name:             "priority linear equation coefficient A out of range (too small)",
			flagSetName:      "priCoEffAOutOfRangeTooSmall",
			args:             []string{"--work-applier-priority-linear-equation-coeff-a=-101"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("priority linear equation coefficient A is set to an invalid value (%d), must be a negative integer no less than -100", -101),
		},
		{
			name:             "priority linear equation coefficient B parse error",
			flagSetName:      "priCoEffBParseError",
			args:             []string{"--work-applier-priority-linear-equation-coeff-b=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse integer value",
		},
		{
			name:             "priority linear equation coefficient B out of range (too small)",
			flagSetName:      "priCoEffBOutOfRangeTooSmall",
			args:             []string{"--work-applier-priority-linear-equation-coeff-b=0"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("priority linear equation coefficient B is set to an invalid value (%d), must be a positive integer no greater than 1000", 0),
		},
		{
			name:             "priority linear equation coefficient B out of range (too large)",
			flagSetName:      "priCoEffBOutOfRangeTooLarge",
			args:             []string{"--work-applier-priority-linear-equation-coeff-b=1001"},
			wantErred:        true,
			wantErrMsgSubStr: fmt.Sprintf("priority linear equation coefficient B is set to an invalid value (%d), must be a positive integer no greater than 1000", 1001),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet(tc.flagSetName, flag.ContinueOnError)
			applierOpts := ApplierOptions{}
			applierOpts.AddFlags(flags)

			err := flags.Parse(tc.args)
			if tc.wantErred {
				if err == nil {
					t.Fatalf("flag Parse() = nil, want erred")
				}

				if !strings.Contains(err.Error(), tc.wantErrMsgSubStr) {
					t.Fatalf("flag Parse() error = %v, want error msg with sub-string %s", err, tc.wantErrMsgSubStr)
				}
				return
			}

			if err != nil {
				t.Fatalf("flag Parse() = %v, want nil", err)
			}

			if diff := cmp.Diff(applierOpts, tc.wantApplierOpts); diff != "" {
				t.Errorf("applier options diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestPropertyProviderOptions tests the parsing of the property provider options defined in PropertyProviderOptions.
func TestPropertyProviderOptions(t *testing.T) {
	testCases := []struct {
		name                 string
		flagSetName          string
		args                 []string
		wantPropertyProvOpts PropertyProviderOptions
		wantErred            bool
		wantErrMsgSubStr     string
	}{
		{
			name:        "all default",
			flagSetName: "allDefault",
			args:        []string{},
			wantPropertyProvOpts: PropertyProviderOptions{
				Region:                         "",
				Name:                           "none",
				CloudConfigFilePath:            "/etc/kubernetes/provider/config.json",
				EnableAzProviderCostProperties: true,
				EnableAzProviderAvailableResourceProperties: true,
				EnableAzProviderNamespaceCollection:         false,
			},
		},
		{
			name:        "all specified",
			flagSetName: "allSpecified",
			args: []string{
				"--region=eastus",
				"--property-provider=azure",
				"--cloud-config=/custom/path/config.json",
				"--use-cost-properties-in-azure-provider=false",
				"--use-available-res-properties-in-azure-provider=false",
				"--enable-namespace-collection-in-property-provider=true",
			},
			wantPropertyProvOpts: PropertyProviderOptions{
				Region:                         "eastus",
				Name:                           "azure",
				CloudConfigFilePath:            "/custom/path/config.json",
				EnableAzProviderCostProperties: false,
				EnableAzProviderAvailableResourceProperties: false,
				EnableAzProviderNamespaceCollection:         true,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet(tc.flagSetName, flag.ContinueOnError)
			propertyProvOpts := PropertyProviderOptions{}
			propertyProvOpts.AddFlags(flags)

			err := flags.Parse(tc.args)
			if tc.wantErred {
				if err == nil {
					t.Fatalf("flag Parse() = nil, want erred")
				}

				if !strings.Contains(err.Error(), tc.wantErrMsgSubStr) {
					t.Fatalf("flag Parse() error = %v, want error msg with sub-string %s", err, tc.wantErrMsgSubStr)
				}
				return
			}

			if err != nil {
				t.Fatalf("flag Parse() = %v, want nil", err)
			}

			if diff := cmp.Diff(propertyProvOpts, tc.wantPropertyProvOpts); diff != "" {
				t.Errorf("property provider options diff (-got, +want):\n%s", diff)
			}
		})
	}
}
