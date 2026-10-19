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
	"cmp"
	"math"
	"slices"

	corev1 "k8s.io/api/core/v1"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
)

// criterionWeight is the weight the FEP assigns to each ranking criterion. All criteria weigh
// the same, so a cluster's total lies in [0, 100 * len(rankingCriteria)].
const criterionWeight = 100.0

// rankingCriterion reads one preference metric off a member cluster. A cluster that does not
// report the metric does not take part in the criterion and scores 0 on it, which after
// normalization is the least preferred position whichever direction the criterion prefers.
type rankingCriterion struct {
	preferHigher bool
	value        func(*clusterv1beta1.MemberCluster) (float64, bool)
}

// rankingCriteria are the FEP's cluster preferences, in its order: spread placements as evenly
// as possible across a homogeneous fleet by preferring the smaller and emptier clusters. This is
// the same idea as the Kubernetes scheduler's LeastAllocated strategy.
var rankingCriteria = []rankingCriterion{
	{preferHigher: false, value: nodeCount},
	{preferHigher: true, value: availableResource(corev1.ResourceCPU)},
	{preferHigher: true, value: availableResource(corev1.ResourceMemory)},
}

func nodeCount(cluster *clusterv1beta1.MemberCluster) (float64, bool) {
	q, err := numericPropertyValueFrom(cluster, propertyprovider.NodeCountProperty)
	if err != nil || q == nil {
		// Malformed self-reported data is handled the same way as absent data: the cluster
		// simply does not take part in this criterion. Selector evaluation already logs it.
		return 0, false
	}
	return q.AsApproximateFloat64(), true
}

func availableResource(name corev1.ResourceName) func(*clusterv1beta1.MemberCluster) (float64, bool) {
	return func(cluster *clusterv1beta1.MemberCluster) (float64, bool) {
		q, reported := cluster.Status.ResourceUsage.Available[name]
		if !reported {
			return 0, false
		}
		return q.AsApproximateFloat64(), true
	}
}

// rankClusters orders the candidate clusters of one selector from most to least preferred, per
// the FEP: on every criterion a cluster scores 100 * (X - Min) / (Max - Min) against the
// candidates' own minimum and maximum (inverted when lower is better), 0 for all of them when
// they tie, and the criteria add up. Equal totals break on the cluster name, so the order is
// deterministic. Only the order leaves this function; the scores are reported nowhere.
func rankClusters(candidates []*clusterv1beta1.MemberCluster) []string {
	totals := make(map[string]float64, len(candidates))
	for i := range rankingCriteria {
		scoreCriterion(&rankingCriteria[i], candidates, totals)
	}

	names := make([]string, 0, len(candidates))
	for _, cluster := range candidates {
		names = append(names, cluster.Name)
	}
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(totals[b], totals[a]), cmp.Compare(a, b))
	})
	return names
}

// scoreCriterion adds each candidate's normalized score on one criterion to its total.
func scoreCriterion(criterion *rankingCriterion, candidates []*clusterv1beta1.MemberCluster, totals map[string]float64) {
	values := make(map[string]float64, len(candidates))
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, cluster := range candidates {
		v, reported := criterion.value(cluster)
		if !reported || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			// Self-reported data: a negative or non-finite value would set the range for every
			// other candidate, so it is treated as unreported instead.
			continue
		}
		values[cluster.Name] = v
		lo, hi = min(lo, v), max(hi, v)
	}
	if lo >= hi {
		// Every reporting cluster has the same value (or none reports): the criterion cannot
		// tell the candidates apart and contributes 0 to each of them.
		return
	}
	for name, v := range values {
		score := criterionWeight * (v - lo) / (hi - lo)
		if !criterion.preferHigher {
			score = criterionWeight - score
		}
		totals[name] += score
	}
}
