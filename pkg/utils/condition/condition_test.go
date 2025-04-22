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
