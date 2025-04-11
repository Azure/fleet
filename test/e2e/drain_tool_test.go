/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"fmt"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = Describe("Drain cluster successfully", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	var drainClusters, noDrainClusters []*framework.Cluster
	var noDrainClusterNames []string

	BeforeAll(func() {
		drainClusters = []*framework.Cluster{memberCluster1EastProd}
		noDrainClusters = []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}
		noDrainClusterNames = []string{memberCluster2EastCanaryName, memberCluster3WestProdName}

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
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		// Uncordon member cluster 1 again to guarantee clean up of cordon taint on test failure.
		runUncordonClusterBinary(hubClusterName, memberCluster1EastProdName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("drain cluster using binary, should succeed", func() { runDrainClusterBinary(hubClusterName, memberCluster1EastProdName) })

	It("should ensure no resources exist on drained clusters", func() {
		for _, cluster := range drainClusters {
			resourceRemovedActual := workNamespaceRemovedFromClusterActual(cluster)
			Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to check if resources doesn't exist on member cluster")
		}
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), noDrainClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the selected clusters with no taint", func() {
		for _, cluster := range noDrainClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the selected clusters")
		}
	})

	It("uncordon cluster using binary", func() { runUncordonClusterBinary(hubClusterName, memberCluster1EastProdName) })

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})

var _ = Describe("Drain cluster blocked - ClusterResourcePlacementDisruptionBudget blocks evictions on all clusters", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

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
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		// Uncordon member cluster 1 again to guarantee clean up of cordon taint on test failure.
		runUncordonClusterBinary(hubClusterName, memberCluster1EastProdName)

		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block draining", func() {
		crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
				MinAvailable: &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: int32(len(allMemberClusterNames)),
				},
			},
		}
		Expect(hubClient.Create(ctx, &crpdb)).To(Succeed(), "Failed to create CRP Disruption Budget %s", crpName)
	})

	It("drain cluster using binary, should fail due to CRPDB", func() { runDrainClusterBinary(hubClusterName, memberCluster1EastProdName) })

	It("should ensure cluster resource placement status remains unchanged", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("uncordon cluster using binary", func() { runUncordonClusterBinary(hubClusterName, memberCluster1EastProdName) })

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})

var _ = Describe("Drain is allowed on one cluster, blocked on others - ClusterResourcePlacementDisruptionBudget blocks evictions on some clusters", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	var drainClusters, noDrainClusters []*framework.Cluster
	var noDrainClusterNames []string

	BeforeAll(func() {
		drainClusters = []*framework.Cluster{memberCluster1EastProd}
		noDrainClusters = []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}
		noDrainClusterNames = []string{memberCluster2EastCanaryName, memberCluster3WestProdName}

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
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		// Uncordon member clusters 1, 2 again to guarantee clean up of cordon taint on test failure.
		runUncordonClusterBinary(hubClusterName, memberCluster1EastProdName)
		runUncordonClusterBinary(hubClusterName, memberCluster2EastCanaryName)

		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")

		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block draining on 2/3 clusters", func() {
		crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
				MinAvailable: &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: int32(len(allMemberClusterNames)) - 1,
				},
			},
		}
		Expect(hubClient.Create(ctx, &crpdb)).To(Succeed(), "Failed to create CRP Disruption Budget %s", crpName)
	})

	It("drain cluster using binary, should succeed", func() { runDrainClusterBinary(hubClusterName, memberCluster1EastProdName) })

	It("drain cluster using binary, should fail due to CRPDB", func() { runDrainClusterBinary(hubClusterName, memberCluster2EastCanaryName) })

	It("should ensure no resources exist on drained clusters", func() {
		for _, cluster := range drainClusters {
			resourceRemovedActual := workNamespaceRemovedFromClusterActual(cluster)
			Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to check if resources doesn't exist on member cluster")
		}
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), noDrainClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the selected clusters with no taint", func() {
		for _, cluster := range noDrainClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the selected clusters")
		}
	})

	It("uncordon cluster using binary", func() { runUncordonClusterBinary(hubClusterName, memberCluster1EastProdName) })

	It("uncordon cluster using binary", func() { runUncordonClusterBinary(hubClusterName, memberCluster2EastCanaryName) })

	It("should place resources on all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})

func runDrainClusterBinary(hubClusterName, memberClusterName string) {
	By(fmt.Sprintf("draining cluster %s", memberClusterName))
	cmd := exec.Command(drainBinaryPath,
		"--hubClusterContext", hubClusterName,
		"--clusterName", memberClusterName)
	output, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), "Drain command failed with error: %v\nOutput: %s", err, string(output))
}

func runUncordonClusterBinary(hubClusterName, memberClusterName string) {
	By(fmt.Sprintf("uncordoning cluster %s", memberClusterName))
	cmd := exec.Command(uncordonBinaryPath,
		"--hubClusterContext", hubClusterName,
		"--clusterName", memberClusterName)
	output, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), "Uncordon command failed with error: %v\nOutput: %s", err, string(output))
}
