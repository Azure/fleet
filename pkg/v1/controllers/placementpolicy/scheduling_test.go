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
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
)

// allEligible is a test eligibility gate that admits every cluster.
type allEligible struct{}

func (allEligible) IsEligible(*clusterv1beta1.MemberCluster) (bool, string) {
	return true, ""
}

func TestEvaluateSelectorsSkipsClustersWithUnevaluableData(t *testing.T) {
	spec := &kfplacementv1alpha1.PlacementPolicySpec{
		ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{{
			Terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: propertyprovider.NodeCountProperty, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGe, Values: []string{"2"}},
				},
			}},
			Count: ptr.To(intstr.FromInt32(1)),
		}},
	}
	clusters := []clusterv1beta1.MemberCluster{
		*testCluster("good-cluster", nil, map[string]string{propertyprovider.NodeCountProperty: "4"}, nil),
		*testCluster("bad-data-cluster", nil, map[string]string{propertyprovider.NodeCountProperty: "not-a-number"}, nil),
	}

	outcomes, err := evaluateSelectors(spec, clusters, allEligible{})
	if err != nil {
		t.Fatalf("evaluateSelectors(%v) returned unexpected error: %v", spec, err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("evaluateSelectors(%v) returned %d outcomes, want 1", spec, len(outcomes))
	}
	wantMatched := []string{"good-cluster"}
	if diff := cmp.Diff(outcomes[0].matched, wantMatched); diff != "" {
		t.Errorf("evaluateSelectors(%v) matched mismatch (-got, +want):\n%s", spec, diff)
	}
}

func TestAggregateCounts(t *testing.T) {
	testCases := []struct {
		name          string
		outcomes      []selectorOutcome
		wantDesired   int32
		wantScheduled int32
	}{
		{
			name: "integer counts sum per selector",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 2, minimum: 2}, matched: []string{"a", "b", "c"}},
				{counts: resolvedCounts{desired: 3, minimum: 3}, matched: []string{"a"}},
			},
			wantDesired:   5,
			wantScheduled: 3,
		},
		{
			name: "selectAll desired floors at the minimum when unfulfilled",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{selectAll: true, minimum: 2}, matched: []string{"a"}},
			},
			wantDesired:   2,
			wantScheduled: 1,
		},
		{
			name: "selectAll desired follows the match count when fulfilled",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{selectAll: true, minimum: 1}, matched: []string{"a", "b", "c"}},
			},
			wantDesired:   3,
			wantScheduled: 3,
		},
		{
			name: "empty fleet with a bare selectAll selector shows the gap",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{selectAll: true, minimum: 1}},
			},
			wantDesired:   1,
			wantScheduled: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			desired, scheduled := aggregateCounts(tc.outcomes)
			if desired != tc.wantDesired || scheduled != tc.wantScheduled {
				t.Errorf("aggregateCounts(%v) = %d, %d, want %d, %d", tc.outcomes, desired, scheduled, tc.wantDesired, tc.wantScheduled)
			}
		})
	}
}

func TestScheduledCondition(t *testing.T) {
	testCases := []struct {
		name       string
		outcomes   []selectorOutcome
		wantStatus metav1.ConditionStatus
		wantReason string
	}{
		{
			name: "all selectors fulfilled in full",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 1, minimum: 1}, matched: []string{"a"}},
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters,
		},
		{
			name: "floor reached but short of desired stays False per the binary contract",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 3, minimum: 1}, matched: []string{"a"}},
			},
			wantStatus: metav1.ConditionFalse,
			wantReason: kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters,
		},
		{
			name: "below the floor",
			outcomes: []selectorOutcome{
				{counts: resolvedCounts{desired: 3, minimum: 2}, matched: []string{"a"}},
			},
			wantStatus: metav1.ConditionFalse,
			wantReason: kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := scheduledCondition(7, tc.outcomes)
			if got.Status != tc.wantStatus || got.Reason != tc.wantReason {
				t.Errorf("scheduledCondition(7, %v) = %s/%s, want %s/%s", tc.outcomes, got.Status, got.Reason, tc.wantStatus, tc.wantReason)
			}
			if got.ObservedGeneration != 7 {
				t.Errorf("scheduledCondition(7, %v) observedGeneration = %d, want 7", tc.outcomes, got.ObservedGeneration)
			}
		})
	}
}

func TestTolerationTolerates(t *testing.T) {
	noScheduleTaint := clusterv1beta1.Taint{Key: "workload", Value: "restricted", Effect: "NoSchedule"}

	testCases := []struct {
		name       string
		toleration kfplacementv1alpha1.Toleration
		want       bool
	}{
		{
			name:       "equal operator with matching value",
			toleration: kfplacementv1alpha1.Toleration{Key: "workload", Operator: "Equal", Value: "restricted", Effect: "NoSchedule"},
			want:       true,
		},
		{
			name:       "equal operator with mismatched value",
			toleration: kfplacementv1alpha1.Toleration{Key: "workload", Operator: "Equal", Value: "other", Effect: "NoSchedule"},
			want:       false,
		},
		{
			name:       "exists operator ignores the value",
			toleration: kfplacementv1alpha1.Toleration{Key: "workload", Operator: "Exists", Effect: "NoSchedule"},
			want:       true,
		},
		{
			name:       "empty key with exists matches every taint",
			toleration: kfplacementv1alpha1.Toleration{Operator: "Exists"},
			want:       true,
		},
		{
			name:       "empty effect matches every effect",
			toleration: kfplacementv1alpha1.Toleration{Key: "workload", Operator: "Equal", Value: "restricted"},
			want:       true,
		},
		{
			name:       "unrecognized operator tolerates nothing",
			toleration: kfplacementv1alpha1.Toleration{Key: "workload", Operator: "Fuzzy", Value: "restricted", Effect: "NoSchedule"},
			want:       false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tolerationTolerates(&noScheduleTaint, &tc.toleration)
			if got != tc.want {
				t.Errorf("tolerationTolerates(%v, %v) = %t, want %t", noScheduleTaint, tc.toleration, got, tc.want)
			}
		})
	}
}
