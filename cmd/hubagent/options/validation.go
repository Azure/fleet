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

	if o.AllowedPropagatingAPIs != "" && o.SkippedPropagatingAPIs != "" {
		errs = append(errs, field.Invalid(newPath.Child("AllowedPropagatingAPIs"), o.AllowedPropagatingAPIs, "AllowedPropagatingAPIs and SkippedPropagatingAPIs are mutually exclusive"))
	}

	disabledResourceConfig := utils.NewResourceConfig(true)
	if err := disabledResourceConfig.Parse(o.SkippedPropagatingAPIs); err != nil {
		errs = append(errs, field.Invalid(newPath.Child("SkippedPropagatingAPIs"), o.SkippedPropagatingAPIs, "Invalid API string"))
	}

	allowedResourceConfig := utils.NewResourceConfig(false)
	if err := allowedResourceConfig.Parse(o.AllowedPropagatingAPIs); err != nil {
		errs = append(errs, field.Invalid(newPath.Child("AllowedPropagatingAPIs"), o.AllowedPropagatingAPIs, "Invalid API string"))
	}

	if o.ClusterUnhealthyThreshold.Duration <= 0 {
		errs = append(errs, field.Invalid(newPath.Child("ClusterUnhealthyThreshold"), o.ClusterUnhealthyThreshold, "Must be greater than 0"))
	}
	if o.WorkPendingGracePeriod.Duration <= 0 {
		errs = append(errs, field.Invalid(newPath.Child("WorkPendingGracePeriod"), o.WorkPendingGracePeriod, "Must be greater than 0"))
	}

	connectionType := o.WebhookClientConnectionType
	if _, err := parseWebhookClientConnectionString(connectionType); err != nil {
		errs = append(errs, field.Invalid(newPath.Child("WebhookClientConnectionType"), o.EnableWebhook, err.Error()))
	}

	if !o.EnableV1Alpha1APIs && !o.EnableV1Beta1APIs {
		errs = append(errs, field.Required(newPath.Child("EnableV1Alpha1APIs"), "Either EnableV1Alpha1APIs or EnableV1Beta1APIs is required"))
	}

	return errs
}
