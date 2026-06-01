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
	"os"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/kubefleet-dev/kubefleet/pkg/admissionpolicymanager"
)

// Validate checks Options and return a slice of found errs.
//
// Note: the logic here concerns primarily cross-option validation; for single-option validation,
// consider adding the logic directly as part of the flag parsing function, for clarity reasons.
func (o *Options) Validate() field.ErrorList {
	errs := field.ErrorList{}
	newPath := field.NewPath("Options")

	// Cross-field validation for controller manager options.
	if float64(o.CtrlMgrOpts.HubBurst) < float64(o.CtrlMgrOpts.HubQPS) {
		errs = append(errs, field.Invalid(newPath.Child("HubBurst"), o.CtrlMgrOpts.HubBurst, "The burst limit for client-side throttling must be greater than or equal to its QPS limit"))
	}

	// Cross-field validation for leader election options.
	if float64(o.LeaderElectionOpts.LeaderElectionBurst) < float64(o.LeaderElectionOpts.LeaderElectionQPS) {
		errs = append(errs, field.Invalid(newPath.Child("LeaderElectionBurst"), o.LeaderElectionOpts.LeaderElectionBurst, "The burst limit for client-side throttling of leader election related operations must be greater than or equal to its QPS limit"))
	}

	// Cross-field validation for webhook options.

	// Note: this validation logic is a bit weird in the sense that the system accepts
	// either a URL-based connection or a service-based connection for webhook calls,
	// but here the logic enforces that a service name must be provided. The way we handle
	// URLs is also problematic as the code will always format a service-targeted URL using
	// the input. We keep this logic for now for compatibility reasons.
	if o.WebhookAndAdmissionPolicyOpts.EnableWebhooks && o.WebhookAndAdmissionPolicyOpts.ServiceName == "" {
		errs = append(errs, field.Invalid(newPath.Child("WebhookServiceName"), o.WebhookAndAdmissionPolicyOpts.ServiceName, "A webhook service name is required when webhooks are enabled"))
	}

	if o.WebhookAndAdmissionPolicyOpts.UseCertManager && !o.WebhookAndAdmissionPolicyOpts.EnableWorkload {
		errs = append(errs, field.Invalid(newPath.Child("UseCertManager"), o.WebhookAndAdmissionPolicyOpts.UseCertManager, "If cert manager is used for securing webhook connections, the EnableWorkload option must be set to true, so that cert manager pods can run in the hub cluster."))
	}

	if o.PlacementMgmtOpts.AllowedPropagatingAPIs != "" && o.PlacementMgmtOpts.SkippedPropagatingAPIs != "" {
		errs = append(errs, field.Invalid(newPath.Child("AllowedPropagatingAPIs"), o.PlacementMgmtOpts.AllowedPropagatingAPIs, "AllowedPropagatingAPIs and SkippedPropagatingAPIs options are mutually exclusive"))
	}

	// Cross-field validation for placement management options.
	if o.PlacementMgmtOpts.PlacementControllerWorkQueueRateLimiterOpts.RateLimiterBaseDelay >= o.PlacementMgmtOpts.PlacementControllerWorkQueueRateLimiterOpts.RateLimiterMaxDelay {
		errs = append(errs, field.Invalid(newPath.Child("PlacementControllerWorkQueueRateLimiterOpts").Child("RateLimiterBaseDelay"), o.PlacementMgmtOpts.PlacementControllerWorkQueueRateLimiterOpts.RateLimiterBaseDelay, "the base delay for the placement controller set rate limiter must be less than its max delay"))
	}

	if o.PlacementMgmtOpts.PlacementControllerWorkQueueRateLimiterOpts.RateLimiterQPS > o.PlacementMgmtOpts.PlacementControllerWorkQueueRateLimiterOpts.RateLimiterBucketSize {
		errs = append(errs, field.Invalid(newPath.Child("PlacementControllerWorkQueueRateLimiterOpts").Child("RateLimiterQPS"), o.PlacementMgmtOpts.PlacementControllerWorkQueueRateLimiterOpts.RateLimiterQPS, "the QPS for the placement controller set rate limiter must be less than its bucket size"))
	}

	// Validate admission policy manager setup (if enabled).
	if err := o.validateAdmissionPolicyManagerConfig(newPath); err != nil {
		errs = append(errs, err)
	}

	return errs
}

func (o *Options) validateAdmissionPolicyManagerConfig(newPath *field.Path) *field.Error {
	if o.WebhookAndAdmissionPolicyOpts.EnableAdmissionPolicyManager {
		managerConfigPath := o.WebhookAndAdmissionPolicyOpts.AdmissionPolicyManagerConfig
		if len(managerConfigPath) != 0 {
			// Read the file from the path.
			data, err := os.ReadFile(managerConfigPath)
			if err != nil {
				return field.Invalid(newPath.Child("AdmissionPolicyManagerConfig"), managerConfigPath, "failed to read the admission policy manager config file: "+err.Error())
			}

			policyGeneratorConfigs := &admissionpolicymanager.PolicyGeneratorConfigs{}
			if err := yaml.Unmarshal(data, policyGeneratorConfigs); err != nil {
				return field.Invalid(newPath.Child("AdmissionPolicyManagerConfig"), managerConfigPath, "failed to unmarshal the admission policy manager config file: "+err.Error())
			}

			if err := policyGeneratorConfigs.Validate(); err != nil {
				return field.Invalid(newPath.Child("AdmissionPolicyManagerConfig"), managerConfigPath, "invalid admission policy manager config: "+err.Error())
			}
		}
	}
	return nil
}
