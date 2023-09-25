/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// Note that this container will run in parallel with other containers.
var _ = Describe("placing resources using a CRP with no placement policy specified", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	appConfigMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

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
						UnavailablePeriodSeconds: pointer.Int(2),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
	})

	It("should place the resources on all member clusters", func() {
		for idx := range allMemberClusters {
			workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(allMemberClusters[idx])
			Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster")
		}
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual()
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("can update the resource", func() {
		// Get the config map.
		configMap := &corev1.ConfigMap{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: workNamespaceName, Name: appConfigMapName}, configMap)).To(Succeed(), "Failed to get config map")

		configMap.Data = map[string]string{
			"data": "updated",
		}
		Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update config map")
	})

	It("should place the resources on all member clusters", func() {
		for idx := range allMemberClusters {
			workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(allMemberClusters[idx])
			Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster")
		}
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual()
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("can delete the CRP", func() {
		// Delete the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
		}
		Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP")
	})

	It("should remove placed resources from all member clusters", func() {
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]

			workResourcesRemovedActual := workNamespaceRemovedFromClusterActual(memberCluster)
			Eventually(workResourcesRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work resources from member cluster")
		}
	})

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual()
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP")
	})

	AfterAll(func() {
		// Remove the custom deletion blocker finalizer from the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp)).To(Succeed(), "Failed to get CRP")

		// Delete the CRP (again, if applicable).
		//
		// This helps the AfterAll node to run successfully even if the steps above fail early.
		Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP")

		crp.Finalizers = []string{}
		Expect(hubClient.Update(ctx, crp)).To(Succeed(), "Failed to update CRP")

		// Wait until the CRP is removed.
		removedActual := crpRemovedActual()
		Eventually(removedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove CRP")

		// Delete the created resources.
		cleanupWorkResources()
	})
})
