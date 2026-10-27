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

package v1alpha1

const (
	// AutoRolloutEnabledAnnotationKey is the annotation key that, once added to a placement policy, enables
	// auto-rollout for the placement policy. Changes to the resources selected by the placement policy will
	// be rolled out automatically, at most once per minimum wait period (see
	// AutoRolloutMinimumWaitPeriodAnnotationKey).
	//
	// Important: the auto-rollout feature is best used with experimental/staging/testing placements and/or with
	// resources that are generally safe to be rolled out automatically, such as policies, configuration data, and namespaces.
	// Each auto-rollout syncs the state of the selected resources between the hub cluster and all selected
	// member clusters at once, with no gates/checks, and no baking period. For a safer, more controlled rollout
	// experience, consider using KubeFleet's staged update APIs.
	AutoRolloutEnabledAnnotationKey = "kubefleet.dev/auto-rollout-enabled"
	// AutoRolloutEnabledAnnotationValue is the value that should be set for the AutoRolloutEnabledAnnotationKey to
	// enable auto-rollout. If the auto-rollout annotation is set to a different value, auto-rollout will be
	// considered disabled.
	AutoRolloutEnabledAnnotationValue = "true"

	// AutoRolloutMinimumWaitPeriodAnnotationKey is the annotation key that specifies the minimum wait period between
	// consecutive auto-rollouts for the placement policy; it also spaces out the retries of an auto-rollout that failed
	// due to a user error. The value should be a duration string, e.g., "5m" for 5 minutes.
	// The lower and upper bound for this period is 1 minute and 1 hour, respectively. The default is 3 minutes.
	// A value that cannot be parsed is ignored in favor of the default; a value out of the allowed range is clamped
	// to the closest bound.
	AutoRolloutMinimumWaitPeriodAnnotationKey = "kubefleet.dev/auto-rollout-minimum-wait-period"

	// AutoRolloutLastRolloutAttemptTimestampAnnotationKey is the annotation key that stores the timestamp of the last
	// auto-rollout attempt for the placement policy, in the RFC 3339 format. It is updated when an auto-rollout
	// succeeds, or fails due to a user error (e.g., a selected resource that is too large to snapshot), so that such an
	// auto-rollout is retried at most once per minimum wait period; failures that are retried right away (with backoff)
	// do not update it.
	//
	// KubeFleet manages this annotation. A missing or malformed value is treated as if the placement policy has never been
	// rolled out; a value in the future is treated as if the placement policy has just been rolled out.
	AutoRolloutLastRolloutAttemptTimestampAnnotationKey = "kubefleet.dev/auto-rollout-last-rollout-attempt-timestamp"
)
