/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package tests

// This test suite features a number of test cases which cover the workflow of scheduling CRPs
// of the PickN placement type.

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

var _ = Describe("scheduling CRPs of the PickN placement type", func() {
	Context("pick N clusters with no affinities/topology spread constraints specified", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(3) // Less than the number of clusters available (7) in the fleet.

		// The scheduler is designed to produce only deterministic decisions; if there are no
		// comparable scores available for selected clusters, the scheduler will rank the clusters
		// by their names.
		wantPickedClusters := []string{
			memberCluster5CentralProd,
			memberCluster6WestProd,
			memberCluster7WestCanary,
		}
		wantNotPickedClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster3EastCanary,
			memberCluster4CentralProd,
		}
		wantFilteredClusters := []string{
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, nil, nil, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, int(numOfClusters))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClusters, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(int(numOfClusters), wantPickedClusters, wantNotPickedClusters, wantFilteredClusters, zeroScoreByCluster, policySnapshotName)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	Context("not enough clusters to pick", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(10) // More than the number of clusters available (7) in the fleet.

		// The scheduler is designed to produce only deterministic decisions; if there are no
		// comparable scores available for selected clusters, the scheduler will rank the clusters
		// by their names.
		wantPickedClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster3EastCanary,
			memberCluster4CentralProd,
			memberCluster5CentralProd,
			memberCluster6WestProd,
			memberCluster7WestCanary,
		}
		wantFilteredClusters := []string{
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, nil, nil, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, len(wantPickedClusters))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClusters, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(int(numOfClusters), wantPickedClusters, []string{}, wantFilteredClusters, zeroScoreByCluster, policySnapshotName)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	Context("pick 0 clusters", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(0)

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, nil, nil, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, int(numOfClusters))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(int(numOfClusters), []string{}, []string{}, []string{}, zeroScoreByCluster, policySnapshotName)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	Context("pick with required affinity", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(2)

		wantPickedClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
		}
		wantFilteredClusters := []string{
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

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			affinity := &placementv1beta1.Affinity{
				ClusterAffinity: &placementv1beta1.ClusterAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
						ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
							{
								LabelSelector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										regionLabel: "east",
									},
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      envLabel,
											Operator: metav1.LabelSelectorOpIn,
											Values: []string{
												"prod",
											},
										},
									},
								},
							},
						},
					},
				},
			}
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, affinity, nil, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, int(numOfClusters))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClusters, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(int(numOfClusters), wantPickedClusters, []string{}, wantFilteredClusters, zeroScoreByCluster, policySnapshotName)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	Context("pick with required affinity, multiple terms", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(4)

		// Note that the number of matching clusters is less than the desired one.
		wantPickedClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster7WestCanary,
		}
		wantFilteredClusters := []string{
			memberCluster3EastCanary,
			memberCluster4CentralProd,
			memberCluster5CentralProd,
			memberCluster6WestProd,
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			affinity := &placementv1beta1.Affinity{
				ClusterAffinity: &placementv1beta1.ClusterAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
						ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
							{
								LabelSelector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										regionLabel: "east",
									},
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      envLabel,
											Operator: metav1.LabelSelectorOpIn,
											Values: []string{
												"prod",
											},
										},
									},
								},
							},
							{
								LabelSelector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										regionLabel: "west",
									},
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      envLabel,
											Operator: metav1.LabelSelectorOpNotIn,
											Values: []string{
												"prod",
											},
										},
									},
								},
							},
						},
					},
				},
			}
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, affinity, nil, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, len(wantPickedClusters))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClusters, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(int(numOfClusters), wantPickedClusters, []string{}, wantFilteredClusters, zeroScoreByCluster, policySnapshotName)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	Context("pick with preferred affinity", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(4)

		wantPickedClusters := []string{
			memberCluster4CentralProd,
			memberCluster5CentralProd,
			memberCluster7WestCanary,
			memberCluster6WestProd,
		}
		wantNotPickedClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster3EastCanary,
		}
		wantFilteredClusters := []string{
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		scoreByCluster := map[string]*placementv1beta1.ClusterScore{
			memberCluster1EastProd:   &zeroScore,
			memberCluster2EastProd:   &zeroScore,
			memberCluster3EastCanary: &zeroScore,
			memberCluster4CentralProd: {
				AffinityScore:       pointer.Int32(10),
				TopologySpreadScore: pointer.Int32(0),
			},
			memberCluster5CentralProd: {
				AffinityScore:       pointer.Int32(10),
				TopologySpreadScore: pointer.Int32(0),
			},
			memberCluster6WestProd:   &zeroScore,
			memberCluster7WestCanary: &zeroScore,
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			affinity := &placementv1beta1.Affinity{
				ClusterAffinity: &placementv1beta1.ClusterAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
						{
							Weight: 10,
							Preference: placementv1beta1.ClusterSelectorTerm{
								LabelSelector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										regionLabel: "central",
									},
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      envLabel,
											Operator: metav1.LabelSelectorOpIn,
											Values: []string{
												"prod",
											},
										},
									},
								},
							},
						},
					},
				},
			}
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, affinity, nil, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, len(wantPickedClusters))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClusters, scoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(int(numOfClusters), wantPickedClusters, wantNotPickedClusters, wantFilteredClusters, scoreByCluster, policySnapshotName)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})
})
