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

var (
	unavailablePeriodSeconds       = -10
	numberOfClusters         int32 = 1
	resourceSelector               = placementv1beta1.ClusterResourceSelector{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "ClusterRole",
		Name:    "test-cluster-role",
	}
)

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

func Test_ValidateClusterResourcePlacement(t *testing.T) {
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

func Test_ValidateClusterResourcePlacement_RolloutStrategy(t *testing.T) {
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

func Test_ValidateClusterResourcePlacement_PickFixedPlacementPolicy(t *testing.T) {
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
						NumberOfClusters: &numberOfClusters,
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

func Test_ValidateClusterResourcePlacement_PickAllPlacementPolicy(t *testing.T) {
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
						NumberOfClusters: &numberOfClusters,
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
		"invalid placement policy - PickAll with invalid label selector terms in affinity": {
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
