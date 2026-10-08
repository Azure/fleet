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
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
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

// TestGenerateStagesByStrategy_InvalidDeleteStageTasks tests that an update run whose strategy has invalid before
// stage tasks in its delete stage fails the validation.
func TestGenerateStagesByStrategy_InvalidDeleteStageTasks(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	strategy := &placementv1beta1.ClusterStagedUpdateStrategy{
		ObjectMeta: metav1.ObjectMeta{Name: "test-strategy"},
		Spec: placementv1beta1.UpdateStrategySpec{
			DeleteStage: &placementv1beta1.DeleteStageConfig{
				BeforeStageTasks: []placementv1beta1.StageTask{{Type: placementv1beta1.StageTaskTypeTimedWait}},
			},
		},
	}
	r := &Reconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(strategy).Build()}
	updateRun := &placementv1beta1.ClusterStagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{Name: "test-update-run", Generation: 1},
		Spec:       placementv1beta1.UpdateRunSpec{StagedUpdateStrategyName: strategy.Name},
	}

	err := r.generateStagesByStrategy(context.Background(), nil, nil, updateRun)
	if !errors.Is(err, errValidationFailed) {
		t.Fatalf("generateStagesByStrategy() error = %v, want %v", err, errValidationFailed)
	}
	wantErrMsg := fmt.Sprintf("the before stage tasks are invalid, updateStrategy: `/test-strategy`, stage: %s, err: task 0 of type TimedWait is not allowed in beforeStageTasks, allowed type: Approval", placementv1beta1.UpdateRunDeleteStageName)
	if !strings.Contains(err.Error(), wantErrMsg) {
		t.Errorf("generateStagesByStrategy() error = %v, want an error containing %q", err, wantErrMsg)
	}
}
