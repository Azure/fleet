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

package condition

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	conditionType    = "some-type"
	altConditionType = "some-other-type"
	reason           = "some reason"
	altReason        = "some other reason"
	message          = "some message"
	altMessage       = "some other message"
)

func TestEqualCondition(t *testing.T) {
	tests := []struct {
		name    string
		current *metav1.Condition
		desired *metav1.Condition
		want    bool
	}{
		{
			name:    "both are nil",
			current: nil,
			desired: nil,
			want:    true,
		},
		{
			name:    "current is nil",
			current: nil,
			desired: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionUnknown,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: 1,
			},
			want: false,
		},
		{
			name: "messages are different",
			current: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionUnknown,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: 1,
			},
			desired: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionUnknown,
				Reason:             reason,
				Message:            altMessage,
				ObservedGeneration: 1,
			},
			want: true,
		},
		{
			name: "observedGenerations are different (current is larger)",
			current: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionUnknown,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: 2,
			},
			desired: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionUnknown,
				Reason:             reason,
				Message:            altMessage,
				ObservedGeneration: 1,
			},
			want: true,
		},
		{
			name: "observedGenerations are different (current is smaller)",
			current: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionUnknown,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: 3,
			},
			desired: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionUnknown,
				Reason:             reason,
				Message:            altMessage,
				ObservedGeneration: 4,
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EqualCondition(tc.current, tc.desired)
			if !cmp.Equal(got, tc.want) {
				t.Errorf("EqualCondition() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEqualConditionIgnoreReason tests the EqualConditionIgnoreReason function.
func TestEqualConditionIgnoreReason(t *testing.T) {
	testCases := []struct {
		name    string
		current *metav1.Condition
		desired *metav1.Condition
		want    bool
	}{
		{
			name:    "nil conditions",
			current: nil,
			desired: nil,
			want:    true,
		},
		{
			name:    "current is nil",
			current: nil,
			desired: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 7,
			},
			want: false,
		},
		{
			name: "conditions are equal",
			current: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 0,
			},
			desired: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 0,
			},
			want: true,
		},
		{
			name: "conditions are equal (different reasons)",
			current: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionTrue,
				Reason:             reason,
				ObservedGeneration: 0,
			},
			desired: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionTrue,
				Reason:             altReason,
				ObservedGeneration: 0,
			},
			want: true,
		},
		{
			name: "conditions are not equal (different type)",
			current: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 1,
			},
			desired: &metav1.Condition{
				Type:               altConditionType,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 1,
			},
			want: false,
		},
		{
			name: "conditions are not equal (different status)",
			current: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 4,
			},
			desired: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 4,
			},
			want: false,
		},
		{
			name: "conditions are equal (current condition is newer)",
			current: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 3,
			},
			desired: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 2,
			},
			want: true,
		},
		{
			name: "conditions are not equal (current condition is older)",
			current: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 5,
			},
			desired: &metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 6,
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EqualConditionIgnoreReason(tc.current, tc.desired); got != tc.want {
				t.Fatalf("EqualConditionIgnoreReason(%+v, %+v) = %t, want %t",
					tc.current, tc.desired, got, tc.want)
			}
		})
	}
}

func TestIsConditionStatusTrue(t *testing.T) {
	tests := map[string]struct {
		cond             *metav1.Condition
		latestGeneration int64
		want             bool
	}{
		"nil condition is considered false": {
			cond: nil,
			want: false,
		},
		"condition is considered false if status is not true": {
			cond: &metav1.Condition{
				Status: metav1.ConditionFalse,
			},
			want: false,
		},
		"condition is considered false if status is unknown": {
			cond: &metav1.Condition{
				Status: metav1.ConditionUnknown,
			},
			want: false,
		},
		"condition is considered false if status is true but generation is not up to date": {
			cond: &metav1.Condition{
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
			},
			latestGeneration: 2,
			want:             false,
		},
		"condition is considered true if status is true and generation is up to date": {
			cond: &metav1.Condition{
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 2,
			},
			latestGeneration: 2,
			want:             true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := IsConditionStatusTrue(tt.cond, tt.latestGeneration); got != tt.want {
				t.Errorf("IsConditionStatusTrue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsConditionStatusFalse(t *testing.T) {
	tests := map[string]struct {
		cond             *metav1.Condition
		latestGeneration int64
		want             bool
	}{
		"nil condition is considered false": {
			cond: nil,
			want: false,
		},
		"condition is considered false if status is true": {
			cond: &metav1.Condition{
				Status: metav1.ConditionTrue,
			},
			want: false,
		},
		"condition is considered false if status is unknown": {
			cond: &metav1.Condition{
				Status: metav1.ConditionUnknown,
			},
			want: false,
		},
		"condition is considered false if status is false but generation is not up to date": {
			cond: &metav1.Condition{
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
			},
			latestGeneration: 2,
			want:             false,
		},
		"condition is considered true if status is false and generation is up to date": {
			cond: &metav1.Condition{
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 2,
			},
			latestGeneration: 2,
			want:             true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := IsConditionStatusFalse(tt.cond, tt.latestGeneration); got != tt.want {
				t.Errorf("IsConditionStatusFalse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrueWorkSynchronizedConditionMessage(t *testing.T) {
	const (
		generation   int64 = 3
		clusterCount       = 2
		wantMessage        = "Work(s) are successfully created or updated in 2 target cluster(s)' namespaces"
	)

	tests := []struct {
		name string
		got  metav1.Condition
		want metav1.Condition
	}{
		{
			name: "cluster resource placement",
			got:  WorkSynchronizedCondition.TrueClusterResourcePlacementCondition(generation, clusterCount),
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
				Reason:             WorkSynchronizedReason,
				Message:            wantMessage,
				ObservedGeneration: generation,
			},
		},
		{
			name: "resource placement",
			got:  WorkSynchronizedCondition.TrueResourcePlacementCondition(generation, clusterCount),
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourcePlacementWorkSynchronizedConditionType),
				Reason:             WorkSynchronizedReason,
				Message:            wantMessage,
				ObservedGeneration: generation,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, tc.got); diff != "" {
				t.Fatalf("True WorkSynchronized condition mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTrueClusterResourcePlacementCondition(t *testing.T) {
	const (
		generation   = int64(3)
		clusterCount = 2
	)
	tests := []struct {
		name      string
		condition ResourceCondition
		want      metav1.Condition
	}{
		{
			name:      "rollout started",
			condition: RolloutStartedCondition,
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ClusterResourcePlacementRolloutStartedConditionType),
				Reason:             RolloutStartedReason,
				Message:            "All 2 cluster(s) start rolling out the latest resource",
				ObservedGeneration: generation,
			},
		},
		{
			name:      "overridden",
			condition: OverriddenCondition,
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ClusterResourcePlacementOverriddenConditionType),
				Reason:             OverriddenSucceededReason,
				Message:            "The selected resources are successfully overridden in 2 cluster(s)",
				ObservedGeneration: generation,
			},
		},
		{
			name:      "work synchronized",
			condition: WorkSynchronizedCondition,
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ClusterResourcePlacementWorkSynchronizedConditionType),
				Reason:             WorkSynchronizedReason,
				Message:            "Work(s) are successfully created or updated in 2 target cluster(s)' namespaces",
				ObservedGeneration: generation,
			},
		},
		{
			name:      "applied",
			condition: AppliedCondition,
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
				Reason:             ApplySucceededReason,
				Message:            "The selected resources are successfully applied to 2 cluster(s)",
				ObservedGeneration: generation,
			},
		},
		{
			name:      "available",
			condition: AvailableCondition,
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ClusterResourcePlacementAvailableConditionType),
				Reason:             AvailableReason,
				Message:            "The selected resources in 2 cluster(s) are available now",
				ObservedGeneration: generation,
			},
		},
		{
			name:      "diff reported",
			condition: DiffReportedCondition,
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ClusterResourcePlacementDiffReportedConditionType),
				Reason:             DiffReportedStatusTrueReason,
				Message:            "Diff reporting in 2 cluster(s) has been completed",
				ObservedGeneration: generation,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.condition.TrueClusterResourcePlacementCondition(generation, clusterCount)
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("TrueClusterResourcePlacementCondition(%d, %d) mismatch (-got, +want):\n%s",
					generation, clusterCount, diff)
			}
		})
	}
}

func TestTrueResourcePlacementCondition(t *testing.T) {
	const (
		generation   = int64(3)
		clusterCount = 2
	)
	tests := []struct {
		name      string
		condition ResourceCondition
		want      metav1.Condition
	}{
		{
			name:      "rollout started",
			condition: RolloutStartedCondition,
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourcePlacementRolloutStartedConditionType),
				Reason:             RolloutStartedReason,
				Message:            "All 2 cluster(s) start rolling out the latest resource",
				ObservedGeneration: generation,
			},
		},
		{
			name:      "overridden",
			condition: OverriddenCondition,
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourcePlacementOverriddenConditionType),
				Reason:             OverriddenSucceededReason,
				Message:            "The selected resources are successfully overridden in 2 cluster(s)",
				ObservedGeneration: generation,
			},
		},
		{
			name:      "work synchronized",
			condition: WorkSynchronizedCondition,
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourcePlacementWorkSynchronizedConditionType),
				Reason:             WorkSynchronizedReason,
				Message:            "Work(s) are successfully created or updated in 2 target cluster(s)' namespaces",
				ObservedGeneration: generation,
			},
		},
		{
			name:      "applied",
			condition: AppliedCondition,
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourcePlacementAppliedConditionType),
				Reason:             ApplySucceededReason,
				Message:            "The selected resources are successfully applied to 2 cluster(s)",
				ObservedGeneration: generation,
			},
		},
		{
			name:      "available",
			condition: AvailableCondition,
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourcePlacementAvailableConditionType),
				Reason:             AvailableReason,
				Message:            "The selected resources in 2 cluster(s) are available now",
				ObservedGeneration: generation,
			},
		},
		{
			name:      "diff reported",
			condition: DiffReportedCondition,
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourcePlacementDiffReportedConditionType),
				Reason:             DiffReportedStatusTrueReason,
				Message:            "Diff reporting in 2 cluster(s) has been completed",
				ObservedGeneration: generation,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.condition.TrueResourcePlacementCondition(generation, clusterCount)
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("TrueResourcePlacementCondition(%d, %d) mismatch (-got, +want):\n%s",
					generation, clusterCount, diff)
			}
		})
	}
}
