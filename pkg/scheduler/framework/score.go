/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// ClusterScore is the scores the scheduler assigns to a cluster.
type ClusterScore struct {
	// TopologySpreadScore determines how much a binding would satisfy the topology spread
	// constraints specified by the user.
	TopologySpreadScore int
	// AffinityScore determines how much a binding would satisfy the affinity terms
	// specified by the user.
	AffinityScore int
	// ActiveOrCreatingBindingScore reflects if there has already been an active/creating binding from
	// the same cluster resource placement associated with the cluster; it value range should
	// be [0, 1], where 1 signals that an active/creating binding is present.
	//
	// Note that this score is for internal usage only; it serves the purpose of implementing
	// a preference for already selected clusters when all the other conditions are the same,
	// so as to minimize interruption between different scheduling runs.
	ActiveOrCreatingBindingScore int
}

// Add adds a ClusterScore to another ClusterScore.
func (s1 *ClusterScore) Add(s2 *ClusterScore) {
	s1.TopologySpreadScore += s2.TopologySpreadScore
	s1.AffinityScore += s2.AffinityScore
	s1.ActiveOrCreatingBindingScore += s2.ActiveOrCreatingBindingScore
}

// Equal returns true if a ClusterScore is equal to another.
func (s1 *ClusterScore) Equal(s2 *ClusterScore) bool {
	return s1.TopologySpreadScore == s2.TopologySpreadScore &&
		s1.AffinityScore == s2.AffinityScore &&
		s1.ActiveOrCreatingBindingScore == s2.ActiveOrCreatingBindingScore
}

// Less returns true if a ClusterScore is less than another.
func (s1 *ClusterScore) Less(s2 *ClusterScore) bool {
	if s1.TopologySpreadScore != s2.TopologySpreadScore {
		return s1.TopologySpreadScore < s2.TopologySpreadScore
	}

	if s1.AffinityScore != s2.AffinityScore {
		return s1.AffinityScore < s2.AffinityScore
	}

	return s1.ActiveOrCreatingBindingScore < s2.ActiveOrCreatingBindingScore
}

// ScoredCluster is a cluster with a score.
type ScoredCluster struct {
	Cluster *fleetv1beta1.MemberCluster
	Score   *ClusterScore
}

// ScoredClusters is a list of ScoredClusters; this type implements the sort.Interface.
type ScoredClusters []*ScoredCluster

// Len returns the length of a ScoredClusters; it implemented sort.Interface.Len().
func (sc ScoredClusters) Len() int { return len(sc) }

// Less returns true if a ScoredCluster is of a lower score than another; when two clusters have
// the same score, the one with a name that is lexicographically smaller is considered to be the
// smaller one.
//
// It implemented sort.Interface.Less().
func (sc ScoredClusters) Less(i, j int) bool {
	if sc[i].Score.Equal(sc[j].Score) {
		return sc[i].Cluster.Name < sc[j].Cluster.Name
	}

	return sc[i].Score.Less(sc[j].Score)
}

// Swap swaps two ScoredClusters in the list; it implemented sort.Interface.Swap().
func (sc ScoredClusters) Swap(i, j int) {
	sc[i], sc[j] = sc[j], sc[i]
}
