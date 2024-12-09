/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
)

var statusCmpOptions = []cmp.Option{
	// ignore the message as we may change the message in the future
	cmpopts.IgnoreFields(metav1.Condition{}, "Message"),
	cmpopts.SortSlices(func(c1, c2 metav1.Condition) bool {
		return c1.Type < c2.Type
	}),
	cmpopts.SortSlices(func(s1, s2 string) bool {
		return s1 < s2
	}),
	cmpopts.SortSlices(func(n1, n2 fleetv1beta1.NamespacedName) bool {
		if n1.Namespace == n2.Namespace {
			return n1.Name < n2.Name
		}
		return n1.Namespace < n2.Namespace
	}),
	cmpopts.SortSlices(func(f1, f2 fleetv1beta1.FailedResourcePlacement) bool {
		return f1.ResourceIdentifier.Kind < f2.ResourceIdentifier.Kind
	}),
	cmp.Comparer(func(t1, t2 metav1.Time) bool {
		if t1.Time.IsZero() || t2.Time.IsZero() {
			return true // treat them as equal
		}
		if t1.Time.After(t2.Time) {
			t1, t2 = t2, t1 // ensure t1 is always before t2
		}
		// we're within the margin (10s) if x + margin >= y
		return !t1.Time.Add(10 * time.Second).Before(t2.Time)
	}),
}

func TestSetPlacementStatus(t *testing.T) {
	currentTime := time.Now()
	oldTransitionTime := metav1.NewTime(currentTime.Add(-1 * time.Hour))

	crpGeneration := int64(25)
	selectedResources := []fleetv1beta1.ResourceIdentifier{
		{
			Group:     "",
			Version:   "v1",
			Kind:      "Service",
			Name:      "svc-name",
			Namespace: "svc-namespace",
		},
		{
			Group:     "",
			Version:   "v1",
			Kind:      "Deployment",
			Name:      "deployment-name",
			Namespace: "deployment-namespace",
		},
		{
			Group:     "",
			Version:   "v1",
			Kind:      "ConfigMap",
			Name:      "config-name",
			Namespace: "config-namespace",
		},
	}
	tests := []struct {
		name                    string
		crpStatus               fleetv1beta1.ClusterResourcePlacementStatus
		policy                  *fleetv1beta1.PlacementPolicy
		latestPolicySnapshot    *fleetv1beta1.ClusterSchedulingPolicySnapshot
		latestResourceSnapshot  *fleetv1beta1.ClusterResourceSnapshot
		clusterResourceBindings []fleetv1beta1.ClusterResourceBinding
		want                    bool
		wantStatus              *fleetv1beta1.ClusterResourcePlacementStatus
		wantErr                 error
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: false,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             SchedulingUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: false,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             SchedulingUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: false,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             SchedulingUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: false,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             SchedulingUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{},
			},
		},
		{
			name:   "the placement has been scheduled and no clusterResourcebindings and works",
			policy: placementPolicyForTest(),
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName: "member-1",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName: "member-2",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName: "member-3",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		{
			// TODO special handling no cluster is selected
			name: "the placement has been scheduled for pickAll; none of clusters are selected; no clusterResourceBindings and works",
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: false,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "SchedulingFailed",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName: "member-1",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             ResourceScheduleFailedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             ResourceScheduleFailedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		// TODO special handling when selected cluster is 0
		{
			name: "the placement scheduling succeeded when numberOfClusters is 0",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(0)),
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
							BlockOwnerDeletion: ptr.To(true),
							Controller:         ptr.To(true),
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
							BlockOwnerDeletion: ptr.To(true),
							Controller:         ptr.To(true),
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
			want: false,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{},
			},
		},
		{
			name: "the placement is completed with clusterResourceBindings and works",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(1)),
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(1),
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			clusterResourceBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						ResourceOverrideSnapshots: []fleetv1beta1.NamespacedName{
							{
								Name:      "override-1",
								Namespace: "override-ns",
							},
							{
								Name: "override-2",
							},
						},
						ClusterResourceOverrideSnapshots: []string{"o-1", "o-2"},
						SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						TargetCluster:                    "member-1",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingOverridden),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingApplied),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingAvailable),
								Reason:             condition.AvailableReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
						Reason:             condition.AvailableReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName:                        "member-1",
						ApplicableClusterResourceOverrides: []string{"o-1", "o-2"},
						ApplicableResourceOverrides: []fleetv1beta1.NamespacedName{
							{
								Name:      "override-1",
								Namespace: "override-ns",
							},
							{
								Name: "override-2",
							},
						},
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourcesAvailableConditionType),
								Reason:             condition.AvailableReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		{
			name: "the placement is completed with clusterResourceBindings and works (no overrides)",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(1)),
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(1),
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			clusterResourceBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						TargetCluster:                "member-1",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingOverridden),
								Reason:             condition.OverrideNotSpecifiedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingApplied),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingAvailable),
								Reason:             condition.AvailableReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-2",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						TargetCluster:                "member-2",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingOverridden),
								Reason:             condition.OverrideNotSpecifiedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingApplied),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingAvailable),
								Reason:             condition.AvailableReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
						Reason:             condition.AvailableReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
						Reason:             condition.OverrideNotSpecifiedReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName: "member-1",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourcesAvailableConditionType),
								Reason:             condition.AvailableReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
								Reason:             condition.OverrideNotSpecifiedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName: "member-2",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourcesAvailableConditionType),
								Reason:             condition.AvailableReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
								Reason:             condition.OverrideNotSpecifiedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		{
			name: "one of the placement condition is unknown with multiple bindings",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(7)),
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(1),
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
							Selected:    true,
							Reason:      "success",
						},
						{
							ClusterName: "member-5",
							Selected:    true,
							Reason:      "success",
						},
						{
							ClusterName: "member-6",
							Selected:    true,
							Reason:      "success",
						},
						{
							ClusterName: "member-7",
							Selected:    true,
							Reason:      "success",
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			clusterResourceBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "deleting-binding",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						Generation:        1,
						DeletionTimestamp: &metav1.Time{Time: time.Date(00002, time.January, 1, 1, 1, 1, 1, time.UTC)},
						Finalizers:        []string{"dummy-finalizer"},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						TargetCluster:                "member-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "not-latest-binding-with-old-observed-generation",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						TargetCluster:                "member-2",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingOverridden),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: 0,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-with-unknown-condition",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						TargetCluster:                "member-3",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingOverridden),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
								Reason:             condition.WorkSynchronizedUnknownReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
				// missing member-4 binding
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-with-nil-condition",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						TargetCluster:                "member-5",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingOverridden),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "not-having-latest-resource-binding",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         "not-latest-resource-snapshot",
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						TargetCluster:                "member-6",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingOverridden),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-without-latest-policy-snapshot",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						SchedulingPolicySnapshotName: "not-latest-policy-snapshot",
						TargetCluster:                "member-7",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName: "member-1",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName: "member-2",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
								Reason:             condition.OverriddenPendingReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName: "member-3",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName: "member-4",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName: "member-5",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
								Reason:             condition.ApplyPendingReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName: "member-6",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName: "member-7",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		{
			name: "placement rollout started condition false",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(1)),
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(1),
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			clusterResourceBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-with-not-latest-resource-snapshot",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         "not-latest",
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						TargetCluster:                "member-1",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Reason:             condition.RolloutNotStartedYetReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutNotStartedYetReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName: "member-1",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutNotStartedYetReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		{
			name: "placement apply condition false",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(2)),
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(1),
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			clusterResourceBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-with-false-apply-and-works",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						TargetCluster:                "member-1",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingOverridden),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceBindingApplied),
								Reason:             condition.ApplyFailedReason,
								ObservedGeneration: 1,
							},
						},
						FailedPlacements: []fleetv1beta1.FailedResourcePlacement{
							{
								ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Condition: metav1.Condition{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionFalse,
								},
							},
							{
								ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
									Envelope: &fleetv1beta1.EnvelopeIdentifier{
										Name:      "test-env",
										Namespace: "test-env-ns",
										Type:      "pod",
									},
								},
								Condition: metav1.Condition{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-with-false-work-created",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						ResourceOverrideSnapshots: []fleetv1beta1.NamespacedName{
							{
								Name:      "override-1",
								Namespace: "override-ns",
							},
							{
								Name: "override-2",
							},
						},
						ClusterResourceOverrideSnapshots: []string{"o-1", "o-2"},
						SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						TargetCluster:                    "member-2",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingOverridden),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingApplied),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             condition.ApplyFailedReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName: "member-1",
						FailedPlacements: []fleetv1beta1.FailedResourcePlacement{
							{
								ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Condition: metav1.Condition{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionFalse,
								},
							},
							{
								ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
									Envelope: &fleetv1beta1.EnvelopeIdentifier{
										Name:      "test-env",
										Namespace: "test-env-ns",
										Type:      "pod",
									},
								},
								Condition: metav1.Condition{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionFalse,
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
								Reason:             condition.ApplyFailedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:                        "member-2",
						ApplicableClusterResourceOverrides: []string{"o-1", "o-2"},
						ApplicableResourceOverrides: []fleetv1beta1.NamespacedName{
							{
								Name:      "override-1",
								Namespace: "override-ns",
							},
							{
								Name: "override-2",
							},
						},
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourcesAvailableConditionType),
								Reason:             condition.AvailableUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		{
			name: "placement available condition false",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(1)),
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(1),
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			clusterResourceBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-with-false-available-and-works",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
						TargetCluster:                "member-1",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingOverridden),
								Reason:             condition.OverrideNotSpecifiedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingApplied),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceBindingAvailable),
								Reason:             condition.NotAvailableYetReason,
								ObservedGeneration: 1,
							},
						},
						FailedPlacements: []fleetv1beta1.FailedResourcePlacement{
							{
								ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Condition: metav1.Condition{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionFalse,
								},
							},
							{
								ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
									Envelope: &fleetv1beta1.EnvelopeIdentifier{
										Name:      "test-env",
										Namespace: "test-env-ns",
										Type:      "pod",
									},
								},
								Condition: metav1.Condition{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
						Reason:             condition.NotAvailableYetReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
						Reason:             condition.OverrideNotSpecifiedReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName: "member-1",
						FailedPlacements: []fleetv1beta1.FailedResourcePlacement{
							{
								ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Condition: metav1.Condition{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionFalse,
								},
							},
							{
								ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
									Envelope: &fleetv1beta1.EnvelopeIdentifier{
										Name:      "test-env",
										Namespace: "test-env-ns",
										Type:      "pod",
									},
								},
								Condition: metav1.Condition{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionFalse,
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourcesAvailableConditionType),
								Reason:             condition.NotAvailableYetReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
								Reason:             condition.OverrideNotSpecifiedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		{
			name: "update the CRP and it rollout status becomes unknown (reset the existing conditions)",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(1)),
			},
			crpStatus: fleetv1beta1.ClusterResourcePlacementStatus{
				ObservedResourceIndex: "-1",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: 1,
						LastTransitionTime: oldTransitionTime,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
						Reason:             condition.AvailableReason,
						ObservedGeneration: 1,
						LastTransitionTime: oldTransitionTime,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: 1,
						LastTransitionTime: oldTransitionTime,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: 1,
						LastTransitionTime: oldTransitionTime,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: 1,
						LastTransitionTime: oldTransitionTime,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: 1,
						LastTransitionTime: oldTransitionTime,
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName:                        "member-1",
						ApplicableClusterResourceOverrides: []string{"o-1", "o-2"},
						ApplicableResourceOverrides: []fleetv1beta1.NamespacedName{
							{
								Name:      "override-1",
								Namespace: "override-ns",
							},
							{
								Name: "override-2",
							},
						},
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourcesAvailableConditionType),
								Reason:             condition.AvailableReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
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
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(1),
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: oldTransitionTime,
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName: "member-1",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: oldTransitionTime,
							},
						},
					},
				},
			},
		},
		{
			name: "the placement cannot be fulfilled for picFixed",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType: fleetv1beta1.PickFixedPlacementType,
				ClusterNames: []string{
					"member-1",
					"unselected-cluster",
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
					Generation: 1,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: crpGeneration,
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetv1beta1.PolicySnapshotScheduled),
							Reason:             "Failed",
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
							ClusterName: "unselected-cluster",
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Failed",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName: "member-1",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             ResourceScheduleFailedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		{
			name: "the placement cannot be fulfilled for pickN",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(3)),
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:      "0",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      testName,
					},
					Generation: 1,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: crpGeneration,
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetv1beta1.PolicySnapshotScheduled),
							Reason:             "Failed",
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
							ClusterName: "unselected-cluster",
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Failed",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName: "member-1",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             ResourceScheduleFailedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		{
			name: "the placement cannot be fulfilled for pickN (reset existing status)",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(3)),
			},
			crpStatus: fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: 1,
						LastTransitionTime: oldTransitionTime,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
						Reason:             condition.AvailableReason,
						ObservedGeneration: 1,
						LastTransitionTime: oldTransitionTime,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: 1,
						LastTransitionTime: oldTransitionTime,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: oldTransitionTime,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: 1,
						LastTransitionTime: oldTransitionTime,
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName: "member-1",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourcesAvailableConditionType),
								Reason:             condition.AvailableReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
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
					Generation: 1,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: crpGeneration,
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetv1beta1.PolicySnapshotScheduled),
							Reason:             "Failed",
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
							ClusterName: "unselected-cluster",
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
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.ClusterResourcePlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Failed",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PlacementStatuses: []fleetv1beta1.ResourcePlacementStatus{
					{
						ClusterName: "member-1",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: oldTransitionTime,
							},
						},
					},
					{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceScheduledConditionType),
								Reason:             ResourceScheduleFailedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
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
				Status: tc.crpStatus,
			}
			scheme := serviceScheme(t)
			var objects []client.Object
			for i := range tc.clusterResourceBindings {
				objects = append(objects, &tc.clusterResourceBindings[i])
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}
			crp.Generation = crpGeneration
			got, err := r.setPlacementStatus(context.Background(), crp, selectedResources, tc.latestPolicySnapshot, tc.latestResourceSnapshot)
			if gotErr, wantErr := err != nil, tc.wantErr != nil; gotErr != wantErr || !errors.Is(err, tc.wantErr) {
				t.Fatalf("setPlacementStatus() got error %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if got != tc.want {
				t.Errorf("setPlacementStatus() = %v, want %v", got, tc.want)
			}

			if diff := cmp.Diff(tc.wantStatus, &crp.Status, statusCmpOptions...); diff != "" {
				t.Errorf("setPlacementStatus() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestBuildResourcePlacementStatusMap(t *testing.T) {
	tests := []struct {
		name   string
		status []fleetv1beta1.ResourcePlacementStatus
		want   map[string][]metav1.Condition
	}{
		{
			name:   "empty status",
			status: []fleetv1beta1.ResourcePlacementStatus{},
			want:   map[string][]metav1.Condition{},
		},
		{
			name:   "nil status",
			status: nil,
			want:   map[string][]metav1.Condition{},
		},
		{
			name: "contain unselected cluster status",
			status: []fleetv1beta1.ResourcePlacementStatus{
				{
					Conditions: []metav1.Condition{
						{
							Type: "any",
						},
					},
				},
			},
			want: map[string][]metav1.Condition{},
		},
		{
			name: "the status of the selected cluster is not set",
			status: []fleetv1beta1.ResourcePlacementStatus{
				{
					ClusterName: "member-1",
					Conditions:  []metav1.Condition{},
				},
			},
			want: map[string][]metav1.Condition{},
		},
		{
			name: "the status of the selected clusters are set",
			status: []fleetv1beta1.ResourcePlacementStatus{
				{
					ClusterName: "member-1",
					Conditions: []metav1.Condition{
						{
							Type: "any",
						},
					},
				},
				{
					ClusterName: "member-2",
					Conditions: []metav1.Condition{
						{
							Type: "other",
						},
					},
				},
			},
			want: map[string][]metav1.Condition{
				"member-1": {
					{
						Type: "any",
					},
				},
				"member-2": {
					{
						Type: "other",
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			crp := fleetv1beta1.ClusterResourcePlacement{
				Status: fleetv1beta1.ClusterResourcePlacementStatus{
					PlacementStatuses: tc.status,
				},
			}
			got := buildResourcePlacementStatusMap(&crp)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildResourcePlacementStatusMap() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestBuildClusterResourceBindings(t *testing.T) {
	policySnapshotName := "policy-2"
	tests := []struct {
		name     string
		bindings []fleetv1beta1.ClusterResourceBinding
		want     map[string]*fleetv1beta1.ClusterResourceBinding
	}{
		{
			name: "no associated bindings",
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-binding",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: "other-crp",
						},
					},
				},
			},
			want: map[string]*fleetv1beta1.ClusterResourceBinding{},
		},
		{
			name: "deleting binding",
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "deleting-binding",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
						DeletionTimestamp: &metav1.Time{Time: time.Date(00002, time.January, 1, 1, 1, 1, 1, time.UTC)},
						Finalizers:        []string{"dummy-finalizer"},
					},
				},
			},
			want: map[string]*fleetv1beta1.ClusterResourceBinding{},
		},
		{
			name: "binding having stale policy snapshot",
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-without-latest-policy-snapshot",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: "not-latest-policy-snapshot",
						TargetCluster:                "member-7",
					},
				},
			},
			want: map[string]*fleetv1beta1.ClusterResourceBinding{},
		},
		{
			name: "matched bindings",
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-2",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-2",
					},
				},
			},
			want: map[string]*fleetv1beta1.ClusterResourceBinding{
				"member-1": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-1",
					},
				},
				"member-2": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-2",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-2",
					},
				},
			},
		},
		{
			name: "invalid binding with missing target cluster",
			bindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-without-latest-policy-snapshot",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel: testName,
						},
					},
				},
			},
			want: map[string]*fleetv1beta1.ClusterResourceBinding{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			crp := fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testName,
				},
			}
			policySnapshot := fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policySnapshotName,
				},
			}
			scheme := serviceScheme(t)
			var objects []client.Object
			for i := range tc.bindings {
				objects = append(objects, &tc.bindings[i])
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client: fakeClient,
			}
			got, err := r.buildClusterResourceBindings(ctx, &crp, &policySnapshot)
			if err != nil {
				t.Fatalf("buildClusterResourceBindings() got err %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")); diff != "" {
				t.Errorf("buildClusterResourceBindings() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestSetResourcePlacementStatusPerCluster(t *testing.T) {
	resourceSnapshotName := "snapshot-1"
	cluster := "member-1"
	bindingName := "binding-1"
	tests := []struct {
		name       string
		binding    *fleetv1beta1.ClusterResourceBinding
		want       []metav1.ConditionStatus
		wantStatus fleetv1beta1.ResourcePlacementStatus
	}{
		{
			name:    "binding not found",
			binding: nil,
			want:    []metav1.ConditionStatus{metav1.ConditionUnknown},
			wantStatus: fleetv1beta1.ResourcePlacementStatus{
				ClusterName: cluster,
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: crpGeneration,
					},
				},
			},
		},
		{
			name: "stale binding with false rollout started condition",
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: "not-latest",
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Reason:             condition.RolloutNotStartedYetReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			want: []metav1.ConditionStatus{metav1.ConditionFalse},
			wantStatus: fleetv1beta1.ResourcePlacementStatus{
				ClusterName: cluster,
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
						Reason:             condition.RolloutNotStartedYetReason,
						ObservedGeneration: crpGeneration,
					},
				},
			},
		},
		{
			name: "stale binding with true rollout started condition",
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: "not-latest",
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							ObservedGeneration: 1,
						},
					},
				},
			},
			want: []metav1.ConditionStatus{metav1.ConditionUnknown},
			wantStatus: fleetv1beta1.ResourcePlacementStatus{
				ClusterName: cluster,
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: crpGeneration,
					},
				},
			},
		},
		{
			name: "completed binding",
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: resourceSnapshotName,
					ResourceOverrideSnapshots: []fleetv1beta1.NamespacedName{
						{
							Name:      "override-1",
							Namespace: "override-ns",
						},
						{
							Name: "override-2",
						},
					},
					ClusterResourceOverrideSnapshots: []string{"o-1", "o-2"},
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					TargetCluster:                    cluster,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingOverridden),
							Reason:             condition.OverriddenSucceededReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
							Reason:             condition.WorkSynchronizedReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Reason:             condition.ApplySucceededReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingAvailable),
							Reason:             condition.AvailableReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			want: []metav1.ConditionStatus{
				metav1.ConditionTrue,
				metav1.ConditionTrue,
				metav1.ConditionTrue,
				metav1.ConditionTrue,
				metav1.ConditionTrue,
			},
			wantStatus: fleetv1beta1.ResourcePlacementStatus{
				ClusterName:                        cluster,
				ApplicableClusterResourceOverrides: []string{"o-1", "o-2"},
				ApplicableResourceOverrides: []fleetv1beta1.NamespacedName{
					{
						Name:      "override-1",
						Namespace: "override-ns",
					},
					{
						Name: "override-2",
					},
				},
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourcesAvailableConditionType),
						Reason:             condition.AvailableReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: crpGeneration,
					},
				},
			},
		},
		{
			name: "unknown rollout started condition",
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: resourceSnapshotName,
					TargetCluster:        cluster,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							ObservedGeneration: 0,
						},
					},
				},
			},
			want: []metav1.ConditionStatus{
				metav1.ConditionUnknown,
			},
			wantStatus: fleetv1beta1.ResourcePlacementStatus{
				ClusterName: cluster,
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: crpGeneration,
					},
				},
			},
		},
		{
			name: "false overridden condition",
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: resourceSnapshotName,
					ResourceOverrideSnapshots: []fleetv1beta1.NamespacedName{
						{
							Name:      "override-1",
							Namespace: "override-ns",
						},
						{
							Name: "override-2",
						},
					},
					ClusterResourceOverrideSnapshots: []string{"o-1", "o-2"},
					TargetCluster:                    cluster,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetv1beta1.ResourceBindingOverridden),
							Reason:             condition.OverriddenFailedReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			want: []metav1.ConditionStatus{
				metav1.ConditionTrue,
				metav1.ConditionFalse,
			},
			wantStatus: fleetv1beta1.ResourcePlacementStatus{
				ClusterName:                        cluster,
				ApplicableClusterResourceOverrides: []string{"o-1", "o-2"},
				ApplicableResourceOverrides: []fleetv1beta1.NamespacedName{
					{
						Name:      "override-1",
						Namespace: "override-ns",
					},
					{
						Name: "override-2",
					},
				},
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
						Reason:             condition.OverriddenFailedReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: crpGeneration,
					},
				},
			},
		},
		{
			name: "unknown work created condition",
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: resourceSnapshotName,
					ResourceOverrideSnapshots: []fleetv1beta1.NamespacedName{
						{
							Name:      "override-1",
							Namespace: "override-ns",
						},
						{
							Name: "override-2",
						},
					},
					ClusterResourceOverrideSnapshots: []string{"o-1", "o-2"},
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					TargetCluster:                    cluster,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingOverridden),
							Reason:             condition.OverriddenSucceededReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionUnknown,
							Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
							Reason:             condition.WorkSynchronizedUnknownReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			want: []metav1.ConditionStatus{
				metav1.ConditionTrue,
				metav1.ConditionTrue,
				metav1.ConditionUnknown,
			},
			wantStatus: fleetv1beta1.ResourcePlacementStatus{
				ClusterName:                        cluster,
				ApplicableClusterResourceOverrides: []string{"o-1", "o-2"},
				ApplicableResourceOverrides: []fleetv1beta1.NamespacedName{
					{
						Name:      "override-1",
						Namespace: "override-ns",
					},
					{
						Name: "override-2",
					},
				},
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedUnknownReason,
						ObservedGeneration: crpGeneration,
					},
				},
			},
		},
		{
			name: "false applied condition",
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: resourceSnapshotName,
					ResourceOverrideSnapshots: []fleetv1beta1.NamespacedName{
						{
							Name:      "override-1",
							Namespace: "override-ns",
						},
						{
							Name: "override-2",
						},
					},
					ClusterResourceOverrideSnapshots: []string{"o-1", "o-2"},
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					TargetCluster:                    cluster,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					FailedPlacements: []fleetv1beta1.FailedResourcePlacement{
						{
							ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "config-name",
								Namespace: "config-namespace",
							},
							Condition: metav1.Condition{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingOverridden),
							Reason:             condition.OverriddenSucceededReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
							Reason:             condition.WorkSynchronizedReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Reason:             condition.ApplyFailedReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			want: []metav1.ConditionStatus{
				metav1.ConditionTrue,
				metav1.ConditionTrue,
				metav1.ConditionTrue,
				metav1.ConditionFalse,
			},
			wantStatus: fleetv1beta1.ResourcePlacementStatus{
				ClusterName:                        cluster,
				ApplicableClusterResourceOverrides: []string{"o-1", "o-2"},
				ApplicableResourceOverrides: []fleetv1beta1.NamespacedName{
					{
						Name:      "override-1",
						Namespace: "override-ns",
					},
					{
						Name: "override-2",
					},
				},
				FailedPlacements: []fleetv1beta1.FailedResourcePlacement{
					{
						ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
							Group:     "",
							Version:   "v1",
							Kind:      "ConfigMap",
							Name:      "config-name",
							Namespace: "config-namespace",
						},
						Condition: metav1.Condition{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionFalse,
						},
					},
				},
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
						Reason:             condition.ApplyFailedReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: crpGeneration,
					},
				},
			},
		},
		{
			name: "false available condition",
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: resourceSnapshotName,
					ResourceOverrideSnapshots: []fleetv1beta1.NamespacedName{
						{
							Name:      "override-1",
							Namespace: "override-ns",
						},
						{
							Name: "override-2",
						},
					},
					ClusterResourceOverrideSnapshots: []string{"o-1", "o-2"},
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					TargetCluster:                    cluster,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					FailedPlacements: []fleetv1beta1.FailedResourcePlacement{
						{
							ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "config-name",
								Namespace: "config-namespace",
							},
							Condition: metav1.Condition{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionFalse,
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingOverridden),
							Reason:             condition.OverriddenSucceededReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
							Reason:             condition.WorkSynchronizedReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Reason:             condition.ApplySucceededReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetv1beta1.ResourceBindingAvailable),
							Reason:             condition.NotAvailableYetReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			want: []metav1.ConditionStatus{
				metav1.ConditionTrue,
				metav1.ConditionTrue,
				metav1.ConditionTrue,
				metav1.ConditionTrue,
				metav1.ConditionFalse,
			},
			wantStatus: fleetv1beta1.ResourcePlacementStatus{
				ClusterName:                        cluster,
				ApplicableClusterResourceOverrides: []string{"o-1", "o-2"},
				ApplicableResourceOverrides: []fleetv1beta1.NamespacedName{
					{
						Name:      "override-1",
						Namespace: "override-ns",
					},
					{
						Name: "override-2",
					},
				},
				FailedPlacements: []fleetv1beta1.FailedResourcePlacement{
					{
						ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
							Group:     "",
							Version:   "v1",
							Kind:      "ConfigMap",
							Name:      "config-name",
							Namespace: "config-namespace",
						},
						Condition: metav1.Condition{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionFalse,
						},
					},
				},
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourcesAppliedConditionType),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ResourcesAvailableConditionType),
						Reason:             condition.NotAvailableYetReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: crpGeneration,
					},
				},
			},
		},
		{
			name: "drifts and configuration diffs",
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName:             resourceSnapshotName,
					ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{},
					ClusterResourceOverrideSnapshots: []string{},
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testName, 0),
					TargetCluster:                    cluster,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					FailedPlacements: []fleetv1beta1.FailedResourcePlacement{
						{
							ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "cm-1",
								Namespace: "ns-1",
							},
							Condition: metav1.Condition{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
							},
						},
					},
					DriftedPlacements: []fleetv1beta1.DriftedResourcePlacement{
						{
							ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "cm-1",
								Namespace: "ns-1",
							},
							ObservationTime:                 metav1.Time{Time: time.Now()},
							TargetClusterObservedGeneration: 1,
							FirstDriftedObservedTime:        metav1.Time{Time: time.Now()},
							ObservedDrifts: []fleetv1beta1.PatchDetail{
								{
									Path:          "/data",
									ValueInMember: "k=1",
									ValueInHub:    "k=2",
								},
							},
						},
					},
					DiffedPlacements: []fleetv1beta1.DiffedResourcePlacement{
						{
							ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
								Group:     "apps",
								Version:   "v1",
								Kind:      "Deployment",
								Name:      "app-1",
								Namespace: "ns-1",
							},
							ObservationTime:                 metav1.Time{Time: time.Now()},
							TargetClusterObservedGeneration: ptr.To(int64(2)),
							FirstDiffedObservedTime:         metav1.Time{Time: time.Now()},
							ObservedDiffs: []fleetv1beta1.PatchDetail{
								{
									Path:          "/spec/replicas",
									ValueInMember: "1",
									ValueInHub:    "2",
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingOverridden),
							Reason:             condition.OverriddenSucceededReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
							Reason:             condition.WorkSynchronizedReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Reason:             condition.ApplySucceededReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetv1beta1.ResourceBindingAvailable),
							Reason:             condition.NotAvailableYetReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			want: []metav1.ConditionStatus{
				metav1.ConditionTrue,
				metav1.ConditionTrue,
				metav1.ConditionTrue,
				metav1.ConditionFalse,
			},
			wantStatus: fleetv1beta1.ResourcePlacementStatus{
				ClusterName:                        cluster,
				ApplicableResourceOverrides:        []fleetv1beta1.NamespacedName{},
				ApplicableClusterResourceOverrides: []string{},
				FailedPlacements: []fleetv1beta1.FailedResourcePlacement{
					{
						ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
							Group:     "",
							Version:   "v1",
							Kind:      "ConfigMap",
							Name:      "cm-1",
							Namespace: "ns-1",
						},
						Condition: metav1.Condition{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionFalse,
						},
					},
				},
				DriftedPlacements: []fleetv1beta1.DriftedResourcePlacement{
					{
						ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
							Group:     "",
							Version:   "v1",
							Kind:      "ConfigMap",
							Name:      "cm-1",
							Namespace: "ns-1",
						},
						ObservationTime:                 metav1.Time{Time: time.Now()},
						TargetClusterObservedGeneration: 1,
						FirstDriftedObservedTime:        metav1.Time{Time: time.Now()},
						ObservedDrifts: []fleetv1beta1.PatchDetail{
							{
								Path:          "/data",
								ValueInMember: "k=1",
								ValueInHub:    "k=2",
							},
						},
					},
				},
				DiffedPlacements: []fleetv1beta1.DiffedResourcePlacement{
					{
						ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
							Group:     "apps",
							Version:   "v1",
							Kind:      "Deployment",
							Name:      "app-1",
							Namespace: "ns-1",
						},
						ObservationTime:                 metav1.Time{Time: time.Now()},
						TargetClusterObservedGeneration: ptr.To(int64(2)),
						FirstDiffedObservedTime:         metav1.Time{Time: time.Now()},
						ObservedDiffs: []fleetv1beta1.PatchDetail{
							{
								Path:          "/spec/replicas",
								ValueInMember: "1",
								ValueInHub:    "2",
							},
						},
					},
				},
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingOverridden),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: crpGeneration,
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ResourceBindingApplied),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: crpGeneration,
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			crp := &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testName,
					Generation: crpGeneration,
				},
			}
			resourceSnapshot := &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceSnapshotName,
				},
			}
			r := Reconciler{
				Recorder: record.NewFakeRecorder(10),
			}
			status := fleetv1beta1.ResourcePlacementStatus{ClusterName: cluster}
			got, err := r.setResourcePlacementStatusPerCluster(crp, resourceSnapshot, tc.binding, &status)
			if err != nil {
				t.Fatalf("setResourcePlacementStatusPerCluster() got err %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("setResourcePlacementStatusPerCluster() conditionStatus mismatch (-want, +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantStatus, status, statusCmpOptions...); diff != "" {
				t.Errorf("setResourcePlacementStatusPerCluster() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
