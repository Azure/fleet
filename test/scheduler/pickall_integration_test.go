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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
			createNilSchedulingPolicyCRPWithPolicySnapshot(crpName, policySnapshotName, nil)
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
			createNilSchedulingPolicyCRPWithPolicySnapshot(crpName, policySnapshotName, nil)

			// Create a new member cluster.
			createMemberCluster(newUnhealthyMemberClusterName, nil)

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
			createNilSchedulingPolicyCRPWithPolicySnapshot(crpName, policySnapshotName, nil)

			// Create a new member cluster.
			createMemberCluster(newUnhealthyMemberClusterName, nil)

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

var _ = Describe("scheduling CRPs of the PickAll placement type", func() {
	Context("pick all valid clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickAll placement type, along with its associated policy snapshot.
			createPickAllCRPWithPolicySnapshot(crpName, policySnapshotName, nil)
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

	Context("pick clusters with specific affinities (single term, multiple selectors)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		wantTargetClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster6WestProd,
		}
		wantIgnoredClusters := []string{
			memberCluster3EastCanary,
			memberCluster4CentralProd,
			memberCluster5CentralProd,
			memberCluster7WestCanary,
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickAll placement type, along with its associated policy snapshot.
			policy := &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											envLabel: "prod",
										},
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      regionLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{"east", "west"},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			createPickAllCRPWithPolicySnapshot(crpName, policySnapshotName, policy)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for all matching clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantTargetClusters, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters, crpName)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(wantTargetClusters, wantIgnoredClusters, policySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	Context("pick clusters with specific affinities (multiple terms, single selector)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		wantTargetClusters := []string{
			memberCluster3EastCanary,
			memberCluster6WestProd,
			memberCluster7WestCanary,
		}
		wantIgnoredClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster4CentralProd,
			memberCluster5CentralProd,
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickAll placement type, along with its associated policy snapshot.
			policy := &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											envLabel: "canary",
										},
									},
								},
								{
									LabelSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      regionLabel,
												Operator: metav1.LabelSelectorOpNotIn,
												Values: []string{
													"east",
													"central",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			createPickAllCRPWithPolicySnapshot(crpName, policySnapshotName, policy)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for all matching clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantTargetClusters, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters, crpName)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(wantTargetClusters, wantIgnoredClusters, policySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	Context("affinities updated", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName1 := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)
		policySnapshotName2 := fmt.Sprintf(policySnapshotNameTemplate, crpName, 2)

		wantTargetClusters1 := []string{
			memberCluster3EastCanary,
			memberCluster7WestCanary,
		}
		wantTargetClusters2 := []string{
			memberCluster3EastCanary,
			memberCluster6WestProd,
			memberCluster7WestCanary,
		}
		wantIgnoredClusters2 := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster4CentralProd,
			memberCluster5CentralProd,
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}
		boundClusters := []string{
			memberCluster3EastCanary,
		}
		scheduledClusters := []string{
			memberCluster6WestProd,
			memberCluster7WestCanary,
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickAll placement type, along with its associated policy snapshot.
			policy := &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											envLabel: "canary",
										},
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      regionLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values: []string{
													"east",
													"west",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			createPickAllCRPWithPolicySnapshot(crpName, policySnapshotName1, policy)

			// Verify that bindings have been created as expected.
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantTargetClusters1, zeroScoreByCluster, crpName, policySnapshotName1)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")

			// Bind some bindings.
			markBindingsAsBoundForClusters(crpName, boundClusters)

			// Update the CRP with a new affinity.
			affinity := &placementv1beta1.Affinity{
				ClusterAffinity: &placementv1beta1.ClusterAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
						ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										envLabel: "canary",
									},
								},
							},
							{
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      regionLabel,
											Operator: metav1.LabelSelectorOpNotIn,
											Values: []string{
												"east",
												"central",
											},
										},
									},
								},
							},
						},
					},
				},
			}
			updatePickAllCRPWithNewAffinity(crpName, affinity, policySnapshotName1, policySnapshotName2)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create/update scheduled bindings for newly matched clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(scheduledClusters, zeroScoreByCluster, crpName, policySnapshotName2)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should update bound bindings for newly matched clusters", func() {
			boundBindingsUpdatedActual := boundBindingsCreatedOrUpdatedForClustersActual(boundClusters, zeroScoreByCluster, crpName, policySnapshotName2)
			Eventually(boundBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(boundBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters2, crpName)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(wantTargetClusters2, wantIgnoredClusters2, policySnapshotName2)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	Context("no matching clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		wantIgnoredClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster3EastCanary,
			memberCluster4CentralProd,
			memberCluster5CentralProd,
			memberCluster6WestProd,
			memberCluster7WestCanary,
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			policy := &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											envLabel: "wonderland",
										},
									},
								},
								{
									LabelSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      regionLabel,
												Operator: metav1.LabelSelectorOpNotIn,
												Values: []string{
													"east",
													"central",
													"west",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			createPickAllCRPWithPolicySnapshot(crpName, policySnapshotName, policy)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters, crpName)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual([]string{}, wantIgnoredClusters, policySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})
})
