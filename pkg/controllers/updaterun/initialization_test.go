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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func TestValidateBeforeStageTask(t *testing.T) {
	tests := []struct {
		name       string
		task       []placementv1beta1.StageTask
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "valid BeforeTasks",
			task: []placementv1beta1.StageTask{
				{
					Type: placementv1beta1.StageTaskTypeApproval,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid BeforeTasks, greater than 1 task",
			task: []placementv1beta1.StageTask{
				{
					Type: placementv1beta1.StageTaskTypeApproval,
				},
				{
					Type: placementv1beta1.StageTaskTypeApproval,
				},
			},
			wantErr:    true,
			wantErrMsg: "beforeStageTasks can have at most one task",
		},
		{
			name: "invalid BeforeTasks, with invalid task type",
			task: []placementv1beta1.StageTask{
				{
					Type:     placementv1beta1.StageTaskTypeTimedWait,
					WaitTime: ptr.To(metav1.Duration{Duration: 5 * time.Minute}),
				},
			},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("task %d of type %s is not allowed in beforeStageTasks, allowed type: Approval", 0, placementv1beta1.StageTaskTypeTimedWait),
		},
		{
			name: "invalid BeforeTasks, with duration for Approval",
			task: []placementv1beta1.StageTask{
				{
					Type:     placementv1beta1.StageTaskTypeApproval,
					WaitTime: ptr.To(metav1.Duration{Duration: 1 * time.Minute}),
				},
			},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("task %d of type Approval cannot have wait duration set", 0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotErr := validateBeforeStageTask(tt.task)
			if tt.wantErr {
				if gotErr == nil || gotErr.Error() != tt.wantErrMsg {
					t.Fatalf("validateBeforeStageTask() error = %v, wantErr %v", gotErr, tt.wantErrMsg)
				}
			} else if gotErr != nil {
				t.Fatalf("validateBeforeStageTask() error = %v, wantErr %v", gotErr, tt.wantErr)
			}
		})
	}
}

func TestValidateAfterStageTask(t *testing.T) {
	tests := []struct {
		name    string
		task    []placementv1beta1.StageTask
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid AfterTasks",
			task: []placementv1beta1.StageTask{
				{
					Type: placementv1beta1.StageTaskTypeApproval,
				},
				{
					Type:     placementv1beta1.StageTaskTypeTimedWait,
					WaitTime: ptr.To(metav1.Duration{Duration: 5 * time.Minute}),
				},
			},
			wantErr: false,
		},
		{
			name: "invalid AfterTasks, same type of tasks",
			task: []placementv1beta1.StageTask{
				{
					Type:     placementv1beta1.StageTaskTypeTimedWait,
					WaitTime: ptr.To(metav1.Duration{Duration: 1 * time.Minute}),
				},
				{
					Type:     placementv1beta1.StageTaskTypeTimedWait,
					WaitTime: ptr.To(metav1.Duration{Duration: 5 * time.Minute}),
				},
			},
			wantErr: true,
			errMsg:  "afterStageTasks cannot have two tasks of the same type: TimedWait",
		},
		{
			name: "invalid AfterTasks, with nil duration for TimedWait",
			task: []placementv1beta1.StageTask{
				{
					Type: placementv1beta1.StageTaskTypeTimedWait,
				},
			},
			wantErr: true,
			errMsg:  "task 0 of type TimedWait has wait duration set to nil",
		},
		{
			name: "invalid AfterTasks, with zero duration for TimedWait",
			task: []placementv1beta1.StageTask{
				{
					Type:     placementv1beta1.StageTaskTypeTimedWait,
					WaitTime: ptr.To(metav1.Duration{Duration: 0 * time.Minute}),
				},
			},
			wantErr: true,
			errMsg:  "task 0 of type TimedWait has wait duration <= 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAfterStageTask(tt.task)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateAfterStageTask() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if err.Error() != tt.errMsg {
					t.Errorf("validateAfterStageTask() error = %v, wantErr %v", err, tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("validateAfterStageTask() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
