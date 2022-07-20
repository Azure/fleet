/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package options

import (
	"k8s.io/apimachinery/pkg/util/validation/field"

	"go.goms.io/fleet/pkg/utils"
)

// TODO: Clean up the validations we don't need and add the ones we need

// Validate checks Options and return a slice of found errs.
func (o *Options) Validate() field.ErrorList {
	errs := field.ErrorList{}
	newPath := field.NewPath("Options")

	disabledResourceConfig := utils.NewDisabledResourceConfig()
	if err := disabledResourceConfig.Parse(o.SkippedPropagatingAPIs); err != nil {
		errs = append(errs, field.Invalid(newPath.Child("SkippedPropagatingAPIs"), o.SkippedPropagatingAPIs, "Invalid API string"))
	}
	if o.SecurePort < 0 || o.SecurePort > 65535 {
		errs = append(errs, field.Invalid(newPath.Child("SecurePort"), o.SecurePort, "must be between 0 and 65535 inclusive"))
	}
	if o.ClusterDegradedGracePeriod.Duration <= 0 {
		errs = append(errs, field.Invalid(newPath.Child("ClusterDegradedGracePeriod"), o.ClusterDegradedGracePeriod, "must be greater than 0"))
	}

	return errs
}
