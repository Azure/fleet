package validator

import (
	"fmt"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/errors"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestValidateClusterResourceSelectors(t *testing.T) {
	tests := map[string]struct {
		cro        fleetv1alpha1.ClusterResourceOverride
		wantErrMsg error
	}{
		"resource selected by label selector": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			cro: fleetv1alpha1.ClusterResourceOverride{
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			cro: fleetv1alpha1.ClusterResourceOverride{
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
				fleetv1beta1.ClusterResourceSelector{Group: "group", Version: "v1", Kind: "Kind", Name: "example"}),
		},
		"resource selected by name": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			cro: fleetv1alpha1.ClusterResourceOverride{
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			wantErrMsg: errors.NewAggregate([]error{fmt.Errorf("label selector is not supported for resource selection %+v", fleetv1beta1.ClusterResourceSelector{Group: "group", Version: "v1", Kind: "Kind", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"key": "value"}}}),
				fmt.Errorf("resource name is required for resource selection %+v", fleetv1beta1.ClusterResourceSelector{Group: "group", Version: "v1", Kind: "Kind", Name: ""}),
				fmt.Errorf("resource selector %+v already exists, and must be unique", fleetv1beta1.ClusterResourceSelector{Group: "group", Version: "v1", Kind: "Kind", Name: "example"})}),
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
		cro           fleetv1alpha1.ClusterResourceOverride
		overrideCount int
		wantErrMsg    error
	}{
		"create one cluster resource override for resource foo": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			cro: fleetv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-2",
				},
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
				fleetv1beta1.ClusterResourceSelector{Group: "group", Version: "v1", Kind: "kind", Name: "example-0"}, "override-2", "override-0"),
		},
		"one override, which exists": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			cro: fleetv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-2",
				},
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			croList := &fleetv1alpha1.ClusterResourceOverrideList{}
			for i := 0; i < tt.overrideCount; i++ {
				cro := fleetv1alpha1.ClusterResourceOverride{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("override-%d", i),
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
	tests := map[string]struct {
		cro        fleetv1alpha1.ClusterResourceOverride
		croList    *fleetv1alpha1.ClusterResourceOverrideList
		wantErrMsg error
	}{
		"valid cluster resource override": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
				},
			},
			croList:    &fleetv1alpha1.ClusterResourceOverrideList{},
			wantErrMsg: nil,
		},
		"invalid cluster resource override - fail validateResourceSelector": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
				},
			},
			croList: &fleetv1alpha1.ClusterResourceOverrideList{},
			wantErrMsg: fmt.Errorf("resource selector %+v already exists, and must be unique",
				fleetv1beta1.ClusterResourceSelector{Group: "group", Version: "v1", Kind: "kind", Name: "example"}),
		},
		"invalid cluster resource override - fail ValidateClusterResourceOverrideResourceLimit": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
						{
							Group:   "group",
							Version: "v1",
							Kind:    "kind",
							Name:    "duplicate-example",
						},
					},
				},
			},
			croList: &fleetv1alpha1.ClusterResourceOverrideList{
				Items: []fleetv1alpha1.ClusterResourceOverride{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "override-0"},
						Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
				fleetv1beta1.ClusterResourceSelector{Group: "group", Version: "v1", Kind: "kind", Name: "duplicate-example"}, "override-1", "override-0"),
		},
		"valid cluster resource override - empty croList": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
				},
			},
			croList:    &fleetv1alpha1.ClusterResourceOverrideList{},
			wantErrMsg: nil,
		},
		"valid cluster resource override - croList nil": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
				},
			},
			croList:    nil,
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
