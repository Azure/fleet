/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package updaterun

import (
	"fmt"
	"testing"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	"go.goms.io/fleet/pkg/utils/controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDetermineUpdatingStage(t *testing.T) {
	updateRun := &placementv1alpha1.ClusterStagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-run",
			Generation: 1,
		},
	}

	tests := []struct {
		name                       string
		curStage                   int
		updatingStageIndex         int
		lastFinishedStageIndex     int
		stageStatus                *placementv1alpha1.StageUpdatingStatus
		wantErr                    error
		wantUpdatingStageIndex     int
		wantLastFinishedStageIndex int
	}{
		{
			name:                   "determineUpdatingStage should return error if some stage finished after the updating stage",
			curStage:               2,
			updatingStageIndex:     1,
			lastFinishedStageIndex: -1,
			stageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "test-stage",
				Conditions: []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionSucceeded)},
			},
			wantErr:                    wrapErr(true, fmt.Errorf("the finished stage `2` is after the updating stage `1`")),
			wantUpdatingStageIndex:     -1,
			wantLastFinishedStageIndex: -1,
		},
		{
			name:                   "determineUpdatingStage should return error if some cluster has not succeeded but the stage has succeeded",
			curStage:               0,
			updatingStageIndex:     -1,
			lastFinishedStageIndex: -1,
			stageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "test-stage",
				Conditions: []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionSucceeded)},
				Clusters: []placementv1alpha1.ClusterUpdatingStatus{
					{
						ClusterName: "cluster-1",
						Conditions:  []metav1.Condition{generateFalseCondition(updateRun, placementv1alpha1.ClusterUpdatingConditionSucceeded)},
					},
				},
			},
			wantErr:                    wrapErr(true, fmt.Errorf("cluster `cluster-1` in the finished stage `test-stage` has not succeeded")),
			wantUpdatingStageIndex:     -1,
			wantLastFinishedStageIndex: -1,
		},
		{
			name:                   "determineUpdatingStage should return error if some cluster has not finished but the stage has succeeded",
			curStage:               0,
			updatingStageIndex:     -1,
			lastFinishedStageIndex: -1,
			stageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "test-stage",
				Conditions: []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionSucceeded)},
				Clusters: []placementv1alpha1.ClusterUpdatingStatus{
					{ClusterName: "cluster-1"},
				},
			},
			wantErr:                    wrapErr(true, fmt.Errorf("cluster `cluster-1` in the finished stage `test-stage` has not succeeded")),
			wantUpdatingStageIndex:     -1,
			wantLastFinishedStageIndex: -1,
		},
		{
			name:                   "determineUpdatingStage should return error if the finished stage is not right after the last finished stage",
			curStage:               2,
			updatingStageIndex:     -1,
			lastFinishedStageIndex: 0,
			stageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "test-stage",
				Conditions: []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionSucceeded)},
			},
			wantErr:                    wrapErr(true, fmt.Errorf("the finished stage `test-stage` is not right after the last finished stage with index `0`")),
			wantUpdatingStageIndex:     -1,
			wantLastFinishedStageIndex: -1,
		},
		{
			name:                   "determineUpdatingStage should return error if some stage has failed",
			curStage:               0,
			updatingStageIndex:     -1,
			lastFinishedStageIndex: -1,
			stageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "test-stage",
				Conditions: []metav1.Condition{generateFalseCondition(updateRun, placementv1alpha1.StageUpdatingConditionSucceeded)},
			},
			wantErr:                    wrapErr(false, fmt.Errorf("the stage `test-stage` has failed, err: ")),
			wantUpdatingStageIndex:     -1,
			wantLastFinishedStageIndex: -1,
		},
		{
			name:                   "determineUpdatingStage should return error if there are multiple stages updating",
			curStage:               1,
			updatingStageIndex:     0,
			lastFinishedStageIndex: -1,
			stageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "test-stage",
				Conditions: []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionProgressing)},
			},
			wantErr:                    wrapErr(true, fmt.Errorf("the stage `test-stage` is updating, but there is already a stage with index `0` updating")),
			wantUpdatingStageIndex:     -1,
			wantLastFinishedStageIndex: -1,
		},
		{
			name:                   "determineUpdatignStage should return error if the updating stage is not right after the last finished stage",
			curStage:               1,
			updatingStageIndex:     -1,
			lastFinishedStageIndex: -1,
			stageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "test-stage",
				Conditions: []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionProgressing)},
			},
			wantErr:                    wrapErr(true, fmt.Errorf("the updating stage `test-stage` is not right after the last finished stage with index `-1`")),
			wantUpdatingStageIndex:     -1,
			wantLastFinishedStageIndex: -1,
		},
		{
			name:                   "determineUpdatignStage should return error if there are multiple clusters updating in an updating stage",
			curStage:               0,
			updatingStageIndex:     -1,
			lastFinishedStageIndex: -1,
			stageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "test-stage",
				Conditions: []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionProgressing)},
				Clusters: []placementv1alpha1.ClusterUpdatingStatus{
					{
						ClusterName: "cluster-1",
						Conditions:  []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.ClusterUpdatingConditionStarted)},
					},
					{
						ClusterName: "cluster-2",
						Conditions:  []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.ClusterUpdatingConditionStarted)},
					},
				},
			},
			wantErr:                    wrapErr(true, fmt.Errorf("more than one cluster is updating in the stage `test-stage`, clusters: [cluster-1 cluster-2]")),
			wantUpdatingStageIndex:     -1,
			wantLastFinishedStageIndex: -1,
		},
		{
			name:                   "determineUpdatingStage should return -1 as the updatingStageIndex if no stage is updating",
			curStage:               0,
			updatingStageIndex:     -1,
			lastFinishedStageIndex: -1,
			stageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName: "test-stage",
			},
			wantErr:                    nil,
			wantUpdatingStageIndex:     -1,
			wantLastFinishedStageIndex: -1,
		},
		{
			name:                   "determineUpdatingStage should return the index of the updating stage in updatingStageIndex",
			curStage:               2,
			updatingStageIndex:     -1,
			lastFinishedStageIndex: 1,
			stageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "test-stage",
				Conditions: []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionProgressing)},
			},
			wantErr:                    nil,
			wantUpdatingStageIndex:     2,
			wantLastFinishedStageIndex: 1,
		},
		{
			name:                   "determineUpdatingStage should return the index of the succeeded stage in lastFinishedStageIndex",
			curStage:               2,
			updatingStageIndex:     -1,
			lastFinishedStageIndex: 1,
			stageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName: "test-stage",
				Conditions: []metav1.Condition{
					generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionProgressing),
					generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionSucceeded),
				},
			},
			wantErr:                    nil,
			wantUpdatingStageIndex:     -1,
			wantLastFinishedStageIndex: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotUpdatingStageIndex, gotLastFinishedStageIndex, err :=
				determineUpdatingStage(test.curStage, test.updatingStageIndex, test.lastFinishedStageIndex, test.stageStatus, updateRun)
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("determineUpdatingStage() got error = %+v, want error = nil", err)
				}
			} else if err == nil || err.Error() != test.wantErr.Error() {
				t.Fatalf("determineUpdatingStage() got error = %+v, want error = %+v", err, test.wantErr)
			}
			if gotUpdatingStageIndex != test.wantUpdatingStageIndex {
				t.Fatalf("determineUpdatingStage() got updatingStageIndex = %d, want updatingStageIndex = %d", gotUpdatingStageIndex, test.wantUpdatingStageIndex)
			}
			if gotLastFinishedStageIndex != test.wantLastFinishedStageIndex {
				t.Fatalf("determineUpdatingStage() got lastFinishedStageIndex = %d, want lastFinishedStageIndex = %d", gotLastFinishedStageIndex, test.wantLastFinishedStageIndex)
			}
		})
	}
}

func TestValidateDeleteStageStatus(t *testing.T) {
	totalStages := 3
	updateRun := &placementv1alpha1.ClusterStagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-run",
			Generation: 1,
		},
	}

	tests := []struct {
		name                   string
		updatingStageIndex     int
		lastFinishedStageIndex int
		deleteStageStatus      *placementv1alpha1.StageUpdatingStatus
		wantErr                error
		wantUpdatingStageIndex int
	}{
		{
			name:                   "validateDeleteStageStatus should return error if delete stage status is nil",
			deleteStageStatus:      nil,
			wantErr:                wrapErr(true, fmt.Errorf("the clusterStagedUpdateRun has nil deletionStageStatus")),
			wantUpdatingStageIndex: -1,
		},
		{
			name:                   "validateDeleteStageStatus should return error if there's stage updating but the delete stage has started",
			updatingStageIndex:     2,
			lastFinishedStageIndex: 1,
			deleteStageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "delete-stage",
				Conditions: []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionProgressing)},
			},
			wantErr:                wrapErr(true, fmt.Errorf("the delete stage is active, but there are still stages updating, updatingStageIndex: 2, lastFinishedStageIndex: 1")),
			wantUpdatingStageIndex: -1,
		},
		{
			name:                   "validateDeleteStageStatus should return error if there's stage not started yet but the delete stage has finished",
			updatingStageIndex:     -1,
			lastFinishedStageIndex: 1, // < totalStages - 1
			deleteStageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "delete-stage",
				Conditions: []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionSucceeded)},
			},
			wantErr:                wrapErr(true, fmt.Errorf("the delete stage is active, but there are still stages updating, updatingStageIndex: -1, lastFinishedStageIndex: 1")),
			wantUpdatingStageIndex: -1,
		},
		{
			name:                   "validateDeleteStageStatus should return error if there's stage not started yet but the delete stage has failed",
			updatingStageIndex:     -1,
			lastFinishedStageIndex: 1, // < totalStages - 1
			deleteStageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "delete-stage",
				Conditions: []metav1.Condition{generateFalseCondition(updateRun, placementv1alpha1.StageUpdatingConditionSucceeded)},
			},
			wantErr:                wrapErr(true, fmt.Errorf("the delete stage is active, but there are still stages updating, updatingStageIndex: -1, lastFinishedStageIndex: 1")),
			wantUpdatingStageIndex: -1,
		},
		{
			name:                   "validateDeleteStageStatus should return the updatingStageIndex if there's still stage updating",
			updatingStageIndex:     2,
			deleteStageStatus:      &placementv1alpha1.StageUpdatingStatus{StageName: "delete-stage"},
			wantErr:                nil,
			wantUpdatingStageIndex: 2,
		},
		{
			name:                   "validateDeleteStageStatus should return the next stage after lastUpdatingStageIndex if there's no stage updating but stage not started yet",
			updatingStageIndex:     -1,
			lastFinishedStageIndex: 0,
			deleteStageStatus:      &placementv1alpha1.StageUpdatingStatus{StageName: "delete-stage"},
			wantErr:                nil,
			wantUpdatingStageIndex: 1,
		},
		{
			name:                   "validateDeleteStageStatus should return -1 if all stages have finished",
			updatingStageIndex:     -1,
			lastFinishedStageIndex: totalStages - 1,
			deleteStageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "delete-stage",
				Conditions: []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionSucceeded)},
			},
			wantErr:                nil,
			wantUpdatingStageIndex: -1,
		},
		{
			name:                   "validateDeleteStageStatus should return error if the delete stage has failed",
			updatingStageIndex:     -1,
			lastFinishedStageIndex: totalStages - 1,
			deleteStageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "delete-stage",
				Conditions: []metav1.Condition{generateFalseCondition(updateRun, placementv1alpha1.StageUpdatingConditionSucceeded)},
			},
			wantErr:                wrapErr(false, fmt.Errorf("the delete stage has failed, err: ")),
			wantUpdatingStageIndex: -1,
		},
		{
			name:                   "validateDeleteStageStatus should return totalStaged if the delete stage is still running",
			updatingStageIndex:     -1,
			lastFinishedStageIndex: totalStages - 1,
			deleteStageStatus: &placementv1alpha1.StageUpdatingStatus{
				StageName:  "delete-stage",
				Conditions: []metav1.Condition{generateTrueCondition(updateRun, placementv1alpha1.StageUpdatingConditionProgressing)},
			},
			wantErr:                nil,
			wantUpdatingStageIndex: totalStages,
		},
		{
			name:                   "validateDeleteStageStatus should return error if all updating stages have finished but the delete stage is not active or finished",
			updatingStageIndex:     -1,
			lastFinishedStageIndex: totalStages - 1,
			deleteStageStatus:      &placementv1alpha1.StageUpdatingStatus{StageName: "delete-stage"},
			wantErr:                wrapErr(true, fmt.Errorf("the delete stage is not active, but all stages finished")),
			wantUpdatingStageIndex: -1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updateRun.Status.DeletionStageStatus = test.deleteStageStatus
			gotUpdatingStageIndex, err := validateDeleteStageStatus(test.updatingStageIndex, test.lastFinishedStageIndex, totalStages, updateRun)
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("validateDeleteStageStatus() got error = %+v, want error = nil", err)
				}
			} else if err == nil || err.Error() != test.wantErr.Error() {
				t.Fatalf("validateDeleteStageStatus() got error = %+v, want error = %+v", err, test.wantErr)
			}
			if gotUpdatingStageIndex != test.wantUpdatingStageIndex {
				t.Fatalf("validateDeleteStageStatus() got updatingStageIndex = %d, want updatingStageIndex = %d", gotUpdatingStageIndex, test.wantUpdatingStageIndex)
			}
		})
	}
}

func wrapErr(unexpected bool, err error) error {
	if unexpected {
		err = controller.NewUnexpectedBehaviorError(err)
	}
	return fmt.Errorf("%w: %s", errStagedUpdatedAborted, err.Error())
}
