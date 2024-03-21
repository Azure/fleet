package validator

import (
	"fmt"
	"reflect"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
			wantErrMsg: fmt.Errorf("label selector is not supported for resource selection %+v",
				fleetv1beta1.ClusterResourceSelector{
					Group:   "group",
					Version: "v1",
					Kind:    "Kind",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"key": "value",
						},
					},
				}),
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
			wantErrMsg: fmt.Errorf("resource name is required for resource selection %+v",
				fleetv1beta1.ClusterResourceSelector{
					Group:   "group",
					Version: "v1",
					Kind:    "Kind",
					Name:    "",
				}),
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
			wantErrMsg: fmt.Errorf("duplicate selector %+v",
				fleetv1beta1.ClusterResourceSelector{
					Group:   "group",
					Version: "v1",
					Kind:    "Kind",
					Name:    "example",
				}),
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			if got := validateClusterResourceSelectors(tt.cro); !reflect.DeepEqual(tt.wantErrMsg, got) {
				t.Errorf("validateClusterResourceSelectors() = %v, want %v", got, tt.wantErrMsg)
			}
		})
	}
}

func TestValidateClusterResourceOverrideLimit(t *testing.T) {
	tests := map[string]struct {
		overrideCount int
		operation     admissionv1.Operation
		want          bool
	}{
		"create override with zero overrides": {
			overrideCount: 0,
			operation:     admissionv1.Create,
			want:          true,
		},
		"create override with less than 100 overrides": {
			overrideCount: 99,
			operation:     admissionv1.Create,
			want:          true,
		},
		"create override with exactly 100 overrides": {
			overrideCount: 100,
			operation:     admissionv1.Create,
			want:          false,
		},
		"update override with exactly 100 overrides": {
			overrideCount: 100,
			operation:     admissionv1.Update,
			want:          true,
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
				}
				croList.Items = append(croList.Items, cro)
			}
			if got := ValidateClusterResourceOverrideLimit(tt.operation, croList); got != tt.want {
				t.Errorf("ValidateClusterResourceOverrideLimit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateClusterResourceOverrideResourceLimit(t *testing.T) {
	tests := map[string]struct {
		cro        fleetv1alpha1.ClusterResourceOverride
		wantErrMsg error
	}{
		"create one cluster resource override for resource foo": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override1",
				},
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
						{
							Group:   "group",
							Version: "v1",
							Kind:    "Kind",
							Name:    "foo",
						},
					},
				},
			},
			wantErrMsg: nil,
		},
		"one override, multiple selectors for 1 existing cluster override": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override2",
				},
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
						{
							Group:   "group",
							Version: "v1",
							Kind:    "Kind",
							Name:    "example-0",
						},
						{
							Group:   "group",
							Version: "v1",
							Kind:    "Kind",
							Name:    "bar",
						},
					},
				},
			},
			wantErrMsg: fmt.Errorf("the resource %v has been selected by both %v and %v, which is not supported", "example-0", "override2", "override-0"),
		},
		"one override, multiple selectors for existing cluster overrides": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "override-1",
				},
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
						{
							Group:   "group",
							Version: "v1",
							Kind:    "Kind",
							Name:    "example-1",
						},
					},
				},
			},
			wantErrMsg: nil,
		},
	}
	croList := &fleetv1alpha1.ClusterResourceOverrideList{}
	for i := 0; i < 2; i++ {
		cro := fleetv1alpha1.ClusterResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("override-%d", i),
			},
			Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
					{
						Group:   "group",
						Version: "v1",
						Kind:    "Kind",
						Name:    fmt.Sprintf("example-%d", i),
					},
				},
			},
		}
		croList.Items = append(croList.Items, cro)
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			if got := ValidateClusterResourceOverrideResourceLimit(tt.cro, croList); !reflect.DeepEqual(tt.wantErrMsg, got) {
				t.Errorf("ValidateClusterResourceOverrideResourceLimit() = %v, want %v", got, tt.wantErrMsg)
			}
		})
	}
}
