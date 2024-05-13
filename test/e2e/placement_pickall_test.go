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
	"go.goms.io/fleet/pkg/propertyprovider/aks"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = Describe("placing resources using a CRP with no placement policy specified", Ordered, func() {
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
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("should place the resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	AfterAll(func() {
		ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
	})
})

var _ = Describe("placing resources using a CRP of PickAll placement type", func() {
	Context("with no affinities specified", Ordered, func() {
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
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity:      nil,
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
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place the resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
		})
	})

	Context("with affinities specified, label selector only", Ordered, func() {
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
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
												},
												MatchExpressions: []metav1.LabelSelectorRequirement{
													{
														Key:      envLabelName,
														Operator: metav1.LabelSelectorOpIn,
														Values: []string{
															envLabelValue1,
														},
													},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1EastProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on matching clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on matching clusters")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster1EastProd})
		})
	})

	Context("with affinities, label selector only, updated", Ordered, func() {
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
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
												},
												MatchExpressions: []metav1.LabelSelectorRequirement{
													{
														Key:      envLabelName,
														Operator: metav1.LabelSelectorOpIn,
														Values: []string{
															envLabelValue1,
														},
													},
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

		It("should place resources on matching clusters", func() {
			// Verify that resources have been placed on the matching clusters.
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on matching clusters")
		})

		It("can update the CRP", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				crp.Spec.Policy.Affinity = &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											regionLabelName: regionLabelValue2,
										},
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      envLabelName,
												Operator: metav1.LabelSelectorOpIn,
												Values: []string{
													envLabelValue1,
												},
											},
										},
									},
								},
							},
						},
					},
				}
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP")
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster3WestProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on matching clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster3WestProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on matching clusters")
		})

		It("should remove resources on previously matched clusters", func() {
			resourceRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster1EastProd)
			Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove resources on previously matched clusters")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster3WestProd})
		})
	})

	Context("with affinities, label selector only, no matching clusters", Ordered, func() {
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
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue2,
												},
												MatchExpressions: []metav1.LabelSelectorRequirement{
													{
														Key:      envLabelName,
														Operator: metav1.LabelSelectorOpIn,
														Values: []string{
															envLabelValue2,
														},
													},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should not place resources on any cluster", checkIfRemovedWorkResourcesFromAllMemberClusters)

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, nil)
		})
	})

	Context("with affinities, metric selector only", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			if !isAKSPropertyProviderEnabled {
				Skip("Skipping this test spec as AKS property provider is not enabled in the test environment")
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
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.NodeCountProperty,
														Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
														Values: []string{
															"3",
														},
													},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster2EastCanaryName, memberCluster3WestProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on matching clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd})
		})
	})

	Context("with affinities, metric selector only, updated", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			if !isAKSPropertyProviderEnabled {
				Skip("Skipping this test spec as AKS property provider is not enabled in the test environment")
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
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.NodeCountProperty,
														Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
														Values: []string{
															"3",
														},
													},
													{
														Name:     propertyprovider.TotalCPUCapacityProperty,
														Operator: placementv1beta1.PropertySelectorLessThan,
														Values: []string{
															"10000",
														},
													},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster2EastCanaryName, memberCluster3WestProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on matching clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("can update the CRP", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				crp.Spec.Policy.Affinity = &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     propertyprovider.NodeCountProperty,
												Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
												Values: []string{
													"3",
												},
											},
											{
												Name:     propertyprovider.TotalCPUCapacityProperty,
												Operator: placementv1beta1.PropertySelectorLessThan,
												Values: []string{
													"10000",
												},
											},
										},
									},
								},
								{
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     propertyprovider.NodeCountProperty,
												Operator: placementv1beta1.PropertySelectorEqualTo,
												Values: []string{
													"4",
												},
											},
											{
												Name:     propertyprovider.AvailableMemoryCapacityProperty,
												Operator: placementv1beta1.PropertySelectorNotEqualTo,
												Values: []string{
													"20000Gi",
												},
											},
										},
									},
								},
							},
						},
					},
				}
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP")
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster2EastCanaryName, memberCluster3WestProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on matching clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd})
		})
	})

	Context("with affinities, metric selector only, no matching clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			if !isAKSPropertyProviderEnabled {
				Skip("Skipping this test spec as AKS property provider is not enabled in the test environment")
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
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     aks.PerCPUCoreCostProperty,
														Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
														Values: []string{
															"0.01",
														},
													},
													{
														Name:     propertyprovider.AllocatableCPUCapacityProperty,
														Operator: placementv1beta1.PropertySelectorGreaterThan,
														Values: []string{
															"10000",
														},
													},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should not place resources on any cluster", checkIfRemovedWorkResourcesFromAllMemberClusters)

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, nil)
		})
	})

	Context("with affinities, label and metric selectors", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			if !isAKSPropertyProviderEnabled {
				Skip("Skipping this test spec as AKS property provider is not enabled in the test environment")
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
						PlacementType: placementv1beta1.PickAllPlacementType,
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
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.NodeCountProperty,
														Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
														Values: []string{
															"3",
														},
													},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster2EastCanaryName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on matching clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster2EastCanary)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on matching clusters")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster2EastCanary})
		})
	})

	Context("with affinities, label and metric selectors, updated", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			if !isAKSPropertyProviderEnabled {
				Skip("Skipping this test spec as AKS property provider is not enabled in the test environment")
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
						PlacementType: placementv1beta1.PickAllPlacementType,
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
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.AllocatableCPUCapacityProperty,
														Operator: placementv1beta1.PropertySelectorLessThanOrEqualTo,
														Values: []string{
															"10000",
														},
													},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1EastProdName, memberCluster2EastCanaryName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on matching clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("can update the CRP", func() {
			Eventually(func() error {
				// Retrieve the CRP.
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				// Update the CRP.
				crp.Spec.Policy.Affinity = &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											regionLabelName: regionLabelValue1,
										},
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      envLabelName,
												Operator: metav1.LabelSelectorOpIn,
												Values: []string{
													envLabelValue2,
												},
											},
										},
									},
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     propertyprovider.AllocatableMemoryCapacityProperty,
												Operator: placementv1beta1.PropertySelectorLessThan,
												Values: []string{
													"1Ki",
												},
											},
										},
									},
								},
								{
									LabelSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      regionLabelName,
												Operator: metav1.LabelSelectorOpNotIn,
												Values: []string{
													regionLabelValue2,
												},
											},
											{
												Key:      envLabelName,
												Operator: metav1.LabelSelectorOpIn,
												Values: []string{
													envLabelValue1,
												},
											},
										},
									},
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     propertyprovider.NodeCountProperty,
												Operator: placementv1beta1.PropertySelectorEqualTo,
												Values: []string{
													"2",
												},
											},
											{
												Name:     propertyprovider.TotalMemoryCapacityProperty,
												Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
												Values: []string{
													"1Ki",
												},
											},
										},
									},
								},
							},
						},
					},
				}
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP")
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1EastProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on matching clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on matching clusters")
		})

		It("should remove resources on previously matched clusters", func() {
			resourceRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster2EastCanary)
			Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove resources on previously matched clusters")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, []*framework.Cluster{memberCluster1EastProd})
		})
	})

	Context("with affinities, label and metric selectors, no matching clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			if !isAKSPropertyProviderEnabled {
				Skip("Skipping this test spec as AKS property provider is not enabled in the test environment")
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
						PlacementType: placementv1beta1.PickAllPlacementType,
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
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     aks.PerGBMemoryCostProperty,
														Operator: placementv1beta1.PropertySelectorEqualTo,
														Values: []string{
															"0",
														},
													},
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
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should not place resources on any cluster", checkIfRemovedWorkResourcesFromAllMemberClusters)

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, nil)
		})
	})
})
