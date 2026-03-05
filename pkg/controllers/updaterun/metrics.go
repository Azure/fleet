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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/klog/v2"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	hubmetrics "go.goms.io/fleet/pkg/metrics/hub"
	"go.goms.io/fleet/pkg/utils/condition"
)

// deleteUpdateRunMetrics deletes the metrics related to the update run when the update run is deleted.
func deleteUpdateRunMetrics(updateRun placementv1beta1.UpdateRunObj) {
	hubmetrics.FleetUpdateRunStatusLastTimestampSeconds.DeletePartialMatch(prometheus.Labels{"namespace": updateRun.GetNamespace(), "name": updateRun.GetName()})
	hubmetrics.FleetUpdateRunStageClusterUpdatingDurationSeconds.DeletePartialMatch(prometheus.Labels{"namespace": updateRun.GetNamespace(), "name": updateRun.GetName()})
	hubmetrics.FleetUpdateRunApprovalRequestLatencySeconds.DeletePartialMatch(prometheus.Labels{"namespace": updateRun.GetNamespace(), "name": updateRun.GetName()})
}

// emitUpdateRunStatusMetric emits the update run status metric based on status conditions in the updateRun.
func emitUpdateRunStatusMetric(updateRun placementv1beta1.UpdateRunObj) {
	generation := updateRun.GetGeneration()
	state := updateRun.GetUpdateRunSpec().State

	updateRunStatus := updateRun.GetUpdateRunStatus()
	succeedCond := meta.FindStatusCondition(updateRunStatus.Conditions, string(placementv1beta1.StagedUpdateRunConditionSucceeded))
	if succeedCond != nil && succeedCond.ObservedGeneration == generation {
		hubmetrics.FleetUpdateRunStatusLastTimestampSeconds.WithLabelValues(updateRun.GetNamespace(), updateRun.GetName(), string(state),
			string(placementv1beta1.StagedUpdateRunConditionSucceeded), string(succeedCond.Status), succeedCond.Reason).SetToCurrentTime()
		return
	}

	progressingCond := meta.FindStatusCondition(updateRunStatus.Conditions, string(placementv1beta1.StagedUpdateRunConditionProgressing))
	if progressingCond != nil && progressingCond.ObservedGeneration == generation {
		hubmetrics.FleetUpdateRunStatusLastTimestampSeconds.WithLabelValues(updateRun.GetNamespace(), updateRun.GetName(), string(state),
			string(placementv1beta1.StagedUpdateRunConditionProgressing), string(progressingCond.Status), progressingCond.Reason).SetToCurrentTime()
		return
	}

	initializedCond := meta.FindStatusCondition(updateRunStatus.Conditions, string(placementv1beta1.StagedUpdateRunConditionInitialized))
	if initializedCond != nil && initializedCond.ObservedGeneration == generation {
		hubmetrics.FleetUpdateRunStatusLastTimestampSeconds.WithLabelValues(updateRun.GetNamespace(), updateRun.GetName(), string(state),
			string(placementv1beta1.StagedUpdateRunConditionInitialized), string(initializedCond.Status), initializedCond.Reason).SetToCurrentTime()
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
