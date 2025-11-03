package updaterun

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func TestValidateAfterStageTask(t *testing.T) {
	tests := []struct {
		name    string
		task    []v1beta1.StageTask
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid AfterTasks",
			task: []v1beta1.StageTask{
				{
					Type: v1beta1.StageTaskTypeApproval,
				},
				{
					Type:     v1beta1.StageTaskTypeTimedWait,
					WaitTime: ptr.To(metav1.Duration{Duration: 5 * time.Minute}),
				},
			},
			wantErr: false,
		},
		{
			name: "invalid AfterTasks, same type of tasks",
			task: []v1beta1.StageTask{
				{
					Type:     v1beta1.StageTaskTypeTimedWait,
					WaitTime: ptr.To(metav1.Duration{Duration: 1 * time.Minute}),
				},
				{
					Type:     v1beta1.StageTaskTypeTimedWait,
					WaitTime: ptr.To(metav1.Duration{Duration: 5 * time.Minute}),
				},
			},
			wantErr: true,
			errMsg:  "afterStageTasks cannot have two tasks of the same type: TimedWait",
		},
		{
			name: "invalid AfterTasks, with nil duration for TimedWait",
			task: []v1beta1.StageTask{
				{
					Type: v1beta1.StageTaskTypeTimedWait,
				},
			},
			wantErr: true,
			errMsg:  "task 0 of type TimedWait has wait duration set to nil",
		},
		{
			name: "invalid AfterTasks, with zero duration for TimedWait",
			task: []v1beta1.StageTask{
				{
					Type:     v1beta1.StageTaskTypeTimedWait,
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
