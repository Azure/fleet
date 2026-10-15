/*
Copyright 2026 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package placementpolicy

import (
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
)

const (
	regionLabel = "topology.kubernetes.io/region"
	envLabel    = "environment"
)

// testCluster builds a MemberCluster with the given labels, non-resource properties, and
// allocatable resource usage for matcher tests.
func testCluster(name string, clusterLabels map[string]string, properties map[string]string, allocatable corev1.ResourceList) *clusterv1beta1.MemberCluster {
	mc := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: clusterLabels,
		},
	}
	if len(properties) > 0 {
		mc.Status.Properties = map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{}
		for k, v := range properties {
			mc.Status.Properties[clusterv1beta1.PropertyName(k)] = clusterv1beta1.PropertyValue{Value: v}
		}
	}
	mc.Status.ResourceUsage.Allocatable = allocatable
	return mc
}

func TestResolveCounts(t *testing.T) {
	testCases := []struct {
		name      string
		selector  *kfplacementv1alpha1.ClusterSelector
		want      resolvedCounts
		wantError bool
	}{
		{
			name:     "count defaults to one when unset",
			selector: &kfplacementv1alpha1.ClusterSelector{},
			want:     resolvedCounts{desired: 1, minimum: 1},
		},
		{
			name: "integer count",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count: ptr.To(intstr.FromInt32(3)),
			},
			want: resolvedCounts{desired: 3, minimum: 3},
		},
		{
			name: "integer count with explicit minCount",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count:    ptr.To(intstr.FromInt32(3)),
				MinCount: ptr.To(int32(2)),
			},
			want: resolvedCounts{desired: 3, minimum: 2},
		},
		{
			name: "count All defaults minCount to one",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count: ptr.To(intstr.FromString("All")),
			},
			want: resolvedCounts{selectAll: true, minimum: 1},
		},
		{
			name: "count All with explicit minCount",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count:    ptr.To(intstr.FromString("All")),
				MinCount: ptr.To(int32(5)),
			},
			want: resolvedCounts{selectAll: true, minimum: 5},
		},
		{
			name: "invalid string count",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count: ptr.To(intstr.FromString("Some")),
			},
			wantError: true,
		},
		{
			// The CRD pattern admits digit strings, so a manifest with count: "3" passes admission
			// and must resolve the same as the integer form rather than being rejected.
			name: "digit string count is parsed like an integer",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count: ptr.To(intstr.FromString("3")),
			},
			want: resolvedCounts{desired: 3, minimum: 3},
		},
		{
			name: "digit string count with explicit minCount",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count:    ptr.To(intstr.FromString("3")),
				MinCount: ptr.To(int32(2)),
			},
			want: resolvedCounts{desired: 3, minimum: 2},
		},
		{
			name: "string count at the upper bound",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count: ptr.To(intstr.FromString("999")),
			},
			want: resolvedCounts{desired: 999, minimum: 999},
		},
		{
			// Parseable as an integer but below the floor: the string branch must reject it just as
			// the integer branch rejects a non-positive count.
			name: "non-positive digit string count",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count: ptr.To(intstr.FromString("0")),
			},
			wantError: true,
		},
		{
			name: "non-positive integer count",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count: ptr.To(intstr.FromInt32(0)),
			},
			wantError: true,
		},
		{
			// The CRD pattern bounds only the string form; an integer past the limit bypasses
			// admission, and unchecked it would overflow the aggregated desired total.
			name: "integer count above the upper bound",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count: ptr.To(intstr.FromInt32(1000)),
			},
			wantError: true,
		},
		{
			name: "maximum int32 integer count",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count: ptr.To(intstr.FromInt32(math.MaxInt32)),
			},
			wantError: true,
		},
		{
			name: "integer count at the upper bound",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count: ptr.To(intstr.FromInt32(999)),
			},
			want: resolvedCounts{desired: 999, minimum: 999},
		},
		{
			// A digit string past the bound (only reachable by bypassing admission) is rejected.
			name: "digit string count above the upper bound",
			selector: &kfplacementv1alpha1.ClusterSelector{
				Count: ptr.To(intstr.FromString("1000")),
			},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveCounts(tc.selector)
			if tc.wantError {
				if err == nil {
					t.Fatalf("resolveCounts(%v) = %v, nil, want error", tc.selector, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveCounts(%v) returned unexpected error: %v", tc.selector, err)
			}
			if diff := cmp.Diff(got, tc.want, cmp.AllowUnexported(resolvedCounts{})); diff != "" {
				t.Errorf("resolveCounts(%v) mismatch (-got, +want):\n%s", tc.selector, diff)
			}
		})
	}
}

// TestMatchesTermsWithBrokenTerms covers the OR semantics under evaluation errors: one broken
// term must not veto a later term that matches on its own, and the error surfaces only when no
// term matches, since only then does the broken term's answer matter.
func TestMatchesTermsWithBrokenTerms(t *testing.T) {
	eastusCluster := testCluster("eastus-cluster", map[string]string{regionLabel: "eastus"}, nil, nil)
	// An operator the evaluator does not know fails unconditionally, unlike a numeric
	// comparison, whose absence semantics can swallow a malformed value into a clean no-match.
	brokenTerm := kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
		MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
			{Key: "resources.kubefleet.dev/total-cpu", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperator("Bogus"), Values: []string{"1"}},
		},
	}

	testCases := []struct {
		name    string
		terms   []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm
		want    bool
		wantErr bool
	}{
		{
			name: "a broken term does not veto a later matching term",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
				brokenTerm,
				{MatchLabels: map[string]string{regionLabel: "eastus"}},
			},
			want: true,
		},
		{
			name: "a broken term after the matching term changes nothing",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
				{MatchLabels: map[string]string{regionLabel: "eastus"}},
				brokenTerm,
			},
			want: true,
		},
		{
			name: "the error surfaces when no term matches",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
				brokenTerm,
				{MatchLabels: map[string]string{regionLabel: "westus"}},
			},
			wantErr: true,
		},
		{
			name:    "only broken terms surface the error",
			terms:   []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{brokenTerm},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := matchesTerms(eastusCluster, tc.terms)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("matchesTerms() = %v, nil, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("matchesTerms() = %v, want no error", err)
			}
			if got != tc.want {
				t.Errorf("matchesTerms() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMatchesTerms(t *testing.T) {
	eastusCluster := testCluster("eastus-cluster", map[string]string{regionLabel: "eastus", envLabel: "prod"}, nil, nil)

	testCases := []struct {
		name    string
		cluster *clusterv1beta1.MemberCluster
		terms   []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm
		want    bool
	}{
		{
			name:    "empty terms match every cluster",
			cluster: eastusCluster,
			terms:   nil,
			want:    true,
		},
		{
			name:    "terms are ORed",
			cluster: eastusCluster,
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
				{MatchLabels: map[string]string{regionLabel: "westus"}},
				{MatchLabels: map[string]string{regionLabel: "eastus"}},
			},
			want: true,
		},
		{
			name:    "no term matches",
			cluster: eastusCluster,
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
				{MatchLabels: map[string]string{regionLabel: "westus"}},
				{MatchLabels: map[string]string{regionLabel: "centralus"}},
			},
			want: false,
		},
		{
			name:    "requirement groups within a term are ANDed",
			cluster: eastusCluster,
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
				{
					MatchLabels: map[string]string{regionLabel: "eastus"},
					MatchLabelExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
						{Key: envLabel, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorIn, Values: []string{"dev"}},
					},
				},
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := matchesTerms(tc.cluster, tc.terms)
			if err != nil {
				t.Fatalf("matchesTerms(%s, %v) returned unexpected error: %v", tc.cluster.Name, tc.terms, err)
			}
			if got != tc.want {
				t.Errorf("matchesTerms(%s, %v) = %t, want %t", tc.cluster.Name, tc.terms, got, tc.want)
			}
		})
	}
}

func TestMatchesTermLabelExpressions(t *testing.T) {
	labeledCluster := testCluster("labeled-cluster", map[string]string{regionLabel: "eastus"}, nil, nil)

	testCases := []struct {
		name      string
		cluster   *clusterv1beta1.MemberCluster
		expr      kfplacementv1alpha1.LabelClusterPropertyExpression
		want      bool
		wantError bool
	}{
		{
			name:    "In matches present value",
			cluster: labeledCluster,
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: regionLabel, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorIn, Values: []string{"eastus", "westus"},
			},
			want: true,
		},
		{
			name:    "In does not match an absent key",
			cluster: labeledCluster,
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: envLabel, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorIn, Values: []string{"prod"},
			},
			want: false,
		},
		{
			name:    "NotIn matches an absent key",
			cluster: labeledCluster,
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: envLabel, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorNotIn, Values: []string{"prod"},
			},
			want: true,
		},
		{
			name:    "Exists matches a present key",
			cluster: labeledCluster,
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: regionLabel, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists,
			},
			want: true,
		},
		{
			name:    "DoesNotExist matches an absent key",
			cluster: labeledCluster,
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: envLabel, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorDoesNotExist,
			},
			want: true,
		},
		{
			name:    "numeric operator on a label expression is an error",
			cluster: labeledCluster,
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: regionLabel, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"1"},
			},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			term := &kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
				MatchLabelExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{tc.expr},
			}
			got, err := matchesTerm(tc.cluster, term)
			if tc.wantError {
				if err == nil {
					t.Fatalf("matchesTerm(%s, %v) = %t, nil, want error", tc.cluster.Name, term, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("matchesTerm(%s, %v) returned unexpected error: %v", tc.cluster.Name, term, err)
			}
			if got != tc.want {
				t.Errorf("matchesTerm(%s, %v) = %t, want %t", tc.cluster.Name, term, got, tc.want)
			}
		})
	}
}

func TestMatchesTermPropertyExpressions(t *testing.T) {
	propertiedCluster := testCluster(
		"propertied-cluster",
		nil,
		map[string]string{
			propertyprovider.NodeCountProperty: "4",
			"example.com/tier":                 "premium",
			"example.com/invalid-quantity":     "not-a-number",
		},
		corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse("8"),
			corev1.ResourceEphemeralStorage: resource.MustParse("100Gi"),
		},
	)

	testCases := []struct {
		name      string
		expr      kfplacementv1alpha1.LabelClusterPropertyExpression
		want      bool
		wantError bool
	}{
		{
			name: "Gt on a non-resource property",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: propertyprovider.NodeCountProperty, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"3"},
			},
			want: true,
		},
		{
			name: "Le on a non-resource property",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: propertyprovider.NodeCountProperty, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorLe, Values: []string{"3"},
			},
			want: false,
		},
		{
			name: "Eq compares quantities semantically",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: propertyprovider.ResourcePropertyNamePrefix + "allocatable-cpu", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorEq, Values: []string{"8000m"},
			},
			want: true,
		},
		{
			name: "Ne on a reported resource property",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: propertyprovider.ResourcePropertyNamePrefix + "allocatable-cpu", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorNe, Values: []string{"8"},
			},
			want: false,
		},
		{
			name: "resource names with dashes resolve correctly",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: propertyprovider.ResourcePropertyNamePrefix + "allocatable-ephemeral-storage", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGe, Values: []string{"50Gi"},
			},
			want: true,
		},
		{
			name: "numeric operator on an unreported property does not match",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: "example.com/unreported", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"1"},
			},
			want: false,
		},
		{
			name: "numeric operator on an unreported resource capacity does not match",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: propertyprovider.ResourcePropertyNamePrefix + "available-cpu", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"1"},
			},
			want: false,
		},
		{
			name: "numeric operator on a non-numeric property value is an error",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: "example.com/invalid-quantity", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"1"},
			},
			wantError: true,
		},
		{
			name: "numeric operator with a non-quantity expected value is an error",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: propertyprovider.NodeCountProperty, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"three"},
			},
			wantError: true,
		},
		{
			name: "invalid capacity type in a resource property name is an error",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: propertyprovider.ResourcePropertyNamePrefix + "reserved-cpu", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"1"},
			},
			wantError: true,
		},
		{
			name: "In on a string property",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: "example.com/tier", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorIn, Values: []string{"premium", "standard"},
			},
			want: true,
		},
		{
			name: "NotIn matches an unreported property",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: "example.com/unreported", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorNotIn, Values: []string{"premium"},
			},
			want: true,
		},
		{
			name: "Exists on a reported resource property",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: propertyprovider.ResourcePropertyNamePrefix + "allocatable-cpu", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists,
			},
			want: true,
		},
		{
			name: "DoesNotExist on an unreported property",
			expr: kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: "example.com/unreported", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorDoesNotExist,
			},
			want: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			term := &kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{tc.expr},
			}
			got, err := matchesTerm(propertiedCluster, term)
			if tc.wantError {
				if err == nil {
					t.Fatalf("matchesTerm(%s, %v) = %t, nil, want error", propertiedCluster.Name, term, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("matchesTerm(%s, %v) returned unexpected error: %v", propertiedCluster.Name, term, err)
			}
			if got != tc.want {
				t.Errorf("matchesTerm(%s, %v) = %t, want %t", propertiedCluster.Name, term, got, tc.want)
			}
		})
	}
}

func TestValidateTerms(t *testing.T) {
	testCases := []struct {
		name      string
		terms     []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm
		wantError bool
	}{
		{
			name: "valid mixed term",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchLabels: map[string]string{regionLabel: "eastus"},
				MatchLabelExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: envLabel, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists},
				},
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: propertyprovider.NodeCountProperty, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGe, Values: []string{"2"}},
				},
			}},
		},
		{
			name: "numeric operator in a label expression",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchLabelExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: envLabel, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"1"}},
				},
			}},
			wantError: true,
		},
		{
			name: "numeric property expression with two values",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: propertyprovider.NodeCountProperty, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"1", "2"}},
				},
			}},
			wantError: true,
		},
		{
			name: "numeric property expression with a non-quantity value",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: propertyprovider.NodeCountProperty, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"three"}},
				},
			}},
			wantError: true,
		},
		{
			name: "resource property key with an unknown capacity type",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: propertyprovider.ResourcePropertyNamePrefix + "reserved-cpu", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists},
				},
			}},
			wantError: true,
		},
		{
			name: "resource property key without a resource name",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: propertyprovider.ResourcePropertyNamePrefix + "allocatable-", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"1"}},
				},
			}},
			wantError: true,
		},
		{
			name: "matchLabels with an invalid key",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchLabels: map[string]string{"not a valid key": "eastus"},
			}},
			wantError: true,
		},
		{
			name: "matchLabels with an invalid value",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchLabels: map[string]string{regionLabel: "not a valid value"},
			}},
			wantError: true,
		},
		{
			name: "non-resource property key that is empty",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: "", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorDoesNotExist},
				},
			}},
			wantError: true,
		},
		{
			name: "non-resource property key that is not a qualified name",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: "not a valid key", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists},
				},
			}},
			wantError: true,
		},
		{
			name: "valid non-resource property key",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: propertyprovider.NodeCountProperty, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists},
				},
			}},
		},
		{
			name: "valid multi-segment non-resource property key",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					// The Azure property provider ships per-SKU capacity keys in this multi-slash form;
					// a whole-string IsQualifiedName check would wrongly reject them.
					{Key: "kubernetes.azure.com/vm-sizes/Standard_D2s_v3/count", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists},
				},
			}},
		},
		{
			name: "In on a resource property is rejected",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: propertyprovider.ResourcePropertyNamePrefix + "allocatable-cpu", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorIn, Values: []string{"8000m"}},
				},
			}},
			wantError: true,
		},
		{
			name: "NotIn on a resource property is rejected",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: propertyprovider.ResourcePropertyNamePrefix + "allocatable-cpu", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorNotIn, Values: []string{"8"}},
				},
			}},
			wantError: true,
		},
		{
			name: "Exists on a resource property is allowed",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: propertyprovider.ResourcePropertyNamePrefix + "allocatable-cpu", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists},
				},
			}},
		},
		{
			name: "In on a string property is allowed",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: propertyprovider.NodeCountProperty, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorIn, Values: []string{"3"}},
				},
			}},
		},
		{
			name: "resource property with an invalid resource name",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: propertyprovider.ResourcePropertyNamePrefix + "allocatable-not a resource", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists},
				},
			}},
			wantError: true,
		},
		{
			name: "valid resource property with a dashed resource name",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: propertyprovider.ResourcePropertyNamePrefix + "allocatable-ephemeral-storage", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists},
				},
			}},
		},
		{
			name: "valid resource property with a domain-qualified extended resource name",
			terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					// An extended resource name carries its own slash; validating the whole key would
					// reject it for a second slash even though the cluster reports it and evaluation
					// resolves it. Only the resource-name portion is validated, so this is accepted.
					{Key: propertyprovider.ResourcePropertyNamePrefix + "allocatable-nvidia.com/gpu", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists},
				},
			}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTerms(tc.terms)
			if gotError := err != nil; gotError != tc.wantError {
				t.Errorf("validateTerms(%v) error = %v, want error %t", tc.terms, err, tc.wantError)
			}
		})
	}
}

// TestValidateTermsDeterministicError guards against a reconcile hot-loop: the validation error
// surfaces on the policy's Scheduled condition message, so a term with more than one invalid
// matchLabels entry must always report the same one. Go randomizes map iteration, so validating in
// map order would return a different message from call to call and defeat the status no-op check.
func TestValidateTermsDeterministicError(t *testing.T) {
	terms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
		MatchLabels: map[string]string{
			"bad key one": "eastus",
			"bad key two": "westus",
		},
	}}
	first := validateTerms(terms)
	if first == nil {
		t.Fatalf("validateTerms(%v) error = nil, want error", terms)
	}
	// Many iterations so a randomized order would almost certainly have produced a different
	// message at least once.
	for i := 0; i < 100; i++ {
		got := validateTerms(terms)
		if got == nil || got.Error() != first.Error() {
			t.Fatalf("validateTerms(%v) error = %v, want stable %v", terms, got, first)
		}
	}
}

func TestMatchesTermMixedGroups(t *testing.T) {
	cluster := testCluster(
		"mixed-cluster",
		map[string]string{regionLabel: "eastus"},
		map[string]string{propertyprovider.NodeCountProperty: "4"},
		nil,
	)

	term := &kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
		MatchLabels: map[string]string{regionLabel: "eastus"},
		MatchLabelExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
			{Key: envLabel, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorDoesNotExist},
		},
		MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
			{Key: propertyprovider.NodeCountProperty, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGe, Values: []string{"4"}},
		},
	}

	got, err := matchesTerm(cluster, term)
	if err != nil {
		t.Fatalf("matchesTerm(%s, %v) returned unexpected error: %v", cluster.Name, term, err)
	}
	if !got {
		t.Errorf("matchesTerm(%s, %v) = false, want true", cluster.Name, term)
	}
}
