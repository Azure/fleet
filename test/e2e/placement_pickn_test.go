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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/propertyprovider"
	"go.goms.io/fleet/pkg/propertyprovider/azure"
	"go.goms.io/fleet/test/e2e/framework"
)

// Note that the scheduler will pick by names in alphanumeric order, when there are
// multiple candidate clusters with the same score.

var _ = Describe("placing resources using a CRP of PickN placement", func() {
	Context("picking N clusters with no affinities/topology spread constraints (pick by cluster names in alphanumeric order)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
					},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster3WestProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster3WestProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster3WestProd})
		})
	})

	Context("downscaling", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(2)),
					},
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

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster3WestProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("can downscale", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				crp.Spec.Policy.NumberOfClusters = ptr.To(int32(1))
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to downscale")
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster3WestProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on the newly picked clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster3WestProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
		})

		It("should remove resources from the downscaled clusters", func() {
			resourceRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster2EastCanary)
			Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove resources from the downscaled clusters")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster3WestProd})
		})
	})

	Context("upscaling", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
					},
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

		It("should place resources on the picked clusters", func() {
			// Verify that resources have been placed on the picked clusters.
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster3WestProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
		})

		It("can upscale", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				crp.Spec.Policy.NumberOfClusters = ptr.To(int32(2))
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to downscale")
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster3WestProdName, memberCluster2EastCanaryName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on the newly picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster3WestProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster3WestProd, memberCluster2EastCanary})
		})
	})

	Context("picking N clusters with affinities and topology spread constraints", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
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
													regionLabelName: regionLabelValue1,
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
					},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1EastProdName, memberCluster2EastCanaryName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary})
		})
	})

	Context("affinities and topology spread constraints updated", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(2)),
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
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
					},
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

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("can update the CRP", func() {
			// Specify new affinity and topology spread constraints.
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				crp.Spec.Policy.Affinity = &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
							{
								Weight: 20,
								Preference: placementv1beta1.ClusterSelectorTerm{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											envLabelName: envLabelValue1,
										},
									},
								},
							},
						},
					},
				}
				crp.Spec.Policy.TopologySpreadConstraints = []placementv1beta1.TopologySpreadConstraint{
					{
						MaxSkew:           ptr.To(int32(1)),
						TopologyKey:       regionLabelName,
						WhenUnsatisfiable: placementv1beta1.ScheduleAnyway,
					},
				}
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP with new affinity and topology spread constraints")
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1EastProdName, memberCluster3WestProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on the newly picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster3WestProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("should remove resources from the unpicked clusters", func() {
			resourceRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster2EastCanary)
			Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove resources from the downscaled clusters")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster1EastProd, memberCluster3WestProd})
		})
	})

	Context("not enough clusters to pick", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickNPlacementType,
						// This spec uses a CRP of the PickN placement type with the number of
						// target clusters equal to that of all clusters present in the environment.
						//
						// This is necessary as the CRP controller reports status for unselected clusters
						// only in a partial manner; specifically, for a CRP of the PickN placement with
						// N target clusters but only M matching clusters, only N - M decisions for
						// unselected clusters will be reported in the CRP status. To avoid
						// undeterministic behaviors, here this value is set to make sure that all
						// unselected clusters will be included in the status.
						NumberOfClusters: ptr.To(int32(5)),
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
												},
											},
										},
									},
								},
							},
						},
					},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1EastProdName, memberCluster2EastCanaryName}, []string{memberCluster3WestProdName, memberCluster4UnhealthyName, memberCluster5LeftName}, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary})
		})
	})

	Context("downscaling to zero", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(2)),
					},
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

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster3WestProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("can downscale", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				crp.Spec.Policy.NumberOfClusters = ptr.To(int32(0))
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to downscale")
		})

		It("should remove resources from the downscaled clusters", func() {
			downscaledClusters := []*framework.Cluster{memberCluster3WestProd, memberCluster2EastCanary}
			for _, cluster := range downscaledClusters {
				resourceRemovedActual := workNamespaceRemovedFromClusterActual(cluster)
				Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove resources from the downscaled clusters")
			}
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, nil)
		})
	})

	Context("picking N clusters with single property sorter", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
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
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 20,
										Preference: placementv1beta1.ClusterSelectorTerm{
											PropertySorter: &placementv1beta1.PropertySorter{
												Name:      propertyprovider.NodeCountProperty,
												SortOrder: placementv1beta1.Ascending,
											},
										},
									},
								},
							},
						},
					},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1EastProdName, memberCluster2EastCanaryName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary})
		})
	})

	Context("picking N clusters with multiple property sorters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
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
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 20,
										Preference: placementv1beta1.ClusterSelectorTerm{
											PropertySorter: &placementv1beta1.PropertySorter{
												Name:      propertyprovider.NodeCountProperty,
												SortOrder: placementv1beta1.Ascending,
											},
										},
									},
									{
										Weight: 20,
										Preference: placementv1beta1.ClusterSelectorTerm{
											PropertySorter: &placementv1beta1.PropertySorter{
												Name:      propertyprovider.AvailableMemoryCapacityProperty,
												SortOrder: placementv1beta1.Descending,
											},
										},
									},
								},
							},
						},
					},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster3WestProdName, memberCluster2EastCanaryName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster3WestProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster3WestProd, memberCluster2EastCanary})
		})
	})

	Context("picking N clusters with label selector and property sorter", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
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
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 20,
										Preference: placementv1beta1.ClusterSelectorTerm{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
												},
											},
											PropertySorter: &placementv1beta1.PropertySorter{
												Name:      propertyprovider.NodeCountProperty,
												SortOrder: placementv1beta1.Ascending,
											},
										},
									},
									{
										Weight: 20,
										Preference: placementv1beta1.ClusterSelectorTerm{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													envLabelName: envLabelValue2,
												},
											},
											PropertySorter: &placementv1beta1.PropertySorter{
												Name:      propertyprovider.AvailableMemoryCapacityProperty,
												SortOrder: placementv1beta1.Descending,
											},
										},
									},
								},
							},
						},
					},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster2EastCanaryName, memberCluster1EastProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster2EastCanary, memberCluster1EastProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster2EastCanary, memberCluster1EastProd})
		})
	})

	Context("picking N clusters with required and preferred affinity terms", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
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
													envLabelName: envLabelValue1,
												},
											},
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     azure.PerCPUCoreCostProperty,
														Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
														Values: []string{
															"0",
														},
													},
													{
														Name:     propertyprovider.NodeCountProperty,
														Operator: placementv1beta1.PropertySelectorNotEqualTo,
														Values: []string{
															"3",
														},
													},
												},
											},
										},
									},
								},
								PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
									{
										Weight: 30,
										Preference: placementv1beta1.ClusterSelectorTerm{
											PropertySorter: &placementv1beta1.PropertySorter{
												Name:      propertyprovider.NodeCountProperty,
												SortOrder: placementv1beta1.Ascending,
											},
										},
									},
									{
										Weight: 40,
										Preference: placementv1beta1.ClusterSelectorTerm{
											PropertySorter: &placementv1beta1.PropertySorter{
												Name:      propertyprovider.AvailableMemoryCapacityProperty,
												SortOrder: placementv1beta1.Descending,
											},
										},
									},
								},
							},
						},
					},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster3WestProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster3WestProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster3WestProd})
		})
	})
})
