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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const (
	testWebhookServiceName = "test-webhook"
)

// a callback function to modify options
type ModifyOptions func(option *Options)

// newTestOptions creates an Options with default parameters.
func newTestOptions(modifyOptions ModifyOptions) Options {
	option := Options{
		SkippedPropagatingAPIs:      "fleet.azure.com;multicluster.x-k8s.io",
		WorkPendingGracePeriod:      metav1.Duration{Duration: 10 * time.Second},
		ClusterUnhealthyThreshold:   metav1.Duration{Duration: 60 * time.Second},
		WebhookClientConnectionType: "url",
		EnableV1Alpha1APIs:          true,
	}

	if modifyOptions != nil {
		modifyOptions(&option)
	}
	return option
}

func TestValidateControllerManagerConfiguration(t *testing.T) {
	newPath := field.NewPath("Options")
	testCases := map[string]struct {
		opt  Options
		want field.ErrorList
	}{
		"valid Options": {
			opt:  newTestOptions(nil),
			want: field.ErrorList{},
		},
		"invalid SkippedPropagatingAPIs": {
			opt: newTestOptions(func(options *Options) {
				options.SkippedPropagatingAPIs = "a/b/c/d?"
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("SkippedPropagatingAPIs"), "a/b/c/d?", "Invalid API string")},
		},
		"invalid ClusterUnhealthyThreshold": {
			opt: newTestOptions(func(options *Options) {
				options.ClusterUnhealthyThreshold.Duration = -40 * time.Second
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("ClusterUnhealthyThreshold"), metav1.Duration{Duration: -40 * time.Second}, "Must be greater than 0")},
		},
		"invalid WorkPendingGracePeriod": {
			opt: newTestOptions(func(options *Options) {
				options.WorkPendingGracePeriod.Duration = -40 * time.Second
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("WorkPendingGracePeriod"), metav1.Duration{Duration: -40 * time.Second}, "Must be greater than 0")},
		},
		"invalid EnableV1Alpha1APIs": {
			opt: newTestOptions(func(option *Options) {
				option.EnableV1Alpha1APIs = false
			}),
			want: field.ErrorList{field.Required(newPath.Child("EnableV1Alpha1APIs"), "Either EnableV1Alpha1APIs or EnableV1Beta1APIs is required")},
		},
		"invalid WebhookClientConnectionType": {
			opt: newTestOptions(func(option *Options) {
				option.WebhookClientConnectionType = "invalid"
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("WebhookClientConnectionType"), "invalid", `must be "service" or "url"`)},
		},
		"WebhookServiceName is empty": {
			opt: newTestOptions(func(option *Options) {
				option.EnableWebhook = true
				option.WebhookServiceName = ""
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("WebhookServiceName"), "", "Webhook service name is required when webhook is enabled")},
		},
		"UseCertManager with EnableWebhook": {
			opt: newTestOptions(func(option *Options) {
				option.EnableWebhook = true
				option.WebhookServiceName = testWebhookServiceName
				option.UseCertManager = true
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("UseCertManager"), true, "UseCertManager requires EnableWorkload to be true (when EnableWorkload is false, a validating webhook blocks pod creation except for certain system pods; cert-manager controller pods must be allowed to run in the hub cluster)")},
		},
		"UseCertManager without EnableWorkload": {
			opt: newTestOptions(func(option *Options) {
				option.EnableWebhook = true
				option.WebhookServiceName = testWebhookServiceName
				option.UseCertManager = true
				option.EnableWorkload = false
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("UseCertManager"), true, "UseCertManager requires EnableWorkload to be true (when EnableWorkload is false, a validating webhook blocks pod creation except for certain system pods; cert-manager controller pods must be allowed to run in the hub cluster)")},
		},
		"UseCertManager with EnableWebhook and EnableWorkload": {
			opt: newTestOptions(func(option *Options) {
				option.EnableWebhook = true
				option.WebhookServiceName = testWebhookServiceName
				option.UseCertManager = true
				option.EnableWorkload = true
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

	g.Expect(opts.DenyModifyMemberClusterLabels).To(gomega.BeFalse(), "deny-modify-member-cluster-labels should be false by default")
}
