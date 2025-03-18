/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package topologyspreadconstraints

import (
	"fmt"
	"sort"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

// countByDomain counts the number of scheduled or bound bindings in each domain per a given
// topology key.
func countByDomain(clusters []clusterv1beta1.MemberCluster, state framework.CycleStatePluginReadWriter, topologyKey string) *bindingCounterByDomain {
	// Calculate the number of bindings in each domain.
	//
	// Note that all domains will have their corresponding counts, even if the counts are zero.
	counter := make(map[domainName]count)
	for _, cluster := range clusters {
		val, ok := cluster.Labels[topologyKey]
		if !ok {
			// The cluster under inspection does not have the topology key and thus is
			// not part of the spread.
			continue
		}

		name := domainName(val)
		count, ok := counter[name]
		if !ok {
			// Initialize the count for the domain (even if there is no scheduled or bound
			// binding in the domain).
			counter[name] = 0
		}

		if state.HasScheduledOrBoundBindingFor(cluster.Name) {
			// The cluster under inspection owns a scheduled or bound binding.
			counter[name] = count + 1
		}
	}

	// Prepare the special counts.
	//
	// Perform a sort; the counter only needs to keep the special counts for this plugin
	// to function, namely the smallest count, the second smallest count, and the largest
	// count.

	// Initialize the special counts with a placeholder value.
	var smallest, secondSmallest, largest int = -1, -1, -1
	sorted := make([]int, 0, len(counter))

	for _, c := range counter {
		sorted = append(sorted, int(c))
	}
	sort.Ints(sorted)

	switch {
	case len(sorted) == 0:
		// The counter is not initialized; do nothing.
	case len(sorted) == 1:
		// There is only one count.
		smallest = sorted[0]
		secondSmallest = sorted[0]
		largest = sorted[0]
	case len(sorted) == 2:
		// There are two counts.
		if sorted[0] < sorted[1] {
			smallest = sorted[0]
			secondSmallest = sorted[1]
			largest = sorted[1]
		} else { // counts[0] == counts[1]
			smallest = sorted[0]
			secondSmallest = sorted[0]
			largest = sorted[0]
		}
	default: // len(sorted) >= 3
		smallest = sorted[0]
		secondSmallest = sorted[1]
		largest = sorted[len(sorted)-1]
	}

	return &bindingCounterByDomain{
		counter:        counter,
		smallest:       count(smallest),
		secondSmallest: count(secondSmallest),
		largest:        count(largest),
	}
}

// willViolate returns whether producing one more binding in a domain would lead
// to violations; it will also return the skew change (the delta between the max skew after setting
// up a placement and the one before) caused by the provisional placement.
func willViolate(counter *bindingCounterByDomain, name domainName, maxSkew int) (violated bool, skewChange int32, err error) {
	count, ok := counter.Count(name)
	if !ok {
		// The domain is not registered in the counter; normally this would never
		// happen as the state being evaluated is consistent and the counter tracks
		// all domains.
		return false, 0, fmt.Errorf("domain %s is not registered in the counter", name)
	}

	// Note if the earlier check passes, all special counts must be present.
	smallest, _ := counter.Smallest()
	secondSmallest, _ := counter.SecondSmallest()
	largest, _ := counter.Largest()

	if count < smallest || count > largest {
		// Perform a sanity check here; normally this would never happen as the counter
		// tracks all domains.
		return false, 0, fmt.Errorf("the counter has invalid special counts: [%d, %d], received %d", smallest, largest, count)
	}

	currentSkew := int(largest - smallest)
	switch {
	case largest == smallest:
		// Currently all domains have the same count of bindings.
		//
		// In this case, the placement will increase the skew by 1.
		return currentSkew+1 > maxSkew, 1, nil
	case count == smallest && smallest != secondSmallest:
		// The plan is to place at the domain with the smallest count of bindings, and currently
		// there are no other domains with the same smallest count.
		//
		// In this case, the placement will decrease the skew by 1.
		return currentSkew-1 > maxSkew, -1, nil
	case count == largest:
		// The plan is to place at the domain with the largest count of bindings.
		//
		// In this case, the placement will increase the skew by 1.
		return currentSkew+1 > maxSkew, 1, nil
	default:
		// In all the other cases, the skew will not be affected.
		//
		// These cases include:
		//
		// * place at the domain with the smallest count of bindings, but there are other
		//   domains with the same smallest count; and
		// * place at the domain with a count of bindings that is greater than the smallest
		//   count, but less than the largest count.
		return currentSkew > maxSkew, 0, nil
	}
}

// classifyConstraints classifies topology spread constraints in a policy based on their
// whenUnsatisfiable requirements.
func classifyConstraints(policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (doNotSchedule, scheduleAnyway []*placementv1beta1.TopologySpreadConstraint) {
	// Pre-allocate arrays.
	doNotSchedule = make([]*placementv1beta1.TopologySpreadConstraint, 0, len(policy.Spec.Policy.TopologySpreadConstraints))
	scheduleAnyway = make([]*placementv1beta1.TopologySpreadConstraint, 0, len(policy.Spec.Policy.TopologySpreadConstraints))

	for idx := range policy.Spec.Policy.TopologySpreadConstraints {
		constraint := policy.Spec.Policy.TopologySpreadConstraints[idx]
		if constraint.WhenUnsatisfiable == placementv1beta1.ScheduleAnyway {
			scheduleAnyway = append(scheduleAnyway, &constraint)
			continue
		}

		// DoNotSchedule is the default value for the whenUnsatisfiable field; currently the only
		// two supported requirements are DoNotSchedule and ScheduleAnyway.
		doNotSchedule = append(doNotSchedule, &constraint)
	}

	return doNotSchedule, scheduleAnyway
}

// evaluateAllConstraints evaluates all topology spread constraints in a policy againsts all
// clusters being inspected in the current scheduling cycle.
//
// Note that every cluster that does not lead to violations will be assigned a score, even if
// the cluster does not concern any of the topology spread constraints.
func evaluateAllConstraints(
	state framework.CycleStatePluginReadWriter,
	doNotSchedule, scheduleAnyway []*placementv1beta1.TopologySpreadConstraint,
) (violations doNotScheduleViolations, scores topologySpreadScores, err error) {
	violations = make(doNotScheduleViolations)
	// Note that this function guarantees that all clusters that do not lead to violations of
	// DoNotSchedule topology spread constraints will be assigned a score, even if none of the
	// topology spread constraints concerns these clusters.
	scores = make(topologySpreadScores)

	clusters := state.ListClusters()

	for _, constraint := range doNotSchedule {
		domainCounter := countByDomain(clusters, state, constraint.TopologyKey)

		for _, cluster := range clusters {
			if _, ok := violations[clusterName(cluster.Name)]; ok {
				// The cluster has violated a DoNotSchedule topology spread constraint; no need
				// evaluate other constraints for this cluster.
				continue
			}

			val, ok := cluster.Labels[constraint.TopologyKey]
			if !ok {
				// The cluster under inspection does not have the topology key and thus is not part
				// of the spread.
				//
				// Placing resources on such clusters will not lead to topology spread constraint
				// violations.
				//
				// Assign a score even if the constraint does not concern the cluster.
				scores[clusterName(cluster.Name)] += 0
				continue
			}

			// The cluster under inspection is part of the spread.

			// Verify if the placement will violate the constraint.

			// The default value for maxSkew is 1.
			maxSkew := 1
			if constraint.MaxSkew != nil {
				maxSkew = int(*constraint.MaxSkew)
			}
			violated, skewChange, err := willViolate(domainCounter, domainName(val), maxSkew)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to evaluate DoNotSchedule topology spread constraints: %w", err)
			}
			if violated {
				// A violation happens.
				reasons := violationReasons{
					fmt.Sprintf(doNotScheduleConstraintViolationReasonTemplate, constraint.TopologyKey, *constraint.MaxSkew),
				}
				violations[clusterName(cluster.Name)] = reasons

				// Untrack the cluster's score.
				delete(scores, clusterName(cluster.Name))

				continue
			}
			scores[clusterName(cluster.Name)] += skewChange * int32(skewChangeScoreFactor)
		}
	}

	for _, constraint := range scheduleAnyway {
		domainCounter := countByDomain(clusters, state, constraint.TopologyKey)

		for _, cluster := range clusters {
			if _, ok := violations[clusterName(cluster.Name)]; ok {
				// The cluster has violated a DoNotSchedule topology spread constraint; no need
				// evaluate other constraints for this cluster.
				continue
			}

			val, ok := cluster.Labels[constraint.TopologyKey]
			if !ok {
				// The cluster under inspection does not have the topology key and thus is not part
				// of the spread.
				//
				// Placing resources on such clusters will not lead to topology spread constraint
				// violations.
				//
				// Assign a score even if the constraint does not concern the cluster.
				scores[clusterName(cluster.Name)] += 0
				continue
			}

			// The cluster under inspection is part of the spread.

			// Verify if the placement will violate the constraint.

			// The default value for maxSkew is 1.
			maxSkew := 1
			if constraint.MaxSkew != nil {
				maxSkew = int(*constraint.MaxSkew)
			}
			violated, skewChange, err := willViolate(domainCounter, domainName(val), maxSkew)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to evaluate ScheduleAnyway topology spread constraints: %w", err)
			}
			if violated {
				// A violation happens; since this is a ScheduleAnyway topology spread constraint,
				// a violation score penalty is applied to the score.
				scores[clusterName(cluster.Name)] -= maxSkewViolationPenality
				continue
			}
			scores[clusterName(cluster.Name)] += skewChange * int32(skewChangeScoreFactor)
		}
	}

	return violations, scores, nil
}

// prepareTopologySpreadConstraintsPluginState initializes the state for the plugin to use
// in the scheduling cycle.
func prepareTopologySpreadConstraintsPluginState(state framework.CycleStatePluginReadWriter, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) (*pluginState, error) {
	// Classify the topology spread constraints.
	doNotSchedule, scheduleAnyway := classifyConstraints(policy)

	// Based on current spread, inspect the clusters.
	//
	// Specifically, check if a cluster violates any DoNotSchedule topology spread constraint,
	// and how much of a skew change it will incur for each constraint.
	violations, scores, err := evaluateAllConstraints(state, doNotSchedule, scheduleAnyway)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare topology spread constraints plugin state: %w", err)
	}

	pluginState := &pluginState{
		doNotScheduleConstraints:  doNotSchedule,
		scheduleAnywayConstraints: scheduleAnyway,
		violations:                violations,
		scores:                    scores,
	}
	return pluginState, nil
}
