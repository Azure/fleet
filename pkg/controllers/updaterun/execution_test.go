/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package updaterun

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
)

func TestIsBindingSyncedWithClusterStatus(t *testing.T) {
	tests := []struct {
		name      string
		updateRun *placementv1beta1.ClusterStagedUpdateRun
		binding   *placementv1beta1.ClusterResourceBinding
		cluster   *placementv1beta1.ClusterUpdatingStatus
		wantEqual bool
	}{
		{
			name: "isBindingSyncedWithClusterStatus should return false if binding and updateRun have different resourceSnapshot",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				Spec: placementv1beta1.StagedUpdateRunSpec{
					ResourceSnapshotIndex: "test-snapshot",
				},
			},
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: "test-snapshot-1",
				},
			},
			wantEqual: false,
		},
		{
			name: "isBindingSyncedWithClusterStatus should return false if binding and cluster status have different resourceOverrideSnapshot list",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				Spec: placementv1beta1.StagedUpdateRunSpec{
					ResourceSnapshotIndex: "test-snapshot",
				},
			},
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: "test-snapshot",
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
			name: "isBindingSyncedWithClusterStatus should return false if binding and cluster status have different clusterResourceOverrideSnapshot list",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				Spec: placementv1beta1.StagedUpdateRunSpec{
					ResourceSnapshotIndex: "test-snapshot",
				},
			},
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: "test-snapshot",
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
			name: "isBindingSyncedWithClusterStatus should return false if binding and updateRun have different applyStrategy",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				Spec: placementv1beta1.StagedUpdateRunSpec{
					ResourceSnapshotIndex: "test-snapshot",
				},
				Status: placementv1beta1.StagedUpdateRunStatus{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type: placementv1beta1.ApplyStrategyTypeClientSideApply,
					},
				},
			},
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: "test-snapshot",
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
			name: "isBindingSyncedWithClusterStatus should return true if resourceSnapshot, applyStrategy, and override lists are all deep equal",
			updateRun: &placementv1beta1.ClusterStagedUpdateRun{
				Spec: placementv1beta1.StagedUpdateRunSpec{
					ResourceSnapshotIndex: "test-snapshot",
				},
				Status: placementv1beta1.StagedUpdateRunStatus{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type: placementv1beta1.ApplyStrategyTypeReportDiff,
					},
				},
			},
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: "test-snapshot",
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
			got := isBindingSyncedWithClusterStatus(test.updateRun, test.binding, test.cluster)
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
