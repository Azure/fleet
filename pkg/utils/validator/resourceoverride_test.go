package validator

import (
	"fmt"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/errors"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
)

func TestValidateResourceSelectors(t *testing.T) {
	tests := map[string]struct {
		ro         fleetv1alpha1.ResourceOverride
		wantErrMsg error
	}{
		"resource selected by empty name": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
		"multiple invalid resources selected": {
			ro: fleetv1alpha1.ResourceOverride{
				Spec: fleetv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
			wantErrMsg: errors.NewAggregate([]error{fmt.Errorf("resource name is required for resource selection %+v", fleetv1alpha1.ResourceSelector{Group: "group", Version: "v1", Kind: "Kind", Name: ""}),
				fmt.Errorf("resource selector %+v already exists, and must be unique", fleetv1alpha1.ResourceSelector{Group: "group", Version: "v1", Kind: "Kind", Name: "example"})}),
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
				},
			},
			roList:     nil,
			wantErrMsg: nil,
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
