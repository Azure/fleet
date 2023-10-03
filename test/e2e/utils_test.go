/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"fmt"
	testutils "go.goms.io/fleet/test/e2e/v1alpha1/utils"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// setAllMemberClustersToJoin creates a MemberCluster object for each member cluster.
func setAllMemberClustersToJoin() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		mcObj := &clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: memberCluster.ClusterName,
			},
			Spec: clusterv1beta1.MemberClusterSpec{
				Identity: rbacv1.Subject{
					Name:      hubClusterSAName,
					Kind:      "ServiceAccount",
					Namespace: fleetSystemNS,
				},
			},
		}
		Expect(hubClient.Create(ctx, mcObj)).To(Succeed(), "Failed to create member cluster object")
	}
}

// checkIfAllMemberClustersHaveJoined verifies if all member clusters have connected to the hub
// cluster, i.e., updated the MemberCluster object status as expected.
func checkIfAllMemberClustersHaveJoined() {
	wantAgentStatus := []clusterv1beta1.AgentStatus{
		{
			Type: clusterv1beta1.MemberAgent,
			Conditions: []metav1.Condition{
				{
					Status: metav1.ConditionTrue,
					Type:   string(clusterv1beta1.AgentHealthy),
				},
				{
					Status: metav1.ConditionTrue,
					Type:   string(clusterv1beta1.AgentJoined),
				},
			},
		},
	}

	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		Eventually(func() error {
			mcObj := &clusterv1beta1.MemberCluster{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: memberCluster.ClusterName}, mcObj); err != nil {
				return err
			}

			if diff := cmp.Diff(
				mcObj.Status.AgentStatus,
				wantAgentStatus,
				cmpopts.SortSlices(lessFuncCondition),
				ignoreConditionObservedGenerationField,
				ignoreConditionLTTReasonAndMessageFields,
				ignoreAgentStatusHeartbeatField,
			); diff != "" {
				return fmt.Errorf("agent status diff (-got, +want): %s", diff)
			}

			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Member cluster has not joined yet")
	}
}

// createWorkResources creates some resources on the hub cluster for testing purposes.
func createWorkResources() {
	ns := workNamespace()
	Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create namespace %s", ns.Namespace)

	configMap := appConfigMap()
	Expect(hubClient.Create(ctx, &configMap)).To(Succeed(), "Failed to create config map %s", configMap.Name)
}

// cleanupWorkResources deletes the resources created by createWorkResources and waits until the resources are not found.
func cleanupWorkResources() {
	ns := workNamespace()
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, &ns))).To(Succeed(), "Failed to delete namespace %s", ns.Namespace)

	workResourcesRemovedActual := workNamespaceRemovedFromClusterActual(hubCluster)
	Eventually(workResourcesRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work resources from hub cluster")
}

// setAllMemberClustersToLeave sets all member clusters to leave the fleet.
func setAllMemberClustersToLeave() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		mcObj := &clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: memberCluster.ClusterName,
			},
		}
		Expect(client.IgnoreNotFound(hubClient.Delete(ctx, mcObj))).To(Succeed(), "Failed to set member cluster to leave state")
	}
}

func checkIfAllMemberClustersHaveLeft() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		Eventually(func() error {
			mcObj := &clusterv1beta1.MemberCluster{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: memberCluster.ClusterName}, mcObj); !errors.IsNotFound(err) {
				return fmt.Errorf("member cluster still exists or an unexpected error occurred: %w", err)
			}

			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete member cluster")
	}
}

func checkIfPlacedWorkResourcesOnAllMemberClusters() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
		Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
	}
}

func checkIfRemovedWorkResourcesFromAllMemberClusters() {
	for idx := range allMemberClusters {
		memberCluster := allMemberClusters[idx]

		workResourcesRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster)
		Eventually(workResourcesRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work resources from member cluster %s", memberCluster.ClusterName)
	}
}

// cleanupCRP deletes the CRP and waits until the resources are not found.
func cleanupCRP(name string) {
	Eventually(func(g Gomega) error {
		crp := &placementv1beta1.ClusterResourcePlacement{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: name}, crp)).To(Succeed(), "Failed to get CRP %s", name)

		// Delete the CRP (again, if applicable).
		//
		// This helps the AfterAll node to run successfully even if the steps above fail early.
		Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", name)

		crp.Finalizers = []string{}
		err := hubClient.Update(ctx, crp)
		return err
	}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())

	// Wait until the CRP is removed.
	removedActual := crpRemovedActual()
	Eventually(removedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove CRP %s", name)
}
