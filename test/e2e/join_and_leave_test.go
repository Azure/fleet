/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// Note that this container will run in parallel with other containers.
var _ = Describe("placing wrapped resources using a CRP", Ordered, Serial, func() {
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
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			// resourceQuota is enveloped so it's not trackable yet
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

		It("should update CRP status as expected", func() {
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
	})

	AfterAll(func() {
		// Remove the custom deletion blocker finalizer from the CRP.
		cleanupCRP(crpName)

		// Delete the created resources.
		cleanupWorkResources()
	})
})
