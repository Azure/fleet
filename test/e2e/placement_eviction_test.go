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

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/test/e2e/framework"
	testutilseviction "go.goms.io/fleet/test/utils/eviction"
)

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickFixed CRP, invalid eviction denied - No PDB specified", Ordered, func() {
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
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickFixedPlacementType,
					ClusterNames:  allMemberClusterNames,
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
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
			&testutilseviction.IsValidEviction{IsValid: false, Msg: condition.EvictionInvalidPickFixedCRPMessage},
			nil)
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure cluster resource placement status is unchanged", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding, taint cluster before eviction - No PDB specified", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())
	var taintClusterNames, noTaintClusterNames []string
	var taintClusters, noTaintClusters []*framework.Cluster

	BeforeAll(func() {
		taintClusterNames = []string{memberCluster1EastProdName}
		taintClusters = []*framework.Cluster{memberCluster1EastProd}

		noTaintClusterNames = []string{memberCluster2EastCanaryName, memberCluster3WestProdName}
		noTaintClusters = []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}

		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		// Remove taint from member cluster 1.
		removeTaintsFromMemberClusters(taintClusterNames)
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
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
		checkIfRemovedWorkResourcesFromMemberClusters(taintClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), noTaintClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the selected clusters with no taint", func() {
		for _, cluster := range noTaintClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the selected clusters")
		}
	})
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding, no taint specified, evicted cluster is picked again by scheduler - No PDB specified", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
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

	// specifying a longer timeout, the namespace is being evicted while a new namespace is propagated with the same name leading to a failed takeover.
	It("should ensure evicted cluster is picked again by scheduler & update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickAll CRP, PDB with MaxUnavailable set as Integer, eviction denied due to misconfigured PDB", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block eviction", func() {
		crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
				MaxUnavailable: &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: int32(len(allMemberClusterNames)),
				},
			},
		}
		Expect(hubClient.Create(ctx, &crpdb)).To(Succeed(), "Failed to create CRP Disruption Budget %s", crpName)
	})

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
			&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: condition.EvictionBlockedMisconfiguredPDBSpecifiedMessage})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure cluster resource placement status is unchanged", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickAll CRP, PDB with MaxUnavailable set as Percentage, eviction denied due to misconfigured PDB", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block eviction", func() {
		crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
				MaxUnavailable: &intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "100%",
				},
			},
		}
		Expect(hubClient.Create(ctx, &crpdb)).To(Succeed(), "Failed to create CRP Disruption Budget %s", crpName)
	})

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
			&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: condition.EvictionBlockedMisconfiguredPDBSpecifiedMessage})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure cluster resource placement status is unchanged", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickAll CRP, PDB with MinAvailable set as Percentage, eviction denied due to misconfigured PDB", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block eviction", func() {
		crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
				MinAvailable: &intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "100%",
				},
			},
		}
		Expect(hubClient.Create(ctx, &crpdb)).To(Succeed(), "Failed to create CRP Disruption Budget %s", crpName)
	})

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
			&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: condition.EvictionBlockedMisconfiguredPDBSpecifiedMessage})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure cluster resource placement status is unchanged", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickAll CRP, PDB with MinAvailable specified as Integer to protect resources on all clusters, eviction denied", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block eviction", func() {
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
			&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: fmt.Sprintf(condition.EvictionBlockedPDBSpecifiedMessageFmt, 3, 3)})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure cluster resource placement status is unchanged", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickAll CRP, PDB with MinAvailable specified as Integer to protect resources in all but one cluster, eviction allowed", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())
	var taintClusterNames, noTaintClusterNames []string
	var taintClusters, noTaintClusters []*framework.Cluster

	BeforeAll(func() {
		taintClusterNames = []string{memberCluster1EastProdName}
		taintClusters = []*framework.Cluster{memberCluster1EastProd}

		noTaintClusterNames = []string{memberCluster2EastCanaryName, memberCluster3WestProdName}
		noTaintClusters = []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}

		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		removeTaintsFromMemberClusters(taintClusterNames)
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block eviction", func() {
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
				ClusterName:   memberCluster1EastProdName,
			},
		}
		Expect(hubClient.Create(ctx, crpe)).To(Succeed(), "Failed to create CRP eviction %s", crpe.Name)
	})

	It("should update cluster resource placement eviction status as expected", func() {
		crpEvictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
			ctx, hubClient, crpEvictionName,
			&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
			&testutilseviction.IsExecutedEviction{IsExecuted: true, Msg: fmt.Sprintf(condition.EvictionAllowedPDBSpecifiedMessageFmt, 3, 3)})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure no resources exist on evicted member cluster with taint", func() {
		checkIfRemovedWorkResourcesFromMemberClusters(taintClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), noTaintClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the selected clusters with no taint", func() {
		for _, cluster := range noTaintClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the selected clusters")
		}
	})
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickN CRP, PDB with MaxUnavailable specified as Integer to protect resources on all clusters, eviction denied", Ordered, func() {
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
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType:    placementv1beta1.PickNPlacementType,
					NumberOfClusters: ptr.To(int32(len(allMemberClusterNames))),
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block eviction", func() {
		crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
				MaxUnavailable: &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 0,
				},
			},
		}
		Expect(hubClient.Create(ctx, &crpdb)).To(Succeed(), "Failed to create CRP Disruption Budget %s", crpName)
	})

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
			&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: fmt.Sprintf(condition.EvictionBlockedPDBSpecifiedMessageFmt, 3, 3)})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure cluster resource placement status is unchanged", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickN CRP, PDB with MaxUnavailable specified as Integer to protect resources all clusters but one cluster, eviction allowed", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())
	var taintClusterNames, noTaintClusterNames []string
	var taintClusters, noTaintClusters []*framework.Cluster

	BeforeAll(func() {
		taintClusterNames = []string{memberCluster1EastProdName}
		taintClusters = []*framework.Cluster{memberCluster1EastProd}

		noTaintClusterNames = []string{memberCluster2EastCanaryName, memberCluster3WestProdName}
		noTaintClusters = []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}

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
					PlacementType:    placementv1beta1.PickNPlacementType,
					NumberOfClusters: ptr.To(int32(len(allMemberClusterNames))),
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		removeTaintsFromMemberClusters(taintClusterNames)
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block eviction", func() {
		crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
				MaxUnavailable: &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 1,
				},
			},
		}
		Expect(hubClient.Create(ctx, &crpdb)).To(Succeed(), "Failed to create CRP Disruption Budget %s", crpName)
	})

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
				ClusterName:   memberCluster1EastProdName,
			},
		}
		Expect(hubClient.Create(ctx, crpe)).To(Succeed(), "Failed to create CRP eviction %s", crpe.Name)
	})

	It("should update cluster resource placement eviction status as expected", func() {
		crpEvictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
			ctx, hubClient, crpEvictionName,
			&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
			&testutilseviction.IsExecutedEviction{IsExecuted: true, Msg: fmt.Sprintf(condition.EvictionAllowedPDBSpecifiedMessageFmt, 3, 3)})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure no resources exist on evicted member cluster with taint", func() {
		checkIfRemovedWorkResourcesFromMemberClusters(taintClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), noTaintClusterNames, taintClusterNames, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the selected clusters with no taint", func() {
		for _, cluster := range noTaintClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the selected clusters")
		}
	})
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickN CRP, PDB with MaxUnavailable specified as percentage to protect all clusters but one, eviction allowed", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())
	var taintClusterNames, noTaintClusterNames []string
	var taintClusters, noTaintClusters []*framework.Cluster

	BeforeAll(func() {
		taintClusterNames = []string{memberCluster1EastProdName}
		taintClusters = []*framework.Cluster{memberCluster1EastProd}

		noTaintClusterNames = []string{memberCluster2EastCanaryName, memberCluster3WestProdName}
		noTaintClusters = []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}

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
					PlacementType:    placementv1beta1.PickNPlacementType,
					NumberOfClusters: ptr.To(int32(len(allMemberClusterNames))),
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		removeTaintsFromMemberClusters(taintClusterNames)
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("add taint to member cluster 1", func() {
		addTaintsToMemberClusters(taintClusterNames, buildTaints(taintClusterNames))
	})

	It("create cluster resource placement disruption budget to block eviction", func() {
		crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
				MaxUnavailable: &intstr.IntOrString{
					Type:   intstr.String,
					StrVal: fmt.Sprintf("%d%%", 100/len(allMemberClusterNames)),
				},
			},
		}
		Expect(hubClient.Create(ctx, &crpdb)).To(Succeed(), "Failed to create CRP Disruption Budget %s", crpName)
	})

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
			&testutilseviction.IsExecutedEviction{IsExecuted: true, Msg: fmt.Sprintf(condition.EvictionAllowedPDBSpecifiedMessageFmt, 3, 3)})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure no resources exist on evicted member cluster with taint", func() {
		checkIfRemovedWorkResourcesFromMemberClusters(taintClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), noTaintClusterNames, taintClusterNames, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the selected clusters with no taint", func() {
		for _, cluster := range noTaintClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the selected clusters")
		}
	})
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickN CRP, PDB with MaxUnavailable specified as percentage to protect resources on all clusters, eviction denied", Ordered, func() {
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
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType:    placementv1beta1.PickNPlacementType,
					NumberOfClusters: ptr.To(int32(len(allMemberClusterNames))),
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block eviction", func() {
		crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
				MaxUnavailable: &intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "0%",
				},
			},
		}
		Expect(hubClient.Create(ctx, &crpdb)).To(Succeed(), "Failed to create CRP Disruption Budget %s", crpName)
	})

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
			&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: fmt.Sprintf(condition.EvictionBlockedPDBSpecifiedMessageFmt, 3, 3)})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should update cluster resource placement eviction status as expected", func() {
		crpEvictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
			ctx, hubClient, crpEvictionName,
			&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
			&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: fmt.Sprintf(condition.EvictionBlockedPDBSpecifiedMessageFmt, 3, 3)})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure cluster resource placement status is unchanged", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickN CRP, PDB with MinAvailable specified as Integer to protect resources on all clusters, eviction denied", Ordered, func() {
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
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType:    placementv1beta1.PickNPlacementType,
					NumberOfClusters: ptr.To(int32(len(allMemberClusterNames))),
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block eviction", func() {
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
			&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: fmt.Sprintf(condition.EvictionBlockedPDBSpecifiedMessageFmt, 3, 3)})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure cluster resource placement status is unchanged", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickN CRP, PDB with MinAvailable specified as Integer to protect resources all clusters but one cluster, eviction allowed", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())
	var taintClusterNames, noTaintClusterNames []string
	var taintClusters, noTaintClusters []*framework.Cluster

	BeforeAll(func() {
		taintClusterNames = []string{memberCluster1EastProdName}
		taintClusters = []*framework.Cluster{memberCluster1EastProd}

		noTaintClusterNames = []string{memberCluster2EastCanaryName, memberCluster3WestProdName}
		noTaintClusters = []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}

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
					PlacementType:    placementv1beta1.PickNPlacementType,
					NumberOfClusters: ptr.To(int32(len(allMemberClusterNames))),
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		removeTaintsFromMemberClusters(taintClusterNames)
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block eviction", func() {
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
				ClusterName:   memberCluster1EastProdName,
			},
		}
		Expect(hubClient.Create(ctx, crpe)).To(Succeed(), "Failed to create CRP eviction %s", crpe.Name)
	})

	It("should update cluster resource placement eviction status as expected", func() {
		crpEvictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
			ctx, hubClient, crpEvictionName,
			&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
			&testutilseviction.IsExecutedEviction{IsExecuted: true, Msg: fmt.Sprintf(condition.EvictionAllowedPDBSpecifiedMessageFmt, 3, 3)})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure no resources exist on evicted member cluster with taint", func() {
		checkIfRemovedWorkResourcesFromMemberClusters(taintClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), noTaintClusterNames, taintClusterNames, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the selected clusters with no taint", func() {
		for _, cluster := range noTaintClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the selected clusters")
		}
	})
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickN CRP, PDB with MinAvailable specified as percentage to protect all clusters but one, eviction allowed", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crpEvictionName := fmt.Sprintf(crpEvictionNameTemplate, GinkgoParallelProcess())
	var taintClusterNames, noTaintClusterNames []string
	var taintClusters, noTaintClusters []*framework.Cluster

	BeforeAll(func() {
		taintClusterNames = []string{memberCluster1EastProdName}
		taintClusters = []*framework.Cluster{memberCluster1EastProd}

		noTaintClusterNames = []string{memberCluster2EastCanaryName, memberCluster3WestProdName}
		noTaintClusters = []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}

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
					PlacementType:    placementv1beta1.PickNPlacementType,
					NumberOfClusters: ptr.To(int32(len(allMemberClusterNames))),
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		removeTaintsFromMemberClusters(taintClusterNames)
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("add taint to member cluster 1", func() {
		addTaintsToMemberClusters(taintClusterNames, buildTaints(taintClusterNames))
	})

	It("create cluster resource placement disruption budget to block eviction", func() {
		crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
				MinAvailable: &intstr.IntOrString{
					Type:   intstr.String,
					StrVal: fmt.Sprintf("%d%%", (len(allMemberClusterNames)-1)*100/len(allMemberClusterNames)),
				},
			},
		}
		Expect(hubClient.Create(ctx, &crpdb)).To(Succeed(), "Failed to create CRP Disruption Budget %s", crpName)
	})

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
			&testutilseviction.IsExecutedEviction{IsExecuted: true, Msg: fmt.Sprintf(condition.EvictionAllowedPDBSpecifiedMessageFmt, 3, 3)})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure no resources exist on evicted member cluster with taint", func() {
		checkIfRemovedWorkResourcesFromMemberClusters(taintClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), noTaintClusterNames, taintClusterNames, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the selected clusters with no taint", func() {
		for _, cluster := range noTaintClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the selected clusters")
		}
	})
})

var _ = Describe("ClusterResourcePlacement eviction of bound binding - PickN CRP, PDB with MinAvailable specified as percentage to protect resources on all clusters, eviction denied", Ordered, func() {
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
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType:    placementv1beta1.PickNPlacementType,
					NumberOfClusters: ptr.To(int32(len(allMemberClusterNames))),
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		ensureCRPEvictionDeleted(crpEvictionName)
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block eviction", func() {
		crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
				MinAvailable: &intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "100%",
				},
			},
		}
		Expect(hubClient.Create(ctx, &crpdb)).To(Succeed(), "Failed to create CRP Disruption Budget %s", crpName)
	})

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
			&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: fmt.Sprintf(condition.EvictionBlockedPDBSpecifiedMessageFmt, 3, 3)})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should update cluster resource placement eviction status as expected", func() {
		crpEvictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
			ctx, hubClient, crpEvictionName,
			&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
			&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: fmt.Sprintf(condition.EvictionBlockedPDBSpecifiedMessageFmt, 3, 3)})
		Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
	})

	It("should ensure cluster resource placement status is unchanged", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})
