/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package topologyspreadconstraints

import (
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// clusterName is the name of a cluster.
type clusterName string

// violationReasons is a list of string explaining why a DoNotSchedule topology spread constraint
// is violated.
type violationReasons []string

// doNotScheduleViolations is map between cluster names and the reasons why these clusters violate
// a DoNotSchedule topology spread constraint.
//
// Note that if the placement in a cluster violates multiple DoNotSchedule topology spread constraints,
// only the first violation is recorded.
type doNotScheduleViolations map[clusterName]violationReasons

// topologySpreadScores is a map between cluster names and their topology spread scores.
type topologySpreadScores map[clusterName]int

type pluginState struct {
	// doNotScheduleConstraints is a list of topology spread constraints with a DoNotSchedule
	// requirement.
	doNotScheduleConstraints []*fleetv1beta1.TopologySpreadConstraint

	// scheduleAnywayConstraints is a list of topology spread constraints with a ScheduleAnyway
	// requirement.
	scheduleAnywayConstraints []*fleetv1beta1.TopologySpreadConstraint

	// violations documents the reasons why a DoNotSchedule topology spread constraint is violated
	// for a specific cluster.
	violations doNotScheduleViolations

	// scores is the topology spread scores for each cluster.
	scores topologySpreadScores
}
