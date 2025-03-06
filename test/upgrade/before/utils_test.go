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

package before

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	imcv1beta1 "go.goms.io/fleet/pkg/controllers/internalmembercluster/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/test/e2e/framework"
)

// createMemberCluster creates a MemberCluster object.
func createMemberCluster(name, svcAccountName string, labels, annotations map[string]string) {
	mcObj := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Name:      svcAccountName,
				Kind:      "ServiceAccount",
				Namespace: fleetSystemNS,
			},
			HeartbeatPeriodSeconds: 60,
		},
	}
	Expect(hubClient.Create(ctx, mcObj)).To(Succeed(), "Failed to create member cluster object %s", name)
}

// setAllMemberClustersToJoin creates a MemberCluster object for each member cluster.
func setAllMemberClustersToJoin() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]
		createMemberCluster(memberCluster.ClusterName, memberCluster.PresentingServiceAccountInHubClusterName, nil, nil)
	}
}

// checkIfMemberClusterHasJoined verifies if the specified member cluster has connected to the hub
// cluster, i.e., updated the MemberCluster object status as expected.
func checkIfMemberClusterHasJoined(memberCluster *framework.Cluster) {
	wantAgentStatus := []clusterv1beta1.AgentStatus{
		{
			Type: clusterv1beta1.MemberAgent,
			Conditions: []metav1.Condition{
				{
					Status: metav1.ConditionTrue,
					Type:   string(clusterv1beta1.AgentHealthy),
					Reason: imcv1beta1.EventReasonInternalMemberClusterHealthy,
				},
				{
					Status: metav1.ConditionTrue,
					Type:   string(clusterv1beta1.AgentJoined),
					Reason: imcv1beta1.EventReasonInternalMemberClusterJoined,
				},
			},
		},
	}

	Eventually(func() error {
		mcObj := &clusterv1beta1.MemberCluster{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: memberCluster.ClusterName}, mcObj); err != nil {
			By(fmt.Sprintf("Failed to get member cluster object %s", memberCluster.ClusterName))
			return err
		}

		if diff := cmp.Diff(
			mcObj.Status.AgentStatus,
			wantAgentStatus,
			cmpopts.SortSlices(lessFuncConditionByType),
			ignoreConditionObservedGenerationField,
			utils.IgnoreConditionLTTAndMessageFields,
			ignoreAgentStatusHeartbeatField,
		); diff != "" {
			return fmt.Errorf("agent status diff (-got, +want): %s", diff)
		}

		return nil
	}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Member cluster has not joined yet")
}

// checkIfAllMemberClustersHaveJoined verifies if all member clusters have connected to the hub
// cluster, i.e., updated the MemberCluster object status as expected.
func checkIfAllMemberClustersHaveJoined() {
	for idx := range allMemberClusters {
		checkIfMemberClusterHasJoined(allMemberClusters[idx])
	}
}

// createWorkResources creates some resources on the hub cluster for testing purposes.
func createWorkResources(workNamespaceName, appConfigMapName, crpName string) {
	ns := appNamespace(workNamespaceName, crpName)
	Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Namespace)

	configMap := appConfigMap(workNamespaceName, appConfigMapName)
	Expect(hubClient.Create(ctx, &configMap)).To(Succeed(), "Failed to create config map %s", configMap.Name)
}

// checkIfPlacedWorkResourcesOnAllMemberClusters verifies if the work resources have been placed on
// all applicable member clusters.
func checkIfPlacedWorkResourcesOnAllMemberClusters(workNamespaceName, appConfigMapName string) {
	checkIfPlacedWorkResourcesOnMemberClusters(allMemberClusters, workNamespaceName, appConfigMapName)
}

// checkIfPlacedWorkResourcesOnMemberClusters verifies if the work resources have been placed on
// the specified set of member clusters.
func checkIfPlacedWorkResourcesOnMemberClusters(clusters []*framework.Cluster, workNamespaceName, appConfigMapName string) {
	for idx := range clusters {
		memberCluster := clusters[idx]
		workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster, workNamespaceName, appConfigMapName)
		// Give the system a bit more breathing room when process resource placement.
		Eventually(workResourcesPlacedActual, eventuallyDuration*2, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
	}
}
