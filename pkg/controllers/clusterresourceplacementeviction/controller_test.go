/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacementeviction

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	testBindingName          = "test-binding"
	testClusterName          = "test-cluster"
	testCRPName              = "test-crp"
	testDisruptionBudgetName = "test-disruption-budget"
	testEvictionName         = "test-eviction"
)

func TestValidateEviction(t *testing.T) {
	testCRP := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: testCRPName,
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
		},
	}
	testBinding1 := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-binding-1",
			Labels: map[string]string{placementv1beta1.CRPTrackingLabel: testCRPName},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:         placementv1beta1.BindingStateUnscheduled,
			TargetCluster: "test-cluster",
		},
	}
	testBinding2 := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-binding-2",
			Labels: map[string]string{placementv1beta1.CRPTrackingLabel: testCRPName},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:         placementv1beta1.BindingStateScheduled,
			TargetCluster: "test-cluster",
		},
	}
	tests := []struct {
		name                         string
		eviction                     *placementv1alpha1.ClusterResourcePlacementEviction
		crp                          *placementv1beta1.ClusterResourcePlacement
		bindings                     []placementv1beta1.ClusterResourceBinding
		wantValidationResult         *evictionValidationResult
		wantEvictionInvalidCondition *metav1.Condition
		wantErr                      error
	}{
		{
			name:     "invalid eviction - CRP not found",
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			wantValidationResult: &evictionValidationResult{
				isValid: false,
			},
			wantEvictionInvalidCondition: &metav1.Condition{
				Type:               string(placementv1alpha1.PlacementEvictionConditionTypeValid),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				Reason:             clusterResourcePlacementEvictionInvalidReason,
				Message:            evictionInvalidMissingCRPMessage,
			},
			wantErr: nil,
		},
		{
			name:     "invalid eviction - deleting CRP",
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:              testCRPName,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"test-finalizer"},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
				},
			},
			wantValidationResult: &evictionValidationResult{
				isValid: false,
			},
			wantEvictionInvalidCondition: &metav1.Condition{
				Type:               string(placementv1alpha1.PlacementEvictionConditionTypeValid),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				Reason:             clusterResourcePlacementEvictionInvalidReason,
				Message:            evictionInvalidDeletingCRPMessage,
			},
			wantErr: nil,
		},
		{
			name:     "invalid eviction - multiple CRBs for same cluster",
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			crp:      testCRP,
			bindings: []placementv1beta1.ClusterResourceBinding{
				testBinding1, testBinding2,
			},
			wantValidationResult: &evictionValidationResult{
				isValid:  false,
				crp:      testCRP,
				bindings: []placementv1beta1.ClusterResourceBinding{testBinding1, testBinding2},
			},
			wantEvictionInvalidCondition: &metav1.Condition{
				Type:               string(placementv1alpha1.PlacementEvictionConditionTypeValid),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				Reason:             clusterResourcePlacementEvictionInvalidReason,
				Message:            evictionInvalidMultipleCRBMessage,
			},
			wantErr: nil,
		},
		{
			name:     "invalid eviction - CRB not found",
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			crp:      testCRP,
			wantValidationResult: &evictionValidationResult{
				isValid:  false,
				crp:      testCRP,
				bindings: []placementv1beta1.ClusterResourceBinding{},
			},
			wantEvictionInvalidCondition: &metav1.Condition{
				Type:               string(placementv1alpha1.PlacementEvictionConditionTypeValid),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				Reason:             clusterResourcePlacementEvictionInvalidReason,
				Message:            evictionInvalidMissingCRBMessage,
			},
			wantErr: nil,
		},
		{
			name:     "valid eviction",
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			crp:      testCRP,
			bindings: []placementv1beta1.ClusterResourceBinding{testBinding2},
			wantValidationResult: &evictionValidationResult{
				isValid:  true,
				crp:      testCRP,
				crb:      &testBinding2,
				bindings: []placementv1beta1.ClusterResourceBinding{testBinding2},
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var objects []client.Object
			if tc.crp != nil {
				objects = append(objects, tc.crp)
			}
			for i := range tc.bindings {
				objects = append(objects, &tc.bindings[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client: fakeClient,
			}
			gotValidationResult, gotErr := r.validateEviction(ctx, tc.eviction)
			if diff := cmp.Diff(tc.wantValidationResult, gotValidationResult, cmp.AllowUnexported(evictionValidationResult{}), cmpopts.IgnoreFields(placementv1beta1.ClusterResourceBinding{}, "ResourceVersion")); diff != "" {
				t.Errorf("validateEviction() validation result mismatch (-want, +got):\n%s", diff)
			}
			gotInvalidCondition := tc.eviction.GetCondition(string(placementv1alpha1.PlacementEvictionConditionTypeValid))
			if diff := cmp.Diff(tc.wantEvictionInvalidCondition, gotInvalidCondition, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")); diff != "" {
				t.Errorf("validateEviction() eviction invalid condition mismatch (-want, +got):\n%s", diff)
			}
			if tc.wantErr == nil {
				if gotErr != nil {
					t.Errorf("test case `%s` didn't return the expected error,  want no error, got error = %+v ", tc.name, gotErr)
				}
			} else if gotErr == nil || gotErr.Error() != tc.wantErr.Error() {
				t.Errorf("test case `%s` didn't return the expected error, want error = %+v, got error = %+v", tc.name, tc.wantErr, gotErr)
			}
		})
	}
}

func TestDeleteClusterResourceBinding(t *testing.T) {
	tests := []struct {
		name          string
		inputBinding  *placementv1beta1.ClusterResourceBinding
		storedBinding *placementv1beta1.ClusterResourceBinding
		wantErr       error
	}{
		{
			name: "conflict on delete - pre-conditions don't match",
			inputBinding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:            testBindingName,
					ResourceVersion: "2",
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateBound,
				},
			},
			storedBinding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:            testBindingName,
					ResourceVersion: "1",
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateBound,
				},
			},
			wantErr: errors.New("object might have been modified"),
		},
		{
			name: "successful delete",
			inputBinding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:            testBindingName,
					ResourceVersion: "1",
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateBound,
				},
			},
			storedBinding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:            testBindingName,
					ResourceVersion: "1",
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateBound,
				},
			},
			wantErr: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var objects []client.Object
			if tc.storedBinding != nil {
				objects = append(objects, tc.storedBinding)
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client: fakeClient,
			}
			gotErr := r.deleteClusterResourceBinding(ctx, tc.inputBinding)
			if tc.wantErr == nil {
				if gotErr != nil {
					t.Errorf("test case `%s` didn't return the expected error,  want no error, got error = %+v ", tc.name, gotErr)
				}
			} else if gotErr == nil || !strings.Contains(gotErr.Error(), tc.wantErr.Error()) {
				t.Errorf("test case `%s` didn't return the expected error, want error = %+v, got error = %+v", tc.name, tc.wantErr, gotErr)
			}
		})
	}
}

func TestExecuteEviction(t *testing.T) {
	availableBinding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testBindingName,
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
					Reason:             "applied",
					ObservedGeneration: 1,
				},
				{
					Type:               string(placementv1beta1.ResourceBindingAvailable),
					Status:             metav1.ConditionTrue,
					Reason:             "available",
					ObservedGeneration: 1,
				},
			},
		},
	}
	tests := []struct {
		name                          string
		validationResult              *evictionValidationResult
		eviction                      *placementv1alpha1.ClusterResourcePlacementEviction
		pdb                           *placementv1alpha1.ClusterResourcePlacementDisruptionBudget
		wantEvictionExecutedCondition *metav1.Condition
		wantErr                       error
	}{
		{
			name: "scheduled binding - eviction not executed",
			validationResult: &evictionValidationResult{
				crb: &placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: testBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateScheduled,
					},
				},
			},
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			wantEvictionExecutedCondition: &metav1.Condition{
				Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				Reason:             clusterResourcePlacementEvictionNotExecutedReason,
				Message:            evictionBlockedMissingPlacementMessage,
			},
			wantErr: nil,
		},
		{
			name: "unscheduled binding with previous state annotation doesn't exist - eviction not executed",
			validationResult: &evictionValidationResult{
				crb: &placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: testBindingName,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateUnscheduled,
					},
				},
			},
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			wantEvictionExecutedCondition: &metav1.Condition{
				Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				Reason:             clusterResourcePlacementEvictionNotExecutedReason,
				Message:            evictionBlockedMissingPlacementMessage,
			},
			wantErr: nil,
		},
		{
			name: "unscheduled binding with previous state as scheduled - eviction not executed",
			validationResult: &evictionValidationResult{
				crb: &placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:        testBindingName,
						Annotations: map[string]string{placementv1beta1.PreviousBindingStateAnnotation: string(placementv1beta1.BindingStateScheduled)},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateUnscheduled,
					},
				},
			},
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			wantEvictionExecutedCondition: &metav1.Condition{
				Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				Reason:             clusterResourcePlacementEvictionNotExecutedReason,
				Message:            evictionBlockedMissingPlacementMessage,
			},
			wantErr: nil,
		},
		{
			name: "deleting binding - eviction executed",
			validationResult: &evictionValidationResult{
				crb: &placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:              testBindingName,
						Annotations:       map[string]string{placementv1beta1.PreviousBindingStateAnnotation: string(placementv1beta1.BindingStateBound)},
						Finalizers:        []string{"test-finalizer"},
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State: placementv1beta1.BindingStateUnscheduled,
					},
				},
			},
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			wantEvictionExecutedCondition: &metav1.Condition{
				Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				Reason:             clusterResourcePlacementEvictionExecutedReason,
				Message:            evictionAllowedPlacementRemovedMessage,
			},
			wantErr: nil,
		},
		{
			name: "failed to apply binding - eviction executed",
			validationResult: &evictionValidationResult{
				crb: &placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:       testBindingName,
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
								Reason:             "applied",
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			wantEvictionExecutedCondition: &metav1.Condition{
				Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				Reason:             clusterResourcePlacementEvictionExecutedReason,
				Message:            evictionAllowedPlacementFailedMessage,
			},
			wantErr: nil,
		},
		{
			name: "failed to be available binding - eviction executed",
			validationResult: &evictionValidationResult{
				crb: &placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:       testBindingName,
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
								Reason:             "applied",
								ObservedGeneration: 1,
							},
							{
								Type:               string(placementv1beta1.ResourceBindingAvailable),
								Status:             metav1.ConditionFalse,
								Reason:             "available",
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			wantEvictionExecutedCondition: &metav1.Condition{
				Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				Reason:             clusterResourcePlacementEvictionExecutedReason,
				Message:            evictionAllowedPlacementFailedMessage,
			},
			wantErr: nil,
		},
		{
			name: "pdb not found - eviction executed",
			validationResult: &evictionValidationResult{
				crb: availableBinding,
				crp: &placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: testCRPName,
					},
				},
			},
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			wantEvictionExecutedCondition: &metav1.Condition{
				Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				Reason:             clusterResourcePlacementEvictionExecutedReason,
				Message:            evictionAllowedNoPDBMessage,
			},
			wantErr: nil,
		},
		{
			name: "PickAll CRP, Misconfigured PDB MaxUnavailable specified - eviction not executed",
			validationResult: &evictionValidationResult{
				crb: availableBinding,
				crp: &placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: testCRPName,
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
			},
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			pdb: &placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			wantEvictionExecutedCondition: &metav1.Condition{
				Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				Reason:             clusterResourcePlacementEvictionNotExecutedReason,
				Message:            evictionBlockedMisconfiguredPDBSpecifiedMessage,
			},
			wantErr: nil,
		},
		{
			name: "PickAll CRP policy not specified, Misconfigured PDB MinAvailable specified as percentage - eviction not executed",
			validationResult: &evictionValidationResult{
				crb: availableBinding,
				crp: &placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: testCRPName,
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{},
				},
			},
			eviction: buildTestEviction(testEvictionName, testCRPName, testClusterName),
			pdb: &placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "10%",
					},
				},
			},
			wantEvictionExecutedCondition: &metav1.Condition{
				Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 1,
				Reason:             clusterResourcePlacementEvictionNotExecutedReason,
				Message:            evictionBlockedMisconfiguredPDBSpecifiedMessage,
			},
			wantErr: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var objects []client.Object
			if tc.pdb != nil {
				objects = append(objects, tc.pdb)
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			r := Reconciler{
				Client: fakeClient,
			}
			gotErr := r.executeEviction(ctx, tc.validationResult, tc.eviction)
			gotExecutedCondition := tc.eviction.GetCondition(string(placementv1alpha1.PlacementEvictionConditionTypeExecuted))
			if diff := cmp.Diff(tc.wantEvictionExecutedCondition, gotExecutedCondition, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")); diff != "" {
				t.Errorf("executeEviction() eviction executed condition mismatch (-want, +got):\n%s", diff)
			}
			if tc.wantErr == nil {
				if gotErr != nil {
					t.Errorf("test case `%s` didn't return the expected error,  want no error, got error = %+v ", tc.name, gotErr)
				}
			} else if gotErr == nil || gotErr.Error() != tc.wantErr.Error() {
				t.Errorf("test case `%s` didn't return the expected error, want error = %+v, got error = %+v", tc.name, tc.wantErr, gotErr)
			}
		})
	}
}

func TestIsEvictionAllowed(t *testing.T) {
	availableCondition := metav1.Condition{
		Type:               string(placementv1beta1.ResourceBindingAvailable),
		Status:             metav1.ConditionTrue,
		Reason:             "available",
		ObservedGeneration: 0,
	}
	scheduledUnavailableBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "scheduled-binding",
			Labels: map[string]string{placementv1beta1.CRPTrackingLabel: testCRPName},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:         placementv1beta1.BindingStateScheduled,
			TargetCluster: "test-cluster-1",
		},
	}
	boundAvailableBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "bound-available-binding",
			Labels: map[string]string{placementv1beta1.CRPTrackingLabel: testCRPName},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:         placementv1beta1.BindingStateBound,
			TargetCluster: "test-cluster-2",
		},
		Status: placementv1beta1.ResourceBindingStatus{
			Conditions: []metav1.Condition{availableCondition},
		},
	}
	anotherBoundAvailableBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "another-bound-available-binding",
			Labels: map[string]string{placementv1beta1.CRPTrackingLabel: testCRPName},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:         placementv1beta1.BindingStateBound,
			TargetCluster: "test-cluster-3",
		},
		Status: placementv1beta1.ResourceBindingStatus{
			Conditions: []metav1.Condition{availableCondition},
		},
	}
	boundUnavailableBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "bound-unavailable-binding",
			Labels: map[string]string{placementv1beta1.CRPTrackingLabel: testCRPName},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:         placementv1beta1.BindingStateBound,
			TargetCluster: "test-cluster-4",
		},
	}
	unScheduledAvailableBinding := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "unscheduled-available-binding",
			Labels: map[string]string{placementv1beta1.CRPTrackingLabel: testCRPName},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:         placementv1beta1.BindingStateUnscheduled,
			TargetCluster: "test-cluster-5",
		},
		Status: placementv1beta1.ResourceBindingStatus{
			Conditions: []metav1.Condition{availableCondition},
		},
	}
	tests := []struct {
		name                  string
		crp                   placementv1beta1.ClusterResourcePlacement
		bindings              []placementv1beta1.ClusterResourceBinding
		disruptionBudget      placementv1alpha1.ClusterResourcePlacementDisruptionBudget
		wantAllowed           bool
		wantAvailableBindings int
	}{
		{
			name:     "MaxUnavailable specified as Integer zero, one available binding - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 1),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 1,
		},
		{
			name:     "MaxUnavailable specified as Integer zero, one unavailable bindings - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 1),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 0,
		},
		{
			name:     "MaxUnavailable specified as Integer one, one unavailable binding - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 1),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 0,
		},
		{
			name:     "MaxUnavailable specified as Integer one, one available binding, upscaling - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 2),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 1,
		},
		{
			name:     "MaxUnavailable specified as Integer one, one available, one unavailable binding - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 2),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, boundUnavailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 1,
		},
		{
			name:     "MaxUnavailable specified as Integer one, two available binding - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 1),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 2,
		},
		{
			name:     "MaxUnavailable specified as Integer one, available bindings greater than target, downscaling - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 1),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, anotherBoundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 3,
		},
		{
			name:     "MaxUnavailable specified as Integer greater than one - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 4),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding, boundAvailableBinding, anotherBoundAvailableBinding, boundUnavailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 2,
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 2,
		},
		{
			name:     "MaxUnavailable specified as Integer greater than one - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 3),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding, boundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 2,
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 2,
		},
		{
			name:     "MaxUnavailable specified as Integer large number greater than target number - allows eviction",
			crp:      buildTestPickNCRP(testCRPName, 4),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding, boundAvailableBinding, boundUnavailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 10,
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 2,
		},
		{
			name:     "MaxUnavailable specified as percentage zero - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 2),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "0%",
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 2,
		},
		{
			name:     "MaxUnavailable specified as percentage greater than zero, rounds up to 1 - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 1),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "10%",
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 0,
		},
		{
			name:     "MaxUnavailable specified as percentage greater than zero, rounds up to 1 - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 1),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "10%",
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 1,
		},
		{
			name:     "MaxUnavailable specified as percentage greater than zero, rounds up to greater than 1 - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 4),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding, boundAvailableBinding, boundUnavailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{ // equates to 2.
						Type:   intstr.String,
						StrVal: "40%",
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 2,
		},
		{
			name:     "MaxUnavailable specified as percentage greater than zero, rounds up to greater than 1 - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 3),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding, boundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{ // equates to 2.
						Type:   intstr.String,
						StrVal: "50%",
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 2,
		},
		{
			name:     "MaxUnavailable specified as percentage hundred, target number greater than bindings - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 10),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding, boundAvailableBinding, boundUnavailableBinding, anotherBoundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{ // equates to 10.
						Type:   intstr.String,
						StrVal: "100%",
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 3,
		},
		{
			name:     "MaxUnavailable specified as percentage hundred, target number equal to bindings - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 2),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding, boundUnavailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{ // equates to 2.
						Type:   intstr.String,
						StrVal: "100%",
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 0,
		},
		{
			name:     "MaxUnavailable specified as percentage hundred, target number equal to bindings - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 4),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding, boundAvailableBinding, boundUnavailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{ // equates to 4.
						Type:   intstr.String,
						StrVal: "100%",
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 2,
		},
		{
			name:     "MinAvailable specified as Integer zero, unavailable binding - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 2),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 0,
		},
		{
			name:     "MinAvailable specified as Integer zero, available binding - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 2),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 1,
		},
		{
			name:     "MinAvailable specified as Integer one, unavailable binding - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 1),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 0,
		},
		{
			name:     "MinAvailable specified as Integer one, available binding, upscaling - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 2),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 1,
		},
		{
			name:     "MinAvailable specified as Integer one, one available, one unavailable binding - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 1),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, boundUnavailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 1,
		},
		{
			name:     "MinAvailable specified as Integer one, two available bindings - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 2),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 2,
		},
		{
			name:     "MinAvailable specified as Integer one, available bindings greater than target number, downscaling - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 1),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, anotherBoundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 3,
		},
		{
			name:     "MinAvailable specified as Integer greater than one - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 2),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 2,
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 2,
		},
		{
			name:     "MinAvailable specified as Integer greater than one - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 4),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding, boundAvailableBinding, anotherBoundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 2,
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 3,
		},
		{
			name:     "MinAvailable specified as Integer greater than one, available bindings greater than target number, downscaling - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 1),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, anotherBoundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 3,
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 3,
		},
		{
			name:     "MinAvailable specified as Integer large number greater than target number - blocks eviction",
			crp:      buildTestPickNCRP(testCRPName, 5),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding, boundAvailableBinding, anotherBoundAvailableBinding, boundUnavailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 10,
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 3,
		},
		{
			name:     "MinAvailable specified as percentage zero, all bindings are unavailable - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 2),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding, boundUnavailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "0%",
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 0,
		},
		{
			name:     "MinAvailable specified as percentage zero, all bindings are available - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 3),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, anotherBoundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "0%",
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 3,
		},
		{
			name:     "MinAvailable specified as percentage rounds upto one - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 1),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "10%",
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 0,
		},
		{
			name:     "MinAvailable specified as percentage rounds upto one - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 2),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "10%",
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 2,
		},
		{
			name:     "MinAvailable specified as percentage greater than zero, rounds up to greater than 1 - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 3),
			bindings: []placementv1beta1.ClusterResourceBinding{scheduledUnavailableBinding, boundAvailableBinding, anotherBoundAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{ // equates to 2.
						Type:   intstr.String,
						StrVal: "40%",
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 2,
		},
		{
			name:     "MinAvailable specified as percentage greater than zero, rounds up to greater than 1 - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 3),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, anotherBoundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{ // equates to 2.
						Type:   intstr.String,
						StrVal: "40%",
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 3,
		},
		{
			name:     "MinAvailable specified as percentage hundred, bindings less than target number - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 10),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, anotherBoundAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{ // equates to 10.
						Type:   intstr.String,
						StrVal: "100%",
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 2,
		},
		{
			name:     "MinAvailable specified as percentage hundred, bindings equal to target number  - block eviction",
			crp:      buildTestPickNCRP(testCRPName, 3),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, anotherBoundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{ // equates to 3.
						Type:   intstr.String,
						StrVal: "100%",
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 3,
		},
		{
			name:     "MinAvailable specified as percentage hundred, bindings greater than target number - allow eviction",
			crp:      buildTestPickNCRP(testCRPName, 2),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, anotherBoundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{ // equates to 2.
						Type:   intstr.String,
						StrVal: "100%",
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 3,
		},
		{
			name: "MinAvailable specified as Integer zero, available binding, PickAll CRP, no policy specified - allow eviction",
			crp: placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRPName,
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{},
			},
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 1,
		},
		{
			name:     "MinAvailable specified as Integer one, available binding, PickAll CRP - block eviction",
			crp:      buildTestPickAllCRP(testCRPName),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			wantAllowed:           false,
			wantAvailableBindings: 1,
		},
		{
			name:     "MinAvailable specified as Integer greater than one, available binding, PickAll CRP - allow eviction",
			crp:      buildTestPickAllCRP(testCRPName),
			bindings: []placementv1beta1.ClusterResourceBinding{boundAvailableBinding, anotherBoundAvailableBinding, unScheduledAvailableBinding},
			disruptionBudget: placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDisruptionBudgetName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 2,
					},
				},
			},
			wantAllowed:           true,
			wantAvailableBindings: 3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotAllowed, gotAvailableBindings := isEvictionAllowed(tc.bindings, tc.crp, tc.disruptionBudget)
			if gotAllowed != tc.wantAllowed {
				t.Errorf("isEvictionAllowed test `%s` failed gotAllowed: %v, wantAllowed: %v", tc.name, gotAllowed, tc.wantAllowed)
			}
			if gotAvailableBindings != tc.wantAvailableBindings {
				t.Errorf("isEvictionAllowed test `%s` failed gotAvailableBindings: %v, wantAvailableBindings: %v", tc.name, gotAvailableBindings, tc.wantAvailableBindings)
			}
		})
	}
}

func buildTestPickAllCRP(crpName string) placementv1beta1.ClusterResourcePlacement {
	return placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpName,
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
		},
	}
}

func serviceScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add placement v1beta1 scheme: %v", err)
	}
	if err := placementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add v1alpha1 scheme: %v", err)
	}
	return scheme
}
