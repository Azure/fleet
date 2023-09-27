/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package tests

// This test suite features a number of test cases which cover the workflow of scheduling CRPs
// of the PickAll placement type.

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

var _ = Describe("scheduling CRPs with no scheduling policy specified", func() {
	Context("pick all valid clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP with no scheduling policy specified, along with its associated policy snapshot.
			createNilSchedulingPolicyCRPWithPolicySnapshot(crpName, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for all healthy clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(healthyClusters, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for unhealthy clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(unhealthyClusters, crpName)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(healthyClusters, unhealthyClusters, policySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	// This is a serial test as adding a new member cluster may interrupt other test cases.
	Context("add a new healthy cluster", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		// Prepare a new cluster to avoid interrupting other concurrently running test cases.
		newUnhealthyMemberClusterName := fmt.Sprintf(provisionalClusterNameTemplate, GinkgoParallelProcess())
		updatedHealthyClusters := healthyClusters
		updatedHealthyClusters = append(updatedHealthyClusters, newUnhealthyMemberClusterName)

		// Copy the map to avoid interrupting other concurrently running test cases.
		updatedZeroScoreByCluster := make(map[string]*placementv1beta1.ClusterScore)
		for k, v := range zeroScoreByCluster {
			updatedZeroScoreByCluster[k] = v
		}
		updatedZeroScoreByCluster[newUnhealthyMemberClusterName] = &zeroScore

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP with no scheduling policy specified, along with its associated policy snapshot.
			createNilSchedulingPolicyCRPWithPolicySnapshot(crpName, policySnapshotName)

			// Create a new member cluster.
			createMemberCluster(newUnhealthyMemberClusterName)

			// Mark this cluster as healthy.
			markClusterAsHealthy(newUnhealthyMemberClusterName)
		})

		It("should create scheduled bindings for the newly recovered cluster", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(updatedHealthyClusters, updatedZeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)

			// Delete the provisional cluster.
			ensureProvisionalClusterDeletion(newUnhealthyMemberClusterName)
		})
	})

	// This is a serial test as adding a new member cluster may interrupt other test cases.
	Context("a healthy cluster becomes unhealthy", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		// Prepare a new cluster to avoid interrupting other concurrently running test cases.
		newUnhealthyMemberClusterName := fmt.Sprintf(provisionalClusterNameTemplate, GinkgoParallelProcess())
		updatedHealthyClusters := healthyClusters
		updatedHealthyClusters = append(updatedHealthyClusters, newUnhealthyMemberClusterName)

		// Copy the map to avoid interrupting other concurrently running test cases.
		updatedZeroScoreByCluster := make(map[string]*placementv1beta1.ClusterScore)
		for k, v := range zeroScoreByCluster {
			updatedZeroScoreByCluster[k] = v
		}
		updatedZeroScoreByCluster[newUnhealthyMemberClusterName] = &zeroScore

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP with no scheduling policy specified, along with its associated policy snapshot.
			createNilSchedulingPolicyCRPWithPolicySnapshot(crpName, policySnapshotName)

			// Create a new member cluster.
			createMemberCluster(newUnhealthyMemberClusterName)

			// Mark this cluster as healthy.
			markClusterAsHealthy(newUnhealthyMemberClusterName)

			// Verify that a binding has been created for the cluster.
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(updatedHealthyClusters, updatedZeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")

			// Mark the cluster as unhealthy.
			markClusterAsUnhealthy(newUnhealthyMemberClusterName)
		})

		It("should not remove binding for the cluster that just becomes unhealthy", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(updatedHealthyClusters, updatedZeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)

			// Delete the provisional cluster.
			ensureProvisionalClusterDeletion(newUnhealthyMemberClusterName)
		})
	})
})
