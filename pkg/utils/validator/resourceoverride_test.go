package validator

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apierrors "k8s.io/apimachinery/pkg/util/errors"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestValidateResourceSelectors(t *testing.T) {
	tests := map[string]struct {
		ro         placementv1alpha1.ResourceOverride
		wantErrMsg error
	}{
		"duplicate resources selected": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
				placementv1alpha1.ResourceSelector{Group: "group", Version: "v1", Kind: "Kind", Name: "example"}),
		},
		"resource selected by name": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
		ro            placementv1alpha1.ResourceOverride
		overrideCount int
		wantErrMsg    error
	}{
		"create one resource override for resource foo": {
			ro: placementv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
			ro: placementv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-2",
				},
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
				placementv1alpha1.ResourceSelector{Group: "group", Version: "v1", Kind: "kind", Name: "example-0"}, "override-2", "override-0"),
		},
		"one override, which exists": {
			ro: placementv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
			ro: placementv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-2",
				},
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
			roList := &placementv1alpha1.ResourceOverrideList{}
			for i := 0; i < tt.overrideCount; i++ {
				ro := placementv1alpha1.ResourceOverride{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("override-%d", i),
					},
					Spec: placementv1alpha1.ResourceOverrideSpec{
						ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
	validPolicy := &placementv1alpha1.OverridePolicy{
		OverrideRules: []placementv1alpha1.OverrideRule{
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
				JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
					{
						Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
						Path:     "/metadata/labels/new-label",
						Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
					},
				},
			},
		},
	}

	tests := map[string]struct {
		ro         placementv1alpha1.ResourceOverride
		roList     *placementv1alpha1.ResourceOverrideList
		wantErrMsg error
	}{
		"valid resource override": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
			roList:     &placementv1alpha1.ResourceOverrideList{},
			wantErrMsg: nil,
		},
		"invalid resource override - fail validateResourceSelector": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
			roList: &placementv1alpha1.ResourceOverrideList{},
			wantErrMsg: fmt.Errorf("resource selector %+v already exists, and must be unique",
				placementv1alpha1.ResourceSelector{Group: "group", Version: "v1", Kind: "kind", Name: "example"}),
		},
		"invalid resource override - fail ValidateResourceOverrideResourceLimit": {
			ro: placementv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
			roList: &placementv1alpha1.ResourceOverrideList{
				Items: []placementv1alpha1.ResourceOverride{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "override-0"},
						Spec: placementv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
				placementv1alpha1.ResourceSelector{Group: "group", Version: "v1", Kind: "kind", Name: "duplicate-example"}, "override-1", "override-0"),
		},
		"valid resource override - empty roList": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
			roList:     &placementv1alpha1.ResourceOverrideList{},
			wantErrMsg: nil,
		},
		"valid resource override - roList nil": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
		"invalid resource override - fail validateResourceOverridePolicy with unsupported type ": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
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
			roList:     &placementv1alpha1.ResourceOverrideList{},
			wantErrMsg: fmt.Errorf("only labelSelector is supported"),
		},
		"invalid resource override - fail validateResourceOverridePolicy with nil label selector": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
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
		"valid resource override - empty cluster selector": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{},
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"invalid resource override - fail validateResourceOverridePolicy with empty terms": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
								},
							},
						},
					},
				},
			},
			wantErrMsg: errors.New("clusterSelector must have at least one term"),
		},
		"valid resource override - empty match labels & match expressions": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{MatchLabels: nil},
										},
									},
								},
							},
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{MatchExpressions: nil},
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
		"valid resource override - no policy": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					Policy: nil,
				},
			},
			wantErrMsg: nil,
		},
		"invalid resource override - multiple invalid override paths": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
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
								JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
										Path:     "/apiVersion",
									},
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/annotations/0",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
									},
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpReplace,
										Path:     "/status/conditions/0/reason",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-reason"`)},
									},
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpReplace,
										Path:     "/metadata/creationTimestamp",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"2021-08-01T00:00:00Z"`)},
									},
								},
							},
						},
					},
				},
			},
			wantErrMsg: apierrors.NewAggregate([]error{fmt.Errorf("invalid JSONPatchOverride %s: cannot override typeMeta fields",
				placementv1alpha1.JSONPatchOverride{Operator: placementv1alpha1.JSONPatchOverrideOpRemove, Path: "/apiVersion"}),
				fmt.Errorf("invalid JSONPatchOverride %s: cannot override status fields",
					placementv1alpha1.JSONPatchOverride{Operator: placementv1alpha1.JSONPatchOverrideOpReplace, Path: "/status/conditions/0/reason", Value: apiextensionsv1.JSON{Raw: []byte(`"new-reason"`)}}),
				fmt.Errorf("invalid JSONPatchOverride %s: cannot override metadata fields",
					placementv1alpha1.JSONPatchOverride{Operator: placementv1alpha1.JSONPatchOverrideOpReplace, Path: "/metadata/creationTimestamp", Value: apiextensionsv1.JSON{Raw: []byte(`"2021-08-01T00:00:00Z"`)}}),
			}),
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

func TestValidateOverridePolicy(t *testing.T) {
	tests := map[string]struct {
		ro         placementv1alpha1.ResourceOverride
		wantErrMsg error
	}{
		"all label selectors": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
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
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"unsupported selector type": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
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
			wantErrMsg: fmt.Errorf("only labelSelector is supported"),
		},
		"no cluster selector": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
							{
								JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/labels/new-label",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
									},
								},
							},
						},
					},
				},
			},
			wantErrMsg: errors.New("clusterSelector is required"),
		},
		"empty cluster selector": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{},
								JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
										Path:     "/metadata/labels/new-label",
									},
								},
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"nil label selector": {
			ro: placementv1alpha1.ResourceOverride{
				Spec: placementv1alpha1.ResourceOverrideSpec{
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{},
									},
								},
							},
						},
					},
				},
			},
			wantErrMsg: errors.New("labelSelector is required"),
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got := validateOverridePolicy(tt.ro.Spec.Policy)
			if gotErr, wantErr := got != nil, tt.wantErrMsg != nil; gotErr != wantErr {
				t.Fatalf("validateOverridePolicy() = %v, want %v", got, tt.wantErrMsg)
			}

			if got != nil && !strings.Contains(got.Error(), tt.wantErrMsg.Error()) {
				t.Errorf("validateOverridePolicy() = %v, want %v", got, tt.wantErrMsg)
			}
		})
	}
}

func TestValidateJSONPatchOverride(t *testing.T) {
	tests := map[string]struct {
		jsonPatchOverrides []placementv1alpha1.JSONPatchOverride
		wantErrMsg         error
	}{
		"valid json override patch": {
			jsonPatchOverrides: []placementv1alpha1.JSONPatchOverride{
				{
					Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
					Path:     "/metadata/labels/label1",
				},
			},
			wantErrMsg: nil,
		},
		"invalid resource override path - cannot override typeMeta fields": {
			jsonPatchOverrides: []placementv1alpha1.JSONPatchOverride{
				{
					Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
					Path:     "/kind",
				},
			},
			wantErrMsg: fmt.Errorf("invalid JSONPatchOverride %s: cannot override typeMeta fields",
				placementv1alpha1.JSONPatchOverride{Operator: placementv1alpha1.JSONPatchOverrideOpRemove, Path: "/kind"}),
		},
		"invalid resource override path - cannot override metadata fields": {
			jsonPatchOverrides: []placementv1alpha1.JSONPatchOverride{
				{
					Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
					Path:     "/metadata/finalizers/0",
					Value:    apiextensionsv1.JSON{Raw: []byte(`"kubernetes.io/scheduler-cleanup"`)},
				},
			},
			wantErrMsg: fmt.Errorf("invalid JSONPatchOverride %s: cannot override metadata fields",
				placementv1alpha1.JSONPatchOverride{Operator: "add", Path: "/metadata/finalizers/0", Value: apiextensionsv1.JSON{Raw: []byte(`"kubernetes.io/scheduler-cleanup"`)}}),
		},
		"invalid resource override path - cannot override status fields": {
			jsonPatchOverrides: []placementv1alpha1.JSONPatchOverride{
				{
					Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
					Path:     "/status/conditions/0/reason",
				},
			},
			wantErrMsg: fmt.Errorf("invalid JSONPatchOverride %s: cannot override status fields",
				placementv1alpha1.JSONPatchOverride{Operator: placementv1alpha1.JSONPatchOverrideOpRemove, Path: "/status/conditions/0/reason"}),
		},
		"invalid resource override path - remove with value": {
			jsonPatchOverrides: []placementv1alpha1.JSONPatchOverride{
				{
					Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
					Path:     "/metadata/labels/label1",
					Value:    apiextensionsv1.JSON{Raw: []byte(`"value"`)},
				},
			},
			wantErrMsg: fmt.Errorf("invalid JSONPatchOverride %v: remove operation cannot have value",
				placementv1alpha1.JSONPatchOverride{Operator: placementv1alpha1.JSONPatchOverrideOpRemove, Path: "/metadata/labels/label1", Value: apiextensionsv1.JSON{Raw: []byte(`"value"`)}}),
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got := validateJSONPatchOverride(tt.jsonPatchOverrides)
			if gotErr, wantErr := got != nil, tt.wantErrMsg != nil; gotErr != wantErr {
				t.Fatalf("validateJSONPatchOverride() = %v, want %v", got, tt.wantErrMsg)
			}

			if got != nil && !strings.Contains(got.Error(), tt.wantErrMsg.Error()) {
				t.Errorf("validateJSONPatchOverride() = %v, want %v", got, tt.wantErrMsg)
			}
		})
	}
}
