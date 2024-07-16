package utils

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestIsFailedResourcePlacementsEqual(t *testing.T) {
	time1 := metav1.NewTime(time.Now())
	time2 := metav1.NewTime(time1.Add(-1 * time.Hour))
	tests := []struct {
		name string
		old  []fleetv1beta1.FailedResourcePlacement
		new  []fleetv1beta1.FailedResourcePlacement
		want bool
	}{
		{
			name: "compare two empty failed resource placements, where old, new are non nil",
			old:  []fleetv1beta1.FailedResourcePlacement{},
			new:  []fleetv1beta1.FailedResourcePlacement{},
			want: true,
		},
		{
			name: "compare two empty failed resource placements, where new is nil",
			old:  []fleetv1beta1.FailedResourcePlacement{},
			new:  nil,
			want: true,
		},
		{
			name: "compare two empty failed resource placements, where old is nil",
			old:  nil,
			new:  []fleetv1beta1.FailedResourcePlacement{},
			want: true,
		},
		{
			name: "compare two equal failed resource placements of equal length - mix of regular and enveloped objects",
			old: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "deployment1",
						Namespace: "default",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-envelope-object",
							Namespace: "default",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 0,
						LastTransitionTime: time1,
						Reason:             "ManifestApplyFailed",
						Message:            "message1",
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "StatefulSet",
						Name:      "statefulset1",
						Namespace: "default",
						Envelope:  nil,
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 0,
						LastTransitionTime: time1,
						Reason:             "WorkNotAvailableYet",
						Message:            "message1",
					},
				},
			},
			new: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "StatefulSet",
						Name:      "statefulset1",
						Namespace: "default",
						Envelope:  nil,
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						LastTransitionTime: time2,
						Reason:             "WorkNotAvailableYet",
						Message:            "message2",
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "deployment1",
						Namespace: "default",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-envelope-object",
							Namespace: "default",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						LastTransitionTime: time2,
						Reason:             "ManifestApplyFailed",
						Message:            "message2",
					},
				},
			},
			want: true,
		},
		{
			name: "compare two equal failed resource placements of equal length - only enveloped objects",
			old: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "deployment1",
						Namespace: "default",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-envelope-object",
							Namespace: "default",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 0,
						LastTransitionTime: time1,
						Reason:             "ManifestApplyFailed",
						Message:            "message1",
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "StatefulSet",
						Name:      "statefulset1",
						Namespace: "default",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-envelope-object",
							Namespace: "default",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 0,
						LastTransitionTime: time1,
						Reason:             "ManifestApplyFailed",
						Message:            "message1",
					},
				},
			},
			new: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "StatefulSet",
						Name:      "statefulset1",
						Namespace: "default",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-envelope-object",
							Namespace: "default",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						LastTransitionTime: time2,
						Reason:             "ManifestApplyFailed",
						Message:            "message2",
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "deployment1",
						Namespace: "default",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-envelope-object",
							Namespace: "default",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						LastTransitionTime: time2,
						Reason:             "ManifestApplyFailed",
						Message:            "message2",
					},
				},
			},
			want: true,
		},
		{
			name: "compare two non-equal failed resource placements of equal length - resource identifiers are different",
			old: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "deployment1",
						Namespace: "default",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-envelope-object",
							Namespace: "default",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 0,
						LastTransitionTime: time1,
						Reason:             "ManifestApplyFailed",
						Message:            "message1",
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "StatefulSet",
						Name:      "statefulset1",
						Namespace: "default",
						Envelope:  nil,
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 0,
						LastTransitionTime: time1,
						Reason:             "WorkNotAvailableYet",
						Message:            "message1",
					},
				},
			},
			new: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "StatefulSet",
						Name:      "statefulset2",
						Namespace: "default",
						Envelope:  nil,
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						LastTransitionTime: time2,
						Reason:             "WorkNotAvailableYet",
						Message:            "message2",
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "deployment1",
						Namespace: "default",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-envelope-object",
							Namespace: "default",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						LastTransitionTime: time2,
						Reason:             "ManifestApplyFailed",
						Message:            "message2",
					},
				},
			},
			want: false,
		},
		{
			name: "compare two non-equal failed resource placements of equal length - conditions are different",
			old: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "deployment1",
						Namespace: "default",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-envelope-object",
							Namespace: "default",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 0,
						LastTransitionTime: time1,
						Reason:             "ManifestApplyFailed",
						Message:            "message1",
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "StatefulSet",
						Name:      "statefulset1",
						Namespace: "default",
						Envelope:  nil,
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 0,
						LastTransitionTime: time1,
						Reason:             "WorkNotAvailableYet",
						Message:            "message1",
					},
				},
			},
			new: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "StatefulSet",
						Name:      "statefulset1",
						Namespace: "default",
						Envelope:  nil,
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						LastTransitionTime: time2,
						Reason:             "WorkNotAvailableYet",
						Message:            "message2",
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "deployment1",
						Namespace: "default",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-envelope-object",
							Namespace: "default",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						LastTransitionTime: time2,
						Reason:             "WorkNotAvailableYet",
						Message:            "message2",
					},
				},
			},
			want: false,
		},
		{
			name: "compare two non-equal, non-empty failed resource placements of different length",
			old: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "deployment1",
						Namespace: "default",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-envelope-object",
							Namespace: "default",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 0,
						LastTransitionTime: time1,
						Reason:             "ManifestApplyFailed",
						Message:            "message1",
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "StatefulSet",
						Name:      "statefulset1",
						Namespace: "default",
						Envelope:  nil,
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 0,
						LastTransitionTime: time1,
						Reason:             "WorkNotAvailableYet",
						Message:            "message1",
					},
				},
			},
			new: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "deployment1",
						Namespace: "default",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-envelope-object",
							Namespace: "default",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						LastTransitionTime: time2,
						Reason:             "ManifestApplyFailed",
						Message:            "message2",
					},
				},
			},
			want: false,
		},
		{
			name: "compare two non-equal failed resource placements of different length, new is empty",
			old: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "deployment1",
						Namespace: "default",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-envelope-object",
							Namespace: "default",
							Type:      fleetv1beta1.ConfigMapEnvelopeType,
						},
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeApplied,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 0,
						LastTransitionTime: time1,
						Reason:             "ManifestApplyFailed",
						Message:            "message1",
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "StatefulSet",
						Name:      "statefulset1",
						Namespace: "default",
						Envelope:  nil,
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 0,
						LastTransitionTime: time1,
						Reason:             "WorkNotAvailableYet",
						Message:            "message1",
					},
				},
			},
			new:  []fleetv1beta1.FailedResourcePlacement{},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsFailedResourcePlacementsEqual(tc.old, tc.new)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("IsFailedResourcePlacementsEqual() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
