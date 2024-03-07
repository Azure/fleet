/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package validator

import (
	"strings"
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
		wantErrMsg       string
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
			wantErrMsg:       "the labelSelector and name fields are mutually exclusive in selector",
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
			wantErrMsg:       "for 'in', 'notin' operators, values set can't be empty",
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
			wantErrMsg:       "the resource is not found in schema (please retry) or it is not a cluster scoped resource",
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
			wantErrMsg:       "cannot perform resource scope check for now, please retry",
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
			wantErrMsg:       "for 'in', 'notin' operators, values set can't be empty",
		},
	}
	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			ResourceInformer = testCase.resourceInformer
			gotErr := ValidateClusterResourcePlacementAlpha(testCase.crp)
			if (gotErr != nil) != testCase.wantErr {
				t.Errorf("ValidateClusterResourcePlacementAlpha() error = %v, wantErr %v", gotErr, testCase.wantErr)
			}
			if testCase.wantErr && !strings.Contains(gotErr.Error(), testCase.wantErrMsg) {
				t.Errorf("ValidateClusterResourcePlacementAlpha() failed to find expected error message = %v, in error = %v", testCase.wantErrMsg, gotErr.Error())
			}
		})
	}
}

func TestValidateClusterResourcePlacement(t *testing.T) {
	tests := map[string]struct {
		crp        *placementv1beta1.ClusterResourcePlacement
		wantErr    bool
		wantErrMsg string
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
			wantErr:    true,
			wantErrMsg: "the name field cannot have length exceeding 63",
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
			wantErr:    true,
			wantErrMsg: "the labelSelector and name fields are mutually exclusive in selector",
		},
	}
	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			gotErr := ValidateClusterResourcePlacement(testCase.crp)
			if (gotErr != nil) != testCase.wantErr {
				t.Errorf("ValidateClusterResourcePlacement() error = %v, wantErr %v", gotErr, testCase.wantErr)
			}
			if testCase.wantErr && !strings.Contains(gotErr.Error(), testCase.wantErrMsg) {
				t.Errorf("ValidateClusterResourcePlacement() failed to find expected error message = %v, in error = %v", testCase.wantErrMsg, gotErr.Error())
			}
		})
	}
}

func TestValidateClusterResourcePlacement_RolloutStrategy(t *testing.T) {
	tests := map[string]struct {
		strategy   placementv1beta1.RolloutStrategy
		wantErr    bool
		wantErrMsg string
	}{
		"empty rollout strategy": {
			wantErr: false,
		},
		"invalid rollout strategy": {
			strategy: placementv1beta1.RolloutStrategy{
				Type: "random type",
			},
			wantErr:    true,
			wantErrMsg: "unsupported rollout strategy type `random type`",
		},
		"invalid rollout strategy - UnavailablePeriodSeconds": {
			strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					UnavailablePeriodSeconds: &unavailablePeriodSeconds,
				},
			},
			wantErr:    true,
			wantErrMsg: "unavailablePeriodSeconds must be greater than or equal to 0, got -10",
		},
		"invalid rollout strategy - % error MaxUnavailable": {
			strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{
						Type:   1,
						StrVal: "25",
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "maxUnavailable `25` is invalid",
		},
		"invalid rollout strategy - negative MaxUnavailable": {
			strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{
						Type:   0,
						IntVal: -10,
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "maxUnavailable must be greater than or equal to 0, got `-10`",
		},
		"invalid rollout strategy - % error MaxSurge": {
			strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxSurge: &intstr.IntOrString{
						Type:   1,
						StrVal: "25",
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "maxSurge `25` is invalid",
		},
		"invalid rollout strategy - negative MaxSurge": {
			strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxSurge: &intstr.IntOrString{
						Type:   0,
						IntVal: -10,
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "maxSurge must be greater than or equal to 0, got `-10`",
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			gotErr := validateRolloutStrategy(testCase.strategy)
			if (gotErr != nil) != testCase.wantErr {
				t.Errorf("validateRolloutStrategy() error = %v, wantErr %v", gotErr, testCase.wantErr)
			}
			if testCase.wantErr && !strings.Contains(gotErr.Error(), testCase.wantErrMsg) {
				t.Errorf("validateRolloutStrategy() failed to find expected error message = %v, in error = %v", testCase.wantErrMsg, gotErr.Error())
			}
		})
	}
}

func TestValidateClusterResourcePlacement_PickFixedPlacementPolicy(t *testing.T) {
	tests := map[string]struct {
		policy     *placementv1beta1.PlacementPolicy
		wantErr    bool
		wantErrMsg string
	}{
		"valid placement policy - PickFixed with non-empty cluster names": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickFixedPlacementType,
				ClusterNames:  []string{"test-cluster"},
			},
			wantErr: false,
		},
		"invalid placement policy - PickFixed with empty cluster names": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickFixedPlacementType,
			},
			wantErr:    true,
			wantErrMsg: "cluster names cannot be empty for policy type PickFixed",
		},
		"invalid placement policy - PickFixed with non nil number of clusters": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickFixedPlacementType,
				ClusterNames:     []string{"test-cluster"},
				NumberOfClusters: &positiveNumberOfClusters,
			},
			wantErr:    true,
			wantErrMsg: "number of clusters must be nil for policy type PickFixed, only valid for PickN placement policy type",
		},
		"invalid placement policy - PickFixed with non nil affinity": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickFixedPlacementType,
				ClusterNames:  []string{"test-cluster"},
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key": "test-value"},
									},
								},
							},
						},
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "affinity must be nil for policy type PickFixed, only valid for PickAll/PickN placement policy types",
		},
		"invalid placement policy - PickFixed with non empty topology constraints": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickFixedPlacementType,
				ClusterNames:  []string{"test-cluster"},
				TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
					{
						TopologyKey: "test-key",
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "topology spread constraints needs to be empty for policy type PickFixed, only valid for PickN policy type",
		},
		"valid placement policy, PickFixed placementType, empty toleration, nil error": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickFixedPlacementType,
				ClusterNames:  []string{"test-cluster"},
			},
			wantErr: false,
		},
		"invalid placement policy - PickFixed placementType, non empty valid tolerations, error": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickFixedPlacementType,
				Tolerations: []placementv1beta1.Toleration{
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
			},
			wantErr:    true,
			wantErrMsg: "tolerations needs to be empty for policy type PickFixed, only valid for PickAll/PickN",
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			gotErr := validatePlacementPolicy(testCase.policy)
			if (gotErr != nil) != testCase.wantErr {
				t.Errorf("validatePlacementPolicy() error = %v, wantErr %v", gotErr, testCase.wantErr)
			}
			if testCase.wantErr && !strings.Contains(gotErr.Error(), testCase.wantErrMsg) {
				t.Errorf("validatePlacementPolicy() failed to find expected error message = %v, in error = %v", testCase.wantErrMsg, gotErr.Error())
			}
		})
	}
}

func TestValidateClusterResourcePlacement_PickAllPlacementPolicy(t *testing.T) {
	tests := map[string]struct {
		policy     *placementv1beta1.PlacementPolicy
		wantErr    bool
		wantErrMsg string
	}{
		"invalid placement policy - PickAll with non-empty cluster names": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				ClusterNames:  []string{"test-cluster"},
			},
			wantErr:    true,
			wantErrMsg: "cluster names needs to be empty for policy type PickAll, only valid for PickFixed policy type",
		},
		"invalid placement policy - PickAll with non nil number of clusters": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickAllPlacementType,
				NumberOfClusters: &positiveNumberOfClusters,
			},
			wantErr:    true,
			wantErrMsg: "number of clusters must be nil for policy type PickAll, only valid for PickN placement policy type",
		},
		"invalid placement policy - PickAll with invalid label selector terms in RequiredDuringSchedulingIgnoredDuringExecution in affinity": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
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
				},
			},
			wantErr:    true,
			wantErrMsg: "for 'in', 'notin' operators, values set can't be empty",
		},
		"invalid placement policy - PickAll with non empty PreferredDuringSchedulingIgnoredDuringExecution": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
							{
								Weight: 1,
								Preference: placementv1beta1.ClusterSelectorTerm{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key": "test-value"},
									},
								},
							},
						},
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "PreferredDuringSchedulingIgnoredDuringExecution will be ignored for placement policy type PickAll",
		},
		"invalid placement policy - PickAll with non empty topology constraints": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
					{
						TopologyKey: "test-key",
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "topology spread constraints needs to be empty for policy type PickAll, only valid for PickN policy type",
		},
		"valid placement policy - PickAll with non nil affinity": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key": "test-value"},
									},
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		"valid placement policy - PickAll placementType, empty tolerations, nil error": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			wantErr: false,
		},
		"invalid placement policy - PickAll placementType, non empty invalid tolerations, error": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Tolerations: []placementv1beta1.Toleration{
					{
						Key:      "key1",
						Operator: corev1.TolerationOpExists,
						Value:    "value1",
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "toleration value needs to be empty, when operator is Exists",
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			gotErr := validatePlacementPolicy(testCase.policy)
			if (gotErr != nil) != testCase.wantErr {
				t.Errorf("validatePlacementPolicy() error = %v, wantErr %v", gotErr, testCase.wantErr)
			}
			if testCase.wantErr && !strings.Contains(gotErr.Error(), testCase.wantErrMsg) {
				t.Errorf("validatePlacementPolicy() failed to find expected error message = %v, in error = %v", testCase.wantErrMsg, gotErr.Error())
			}
		})
	}
}

func TestValidateClusterResourcePlacement_PickNPlacementPolicy(t *testing.T) {
	tests := map[string]struct {
		policy     *placementv1beta1.PlacementPolicy
		wantErr    bool
		wantErrMsg string
	}{
		"invalid placement policy - PickN with non-empty cluster names": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				ClusterNames:     []string{"test-cluster"},
				NumberOfClusters: &positiveNumberOfClusters,
			},
			wantErr:    true,
			wantErrMsg: "cluster names needs to be empty for policy type PickN, only valid for PickFixed policy type",
		},
		"invalid placement policy - PickN with nil number of clusters": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickNPlacementType,
			},
			wantErr:    true,
			wantErrMsg: "number of cluster cannot be nil for policy type PickN",
		},
		"invalid placement policy - PickN with negative number of clusters": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: &negativeNumberOfClusters,
			},
			wantErr:    true,
			wantErrMsg: "number of clusters cannot be -1 for policy type PickN",
		},
		"invalid placement policy - PickN with invalid label selector terms in RequiredDuringSchedulingIgnoredDuringExecution affinity": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: &positiveNumberOfClusters,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
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
				},
			},
			wantErr:    true,
			wantErrMsg: "for 'in', 'notin' operators, values set can't be empty",
		},
		"invalid placement policy - PickN with invalid label selector terms in PreferredDuringSchedulingIgnoredDuringExecution affinity": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: &positiveNumberOfClusters,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
							{
								Preference: placementv1beta1.ClusterSelectorTerm{
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
				},
			},
			wantErr:    true,
			wantErrMsg: "for 'in', 'notin' operators, values set can't be empty",
		},
		"invalid placement policy - PickN with invalid topology constraint with unknown unsatisfiable type": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: &positiveNumberOfClusters,
				TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
					{
						TopologyKey:       "test-key",
						WhenUnsatisfiable: "random-type",
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "unknown unsatisfiable type random-type",
		},
		"valid placement policy - PickN with non nil affinity, non empty topology constraints": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: &positiveNumberOfClusters,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
								},
							},
						},
						PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
							{
								Preference: placementv1beta1.ClusterSelectorTerm{
									LabelSelector: &metav1.LabelSelector{
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
			wantErr: false,
		},
		"valid placement policy - PickN placementType, empty tolerations, nil error": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: &positiveNumberOfClusters,
			},
			wantErr: false,
		},
		"invalid placement policy - PickN placementType, non empty invalid tolerations, error": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickAllPlacementType,
				NumberOfClusters: &positiveNumberOfClusters,
				Tolerations: []placementv1beta1.Toleration{
					{
						Operator: corev1.TolerationOpEqual,
						Value:    "value1",
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "toleration key cannot be empty, when operator is Equal",
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			gotErr := validatePlacementPolicy(testCase.policy)
			if (gotErr != nil) != testCase.wantErr {
				t.Errorf("validatePlacementPolicy() error = %v, wantErr %v", gotErr, testCase.wantErr)
			}
			if testCase.wantErr && !strings.Contains(gotErr.Error(), testCase.wantErrMsg) {
				t.Errorf("validatePlacementPolicy() failed to find expected error message = %v, in error = %v", testCase.wantErrMsg, gotErr.Error())
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
									LabelSelector: &metav1.LabelSelector{
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
									LabelSelector: &metav1.LabelSelector{
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
									LabelSelector: &metav1.LabelSelector{
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
									LabelSelector: &metav1.LabelSelector{
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
									LabelSelector: &metav1.LabelSelector{
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
									LabelSelector: &metav1.LabelSelector{
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
									LabelSelector: &metav1.LabelSelector{
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
									LabelSelector: &metav1.LabelSelector{
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
		wantErr     bool
		wantErrMsg  string
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
			wantErr: false,
		},
		"valid toleration, value is empty, operator is Equal": {
			tolerations: []placementv1beta1.Toleration{
				{
					Key:      "key1",
					Operator: corev1.TolerationOpEqual,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			wantErr: false,
		},
		"invalid toleration, key is empty, operator is Equal": {
			tolerations: []placementv1beta1.Toleration{
				{
					Operator: corev1.TolerationOpEqual,
					Value:    "value1",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			wantErr:    true,
			wantErrMsg: "toleration key cannot be empty, when operator is Equal",
		},
		"invalid toleration, key is invalid, operator is Equal": {
			tolerations: []placementv1beta1.Toleration{
				{
					Key:      "key:123*",
					Operator: corev1.TolerationOpEqual,
					Value:    "value1",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			wantErr:    true,
			wantErrMsg: "name part must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character",
		},
		"invalid toleration, value is invalid, operator is Equal": {
			tolerations: []placementv1beta1.Toleration{
				{
					Key:      "key1",
					Operator: corev1.TolerationOpEqual,
					Value:    "val#%/-123",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			wantErr:    true,
			wantErrMsg: "a valid label must be an empty string or consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character",
		},
		"invalid toleration, key is invalid, operator is Exists": {
			tolerations: []placementv1beta1.Toleration{
				{
					Key:      "key:123*",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			wantErr:    true,
			wantErrMsg: "name part must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character",
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
			wantErr:    true,
			wantErrMsg: "toleration value needs to be empty, when operator is Exists",
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
			wantErr:    true,
			wantErrMsg: "tolerations must be unique",
		},
	}
	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			gotErr := validateTolerations(testCase.tolerations)
			if (gotErr != nil) != testCase.wantErr {
				t.Errorf("validateTolerations() error = %v, wantErr %v", gotErr, testCase.wantErr)
			}
			if testCase.wantErr && !strings.Contains(gotErr.Error(), testCase.wantErrMsg) {
				t.Errorf("validateTolerations() failed to find expected error message = %v, in error = %v", testCase.wantErrMsg, gotErr.Error())
			}
		})
	}
}

func TestIsTolerationsUpdatedOrDeleted(t *testing.T) {
	tests := map[string]struct {
		oldTolerations []placementv1beta1.Toleration
		newTolerations []placementv1beta1.Toleration
		want           bool
	}{
		"old tolerations is nil": {
			newTolerations: []placementv1beta1.Toleration{
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
			},
			want: false,
		},
		"new tolerations is nil": {
			oldTolerations: []placementv1beta1.Toleration{
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
			},
			want: true,
		},
		"one toleration was updated in new tolerations": {
			oldTolerations: []placementv1beta1.Toleration{
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
			},
			newTolerations: []placementv1beta1.Toleration{
				{
					Key:      "key1",
					Operator: corev1.TolerationOpEqual,
					Value:    "value1",
					Effect:   corev1.TaintEffectNoSchedule,
				},
				{
					Key:      "key3",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			want: true,
		},
		"one toleration was deleted in new tolerations": {
			oldTolerations: []placementv1beta1.Toleration{
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
			},
			newTolerations: []placementv1beta1.Toleration{
				{
					Key:      "key1",
					Operator: corev1.TolerationOpEqual,
					Value:    "value1",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			want: true,
		},
		"old tolerations, new tolerations are same": {
			oldTolerations: []placementv1beta1.Toleration{
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
			},
			newTolerations: []placementv1beta1.Toleration{
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
			},
			want: false,
		},
		"a toleration was added to new tolerations": {
			oldTolerations: []placementv1beta1.Toleration{
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
			},
			newTolerations: []placementv1beta1.Toleration{
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
			want: false,
		},
	}
	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			if got := IsTolerationsUpdatedOrDeleted(testCase.oldTolerations, testCase.newTolerations); got != testCase.want {
				t.Errorf("IsTolerationsUpdatedOrDeleted() got = %v, want = %v", got, testCase.want)
			}
		})
	}
}
