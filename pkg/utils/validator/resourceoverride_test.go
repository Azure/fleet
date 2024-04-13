package validator

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apierrors "k8s.io/apimachinery/pkg/util/errors"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
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
				JSONPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
					{
						Operator: fleetv1alpha1.JSONPatchOverrideOpAdd,
						Path:     "/metadata/labels/new-label",
						Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
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
		"invalid resource override - fail validateResourceOverridePolicy with unsupported type ": {
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
			wantErrMsg: fmt.Errorf("only labelSelector is supported"),
		},
		"invalid resource override - fail validateResourceOverridePolicy with nil label selector": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					Policy: &fleetv1alpha1.OverridePolicy{
						OverrideRules: []fleetv1alpha1.OverrideRule{
							{
								ClusterSelector: &fleetv1beta1.ClusterSelector{
									ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
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
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					Policy: &fleetv1alpha1.OverridePolicy{
						OverrideRules: []fleetv1alpha1.OverrideRule{
							{
								ClusterSelector: &fleetv1beta1.ClusterSelector{},
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"invalid resource override - fail validateResourceOverridePolicy with empty terms": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					Policy: &fleetv1alpha1.OverridePolicy{
						OverrideRules: []fleetv1alpha1.OverrideRule{
							{
								ClusterSelector: &fleetv1beta1.ClusterSelector{
									ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{},
								},
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"valid resource override - empty match labels & match expressions": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					Policy: &fleetv1alpha1.OverridePolicy{
						OverrideRules: []fleetv1alpha1.OverrideRule{
							{
								ClusterSelector: &fleetv1beta1.ClusterSelector{
									ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{MatchLabels: nil},
										},
									},
								},
							},
							{
								ClusterSelector: &fleetv1beta1.ClusterSelector{
									ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
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
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					Policy: nil,
				},
			},
			wantErrMsg: nil,
		},
		"invalid resource override - multiple invalid override paths": {
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
									},
								},
								JSONPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
									{
										Operator: fleetv1alpha1.JSONPatchOverrideOpRemove,
										Path:     "/apiVersion",
									},
									{
										Operator: fleetv1alpha1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/annotations/0",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
									},
									{
										Operator: fleetv1alpha1.JSONPatchOverrideOpReplace,
										Path:     "/status/conditions/0/reason",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-reason"`)},
									},
									{
										Operator: fleetv1alpha1.JSONPatchOverrideOpReplace,
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
				fleetv1alpha1.JSONPatchOverride{Operator: fleetv1alpha1.JSONPatchOverrideOpRemove, Path: "/apiVersion"}),
				fmt.Errorf("invalid JSONPatchOverride %s: cannot override status fields",
					fleetv1alpha1.JSONPatchOverride{Operator: fleetv1alpha1.JSONPatchOverrideOpReplace, Path: "/status/conditions/0/reason", Value: apiextensionsv1.JSON{Raw: []byte(`"new-reason"`)}}),
				fmt.Errorf("invalid JSONPatchOverride %s: cannot override metadata fields",
					fleetv1alpha1.JSONPatchOverride{Operator: fleetv1alpha1.JSONPatchOverrideOpReplace, Path: "/metadata/creationTimestamp", Value: apiextensionsv1.JSON{Raw: []byte(`"2021-08-01T00:00:00Z"`)}}),
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
			wantErrMsg: fmt.Errorf("only labelSelector is supported"),
		},
		"no cluster selector": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					Policy: &fleetv1alpha1.OverridePolicy{
						OverrideRules: []fleetv1alpha1.OverrideRule{
							{
								JSONPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
									{
										Operator: fleetv1alpha1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/labels/new-label",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
									},
								},
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"empty cluster selector": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					Policy: &fleetv1alpha1.OverridePolicy{
						OverrideRules: []fleetv1alpha1.OverrideRule{
							{
								ClusterSelector: &fleetv1beta1.ClusterSelector{},
								JSONPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
									{
										Operator: fleetv1alpha1.JSONPatchOverrideOpRemove,
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
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					Policy: &fleetv1alpha1.OverridePolicy{
						OverrideRules: []fleetv1alpha1.OverrideRule{
							{
								ClusterSelector: &fleetv1beta1.ClusterSelector{
									ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
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
		jsonPatchOverrides []fleetv1alpha1.JSONPatchOverride
		wantErrMsg         error
	}{
		"valid json override patch": {
			jsonPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
				{
					Operator: fleetv1alpha1.JSONPatchOverrideOpReplace,
					Path:     "/spec/clusterResourceSelector/kind",
					Value:    apiextensionsv1.JSON{Raw: []byte(`"ClusterRole"`)},
				},
			},
			wantErrMsg: nil,
		},
		"invalid resource override path - cannot override typeMeta fields (kind)": {
			jsonPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
				{
					Operator: fleetv1alpha1.JSONPatchOverrideOpRemove,
					Path:     "/kind",
				},
			},
			wantErrMsg: errors.New("cannot override typeMeta fields"),
		},
		"invalid resource override path - cannot override typeMeta fields (apiVersion)": {
			jsonPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
				{
					Operator: fleetv1alpha1.JSONPatchOverrideOpReplace,
					Path:     "/apiVersion",
					Value:    apiextensionsv1.JSON{Raw: []byte(`"v1"`)},
				},
			},
			wantErrMsg: errors.New("cannot override typeMeta fields"),
		},
		"invalid resource override path - cannot override metadata fields": {
			jsonPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
				{
					Operator: fleetv1alpha1.JSONPatchOverrideOpAdd,
					Path:     "/metadata/finalizers/0",
					Value:    apiextensionsv1.JSON{Raw: []byte(`"kubernetes.io/scheduler-cleanup"`)},
				},
			},
			wantErrMsg: errors.New("cannot override metadata fields"),
		},
		"invalid resource override path - cannot override status fields": {
			jsonPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
				{
					Operator: fleetv1alpha1.JSONPatchOverrideOpRemove,
					Path:     "/status/conditions/0/reason",
				},
			},
			wantErrMsg: errors.New("cannot override status fields"),
		},
		"invalid resource override path - remove with value": {
			jsonPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
				{
					Operator: fleetv1alpha1.JSONPatchOverrideOpRemove,
					Path:     "/metadata/labels/label1",
					Value:    apiextensionsv1.JSON{Raw: []byte(`"value"`)},
				},
			},
			wantErrMsg: errors.New("remove operation cannot have value"),
		},
		"valid resource override path - correct metadata field": {
			jsonPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
				{
					Operator: fleetv1alpha1.JSONPatchOverrideOpAdd,
					Path:     "/metadata/annotations/new-annotation",
					Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
				},
			},
			wantErrMsg: nil,
		},
		"valid resource override path - apiVersion used as label": {
			jsonPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
				{
					Operator: fleetv1alpha1.JSONPatchOverrideOpRemove,
					Path:     "/metadata/labels/apiVersion",
				},
			},
			wantErrMsg: nil,
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
