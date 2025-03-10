package eviction

import (
	"testing"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
				t.Errorf("IsEvictionInTerminalState test failed got = %v, want %v", got, tt.want)
			}
		})
	}
}
