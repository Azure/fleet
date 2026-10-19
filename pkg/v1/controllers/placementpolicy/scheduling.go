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
	"fmt"
	"math"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

// eligibilityChecker is the subset of the scheduler's cluster eligibility gate that the
// placement policy controller depends on. Selector fulfillment is judged against this predicate
// rather than raw label matching, so that a cluster only counts once it is actually usable for
// scheduling (member agent online, heartbeating, and joined); taking it as an interface keeps
// the predicate pluggable and fakeable in tests.
type eligibilityChecker interface {
	IsEligible(cluster *clusterv1beta1.MemberCluster) (eligible bool, reason string)
}

// The Scheduled condition uses the reason constants defined on the API
// (PlacementPolicyScheduledCondReasonFoundAllClusters and
// PlacementPolicyScheduledCondReasonFailedToFindSomeClusters), which encode a binary contract:
// True only when every selector has found all of its desired clusters. One additional local
// reason distinguishes selectors that cannot be evaluated at all from selectors that are simply
// unfulfilled.
const (
	// reasonInvalidClusterSelectors denotes that the cluster selectors cannot be evaluated,
	// e.g., an operator is applied to a key it does not support; unlike
	// FailedToFindSomeRequiredClusters, waiting for more clusters cannot resolve it.
	reasonInvalidClusterSelectors = "InvalidClusterSelectors"
)

// selectorOutcome is the evaluation result of a single cluster selector against the current
// member cluster inventory.
type selectorOutcome struct {
	counts resolvedCounts
	// matched holds the names of the schedulable clusters that satisfy the selector terms,
	// sorted for determinism.
	matched []string
	// sticky holds, sorted, the names of the clusters an existing binding may stick to but
	// that are neither counted nor newly picked: they satisfy the selector terms but are not
	// schedulable right now (ineligible, or carrying a taint the policy does not tolerate), or
	// their reported data could not be evaluated at all. See chooseClusters.
	sticky []string
	// unevaluable holds, sorted, the names of the clusters whose reported data could not be
	// evaluated against the selector terms; they are among the sticky ones, and the Scheduled
	// condition names them, since nothing else tells the user why they are not counted.
	unevaluable []string
	// stale holds, sorted, the names of the clusters, matched or sticky, that the selector keeps
	// only because their binding records its terms: they no longer satisfy them. A scale-down
	// trims them first. See chooseClusters.
	stale []string
	// chosen holds the names of the clusters counted as selected for this selector, sorted;
	// chooseClusters fills it in.
	chosen []string
	// terms references the originating selector's terms; the claim lifecycle propagates them
	// onto ClusterClaim objects for unfulfilled selectors.
	terms []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm
	// whenUnfulfilled is the originating selector's unfulfillment stance; claims are issued
	// only for selectors set to AddClusterClaim.
	whenUnfulfilled kfplacementv1alpha1.WhenUnfulfilledOption
}

// int32Len returns the length of a cluster-name list as an int32, saturating at the type's
// maximum; fleet sizes sit far below that bound, the saturation only exists to make the
// conversion provably safe. It is the single int-to-int32 conversion point required because
// the API status counts are int32 while Go slice lengths are int.
func int32Len(s []string) int32 {
	n := len(s)
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n)
}

// fulfilled reports whether the selector has reached its fulfillment floor (minCount).
func (o *selectorOutcome) fulfilled() bool {
	return int32Len(o.matched) >= o.counts.minimum
}

// satisfiedInFull reports whether the selector needs no further clusters: the desired count is
// reached, or the selector requests all matching clusters and its floor is met.
func (o *selectorOutcome) satisfiedInFull() bool {
	if o.counts.selectAll {
		return o.fulfilled()
	}
	return int32Len(o.matched) >= o.counts.desired
}

// evaluateSelectors evaluates every cluster selector of a policy against the given member
// clusters. Only schedulable clusters count toward fulfillment: the cluster must pass the
// scheduler's eligibility gate (member agent online, heartbeating, and joined) and all of its
// taints must be tolerated by the policy. An empty selector list is interpreted as a single
// selector matching all available clusters, per the API contract.
//
// Scheduling decisions stick, as they do for ClusterResourcePlacement and ResourcePlacement: a
// cluster bound for a selector stays selected by it even once its labels or properties stop
// matching, so that a cluster whose properties hover around a selector's threshold (a node count
// moving with autoscaling) does not have its resources removed and placed again on every swing.
// recorded maps each bound cluster to the selectors its binding was made for; a cluster stays
// selected by a selector whose terms the binding still records. Changing the selector's terms is
// what re-evaluates its clusters from scratch, as a new scheduling policy does for placements; a
// cluster leaving the fleet, or its binding being deleted, releases it too.
func evaluateSelectors(spec *kfplacementv1alpha1.PlacementPolicySpec, clusters []clusterv1beta1.MemberCluster, checker eligibilityChecker, recorded map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly) ([]selectorOutcome, error) {
	schedulable := make([]*clusterv1beta1.MemberCluster, 0, len(clusters))
	// unschedulable keeps the clusters a binding may stick to while they are temporarily out
	// of the running. A cluster on its way out is left out, so that its binding is released.
	var unschedulable []*clusterv1beta1.MemberCluster
	for i := range clusters {
		cluster := &clusters[i]
		eligible, _ := checker.IsEligible(cluster)
		switch {
		case eligible && taintsTolerated(cluster.Spec.Taints, spec.Tolerations):
			schedulable = append(schedulable, cluster)
		case cluster.DeletionTimestamp.IsZero():
			unschedulable = append(unschedulable, cluster)
		}
	}

	selectors := spec.ClusterSelectors
	if len(selectors) == 0 {
		// The synthesized selector mirrors what CRD defaulting would have produced had the
		// user written it out — including the AddClusterClaim default, so that a bare policy
		// in a fleet with no schedulable clusters can still signal that a cluster is needed.
		selectors = []kfplacementv1alpha1.ClusterSelector{
			{
				Count:           ptr.To(intstr.FromString(countAllClusters)),
				WhenUnfulfilled: kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim,
			},
		}
	}

	outcomes := make([]selectorOutcome, 0, len(selectors))
	for i := range selectors {
		selector := &selectors[i]
		counts, err := resolveCounts(selector)
		if err != nil {
			return nil, err
		}
		// Validate the terms structurally so that invalid selectors surface even when no
		// schedulable cluster exists to evaluate them against.
		if err := validateTerms(selector.Terms); err != nil {
			return nil, err
		}

		matched, unevaluable := matchingClusterNames(schedulable, selector.Terms)
		alsoMatched, alsoUnevaluable := matchingClusterNames(unschedulable, selector.Terms)
		staleSchedulable := selectedBefore(schedulable, selector.Terms, recorded, matched, unevaluable)
		staleUnschedulable := selectedBefore(unschedulable, selector.Terms, recorded, alsoMatched, alsoUnevaluable)
		matched = append(matched, staleSchedulable...)
		slices.Sort(matched)
		sticky := slices.Concat(alsoMatched, unevaluable, alsoUnevaluable, staleUnschedulable)
		slices.Sort(sticky)
		stale := slices.Concat(staleSchedulable, staleUnschedulable)
		slices.Sort(stale)
		unevaluableHere := slices.Concat(unevaluable, alsoUnevaluable)
		slices.Sort(unevaluableHere)
		outcomes = append(outcomes, selectorOutcome{
			counts:          counts,
			matched:         matched,
			sticky:          sticky,
			unevaluable:     unevaluableHere,
			stale:           stale,
			terms:           selector.Terms,
			whenUnfulfilled: selector.WhenUnfulfilled,
		})
	}
	return outcomes, nil
}

// selectedBefore returns the names of the clusters, other than those already accounted for, whose
// binding records the given selector terms, i.e., that the selector picked earlier and that stay
// selected by it whether or not they still match.
func selectedBefore(clusters []*clusterv1beta1.MemberCluster, terms []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm, recorded map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly, accountedFor ...[]string) []string {
	var names []string
	for _, cluster := range clusters {
		if slices.ContainsFunc(accountedFor, func(s []string) bool { return slices.Contains(s, cluster.Name) }) {
			continue
		}
		if slices.ContainsFunc(recorded[cluster.Name], func(s kfplacementv1alpha1.ClusterSelectorWithTermsOnly) bool {
			return apiequality.Semantic.DeepEqual(s.Terms, terms)
		}) {
			names = append(names, cluster.Name)
		}
	}
	return names
}

// matchingClusterNames returns, sorted, the names of the clusters that satisfy the terms, and
// separately the names of those whose data could not be evaluated. validateTerms has vetted the
// selector itself, so an evaluation error stems from malformed runtime data self-reported by
// the cluster (e.g., a property value that is not a quantity); one misbehaving cluster must not
// fail the whole policy, so it is reported apart rather than failing the evaluation.
func matchingClusterNames(clusters []*clusterv1beta1.MemberCluster, terms []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm) (matched, unevaluable []string) {
	for _, cluster := range clusters {
		ok, err := matchesTerms(cluster, terms)
		if err != nil {
			klog.V(2).InfoS("Skipping a member cluster whose reported data cannot be evaluated", "memberCluster", cluster.Name, "error", err)
			unevaluable = append(unevaluable, cluster.Name)
			continue
		}
		if ok {
			matched = append(matched, cluster.Name)
		}
	}
	slices.Sort(matched)
	return matched, unevaluable
}

// aggregateCounts sums the desired and selected cluster counts across all selector outcomes.
// The sums are per selector: a cluster that satisfies multiple selectors of the same policy
// contributes to each of them, in symmetry with how the desired counts add up (see the FEP's
// "About overlapping cluster selectors" note). For a count: All selector the desired count
// floors at the selector's minimum, so that an unfulfilled All selector shows a gap between
// desiredClusters and scheduledClusters instead of trivially reporting desired == scheduled.
func aggregateCounts(outcomes []selectorOutcome) (desired, scheduled int32) {
	for i := range outcomes {
		o := &outcomes[i]
		if o.counts.selectAll {
			desired += max(int32Len(o.matched), o.counts.minimum)
		} else {
			desired += o.counts.desired
		}
		scheduled += int32Len(o.chosen)
	}
	return desired, scheduled
}

// scheduledCondition builds the Scheduled condition for a policy from its selector outcomes,
// honoring the API's binary contract: True only when every selector has found all of its
// desired clusters. Whether the minCount floor has at least been reached everywhere is surfaced
// through the message, not a separate reason or status, as are the member clusters whose reported
// data cannot be evaluated against the selectors.
func scheduledCondition(generation int64, outcomes []selectorOutcome) metav1.Condition {
	cond := scheduledConditionByCounts(generation, outcomes)
	var unevaluable []string
	for i := range outcomes {
		unevaluable = append(unevaluable, outcomes[i].unevaluable...)
	}
	if len(unevaluable) > 0 {
		slices.Sort(unevaluable)
		cond.Message += fmt.Sprintf("; the data reported by member clusters %s cannot be evaluated against the cluster selectors, so they keep any existing binding but are not counted",
			truncatedNameList(slices.Compact(unevaluable)))
	}
	return cond
}

// truncatedNameList formats names for a condition message, listing at most a handful so that a
// fleet-wide problem cannot bloat the policy status.
func truncatedNameList(names []string) string {
	const maxListed = 5
	if len(names) <= maxListed {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:maxListed], ", "), len(names)-maxListed)
}

// scheduledConditionByCounts builds the Scheduled condition from the selectors' cluster counts.
func scheduledConditionByCounts(generation int64, outcomes []selectorOutcome) metav1.Condition {
	allInFull, allFulfilled := true, true
	for i := range outcomes {
		if !outcomes[i].satisfiedInFull() {
			allInFull = false
		}
		if !outcomes[i].fulfilled() {
			allFulfilled = false
		}
	}

	switch {
	case allInFull:
		return metav1.Condition{
			Type:               kfplacementv1alpha1.PlacementPolicyCondTypeScheduled,
			Status:             metav1.ConditionTrue,
			Reason:             kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters,
			Message:            "All cluster selectors have found their desired numbers of clusters",
			ObservedGeneration: generation,
		}
	case allFulfilled:
		return metav1.Condition{
			Type:               kfplacementv1alpha1.PlacementPolicyCondTypeScheduled,
			Status:             metav1.ConditionFalse,
			Reason:             kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters,
			Message:            "All cluster selectors have reached their minimum cluster counts, but some are still short of their desired counts",
			ObservedGeneration: generation,
		}
	default:
		return metav1.Condition{
			Type:               kfplacementv1alpha1.PlacementPolicyCondTypeScheduled,
			Status:             metav1.ConditionFalse,
			Reason:             kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters,
			Message:            "One or more cluster selectors have not found their minimum numbers of clusters",
			ObservedGeneration: generation,
		}
	}
}

// invalidSelectorsCondition builds the Scheduled condition for a policy whose cluster selectors
// cannot be evaluated.
func invalidSelectorsCondition(generation int64, err error) metav1.Condition {
	return metav1.Condition{
		Type:               kfplacementv1alpha1.PlacementPolicyCondTypeScheduled,
		Status:             metav1.ConditionFalse,
		Reason:             reasonInvalidClusterSelectors,
		Message:            "The cluster selectors cannot be evaluated: " + err.Error(),
		ObservedGeneration: generation,
	}
}

// taintsTolerated reports whether every given taint is tolerated by at least one of the given
// tolerations.
func taintsTolerated(taints []clusterv1beta1.Taint, tolerations []kfplacementv1alpha1.Toleration) bool {
	for i := range taints {
		if !taintTolerated(&taints[i], tolerations) {
			return false
		}
	}
	return true
}

func taintTolerated(taint *clusterv1beta1.Taint, tolerations []kfplacementv1alpha1.Toleration) bool {
	for i := range tolerations {
		if tolerationTolerates(taint, &tolerations[i]) {
			return true
		}
	}
	return false
}

// tolerationTolerates mirrors the core Kubernetes toleration semantics: an empty effect matches
// every effect, an empty key with the Exists operator matches every taint, and the Equal
// operator (the default) compares values. It parallels the scheduler's
// tainttoleration/filtering.go implementation, which operates on the v1beta1 Toleration type;
// semantic changes must be kept in sync between the two.
func tolerationTolerates(taint *clusterv1beta1.Taint, toleration *kfplacementv1alpha1.Toleration) bool {
	if toleration.Effect != "" && toleration.Effect != taint.Effect {
		return false
	}
	if toleration.Key == "" {
		return toleration.Operator == corev1.TolerationOpExists
	}
	if toleration.Key != taint.Key {
		return false
	}
	switch toleration.Operator {
	case corev1.TolerationOpExists:
		return true
	case corev1.TolerationOpEqual, "":
		return toleration.Value == taint.Value
	default:
		// The CRD enum admits only Equal and Exists; an unrecognized operator can only come
		// from an object that bypassed admission and tolerates nothing, in consistency with
		// the upstream Toleration.ToleratesTaint behavior.
		return false
	}
}
