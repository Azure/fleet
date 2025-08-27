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

package tests

// This test suite features a number of test cases which cover the workflow of scheduling CRPs
// of the PickFixed placement type.

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("scheduling CRPs of the PickFixed placement type", func() {
	Context("with valid target clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpKey := types.NamespacedName{Name: crpName}

		targetClusters := []string{
			memberCluster1EastProd,
			memberCluster4CentralProd,
			memberCluster6WestProd,
		}

		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(crpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create the CRP and its associated policy snapshot.
			createPickFixedCRPWithPolicySnapshot(crpName, targetClusters, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(crpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(targetClusters, nilScoreByCluster, crpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(targetClusters, []string{}, types.NamespacedName{Name: policySnapshotName})
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report correct policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report correct policy snapshot status")
		})

		AfterAll(func() {
			ensurePlacementAndAllRelatedResourcesDeletion(crpKey)
		})
	})

	Context("with both valid and invalid/non-existent target clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpKey := types.NamespacedName{Name: crpName}

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
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(crpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create the CRP and its associated policy snapshot.
			createPickFixedCRPWithPolicySnapshot(crpName, targetClusters, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(crpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(validClusters, nilScoreByCluster, crpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create bindings for invalid target clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(invalidClusters, crpKey)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Created a binding for invalid or not found cluster")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Created a binding for invalid or not found cluster")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(validClusters, invalidClusters, types.NamespacedName{Name: policySnapshotName})
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update policy snapshot status")
		})

		AfterAll(func() {
			ensurePlacementAndAllRelatedResourcesDeletion(crpKey)
		})
	})

	Context("policy snapshot refresh with added clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpKey := types.NamespacedName{Name: crpName}

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
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(crpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create the CRP and its associated policy snapshot.
			createPickFixedCRPWithPolicySnapshot(crpName, targetClusters1, policySnapshotName1)

			// Make sure that the bindings have been created.
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(targetClusters1, nilScoreByCluster, crpKey, policySnapshotName1)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")

			// Mark all previously created bindings as bound.
			markBindingsAsBoundForClusters(crpKey, previouslyBoundClusters)

			// Update the CRP with new target clusters and refresh scheduling policy snapshots.
			updatePickFixedCRPWithNewTargetClustersAndRefreshSnapshots(crpName, targetClusters2, policySnapshotName1, policySnapshotName2)
		})

		It("should create scheduled bindings for newly added valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(newScheduledClusters, nilScoreByCluster, crpKey, policySnapshotName2)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should update bound bindings for previously added valid target clusters", func() {
			boundBindingsUpdatedActual := boundBindingsCreatedOrUpdatedForClustersActual(previouslyBoundClusters, nilScoreByCluster, crpKey, policySnapshotName2)
			Eventually(boundBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(boundBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should update scheduled bindings for previously added valid target clusters", func() {
			scheduledBindingsUpdatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(previouslyScheduledClusters, nilScoreByCluster, crpKey, policySnapshotName2)
			Eventually(scheduledBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(scheduledBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(targetClusters2, []string{}, types.NamespacedName{Name: policySnapshotName2})
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update policy snapshot status")
		})

		AfterAll(func() {
			ensurePlacementAndAllRelatedResourcesDeletion(crpKey)
		})
	})

	Context("policy snapshot refresh with removed clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpKey := types.NamespacedName{Name: crpName}

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
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(crpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create the CRP and its associated policy snapshot.
			createPickFixedCRPWithPolicySnapshot(crpName, targetClusters1, policySnapshotName1)

			// Make sure that the bindings have been created.
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(targetClusters1, nilScoreByCluster, crpKey, policySnapshotName1)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")

			// Mark some previously created bindings as bound.
			markBindingsAsBoundForClusters(crpKey, previouslyBoundClusters)

			// Update the CRP with new target clusters and refresh scheduling policy snapshots.
			updatePickFixedCRPWithNewTargetClustersAndRefreshSnapshots(crpName, targetClusters2, policySnapshotName1, policySnapshotName2)
		})

		It("should create scheduled bindings for newly added valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(scheduledClusters, nilScoreByCluster, crpKey, policySnapshotName2)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should mark bindings as unscheduled for removed target clusters", func() {
			unscheduledBindingsCreatedActual := unscheduledBindingsCreatedOrUpdatedForClustersActual(unscheduledClusters, nilScoreByCluster, crpKey, policySnapshotName1)
			Eventually(unscheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark bindings as unscheduled")
			Consistently(unscheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to mark bindings as unscheduled")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(scheduledClusters, []string{}, types.NamespacedName{Name: policySnapshotName2})
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update policy snapshot status")
		})

		AfterAll(func() {
			ensurePlacementAndAllRelatedResourcesDeletion(crpKey)
		})
	})
})

var _ = Describe("scheduling RPs of the PickFixed placement type", func() {
	Context("with valid target clusters", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		rpKey := types.NamespacedName{Namespace: testNamespace, Name: rpName}

		targetClusters := []string{
			memberCluster1EastProd,
			memberCluster4CentralProd,
			memberCluster6WestProd,
		}

		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, rpName, 1)
		policySnapshotKey := types.NamespacedName{Namespace: testNamespace, Name: policySnapshotName}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(rpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create the RP and its associated policy snapshot.
			createPickFixedRPWithPolicySnapshot(testNamespace, rpName, targetClusters, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the RP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(rpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to RP")
		})

		It("should create scheduled bindings for valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(targetClusters, nilScoreByCluster, rpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(targetClusters, []string{}, policySnapshotKey)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report correct policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report correct policy snapshot status")
		})

		AfterAll(func() {
			ensurePlacementAndAllRelatedResourcesDeletion(rpKey)
		})
	})

	Context("with both valid and invalid/non-existent target clusters", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		rpKey := types.NamespacedName{Namespace: testNamespace, Name: rpName}

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

		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, rpName, 1)
		policySnapshotKey := types.NamespacedName{Namespace: testNamespace, Name: policySnapshotName}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(rpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create the RP and its associated policy snapshot.
			createPickFixedRPWithPolicySnapshot(testNamespace, rpName, targetClusters, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the RP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(rpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to RP")
		})

		It("should create scheduled bindings for valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(validClusters, nilScoreByCluster, rpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create bindings for invalid target clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(invalidClusters, rpKey)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Created a binding for invalid or not found cluster")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Created a binding for invalid or not found cluster")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(validClusters, invalidClusters, policySnapshotKey)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update policy snapshot status")
		})

		AfterAll(func() {
			ensurePlacementAndAllRelatedResourcesDeletion(rpKey)
		})
	})

	Context("policy snapshot refresh with added clusters", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		rpKey := types.NamespacedName{Namespace: testNamespace, Name: rpName}

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

		policySnapshotName1 := fmt.Sprintf(policySnapshotNameTemplate, rpName, 1)
		policySnapshotName2 := fmt.Sprintf(policySnapshotNameTemplate, rpName, 2)
		policySnapshotKey2 := types.NamespacedName{Namespace: testNamespace, Name: policySnapshotName2}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(rpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create the RP and its associated policy snapshot.
			createPickFixedRPWithPolicySnapshot(testNamespace, rpName, targetClusters1, policySnapshotName1)

			// Make sure that the bindings have been created.
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(targetClusters1, nilScoreByCluster, rpKey, policySnapshotName1)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")

			// Mark all previously created bindings as bound.
			markBindingsAsBoundForClusters(rpKey, previouslyBoundClusters)

			// Update the CRP with new target clusters and refresh scheduling policy snapshots.
			updatePickFixedRPWithNewTargetClustersAndRefreshSnapshots(testNamespace, rpName, targetClusters2, policySnapshotName1, policySnapshotName2)
		})

		It("should create scheduled bindings for newly added valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(newScheduledClusters, nilScoreByCluster, rpKey, policySnapshotName2)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should update bound bindings for previously added valid target clusters", func() {
			boundBindingsUpdatedActual := boundBindingsCreatedOrUpdatedForClustersActual(previouslyBoundClusters, nilScoreByCluster, rpKey, policySnapshotName2)
			Eventually(boundBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(boundBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should update scheduled bindings for previously added valid target clusters", func() {
			scheduledBindingsUpdatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(previouslyScheduledClusters, nilScoreByCluster, rpKey, policySnapshotName2)
			Eventually(scheduledBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(scheduledBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(targetClusters2, []string{}, policySnapshotKey2)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update policy snapshot status")
		})

		AfterAll(func() {
			ensurePlacementAndAllRelatedResourcesDeletion(rpKey)
		})
	})

	Context("policy snapshot refresh with removed clusters", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		rpKey := types.NamespacedName{Namespace: testNamespace, Name: rpName}

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

		policySnapshotName1 := fmt.Sprintf(policySnapshotNameTemplate, rpName, 1)
		policySnapshotName2 := fmt.Sprintf(policySnapshotNameTemplate, rpName, 2)
		policySnapshotKey2 := types.NamespacedName{Namespace: testNamespace, Name: policySnapshotName2}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(rpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create the RP and its associated policy snapshot.
			createPickFixedRPWithPolicySnapshot(testNamespace, rpName, targetClusters1, policySnapshotName1)

			// Make sure that the bindings have been created.
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(targetClusters1, nilScoreByCluster, rpKey, policySnapshotName1)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")

			// Mark some previously created bindings as bound.
			markBindingsAsBoundForClusters(rpKey, previouslyBoundClusters)

			// Update the RP with new target clusters and refresh scheduling policy snapshots.
			updatePickFixedRPWithNewTargetClustersAndRefreshSnapshots(testNamespace, rpName, targetClusters2, policySnapshotName1, policySnapshotName2)
		})

		It("should create scheduled bindings for newly added valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(scheduledClusters, nilScoreByCluster, rpKey, policySnapshotName2)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should mark bindings as unscheduled for removed target clusters", func() {
			unscheduledBindingsCreatedActual := unscheduledBindingsCreatedOrUpdatedForClustersActual(unscheduledClusters, nilScoreByCluster, rpKey, policySnapshotName1)
			Eventually(unscheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark bindings as unscheduled")
			Consistently(unscheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to mark bindings as unscheduled")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(scheduledClusters, []string{}, policySnapshotKey2)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update policy snapshot status")
		})

		AfterAll(func() {
			ensurePlacementAndAllRelatedResourcesDeletion(rpKey)
		})
	})
})
