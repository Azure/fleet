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

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/test/e2e/framework"
	testutilseviction "go.goms.io/fleet/test/utils/eviction"
)

var _ = Describe("ClusterResourcePlacement eviction of bound binding, taint cluster before eviction - No PDB specified", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())
	taintClusterNames := []string{memberCluster1EastProdName}

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		// Remove taint from member cluster 1.
		removeTaintsFromMemberClusters(taintClusterNames)
		ensureCRPEvictionDeletion(crpEvictionName)
		ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("add taint to member cluster 1", func() {
		addTaintsToMemberClusters(taintClusterNames, buildTaints(taintClusterNames))
	})

	It("create cluster resource placement eviction targeting member cluster 1", func() {
		crpe := &placementv1beta1.ClusterResourcePlacementEviction{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpEvictionName,
			},
			Spec: placementv1beta1.PlacementEvictionSpec{
				PlacementName: crpName,
				ClusterName:   taintClusterNames[0],
			},
		}
		Expect(hubClient.Create(ctx, crpe)).To(Succeed(), "Failed to create CRP eviction %s", crpe.Name)
	})

	It("should update cluster resource placement eviction status as expected", func() {
		crpEvictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
			ctx, hubClient, crpEvictionName,
			&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
			&testutilseviction.IsExecutedEviction{IsExecuted: true, Msg: condition.EvictionAllowedNoPDBMessage})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure no resources exist on evicted member cluster with taint", func() {
		unSelectedClusters := []*framework.Cluster{memberCluster1EastProd}
		for _, cluster := range unSelectedClusters {
			resourceRemovedActual := workNamespaceRemovedFromClusterActual(cluster)
			Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to check if resources doesn't exist on member cluster")
		}
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster2EastCanaryName, memberCluster3WestProdName}, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the selected clusters with no taint", func() {
		targetClusters := []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}
		for _, cluster := range targetClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the selected clusters")
		}
	})
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding, no taint specified, evicted cluster is picked again by scheduler - No PDB specified", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		ensureCRPEvictionDeletion(crpEvictionName)
		ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement eviction targeting member cluster 1", func() {
		crpe := &placementv1beta1.ClusterResourcePlacementEviction{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpEvictionName,
			},
			Spec: placementv1beta1.PlacementEvictionSpec{
				PlacementName: crpName,
				ClusterName:   memberCluster1EastProdName,
			},
		}
		Expect(hubClient.Create(ctx, crpe)).To(Succeed(), "Failed to create CRP eviction %s", crpe.Name)
	})

	It("should update cluster resource placement eviction status as expected", func() {
		crpEvictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
			ctx, hubClient, crpEvictionName,
			&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
			&testutilseviction.IsExecutedEviction{IsExecuted: true, Msg: condition.EvictionAllowedNoPDBMessage})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure evicted cluster is picked again by scheduler & update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})
