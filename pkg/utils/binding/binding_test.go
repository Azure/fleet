/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
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
			name: "apply, available conditions not set for binding",
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
			name: "apply failed binding",
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
			name: "non available binding",
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
			name: "available binding",
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
