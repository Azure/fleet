/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workgenerator

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	workapi "go.goms.io/fleet/pkg/controllers/work"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
)

func Test_getWorkNameFromSnapshotName(t *testing.T) {
	tests := map[string]struct {
		resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot
		wantErr          error
		wantedName       string
	}{
		"the work name is crp name + \"work\", if there is only one resource snapshot": {
			resourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "placement-2",
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: "placement",
					},
				},
			},
			wantErr:    nil,
			wantedName: "placement-work",
		},
		"should return error if the resource snapshot has negative subindex": {
			resourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "placement-1-2",
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: "placement",
					},
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "-1",
					},
				},
			},
			wantErr:    controller.ErrUnexpectedBehavior,
			wantedName: "",
		},
		"the work name is the concatenation of the crp name and subindex start at 0": {
			resourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "placement-1-2",
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: "placement",
					},
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
					},
				},
			},
			wantErr:    nil,
			wantedName: "placement-0",
		},
		"the work name is the concatenation of the crp name and subindex": {
			resourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "placement-1-2",
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: "placement",
					},
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "2",
					},
				},
			},
			wantErr:    nil,
			wantedName: "placement-2",
		},
		"test return error if the resource snapshot has invalid subindex": {
			resourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "placement-1-2",
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: "placement",
					},
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "what?",
					},
				},
			},
			wantErr:    controller.ErrUnexpectedBehavior,
			wantedName: "",
		},
		"test return error if the resource snapshot does not have CRP track": {
			resourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "placement-1-2",
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "what?",
					},
				},
			},
			wantErr:    controller.ErrUnexpectedBehavior,
			wantedName: "",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			workName, err := getWorkNameFromSnapshotName(tt.resourceSnapshot)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("failed getWorkNameFromSnapshotName test `%s` error = %v, wantErr %v", name, err, tt.wantErr)
				return
			}
			if workName != tt.wantedName {
				t.Errorf("getWorkNameFromSnapshotName test `%s` workName = `%v`, wantedName `%v`", name, workName, tt.wantedName)
			}
		})
	}
}

func Test_buildAllWorkAppliedCondition(t *testing.T) {
	tests := map[string]struct {
		works      map[string]*workv1alpha1.Work
		generation int64
		want       metav1.Condition
	}{
		"applied should be true if all work applied": {
			works: map[string]*workv1alpha1.Work{
				"appliedWork1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work1",
						Generation: 123,
					},
					Status: workv1alpha1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               workapi.ConditionTypeApplied,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 123,
							},
						},
					},
				},
				"appliedWork2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work2",
						Generation: 12,
					},
					Status: workv1alpha1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               workapi.ConditionTypeApplied,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 12,
							},
						},
					},
				},
			},
			generation: 1,
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourceBindingApplied),
				Reason:             allWorkAppliedReason,
				ObservedGeneration: 1,
			},
		},
		"applied should be false if not all work applied to the latest generation": {
			works: map[string]*workv1alpha1.Work{
				"notAppliedWork1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work1",
						Generation: 123,
					},
					Status: workv1alpha1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               workapi.ConditionTypeApplied,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 122, // not the latest generation
							},
						},
					},
				},
				"appliedWork2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work2",
						Generation: 12,
					},
					Status: workv1alpha1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               workapi.ConditionTypeApplied,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 12,
							},
						},
					},
				},
			},
			generation: 1,
			want: metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingApplied),
				Reason:             workNotAppliedReason,
				ObservedGeneration: 1,
			},
		},
		"applied should be false if not all work has applied": {
			works: map[string]*workv1alpha1.Work{
				"appliedWork1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work1",
						Generation: 123,
					},
					Status: workv1alpha1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               workapi.ConditionTypeApplied,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 122, // not the latest generation
							},
						},
					},
				},
				"notAppliedWork2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work2",
						Generation: 12,
					},
				},
			},
			generation: 1,
			want: metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingApplied),
				Reason:             workNotAppliedReason,
				ObservedGeneration: 1,
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			binding := &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test",
					Generation: tt.generation,
				},
			}
			got := buildAllWorkAppliedCondition(tt.works, binding)
			if diff := cmp.Diff(got, tt.want, ignoreConditionOption); diff != "" {
				t.Errorf("buildAllWorkAppliedCondition test `%s` mismatch (-want +got):\n%s", name, diff)
			}
		})
	}
}
