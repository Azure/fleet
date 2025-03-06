/*
Copyright 2025 The KubeFleet Authors.

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

package framework

import (
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
)

// ClusterScore is the scores the scheduler assigns to a cluster.
type ClusterScore struct {
	// TopologySpreadScore determines how much a binding would satisfy the topology spread
	// constraints specified by the user.
	TopologySpreadScore int
	// AffinityScore determines how much a binding would satisfy the affinity terms
	// specified by the user.
	AffinityScore int
	// ObsoletePlacementAffinityScore reflects if there has already been an obsolete binding from
	// the same cluster resource placement associated with the cluster; it value range should
	// be [0, 1], where 1 signals that an obsolete binding is present.
	//
	// Note that this score is for internal usage only; it serves the purpose of implementing
	// a preference for already selected clusters when all the other conditions are the same,
	// so as to minimize interruption between different scheduling runs.
	ObsoletePlacementAffinityScore int
}

// Add adds a ClusterScore to another ClusterScore.
//
// Note that this will panic if either score is nil.
func (s1 *ClusterScore) Add(s2 *ClusterScore) {
	s1.TopologySpreadScore += s2.TopologySpreadScore
	s1.AffinityScore += s2.AffinityScore
	s1.ObsoletePlacementAffinityScore += s2.ObsoletePlacementAffinityScore
}

// Equal returns true if a ClusterScore is equal to another.
func (s1 *ClusterScore) Equal(s2 *ClusterScore) bool {
	switch {
	case s1 == nil && s2 == nil:
		// Both are nils.
		return true
	case s1 == nil || s2 == nil:
		// One is nil and the other is not.
		return false
	default:
		// Both are not nils.
		return s1.TopologySpreadScore == s2.TopologySpreadScore &&
			s1.AffinityScore == s2.AffinityScore &&
			s1.ObsoletePlacementAffinityScore == s2.ObsoletePlacementAffinityScore
	}
}

// Less returns true if a ClusterScore is less than another.
//
// Note that this will panic if either score is nil.
func (s1 *ClusterScore) Less(s2 *ClusterScore) bool {
	if s1.TopologySpreadScore != s2.TopologySpreadScore {
		return s1.TopologySpreadScore < s2.TopologySpreadScore
	}

	if s1.AffinityScore != s2.AffinityScore {
		return s1.AffinityScore < s2.AffinityScore
	}

	return s1.ObsoletePlacementAffinityScore < s2.ObsoletePlacementAffinityScore
}

// ScoredCluster is a cluster with a score.
type ScoredCluster struct {
	Cluster *clusterv1beta1.MemberCluster
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
//
// Note that this will panic if there is a reference to nil scores and/or clusters; caller
// should verify if the list is valid.
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
