/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package topologyspreadconstraints

import (
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// domainName is the name of a topology domain.
type domainName string

// count is the number of bindings in a topology domain.
type count int

// bindingCounterByDomain implements the bindingCounter interface, counting the number of
// bindings in each topology domain.
type bindingCounterByDomain struct {
	// counter tracks the number of bindings in each domain.
	counter map[domainName]count

	// smallest is the smallest count in the counter.
	smallest count
	// secondSmallest is the second smallest count in the counter.
	//
	// Note that the second smallest count might be the same as the smallest count.
	secondSmallest count
	// largest is the largest count in the counter.
	//
	// Note that the largest count might be the same as the second smallest (and consequently
	// the smallest) count.
	largest count
}

// Count returns the number of bindings in a topology domain.
func (c *bindingCounterByDomain) Count(name domainName) (count, bool) {
	if len(c.counter) == 0 {
		return 0, false
	}

	val, ok := c.counter[name]
	return val, ok
}

// Smallest returns the smallest count in the counter.
func (c *bindingCounterByDomain) Smallest() (count, bool) {
	if len(c.counter) == 0 {
		return 0, false
	}

	return c.smallest, true
}

// SecondSmallest returns the second smallest count in the counter.
func (c *bindingCounterByDomain) SecondSmallest() (count, bool) {
	if len(c.counter) == 0 {
		return 0, false
	}

	return c.secondSmallest, true
}

// Largest returns the largest count in the counter.
func (c *bindingCounterByDomain) Largest() (count, bool) {
	if len(c.counter) == 0 {
		return 0, false
	}

	return c.largest, true
}

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
