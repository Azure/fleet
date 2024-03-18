package validator

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestValidateResourceSelectedByName(t *testing.T) {
	tests := map[string]struct {
		name       string
		cro        fleetv1alpha1.ClusterResourceOverride
		want       bool
		wantErrMsg string
	}{
		// TODO: Add test cases.
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
			want:       false,
			wantErrMsg: "resource is not being selected by resource name",
		},
		"resource selected by name": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
						{
							Group:   "group",
							Version: "v1",
							Kind:    "Kind",
							Name:    "valid-name",
						},
					},
				},
			},
			want:       true,
			wantErrMsg: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateResourceSelectedByName(tt.cro); got != tt.want {
				t.Errorf("ValidateResourceSelectedByName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateCROLimit(t *testing.T) {
	tests := map[string]struct {
		overrideCount int
		want          bool
		wantErrMsg    string
	}{
		// TODO: Add test cases.
		"no overrides": {
			overrideCount: 0,
			want:          true,
			wantErrMsg:    "",
		},
		"less than 100 overrides": {
			overrideCount: 75,
			want:          true,
			wantErrMsg:    "",
		},
		"exactly 100 overrides": {
			overrideCount: 100,
			want:          false,
			wantErrMsg:    "",
		},
	}

	// Run the tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Create a fake client for testing
			scheme := runtime.NewScheme()
			if err := fleetv1alpha1.AddToScheme(scheme); err != nil {
				t.Fatalf("failed to add scheme: %v", err)
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			ctx := context.Background()

			// Add overrides to the fake client for testing
			for i := 0; i < tt.overrideCount; i++ {
				cro := &fleetv1alpha1.ClusterResourceOverride{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("override-%d", i),
					},
				}
				if err := fakeClient.Create(ctx, cro); err != nil {
					t.Fatalf("failed to create override: %v", err)
				}
			}
			if got := ValidateCROLimit(ctx, fakeClient); got != tt.want {
				t.Errorf("ValidateCROLimit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateCROResourceLimit(t *testing.T) {
	// Create a fake client for testing
	scheme := runtime.NewScheme()
	if err := fleetv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()

	selectors := []fleetv1beta1.ClusterResourceSelector{{Name: "foo"}, {Name: "bar"}, {Name: "baz"}}

	tests := map[string]struct {
		cro           fleetv1alpha1.ClusterResourceOverride
		overrideCount int
		want          bool
		wantErrMsg    string
	}{
		// TODO: Add test cases.
		"create one cluster resource override for already exist for resource ": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
						{
							Name: "foo",
						},
					},
				},
			},
			want:       false,
			wantErrMsg: "only 1 cluster resource override per resource can be created",
		},
		"one override, multiple selectors for existing overrides": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
						{
							Name: "example",
						},
						{
							Name: "bar",
						},
					},
				},
			},
			want:       false,
			wantErrMsg: "only 1 cluster resource override per resource can be created",
		},
		"one override multiple resource overrides": {
			cro: fleetv1alpha1.ClusterResourceOverride{
				Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
						{
							Name: "example",
						},
						{
							Name: "example-1",
						},
					},
				},
			},
			want:       true,
			wantErrMsg: "",
		},
	}
	// Add overrides to the fake client for testing
	for i := 0; i < 5; i++ {
		cro := &fleetv1alpha1.ClusterResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("override-%d", i),
			},
			Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: selectors,
			},
		}
		if err := fakeClient.Create(ctx, cro); err != nil {
			t.Fatalf("failed to create override: %v", err)
		}
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := ValidateCROResourceLimit(ctx, fakeClient, tt.cro); got != tt.want {
				t.Errorf("ValidateCROResourceLimit() = %v, want %v", got, tt.want)
			}
		})
	}
}
