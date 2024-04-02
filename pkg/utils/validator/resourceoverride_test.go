package validator

import (
	"fmt"
	"strings"
	"testing"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
)

func TestValidateResourceSelectors(t *testing.T) {
	tests := map[string]struct {
		ro         fleetv1alpha1.ResourceOverride
		wantErrMsg error
	}{
		"duplicate resources selected": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
				fleetv1alpha1.ResourceSelector{Group: "group", Version: "v1", Kind: "Kind", Name: "example"}),
		},
		"resource selected by name": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got := validateResourceSelectors(tt.ro)
			if gotErr, wantErr := got != nil, tt.wantErrMsg != nil; gotErr != wantErr {
				t.Fatalf("validateResourceSelectors() = %v, want %v", got, tt.wantErrMsg)
			}

			if got != nil && !strings.Contains(got.Error(), tt.wantErrMsg.Error()) {
				t.Errorf("validateResourceSelectors() = %v, want %v", got, tt.wantErrMsg)
			}
		})
	}
}

func TestValidateResourceOverrideResourceLimit(t *testing.T) {
	tests := map[string]struct {
		ro            fleetv1alpha1.ResourceOverride
		overrideCount int
		wantErrMsg    error
	}{
		"create one resource override for resource foo": {
			ro: fleetv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
			ro: fleetv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-2",
				},
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
				fleetv1alpha1.ResourceSelector{Group: "group", Version: "v1", Kind: "kind", Name: "example-0"}, "override-2", "override-0"),
		},
		"one override, which exists": {
			ro: fleetv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
		"roList is empty": {
			ro: fleetv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-2",
				},
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
			roList := &fleetv1alpha1.ResourceOverrideList{}
			for i := 0; i < tt.overrideCount; i++ {
				ro := fleetv1alpha1.ResourceOverride{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("override-%d", i),
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						ResourceSelectors: []fleetv1alpha1.ResourceSelector{
							{
								Group:   "group",
								Version: "v1",
								Kind:    "kind",
								Name:    fmt.Sprintf("example-%d", i),
							},
						},
					},
				}
				roList.Items = append(roList.Items, ro)
			}
			got := validateResourceOverrideResourceLimit(tt.ro, roList)
			if gotErr, wantErr := got != nil, tt.wantErrMsg != nil; gotErr != wantErr {
				t.Fatalf("validateResourceOverrideResourceLimit() = %v, want %v", got, tt.wantErrMsg)
			}

			if got != nil && !strings.Contains(got.Error(), tt.wantErrMsg.Error()) {
				t.Errorf("validateResourceOverrideResourceLimit() = %v, want %v", got, tt.wantErrMsg)
			}
		})
	}
}

func TestValidateResourceOverride(t *testing.T) {
	validPolicy := &fleetv1alpha1.OverridePolicy{
		OverrideRules: []fleetv1alpha1.OverrideRule{
			{
				ClusterSelector: &fleetv1beta1.ClusterSelector{
					ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
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
			},
		},
	}

	tests := map[string]struct {
		ro         fleetv1alpha1.ResourceOverride
		roList     *fleetv1alpha1.ResourceOverrideList
		wantErrMsg error
	}{
		"valid resource override": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
			roList:     &fleetv1alpha1.ResourceOverrideList{},
			wantErrMsg: nil,
		},
		"invalid resource override - fail validateResourceSelector": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
					},
					Policy: validPolicy,
				},
			},
			roList: &fleetv1alpha1.ResourceOverrideList{},
			wantErrMsg: fmt.Errorf("resource selector %+v already exists, and must be unique",
				fleetv1alpha1.ResourceSelector{Group: "group", Version: "v1", Kind: "kind", Name: "example"}),
		},
		"invalid resource override - fail ValidateResourceOverrideResourceLimit": {
			ro: fleetv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
			roList: &fleetv1alpha1.ResourceOverrideList{
				Items: []fleetv1alpha1.ResourceOverride{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "override-0"},
						Spec: fleetv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []fleetv1alpha1.ResourceSelector{
								{
									Group:   "group",
									Version: "v1",
									Kind:    "kind",
									Name:    "duplicate-example",
								},
							},
						},
					},
				},
			},
			wantErrMsg: fmt.Errorf("invalid resource selector %+v: the resource has been selected by both %v and %v, which is not supported",
				fleetv1alpha1.ResourceSelector{Group: "group", Version: "v1", Kind: "kind", Name: "duplicate-example"}, "override-1", "override-0"),
		},
		"valid resource override - empty roList": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
			roList:     &fleetv1alpha1.ResourceOverrideList{},
			wantErrMsg: nil,
		},
		"valid resource override - roList nil": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
			roList:     nil,
			wantErrMsg: nil,
		},
		"invalid cluster resource override - fail validateClusterResourceOverrideRuleSelector": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					Policy: &fleetv1alpha1.OverridePolicy{
						OverrideRules: []fleetv1alpha1.OverrideRule{
							{
								ClusterSelector: &fleetv1beta1.ClusterSelector{
									ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &fleetv1beta1.PropertySelector{
												MatchExpressions: []fleetv1beta1.PropertySelectorRequirement{
													{
														Name:     "example",
														Operator: fleetv1beta1.PropertySelectorGreaterThanOrEqualTo,
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
			roList:     &fleetv1alpha1.ResourceOverrideList{},
			wantErrMsg: fmt.Errorf("label selector is only supported for resource selection"),
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got := ValidateResourceOverride(tt.ro, tt.roList)
			if gotErr, wantErr := got != nil, tt.wantErrMsg != nil; gotErr != wantErr {
				t.Fatalf("ValidateResourceOverride() = %v, want %v", got, tt.wantErrMsg)
			}

			if got != nil && !strings.Contains(got.Error(), tt.wantErrMsg.Error()) {
				t.Errorf("ValidateResourceOverride() = %v, want %v", got, tt.wantErrMsg)
			}
		})
	}
}

func TestValidateResourceOverrideRuleSelector(t *testing.T) {
	tests := map[string]struct {
		ro         fleetv1alpha1.ResourceOverride
		wantErrMsg error
	}{
		"all label selectors": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					Policy: &fleetv1alpha1.OverridePolicy{
						OverrideRules: []fleetv1alpha1.OverrideRule{
							{
								ClusterSelector: &fleetv1beta1.ClusterSelector{
									ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
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
							},
							{
								ClusterSelector: &fleetv1beta1.ClusterSelector{
									ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
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
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"unsupported selector type": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					Policy: &fleetv1alpha1.OverridePolicy{
						OverrideRules: []fleetv1alpha1.OverrideRule{
							{
								ClusterSelector: &fleetv1beta1.ClusterSelector{
									ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &fleetv1beta1.PropertySelector{
												MatchExpressions: []fleetv1beta1.PropertySelectorRequirement{
													{
														Name:     "example",
														Operator: fleetv1beta1.PropertySelectorGreaterThanOrEqualTo,
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
			wantErrMsg: fmt.Errorf("label selector is only supported for resource selection"),
		},
		"no cluster selector": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					Policy: &fleetv1alpha1.OverridePolicy{
						OverrideRules: []fleetv1alpha1.OverrideRule{
							{},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"multiple rules": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					Policy: &fleetv1alpha1.OverridePolicy{
						OverrideRules: []fleetv1alpha1.OverrideRule{
							{
								ClusterSelector: &fleetv1beta1.ClusterSelector{
									ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"key": "value",
												},
											},
										},
										{
											PropertySorter: &fleetv1beta1.PropertySorter{
												Name:      "example",
												SortOrder: fleetv1beta1.Descending,
											},
										},
									},
								},
							},
							{
								ClusterSelector: &fleetv1beta1.ClusterSelector{
									ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &fleetv1beta1.PropertySelector{
												MatchExpressions: []fleetv1beta1.PropertySelectorRequirement{
													{
														Name:     "example",
														Operator: fleetv1beta1.PropertySelectorGreaterThanOrEqualTo,
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
			wantErrMsg: fmt.Errorf("label selector is only supported for resource selection"),
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got := validateResourceOverrideRuleSelector(tt.ro)
			if gotErr, wantErr := got != nil, tt.wantErrMsg != nil; gotErr != wantErr {
				t.Fatalf("validateResourceOverrideRuleSelector() = %v, want %v", got, tt.wantErrMsg)
			}

			if got != nil && !strings.Contains(got.Error(), tt.wantErrMsg.Error()) {
				t.Errorf("validateResourceOverrideRuleSelector() = %v, want %v", got, tt.wantErrMsg)
			}
		})
	}
}
