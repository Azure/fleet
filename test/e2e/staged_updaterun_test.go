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

package e2e

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

// Note that this container will run in parallel with other containers.
var _ = Describe("test RP rollout with staged update run", Label("resourceplacement"), func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
	strategyName := fmt.Sprintf(stagedUpdateRunStrategyNameTemplate, GinkgoParallelProcess())
	testNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeEach(OncePerOrdered, func() {
		// Create a test namespace and a configMap inside it on the hub cluster.
		createWorkResources()

		// Create the CRP with Namespace-only selector.
		createNamespaceOnlyCRP(crpName)

		By("should update CRP status as expected")
		crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterEach(OncePerOrdered, func() {
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("Test resource rollout and rollback with staged update run", Ordered, func() {
		updateRunNames := []string{}
		var strategy *placementv1beta1.StagedUpdateStrategy
		var oldConfigMap, newConfigMap corev1.ConfigMap

		BeforeAll(func() {
			// Create the RP with external rollout strategy.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: testNamespace,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.ExternalRolloutStrategyType,
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")

			// Create the stagedUpdateStrategy.
			strategy = createStagedUpdateStrategySucceed(strategyName, testNamespace)

			for i := 0; i < 3; i++ {
				updateRunNames = append(updateRunNames, fmt.Sprintf(stagedUpdateRunNameWithSubIndexTemplate, GinkgoParallelProcess(), i))
			}

			oldConfigMap = appConfigMap()
			newConfigMap = appConfigMap()
			newConfigMap.Data["data"] = testConfigMapDataValue
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the RP.
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: testNamespace}, allMemberClusters)

			// Remove all the stagedUpdateRuns.
			for _, name := range updateRunNames {
				ensureStagedUpdateRunDeletion(name, testNamespace)
			}

			// Delete the stagedUpdateStrategy.
			ensureStagedUpdateRunStrategyDeletion(strategyName, testNamespace)
		})

		It("Should not rollout any resources to member clusters as there's no update run yet", checkIfRemovedConfigMapFromAllMemberClustersConsistently)

		It("Should have the latest resource snapshot", func() {
			validateLatestResourceSnapshot(rpName, testNamespace, resourceSnapshotIndex1st)
		})

		It("Should successfully schedule the rp", func() {
			validateLatestSchedulingPolicySnapshot(rpName, testNamespace, policySnapshotIndex1st, 3)
		})

		It("Should update rp status as pending rollout", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{"", "", ""}, []bool{false, false, false}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[0], testNamespace, rpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should rollout resources to member-cluster-2 only and complete stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[1]})
			checkIfRemovedConfigMapFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[0], allMemberClusters[2]})

			By("Validating rp status as member-cluster-2 updated")
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{"", resourceSnapshotIndex1st, ""}, []bool{false, true, false}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)

			validateAndApproveNamespacedApprovalRequests(updateRunNames[0], testNamespace, envCanary)
		})

		It("Should rollout resources to member-cluster-1 first because of its name", func() {
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[0]})
		})

		It("Should rollout resources to all the members and complete the staged update run successfully", func() {
			surSucceededActual := stagedUpdateRunStatusSucceededActual(updateRunNames[0], testNamespace, policySnapshotIndex1st, len(allMemberClusters), defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(surSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s/%s succeeded", testNamespace, updateRunNames[0])
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Should update rp status as completed", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Should update the configmap successfully on hub but not change member clusters", func() {
			updateConfigMapSucceed(&newConfigMap)

			for _, cluster := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(cluster, &oldConfigMap)
				Consistently(configMapActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep configmap %s data as expected", oldConfigMap.Name)
			}
		})

		It("Should not update rp status, should still be completed", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Consistently(rpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Should create a new latest resource snapshot", func() {
			rsList := &placementv1beta1.ResourceSnapshotList{}
			Eventually(func() error {
				if err := hubClient.List(ctx, rsList, client.InNamespace(testNamespace), client.MatchingLabels{placementv1beta1.PlacementTrackingLabel: rpName, placementv1beta1.IsLatestSnapshotLabel: "true"}); err != nil {
					return fmt.Errorf("failed to list the resourcesnapshot: %w", err)
				}
				if len(rsList.Items) != 1 {
					return fmt.Errorf("got %d latest resourcesnapshots, want 1", len(rsList.Items))
				}
				if rsList.Items[0].Labels[placementv1beta1.ResourceIndexLabel] != resourceSnapshotIndex2nd {
					return fmt.Errorf("got resource snapshot index %s, want %s", rsList.Items[0].Labels[placementv1beta1.ResourceIndexLabel], resourceSnapshotIndex2nd)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed get the new latest resourcensnapshot")
		})

		It("Should create a new staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[1], testNamespace, rpName, resourceSnapshotIndex2nd, strategyName)
		})

		It("Should rollout resources to member-cluster-2 only and complete stage canary", func() {
			By("Verify that the new configmap is updated on member-cluster-2")
			configMapActual := configMapPlacedOnClusterActual(allMemberClusters[1], &newConfigMap)
			Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update to the new configmap %s on cluster %s", newConfigMap.Name, allMemberClusterNames[1])
			By("Verify that the configmap is not updated on member-cluster-1 and member-cluster-3")
			for _, cluster := range []*framework.Cluster{allMemberClusters[0], allMemberClusters[2]} {
				configMapActual := configMapPlacedOnClusterActual(cluster, &oldConfigMap)
				Consistently(configMapActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep configmap %s data as expected", newConfigMap.Name)
			}

			By("Validating rp status as member-cluster-2 updated")
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex2nd, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)

			validateAndApproveNamespacedApprovalRequests(updateRunNames[1], testNamespace, envCanary)
		})

		It("Should rollout resources to member-cluster-1 and member-cluster-3 too and complete the staged update run successfully", func() {
			surSucceededActual := stagedUpdateRunStatusSucceededActual(updateRunNames[1], testNamespace, policySnapshotIndex1st, len(allMemberClusters), defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(surSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s/%s succeeded", testNamespace, updateRunNames[1])
			By("Verify that new the configmap is updated on all member clusters")
			for idx := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(allMemberClusters[idx], &newConfigMap)
				Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update to the new configmap %s on cluster %s as expected", newConfigMap.Name, allMemberClusterNames[idx])
			}
		})

		It("Should update rp status as completed", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex2nd, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex2nd, resourceSnapshotIndex2nd, resourceSnapshotIndex2nd}, []bool{true, true, true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Should create a new staged update run with old resourceSnapshotIndex successfully to rollback", func() {
			createStagedUpdateRunSucceed(updateRunNames[2], testNamespace, rpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should rollback resources to member-cluster-2 only and completes stage canary", func() {
			By("Verify that the configmap is rolled back on member-cluster-2")
			configMapActual := configMapPlacedOnClusterActual(allMemberClusters[1], &oldConfigMap)
			Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to rollback the configmap change on cluster %s", allMemberClusterNames[1])
			By("Verify that the configmap is not rolled back on member-cluster-1 and member-cluster-3")
			for _, cluster := range []*framework.Cluster{allMemberClusters[0], allMemberClusters[2]} {
				configMapActual := configMapPlacedOnClusterActual(cluster, &newConfigMap)
				Consistently(configMapActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep configmap %s data as expected", newConfigMap.Name)
			}

			By("Validating rp status as member-cluster-2 updated")
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames,
				[]string{resourceSnapshotIndex2nd, resourceSnapshotIndex1st, resourceSnapshotIndex2nd}, []bool{true, true, true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)

			validateAndApproveNamespacedApprovalRequests(updateRunNames[2], testNamespace, envCanary)
		})

		It("Should rollback resources to member-cluster-1 and member-cluster-3 too and complete the staged update run successfully", func() {
			surSucceededActual := stagedUpdateRunStatusSucceededActual(updateRunNames[2], testNamespace, policySnapshotIndex1st, len(allMemberClusters), defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(surSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[1])
			for idx := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(allMemberClusters[idx], &oldConfigMap)
				Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to rollback the configmap %s data on cluster %s as expected", oldConfigMap.Name, allMemberClusterNames[idx])
			}
		})

		It("Should update rp status as completed", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})
	})

	Context("Test cluster scale out and shrink using pickFixed policy with staged update run", Ordered, func() {
		var strategy *placementv1beta1.StagedUpdateStrategy
		updateRunNames := []string{}

		BeforeAll(func() {
			// Create the RP with external rollout strategy and pick fixed policy.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: testNamespace,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{allMemberClusterNames[0], allMemberClusterNames[1]}, // member-cluster-1 and member-cluster-2
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.ExternalRolloutStrategyType,
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")

			// Create the stagedUpdateStrategy.
			strategy = createStagedUpdateStrategySucceed(strategyName, testNamespace)

			for i := 0; i < 3; i++ {
				updateRunNames = append(updateRunNames, fmt.Sprintf(stagedUpdateRunNameWithSubIndexTemplate, GinkgoParallelProcess(), i))
			}
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the RP.
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: testNamespace}, allMemberClusters)

			// Remove all the stagedUpdateRuns.
			for _, name := range updateRunNames {
				ensureStagedUpdateRunDeletion(name, testNamespace)
			}

			// Delete the stagedUpdateStrategy.
			ensureStagedUpdateRunStrategyDeletion(strategyName, testNamespace)
		})

		It("Should not rollout any resources to member clusters as there's no update run yet", checkIfRemovedConfigMapFromAllMemberClustersConsistently)

		It("Should have the latest resource snapshot", func() {
			validateLatestResourceSnapshot(rpName, testNamespace, resourceSnapshotIndex1st)
		})

		It("Should successfully schedule the rp", func() {
			validateLatestSchedulingPolicySnapshot(rpName, testNamespace, policySnapshotIndex1st, 2)
		})

		It("Should update rp status as pending rollout", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames[:2], []string{"", ""}, []bool{false, false}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[0], testNamespace, rpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should rollout resources to member-cluster-2 only and complete stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[1]})
			checkIfRemovedConfigMapFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[0], allMemberClusters[2]})

			By("Validating rp status as member-cluster-2 updated")
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames[:2], []string{"", resourceSnapshotIndex1st}, []bool{false, true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)

			validateAndApproveNamespacedApprovalRequests(updateRunNames[0], testNamespace, envCanary)
		})

		It("Should rollout resources to member-cluster-1 too but not member-cluster-3 and complete the staged update run successfully", func() {
			surSucceededActual := stagedUpdateRunStatusSucceededActual(updateRunNames[0], testNamespace, policySnapshotIndex1st, 2, defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0]}}, nil, nil, nil)
			Eventually(surSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[0])
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[0], allMemberClusters[1]})
			checkIfRemovedConfigMapFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[2]})
		})

		It("Should update rp status as completed", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames[:2],
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Update the rp to pick member-cluster-3 too", func() {
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: rpName, Namespace: testNamespace}, rp); err != nil {
					return fmt.Errorf("Failed to get the rp: %w", err)
				}
				rp.Spec.Policy.ClusterNames = allMemberClusterNames
				return hubClient.Update(ctx, rp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the rp to pick member-cluster-3 too")
		})

		It("Should successfully schedule the rp", func() {
			validateLatestSchedulingPolicySnapshot(rpName, testNamespace, policySnapshotIndex2nd, 3)
		})

		It("Should update rp status as rollout pending", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, ""}, []bool{false, false, false}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[1], testNamespace, rpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should still have resources on member-cluster-1 and member-cluster-2 only and completes stage canary", func() {
			// this check is meaningless as resources were already placed on member-cluster-1 and member-cluster-2
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[0], allMemberClusters[1]})
			// TODO: need a way to check the status of staged update run that is completed partially.
			checkIfRemovedConfigMapFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[2]})

			By("Validating rp status as member-cluster-2 updated")
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, ""}, []bool{false, true, false}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to keep RP %s/%s status as expected", testNamespace, rpName)

			validateAndApproveNamespacedApprovalRequests(updateRunNames[1], testNamespace, envCanary)
		})

		It("Should rollout resources to member-cluster-3 too and complete the staged update run successfully", func() {
			surSucceededActual := stagedUpdateRunStatusSucceededActual(updateRunNames[1], testNamespace, policySnapshotIndex2nd, 3, defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(surSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s/%s succeeded", testNamespace, updateRunNames[1])
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Should update rp status as completed", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Update the rp to only keep member-cluster-3", func() {
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: rpName, Namespace: testNamespace}, rp); err != nil {
					return fmt.Errorf("failed to get the rp: %w", err)
				}
				rp.Spec.Policy.ClusterNames = []string{allMemberClusterNames[2]}
				return hubClient.Update(ctx, rp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the rp to only keep member-cluster-3")
		})

		It("Should successfully schedule the rp", func() {
			validateLatestSchedulingPolicySnapshot(rpName, testNamespace, policySnapshotIndex3rd, 1)
		})

		It("Should update rp status as rollout pending with member-cluster-3 only", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex1st, false, []string{allMemberClusterNames[2]}, []string{resourceSnapshotIndex1st}, []bool{false}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[2], testNamespace, rpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should still have resources on all member clusters and complete stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersConsistently(allMemberClusters)

			By("Validating rp status keeping as rollout pending with member-cluster-3 only")
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex1st, false, []string{allMemberClusterNames[2]}, []string{resourceSnapshotIndex1st}, []bool{false}, nil, nil)
			Consistently(rpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)

			validateAndApproveNamespacedApprovalRequests(updateRunNames[2], testNamespace, envCanary)
		})

		It("Should remove resources on member-cluster-1 and member-cluster-2 and complete the staged update run successfully", func() {
			// need to go through two stages
			surSucceededActual := stagedUpdateRunStatusSucceededActual(updateRunNames[2], testNamespace, policySnapshotIndex3rd, 1, defaultApplyStrategy, &strategy.Spec, [][]string{{}, {allMemberClusterNames[2]}}, []string{allMemberClusterNames[0], allMemberClusterNames[1]}, nil, nil)
			Eventually(surSucceededActual, 2*updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s/%s succeeded", testNamespace, updateRunNames[2])
			checkIfRemovedConfigMapFromMemberClusters([]*framework.Cluster{allMemberClusters[0], allMemberClusters[1]})
			checkIfPlacedWorkResourcesOnMemberClustersConsistently([]*framework.Cluster{allMemberClusters[2]})
		})

		It("Should update rp status as completed with member-cluster-3 only", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex1st, true, []string{allMemberClusterNames[2]}, []string{resourceSnapshotIndex1st}, []bool{true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to keep RP %s/%s status as expected", testNamespace, rpName)
		})
	})

	Context("Test cluster scale out and shrink using pickN policy with namespaced staged update run", Ordered, func() {
		var strategy *placementv1beta1.StagedUpdateStrategy
		updateRunNames := []string{}

		BeforeAll(func() {
			// Create the RP with external rollout strategy and pick N=1 policy.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: testNamespace,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)), // pick 1 cluster
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.ExternalRolloutStrategyType,
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")

			// Create the stagedUpdateStrategy.
			strategy = createStagedUpdateStrategySucceed(strategyName, testNamespace)

			for i := 0; i < 3; i++ {
				updateRunNames = append(updateRunNames, fmt.Sprintf(stagedUpdateRunNameWithSubIndexTemplate, GinkgoParallelProcess(), i))
			}
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the RP.
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: testNamespace}, allMemberClusters)

			// Remove all the stagedUpdateRuns.
			for _, name := range updateRunNames {
				ensureStagedUpdateRunDeletion(name, testNamespace)
			}

			// Delete the stagedUpdateStrategy.
			ensureStagedUpdateRunStrategyDeletion(strategyName, testNamespace)
		})

		It("Should not rollout any resources to member clusters as there's no update run yet", checkIfRemovedConfigMapFromAllMemberClustersConsistently)

		It("Should have the latest resource snapshot", func() {
			validateLatestResourceSnapshot(rpName, testNamespace, resourceSnapshotIndex1st)
		})

		It("Should successfully schedule the rp", func() {
			validateLatestSchedulingPolicySnapshot(rpName, testNamespace, policySnapshotIndex1st, 1)
		})

		It("Should update rp status as pending rollout", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames[2:], []string{""}, []bool{false}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Should create a namespaced staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[0], testNamespace, rpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should not rollout any resources to member clusters and complete stage canary", func() {
			checkIfRemovedConfigMapFromMemberClustersConsistently(allMemberClusters)

			By("Validating rp status as pending rollout still")
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames[2:], []string{""}, []bool{false}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)

			validateAndApproveNamespacedApprovalRequests(updateRunNames[0], testNamespace, envCanary)
		})

		It("Should rollout resources to member-cluster-3 and complete the staged update run successfully", func() {
			surSucceededActual := stagedUpdateRunStatusSucceededActual(updateRunNames[0], testNamespace, policySnapshotIndex1st, 1, defaultApplyStrategy, &strategy.Spec, [][]string{{}, {allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(surSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s/%s succeeded", testNamespace, updateRunNames[0])
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[2]})
			checkIfRemovedConfigMapFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[0], allMemberClusters[1]})
		})

		It("Should update rp status as completed", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames[2:],
				[]string{resourceSnapshotIndex1st}, []bool{true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Update the rp to pick all 3 member clusters", func() {
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: rpName, Namespace: testNamespace}, rp); err != nil {
					return fmt.Errorf("Failed to get the rp: %w", err)
				}
				rp.Spec.Policy.NumberOfClusters = ptr.To(int32(3)) // pick 3 clusters
				return hubClient.Update(ctx, rp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the rp to pick all 3 member clusters")
		})

		It("Should successfully schedule the rp without creating a new policy snapshot", func() {
			validateLatestSchedulingPolicySnapshot(rpName, testNamespace, policySnapshotIndex1st, 3)
		})

		It("Should update rp status as rollout pending", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{"", "", resourceSnapshotIndex1st}, []bool{false, false, true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Should create a namespaced staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[1], testNamespace, rpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should still have resources on member-cluster-2 and member-cluster-3 only and completes stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters[1:])
			// TODO: need a way to check the status of staged update run that is not fully completed yet.
			checkIfRemovedConfigMapFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[0]})

			By("Validating rp status as member-cluster-2 updated")
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{"", resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{false, true, true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to keep RP %s/%s status as expected", testNamespace, rpName)

			validateAndApproveNamespacedApprovalRequests(updateRunNames[1], testNamespace, envCanary)
		})

		It("Should rollout resources to member-cluster-1 too and complete the staged update run successfully", func() {
			surSucceededActual := stagedUpdateRunStatusSucceededActual(updateRunNames[1], testNamespace, policySnapshotIndex1st, 3, defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(surSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s/%s succeeded", testNamespace, updateRunNames[1])
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Should update rp status as completed", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Update the rp to only keep 2 clusters (member-cluster-2 and member-cluster-3)", func() {
			Eventually(func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: rpName, Namespace: testNamespace}, rp); err != nil {
					return fmt.Errorf("failed to get the rp: %w", err)
				}
				rp.Spec.Policy.NumberOfClusters = ptr.To(int32(2)) // pick 2 clusters
				return hubClient.Update(ctx, rp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the rp to only keep member-cluster-3")
		})

		It("Should successfully schedule the rp without creating a new policy snapshot", func() {
			validateLatestSchedulingPolicySnapshot(rpName, testNamespace, policySnapshotIndex1st, 2)
		})

		It("Should update rp status as rollout completed with member-cluster-2 and member-cluster-3", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames[1:], []string{resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)
		})

		It("Should create a namespaced staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[2], testNamespace, rpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should still have resources on all member clusters and complete stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersConsistently(allMemberClusters)

			By("Validating rp status keeping as rollout completed with member-cluster-2 and member-cluster-3 only")
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames[1:], []string{resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true}, nil, nil)
			Consistently(rpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update RP %s/%s status as expected", testNamespace, rpName)

			validateAndApproveNamespacedApprovalRequests(updateRunNames[2], testNamespace, envCanary)
		})

		It("Should remove resources on member-cluster-1 and complete the staged update run successfully", func() {
			surSucceededActual := stagedUpdateRunStatusSucceededActual(updateRunNames[2], testNamespace, policySnapshotIndex1st, 2, defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[2]}}, []string{allMemberClusterNames[0]}, nil, nil)
			Eventually(surSucceededActual, 2*updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s/%s succeeded", testNamespace, updateRunNames[2])
			checkIfRemovedConfigMapFromMemberClusters([]*framework.Cluster{allMemberClusters[0]})
			checkIfPlacedWorkResourcesOnMemberClustersConsistently([]*framework.Cluster{allMemberClusters[1], allMemberClusters[2]})
		})

		It("Should update rp status as completed with member-cluster-2 and member-cluster-3 only", func() {
			rpStatusUpdatedActual := rpStatusWithExternalStrategyActual(appConfigMapIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames[1:], []string{resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true}, nil, nil)
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to keep RP %s/%s status as expected", testNamespace, rpName)
		})
	})
})

func createStagedUpdateStrategySucceed(strategyName, namespace string) *placementv1beta1.StagedUpdateStrategy {
	strategy := &placementv1beta1.StagedUpdateStrategy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strategyName,
			Namespace: namespace,
		},
		Spec: placementv1beta1.UpdateStrategySpec{
			Stages: []placementv1beta1.StageConfig{
				{
					Name: envCanary,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							envLabelName: envCanary, // member-cluster-2
						},
					},
					AfterStageTasks: []placementv1beta1.AfterStageTask{
						{
							Type: placementv1beta1.AfterStageTaskTypeApproval,
						},
						{
							Type: placementv1beta1.AfterStageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{
								Duration: time.Second * 5,
							},
						},
					},
				},
				{
					Name: envProd,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							envLabelName: envProd, // member-cluster-1 and member-cluster-3
						},
					},
				},
			},
		},
	}
	Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create StagedUpdateStrategy")
	return strategy
}

func validateLatestSchedulingPolicySnapshot(rpName, namespace, wantPolicySnapshotIndex string, wantSelectedClusterCount int) {
	Eventually(func() (string, error) {
		var policySnapshotList placementv1beta1.SchedulingPolicySnapshotList
		if err := hubClient.List(ctx, &policySnapshotList, client.InNamespace(namespace), client.MatchingLabels{
			placementv1beta1.PlacementTrackingLabel: rpName,
			placementv1beta1.IsLatestSnapshotLabel:  "true",
		}); err != nil {
			return "", fmt.Errorf("failed to list the latest scheduling policy snapshot: %w", err)
		}
		if len(policySnapshotList.Items) != 1 {
			return "", fmt.Errorf("failed to find the latest scheduling policy snapshot")
		}
		latestPolicySnapshot := policySnapshotList.Items[0]
		if !condition.IsConditionStatusTrue(latestPolicySnapshot.GetCondition(string(placementv1beta1.PolicySnapshotScheduled)), latestPolicySnapshot.Generation) {
			return "", fmt.Errorf("the latest scheduling policy snapshot is not scheduled yet")
		}

		selectedClusterCount := 0
		for _, decision := range latestPolicySnapshot.Status.ClusterDecisions {
			if decision.Selected {
				selectedClusterCount++
			}
		}
		if selectedClusterCount != wantSelectedClusterCount {
			return "", fmt.Errorf("want %d selected clusters, got %d", wantSelectedClusterCount, selectedClusterCount)
		}
		return latestPolicySnapshot.Labels[placementv1beta1.PolicyIndexLabel], nil
	}, eventuallyDuration, eventuallyInterval).Should(Equal(wantPolicySnapshotIndex), "Policy snapshot index does not match")
}

func validateLatestResourceSnapshot(rpName, namespace, wantResourceSnapshotIndex string) {
	Eventually(func() (string, error) {
		rsList := &placementv1beta1.ResourceSnapshotList{}
		if err := hubClient.List(ctx, rsList, client.InNamespace(namespace), client.MatchingLabels{
			placementv1beta1.PlacementTrackingLabel: rpName,
			placementv1beta1.IsLatestSnapshotLabel:  "true",
		}); err != nil {
			return "", fmt.Errorf("failed to list the latestresourcesnapshot: %w", err)
		}
		if len(rsList.Items) != 1 {
			return "", fmt.Errorf("got %d resourcesnapshots, want 1", len(rsList.Items))
		}
		return rsList.Items[0].Labels[placementv1beta1.ResourceIndexLabel], nil
	}, eventuallyDuration, eventuallyInterval).Should(Equal(wantResourceSnapshotIndex), "Resource snapshot index does not match")
}

func createStagedUpdateRunSucceed(updateRunName, namespace, rpName, resourceSnapshotIndex, strategyName string) {
	updateRun := &placementv1beta1.StagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      updateRunName,
			Namespace: namespace,
		},
		Spec: placementv1beta1.UpdateRunSpec{
			PlacementName:            rpName,
			ResourceSnapshotIndex:    resourceSnapshotIndex,
			StagedUpdateStrategyName: strategyName,
		},
	}
	Expect(hubClient.Create(ctx, updateRun)).To(Succeed(), "Failed to create StagedUpdateRun %s", updateRunName)
}

func validateAndApproveNamespacedApprovalRequests(updateRunName, namespace, stageName string) {
	Eventually(func() error {
		appReqList := &placementv1beta1.ApprovalRequestList{}
		if err := hubClient.List(ctx, appReqList, client.InNamespace(namespace), client.MatchingLabels{
			placementv1beta1.TargetUpdatingStageNameLabel: stageName,
			placementv1beta1.TargetUpdateRunLabel:         updateRunName,
		}); err != nil {
			return fmt.Errorf("failed to list approval requests: %w", err)
		}

		if len(appReqList.Items) != 1 {
			return fmt.Errorf("got %d approval requests, want 1", len(appReqList.Items))
		}
		appReq := &appReqList.Items[0]
		meta.SetStatusCondition(&appReq.Status.Conditions, metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(placementv1beta1.ApprovalRequestConditionApproved),
			ObservedGeneration: appReq.GetGeneration(),
			Reason:             "lgtm",
		})
		return hubClient.Status().Update(ctx, appReq)
	}, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get or approve approval request")
}
