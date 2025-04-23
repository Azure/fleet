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

// This suite features test cases that verify the behavior of the scheduler watchers.

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/propertyprovider"
	"go.goms.io/fleet/test/e2e/framework"
)

const (
	fakeClusterName1ForWatcherTests = "fake-cluster-1"
	fakeClusterName2ForWatcherTests = "fake-cluster-2"

	labelNameForWatcherTests  = "test-label"
	labelValueForWatcherTests = "test-value"

	nodeNameForWatcherTests = "test-node"
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should place resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should pick only healthy clusters in the system", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can add a new member cluster", func() {
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil, nil)

			// Mark the newly created member cluster as healthy.
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
		})

		It("should propagate works for the new cluster; can mark them as available", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster along with other healthy clusters", func() {
			targetClusterNames := allMemberClusterNames
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("cluster becomes eligible for PickAll CRPs, label changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil, nil)
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
											LabelSelector: &metav1.LabelSelector{
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should not pick any cluster", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, nil, "0")
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
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("cluster becomes eligible for PickAll CRPs, node count changed", Serial, Ordered, func() {
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
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.NodeCountProperty,
														Operator: placementv1beta1.PropertySelectorGreaterThan,
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
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should not pick any cluster", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Should not select any cluster")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Should not select any cluster")
		})

		It("can add a new node", func() {
			Eventually(func() error {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeNameForWatcherTests,
					},
				}

				return memberCluster3WestProdClient.Create(ctx, node)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create a new node")
		})
		// need to wait for the resource change to be propagated back to the hub cluster
		It("should pick the new cluster", func() {
			targetClusterNames := []string{memberCluster3WestProdName}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, 2*workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster3WestProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		AfterAll(func() {
			// Delete the node.
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeNameForWatcherTests,
				},
			}
			Expect(client.IgnoreNotFound(memberCluster3WestProdClient.Delete(ctx, node))).To(Succeed())
			Eventually(func() error {
				node := &corev1.Node{}
				if err := memberCluster3WestProdClient.Get(ctx, types.NamespacedName{Name: nodeNameForWatcherTests}, node); !errors.IsNotFound(err) {
					return err
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete the node")

			ensureCRPAndRelatedResourcesDeleted(crpName, []*framework.Cluster{memberCluster3WestProd})
		})
	})

	Context("cluster becomes eligible for PickAll CRPs, health condition changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil, nil)
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should place resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should pick only healthy clusters in the system", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the cluster as healthy", func() {
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
		})

		It("should propagate works for the cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the cluster, along with other healthy clusters", func() {
			targetClusterNames := allMemberClusterNames
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
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
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, labels, nil)
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
											LabelSelector: &metav1.LabelSelector{
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
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
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
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
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, labels, nil)
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster, along with other healthy clusters", func() {
			targetClusterNames := allMemberClusterNames
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the new cluster as unhealthy", func() {
			markMemberClusterAsUnhealthy(fakeClusterName1ForWatcherTests)
		})

		It("should keep the cluster in the scheduling decision", func() {
			targetClusterNames := allMemberClusterNames
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("selected cluster becomes ineligible for PickAll CRPs, left", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil, nil)
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster, along with other healthy clusters", func() {
			targetClusterNames := allMemberClusterNames
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the cluster as left", func() {
			markMemberClusterAsLeft(fakeClusterName1ForWatcherTests)
		})

		It("should remove the cluster from the scheduling decision", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("cluster becomes ineligible for PickAll CRPs, capacity changed", Serial, Ordered, func() {
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
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.TotalCPUCapacityProperty,
														Operator: placementv1beta1.PropertySelectorLessThan,
														Values: []string{
															"10000",
														},
													},
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should pick all clusters", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Should not select any cluster")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Should not select any cluster")
		})

		It("can add a new node", func() {
			Eventually(func() error {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeNameForWatcherTests,
					},
				}
				if err := memberCluster3WestProdClient.Create(ctx, node); err != nil && !errors.IsAlreadyExists(err) {
					return err
				}

				// Update the node's capacity.
				if err := memberCluster3WestProdClient.Get(ctx, types.NamespacedName{Name: nodeNameForWatcherTests}, node); err != nil {
					return err
				}
				node.Status.Capacity = corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("10000"),
				}
				return memberCluster3WestProdClient.Status().Update(ctx, node)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create and update a new node")
		})

		It("should keep the cluster in the scheduling decision", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			// Delete the node.
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeNameForWatcherTests,
				},
			}
			Expect(client.IgnoreNotFound(memberCluster3WestProdClient.Delete(ctx, node))).To(Succeed())
			Eventually(func() error {
				node := &corev1.Node{}
				if err := memberCluster3WestProdClient.Get(ctx, types.NamespacedName{Name: nodeNameForWatcherTests}, node); !errors.IsNotFound(err) {
					return err
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete the node")

			ensureCRPAndRelatedResourcesDeleted(crpName, []*framework.Cluster{memberCluster3WestProd})
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should report in CRP status that the cluster is not available", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, []string{fakeClusterName1ForWatcherTests}, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can add a new member cluster", func() {
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil, nil)

			// Mark the newly created member cluster as healthy.
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("cluster becomes eligible for PickFixed CRPs, health condition changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil, nil)
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should report in CRP status that the cluster is not available", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, []string{fakeClusterName1ForWatcherTests}, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the cluster as healthy", func() {
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("selected cluster becomes ineligible for PickFixed CRPs, left", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil, nil)
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the new cluster as left", func() {
			markMemberClusterAsLeft(fakeClusterName1ForWatcherTests)
		})

		It("should report in CRP status that the cluster becomes not available", func() {
			// resource are still applied in the cluster, crp available condition is true though the scheduled condition is false.
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, []string{fakeClusterName1ForWatcherTests}, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("selected cluster becomes ineligible for PickFixed CRPs, health condition changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil, nil)
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the new cluster as unhealthy", func() {
			markMemberClusterAsUnhealthy(fakeClusterName1ForWatcherTests)
		})

		It("should keep the cluster as picked", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Should keep the cluster as picked")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("cluster becomes eligible for unfulfilled PickN CRPs, just joined", Serial, Ordered, func() {
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
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(4)),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should place resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should pick only healthy clusters in the system", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, []string{fakeClusterName1ForWatcherTests}, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can add a new member cluster", func() {
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil, nil)

			// Mark the newly created member cluster as healthy.
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster along with other healthy clusters", func() {
			var targetClusterNames []string
			targetClusterNames = append(targetClusterNames, allMemberClusterNames...)
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("cluster becomes eligible for unfulfilled PickN CRPs, label changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, nil, nil)
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
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should not pick any cluster", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, []string{fakeClusterName1ForWatcherTests}, "0")
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
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("cluster becomes eligible for unfulfilled PickN CRPs, health condition changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, map[string]string{labelNameForWatcherTests: labelValueForWatcherTests}, nil)
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
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should not pick any cluster", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, []string{fakeClusterName1ForWatcherTests}, "0")
			Eventually(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Should not select any cluster")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Should not select any cluster")
		})

		It("can mark the cluster as healthy", func() {
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
		})

		It("should propagate works for the updated cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("cluster becomes eligible for unfulfilled PickN CRPs, topology spread balanced", Serial, Ordered, func() {
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
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(5)),
						TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
							{
								MaxSkew:           ptr.To(int32(1)),
								TopologyKey:       regionLabelName,
								WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
							},
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should place resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should pick only healthy clusters in the system", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, []string{fakeClusterName1ForWatcherTests, fakeClusterName2ForWatcherTests}, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can add a new member cluster in a region which would violate the topology spread constraint", func() {
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, map[string]string{regionLabelName: regionEast}, nil)
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
		})

		It("should not pick the member cluster", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, []string{fakeClusterName1ForWatcherTests, fakeClusterName2ForWatcherTests}, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can add a new member cluster in a region which would re-balance the topology spread", func() {
			createMemberCluster(fakeClusterName2ForWatcherTests, hubClusterSAName, map[string]string{regionLabelName: regionWest}, nil)
			markMemberClusterAsHealthy(fakeClusterName2ForWatcherTests)
		})

		It("should propagate works for both new clusters; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName2ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick both new clusters, along with other clusters", func() {
			var targetClusterNames []string
			targetClusterNames = append(targetClusterNames, allMemberClusterNames...)
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests, fakeClusterName2ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName2ForWatcherTests)
		})
	})

	Context("cluster becomes eligible for unfulfilled PickN CRPs, capacity changed", Serial, Ordered, func() {
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
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.AllocatableMemoryCapacityProperty,
														Operator: placementv1beta1.PropertySelectorGreaterThan,
														Values: []string{
															"10000Gi",
														},
													},
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should not pick any cluster", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, []string{memberCluster3WestProdName}, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Should not select any cluster")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Should not select any cluster")
		})

		It("can add a new node", func() {
			Eventually(func() error {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeNameForWatcherTests,
					},
				}
				if err := memberCluster3WestProdClient.Create(ctx, node); err != nil && !errors.IsAlreadyExists(err) {
					return err
				}

				// Update the node's capacity.
				if err := memberCluster3WestProdClient.Get(ctx, types.NamespacedName{Name: nodeNameForWatcherTests}, node); err != nil {
					return err
				}
				node.Status.Capacity = corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("10000Gi"),
				}
				node.Status.Allocatable = corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("10000Gi"),
				}
				return memberCluster3WestProdClient.Status().Update(ctx, node)
			}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create and update a new node")
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{memberCluster3WestProdName}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should place resources on the picked clusters", func() {
			targetClusters := []*framework.Cluster{memberCluster3WestProd}
			for _, cluster := range targetClusters {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the picked clusters")
			}
		})

		AfterAll(func() {
			// Delete the node.
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeNameForWatcherTests,
				},
			}
			Expect(client.IgnoreNotFound(memberCluster3WestProdClient.Delete(ctx, node))).To(Succeed())
			Eventually(func() error {
				node := &corev1.Node{}
				if err := memberCluster3WestProdClient.Get(ctx, types.NamespacedName{Name: nodeNameForWatcherTests}, node); !errors.IsNotFound(err) {
					return err
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete the node")

			ensureCRPAndRelatedResourcesDeleted(crpName, []*framework.Cluster{memberCluster3WestProd})
		})
	})

	Context("cluster appears for unfulfilled PickN CRPs, topology spread constraints violated", Serial, Ordered, func() {
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
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(4)),
						TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
							{
								MaxSkew:           ptr.To(int32(1)),
								TopologyKey:       regionLabelName,
								WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
							},
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should place resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should pick only healthy clusters in the system", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, []string{fakeClusterName1ForWatcherTests}, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can add a new member cluster in a region which would violate the topology spread constraint", func() {
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, map[string]string{regionLabelName: regionEast}, nil)
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
		})

		It("should not pick the member cluster", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, []string{fakeClusterName1ForWatcherTests}, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("selected cluster becomes ineligible for fulfilled PickN CRPs, left", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, map[string]string{labelNameForWatcherTests: labelValueForWatcherTests}, nil)
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
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(4)),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster, along with other healthy clusters", func() {
			targetClusterNames := allMemberClusterNames
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the cluster as left", func() {
			markMemberClusterAsLeft(fakeClusterName1ForWatcherTests)
		})

		It("should remove the cluster from the scheduling decision", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, []string{fakeClusterName1ForWatcherTests}, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("selected cluster becomes ineligible for fulfilled PickN CRPs, label changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, map[string]string{labelNameForWatcherTests: labelValueForWatcherTests}, nil)
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
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can update the member cluster label", func() {
			Eventually(func() error {
				memberCluster := clusterv1beta1.MemberCluster{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: fakeClusterName1ForWatcherTests}, &memberCluster); err != nil {
					return err
				}

				memberCluster.Labels = map[string]string{}
				return hubClient.Update(ctx, &memberCluster)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the member cluster with new label")
		})

		It("should keep the cluster as picked", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Should keep the cluster as picked")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("selected cluster becomes ineligible for fulfilled PickN CRPs, health condition changed", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create a new member cluster.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, map[string]string{labelNameForWatcherTests: labelValueForWatcherTests}, nil)
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
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
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
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should propagate works for the new cluster; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick the new cluster", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can mark the member cluster as unhealthy", func() {
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
		})

		It("should keep the cluster as picked", func() {
			targetClusterNames := []string{fakeClusterName1ForWatcherTests}
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Should keep the cluster as picked")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
		})
	})

	Context("selected cluster becomes ineligible for fulfilled PickN CRPs, topology spread constraint violated", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create new member clusters.
			createMemberCluster(fakeClusterName1ForWatcherTests, hubClusterSAName, map[string]string{regionLabelName: regionEast}, nil)
			createMemberCluster(fakeClusterName2ForWatcherTests, hubClusterSAName, map[string]string{regionLabelName: regionWest}, nil)
			// Mark the newly created member clusters as healthy.
			markMemberClusterAsHealthy(fakeClusterName1ForWatcherTests)
			markMemberClusterAsHealthy(fakeClusterName2ForWatcherTests)

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
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(5)),
						TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
							{
								MaxSkew:           ptr.To(int32(1)),
								TopologyKey:       regionLabelName,
								WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
							},
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should place resources on all real member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

		It("should propagate works for both new clusters; can mark them as applied", func() {
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName1ForWatcherTests, crpName, workResourceIdentifiers())
			verifyWorkPropagationAndMarkAsAvailable(fakeClusterName2ForWatcherTests, crpName, workResourceIdentifiers())
		})

		It("should pick both new clusters, along with other clusters", func() {
			var targetClusterNames []string
			targetClusterNames = append(targetClusterNames, allMemberClusterNames...)
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests, fakeClusterName2ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("can update the labels of the cluster in the region with less clusters", func() {
			Eventually(func() error {
				memberCluster := clusterv1beta1.MemberCluster{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: fakeClusterName2ForWatcherTests}, &memberCluster); err != nil {
					return err
				}

				memberCluster.Labels = map[string]string{}
				return hubClient.Update(ctx, &memberCluster)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the member cluster with new label")
		})

		It("should keep the cluster as picked", func() {
			var targetClusterNames []string
			targetClusterNames = append(targetClusterNames, allMemberClusterNames...)
			targetClusterNames = append(targetClusterNames, fakeClusterName1ForWatcherTests, fakeClusterName2ForWatcherTests)
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), targetClusterNames, nil, "0")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName1ForWatcherTests)
			ensureMemberClusterAndRelatedResourcesDeletion(fakeClusterName2ForWatcherTests)
		})
	})

	Context("selected cluster becomes ineligible for fulfilled PickN CRPs, node count changed", Serial, Ordered, func() {
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
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)),
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.NodeCountProperty,
														Operator: placementv1beta1.PropertySelectorEqualTo,
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
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())
		})

		It("should pick one cluster", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster3WestProdName}, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Should not select any cluster")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Should not select any cluster")
		})

		It("can add a new node", func() {
			Eventually(func() error {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeNameForWatcherTests,
					},
				}
				return memberCluster3WestProdClient.Create(ctx, node)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create and update a new node")
		})

		It("should keep the cluster in the scheduling decision", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster3WestProdName}, nil, "0")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			// Delete the node.
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeNameForWatcherTests,
				},
			}
			Expect(client.IgnoreNotFound(memberCluster3WestProdClient.Delete(ctx, node))).To(Succeed())
			Eventually(func() error {
				node := &corev1.Node{}
				if err := memberCluster3WestProdClient.Get(ctx, types.NamespacedName{Name: nodeNameForWatcherTests}, node); !errors.IsNotFound(err) {
					return err
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete the node")

			ensureCRPAndRelatedResourcesDeleted(crpName, []*framework.Cluster{memberCluster3WestProd})
		})
	})
})
