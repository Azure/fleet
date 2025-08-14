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
	"math"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

var _ = Describe("validating CRP when using customized resourceSnapshotCreationMinimumInterval and resourceChangesCollectionDuration", Label("custom"), Ordered, func() {
	// skip entire suite if interval is zero
	BeforeAll(func() {
		if resourceSnapshotCreationMinimumInterval == 0 && resourceChangesCollectionDuration == 0 {
			Skip("Skipping customized-config placement test when RESOURCE_SNAPSHOT_CREATION_MINIMUM_INTERVAL=0m and RESOURCE_CHANGES_COLLECTION_DURATION=0m")
		}
	})

	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
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
				ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
					{
						Group:   "",
						Kind:    "Namespace",
						Version: "v1",
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								workNamespaceLabelName: fmt.Sprintf("test-%d", GinkgoParallelProcess()),
							},
						},
					},
				},
				Strategy: placementv1beta1.RolloutStrategy{
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(5),
					},
				},
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("garbage all things related to placement %s", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual([]placementv1beta1.ResourceIdentifier{}, allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should not place work resources on member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("updating the resources on the hub and the namespace becomes selected", func() {
		workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		ns := &corev1.Namespace{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: workNamespaceName}, ns)).Should(Succeed(), "Failed to get the namespace %s", workNamespaceName)
		ns.Labels = map[string]string{
			workNamespaceLabelName: fmt.Sprintf("test-%d", GinkgoParallelProcess()),
		}
		Expect(hubClient.Update(ctx, ns)).Should(Succeed(), "Failed to update namespace %s", workNamespaceName)
	})

	It("should not update CRP status immediately", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual([]placementv1beta1.ResourceIdentifier{}, allMemberClusterNames, nil, "0")
		Consistently(crpStatusUpdatedActual, resourceSnapshotDelayDuration-3*time.Second, consistentlyInterval).Should(Succeed(), "CRP %s status should be unchanged", crpName)
	})

	It("should update CRP status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "1")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("validating the clusterResourceSnapshots are created", func() {
		var resourceSnapshotList placementv1beta1.ClusterResourceSnapshotList
		masterResourceSnapshotLabels := client.MatchingLabels{
			placementv1beta1.PlacementTrackingLabel: crpName,
		}
		Expect(hubClient.List(ctx, &resourceSnapshotList, masterResourceSnapshotLabels)).Should(Succeed(), "Failed to list ClusterResourceSnapshots for CRP %s", crpName)
		Expect(len(resourceSnapshotList.Items)).Should(Equal(2), "Expected 2 ClusterResourceSnapshots for CRP %s, got %d", crpName, len(resourceSnapshotList.Items))
		// Use math.Abs to get the absolute value of the time difference in seconds.
		snapshotDiffInSeconds := resourceSnapshotList.Items[0].CreationTimestamp.Time.Sub(resourceSnapshotList.Items[1].CreationTimestamp.Time).Seconds()
		diff := math.Abs(snapshotDiffInSeconds)
		Expect(time.Duration(diff)*time.Second >= resourceSnapshotDelayDuration).To(BeTrue(), "The time difference between ClusterResourceSnapshots should be more than resourceSnapshotDelayDuration")
	})

	It("can delete the CRP", func() {
		// Delete the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
		}
		Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", crpName)
	})

	It("should remove placed resources from all member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual(crpName)
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})

var _ = Describe("validating that CRP status can be updated after updating the resources when using customized resourceSnapshotCreationMinimumInterval and resourceChangesCollectionDuration", Label("custom"), Ordered, func() {
	// skip entire suite if interval is zero
	BeforeAll(func() {
		if resourceSnapshotCreationMinimumInterval == 0 && resourceChangesCollectionDuration == 0 {
			Skip("Skipping customized-config placement test when RESOURCE_SNAPSHOT_CREATION_MINIMUM_INTERVAL=0m and RESOURCE_CHANGES_COLLECTION_DURATION=0m")
		}
	})

	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
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
				ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
					{
						Group:   "",
						Kind:    "Namespace",
						Version: "v1",
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								workNamespaceLabelName: fmt.Sprintf("test-%d", GinkgoParallelProcess()),
							},
						},
					},
				},
				Strategy: placementv1beta1.RolloutStrategy{
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(5),
					},
				},
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("garbage all things related to placement %s", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("validating the clusterResourceSnapshots are created", func() {
		Eventually(func() error {
			var resourceSnapshotList placementv1beta1.ClusterResourceSnapshotList
			masterResourceSnapshotLabels := client.MatchingLabels{
				placementv1beta1.PlacementTrackingLabel: crpName,
			}
			if err := hubClient.List(ctx, &resourceSnapshotList, masterResourceSnapshotLabels); err != nil {
				return fmt.Errorf("failed to list ClusterResourceSnapshots for CRP %s: %w", crpName, err)
			}
			if len(resourceSnapshotList.Items) != 1 {
				return fmt.Errorf("got %d ClusterResourceSnapshot for CRP %s, want 1", len(resourceSnapshotList.Items), crpName)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to wait for ClusterResourceSnapshots to be created")
	})

	It("updating the resources on the hub and the namespace becomes selected", func() {
		workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
		ns := &corev1.Namespace{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: workNamespaceName}, ns)).Should(Succeed(), "Failed to get the namespace %s", workNamespaceName)
		ns.Labels = map[string]string{
			workNamespaceLabelName: fmt.Sprintf("test-%d", GinkgoParallelProcess()),
		}
		Expect(hubClient.Update(ctx, ns)).Should(Succeed(), "Failed to update namespace %s", workNamespaceName)
	})

	It("should update CRP status for snapshot 0 as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual([]placementv1beta1.ResourceIdentifier{}, allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should update CRP status for snapshot 1 as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "1")
		Eventually(crpStatusUpdatedActual, resourceSnapshotDelayDuration+eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
	})

	It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("validating the clusterResourceSnapshots are created", func() {
		var resourceSnapshotList placementv1beta1.ClusterResourceSnapshotList
		masterResourceSnapshotLabels := client.MatchingLabels{
			placementv1beta1.PlacementTrackingLabel: crpName,
		}
		Expect(hubClient.List(ctx, &resourceSnapshotList, masterResourceSnapshotLabels)).Should(Succeed(), "Failed to list ClusterResourceSnapshots for CRP %s", crpName)
		Expect(len(resourceSnapshotList.Items)).Should(Equal(2), "Expected 2 ClusterResourceSnapshots for CRP %s, got %d", crpName, len(resourceSnapshotList.Items))
		// Use math.Abs to get the absolute value of the time difference in seconds.
		snapshotDiffInSeconds := resourceSnapshotList.Items[0].CreationTimestamp.Time.Sub(resourceSnapshotList.Items[1].CreationTimestamp.Time).Seconds()
		diff := math.Abs(snapshotDiffInSeconds)
		Expect(time.Duration(diff)*time.Second >= resourceSnapshotDelayDuration).To(BeTrue(), "The time difference between ClusterResourceSnapshots should be more than resourceSnapshotDelayDuration")
	})

	It("can delete the CRP", func() {
		// Delete the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
		}
		Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", crpName)
	})

	It("should remove placed resources from all member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("should remove controller finalizers from CRP", func() {
		finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromCRPActual(crpName)
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
	})
})
