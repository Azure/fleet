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
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

// TODO: Clean up the validations we don't need and add the ones we need

// Validate checks Options and return a slice of found errs.
func (o *Options) Validate() field.ErrorList {
	errs := field.ErrorList{}
	newPath := field.NewPath("Options")

	if o.AllowedPropagatingAPIs != "" && o.SkippedPropagatingAPIs != "" {
		errs = append(errs, field.Invalid(newPath.Child("AllowedPropagatingAPIs"), o.AllowedPropagatingAPIs, "AllowedPropagatingAPIs and SkippedPropagatingAPIs are mutually exclusive"))
	}

	resourceConfig := utils.NewResourceConfig(o.AllowedPropagatingAPIs != "")
	if err := resourceConfig.Parse(o.SkippedPropagatingAPIs); err != nil {
		errs = append(errs, field.Invalid(newPath.Child("SkippedPropagatingAPIs"), o.SkippedPropagatingAPIs, "Invalid API string"))
	}
	if err := resourceConfig.Parse(o.AllowedPropagatingAPIs); err != nil {
		errs = append(errs, field.Invalid(newPath.Child("AllowedPropagatingAPIs"), o.AllowedPropagatingAPIs, "Invalid API string"))
	}

	if o.ClusterUnhealthyThreshold.Duration <= 0 {
		errs = append(errs, field.Invalid(newPath.Child("ClusterUnhealthyThreshold"), o.ClusterUnhealthyThreshold, "Must be greater than 0"))
	}
	if o.WorkPendingGracePeriod.Duration <= 0 {
		errs = append(errs, field.Invalid(newPath.Child("WorkPendingGracePeriod"), o.WorkPendingGracePeriod, "Must be greater than 0"))
	}

	if o.EnableWebhook && o.WebhookServiceName == "" {
		errs = append(errs, field.Invalid(newPath.Child("WebhookServiceName"), o.WebhookServiceName, "Webhook service name is required when webhook is enabled"))
	}

	if o.UseCertManager && !o.EnableWorkload {
		errs = append(errs, field.Invalid(newPath.Child("UseCertManager"), o.UseCertManager, "UseCertManager requires EnableWorkload to be true (when EnableWorkload is false, a validating webhook blocks pod creation except for certain system pods; cert-manager controller pods must be allowed to run in the hub cluster)"))
	}

	connectionType := o.WebhookClientConnectionType
	if _, err := parseWebhookClientConnectionString(connectionType); err != nil {
		errs = append(errs, field.Invalid(newPath.Child("WebhookClientConnectionType"), o.WebhookClientConnectionType, err.Error()))
	}

	if !o.EnableV1Alpha1APIs && !o.EnableV1Beta1APIs {
		errs = append(errs, field.Required(newPath.Child("EnableV1Alpha1APIs"), "Either EnableV1Alpha1APIs or EnableV1Beta1APIs is required"))
	}

	return errs
}
