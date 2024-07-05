/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package tests

// This test suite features a number of test cases which cover the workflow of scheduling
// based on cluster properties.

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/propertyprovider"
)

var _ = Describe("scheduling CRPs of the PickAll placement type using cluster properties", func() {
	Context("pick clusters with specific properties (single term, multiple expressions)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		wantTargetClusters := []string{
			memberCluster3EastCanary,
		}
		wantIgnoredClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
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

			// Create a CRP of the PickAll placement type, along with its associated policy snapshot.
			policy := &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     propertyprovider.NodeCountProperty,
												Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
												Values: []string{
													"4",
												},
											},
											{
												Name:     energyEfficiencyRatingPropertyName,
												Operator: placementv1beta1.PropertySelectorLessThan,
												Values: []string{
													"45",
												},
											},
											{
												Name:     propertyprovider.AllocatableCPUCapacityProperty,
												Operator: placementv1beta1.PropertySelectorNotEqualTo,
												Values: []string{
													"14",
												},
											},
											{
												Name:     propertyprovider.AvailableMemoryCapacityProperty,
												Operator: placementv1beta1.PropertySelectorGreaterThan,
												Values: []string{
													"4Gi",
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

	Context("pick clusters with specific properties (multiple terms, single expression)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		wantTargetClusters := []string{
			memberCluster1EastProd,
			memberCluster3EastCanary,
			memberCluster4CentralProd,
			memberCluster5CentralProd,
			memberCluster7WestCanary,
		}
		wantIgnoredClusters := []string{
			memberCluster2EastProd,
			memberCluster6WestProd,
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
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     propertyprovider.NodeCountProperty,
												Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
												Values: []string{
													"8",
												},
											},
										},
									},
								},
								{
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     energyEfficiencyRatingPropertyName,
												Operator: placementv1beta1.PropertySelectorGreaterThan,
												Values: []string{
													"99",
												},
											},
										},
									},
								},
								{
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     propertyprovider.TotalCPUCapacityProperty,
												Operator: placementv1beta1.PropertySelectorEqualTo,
												Values: []string{
													"12",
												},
											},
										},
									},
								},
								{
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     propertyprovider.TotalMemoryCapacityProperty,
												Operator: placementv1beta1.PropertySelectorLessThanOrEqualTo,
												Values: []string{
													"4Gi",
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

	Context("pick clusters with both label and property selectors (single term)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		wantTargetClusters := []string{
			memberCluster2EastProd,
			memberCluster3EastCanary,
		}
		wantIgnoredClusters := []string{
			memberCluster1EastProd,
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
											regionLabel: "east",
										},
									},
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     propertyprovider.NodeCountProperty,
												Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
												Values: []string{
													"4",
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

	Context("pick clusters with both label and property selectors (multiple terms)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		wantTargetClusters := []string{
			memberCluster5CentralProd,
			memberCluster6WestProd,
		}
		wantIgnoredClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster3EastCanary,
			memberCluster4CentralProd,
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
											envLabel:    "prod",
											regionLabel: "west",
										},
									},
								},
								{
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     energyEfficiencyRatingPropertyName,
												Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
												Values: []string{
													"40",
												},
											},
											{
												Name:     propertyprovider.TotalCPUCapacityProperty,
												Operator: placementv1beta1.PropertySelectorGreaterThan,
												Values: []string{
													"12",
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

	Context("property selector updated", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName1 := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)
		policySnapshotName2 := fmt.Sprintf(policySnapshotNameTemplate, crpName, 2)

		// wantScheduledClusters1, wantIgnoredClusters1, and wantBoundClusters1 are
		// the clusters picked (bound) and unpicked respectively with the original
		// property selector (before the property selector update).
		wantScheduledClusters1 := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster3EastCanary,
			memberCluster4CentralProd,
			memberCluster6WestProd,
		}
		wantIgnoredClusters1 := []string{
			memberCluster5CentralProd,
			memberCluster7WestCanary,
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}
		wantBoundClusters1 := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster4CentralProd,
		}

		// wantScheduledClusters2, wantIgnoredClusters2, and wantBoundClusters2 are
		// the clusters picked (bound) and unpicked respectively with the new
		// property selector (after the property selector update).
		wantScheduledClusters2 := []string{
			memberCluster3EastCanary,
			memberCluster5CentralProd,
			memberCluster7WestCanary,
		}
		wantBoundClusters2 := []string{
			memberCluster2EastProd,
		}
		wantUnscheduledClusters2 := []string{
			memberCluster1EastProd,
			memberCluster4CentralProd,
			memberCluster6WestProd,
		}
		wantIgnoredClusters2 := []string{
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}
		// wantTargetClusters and wantUnselectedClusters are the clusters picked
		// and unpicked respectively after the property selector update.
		wantTargetClusters := []string{}
		wantTargetClusters = append(wantTargetClusters, wantScheduledClusters2...)
		wantTargetClusters = append(wantTargetClusters, wantBoundClusters2...)
		wantUnselectedClusters := []string{}
		wantUnselectedClusters = append(wantUnselectedClusters, wantUnscheduledClusters2...)
		wantUnselectedClusters = append(wantUnselectedClusters, wantIgnoredClusters2...)

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
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     propertyprovider.NodeCountProperty,
												Operator: placementv1beta1.PropertySelectorLessThanOrEqualTo,
												Values: []string{
													"6",
												},
											},
											{
												Name:     propertyprovider.NodeCountProperty,
												Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
												Values: []string{
													"2",
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
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for all matching clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantScheduledClusters1, zeroScoreByCluster, crpName, policySnapshotName1)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters1, crpName)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(wantScheduledClusters1, wantIgnoredClusters1, policySnapshotName1)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		It("can mark some bindings as bound", func() {
			markBindingsAsBoundForClusters(crpName, wantBoundClusters1)
		})

		It("can update the scheduling policy with a new property selector", func() {
			affinity := &placementv1beta1.Affinity{
				ClusterAffinity: &placementv1beta1.ClusterAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
						ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
							{
								PropertySelector: &placementv1beta1.PropertySelector{
									MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
										{
											Name:     propertyprovider.NodeCountProperty,
											Operator: placementv1beta1.PropertySelectorLessThanOrEqualTo,
											Values: []string{
												"8",
											},
										},
										{
											Name:     propertyprovider.NodeCountProperty,
											Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
											Values: []string{
												"4",
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

		It("should create/update scheduled bindings for newly matched clusters", func() {
			scheduledBindingsCreatedOrUpdatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantScheduledClusters2, zeroScoreByCluster, crpName, policySnapshotName2)
			Eventually(scheduledBindingsCreatedOrUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create/update the expected set of bindings")
			Consistently(scheduledBindingsCreatedOrUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create/update the expected set of bindings")
		})

		It("should update bound bindings for newly matched clusters", func() {
			boundBindingsUpdatedActual := boundBindingsCreatedOrUpdatedForClustersActual(wantBoundClusters2, zeroScoreByCluster, crpName, policySnapshotName2)
			Eventually(boundBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(boundBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters2, crpName)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should mark bindings as unscheduled for clusters that were unselected", func() {
			unscheduledBindingsUpdatedActual := unscheduledBindingsCreatedOrUpdatedForClustersActual(wantUnscheduledClusters2, zeroScoreByCluster, crpName, policySnapshotName1)
			Eventually(unscheduledBindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
			Consistently(unscheduledBindingsUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update the expected set of bindings")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(wantTargetClusters, wantUnselectedClusters, policySnapshotName2)
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

			// Create a CRP of the PickAll placement type, along with its associated policy snapshot.
			policy := &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     energyEfficiencyRatingPropertyName,
												Operator: placementv1beta1.PropertySelectorLessThanOrEqualTo,
												Values: []string{
													"0",
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

	// This spec has been marked as serial as it updates the cluster properties, which may
	// interfere with other specs if run in parallel.
	Context("cluster properties refreshed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		// wantTargetClusters1 and wantIgnoredClusters1 are the picked and unpicked clusters
		// respectively before the cluster properties refresh.
		wantTargetClusters1 := []string{
			memberCluster3EastCanary,
			memberCluster5CentralProd,
		}
		wantIgnoredClusters1 := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster4CentralProd,
			memberCluster6WestProd,
			memberCluster7WestCanary,
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		// wantTargetClusters2 and wantIgnoredClusters2 are the picked and unpicked clusters
		// respectively after the cluster properties refresh.
		wantTargetClusters2 := []string{
			memberCluster3EastCanary,
			memberCluster5CentralProd,
			memberCluster7WestCanary,
		}
		wantIgnoredClusters2 := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster4CentralProd,
			memberCluster6WestProd,
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
									PropertySelector: &placementv1beta1.PropertySelector{
										MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
											{
												Name:     propertyprovider.AvailableCPUCapacityProperty,
												Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
												Values: []string{
													"6",
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
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantTargetClusters1, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters1, crpName)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(wantTargetClusters1, wantIgnoredClusters1, policySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		It("can update the cluster properties", func() {
			// Set the available CPU capacity of all previously picked clusters to 4 (below the
			// the selector requirement).
			for idx := range wantTargetClusters1 {
				clusterName := wantTargetClusters1[idx]
				Eventually(func() error {
					memberCluster := &clusterv1beta1.MemberCluster{}
					if err := hubClient.Get(ctx, types.NamespacedName{Name: clusterName}, memberCluster); err != nil {
						return fmt.Errorf("failed to get member cluster %s: %w", clusterName, err)
					}

					memberCluster.Status.ResourceUsage.Available[corev1.ResourceCPU] = resource.MustParse("4")
					if err := hubClient.Status().Update(ctx, memberCluster); err != nil {
						return fmt.Errorf("failed to update the available CPU capacity of member cluster %s: %w", clusterName, err)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the available CPU capacity of member cluster")
			}

			// Set cluster 7 to have more available CPU (above the selector requirement).
			Eventually(func() error {
				memberCluster := &clusterv1beta1.MemberCluster{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: memberCluster7WestCanary}, memberCluster); err != nil {
					return fmt.Errorf("failed to get member cluster %s: %w", memberCluster7WestCanary, err)
				}

				memberCluster.Status.ResourceUsage.Available[corev1.ResourceCPU] = resource.MustParse("8")
				if err := hubClient.Status().Update(ctx, memberCluster); err != nil {
					return fmt.Errorf("failed to update the available CPU capacity of member cluster %s: %w", memberCluster7WestCanary, err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the available CPU capacity of member cluster")
		})

		It("should create scheduled bindings for newly matched clusters while retaining old ones", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantTargetClusters2, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for non-matching clusters", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(wantIgnoredClusters2, crpName)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(wantTargetClusters2, wantIgnoredClusters2, policySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)

			// Reset the cluster properties.
			for idx := range wantTargetClusters2 {
				resetClusterPropertiesFor(wantIgnoredClusters2[idx])
			}
		})
	})
})
