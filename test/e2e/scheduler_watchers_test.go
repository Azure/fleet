/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// This suite features test cases that verify the behavior of the scheduler watchers.

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	fakeClusterName1ForWatcherTests = "fake-cluster-1"
	fakeClusterName2ForWatcherTests = "fake-cluster-2"

	labelNameForWatcherTests  = "test-label"
	labelValueForWatcherTests = "test-value"
)

// Typically, the scheduler watchers watch for changes in the following objects:
// * CRP (whether an object has been deleted); and
// * ClusterSchedulingPolicySnapshot (whether one has been created or its labels and/or
//   annotations have been updated); and
// * MemberCluster (see its code base for specifics).
//
// This test suite, however, covers only changes on the MemberCluster front, as the
// other two cases have been implicitly covered by other test suites, e.g.,
// CRP removal, upscaling/downscaling, resource-only changes, etc.

// Note that most of the cases below are Serial ones, as they manipulate the list of member
// clusters in the test environment directly, which may incur side effects when running in
// parallel with other test cases.
var _ = Describe("responding to specific member cluster changes", func() {
	Context("cluster becomes eligible for PickAll CRPs, just joined", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should place resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should pick only healthy clusters in the system", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can add a new member cluster", func() {
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil)

			// Mark the newly created member cluster as healthy.
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsApplied(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster along with other healthy clusters", func() {
			targetClusterNames := allMemberClusterNames
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("cluster becomes eligible for PickAll CRPs, label changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil)
			// Mark the newly created member cluster as healthy.
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: metav1.LabelSelector{
												MatchLabels: map[string]string{
													labelNameForWatcherTests: labelValueForWatcherTests,
												},
											},
										},
									},
								},
							},
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should not pick any cluster", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, nil)
			Eventually(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Should not select any cluster")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Should not select any cluster")
		})

		It("can update the member cluster label", func() {
			Eventually(func() error {
				memberCluster := clusterv1beta1.MemberCluster{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: fakeClusterName1ForWatcherTests}, &memberCluster); err != nil {
					return err
				}

				memberCluster.Labels = map[string]string{
					labelNameForWatcherTests: labelValueForWatcherTests,
				}
				return hubClient.Update(ctx, &memberCluster)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the member cluster with new label")
		})

		It("should propagate works for the updated cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsApplied(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("cluster becomes eligible for PickAll CRPs, health condition changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil)
			// Mark the newly created member cluster as unhealthy.
			markMemberClusterAsUnhealthy(fakeClusterName1ForWatcherTests)

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should place resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should pick only healthy clusters in the system", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the cluster as healthy", func() {
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
		})

		It("should propagate works for the cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsApplied(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the cluster, along with other healthy clusters", func() {
			targetClusterNames := allMemberClusterNames
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("selected cluster becomes ineligible for PickAll CRPs, label changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			labels := map[string]string{
				labelNameForWatcherTests: labelValueForWatcherTests,
			}
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, labels)
			// Mark the newly created member cluster as healthy.
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: metav1.LabelSelector{
												MatchLabels: map[string]string{
													labelNameForWatcherTests: labelValueForWatcherTests,
												},
											},
										},
									},
								},
							},
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsApplied(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can drop the label from the cluster", func() {
			Eventually(func() error {
				mcObj := &clusterv1beta1.MemberCluster{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: fakeClusterName1ForWatcherTests}, mcObj); err != nil {
					return err
				}

				mcObj.Labels = nil
				return hubClient.Update(ctx, mcObj)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to drop the label from the cluster")
		})

		It("should keep the cluster in the scheduling decision (ignored during execution semantics)", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("selected cluster becomes ineligible for PickAll CRPs, health condition changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			labels := map[string]string{
				labelNameForWatcherTests: labelValueForWatcherTests,
			}
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, labels)
			// Mark the newly created member cluster as healthy.
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should propgate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsApplied(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster, along with other healthy clusters", func() {
			targetClusterNames := allMemberClusterNames
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the new cluster as unhealthy", func() {
			markMemberClusterAsUnhealthy(fakeClusterName1ForWatcherTests)
		})

		It("should keep the cluster in the scheduling decision", func() {
			targetClusterNames := allMemberClusterNames
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("selected cluster becomes ineligible for PickAll CRPs, left", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil)
			// Mark the newly created member cluster as healthy.
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsApplied(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster, along with other healthy clusters", func() {
			targetClusterNames := allMemberClusterNames
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the cluster as left", func() {
			markMemberClusterAsLeft(fakeClusterName1ForWatcherTests)
		})

		It("should remove the cluster from the scheduling decision", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("cluster becomes eligible for PickFixed CRPs, just joined", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{fakeClusterName1ForWatcherTests},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should report in CRP status that the cluster is not available", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, []string{fakeClusterName1ForWatcherTests})
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can add a new member cluster", func() {
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil)

			// Mark the newly created member cluster as healthy.
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsApplied(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("cluster becomes eligible for PickFixed CRPs, health condition changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil)
			// Mark the newly created member cluster as unhealthy.
			markMemberClusterAsUnhealthy(fakeClusterName1ForWatcherTests)

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{fakeClusterName1ForWatcherTests},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should report in CRP status that the cluster is not available", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, []string{fakeClusterName1ForWatcherTests})
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the cluster as healthy", func() {
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsApplied(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("selected cluster becomes ineligible for PickFixed CRPs, left", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil)
			// Mark the newly created member cluster as healthy.
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{fakeClusterName1ForWatcherTests},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsApplied(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the new cluster as left", func() {
			markMemberClusterAsLeft(fakeClusterName1ForWatcherTests)
		})

		It("should report in CRP status that the cluster becomes not available", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, []string{fakeClusterName1ForWatcherTests})
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("selected cluster becomes ineligible for PickFixed CRPs, health condition changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil)
			// Mark the newly created member cluster as healthy.
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{fakeClusterName1ForWatcherTests},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsApplied(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the new cluster as unhealthy", func() {
			markMemberClusterAsUnhealthy(fakeClusterName1ForWatcherTests)
		})

		It("should keep the cluster as picked", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Should keep the cluster as picked")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeletion(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})
})
