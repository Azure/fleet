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
