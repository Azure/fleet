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

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = Describe("placing resources using a CRP of PickFixed placement type", func() {
	Context("pick some clusters", Ordered, func() {
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
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames: []string{
							memberCluster1EastProdName,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1EastProdName}, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on specified clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on specified clusters")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, []*framework.Cluster{memberCluster1EastProd})
		})
	})

	Context("refreshing target clusters", Ordered, func() {
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
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames: []string{
							memberCluster1EastProdName,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Verify that resources are placed on specified clusters.
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on specified clusters")

			// Update the CRP to pick a different cluster.
			Eventually(func() error {
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				crp.Spec.Policy.ClusterNames = []string{memberCluster2EastCanaryName}
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster2EastCanaryName}, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on newly specified clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster2EastCanary)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on specified clusters")
		})

		It("should remove resources from previously specified clusters", func() {
			checkIfRemovedWorkResourcesFromMemberClusters([]*framework.Cluster{memberCluster1EastProd})
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, []*framework.Cluster{memberCluster2EastCanary})
		})
	})

	Context("pick unhealthy and non-existent clusters", Ordered, func() {
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
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, []string{memberCluster4UnhealthyName, memberCluster5LeftName, memberCluster6NonExistentName}, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, []*framework.Cluster{})
		})
	})
})
