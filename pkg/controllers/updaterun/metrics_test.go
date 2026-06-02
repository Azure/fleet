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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.goms.io/fleet/apis/placement/v1beta1"
	hubmetrics "go.goms.io/fleet/pkg/metrics/hub"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

func TestDetermineFailureType(t *testing.T) {
	tests := []struct {
		name string
		cond *metav1.Condition
		want hubmetrics.UpdateRunFailureType
	}{
		{
			name: "nil condition",
			cond: nil,
			want: hubmetrics.UpdateRunFailureTypeNone,
		},
		{
			name: "succeeded condition status is true",
			cond: &metav1.Condition{
				Type:    string(v1beta1.StagedUpdateRunConditionSucceeded),
				Status:  metav1.ConditionTrue,
				Reason:  condition.UpdateRunSucceededReason,
				Message: "update run succeeded",
			},
			want: hubmetrics.UpdateRunFailureTypeNone,
		},
		{
			name: "progressing condition is false with reason stuck - internal error",
			cond: &metav1.Condition{
				Type:    string(v1beta1.StagedUpdateRunConditionProgressing),
				Status:  metav1.ConditionFalse,
				Reason:  condition.UpdateRunStuckReason,
				Message: "updateRun is stuck waiting for 1 cluster(s) in stage stage1 to finish updating",
			},
			want: hubmetrics.UpdateRunFailureTypeInternalError,
		},
		{
			name: "progressing condition is false but reason is waiting - not a failure",
			cond: &metav1.Condition{
				Type:    string(v1beta1.StagedUpdateRunConditionProgressing),
				Status:  metav1.ConditionFalse,
				Reason:  condition.UpdateRunWaitingReason,
				Message: "waiting for approval",
			},
			want: hubmetrics.UpdateRunFailureTypeNone,
		},
		{
			name: "progressing condition is unknown but reason is stopping - not a failure",
			cond: &metav1.Condition{
				Type:    string(v1beta1.StagedUpdateRunConditionProgressing),
				Status:  metav1.ConditionUnknown,
				Reason:  condition.UpdateRunStoppingReason,
				Message: "stopping the update run",
			},
			want: hubmetrics.UpdateRunFailureTypeNone,
		},
		{
			name: "progressing condition is false but reason is stopped - not a failure",
			cond: &metav1.Condition{
				Type:    string(v1beta1.StagedUpdateRunConditionProgressing),
				Status:  metav1.ConditionFalse,
				Reason:  condition.UpdateRunStoppedReason,
				Message: "update run has been stopped",
			},
			want: hubmetrics.UpdateRunFailureTypeNone,
		},
		{
			name: "succeeded condition is false with UpdateRunFailed reason and user error in message",
			cond: &metav1.Condition{
				Type:    string(v1beta1.StagedUpdateRunConditionSucceeded),
				Status:  metav1.ConditionFalse,
				Reason:  condition.UpdateRunFailedReason,
				Message: controller.NewUserError(fmt.Errorf("invalid CRP selector")).Error(),
			},
			want: hubmetrics.UpdateRunFailureTypeUserError,
		},
		{
			name: "succeeded condition is false with UpdateRunFailed reason but no user error in message",
			cond: &metav1.Condition{
				Type:    string(v1beta1.StagedUpdateRunConditionSucceeded),
				Status:  metav1.ConditionFalse,
				Reason:  condition.UpdateRunFailedReason,
				Message: "cannot continue the updateRun: some internal error",
			},
			want: hubmetrics.UpdateRunFailureTypeInternalError,
		},
		{
			name: "progressing condition is false with unexpected error in message",
			cond: &metav1.Condition{
				Type:    string(v1beta1.StagedUpdateRunConditionProgressing),
				Status:  metav1.ConditionFalse,
				Reason:  condition.UpdateRunFailedReason,
				Message: controller.NewUnexpectedBehaviorError(fmt.Errorf("found unsupported task type in before stage tasks: %s", v1beta1.StageTaskTypeTimedWait)).Error(),
			},
			want: hubmetrics.UpdateRunFailureTypeInternalError,
		},
		{
			name: "initialized condition is false with UpdateRunInitializeFailed reason but no user error in message",
			cond: &metav1.Condition{
				Type:    string(v1beta1.StagedUpdateRunConditionInitialized),
				Status:  metav1.ConditionFalse,
				Reason:  condition.UpdateRunInitializeFailedReason,
				Message: "failed to initialize: internal error",
			},
			want: hubmetrics.UpdateRunFailureTypeInternalError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := determineFailureType(tc.cond)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("determineFailureType() = %v, want %v, diff (-want +got):\n%s", got, tc.want, diff)
			}
		})
	}
}

func TestIsFailureReason(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   bool
	}{
		{
			name:   "UpdateRunFailedReason is a failure",
			reason: condition.UpdateRunFailedReason,
			want:   true,
		},
		{
			name:   "UpdateRunInitializeFailedReason is a failure",
			reason: condition.UpdateRunInitializeFailedReason,
			want:   true,
		},
		{
			name:   "UpdateRunStuckReason is not a terminal failure",
			reason: condition.UpdateRunStuckReason,
			want:   false,
		},
		{
			name:   "UpdateRunWaitingReason is not a failure",
			reason: condition.UpdateRunWaitingReason,
			want:   false,
		},
		{
			name:   "UpdateRunStoppingReason is not a failure",
			reason: condition.UpdateRunStoppingReason,
			want:   false,
		},
		{
			name:   "UpdateRunStoppedReason is not a failure",
			reason: condition.UpdateRunStoppedReason,
			want:   false,
		},
		{
			name:   "UpdateRunSucceededReason is not a failure",
			reason: condition.UpdateRunSucceededReason,
			want:   false,
		},
		{
			name:   "UpdateRunProgressingReason is not a failure",
			reason: condition.UpdateRunProgressingReason,
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isFailureReason(tc.reason)
			if got != tc.want {
				t.Errorf("isFailureReason(%q) = %v, want %v", tc.reason, got, tc.want)
			}
		})
	}
}
