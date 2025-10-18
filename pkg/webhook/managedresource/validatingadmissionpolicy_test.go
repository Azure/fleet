/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package managedresource

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	admv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetValidatingAdmissionPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		isHub bool
	}{
		{
			name:  "member cluster",
			isHub: false,
		},
		{
			name:  "hub cluster",
			isHub: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vap := getValidatingAdmissionPolicy(tt.isHub)

			// Both hub and member clusters should have the same single rule that covers all resources
			expectedRule := admv1.NamedRuleWithOperations{
				RuleWithOperations: admv1.RuleWithOperations{
					Rule: admv1.Rule{
						APIGroups:   []string{"*"},
						Resources:   []string{"*"},
						APIVersions: []string{"*"},
					},
					Operations: []admv1.OperationType{admv1.Create, admv1.Update, admv1.Delete},
				},
			}

			if vap.Spec.MatchConstraints == nil {
				t.Fatal("MatchConstraints should not be nil")
			}

			if len(vap.Spec.MatchConstraints.ResourceRules) != 1 {
				t.Errorf("Expected exactly 1 resource rule, got %d", len(vap.Spec.MatchConstraints.ResourceRules))
			}

			actualRule := vap.Spec.MatchConstraints.ResourceRules[0]
			if diff := cmp.Diff(expectedRule, actualRule); diff != "" {
				t.Errorf("Resource rule mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMutateValidatingAdmissionPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		isHub           bool
		resourceVersion string
		initialLabels   map[string]string
	}{
		{
			name:            "preserves ResourceVersion and updates labels for member cluster",
			isHub:           false,
			resourceVersion: "12345",
			initialLabels:   map[string]string{"existing": "label"},
		},
		{
			name:            "preserves ResourceVersion and updates labels for hub cluster",
			isHub:           true,
			resourceVersion: "67890",
			initialLabels:   map[string]string{"old": "value"},
		},
		{
			name:            "preserves empty ResourceVersion and sets labels",
			isHub:           false,
			resourceVersion: "",
			initialLabels:   nil,
		},
		{
			name:            "overwrites existing managed label while preserving ResourceVersion",
			isHub:           false,
			resourceVersion: "54321",
			initialLabels:   map[string]string{"fleet.azure.com/managed-by": "old-value", "other": "label"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vap := &admv1.ValidatingAdmissionPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-policy",
					ResourceVersion: tt.resourceVersion,
					Labels:          tt.initialLabels,
				},
			}

			mutateValidatingAdmissionPolicy(vap, tt.isHub)

			if vap.ResourceVersion != tt.resourceVersion {
				t.Errorf("mutateValidatingAdmissionPolicy() ResourceVersion = %v, want %v", vap.ResourceVersion, tt.resourceVersion)
			}

			wantManagedByLabel := "arm"
			if got := vap.Labels["fleet.azure.com/managed-by"]; got != wantManagedByLabel {
				t.Errorf("mutateValidatingAdmissionPolicy() managed-by label = %v, want %v", got, wantManagedByLabel)
			}

			// Verify that only the managed-by label exists (other labels are not preserved)
			wantLabels := map[string]string{
				"fleet.azure.com/managed-by": "arm",
			}
			if diff := cmp.Diff(wantLabels, vap.Labels); diff != "" {
				t.Errorf("mutateValidatingAdmissionPolicy() labels mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetValidatingAdmissionPolicyBinding(t *testing.T) {
	t.Parallel()

	vap := getValidatingAdmissionPolicyBinding()
	if vap == nil {
		t.Errorf("getValidatingAdmissionPolicyBinding() = nil, want non-nil")
	}
}

func TestMutateValidatingAdmissionPolicyBinding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		resourceVersion string
		initialLabels   map[string]string
	}{
		{
			name:            "preserves ResourceVersion and updates labels",
			resourceVersion: "12345",
			initialLabels:   map[string]string{"existing": "label"},
		},
		{
			name:            "preserves empty ResourceVersion and sets labels",
			resourceVersion: "",
			initialLabels:   nil,
		},
		{
			name:            "overwrites existing managed label while preserving ResourceVersion",
			resourceVersion: "67890",
			initialLabels:   map[string]string{"fleet.azure.com/managed-by": "old-value", "other": "label"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vapb := &admv1.ValidatingAdmissionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-binding",
					ResourceVersion: tt.resourceVersion,
					Labels:          tt.initialLabels,
				},
			}

			mutateValidatingAdmissionPolicyBinding(vapb)

			if vapb.ResourceVersion != tt.resourceVersion {
				t.Errorf("mutateValidatingAdmissionPolicyBinding() ResourceVersion = %v, want %v", vapb.ResourceVersion, tt.resourceVersion)
			}

			wantManagedByLabel := "arm"
			if got := vapb.Labels["fleet.azure.com/managed-by"]; got != wantManagedByLabel {
				t.Errorf("mutateValidatingAdmissionPolicyBinding() managed-by label = %v, want %v", got, wantManagedByLabel)
			}

			// Verify that only the managed-by label exists (other labels are not preserved)
			wantLabels := map[string]string{
				"fleet.azure.com/managed-by": "arm",
			}
			if diff := cmp.Diff(wantLabels, vapb.Labels); diff != "" {
				t.Errorf("mutateValidatingAdmissionPolicyBinding() labels mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
