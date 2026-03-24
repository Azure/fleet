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
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// Validate checks Options and return a slice of found errs.
//
// Note: the logic here concerns primarily cross-option validation; for single-option validation,
// consider adding the logic directly as part of the flag parsing function, for clarity reasons.
func (o *Options) Validate() field.ErrorList {
	errs := field.ErrorList{}
	newPath := field.NewPath("Options")

	// Cross-field validation for controller manager options.
	if float64(o.CtrlManagerOptions.HubManagerOpts.Burst) < o.CtrlManagerOptions.HubManagerOpts.QPS {
		errs = append(errs, field.Invalid(newPath.Child("HubManagerOpts").Child("Burst"), o.CtrlManagerOptions.HubManagerOpts.Burst, "The burst limit for hub cluster client-side throttling must be greater than or equal to its QPS limit"))
	}

	if float64(o.CtrlManagerOptions.MemberManagerOpts.Burst) < o.CtrlManagerOptions.MemberManagerOpts.QPS {
		errs = append(errs, field.Invalid(newPath.Child("MemberManagerOpts").Child("Burst"), o.CtrlManagerOptions.MemberManagerOpts.Burst, "The burst limit for member cluster client-side throttling must be greater than or equal to its QPS limit"))
	}

	// Cross-field validation for applier options.
	if o.ApplierOpts.RequeueRateLimiterInitialSlowBackoffDelaySeconds > o.ApplierOpts.RequeueRateLimiterMaxSlowBackoffDelaySeconds {
		errs = append(errs, field.Invalid(newPath.Child("ApplierOpts").Child("RequeueRateLimiterInitialSlowBackoffDelaySeconds"), o.ApplierOpts.RequeueRateLimiterInitialSlowBackoffDelaySeconds, "The initial delay for the slow backoff stage must not exceed the maximum delay for the slow backoff stage"))
	}

	if o.ApplierOpts.RequeueRateLimiterMaxSlowBackoffDelaySeconds > o.ApplierOpts.RequeueRateLimiterMaxFastBackoffDelaySeconds {
		errs = append(errs, field.Invalid(newPath.Child("ApplierOpts").Child("RequeueRateLimiterMaxSlowBackoffDelaySeconds"), o.ApplierOpts.RequeueRateLimiterMaxSlowBackoffDelaySeconds, "The maximum delay for the slow backoff stage must not exceed the maximum delay for the fast backoff stage"))
	}

	if o.ApplierOpts.RequeueRateLimiterExponentialBaseForFastBackoff < o.ApplierOpts.RequeueRateLimiterExponentialBaseForSlowBackoff {
		errs = append(errs, field.Invalid(newPath.Child("ApplierOpts").Child("RequeueRateLimiterExponentialBaseForFastBackoff"), o.ApplierOpts.RequeueRateLimiterExponentialBaseForFastBackoff, "The exponential base for the fast backoff stage must be greater than or equal to the exponential base for the slow backoff stage"))
	}

	return errs
}
