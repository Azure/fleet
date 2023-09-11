/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package tests

// This test suite features a number of test cases which cover the happy paths of the scheduler
// workflow.

import (
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

var _ = Describe("scheduling CRPs of the PickFixed placement type", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	Context("create a CRP with some valid target clusters", func() {
		targetClusters := []string{
			memberCluster1,
			memberCluster4,
			memberCluster6,
		}

		policySnapshotIdx := 1
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, policySnapshotIdx)

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			Consistently(noBindingCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			policy := &fleetv1beta1.PlacementPolicy{
				PlacementType: fleetv1beta1.PickFixedPlacementType,
				ClusterNames:  targetClusters,
			}

			// Create the CRP.
			crp := &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: defaultResourceSelectors,
					Policy:            policy,
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			crpGeneration := crp.Generation

			// Create the associated policy snapshot.
			policySnapshot := &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policySnapshotName,
					Labels: map[string]string{
						fleetv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
						fleetv1beta1.CRPTrackingLabel:      crpName,
					},
					Annotations: map[string]string{
						fleetv1beta1.CRPGenerationAnnotation: strconv.FormatInt(crpGeneration, 10),
					},
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy:     policy,
					PolicyHash: []byte(policyHash),
				},
			}
			Expect(hubClient.Create(ctx, policySnapshot)).To(Succeed(), "Failed to create policy snapshot")
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedForClustersActual(targetClusters, nilScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(targetClusters, []string{}, policySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report correct policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report correct policy snapshot status")
		})
	})

	Context("add additional valid target clusters", func() {
		targetClusters := []string{
			memberCluster1,
			memberCluster2,
			memberCluster4,
			memberCluster5,
			memberCluster6,
		}

		boundClusters := []string{
			memberCluster1,
			memberCluster4,
			memberCluster6,
		}
		scheduledClusters := []string{
			memberCluster2,
			memberCluster5,
		}

		policySnapshotIdx := 2
		oldPolicySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, policySnapshotIdx-1)
		newPolicySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, policySnapshotIdx)

		BeforeAll(func() {
			// Mark all previously created bindings as bound.
			bindingList := &fleetv1beta1.ClusterResourceBindingList{}
			Expect(hubClient.List(ctx, bindingList)).To(Succeed(), "Failed to list bindings")
			for idx := range bindingList.Items {
				binding := bindingList.Items[idx]
				if binding.Spec.State == fleetv1beta1.BindingStateScheduled {
					binding.Spec.State = fleetv1beta1.BindingStateBound
					Expect(hubClient.Update(ctx, &binding)).To(Succeed(), "Failed to update binding")
				}
			}

			// Update the CRP with new target clusters and refresh scheduling policy snapshots.
			updatePickedFixedCRPWithNewTargetClustersAndRefreshSnapshots(crpName, targetClusters, oldPolicySnapshotName, newPolicySnapshotName)
		})

		It("should create scheduled bindings for newly added valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedForClustersActual(scheduledClusters, nilScoreByCluster, crpName, newPolicySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should update bound bindings for previously added valid target clusters", func() {
			boundBindingsUpdatedActual := boundBindingsUpdatedForClustersActual(boundClusters, nilScoreByCluster, crpName, newPolicySnapshotName)
			Eventually(boundBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(boundBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(targetClusters, []string{}, newPolicySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update policy snapshot status")
		})
	})

	Context("add invalid (unhealthy, or left) and not found target clusters", func() {
		targetClusters := []string{
			memberCluster1,
			memberCluster2,
			memberCluster4,
			memberCluster5,
			memberCluster6,
			memberCluster8,  // An invalid cluster (unhealthy).
			memberCluster9,  // An invalid cluster (left).
			memberCluster10, // A cluster that cannot be found in the fleet.
		}

		validClusters := []string{
			memberCluster1,
			memberCluster2,
			memberCluster4,
			memberCluster5,
			memberCluster6,
		}
		boundClusters := []string{
			memberCluster1,
			memberCluster4,
			memberCluster6,
		}
		scheduledClusters := []string{
			memberCluster2,
			memberCluster5,
		}

		invalidClusters := []string{
			memberCluster8,
			memberCluster9,
			memberCluster10,
		}

		policySnapshotIdx := 3
		oldPolicySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, policySnapshotIdx-1)
		newPolicySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, policySnapshotIdx)

		BeforeAll(func() {
			// Update the CRP with new target clusters and refresh scheduling policy snapshots.
			updatePickedFixedCRPWithNewTargetClustersAndRefreshSnapshots(crpName, targetClusters, oldPolicySnapshotName, newPolicySnapshotName)
		})

		It("should update bound bindings for previously added valid target clusters", func() {
			boundBindingsUpdatedActual := boundBindingsUpdatedForClustersActual(boundClusters, nilScoreByCluster, crpName, newPolicySnapshotName)
			Eventually(boundBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(boundBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should update scheduled bindings for previously added valid target clusters", func() {
			scheduledBindingsUpdatedActual := scheduledBindingsUpdatedForClustersActual(scheduledClusters, nilScoreByCluster, crpName, newPolicySnapshotName)
			Eventually(scheduledBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(scheduledBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should not create bindings for invalid target clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(invalidClusters)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Created a binding for invalid or not found cluster")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Created a binding for invalid or not found cluster")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(validClusters, invalidClusters, newPolicySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update policy snapshot status")
		})
	})

	Context("remove some target clusters (valid + invalid)", func() {
		targetClusters := []string{
			memberCluster2,
			memberCluster5,
			memberCluster6,
			memberCluster8,
			memberCluster10,
		}

		validClusters := []string{
			memberCluster2,
			memberCluster5,
			memberCluster6,
		}
		boundClusters := []string{
			memberCluster6,
		}
		scheduledClusters := []string{
			memberCluster2,
			memberCluster5,
		}

		invalidClusters := []string{
			memberCluster8,
			memberCluster10,
		}

		unscheduledClusters := []string{
			memberCluster1,
			memberCluster4,
		}

		policySnapshotIdx := 4
		oldPolicySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, policySnapshotIdx-1)
		newPolicySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, policySnapshotIdx)

		BeforeAll(func() {
			// Update the CRP with new target clusters and refresh scheduling policy snapshots.
			updatePickedFixedCRPWithNewTargetClustersAndRefreshSnapshots(crpName, targetClusters, oldPolicySnapshotName, newPolicySnapshotName)
		})

		It("should update bound bindings for previously added valid target clusters", func() {
			boundBindingsUpdatedActual := boundBindingsUpdatedForClustersActual(boundClusters, nilScoreByCluster, crpName, newPolicySnapshotName)
			Eventually(boundBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(boundBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should update scheduled bindings for previously added valid target clusters", func() {
			scheduledBindingsUpdatedActual := scheduledBindingsUpdatedForClustersActual(scheduledClusters, nilScoreByCluster, crpName, newPolicySnapshotName)
			Eventually(scheduledBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(scheduledBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should mark bindings as unscheduled for removed valid target clusters", func() {
			unscheduledBindingsCreatedActual := unscheduledBindingsCreatedForClustersActual(unscheduledClusters, nilScoreByCluster, crpName, oldPolicySnapshotName)
			Eventually(unscheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark bindings as unscheduled")
			Consistently(unscheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to mark bindings as unscheduled")
		})

		It("should not create bindings for invalid target clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(invalidClusters)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Created a binding for invalid or not found cluster")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Created a binding for invalid or not found cluster")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(validClusters, invalidClusters, newPolicySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update policy snapshot status")
		})

		AfterAll(func() {
			clearUnscheduledBindings()
		})
	})

	Context("pick a different set of clusters", func() {
		targetClusters := []string{
			memberCluster3,
			memberCluster7,
		}

		validClusters := []string{
			memberCluster3,
			memberCluster7,
		}
		scheduledClusters := []string{
			memberCluster3,
			memberCluster7,
		}

		unscheduledClusters := []string{
			memberCluster2,
			memberCluster5,
			memberCluster6,
		}

		policySnapshotIdx := 5
		oldPolicySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, policySnapshotIdx-1)
		newPolicySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, policySnapshotIdx)

		BeforeAll(func() {
			// Update the CRP with new target clusters and refresh scheduling policy snapshots.
			updatePickedFixedCRPWithNewTargetClustersAndRefreshSnapshots(crpName, targetClusters, oldPolicySnapshotName, newPolicySnapshotName)
		})

		It("should create scheduled bindings for newly added valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedForClustersActual(scheduledClusters, nilScoreByCluster, crpName, newPolicySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not have any bound bindings", func() {
			noBoundBindingsActual := boundBindingsUpdatedForClustersActual([]string{}, nilScoreByCluster, crpName, newPolicySnapshotName)
			Eventually(noBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Unexpected bound bindings are present")
			Consistently(noBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Unexpected bound bindings are present")
		})

		It("should mark bindings as unscheduled for removed valid target clusters", func() {
			unscheduledBindingsCreatedActual := unscheduledBindingsCreatedForClustersActual(unscheduledClusters, nilScoreByCluster, crpName, oldPolicySnapshotName)
			Eventually(unscheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark bindings as unscheduled")
			Consistently(unscheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to mark bindings as unscheduled")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(validClusters, []string{}, newPolicySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update policy snapshot status")
		})
	})

	Context("delete the CRP", func() {
		additionalFinalizer := "test-purpose-finalizer"

		BeforeAll(func() {
			// Retrieve the CRP.
			crp := &fleetv1beta1.ClusterResourcePlacement{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp)).To(Succeed(), "Failed to get CRP")

			// Ensure that the CRP has the scheduler cleanup finalizer.
			Expect(controllerutil.ContainsFinalizer(crp, fleetv1beta1.SchedulerCRPCleanupFinalizer)).To(BeTrue(), "CRP does not have the scheduler cleanup finalizer")

			// Add an additional finalizer to the CRP to block its deletion; this helps to better
			// observe the scheduler's behavior.
			controllerutil.AddFinalizer(crp, additionalFinalizer)
			Expect(hubClient.Update(ctx, crp)).To(Succeed(), "Failed to update CRP")

			// Delete the CRP.
			Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP")
		})

		It("should clear all bindings", func() {
			Eventually(noBindingCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to clear all bindings")
			Consistently(noBindingCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to clear all bindings")
		})

		It("should remove the scheduler cleanup finalizer from the CRP", func() {
			finalizerRemovedActual := crpSchedulerFinalizerRemovedActual(crpName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove scheduler cleanup finalizer from CRP")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPDeletion(crpName)

			// Remove all policy snapshots.
			clearPolicySnapshots()
		})
	})
})
