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

package placementpolicy

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	hubmetrics "github.com/kubefleet-dev/kubefleet/pkg/metrics/hub"
)

// reportPolicyMetrics publishes the scheduling status and the outstanding claim count for a
// policy. The namespace label is empty for cluster-scoped policies, which is how the two kinds
// are told apart in the metric.
func reportPolicyMetrics(policy kfplacementv1alpha1.PlacementPolicyAccessor, status *kfplacementv1alpha1.PlacementPolicyStatus, scheduledCond metav1.Condition) {
	namespace, name := policy.GetNamespace(), policy.GetName()

	hubmetrics.FleetPlacementPolicyStatusLastTimestampSeconds.
		WithLabelValues(
			namespace,
			name,
			strconv.FormatInt(policy.GetGeneration(), 10),
			scheduledCond.Type,
			string(scheduledCond.Status),
			scheduledCond.Reason,
		).SetToCurrentTime()

	if status.ActiveClusterClaims != nil {
		hubmetrics.FleetPlacementPolicyActiveClusterClaims.
			WithLabelValues(namespace, name).
			Set(float64(*status.ActiveClusterClaims))
	}
}

// forgetPolicyMetrics drops the metric series of a policy that has been deleted, so that gauges
// do not keep reporting for objects that no longer exist. It is keyed by namespace and name
// rather than by object, so that it can run from the reconcile that observes the deletion,
// whether or not the policy ever held a cleanup finalizer.
func forgetPolicyMetrics(namespace, name string) {
	hubmetrics.FleetPlacementPolicyStatusLastTimestampSeconds.DeletePartialMatch(prometheus.Labels{
		"namespace": namespace,
		"name":      name,
	})
	hubmetrics.FleetPlacementPolicyActiveClusterClaims.DeletePartialMatch(prometheus.Labels{
		"namespace": namespace,
		"name":      name,
	})
}
