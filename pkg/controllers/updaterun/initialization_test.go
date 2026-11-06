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

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	testinformer "go.goms.io/fleet/test/utils/informer"
	"go.goms.io/fleet/test/utils/resource"
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

func TestRecordOverrideSnapshots(t *testing.T) {
	const (
		placementName = "test-placement"
		namespace     = "deployment-namespace"
		clusterName   = "cluster-1"
		roName        = "test-ro-0"
	)
	scheme := runtime.NewScheme()
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	snapshotMeta := func(ns string) metav1.ObjectMeta {
		return metav1.ObjectMeta{
			Name:      fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, placementName, 0),
			Namespace: ns,
			Labels: map[string]string{
				placementv1beta1.ResourceIndexLabel:     "0",
				placementv1beta1.PlacementTrackingLabel: placementName,
			},
			Annotations: map[string]string{
				placementv1beta1.ResourceGroupHashAnnotation:         "abc",
				placementv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
			},
		}
	}
	snapshotSpec := placementv1beta1.ResourceSnapshotSpec{
		SelectedResources: []placementv1beta1.ResourceContent{*resource.DeploymentResourceContentForTest(t)},
	}
	roSnapshot := func(scope placementv1beta1.ResourceScope) *placementv1beta1.ResourceOverrideSnapshot {
		return &placementv1beta1.ResourceOverrideSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: namespace,
				Labels:    map[string]string{placementv1beta1.IsLatestSnapshotLabel: "true"},
			},
			Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
				OverrideSpec: placementv1beta1.ResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{Name: placementName, Scope: scope},
					ResourceSelectors: []placementv1beta1.ResourceSelector{
						{Group: "apps", Version: "v1", Kind: "Deployment", Name: "deployment-name"},
					},
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{ClusterSelector: &placementv1beta1.ClusterSelector{}},
						},
					},
				},
			},
		}
	}
	updateRunStatus := placementv1beta1.UpdateRunStatus{
		StagesStatus: []placementv1beta1.StageUpdatingStatus{
			{StageName: "stage-1", Clusters: []placementv1beta1.ClusterUpdatingStatus{{ClusterName: clusterName}}},
		},
	}
	updateRunSpec := placementv1beta1.UpdateRunSpec{PlacementName: placementName, ResourceSnapshotIndex: "0"}

	tests := []struct {
		name      string
		placement placementv1beta1.PlacementObj
		snapshot  client.Object
		ro        *placementv1beta1.ResourceOverrideSnapshot
		updateRun placementv1beta1.UpdateRunObj
	}{
		{
			name:      "cluster-scoped placement",
			placement: &placementv1beta1.ClusterResourcePlacement{ObjectMeta: metav1.ObjectMeta{Name: placementName}},
			snapshot:  &placementv1beta1.ClusterResourceSnapshot{ObjectMeta: snapshotMeta(""), Spec: snapshotSpec},
			ro:        roSnapshot(placementv1beta1.ClusterScoped),
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{Name: "test-update-run"},
				Spec:       updateRunSpec,
				Status:     *updateRunStatus.DeepCopy(),
			},
		},
		{
			name:      "namespaced placement",
			placement: &placementv1beta1.ResourcePlacement{ObjectMeta: metav1.ObjectMeta{Name: placementName, Namespace: namespace}},
			snapshot:  &placementv1beta1.ResourceSnapshot{ObjectMeta: snapshotMeta(namespace), Spec: snapshotSpec},
			ro:        roSnapshot(placementv1beta1.NamespaceScoped),
			updateRun: &placementv1beta1.StagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{Name: "test-update-run", Namespace: namespace},
				Spec:       updateRunSpec,
				Status:     *updateRunStatus.DeepCopy(),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: clusterName}}
			r := &Reconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.snapshot, tc.ro, cluster).Build(),
				InformerManager: &testinformer.FakeManager{
					APIResources: map[schema.GroupVersionKind]bool{{Group: "apps", Version: "v1", Kind: "Deployment"}: true},
				},
			}
			if err := r.recordOverrideSnapshots(context.Background(), tc.placement, tc.updateRun); err != nil {
				t.Fatalf("recordOverrideSnapshots() = %v, want no error", err)
			}
			got := tc.updateRun.GetUpdateRunStatus().StagesStatus[0].Clusters[0].ResourceOverrideSnapshots
			want := []placementv1beta1.NamespacedName{{Name: roName, Namespace: namespace}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("recordOverrideSnapshots() resourceOverrideSnapshots mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
