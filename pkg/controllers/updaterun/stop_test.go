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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
)

func TestStopUpdatingStage(t *testing.T) {
	tests := []struct {
		name             string
		updateRun        *placementv1beta1.ClusterStagedUpdateRun
		bindings         []placementv1beta1.BindingObj
		wantErr          error
		wantFinished     bool
		wantWaitTime     time.Duration
		wantProgressCond metav1.Condition
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
											Type:               string(placementv1beta1.ClusterUpdatingConditionStarted),
											Status:             metav1.ConditionTrue,
											ObservedGeneration: 1,
											Reason:             condition.ClusterUpdatingStartedReason,
										},
										{
											Type:               string(placementv1beta1.ClusterUpdatingConditionSucceeded),
											Status:             metav1.ConditionFalse,
											ObservedGeneration: 1,
											Reason:             condition.ClusterUpdatingFailedReason,
										},
									},
								},
							},
						},
					},
				},
			},
			bindings:     nil,
			wantFinished: true,
			wantErr:      nil,
			wantWaitTime: 0,
			wantProgressCond: metav1.Condition{
				Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				Reason:             condition.StageUpdatingStoppedReason,
			},
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
			wantErr:      errors.New("cluster updating encountered an error at stage"),
			wantFinished: false,
			wantWaitTime: 0,
			wantProgressCond: metav1.Condition{
				Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 1,
				Reason:             condition.StageUpdatingStoppingReason,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = placementv1beta1.AddToScheme(scheme)
			objs := make([]client.Object, len(tt.bindings))
			for i := range tt.bindings {
				objs[i] = tt.bindings[i]
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			r := &Reconciler{
				Client: fakeClient,
			}

			// Stop the stage.
			finished, waitTime, gotErr := r.stopUpdatingStage(tt.updateRun, 0, tt.bindings)

			// Verify error expectation.
			if (tt.wantErr != nil) != (gotErr != nil) {
				t.Fatalf("stopUpdatingStage() want error: %v, got error: %v", tt.wantErr, gotErr)
			}

			// Verify error message contains expected substring.
			if tt.wantErr != nil && gotErr != nil {
				if !strings.Contains(gotErr.Error(), tt.wantErr.Error()) {
					t.Fatalf("stopUpdatingStage() want error: %v, got error: %v", tt.wantErr, gotErr)
				}
			}

			// Verify finished result.
			if finished != tt.wantFinished {
				t.Fatalf("stopUpdatingStage() want finished: %v, got finished: %v", tt.wantFinished, finished)
			}

			// Verify wait time.
			if waitTime != tt.wantWaitTime {
				t.Fatalf("stopUpdatingStage() want waitTime: %v, got waitTime: %v", tt.wantWaitTime, waitTime)
			}

			// Verify progressing condition.
			progressingCond := meta.FindStatusCondition(
				tt.updateRun.Status.StagesStatus[0].Conditions,
				string(placementv1beta1.StageUpdatingConditionProgressing),
			)
			if diff := cmp.Diff(tt.wantProgressCond, *progressingCond, cmpOptions...); diff != "" {
				t.Errorf("stopUpdatingStage() status mismatch: (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStopDeleteStage(t *testing.T) {
	now := metav1.Now()
	deletionTime := metav1.NewTime(now.Add(-1 * time.Minute))

	tests := []struct {
		name                string
		updateRun           *placementv1beta1.ClusterStagedUpdateRun
		toBeDeletedBindings []placementv1beta1.BindingObj
		wantFinished        bool
		wantError           error
		wantProgressCond    metav1.Condition
	}{
		{
			name: "no bindings to delete - should finish and mark stage as stopped",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-updaterun",
					Generation: 1,
				},
				Status: placementv1beta1.UpdateRunStatus{
					DeletionStageStatus: &placementv1beta1.StageUpdatingStatus{
						StageName: "deletion",
						Clusters:  []placementv1beta1.ClusterUpdatingStatus{},
					},
				},
			},
			toBeDeletedBindings: []placementv1beta1.BindingObj{},
			wantFinished:        true,
			wantError:           nil,
			wantProgressCond: metav1.Condition{
				Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				Reason:             condition.StageUpdatingStoppedReason,
			},
		},
		{
			name: "cluster being deleted with proper binding deletion timestamp - should not finish",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-updaterun",
					Generation: 1,
				},
				Status: placementv1beta1.UpdateRunStatus{
					DeletionStageStatus: &placementv1beta1.StageUpdatingStatus{
						StageName: "deletion",
						Clusters: []placementv1beta1.ClusterUpdatingStatus{
							{
								ClusterName: "cluster-1",
								Conditions: []metav1.Condition{
									{
										Type:               string(placementv1beta1.ClusterUpdatingConditionStarted),
										Status:             metav1.ConditionTrue,
										ObservedGeneration: 1,
										LastTransitionTime: now,
										Reason:             condition.ClusterUpdatingStartedReason,
									},
								},
							},
						},
					},
				},
			},
			toBeDeletedBindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						DeletionTimestamp: &deletionTime,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
			wantFinished: false,
			wantError:    nil,
			wantProgressCond: metav1.Condition{
				Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 1,
				Reason:             condition.StageUpdatingStoppingReason,
			},
		},
		{
			name: "cluster marked as deleting but binding not deleting - should abort",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-updaterun",
					Generation: 1,
				},
				Status: placementv1beta1.UpdateRunStatus{
					DeletionStageStatus: &placementv1beta1.StageUpdatingStatus{
						StageName: "deletion",
						Clusters: []placementv1beta1.ClusterUpdatingStatus{
							{
								ClusterName: "cluster-1",
								Conditions: []metav1.Condition{
									{
										Type:               string(placementv1beta1.ClusterUpdatingConditionStarted),
										Status:             metav1.ConditionTrue,
										ObservedGeneration: 1,
										LastTransitionTime: now,
										Reason:             condition.ClusterUpdatingStartedReason,
									},
								},
							},
						},
					},
				},
			},
			toBeDeletedBindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						// No DeletionTimestamp set
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
			wantFinished: false,
			wantError:    errors.New("the cluster `cluster-1` in the deleting stage is marked as deleting but its corresponding binding is not deleting"),
			wantProgressCond: metav1.Condition{
				Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 1,
				Reason:             condition.StageUpdatingStoppingReason,
			},
		},
		{
			name: "cluster not marked as deleting and binding not deleting",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-updaterun",
					Generation: 1,
				},
				Status: placementv1beta1.UpdateRunStatus{
					DeletionStageStatus: &placementv1beta1.StageUpdatingStatus{
						StageName: "deletion",
						Clusters: []placementv1beta1.ClusterUpdatingStatus{
							{
								ClusterName: "cluster-1",
							},
						},
					},
				},
			},
			toBeDeletedBindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						// No DeletionTimestamp set
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
			wantFinished: false,
			wantError:    nil,
			wantProgressCond: metav1.Condition{
				Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				Reason:             condition.StageUpdatingStoppedReason,
			},
		},
		{
			name: "multiple clusters with mixed states",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-updaterun",
					Generation: 1,
				},
				Status: placementv1beta1.UpdateRunStatus{
					DeletionStageStatus: &placementv1beta1.StageUpdatingStatus{
						StageName: "deletion",
						Clusters: []placementv1beta1.ClusterUpdatingStatus{
							{
								ClusterName: "cluster-1",
								Conditions: []metav1.Condition{
									{
										Type:               string(placementv1beta1.ClusterUpdatingConditionStarted),
										Status:             metav1.ConditionTrue,
										ObservedGeneration: 1,
										LastTransitionTime: now,
										Reason:             condition.ClusterUpdatingStartedReason,
									},
									{
										Type:               string(placementv1beta1.ClusterUpdatingConditionSucceeded),
										Status:             metav1.ConditionTrue,
										ObservedGeneration: 1,
										LastTransitionTime: now,
										Reason:             condition.ClusterUpdatingSucceededReason,
									},
								},
							},
							{
								ClusterName: "cluster-2",
								Conditions: []metav1.Condition{
									{
										Type:               string(placementv1beta1.ClusterUpdatingConditionStarted),
										Status:             metav1.ConditionTrue,
										ObservedGeneration: 1,
										LastTransitionTime: now,
										Reason:             condition.ClusterUpdatingStartedReason,
									},
								},
							},
						},
					},
				},
			},
			toBeDeletedBindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						DeletionTimestamp: &deletionTime,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
			wantFinished: false,
			wantError:    nil,
			wantProgressCond: metav1.Condition{
				Type:               string(placementv1beta1.StageUpdatingConditionProgressing),
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 1,
				Reason:             condition.StageUpdatingStoppingReason,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Reconciler{}

			gotFinished, gotErr := r.stopDeleteStage(tt.updateRun, tt.toBeDeletedBindings)

			// Check finished result.
			if gotFinished != tt.wantFinished {
				t.Errorf("stopDeleteStage() finished = %v, want %v", gotFinished, tt.wantFinished)
			}

			// Verify error expectation.
			if (tt.wantError != nil) != (gotErr != nil) {
				t.Fatalf("stopUpdatingStage() want error: %v, got error: %v", tt.wantError, gotErr)
			}

			// Verify error message contains expected substring.
			if tt.wantError != nil && gotErr != nil {
				if !strings.Contains(gotErr.Error(), tt.wantError.Error()) {
					t.Fatalf("stopUpdatingStage() want error: %v, got error: %v", tt.wantError, gotErr)
				}
			}

			// Check stage status condition.
			progressingCond := meta.FindStatusCondition(
				tt.updateRun.Status.DeletionStageStatus.Conditions,
				string(placementv1beta1.StageUpdatingConditionProgressing),
			)
			if diff := cmp.Diff(tt.wantProgressCond, *progressingCond, cmpOptions...); diff != "" {
				t.Errorf("stopDeleteStage() status mismatch: (-want +got):\n%s", diff)
			}
		})
	}
}
