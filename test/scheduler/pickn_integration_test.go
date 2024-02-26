/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package tests

// This test suite features a number of test cases which cover the workflow of scheduling CRPs
// of the PickN placement type.

import (
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

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
				AffinityScore:       ptr.To(int32(10)),
				TopologySpreadScore: ptr.To(int32(0)),
			},
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(10)),
				TopologySpreadScore: ptr.To(int32(0)),
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

	Context("pick with preferred affinity, multiple terms", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(4)

		wantPickedClusters := []string{
			memberCluster4CentralProd,
			memberCluster5CentralProd,
			memberCluster3EastCanary,
			memberCluster7WestCanary,
		}
		wantNotPickedClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster6WestProd,
		}
		wantFilteredClusters := []string{
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		scoreByCluster := map[string]*placementv1beta1.ClusterScore{
			memberCluster1EastProd: &zeroScore,
			memberCluster2EastProd: &zeroScore,
			memberCluster3EastCanary: {
				AffinityScore:       ptr.To(int32(20)),
				TopologySpreadScore: ptr.To(int32(0)),
			},
			memberCluster4CentralProd: {
				AffinityScore:       ptr.To(int32(10)),
				TopologySpreadScore: ptr.To(int32(0)),
			},
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(10)),
				TopologySpreadScore: ptr.To(int32(0)),
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
						{
							Weight: 20,
							Preference: placementv1beta1.ClusterSelectorTerm{
								LabelSelector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										regionLabel: "east",
									},
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      envLabel,
											Operator: metav1.LabelSelectorOpIn,
											Values: []string{
												"canary",
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

	Context("pick with required topology spread constraints", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(2)

		wantPickedClusters := []string{
			memberCluster7WestCanary,
			memberCluster5CentralProd,
		}
		wantNotPickedClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster3EastCanary,
			memberCluster4CentralProd,
			memberCluster6WestProd,
		}
		wantFilteredClusters := []string{
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		scoreByCluster := map[string]*placementv1beta1.ClusterScore{
			memberCluster1EastProd:    &zeroScore,
			memberCluster2EastProd:    &zeroScore,
			memberCluster3EastCanary:  &zeroScore,
			memberCluster4CentralProd: &zeroScore,
			// Cluster 5 is picked in the second iteration, as placing resources on it does
			// not violate any topology spread constraints + does not increase the skew. It
			// is assigned a topology spread score of 0 as the skew is unchanged.
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(0)),
			},
			// Cluster 6 is considered to be unschedulable in the second iteration as placing
			// resources on it would violate the topology spread constraint (skew becomes 2,
			// the limit is 1); unschedulable clusters do not have scores assigned.
			memberCluster6WestProd: nil,
			// Cluster 7 is picked in the first iteration, as placing resources on it does not
			// violate any topology spread constraints + increases the skew only by one (so do other
			// clusters), and its name is the largest in alphanumeric order. It is assigned
			// a topology spread score of -1 as placing resources on it increases the skew.
			memberCluster7WestCanary: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			topologySpreadConstraints := []placementv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:           ptr.To(int32(1)),
					TopologyKey:       regionLabel,
					WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
				},
			}
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, nil, topologySpreadConstraints, policySnapshotName)
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

	Context("pick with required topology spread constraints, multiple terms", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(2)

		wantPickedClusters := []string{
			memberCluster7WestCanary,
			memberCluster5CentralProd,
		}
		wantNotPickedClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster3EastCanary,
			memberCluster4CentralProd,
			memberCluster6WestProd,
		}
		wantFilteredClusters := []string{
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		scoreByCluster := map[string]*placementv1beta1.ClusterScore{
			// Cluster 1 is not picked in the second iteration, but placing resources on it
			// would leave the skew for the region-based topology spread constraint unchanged,
			// and decrease the skew for the environment-based topology spread constraint by 1;
			// consequently it receives a topology spread score of 1.
			memberCluster1EastProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(1)),
			},
			// Cluster 2 is not picked in the second iteration, but placing resources on it
			// would leave the skew for the region-based topology spread constraint unchanged,
			// and decrease the skew for the environment-based topology spread constraint by 1;
			// consequently it receives a topology spread score of 1.
			memberCluster2EastProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(1)),
			},
			// Cluster 3 is considered to be unschedulable in the second iteration as placing
			// resources on it would violate the environment-based topology spread constraint
			// (skew becomes 2, the limit is 1); unschedulable clusters do not have scores assigned.
			memberCluster3EastCanary: nil,
			// Cluster 4 is not picked in the second iteration, but placing resources on it
			// would leave the skew for the region-based topology spread constraint unchanged,
			// and decrease the skew for the environment-based topology spread constraint by 1;
			// consequently it receives a topology spread score of 1.
			memberCluster4CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(1)),
			},
			// Cluster 5 is picked in the second iteration, as placing resources on it does
			// not violate any topology spread constraints + does not increase the skew. It
			// is assigned a topology spread score of 0 as the skew is unchanged for both
			// topology spread constraints.
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(1)),
			},
			// Cluster 6 is considered to be unschedulable in the second iteration as placing
			// resources on it would violate the region-based topology spread constraint
			// (skew becomes 2, the limit is 1); unschedulable clusters do not have scores assigned.
			memberCluster6WestProd: nil,
			// Cluster 7 is picked in the first iteration, as placing resources on it does not
			// violate any topology spread constraints + increases the skew only by one in both
			// topology spread constraints (so do other clusters), and its name is the largest
			// in alphanumeric order. It is assigned a topology spread score of -2 as placing
			// resources on it increases the skew in both topology spread constraints..
			memberCluster7WestCanary: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-2)),
			},
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			topologySpreadConstraints := []placementv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:           ptr.To(int32(1)),
					TopologyKey:       regionLabel,
					WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
				},
				{
					MaxSkew:           ptr.To(int32(1)),
					TopologyKey:       envLabel,
					WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
				},
			}
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, nil, topologySpreadConstraints, policySnapshotName)
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

	Context("pick with preferred topology spread constraints", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(2)

		wantPickedClusters := []string{
			memberCluster7WestCanary,
			memberCluster5CentralProd,
		}
		wantNotPickedClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster3EastCanary,
			memberCluster4CentralProd,
			memberCluster6WestProd,
		}
		wantFilteredClusters := []string{
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		scoreByCluster := map[string]*placementv1beta1.ClusterScore{
			memberCluster1EastProd:    &zeroScore,
			memberCluster2EastProd:    &zeroScore,
			memberCluster3EastCanary:  &zeroScore,
			memberCluster4CentralProd: &zeroScore,
			// Cluster 5 is picked in the second iteration, as placing resources on it does
			// not violate any topology spread constraints + does not increase the skew. It
			// is assigned a topology spread score of 0 as the skew is unchanged.
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(0)),
			},
			// Cluster 6 is not picked in the second iteration, and placing
			// resources on it would violate the topology spread constraint (skew becomes 2,
			// the limit is 1); the violation leads to a topology spread score of -1000.
			memberCluster6WestProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1000)),
			},
			// Cluster 7 is picked in the first iteration, as placing resources on it does not
			// violate any topology spread constraints + increases the skew only by one (so do other
			// clusters), and its name is the largest in alphanumeric order. It is assigned
			// a topology spread score of -1 as placing resources on it increases the skew.
			memberCluster7WestCanary: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			topologySpreadConstraints := []placementv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:           ptr.To(int32(1)),
					TopologyKey:       regionLabel,
					WhenUnsatisfiable: placementv1beta1.ScheduleAnyway,
				},
			}
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, nil, topologySpreadConstraints, policySnapshotName)
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

	Context("pick with preferred topology spread constraints, multiple terms", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(2)

		wantPickedClusters := []string{
			memberCluster7WestCanary,
			memberCluster5CentralProd,
		}
		wantNotPickedClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster3EastCanary,
			memberCluster4CentralProd,
			memberCluster6WestProd,
		}
		wantFilteredClusters := []string{
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		scoreByCluster := map[string]*placementv1beta1.ClusterScore{
			// Cluster 1 is not picked in the second iteration, but placing resources on it
			// would leave the skew for the region-based topology spread constraint unchanged,
			// and decrease the skew for the environment-based topology spread constraint by 1;
			// consequently it receives a topology spread score of 1.
			memberCluster1EastProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(1)),
			},
			// Cluster 2 is not picked in the second iteration, but placing resources on it
			// would leave the skew for the region-based topology spread constraint unchanged,
			// and decrease the skew for the environment-based topology spread constraint by 1;
			// consequently it receives a topology spread score of 1.
			memberCluster2EastProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(1)),
			},
			// Cluster 3 is not picked in the second iteration as placing
			// resources on it would violate the region-based topology spread constraint
			// (skew becomes 2, the limit is 1); the violation leads to a topology spread score
			// of -1000.
			memberCluster3EastCanary: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1000)),
			},
			// Cluster 4 is not picked in the second iteration, but placing resources on it
			// would leave the skew for the region-based topology spread constraint unchanged,
			// and decrease the skew for the environment-based topology spread constraint by 1;
			// consequently it receives a topology spread score of 1.
			memberCluster4CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(1)),
			},
			// Cluster 5 is picked in the second iteration, as placing resources on it does
			// not violate any topology spread constraints + does not increase the skew. It
			// is assigned a topology spread score of 0 as the skew is unchanged for both
			// topology spread constraints.
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(1)),
			},
			// Cluster 6 is not picked in the second iteration as placing
			// resources on it would violate the region-based topology spread constraint
			// (skew becomes 2, the limit is 1); however, it does decrease the skew for the
			// environment based topology spread constraint by 1, so it receives a topology
			// spread score of -999 (-1000 for the violation, +1 for the skew decrease).
			memberCluster6WestProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-999)),
			},
			// Cluster 7 is picked in the first iteration, as placing resources on it does not
			// violate any topology spread constraints + increases the skew only by one in both
			// topology spread constraints (so do other clusters), and its name is the largest
			// in alphanumeric order. It is assigned a topology spread score of -2 as placing
			// resources on it increases the skew in both topology spread constraints..
			memberCluster7WestCanary: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-2)),
			},
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			topologySpreadConstraints := []placementv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:           ptr.To(int32(1)),
					TopologyKey:       regionLabel,
					WhenUnsatisfiable: placementv1beta1.ScheduleAnyway,
				},
				{
					MaxSkew:           ptr.To(int32(1)),
					TopologyKey:       envLabel,
					WhenUnsatisfiable: placementv1beta1.ScheduleAnyway,
				},
			}
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, nil, topologySpreadConstraints, policySnapshotName)
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

	Context("pick with mixed affinities and topology spread constraints, required only", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(2)

		wantPickedClusters := []string{
			memberCluster3EastCanary,
			memberCluster5CentralProd,
		}
		wantNotPickedClusters := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster4CentralProd,
		}
		wantFilteredClusters := []string{
			memberCluster6WestProd,
			memberCluster7WestCanary,
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		scoreByCluster := map[string]*placementv1beta1.ClusterScore{
			// Cluster 1 is not picked in the second iteration; it does not lead to any skew
			// change.
			memberCluster1EastProd: &zeroScore,
			// Cluster 2 is not picked in the second iteration; it does not lead to any skew
			// change.
			memberCluster2EastProd: &zeroScore,
			// Cluster 3 is picked in the second iteration, it does not lead to any skew
			// change, but its name ranks higher in alphanumeric order.
			memberCluster3EastCanary: &zeroScore,
			// Cluster 4 is unschedulable in the second iteration, as it violates the topology
			// spread constraint (skew becomes 2, the limit is 1). Unschedulable clusters do not
			// have scores assigned.
			memberCluster4CentralProd: nil,
			// Cluster 5 is picked in the first iteration, as placing resources on it does
			// not violate any topology spread constraints; but it increases the skew by 1, hence
			// the -1 topology spread score.
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			// Cluster 6 is filtered out (does not match with required affinity term).
			memberCluster6WestProd: nil,
			// Cluster 7 is filtered out (does not match with required affinity term).
			memberCluster7WestCanary: nil,
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
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      regionLabel,
											Operator: metav1.LabelSelectorOpNotIn,
											Values: []string{
												"west",
											},
										},
									},
								},
							},
						},
					},
				},
			}
			topologySpreadConstraints := []placementv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:           ptr.To(int32(1)),
					TopologyKey:       regionLabel,
					WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
				},
			}
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, affinity, topologySpreadConstraints, policySnapshotName)
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

	Context("pick with mixed affinities and topology spread constraints, preferred only", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(6)

		wantPickedClusters := []string{
			memberCluster3EastCanary,
			memberCluster2EastProd,
			memberCluster1EastProd,
			memberCluster7WestCanary,
			memberCluster6WestProd,
			memberCluster5CentralProd,
		}
		wantNotPickedClusters := []string{
			memberCluster4CentralProd,
		}
		wantFilteredClusters := []string{
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		scoreByCluster := map[string]*placementv1beta1.ClusterScore{
			// Cluster 1 is picked in the third iteration, as placing resources on it does
			// not violate any topology spread constraints and it is preferred per affinity
			// configuration (with a weight of 30); but it increases the skew by 1, hence
			// the -1 topology spread score.
			memberCluster1EastProd: {
				AffinityScore:       ptr.To(int32(30)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			// Cluster 2 is picked in the second iteration, as placing resources on it reduces
			// the skew by 1, hence the topology spread score of 1, and it is preferred
			// per affinity configuration (with a weight of 30).
			memberCluster2EastProd: {
				AffinityScore:       ptr.To(int32(30)),
				TopologySpreadScore: ptr.To(int32(1)),
			},
			// Cluster 3 is picked in the first iteration, as placing resources on it does
			// not violate any topology spread constraints and it is preferred per affinity
			// configuration (with a weight of 30); but it increases the skew by 1, hence
			// the -1 topology spread score.
			memberCluster3EastCanary: {
				AffinityScore:       ptr.To(int32(30)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			// Cluster 4 is not picked in the 6th iteration; placing resources on it violates
			// the topology spread constraint, hence the topology spread score of -1000.
			memberCluster4CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1000)),
			},
			// Cluster 5 is picked in the 6th iteration; placing resources on it violates the
			// topology spread constraint, hence the topology spread score of -1000. It ranks
			// higher by name in alphanumeric order than cluster 4.
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1000)),
			},
			// Cluster 6 is picked in the 5th iteration, as placing resources on it does
			// not violate any topology spread constraints + the cluster is ranked higher by name
			// in alphanumeric order; but it increases the skew by 1, hence
			// the -1 topology spread score.
			memberCluster6WestProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			// Cluster 7 is picked in the 4th iteration, as placing resources on it reduces
			// the skew by 1, hence the topology spread score of 1.
			memberCluster7WestCanary: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(1)),
			},
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
							Weight: 30,
							Preference: placementv1beta1.ClusterSelectorTerm{
								LabelSelector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										regionLabel: "east",
									},
								},
							},
						},
					},
				},
			}
			topologySpreadConstraints := []placementv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:           ptr.To(int32(1)),
					TopologyKey:       envLabel,
					WhenUnsatisfiable: placementv1beta1.ScheduleAnyway,
				},
			}
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, affinity, topologySpreadConstraints, policySnapshotName)
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

	Context("pick with mixed affinities and topology spread constraints, mixed", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClusters := int32(3)

		wantPickedClusters := []string{
			memberCluster2EastProd,
			memberCluster6WestProd,
			memberCluster5CentralProd,
		}
		wantNotPickedClusters := []string{
			memberCluster4CentralProd,
		}
		wantFilteredClusters := []string{
			memberCluster1EastProd,
			memberCluster3EastCanary,
			memberCluster7WestCanary,
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		scoreByCluster := map[string]*placementv1beta1.ClusterScore{
			// Cluster 1 is not picked in the first iteration as it ranks lower by name in alpha
			// numeric order; it then becomes unschedulable (filtered out) in later iterations as
			// placement would violate the DoNotSchedule topology spread constraint.
			memberCluster1EastProd: nil,
			// Cluster 2 is picked in the first iteration, as
			// * placing resources on it does not violate any topology spread constraints +
			//   increases the skew only by one for both topology spread constraints
			//   (so do other clusters); and
			// * it is preferred per affinity configuration (with a weight of 40);
			// * it is ranked higher by name in alphanumeric order.
			memberCluster2EastProd: {
				AffinityScore:       ptr.To(int32(40)),
				TopologySpreadScore: ptr.To(int32(-2)),
			},
			// Cluster 3 is filtered out as it does not meet the affinity requirements.
			memberCluster3EastCanary: nil,
			// Cluster 4 is not picked as it ranks lower by name in alphanumeric order.
			memberCluster4CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-999)),
			},
			// Cluster 5 is picked in the 3rd iteration, as
			// * placing resources on it does not violate the DoNotSchedule topology spread
			//   constraint (it decreases the skew by one), though it violates the ScheduleAnyway
			//   topology spread constraint (skew becomes 3, limit is 2); and
			// * it is ranked higher by name in alphanumeric order.
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-999)),
			},
			// Cluster 6 is picked in the second iteration, as
			// * placing resources on it does not violate any topology spread constraints (it
			//   increase the skew by 1 for environment-based topology spread constraint); and
			// * it is ranked higher by name in alphanumeric order.
			memberCluster6WestProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			// Cluster 7 is not picked as it does not meet the affinity requirements.
			memberCluster7WestCanary: nil,
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
							Weight: 40,
							Preference: placementv1beta1.ClusterSelectorTerm{
								LabelSelector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										regionLabel: "east",
									},
								},
							},
						},
					},
					RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
						ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
							{
								LabelSelector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										envLabel: "prod",
									},
								},
							},
						},
					},
				},
			}
			topologySpreadConstraints := []placementv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:           ptr.To(int32(2)),
					TopologyKey:       envLabel,
					WhenUnsatisfiable: placementv1beta1.ScheduleAnyway,
				},
				{
					MaxSkew:           ptr.To(int32(1)),
					TopologyKey:       regionLabel,
					WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
				},
			}
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, affinity, topologySpreadConstraints, policySnapshotName)
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

	Context("upscaling", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClustersBefore := int32(1)

		// The scheduler is designed to produce only deterministic decisions; if there are no
		// comparable scores available for selected clusters, the scheduler will rank the clusters
		// by their names.
		wantPickedClustersBefore := []string{
			memberCluster7WestCanary,
		}

		numOfClustersAfter := int32(3)
		wantPickedClustersAfter := []string{
			memberCluster5CentralProd,
			memberCluster6WestProd,
			memberCluster7WestCanary,
		}
		wantNotPickedClustersAfter := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster3EastCanary,
			memberCluster4CentralProd,
		}
		wantFilteredClustersAfter := []string{
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			createPickNCRPWithPolicySnapshot(crpName, numOfClustersBefore, nil, nil, policySnapshotName)

			// Verify that scheduling has been completed.
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, int(numOfClustersBefore))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")

			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClustersBefore, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")

			// Update the policy snapshot.
			//
			// Normally upscaling is done by increasing the number of clusters field in the CRP;
			// however, since in the integration test environment, CRP controller is not available,
			// we directly manipulate the number of clusters annoation on the policy snapshot
			// to trigger upscaling.
			Eventually(func() error {
				policySnapshot := &placementv1beta1.ClusterSchedulingPolicySnapshot{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: policySnapshotName}, policySnapshot); err != nil {
					return err
				}

				policySnapshot.Annotations[placementv1beta1.NumberOfClustersAnnotation] = strconv.Itoa(int(numOfClustersAfter))
				return hubClient.Update(ctx, policySnapshot)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot")
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, int(numOfClustersAfter))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClustersAfter, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(int(numOfClustersAfter), wantPickedClustersAfter, wantNotPickedClustersAfter, wantFilteredClustersAfter, zeroScoreByCluster, policySnapshotName)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	Context("downscaling", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		numOfClustersBefore := int32(3)

		// The scheduler is designed to produce only deterministic decisions; if there are no
		// comparable scores available for selected clusters, the scheduler will rank the clusters
		// by their names.
		wantPickedClustersBefore := []string{
			memberCluster7WestCanary,
			memberCluster6WestProd,
			memberCluster5CentralProd,
		}

		numOfClustersAfter := int32(1)
		wantPickedClustersAfter := []string{
			memberCluster7WestCanary,
		}
		// We do not keep the past scheduling decisions for clusters that are not selected;
		// as a result, when downscaling happens, only the decisions for selected clusters are kept.
		wantNotPickedClustersAfter := []string{}
		wantFilteredClustersAfter := []string{}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			createPickNCRPWithPolicySnapshot(crpName, numOfClustersBefore, nil, nil, policySnapshotName)

			// Verify that scheduling has been completed.
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, int(numOfClustersBefore))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")

			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClustersBefore, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")

			// Update the policy snapshot.
			//
			// Normally downscaling is done by increasing the number of clusters field in the CRP;
			// however, since in the integration test environment, CRP controller is not available,
			// we directly manipulate the number of clusters annoation on the policy snapshot
			// to trigger downscaling.
			Eventually(func() error {
				policySnapshot := &placementv1beta1.ClusterSchedulingPolicySnapshot{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: policySnapshotName}, policySnapshot); err != nil {
					return err
				}

				policySnapshot.Annotations[placementv1beta1.NumberOfClustersAnnotation] = strconv.Itoa(int(numOfClustersAfter))
				return hubClient.Update(ctx, policySnapshot)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP")
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, int(numOfClustersAfter))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClustersAfter, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(int(numOfClustersAfter), wantPickedClustersAfter, wantNotPickedClustersAfter, wantFilteredClustersAfter, zeroScoreByCluster, policySnapshotName)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	Context("affinities and topology spread constraints updated", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotNameBefore := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)
		policySnapshotNameAfter := fmt.Sprintf(policySnapshotNameTemplate, crpName, 2)

		numOfClusters := int32(3)
		wantPickedClustersBefore := []string{
			memberCluster2EastProd,
			memberCluster6WestProd,
			memberCluster5CentralProd,
		}

		wantPickedClustersAfter := []string{
			memberCluster5CentralProd,
			memberCluster7WestCanary,
			memberCluster4CentralProd,
		}
		wantNotPickedClustersAfter := []string{
			memberCluster1EastProd,
			memberCluster2EastProd,
			memberCluster3EastCanary,
			memberCluster6WestProd,
		}
		wantFilteredClustersAfter := []string{
			memberCluster8UnhealthyEastProd,
			memberCluster9LeftCentralProd,
		}

		scoreByClusterBefore := map[string]*placementv1beta1.ClusterScore{
			// Cluster 1 is not picked in the first iteration as it ranks lower by name in alpha
			// numeric order; it then becomes unschedulable (filtered out) in later iterations as
			// placement would violate the DoNotSchedule topology spread constraint.
			memberCluster1EastProd: nil,
			// Cluster 2 is picked in the first iteration, as
			// * placing resources on it does not violate any topology spread constraints +
			//   increases the skew only by one for both topology spread constraints
			//   (so do other clusters); and
			// * it is preferred per affinity configuration (with a weight of 40);
			// * it is ranked higher by name in alphanumeric order.
			memberCluster2EastProd: {
				AffinityScore:       ptr.To(int32(40)),
				TopologySpreadScore: ptr.To(int32(-2)),
			},
			// Cluster 3 is filtered out as it does not meet the affinity requirements.
			memberCluster3EastCanary: nil,
			// Cluster 4 is not picked as it ranks lower by name in alphanumeric order.
			memberCluster4CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-999)),
			},
			// Cluster 5 is picked in the 3rd iteration, as
			// * placing resources on it does not violate the DoNotSchedule topology spread
			//   constraint (it decreases the skew by one), though it violates the ScheduleAnyway
			//   topology spread constraint (skew becomes 3, limit is 2); and
			// * it is ranked higher by name in alphanumeric order.
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-999)),
			},
			// Cluster 6 is picked in the second iteration, as
			// * placing resources on it does not violate any topology spread constraints (it
			//   increase the skew by 1 for environment-based topology spread constraint); and
			// * it is ranked higher by name in alphanumeric order.
			memberCluster6WestProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			// Cluster 7 is not picked as it does not meet the affinity requirements.
			memberCluster7WestCanary: nil,
		}

		scoreByClusterAfter := map[string]*placementv1beta1.ClusterScore{
			// Cluster 1 is not picked as it is not preferred per affinity configuration.
			memberCluster1EastProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			// Cluster 2 is not picked as it is not preferred per affinity configuration.
			memberCluster2EastProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			// Cluster 3 is not picked as it is not preferred per affinity configuration.
			memberCluster3EastCanary: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			// Cluster 4 is picked in the 3rd iteration, as
			// * placing resources on it increases the skew by 1 (so does other clusters); and
			// * it is preferred per affinity configuration (with a weight of 50);
			// * it is ranked higher by name in alphanumeric order.
			memberCluster4CentralProd: {
				AffinityScore:       ptr.To(int32(50)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			// Cluster 5 is picked in the first iteration, as
			// * placing resources on it increases the skew by 1 (so does other clusters); and
			// * it is preferred per affinity configuration (with a weight of 50);
			// * it is ranked higher by name in alphanumeric order.
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(50)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			// Cluster 6 is not picked as it is not preferred per affinity configuration.
			memberCluster6WestProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			// Cluster 7 is picked in the second iteration, as
			// * placing resources on it decreases the skew by 1; and
			// * it is ranked higher by name in alphanumeric order.
			memberCluster7WestCanary: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(1)),
			},
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			affinityBefore := &placementv1beta1.Affinity{
				ClusterAffinity: &placementv1beta1.ClusterAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
						{
							Weight: 40,
							Preference: placementv1beta1.ClusterSelectorTerm{
								LabelSelector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										regionLabel: "east",
									},
								},
							},
						},
					},
					RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
						ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
							{
								LabelSelector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										envLabel: "prod",
									},
								},
							},
						},
					},
				},
			}
			topologySpreadConstraintsBefore := []placementv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:           ptr.To(int32(2)),
					TopologyKey:       envLabel,
					WhenUnsatisfiable: placementv1beta1.ScheduleAnyway,
				},
				{
					MaxSkew:           ptr.To(int32(1)),
					TopologyKey:       regionLabel,
					WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
				},
			}
			createPickNCRPWithPolicySnapshot(crpName, numOfClusters, affinityBefore, topologySpreadConstraintsBefore, policySnapshotNameBefore)

			// Verify that scheduling has been completed.
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, len(wantPickedClustersBefore))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")

			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClustersBefore, scoreByClusterBefore, crpName, policySnapshotNameBefore)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")

			// Update the CRP and create a new policy snapshot.
			affinityAfter := &placementv1beta1.Affinity{
				ClusterAffinity: &placementv1beta1.ClusterAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
						{
							Weight: 50,
							Preference: placementv1beta1.ClusterSelectorTerm{
								LabelSelector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										regionLabel: "central",
									},
								},
							},
						},
					},
				},
			}
			topologySpreadConstraintsAfter := []placementv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:           ptr.To(int32(2)),
					TopologyKey:       envLabel,
					WhenUnsatisfiable: placementv1beta1.ScheduleAnyway,
				},
			}
			updatePickNCRPWithNewAffinityAndTopologySpreadConstraints(crpName, affinityAfter, topologySpreadConstraintsAfter, policySnapshotNameBefore, policySnapshotNameAfter)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, len(wantPickedClustersAfter))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClustersAfter, scoreByClusterAfter, crpName, policySnapshotNameAfter)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(int(numOfClusters), wantPickedClustersAfter, wantNotPickedClustersAfter, wantFilteredClustersAfter, scoreByClusterAfter, policySnapshotNameAfter)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})
})
