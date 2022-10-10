/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package options

import (
	"strings"

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
	if o.ClusterUnhealthyThreshold.Duration <= 0 {
		errs = append(errs, field.Invalid(newPath.Child("ClusterUnhealthyThreshold"), o.ClusterUnhealthyThreshold, "must be greater than 0"))
	}
	if o.WorkPendingGracePeriod.Duration <= 0 {
		errs = append(errs, field.Invalid(newPath.Child("WorkPendingGracePeriod"), o.WorkPendingGracePeriod, "must be greater than 0"))
	}
	connectionType := strings.ToLower(o.WebhookClientConnectionType)
	if !(connectionType == "url" || connectionType == "service") {
		errs = append(errs, field.Invalid(newPath.Child("WebhookClientConnectionType"), o.WebhookClientConnectionType, "must be \"service\" or \"url\""))
	}

	return errs
}
