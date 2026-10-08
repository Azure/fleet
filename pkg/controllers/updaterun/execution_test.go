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
	"github.com/google/go-cmp/cmp/cmpopts"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

func TestIsBindingSyncedWithClusterStatus(t *testing.T) {
	tests := []struct {
		name                 string
		resourceSnapshotName string
		updateRun            *placementv1beta1.ClusterStagedUpdateRun
		binding              *placementv1beta1.ClusterResourceBinding
		cluster              *placementv1beta1.ClusterUpdatingStatus
		wantEqual            bool
	}{
		{
			name:                 "isBindingSyncedWithClusterStatus should return false if binding and updateRun have different resourceSnapshot",
			resourceSnapshotName: "test-1-snapshot",
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: "test-2-snapshot",
				},
			},
			wantEqual: false,
		},
		{
			name:                 "isBindingSyncedWithClusterStatus should return false if binding and cluster status have different resourceOverrideSnapshot list",
			resourceSnapshotName: "test-1-snapshot",
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: "test-1-snapshot",
					ResourceOverrideSnapshots: []placementv1beta1.NamespacedName{
						{
							Name:      "ro2",
							Namespace: "ns2",
						},
						{
							Name:      "ro1",
							Namespace: "ns1",
						},
					},
				},
			},
			cluster: &placementv1beta1.ClusterUpdatingStatus{
				ResourceOverrideSnapshots: []placementv1beta1.NamespacedName{
					{
						Name:      "ro1",
						Namespace: "ns1",
					},
					{
						Name:      "ro2",
						Namespace: "ns2",
					},
				},
			},
			wantEqual: false,
		},
		{
			name:                 "isBindingSyncedWithClusterStatus should return false if binding and cluster status have different clusterResourceOverrideSnapshot list",
			resourceSnapshotName: "test-1-snapshot",
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: "test-1-snapshot",
					ResourceOverrideSnapshots: []placementv1beta1.NamespacedName{
						{Name: "ro1", Namespace: "ns1"},
						{Name: "ro2", Namespace: "ns2"},
					},
					ClusterResourceOverrideSnapshots: []string{"cr1", "cr2"},
				},
			},
			cluster: &placementv1beta1.ClusterUpdatingStatus{
				ResourceOverrideSnapshots: []placementv1beta1.NamespacedName{
					{Name: "ro1", Namespace: "ns1"},
					{Name: "ro2", Namespace: "ns2"},
				},
				ClusterResourceOverrideSnapshots: []string{"cr1"},
			},
			wantEqual: false,
		},
		{
			name:                 "isBindingSyncedWithClusterStatus should return false if binding and updateRun have different applyStrategy",
			resourceSnapshotName: "test-1-snapshot",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				Status: placementv1beta1.UpdateRunStatus{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type: placementv1beta1.ApplyStrategyTypeClientSideApply,
					},
				},
			},
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: "test-1-snapshot",
					ResourceOverrideSnapshots: []placementv1beta1.NamespacedName{
						{Name: "ro1", Namespace: "ns1"},
						{Name: "ro2", Namespace: "ns2"},
					},
					ClusterResourceOverrideSnapshots: []string{"cr1", "cr2"},
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type: placementv1beta1.ApplyStrategyTypeReportDiff,
					},
				},
			},
			cluster: &placementv1beta1.ClusterUpdatingStatus{
				ResourceOverrideSnapshots: []placementv1beta1.NamespacedName{
					{Name: "ro1", Namespace: "ns1"},
					{Name: "ro2", Namespace: "ns2"},
				},
				ClusterResourceOverrideSnapshots: []string{"cr1", "cr2"},
			},
			wantEqual: false,
		},
		{
			name:                 "isBindingSyncedWithClusterStatus should return true if resourceSnapshot, applyStrategy, and override lists are all deep equal",
			resourceSnapshotName: "test-1-snapshot",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				Status: placementv1beta1.UpdateRunStatus{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type: placementv1beta1.ApplyStrategyTypeReportDiff,
					},
				},
			},
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: "test-1-snapshot",
					ResourceOverrideSnapshots: []placementv1beta1.NamespacedName{
						{Name: "ro1", Namespace: "ns1"},
						{Name: "ro2", Namespace: "ns2"},
					},
					ClusterResourceOverrideSnapshots: []string{"cr1", "cr2"},
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type: placementv1beta1.ApplyStrategyTypeReportDiff,
					},
				},
			},
			cluster: &placementv1beta1.ClusterUpdatingStatus{
				ResourceOverrideSnapshots: []placementv1beta1.NamespacedName{
					{Name: "ro1", Namespace: "ns1"},
					{Name: "ro2", Namespace: "ns2"},
				},
				ClusterResourceOverrideSnapshots: []string{"cr1", "cr2"},
			},
			wantEqual: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := isBindingSyncedWithClusterStatus(test.resourceSnapshotName, test.updateRun, test.binding, test.cluster)
			if got != test.wantEqual {
				t.Fatalf("isBindingSyncedWithClusterStatus() got %v; want %v", got, test.wantEqual)
			}
		})
	}
}

func TestCheckClusterUpdateResult(t *testing.T) {
	updatingStage := &placementv1beta1.StageUpdatingStatus{
		StageName: "test-stage",
	}
	updateRun := &placementv1beta1.ClusterStagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
		},
	}
	tests := []struct {
		name          string
		binding       *placementv1beta1.ClusterResourceBinding
		clusterStatus *placementv1beta1.ClusterUpdatingStatus
		wantSucceeded bool
		wantErr       bool
	}{
		{
			name: "checkClusterUpdateResult should return true if the binding has available condition",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingAvailable),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
							Reason:             condition.AvailableReason,
						},
					},
				},
			},
			clusterStatus: &placementv1beta1.ClusterUpdatingStatus{ClusterName: "test-cluster"},
			wantSucceeded: true,
			wantErr:       false,
		},
		{
			name: "checkClusterUpdateResult should return false and error if the binding has false overridden condition",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingOverridden),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
							Reason:             condition.OverriddenFailedReason,
						},
					},
				},
			},
			clusterStatus: &placementv1beta1.ClusterUpdatingStatus{ClusterName: "test-cluster"},
			wantSucceeded: false,
			wantErr:       true,
		},
		{
			// WorkNotSynchronizedYet is an in-progress reason, not a terminal failure (issue #648).
			// checkClusterUpdateResult now treats it as "still updating" rather than an error.
			name: "checkClusterUpdateResult should return false but no error if workSynchronized is in-progress (WorkNotSynchronizedYet)",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
							Reason:             condition.WorkNotSynchronizedYetReason,
						},
					},
				},
			},
			clusterStatus: &placementv1beta1.ClusterUpdatingStatus{ClusterName: "test-cluster"},
			wantSucceeded: false,
			wantErr:       false,
		},
		{
			name: "checkClusterUpdateResult should return false and error if workSynchronized failed terminally (SyncWorkFailed)",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
							Reason:             condition.SyncWorkFailedReason,
						},
					},
				},
			},
			clusterStatus: &placementv1beta1.ClusterUpdatingStatus{ClusterName: "test-cluster"},
			wantSucceeded: false,
			wantErr:       true,
		},
		{
			name: "checkClusterUpdateResult should return false and error if the binding has false applied condition",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
							Reason:             condition.ApplyFailedReason,
						},
					},
				},
			},
			clusterStatus: &placementv1beta1.ClusterUpdatingStatus{ClusterName: "test-cluster"},
			wantSucceeded: false,
			wantErr:       true,
		},
		{
			name: "checkClusterUpdateResult should return false but no error if the binding is not available yet",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingOverridden),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
							Reason:             condition.OverriddenSucceededReason,
						},
						{
							Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
							Reason:             condition.WorkSynchronizedReason,
						},
						{
							Type:               string(placementv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
							Reason:             condition.ApplySucceededReason,
						},
					},
				},
			},
			clusterStatus: &placementv1beta1.ClusterUpdatingStatus{ClusterName: "test-cluster"},
			wantSucceeded: false,
			wantErr:       false,
		},
		{
			name: "checkClusterUpdateResult should return false but no error if the binding does not have any conditions",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{},
				},
			},
			clusterStatus: &placementv1beta1.ClusterUpdatingStatus{ClusterName: "test-cluster"},
			wantSucceeded: false,
			wantErr:       false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotSucceeded, gotErr := checkClusterUpdateResult(test.binding, test.clusterStatus, updatingStage, updateRun)
			if gotSucceeded != test.wantSucceeded {
				t.Fatalf("checkClusterUpdateResult() got %v; want %v", gotSucceeded, test.wantSucceeded)
			}
			if (gotErr != nil) != test.wantErr {
				t.Fatalf("checkClusterUpdateResult() got error %v; want error %v", gotErr, test.wantErr)
			}
			if test.wantSucceeded {
				if !condition.IsConditionStatusTrue(meta.FindStatusCondition(test.clusterStatus.Conditions, string(placementv1beta1.ClusterUpdatingConditionSucceeded)), updateRun.Generation) {
					t.Fatalf("checkClusterUpdateResult() failed to set ClusterUpdatingConditionSucceeded condition")
				}
			}
		})
	}
}

func TestBuildApprovalRequestObject(t *testing.T) {
	// Pin the values that show up on the approval requests of the delete stage, which users see and select with.
	const (
		deleteStageName                = "kubernetes-fleet.io/deleteStage"
		deleteStageLabelValue          = "delete-stage"
		deleteStageApprovalRequestName = "test-update-run-before-delete-stage"
	)
	tests := []struct {
		name           string
		namespacedName types.NamespacedName
		stageName      string
		updateRunName  string
		stageTaskType  string
		want           placementv1beta1.ApprovalRequestObj
	}{
		{
			name: "should create ClusterApprovalRequest when namespace is empty",
			namespacedName: types.NamespacedName{
				Name:      fmt.Sprintf(placementv1beta1.BeforeStageApprovalTaskNameFmt, "test-update-run", "test-stage"),
				Namespace: "",
			},
			stageName:     "test-stage",
			updateRunName: "test-update-run",
			stageTaskType: placementv1beta1.BeforeStageTaskLabelValue,
			want: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.BeforeStageApprovalTaskNameFmt, "test-update-run", "test-stage"),
					Labels: map[string]string{
						placementv1beta1.TargetUpdatingStageNameLabel:   "test-stage",
						placementv1beta1.TargetUpdateRunLabel:           "test-update-run",
						placementv1beta1.TaskTypeLabel:                  placementv1beta1.BeforeStageTaskLabelValue,
						placementv1beta1.IsLatestUpdateRunApprovalLabel: "true",
					},
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test-update-run",
					TargetStage:     "test-stage",
				},
			},
		},
		{
			name: "should create namespaced ApprovalRequest when namespace is provided",
			namespacedName: types.NamespacedName{
				Name:      fmt.Sprintf(placementv1beta1.AfterStageApprovalTaskNameFmt, "test-update-run", "test-stage"),
				Namespace: testNamespaceName,
			},
			stageName:     "test-stage",
			updateRunName: "test-update-run",
			stageTaskType: placementv1beta1.AfterStageTaskLabelValue,
			want: &placementv1beta1.ApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(placementv1beta1.AfterStageApprovalTaskNameFmt, "test-update-run", "test-stage"),
					Namespace: testNamespaceName,
					Labels: map[string]string{
						placementv1beta1.TargetUpdatingStageNameLabel:   "test-stage",
						placementv1beta1.TargetUpdateRunLabel:           "test-update-run",
						placementv1beta1.TaskTypeLabel:                  placementv1beta1.AfterStageTaskLabelValue,
						placementv1beta1.IsLatestUpdateRunApprovalLabel: "true",
					},
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test-update-run",
					TargetStage:     "test-stage",
				},
			},
		},
		{
			name: "should create ClusterApprovalRequest with a valid stage label for the delete stage",
			namespacedName: types.NamespacedName{
				Name: deleteStageApprovalRequestName,
			},
			stageName:     deleteStageName,
			updateRunName: "test-update-run",
			stageTaskType: placementv1beta1.BeforeStageTaskLabelValue,
			want: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: deleteStageApprovalRequestName,
					Labels: map[string]string{
						placementv1beta1.TargetUpdatingStageNameLabel:   deleteStageLabelValue,
						placementv1beta1.TargetUpdateRunLabel:           "test-update-run",
						placementv1beta1.TaskTypeLabel:                  placementv1beta1.BeforeStageTaskLabelValue,
						placementv1beta1.IsLatestUpdateRunApprovalLabel: "true",
					},
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test-update-run",
					TargetStage:     deleteStageName,
				},
			},
		},
		{
			name: "should create namespaced ApprovalRequest with a valid stage label for the delete stage",
			namespacedName: types.NamespacedName{
				Name:      deleteStageApprovalRequestName,
				Namespace: testNamespaceName,
			},
			stageName:     deleteStageName,
			updateRunName: "test-update-run",
			stageTaskType: placementv1beta1.BeforeStageTaskLabelValue,
			want: &placementv1beta1.ApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deleteStageApprovalRequestName,
					Namespace: testNamespaceName,
					Labels: map[string]string{
						placementv1beta1.TargetUpdatingStageNameLabel:   deleteStageLabelValue,
						placementv1beta1.TargetUpdateRunLabel:           "test-update-run",
						placementv1beta1.TaskTypeLabel:                  placementv1beta1.BeforeStageTaskLabelValue,
						placementv1beta1.IsLatestUpdateRunApprovalLabel: "true",
					},
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test-update-run",
					TargetStage:     deleteStageName,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildApprovalRequestObject(test.namespacedName, test.stageName, test.updateRunName, test.stageTaskType)

			// Compare the whole objects using cmp.Diff with ignore options
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("buildApprovalRequestObject() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TODO(arvindth): Add more test cases to cover aggregate error scenarios both positive and negative cases.
func TestExecuteUpdatingStage_Error(t *testing.T) {
	tests := []struct {
		name            string
		updateRun       *placementv1beta1.ClusterStagedUpdateRun
		bindings        []placementv1beta1.BindingObj
		interceptorFunc *interceptor.Funcs
		wantErr         error
		wantAbortErr    bool
		wantWaitTime    time.Duration
	}{
		{
			name: "cluster update failed",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-update-run",
					Generation: 1,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:         "test-placement",
					ResourceSnapshotIndex: "1",
				},
				Status: placementv1beta1.UpdateRunStatus{
					StagesStatus: []placementv1beta1.StageUpdatingStatus{
						{
							StageName: "test-stage",
							Clusters: []placementv1beta1.ClusterUpdatingStatus{
								{
									ClusterName: "cluster-1",
									Conditions: []metav1.Condition{
										{
											Type:               string(placementv1beta1.ClusterUpdatingConditionSucceeded),
											Status:             metav1.ConditionFalse,
											ObservedGeneration: 1,
											Reason:             condition.ClusterUpdatingFailedReason,
											Message:            "cluster update failed",
										},
									},
								},
							},
						},
					},
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name:           "test-stage",
								MaxConcurrency: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
							},
						},
					},
				},
			},
			bindings:        nil,
			interceptorFunc: nil,
			wantErr:         errors.New("the cluster `cluster-1` in the stage test-stage has failed"),
			wantAbortErr:    true,
			wantWaitTime:    0,
		},
		{
			name: "binding update failure",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-update-run",
					Generation: 1,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:         "test-placement",
					ResourceSnapshotIndex: "1",
				},
				Status: placementv1beta1.UpdateRunStatus{
					StagesStatus: []placementv1beta1.StageUpdatingStatus{
						{
							StageName: "test-stage",
							Clusters: []placementv1beta1.ClusterUpdatingStatus{
								{
									ClusterName: "cluster-1",
								},
							},
						},
					},
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name:           "test-stage",
								MaxConcurrency: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
							},
						},
					},
				},
			},
			bindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
						State:         placementv1beta1.BindingStateScheduled,
					},
				},
			},
			interceptorFunc: &interceptor.Funcs{
				Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					return errors.New("simulated update error")
				},
			},
			wantErr:      errors.New("simulated update error"),
			wantWaitTime: 0,
		},
		{
			name: "missing binding in map lookup - nil pointer guard",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-update-run",
					Generation: 1,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:         "test-placement",
					ResourceSnapshotIndex: "1",
				},
				Status: placementv1beta1.UpdateRunStatus{
					StagesStatus: []placementv1beta1.StageUpdatingStatus{
						{
							StageName: "test-stage",
							Clusters: []placementv1beta1.ClusterUpdatingStatus{
								{
									ClusterName: "cluster-1",
								},
							},
						},
					},
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name:           "test-stage",
								MaxConcurrency: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
							},
						},
					},
				},
			},
			bindings:     nil, // No bindings provided, so cluster-1 will not be found in the map.
			wantErr:      errors.New("the binding for cluster `cluster-1` in stage `test-stage` is not found in the toBeUpdatedBindings map"),
			wantAbortErr: true,
			wantWaitTime: 0,
		},
		{
			name: "binding preemption",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-update-run",
					Generation: 1,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:         "test-placement",
					ResourceSnapshotIndex: "1",
				},
				Status: placementv1beta1.UpdateRunStatus{
					StagesStatus: []placementv1beta1.StageUpdatingStatus{
						{
							StageName: "test-stage",
							Clusters: []placementv1beta1.ClusterUpdatingStatus{
								{
									ClusterName: "cluster-1",
									Conditions: []metav1.Condition{
										{
											Type:               string(placementv1beta1.ClusterUpdatingConditionStarted),
											Status:             metav1.ConditionTrue,
											ObservedGeneration: 1,
											Reason:             condition.ClusterUpdatingStartedReason,
										},
									},
								},
							},
						},
					},
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name:           "test-stage",
								MaxConcurrency: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
							},
						},
					},
				},
			},
			bindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster:        "cluster-1",
						ResourceSnapshotName: "wrong-snapshot",
						State:                placementv1beta1.BindingStateBound,
					},
					Status: placementv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			interceptorFunc: nil,
			wantErr:         errors.New("the binding of the updating cluster `cluster-1` in the stage `test-stage` is not up-to-date with the desired status"),
			wantAbortErr:    true,
			wantWaitTime:    0,
		},
		{
			name: "binding synced but state not bound - update binding state fails",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-update-run",
					Generation: 1,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:         "test-placement",
					ResourceSnapshotIndex: "1",
				},
				Status: placementv1beta1.UpdateRunStatus{
					ResourceSnapshotIndexUsed: "1",
					StagesStatus: []placementv1beta1.StageUpdatingStatus{
						{
							StageName: "test-stage",
							Clusters: []placementv1beta1.ClusterUpdatingStatus{
								{
									ClusterName: "cluster-1",
									// No conditions - cluster has not started updating yet.
								},
							},
						},
					},
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name:           "test-stage",
								MaxConcurrency: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
							},
						},
					},
				},
			},
			bindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster:        "cluster-1",
						ResourceSnapshotName: "test-placement-1-snapshot",            // Already synced.
						State:                placementv1beta1.BindingStateScheduled, // But not Bound yet.
					},
				},
			},
			interceptorFunc: &interceptor.Funcs{
				Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					return errors.New("failed to update binding state")
				},
			},
			wantErr:      errors.New("failed to update binding state"),
			wantWaitTime: 0,
		},
		{
			name: "binding synced and bound but generation updated - update rolloutStarted fails",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-update-run",
					Generation: 1,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:         "test-placement",
					ResourceSnapshotIndex: "1",
				},
				Status: placementv1beta1.UpdateRunStatus{
					ResourceSnapshotIndexUsed: "1",
					StagesStatus: []placementv1beta1.StageUpdatingStatus{
						{
							StageName: "test-stage",
							Clusters: []placementv1beta1.ClusterUpdatingStatus{
								{
									ClusterName: "cluster-1",
									// No conditions - cluster has not started updating yet.
								},
							},
						},
					},
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name:           "test-stage",
								MaxConcurrency: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
							},
						},
					},
				},
			},
			bindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 2, // Generation updated by scheduler.
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster:        "cluster-1",
						ResourceSnapshotName: "test-placement-1-snapshot",        // Already synced.
						State:                placementv1beta1.BindingStateBound, // Already Bound.
					},
					Status: placementv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1, // Old generation - needs update.
								Reason:             condition.RolloutStartedReason,
							},
						},
					},
				},
			},
			interceptorFunc: &interceptor.Funcs{
				SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					// Fail the status update for rolloutStarted.
					return errors.New("failed to update binding rolloutStarted status")
				},
			},
			wantErr:      errors.New("failed to update binding rolloutStarted status"),
			wantWaitTime: 0,
		},
		{
			name: "binding synced, bound, rolloutStarted true, but binding has failed condition",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-update-run",
					Generation: 1,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:         "test-placement",
					ResourceSnapshotIndex: "1",
				},
				Status: placementv1beta1.UpdateRunStatus{
					ResourceSnapshotIndexUsed: "1",
					StagesStatus: []placementv1beta1.StageUpdatingStatus{
						{
							StageName: "test-stage",
							Clusters: []placementv1beta1.ClusterUpdatingStatus{
								{
									ClusterName: "cluster-1",
									// No conditions - cluster has not started updating yet.
								},
							},
						},
					},
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name:           "test-stage",
								MaxConcurrency: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
							},
						},
					},
				},
			},
			bindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "binding-1",
						Generation: 1,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster:        "cluster-1",
						ResourceSnapshotName: "test-placement-1-snapshot",        // Already synced.
						State:                placementv1beta1.BindingStateBound, // Already Bound.
					},
					Status: placementv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
								Reason:             condition.RolloutStartedReason,
							},
							{
								Type:               string(placementv1beta1.ResourceBindingApplied),
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             condition.ApplyFailedReason,
							},
						},
					},
				},
			},
			interceptorFunc: nil,
			wantErr:         errors.New("cluster updating encountered an error at stage"),
			wantWaitTime:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			_ = placementv1beta1.AddToScheme(scheme)

			var fakeClient client.Client
			objs := make([]client.Object, len(tt.bindings))
			for i := range tt.bindings {
				objs[i] = tt.bindings[i]
			}
			if tt.interceptorFunc != nil {
				fakeClient = interceptor.NewClient(
					fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(),
					*tt.interceptorFunc,
				)
			} else {
				fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			}

			r := &Reconciler{
				Client: fakeClient,
			}

			// Execute the stage.
			waitTime, gotErr := r.executeUpdatingStage(ctx, tt.updateRun, 0, tt.bindings, 1)

			// Verify error expectation.
			if (tt.wantErr != nil) != (gotErr != nil) {
				t.Fatalf("executeUpdatingStage() want error: %v, got error: %v", tt.wantErr, gotErr)
			}

			// Verify error message contains expected substring.
			if tt.wantErr != nil && gotErr != nil {
				if errors.Is(gotErr, errStagedUpdatedAborted) != tt.wantAbortErr {
					t.Fatalf("executeUpdatingStage() want abort error: %v, got error: %v", tt.wantAbortErr, gotErr)
				}
				if !strings.Contains(gotErr.Error(), tt.wantErr.Error()) {
					t.Fatalf("executeUpdatingStage() want error: %v, got error: %v", tt.wantErr, gotErr)
				}
			}

			// Verify wait time.
			if waitTime != tt.wantWaitTime {
				t.Fatalf("executeUpdatingStage() want waitTime: %v, got waitTime: %v", tt.wantWaitTime, waitTime)
			}
		})
	}
}

func TestCalculateMaxConcurrencyValue(t *testing.T) {
	tests := []struct {
		name           string
		maxConcurrency *intstr.IntOrString
		clusterCount   int
		wantValue      int
		wantErr        bool
	}{
		{
			name:           "integer value - less than cluster count",
			maxConcurrency: &intstr.IntOrString{Type: intstr.Int, IntVal: 3},
			clusterCount:   10,
			wantValue:      3,
			wantErr:        false,
		},
		{
			name:           "integer value - equal to cluster count",
			maxConcurrency: &intstr.IntOrString{Type: intstr.Int, IntVal: 10},
			clusterCount:   10,
			wantValue:      10,
			wantErr:        false,
		},
		{
			name:           "integer value - greater than cluster count",
			maxConcurrency: &intstr.IntOrString{Type: intstr.Int, IntVal: 15},
			clusterCount:   10,
			wantValue:      15,
			wantErr:        false,
		},
		{
			name:           "percentage value - 50% with cluster count > 1",
			maxConcurrency: &intstr.IntOrString{Type: intstr.String, StrVal: "50%"},
			clusterCount:   10,
			wantValue:      5,
			wantErr:        false,
		},
		{
			name:           "percentage value - non zero percentage with cluster count equal to 1",
			maxConcurrency: &intstr.IntOrString{Type: intstr.String, StrVal: "10%"},
			clusterCount:   1,
			wantValue:      1,
			wantErr:        false,
		},
		{
			name:           "percentage value - 33% rounds down",
			maxConcurrency: &intstr.IntOrString{Type: intstr.String, StrVal: "33%"},
			clusterCount:   10,
			wantValue:      3,
			wantErr:        false,
		},
		{
			name:           "percentage value - 100%",
			maxConcurrency: &intstr.IntOrString{Type: intstr.String, StrVal: "100%"},
			clusterCount:   10,
			wantValue:      10,
			wantErr:        false,
		},
		{
			name:           "percentage value - 25% with 7 clusters",
			maxConcurrency: &intstr.IntOrString{Type: intstr.String, StrVal: "25%"},
			clusterCount:   7,
			wantValue:      1,
			wantErr:        false,
		},
		{
			name:           "zero clusters",
			maxConcurrency: &intstr.IntOrString{Type: intstr.Int, IntVal: 3},
			clusterCount:   0,
			wantValue:      3,
			wantErr:        false,
		},
		{
			name:           "non-zero percentage with zero clusters",
			maxConcurrency: &intstr.IntOrString{Type: intstr.String, StrVal: "50%"},
			clusterCount:   0,
			wantValue:      1,
			wantErr:        false,
		},
		{
			name:           "non-zero value as string without percentage with zero clusters",
			maxConcurrency: &intstr.IntOrString{Type: intstr.String, StrVal: "50"},
			clusterCount:   0,
			wantValue:      0,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := &placementv1beta1.UpdateRunStatus{
				StagesStatus: []placementv1beta1.StageUpdatingStatus{
					{
						StageName: "test-stage",
						Clusters:  make([]placementv1beta1.ClusterUpdatingStatus, tt.clusterCount),
					},
				},
				UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name:           "test-stage",
							MaxConcurrency: tt.maxConcurrency,
						},
					},
				},
			}

			gotValue, gotErr := calculateMaxConcurrencyValue(status, 0)

			if (gotErr != nil) != tt.wantErr {
				t.Fatalf("calculateMaxConcurrencyValue() error = %v, wantErr %v", gotErr, tt.wantErr)
			}

			if gotValue != tt.wantValue {
				t.Fatalf("calculateMaxConcurrencyValue() = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}

func TestCheckBeforeStageTasksStatus_NegativeCases(t *testing.T) {
	stageName := "stage-0"
	testUpdateRunName = "test-update-run"
	approvalRequestName := fmt.Sprintf(placementv1beta1.BeforeStageApprovalTaskNameFmt, testUpdateRunName, stageName)
	tests := []struct {
		name            string
		stageIndex      int
		updateRun       *placementv1beta1.ClusterStagedUpdateRun
		approvalRequest *placementv1beta1.ClusterApprovalRequest
		wantErrMsg      string
		wantErrAborted  bool
	}{
		// Negative test cases only
		{
			name:       "should return err if before stage task is TimedWait",
			stageIndex: 0,
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				Status: placementv1beta1.UpdateRunStatus{
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: stageName,
								BeforeStageTasks: []placementv1beta1.StageTask{
									{
										Type: placementv1beta1.StageTaskTypeTimedWait,
									},
								},
							},
						},
					},
					StagesStatus: []placementv1beta1.StageUpdatingStatus{
						{
							StageName: stageName,
							BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{
								{
									Type: placementv1beta1.StageTaskTypeTimedWait,
								},
							},
						},
					},
				},
			},
			wantErrMsg:     fmt.Sprintf("found unsupported task type in before stage tasks: %s", placementv1beta1.StageTaskTypeTimedWait),
			wantErrAborted: true,
		},
		{
			name:       "should return err if Approval request has wrong target stage in spec",
			stageIndex: 0,
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: testUpdateRunName,
				},
				Status: placementv1beta1.UpdateRunStatus{
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: stageName,
								BeforeStageTasks: []placementv1beta1.StageTask{
									{
										Type: placementv1beta1.StageTaskTypeApproval,
									},
								},
							},
						},
					},
					StagesStatus: []placementv1beta1.StageUpdatingStatus{
						{
							StageName: stageName,
							BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{
								{
									Type:                placementv1beta1.StageTaskTypeApproval,
									ApprovalRequestName: fmt.Sprintf(placementv1beta1.BeforeStageApprovalTaskNameFmt, testUpdateRunName, stageName),
									Conditions: []metav1.Condition{
										{
											Type:   string(placementv1beta1.StageTaskConditionApprovalRequestCreated),
											Status: metav1.ConditionTrue,
										},
									},
								},
							},
						},
					},
				},
			},
			approvalRequest: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: approvalRequestName,
					Labels: map[string]string{
						placementv1beta1.TargetUpdatingStageNameLabel:   stageName,
						placementv1beta1.TargetUpdateRunLabel:           testUpdateRunName,
						placementv1beta1.TaskTypeLabel:                  placementv1beta1.BeforeStageTaskLabelValue,
						placementv1beta1.IsLatestUpdateRunApprovalLabel: "true",
					},
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: testUpdateRunName,
					TargetStage:     "stage-1",
				},
			},
			wantErrMsg:     fmt.Sprintf("the approval request task `/%s` is targeting update run `/%s` and stage `stage-1`", approvalRequestName, testUpdateRunName),
			wantErrAborted: true,
		},
		{
			name:       "should return err if Approval request has wrong target update run in spec",
			stageIndex: 0,
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: testUpdateRunName,
				},
				Status: placementv1beta1.UpdateRunStatus{
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: stageName,
								BeforeStageTasks: []placementv1beta1.StageTask{
									{
										Type: placementv1beta1.StageTaskTypeApproval,
									},
								},
							},
						},
					},
					StagesStatus: []placementv1beta1.StageUpdatingStatus{
						{
							StageName: stageName,
							BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{
								{
									Type:                placementv1beta1.StageTaskTypeApproval,
									ApprovalRequestName: fmt.Sprintf(placementv1beta1.BeforeStageApprovalTaskNameFmt, testUpdateRunName, stageName),
									Conditions: []metav1.Condition{
										{
											Type:   string(placementv1beta1.StageTaskConditionApprovalRequestCreated),
											Status: metav1.ConditionTrue,
										},
									},
								},
							},
						},
					},
				},
			},
			approvalRequest: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.BeforeStageApprovalTaskNameFmt, testUpdateRunName, stageName),
					Labels: map[string]string{
						placementv1beta1.TargetUpdatingStageNameLabel:   stageName,
						placementv1beta1.TargetUpdateRunLabel:           testUpdateRunName,
						placementv1beta1.TaskTypeLabel:                  placementv1beta1.BeforeStageTaskLabelValue,
						placementv1beta1.IsLatestUpdateRunApprovalLabel: "true",
					},
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "wrong-update-run",
					TargetStage:     stageName,
				},
			},
			wantErrMsg:     fmt.Sprintf("the approval request task `/%s` is targeting update run `/wrong-update-run` and stage `%s`", approvalRequestName, stageName),
			wantErrAborted: true,
		},
		{
			name:       "should return err if cannot update Approval request that is approved as accepted",
			stageIndex: 0,
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: testUpdateRunName,
				},
				Status: placementv1beta1.UpdateRunStatus{
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: stageName,
								BeforeStageTasks: []placementv1beta1.StageTask{
									{
										Type: placementv1beta1.StageTaskTypeApproval,
									},
								},
							},
						},
					},
					StagesStatus: []placementv1beta1.StageUpdatingStatus{
						{
							StageName: stageName,
							BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{
								{
									Type:                placementv1beta1.StageTaskTypeApproval,
									ApprovalRequestName: fmt.Sprintf(placementv1beta1.BeforeStageApprovalTaskNameFmt, testUpdateRunName, stageName),
									Conditions: []metav1.Condition{
										{
											Type:   string(placementv1beta1.StageTaskConditionApprovalRequestCreated),
											Status: metav1.ConditionTrue,
										},
									},
								},
							},
						},
					},
				},
			},
			approvalRequest: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.BeforeStageApprovalTaskNameFmt, testUpdateRunName, stageName),
					Labels: map[string]string{
						placementv1beta1.TargetUpdatingStageNameLabel:   stageName,
						placementv1beta1.TargetUpdateRunLabel:           testUpdateRunName,
						placementv1beta1.TaskTypeLabel:                  placementv1beta1.BeforeStageTaskLabelValue,
						placementv1beta1.IsLatestUpdateRunApprovalLabel: "true",
					},
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: testUpdateRunName,
					TargetStage:     stageName,
				},
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(placementv1beta1.ApprovalRequestConditionApproved),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			wantErrMsg: fmt.Sprintf("error returned by the API server: clusterapprovalrequests.placement.kubernetes-fleet.io \"%s\" not found", approvalRequestName),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []client.Object{tt.updateRun}
			if tt.approvalRequest != nil {
				objects = append(objects, tt.approvalRequest)
			}
			objectsWithStatus := []client.Object{tt.updateRun}
			scheme := runtime.NewScheme()
			_ = placementv1beta1.AddToScheme(scheme)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(objectsWithStatus...).
				Build()
			r := Reconciler{
				Client: fakeClient,
			}
			ctx := context.Background()
			status := tt.updateRun.GetUpdateRunStatus()
			tasks := status.UpdateStrategySnapshot.Stages[tt.stageIndex].BeforeStageTasks
			_, gotErr := r.checkBeforeStageTasksStatus(ctx, &status.StagesStatus[tt.stageIndex], tasks, tt.updateRun)
			if gotErr == nil {
				t.Fatalf("checkBeforeStageTasksStatus() want error but got nil")
			}
			if !strings.Contains(gotErr.Error(), tt.wantErrMsg) {
				t.Fatalf("checkBeforeStageTasksStatus() error = %v, wantErr %v", gotErr, tt.wantErrMsg)
			}
			if tt.wantErrAborted && !errors.Is(gotErr, errStagedUpdatedAborted) {
				t.Fatalf("checkBeforeStageTasksStatus() want aborted error but got different error: %v", gotErr)
			}
		})
	}
}

func TestExecuteDeleteStage(t *testing.T) {
	const (
		updateRunName = "test-update-run"
		clusterName   = "cluster-1"
	)
	approvalRequestName := fmt.Sprintf(placementv1beta1.BeforeStageApprovalTaskNameFmt, updateRunName, placementv1beta1.UpdateRunDeleteStageTaskName)
	now := metav1.Now()

	approvalTask := placementv1beta1.StageTask{Type: placementv1beta1.StageTaskTypeApproval}
	approvalTaskStatus := placementv1beta1.StageTaskStatus{Type: placementv1beta1.StageTaskTypeApproval, ApprovalRequestName: approvalRequestName}
	newCondition := func(condType any, status metav1.ConditionStatus, reason string) metav1.Condition {
		return metav1.Condition{Type: fmt.Sprint(condType), Status: status, Reason: reason, ObservedGeneration: 1}
	}
	stageWaitingCond := newCondition(placementv1beta1.StageUpdatingConditionProgressing, metav1.ConditionFalse, condition.StageUpdatingWaitingReason)
	stageStartedCond := newCondition(placementv1beta1.StageUpdatingConditionProgressing, metav1.ConditionTrue, condition.StageUpdatingStartedReason)
	runWaitingCond := newCondition(placementv1beta1.StagedUpdateRunConditionProgressing, metav1.ConditionFalse, condition.UpdateRunWaitingReason)
	runProgressingCond := newCondition(placementv1beta1.StagedUpdateRunConditionProgressing, metav1.ConditionTrue, condition.UpdateRunProgressingReason)
	requestCreatedCond := newCondition(placementv1beta1.StageTaskConditionApprovalRequestCreated, metav1.ConditionTrue, condition.StageTaskApprovalRequestCreatedReason)
	requestApprovedCond := newCondition(placementv1beta1.StageTaskConditionApprovalRequestApproved, metav1.ConditionTrue, condition.StageTaskApprovalRequestApprovedReason)
	clusterStartedCond := newCondition(placementv1beta1.ClusterUpdatingConditionStarted, metav1.ConditionTrue, condition.ClusterUpdatingStartedReason)
	approvedRequest := &placementv1beta1.ClusterApprovalRequest{
		ObjectMeta: metav1.ObjectMeta{Name: approvalRequestName, Generation: 1},
		Spec: placementv1beta1.ApprovalRequestSpec{
			TargetUpdateRun: updateRunName,
			TargetStage:     placementv1beta1.UpdateRunDeleteStageName,
		},
		Status: placementv1beta1.ApprovalRequestStatus{
			Conditions: []metav1.Condition{
				newCondition(placementv1beta1.ApprovalRequestConditionApproved, metav1.ConditionTrue, "Approved"),
			},
		},
	}
	noClusters := []placementv1beta1.ClusterUpdatingStatus{}

	tests := []struct {
		name string
		// tasks are the before stage tasks of the delete stage.
		tasks []placementv1beta1.StageTask
		// deleteStageStatus is the status of the delete stage, whose name and clusters are defaulted.
		deleteStageStatus placementv1beta1.StageUpdatingStatus
		approvalRequest   *placementv1beta1.ClusterApprovalRequest
		noBinding         bool
		wantFinished      bool
		wantWaitTime      time.Duration
		wantErr           error
		wantBindingKept   bool
		// wantDeleteStageStatus is the wanted status of the delete stage, whose name and clusters are defaulted.
		wantDeleteStageStatus placementv1beta1.StageUpdatingStatus
		wantRunConditions     []metav1.Condition
		wantApprovalRequest   bool
	}{
		{
			name:         "no delete stage configuration should delete the binding",
			wantWaitTime: clusterUpdatingWaitTime,
			wantDeleteStageStatus: placementv1beta1.StageUpdatingStatus{
				Clusters:   []placementv1beta1.ClusterUpdatingStatus{{ClusterName: clusterName, Conditions: []metav1.Condition{clusterStartedCond}}},
				Conditions: []metav1.Condition{stageStartedCond},
			},
		},
		{
			name:  "pending approval task should create the approval request and keep the binding",
			tasks: []placementv1beta1.StageTask{approvalTask},
			deleteStageStatus: placementv1beta1.StageUpdatingStatus{
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{approvalTaskStatus},
			},
			wantWaitTime:    stageUpdatingWaitTime,
			wantBindingKept: true,
			wantDeleteStageStatus: placementv1beta1.StageUpdatingStatus{
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{{
					Type:                placementv1beta1.StageTaskTypeApproval,
					ApprovalRequestName: approvalRequestName,
					Conditions:          []metav1.Condition{requestCreatedCond},
				}},
				Conditions: []metav1.Condition{stageWaitingCond},
			},
			wantRunConditions:   []metav1.Condition{runWaitingCond},
			wantApprovalRequest: true,
		},
		{
			name:  "approved approval task should delete the binding",
			tasks: []placementv1beta1.StageTask{approvalTask},
			deleteStageStatus: placementv1beta1.StageUpdatingStatus{
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{approvalTaskStatus},
				Conditions:            []metav1.Condition{stageWaitingCond},
			},
			approvalRequest: approvedRequest,
			wantWaitTime:    clusterUpdatingWaitTime,
			wantDeleteStageStatus: placementv1beta1.StageUpdatingStatus{
				Clusters: []placementv1beta1.ClusterUpdatingStatus{{ClusterName: clusterName, Conditions: []metav1.Condition{clusterStartedCond}}},
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{{
					Type:                placementv1beta1.StageTaskTypeApproval,
					ApprovalRequestName: approvalRequestName,
					Conditions:          []metav1.Condition{requestCreatedCond, requestApprovedCond},
				}},
				Conditions: []metav1.Condition{stageStartedCond},
			},
			wantRunConditions:   []metav1.Condition{runProgressingCond},
			wantApprovalRequest: true,
		},
		{
			name:  "accepted approval request that is unapproved afterwards should still delete the binding",
			tasks: []placementv1beta1.StageTask{approvalTask},
			deleteStageStatus: placementv1beta1.StageUpdatingStatus{
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{approvalTaskStatus},
			},
			approvalRequest: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: approvedRequest.ObjectMeta,
				Spec:       approvedRequest.Spec,
				Status: placementv1beta1.ApprovalRequestStatus{
					Conditions: []metav1.Condition{
						newCondition(placementv1beta1.ApprovalRequestConditionApproved, metav1.ConditionFalse, "Unapproved"),
						newCondition(placementv1beta1.ApprovalRequestConditionApprovalAccepted, metav1.ConditionTrue, condition.ApprovalRequestApprovalAcceptedReason),
					},
				},
			},
			wantWaitTime: clusterUpdatingWaitTime,
			wantDeleteStageStatus: placementv1beta1.StageUpdatingStatus{
				Clusters: []placementv1beta1.ClusterUpdatingStatus{{ClusterName: clusterName, Conditions: []metav1.Condition{clusterStartedCond}}},
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{{
					Type:                placementv1beta1.StageTaskTypeApproval,
					ApprovalRequestName: approvalRequestName,
					Conditions:          []metav1.Condition{requestCreatedCond, requestApprovedCond},
				}},
				Conditions: []metav1.Condition{stageStartedCond},
			},
			wantRunConditions:   []metav1.Condition{runProgressingCond},
			wantApprovalRequest: true,
		},
		{
			// A delete stage that has started is not gated again, so no approval request is created.
			name:  "tasks should not be checked again after the delete stage has started",
			tasks: []placementv1beta1.StageTask{approvalTask},
			deleteStageStatus: placementv1beta1.StageUpdatingStatus{
				StartTime:             &now,
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{approvalTaskStatus},
				Conditions:            []metav1.Condition{stageStartedCond},
			},
			wantWaitTime: clusterUpdatingWaitTime,
			wantDeleteStageStatus: placementv1beta1.StageUpdatingStatus{
				Clusters:              []placementv1beta1.ClusterUpdatingStatus{{ClusterName: clusterName, Conditions: []metav1.Condition{clusterStartedCond}}},
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{approvalTaskStatus},
				Conditions:            []metav1.Condition{stageStartedCond},
			},
		},
		{
			name:  "tasks should be skipped when there is no cluster to delete",
			tasks: []placementv1beta1.StageTask{approvalTask},
			deleteStageStatus: placementv1beta1.StageUpdatingStatus{
				Clusters:              noClusters,
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{approvalTaskStatus},
			},
			noBinding:    true,
			wantFinished: true,
			wantWaitTime: clusterUpdatingWaitTime,
			wantDeleteStageStatus: placementv1beta1.StageUpdatingStatus{
				Clusters:              noClusters,
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{approvalTaskStatus},
				Conditions: []metav1.Condition{
					newCondition(placementv1beta1.StageUpdatingConditionProgressing, metav1.ConditionFalse, condition.StageUpdatingSucceededReason),
					newCondition(placementv1beta1.StageUpdatingConditionSucceeded, metav1.ConditionTrue, condition.StageUpdatingSucceededReason),
				},
			},
		},
		{
			name:  "tasks should be skipped when the bindings are already deleted",
			tasks: []placementv1beta1.StageTask{approvalTask},
			deleteStageStatus: placementv1beta1.StageUpdatingStatus{
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{approvalTaskStatus},
			},
			noBinding:    true,
			wantFinished: true,
			wantWaitTime: clusterUpdatingWaitTime,
			wantDeleteStageStatus: placementv1beta1.StageUpdatingStatus{
				Clusters: []placementv1beta1.ClusterUpdatingStatus{{
					ClusterName: clusterName,
					Conditions: []metav1.Condition{
						clusterStartedCond,
						newCondition(placementv1beta1.ClusterUpdatingConditionSucceeded, metav1.ConditionTrue, condition.ClusterUpdatingSucceededReason),
					},
				}},
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{approvalTaskStatus},
				Conditions: []metav1.Condition{
					newCondition(placementv1beta1.StageUpdatingConditionProgressing, metav1.ConditionFalse, condition.StageUpdatingSucceededReason),
					newCondition(placementv1beta1.StageUpdatingConditionSucceeded, metav1.ConditionTrue, condition.StageUpdatingSucceededReason),
				},
			},
		},
		{
			name:  "approval request targeting another update run should abort the update run",
			tasks: []placementv1beta1.StageTask{approvalTask},
			deleteStageStatus: placementv1beta1.StageUpdatingStatus{
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{approvalTaskStatus},
			},
			approvalRequest: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{Name: approvalRequestName, Generation: 1},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "another-update-run",
					TargetStage:     placementv1beta1.UpdateRunDeleteStageName,
				},
			},
			wantErr:         errStagedUpdatedAborted,
			wantBindingKept: true,
			wantDeleteStageStatus: placementv1beta1.StageUpdatingStatus{
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{{
					Type:                placementv1beta1.StageTaskTypeApproval,
					ApprovalRequestName: approvalRequestName,
					Conditions:          []metav1.Condition{requestCreatedCond},
				}},
			},
			wantApprovalRequest: true,
		},
		{
			// The API rejects a TimedWait task on the delete stage, so this only happens with a corrupted snapshot.
			name:  "unsupported timed wait task should abort the update run",
			tasks: []placementv1beta1.StageTask{{Type: placementv1beta1.StageTaskTypeTimedWait, WaitTime: &metav1.Duration{Duration: time.Hour}}},
			deleteStageStatus: placementv1beta1.StageUpdatingStatus{
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{{Type: placementv1beta1.StageTaskTypeTimedWait}},
			},
			wantErr:         errStagedUpdatedAborted,
			wantBindingKept: true,
			wantDeleteStageStatus: placementv1beta1.StageUpdatingStatus{
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{{Type: placementv1beta1.StageTaskTypeTimedWait}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			if err := placementv1beta1.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme() = %v, want no error", err)
			}
			binding := &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "test-binding"},
				Spec:       placementv1beta1.ResourceBindingSpec{TargetCluster: clusterName},
			}
			clientBuilder := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&placementv1beta1.ClusterApprovalRequest{})
			var toBeDeletedBindings []placementv1beta1.BindingObj
			if !tt.noBinding {
				clientBuilder = clientBuilder.WithObjects(binding)
				toBeDeletedBindings = append(toBeDeletedBindings, binding)
			}
			if tt.approvalRequest != nil {
				clientBuilder = clientBuilder.WithObjects(tt.approvalRequest.DeepCopy())
			}
			fakeClient := clientBuilder.Build()
			r := &Reconciler{Client: fakeClient}

			deleteStageStatus := tt.deleteStageStatus.DeepCopy()
			deleteStageStatus.StageName = placementv1beta1.UpdateRunDeleteStageName
			if deleteStageStatus.Clusters == nil {
				deleteStageStatus.Clusters = []placementv1beta1.ClusterUpdatingStatus{{ClusterName: clusterName}}
			}
			updateRun := &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{Name: updateRunName, Generation: 1},
				Status: placementv1beta1.UpdateRunStatus{
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{},
					DeletionStageStatus:    deleteStageStatus,
				},
			}
			if tt.tasks != nil {
				updateRun.Status.UpdateStrategySnapshot.DeleteStage = &placementv1beta1.DeleteStageConfig{BeforeStageTasks: tt.tasks}
			}
			// The metrics are global; an approved approval request records one.
			t.Cleanup(func() { deleteUpdateRunMetrics(updateRun) })

			gotFinished, gotWaitTime, gotErr := r.executeDeleteStage(ctx, updateRun, toBeDeletedBindings)
			if !errors.Is(gotErr, tt.wantErr) {
				t.Fatalf("executeDeleteStage() error = %v, want %v", gotErr, tt.wantErr)
			}
			if gotFinished != tt.wantFinished {
				t.Errorf("executeDeleteStage() finished = %v, want %v", gotFinished, tt.wantFinished)
			}
			if gotWaitTime != tt.wantWaitTime {
				t.Errorf("executeDeleteStage() waitTime = %v, want %v", gotWaitTime, tt.wantWaitTime)
			}

			if !tt.noBinding {
				err := fakeClient.Get(ctx, client.ObjectKeyFromObject(binding), &placementv1beta1.ClusterResourceBinding{})
				if gotBindingKept := err == nil; gotBindingKept != tt.wantBindingKept || (err != nil && !apierrors.IsNotFound(err)) {
					t.Errorf("executeDeleteStage() binding kept = %v (get error: %v), want %v", gotBindingKept, err, tt.wantBindingKept)
				}
			}
			err := fakeClient.Get(ctx, client.ObjectKey{Name: approvalRequestName}, &placementv1beta1.ClusterApprovalRequest{})
			if gotApprovalRequest := err == nil; gotApprovalRequest != tt.wantApprovalRequest || (err != nil && !apierrors.IsNotFound(err)) {
				t.Errorf("executeDeleteStage() approval request exists = %v (get error: %v), want %v", gotApprovalRequest, err, tt.wantApprovalRequest)
			}

			wantDeleteStageStatus := tt.wantDeleteStageStatus.DeepCopy()
			wantDeleteStageStatus.StageName = placementv1beta1.UpdateRunDeleteStageName
			if wantDeleteStageStatus.Clusters == nil {
				wantDeleteStageStatus.Clusters = []placementv1beta1.ClusterUpdatingStatus{{ClusterName: clusterName}}
			}
			cmpOpts := []cmp.Option{
				cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message"),
				cmpopts.IgnoreFields(placementv1beta1.StageUpdatingStatus{}, "StartTime", "EndTime"),
			}
			if diff := cmp.Diff(wantDeleteStageStatus, updateRun.Status.DeletionStageStatus, cmpOpts...); diff != "" {
				t.Errorf("executeDeleteStage() delete stage status mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantRunConditions, updateRun.Status.Conditions, cmpOpts...); diff != "" {
				t.Errorf("executeDeleteStage() update run conditions mismatch (-want +got):\n%s", diff)
			}
			// The delete stage is only considered started once the tasks are completed.
			if gotStarted, wantStarted := updateRun.Status.DeletionStageStatus.StartTime != nil, !tt.wantBindingKept; gotStarted != wantStarted {
				t.Errorf("executeDeleteStage() delete stage started = %v, want %v", gotStarted, wantStarted)
			}
		})
	}
}

func TestGenerateStuckClustersString(t *testing.T) {
	tests := []struct {
		name              string
		stuckClusterNames []string
		wantClusterString string
	}{
		{
			name:              "empty cluster list",
			stuckClusterNames: []string{},
			wantClusterString: "",
		},
		{
			name:              "single cluster",
			stuckClusterNames: []string{"cluster1"},
			wantClusterString: "cluster1",
		},
		{
			name:              "two clusters",
			stuckClusterNames: []string{"cluster1", "cluster2"},
			wantClusterString: "cluster1, cluster2",
		},
		{
			name:              "three clusters",
			stuckClusterNames: []string{"cluster1", "cluster2", "cluster3"},
			wantClusterString: "cluster1, cluster2, cluster3",
		},
		{
			name:              "five clusters - should only show first three",
			stuckClusterNames: []string{"cluster1", "cluster2", "cluster3", "cluster4", "cluster5"},
			wantClusterString: "cluster1, cluster2, cluster3...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateStuckClustersString(tt.stuckClusterNames)

			if got != tt.wantClusterString {
				t.Fatalf("generateStuckClustersString() = %v, want %v", got, tt.wantClusterString)
			}
		})
	}
}

func TestExecute_ZeroClustersSkipsEntireStage(t *testing.T) {
	tests := []struct {
		name            string
		updateRun       *placementv1beta1.ClusterStagedUpdateRun
		stageIndex      int
		wantWaitTime    time.Duration
		wantErr         bool
		wantStageStatus placementv1beta1.StageUpdatingStatus
	}{
		{
			name: "zero clusters should skip entire stage including before-stage tasks",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-update-run",
					Generation: 1,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:         "test-placement",
					ResourceSnapshotIndex: "1",
					State:                 placementv1beta1.StateRun,
				},
				Status: placementv1beta1.UpdateRunStatus{
					ResourceSnapshotIndexUsed: "1",
					StagesStatus: []placementv1beta1.StageUpdatingStatus{
						{
							StageName: "empty-stage",
							Clusters:  []placementv1beta1.ClusterUpdatingStatus{}, // Zero clusters.
							BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{
								{
									Type:                placementv1beta1.StageTaskTypeApproval,
									ApprovalRequestName: "test-update-run-empty-before-stage",
								},
							},
							AfterStageTaskStatus: []placementv1beta1.StageTaskStatus{
								{
									Type: placementv1beta1.StageTaskTypeTimedWait,
								},
								{
									Type:                placementv1beta1.StageTaskTypeApproval,
									ApprovalRequestName: "test-update-run-after-empty-stage",
								},
							},
						},
					},
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name:           "empty-stage",
								MaxConcurrency: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
								BeforeStageTasks: []placementv1beta1.StageTask{
									{
										Type: placementv1beta1.StageTaskTypeApproval,
									},
								},
								AfterStageTasks: []placementv1beta1.StageTask{
									{
										Type:     placementv1beta1.StageTaskTypeTimedWait,
										WaitTime: &metav1.Duration{Duration: 5 * time.Minute},
									},
									{
										Type: placementv1beta1.StageTaskTypeApproval,
									},
								},
							},
						},
					},
				},
			},
			stageIndex:   0,
			wantWaitTime: 0, // No wait time, stage is skipped.
			wantErr:      false,
			wantStageStatus: placementv1beta1.StageUpdatingStatus{
				StageName: "empty-stage",
				Clusters:  []placementv1beta1.ClusterUpdatingStatus{}, // Zero clusters.
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{
					{
						Type:                placementv1beta1.StageTaskTypeApproval,
						ApprovalRequestName: "test-update-run-empty-before-stage",
					},
				},
				AfterStageTaskStatus: []placementv1beta1.StageTaskStatus{
					{
						Type: placementv1beta1.StageTaskTypeTimedWait,
					},
					{
						Type:                placementv1beta1.StageTaskTypeApproval,
						ApprovalRequestName: "test-update-run-after-empty-stage",
					},
				},
				StartTime: &metav1.Time{Time: time.Now()},
				EndTime:   &metav1.Time{Time: time.Now()},
				Conditions: []metav1.Condition{
					{
						Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						Reason:             condition.StageUpdatingSkippedNoClustersReason,
						Message:            "Stage skipped because it has no clusters",
					},
					{
						Type:               string(placementv1beta1.StageUpdatingConditionSucceeded),
						Status:             metav1.ConditionTrue,
						ObservedGeneration: 1,
						Reason:             condition.StageUpdatingSkippedNoClustersReason,
						Message:            "Stage skipped because it has no clusters",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = placementv1beta1.AddToScheme(scheme)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.updateRun).
				WithStatusSubresource(tt.updateRun).
				Build()
			r := Reconciler{
				Client: fakeClient,
			}
			ctx := context.Background()

			_, waitTime, gotErr := r.execute(ctx, tt.updateRun, tt.stageIndex, nil, nil)

			if (gotErr != nil) != tt.wantErr {
				t.Fatalf("execute() error = %v, wantErr %v", gotErr, tt.wantErr)
			}

			if waitTime != tt.wantWaitTime {
				t.Fatalf("execute() waitTime = %v, want %v", waitTime, tt.wantWaitTime)
			}

			gotStageStatus := tt.updateRun.Status.StagesStatus[tt.stageIndex]

			// Verify StartTime and EndTime are set for skipped stages.
			if gotStageStatus.StartTime == nil {
				t.Fatal("execute() StartTime should be set for skipped stage")
			}
			if gotStageStatus.EndTime == nil {
				t.Fatal("execute() EndTime should be set for skipped stage")
			}

			// Compare stage status using cmp.Diff, ignoring time fields.
			if diff := cmp.Diff(tt.wantStageStatus, gotStageStatus,
				cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
				cmpopts.IgnoreFields(placementv1beta1.StageUpdatingStatus{}, "StartTime", "EndTime"),
			); diff != "" {
				t.Fatalf("execute() stage status mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestExecuteDeleteStage_MultipleClusters tests that executeDeleteStage only deletes the bindings whose deletion has
// not started yet, and marks the clusters with no binding left as deleted.
func TestExecuteDeleteStage_MultipleClusters(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	newCondition := func(condType any, reason string) metav1.Condition {
		return metav1.Condition{Type: fmt.Sprint(condType), Status: metav1.ConditionTrue, Reason: reason, ObservedGeneration: 1}
	}
	clusterStartedCond := newCondition(placementv1beta1.ClusterUpdatingConditionStarted, condition.ClusterUpdatingStartedReason)
	clusterSucceededCond := newCondition(placementv1beta1.ClusterUpdatingConditionSucceeded, condition.ClusterUpdatingSucceededReason)
	deletionTime := metav1.Now()

	// The deletion of the binding on cluster-1 has started already; the fake client keeps it as it has a finalizer.
	deletingBinding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "binding-1", DeletionTimestamp: &deletionTime, Finalizers: []string{"test-finalizer"}},
		Spec:       placementv1beta1.ResourceBindingSpec{TargetCluster: "cluster-1"},
	}
	binding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "binding-2"},
		Spec:       placementv1beta1.ResourceBindingSpec{TargetCluster: "cluster-2"},
	}
	var deletedBindings []string
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deletingBinding, binding).WithInterceptorFuncs(interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			deletedBindings = append(deletedBindings, obj.GetName())
			return c.Delete(ctx, obj, opts...)
		},
	}).Build()
	r := &Reconciler{Client: fakeClient}
	updateRun := &placementv1beta1.ClusterStagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{Name: "test-update-run", Generation: 1},
		Status: placementv1beta1.UpdateRunStatus{
			UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{},
			DeletionStageStatus: &placementv1beta1.StageUpdatingStatus{
				StageName: placementv1beta1.UpdateRunDeleteStageName,
				Clusters: []placementv1beta1.ClusterUpdatingStatus{
					{ClusterName: "cluster-1", Conditions: []metav1.Condition{clusterStartedCond}},
					{ClusterName: "cluster-2"},
					// The binding on cluster-3 is gone, so the cluster is deleted.
					{ClusterName: "cluster-3", Conditions: []metav1.Condition{clusterStartedCond}},
				},
			},
		},
	}

	gotFinished, gotWaitTime, err := r.executeDeleteStage(ctx, updateRun, []placementv1beta1.BindingObj{deletingBinding, binding})
	if err != nil {
		t.Fatalf("executeDeleteStage() error = %v, want no error", err)
	}
	if gotFinished || gotWaitTime != clusterUpdatingWaitTime {
		t.Errorf("executeDeleteStage() = (%v, %v), want (false, %v)", gotFinished, gotWaitTime, clusterUpdatingWaitTime)
	}
	if diff := cmp.Diff([]string{"binding-2"}, deletedBindings); diff != "" {
		t.Errorf("executeDeleteStage() deleted bindings mismatch (-want +got):\n%s", diff)
	}
	wantClusters := []placementv1beta1.ClusterUpdatingStatus{
		{ClusterName: "cluster-1", Conditions: []metav1.Condition{clusterStartedCond}},
		{ClusterName: "cluster-2", Conditions: []metav1.Condition{clusterStartedCond}},
		{ClusterName: "cluster-3", Conditions: []metav1.Condition{clusterStartedCond, clusterSucceededCond}},
	}
	if diff := cmp.Diff(wantClusters, updateRun.Status.DeletionStageStatus.Clusters, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message")); diff != "" {
		t.Errorf("executeDeleteStage() delete stage clusters mismatch (-want +got):\n%s", diff)
	}
}

// TestExecuteDeleteStage_Errors tests that executeDeleteStage reports the API server errors as retriable and aborts
// the update run, without deleting any binding, if the delete stage status does not match the bindings.
func TestExecuteDeleteStage_Errors(t *testing.T) {
	const updateRunName = "test-update-run"
	approvalRequestName := fmt.Sprintf(placementv1beta1.BeforeStageApprovalTaskNameFmt, updateRunName, placementv1beta1.UpdateRunDeleteStageTaskName)
	errAPIServer := apierrors.NewServiceUnavailable("api server error")
	newCondition := func(condType any, reason string) metav1.Condition {
		return metav1.Condition{Type: fmt.Sprint(condType), Status: metav1.ConditionTrue, Reason: reason, ObservedGeneration: 1}
	}
	clusterStartedCond := newCondition(placementv1beta1.ClusterUpdatingConditionStarted, condition.ClusterUpdatingStartedReason)
	clusterSucceededCond := newCondition(placementv1beta1.ClusterUpdatingConditionSucceeded, condition.ClusterUpdatingSucceededReason)
	approvalRequest := func(conds ...metav1.Condition) *placementv1beta1.ClusterApprovalRequest {
		return &placementv1beta1.ClusterApprovalRequest{
			ObjectMeta: metav1.ObjectMeta{Name: approvalRequestName, Generation: 1},
			Spec: placementv1beta1.ApprovalRequestSpec{
				TargetUpdateRun: updateRunName,
				TargetStage:     placementv1beta1.UpdateRunDeleteStageName,
			},
			Status: placementv1beta1.ApprovalRequestStatus{Conditions: conds},
		}
	}
	isApprovalRequest := func(obj client.Object) bool {
		_, ok := obj.(*placementv1beta1.ClusterApprovalRequest)
		return ok
	}

	tests := []struct {
		name            string
		withApproval    bool
		approvalRequest *placementv1beta1.ClusterApprovalRequest
		// clusters are the clusters in the delete stage; cluster-2 is always added without any condition.
		clusters     []placementv1beta1.ClusterUpdatingStatus
		interceptors interceptor.Funcs
		wantErr      error
	}{
		{
			name:         "failing to create the approval request should be retriable",
			withApproval: true,
			clusters:     []placementv1beta1.ClusterUpdatingStatus{{ClusterName: "cluster-1"}},
			interceptors: interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if isApprovalRequest(obj) {
						return errAPIServer
					}
					return c.Create(ctx, obj, opts...)
				},
			},
			wantErr: controller.ErrAPIServerError,
		},
		{
			name:            "failing to get the existing approval request should be retriable",
			withApproval:    true,
			approvalRequest: approvalRequest(),
			clusters:        []placementv1beta1.ClusterUpdatingStatus{{ClusterName: "cluster-1"}},
			interceptors: interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if isApprovalRequest(obj) {
						return errAPIServer
					}
					return c.Get(ctx, key, obj, opts...)
				},
			},
			wantErr: controller.ErrAPIServerError,
		},
		{
			name:            "failing to accept the approval should be retriable",
			withApproval:    true,
			approvalRequest: approvalRequest(newCondition(placementv1beta1.ApprovalRequestConditionApproved, "Approved")),
			clusters:        []placementv1beta1.ClusterUpdatingStatus{{ClusterName: "cluster-1"}},
			interceptors: interceptor.Funcs{
				SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					return errAPIServer
				},
			},
			wantErr: controller.ErrAPIServerError,
		},
		{
			name:     "failing to delete a binding should be retriable",
			clusters: []placementv1beta1.ClusterUpdatingStatus{{ClusterName: "cluster-1"}},
			interceptors: interceptor.Funcs{
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					return errAPIServer
				},
			},
			wantErr: controller.ErrAPIServerError,
		},
		{
			name:     "binding on a cluster missing from the delete stage should abort the update run",
			clusters: []placementv1beta1.ClusterUpdatingStatus{{ClusterName: "another-cluster"}},
			wantErr:  errStagedUpdatedAborted,
		},
		{
			name:     "deleted cluster that still has a binding should abort the update run",
			clusters: []placementv1beta1.ClusterUpdatingStatus{{ClusterName: "cluster-1", Conditions: []metav1.Condition{clusterStartedCond, clusterSucceededCond}}},
			wantErr:  errStagedUpdatedAborted,
		},
		{
			name:     "deleting cluster whose binding is not deleting should abort the update run",
			clusters: []placementv1beta1.ClusterUpdatingStatus{{ClusterName: "cluster-1", Conditions: []metav1.Condition{clusterStartedCond}}},
			wantErr:  errStagedUpdatedAborted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			if err := placementv1beta1.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme() = %v, want no error", err)
			}
			// The binding on cluster-2 comes after the binding on cluster-1, so that an abort caused by the
			// cluster-1 shows that no binding is deleted before all the clusters are checked.
			bindings := []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "binding-1"},
					Spec:       placementv1beta1.ResourceBindingSpec{TargetCluster: "cluster-1"},
				},
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "binding-2"},
					Spec:       placementv1beta1.ResourceBindingSpec{TargetCluster: "cluster-2"},
				},
			}
			clientBuilder := fake.NewClientBuilder().WithScheme(scheme).
				WithStatusSubresource(&placementv1beta1.ClusterApprovalRequest{}).
				WithObjects(bindings[0], bindings[1])
			if tt.approvalRequest != nil {
				clientBuilder = clientBuilder.WithObjects(tt.approvalRequest)
			}
			var deletedBindings []string
			interceptors := tt.interceptors
			if interceptors.Delete == nil {
				interceptors.Delete = func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					deletedBindings = append(deletedBindings, obj.GetName())
					return c.Delete(ctx, obj, opts...)
				}
			}
			r := &Reconciler{Client: clientBuilder.WithInterceptorFuncs(interceptors).Build()}

			updateRun := &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{Name: updateRunName, Generation: 1},
				Status: placementv1beta1.UpdateRunStatus{
					UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{},
					DeletionStageStatus: &placementv1beta1.StageUpdatingStatus{
						StageName: placementv1beta1.UpdateRunDeleteStageName,
						Clusters:  append(tt.clusters, placementv1beta1.ClusterUpdatingStatus{ClusterName: "cluster-2"}),
					},
				},
			}
			if tt.withApproval {
				updateRun.Status.UpdateStrategySnapshot.DeleteStage = &placementv1beta1.DeleteStageConfig{
					BeforeStageTasks: []placementv1beta1.StageTask{{Type: placementv1beta1.StageTaskTypeApproval}},
				}
				updateRun.Status.DeletionStageStatus.BeforeStageTaskStatus = []placementv1beta1.StageTaskStatus{
					{Type: placementv1beta1.StageTaskTypeApproval, ApprovalRequestName: approvalRequestName},
				}
			}
			t.Cleanup(func() { deleteUpdateRunMetrics(updateRun) })

			gotFinished, _, gotErr := r.executeDeleteStage(ctx, updateRun, bindings)
			if !errors.Is(gotErr, tt.wantErr) {
				t.Fatalf("executeDeleteStage() error = %v, want %v", gotErr, tt.wantErr)
			}
			if tt.wantErr == controller.ErrAPIServerError && errors.Is(gotErr, errStagedUpdatedAborted) {
				t.Errorf("executeDeleteStage() error = %v, want a retriable error that does not abort the update run", gotErr)
			}
			if gotFinished {
				t.Errorf("executeDeleteStage() finished = true, want false")
			}
			if len(deletedBindings) != 0 {
				t.Errorf("executeDeleteStage() deleted bindings %v, want none", deletedBindings)
			}
		})
	}
}

// TestExecuteDeleteStage_Namespaced tests that the approval request of the delete stage of a namespaced update run
// is created in the namespace of the update run.
func TestExecuteDeleteStage_Namespaced(t *testing.T) {
	const (
		namespace     = "test-namespace"
		updateRunName = "test-update-run"
	)
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	approvalRequestName := fmt.Sprintf(placementv1beta1.BeforeStageApprovalTaskNameFmt, updateRunName, placementv1beta1.UpdateRunDeleteStageTaskName)
	binding := &placementv1beta1.ResourceBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "test-binding", Namespace: namespace},
		Spec:       placementv1beta1.ResourceBindingSpec{TargetCluster: "cluster-1"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(binding).Build()
	r := &Reconciler{Client: fakeClient}
	updateRun := &placementv1beta1.StagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{Name: updateRunName, Namespace: namespace, Generation: 1},
		Status: placementv1beta1.UpdateRunStatus{
			UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
				DeleteStage: &placementv1beta1.DeleteStageConfig{
					BeforeStageTasks: []placementv1beta1.StageTask{{Type: placementv1beta1.StageTaskTypeApproval}},
				},
			},
			DeletionStageStatus: &placementv1beta1.StageUpdatingStatus{
				StageName: placementv1beta1.UpdateRunDeleteStageName,
				Clusters:  []placementv1beta1.ClusterUpdatingStatus{{ClusterName: "cluster-1"}},
				BeforeStageTaskStatus: []placementv1beta1.StageTaskStatus{
					{Type: placementv1beta1.StageTaskTypeApproval, ApprovalRequestName: approvalRequestName},
				},
			},
		},
	}

	gotFinished, gotWaitTime, err := r.executeDeleteStage(ctx, updateRun, []placementv1beta1.BindingObj{binding})
	if err != nil {
		t.Fatalf("executeDeleteStage() error = %v, want no error", err)
	}
	if gotFinished || gotWaitTime != stageUpdatingWaitTime {
		t.Errorf("executeDeleteStage() = (%v, %v), want (false, %v)", gotFinished, gotWaitTime, stageUpdatingWaitTime)
	}
	gotApprovalRequest := &placementv1beta1.ApprovalRequest{}
	if err := fakeClient.Get(ctx, client.ObjectKey{Name: approvalRequestName, Namespace: namespace}, gotApprovalRequest); err != nil {
		t.Fatalf("Get() approval request = %v, want no error", err)
	}
	wantApprovalRequest := buildApprovalRequestObject(types.NamespacedName{Name: approvalRequestName, Namespace: namespace},
		placementv1beta1.UpdateRunDeleteStageName, updateRunName, placementv1beta1.BeforeStageTaskLabelValue)
	if diff := cmp.Diff(wantApprovalRequest, gotApprovalRequest, cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")); diff != "" {
		t.Errorf("executeDeleteStage() approval request mismatch (-want +got):\n%s", diff)
	}
	if got := gotApprovalRequest.Labels[placementv1beta1.TargetUpdatingStageNameLabel]; got != placementv1beta1.UpdateRunDeleteStageTaskName {
		t.Errorf("executeDeleteStage() approval request stage label = %q, want %q", got, placementv1beta1.UpdateRunDeleteStageTaskName)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(binding), &placementv1beta1.ResourceBinding{}); err != nil {
		t.Errorf("Get() binding = %v, want the binding to be kept", err)
	}
}

// TestExecute_DeleteStageAbort tests that an abort in the delete stage marks the delete stage as failed.
func TestExecute_DeleteStageAbort(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() = %v, want no error", err)
	}
	binding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "test-binding"},
		Spec:       placementv1beta1.ResourceBindingSpec{TargetCluster: "cluster-1"},
	}
	r := &Reconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(binding).Build()}
	// The delete stage does not include the cluster of the binding.
	updateRun := &placementv1beta1.ClusterStagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{Name: "test-update-run", Generation: 1},
		Status: placementv1beta1.UpdateRunStatus{
			UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{},
			DeletionStageStatus: &placementv1beta1.StageUpdatingStatus{
				StageName: placementv1beta1.UpdateRunDeleteStageName,
				Clusters:  []placementv1beta1.ClusterUpdatingStatus{{ClusterName: "another-cluster"}},
			},
		},
	}

	_, _, err := r.execute(ctx, updateRun, 0, nil, []placementv1beta1.BindingObj{binding})
	if !errors.Is(err, errStagedUpdatedAborted) {
		t.Fatalf("execute() error = %v, want %v", err, errStagedUpdatedAborted)
	}
	wantConds := []metav1.Condition{
		{
			Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: 1,
			Reason:             condition.StageUpdatingFailedReason,
		},
		{
			Type:               string(placementv1beta1.StageUpdatingConditionSucceeded),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: 1,
			Reason:             condition.StageUpdatingFailedReason,
		},
	}
	if diff := cmp.Diff(wantConds, updateRun.Status.DeletionStageStatus.Conditions, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message")); diff != "" {
		t.Errorf("execute() delete stage conditions mismatch (-want +got):\n%s", diff)
	}
}

// TestCheckAfterStageTasksStatus_ElapsedTimedWait tests that a timed wait task that has elapsed stays elapsed after
// the stage transitions again, e.g., when the update run is stopped and resumed.
func TestCheckAfterStageTasksStatus_ElapsedTimedWait(t *testing.T) {
	updateRun := &placementv1beta1.ClusterStagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{Name: "test-update-run", Generation: 2},
		Status: placementv1beta1.UpdateRunStatus{
			UpdateStrategySnapshot: &placementv1beta1.UpdateStrategySpec{
				Stages: []placementv1beta1.StageConfig{{
					Name: "stage-1",
					AfterStageTasks: []placementv1beta1.StageTask{
						{Type: placementv1beta1.StageTaskTypeTimedWait, WaitTime: &metav1.Duration{Duration: time.Hour}},
					},
				}},
			},
			StagesStatus: []placementv1beta1.StageUpdatingStatus{{
				StageName: "stage-1",
				Conditions: []metav1.Condition{{
					Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
					Status:             metav1.ConditionFalse,
					ObservedGeneration: 2,
					Reason:             condition.StageUpdatingStoppedReason,
					LastTransitionTime: metav1.Now(),
				}},
				AfterStageTaskStatus: []placementv1beta1.StageTaskStatus{{
					Type: placementv1beta1.StageTaskTypeTimedWait,
					Conditions: []metav1.Condition{{
						Type:               string(placementv1beta1.StageTaskConditionWaitTimeElapsed),
						Status:             metav1.ConditionTrue,
						ObservedGeneration: 1,
						Reason:             condition.AfterStageTaskWaitTimeElapsedReason,
					}},
				}},
			}},
		},
	}
	r := &Reconciler{}

	gotPassed, gotWaitTime, err := r.checkAfterStageTasksStatus(context.Background(), 0, updateRun)
	if err != nil {
		t.Fatalf("checkAfterStageTasksStatus() error = %v, want no error", err)
	}
	if !gotPassed || gotWaitTime != 0 {
		t.Errorf("checkAfterStageTasksStatus() = (%v, %v), want (true, 0)", gotPassed, gotWaitTime)
	}
}
