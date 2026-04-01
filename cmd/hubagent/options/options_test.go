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
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestLeaderElectionOpts tests the parsing and validation logic of the leader election options defined in LeaderElectionOptions.
func TestLeaderElectionOpts(t *testing.T) {
	testCases := []struct {
		name                   string
		flagSetName            string
		args                   []string
		wantLeaderElectionOpts LeaderElectionOptions
		wantErred              bool
		wantErrMsgSubStr       string
	}{
		{
			name:        "all default",
			flagSetName: "allDefault",
			args:        []string{},
			wantLeaderElectionOpts: LeaderElectionOptions{
				LeaderElect:         false,
				LeaseDuration:       metav1.Duration{Duration: 60 * time.Second},
				RenewDeadline:       metav1.Duration{Duration: 45 * time.Second},
				RetryPeriod:         metav1.Duration{Duration: 5 * time.Second},
				ResourceNamespace:   utils.FleetSystemNamespace,
				LeaderElectionQPS:   250.0,
				LeaderElectionBurst: 1000,
			},
		},
		{
			name:        "all specified",
			flagSetName: "allSpecified",
			args: []string{
				"--leader-elect=true",
				"--leader-lease-duration=30s",
				"--leader-renew-deadline=20s",
				"--leader-retry-period=5s",
				"--leader-election-namespace=test-namespace",
				"--leader-election-qps=500",
				"--leader-election-burst=1500",
			},
			wantLeaderElectionOpts: LeaderElectionOptions{
				LeaderElect:         true,
				LeaseDuration:       metav1.Duration{Duration: 30 * time.Second},
				RenewDeadline:       metav1.Duration{Duration: 20 * time.Second},
				RetryPeriod:         metav1.Duration{Duration: 5 * time.Second},
				ResourceNamespace:   "test-namespace",
				LeaderElectionQPS:   500.0,
				LeaderElectionBurst: 1500,
			},
		},
		{
			name:        "negative leader election QPS value",
			flagSetName: "qpsNegative",
			args:        []string{"--leader-election-qps=-5"},
			wantLeaderElectionOpts: LeaderElectionOptions{
				LeaderElect:         false,
				LeaseDuration:       metav1.Duration{Duration: 60 * time.Second},
				RenewDeadline:       metav1.Duration{Duration: 45 * time.Second},
				RetryPeriod:         metav1.Duration{Duration: 5 * time.Second},
				ResourceNamespace:   utils.FleetSystemNamespace,
				LeaderElectionQPS:   -1,
				LeaderElectionBurst: 1000,
			},
		},
		{
			name:             "leader election QPS parse error",
			flagSetName:      "qpsParseError",
			args:             []string{"--leader-election-qps=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse float64 value",
		},
		{
			name:             "leader election QPS out of range (too small)",
			flagSetName:      "qpsOutOfRangeTooSmall",
			args:             []string{"--leader-election-qps=9.9"},
			wantErred:        true,
			wantErrMsgSubStr: "QPS limit is set to an invalid value",
		},
		{
			name:             "leader election QPS out of range (too large)",
			flagSetName:      "qpsOutOfRangeTooLarge",
			args:             []string{"--leader-election-qps=1000.1"},
			wantErred:        true,
			wantErrMsgSubStr: "QPS limit is set to an invalid value",
		},
		{
			name:             "leader election burst parse error",
			flagSetName:      "burstParseError",
			args:             []string{"--leader-election-burst=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse int value",
		},
		{
			name:             "leader election burst out of range (too small)",
			flagSetName:      "burstOutOfRangeTooSmall",
			args:             []string{"--leader-election-burst=9"},
			wantErred:        true,
			wantErrMsgSubStr: "burst limit is set to an invalid value",
		},
		{
			name:             "leader election burst out of range (too large)",
			flagSetName:      "burstOutOfRangeTooLarge",
			args:             []string{"--leader-election-burst=2001"},
			wantErred:        true,
			wantErrMsgSubStr: "burst limit is set to an invalid value",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet(tc.flagSetName, flag.ContinueOnError)
			leaderElectionOpts := LeaderElectionOptions{}
			leaderElectionOpts.AddFlags(flags)

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

			if diff := cmp.Diff(leaderElectionOpts, tc.wantLeaderElectionOpts); diff != "" {
				t.Errorf("leader election options diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestControllerManagerOptions tests the parsing and validation logic of the controller manager options defined in ControllerManagerOptions.
func TestControllerManagerOptions(t *testing.T) {
	testCases := []struct {
		name             string
		flagSetName      string
		args             []string
		wantCtrlMgrOpts  ControllerManagerOptions
		wantErred        bool
		wantErrMsgSubStr string
	}{
		{
			name:        "all default",
			flagSetName: "allDefault",
			args:        []string{},
			wantCtrlMgrOpts: ControllerManagerOptions{
				HealthProbeBindAddress: ":8081",
				MetricsBindAddress:     ":8080",
				EnablePprof:            false,
				PprofPort:              6065,
				HubQPS:                 250.0,
				HubBurst:               1000,
				ResyncPeriod:           metav1.Duration{Duration: 6 * time.Hour},
			},
		},
		{
			name:        "all specified",
			flagSetName: "allSpecified",
			args: []string{
				"--health-probe-bind-address=:18081",
				"--metrics-bind-address=:18080",
				"--enable-pprof=true",
				"--pprof-port=16065",
				"--hub-api-qps=500",
				"--hub-api-burst=1500",
				"--resync-period=2h",
			},
			wantCtrlMgrOpts: ControllerManagerOptions{
				HealthProbeBindAddress: ":18081",
				MetricsBindAddress:     ":18080",
				EnablePprof:            true,
				PprofPort:              16065,
				HubQPS:                 500,
				HubBurst:               1500,
				ResyncPeriod:           metav1.Duration{Duration: 2 * time.Hour},
			},
		},
		{
			name:        "negative hub client QPS value",
			flagSetName: "qpsNegative",
			args:        []string{"--hub-api-qps=-5"},
			wantCtrlMgrOpts: ControllerManagerOptions{
				HealthProbeBindAddress: ":8081",
				MetricsBindAddress:     ":8080",
				EnablePprof:            false,
				PprofPort:              6065,
				HubQPS:                 -1,
				HubBurst:               1000,
				ResyncPeriod:           metav1.Duration{Duration: 6 * time.Hour},
			},
		},
		{
			name:             "hub client QPS parse error",
			flagSetName:      "qpsParseError",
			args:             []string{"--hub-api-qps=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse float64 value",
		},
		{
			name:             "hub client QPS out of range (too small)",
			flagSetName:      "qpsOutOfRangeTooSmall",
			args:             []string{"--hub-api-qps=9.9"},
			wantErred:        true,
			wantErrMsgSubStr: "QPS limit is set to an invalid value",
		},
		{
			name:             "hub client QPS out of range (too large)",
			flagSetName:      "qpsOutOfRangeTooLarge",
			args:             []string{"--hub-api-qps=10000.1"},
			wantErred:        true,
			wantErrMsgSubStr: "QPS limit is set to an invalid value",
		},
		{
			name:             "hub client burst parse error",
			flagSetName:      "burstParseError",
			args:             []string{"--hub-api-burst=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse int value",
		},
		{
			name:             "hub client burst out of range (too small)",
			flagSetName:      "burstOutOfRangeTooSmall",
			args:             []string{"--hub-api-burst=9"},
			wantErred:        true,
			wantErrMsgSubStr: "burst limit is set to an invalid value",
		},
		{
			name:             "hub client burst out of range (too large)",
			flagSetName:      "burstOutOfRangeTooLarge",
			args:             []string{"--hub-api-burst=20001"},
			wantErred:        true,
			wantErrMsgSubStr: "burst limit is set to an invalid value",
		},
		{
			name:             "resync period parse error",
			flagSetName:      "resyncParseError",
			args:             []string{"--resync-period=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse duration value",
		},
		{
			name:             "resync period out of range (too small)",
			flagSetName:      "resyncOutOfRangeTooSmall",
			args:             []string{"--resync-period=59m"},
			wantErred:        true,
			wantErrMsgSubStr: "resync period is set to an invalid value",
		},
		{
			name:             "resync period out of range (too large)",
			flagSetName:      "resyncOutOfRangeTooLarge",
			args:             []string{"--resync-period=13h"},
			wantErred:        true,
			wantErrMsgSubStr: "resync period is set to an invalid value",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet(tc.flagSetName, flag.ContinueOnError)
			ctrlMgrOpts := ControllerManagerOptions{}
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

// TestFeatureFlags tests the parsing and validation logic of the feature flags defined in FeatureFlags.
func TestFeatureFlags(t *testing.T) {
	testCases := []struct {
		name             string
		flagSetName      string
		args             []string
		wantFeatureFlags FeatureFlags
		wantErred        bool
		wantErrMsgSubStr string
	}{
		{
			name:        "all default",
			flagSetName: "allDefault",
			args:        []string{},
			wantFeatureFlags: FeatureFlags{
				EnableV1Beta1APIs:           true,
				EnableClusterInventoryAPIs:  true,
				EnableStagedUpdateRunAPIs:   true,
				EnableEvictionAPIs:          true,
				EnableResourcePlacementAPIs: true,
			},
		},
		{
			name:        "all specified",
			flagSetName: "allSpecified",
			args: []string{
				"--enable-v1beta1-apis=true",
				"--enable-cluster-inventory-apis=false",
				"--enable-staged-update-run-apis=false",
				"--enable-eviction-apis=false",
				"--enable-resource-placement=false",
			},
			wantFeatureFlags: FeatureFlags{
				EnableV1Beta1APIs:           true,
				EnableClusterInventoryAPIs:  false,
				EnableStagedUpdateRunAPIs:   false,
				EnableEvictionAPIs:          false,
				EnableResourcePlacementAPIs: false,
			},
		},
		{
			name:             "enable v1beta1 API option parse error",
			flagSetName:      "enableV1Beta1ParseError",
			args:             []string{"--enable-v1beta1-apis=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse bool value",
		},
		{
			name:             "enable v1beta1 API validation error",
			flagSetName:      "enableV1Beta1ValidationError",
			args:             []string{"--enable-v1beta1-apis=false"},
			wantErred:        true,
			wantErrMsgSubStr: "KubeFleet v1beta1 APIs are the storage version and must be enabled",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet(tc.flagSetName, flag.ContinueOnError)
			featureFlags := FeatureFlags{}
			featureFlags.AddFlags(flags)

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

			if diff := cmp.Diff(featureFlags, tc.wantFeatureFlags); diff != "" {
				t.Errorf("feature flags diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestClusterManagementOptions tests the parsing and validation logic of the cluster management options defined in ClusterManagementOptions.
func TestClusterManagementOptions(t *testing.T) {
	testCases := []struct {
		name                string
		flagSetName         string
		args                []string
		wantClusterMgmtOpts ClusterManagementOptions
		wantErred           bool
		wantErrMsgSubStr    string
	}{
		{
			name:        "all default",
			flagSetName: "allDefault",
			args:        []string{},
			wantClusterMgmtOpts: ClusterManagementOptions{
				NetworkingAgentsEnabled: false,
				UnhealthyThreshold:      metav1.Duration{Duration: 60 * time.Second},
				ForceDeleteWaitTime:     metav1.Duration{Duration: 15 * time.Minute},
			},
		},
		{
			name:        "all specified",
			flagSetName: "allSpecified",
			args: []string{
				"--networking-agents-enabled=true",
				"--cluster-unhealthy-threshold=45s",
				"--force-delete-wait-time=10m",
			},
			wantClusterMgmtOpts: ClusterManagementOptions{
				NetworkingAgentsEnabled: true,
				UnhealthyThreshold:      metav1.Duration{Duration: 45 * time.Second},
				ForceDeleteWaitTime:     metav1.Duration{Duration: 10 * time.Minute},
			},
		},
		{
			name:             "cluster unhealthy threshold parse error",
			flagSetName:      "clusterUnhealthyThresholdParseError",
			args:             []string{"--cluster-unhealthy-threshold=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse duration",
		},
		{
			name:             "cluster unhealthy threshold out of range (too small)",
			flagSetName:      "clusterUnhealthyThresholdOutOfRangeTooSmall",
			args:             []string{"--cluster-unhealthy-threshold=29s"},
			wantErred:        true,
			wantErrMsgSubStr: "duration must be in the range [30s, 1h]",
		},
		{
			name:             "cluster unhealthy threshold out of range (too large)",
			flagSetName:      "clusterUnhealthyThresholdOutOfRangeTooLarge",
			args:             []string{"--cluster-unhealthy-threshold=1h1s"},
			wantErred:        true,
			wantErrMsgSubStr: "duration must be in the range [30s, 1h]",
		},
		{
			name:             "force delete wait time parse error",
			flagSetName:      "forceDeleteWaitTimeParseError",
			args:             []string{"--force-delete-wait-time=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse duration",
		},
		{
			name:             "force delete wait time out of range (too small)",
			flagSetName:      "forceDeleteWaitTimeOutOfRangeTooSmall",
			args:             []string{"--force-delete-wait-time=20s"},
			wantErred:        true,
			wantErrMsgSubStr: "duration must be in the range [30s, 1h]",
		},
		{
			name:             "force delete wait time out of range (too large)",
			flagSetName:      "forceDeleteWaitTimeOutOfRangeTooLarge",
			args:             []string{"--force-delete-wait-time=1h1s"},
			wantErred:        true,
			wantErrMsgSubStr: "duration must be in the range [30s, 1h]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet(tc.flagSetName, flag.ContinueOnError)
			clusterMgmtOpts := ClusterManagementOptions{}
			clusterMgmtOpts.AddFlags(flags)

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

			if diff := cmp.Diff(clusterMgmtOpts, tc.wantClusterMgmtOpts); diff != "" {
				t.Errorf("cluster management options diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestPlacementManagementOptions tests the parsing and validation logic of the placement management options defined in PlacementManagementOptions.
func TestPlacementManagementOptions(t *testing.T) {
	testCases := []struct {
		name                  string
		flagSetName           string
		args                  []string
		wantPlacementMgmtOpts PlacementManagementOptions
		wantErred             bool
		wantErrMsgSubStr      string
	}{
		{
			name:        "all default",
			flagSetName: "allDefault",
			args:        []string{},
			wantPlacementMgmtOpts: PlacementManagementOptions{
				WorkPendingGracePeriod:        metav1.Duration{Duration: 15 * time.Second},
				SkippedPropagatingAPIs:        "",
				AllowedPropagatingAPIs:        "",
				SkippedPropagatingNamespaces:  "",
				ConcurrentResourceChangeSyncs: 20,
				MaxFleetSize:                  100,
				MaxConcurrentClusterPlacement: 100,
				PlacementControllerWorkQueueRateLimiterOpts: RateLimitOptions{
					RateLimiterBaseDelay:  5 * time.Millisecond,
					RateLimiterMaxDelay:   60 * time.Second,
					RateLimiterQPS:        10,
					RateLimiterBucketSize: 100,
				},
				ResourceSnapshotCreationMinimumInterval: 30 * time.Second,
				ResourceChangesCollectionDuration:       15 * time.Second,
			},
		},
		{
			name:        "all specified",
			flagSetName: "allSpecified",
			args: []string{
				"--work-pending-grace-period=30s",
				"--skipped-propagating-apis=apps/v1/Deployment",
				"--allowed-propagating-apis=batch/v1/Job",
				"--skipped-propagating-namespaces=ns1,ns2",
				"--concurrent-resource-change-syncs=30",
				"--max-fleet-size=150",
				"--max-concurrent-cluster-placement=120",
				"--resource-snapshot-creation-minimum-interval=45s",
				"--resource-changes-collection-duration=20s",
			},
			wantPlacementMgmtOpts: PlacementManagementOptions{
				WorkPendingGracePeriod:        metav1.Duration{Duration: 15 * time.Second},
				SkippedPropagatingAPIs:        "apps/v1/Deployment",
				AllowedPropagatingAPIs:        "batch/v1/Job",
				SkippedPropagatingNamespaces:  "ns1,ns2",
				ConcurrentResourceChangeSyncs: 30,
				MaxFleetSize:                  150,
				MaxConcurrentClusterPlacement: 120,
				PlacementControllerWorkQueueRateLimiterOpts: RateLimitOptions{
					RateLimiterBaseDelay:  5 * time.Millisecond,
					RateLimiterMaxDelay:   60 * time.Second,
					RateLimiterQPS:        10,
					RateLimiterBucketSize: 100,
				},
				ResourceSnapshotCreationMinimumInterval: 45 * time.Second,
				ResourceChangesCollectionDuration:       20 * time.Second,
			},
		},
		{
			name:             "skipped propagating APIs parse error",
			flagSetName:      "skippedPropagatingAPIsParseError",
			args:             []string{"--skipped-propagating-apis=a/b/c/d"},
			wantErred:        true,
			wantErrMsgSubStr: "invalid list of skipped for propagation APIs",
		},
		{
			name:             "allowed propagating APIs parse error",
			flagSetName:      "allowedPropagatingAPIsParseError",
			args:             []string{"--allowed-propagating-apis=a/b/c/d"},
			wantErred:        true,
			wantErrMsgSubStr: "invalid list of allowed for propagation APIs",
		},
		{
			name:             "concurrent resource change syncs parse error",
			flagSetName:      "concurrentResourceChangeSyncsParseError",
			args:             []string{"--concurrent-resource-change-syncs=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse int value",
		},
		{
			name:             "concurrent resource change syncs out of range (too small)",
			flagSetName:      "concurrentResourceChangeSyncsOutOfRangeTooSmall",
			args:             []string{"--concurrent-resource-change-syncs=0"},
			wantErred:        true,
			wantErrMsgSubStr: "number of concurrent resource change syncs must be in the range [1, 100]",
		},
		{
			name:             "concurrent resource change syncs out of range (too large)",
			flagSetName:      "concurrentResourceChangeSyncsOutOfRangeTooLarge",
			args:             []string{"--concurrent-resource-change-syncs=101"},
			wantErred:        true,
			wantErrMsgSubStr: "number of concurrent resource change syncs must be in the range [1, 100]",
		},
		{
			name:             "max fleet size parse error",
			flagSetName:      "maxFleetSizeParseError",
			args:             []string{"--max-fleet-size=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse int value",
		},
		{
			name:             "max fleet size out of range (too small)",
			flagSetName:      "maxFleetSizeOutOfRangeTooSmall",
			args:             []string{"--max-fleet-size=29"},
			wantErred:        true,
			wantErrMsgSubStr: "number of max fleet size must be in the range [30, 200]",
		},
		{
			name:             "max fleet size out of range (too large)",
			flagSetName:      "maxFleetSizeOutOfRangeTooLarge",
			args:             []string{"--max-fleet-size=201"},
			wantErred:        true,
			wantErrMsgSubStr: "number of max fleet size must be in the range [30, 200]",
		},
		{
			name:             "max concurrent cluster placement parse error",
			flagSetName:      "maxConcurrentClusterPlacementParseError",
			args:             []string{"--max-concurrent-cluster-placement=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse int value",
		},
		{
			name:             "max concurrent cluster placement out of range (too small)",
			flagSetName:      "maxConcurrentClusterPlacementOutOfRangeTooSmall",
			args:             []string{"--max-concurrent-cluster-placement=9"},
			wantErred:        true,
			wantErrMsgSubStr: "number of max concurrent cluster placements must be in the range [10, 200]",
		},
		{
			name:             "max concurrent cluster placement out of range (too large)",
			flagSetName:      "maxConcurrentClusterPlacementOutOfRangeTooLarge",
			args:             []string{"--max-concurrent-cluster-placement=201"},
			wantErred:        true,
			wantErrMsgSubStr: "number of max concurrent cluster placements must be in the range [10, 200]",
		},
		{
			name:             "resource snapshot creation minimum interval parse error",
			flagSetName:      "resourceSnapshotCreationMinimumIntervalParseError",
			args:             []string{"--resource-snapshot-creation-minimum-interval=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse duration",
		},
		{
			name:             "resource snapshot creation minimum interval out of range (too large)",
			flagSetName:      "resourceSnapshotCreationMinimumIntervalOutOfRangeTooLarge",
			args:             []string{"--resource-snapshot-creation-minimum-interval=6m"},
			wantErred:        true,
			wantErrMsgSubStr: "duration must be in the range [0s, 5m]",
		},
		{
			name:             "resource changes collection duration parse error",
			flagSetName:      "resourceChangesCollectionDurationParseError",
			args:             []string{"--resource-changes-collection-duration=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse duration",
		},
		{
			name:             "resource changes collection duration out of range (too large)",
			flagSetName:      "resourceChangesCollectionDurationOutOfRangeTooLarge",
			args:             []string{"--resource-changes-collection-duration=61s"},
			wantErred:        true,
			wantErrMsgSubStr: "duration must be in the range [0s, 1m]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet(tc.flagSetName, flag.ContinueOnError)
			placementMgmtOpts := PlacementManagementOptions{}
			placementMgmtOpts.AddFlags(flags)

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

			if diff := cmp.Diff(placementMgmtOpts, tc.wantPlacementMgmtOpts); diff != "" {
				t.Errorf("placement management options diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestWebhookOptions tests the parsing and validation logic of the webhook options defined in WebhookOptions.
func TestWebhookOptions(t *testing.T) {
	testCases := []struct {
		name             string
		flagSetName      string
		args             []string
		wantWebhookOpts  WebhookOptions
		wantErred        bool
		wantErrMsgSubStr string
	}{
		{
			name:        "all default",
			flagSetName: "allDefault",
			args:        []string{},
			wantWebhookOpts: WebhookOptions{
				EnableWebhooks:                         true,
				ClientConnectionType:                   "url",
				ServiceName:                            "fleetwebhook",
				EnableGuardRail:                        false,
				GuardRailWhitelistedUsers:              "",
				GuardRailDenyModifyMemberClusterLabels: false,
				EnableWorkload:                         false,
				EnablePDBs:                             true,
				UseCertManager:                         false,
			},
		},
		{
			name:        "all specified",
			flagSetName: "allSpecified",
			args: []string{
				"--enable-webhook=false",
				"--webhook-client-connection-type=service",
				"--webhook-service-name=customwebhook",
				"--enable-guard-rail=true",
				"--whitelisted-users=user1,user2",
				"--deny-modify-member-cluster-labels=true",
				"--enable-workload=true",
				"--use-cert-manager=true",
			},
			wantWebhookOpts: WebhookOptions{
				EnableWebhooks:                         false,
				ClientConnectionType:                   "service",
				ServiceName:                            "customwebhook",
				EnableGuardRail:                        true,
				GuardRailWhitelistedUsers:              "user1,user2",
				GuardRailDenyModifyMemberClusterLabels: true,
				EnableWorkload:                         true,
				EnablePDBs:                             true,
				UseCertManager:                         true,
			},
		},
		{
			name:        "webhook client connection type URL (case-insensitive)",
			flagSetName: "webhookClientConnTypeURL",
			args:        []string{"--webhook-client-connection-type=URL"},
			wantWebhookOpts: WebhookOptions{
				EnableWebhooks:                         true,
				ClientConnectionType:                   "url",
				ServiceName:                            "fleetwebhook",
				EnableGuardRail:                        false,
				GuardRailWhitelistedUsers:              "",
				GuardRailDenyModifyMemberClusterLabels: false,
				EnableWorkload:                         false,
				EnablePDBs:                             true,
				UseCertManager:                         false,
			},
		},
		{
			name:        "webhook client connection type service (case-insensitive)",
			flagSetName: "webhookClientConnTypeService",
			args:        []string{"--webhook-client-connection-type=Service"},
			wantWebhookOpts: WebhookOptions{
				EnableWebhooks:                         true,
				ClientConnectionType:                   "service",
				ServiceName:                            "fleetwebhook",
				EnableGuardRail:                        false,
				GuardRailWhitelistedUsers:              "",
				GuardRailDenyModifyMemberClusterLabels: false,
				EnableWorkload:                         false,
				EnablePDBs:                             true,
				UseCertManager:                         false,
			},
		},
		{
			name:             "invalid webhook client connection type",
			flagSetName:      "webhookClientConnTypeInvalid",
			args:             []string{"--webhook-client-connection-type=ftp"},
			wantErred:        true,
			wantErrMsgSubStr: "invalid webhook client connection type",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet(tc.flagSetName, flag.ContinueOnError)
			webhookOpts := WebhookOptions{}
			webhookOpts.AddFlags(flags)

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

			if diff := cmp.Diff(webhookOpts, tc.wantWebhookOpts); diff != "" {
				t.Errorf("webhook options diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestRateLimitOptions tests the parsing and validation logic of the rate limit options defined in RateLimitOptions.
func TestRateLimitOptions(t *testing.T) {
	testCases := []struct {
		name              string
		flagSetName       string
		args              []string
		wantRateLimitOpts RateLimitOptions
		wantErred         bool
		wantErrMsgSubStr  string
	}{
		{
			name:        "all default",
			flagSetName: "allDefault",
			args:        []string{},
			wantRateLimitOpts: RateLimitOptions{
				RateLimiterBaseDelay:  5 * time.Millisecond,
				RateLimiterMaxDelay:   60 * time.Second,
				RateLimiterQPS:        10,
				RateLimiterBucketSize: 100,
			},
		},
		{
			name:        "all specified",
			flagSetName: "allSpecified",
			args: []string{
				"--rate-limiter-base-delay=10ms",
				"--rate-limiter-max-delay=2s",
				"--rate-limiter-qps=20",
				"--rate-limiter-bucket-size=200",
			},
			wantRateLimitOpts: RateLimitOptions{
				RateLimiterBaseDelay:  10 * time.Millisecond,
				RateLimiterMaxDelay:   2 * time.Second,
				RateLimiterQPS:        20,
				RateLimiterBucketSize: 200,
			},
		},
		{
			name:             "rate limiter base delay parse error",
			flagSetName:      "rateLimiterBaseDelayParseError",
			args:             []string{"--rate-limiter-base-delay=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse time duration",
		},
		{
			name:             "rate limiter base delay out of range (too small)",
			flagSetName:      "rateLimiterBaseDelayOutOfRangeTooSmall",
			args:             []string{"--rate-limiter-base-delay=500us"},
			wantErred:        true,
			wantErrMsgSubStr: "the base delay must be a value between [1ms, 200ms]",
		},
		{
			name:             "rate limiter base delay out of range (too large)",
			flagSetName:      "rateLimiterBaseDelayOutOfRangeTooLarge",
			args:             []string{"--rate-limiter-base-delay=201ms"},
			wantErred:        true,
			wantErrMsgSubStr: "the base delay must be a value between [1ms, 200ms]",
		},
		{
			name:             "rate limiter max delay parse error",
			flagSetName:      "rateLimiterMaxDelayParseError",
			args:             []string{"--rate-limiter-max-delay=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse time duration",
		},
		{
			name:             "rate limiter max delay out of range (too small)",
			flagSetName:      "rateLimiterMaxDelayOutOfRangeTooSmall",
			args:             []string{"--rate-limiter-max-delay=500ms"},
			wantErred:        true,
			wantErrMsgSubStr: "the max delay must be a value between [1s, 5m]",
		},
		{
			name:             "rate limiter max delay out of range (too large)",
			flagSetName:      "rateLimiterMaxDelayOutOfRangeTooLarge",
			args:             []string{"--rate-limiter-max-delay=6m"},
			wantErred:        true,
			wantErrMsgSubStr: "the max delay must be a value between [1s, 5m]",
		},
		{
			name:             "rate limiter QPS parse error",
			flagSetName:      "rateLimiterQPSParseError",
			args:             []string{"--rate-limiter-qps=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse integer",
		},
		{
			name:             "rate limiter QPS out of range (too small)",
			flagSetName:      "rateLimiterQPSOutOfRangeTooSmall",
			args:             []string{"--rate-limiter-qps=0"},
			wantErred:        true,
			wantErrMsgSubStr: "the QPS must be a positive integer in the range [1, 1000]",
		},
		{
			name:             "rate limiter QPS out of range (too large)",
			flagSetName:      "rateLimiterQPSOutOfRangeTooLarge",
			args:             []string{"--rate-limiter-qps=1001"},
			wantErred:        true,
			wantErrMsgSubStr: "the QPS must be a positive integer in the range [1, 1000]",
		},
		{
			name:             "rate limiter bucket size parse error",
			flagSetName:      "rateLimiterBucketSizeParseError",
			args:             []string{"--rate-limiter-bucket-size=abc"},
			wantErred:        true,
			wantErrMsgSubStr: "failed to parse integer",
		},
		{
			name:             "rate limiter bucket size out of range (too small)",
			flagSetName:      "rateLimiterBucketSizeOutOfRangeTooSmall",
			args:             []string{"--rate-limiter-bucket-size=0"},
			wantErred:        true,
			wantErrMsgSubStr: "the bucket size must be a positive integer in the range [1, 10000]",
		},
		{
			name:             "rate limiter bucket size out of range (too large)",
			flagSetName:      "rateLimiterBucketSizeOutOfRangeTooLarge",
			args:             []string{"--rate-limiter-bucket-size=10001"},
			wantErred:        true,
			wantErrMsgSubStr: "the bucket size must be a positive integer in the range [1, 10000]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet(tc.flagSetName, flag.ContinueOnError)
			rateLimitOpts := RateLimitOptions{}
			rateLimitOpts.AddFlags(flags)

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

			if diff := cmp.Diff(rateLimitOpts, tc.wantRateLimitOpts); diff != "" {
				t.Errorf("rate limit options diff (-got, +want):\n%s", diff)
			}
		})
	}
}
