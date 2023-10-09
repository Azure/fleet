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
	"k8s.io/utils/pointer"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = Describe("placing resources using a CRP of PickN placement", func() {
	Context("picking N clusters with no affinities/topology spread constraints", Ordered, func() {
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
						NumberOfClusters: pointer.Int32(1),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should place resources on the picked clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster3)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster3Name}, nil)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster3})
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
						NumberOfClusters: pointer.Int32(2),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Verify that resources have been placed on the picked clusters.
			targetClusters := []*framework.Cluster{memberCluster3, memberCluster2}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}

			// Downscale.
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				crp.Spec.Policy.NumberOfClusters = pointer.Int32(1)
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to downscale")
		})

		It("should place resources on the picked clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster3)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
		})

		It("should remove resources from the downscaled clusters", func() {
			resourceRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster2)
			Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove resources from the downscaled clusters")
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster3Name}, nil)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster3})
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
						NumberOfClusters: pointer.Int32(1),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Verify that resources have been placed on the picked clusters.
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster3)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")

			// Upscale.
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				crp.Spec.Policy.NumberOfClusters = pointer.Int32(2)
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to downscale")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster3, memberCluster2}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster3Name, memberCluster2Name}, nil)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster3, memberCluster2})
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
						NumberOfClusters: pointer.Int32(2),
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
											LabelSelector: metav1.LabelSelector{
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
								MaxSkew:           pointer.Int32(1),
								TopologyKey:       envLabelName,
								WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
							},
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1, memberCluster2}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1Name, memberCluster2Name}, nil)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster1, memberCluster2})
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
						NumberOfClusters: pointer.Int32(2),
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: metav1.LabelSelector{
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
								MaxSkew:           pointer.Int32(1),
								TopologyKey:       envLabelName,
								WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
							},
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Verify that resources have been placed on the picked clusters.
			targetClusters := []*framework.Cluster{memberCluster1, memberCluster2}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}

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
									LabelSelector: metav1.LabelSelector{
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
						MaxSkew:           pointer.Int32(1),
						TopologyKey:       regionLabelName,
						WhenUnsatisfiable: placementv1beta1.ScheduleAnyway,
					},
				}
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP with new affinity and topology spread constraints")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1, memberCluster3}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("should remove resources from the unpicked clusters", func() {
			resourceRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster2)
			Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove resources from the downscaled clusters")
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1Name, memberCluster3Name}, nil)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster1, memberCluster3})
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
						NumberOfClusters: pointer.Int32(5),
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: metav1.LabelSelector{
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
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1, memberCluster2}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1Name, memberCluster2Name}, []string{memberCluster3Name, memberCluster4Name, memberCluster5Name})
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster1, memberCluster2})
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
						NumberOfClusters: pointer.Int32(2),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Verify that resources have been placed on the picked clusters.
			targetClusters := []*framework.Cluster{memberCluster3, memberCluster2}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}

			// Downscale.
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				crp.Spec.Policy.NumberOfClusters = pointer.Int32(0)
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to downscale")
		})

		It("should remove resources from the downscaled clusters", func() {
			downscaledClusters := []*framework.Cluster{memberCluster3, memberCluster2}
			for _, cluster := range downscaledClusters {
				resourceRemovedActual := workNamespaceRemovedFromClusterActual(cluster)
				Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove resources from the downscaled clusters")
			}
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, nil)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, nil)
		})
	})
})
