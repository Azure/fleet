package utils

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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
							Type:      fleetv1beta1.ResourceEnvelopeType,
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

func TestShouldPropagateObj(t *testing.T) {
	tests := []struct {
		name            string
		obj             map[string]interface{}
		ownerReferences []metav1.OwnerReference
		enableWorkload  bool
		want            bool
	}{
		{
			name: "standalone replicaset without ownerReferences should propagate",
			obj: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "ReplicaSet",
				"metadata": map[string]interface{}{
					"name":      "standalone-rs",
					"namespace": "default",
				},
			},
			ownerReferences: nil,
			enableWorkload:  true,
			want:            true,
		},
		{
			name: "standalone replicaset without ownerReferences should propagate if workload is disabled",
			obj: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "ReplicaSet",
				"metadata": map[string]interface{}{
					"name":      "standalone-rs",
					"namespace": "default",
				},
			},
			ownerReferences: nil,
			enableWorkload:  false,
			want:            true,
		},
		{
			name: "standalone pod without ownerReferences should propagate",
			obj: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name":      "standalone-pod",
					"namespace": "default",
				},
			},
			ownerReferences: nil,
			enableWorkload:  true,
			want:            true,
		},
		{
			name: "replicaset with deployment owner should NOT propagate",
			obj: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "ReplicaSet",
				"metadata": map[string]interface{}{
					"name":      "test-deploy-abc123",
					"namespace": "default",
				},
			},
			ownerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "test-deploy",
					UID:        "12345",
				},
			},
			enableWorkload: true,
			want:           false,
		},
		{
			name: "pod owned by replicaset - passes ShouldPropagateObj but filtered by resource config",
			obj: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name":      "test-deploy-abc123-xyz",
					"namespace": "default",
				},
			},
			ownerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "ReplicaSet",
					Name:       "test-deploy-abc123",
					UID:        "67890",
				},
			},
			enableWorkload: false,
			want:           true, // ShouldPropagateObj doesn't filter Pods - they're filtered by NewResourceConfig
		},
		{
			name: "controllerrevision owned by daemonset should NOT propagate",
			obj: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "ControllerRevision",
				"metadata": map[string]interface{}{
					"name":      "test-ds-7b9848797f",
					"namespace": "default",
				},
			},
			ownerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Name:       "test-ds",
					UID:        "abcdef",
				},
			},
			enableWorkload: false,
			want:           false,
		},
		{
			name: "controllerrevision owned by statefulset should NOT propagate",
			obj: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "ControllerRevision",
				"metadata": map[string]interface{}{
					"name":      "test-ss-7878b4b446",
					"namespace": "default",
				},
			},
			ownerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Name:       "test-ss",
					UID:        "fedcba",
				},
			},
			enableWorkload: false,
			want:           false,
		},
		{
			name: "standalone controllerrevision without owner should propagate",
			obj: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "ControllerRevision",
				"metadata": map[string]interface{}{
					"name":      "custom-revision",
					"namespace": "default",
				},
			},
			ownerReferences: nil,
			enableWorkload:  false,
			want:            true,
		},
		{
			name: "PVC should propagate when workload is disabled",
			obj: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "PersistentVolumeClaim",
				"metadata": map[string]interface{}{
					"name":      "test-pvc",
					"namespace": "default",
				},
			},
			ownerReferences: nil,
			enableWorkload:  false,
			want:            true,
		},
		{
			name: "PVC should NOT propagate when workload is enabled",
			obj: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "PersistentVolumeClaim",
				"metadata": map[string]interface{}{
					"name":      "test-pvc",
					"namespace": "default",
				},
			},
			ownerReferences: nil,
			enableWorkload:  true,
			want:            false,
		},
		{
			name: "PVC with ownerReferences should NOT propagate when workload is enabled",
			obj: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "PersistentVolumeClaim",
				"metadata": map[string]interface{}{
					"name":      "data-statefulset-0",
					"namespace": "default",
				},
			},
			ownerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Name:       "statefulset",
					UID:        "sts-uid",
				},
			},
			enableWorkload: true,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uObj := &unstructured.Unstructured{Object: tt.obj}
			if tt.ownerReferences != nil {
				uObj.SetOwnerReferences(tt.ownerReferences)
			}

			got, err := ShouldPropagateObj(nil, uObj, tt.enableWorkload)
			if err != nil {
				t.Errorf("ShouldPropagateObj() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("ShouldPropagateObj() = %v, want %v", got, tt.want)
			}
		})
	}
}
