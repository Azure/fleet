/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package validator

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils/informer"
)

func Test_validateRolloutStrategy(t *testing.T) {
	tests := map[string]struct {
		rolloutStrategy placementv1beta1.RolloutStrategy
		wantErr         bool
	}{
		// TODO: Add test cases.
		"invalid RolloutStrategyType should fail": {
			rolloutStrategy: placementv1beta1.RolloutStrategy{
				Type: "random type",
			},
			wantErr: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if err := validateRolloutStrategy(tt.rolloutStrategy); (err != nil) != tt.wantErr {
				t.Errorf("validateRolloutStrategy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_validateClusterResourcePlacementAlpha(t *testing.T) {
	tests := map[string]struct {
		crp              *fleetv1alpha1.ClusterResourcePlacement
		resourceInformer informer.Manager
		wantErr          bool
	}{
		"valid CRP": {
			crp: &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
				},
			},
			resourceInformer: MockResourceInformer{},
			wantErr:          false,
		},
		"invalid Resource Selector with name & label selector": {
			crp: &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"test-key": "test-value"},
							},
						},
					},
				},
			},
			resourceInformer: MockResourceInformer{},
			wantErr:          true,
		},
		"invalid Resource Selector with invalid label selector": {
			crp: &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "test-key",
										Operator: metav1.LabelSelectorOpIn,
									},
								},
							},
						},
					},
				},
			},
			resourceInformer: MockResourceInformer{},
			wantErr:          true,
		},
		"invalid Resource Selector with invalid cluster resource selector": {
			crp: &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "Role",
							Name:    "test-role",
						},
					},
				},
			},
			resourceInformer: MockResourceInformer{},
			wantErr:          true,
		},
		"nil resource informer": {
			crp: &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
				},
			},
			resourceInformer: nil,
			wantErr:          true,
		},
		"invalid placement policy with invalid label selector": {
			crp: &fleetv1alpha1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
					Policy: &fleetv1alpha1.PlacementPolicy{
						Affinity: &fleetv1alpha1.Affinity{
							ClusterAffinity: &fleetv1alpha1.ClusterAffinity{
								ClusterSelectorTerms: []fleetv1alpha1.ClusterSelectorTerm{
									{
										LabelSelector: metav1.LabelSelector{
											MatchExpressions: []metav1.LabelSelectorRequirement{
												{
													Key:      "test-key",
													Operator: metav1.LabelSelectorOpIn,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			resourceInformer: MockResourceInformer{},
			wantErr:          true,
		},
	}
	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			ResourceInformer = testCase.resourceInformer
			if err := ValidateClusterResourcePlacementAlpha(testCase.crp); (err != nil) != testCase.wantErr {
				t.Errorf("ValidateClusterResourcePlacementAlpha() error = %v, wantErr %v", err, testCase.wantErr)
			}
		})
	}
}

func Test_validateClusterResourcePlacement(t *testing.T) {
	unavailablePeriodSeconds := -10
	tests := map[string]struct {
		crp              *placementv1beta1.ClusterResourcePlacement
		resourceInformer informer.Manager
		wantErr          bool
	}{
		"valid CRP": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					},
				},
			},
			wantErr: false,
		},

		"invalid Resource Selector with name & label selector": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"test-key": "test-value"},
							},
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					},
				},
			},
			wantErr: true,
		},
		"empty rollout strategy": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
				},
			},
			wantErr: true,
		},
		"invalid rollout strategy - UnavailablePeriodSeconds": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: &unavailablePeriodSeconds,
						},
					},
				},
			},
			wantErr: true,
		},
		"invalid rollout strategy - % error MaxUnavailable": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							MaxUnavailable: &intstr.IntOrString{
								Type:   1,
								StrVal: "25",
							},
						},
					},
				},
			},
			wantErr: true,
		},
		"invalid rollout strategy - negative MaxUnavailable": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							MaxUnavailable: &intstr.IntOrString{
								Type:   0,
								IntVal: -10,
							},
						},
					},
				},
			},
			wantErr: true,
		},
		"invalid rollout strategy - % error MaxSurge": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							MaxSurge: &intstr.IntOrString{
								Type:   1,
								StrVal: "25",
							},
						},
					},
				},
			},
			wantErr: true,
		},
		"invalid rollout strategy - negative MaxSurge": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							MaxSurge: &intstr.IntOrString{
								Type:   0,
								IntVal: -10,
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			ResourceInformer = testCase.resourceInformer
			if err := ValidateClusterResourcePlacement(testCase.crp); (err != nil) != testCase.wantErr {
				t.Errorf("ValidateClusterResourcePlacementAlpha() error = %v, wantErr %v", err, testCase.wantErr)
			}
		})
	}
}
