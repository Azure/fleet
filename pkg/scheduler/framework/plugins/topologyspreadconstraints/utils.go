/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package topologyspreadconstraints

import (
	"fmt"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

// countByDomain counts the number of scheduled or bound bindings in each domain per a given
// topology key.
func countByDomain(_ []fleetv1beta1.MemberCluster, _ framework.CycleStatePluginReadWriter, _ string) bindingCounter {
	// Not yet implemented.
	return &bindingCounterByDomain{}
}

// willViolate returns whether producing one more binding in a domain would lead
// to violations; it will also return the skew change (the delta between the max skew after setting
// up a placement and the one before) caused by the provisional placement.
func willViolate(_ bindingCounter, _ domainName, _ int) (violated bool, skewChange int, err error) {
	// Not yet implemented.
	return false, 0, nil
}

// classifyConstraints classifies topology spread constraints in a policy based on their
// whenUnsatisfiable requirements.
func classifyConstraints(policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) (doNotSchedule, scheduleAnyway []*fleetv1beta1.TopologySpreadConstraint) {
	// Pre-allocate arrays.
	doNotSchedule = make([]*fleetv1beta1.TopologySpreadConstraint, 0, len(policy.Spec.Policy.TopologySpreadConstraints))
	scheduleAnyway = make([]*fleetv1beta1.TopologySpreadConstraint, 0, len(policy.Spec.Policy.TopologySpreadConstraints))

	for idx := range policy.Spec.Policy.TopologySpreadConstraints {
		constraint := policy.Spec.Policy.TopologySpreadConstraints[idx]
		if constraint.WhenUnsatisfiable == fleetv1beta1.ScheduleAnyway {
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
	doNotSchedule, scheduleAnyway []*fleetv1beta1.TopologySpreadConstraint,
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
				continue
			}
			scores[clusterName(cluster.Name)] += skewChange * skewChangeScoreFactor
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
				// a violation score penality is applied to the score.
				scores[clusterName(cluster.Name)] -= maxSkewViolationPenality
				continue
			}
			scores[clusterName(cluster.Name)] += skewChange * skewChangeScoreFactor
		}
	}

	return violations, scores, nil
}

// prepareTopologySpreadConstraintsPluginState initializes the state for the plugin to use
// in the scheduling cycle.
func prepareTopologySpreadConstraintsPluginState(state framework.CycleStatePluginReadWriter, policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) (*pluginState, error) {
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
