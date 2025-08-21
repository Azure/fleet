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

package placement

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

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/test/utils/resource"
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

func TestSetPlacementStatusForClusterResourcePlacement(t *testing.T) {
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

	oldClusterResourcePlacementAvailableConditions := []metav1.Condition{
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
			Reason:             condition.OverrideNotSpecifiedReason,
			ObservedGeneration: crpGeneration - 1,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
			Reason:             condition.RolloutStartedReason,
			ObservedGeneration: crpGeneration - 1,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
			Reason:             "Scheduled",
			ObservedGeneration: crpGeneration - 1,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
			Reason:             condition.WorkSynchronizedReason,
			ObservedGeneration: crpGeneration - 1,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
			Reason:             condition.ApplySucceededReason,
			ObservedGeneration: crpGeneration - 1,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
			Reason:             condition.AvailableReason,
			ObservedGeneration: crpGeneration - 1,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
	}

	clusterResourcePlacementAvailableConditions := []metav1.Condition{
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
	}

	oldResourcePlacementAvailableConditions := []metav1.Condition{
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
			Reason:             condition.OverrideNotSpecifiedReason,
			ObservedGeneration: crpGeneration - 1,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
			Reason:             condition.RolloutStartedReason,
			ObservedGeneration: crpGeneration - 1,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
			Reason:             condition.ScheduleSucceededReason,
			ObservedGeneration: crpGeneration - 1,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
			Reason:             condition.WorkSynchronizedReason,
			ObservedGeneration: crpGeneration - 1,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
			Reason:             condition.ApplySucceededReason,
			ObservedGeneration: crpGeneration - 1,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
			Reason:             condition.AvailableReason,
			ObservedGeneration: crpGeneration - 1,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
	}

	resourcePlacementAvailableConditions := []metav1.Condition{
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
			Reason:             condition.OverrideNotSpecifiedReason,
			ObservedGeneration: crpGeneration,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
			Reason:             condition.RolloutStartedReason,
			ObservedGeneration: crpGeneration,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
			Reason:             condition.ScheduleSucceededReason,
			ObservedGeneration: crpGeneration,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
			Reason:             condition.WorkSynchronizedReason,
			ObservedGeneration: crpGeneration,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
			Reason:             condition.ApplySucceededReason,
			ObservedGeneration: crpGeneration,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
			Reason:             condition.AvailableReason,
			ObservedGeneration: crpGeneration,
			LastTransitionTime: metav1.NewTime(currentTime),
		},
	}

	bindingAvailableConditions := []metav1.Condition{
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
	}

	tests := []struct {
		name                    string
		crpStatus               fleetv1beta1.PlacementStatus
		policy                  *fleetv1beta1.PlacementPolicy
		strategy                fleetv1beta1.RolloutStrategy
		latestPolicySnapshot    *fleetv1beta1.ClusterSchedulingPolicySnapshot
		latestResourceSnapshot  *fleetv1beta1.ClusterResourceSnapshot
		otherResourceSnapshots  []*fleetv1beta1.ClusterResourceSnapshot
		clusterResourceBindings []fleetv1beta1.ClusterResourceBinding
		want                    bool
		wantStatus              *fleetv1beta1.PlacementStatus
		wantErr                 error
	}{
		{
			name: "empty policy and resource status",
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
					},
				},
			},
			latestResourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: false,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             condition.SchedulingUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{},
			},
		},
		{
			name: "unknown status of policy snapshot",
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: false,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             condition.SchedulingUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{},
			},
		},
		{
			name: "scheduler does not report the latest status for policy snapshot (annotation change)",
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: false,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             condition.SchedulingUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{},
			},
		},
		{
			// should not happen in the production as the policySnapshot is immutable
			name: "scheduler does not report the latest status for policy snapshot and snapshot observation does not match",
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: false,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             condition.SchedulingUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{},
			},
		},
		{
			name:   "the placement has been scheduled and no clusterResourcebindings and works",
			policy: placementPolicyForTest(),
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "", // Empty as there's no binding created.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:           "member-2",
						ObservedResourceIndex: "", // Empty as there's no binding created.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:           "member-3",
						ObservedResourceIndex: "", // Empty as there's no binding created.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
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
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: false,
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{},
			},
		},
		{
			// TODO special handling no cluster is selected
			name: "the placement uses External rollout strategy; none of clusters are selected; no clusterResourceBindings and works",
			strategy: fleetv1beta1.RolloutStrategy{
				Type: fleetv1beta1.ExternalRolloutStrategyType,
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: false,
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{},
			},
		},
		{
			name:   "the placement scheduling failed",
			policy: placementPolicyForTest(),
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "", // Empty as there's no binding created.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ObservedResourceIndex: "", // Empty as schedule failed.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ResourceScheduleFailedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ObservedResourceIndex: "", // Empty as schedule failed.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ResourceScheduleFailedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
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
				NumberOfClusters: ptr.To(int32(0)),
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testCRPName,
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
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{},
			},
		},

		{
			name: "the placement scheduling succeeded when numberOfClusters is 0 but user actually wants to select some",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(3)),
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testCRPName,
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
							Status:             metav1.ConditionFalse,
							Type:               string(fleetv1beta1.PolicySnapshotScheduled),
							Reason:             "Scheduled not met",
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:               testCRPName,
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
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled not met",
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ResourceScheduleFailedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
								Message:            "filtered",
							},
						},
					},
					{
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ResourceScheduleFailedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
								Message:            "filtered",
							},
						},
					},
				},
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
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
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
						SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:                        "member-1",
						ObservedResourceIndex:              "0",
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
								Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
								Reason:             condition.AvailableReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
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
				NumberOfClusters: ptr.To(int32(2)),
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-1",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: bindingAvailableConditions,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-2",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: bindingAvailableConditions,
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
								Reason:             condition.AvailableReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverrideNotSpecifiedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:           "member-2",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
								Reason:             condition.AvailableReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverrideNotSpecifiedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
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
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation:        1,
						DeletionTimestamp: &metav1.Time{Time: time.Date(00002, time.January, 1, 1, 1, 1, 1, time.UTC)},
						Finalizers:        []string{"dummy-finalizer"},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "not-latest-binding-with-old-observed-generation",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         "not-latest-resource-snapshot",
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
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
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "", // Empty as the binding is deleting.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:           "member-2",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenPendingReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:           "member-3",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:           "member-4",
						ObservedResourceIndex: "", // Empty as there is no binding.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:           "member-5",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
								Reason:             condition.ApplyPendingReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:           "member-6",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:           "member-7",
						ObservedResourceIndex: "", // Empty as the binding does not have latest policy snapshot.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
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
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         "not-latest",
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutNotStartedYetReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
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
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
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
						SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
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
								Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
								Reason:             condition.ApplyFailedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:                        "member-2",
						ObservedResourceIndex:              "0",
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
								Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
								Reason:             condition.AvailableUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
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
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
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
								Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
								Reason:             condition.NotAvailableYetReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverrideNotSpecifiedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
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
			crpStatus: fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:                        "member-1",
						ObservedResourceIndex:              "-1",
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
								Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
								Reason:             condition.AvailableReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
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
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "", // Empty as there's no binding.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
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
			name: "the placement cannot be fulfilled for pickFixed",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType: fleetv1beta1.PickFixedPlacementType,
				ClusterNames: []string{
					"member-1",
					"unselected-cluster",
				},
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "", // Empty as there's no binding.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ObservedResourceIndex: "", // Empty as the cluster is not selected.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ResourceScheduleFailedReason,
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
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "", // Empty as there's no binding.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ObservedResourceIndex: "", // Empty as the cluster is not selected.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ResourceScheduleFailedReason,
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
			crpStatus: fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
								Reason:             condition.ApplySucceededReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
								Reason:             condition.AvailableReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: 1,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: oldTransitionTime,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
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
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
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
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "", // Empty as there's no binding.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: oldTransitionTime,
							},
						},
					},
					{
						ObservedResourceIndex: "", // Empty as the cluster is not selected.
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ResourceScheduleFailedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		{
			name: "ReportDiff apply strategy, all diff reported",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(2)),
			},
			strategy: fleetv1beta1.RolloutStrategy{
				ApplyStrategy: &fleetv1beta1.ApplyStrategy{
					Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
				},
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
						Name: "binding-diff-reported-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-1",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
						},
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
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingDiffReported),
								Reason:             condition.DiffReportedStatusTrueReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-diff-reported-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
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
						SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                    "member-2",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
						},
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
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingDiffReported),
								Reason:             condition.DiffReportedStatusTrueReason,
								ObservedGeneration: 1,
							},
						},
						DiffedPlacements: []fleetv1beta1.DiffedResourcePlacement{
							{
								ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								ObservationTime:         metav1.Time{Time: currentTime},
								FirstDiffedObservedTime: metav1.Time{Time: currentTime},
								ObservedDiffs: []fleetv1beta1.PatchDetail{
									{
										Path:       "/",
										ValueInHub: "(the whole object)",
									},
								},
							},
						},
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
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
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementDiffReportedConditionType),
						Reason:             condition.DiffReportedStatusTrueReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterDiffReportedConditionType),
								Reason:             condition.DiffReportedStatusTrueReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:                        "member-2",
						ObservedResourceIndex:              "0",
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
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterDiffReportedConditionType),
								Reason:             condition.DiffReportedStatusTrueReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
						DiffedPlacements: []fleetv1beta1.DiffedResourcePlacement{
							{
								ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								ObservationTime:         metav1.Time{Time: currentTime},
								FirstDiffedObservedTime: metav1.Time{Time: currentTime},
								ObservedDiffs: []fleetv1beta1.PatchDetail{
									{
										Path:       "/",
										ValueInHub: "(the whole object)",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "ReportDiff apply strategy, one cluster has not reported diff yet",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(2)),
			},
			strategy: fleetv1beta1.RolloutStrategy{
				ApplyStrategy: &fleetv1beta1.ApplyStrategy{
					Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
				},
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
						Name: "binding-diff-reported-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-1",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
						},
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
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingDiffReported),
								Reason:             condition.DiffReportedStatusTrueReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-diff-reported-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
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
						SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                    "member-2",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
						},
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
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
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
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementDiffReportedConditionType),
						Reason:             condition.DiffReportedStatusUnknownReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterDiffReportedConditionType),
								Reason:             condition.DiffReportedStatusTrueReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:                        "member-2",
						ObservedResourceIndex:              "0",
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
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterDiffReportedConditionType),
								Reason:             condition.DiffReportedStatusUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		{
			name: "ReportDiff apply strategy, one cluster has failed to report diff",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(2)),
			},
			strategy: fleetv1beta1.RolloutStrategy{
				ApplyStrategy: &fleetv1beta1.ApplyStrategy{
					Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
				},
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
						Name: "binding-diff-reported-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-1",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
						},
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
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingDiffReported),
								Reason:             condition.DiffReportedStatusTrueReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-diff-reported-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
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
						SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                    "member-2",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
						},
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
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceBindingDiffReported),
								Reason:             condition.DiffReportedStatusFalseReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
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
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ClusterResourcePlacementDiffReportedConditionType),
						Reason:             condition.DiffReportedStatusFalseReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterDiffReportedConditionType),
								Reason:             condition.DiffReportedStatusTrueReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:                        "member-2",
						ObservedResourceIndex:              "0",
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
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceBindingDiffReported),
								Reason:             condition.DiffReportedStatusFalseReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		{
			name: "ReportDiff apply strategy, one cluster has failed to synchronized work",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(2)),
			},
			strategy: fleetv1beta1.RolloutStrategy{
				ApplyStrategy: &fleetv1beta1.ApplyStrategy{
					Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
				},
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
						Name: "binding-diff-reported-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-1",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
						},
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
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingDiffReported),
								Reason:             condition.DiffReportedStatusTrueReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-diff-reported-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
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
						SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                    "member-2",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
						},
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
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
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
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
						Reason:             condition.WorkNotSynchronizedYetReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterDiffReportedConditionType),
								Reason:             condition.DiffReportedStatusTrueReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
					{
						ClusterName:                        "member-2",
						ObservedResourceIndex:              "0",
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
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverriddenSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
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
			name: "Removing Applied/Available condition from status as apply strategy has changed",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(1)),
			},
			strategy: fleetv1beta1.RolloutStrategy{
				ApplyStrategy: &fleetv1beta1.ApplyStrategy{
					Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
				},
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
						Name: "binding-diff-reported-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-1",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
						},
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
								Status:             metav1.ConditionFalse,
								Type:               string(fleetv1beta1.ResourceBindingApplied),
								Reason:             condition.ApplyFailedReason,
								ObservedGeneration: 1,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.ResourceBindingDiffReported),
								Reason:             condition.DiffReportedStatusTrueReason,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: true,
			crpStatus: fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions:            oldClusterResourcePlacementAvailableConditions,
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName: "member-1",
						Conditions:  oldResourcePlacementAvailableConditions,
					},
				},
			},
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
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
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementDiffReportedConditionType),
						Reason:             condition.DiffReportedStatusTrueReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverrideNotSpecifiedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterDiffReportedConditionType),
								Reason:             condition.DiffReportedStatusTrueReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
		},
		{
			name: "Removing DiffReported condition from status as apply strategy has changed",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(1)),
			},
			strategy: fleetv1beta1.RolloutStrategy{
				ApplyStrategy: &fleetv1beta1.ApplyStrategy{
					Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
				},
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
						Name: "binding-diff-reported-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-1",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: bindingAvailableConditions,
					},
				},
			},
			want: true,
			crpStatus: fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
						Reason:             condition.OverrideNotSpecifiedReason,
						ObservedGeneration: crpGeneration - 1,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: crpGeneration - 1,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: crpGeneration - 1,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: crpGeneration - 1,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementDiffReportedConditionType),
						Reason:             condition.DiffReportedStatusTrueReason,
						ObservedGeneration: crpGeneration - 1,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
								Reason:             condition.OverrideNotSpecifiedReason,
								ObservedGeneration: crpGeneration - 1,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedReason,
								ObservedGeneration: crpGeneration - 1,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             condition.ScheduleSucceededReason,
								ObservedGeneration: crpGeneration - 1,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
								Reason:             condition.WorkSynchronizedReason,
								ObservedGeneration: crpGeneration - 1,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterDiffReportedConditionType),
								Reason:             condition.DiffReportedStatusTrueReason,
								ObservedGeneration: crpGeneration - 1,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
						},
					},
				},
			},
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
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
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions:            resourcePlacementAvailableConditions,
					},
				},
			},
		},
		{
			name: "update the placement has External rollout strategy and all bindings are scheduled and pending rollout",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(1)),
			},
			strategy: fleetv1beta1.RolloutStrategy{
				Type: fleetv1beta1.ExternalRolloutStrategyType,
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
						Name: "binding-with-empty-snapshot-name",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         "",
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-1",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{},
					},
				},
			},
			want: true,
			crpStatus: fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions:            oldClusterResourcePlacementAvailableConditions,
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions:            oldResourcePlacementAvailableConditions,
					},
				},
			},
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     []fleetv1beta1.ResourceIdentifier{},
				ObservedResourceIndex: "",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutControlledByExternalControllerReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             condition.ScheduleSucceededReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
								LastTransitionTime: metav1.NewTime(currentTime),
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
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
			// Simulate the scenario where rollout is first happened to a CRP with External rollout strategy.
			name: "placement has External rollout strategy and rollout only reaches to some of the clusters",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(2)),
			},
			strategy: fleetv1beta1.RolloutStrategy{
				Type: fleetv1beta1.ExternalRolloutStrategyType,
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
						Name: "binding-rolled-out",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-1",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: bindingAvailableConditions,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-with-empty-snapshot-name",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         "",
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-2",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{},
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     []fleetv1beta1.ResourceIdentifier{},
				ObservedResourceIndex: "", // Empty as not all bindings have the resource snapshot name.
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutControlledByExternalControllerReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             condition.ScheduleSucceededReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions:            resourcePlacementAvailableConditions,
					},
					{
						ClusterName:           "member-2",
						ObservedResourceIndex: "",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: crpGeneration,
							},
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
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
			// Simulate the scenario where a new version is being rolled out for CRP with External rollout strategy.
			name: "placement has External rollout strategy and is still rolling out and cluster observe different resource indices",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(2)),
			},
			strategy: fleetv1beta1.RolloutStrategy{
				Type: fleetv1beta1.ExternalRolloutStrategyType,
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(2),
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "1",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			otherResourceSnapshots: []*fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
				},
			},
			crpStatus: fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions:            oldClusterResourcePlacementAvailableConditions,
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions:            oldResourcePlacementAvailableConditions,
					},
					{
						ClusterName:           "member-2",
						ObservedResourceIndex: "0",
						Conditions:            oldResourcePlacementAvailableConditions,
					},
				},
			},
			clusterResourceBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-rolled-out-with-latest-snapshot-name",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-1",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: bindingAvailableConditions,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-with-old-snapshot-name",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-2",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: bindingAvailableConditions,
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     []fleetv1beta1.ResourceIdentifier{},
				ObservedResourceIndex: "", // Empty as not all bindings have the same resource snapshot name.
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutControlledByExternalControllerReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
						Reason:             condition.ScheduleSucceededReason,
						ObservedGeneration: crpGeneration,
						LastTransitionTime: metav1.NewTime(currentTime),
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "1",
						Conditions:            resourcePlacementAvailableConditions,
					},
					{
						ClusterName:           "member-2",
						ObservedResourceIndex: "0",
						Conditions:            resourcePlacementAvailableConditions,
					},
				},
			},
		},
		{
			// Simulate the scenario where rollout has completed on CRP with External rollout strategy.
			name: "placement has External rollout strategy and all clusters are rolled out to the latest resource snapshot",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(2)),
			},
			strategy: fleetv1beta1.RolloutStrategy{
				Type: fleetv1beta1.ExternalRolloutStrategyType,
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(2),
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "1",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			crpStatus: fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions:            oldClusterResourcePlacementAvailableConditions,
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions:            oldResourcePlacementAvailableConditions,
					},
					{
						ClusterName:           "member-2",
						ObservedResourceIndex: "0",
						Conditions:            oldResourcePlacementAvailableConditions,
					},
				},
			},
			clusterResourceBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-rolled-out-with-latest-snapshot-name",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-1",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: bindingAvailableConditions,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-rolled-out-with-latest-snapshot-name-too",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-2",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: bindingAvailableConditions,
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "1",
				Conditions:            clusterResourcePlacementAvailableConditions,
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "1",
						Conditions:            resourcePlacementAvailableConditions,
					},
					{
						ClusterName:           "member-2",
						ObservedResourceIndex: "1",
						Conditions:            resourcePlacementAvailableConditions,
					},
				},
			},
		},
		{
			// Simulate the scenario where rollback to an older version has completed on CRP with External rollout strategy.
			name: "placement has External rollout strategy and all clusters are rolled out to a not-latest resource snapshot",
			policy: &fleetv1beta1.PlacementPolicy{
				PlacementType:    fleetv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(2)),
			},
			strategy: fleetv1beta1.RolloutStrategy{
				Type: fleetv1beta1.ExternalRolloutStrategyType,
			},
			latestPolicySnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(2),
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
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "1",
						fleetv1beta1.PlacementTrackingLabel: testCRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			otherResourceSnapshots: []*fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							*resource.NamespaceResourceContentForTest(t),
						},
					},
				},
			},
			crpStatus: fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "1",
				Conditions:            oldClusterResourcePlacementAvailableConditions,
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "1",
						Conditions:            oldResourcePlacementAvailableConditions,
					},
					{
						ClusterName:           "member-2",
						ObservedResourceIndex: "1",
						Conditions:            oldResourcePlacementAvailableConditions,
					},
				},
			},
			clusterResourceBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-rolled-out-with-old-snapshot-name",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-1",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: bindingAvailableConditions,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-rolled-out-with-old-snapshot-name-too",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
						TargetCluster:                "member-2",
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: bindingAvailableConditions,
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources: []fleetv1beta1.ResourceIdentifier{
					// Only show resources on the old snapshot.
					{
						Group:     "",
						Version:   "v1",
						Kind:      "Namespace",
						Namespace: "",
						Name:      "namespace-name",
					},
				},
				ObservedResourceIndex: "0",
				Conditions:            clusterResourcePlacementAvailableConditions,
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions:            resourcePlacementAvailableConditions,
					},
					{
						ClusterName:           "member-2",
						ObservedResourceIndex: "0",
						Conditions:            resourcePlacementAvailableConditions,
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			crp := &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
				Spec: fleetv1beta1.PlacementSpec{
					ResourceSelectors: []fleetv1beta1.ResourceSelectorTerm{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Service",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"region": "east"},
							},
						},
					},
					Policy:   tc.policy,
					Strategy: tc.strategy,
				},
				Status: tc.crpStatus,
			}
			scheme := serviceScheme(t)
			var objects []client.Object
			for i := range tc.clusterResourceBindings {
				objects = append(objects, &tc.clusterResourceBindings[i])
			}
			for _, snapshot := range tc.otherResourceSnapshots {
				objects = append(objects, snapshot)
			}
			objects = append(objects, tc.latestResourceSnapshot)
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

func TestSetResourcePlacementStatus(t *testing.T) {
	rpGeneration := int64(5)
	testRPName := "test-rp"
	testRPNamespace := "test-namespace"

	selectedResources := []fleetv1beta1.ResourceIdentifier{
		{
			Group:     "",
			Version:   "v1",
			Kind:      "Service",
			Name:      "svc-name",
			Namespace: "svc-namespace",
		},
		{
			Group:     "apps",
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

	resourcePlacementAvailableConditions := []metav1.Condition{
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourcePlacementScheduledConditionType),
			Reason:             "Scheduled",
			ObservedGeneration: rpGeneration,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourcePlacementRolloutStartedConditionType),
			Reason:             condition.RolloutStartedReason,
			ObservedGeneration: rpGeneration,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourcePlacementOverriddenConditionType),
			Reason:             condition.OverrideNotSpecifiedReason,
			ObservedGeneration: rpGeneration,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourcePlacementWorkSynchronizedConditionType),
			Reason:             condition.WorkSynchronizedReason,
			ObservedGeneration: rpGeneration,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourcePlacementAppliedConditionType),
			Reason:             condition.ApplySucceededReason,
			ObservedGeneration: rpGeneration,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.ResourcePlacementAvailableConditionType),
			Reason:             condition.AvailableReason,
			ObservedGeneration: rpGeneration,
		},
	}

	// Per-cluster conditions use the short form (PerClusterPlacementConditionType)
	perClusterAvailableConditions := []metav1.Condition{
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
			Reason:             "Scheduled", //copy from policySnapshot condition in this test
			ObservedGeneration: rpGeneration,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
			Reason:             condition.RolloutStartedReason,
			ObservedGeneration: rpGeneration,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
			Reason:             condition.OverrideNotSpecifiedReason,
			ObservedGeneration: rpGeneration,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
			Reason:             condition.WorkSynchronizedReason,
			ObservedGeneration: rpGeneration,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
			Reason:             condition.ApplySucceededReason,
			ObservedGeneration: rpGeneration,
		},
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
			Reason:             condition.AvailableReason,
			ObservedGeneration: rpGeneration,
		},
	}

	// Resource bindings for namespace-scoped resources
	resourceBindingRolloutStartedConditions := []metav1.Condition{
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
	}

	tests := []struct {
		name                   string
		rpStatus               fleetv1beta1.PlacementStatus
		strategy               fleetv1beta1.RolloutStrategy
		latestPolicySnapshot   *fleetv1beta1.SchedulingPolicySnapshot
		latestResourceSnapshot *fleetv1beta1.ResourceSnapshot
		otherResourceSnapshots []*fleetv1beta1.ResourceSnapshot
		resourceBindings       []fleetv1beta1.ResourceBinding
		want                   bool
		wantStatus             *fleetv1beta1.PlacementStatus
		wantErr                error
	}{
		{
			name: "empty policy and resource status for namespace-scoped resources",
			latestPolicySnapshot: &fleetv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testRPName, 0),
					Namespace: testRPNamespace,
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testRPName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
					},
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: rpGeneration,
				},
			},
			latestResourceSnapshot: &fleetv1beta1.ResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testRPName, 0),
					Namespace: testRPNamespace,
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: false,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ResourcePlacementScheduledConditionType),
						Reason:             condition.SchedulingUnknownReason,
						ObservedGeneration: rpGeneration,
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{},
			},
		},
		{
			name: "namespace-scoped resource placement scheduling succeeded but no binding",
			latestPolicySnapshot: &fleetv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testRPName, 0),
					Namespace: testRPNamespace,
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testRPName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
					},
					Generation: 1,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: rpGeneration,
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
					},
				},
			},
			latestResourceSnapshot: &fleetv1beta1.ResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testRPName, 0),
					Namespace: testRPNamespace,
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourcePlacementScheduledConditionType),
						Reason:             "Scheduled",
						ObservedGeneration: rpGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ResourcePlacementRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: rpGeneration,
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             "Scheduled",
								Message:            "success",
								ObservedGeneration: rpGeneration,
							},
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: rpGeneration,
							},
						},
					},
					{
						ClusterName:           "member-2",
						ObservedResourceIndex: "",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             "Scheduled",
								Message:            "success",
								ObservedGeneration: rpGeneration,
							},
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: rpGeneration,
							},
						},
					},
					{
						ClusterName:           "member-3",
						ObservedResourceIndex: "",
						Conditions: []metav1.Condition{
							{
								Status:             metav1.ConditionTrue,
								Type:               string(fleetv1beta1.PerClusterScheduledConditionType),
								Reason:             "Scheduled",
								Message:            "success",
								ObservedGeneration: rpGeneration,
							},
							{
								Status:             metav1.ConditionUnknown,
								Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
								Reason:             condition.RolloutStartedUnknownReason,
								ObservedGeneration: rpGeneration,
							},
						},
					},
				},
			},
		},
		{
			name: "namespace-scoped resource placement scheduling failed",
			latestPolicySnapshot: &fleetv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testRPName, 0),
					Namespace: testRPNamespace,
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testRPName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
					},
					Generation: 1,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: rpGeneration,
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
							Reason:      "failed",
						},
						{
							ClusterName: "member-2",
							Reason:      "failed",
						},
						{
							ClusterName: "member-3",
							Reason:      "failed",
						},
					},
				},
			},
			latestResourceSnapshot: &fleetv1beta1.ResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testRPName, 0),
					Namespace: testRPNamespace,
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: false,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ResourcePlacementScheduledConditionType),
						Reason:             "SchedulingFailed",
						ObservedGeneration: rpGeneration,
					},
				},
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{},
			},
		},
		{
			name: "namespace-scoped resource placement completed with resourceBindings and works",
			latestPolicySnapshot: &fleetv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testRPName, 0),
					Namespace: testRPNamespace,
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testRPName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(3),
					},
					Generation: 1,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: rpGeneration,
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
			latestResourceSnapshot: &fleetv1beta1.ResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testRPName, 0),
					Namespace: testRPNamespace,
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			resourceBindings: []fleetv1beta1.ResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-1",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testRPName, 0),
						TargetCluster:                "member-1",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: resourceBindingRolloutStartedConditions,
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0", //latest resource snapshot index
				Conditions:            resourcePlacementAvailableConditions,
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions:            perClusterAvailableConditions,
					},
				},
			},
		},
		{
			name: "namespace-scoped resource placement with external rollout strategy",
			strategy: fleetv1beta1.RolloutStrategy{
				Type: fleetv1beta1.ExternalRolloutStrategyType,
			},
			latestPolicySnapshot: &fleetv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testRPName, 0),
					Namespace: testRPNamespace,
					Labels: map[string]string{
						fleetv1beta1.PolicyIndexLabel:       "0",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PlacementTrackingLabel: testRPName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(2),
					},
					Generation: 1,
				},
				Status: fleetv1beta1.SchedulingPolicySnapshotStatus{
					ObservedCRPGeneration: rpGeneration,
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
			latestResourceSnapshot: &fleetv1beta1.ResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testRPName, 0),
					Namespace: testRPNamespace,
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: testRPName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			resourceBindings: []fleetv1beta1.ResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-1",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testRPName, 0),
						TargetCluster:                "member-1",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: resourceBindingRolloutStartedConditions,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-2",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
						},
						Generation: 1,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName:         fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testRPName, 0),
						SchedulingPolicySnapshotName: fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testRPName, 0),
						TargetCluster:                "member-2",
					},
					Status: fleetv1beta1.ResourceBindingStatus{
						Conditions: resourceBindingRolloutStartedConditions,
					},
				},
			},
			want: true,
			wantStatus: &fleetv1beta1.PlacementStatus{
				SelectedResources:     selectedResources,
				ObservedResourceIndex: "0", //latest resource snapshot index
				Conditions:            resourcePlacementAvailableConditions,
				PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
					{
						ClusterName:           "member-1",
						ObservedResourceIndex: "0",
						Conditions:            perClusterAvailableConditions,
					},
					{
						ClusterName:           "member-2",
						ObservedResourceIndex: "0",
						Conditions:            perClusterAvailableConditions,
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rp := &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRPName,
					Namespace: testRPNamespace,
				},
				Spec: fleetv1beta1.PlacementSpec{
					ResourceSelectors: []fleetv1beta1.ResourceSelectorTerm{
						{
							Group:   corev1.GroupName,
							Version: "v1",
							Kind:    "Service",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"region": "east"},
							},
						},
						{
							Group:   "apps",
							Version: "v1",
							Kind:    "Deployment",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "test"},
							},
						},
					},
					Strategy: tc.strategy,
				},
				Status: tc.rpStatus,
			}
			scheme := serviceScheme(t)
			var objects []client.Object
			for i := range tc.resourceBindings {
				objects = append(objects, &tc.resourceBindings[i])
			}
			for _, snapshot := range tc.otherResourceSnapshots {
				objects = append(objects, snapshot)
			}
			objects = append(objects, tc.latestResourceSnapshot)
			objects = append(objects, tc.latestPolicySnapshot)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}
			rp.Generation = rpGeneration
			got, err := r.setPlacementStatus(context.Background(), rp, selectedResources, tc.latestPolicySnapshot, tc.latestResourceSnapshot)
			if gotErr, wantErr := err != nil, tc.wantErr != nil; gotErr != wantErr || !errors.Is(err, tc.wantErr) {
				t.Fatalf("setPlacementStatus() got error %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if got != tc.want {
				t.Errorf("setPlacementStatus() = %v, want %v", got, tc.want)
			}

			if diff := cmp.Diff(tc.wantStatus, &rp.Status, statusCmpOptions...); diff != "" {
				t.Errorf("setPlacementStatus() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestBuildPerClusterPlacementStatusMap(t *testing.T) {
	tests := []struct {
		name         string
		placementObj fleetv1beta1.PlacementObj
		want         map[string][]metav1.Condition
	}{
		{
			name: "cluster-scoped: the status of the selected cluster is not set",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				Status: fleetv1beta1.PlacementStatus{
					PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
						{
							ClusterName: "member-1",
							Conditions:  []metav1.Condition{},
						},
					},
				},
			},
			want: map[string][]metav1.Condition{},
		},
		{
			name: "cluster-scoped: the status of the selected clusters are set",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				Status: fleetv1beta1.PlacementStatus{
					PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
						{
							ClusterName: "member-1",
							Conditions: []metav1.Condition{
								{
									Type:    string(fleetv1beta1.PerClusterScheduledConditionType),
									Status:  metav1.ConditionTrue,
									Reason:  "ScheduleSucceeded",
									Message: "Successfully scheduled resources to cluster member-1",
								},
								{
									Type:    string(fleetv1beta1.PerClusterRolloutStartedConditionType),
									Status:  metav1.ConditionTrue,
									Reason:  "RolloutStarted",
									Message: "Rollout has started on cluster member-1",
								},
							},
						},
						{
							ClusterName: "member-2",
							Conditions: []metav1.Condition{
								{
									Type:    string(fleetv1beta1.PerClusterAppliedConditionType),
									Status:  metav1.ConditionTrue,
									Reason:  "ApplySucceeded",
									Message: "All resources successfully applied to cluster member-2",
								},
								{
									Type:    string(fleetv1beta1.PerClusterAvailableConditionType),
									Status:  metav1.ConditionFalse,
									Reason:  "ResourcesNotReady",
									Message: "Some resources are not yet available on cluster member-2",
								},
							},
						},
					},
				},
			},
			want: map[string][]metav1.Condition{
				"member-1": {
					{
						Type:    string(fleetv1beta1.PerClusterScheduledConditionType),
						Status:  metav1.ConditionTrue,
						Reason:  "ScheduleSucceeded",
						Message: "Successfully scheduled resources to cluster member-1",
					},
					{
						Type:    string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Status:  metav1.ConditionTrue,
						Reason:  "RolloutStarted",
						Message: "Rollout has started on cluster member-1",
					},
				},
				"member-2": {
					{
						Type:    string(fleetv1beta1.PerClusterAppliedConditionType),
						Status:  metav1.ConditionTrue,
						Reason:  "ApplySucceeded",
						Message: "All resources successfully applied to cluster member-2",
					},
					{
						Type:    string(fleetv1beta1.PerClusterAvailableConditionType),
						Status:  metav1.ConditionFalse,
						Reason:  "ResourcesNotReady",
						Message: "Some resources are not yet available on cluster member-2",
					},
				},
			},
		},
		{
			name: "namespace-scoped: the status of the selected cluster is not set",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rp",
					Namespace: "test-namespace",
				},
				Status: fleetv1beta1.PlacementStatus{
					PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
						{
							ClusterName: "member-1",
							Conditions:  []metav1.Condition{},
						},
					},
				},
			},
			want: map[string][]metav1.Condition{},
		},
		{
			name: "namespace-scoped: the status of the selected clusters are set",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rp",
					Namespace: "test-namespace",
				},
				Status: fleetv1beta1.PlacementStatus{
					PerClusterPlacementStatuses: []fleetv1beta1.PerClusterPlacementStatus{
						{
							ClusterName: "member-1",
							Conditions: []metav1.Condition{
								{
									Type:    string(fleetv1beta1.PerClusterScheduledConditionType),
									Status:  metav1.ConditionTrue,
									Reason:  "ScheduleSucceeded",
									Message: "Successfully scheduled namespace-scoped resources to cluster member-1",
								},
								{
									Type:    string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
									Status:  metav1.ConditionTrue,
									Reason:  "WorkSynchronized",
									Message: "Work objects synchronized successfully on cluster member-1",
								},
								{
									Type:    string(fleetv1beta1.PerClusterAppliedConditionType),
									Status:  metav1.ConditionFalse,
									Reason:  "ApplyPending",
									Message: "Resources are pending application on cluster member-1",
								},
							},
						},
						{
							ClusterName: "member-2",
							Conditions: []metav1.Condition{
								{
									Type:    string(fleetv1beta1.PerClusterAppliedConditionType),
									Status:  metav1.ConditionTrue,
									Reason:  "ApplySucceeded",
									Message: "All namespace-scoped resources successfully applied to cluster member-2",
								},
								{
									Type:    string(fleetv1beta1.PerClusterAvailableConditionType),
									Status:  metav1.ConditionTrue,
									Reason:  "ResourcesReady",
									Message: "All namespace-scoped resources are available on cluster member-2",
								},
								{
									Type:    string(fleetv1beta1.PerClusterRolloutStartedConditionType),
									Status:  metav1.ConditionTrue,
									Reason:  "RolloutStarted",
									Message: "Rollout has started successfully on cluster member-2",
								},
							},
						},
					},
				},
			},
			want: map[string][]metav1.Condition{
				"member-1": {
					{
						Type:    string(fleetv1beta1.PerClusterScheduledConditionType),
						Status:  metav1.ConditionTrue,
						Reason:  "ScheduleSucceeded",
						Message: "Successfully scheduled namespace-scoped resources to cluster member-1",
					},
					{
						Type:    string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
						Status:  metav1.ConditionTrue,
						Reason:  "WorkSynchronized",
						Message: "Work objects synchronized successfully on cluster member-1",
					},
					{
						Type:    string(fleetv1beta1.PerClusterAppliedConditionType),
						Status:  metav1.ConditionFalse,
						Reason:  "ApplyPending",
						Message: "Resources are pending application on cluster member-1",
					},
				},
				"member-2": {
					{
						Type:    string(fleetv1beta1.PerClusterAppliedConditionType),
						Status:  metav1.ConditionTrue,
						Reason:  "ApplySucceeded",
						Message: "All namespace-scoped resources successfully applied to cluster member-2",
					},
					{
						Type:    string(fleetv1beta1.PerClusterAvailableConditionType),
						Status:  metav1.ConditionTrue,
						Reason:  "ResourcesReady",
						Message: "All namespace-scoped resources are available on cluster member-2",
					},
					{
						Type:    string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Status:  metav1.ConditionTrue,
						Reason:  "RolloutStarted",
						Message: "Rollout has started successfully on cluster member-2",
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildPerClusterPlacementStatusMap(tc.placementObj)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildPerClusterPlacementStatusMap() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestBuildClusterToBindingMap(t *testing.T) {
	policySnapshotName := "policy-2"
	testRPName := "test-rp"
	testRPNamespace := "test-namespace"

	tests := []struct {
		name             string
		placementObj     fleetv1beta1.PlacementObj
		clusterBindings  []fleetv1beta1.ClusterResourceBinding
		resourceBindings []fleetv1beta1.ResourceBinding
		want             map[string]fleetv1beta1.BindingObj
	}{
		{
			name: "cluster-scoped: no associated bindings",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
			},
			clusterBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-binding",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-crp",
						},
					},
				},
			},
			want: map[string]fleetv1beta1.BindingObj{},
		},
		{
			name: "cluster-scoped: deleting binding",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
			},
			clusterBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "deleting-binding",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						DeletionTimestamp: &metav1.Time{Time: time.Date(00002, time.January, 1, 1, 1, 1, 1, time.UTC)},
						Finalizers:        []string{"dummy-finalizer"},
					},
				},
			},
			want: map[string]fleetv1beta1.BindingObj{},
		},
		{
			name: "cluster-scoped: binding having stale policy snapshot",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
			},
			clusterBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-without-latest-policy-snapshot",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: "not-latest-policy-snapshot",
						TargetCluster:                "member-7",
					},
				},
			},
			want: map[string]fleetv1beta1.BindingObj{},
		},
		{
			name: "cluster-scoped: matched bindings",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
			},
			clusterBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-2",
					},
				},
			},
			want: map[string]fleetv1beta1.BindingObj{
				"member-1": &fleetv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-1",
					},
				},
				"member-2": &fleetv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
			name: "cluster-scoped: invalid binding with missing target cluster",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
			},
			clusterBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-without-latest-policy-snapshot",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
				},
			},
			want: map[string]fleetv1beta1.BindingObj{},
		},
		// New test cases for namespace-scoped (ResourcePlacement) bindings
		{
			name: "namespace-scoped: no associated bindings",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRPName,
					Namespace: testRPNamespace,
				},
			},
			resourceBindings: []fleetv1beta1.ResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-binding",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-rp",
						},
					},
				},
			},
			want: map[string]fleetv1beta1.BindingObj{},
		},
		{
			name: "namespace-scoped: deleting binding",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRPName,
					Namespace: testRPNamespace,
				},
			},
			resourceBindings: []fleetv1beta1.ResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deleting-binding",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
						},
						DeletionTimestamp: &metav1.Time{Time: time.Date(00002, time.January, 1, 1, 1, 1, 1, time.UTC)},
						Finalizers:        []string{"dummy-finalizer"},
					},
				},
			},
			want: map[string]fleetv1beta1.BindingObj{},
		},
		{
			name: "namespace-scoped: binding having stale policy snapshot",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRPName,
					Namespace: testRPNamespace,
				},
			},
			resourceBindings: []fleetv1beta1.ResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-without-latest-policy-snapshot",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: "not-latest-policy-snapshot",
						TargetCluster:                "member-7",
					},
				},
			},
			want: map[string]fleetv1beta1.BindingObj{},
		},
		{
			name: "namespace-scoped: matched bindings",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRPName,
					Namespace: testRPNamespace,
				},
			},
			resourceBindings: []fleetv1beta1.ResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-1",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-2",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-2",
					},
				},
			},
			want: map[string]fleetv1beta1.BindingObj{
				"member-1": &fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-1",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-1",
					},
				},
				"member-2": &fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-2",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
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
			name: "namespace-scoped: invalid binding with missing target cluster",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRPName,
					Namespace: testRPNamespace,
				},
			},
			resourceBindings: []fleetv1beta1.ResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-without-target-cluster",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
						},
					},
				},
			},
			want: map[string]fleetv1beta1.BindingObj{},
		},
		{
			name: "mixed scoped: matched bindings",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRPName,
					Namespace: testRPNamespace,
				},
			},
			resourceBindings: []fleetv1beta1.ResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-1",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-2",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-2",
					},
				},
			},
			clusterBindings: []fleetv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
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
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-2",
					},
				},
			},
			want: map[string]fleetv1beta1.BindingObj{
				"member-1": &fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-1",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-1",
					},
				},
				"member-2": &fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-2",
						Namespace: testRPNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                "member-2",
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policySnapshot := fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policySnapshotName,
				},
			}
			scheme := serviceScheme(t)
			var objects []client.Object

			// Add cluster resource bindings to objects
			for i := range tc.clusterBindings {
				objects = append(objects, &tc.clusterBindings[i])
			}

			// Add resource bindings to objects
			for i := range tc.resourceBindings {
				objects = append(objects, &tc.resourceBindings[i])
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client: fakeClient,
			}
			got, err := r.buildClusterToBindingMap(ctx, tc.placementObj, &policySnapshot)
			if err != nil {
				t.Fatalf("buildClusterToBindingMap() got err %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")); diff != "" {
				t.Errorf("buildClusterToBindingMap() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestSetPlacementStatusPerCluster(t *testing.T) {
	resourceSnapshotName := "snapshot-1"
	cluster := "member-1"
	bindingName := "binding-1"

	crp := &fleetv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testCRPName,
			Generation: placementGeneration,
		},
	}
	crpWithReportDiffApplyStrategy := crp.DeepCopy()
	crpWithReportDiffApplyStrategy.Spec.Strategy.ApplyStrategy = &fleetv1beta1.ApplyStrategy{
		Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
	}
	crpWithExternalRolloutStrategy := crp.DeepCopy()
	crpWithExternalRolloutStrategy.Spec.Strategy.Type = fleetv1beta1.ExternalRolloutStrategyType

	// Create namespace-scoped ResourcePlacement variants for comprehensive testing
	rp := &fleetv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-rp",
			Namespace:  "test-namespace",
			Generation: placementGeneration,
		},
	}
	rpWithReportDiffApplyStrategy := rp.DeepCopy()
	rpWithReportDiffApplyStrategy.Spec.Strategy.ApplyStrategy = &fleetv1beta1.ApplyStrategy{
		Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
	}
	rpWithExternalRolloutStrategy := rp.DeepCopy()
	rpWithExternalRolloutStrategy.Spec.Strategy.Type = fleetv1beta1.ExternalRolloutStrategyType

	tests := []struct {
		name                          string
		placement                     fleetv1beta1.PlacementObj
		binding                       fleetv1beta1.BindingObj
		allConditionType              []condition.ResourceCondition
		wantConditionStatusMap        map[condition.ResourceCondition]metav1.ConditionStatus
		wantPerClusterPlacementStatus fleetv1beta1.PerClusterPlacementStatus
	}{
		{
			name:      "binding not found",
			placement: crp.DeepCopy(),
			binding:   nil,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition: metav1.ConditionUnknown,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:           cluster,
				ObservedResourceIndex: "", // Empty as binding not found.
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "stale binding with false rollout started condition",
			placement: crp.DeepCopy(),
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition: metav1.ConditionFalse,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:           cluster,
				ObservedResourceIndex: "1",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutNotStartedYetReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "stale binding with true rollout started condition",
			placement: crp.DeepCopy(),
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition: metav1.ConditionUnknown,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:           cluster,
				ObservedResourceIndex: "1",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "completed binding",
			placement: crp.DeepCopy(),
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
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition:   metav1.ConditionTrue,
				condition.OverriddenCondition:       metav1.ConditionTrue,
				condition.WorkSynchronizedCondition: metav1.ConditionTrue,
				condition.AppliedCondition:          metav1.ConditionTrue,
				condition.AvailableCondition:        metav1.ConditionTrue,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:                        cluster,
				ObservedResourceIndex:              "1",
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
						Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
						Reason:             condition.AvailableReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "unknown rollout started condition",
			placement: crp.DeepCopy(),
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition: metav1.ConditionUnknown,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:           cluster,
				ObservedResourceIndex: "1",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "false overridden condition",
			placement: crp.DeepCopy(),
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition: metav1.ConditionTrue,
				condition.OverriddenCondition:     metav1.ConditionFalse,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:                        cluster,
				ObservedResourceIndex:              "1",
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
						Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
						Reason:             condition.OverriddenFailedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "unknown work created condition",
			placement: crp.DeepCopy(),
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
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition:   metav1.ConditionTrue,
				condition.OverriddenCondition:       metav1.ConditionTrue,
				condition.WorkSynchronizedCondition: metav1.ConditionUnknown,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:                        cluster,
				ObservedResourceIndex:              "1",
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
						Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedUnknownReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "false applied condition",
			placement: crp.DeepCopy(),
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
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition:   metav1.ConditionTrue,
				condition.OverriddenCondition:       metav1.ConditionTrue,
				condition.WorkSynchronizedCondition: metav1.ConditionTrue,
				condition.AppliedCondition:          metav1.ConditionFalse,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:                        cluster,
				ObservedResourceIndex:              "1",
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
						Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
						Reason:             condition.ApplyFailedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "false available condition",
			placement: crp.DeepCopy(),
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
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition:   metav1.ConditionTrue,
				condition.OverriddenCondition:       metav1.ConditionTrue,
				condition.WorkSynchronizedCondition: metav1.ConditionTrue,
				condition.AppliedCondition:          metav1.ConditionTrue,
				condition.AvailableCondition:        metav1.ConditionFalse,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:                        cluster,
				ObservedResourceIndex:              "1",
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
						Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
						Reason:             condition.NotAvailableYetReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "drifts and configuration diffs",
			placement: crp.DeepCopy(),
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName:             resourceSnapshotName,
					ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{},
					ClusterResourceOverrideSnapshots: []string{},
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition:   metav1.ConditionTrue,
				condition.OverriddenCondition:       metav1.ConditionTrue,
				condition.WorkSynchronizedCondition: metav1.ConditionTrue,
				condition.AppliedCondition:          metav1.ConditionFalse,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:                        cluster,
				ObservedResourceIndex:              "1",
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
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingOverridden),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ResourceBindingApplied),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "always on drift detection",
			placement: crp.DeepCopy(),
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName:             resourceSnapshotName,
					ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{},
					ClusterResourceOverrideSnapshots: []string{},
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					TargetCluster:                    cluster,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition:   metav1.ConditionTrue,
				condition.OverriddenCondition:       metav1.ConditionTrue,
				condition.WorkSynchronizedCondition: metav1.ConditionTrue,
				condition.AppliedCondition:          metav1.ConditionTrue,
				condition.AvailableCondition:        metav1.ConditionTrue,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:                        cluster,
				ObservedResourceIndex:              "1",
				ApplicableResourceOverrides:        []fleetv1beta1.NamespacedName{},
				ApplicableClusterResourceOverrides: []string{},
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
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingOverridden),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingApplied),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingAvailable),
						Reason:             condition.AvailableReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "ReportDiff apply strategy (diff reported)",
			placement: crpWithReportDiffApplyStrategy.DeepCopy(),
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName:             resourceSnapshotName,
					ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{},
					ClusterResourceOverrideSnapshots: []string{},
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					TargetCluster:                    cluster,
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
					},
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					DiffedPlacements: []fleetv1beta1.DiffedResourcePlacement{
						{
							ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "cm-1",
								Namespace: "ns-1",
							},
							ObservationTime:                 metav1.Time{Time: time.Now()},
							TargetClusterObservedGeneration: ptr.To(int64(1)),
							FirstDiffedObservedTime:         metav1.Time{Time: time.Now()},
							ObservedDiffs: []fleetv1beta1.PatchDetail{
								{
									Path:          "/data",
									ValueInMember: "k=1",
									ValueInHub:    "k=2",
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
							Reason:             condition.ApplyFailedReason,
							ObservedGeneration: 1,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingDiffReported),
							Reason:             condition.DiffReportedStatusTrueReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			allConditionType: condition.CondTypesForReportDiffApplyStrategy,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition:   metav1.ConditionTrue,
				condition.OverriddenCondition:       metav1.ConditionTrue,
				condition.WorkSynchronizedCondition: metav1.ConditionTrue,
				condition.DiffReportedCondition:     metav1.ConditionTrue,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:                        cluster,
				ObservedResourceIndex:              "1",
				ApplicableResourceOverrides:        []fleetv1beta1.NamespacedName{},
				ApplicableClusterResourceOverrides: []string{},
				DiffedPlacements: []fleetv1beta1.DiffedResourcePlacement{
					{
						ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
							Group:     "",
							Version:   "v1",
							Kind:      "ConfigMap",
							Name:      "cm-1",
							Namespace: "ns-1",
						},
						ObservationTime:                 metav1.Time{Time: time.Now()},
						TargetClusterObservedGeneration: ptr.To(int64(1)),
						FirstDiffedObservedTime:         metav1.Time{Time: time.Now()},
						ObservedDiffs: []fleetv1beta1.PatchDetail{
							{
								Path:          "/data",
								ValueInMember: "k=1",
								ValueInHub:    "k=2",
							},
						},
					},
				},
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingOverridden),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingDiffReported),
						Reason:             condition.DiffReportedStatusTrueReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "ReportDiff apply strategy (diff not yet reported)",
			placement: crpWithReportDiffApplyStrategy.DeepCopy(),
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Generation: 2,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName:             resourceSnapshotName,
					ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{},
					ClusterResourceOverrideSnapshots: []string{},
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					TargetCluster:                    cluster,
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
					},
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					DiffedPlacements: []fleetv1beta1.DiffedResourcePlacement{
						{
							ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "cm-1",
								Namespace: "ns-1",
							},
							ObservationTime:                 metav1.Time{Time: time.Now()},
							TargetClusterObservedGeneration: ptr.To(int64(1)),
							FirstDiffedObservedTime:         metav1.Time{Time: time.Now()},
							ObservedDiffs: []fleetv1beta1.PatchDetail{
								{
									Path:          "/data",
									ValueInMember: "k=1",
									ValueInHub:    "k=2",
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: 2,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingOverridden),
							Reason:             condition.OverriddenSucceededReason,
							ObservedGeneration: 2,
						},
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
							Reason:             condition.WorkSynchronizedReason,
							ObservedGeneration: 2,
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
			allConditionType: condition.CondTypesForReportDiffApplyStrategy,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition:   metav1.ConditionTrue,
				condition.OverriddenCondition:       metav1.ConditionTrue,
				condition.WorkSynchronizedCondition: metav1.ConditionTrue,
				condition.DiffReportedCondition:     metav1.ConditionUnknown,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:                        cluster,
				ObservedResourceIndex:              "1",
				ApplicableResourceOverrides:        []fleetv1beta1.NamespacedName{},
				ApplicableClusterResourceOverrides: []string{},
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingOverridden),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.ResourceBindingDiffReported),
						Reason:             condition.DiffReportedStatusUnknownReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "ReportDiff apply strategy (failed to report diff)",
			placement: crpWithReportDiffApplyStrategy.DeepCopy(),
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName:             resourceSnapshotName,
					ResourceOverrideSnapshots:        []fleetv1beta1.NamespacedName{},
					ClusterResourceOverrideSnapshots: []string{},
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
					TargetCluster:                    cluster,
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
					},
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					DiffedPlacements: []fleetv1beta1.DiffedResourcePlacement{
						{
							ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "cm-1",
								Namespace: "ns-1",
							},
							ObservationTime:                 metav1.Time{Time: time.Now()},
							TargetClusterObservedGeneration: ptr.To(int64(1)),
							FirstDiffedObservedTime:         metav1.Time{Time: time.Now()},
							ObservedDiffs: []fleetv1beta1.PatchDetail{
								{
									Path:          "/data",
									ValueInMember: "k=1",
									ValueInHub:    "k=2",
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
							Type:               string(fleetv1beta1.ResourceBindingDiffReported),
							Reason:             condition.DiffReportedStatusFalseReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			allConditionType: condition.CondTypesForReportDiffApplyStrategy,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition:   metav1.ConditionTrue,
				condition.OverriddenCondition:       metav1.ConditionTrue,
				condition.WorkSynchronizedCondition: metav1.ConditionTrue,
				condition.DiffReportedCondition:     metav1.ConditionFalse,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:                        cluster,
				ObservedResourceIndex:              "1",
				ApplicableResourceOverrides:        []fleetv1beta1.NamespacedName{},
				ApplicableClusterResourceOverrides: []string{},
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingOverridden),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.ResourceBindingDiffReported),
						Reason:             condition.DiffReportedStatusFalseReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "crp with External rollout strategy and binding not found",
			placement: crpWithExternalRolloutStrategy.DeepCopy(),
			binding:   nil,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition: metav1.ConditionUnknown,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:           cluster,
				ObservedResourceIndex: "", // Empty as binding not found.
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "crp with External rollout strategy and stale binding with false rollout started condition",
			placement: crpWithExternalRolloutStrategy.DeepCopy(),
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition: metav1.ConditionFalse,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:           cluster,
				ObservedResourceIndex: "0", // Depends on the resourceSnapshotIndexOnBinding passed in.
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutNotStartedYetReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "crp with External rollout strategy and stale binding with true rollout started condition",
			placement: crpWithExternalRolloutStrategy.DeepCopy(),
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
							Reason:             condition.RolloutStartedReason,
						},
					},
				},
			},
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition: metav1.ConditionTrue,
				condition.OverriddenCondition:     metav1.ConditionUnknown,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:           cluster,
				ObservedResourceIndex: "0", // Depends on the resourceSnapshotIndexOnBinding passed in.
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
						Reason:             condition.OverriddenPendingReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "crp with External rollout strategy and completed binding",
			placement: crpWithExternalRolloutStrategy.DeepCopy(),
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
					SchedulingPolicySnapshotName:     fmt.Sprintf(fleetv1beta1.PolicySnapshotNameFmt, testCRPName, 0),
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
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition:   metav1.ConditionTrue,
				condition.OverriddenCondition:       metav1.ConditionTrue,
				condition.WorkSynchronizedCondition: metav1.ConditionTrue,
				condition.AppliedCondition:          metav1.ConditionTrue,
				condition.AvailableCondition:        metav1.ConditionTrue,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:                        cluster,
				ObservedResourceIndex:              "0", // Depends on the resourceSnapshotIndexOnBinding passed in.
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
						Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
						Reason:             condition.AvailableReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
			allConditionType: condition.CondTypesForApplyStrategies,
		},
		// Namespace-scoped ResourcePlacement test cases
		{
			name:             "binding not found - namespace scoped",
			placement:        rp.DeepCopy(),
			binding:          nil,
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition: metav1.ConditionUnknown,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:           cluster,
				ObservedResourceIndex: "", // Empty as binding not found.
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedUnknownReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "stale binding with false rollout started condition - namespace scoped",
			placement: rp.DeepCopy(),
			binding: &fleetv1beta1.ResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Namespace:  "test-namespace",
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition: metav1.ConditionFalse,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:           cluster,
				ObservedResourceIndex: "1",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutNotStartedYetReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "completed binding - namespace scoped",
			placement: rp.DeepCopy(),
			binding: &fleetv1beta1.ResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Namespace:  "test-namespace",
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition:   metav1.ConditionTrue,
				condition.OverriddenCondition:       metav1.ConditionTrue,
				condition.WorkSynchronizedCondition: metav1.ConditionTrue,
				condition.AppliedCondition:          metav1.ConditionTrue,
				condition.AvailableCondition:        metav1.ConditionTrue,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:                        cluster,
				ObservedResourceIndex:              "1",
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
						Type:               string(fleetv1beta1.PerClusterAppliedConditionType),
						Reason:             condition.ApplySucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterAvailableConditionType),
						Reason:             condition.AvailableReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "report diff apply strategy - namespace scoped",
			placement: rpWithReportDiffApplyStrategy.DeepCopy(),
			binding: &fleetv1beta1.ResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Namespace:  "test-namespace",
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
							Type:               string(fleetv1beta1.ResourceBindingDiffReported),
							Reason:             condition.DiffReportedStatusTrueReason,
							ObservedGeneration: 1,
						},
					},
				},
			},
			allConditionType: condition.CondTypesForReportDiffApplyStrategy,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition:   metav1.ConditionTrue,
				condition.OverriddenCondition:       metav1.ConditionTrue,
				condition.WorkSynchronizedCondition: metav1.ConditionTrue,
				condition.DiffReportedCondition:     metav1.ConditionTrue,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:           cluster,
				ObservedResourceIndex: "1",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingOverridden),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
						Reason:             condition.WorkSynchronizedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.ResourceBindingDiffReported),
						Reason:             condition.DiffReportedStatusTrueReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "external rollout strategy - namespace scoped",
			placement: rpWithExternalRolloutStrategy.DeepCopy(),
			binding: &fleetv1beta1.ResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Namespace:  "test-namespace",
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName: resourceSnapshotName,
					TargetCluster:        cluster,
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition: metav1.ConditionFalse,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:           cluster,
				ObservedResourceIndex: "0", //it's the resourceSnapshot index we send in the test setup
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutNotStartedYetReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "namespace placement with cluster override failure",
			placement: rp.DeepCopy(),
			binding: &fleetv1beta1.ResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Namespace:  "test-namespace",
					Generation: 1,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ResourceSnapshotName:             resourceSnapshotName,
					ClusterResourceOverrideSnapshots: []string{"cluster-override-1", "cluster-override-2"},
					ResourceOverrideSnapshots: []fleetv1beta1.NamespacedName{
						{
							Name:      "namespace-override-1",
							Namespace: "test-namespace",
						},
					},
					TargetCluster: cluster,
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition: metav1.ConditionTrue,
				condition.OverriddenCondition:     metav1.ConditionFalse,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:                        cluster,
				ObservedResourceIndex:              "1",
				ApplicableClusterResourceOverrides: []string{"cluster-override-1", "cluster-override-2"},
				ApplicableResourceOverrides: []fleetv1beta1.NamespacedName{
					{
						Name:      "namespace-override-1",
						Namespace: "test-namespace",
					},
				},
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
						Reason:             condition.OverriddenFailedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
		{
			name:      "namespace placement with unknown work synchronization",
			placement: rp.DeepCopy(),
			binding: &fleetv1beta1.ResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bindingName,
					Namespace:  "test-namespace",
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
			allConditionType: condition.CondTypesForApplyStrategies,
			wantConditionStatusMap: map[condition.ResourceCondition]metav1.ConditionStatus{
				condition.RolloutStartedCondition:   metav1.ConditionTrue,
				condition.OverriddenCondition:       metav1.ConditionTrue,
				condition.WorkSynchronizedCondition: metav1.ConditionUnknown,
			},
			wantPerClusterPlacementStatus: fleetv1beta1.PerClusterPlacementStatus{
				ClusterName:           cluster,
				ObservedResourceIndex: "1",
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterRolloutStartedConditionType),
						Reason:             condition.RolloutStartedReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetv1beta1.PerClusterOverriddenConditionType),
						Reason:             condition.OverriddenSucceededReason,
						ObservedGeneration: placementGeneration,
					},
					{
						Status:             metav1.ConditionUnknown,
						Type:               string(fleetv1beta1.PerClusterWorkSynchronizedConditionType),
						Reason:             condition.WorkSynchronizedUnknownReason,
						ObservedGeneration: placementGeneration,
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create appropriate ResourceSnapshot based on placement type
			var resourceSnapshot fleetv1beta1.ResourceSnapshotObj
			if tc.placement.GetNamespace() == "" {
				resourceSnapshot = &fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceSnapshotName,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "1",
						},
					},
				}
			} else {
				resourceSnapshot = &fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceSnapshotName,
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "1",
						},
					},
				}
			}

			r := Reconciler{
				Recorder: record.NewFakeRecorder(10),
			}
			status := fleetv1beta1.PerClusterPlacementStatus{ClusterName: cluster}
			got := r.setPerClusterPlacementStatus(tc.placement, resourceSnapshot, "0", tc.binding, &status, tc.allConditionType)
			if diff := cmp.Diff(got, tc.wantConditionStatusMap); diff != "" {
				t.Errorf("setResourcePlacementStatusPerCluster() conditionStatus mismatch (-got, +want):\n%s", diff)
			}
			if diff := cmp.Diff(status, tc.wantPerClusterPlacementStatus, statusCmpOptions...); diff != "" {
				t.Errorf("setResourcePlacementStatusPerCluster() status mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}

func TestFindResourceSnapshotIndexForBindings(t *testing.T) {
	crp := fleetv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: testCRPName,
		},
	}
	rp := fleetv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rp",
			Namespace: "test-ns",
		},
	}
	tests := []struct {
		name                         string
		placementObj                 fleetv1beta1.PlacementObj
		bindingMap                   map[string]fleetv1beta1.BindingObj
		resourceSnapshots            []fleetv1beta1.ResourceSnapshotObj
		wantResourceSnapshotIndexMap map[string]string
	}{
		{
			name:                         "empty binding map",
			placementObj:                 &crp,
			bindingMap:                   map[string]fleetv1beta1.BindingObj{},
			resourceSnapshots:            []fleetv1beta1.ResourceSnapshotObj{},
			wantResourceSnapshotIndexMap: map[string]string{},
		},
		{
			name:         "binding with empty resource snapshot name",
			placementObj: &crp,
			bindingMap: map[string]fleetv1beta1.BindingObj{
				"member-1": &fleetv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: "",
					},
				},
			},
			resourceSnapshots:            []fleetv1beta1.ResourceSnapshotObj{},
			wantResourceSnapshotIndexMap: map[string]string{"member-1": ""},
		},
		{
			name:         "binding with not found resource snapshot",
			placementObj: &crp,
			bindingMap: map[string]fleetv1beta1.BindingObj{
				"member-1": &fleetv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: "not-found",
					},
				},
			},
			resourceSnapshots:            []fleetv1beta1.ResourceSnapshotObj{},
			wantResourceSnapshotIndexMap: map[string]string{"member-1": ""},
		},
		{
			name:         "single binding with found resource snapshot",
			placementObj: &crp,
			bindingMap: map[string]fleetv1beta1.BindingObj{
				"member-1": &fleetv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					},
				},
			},
			resourceSnapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
				},
			},
			wantResourceSnapshotIndexMap: map[string]string{"member-1": "0"},
		},
		{
			name:         "multiple bindings with both found and not found resource snapshots",
			placementObj: &crp,
			bindingMap: map[string]fleetv1beta1.BindingObj{
				"member-1": &fleetv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
					},
				},
				"member-2": &fleetv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: "not-found",
					},
				},
				"member-3": &fleetv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "binding-3",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
					},
				},
			},
			resourceSnapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
				},
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
					},
				},
			},
			wantResourceSnapshotIndexMap: map[string]string{
				"member-1": "0",
				"member-2": "",
				"member-3": "1",
			},
		},
		{
			name:         "namespace-scoped single binding with found resource snapshot",
			placementObj: &rp,
			bindingMap: map[string]fleetv1beta1.BindingObj{
				"member-1": &fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-1",
						Namespace: "test-ns",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, "test-rp", 0),
					},
				},
			},
			resourceSnapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, "test-rp", 0),
						Namespace: "test-ns",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
						},
					},
				},
			},
			wantResourceSnapshotIndexMap: map[string]string{"member-1": "0"},
		},
		{
			name:         "namespace-scoped multiple bindings with mixed found and not found",
			placementObj: &rp,
			bindingMap: map[string]fleetv1beta1.BindingObj{
				"member-1": &fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-1",
						Namespace: "test-ns",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, "test-rp", 0),
					},
				},
				"member-2": &fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-2",
						Namespace: "test-ns",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: "not-found-ns",
					},
				},
				"member-3": &fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-3",
						Namespace: "test-ns",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, "test-rp", 2),
					},
				},
			},
			resourceSnapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, "test-rp", 0),
						Namespace: "test-ns",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, "test-rp", 2),
						Namespace: "test-ns",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
						},
					},
				},
			},
			wantResourceSnapshotIndexMap: map[string]string{
				"member-1": "0",
				"member-2": "",
				"member-3": "2",
			},
		},
		{
			name:         "namespace-scoped with empty resource snapshot name",
			placementObj: &rp,
			bindingMap: map[string]fleetv1beta1.BindingObj{
				"member-1": &fleetv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding-1",
						Namespace: "test-ns",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-rp",
						},
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						ResourceSnapshotName: "",
					},
				},
			},
			resourceSnapshots:            []fleetv1beta1.ResourceSnapshotObj{},
			wantResourceSnapshotIndexMap: map[string]string{"member-1": ""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := serviceScheme(t)
			var objects []client.Object
			for _, resourceSnapshot := range tc.resourceSnapshots {
				objects = append(objects, resourceSnapshot)
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client: fakeClient,
			}
			got, err := r.findResourceSnapshotIndexForBindings(ctx, tc.placementObj, tc.bindingMap)
			if err != nil {
				t.Fatalf("findResourceSnapshotIndexForBindings() got err %v, want nil", err)
			}
			cmpOptions := cmp.Options{
				cmpopts.SortMaps(func(a, b string) bool { return a < b }),
			}
			if diff := cmp.Diff(tc.wantResourceSnapshotIndexMap, got, cmpOptions...); diff != "" {
				t.Errorf("findResourceSnapshotIndexForBindings() returned resource snapshot index map mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestIsClusterScopedPlacement(t *testing.T) {
	tests := []struct {
		name      string
		placement fleetv1beta1.PlacementObj
		want      bool
	}{
		{
			name: "cluster scoped placement (empty namespace)",
			placement: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
			},
			want: true,
		},
		{
			name: "namespace scoped placement",
			placement: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rp",
					Namespace: "test-ns",
				},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isClusterScopedPlacement(tc.placement)
			if got != tc.want {
				t.Errorf("isClusterScopedPlacement() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGetPlacementConditionType(t *testing.T) {
	tests := []struct {
		name      string
		placement fleetv1beta1.PlacementObj
		condType  condition.ResourceCondition
		want      string
	}{
		{
			name: "cluster scoped placement rollout started condition",
			placement: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
			},
			condType: condition.RolloutStartedCondition,
			want:     string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
		},
		{
			name: "namespace scoped placement rollout started condition",
			placement: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rp",
					Namespace: "test-ns",
				},
			},
			condType: condition.RolloutStartedCondition,
			want:     string(fleetv1beta1.ResourcePlacementRolloutStartedConditionType),
		},
		{
			name: "cluster scoped placement applied condition",
			placement: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
			},
			condType: condition.AppliedCondition,
			want:     string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
		},
		{
			name: "namespace scoped placement applied condition",
			placement: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rp",
					Namespace: "test-ns",
				},
			},
			condType: condition.AppliedCondition,
			want:     string(fleetv1beta1.ResourcePlacementAppliedConditionType),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getPlacementConditionType(tc.placement, tc.condType)
			if got != tc.want {
				t.Errorf("getPlacementConditionType() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGeneratePlacementConditionByStatus(t *testing.T) {
	tests := []struct {
		name         string
		placement    fleetv1beta1.PlacementObj
		condType     condition.ResourceCondition
		status       metav1.ConditionStatus
		generation   int64
		clusterCount int
		want         metav1.Condition
	}{
		{
			name: "cluster scoped placement unknown rollout started condition",
			placement: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
			},
			condType:     condition.RolloutStartedCondition,
			status:       metav1.ConditionUnknown,
			generation:   1,
			clusterCount: 2,
			want: metav1.Condition{
				Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
				Status:             metav1.ConditionUnknown,
				Reason:             condition.RolloutStartedUnknownReason,
				ObservedGeneration: 1,
			},
		},
		{
			name: "namespace scoped placement false rollout started condition",
			placement: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rp",
					Namespace: "test-ns",
				},
			},
			condType:     condition.RolloutStartedCondition,
			status:       metav1.ConditionFalse,
			generation:   1,
			clusterCount: 2,
			want: metav1.Condition{
				Type:               string(fleetv1beta1.ResourcePlacementRolloutStartedConditionType),
				Status:             metav1.ConditionFalse,
				Reason:             condition.RolloutNotStartedYetReason,
				ObservedGeneration: 1,
			},
		},
		{
			name: "cluster scoped placement true applied condition",
			placement: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
			},
			condType:     condition.AppliedCondition,
			status:       metav1.ConditionTrue,
			generation:   2,
			clusterCount: 3,
			want: metav1.Condition{
				Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
				Status:             metav1.ConditionTrue,
				Reason:             condition.ApplySucceededReason,
				ObservedGeneration: 2,
			},
		},
		{
			name: "namespace scoped placement true applied condition",
			placement: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rp",
					Namespace: "test-ns",
				},
			},
			condType:     condition.AppliedCondition,
			status:       metav1.ConditionTrue,
			generation:   2,
			clusterCount: 3,
			want: metav1.Condition{
				Type:               string(fleetv1beta1.ResourcePlacementAppliedConditionType),
				Status:             metav1.ConditionTrue,
				Reason:             condition.ApplySucceededReason,
				ObservedGeneration: 2,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := generatePlacementConditionByStatus(tc.placement, tc.condType, tc.status, tc.generation, tc.clusterCount)
			if diff := cmp.Diff(tc.want, got, statusCmpOptions...); diff != "" {
				t.Errorf("generatePlacementConditionByStatus() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestCalculateFailedToScheduleClusterCount(t *testing.T) {
	// Helper function to create cluster decisions
	createClusterDecisions := func(names []string) []*fleetv1beta1.ClusterDecision {
		decisions := make([]*fleetv1beta1.ClusterDecision, len(names))
		for i, name := range names {
			decisions[i] = &fleetv1beta1.ClusterDecision{
				ClusterName: name,
			}
		}
		return decisions
	}

	tests := []struct {
		name         string
		placementObj fleetv1beta1.PlacementObj
		selected     []*fleetv1beta1.ClusterDecision
		unselected   []*fleetv1beta1.ClusterDecision
		want         int
		wantErr      bool
	}{
		{
			name: "PickAll policy with nil policy - no failed clusters",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.PlacementSpec{
					Policy: nil, // nil policy means PickAll
				},
			},
			selected:   createClusterDecisions([]string{"cluster1", "cluster2"}),
			unselected: createClusterDecisions([]string{"cluster3", "cluster4"}),
			want:       0,
			wantErr:    false,
		},
		{
			name: "PickAll policy explicitly set - no failed clusters",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickAllPlacementType,
					},
				},
			},
			selected:   createClusterDecisions([]string{"cluster1", "cluster2"}),
			unselected: createClusterDecisions([]string{"cluster3"}),
			want:       0,
			wantErr:    false,
		},
		{
			name: "PickN policy - requested 3, selected 2, failed 1",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(3)),
					},
				},
			},
			selected:   createClusterDecisions([]string{"cluster1", "cluster2"}),
			unselected: createClusterDecisions([]string{"cluster3", "cluster4"}),
			want:       1,
			wantErr:    false,
		},
		{
			name: "PickN policy - requested exactly what was selected, no failures",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(2)),
					},
				},
			},
			selected:   createClusterDecisions([]string{"cluster1", "cluster2"}),
			unselected: createClusterDecisions([]string{"cluster3"}),
			want:       0,
			wantErr:    false,
		},
		{
			name: "PickN policy - selected more than requested (should return error)",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
					},
				},
			},
			selected:   createClusterDecisions([]string{"cluster1", "cluster2"}),
			unselected: createClusterDecisions([]string{"cluster3"}),
			want:       0, // function returns 0 with an error
			wantErr:    true,
		},
		{
			name: "PickFixed policy - 3 clusters specified, 2 selected, 1 failed",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{"cluster1", "cluster2", "cluster3"},
					},
				},
			},
			selected:   createClusterDecisions([]string{"cluster1", "cluster2"}),
			unselected: createClusterDecisions([]string{"cluster3", "cluster4"}),
			want:       1,
			wantErr:    false,
		},
		{
			name: "PickFixed policy - all specified clusters selected, no failures",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{"cluster1", "cluster2"},
					},
				},
			},
			selected:   createClusterDecisions([]string{"cluster1", "cluster2"}),
			unselected: createClusterDecisions([]string{"cluster3"}),
			want:       0,
			wantErr:    false,
		},
		{
			name: "PickN policy - corner case: insufficient clusters available",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(5)), // want 5 clusters
					},
				},
			},
			selected:   createClusterDecisions([]string{"cluster1"}), // only 1 selected
			unselected: createClusterDecisions([]string{"cluster2"}), // only 1 unselected
			want:       1,                                            // failed count should be clamped to unselected count (1)
			wantErr:    false,
		},
		{
			name: "PickFixed policy - corner case: insufficient clusters available",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{"cluster1", "cluster2", "cluster3", "cluster4", "cluster5"}, // want 5 clusters
					},
				},
			},
			selected:   createClusterDecisions([]string{"cluster1", "cluster2"}), // only 2 selected
			unselected: createClusterDecisions([]string{"cluster3"}),             // only 1 unselected
			want:       1,                                                        // failed count should be clamped to unselected count (1)
			wantErr:    false,
		},
		{
			name: "PickN policy with nil NumberOfClusters - should not crash",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: nil, // nil number of clusters
					},
				},
			},
			selected:   createClusterDecisions([]string{"cluster1", "cluster2"}),
			unselected: createClusterDecisions([]string{"cluster3"}),
			want:       0, // should handle gracefully
			wantErr:    false,
		},
		{
			name: "ResourcePlacement with PickN policy",
			placementObj: &fleetv1beta1.ResourcePlacement{
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(4)),
					},
				},
			},
			selected:   createClusterDecisions([]string{"cluster1", "cluster2"}),
			unselected: createClusterDecisions([]string{"cluster3", "cluster4", "cluster5"}),
			want:       2, // requested 4, selected 2, failed 2
			wantErr:    false,
		},
		{
			name: "ResourcePlacement with PickFixed policy",
			placementObj: &fleetv1beta1.ResourcePlacement{
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{"cluster1", "cluster2", "cluster3"},
					},
				},
			},
			selected:   createClusterDecisions([]string{"cluster1"}),
			unselected: createClusterDecisions([]string{"cluster2", "cluster3", "cluster4"}),
			want:       2, // specified 3, selected 1, failed 2
			wantErr:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := calculateFailedToScheduleClusterCount(tc.placementObj, tc.selected, tc.unselected)
			if (err != nil) != tc.wantErr {
				if err != nil {
					t.Fatalf("calculateFailedToScheduleClusterCount() unexpected error: %v", err)
				}
				t.Fatalf("calculateFailedToScheduleClusterCount() expected error but got none")
				return
			}
			if got != tc.want {
				t.Errorf("calculateFailedToScheduleClusterCount() = %v, want %v", got, tc.want)
			}
		})
	}
}
