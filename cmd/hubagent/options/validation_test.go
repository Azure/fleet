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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"

	"github.com/kubefleet-dev/kubefleet/pkg/admissionpolicymanager"
)

const (
	testWebhookServiceName = "test-webhook"
)

// a callback function to modify options
type ModifyOptions func(option *Options)

// newTestOptions creates an Options with default parameters.
func newTestOptions(modifyOptions ModifyOptions) Options {
	option := Options{
		CtrlMgrOpts: ControllerManagerOptions{
			HubQPS:   250,
			HubBurst: 1000,
		},
		LeaderElectionOpts: LeaderElectionOptions{
			LeaderElectionQPS:   250.0,
			LeaderElectionBurst: 1000,
		},
		WebhookAndAdmissionPolicyOpts: WebhookAndAdmissionPolicyOptions{
			ClientConnectionType: "url",
			ServiceName:          testWebhookServiceName,
		},
		PlacementMgmtOpts: PlacementManagementOptions{
			SkippedPropagatingAPIs: "fleet.azure.com;multicluster.x-k8s.io",
			PlacementControllerWorkQueueRateLimiterOpts: RateLimitOptions{
				RateLimiterBaseDelay:  5 * time.Millisecond,
				RateLimiterMaxDelay:   60 * time.Second,
				RateLimiterQPS:        10,
				RateLimiterBucketSize: 100,
			},
		},
	}

	if modifyOptions != nil {
		modifyOptions(&option)
	}
	return option
}

func TestValidation(t *testing.T) {
	newPath := field.NewPath("Options")
	testCases := map[string]struct {
		opt  Options
		want field.ErrorList
	}{
		"valid Options": {
			opt:  newTestOptions(nil),
			want: field.ErrorList{},
		},
		"invalid HubBurst less than HubQPS": {
			opt: newTestOptions(func(options *Options) {
				options.CtrlMgrOpts.HubQPS = 100
				options.CtrlMgrOpts.HubBurst = 50
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("HubBurst"), 50, "The burst limit for client-side throttling must be greater than or equal to its QPS limit")},
		},
		"invalid: leader election burst value is less than its QPS value": {
			opt: newTestOptions(func(option *Options) {
				option.LeaderElectionOpts.LeaderElectionQPS = 100
				option.LeaderElectionOpts.LeaderElectionBurst = 50
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("LeaderElectionBurst"), 50, "The burst limit for client-side throttling of leader election related operations must be greater than or equal to its QPS limit")},
		},
		"WebhookServiceName is empty": {
			opt: newTestOptions(func(option *Options) {
				option.WebhookAndAdmissionPolicyOpts.EnableWebhooks = true
				option.WebhookAndAdmissionPolicyOpts.ServiceName = ""
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("WebhookServiceName"), "", "A webhook service name is required when webhooks are enabled")},
		},
		"UseCertManager without EnableWorkload": {
			opt: newTestOptions(func(option *Options) {
				option.WebhookAndAdmissionPolicyOpts.EnableWebhooks = true
				option.WebhookAndAdmissionPolicyOpts.ServiceName = testWebhookServiceName
				option.WebhookAndAdmissionPolicyOpts.UseCertManager = true
				option.WebhookAndAdmissionPolicyOpts.EnableWorkload = false
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("UseCertManager"), true, "If cert manager is used for securing webhook connections, the EnableWorkload option must be set to true, so that cert manager pods can run in the hub cluster.")},
		},
		"UseCertManager with EnableWebhook and EnableWorkload": {
			opt: newTestOptions(func(option *Options) {
				option.WebhookAndAdmissionPolicyOpts.EnableWebhooks = true
				option.WebhookAndAdmissionPolicyOpts.ServiceName = testWebhookServiceName
				option.WebhookAndAdmissionPolicyOpts.UseCertManager = true
				option.WebhookAndAdmissionPolicyOpts.EnableWorkload = true
			}),
			want: field.ErrorList{},
		},
		"mutually exclusive allowed/skipped propagating APIs": {
			opt: newTestOptions(func(option *Options) {
				option.PlacementMgmtOpts.AllowedPropagatingAPIs = "apps/v1/Deployment"
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("AllowedPropagatingAPIs"), "apps/v1/Deployment", "AllowedPropagatingAPIs and SkippedPropagatingAPIs options are mutually exclusive")},
		},
		"rate limiter base delay must be less than max delay": {
			opt: newTestOptions(func(option *Options) {
				option.PlacementMgmtOpts.PlacementControllerWorkQueueRateLimiterOpts.RateLimiterBaseDelay = 60 * time.Second
				option.PlacementMgmtOpts.PlacementControllerWorkQueueRateLimiterOpts.RateLimiterMaxDelay = 60 * time.Second
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("PlacementControllerWorkQueueRateLimiterOpts").Child("RateLimiterBaseDelay"), 60*time.Second, "the base delay for the placement controller set rate limiter must be less than its max delay")},
		},
		"rate limiter qps must be less than bucket size": {
			opt: newTestOptions(func(option *Options) {
				option.PlacementMgmtOpts.PlacementControllerWorkQueueRateLimiterOpts.RateLimiterQPS = 100
				option.PlacementMgmtOpts.PlacementControllerWorkQueueRateLimiterOpts.RateLimiterBucketSize = 10
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("PlacementControllerWorkQueueRateLimiterOpts").Child("RateLimiterQPS"), 100, "the QPS for the placement controller set rate limiter must be less than its bucket size")},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got := tc.opt.Validate()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Validate() errs mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestValidateAdmissionPolicyManagerConfig(t *testing.T) {
	newPath := field.NewPath("Options")
	tmpDir := t.TempDir()

	// Non-existent file: capture the OS error to mirror the exact detail string.
	nonExistentPath := filepath.Join(tmpDir, "nonexistent.yaml")
	_, readErr := os.ReadFile(nonExistentPath)

	// Invalid YAML file.
	invalidYAMLPath := filepath.Join(tmpDir, "invalid.yaml")
	if err := os.WriteFile(invalidYAMLPath, []byte("[unclosed"), 0600); err != nil {
		t.Fatalf("TestValidateAdmissionPolicyManagerConfig: failed to write invalid YAML file: %v", err)
	}
	var dummy admissionpolicymanager.PolicyGeneratorConfigs
	yamlErr := yaml.Unmarshal([]byte("[unclosed"), &dummy)

	// Config file that passes YAML parsing but fails Validate() (empty ReservedNamespacePrefixes).
	invalidConfig := admissionpolicymanager.PolicyGeneratorConfigs{
		PodsAndReplicaSetsVAPGeneratorConfig: &admissionpolicymanager.PodsAndReplicaSetsValidatingAdmissionPolicyGenerator{
			ReservedNamespacePrefixes: []string{},
		},
	}
	invalidConfigData, _ := yaml.Marshal(invalidConfig)
	invalidConfigPath := filepath.Join(tmpDir, "invalid-config.yaml")
	if err := os.WriteFile(invalidConfigPath, invalidConfigData, 0600); err != nil {
		t.Fatalf("TestValidateAdmissionPolicyManagerConfig: failed to write invalid config file: %v", err)
	}
	configValidateErr := invalidConfig.Validate()

	// Valid config file.
	validConfig := admissionpolicymanager.PolicyGeneratorConfigs{
		PodsAndReplicaSetsVAPGeneratorConfig: &admissionpolicymanager.PodsAndReplicaSetsValidatingAdmissionPolicyGenerator{
			ReservedNamespacePrefixes: []string{"fleet-", "kube-"},
		},
	}
	validConfigData, _ := yaml.Marshal(validConfig)
	validConfigPath := filepath.Join(tmpDir, "valid-config.yaml")
	if err := os.WriteFile(validConfigPath, validConfigData, 0600); err != nil {
		t.Fatalf("TestValidateAdmissionPolicyManagerConfig: failed to write valid config file: %v", err)
	}

	testCases := map[string]struct {
		opt  Options
		want field.ErrorList
	}{
		"admission policy manager enabled, no config path specified": {
			opt: newTestOptions(func(option *Options) {
				option.WebhookAndAdmissionPolicyOpts.EnableAdmissionPolicyManager = true
			}),
			want: field.ErrorList{},
		},
		"admission policy manager enabled, config file does not exist": {
			opt: newTestOptions(func(option *Options) {
				option.WebhookAndAdmissionPolicyOpts.EnableAdmissionPolicyManager = true
				option.WebhookAndAdmissionPolicyOpts.AdmissionPolicyManagerConfig = nonExistentPath
			}),
			want: field.ErrorList{
				field.Invalid(newPath.Child("AdmissionPolicyManagerConfig"), nonExistentPath, "failed to read the admission policy manager config file: "+readErr.Error()),
			},
		},
		"admission policy manager enabled, config file contains invalid YAML": {
			opt: newTestOptions(func(option *Options) {
				option.WebhookAndAdmissionPolicyOpts.EnableAdmissionPolicyManager = true
				option.WebhookAndAdmissionPolicyOpts.AdmissionPolicyManagerConfig = invalidYAMLPath
			}),
			want: field.ErrorList{
				field.Invalid(newPath.Child("AdmissionPolicyManagerConfig"), invalidYAMLPath, "failed to unmarshal the admission policy manager config file: "+yamlErr.Error()),
			},
		},
		"admission policy manager enabled, config file fails validation": {
			opt: newTestOptions(func(option *Options) {
				option.WebhookAndAdmissionPolicyOpts.EnableAdmissionPolicyManager = true
				option.WebhookAndAdmissionPolicyOpts.AdmissionPolicyManagerConfig = invalidConfigPath
			}),
			want: field.ErrorList{
				field.Invalid(newPath.Child("AdmissionPolicyManagerConfig"), invalidConfigPath, "invalid admission policy manager config: "+configValidateErr.Error()),
			},
		},
		"admission policy manager enabled, valid config file": {
			opt: newTestOptions(func(option *Options) {
				option.WebhookAndAdmissionPolicyOpts.EnableAdmissionPolicyManager = true
				option.WebhookAndAdmissionPolicyOpts.AdmissionPolicyManagerConfig = validConfigPath
			}),
			want: field.ErrorList{},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got := tc.opt.Validate()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Validate() errs mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestAddFlags(t *testing.T) {
	g := gomega.NewWithT(t)
	opts := NewOptions()

	flags := flag.NewFlagSet("deny-modify-member-cluster-labels", flag.ExitOnError)
	opts.AddFlags(flags)

	g.Expect(opts.WebhookAndAdmissionPolicyOpts.GuardRailDenyModifyMemberClusterLabels).To(gomega.BeFalse(), "deny-modify-member-cluster-labels should be false by default")
}
