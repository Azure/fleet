package validator

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apierrors "k8s.io/apimachinery/pkg/util/errors"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestValidateClusterResourceSelectors(t *testing.T) {
	tests := map[string]struct {
		cro        placementv1beta1.ClusterResourceOverride
		wantErrMsg error
	}{
		"resource selected by label selector": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "group",
							Version: "v1",
							Kind:    "Kind",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key": "value",
								},
							},
						},
					},
				},
			},
			wantErrMsg: fmt.Errorf("label selector is not supported for resource selection"),
		},
		"resource selected by empty name": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "group",
							Version: "v1",
							Kind:    "Kind",
							Name:    "",
						},
					},
				},
			},
			wantErrMsg: fmt.Errorf("resource name is required for resource selection"),
		},
		"duplicate resources selected": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "group",
							Version: "v1",
							Kind:    "Kind",
							Name:    "example",
						},
						{
							Group:   "group",
							Version: "v1",
							Kind:    "Kind",
							Name:    "example",
						},
					},
				},
			},
			wantErrMsg: fmt.Errorf("resource selector %+v already exists, and must be unique",
				placementv1beta1.ResourceSelectorTerm{Group: "group", Version: "v1", Kind: "Kind", Name: "example"}),
		},
		"resource selected by name": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"multiple invalid resources selected": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "group",
							Version: "v1",
							Kind:    "Kind",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key": "value",
								},
							},
						},
						{
							Group:   "group",
							Version: "v1",
							Kind:    "Kind",
							Name:    "",
						},
						{
							Group:   "group",
							Version: "v1",
							Kind:    "Kind",
							Name:    "example",
						},
						{
							Group:   "group",
							Version: "v1",
							Kind:    "Kind",
							Name:    "example",
						},
					},
				},
			},
			wantErrMsg: apierrors.NewAggregate([]error{fmt.Errorf("label selector is not supported for resource selection %+v", placementv1beta1.ResourceSelectorTerm{Group: "group", Version: "v1", Kind: "Kind", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"key": "value"}}}),
				fmt.Errorf("resource name is required for resource selection %+v", placementv1beta1.ResourceSelectorTerm{Group: "group", Version: "v1", Kind: "Kind", Name: ""}),
				fmt.Errorf("resource selector %+v already exists, and must be unique", placementv1beta1.ResourceSelectorTerm{Group: "group", Version: "v1", Kind: "Kind", Name: "example"})}),
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got := validateClusterResourceSelectors(tt.cro)
			if gotErr, wantErr := got != nil, tt.wantErrMsg != nil; gotErr != wantErr {
				t.Fatalf("validateClusterResourceSelectors() = %v, want %v", got, tt.wantErrMsg)
			}

			if got != nil && !strings.Contains(got.Error(), tt.wantErrMsg.Error()) {
				t.Errorf("validateClusterResourceSelectors() = %v, want %v", got, tt.wantErrMsg)
			}
		})
	}
}

func TestValidateClusterResourceOverrideResourceLimit(t *testing.T) {
	tests := map[string]struct {
		cro           placementv1beta1.ClusterResourceOverride
		overrideCount int
		wantErrMsg    error
	}{
		"create one cluster resource override for resource foo": {
			cro: placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
				},
			},
			overrideCount: 1,
			wantErrMsg:    nil,
		},
		"one override, selecting the same resource by other override already": {
			cro: placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-2",
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "group",
							Version: "v1",
							Kind:    "kind",
							Name:    "example-0",
						},
						{
							Group:   "group",
							Version: "v1",
							Kind:    "kind",
							Name:    "bar",
						},
					},
				},
			},
			overrideCount: 1,
			wantErrMsg: fmt.Errorf("invalid resource selector %+v: the resource has been selected by both %v and %v, which is not supported",
				placementv1beta1.ResourceSelectorTerm{Group: "group", Version: "v1", Kind: "kind", Name: "example-0"}, "override-2", "override-0"),
		},
		"one override, which exists": {
			cro: placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
				},
			},
			overrideCount: 2,
			wantErrMsg:    nil,
		},
		"croList is empty": {
			cro: placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-2",
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
				},
			},
			overrideCount: 0,
			wantErrMsg:    nil,
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			croList := &placementv1beta1.ClusterResourceOverrideList{}
			for i := 0; i < tt.overrideCount; i++ {
				cro := placementv1beta1.ClusterResourceOverride{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("override-%d", i),
					},
					Spec: placementv1beta1.ClusterResourceOverrideSpec{
						ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "group",
								Version: "v1",
								Kind:    "kind",
								Name:    fmt.Sprintf("example-%d", i),
							},
						},
					},
				}
				croList.Items = append(croList.Items, cro)
			}
			got := validateClusterResourceOverrideResourceLimit(tt.cro, croList)
			if gotErr, wantErr := got != nil, tt.wantErrMsg != nil; gotErr != wantErr {
				t.Fatalf("validateClusterResourceOverrideResourceLimit() = %v, want %v", got, tt.wantErrMsg)
			}

			if got != nil && !strings.Contains(got.Error(), tt.wantErrMsg.Error()) {
				t.Errorf("validateClusterResourceOverrideResourceLimit() = %v, want %v", got, tt.wantErrMsg)
			}
		})
	}
}

func TestValidateClusterResourceOverride(t *testing.T) {
	validClusterSelector := &placementv1beta1.ClusterSelector{
		ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
			{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"key": "value",
					},
				},
			},
			{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"key1": "value1",
					},
				},
			},
		},
	}

	validJSONPatchOverrides := []placementv1beta1.JSONPatchOverride{
		{
			Operator: placementv1beta1.JSONPatchOverrideOpAdd,
			Path:     "/metadata/labels/new-label",
			Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
		},
	}

	validPolicy := &placementv1beta1.OverridePolicy{
		OverrideRules: []placementv1beta1.OverrideRule{
			{
				ClusterSelector:    validClusterSelector,
				JSONPatchOverrides: validJSONPatchOverrides,
			},
		},
	}

	tests := map[string]struct {
		cro        placementv1beta1.ClusterResourceOverride
		croList    *placementv1beta1.ClusterResourceOverrideList
		wantErrMsg error
	}{
		"valid cluster resource override": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
					Policy: validPolicy,
				},
			},
			croList:    &placementv1beta1.ClusterResourceOverrideList{},
			wantErrMsg: nil,
		},
		"invalid cluster resource override - fail validateResourceSelector": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "group",
							Version: "v1",
							Kind:    "kind",
							Name:    "example",
						},
						{
							Group:   "group",
							Version: "v1",
							Kind:    "kind",
							Name:    "example",
						},
						{
							Group:   "group",
							Version: "v1",
							Kind:    "kind",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key": "value",
								},
							},
						},
					},
					Policy: validPolicy,
				},
			},
			croList: &placementv1beta1.ClusterResourceOverrideList{},
			wantErrMsg: apierrors.NewAggregate([]error{fmt.Errorf("resource selector %+v already exists, and must be unique",
				placementv1beta1.ResourceSelectorTerm{Group: "group", Version: "v1", Kind: "kind", Name: "example"}),
				fmt.Errorf("label selector is not supported for resource selection %+v",
					placementv1beta1.ResourceSelectorTerm{Group: "group", Version: "v1", Kind: "kind",
						LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"key": "value"}}})}),
		},
		"invalid cluster resource override - fail ValidateClusterResourceOverrideResourceLimit": {
			cro: placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "group",
							Version: "v1",
							Kind:    "kind",
							Name:    "duplicate-example",
						},
					},
					Policy: validPolicy,
				},
			},
			croList: &placementv1beta1.ClusterResourceOverrideList{
				Items: []placementv1beta1.ClusterResourceOverride{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "override-0"},
						Spec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "group",
									Version: "v1",
									Kind:    "kind",
									Name:    "duplicate-example",
								},
							},
							Policy: validPolicy,
						},
					},
				},
			},
			wantErrMsg: fmt.Errorf("invalid resource selector %+v: the resource has been selected by both %v and %v, which is not supported",
				placementv1beta1.ResourceSelectorTerm{Group: "group", Version: "v1", Kind: "kind", Name: "duplicate-example"}, "override-1", "override-0"),
		},
		"valid cluster resource override - empty croList": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
					Policy: validPolicy,
				},
			},
			croList:    &placementv1beta1.ClusterResourceOverrideList{},
			wantErrMsg: nil,
		},
		"valid cluster resource override - croList nil": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
					Policy: validPolicy,
				},
			},
			croList:    nil,
			wantErrMsg: nil,
		},
		"invalid cluster resource override - fail validateClusterResourceOverridePolicy with unsupported type": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     "example",
														Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
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
				},
			},
			croList:    &placementv1beta1.ClusterResourceOverrideList{},
			wantErrMsg: errors.New("only labelSelector is supported"),
		},
		"invalid cluster resource override - fail validateClusterResourceOverridePolicy with nil label selector": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: nil,
										},
									},
								},
							},
						},
					},
				},
			},
			wantErrMsg: errors.New("labelSelector is required"),
		},
		"valid cluster resource override - empty cluster selector terms": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
								},
								JSONPatchOverrides: validJSONPatchOverrides,
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"valid cluster resource override - empty match labels & match expressions": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{MatchLabels: nil},
										},
									},
								},
								JSONPatchOverrides: validJSONPatchOverrides,
							},
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{MatchExpressions: nil},
										},
									},
								},
								JSONPatchOverrides: validJSONPatchOverrides,
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"valid cluster resource override - no policy": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Policy: nil,
				},
			},
			wantErrMsg: nil,
		},
		"valid cluster resource override - delete policy": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: validClusterSelector,
								OverrideType:    placementv1beta1.DeleteOverrideType,
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"valid cluster resource override - policy with all label selectors": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"key": "value",
												},
											},
										},
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"key1": "value1",
												},
											},
										},
									},
								},
								OverrideType:       placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: validJSONPatchOverrides,
							},
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"key2": "value2",
												},
											},
										},
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"key3": "value3",
												},
											},
										},
									},
								},
								OverrideType:       placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: validJSONPatchOverrides,
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"invalid cluster resource override - policy with unsupported selector type": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     "example",
														Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
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
				},
			},
			wantErrMsg: errors.New("only labelSelector is supported"),
		},
		"valid cluster resource override - policy with no cluster selector": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								OverrideType:       placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: validJSONPatchOverrides,
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"invalid cluster resource override - policy with multiple rules": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"key": "value",
												},
											},
										},
										{
											PropertySorter: &placementv1beta1.PropertySorter{
												Name:      "example",
												SortOrder: placementv1beta1.Descending,
											},
										},
									},
								},
							},
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     "example",
														Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
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
				},
			},
			wantErrMsg: errors.New("only labelSelector is supported"),
		},
		"valid cluster resource override - policy with nil label selector": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{},
									},
								},
								OverrideType:       placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: validJSONPatchOverrides,
							},
						},
					},
				},
			},
			wantErrMsg: errors.New("labelSelector is required"),
		},
		"invalid cluster resource override - multiple invalid override paths, 1 valid": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"key": "value",
												},
											},
										},
									},
								},
								OverrideType: placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpRemove,
										Path:     "/Kind",
									},
									{
										Operator: placementv1beta1.JSONPatchOverrideOpAdd,
										Path:     "spec.resourceSelectors/matchExpressions",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
									},
									{
										Operator: placementv1beta1.JSONPatchOverrideOpReplace,
										Path:     "",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-reason"`)},
									},
									{
										Operator: placementv1beta1.JSONPatchOverrideOpRemove,
										Path:     "/status",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
									},
								},
							},
						},
					},
				},
			},
			wantErrMsg: apierrors.NewAggregate([]error{
				fmt.Errorf("invalid JSONPatchOverride %s: path must start with /",
					placementv1beta1.JSONPatchOverride{Operator: placementv1beta1.JSONPatchOverrideOpAdd, Path: "spec.resourceSelectors/matchExpressions", Value: apiextensionsv1.JSON{Raw: []byte(`"new-value"`)}}),
				fmt.Errorf("invalid JSONPatchOverride %s: path cannot be empty",
					placementv1beta1.JSONPatchOverride{Operator: placementv1beta1.JSONPatchOverrideOpReplace, Path: "", Value: apiextensionsv1.JSON{Raw: []byte(`"new-reason"`)}}),
				fmt.Errorf("invalid JSONPatchOverride %s: cannot override status fields",
					placementv1beta1.JSONPatchOverride{Operator: placementv1beta1.JSONPatchOverrideOpRemove, Path: "/status", Value: apiextensionsv1.JSON{Raw: []byte(`"new-value"`)}}),
				fmt.Errorf("invalid JSONPatchOverride %s: remove operation cannot have value",
					placementv1beta1.JSONPatchOverride{Operator: placementv1beta1.JSONPatchOverrideOpRemove, Path: "/status", Value: apiextensionsv1.JSON{Raw: []byte(`"new-value"`)}}),
			}),
		},
		"valid cluster resource override - empty cluster selector": {
			cro: placementv1beta1.ClusterResourceOverride{
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector:    &placementv1beta1.ClusterSelector{},
								JSONPatchOverrides: validJSONPatchOverrides,
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got := ValidateClusterResourceOverride(tt.cro, tt.croList)
			if gotErr, wantErr := got != nil, tt.wantErrMsg != nil; gotErr != wantErr {
				t.Fatalf("ValidateClusterResourceOverride() = %v, want %v", got, tt.wantErrMsg)
			}

			if got != nil && !strings.Contains(got.Error(), tt.wantErrMsg.Error()) {
				t.Errorf("ValidateClusterResourceOverride() = %v, want %v", got, tt.wantErrMsg)
			}
		})
	}
}
