/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package validator

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils/informer"
)

var (
	unavailablePeriodSeconds       = -10
	positiveNumberOfClusters int32 = 1
	negativeNumberOfClusters int32 = -1
	resourceSelector               = placementv1beta1.ClusterResourceSelector{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "ClusterRole",
		Name:    "test-cluster-role",
	}
)

func TestValidateClusterResourcePlacementAlpha(t *testing.T) {
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

func TestValidateClusterResourcePlacement(t *testing.T) {
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
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					},
				},
			},
			wantErr: false,
		},
		"CRP with invalid name": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp-with-very-long-name-field-exceeding-DNS1035LabelMaxLength",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					},
				},
			},
			wantErr: true,
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
	}
	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			ResourceInformer = testCase.resourceInformer
			if err := ValidateClusterResourcePlacement(testCase.crp); (err != nil) != testCase.wantErr {
				t.Errorf("ValidateClusterResourcePlacement() error = %v, wantErr %v", err, testCase.wantErr)
			}
		})
	}
}

func TestValidateClusterResourcePlacement_RolloutStrategy(t *testing.T) {
	tests := map[string]struct {
		crp              *placementv1beta1.ClusterResourcePlacement
		resourceInformer informer.Manager
		wantErr          bool
	}{
		"empty rollout strategy": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
				},
			},
			wantErr: false,
		},
		"invalid rollout strategy": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: "random type",
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
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
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
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
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
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
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
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
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
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
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
				t.Errorf("ValidateClusterResourcePlacement_RolloutStrategy() error = %v, wantErr %v", err, testCase.wantErr)
			}
		})
	}
}

func TestValidateClusterResourcePlacement_PickFixedPlacementPolicy(t *testing.T) {
	tests := map[string]struct {
		crp              *placementv1beta1.ClusterResourcePlacement
		resourceInformer informer.Manager
		wantErr          bool
	}{
		"valid placement policy - PickFixed with non-empty cluster names": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{"test-cluster"},
					},
				},
			},
			wantErr: false,
		},
		"invalid placement policy - PickFixed with empty cluster names": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					},
				},
			},
			wantErr: true,
		},
		"invalid placement policy - PickFixed with non nil number of clusters": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickFixedPlacementType,
						ClusterNames:     []string{"test-cluster"},
						NumberOfClusters: &positiveNumberOfClusters,
					},
				},
			},
			wantErr: true,
		},
		"invalid placement policy - PickFixed with non nil affinity": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{"test-cluster"},
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: metav1.LabelSelector{
												MatchLabels: map[string]string{"test-key": "test-value"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		"invalid placement policy - PickFixed with non empty topology constraints": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{"test-cluster"},
						TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
							{
								TopologyKey: "test-key",
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
				t.Errorf("ValidateClusterResourcePlacement_PickFixedPlacementPolicy() error = %v, wantErr %v", err, testCase.wantErr)
			}
		})
	}
}

func TestValidateClusterResourcePlacement_PickAllPlacementPolicy(t *testing.T) {
	tests := map[string]struct {
		crp              *placementv1beta1.ClusterResourcePlacement
		resourceInformer informer.Manager
		wantErr          bool
	}{
		"invalid placement policy - PickAll with non-empty cluster names": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						ClusterNames:  []string{"test-cluster"},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					},
				},
			},
			wantErr: true,
		},
		"invalid placement policy - PickAll with non nil number of clusters": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickFixedPlacementType,
						NumberOfClusters: &positiveNumberOfClusters,
					},
				},
			},
			wantErr: true,
		},
		"invalid placement policy - PickAll with invalid label selector terms in RequiredDuringSchedulingIgnoredDuringExecution in affinity": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
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
			},
			wantErr: true,
		},
		"invalid placement policy - PickAll with non empty PreferredDuringSchedulingIgnoredDuringExecution": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 1,
										Preference: placementv1beta1.ClusterSelectorTerm{
											LabelSelector: metav1.LabelSelector{
												MatchLabels: map[string]string{"test-key": "test-value"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		"invalid placement policy - PickAll with non empty topology constraints": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
							{
								TopologyKey: "test-key",
							},
						},
					},
				},
			},
			wantErr: true,
		},
		"valid placement policy - PickAll with non nil affinity": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: metav1.LabelSelector{
												MatchLabels: map[string]string{"test-key": "test-value"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			ResourceInformer = testCase.resourceInformer
			if err := ValidateClusterResourcePlacement(testCase.crp); (err != nil) != testCase.wantErr {
				t.Errorf("ValidateClusterResourcePlacement_PickAllPlacementPolicy() error = %v, wantErr %v", err, testCase.wantErr)
			}
		})
	}
}

func TestValidateClusterResourcePlacement_PickNPlacementPolicy(t *testing.T) {
	tests := map[string]struct {
		crp              *placementv1beta1.ClusterResourcePlacement
		resourceInformer informer.Manager
		wantErr          bool
	}{
		"invalid placement policy - PickN with non-empty cluster names": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						ClusterNames:     []string{"test-cluster"},
						NumberOfClusters: &positiveNumberOfClusters,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					},
				},
			},
			wantErr: true,
		},
		"invalid placement policy - PickN with nil number of clusters": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickNPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					},
				},
			},
			wantErr: true,
		},
		"invalid placement policy - PickN with negative number of clusters": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &negativeNumberOfClusters,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					},
				},
			},
			wantErr: true,
		},
		"invalid placement policy - PickN with invalid label selector terms in RequiredDuringSchedulingIgnoredDuringExecution affinity": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &positiveNumberOfClusters,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
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
			},
			wantErr: true,
		},
		"invalid placement policy - PickN with invalid label selector terms in PreferredDuringSchedulingIgnoredDuringExecution affinity": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &positiveNumberOfClusters,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Preference: placementv1beta1.ClusterSelectorTerm{
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
			},
			wantErr: true,
		},
		"invalid placement policy - PickN with invalid topology constraint with unknown unsatisfiable type": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &positiveNumberOfClusters,
						TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
							{
								TopologyKey:       "test-key",
								WhenUnsatisfiable: "random-type",
							},
						},
					},
				},
			},
			wantErr: true,
		},
		"valid placement policy - PickN with non nil affinity, non empty topology constraints": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &positiveNumberOfClusters,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: metav1.LabelSelector{
												MatchLabels: map[string]string{"test-key1": "test-value1"},
											},
										},
									},
								},
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Preference: placementv1beta1.ClusterSelectorTerm{
											LabelSelector: metav1.LabelSelector{
												MatchLabels: map[string]string{"test-key2": "test-value2"},
											},
										},
									},
								},
							},
						},
						TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
							{
								TopologyKey:       "test-key",
								WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			ResourceInformer = testCase.resourceInformer
			if err := ValidateClusterResourcePlacement(testCase.crp); (err != nil) != testCase.wantErr {
				t.Errorf("ValidateClusterResourcePlacement_PickNPlacementPolicy() error = %v, wantErr %v", err, testCase.wantErr)
			}
		})
	}
}

func TestIsPlacementPolicyUpdateValid(t *testing.T) {
	tests := map[string]struct {
		oldPolicy     *placementv1beta1.PlacementPolicy
		currentPolicy *placementv1beta1.PlacementPolicy
		wantResult    bool
	}{
		"old policy is nil, current policy is nil": {
			oldPolicy:     nil,
			currentPolicy: nil,
			wantResult:    false,
		},
		"old policy nil, current policy non nil, current placement type is PickAll": {
			oldPolicy: nil,
			currentPolicy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
								},
							},
						},
					},
				},
			},
			wantResult: false,
		},
		"old policy nil, current policy non nil, current placement type is PickN": {
			oldPolicy: nil,
			currentPolicy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickNPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
								},
							},
						},
					},
				},
			},
			wantResult: true,
		},
		"old policy is non nil, current policy is nil, old placement type is PickAll": {
			oldPolicy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
								},
							},
						},
					},
				},
			},
			currentPolicy: nil,
			wantResult:    false,
		},
		"old policy is non nil, current policy is nil, old placement type is PickFixed": {
			oldPolicy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickFixedPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
								},
							},
						},
					},
				},
			},
			currentPolicy: nil,
			wantResult:    true,
		},
		"old policy is non nil, current policy is non nil, placement type changed": {
			oldPolicy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickNPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
								},
							},
						},
					},
				},
			},
			currentPolicy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key2": "test-value2"},
									},
								},
							},
						},
					},
				},
			},
			wantResult: true,
		},
		"old policy is not nil, current policy is non nil, placement type unchanged": {
			oldPolicy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickNPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
								},
							},
						},
					},
				},
			},
			currentPolicy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickNPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key2": "test-value2"},
									},
								},
							},
						},
					},
				},
			},
			wantResult: false,
		},
	}
	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			if actualResult := IsPlacementPolicyTypeUpdated(testCase.oldPolicy, testCase.currentPolicy); actualResult != testCase.wantResult {
				t.Errorf("IsPlacementPolicyUpdateValid() actualResult = %v, wantResult %v", actualResult, testCase.wantResult)
			}
		})
	}
}

func TestValidateTolerations(t *testing.T) {
	tests := map[string]struct {
		tolerations []placementv1beta1.Toleration
		wantErr     error
	}{
		"valid tolerations": {
			tolerations: []placementv1beta1.Toleration{
				{
					Key:      "key1",
					Operator: corev1.TolerationOpEqual,
					Value:    "value1",
					Effect:   corev1.TaintEffectNoSchedule,
				},
				{
					Key:      "key2",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
				{
					Operator: corev1.TolerationOpExists,
				},
			},
			wantErr: nil,
		},
		"invalid toleration, key is empty, operator is Equal": {
			tolerations: []placementv1beta1.Toleration{
				{
					Operator: corev1.TolerationOpEqual,
					Value:    "value1",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			wantErr: fmt.Errorf(invalidTolerationErrFmt, placementv1beta1.Toleration{Operator: corev1.TolerationOpEqual, Value: "value1", Effect: corev1.TaintEffectNoSchedule}, "toleration key cannot be empty, when operator is Equal"),
		},
		"invalid toleration, value is empty, operator is not Exists": {
			tolerations: []placementv1beta1.Toleration{
				{
					Key:      "key1",
					Operator: corev1.TolerationOpEqual,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			wantErr: fmt.Errorf(invalidTolerationErrFmt, placementv1beta1.Toleration{Key: "key1", Operator: corev1.TolerationOpEqual, Effect: corev1.TaintEffectNoSchedule}, "toleration value cannot be empty, when operator is Equal"),
		},
		"invalid toleration, value is not empty, operator is Exists": {
			tolerations: []placementv1beta1.Toleration{
				{
					Key:      "key1",
					Operator: corev1.TolerationOpExists,
					Value:    "value1",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			wantErr: fmt.Errorf(invalidTolerationErrFmt, placementv1beta1.Toleration{Key: "key1", Operator: corev1.TolerationOpExists, Value: "value1", Effect: corev1.TaintEffectNoSchedule}, "toleration value needs to be empty, when operator is Exists"),
		},
		"invalid toleration, non-unique toleration": {
			tolerations: []placementv1beta1.Toleration{
				{
					Key:      "key1",
					Operator: corev1.TolerationOpEqual,
					Value:    "value1",
					Effect:   corev1.TaintEffectNoSchedule,
				},
				{
					Key:      "key2",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
				{
					Key:      "key1",
					Operator: corev1.TolerationOpEqual,
					Value:    "value1",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			wantErr: fmt.Errorf(uniqueTolerationErrFmt, placementv1beta1.Toleration{Key: "key1", Operator: corev1.TolerationOpEqual, Value: "value1", Effect: corev1.TaintEffectNoSchedule}),
		},
	}
	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			if actualErr := validateTolerations(testCase.tolerations); actualErr != nil && actualErr.Error() != testCase.wantErr.Error() {
				t.Errorf("validateTolerations() actualErr = %s, wantErr %s", actualErr.Error(), testCase.wantErr.Error())
			}
		})
	}
}
