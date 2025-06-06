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
	"k8s.io/utils/ptr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = Describe("placing resource using a cluster resource placement with pickFixed placement policy specified, taint clusters, pick all specified clusters", Serial, Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		// Create the resources.
		createWorkResources()
		// Add taint to all member clusters.
		addTaintsToMemberClusters(allMemberClusterNames, buildTaints(allMemberClusterNames))

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: workResourceSelector(),
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickFixedPlacementType,
					ClusterNames:  allMemberClusterNames,
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create cluster resource placement")
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on specified clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	AfterAll(func() {
		// Remove taint from all member clusters.
		removeTaintsFromMemberClusters(allMemberClusterNames)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})
})

var _ = Describe("placing resources using a cluster resource placement with no placement policy specified, taint clusters, update cluster resource placement with tolerations", Serial, Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	var taintClusterNames, noTaintClusterNames []string
	var taintClusters, noTaintClusters []*framework.Cluster

	BeforeAll(func() {
		taintClusterNames = []string{memberCluster1EastProdName, memberCluster2EastCanaryName}
		taintClusters = []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}

		noTaintClusterNames = []string{memberCluster3WestProdName}
		noTaintClusters = []*framework.Cluster{memberCluster3WestProd}

		// Create the resources.
		createWorkResources()
		// Add taint to member clusters 1, 2.
		addTaintsToMemberClusters(taintClusterNames, buildTaints(taintClusterNames))

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: workResourceSelector(),
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create cluster resource placement")
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), noTaintClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should ensure no resources exist on member clusters with taint", func() {
		checkIfRemovedWorkResourcesFromMemberClusters(taintClusters)
	})

	It("should place resources on the selected cluster without taint", func() {
		for _, cluster := range noTaintClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on selected cluster")
		}
	})

	It("should update cluster resource placement spec with tolerations for tainted cluster", func() {
		// update CRP with toleration for member cluster 1,2.
		updateCRPWithTolerations(buildTolerations(taintClusterNames))
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	AfterAll(func() {
		// Remove taint from member cluster 1,2.
		removeTaintsFromMemberClusters(taintClusterNames)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})
})

var _ = Describe("placing resources using a cluster resource placement with no placement policy specified, taint clusters, remove taints from cluster, all cluster should be picked", Serial, Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	var taintClusterNames, noTaintClusterNames []string
	var taintClusters, noTaintClusters []*framework.Cluster

	BeforeAll(func() {
		taintClusterNames = []string{memberCluster1EastProdName, memberCluster2EastCanaryName}
		taintClusters = []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}

		noTaintClusterNames = []string{memberCluster3WestProdName}
		noTaintClusters = []*framework.Cluster{memberCluster3WestProd}

		// Create the resources.
		createWorkResources()
		// Add taint to member clusters 1, 2.
		addTaintsToMemberClusters(taintClusterNames, buildTaints(taintClusterNames))

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: workResourceSelector(),
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create cluster resource placement")
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), noTaintClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should ensure no resources exist on member clusters with taint", func() {
		checkIfRemovedWorkResourcesFromMemberClusters(taintClusters)
	})

	It("should place resources on the selected cluster without taint", func() {
		for _, cluster := range noTaintClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on selected cluster")
		}
	})

	It("should remove taints from member clusters", func() {
		// Remove taint from member cluster 1,2.
		removeTaintsFromMemberClusters(taintClusterNames)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	AfterAll(func() {
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})
})

var _ = Describe("picking N clusters with affinities and topology spread constraints, taint clusters, create cluster resource placement with toleration for one cluster", Serial, Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	var taintClusterNames, tolerateClusterNames, unSelectedClusterNames []string
	var tolerateClusters, unSelectedClusters []*framework.Cluster

	BeforeAll(func() {
		taintClusterNames = []string{memberCluster1EastProdName, memberCluster2EastCanaryName}

		tolerateClusterNames = []string{memberCluster1EastProdName}
		tolerateClusters = []*framework.Cluster{memberCluster1EastProd}

		unSelectedClusterNames = []string{memberCluster2EastCanaryName}
		unSelectedClusters = []*framework.Cluster{memberCluster2EastCanary}

		// Create the resources.
		createWorkResources()
		// Add taint to member cluster 1, 2.
		addTaintsToMemberClusters(taintClusterNames, buildTaints(taintClusterNames))

		// Create the CRP, with toleration for member cluster 1.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: workResourceSelector(),
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType:    placementv1beta1.PickNPlacementType,
					NumberOfClusters: ptr.To(int32(2)),
					// Note that due to limitations in the E2E environment, specifically the limited
					// number of clusters available, the affinity and topology spread constraints
					// specified here are validated only on a very superficial level, i.e., the flow
					// functions. For further evaluations, specifically the correctness check
					// of the affinity and topology spread constraint logic, see the scheduler
					// integration tests.
					Affinity: &placementv1beta1.Affinity{
						ClusterAffinity: &placementv1beta1.ClusterAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												regionLabelName: regionEast,
											},
										},
									},
								},
							},
						},
					},
					TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
						{
							MaxSkew:           ptr.To(int32(1)),
							TopologyKey:       envLabelName,
							WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
						},
					},
					Tolerations: buildTolerations(tolerateClusterNames),
				},
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create cluster resource placement")
	})

	It("should update cluster resource placement status as expected", func() {
		// we choose two clusters using a label.
		statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), tolerateClusterNames, unSelectedClusterNames, "0")
		Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the selected clusters with tolerated taint", func() {
		for _, cluster := range tolerateClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the selected clusters")
		}
	})

	It("should ensure no resources exist on member clusters with untolerated taint", func() {
		checkIfRemovedWorkResourcesFromMemberClusters(unSelectedClusters)
	})

	AfterAll(func() {
		// Remove taint from member cluster 1, 2.
		removeTaintsFromMemberClusters(taintClusterNames)
		ensureCRPAndRelatedResourcesDeleted(crpName, tolerateClusters)
	})
})

var _ = Describe("picking all clusters using pickAll placement policy, add taint to a cluster that's already selected", Serial, Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	taintClusterNames := []string{memberCluster1EastProdName}

	BeforeAll(func() {
		// Create the resources.
		createWorkResources()

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create cluster resource placement")
	})

	It("should update cluster resource placement status as expected", func() {
		statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the selected clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("should add taint to a cluster that's already selected", func() {
		// Add taint to member cluster 1.
		addTaintsToMemberClusters(taintClusterNames, buildTaints(taintClusterNames))
	})

	It("should still update cluster resource placement status as expected, no status updates", func() {
		statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the selected clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	AfterAll(func() {
		// Remove taint from member cluster 1.
		removeTaintsFromMemberClusters(taintClusterNames)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})
})
