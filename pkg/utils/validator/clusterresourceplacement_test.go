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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/informer"
	testinformer "go.goms.io/fleet/test/utils/informer"
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
			resourceInformer: &testinformer.FakeManager{IsClusterScopedResource: false},
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
			resourceInformer: &testinformer.FakeManager{IsClusterScopedResource: false},
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
			resourceInformer: &testinformer.FakeManager{IsClusterScopedResource: false},
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
			resourceInformer: &testinformer.FakeManager{IsClusterScopedResource: true},
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
			resourceInformer: &testinformer.FakeManager{IsClusterScopedResource: false},
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
				t.Errorf("ValidateClusterResourcePlacementAlpha() got %v, should contain want %s", gotErr, testCase.wantErrMsg)
			}
		})
	}
}

func TestValidateClusterResourcePlacement(t *testing.T) {
	tests := map[string]struct {
		crp              *placementv1beta1.ClusterResourcePlacement
		resourceInformer informer.Manager
		wantErr          bool
		wantErrMsg       string
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
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.ClusterRoleGVK: true},
				IsClusterScopedResource: true},
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
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.ClusterRoleGVK: true},
				IsClusterScopedResource: true},
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
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.ClusterRoleGVK: true},
				IsClusterScopedResource: true},
			wantErr:    true,
			wantErrMsg: "the labelSelector and name fields are mutually exclusive in selector",
		},
		"invalid Resource Selector with invalid GVK": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "invalidKind",
							Name:    "test-invalidKind",
						},
					},
				},
			},
			resourceInformer: &testinformer.FakeManager{IsClusterScopedResource: false},
			wantErr:          true,
			wantErrMsg:       "failed to get GVR of the selector",
		},
		"invalid Resource Selector with not ClusterScopedResource": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "apps",
							Kind:    "Deployment",
							Version: "v1",
							Name:    "test-deployment",
						},
					},
				},
			},
			wantErr: true,
			resourceInformer: &testinformer.FakeManager{
				APIResources:            map[schema.GroupVersionKind]bool{utils.DeploymentGVK: true},
				IsClusterScopedResource: false},
			wantErrMsg: "resource is not found in schema (please retry) or it is not a cluster scoped resource",
		},
		"nil resource informer": {
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{resourceSelector},
				},
			},
			resourceInformer: nil,
			wantErr:          true,
			wantErrMsg:       "cannot perform resource scope check for now, please retry",
		},
	}
	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			RestMapper = utils.TestMapper{}
			ResourceInformer = testCase.resourceInformer
			gotErr := ValidateClusterResourcePlacement(testCase.crp)
			if (gotErr != nil) != testCase.wantErr {
				t.Errorf("ValidateClusterResourcePlacement() error = %v, wantErr %v", gotErr, testCase.wantErr)
			}
			if testCase.wantErr && !strings.Contains(gotErr.Error(), testCase.wantErrMsg) {
				t.Errorf("ValidateClusterResourcePlacement() got %v, should contain want %s", gotErr, testCase.wantErrMsg)
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
			wantErrMsg: "maxUnavailable must be greater than or equal to 1, got `-10`",
		},
		"invalid rollout strategy - zero MaxUnavailable": {
			strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable: &intstr.IntOrString{
						Type:   0,
						IntVal: 0,
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "maxUnavailable must be greater than or equal to 1, got `0`",
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
		"invalid rollout strategy - ServerSideApplyConfig not valid when type is not serversideApply": {
			strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					Type: placementv1beta1.ApplyStrategyTypeClientSideApply,
					ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{
						ForceConflicts: false,
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "serverSideApplyConfig is only valid for ServerSideApply strategy type",
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			gotErr := validateRolloutStrategy(testCase.strategy)
			if (gotErr != nil) != testCase.wantErr {
				t.Errorf("validateRolloutStrategy() error = %v, wantErr %v", gotErr, testCase.wantErr)
			}
			if testCase.wantErr && !strings.Contains(gotErr.Error(), testCase.wantErrMsg) {
				t.Errorf("validateRolloutStrategy() got %v, should contain want %s", gotErr, testCase.wantErrMsg)
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
		"invalid placement policy - PickFixed with non-unique cluster names": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickFixedPlacementType,
				ClusterNames:  []string{"test-cluster1", "test-cluster1", "test-cluster2", "test-cluster2"},
			},
			wantErr:    true,
			wantErrMsg: "cluster names must be unique for policy type PickFixed",
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
				t.Errorf("validatePlacementPolicy() got %v, should contain want %s", gotErr, testCase.wantErrMsg)
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
		"valid placement policy - PickAll with property selector in RequiredDuringSchedulingIgnoredDuringExecution affinity": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     "version",
												Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
												Values:   []string{"1"},
											},
											{
												Name:     "cpu",
												Operator: placementv1beta1.PropertySelectorGreaterThan,
												Values:   []string{"500m"},
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
		"invalid placement policy - PickAll with invalid property selector in RequiredDuringSchedulingIgnoredDuringExecution affinity": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     "version",
												Operator: placementv1beta1.PropertySelectorEqualTo,
												Values:   []string{"1", "2"},
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
			wantErrMsg: "operator Eq requires exactly one value, got 2",
		},
		"invalid placement policy - PickAll with invalid property selector name, invalid capacity type": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     "resources.kubernetes-fleet.io/node-count",
												Operator: placementv1beta1.PropertySelectorEqualTo,
												Values:   []string{"2"},
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
			wantErrMsg: "invalid capacity type in resource property name resources.kubernetes-fleet.io/node-count, supported values are [allocatable available total]",
		},
		"invalid placement policy - PickAll with invalid property selector name, no segments": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     "resources.kubernetes-fleet.io/totalcpu",
												Operator: placementv1beta1.PropertySelectorEqualTo,
												Values:   []string{"2"},
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
			wantErrMsg: "invalid resource property name resources.kubernetes-fleet.io/totalcpu, expected format is [PREFIX]/[CAPACITY_TYPE]-[RESOURCE_NAME]",
		},
		"valid placement policy - PickAll with valid property selector name": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     "resources.kubernetes-fleet.io/allocatable-memory",
												Operator: placementv1beta1.PropertySelectorEqualTo,
												Values:   []string{"2"},
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
			gotErr := validatePlacementPolicy(testCase.policy)
			if (gotErr != nil) != testCase.wantErr {
				t.Errorf("validatePlacementPolicy() error = %v, wantErr %v", gotErr, testCase.wantErr)
			}
			if testCase.wantErr && !strings.Contains(gotErr.Error(), testCase.wantErrMsg) {
				t.Errorf("validatePlacementPolicy() got %v, should contain want %s", gotErr, testCase.wantErrMsg)
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
		"invalid placement policy - PickN with non-nil property sorter in RequiredDuringSchedulingIgnoredDuringExecution affinity": {
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
									PropertySorter: &placementv1beta1.PropertySorter{
										Name:      "Name",
										SortOrder: placementv1beta1.Descending,
									},
								},
							},
						},
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "PropertySorter is not allowed for RequiredDuringSchedulingIgnoredDuringExecution affinity",
		},
		"invalid placement policy - PickN with invalid property selector in RequiredDuringSchedulingIgnoredDuringExecution affinity": {
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
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     "version",
												Operator: placementv1beta1.PropertySelectorEqualTo,
												Values:   []string{"1", "2"},
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
			wantErrMsg: "operator Eq requires exactly one value, got 2",
		},
		"valid placement policy - PickN with property selector in RequiredDuringSchedulingIgnoredDuringExecution affinity": {
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
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     "version",
												Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
												Values:   []string{"1"},
											},
											{
												Name:     "cpu",
												Operator: placementv1beta1.PropertySelectorGreaterThan,
												Values:   []string{"500m"},
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
		"invalid placement policy - PickN with non-nil property selector in PreferredDuringSchedulingIgnoredDuringExecution affinity": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: &positiveNumberOfClusters,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
							{
								Preference: placementv1beta1.ClusterSelectorTerm{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     "version",
												Operator: placementv1beta1.PropertySelectorEqualTo,
												Values:   []string{"1"},
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
			wantErrMsg: "PropertySelector is not allowed for PreferredDuringSchedulingIgnoredDuringExecution affinity",
		},
		"invalid placement policy - PickN with invalid property sorter in PreferredDuringSchedulingIgnoredDuringExecution affinity": {
			policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: &positiveNumberOfClusters,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
							{
								Preference: placementv1beta1.ClusterSelectorTerm{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"test-key1": "test-value1"},
									},
									PropertySorter: &placementv1beta1.PropertySorter{
										Name:      "resources.kubernetes-fleet.io/total-cpu",
										SortOrder: "random-order",
									},
								},
							},
						},
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "invalid property sort order random-order",
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
		"invalid placement policy - PickN with invalid property selector name, invalid label name": {
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
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     "resources.kubernetes-fleet.io/total-nospecialchars%^=@",
												Operator: placementv1beta1.PropertySelectorEqualTo,
												Values:   []string{"2"},
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
			wantErrMsg: "name is not a valid Kubernetes label name",
		},
		"valid placement policy - PickN with valid property selector name": {
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
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     "resources.kubernetes-fleet.io/total-allocatable-cpu",
												Operator: placementv1beta1.PropertySelectorEqualTo,
												Values:   []string{"2"},
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
			gotErr := validatePlacementPolicy(testCase.policy)
			if (gotErr != nil) != testCase.wantErr {
				t.Errorf("validatePlacementPolicy() error = %v, wantErr %v", gotErr, testCase.wantErr)
			}
			if testCase.wantErr && !strings.Contains(gotErr.Error(), testCase.wantErrMsg) {
				t.Errorf("validatePlacementPolicy() got %v, should contain want %s", gotErr, testCase.wantErrMsg)
			}
		})
	}
}

func TestIsPlacementPolicyUpdateValid(t *testing.T) {
	tests := map[string]struct {
		oldPolicy     *placementv1beta1.PlacementPolicy
		currentPolicy *placementv1beta1.PlacementPolicy
		want          bool
	}{
		"old policy is nil, current policy is nil": {
			oldPolicy:     nil,
			currentPolicy: nil,
			want:          false,
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
			want: false,
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
			want: true,
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
			want:          false,
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
			want:          true,
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
			want: true,
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
			want: false,
		},
	}
	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			if got := IsPlacementPolicyTypeUpdated(testCase.oldPolicy, testCase.currentPolicy); got != testCase.want {
				t.Errorf("IsPlacementPolicyUpdateValid() got = %v, want %v", got, testCase.want)
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
				t.Errorf("validateTolerations() got %v, should contain want %s", gotErr, testCase.wantErrMsg)
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
