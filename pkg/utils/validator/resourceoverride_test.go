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

func TestValidateResourceSelectors(t *testing.T) {
	tests := map[string]struct {
		ro         placementv1beta1.ResourceOverride
		wantErrMsg error
	}{
		"duplicate resources selected": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
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
				placementv1beta1.ResourceSelector{Group: "group", Version: "v1", Kind: "Kind", Name: "example"}),
		},
		"resource selected by name": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
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
		ro            placementv1beta1.ResourceOverride
		overrideCount int
		wantErrMsg    error
	}{
		"create one resource override for resource foo": {
			ro: placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
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
			ro: placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-2",
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
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
				placementv1beta1.ResourceSelector{Group: "group", Version: "v1", Kind: "kind", Name: "example-0"}, "override-2", "override-0"),
		},
		"one override, which exists": {
			ro: placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
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
			ro: placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-2",
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
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
			roList := &placementv1beta1.ResourceOverrideList{}
			for i := 0; i < tt.overrideCount; i++ {
				ro := placementv1beta1.ResourceOverride{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("override-%d", i),
					},
					Spec: placementv1beta1.ResourceOverrideSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelector{
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
		ro         placementv1beta1.ResourceOverride
		roList     *placementv1beta1.ResourceOverrideList
		wantErrMsg error
	}{
		"valid resource override": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
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
			roList:     &placementv1beta1.ResourceOverrideList{},
			wantErrMsg: nil,
		},
		"invalid resource override - fail validateResourceSelector": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
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
			roList: &placementv1beta1.ResourceOverrideList{},
			wantErrMsg: fmt.Errorf("resource selector %+v already exists, and must be unique",
				placementv1beta1.ResourceSelector{Group: "group", Version: "v1", Kind: "kind", Name: "example"}),
		},
		"invalid resource override - fail ValidateResourceOverrideResourceLimit": {
			ro: placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
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
			roList: &placementv1beta1.ResourceOverrideList{
				Items: []placementv1beta1.ResourceOverride{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "override-0"},
						Spec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
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
				placementv1beta1.ResourceSelector{Group: "group", Version: "v1", Kind: "kind", Name: "duplicate-example"}, "override-1", "override-0"),
		},
		"valid resource override - empty roList": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
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
			roList:     &placementv1beta1.ResourceOverrideList{},
			wantErrMsg: nil,
		},
		"valid resource override - roList nil": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
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
		"valid resource override - delete nil": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
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
			roList:     nil,
			wantErrMsg: nil,
		},
		"invalid resource override - fail validateResourceOverridePolicy with unsupported type ": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
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
			roList:     &placementv1beta1.ResourceOverrideList{},
			wantErrMsg: fmt.Errorf("only labelSelector is supported"),
		},
		"invalid resource override - fail validateResourceOverridePolicy with nil label selector": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
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
		"valid resource override - empty cluster selector": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector:    &placementv1beta1.ClusterSelector{},
								OverrideType:       placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: validJSONPatchOverrides,
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"valid resource override - cluster selector with empty terms": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
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
		"valid resource override - empty match labels & match expressions": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
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
								OverrideType:       placementv1beta1.JSONPatchOverrideType,
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
								OverrideType:       placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: validJSONPatchOverrides,
							},
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"valid resource override - no policy": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
					Policy: nil,
				},
			},
			wantErrMsg: nil,
		},
		"invalid resource override - multiple invalid override paths": {
			ro: placementv1beta1.ResourceOverride{
				Spec: placementv1beta1.ResourceOverrideSpec{
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
										Path:     "/apiVersion",
									},
									{
										Operator: placementv1beta1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/annotations/0",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
									},
									{
										Operator: placementv1beta1.JSONPatchOverrideOpReplace,
										Path:     "/status/conditions/0/reason",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-reason"`)},
									},
									{
										Operator: placementv1beta1.JSONPatchOverrideOpReplace,
										Path:     "/////kind///",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"value"`)},
									},
								},
							},
						},
					},
				},
			},
			wantErrMsg: apierrors.NewAggregate([]error{fmt.Errorf("invalid JSONPatchOverride %s: cannot override typeMeta fields",
				placementv1beta1.JSONPatchOverride{Operator: placementv1beta1.JSONPatchOverrideOpRemove, Path: "/apiVersion"}),
				fmt.Errorf("invalid JSONPatchOverride %s: cannot override status fields",
					placementv1beta1.JSONPatchOverride{Operator: placementv1beta1.JSONPatchOverrideOpReplace, Path: "/status/conditions/0/reason", Value: apiextensionsv1.JSON{Raw: []byte(`"new-reason"`)}}),
				fmt.Errorf("invalid JSONPatchOverride %s: path cannot contain empty string",
					placementv1beta1.JSONPatchOverride{Operator: placementv1beta1.JSONPatchOverrideOpReplace, Path: "/////kind///", Value: apiextensionsv1.JSON{Raw: []byte(`"value"`)}}),
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
	validJSONPatchOverrides := []placementv1beta1.JSONPatchOverride{
		{
			Operator: placementv1beta1.JSONPatchOverrideOpAdd,
			Path:     "/metadata/labels/new-label",
			Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
		},
	}

	tests := map[string]struct {
		policy     *placementv1beta1.OverridePolicy
		wantErrMsg error
	}{
		"all label selectors": {
			policy: &placementv1beta1.OverridePolicy{
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
						JSONPatchOverrides: validJSONPatchOverrides,
					},
				},
			},
			wantErrMsg: nil,
		},
		"unsupported selector type - property selector": {
			policy: &placementv1beta1.OverridePolicy{
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
			wantErrMsg: fmt.Errorf("only labelSelector is supported"),
		},
		"no cluster selector": {
			policy: &placementv1beta1.OverridePolicy{
				OverrideRules: []placementv1beta1.OverrideRule{
					{
						JSONPatchOverrides: validJSONPatchOverrides,
					},
				},
			},
			wantErrMsg: nil,
		},
		"empty cluster selector": {
			policy: &placementv1beta1.OverridePolicy{
				OverrideRules: []placementv1beta1.OverrideRule{
					{
						ClusterSelector:    &placementv1beta1.ClusterSelector{},
						JSONPatchOverrides: validJSONPatchOverrides,
					},
				},
			},
			wantErrMsg: nil,
		},
		"nil label selector": {
			policy: &placementv1beta1.OverridePolicy{
				OverrideRules: []placementv1beta1.OverrideRule{
					{
						ClusterSelector: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{},
							},
						},
					},
				},
			},
			wantErrMsg: errors.New("labelSelector is required"),
		},
		"nil JSONPatchOverride": {
			policy: &placementv1beta1.OverridePolicy{
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
						OverrideType:       placementv1beta1.JSONPatchOverrideType,
						JSONPatchOverrides: nil,
					},
				},
			},
			wantErrMsg: errors.New("JSONPatchOverrides cannot be empty"),
		},
		"empty JSONPatchOverrides with jsonPatch override type": {
			policy: &placementv1beta1.OverridePolicy{
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
						OverrideType:       placementv1beta1.JSONPatchOverrideType,
						JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{},
					},
				},
			},
			wantErrMsg: errors.New("JSONPatchOverrides cannot be empty"),
		},
		"JSONPatchOverrides with delete override type": {
			policy: &placementv1beta1.OverridePolicy{
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
						OverrideType:       placementv1beta1.DeleteOverrideType,
						JSONPatchOverrides: validJSONPatchOverrides,
					},
				},
			},
			wantErrMsg: errors.New("JSONPatchOverrides cannot be set when the override type is Delete"),
		},
		"invalid JSONPatchOverridesPath": {
			policy: &placementv1beta1.OverridePolicy{
				OverrideRules: []placementv1beta1.OverrideRule{
					{
						ClusterSelector: &placementv1beta1.ClusterSelector{},
						OverrideType:    placementv1beta1.JSONPatchOverrideType,
						JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
							{
								Operator: placementv1beta1.JSONPatchOverrideOpReplace,
								Path:     "/metadata/finalizers",
								Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
							},
						},
					},
				},
			},
			wantErrMsg: errors.New("cannot override metadata fields except annotations and labels"),
		},
		"invalid JSONPatchOverride": {
			policy: &placementv1beta1.OverridePolicy{
				OverrideRules: []placementv1beta1.OverrideRule{
					{
						ClusterSelector: &placementv1beta1.ClusterSelector{},
						OverrideType:    placementv1beta1.JSONPatchOverrideType,
						JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
							{
								Operator: placementv1beta1.JSONPatchOverrideOpRemove,
								Path:     "/apiVersionabc",
								Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
							},
						},
					},
				},
			},
			wantErrMsg: errors.New("remove operation cannot have value"),
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got := validateOverridePolicy(tt.policy)
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
		jsonPatchOverrides []placementv1beta1.JSONPatchOverride
		wantErrMsg         error
	}{
		"valid json patch override": {
			jsonPatchOverrides: []placementv1beta1.JSONPatchOverride{
				{
					Operator: placementv1beta1.JSONPatchOverrideOpReplace,
					Path:     "/spec/clusterResourceSelector/kind",
					Value:    apiextensionsv1.JSON{Raw: []byte(`"ClusterRole"`)},
				},
			},
			wantErrMsg: nil,
		},
		"invalid json patch override - invalid remove operation": {
			jsonPatchOverrides: []placementv1beta1.JSONPatchOverride{
				{
					Operator: placementv1beta1.JSONPatchOverrideOpRemove,
					Path:     "/spec/clusterResourceSelector/kind",
					Value:    apiextensionsv1.JSON{Raw: []byte(`"ClusterRole"`)},
				},
			},
			wantErrMsg: errors.New("remove operation cannot have value"),
		},
		"invalid json patch override - nil jsonPatchOverrides": {
			jsonPatchOverrides: nil,
			wantErrMsg:         errors.New("JSONPatchOverrides cannot be empty"),
		},
		"invalid json patch override - empty jsonPatchOverrides": {
			jsonPatchOverrides: []placementv1beta1.JSONPatchOverride{},
			wantErrMsg:         errors.New("JSONPatchOverrides cannot be empty"),
		},
		"invalid json patch override - invalid path": {
			jsonPatchOverrides: []placementv1beta1.JSONPatchOverride{
				{
					Operator: placementv1beta1.JSONPatchOverrideOpReplace,
					Path:     "/status/conditions/0/reason",
					Value:    apiextensionsv1.JSON{Raw: []byte(`"new-reason"`)},
				},
			},
			wantErrMsg: errors.New("cannot override status fields"),
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

func TestValidateJSONPatchOverridePath(t *testing.T) {
	tests := map[string]struct {
		path       string
		wantErrMsg error
	}{
		"valid json patch override path": {
			path:       "/spec/clusterResourceSelector/kind",
			wantErrMsg: nil,
		},
		"invalid json patch override path- cannot override typeMeta fields (kind)": {
			path:       "/kind",
			wantErrMsg: errors.New("cannot override typeMeta fields"),
		},
		"invalid json patch override path - cannot override typeMeta fields (apiVersion)": {
			path:       "/apiVersion",
			wantErrMsg: errors.New("cannot override typeMeta fields"),
		},
		"invalid json patch override path - cannot override metadata fields": {
			path:       "/metadata/finalizers/0",
			wantErrMsg: errors.New("cannot override metadata fields"),
		},
		"invalid json patch override path - cannot override any status field": {
			path:       "/status/conditions/0/reason",
			wantErrMsg: errors.New("cannot override status fields"),
		},
		"valid json patch override path - correct metadata field": {
			path:       "/metadata/annotations/new-annotation",
			wantErrMsg: nil,
		},
		"valid json patch override path - apiVersion used as label": {
			path:       "/metadata/labels/apiVersion",
			wantErrMsg: nil,
		},
		"valid json patch override path- case sensitive check": {
			path:       "/Kind",
			wantErrMsg: nil,
		},
		"invalid json patch override path - cannot override status": {
			path:       "/status",
			wantErrMsg: errors.New("cannot override status fields"),
		},
		"valid json patch override path- apiVersion within path": {
			path:       "/apiVersionabc",
			wantErrMsg: nil,
		},
		"invalid json patch override path - empty path": {
			path:       "",
			wantErrMsg: errors.New("path cannot be empty"),
		},
		"invalid json patch override path - slashes only": {
			path:       "/////",
			wantErrMsg: errors.New("path cannot contain empty string"),
		},
		"invalid json patch override path - path must start with /": {
			path:       "spec.resourceSelectors/selectors/0/name",
			wantErrMsg: errors.New("path must start with /"),
		},
		"invalid json patch override path - cannot override metadata fields (finalizer)": {
			path:       "/metadata/finalizers",
			wantErrMsg: errors.New("cannot override metadata fields except annotations and labels"),
		},
		"invalid json patch override path - invalid metadata field": {
			path:       "/metadata/annotationsabc",
			wantErrMsg: errors.New("cannot override metadata fields"),
		},
		"invalid json patch override path - contains empty string": {
			path:       "/spec/clusterNames///member-1",
			wantErrMsg: errors.New("path cannot contain empty string"),
		},
		"invalid json patch override path - metadata field": {
			path:       "/metadata",
			wantErrMsg: errors.New("cannot override field metadata"),
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got := validateJSONPatchOverridePath(tt.path)
			if gotErr, wantErr := got != nil, tt.wantErrMsg != nil; gotErr != wantErr {
				t.Fatalf("validateJSONPatchOverridePath() = %v, want %v", got, tt.wantErrMsg)
			}

			if got != nil && !strings.Contains(got.Error(), tt.wantErrMsg.Error()) {
				t.Errorf("validateJSONPatchOverridePath() = %v, want %v", got, tt.wantErrMsg)
			}
		})
	}
}
