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
// of the PickAll placement type.

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

var _ = Describe("scheduling CRPs with no scheduling policy specified", func() {
	Context("pick all valid clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpKey := types.NamespacedName{Name: crpName}
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(crpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP with no scheduling policy specified, along with its associated policy snapshot.
			createNilSchedulingPolicyCRPWithPolicySnapshot(crpName, policySnapshotName, nil)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(crpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for all healthy clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(healthyClusters, zeroScoreByCluster, crpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for unhealthy clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(unhealthyClusters, crpKey)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(healthyClusters, unhealthyClusters, types.NamespacedName{Name: policySnapshotName})
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensurePlacementAndAllRelatedResourcesDeletion(crpKey)
		})
	})

	// This is a serial test as adding a new member cluster may interrupt other test cases.
	Context("add a new healthy cluster", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpKey := types.NamespacedName{Name: crpName}
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
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(crpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP with no scheduling policy specified, along with its associated policy snapshot.
			createNilSchedulingPolicyCRPWithPolicySnapshot(crpName, policySnapshotName, nil)

			// Create a new member cluster.
			createMemberCluster(newUnhealthyMemberClusterName, nil)

			// Mark this cluster as healthy.
			markClusterAsHealthy(newUnhealthyMemberClusterName)
		})

		It("should create scheduled bindings for the newly recovered cluster", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(updatedHealthyClusters, updatedZeroScoreByCluster, crpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensurePlacementAndAllRelatedResourcesDeletion(crpKey)

			// Delete the provisional cluster.
			ensureProvisionalClusterDeletion(newUnhealthyMemberClusterName)
		})
	})

	// This is a serial test as adding a new member cluster may interrupt other test cases.
	Context("a healthy cluster becomes unhealthy", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpKey := types.NamespacedName{Name: crpName}
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
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(crpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP with no scheduling policy specified, along with its associated policy snapshot.
			createNilSchedulingPolicyCRPWithPolicySnapshot(crpName, policySnapshotName, nil)

			// Create a new member cluster.
			createMemberCluster(newUnhealthyMemberClusterName, nil)

			// Mark this cluster as healthy.
			markClusterAsHealthy(newUnhealthyMemberClusterName)

			// Verify that a binding has been created for the cluster.
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(updatedHealthyClusters, updatedZeroScoreByCluster, crpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")

			// Mark the cluster as unhealthy.
			markClusterAsUnhealthy(newUnhealthyMemberClusterName)
		})

		It("should not remove binding for the cluster that just becomes unhealthy", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(updatedHealthyClusters, updatedZeroScoreByCluster, crpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensurePlacementAndAllRelatedResourcesDeletion(crpKey)

			// Delete the provisional cluster.
			ensureProvisionalClusterDeletion(newUnhealthyMemberClusterName)
		})
	})
})

var _ = Describe("scheduling CRPs of the PickAll placement type", func() {
	Context("pick all valid clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpKey := types.NamespacedName{Name: crpName}
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(crpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickAll placement type, along with its associated policy snapshot.
			createPickAllCRPWithPolicySnapshot(crpName, policySnapshotName, nil)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(crpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for all healthy clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(healthyClusters, zeroScoreByCluster, crpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for unhealthy clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(unhealthyClusters, crpKey)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(healthyClusters, unhealthyClusters, types.NamespacedName{Name: policySnapshotName})
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensurePlacementAndAllRelatedResourcesDeletion(crpKey)
		})
	})

	Context("pick clusters with specific affinities (single term, multiple selectors)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpKey := types.NamespacedName{Name: crpName}
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
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(crpKey)
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
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(crpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for all matching clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantTargetClusters, zeroScoreByCluster, crpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters, crpKey)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(wantTargetClusters, wantIgnoredClusters, types.NamespacedName{Name: policySnapshotName})
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensurePlacementAndAllRelatedResourcesDeletion(crpKey)
		})
	})

	Context("pick clusters with specific affinities (multiple terms, single selector)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpKey := types.NamespacedName{Name: crpName}
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
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(crpKey)
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
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(crpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for all matching clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantTargetClusters, zeroScoreByCluster, crpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters, crpKey)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(wantTargetClusters, wantIgnoredClusters, types.NamespacedName{Name: policySnapshotName})
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensurePlacementAndAllRelatedResourcesDeletion(crpKey)
		})
	})

	Context("affinities updated", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpKey := types.NamespacedName{Name: crpName}
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
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(crpKey)
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
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantTargetClusters1, zeroScoreByCluster, crpKey, policySnapshotName1)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")

			// Bind some bindings.
			markBindingsAsBoundForClusters(crpKey, boundClusters)

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
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(crpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create/update scheduled bindings for newly matched clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(scheduledClusters, zeroScoreByCluster, crpKey, policySnapshotName2)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should update bound bindings for newly matched clusters", func() {
			boundBindingsUpdatedActual := boundBindingsCreatedOrUpdatedForClustersActual(boundClusters, zeroScoreByCluster, crpKey, policySnapshotName2)
			Eventually(boundBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(boundBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters2, crpKey)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(wantTargetClusters2, wantIgnoredClusters2, types.NamespacedName{Name: policySnapshotName2})
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensurePlacementAndAllRelatedResourcesDeletion(crpKey)
		})
	})

	Context("no matching clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crpKey := types.NamespacedName{Name: crpName}
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
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(crpKey)
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
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(crpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters, crpKey)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual([]string{}, wantIgnoredClusters, types.NamespacedName{Name: policySnapshotName})
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensurePlacementAndAllRelatedResourcesDeletion(crpKey)
		})
	})
})

var _ = Describe("scheduling RPs of the PickAll placement type", func() {
	Context("pick all valid clusters", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		rpKey := types.NamespacedName{Namespace: testNamespace, Name: rpName}
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, rpName, 1)
		policySnapshotKey := types.NamespacedName{Namespace: testNamespace, Name: policySnapshotName}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(rpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a RP of the PickAll placement type, along with its associated policy snapshot.
			createPickAllRPWithPolicySnapshot(testNamespace, rpName, policySnapshotName, nil)
		})

		It("should add scheduler cleanup finalizer to the RP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(rpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to RP")
		})

		It("should create scheduled bindings for all healthy clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(healthyClusters, zeroScoreByCluster, rpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for unhealthy clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(unhealthyClusters, rpKey)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(healthyClusters, unhealthyClusters, policySnapshotKey)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the RP.
			ensurePlacementAndAllRelatedResourcesDeletion(rpKey)
		})
	})

	Context("pick clusters with specific affinities (single term, multiple selectors)", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		rpKey := types.NamespacedName{Namespace: testNamespace, Name: rpName}
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, rpName, 1)
		policySnapshotKey := types.NamespacedName{Namespace: testNamespace, Name: policySnapshotName}

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
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(rpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a RP of the PickAll placement type, along with its associated policy snapshot.
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
			createPickAllRPWithPolicySnapshot(testNamespace, rpName, policySnapshotName, policy)
		})

		It("should add scheduler cleanup finalizer to the RP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(rpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to RP")
		})

		It("should create scheduled bindings for all matching clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantTargetClusters, zeroScoreByCluster, rpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters, rpKey)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(wantTargetClusters, wantIgnoredClusters, policySnapshotKey)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the RP.
			ensurePlacementAndAllRelatedResourcesDeletion(rpKey)
		})
	})

	Context("pick clusters with specific affinities (multiple terms, single selector)", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		rpKey := types.NamespacedName{Namespace: testNamespace, Name: rpName}
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, rpName, 1)
		policySnapshotKey := types.NamespacedName{Namespace: testNamespace, Name: policySnapshotName}

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
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(rpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a RP of the PickAll placement type, along with its associated policy snapshot.
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
			createPickAllRPWithPolicySnapshot(testNamespace, rpName, policySnapshotName, policy)
		})

		It("should add scheduler cleanup finalizer to the RP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(rpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to RP")
		})

		It("should create scheduled bindings for all matching clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantTargetClusters, zeroScoreByCluster, rpKey, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters, rpKey)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(wantTargetClusters, wantIgnoredClusters, policySnapshotKey)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the RP.
			ensurePlacementAndAllRelatedResourcesDeletion(rpKey)
		})
	})

	Context("affinities updated", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		rpKey := types.NamespacedName{Namespace: testNamespace, Name: rpName}
		policySnapshotName1 := fmt.Sprintf(policySnapshotNameTemplate, rpName, 1)
		policySnapshotName2 := fmt.Sprintf(policySnapshotNameTemplate, rpName, 2)
		policySnapshotKey2 := types.NamespacedName{Namespace: testNamespace, Name: policySnapshotName2}

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
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(rpKey)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a RP of the PickAll placement type, along with its associated policy snapshot.
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
			createPickAllRPWithPolicySnapshot(testNamespace, rpName, policySnapshotName1, policy)

			// Verify that bindings have been created as expected.
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantTargetClusters1, zeroScoreByCluster, rpKey, policySnapshotName1)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")

			// Bind some bindings.
			markBindingsAsBoundForClusters(rpKey, boundClusters)

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
			updatePickAllRPWithNewAffinity(testNamespace, rpName, affinity, policySnapshotName1, policySnapshotName2)
		})

		It("should add scheduler cleanup finalizer to the RP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(rpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to RP")
		})

		It("should create/update scheduled bindings for newly matched clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(scheduledClusters, zeroScoreByCluster, rpKey, policySnapshotName2)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should update bound bindings for newly matched clusters", func() {
			boundBindingsUpdatedActual := boundBindingsCreatedOrUpdatedForClustersActual(boundClusters, zeroScoreByCluster, rpKey, policySnapshotName2)
			Eventually(boundBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(boundBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters2, rpKey)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(wantTargetClusters2, wantIgnoredClusters2, policySnapshotKey2)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the RP.
			ensurePlacementAndAllRelatedResourcesDeletion(rpKey)
		})
	})

	Context("no matching clusters", Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		rpKey := types.NamespacedName{Namespace: testNamespace, Name: rpName}
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, rpName, 1)
		policySnapshotKey := types.NamespacedName{Namespace: testNamespace, Name: policySnapshotName}

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
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(rpKey)
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
			createPickAllRPWithPolicySnapshot(testNamespace, rpName, policySnapshotName, policy)
		})

		It("should add scheduler cleanup finalizer to the RP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(rpKey)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to RP")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters, rpKey)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual([]string{}, wantIgnoredClusters, policySnapshotKey)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the RP.
			ensurePlacementAndAllRelatedResourcesDeletion(rpKey)
		})
	})
})
