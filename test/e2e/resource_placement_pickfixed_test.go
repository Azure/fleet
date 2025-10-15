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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

var _ = Describe("placing namespaced scoped resources using an RP with PickFixed policy", Label("resourceplacement"), func() {
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

	Context("pick some clusters", Ordered, func() {
		It("should create rp with pickFixed policy successfully", func() {
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
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames: []string{
							memberCluster1EastProdName,
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
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster1EastProdName}, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
		})
	})

	Context("refreshing target clusters", Ordered, func() {
		It("should should create an RP with pickFixed policy successfully", func() {
			// Create the RP in the same namespace selecting namespaced resources.
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
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames: []string{
							memberCluster1EastProdName,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should place resources on the specified clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on specified clusters")
		})

		It("update RP to pick a different cluster", func() {
			rp := &placementv1beta1.ResourcePlacement{}
			Eventually(func() error {
				if err := hubClient.Get(ctx, rpKey, rp); err != nil {
					return err
				}
				rp.Spec.Policy.ClusterNames = []string{memberCluster2EastCanaryName}
				return hubClient.Update(ctx, rp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster2EastCanaryName}, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on newly specified clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster2EastCanary)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on specified clusters")
		})

		It("should remove resources from previously specified clusters", func() {
			checkIfRemovedConfigMapFromMemberClusters([]*framework.Cluster{memberCluster1EastProd})
		})
	})

	Context("pick unhealthy and non-existent clusters", Ordered, func() {
		It("should create RP with pickFixed policy targeting unhealthy and non-existent clusters", func() {
			// Create the RP in the same namespace selecting namespaced resources.
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
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames: []string{
							memberCluster4UnhealthyName,
							memberCluster5LeftName,
							memberCluster6NonExistentName,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), nil, []string{memberCluster4UnhealthyName, memberCluster5LeftName, memberCluster6NonExistentName}, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})
	})

	Context("switch to another cluster to simulate stuck deleting works", Ordered, func() {
		workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		appConfigMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
		var currentConfigMap corev1.ConfigMap

		It("should create RP with pickFixed policy successfully", func() {
			// Create the RP in the same namespace selecting namespaced resources.
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
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames: []string{
							memberCluster1EastProdName,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster1EastProdName}, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place resources on specified clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on specified clusters")
		})

		It("can add finalizer to work resources on the specified clusters", func() {
			Eventually(func() error {
				if err := memberCluster1EastProd.KubeClient.Get(ctx, types.NamespacedName{Namespace: workNamespaceName, Name: appConfigMapName}, &currentConfigMap); err != nil {
					return err
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get configmap")
			// Add finalizer to block deletion to simulate work stuck
			controllerutil.AddFinalizer(&currentConfigMap, "example.com/finalizer")
			Expect(memberCluster1EastProd.KubeClient.Update(ctx, &currentConfigMap)).To(Succeed(), "Failed to update configmap with finalizer")
		})

		It("update RP to pick another cluster", func() {
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, rpKey, rp); err != nil {
					return err
				}
				rp.Spec.Policy.ClusterNames = []string{memberCluster2EastCanaryName}
				return hubClient.Update(ctx, rp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP")
		})

		It("should update RP status as expected", func() {
			// should successfully apply to the new cluster
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster2EastCanaryName}, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should have a deletion timestamp on work objects", func() {
			// Use an Eventually block as the Fleet controllers might not be fast enough.
			//
			// Note that the CRP controller will ignore deleted bindings when reporting status.
			Eventually(func() error {
				work := &placementv1beta1.Work{}
				reservedMemberNSName := fmt.Sprintf("fleet-member-%s", memberCluster1EastProdName)
				workName := fmt.Sprintf("%s.%s-work", rpKey.Namespace, rpName)
				if err := hubClient.Get(ctx, types.NamespacedName{Namespace: reservedMemberNSName, Name: workName}, work); err != nil {
					return fmt.Errorf("failed to get work: %w", err)
				}

				if work.DeletionTimestamp == nil {
					return fmt.Errorf("work %s in namespace %s has not yet been marked for deletion", workName, reservedMemberNSName)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to verify that work has been marked for deletion")
		})

		It("configmap should still exists on previously specified cluster and be in deleting state", func() {
			// Use an Eventually block as the Fleet agent might not be fast enough.
			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				if err := memberCluster1EastProd.KubeClient.Get(ctx, types.NamespacedName{Namespace: workNamespaceName, Name: appConfigMapName}, configMap); err != nil {
					return err
				}
				if configMap.DeletionTimestamp == nil {
					return fmt.Errorf("configMap %s has not yet been marked for deletion", appConfigMapName)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to verify that configMap has been marked for deletion")
		})

		It("can remove finalizer from work resources on the specified clusters", func() {
			configMap := &corev1.ConfigMap{}
			Expect(memberCluster1EastProd.KubeClient.Get(ctx, types.NamespacedName{Namespace: workNamespaceName, Name: appConfigMapName}, configMap)).Should(Succeed(), "Failed to get configmap")
			controllerutil.RemoveFinalizer(configMap, "example.com/finalizer")
			Expect(memberCluster1EastProd.KubeClient.Update(ctx, configMap)).To(Succeed(), "Failed to update configmap with finalizer")
		})

		It("should remove resources from previously specified clusters", func() {
			checkIfRemovedConfigMapFromMemberClusters([]*framework.Cluster{memberCluster1EastProd})
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), []string{memberCluster2EastCanaryName}, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})
	})
})
