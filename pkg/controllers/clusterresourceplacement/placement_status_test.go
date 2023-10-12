/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

var statusCmpOptions = []cmp.Option{
	// ignore the message as we may change the message in the future
	cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime"),
	cmpopts.SortSlices(func(c1, c2 metav1.Condition) bool {
		return c1.Type < c2.Type
	}),
}

func TestSetPlacementStatus(t *testing.T) {
	crpGeneration := int64(25)
	selectedResources := []fleetv1beta1.ResourceIdentifier{
		{
			Group:     "",
			Version:   "v1",
			Kind:      "Service",
			Name:      "svc-name",
			Namespace: "svc-namespace",
		},
	}
	tests := []struct {
		name                   string
		policy                 *fleetv1beta1.PlacementPolicy
		latestPolicySnapshot   *fleetv1beta1.ClusterSchedulingPolicySnapshot
		latestResourceSnapshot *fleetv1beta1.ClusterResourceSnapshot
		wantStatus             *fleetv1beta1.ClusterResourcePlacementStatus
	}{
		{
			name: "empty policy and resource status",
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
					},
				},
			},
			latestResourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:    "0",
						fleetv1beta1.CRPTrackingLabel:      testName,
						fleetv1beta1.IsLatestSnapshotLabel: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources: selectedResources,
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             ApplyPendingReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             schedulingUnknownReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
						Reason:             synchronizePendingReason,
						ObservedGeneration: crpGeneration,
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{},
			},
		},
		{
			name: "unknown status of policy snapshot",
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
					},
					Generation: 1,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: crpGeneration,
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionUnknown,
							Type:               string(fleetv1beta1.PolicySnapshotScheduled),
							Reason:             "Pending",
							Message:            "message",
							ObservedGeneration: 1,
						},
					},
					ClusterDecisions: nil,
				},
			},
			latestResourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:    "0",
						fleetv1beta1.CRPTrackingLabel:      testName,
						fleetv1beta1.IsLatestSnapshotLabel: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources: []fleetv1beta1.ResourceIdentifier{
					{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
				},
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             ApplyPendingReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             schedulingUnknownReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
						Reason:             synchronizePendingReason,
						ObservedGeneration: crpGeneration,
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{},
			},
		},
		{
			name: "scheduler does not report the latest status for policy snapshot (annotation change)",
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
					},
					Generation: 1,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: crpGeneration - 1,
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.PolicySnapshotScheduled),
							Reason:             "Scheduled",
							Message:            "message",
							ObservedGeneration: 1,
						},
					},
					ClusterDecisions: nil,
				},
			},
			latestResourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:    "0",
						fleetv1beta1.CRPTrackingLabel:      testName,
						fleetv1beta1.IsLatestSnapshotLabel: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources: selectedResources,
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             ApplyPendingReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             schedulingUnknownReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
						Reason:             synchronizePendingReason,
						ObservedGeneration: crpGeneration,
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{},
			},
		},
		{
			// should not happen in the production as the policySnapshot is immutable
			name: "scheduler does not report the latest status for policy snapshot and snapshot observation does not match",
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
					},
					Generation: 2,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: crpGeneration,
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.PolicySnapshotScheduled),
							Reason:             "Scheduled",
							Message:            "message",
							ObservedGeneration: 1,
						},
					},
					ClusterDecisions: nil,
				},
			},
			latestResourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:    "0",
						fleetv1beta1.CRPTrackingLabel:      testName,
						fleetv1beta1.IsLatestSnapshotLabel: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources: selectedResources,
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             ApplyPendingReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             schedulingUnknownReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
						Reason:             synchronizePendingReason,
						ObservedGeneration: crpGeneration,
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{},
			},
		},
		{
			name:   "the placement has been scheduled and no works",
			policy: placementPolicyForTest(),
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
					},
					Generation: 1,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: crpGeneration,
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.PolicySnapshotScheduled),
							Reason:             "Scheduled",
							Message:            "message",
							ObservedGeneration: 1,
						},
					},
					ClusterDecisions: []fleetv1beta1.ClusterDecision{
						{
							ClusterName: "member-1",
							Selected:    true,
							Reason:      "success",
						},
						{
							ClusterName: "member-2",
							Selected:    true,
							Reason:      "success",
						},
						{
							ClusterName: "member-3",
							Selected:    true,
							Reason:      "success",
						},
						{
							ClusterName: "member-4",
							Reason:      "failed",
						},
					},
				},
			},
			latestResourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:    "0",
						fleetv1beta1.CRPTrackingLabel:      testName,
						fleetv1beta1.IsLatestSnapshotLabel: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources: selectedResources,
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             ApplyPendingReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
						Reason:             synchronizePendingReason,
						ObservedGeneration: crpGeneration,
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName:              "member-1",
						FailedResourcePlacements: []fleetv1beta1.FailedResourcePlacement{},
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
								Reason:             resourceApplyPendingReason,
								ObservedGeneration: crpGeneration,
							},
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             workSynchronizePendingReason,
								ObservedGeneration: crpGeneration,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             "ScheduleSucceeded",
								ObservedGeneration: crpGeneration,
							},
						},
					},
					{
						ClusterName:              "member-2",
						FailedResourcePlacements: []fleetv1beta1.FailedResourcePlacement{},
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
								Reason:             resourceApplyPendingReason,
								ObservedGeneration: crpGeneration,
							},
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             workSynchronizePendingReason,
								ObservedGeneration: crpGeneration,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             "ScheduleSucceeded",
								ObservedGeneration: crpGeneration,
							},
						},
					},
					{
						ClusterName:              "member-3",
						FailedResourcePlacements: []fleetv1beta1.FailedResourcePlacement{},
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
								Reason:             resourceApplyPendingReason,
								ObservedGeneration: crpGeneration,
							},
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             workSynchronizePendingReason,
								ObservedGeneration: crpGeneration,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             "ScheduleSucceeded",
								ObservedGeneration: crpGeneration,
							},
						},
					},
				},
			},
		},
		{
			name: "the placement has been scheduled for pickAll; none of clusters are selected; no works",
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
					},
					Generation: 1,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: crpGeneration,
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.PolicySnapshotScheduled),
							Reason:             "Scheduled",
							Message:            "message",
							ObservedGeneration: 1,
						},
					},
					ClusterDecisions: []fleetv1beta1.ClusterDecision{
						{
							ClusterName: "member-1",
							Reason:      "failed",
						},
						{
							ClusterName: "member-2",
							Reason:      "failed",
						},
					},
				},
			},
			latestResourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:    "0",
						fleetv1beta1.CRPTrackingLabel:      testName,
						fleetv1beta1.IsLatestSnapshotLabel: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources: selectedResources,
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             resourceApplySucceededReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
						Reason:             synchronizeSucceededReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{},
			},
		},
		{
			name:   "the placement scheduling failed",
			policy: placementPolicyForTest(),
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
					},
					Generation: 1,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: crpGeneration,
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetv1beta1.PolicySnapshotScheduled),
							Reason:             "SchedulingFailed",
							Message:            "message",
							ObservedGeneration: 1,
						},
					},
					ClusterDecisions: []fleetv1beta1.ClusterDecision{
						{
							ClusterName: "member-1",
							Selected:    true,
							Reason:      "success",
						},
						{
							ClusterName: "member-2",
							Selected:    false,
							Reason:      "score is low",
						},
						{
							ClusterName: "member-3",
							Selected:    false,
							Reason:      "score is low",
						},
					},
				},
			},
			latestResourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:    "0",
						fleetv1beta1.CRPTrackingLabel:      testName,
						fleetv1beta1.IsLatestSnapshotLabel: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources: selectedResources,
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             ApplyPendingReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
						Reason:             synchronizePendingReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "SchedulingFailed",
						ObservedGeneration: crpGeneration,
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName: "member-1",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
								Reason:             resourceApplyPendingReason,
								ObservedGeneration: crpGeneration,
							},
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             workSynchronizePendingReason,
								ObservedGeneration: crpGeneration,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             "ScheduleSucceeded",
								ObservedGeneration: crpGeneration,
							},
						},
						FailedResourcePlacements: []fleetv1beta1.FailedResourcePlacement{},
					},
					{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             "ScheduleFailed",
								ObservedGeneration: crpGeneration,
							},
						},
					},
					{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             "ScheduleFailed",
								ObservedGeneration: crpGeneration,
							},
						},
					},
				},
			},
		},
		{
			name: "the placement scheduling succeeded when numberOfClusters is 0",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: pointer.Int32(0),
				Affinity: &fleetv1beta1.Affinity{
					ClusterAffinity: &fleetv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &fleetv1beta1.ClusterSelector{
							ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: metav1.LabelSelector{
										MatchLabels: map[string]string{
											"key1": "value1",
										},
									},
								},
							},
						},
					},
				},
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(0),
					},
					Generation: 1,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: crpGeneration,
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.PolicySnapshotScheduled),
							Reason:             "Scheduled",
							Message:            "message",
							ObservedGeneration: 1,
						},
					},
					ClusterDecisions: []fleetv1beta1.ClusterDecision{
						{
							ClusterName: "member-1",
							Selected:    false,
							Reason:      "filtered",
						},
						{
							ClusterName: "member-2",
							Selected:    false,
							Reason:      "filtered",
						},
					},
				},
			},
			latestResourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:    "0",
						fleetv1beta1.CRPTrackingLabel:      testName,
						fleetv1beta1.IsLatestSnapshotLabel: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testName,
							BlockOwnerDeletion: pointer.Bool(true),
							Controller:         pointer.Bool(true),
							APIVersion:         fleetAPIVersion,
							Kind:               "ClusterResourcePlacement",
						},
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "0",
					},
				},
			},
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources: selectedResources,
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             resourceApplySucceededReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementSynchronizedConditionType),
						Reason:             synchronizeSucceededReason,
						ObservedGeneration: crpGeneration,
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{},
			},
		},
		// TODO add more tests
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			crp := &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testName,
				},
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Service",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"region": "east"},
							},
						},
					},
					Policy: tc.policy,
				},
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()
			r := Reconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}
			crp.Generation = crpGeneration
			if err := r.setPlacementStatus(context.Background(), crp, selectedResources, tc.latestPolicySnapshot, tc.latestResourceSnapshot); err != nil {
				t.Fatalf("setPlacementStatus() failed: %v", err)
			}

			if diff := cmp.Diff(tc.wantStatus, &crp.Status, statusCmpOptions...); diff != "" {
				t.Errorf("buildPlacementStatus() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestBuildFailedResourcePlacements(t *testing.T) {
	tests := map[string]struct {
		work          *fleetv1beta1.Work
		wantIsPending bool
		wantRes       []fleetv1beta1.FailedResourcePlacement
	}{
		"pending if not applied": {
			work:          &fleetv1beta1.Work{},
			wantIsPending: true,
			wantRes:       nil,
		},
		"No resource if applied successfully": {
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							ObservedGeneration: 1,
							Status:             metav1.ConditionTrue,
						},
					},
				},
			},
			wantIsPending: false,
			wantRes:       nil,
		},
		"pending if applied not on the latest generation": {
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.WorkConditionTypeApplied),
							ObservedGeneration: 1,
							Status:             metav1.ConditionTrue,
						},
					},
				},
			},
			wantIsPending: true,
			wantRes:       nil,
		},
		"pending if applied failed with multiple object": {
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							ObservedGeneration: 1,
							Status:             metav1.ConditionFalse,
						},
					},
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   1,
								Group:     corev1.GroupName,
								Version:   "v1",
								Kind:      "secret",
								Name:      "secretName",
								Namespace: "app",
							},
							Conditions: []metav1.Condition{
								{
									Type:               fleetv1beta1.WorkConditionTypeApplied,
									ObservedGeneration: 1,
									Status:             metav1.ConditionFalse,
								},
							},
						},
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   1,
								Group:     corev1.GroupName,
								Version:   "v1",
								Kind:      "pod",
								Name:      "secretPod",
								Namespace: "app",
							},
							Conditions: []metav1.Condition{
								{
									Type:               fleetv1beta1.WorkConditionTypeApplied,
									ObservedGeneration: 2,
									Status:             metav1.ConditionFalse,
								},
							},
						},
					},
				},
			},
			wantIsPending: false,
			wantRes: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     corev1.GroupName,
						Version:   "v1",
						Kind:      "secret",
						Name:      "secretName",
						Namespace: "app",
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						ObservedGeneration: 1,
						Status:             metav1.ConditionFalse,
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     corev1.GroupName,
						Version:   "v1",
						Kind:      "pod",
						Name:      "secretPod",
						Namespace: "app",
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						ObservedGeneration: 2,
						Status:             metav1.ConditionFalse,
					},
				},
			},
		},
		"pending if applied failed with an envelop object": {
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
					Labels: map[string]string{
						fleetv1beta1.ParentBindingLabel:     "bindingName",
						fleetv1beta1.CRPTrackingLabel:       "testCRPName",
						fleetv1beta1.EnvelopeTypeLabel:      string(fleetv1beta1.ConfigMapEnvelopeType),
						fleetv1beta1.EnvelopeNameLabel:      "envelop-configmap",
						fleetv1beta1.EnvelopeNamespaceLabel: "app",
					},
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							ObservedGeneration: 1,
							Status:             metav1.ConditionFalse,
						},
					},
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   1,
								Group:     corev1.GroupName,
								Version:   "v1",
								Kind:      "secret",
								Name:      "secretName",
								Namespace: "app",
							},
							Conditions: []metav1.Condition{
								{
									Type:               fleetv1beta1.WorkConditionTypeApplied,
									ObservedGeneration: 1,
									Status:             metav1.ConditionFalse,
								},
							},
						},
					},
				},
			},
			wantIsPending: false,
			wantRes: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     corev1.GroupName,
						Version:   "v1",
						Kind:      "secret",
						Name:      "secretName",
						Namespace: "app",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "envelop-configmap",
							Namespace: "app",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{

						Type:               fleetv1beta1.WorkConditionTypeApplied,
						ObservedGeneration: 1,
						Status:             metav1.ConditionFalse,
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			gotIsPending, gotRes := buildFailedResourcePlacements(tt.work)
			if tt.wantIsPending != gotIsPending {
				t.Errorf("buildFailedResourcePlacements `%s` mismatch, want: %t, got : %t", name, tt.wantIsPending, gotIsPending)
			}
			if diff := cmp.Diff(tt.wantRes, gotRes, statusCmpOptions...); diff != "" {
				t.Errorf("buildFailedResourcePlacements `%s` status mismatch (-want, +got):\n%s", name, diff)
			}
		})
	}
}
