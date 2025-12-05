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
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
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
			name: "checkClusterUpdateResult should return false and error if the binding has false workSynchronized condition",
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
	tests := []struct {
		name           string
		namespacedName types.NamespacedName
		stageName      string
		updateRunName  string
		want           placementv1beta1.ApprovalRequestObj
	}{
		{
			name: "should create ClusterApprovalRequest when namespace is empty",
			namespacedName: types.NamespacedName{
				Name:      "test-approval-request",
				Namespace: "",
			},
			stageName:     "test-stage",
			updateRunName: "test-update-run",
			want: &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-approval-request",
					Labels: map[string]string{
						placementv1beta1.TargetUpdatingStageNameLabel:   "test-stage",
						placementv1beta1.TargetUpdateRunLabel:           "test-update-run",
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
				Name:      "test-approval-request",
				Namespace: "test-namespace",
			},
			stageName:     "test-stage",
			updateRunName: "test-update-run",
			want: &placementv1beta1.ApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-approval-request",
					Namespace: "test-namespace",
					Labels: map[string]string{
						placementv1beta1.TargetUpdatingStageNameLabel:   "test-stage",
						placementv1beta1.TargetUpdateRunLabel:           "test-update-run",
						placementv1beta1.IsLatestUpdateRunApprovalLabel: "true",
					},
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: "test-update-run",
					TargetStage:     "test-stage",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildApprovalRequestObject(test.namespacedName, test.stageName, test.updateRunName)

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
