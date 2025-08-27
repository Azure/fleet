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

var _ = Describe("validating RP when using customized resourceSnapshotCreationMinimumInterval and resourceChangesCollectionDuration", Label("custom"), Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())

	// skip entire suite if interval is zero
	BeforeEach(OncePerOrdered, func() {
		if resourceSnapshotCreationMinimumInterval == 0 && resourceChangesCollectionDuration == 0 {
			Skip("Skipping customized-config placement test when RESOURCE_SNAPSHOT_CREATION_MINIMUM_INTERVAL=0m and RESOURCE_CHANGES_COLLECTION_DURATION=0m")
		}

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

		By("should update CRP status as expected")
		crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")

		By("creating RP")
		rp := &placementv1beta1.ResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name:       rpName,
				Namespace:  appNamespace().Name,
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
					{
						Group:   "",
						Kind:    "ConfigMap",
						Version: "v1",
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								workNamespaceLabelName: fmt.Sprintf("test-%d", GinkgoParallelProcess()),
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

	AfterEach(OncePerOrdered, func() {
		ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}, allMemberClusters)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("validating RP status and should not update immediately", func() {
		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual([]placementv1beta1.ResourceIdentifier{}, allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should not place work resources on member clusters", checkIfRemovedConfigMapFromAllMemberClusters)

		It("updating the resources on the hub", func() {
			configMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
			configMap := &corev1.ConfigMap{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: appNamespace().Name}, configMap)).Should(Succeed(), "Failed to get the configMap %s", configMapName)
			configMap.Labels = map[string]string{
				workNamespaceLabelName: fmt.Sprintf("test-%d", GinkgoParallelProcess()),
			}
			Expect(hubClient.Update(ctx, configMap)).Should(Succeed(), "Failed to update configMap %s", configMapName)

		})

		It("should not update RP status immediately", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual([]placementv1beta1.ResourceIdentifier{}, allMemberClusterNames, nil, "0")
			Consistently(rpStatusUpdatedActual, resourceSnapshotDelayDuration-3*time.Second, consistentlyInterval).Should(Succeed(), "RP %s status should be unchanged", rpName)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "1")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("validating the resourceSnapshots are created", func() {
			var resourceSnapshotList placementv1beta1.ResourceSnapshotList
			masterResourceSnapshotLabels := client.MatchingLabels{
				placementv1beta1.PlacementTrackingLabel: rpName,
			}
			Expect(hubClient.List(ctx, &resourceSnapshotList, masterResourceSnapshotLabels, client.InNamespace(appNamespace().Name))).Should(Succeed(), "Failed to list ResourceSnapshots for RP %s", rpName)
			Expect(len(resourceSnapshotList.Items)).Should(Equal(2), "Expected 2 ResourceSnapshots for RP %s, got %d", rpName, len(resourceSnapshotList.Items))
			// Use math.Abs to get the absolute value of the time difference in seconds.
			snapshotDiffInSeconds := resourceSnapshotList.Items[0].CreationTimestamp.Time.Sub(resourceSnapshotList.Items[1].CreationTimestamp.Time).Seconds()
			diff := math.Abs(snapshotDiffInSeconds)
			Expect(time.Duration(diff)*time.Second >= resourceSnapshotDelayDuration).To(BeTrue(), "The time difference between ResourceSnapshots should be more than resourceSnapshotDelayDuration")
		})
	})

	Context("validating that RP status can be updated after updating the resources", func() {
		It("validating the resourceSnapshots are created", func() {
			Eventually(func() error {
				var resourceSnapshotList placementv1beta1.ResourceSnapshotList
				masterResourceSnapshotLabels := client.MatchingLabels{
					placementv1beta1.PlacementTrackingLabel: rpName,
				}
				if err := hubClient.List(ctx, &resourceSnapshotList, masterResourceSnapshotLabels, client.InNamespace(appNamespace().Name)); err != nil {
					return fmt.Errorf("failed to list ResourceSnapshots for RP %s: %w", rpName, err)
				}
				if len(resourceSnapshotList.Items) != 1 {
					return fmt.Errorf("got %d ResourceSnapshot for RP %s, want 1", len(resourceSnapshotList.Items), rpName)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to wait for ResourceSnapshots to be created")
		})

		It("updating the resources on the hub", func() {
			configMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
			configMap := &corev1.ConfigMap{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: appNamespace().Name}, configMap)).Should(Succeed(), "Failed to get the configMap %s", configMapName)
			configMap.Labels = map[string]string{
				workNamespaceLabelName: fmt.Sprintf("test-%d", GinkgoParallelProcess()),
			}
			Expect(hubClient.Update(ctx, configMap)).Should(Succeed(), "Failed to update configMap %s", configMapName)

		})

		It("should update RP status for snapshot 0 as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual([]placementv1beta1.ResourceIdentifier{}, allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should update RP status for snapshot 1 as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "1")
			Eventually(rpStatusUpdatedActual, resourceSnapshotDelayDuration+eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should place the selected resources on member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("validating the resourceSnapshots are created", func() {
			var resourceSnapshotList placementv1beta1.ResourceSnapshotList
			masterResourceSnapshotLabels := client.MatchingLabels{
				placementv1beta1.PlacementTrackingLabel: rpName,
			}
			Expect(hubClient.List(ctx, &resourceSnapshotList, masterResourceSnapshotLabels, client.InNamespace(appNamespace().Name))).Should(Succeed(), "Failed to list ResourceSnapshots for RP %s", rpName)
			Expect(len(resourceSnapshotList.Items)).Should(Equal(2), "Expected 2 ResourceSnapshots for RP %s, got %d", rpName, len(resourceSnapshotList.Items))
			// Use math.Abs to get the absolute value of the time difference in seconds.
			snapshotDiffInSeconds := resourceSnapshotList.Items[0].CreationTimestamp.Time.Sub(resourceSnapshotList.Items[1].CreationTimestamp.Time).Seconds()
			diff := math.Abs(snapshotDiffInSeconds)
			Expect(time.Duration(diff)*time.Second >= resourceSnapshotDelayDuration).To(BeTrue(), "The time difference between ResourceSnapshots should be more than resourceSnapshotDelayDuration")
		})
	})
})
