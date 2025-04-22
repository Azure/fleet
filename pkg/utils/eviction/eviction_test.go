/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package eviction

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func TestIsEvictionInTerminalState(t *testing.T) {
	tests := []struct {
		name     string
		eviction *placementv1beta1.ClusterResourcePlacementEviction
		want     bool
	}{
		{
			name: "Invalid eviction - terminal state",
			eviction: &placementv1beta1.ClusterResourcePlacementEviction{
				Status: placementv1beta1.PlacementEvictionStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(placementv1beta1.PlacementEvictionConditionTypeValid),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Executed eviction set to true - terminal state",
			eviction: &placementv1beta1.ClusterResourcePlacementEviction{
				Status: placementv1beta1.PlacementEvictionStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(placementv1beta1.PlacementEvictionConditionTypeValid),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(placementv1beta1.PlacementEvictionConditionTypeExecuted),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Executed eviction set to false - terminal state",
			eviction: &placementv1beta1.ClusterResourcePlacementEviction{
				Status: placementv1beta1.PlacementEvictionStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(placementv1beta1.PlacementEvictionConditionTypeValid),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(placementv1beta1.PlacementEvictionConditionTypeExecuted),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Eviction with only valid condition set to true - non terminal state",
			eviction: &placementv1beta1.ClusterResourcePlacementEviction{
				Status: placementv1beta1.PlacementEvictionStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(placementv1beta1.PlacementEvictionConditionTypeValid),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEvictionInTerminalState(tt.eviction); got != tt.want {
				t.Errorf("IsEvictionInTerminalState test failed got = %v, want = %v", got, tt.want)
			}
		})
	}
}

func TestIsPlacementPresent(t *testing.T) {
	tests := []struct {
		name    string
		binding *placementv1beta1.ClusterResourceBinding
		want    bool
	}{
		{
			name: "Bound binding - placement present",
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateBound,
				},
			},
			want: true,
		},
		{
			name: "Unscheduled binding with previous state bound - placement present",
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateUnscheduled,
				},
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						placementv1beta1.PreviousBindingStateAnnotation: string(placementv1beta1.BindingStateBound),
					},
				},
			},
			want: true,
		},
		{
			name: "Unscheduled binding with no previous state - placement not present",
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateUnscheduled,
				},
			},
			want: false,
		},
		{
			name: "Scheduled binding - placement not present",
			binding: &placementv1beta1.ClusterResourceBinding{
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateScheduled,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPlacementPresent(tt.binding); got != tt.want {
				t.Errorf("IsPlacementPresent test failed got = %v, want = %v", got, tt.want)
			}
		})
	}
}
