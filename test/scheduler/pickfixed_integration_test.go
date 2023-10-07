/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package tests

// This test suite features a number of test cases which cover the workflow of scheduling CRPs
// of the PickFixed placement type.

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("scheduling CRPs of the PickFixed placement type", func() {
	Context("with valid target clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		targetClusters := []string{
			memberCluster1EastProd,
			memberCluster4CentralProd,
			memberCluster6WestProd,
		}

		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create the CRP and its associated policy snapshot.
			createPickFixedCRPWithPolicySnapshot(crpName, targetClusters, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(targetClusters, nilScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(targetClusters, []string{}, policySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report correct policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report correct policy snapshot status")
		})

		AfterAll(func() {
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	Context("with both valid and invalid/non-existent target clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		targetClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster4CentralProd,
			memberCluster5CentralProd,
			memberCluster6WestProd,
			memberCluster8UnhealthyEastProd, // An invalid cluster (unhealthy).
			memberCluster9LeftCentralProd,   // An invalid cluster (left).
			memberCluster10NonExistent,      // A cluster that cannot be found in the fleet.
		}
		validClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster4CentralProd,
			memberCluster5CentralProd,
			memberCluster6WestProd,
		}
		invalidClusters := []string{
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
			memberCluster10NonExistent,
		}

		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create the CRP and its associated policy snapshot.
			createPickFixedCRPWithPolicySnapshot(crpName, targetClusters, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(validClusters, nilScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create bindings for invalid target clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(invalidClusters, crpName)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Created a binding for invalid or not found cluster")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Created a binding for invalid or not found cluster")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(validClusters, invalidClusters, policySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update policy snapshot status")
		})

		AfterAll(func() {
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	Context("policy snapshot refresh with added clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		targetClusters1 := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster4CentralProd,
		}
		targetClusters2 := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster4CentralProd,
			memberCluster5CentralProd,
			memberCluster6WestProd,
		}
		previouslyBoundClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
		}
		previouslyScheduledClusters := []string{
			memberCluster4CentralProd,
		}
		newScheduledClusters := []string{
			memberCluster5CentralProd,
			memberCluster6WestProd,
		}

		policySnapshotName1 := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)
		policySnapshotName2 := fmt.Sprintf(policySnapshotNameTemplate, crpName, 2)

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create the CRP and its associated policy snapshot.
			createPickFixedCRPWithPolicySnapshot(crpName, targetClusters1, policySnapshotName1)

			// Make sure that the bindings have been created.
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(targetClusters1, nilScoreByCluster, crpName, policySnapshotName1)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")

			// Mark all previously created bindings as bound.
			markBindingsAsBoundForClusters(crpName, previouslyBoundClusters)

			// Update the CRP with new target clusters and refresh scheduling policy snapshots.
			updatePickFixedCRPWithNewTargetClustersAndRefreshSnapshots(crpName, targetClusters2, policySnapshotName1, policySnapshotName2)
		})

		It("should create scheduled bindings for newly added valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(newScheduledClusters, nilScoreByCluster, crpName, policySnapshotName2)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should update bound bindings for previously added valid target clusters", func() {
			boundBindingsUpdatedActual := boundBindingsCreatedOrUpdatedForClustersActual(previouslyBoundClusters, nilScoreByCluster, crpName, policySnapshotName2)
			Eventually(boundBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(boundBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should update scheduled bindings for previously added valid target clusters", func() {
			scheduledBindingsUpdatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(previouslyScheduledClusters, nilScoreByCluster, crpName, policySnapshotName2)
			Eventually(scheduledBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(scheduledBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(targetClusters2, []string{}, policySnapshotName2)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update policy snapshot status")
		})

		AfterAll(func() {
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	Context("policy snapshot refresh with removed clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		targetClusters1 := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster4CentralProd,
		}
		targetClusters2 := []string{
			memberCluster5CentralProd,
			memberCluster6WestProd,
		}
		previouslyBoundClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
		}
		scheduledClusters := []string{
			memberCluster5CentralProd,
			memberCluster6WestProd,
		}
		unscheduledClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster4CentralProd,
		}

		policySnapshotName1 := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)
		policySnapshotName2 := fmt.Sprintf(policySnapshotNameTemplate, crpName, 2)

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create the CRP and its associated policy snapshot.
			createPickFixedCRPWithPolicySnapshot(crpName, targetClusters1, policySnapshotName1)

			// Make sure that the bindings have been created.
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(targetClusters1, nilScoreByCluster, crpName, policySnapshotName1)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")

			// Mark some previously created bindings as bound.
			markBindingsAsBoundForClusters(crpName, previouslyBoundClusters)

			// Update the CRP with new target clusters and refresh scheduling policy snapshots.
			updatePickFixedCRPWithNewTargetClustersAndRefreshSnapshots(crpName, targetClusters2, policySnapshotName1, policySnapshotName2)
		})

		It("should create scheduled bindings for newly added valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(scheduledClusters, nilScoreByCluster, crpName, policySnapshotName2)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should mark bindings as unscheduled for removed target clusters", func() {
			unscheduledBindingsCreatedActual := unscheduledBindingsCreatedOrUpdatedForClustersActual(unscheduledClusters, nilScoreByCluster, crpName, policySnapshotName1)
			Eventually(unscheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark bindings as unscheduled")
			Consistently(unscheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to mark bindings as unscheduled")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(scheduledClusters, []string{}, policySnapshotName2)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update policy snapshot status")
		})

		AfterAll(func() {
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})
})
