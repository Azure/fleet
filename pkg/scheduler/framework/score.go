/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	fleetv1 "go.goms.io/fleet/apis/v1"
)

// ClusterScore is the scores the scheduler assigns to a cluster.
type ClusterScore struct {
	// TopologySpreadScore determines how much a binding would satisfy the topology spread
	// constraints specified by the user.
	TopologySpreadScore int
	// AffinityScore determines how much a binding would satisfy the affinity terms
	// specified by the user.
	AffinityScore int
	// PriorityScore determines how much a binding would satisfy the priority terms
	// specified by the user.
	PriorityScore int
}

// Add adds a ClusterScore to another ClusterScore.
func (s1 *ClusterScore) Add(s2 ClusterScore) {
	s1.TopologySpreadScore += s2.TopologySpreadScore
	s1.AffinityScore += s2.AffinityScore
	s1.PriorityScore += s2.PriorityScore
}

// Less returns true if a ClusterScore is less than another.
func (s1 *ClusterScore) Less(s2 *ClusterScore) bool {
	if s1.TopologySpreadScore != s2.TopologySpreadScore {
		return s1.TopologySpreadScore < s2.TopologySpreadScore
	}

	if s1.AffinityScore != s2.AffinityScore {
		return s1.AffinityScore < s2.AffinityScore
	}

	return s1.PriorityScore < s2.PriorityScore
}

// ScoredCluster is a cluster with a score.
type ScoredCluster struct {
	Cluster *fleetv1.MemberCluster
	Score   *ClusterScore
}

// ScoredClusters is a list of ScoredClusters; this type implements the sort.Interface.
type ScoredClusters []*ScoredCluster

// Len returns the length of a ScoredClusters; it implemented sort.Interface.Len().
func (sc ScoredClusters) Len() int { return len(sc) }

// Less returns true if a ScoredCluster is of a lower score than another; it implemented sort.Interface.Less().
func (sc ScoredClusters) Less(i, j int) bool {
	return sc[i].Score.Less(sc[j].Score)
}

// Swap swaps two ScoredClusters in the list; it implemented sort.Interface.Swap().
func (sc ScoredClusters) Swap(i, j int) {
	sc[i], sc[j] = sc[j], sc[i]
}
