/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacementeviction

import (
	"testing"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	testCRPName              = "test-crp"
	testDisruptionBudgetName = "test-disruption-budget"
)

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
			name:     "MinAvailable specified as Integer zero, available binding, PickAll CRP - allow eviction",
			crp:      buildTestPickAllCRP(testCRPName),
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
			name:     "MinAvailable specified as Integer greater than one, available binding, PickAll CRP - block eviction",
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
