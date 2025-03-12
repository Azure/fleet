/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package after

import (
	. "github.com/onsi/gomega"

	"go.goms.io/fleet/test/e2e/framework"
)

// checkIfPlacedWorkResourcesOnAllMemberClustersConsistently verifies if the work resources have been placed on
// all applicable member clusters.
func checkIfPlacedWorkResourcesOnAllMemberClustersConsistently(workNamespaceName, appConfigMapName string) {
	checkIfPlacedWorkResourcesOnMemberClustersConsistently(allMemberClusters, workNamespaceName, appConfigMapName)
}

// checkIfPlacedWorkResourcesOnMemberClustersConsistently verifies if the work resources have been placed on
// the specified set of member clusters.
func checkIfPlacedWorkResourcesOnMemberClustersConsistently(clusters []*framework.Cluster, workNamespaceName, appConfigMapName string) {
	for idx := range clusters {
		memberCluster := clusters[idx]
		workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster, workNamespaceName, appConfigMapName)
		// Give the system a bit more breathing room when process resource placement.
		Consistently(workResourcesPlacedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
	}
}
