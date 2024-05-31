/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workgenerator

import (
	"errors"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/work"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

func TestGetWorkNamePrefixFromSnapshotName(t *testing.T) {
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
			workName, err := getWorkNamePrefixFromSnapshotName(tt.resourceSnapshot)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("failed getWorkNamePrefixFromSnapshotName test `%s` error = %v, wantErr %v", name, err, tt.wantErr)
				return
			}
			if workName != tt.wantedName {
				t.Errorf("getWorkNamePrefixFromSnapshotName test `%s` workName = `%v`, wantedName `%v`", name, workName, tt.wantedName)
			}
		})
	}
}

func TestBuildAllWorkAppliedCondition(t *testing.T) {
	tests := map[string]struct {
		works      map[string]*fleetv1beta1.Work
		generation int64
		want       metav1.Condition
	}{
		"applied should be true if all work applied": {
			works: map[string]*fleetv1beta1.Work{
				"appliedWork1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work1",
						Generation: 123,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
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
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
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
				Reason:             condition.AllWorkAppliedReason,
				ObservedGeneration: 1,
			},
		},
		"applied should be false if not all work applied to the latest generation": {
			works: map[string]*fleetv1beta1.Work{
				"notAppliedWork1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work1",
						Generation: 123,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
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
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
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
				Reason:             condition.WorkNotAppliedReason,
				ObservedGeneration: 1,
			},
		},
		"applied should be false if not all work has applied": {
			works: map[string]*fleetv1beta1.Work{
				"appliedWork1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work1",
						Generation: 123,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
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
				Reason:             condition.WorkNotAppliedReason,
				ObservedGeneration: 1,
			},
		},
		"applied should be false if some work applied condition is unknown": {
			works: map[string]*fleetv1beta1.Work{
				"appliedWork1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work1",
						Generation: 123,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
								Status:             metav1.ConditionUnknown,
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
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
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
				Reason:             condition.WorkNotAppliedReason,
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
				t.Errorf("buildAllWorkAppliedCondition test `%s` mismatch (-got +want):\n%s", name, diff)
			}
		})
	}
}

func TestBuildAllWorkAvailableCondition(t *testing.T) {
	tests := map[string]struct {
		works   map[string]*fleetv1beta1.Work
		binding *fleetv1beta1.ClusterResourceBinding
		want    metav1.Condition
	}{
		"All works are available": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "work1",
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Reason: "any",
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"work2": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "work2",
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Reason: "any",
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
			},
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourceBindingAvailable),
				Reason:             condition.AllWorkAvailableReason,
				ObservedGeneration: 1,
			},
		},
		"All works are available but one of them is not trackable": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "work1",
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Reason: work.WorkNotTrackableReason,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"work2": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "work2",
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Reason: "any",
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
			},
			want: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourceBindingAvailable),
				Reason:             work.WorkNotTrackableReason,
				ObservedGeneration: 1,
			},
		},
		"Not all works are available": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			},
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
			},
			want: metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingAvailable),
				Reason:             condition.WorkNotAvailableReason,
				Message:            "work object work2 is not available",
				ObservedGeneration: 1,
			},
		},
		"Available condition of one work is unknown": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionUnknown,
							},
						},
					},
				},
			},
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
			},
			want: metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingAvailable),
				Reason:             condition.WorkNotAvailableReason,
				Message:            "work object work2 is not available",
				ObservedGeneration: 1,
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildAllWorkAvailableCondition(tt.works, tt.binding)
			if diff := cmp.Diff(got, tt.want, ignoreConditionOption); diff != "" {
				t.Errorf("buildAllWorkAvailableCondition test `%s` mismatch (-got +want):\n%s", name, diff)
			}
		})
	}
}

func TestSetBindingStatus(t *testing.T) {
	tests := map[string]struct {
		works map[string]*fleetv1beta1.Work
		want  []fleetv1beta1.FailedResourcePlacement
	}{
		"NoWorks": {
			works: map[string]*fleetv1beta1.Work{},
			want:  nil,
		},
		"One work has one not available and one work has one not applied": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionFalse,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionTrue,
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionFalse,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			},
			want: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeAvailable,
						Status: metav1.ConditionFalse,
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeApplied,
						Status: metav1.ConditionFalse,
					},
				},
			},
		},
		"One work has one not available and one work all available": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionTrue,
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"all available work": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionFalse,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			},
			want: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeApplied,
						Status: metav1.ConditionFalse,
					},
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			binding := &fleetv1beta1.ClusterResourceBinding{}
			setBindingStatus(tt.works, binding)
			got := binding.Status.FailedPlacements
			sort.Slice(got, func(i, j int) bool {
				if got[i].Group < got[j].Group {
					return true
				}
				if got[i].Kind < got[j].Kind {
					return true
				}
				return got[i].Name < got[j].Name
			})
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("setBindingStatus test `%s` mismatch (-got +want):\n%s", name, diff)
			}
		})
	}
}
