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
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// a callback function to modify options
type ModifyOptions func(option *Options)

// newTestOptions creates an Options with default parameters.
func newTestOptions(modifyOptions ModifyOptions) Options {
	option := Options{
		CtrlManagerOptions: CtrlManagerOptions{
			HubManagerOpts: PerClusterCtrlManagerOptions{
				QPS:   50,
				Burst: 500,
			},
			MemberManagerOpts: PerClusterCtrlManagerOptions{
				QPS:   250,
				Burst: 1000,
			},
		},
		ApplierOpts: ApplierOptions{
			RequeueRateLimiterInitialSlowBackoffDelaySeconds: 2,
			RequeueRateLimiterMaxSlowBackoffDelaySeconds:     15,
			RequeueRateLimiterMaxFastBackoffDelaySeconds:     900,
			RequeueRateLimiterExponentialBaseForSlowBackoff:  1.2,
			RequeueRateLimiterExponentialBaseForFastBackoff:  1.5,
		},
	}

	if modifyOptions != nil {
		modifyOptions(&option)
	}
	return option
}

func TestValidate(t *testing.T) {
	newPath := field.NewPath("Options")
	testCases := map[string]struct {
		opt  Options
		want field.ErrorList
	}{
		"valid options": {
			opt:  newTestOptions(nil),
			want: field.ErrorList{},
		},
		"hub burst less than hub QPS": {
			opt: newTestOptions(func(option *Options) {
				option.CtrlManagerOptions.HubManagerOpts.QPS = 200
				option.CtrlManagerOptions.HubManagerOpts.Burst = 100
			}),
			want: field.ErrorList{
				field.Invalid(newPath.Child("HubManagerOpts").Child("Burst"), 100, "The burst limit for hub cluster client-side throttling must be greater than or equal to its QPS limit"),
			},
		},
		"member burst less than member QPS": {
			opt: newTestOptions(func(option *Options) {
				option.CtrlManagerOptions.MemberManagerOpts.QPS = 500
				option.CtrlManagerOptions.MemberManagerOpts.Burst = 200
			}),
			want: field.ErrorList{
				field.Invalid(newPath.Child("MemberManagerOpts").Child("Burst"), 200, "The burst limit for member cluster client-side throttling must be greater than or equal to its QPS limit"),
			},
		},
		"slow backoff initial delay exceeds slow backoff max delay": {
			opt: newTestOptions(func(option *Options) {
				option.ApplierOpts.RequeueRateLimiterInitialSlowBackoffDelaySeconds = 30
				option.ApplierOpts.RequeueRateLimiterMaxSlowBackoffDelaySeconds = 15
			}),
			want: field.ErrorList{
				field.Invalid(newPath.Child("ApplierOpts").Child("RequeueRateLimiterInitialSlowBackoffDelaySeconds"), 30, "The initial delay for the slow backoff stage must not exceed the maximum delay for the slow backoff stage"),
			},
		},
		"slow backoff max delay exceeds fast backoff max delay": {
			opt: newTestOptions(func(option *Options) {
				option.ApplierOpts.RequeueRateLimiterMaxSlowBackoffDelaySeconds = 1000
				option.ApplierOpts.RequeueRateLimiterMaxFastBackoffDelaySeconds = 900
			}),
			want: field.ErrorList{
				field.Invalid(newPath.Child("ApplierOpts").Child("RequeueRateLimiterMaxSlowBackoffDelaySeconds"), 1000, "The maximum delay for the slow backoff stage must not exceed the maximum delay for the fast backoff stage"),
			},
		},
		"fast backoff exponential base less than slow backoff exponential base": {
			opt: newTestOptions(func(option *Options) {
				option.ApplierOpts.RequeueRateLimiterExponentialBaseForSlowBackoff = 2.0
				option.ApplierOpts.RequeueRateLimiterExponentialBaseForFastBackoff = 1.5
			}),
			want: field.ErrorList{
				field.Invalid(newPath.Child("ApplierOpts").Child("RequeueRateLimiterExponentialBaseForFastBackoff"), 1.5, "The exponential base for the fast backoff stage must be greater than or equal to the exponential base for the slow backoff stage"),
			},
		},
		"multiple simultaneous violations": {
			opt: newTestOptions(func(option *Options) {
				option.CtrlManagerOptions.HubManagerOpts.QPS = 200
				option.CtrlManagerOptions.HubManagerOpts.Burst = 100
				option.CtrlManagerOptions.MemberManagerOpts.QPS = 500
				option.CtrlManagerOptions.MemberManagerOpts.Burst = 200
				option.ApplierOpts.RequeueRateLimiterInitialSlowBackoffDelaySeconds = 30
				option.ApplierOpts.RequeueRateLimiterMaxSlowBackoffDelaySeconds = 15
			}),
			want: field.ErrorList{
				field.Invalid(newPath.Child("HubManagerOpts").Child("Burst"), 100, "The burst limit for hub cluster client-side throttling must be greater than or equal to its QPS limit"),
				field.Invalid(newPath.Child("MemberManagerOpts").Child("Burst"), 200, "The burst limit for member cluster client-side throttling must be greater than or equal to its QPS limit"),
				field.Invalid(newPath.Child("ApplierOpts").Child("RequeueRateLimiterInitialSlowBackoffDelaySeconds"), 30, "The initial delay for the slow backoff stage must not exceed the maximum delay for the slow backoff stage"),
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got := tc.opt.Validate()
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("Validate() errs mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}
