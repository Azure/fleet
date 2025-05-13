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

package workapplier

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/metrics"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// Note (chenyu1):
// the metrics for the work applier are added the assumptions that:
// a) the applier should provide low-cardinality metrics only to keep the memory footprint low,
//    as there might be a high number (up to 10K) of works/manifests to process in a member cluster; and
// b) keep the overhead of metrics collection low, esp. considering that the applier runs in
//    parallel (5 concurrent reconciliations by default) and is on the critical path of Fleet workflows.
//
// TO-DO (chenyu1): evaluate if there is any need for high-cardinality options and/or stateful
// metric collection for better observability.

// Some of the label values for the work/manifest processing request counter metric.
const (
	workOrManifestStatusSkipped = "Skipped"
	workOrManifestStatusUnknown = "Unknown"

	manifestDriftOrDiffDetectionStatusFound    = "Found"
	manifestDriftOrDiffDetectionStatusNotFound = "NotFound"
)

func trackWorkAndManifestProcessingRequestMetrics(work *fleetv1beta1.Work) {
	// Increment the work processing request counter.

	var workApplyStatus string
	var shouldSkip bool
	workAppliedCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
	switch {
	case workAppliedCond == nil:
		workApplyStatus = workOrManifestStatusSkipped
	case workAppliedCond.ObservedGeneration != work.Generation:
		// Normally this should never occur; as the method is called right after the status
		// is refreshed.
		_ = controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to track work metrics: applied condition is stale"))
		shouldSkip = true
	case workAppliedCond.Status == metav1.ConditionUnknown:
		// At this point Fleet does not set the Applied condition to the Unknown status; this
		// branch is added just for completeness reasons.
		workApplyStatus = workOrManifestStatusUnknown
	case len(workAppliedCond.Reason) == 0:
		// Normally this should never occur; this branch is added just for completeness reasons.
		_ = controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to track work metrics: applied condition has no reason (work=%v)", klog.KObj(work)))
		shouldSkip = true
	default:
		workApplyStatus = workAppliedCond.Reason
	}

	var workAvailabilityStatus string
	workAvailableCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
	switch {
	case workAvailableCond == nil:
		workAvailabilityStatus = workOrManifestStatusSkipped
	case workAvailableCond.ObservedGeneration != work.Generation:
		// Normally this should never occur; as the method is called right after the status
		// is refreshed.
		_ = controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to track work metrics: available condition is stale"))
		shouldSkip = true
	case workAvailableCond.Status == metav1.ConditionUnknown:
		// At this point Fleet does not set the Available condition to the Unknown status; this
		// branch is added just for completeness reasons.
		workAvailabilityStatus = workOrManifestStatusUnknown
	case len(workAvailableCond.Reason) == 0:
		// Normally this should never occur; this branch is added just for completeness reasons.
		_ = controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to track work metrics: available condition has no reason (work=%v)", klog.KObj(work)))
	default:
		workAvailabilityStatus = workAvailableCond.Reason
	}

	var workDiffReportedStatus string
	workDiffReportedCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeDiffReported)
	switch {
	case workDiffReportedCond == nil:
		workDiffReportedStatus = workOrManifestStatusSkipped
	case workDiffReportedCond.ObservedGeneration != work.Generation:
		// Normally this should never occur; as the method is called right after the status
		// is refreshed.
		_ = controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to track work metrics: diff reported condition is stale"))
		shouldSkip = true
	case workDiffReportedCond.Status == metav1.ConditionUnknown:
		// At this point Fleet does not set the DiffReported condition to the Unknown status; this
		// branch is added just for completeness reasons.
		workDiffReportedStatus = workOrManifestStatusUnknown
	case len(workDiffReportedCond.Reason) == 0:
		// Normally this should never occur; this branch is added just for completeness reasons.
		_ = controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to track work metrics: diff reported condition has no reason (work=%v)", klog.KObj(work)))
	default:
		workDiffReportedStatus = workDiffReportedCond.Reason
	}

	// Do a sanity check; if any of the work conditions is stale; do not emit any metric data point.
	if shouldSkip {
		// An error has been logged earlier.
		return
	}

	metrics.FleetWorkProcessingRequestsTotal.WithLabelValues(
		workApplyStatus,
		workAvailabilityStatus,
		workDiffReportedStatus,
	).Inc()

	// Increment the manifest processing request counter.
	for idx := range work.Status.ManifestConditions {
		manifestCond := &work.Status.ManifestConditions[idx]

		// No generation checks are added here, as the observed generation reported
		// on manifest conditions is the generation of the corresponding object in the
		// member cluster, not the generation of the work object. Plus, as this
		// method is called right after the status is refreshed, all conditions are
		// expected to be up-to-date.

		var manifestApplyStatus string
		manifestAppliedCond := meta.FindStatusCondition(manifestCond.Conditions, fleetv1beta1.WorkConditionTypeApplied)
		switch {
		case manifestAppliedCond == nil:
			manifestApplyStatus = workOrManifestStatusSkipped
		case manifestAppliedCond.Status == metav1.ConditionUnknown:
			// At this point Fleet does not set the Applied condition to the Unknown status; this
			// branch is added just for completeness reasons.
			manifestApplyStatus = workOrManifestStatusUnknown
		case len(manifestAppliedCond.Reason) == 0:
			// Normally this should never occur; this branch is added just for completeness reasons.
			_ = controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to track manifest metrics: applied condition has no reason (manifest=%v, work=%v)", manifestCond.Identifier, klog.KObj(work)))
			continue
		default:
			manifestApplyStatus = manifestAppliedCond.Reason
		}

		var manifestAvailabilityStatus string
		manifestAvailableCond := meta.FindStatusCondition(manifestCond.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
		switch {
		case manifestAvailableCond == nil:
			manifestAvailabilityStatus = workOrManifestStatusSkipped
		case manifestAvailableCond.Status == metav1.ConditionUnknown:
			// At this point Fleet does not set the Available condition to the Unknown status; this
			// branch is added just for completeness reasons.
			manifestAvailabilityStatus = workOrManifestStatusUnknown
		case len(manifestAvailableCond.Reason) == 0:
			// Normally this should never occur; this branch is added just for completeness reasons.
			_ = controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to track manifest metrics: available condition has no reason (manifest=%v, work=%v)", manifestCond.Identifier, klog.KObj(work)))
			continue
		default:
			manifestAvailabilityStatus = manifestAvailableCond.Reason
		}

		var manifestDiffReportedStatus string
		manifestDiffReportedCond := meta.FindStatusCondition(manifestCond.Conditions, fleetv1beta1.WorkConditionTypeDiffReported)
		switch {
		case manifestDiffReportedCond == nil:
			manifestDiffReportedStatus = workOrManifestStatusSkipped
		case manifestDiffReportedCond.Status == metav1.ConditionUnknown:
			// At this point Fleet does not set the DiffReported condition to the Unknown status; this
			// branch is added just for completeness reasons.
			manifestDiffReportedStatus = workOrManifestStatusUnknown
		case len(manifestDiffReportedCond.Reason) == 0:
			// Normally this should never occur; this branch is added just for completeness reasons.
			_ = controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to track manifest metrics: diff reported condition has no reason (manifest=%v, work=%v)", manifestCond.Identifier, klog.KObj(work)))
			continue
		default:
			manifestDiffReportedStatus = manifestDiffReportedCond.Reason
		}

		manifestDriftDetectionStatus := manifestDriftOrDiffDetectionStatusNotFound
		if manifestCond.DriftDetails != nil {
			manifestDriftDetectionStatus = manifestDriftOrDiffDetectionStatusFound
		}

		manifestDiffDetectionStatus := manifestDriftOrDiffDetectionStatusNotFound
		if manifestCond.DiffDetails != nil {
			manifestDiffDetectionStatus = manifestDriftOrDiffDetectionStatusFound
		}

		metrics.FleetManifestProcessingRequestsTotal.WithLabelValues(
			manifestApplyStatus,
			manifestAvailabilityStatus,
			manifestDiffReportedStatus,
			manifestDriftDetectionStatus,
			manifestDiffDetectionStatus,
		).Inc()
	}
}
