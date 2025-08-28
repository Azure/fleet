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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider/azure"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

var _ = Describe("placing namespaced scoped resources using a RP with PickAll policy", Label("resourceplacement"), func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())

	BeforeEach(OncePerOrdered, func() {
		// Create the resources.
		createWorkResources()

		// Create the CRP with Namespace-only selector.
		createNamespaceOnlyCRP(crpName)

		By("should update CRP status as expected")
		crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterEach(OncePerOrdered, func() {
		ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, allMemberClusters)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("with no placement policy specified", Ordered, func() {
		It("creating the RP should succeed", func() {
			// Create the RP in the same namespace selecting namespaced resources with no placement policy.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  appNamespace().Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place the resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
	})

	Context("with no affinities specified", Ordered, func() {
		It("creating the RP should succeed", func() {
			// Create the RP in the same namespace selecting namespaced resources.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  appNamespace().Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place the resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
	})

	Context("with affinities, label selector only, updated", Ordered, func() {
		It("creating the RP should succeed", func() {
			// Create the RP in the same namespace selecting namespaced resources.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  appNamespace().Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionEast,
												},
												MatchExpressions: []metav1.LabelSelectorRequirement{
													{
														Key:      envLabelName,
														Operator: metav1.LabelSelectorOpIn,
														Values: []string{
															envProd,
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should place resources on matching clusters", func() {
			// Verify that resources have been placed on the matching clusters.
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on matching clusters")
		})

		It("can update the RP", func() {
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, rp); err != nil {
					return err
				}

				rp.Spec.Policy.Affinity = &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											regionLabelName: regionWest,
										},
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      envLabelName,
												Operator: metav1.LabelSelectorOpIn,
												Values: []string{
													envProd,
												},
											},
										},
									},
								},
							},
						},
					},
				}
				return hubClient.Update(ctx, rp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP")
		})

		It("should update RP status as expected", func() {
			statusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster3WestProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on matching clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster3WestProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on matching clusters")
		})

		It("should remove resources on previously matched clusters", func() {
			checkIfRemovedConfigMapFromMemberClusters([]*framework.Cluster{memberCluster1EastProd})
		})
	})

	Context("with affinities, label selector only, no matching clusters", Ordered, func() {
		It("creating the RP should succeed", func() {
			// Create the RP in the same namespace selecting namespaced resources.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  appNamespace().Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionWest,
												},
												MatchExpressions: []metav1.LabelSelectorRequirement{
													{
														Key:      envLabelName,
														Operator: metav1.LabelSelectorOpIn,
														Values: []string{
															envCanary,
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			statusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), nil, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should not place resources on any cluster", checkIfRemovedConfigMapFromAllMemberClusters)
	})

	Context("with affinities, metric selector only", Ordered, func() {
		It("creating the RP should succeed", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			// Create the RP in the same namespace selecting namespaced resources.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  appNamespace().Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			statusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster2EastCanaryName, memberCluster3WestProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on matching clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})
	})

	Context("with affinities, metric selector only, updated", Ordered, func() {
		It("creating the RP should succeed", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			// Create the RP in the same namespace selecting namespaced resources.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  appNamespace().Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			statusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster2EastCanaryName, memberCluster3WestProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on matching clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("can update the RP", func() {
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, rp); err != nil {
					return err
				}

				rp.Spec.Policy.Affinity = &placementv1beta1.Affinity{
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
				return hubClient.Update(ctx, rp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP")
		})

		It("should update RP status as expected", func() {
			statusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster2EastCanaryName, memberCluster3WestProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on matching clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})
	})

	Context("with affinities, metric selector only, no matching clusters", Ordered, func() {
		It("creating the RP should succeed", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			// Create the RP in the same namespace selecting namespaced resources.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  appNamespace().Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
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
														Name:     azure.PerCPUCoreCostProperty,
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			statusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), nil, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should not place resources on any cluster", checkIfRemovedConfigMapFromAllMemberClusters)
	})

	Context("with affinities, label and metric selectors", Ordered, func() {
		It("creating the RP should succeed", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			// Create the RP in the same namespace selecting namespaced resources.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  appNamespace().Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			statusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster2EastCanaryName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on matching clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster2EastCanary)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on matching clusters")
		})
	})

	Context("with affinities, label and metric selectors, updated", Ordered, func() {
		It("creating the RP should succeed", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			// Create the RP in the same namespace selecting namespaced resources.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  appNamespace().Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			statusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster1EastProdName, memberCluster2EastCanaryName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on matching clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("can update the RP", func() {
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, rp); err != nil {
					return err
				}

				rp.Spec.Policy.Affinity = &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											regionLabelName: regionEast,
										},
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      envLabelName,
												Operator: metav1.LabelSelectorOpIn,
												Values: []string{
													envCanary,
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
													regionWest,
												},
											},
											{
												Key:      envLabelName,
												Operator: metav1.LabelSelectorOpIn,
												Values: []string{
													envProd,
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
				return hubClient.Update(ctx, rp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP")
		})

		It("should update RP status as expected", func() {
			statusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster1EastProdName}, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on matching clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on matching clusters")
		})

		It("should remove resources on previously matched clusters", func() {
			checkIfRemovedConfigMapFromMemberClusters([]*framework.Cluster{memberCluster2EastCanary})
		})
	})

	Context("with affinities, label and metric selectors, no matching clusters", Ordered, func() {
		It("creating the RP should succeed", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			// Create the RP in the same namespace selecting namespaced resources.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  appNamespace().Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
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
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     azure.PerGBMemoryCostProperty,
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			statusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), nil, nil, "0")
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should not place resources on any cluster", checkIfRemovedConfigMapFromAllMemberClusters)
	})
})
