/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
)

// Note that this container cannot run in parallel with other containers.
var _ = Describe("Test member cluster join and leave flow", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	var wantSelectedResources []placementv1beta1.ResourceIdentifier
	BeforeAll(func() {
		// Create the test resources.
		readEnvelopTestManifests()
		wantSelectedResources = []placementv1beta1.ResourceIdentifier{
			{
				Kind:    "Namespace",
				Name:    workNamespaceName,
				Version: "v1",
			},
			{
				Kind:      "ConfigMap",
				Name:      testConfigMap.Name,
				Version:   "v1",
				Namespace: workNamespaceName,
			},
			{
				Kind:      "ConfigMap",
				Name:      testEnvelopConfigMap.Name,
				Version:   "v1",
				Namespace: workNamespaceName,
			},
		}
	})

	Context("Test cluster join and leave flow with CRP not deleted", Ordered, Serial, func() {
		It("Create the test resources in the namespace", createWrappedResourcesForEnvelopTest)

		It("Create the CRP that select the name space and place it to all clusters", func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
						ApplyStrategy: &placementv1beta1.ApplyStrategy{AllowCoOwnership: true},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			// resourceQuota is not trackable yet
			crpStatusUpdatedActual := customizedCRPStatusUpdatedActual(crpName, wantSelectedResources, allMemberClusterNames, nil, "0", false)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on all member clusters", func() {
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				workResourcesPlacedActual := checkEnvelopQuotaAndMutationWebhookPlacement(memberCluster)
				Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
			}
		})

		It("Should be able to unjoin a cluster with crp still running", func() {
			By("remove all the clusters without deleting the CRP")
			setAllMemberClustersToLeave()
			checkIfAllMemberClustersHaveLeft()
		})

		It("should update CRP status to not placing any resources since all clusters are left", func() {
			// resourceQuota is enveloped so it's not trackable yet
			crpStatusUpdatedActual := customizedCRPStatusUpdatedActual(crpName, wantSelectedResources, nil, nil, "0", false)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("Should be able to rejoin the cluster", func() {
			By("rejoin all the clusters without deleting the CRP")
			setAllMemberClustersToJoin()
			checkIfAllMemberClustersHaveJoined()
			checkIfAzurePropertyProviderIsWorking()
		})

		It("should update CRP status to applied to all clusters again automatically after rejoining", func() {
			crpStatusUpdatedActual := customizedCRPStatusUpdatedActual(crpName, wantSelectedResources, allMemberClusterNames, nil, "0", false)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
	})
})

var _ = Describe("Test member cluster force delete flow", Ordered, Serial, func() {
	Context("Test cluster join and leave flow with member agent down and force delete member cluster", Ordered, Serial, func() {
		It("Simulate the member agent going down in member cluster", func() {
			Eventually(func() error {
				return updateMemberAgentDeploymentReplicas(0)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to simulate member agent going down")
		})

		It("Delete member cluster CR associated to the member cluster with member agent down", func() {
			var mc clusterv1beta1.MemberCluster
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: memberCluster3WestProdName}, &mc)).To(Succeed(), "Failed to get member cluster")
			Expect(hubClient.Delete(ctx, &mc)).Should(Succeed())
		})

		It("Should garbage collect resources owned by member cluster and force delete member cluster CR after force delete wait time", func() {
			memberClusterNamespace := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster3WestProdName)
			Eventually(func() bool {
				var ns corev1.Namespace
				return apierrors.IsNotFound(hubClient.Get(ctx, types.NamespacedName{Name: memberClusterNamespace}, &ns))
			}, time.Minute*3, eventuallyInterval).Should(BeTrue(), "Failed to garbage collect resources owned by member cluster")

			Eventually(func() bool {
				return apierrors.IsNotFound(hubClient.Get(ctx, types.NamespacedName{Name: memberCluster3WestProdName}, &clusterv1beta1.MemberCluster{}))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to force delete member cluster")
		})
	})

	AfterAll(func() {
		By("Simulate the member agent coming back up")
		Eventually(func() error {
			return updateMemberAgentDeploymentReplicas(1)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to simulate member agent coming back up")

		createMemberCluster(memberCluster3WestProd.ClusterName, memberCluster3WestProd.PresentingServiceAccountInHubClusterName, labelsByClusterName[memberCluster3WestProd.ClusterName], annotationsByClusterName[memberCluster3WestProd.ClusterName])
		checkIfMemberClusterHasJoined(memberCluster3WestProd)
	})
})

func updateMemberAgentDeploymentReplicas(replicas int32) error {
	var d appsv1.Deployment
	err := memberCluster3WestProdClient.Get(ctx, types.NamespacedName{Name: "member-agent", Namespace: fleetSystemNS}, &d)
	if err != nil {
		return err
	}
	d.Spec.Replicas = ptr.To(replicas)
	return memberCluster3WestProdClient.Update(ctx, &d)
}
