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

package updaterun

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	hubmetrics "github.com/kubefleet-dev/kubefleet/pkg/metrics/hub"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// deleteUpdateRunMetrics deletes the metrics related to the update run when the update run is deleted.
func deleteUpdateRunMetrics(updateRun placementv1beta1.UpdateRunObj) {
	hubmetrics.FleetUpdateRunStatusLastTimestampSeconds.DeletePartialMatch(prometheus.Labels{"namespace": updateRun.GetNamespace(), "name": updateRun.GetName()})
	hubmetrics.FleetUpdateRunStageClusterUpdatingDurationSeconds.DeletePartialMatch(prometheus.Labels{"namespace": updateRun.GetNamespace(), "name": updateRun.GetName()})
	hubmetrics.FleetUpdateRunApprovalRequestLatencySeconds.DeletePartialMatch(prometheus.Labels{"namespace": updateRun.GetNamespace(), "name": updateRun.GetName()})
}

// determineFailureType determines the type of failure based on the condition.
// It returns:
//   - "none" for successful, in-progress, waiting, stopping (unknown status), or stopped conditions
//   - "user_error" for known customer configuration errors (when the condition message
//     contains the user error marker)
//   - "internal_error" for terminal failure conditions or stuck conditions that require investigation
//
// The failure type is derived from the condition message to ensure consistency across
// reconciliations. This handles subsequent reconciliations where the original error
// is not available but the failure type needs to be preserved in the condition message.
//
// Note: A "stuck" update run is classified as "internal_error" because it may never resolve
// and effectively represents a failure state that needs investigation.
func determineFailureType(cond *metav1.Condition) hubmetrics.UpdateRunFailureType {
	if cond == nil || cond.Status != metav1.ConditionFalse {
		return hubmetrics.UpdateRunFailureTypeNone
	}

	// Stuck is always classified as internal error
	if cond.Reason == condition.UpdateRunStuckReason {
		return hubmetrics.UpdateRunFailureTypeInternalError
	}

	// Check if it's a terminal failure condition
	if isFailureReason(cond.Reason) {
		if strings.Contains(cond.Message, controller.ErrUserError.Error()) {
			return hubmetrics.UpdateRunFailureTypeUserError
		}
		return hubmetrics.UpdateRunFailureTypeInternalError
	}

	return hubmetrics.UpdateRunFailureTypeNone
}

// isFailureReason returns true if the condition reason indicates a terminal failure state
// for an UpdateRun. Non-failure reasons like stuck, waiting, stopping, or stopped are
// not considered terminal failures.
func isFailureReason(reason string) bool {
	return reason == condition.UpdateRunFailedReason ||
		reason == condition.UpdateRunInitializeFailedReason
}

// emitUpdateRunStatusMetric emits the update run status metric based on status conditions in the updateRun.
// The failure type is derived from the condition message, not from the reconcile error.
func emitUpdateRunStatusMetric(updateRun placementv1beta1.UpdateRunObj) {
	generation := updateRun.GetGeneration()
	state := updateRun.GetUpdateRunSpec().State

	updateRunStatus := updateRun.GetUpdateRunStatus()
	succeedCond := meta.FindStatusCondition(updateRunStatus.Conditions, string(placementv1beta1.StagedUpdateRunConditionSucceeded))
	if succeedCond != nil && succeedCond.ObservedGeneration == generation {
		failureType := determineFailureType(succeedCond)
		hubmetrics.FleetUpdateRunStatusLastTimestampSeconds.WithLabelValues(updateRun.GetNamespace(), updateRun.GetName(), string(state),
			string(placementv1beta1.StagedUpdateRunConditionSucceeded), string(succeedCond.Status), succeedCond.Reason, string(failureType)).SetToCurrentTime()
		return
	}

	progressingCond := meta.FindStatusCondition(updateRunStatus.Conditions, string(placementv1beta1.StagedUpdateRunConditionProgressing))
	if progressingCond != nil && progressingCond.ObservedGeneration == generation {
		failureType := determineFailureType(progressingCond)
		hubmetrics.FleetUpdateRunStatusLastTimestampSeconds.WithLabelValues(updateRun.GetNamespace(), updateRun.GetName(), string(state),
			string(placementv1beta1.StagedUpdateRunConditionProgressing), string(progressingCond.Status), progressingCond.Reason, string(failureType)).SetToCurrentTime()
		return
	}

	initializedCond := meta.FindStatusCondition(updateRunStatus.Conditions, string(placementv1beta1.StagedUpdateRunConditionInitialized))
	if initializedCond != nil && initializedCond.ObservedGeneration == generation {
		failureType := determineFailureType(initializedCond)
		hubmetrics.FleetUpdateRunStatusLastTimestampSeconds.WithLabelValues(updateRun.GetNamespace(), updateRun.GetName(), string(state),
			string(placementv1beta1.StagedUpdateRunConditionInitialized), string(initializedCond.Status), initializedCond.Reason, string(failureType)).SetToCurrentTime()
		return
	}

	// We should rarely reach here, it can only happen when updating updateRun status fails.
	klog.V(2).InfoS("There's no valid status condition on updateRun, status updating failed possibly", "updateRun", klog.KObj(updateRun))
}

// recordApprovalRequestLatency records the time from approval request creation to user approval.
func recordApprovalRequestLatency(
	stageTaskStatus *placementv1beta1.StageTaskStatus,
	updateRun placementv1beta1.UpdateRunObj,
	taskType string,
) {
	approvalCreatedCond := meta.FindStatusCondition(stageTaskStatus.Conditions, string(placementv1beta1.StageTaskConditionApprovalRequestCreated))
	approvalApprovedCond := meta.FindStatusCondition(stageTaskStatus.Conditions, string(placementv1beta1.StageTaskConditionApprovalRequestApproved))

	// Only record latency when both approval request created and approved conditions are true,
	// and their observed generation is the same as the update run generation to ensure the recorded latency is accurate.
	if !condition.IsConditionStatusTrue(approvalCreatedCond, updateRun.GetGeneration()) || !condition.IsConditionStatusTrue(approvalApprovedCond, updateRun.GetGeneration()) {
		return
	}

	latencySeconds := approvalApprovedCond.LastTransitionTime.Sub(approvalCreatedCond.LastTransitionTime.Time).Seconds()
	hubmetrics.FleetUpdateRunApprovalRequestLatencySeconds.WithLabelValues(
		updateRun.GetNamespace(),
		updateRun.GetName(),
		taskType,
	).Observe(latencySeconds)
}

// recordStageClusterUpdatingDuration records the time from stage start to when all clusters finish updating.
func recordStageClusterUpdatingDuration(stageStatus *placementv1beta1.StageUpdatingStatus, updateRun placementv1beta1.UpdateRunObj) {
	if stageStatus.StartTime == nil {
		return
	}
	durationSeconds := time.Since(stageStatus.StartTime.Time).Seconds()
	hubmetrics.FleetUpdateRunStageClusterUpdatingDurationSeconds.WithLabelValues(
		updateRun.GetNamespace(),
		updateRun.GetName(),
	).Observe(durationSeconds)
}
