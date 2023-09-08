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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// Note that this container will run in parallel with other containers.
var _ = Describe("placing resources using a CRP with no placement policy specified", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		// Create the resources.
		createWorkResources()

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector,
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
	})

	It("should place the resources on all member clusters", func() {
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]

			workResourcesPlacedActual := workNamespaceAndDeploymentPlacedOnClusterActual(memberCluster)
			Eventually(workResourcesPlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster")
		}
	})

	It("should update CRP status as expected", func() {
		statusUpdatedActual := crpStatusUpdatedActual()
		Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	It("can delete the CRP", func() {
		// Update the CRP to add a custom finalizer; this would allow us to better observe
		// the behavior of the controllers.
		crp := &placementv1beta1.ClusterResourcePlacement{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp)).To(Succeed(), "Failed to get CRP")

		controllerutil.AddFinalizer(crp, customDeletionBlockerFinalizer)
		Expect(hubClient.Update(ctx, crp)).To(Succeed(), "Failed to update CRP")

		// Delete the CRP.
		Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP")
	})

	It("should remove placed resources from all member clusters", func() {
		for idx := range allMemberClusters {
			memberCluster := allMemberClusters[idx]

			workResourcesRemovedActual := workNamespaceAndDeploymentRemovedFromClusterActual(memberCluster)
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

		crp.Finalizers = []string{}
		Expect(hubClient.Update(ctx, crp)).To(Succeed(), "Failed to update CRP")

		// Wait until the CRP is removed.
		removedActual := crpRemovedActual()
		Eventually(removedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove CRP")

		// Delete the created resources.
		deleteWorkResources()
	})
})
