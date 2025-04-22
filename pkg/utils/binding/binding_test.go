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

package binding

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestHasBindingFailed(t *testing.T) {
	tests := []struct {
		name    string
		binding *placementv1beta1.ClusterResourceBinding
		want    bool
	}{
		{
			name: "binding should not fail if no conditions is set for binding",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 1,
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateBound,
				},
			},
			want: false,
		},
		{
			name: "binding should fail if binding's apply condition is false",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 1,
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateBound,
				},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionFalse,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "applyFailed",
							Message:            "test message",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "binding should fail if binding's available condition is false",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 1,
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateBound,
				},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "applySucceeded",
							Message:            "test message",
						},
						{
							Type:               string(placementv1beta1.ResourceBindingAvailable),
							Status:             metav1.ConditionFalse,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "availableFailed",
							Message:            "test message",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "binding should NOT fail if binding's conditions are true",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 1,
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateBound,
				},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "applySucceeded",
							Message:            "test message",
						},
						{
							Type:               string(placementv1beta1.ResourceBindingAvailable),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "available",
							Message:            "test message",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "binding with overridden condition false",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 1,
				},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingOverridden),
							Status:             metav1.ConditionFalse,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "overriddenFailed",
							Message:            "test message",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "binding with overridden condition true",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 1,
				},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingOverridden),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "overriddenSucceeded",
							Message:            "test message",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "binding with multiple conditions including overridden false",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 1,
				},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "applySucceeded",
							Message:            "test message",
						},
						{
							Type:               string(placementv1beta1.ResourceBindingOverridden),
							Status:             metav1.ConditionFalse,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "overriddenFailed",
							Message:            "test message",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "binding with multiple conditions all true",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 1,
				},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "applySucceeded",
							Message:            "test message",
						},
						{
							Type:               string(placementv1beta1.ResourceBindingOverridden),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "overriddenSucceeded",
							Message:            "test message",
						},
						{
							Type:               string(placementv1beta1.ResourceBindingWorkSynchronized),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "resourceBindingWorkSynchronized",
							Message:            "test message",
						},

						{
							Type:               string(placementv1beta1.ResourceBindingAvailable),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "available",
							Message:            "test message",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "binding with multiple conditions including overridden false and different generation",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 2,
				},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "applySucceeded",
							Message:            "test message",
						},
						{
							Type:               string(placementv1beta1.ResourceBindingOverridden),
							Status:             metav1.ConditionFalse,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "overriddenFailed",
							Message:            "test message",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "binding with multiple conditions all true and different generation",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 2,
				},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "applySucceeded",
							Message:            "test message",
						},
						{
							Type:               string(placementv1beta1.ResourceBindingOverridden),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "overriddenSucceeded",
							Message:            "test message",
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := HasBindingFailed(tc.binding)
			if got != tc.want {
				t.Errorf("HasBindingFailed test `%s` failed got: %v, want: %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestIsBindingReportDiff(t *testing.T) {
	tests := []struct {
		name    string
		binding *placementv1beta1.ClusterResourceBinding
		want    bool
	}{
		{
			name: "binding should not be in diffReported state if diffReport condition is not set",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 1,
				},
			},
			want: false,
		},
		{
			name: "binding should be in diffReported state if diffReport condition is true and generation matches",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 1,
				},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingDiffReported),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "diffReported",
							Message:            "test message",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "binding should be in diffReported state if diffReport condition is current even if its false",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 1,
				},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingDiffReported),
							Status:             metav1.ConditionFalse,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "diffNotReported",
							Message:            "test message",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "binding should NOT be in diffReported state if diffReport condition is not current",
			binding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-binding",
					Generation: 2,
				},
				Status: placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingDiffReported),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{},
							ObservedGeneration: 1,
							Reason:             "diffReported",
							Message:            "test message",
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsBindingDiffReported(tc.binding)
			if got != tc.want {
				t.Errorf("IsBindingDiffReported test `%s` failed got: %v, want: %v", tc.name, got, tc.want)
			}
		})
	}
}
