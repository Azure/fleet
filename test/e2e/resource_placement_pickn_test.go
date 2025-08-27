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

var _ = Describe("placing namespaced scoped resources using a RP with PickN policy", Label("resourceplacement"), func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
	rpKey := types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}

	BeforeEach(OncePerOrdered, func() {
		// Create the resources.
		createWorkResources()

		// Create the CRP with Namespace-only selector.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: namespaceOnlySelector(),
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
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

		crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterEach(OncePerOrdered, func() {
		ensureRPAndRelatedResourcesDeleted(rpKey, allMemberClusters)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("picking N clusters with no affinities/topology spread constraints (pick by cluster names in alphanumeric order)", Ordered, func() {
		It("should create rp with pickN policy successfully", func() {
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster3WestProdName}, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster3WestProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
		})
	})

	Context("upscaling", Ordered, func() {
		It("should create rp with pickN policy for upscaling test", func() {
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should place resources on the picked clusters", func() {
			// Verify that resources have been placed on the picked clusters.
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster3WestProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
		})

		It("can upscale", func() {
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, rpKey, rp); err != nil {
					return err
				}

				rp.Spec.Policy.NumberOfClusters = ptr.To(int32(2))
				return hubClient.Update(ctx, rp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to upscale")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster3WestProdName, memberCluster2EastCanaryName}, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on the newly picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster3WestProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})
	})

	Context("downscaling", Ordered, func() {
		It("should create rp with pickN policy for downscaling test", func() {
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
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
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, rpKey, rp); err != nil {
					return err
				}

				rp.Spec.Policy.NumberOfClusters = ptr.To(int32(1))
				return hubClient.Update(ctx, rp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to downscale")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster3WestProdName}, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on the newly picked clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster3WestProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
		})

		It("should remove resources from the downscaled clusters", func() {
			checkIfRemovedConfigMapFromMemberClusters([]*framework.Cluster{memberCluster2EastCanary})
		})
	})

	Context("picking N clusters with affinities and topology spread constraints", Ordered, func() {
		It("should create rp with pickN policy and constraints successfully", func() {
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
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster1EastProdName, memberCluster2EastCanaryName}, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})
	})

	Context("affinities and topology spread constraints updated", Ordered, func() {
		It("should create rp with initial constraints", func() {
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
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(2)),
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

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("can update the RP", func() {
			// Specify new affinity and topology spread constraints.
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, rpKey, rp); err != nil {
					return err
				}

				rp.Spec.Policy.Affinity = &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
							{
								Weight: 20,
								Preference: placementv1beta1.ClusterSelectorTerm{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											envLabelName: envProd,
										},
									},
								},
							},
						},
					},
				}
				rp.Spec.Policy.TopologySpreadConstraints = []placementv1beta1.TopologySpreadConstraint{
					{
						MaxSkew:           ptr.To(int32(1)),
						TopologyKey:       regionLabelName,
						WhenUnsatisfiable: placementv1beta1.ScheduleAnyway,
					},
				}
				return hubClient.Update(ctx, rp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP with new affinity and topology spread constraints")
		})

		// topology spread constraints takes a bit longer to be applied
		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster1EastProdName, memberCluster3WestProdName}, nil, "0")
			Eventually(rpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on the newly picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster3WestProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		It("should remove resources from the unpicked clusters", func() {
			checkIfRemovedConfigMapFromMemberClusters([]*framework.Cluster{memberCluster2EastCanary})
		})
	})

	Context("not enough clusters to pick", Ordered, func() {
		It("should create rp with pickN policy requesting more clusters than available", func() {
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
						PlacementType: placementv1beta1.PickNPlacementType,
						// This spec uses an RP of the PickN placement type with the number of
						// target clusters equal to that of all clusters present in the environment.
						//
						// This is necessary as the RP controller reports status for unselected clusters
						// only in a partial manner; specifically, for an RP of the PickN placement with
						// N target clusters but only M matching clusters, only N - M decisions for
						// unselected clusters will be reported in the RP status. To avoid
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
													regionLabelName: regionEast,
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
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster1EastProdName, memberCluster2EastCanaryName}, []string{memberCluster3WestProdName, memberCluster4UnhealthyName, memberCluster5LeftName}, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})
	})

	Context("downscaling to zero", Ordered, func() {
		It("should create rp with pickN policy for downscaling to zero test", func() {
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
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
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, rpKey, rp); err != nil {
					return err
				}

				rp.Spec.Policy.NumberOfClusters = ptr.To(int32(0))
				return hubClient.Update(ctx, rp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to downscale")
		})

		It("should remove resources from the downscaled clusters", func() {
			downscaledClusters := []*framework.Cluster{memberCluster3WestProd, memberCluster2EastCanary}
			checkIfRemovedConfigMapFromMemberClusters(downscaledClusters)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), nil, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})
	})

	Context("picking N clusters with single property sorter", Ordered, func() {
		It("should create rp with pickN policy and single property sorter", func() {
			// Have to add this check in each It() spec, instead of using BeforeAll().
			// Otherwise, the AfterEach() would be skipped too and the namespace does not get cleaned up.
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster1EastProdName, memberCluster2EastCanaryName}, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			targetClusters := []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})
	})

	Context("picking N clusters with multiple property sorters", Ordered, func() {
		It("should create rp with pickN policy and multiple property sorters", func() {
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster3WestProdName, memberCluster2EastCanaryName}, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			targetClusters := []*framework.Cluster{memberCluster3WestProd, memberCluster2EastCanary}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})
	})

	Context("picking N clusters with label selector and property sorter", Ordered, func() {
		It("should create rp with pickN policy, label selector and property sorter", func() {
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
													regionLabelName: regionEast,
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
													envLabelName: envCanary,
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster2EastCanaryName, memberCluster1EastProdName}, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			targetClusters := []*framework.Cluster{memberCluster2EastCanary, memberCluster1EastProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})
	})

	Context("picking N clusters with required and preferred affinity terms", Ordered, func() {
		It("should create rp with pickN policy, required and preferred affinity terms", func() {
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
													envLabelName: envProd,
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
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster3WestProdName}, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			targetClusters := []*framework.Cluster{memberCluster3WestProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})
	})
})
