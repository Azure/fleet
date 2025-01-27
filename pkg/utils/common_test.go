package utils

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

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
						ObservedGeneration: 1,
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
						ObservedGeneration: 1,
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
			name: "compare two equal failed resource placements of equal length - only enveloped objects but new generation",
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
			want: false,
		},
		{
			name: "compare two equal failed resource placements of equal length - only enveloped objects, same generation",
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
						ObservedGeneration: 1,
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
						ObservedGeneration: 1,
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

// TestIsDriftedResourcePlacementsEqual tests the IsDriftedResourcePlacementsEqual function.
func TestIsDriftedResourcePlacementsEqual(t *testing.T) {
	now := metav1.Now()

	testCases := []struct {
		name   string
		oldDRP []fleetv1beta1.DriftedResourcePlacement
		newDRP []fleetv1beta1.DriftedResourcePlacement
		want   bool
	}{
		{
			name:   "two empty slices",
			oldDRP: []fleetv1beta1.DriftedResourcePlacement{},
			newDRP: []fleetv1beta1.DriftedResourcePlacement{},
			want:   true,
		},
		{
			name:   "old is nil, new is empty",
			oldDRP: nil,
			newDRP: []fleetv1beta1.DriftedResourcePlacement{},
			want:   true,
		},
		{
			name:   "old is empty, new is nil",
			oldDRP: []fleetv1beta1.DriftedResourcePlacement{},
			newDRP: nil,
			want:   true,
		},
		{
			name: "different length",
			oldDRP: []fleetv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "nginx",
						Namespace: "default",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInHub:    "1",
							ValueInMember: "2",
						},
					},
				},
			},
			newDRP: []fleetv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "nginx",
						Namespace: "default",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInHub:    "1",
							ValueInMember: "2",
						},
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "1",
						},
					},
				},
			},
		},
		{
			name: "same list, different order",
			oldDRP: []fleetv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "nginx",
						Namespace: "default",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInHub:    "1",
							ValueInMember: "2",
						},
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "1",
						},
					},
				},
			},
			newDRP: []fleetv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "1",
						},
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "nginx",
						Namespace: "default",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInHub:    "1",
							ValueInMember: "2",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "same list, same order",
			oldDRP: []fleetv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "1",
						},
					},
				},
			},
			newDRP: []fleetv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "1",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "drift info updated",
			oldDRP: []fleetv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "1",
						},
					},
				},
			},
			newDRP: []fleetv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 metav1.Now(),
					FirstDriftedObservedTime:        now,
					TargetClusterObservedGeneration: 1,
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "2",
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsDriftedResourcePlacementsEqual(tc.oldDRP, tc.newDRP)
			if got != tc.want {
				t.Errorf("IsDriftedResourcePlacementsEqual() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestIsDiffedResourcePlacementEqual tests the IsDiffedResourcePlacementEqual function.
func TestIsDiffedResourcePlacementEqual(t *testing.T) {
	now := metav1.Now()

	testCases := []struct {
		name   string
		oldDRP []fleetv1beta1.DiffedResourcePlacement
		newDRP []fleetv1beta1.DiffedResourcePlacement
		want   bool
	}{
		{
			name:   "two empty slices",
			oldDRP: []fleetv1beta1.DiffedResourcePlacement{},
			newDRP: []fleetv1beta1.DiffedResourcePlacement{},
			want:   true,
		},
		{
			name:   "old is nil, new is empty",
			oldDRP: nil,
			newDRP: []fleetv1beta1.DiffedResourcePlacement{},
			want:   true,
		},
		{
			name:   "old is empty, new is nil",
			oldDRP: []fleetv1beta1.DiffedResourcePlacement{},
			newDRP: nil,
			want:   true,
		},
		{
			name: "different length",
			oldDRP: []fleetv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "nginx",
						Namespace: "default",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInHub:    "1",
							ValueInMember: "2",
						},
					},
				},
			},
			newDRP: []fleetv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "nginx",
						Namespace: "default",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInHub:    "1",
							ValueInMember: "2",
						},
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "1",
						},
					},
				},
			},
		},
		{
			name: "same list, different order",
			oldDRP: []fleetv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "nginx",
						Namespace: "default",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInHub:    "1",
							ValueInMember: "2",
						},
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "1",
						},
					},
				},
			},
			newDRP: []fleetv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "1",
						},
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "nginx",
						Namespace: "default",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInHub:    "1",
							ValueInMember: "2",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "same list, same order",
			oldDRP: []fleetv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "1",
						},
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "nginx",
						Namespace: "default",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInHub:    "1",
							ValueInMember: "2",
						},
					},
				},
			},
			newDRP: []fleetv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "1",
						},
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Name:      "nginx",
						Namespace: "default",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/replicas",
							ValueInHub:    "1",
							ValueInMember: "2",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "diff info updated",
			oldDRP: []fleetv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 now,
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(1)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "1",
						},
					},
				},
			},
			newDRP: []fleetv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "work",
					},
					ObservationTime:                 metav1.Now(),
					FirstDiffedObservedTime:         now,
					TargetClusterObservedGeneration: ptr.To(int64(2)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/custom",
							ValueInMember: "1",
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsDiffedResourcePlacementsEqual(tc.oldDRP, tc.newDRP)
			if got != tc.want {
				t.Errorf("IsDiffedResourcePlacementsEqual() = %v, want %v", got, tc.want)
			}
		})
	}
}
