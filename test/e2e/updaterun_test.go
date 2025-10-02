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
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

const (
	// The current stage wait between clusters are 15 seconds
	updateRunEventuallyDuration = time.Minute

	resourceSnapshotIndex1st = "0"
	resourceSnapshotIndex2nd = "1"
	policySnapshotIndex1st   = "0"
	policySnapshotIndex2nd   = "1"
	policySnapshotIndex3rd   = "2"

	testConfigMapDataValue = "new"
)

var (
	// defaultApplyStrategy is the default apply strategy that CRP mutation webhook will use
	defaultApplyStrategy = &placementv1beta1.ApplyStrategy{
		ComparisonOption: "PartialComparison",
		WhenToApply:      "Always",
		Type:             "ClientSideApply",
		WhenToTakeOver:   "Always",
	}
)

// Note that this container will run in parallel with other containers.
var _ = Describe("test CRP rollout with staged update run", func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	strategyName := fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess())

	Context("Test resource rollout and rollback with staged update run", Ordered, func() {
		updateRunNames := []string{}
		var strategy *placementv1beta1.ClusterStagedUpdateStrategy
		var oldConfigMap, newConfigMap corev1.ConfigMap

		BeforeAll(func() {
			// Create a test namespace and a configMap inside it on the hub cluster.
			createWorkResources()

			// Create the CRP with external rollout strategy.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.ExternalRolloutStrategyType,
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Create the clusterStagedUpdateStrategy.
			strategy = createStagedUpdateStrategySucceed(strategyName)

			for i := 0; i < 3; i++ {
				updateRunNames = append(updateRunNames, fmt.Sprintf(updateRunNameWithSubIndexTemplate, GinkgoParallelProcess(), i))
			}

			oldConfigMap = appConfigMap()
			newConfigMap = appConfigMap()
			newConfigMap.Data["data"] = testConfigMapDataValue
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

			// Remove all the clusterStagedUpdateRuns.
			for _, name := range updateRunNames {
				ensureUpdateRunDeletion(name)
			}

			// Delete the clusterStagedUpdateStrategy.
			ensureUpdateRunStrategyDeletion(strategyName)
		})

		It("Should not rollout any resources to member clusters as there's no update run yet", checkIfRemovedWorkResourcesFromAllMemberClustersConsistently)

		It("Should have the latest resource snapshot", func() {
			validateLatestResourceSnapshot(crpName, resourceSnapshotIndex1st)
		})

		It("Should successfully schedule the crp", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st, 3)
		})

		It("Should update crp status as pending rollout", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{"", "", ""}, []bool{false, false, false}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[0], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should rollout resources to member-cluster-2 only and complete stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[1]})
			checkIfRemovedWorkResourcesFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[0], allMemberClusters[2]})

			By("Validating crp status as member-cluster-2 updated")
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{"", resourceSnapshotIndex1st, ""}, []bool{false, true, false}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

			validateAndApproveClusterApprovalRequests(updateRunNames[0], envCanary)
		})

		It("Should rollout resources to member-cluster-1 first because of its name", func() {
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[0]})
		})

		It("Should rollout resources to all the members and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[0], policySnapshotIndex1st, len(allMemberClusters), defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[0])
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Should update crp status as completed", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Should update the configmap successfully on hub but not change member clusters", func() {
			updateConfigMapSucceed(&newConfigMap)

			for _, cluster := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(cluster, &oldConfigMap)
				Consistently(configMapActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep configmap %s data as expected", oldConfigMap.Name)
			}
		})

		It("Should not update crp status, should still be completed", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep CRP %s status as expected", crpName)
		})

		It("Should create a new latest resource snapshot", func() {
			crsList := &placementv1beta1.ClusterResourceSnapshotList{}
			Eventually(func() error {
				if err := hubClient.List(ctx, crsList, client.MatchingLabels{placementv1beta1.PlacementTrackingLabel: crpName, placementv1beta1.IsLatestSnapshotLabel: "true"}); err != nil {
					return fmt.Errorf("failed to list the resourcesnapshot: %w", err)
				}
				if len(crsList.Items) != 1 {
					return fmt.Errorf("got %d latest resourcesnapshots, want 1", len(crsList.Items))
				}
				if crsList.Items[0].Labels[placementv1beta1.ResourceIndexLabel] != resourceSnapshotIndex2nd {
					return fmt.Errorf("got resource snapshot index %s, want %s", crsList.Items[0].Labels[placementv1beta1.ResourceIndexLabel], resourceSnapshotIndex2nd)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed get the new latest resourcensnapshot")
		})

		It("Should create a new staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[1], crpName, resourceSnapshotIndex2nd, strategyName)
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

			By("Validating crp status as member-cluster-2 updated")
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex2nd, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

			validateAndApproveClusterApprovalRequests(updateRunNames[1], envCanary)
		})

		It("Should rollout resources to member-cluster-1 and member-cluster-3 too and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[1], policySnapshotIndex1st, len(allMemberClusters), defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[1])
			By("Verify that new the configmap is updated on all member clusters")
			for idx := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(allMemberClusters[idx], &newConfigMap)
				Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update to the new configmap %s on cluster %s as expected", newConfigMap.Name, allMemberClusterNames[idx])
			}
		})

		It("Should update crp status as completed", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex2nd, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex2nd, resourceSnapshotIndex2nd, resourceSnapshotIndex2nd}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Should create a new staged update run with old resourceSnapshotIndex successfully to rollback", func() {
			createStagedUpdateRunSucceed(updateRunNames[2], crpName, resourceSnapshotIndex1st, strategyName)
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

			By("Validating crp status as member-cluster-2 updated")
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames,
				[]string{resourceSnapshotIndex2nd, resourceSnapshotIndex1st, resourceSnapshotIndex2nd}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

			validateAndApproveClusterApprovalRequests(updateRunNames[2], envCanary)
		})

		It("Should rollback resources to member-cluster-1 and member-cluster-3 too and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[2], policySnapshotIndex1st, len(allMemberClusters), defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[1])
			for idx := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(allMemberClusters[idx], &oldConfigMap)
				Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to rollback the configmap %s data on cluster %s as expected", oldConfigMap.Name, allMemberClusterNames[idx])
			}
		})

		It("Should update crp status as completed", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})
	})

	Context("Test cluster scale out and shrink using pickFixed policy with staged update run", Ordered, func() {
		var strategy *placementv1beta1.ClusterStagedUpdateStrategy
		updateRunNames := []string{}

		BeforeAll(func() {
			// Create a test namespace and a configMap inside it on the hub cluster.
			createWorkResources()

			// Create the CRP with external rollout strategy and pick fixed policy.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{allMemberClusterNames[0], allMemberClusterNames[1]}, // member-cluster-1 and member-cluster-2
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.ExternalRolloutStrategyType,
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Create the clusterStagedUpdateStrategy.
			strategy = createStagedUpdateStrategySucceed(strategyName)

			for i := 0; i < 3; i++ {
				updateRunNames = append(updateRunNames, fmt.Sprintf(updateRunNameWithSubIndexTemplate, GinkgoParallelProcess(), i))
			}
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

			// Remove all the clusterStagedUpdateRuns.
			for _, name := range updateRunNames {
				ensureUpdateRunDeletion(name)
			}

			// Delete the clusterStagedUpdateStrategy.
			ensureUpdateRunStrategyDeletion(strategyName)
		})

		It("Should not rollout any resources to member clusters as there's no update run yet", checkIfRemovedWorkResourcesFromAllMemberClustersConsistently)

		It("Should have the latest resource snapshot", func() {
			validateLatestResourceSnapshot(crpName, resourceSnapshotIndex1st)
		})

		It("Should successfully schedule the crp", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st, 2)
		})

		It("Should update crp status as pending rollout", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames[:2], []string{"", ""}, []bool{false, false}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[0], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should rollout resources to member-cluster-2 only and complete stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[1]})
			checkIfRemovedWorkResourcesFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[0], allMemberClusters[2]})

			By("Validating crp status as member-cluster-2 updated")
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames[:2], []string{"", resourceSnapshotIndex1st}, []bool{false, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

			validateAndApproveClusterApprovalRequests(updateRunNames[0], envCanary)
		})

		It("Should rollout resources to member-cluster-1 too but not member-cluster-3 and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[0], policySnapshotIndex1st, 2, defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[0])
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[0], allMemberClusters[1]})
			checkIfRemovedWorkResourcesFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[2]})
		})

		It("Should update crp status as completed", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames[:2],
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Update the crp to pick member-cluster-3 too", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: crpName}, crp); err != nil {
					return fmt.Errorf("Failed to get the crp: %w", err)
				}
				crp.Spec.Policy.ClusterNames = allMemberClusterNames
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the crp to pick member-cluster-3 too")
		})

		It("Should successfully schedule the crp", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex2nd, 3)
		})

		It("Should update crp status as rollout pending", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, ""}, []bool{false, false, false}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[1], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should still have resources on member-cluster-1 and member-cluster-2 only and completes stage canary", func() {
			// this check is meaningless as resources were already placed on member-cluster-1 and member-cluster-2
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[0], allMemberClusters[1]})
			// TODO: need a way to check the status of staged update run that is completed partially.
			checkIfRemovedWorkResourcesFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[2]})

			By("Validating crp status as member-cluster-2 updated")
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, ""}, []bool{false, true, false}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to keep CRP %s status as expected", crpName)

			validateAndApproveClusterApprovalRequests(updateRunNames[1], envCanary)
		})

		It("Should rollout resources to member-cluster-3 too and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[1], policySnapshotIndex2nd, 3, defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[1])
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Should update crp status as completed", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Update the crp to only keep member-cluster-3", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: crpName}, crp); err != nil {
					return fmt.Errorf("failed to get the crp: %w", err)
				}
				crp.Spec.Policy.ClusterNames = []string{allMemberClusterNames[2]}
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the crp to only keep member-cluster-3")
		})

		It("Should successfully schedule the crp", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex3rd, 1)
		})

		It("Should update crp status as rollout pending with member-cluster-3 only", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, false, []string{allMemberClusterNames[2]}, []string{resourceSnapshotIndex1st}, []bool{false}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[2], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should still have resources on all member clusters and complete stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersConsistently(allMemberClusters)

			By("Validating crp status keeping as rollout pending with member-cluster-3 only")
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, false, []string{allMemberClusterNames[2]}, []string{resourceSnapshotIndex1st}, []bool{false}, nil, nil)
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

			validateAndApproveClusterApprovalRequests(updateRunNames[2], envCanary)
		})

		It("Should remove resources on member-cluster-1 and member-cluster-2 and complete the staged update run successfully", func() {
			// need to go through two stages
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[2], policySnapshotIndex3rd, 1, defaultApplyStrategy, &strategy.Spec, [][]string{{}, {allMemberClusterNames[2]}}, []string{allMemberClusterNames[0], allMemberClusterNames[1]}, nil, nil)
			Eventually(updateRunSucceededActual, 2*updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[2])
			checkIfRemovedWorkResourcesFromMemberClusters([]*framework.Cluster{allMemberClusters[0], allMemberClusters[1]})
			checkIfPlacedWorkResourcesOnMemberClustersConsistently([]*framework.Cluster{allMemberClusters[2]})
		})

		It("Should update crp status as completed with member-cluster-3 only", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, []string{allMemberClusterNames[2]}, []string{resourceSnapshotIndex1st}, []bool{true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to keep CRP %s status as expected", crpName)
		})
	})

	Context("Test cluster scale out and shrink using pickN policy with staged update run", Ordered, func() {
		var strategy *placementv1beta1.ClusterStagedUpdateStrategy
		updateRunNames := []string{}

		BeforeAll(func() {
			// Create a test namespace and a configMap inside it on the hub cluster.
			createWorkResources()

			// Create the CRP with external rollout strategy and pick N=1 policy.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(1)), // pick 1 cluster
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.ExternalRolloutStrategyType,
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Create the clusterStagedUpdateStrategy.
			strategy = createStagedUpdateStrategySucceed(strategyName)

			for i := 0; i < 3; i++ {
				updateRunNames = append(updateRunNames, fmt.Sprintf(updateRunNameWithSubIndexTemplate, GinkgoParallelProcess(), i))
			}
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

			// Remove all the clusterStagedUpdateRuns.
			for _, name := range updateRunNames {
				ensureUpdateRunDeletion(name)
			}

			// Delete the clusterStagedUpdateStrategy.
			ensureUpdateRunStrategyDeletion(strategyName)
		})

		It("Should not rollout any resources to member clusters as there's no update run yet", checkIfRemovedWorkResourcesFromAllMemberClustersConsistently)

		It("Should have the latest resource snapshot", func() {
			validateLatestResourceSnapshot(crpName, resourceSnapshotIndex1st)
		})

		It("Should successfully schedule the crp", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st, 1)
		})

		It("Should update crp status as pending rollout", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames[2:], []string{""}, []bool{false}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[0], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should not rollout any resources to member clusters and complete stage canary", func() {
			checkIfRemovedWorkResourcesFromMemberClustersConsistently(allMemberClusters)

			By("Validating crp status as pending rollout still")
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames[2:], []string{""}, []bool{false}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

			validateAndApproveClusterApprovalRequests(updateRunNames[0], envCanary)
		})

		It("Should rollout resources to member-cluster-3 and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[0], policySnapshotIndex1st, 1, defaultApplyStrategy, &strategy.Spec, [][]string{{}, {allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[0])
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[2]})
			checkIfRemovedWorkResourcesFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[0], allMemberClusters[1]})
		})

		It("Should update crp status as completed", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames[2:],
				[]string{resourceSnapshotIndex1st}, []bool{true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Update the crp to pick all 3 member clusters", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: crpName}, crp); err != nil {
					return fmt.Errorf("Failed to get the crp: %w", err)
				}
				crp.Spec.Policy.NumberOfClusters = ptr.To(int32(3)) // pick 3 clusters
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the crp to pick all 3 member clusters")
		})

		It("Should successfully schedule the crp without creating a new policy snapshot", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st, 3)
		})

		It("Should update crp status as rollout pending", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{"", "", resourceSnapshotIndex1st}, []bool{false, false, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[1], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should still have resources on member-cluster-2 and member-cluster-3 only and completes stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters[1:])
			// TODO: need a way to check the status of staged update run that is not fully completed yet.
			checkIfRemovedWorkResourcesFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[0]})

			By("Validating crp status as member-cluster-2 updated")
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{"", resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{false, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to keep CRP %s status as expected", crpName)

			validateAndApproveClusterApprovalRequests(updateRunNames[1], envCanary)
		})

		It("Should rollout resources to member-cluster-1 too and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[1], policySnapshotIndex1st, 3, defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[1])
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Should update crp status as completed", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Update the crp to only keep 2 clusters (member-cluster-2 and member-cluster-3)", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: crpName}, crp); err != nil {
					return fmt.Errorf("failed to get the crp: %w", err)
				}
				crp.Spec.Policy.NumberOfClusters = ptr.To(int32(2)) // pick 2 clusters
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the crp to only keep member-cluster-3")
		})

		It("Should successfully schedule the crp without creating a new policy snapshot", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st, 2)
		})

		It("Should update crp status as rollout completed with member-cluster-2 and member-cluster-3", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames[1:], []string{resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[2], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should still have resources on all member clusters and complete stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersConsistently(allMemberClusters)

			By("Validating crp status keeping as rollout completed with member-cluster-2 and member-cluster-3 only")
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames[1:], []string{resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true}, nil, nil)
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

			validateAndApproveClusterApprovalRequests(updateRunNames[2], envCanary)
		})

		It("Should remove resources on member-cluster-1 and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[2], policySnapshotIndex1st, 2, defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[2]}}, []string{allMemberClusterNames[0]}, nil, nil)
			Eventually(updateRunSucceededActual, 2*updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[2])
			checkIfRemovedWorkResourcesFromMemberClusters([]*framework.Cluster{allMemberClusters[0]})
			checkIfPlacedWorkResourcesOnMemberClustersConsistently([]*framework.Cluster{allMemberClusters[1], allMemberClusters[2]})
		})

		It("Should update crp status as completed with member-cluster-2 and member-cluster-3 only", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames[1:], []string{resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to keep CRP %s status as expected", crpName)
		})
	})

	Context("Test staged update run with overrides", Ordered, func() {
		var strategy *placementv1beta1.ClusterStagedUpdateStrategy
		updateRunName := fmt.Sprintf(updateRunNameWithSubIndexTemplate, GinkgoParallelProcess(), 0)
		croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())
		roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
		roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

		var wantCROs map[string][]string
		var wantROs map[string][]placementv1beta1.NamespacedName

		BeforeAll(func() {
			// Create a test namespace and a configMap inside it on the hub cluster.
			createWorkResources()

			// Create the cro before crp so that the observed resource index is predictable.
			cro := &placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: croName,
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{regionLabelName: regionEast, envLabelName: envProd}, // member-cluster-1
											},
										},
									},
								},
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/annotations",
										Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s-0"}`, croTestAnnotationKey, croTestAnnotationValue))},
									},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, cro)).To(Succeed(), "Failed to create clusterResourceOverride %s", croName)

			// Create the ro before crp so that the observed resource index is predictable.
			ro := &placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: roNamespace,
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: configMapOverrideSelector(),
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{regionLabelName: regionEast, envLabelName: envCanary}, // member-cluster-2
											},
										},
									},
								},
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/annotations",
										Value:    apiextensionsv1.JSON{Raw: []byte(fmt.Sprintf(`{"%s": "%s-1"}`, roTestAnnotationKey, roTestAnnotationValue))},
									},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create resourceOverride %s", roName)

			// Set the wanted overrides.
			wantCROs = map[string][]string{allMemberClusterNames[0]: {croName + "-0"}} // with override snapshot index 0
			wantROs = map[string][]placementv1beta1.NamespacedName{
				allMemberClusterNames[1]: {placementv1beta1.NamespacedName{Namespace: roNamespace, Name: roName + "-0"}}, // with override snapshot index 0
			}

			// Create the CRP with external rollout strategy and pickAll policy.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.ExternalRolloutStrategyType,
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Create the clusterStagedUpdateStrategy.
			strategy = createStagedUpdateStrategySucceed(strategyName)
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

			// Delete the clusterStagedUpdateRun.
			ensureUpdateRunDeletion(updateRunName)

			// Delete the clusterStagedUpdateStrategy.
			ensureUpdateRunStrategyDeletion(strategyName)

			// Delete the clusterResourceOverride.
			cleanupClusterResourceOverride(croName)

			// Delete the resourceOverride.
			cleanupResourceOverride(roName, roNamespace)
		})

		It("Should not rollout any resources to member clusters as there's no update run yet", checkIfRemovedWorkResourcesFromAllMemberClustersConsistently)

		It("Should have the latest resource snapshot", func() {
			validateLatestResourceSnapshot(crpName, resourceSnapshotIndex1st)
		})

		It("Should update crp status as pending rollout", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{"", "", ""}, []bool{false, false, false}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Should successfully schedule the crp", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st, 3)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunName, crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should rollout resources to member-cluster-2 only and complete stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[1]})
			checkIfRemovedWorkResourcesFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[0], allMemberClusters[2]})

			By("Validating crp status as member-cluster-2 updated")
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames,
				[]string{"", resourceSnapshotIndex1st, ""}, []bool{false, true, false}, nil, wantROs)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

			validateAndApproveClusterApprovalRequests(updateRunName, envCanary)
		})

		It("Should rollout resources to member-cluster-1 and member-cluster-3 too and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunName, policySnapshotIndex1st, len(allMemberClusters), defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, wantCROs, wantROs)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunName)
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Should update crp status as completed", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, wantCROs, wantROs)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("should have override annotations on the member cluster 1 and member cluster 2", func() {
			wantCROAnnotations := map[string]string{croTestAnnotationKey: fmt.Sprintf("%s-%d", croTestAnnotationValue, 0)}
			wantROAnnotations := map[string]string{roTestAnnotationKey: fmt.Sprintf("%s-%d", roTestAnnotationValue, 1)}
			Expect(validateAnnotationOfWorkNamespaceOnCluster(allMemberClusters[0], wantCROAnnotations)).Should(Succeed(), "Failed to override the annotation of work namespace on %s", allMemberClusters[0].ClusterName)
			Expect(validateAnnotationOfConfigMapOnCluster(allMemberClusters[0], wantCROAnnotations)).Should(Succeed(), "Failed to override the annotation of configmap on %s", allMemberClusters[0].ClusterName)
			Expect(validateAnnotationOfConfigMapOnCluster(allMemberClusters[1], wantROAnnotations)).Should(Succeed(), "Failed to override the annotation of configmap on %s", allMemberClusters[1].ClusterName)
		})
	})

	Context("Test staged update run with reportDiff mode", Ordered, func() {
		var strategy *placementv1beta1.ClusterStagedUpdateStrategy
		var applyStrategy *placementv1beta1.ApplyStrategy
		updateRunName := fmt.Sprintf(updateRunNameWithSubIndexTemplate, GinkgoParallelProcess(), 0)

		BeforeAll(func() {
			// Create a test namespace and a configMap inside it on the hub cluster.
			createWorkResources()

			// Create the CRP with external rollout strategy, pickAll policy and reportDiff apply strategy.
			applyStrategy = &placementv1beta1.ApplyStrategy{
				Type:             placementv1beta1.ApplyStrategyTypeReportDiff,
				ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
				WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
				WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
			}
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type:          placementv1beta1.ExternalRolloutStrategyType,
						ApplyStrategy: applyStrategy,
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Create the clusterStagedUpdateStrategy.
			strategy = createStagedUpdateStrategySucceed(strategyName)
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

			// Delete the clusterStagedUpdateRun.
			ensureUpdateRunDeletion(updateRunName)

			// Delete the clusterStagedUpdateStrategy.
			ensureUpdateRunStrategyDeletion(strategyName)
		})

		It("Should not rollout any resources to member clusters as there's no update run yet", checkIfRemovedWorkResourcesFromAllMemberClustersConsistently)

		It("Should have the latest resource snapshot", func() {
			validateLatestResourceSnapshot(crpName, resourceSnapshotIndex1st)
		})

		It("Should update crp status as pending rollout", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{"", "", ""}, []bool{false, false, false}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Should successfully schedule the crp", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st, 3)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunName, crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should report diff for member-cluster-2 only and completes stage canary", func() {
			By("Validating crp status as member-cluster-2 diff reported")
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames,
				[]string{"", resourceSnapshotIndex1st, ""}, []bool{false, true, false}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

			validateAndApproveClusterApprovalRequests(updateRunName, envCanary)
		})

		It("Should report diff for member-cluster-1 and member-cluster-3 too and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunName, policySnapshotIndex1st, len(allMemberClusters), applyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunName)
		})

		It("Should update crp status as diff reported", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Should not rollout any resources to member clusters as it's reportDiff mode", checkIfRemovedWorkResourcesFromAllMemberClustersConsistently)
	})

	Context("Test CRP rollout strategy transition from rollingUpdate to external", Ordered, func() {
		var strategy *placementv1beta1.ClusterStagedUpdateStrategy
		updateRunName := fmt.Sprintf(updateRunNameWithSubIndexTemplate, GinkgoParallelProcess(), 0)
		var oldConfigMap, newConfigMap corev1.ConfigMap

		BeforeAll(func() {
			// Create a test namespace and a configMap inside it on the hub cluster.
			createWorkResources()

			// Create the CRP with rollingUpdate strategy initially.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Create the clusterStagedUpdateStrategy for later use.
			strategy = createStagedUpdateStrategySucceed(strategyName)

			oldConfigMap = appConfigMap()
			newConfigMap = appConfigMap()
			newConfigMap.Data["data"] = testConfigMapDataValue
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

			// Delete the clusterStagedUpdateRun.
			ensureUpdateRunDeletion(updateRunName)

			// Delete the clusterStagedUpdateStrategy.
			ensureUpdateRunStrategyDeletion(strategyName)
		})

		It("Should rollout resources to all member clusters with rollingUpdate strategy", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, resourceSnapshotIndex1st)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Update CRP to use external rollout strategy", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: crpName}, crp); err != nil {
					return fmt.Errorf("failed to get the crp: %w", err)
				}
				crp.Spec.Strategy = placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.ExternalRolloutStrategyType,
				}
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP strategy to external rollout")
		})

		It("Should update crp status to reflect external rollout strategy", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status for external strategy", crpName)
		})

		It("Update the configmap on hub but should not rollout to member clusters", func() {
			updateConfigMapSucceed(&newConfigMap)

			// Verify old configmap is still on all member clusters
			for _, cluster := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(cluster, &oldConfigMap)
				Consistently(configMapActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep old configmap %s data on cluster %s", oldConfigMap.Name, cluster.ClusterName)
			}
		})

		It("Should have the new resource snapshot but CRP status should remain completed with old snapshot", func() {
			validateLatestResourceSnapshot(crpName, resourceSnapshotIndex2nd)

			// CRP status should still show completed with old snapshot
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep CRP %s status as expected", crpName)
		})

		It("Create a staged update run with new resourceSnapshotIndex and verify rollout happens", func() {
			createStagedUpdateRunSucceed(updateRunName, crpName, resourceSnapshotIndex2nd, strategyName)

			// Verify rollout to canary cluster first
			By("Verify that the new configmap is updated on member-cluster-2 during canary stage")
			configMapActual := configMapPlacedOnClusterActual(allMemberClusters[1], &newConfigMap)
			Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update to the new configmap %s on cluster %s", newConfigMap.Name, allMemberClusterNames[1])

			validateAndApproveClusterApprovalRequests(updateRunName, envCanary)

			// Verify complete rollout
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunName, policySnapshotIndex1st, len(allMemberClusters), defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunName)

			// Verify new configmap is on all member clusters
			for _, cluster := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(cluster, &newConfigMap)
				Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update to the new configmap %s on cluster %s", newConfigMap.Name, cluster.ClusterName)
			}
		})

		It("Should update crp status as completed with new snapshot", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex2nd, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex2nd, resourceSnapshotIndex2nd, resourceSnapshotIndex2nd}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})
	})

	Context("Test kubectl-fleet approve plugin with cluster approval requests", Ordered, func() {
		var strategy *placementv1beta1.ClusterStagedUpdateStrategy
		updateRunName := fmt.Sprintf(updateRunNameWithSubIndexTemplate, GinkgoParallelProcess(), 0)

		BeforeAll(func() {
			// Create a test namespace and a configMap inside it on the hub cluster.
			createWorkResources()

			// Create the CRP with external rollout strategy.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.ExternalRolloutStrategyType,
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Create the clusterStagedUpdateStrategy.
			strategy = createStagedUpdateStrategySucceed(strategyName)
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

			// Delete the clusterStagedUpdateRun.
			ensureUpdateRunDeletion(updateRunName)

			// Delete the clusterStagedUpdateStrategy.
			ensureUpdateRunStrategyDeletion(strategyName)
		})

		It("Should create a staged update run and verify cluster approval request is created", func() {
			validateLatestResourceSnapshot(crpName, resourceSnapshotIndex1st)
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st, 3)
			createStagedUpdateRunSucceed(updateRunName, crpName, resourceSnapshotIndex1st, strategyName)

			// Verify that cluster approval request is created for canary stage.
			Eventually(func() error {
				appReqList := &placementv1beta1.ClusterApprovalRequestList{}
				if err := hubClient.List(ctx, appReqList, client.MatchingLabels{
					placementv1beta1.TargetUpdatingStageNameLabel: envCanary,
					placementv1beta1.TargetUpdateRunLabel:         updateRunName,
				}); err != nil {
					return fmt.Errorf("failed to list approval requests: %w", err)
				}

				if len(appReqList.Items) != 1 {
					return fmt.Errorf("want 1 approval request, got %d", len(appReqList.Items))
				}
				return nil
			}, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to find cluster approval request")
		})

		It("Should approve cluster approval request using kubectl-fleet approve plugin", func() {
			var approvalRequestName string

			// Get the cluster approval request name.
			Eventually(func() error {
				appReqList := &placementv1beta1.ClusterApprovalRequestList{}
				if err := hubClient.List(ctx, appReqList, client.MatchingLabels{
					placementv1beta1.TargetUpdatingStageNameLabel: envCanary,
					placementv1beta1.TargetUpdateRunLabel:         updateRunName,
				}); err != nil {
					return fmt.Errorf("failed to list approval requests: %w", err)
				}

				if len(appReqList.Items) != 1 {
					return fmt.Errorf("want 1 approval request, got %d", len(appReqList.Items))
				}

				approvalRequestName = appReqList.Items[0].Name
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get approval request name")

			// Use kubectl-fleet approve plugin to approve the request
			cmd := exec.Command(fleetBinaryPath, "approve", "clusterapprovalrequest",
				"--hubClusterContext", "kind-hub",
				"--name", approvalRequestName)
			output, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), "kubectl-fleet approve failed: %s", string(output))

			// Verify the approval request is approved
			Eventually(func() error {
				var appReq placementv1beta1.ClusterApprovalRequest
				if err := hubClient.Get(ctx, client.ObjectKey{Name: approvalRequestName}, &appReq); err != nil {
					return fmt.Errorf("failed to get approval request: %w", err)
				}

				approvedCondition := meta.FindStatusCondition(appReq.Status.Conditions, string(placementv1beta1.ApprovalRequestConditionApproved))
				if approvedCondition == nil {
					return fmt.Errorf("approved condition not found")
				}
				if approvedCondition.Status != metav1.ConditionTrue {
					return fmt.Errorf("approved condition status is %s, want True", approvedCondition.Status)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to verify approval request is approved")
		})

		It("Should complete the staged update run after approval", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunName, policySnapshotIndex1st, len(allMemberClusters), defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunName)
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Should update crp status as completed", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})
	})

	Context("Test CRP rollout strategy transition from external to rollingUpdate", Ordered, func() {
		var strategy *placementv1beta1.ClusterStagedUpdateStrategy
		updateRunName := fmt.Sprintf(updateRunNameWithSubIndexTemplate, GinkgoParallelProcess(), 0)
		var oldConfigMap, newConfigMap corev1.ConfigMap

		BeforeAll(func() {
			// Create a test namespace and a configMap inside it on the hub cluster.
			createWorkResources()

			// Create the CRP with external rollout strategy initially.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.ExternalRolloutStrategyType,
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

			// Create the clusterStagedUpdateStrategy.
			strategy = createStagedUpdateStrategySucceed(strategyName)

			oldConfigMap = appConfigMap()
			newConfigMap = appConfigMap()
			newConfigMap.Data["data"] = testConfigMapDataValue
		})

		AfterAll(func() {
			// Remove the custom deletion blocker finalizer from the CRP.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

			// Delete the clusterStagedUpdateRun.
			ensureUpdateRunDeletion(updateRunName)

			// Delete the clusterStagedUpdateStrategy.
			ensureUpdateRunStrategyDeletion(strategyName)
		})

		It("Should not rollout any resources to member clusters with external strategy", checkIfRemovedWorkResourcesFromAllMemberClustersConsistently)

		It("Should have the latest resource snapshot", func() {
			validateLatestResourceSnapshot(crpName, resourceSnapshotIndex1st)
		})

		It("Should update crp status as pending rollout", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(nil, "", false, allMemberClusterNames, []string{"", "", ""}, []bool{false, false, false}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Create updateRun and verify resources are rolled out", func() {
			createStagedUpdateRunSucceed(updateRunName, crpName, resourceSnapshotIndex1st, strategyName)

			validateAndApproveClusterApprovalRequests(updateRunName, envCanary)

			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunName, policySnapshotIndex1st, len(allMemberClusters), defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunName)

			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Should update crp status as completed", func() {
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Update the configmap on hub but should not rollout to member clusters with external strategy", func() {
			updateConfigMapSucceed(&newConfigMap)

			// Verify old configmap is still on all member clusters
			for _, cluster := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(cluster, &oldConfigMap)
				Consistently(configMapActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep old configmap %s data on cluster %s", oldConfigMap.Name, cluster.ClusterName)
			}
		})

		It("Should have new resource snapshot but CRP status should remain completed with old snapshot", func() {
			validateLatestResourceSnapshot(crpName, resourceSnapshotIndex2nd)

			// CRP status should still show completed with old snapshot
			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep CRP %s status as expected", crpName)
		})

		It("Update CRP to use rollingUpdate strategy", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: crpName}, crp); err != nil {
					return fmt.Errorf("failed to get the crp: %w", err)
				}
				crp.Spec.Strategy = placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				}
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP strategy to rollingUpdate")
		})

		It("Should automatically rollout new resources to all member clusters with rollingUpdate strategy", func() {
			// Verify CRP status shows all clusters with new resource snapshot
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, resourceSnapshotIndex2nd)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status with rollingUpdate strategy", crpName)

			// Verify new configmap is on all member clusters
			for _, cluster := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(cluster, &newConfigMap)
				Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update to the new configmap %s on cluster %s", newConfigMap.Name, cluster.ClusterName)
			}
		})
	})
})

// Note that this container cannot run in parallel with other containers.
var _ = Describe("Test member cluster join and leave flow with updateRun", Label("joinleave"), Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	strategyName := fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess())
	var strategy *placementv1beta1.ClusterStagedUpdateStrategy
	updateRunNames := []string{}

	BeforeEach(OncePerOrdered, func() {
		// Create a test namespace and a configMap inside it on the hub cluster.
		createWorkResources()

		// Create the CRP with external rollout strategy and pickAll policy.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: workResourceSelector(),
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
				},
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.ExternalRolloutStrategyType,
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
						WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
						Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
						WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")

		// Create the clusterStagedUpdateStrategy.
		strategy = &placementv1beta1.ClusterStagedUpdateStrategy{
			ObjectMeta: metav1.ObjectMeta{
				Name: strategyName,
			},
			Spec: placementv1beta1.UpdateStrategySpec{
				Stages: []placementv1beta1.StageConfig{
					{
						Name: "all",
						// Pick all clusters in the single stage.
						LabelSelector: &metav1.LabelSelector{},
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create ClusterStagedUpdateStrategy")

		for i := 0; i < 2; i++ {
			updateRunNames = append(updateRunNames, fmt.Sprintf(updateRunNameWithSubIndexTemplate, GinkgoParallelProcess(), i))
		}

		checkIfRemovedWorkResourcesFromAllMemberClustersConsistently()

		By("Validating created resource snapshot and policy snapshot")
		validateLatestResourceSnapshot(crpName, resourceSnapshotIndex1st)
		validateLatestPolicySnapshot(crpName, policySnapshotIndex1st, 3)

		By("Creating the first staged update run")
		createStagedUpdateRunSucceed(updateRunNames[0], crpName, resourceSnapshotIndex1st, strategyName)

		By("Validating staged update run has succeeded")
		updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[0], policySnapshotIndex1st, 3, defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[0], allMemberClusterNames[1], allMemberClusterNames[2]}}, nil, nil, nil)
		Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[0])

		By("Validating CRP status as completed")
		crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
			[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

		By("Validating all work resources are placed on all member clusters")
		checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)

		By("Unjoining member cluster 1")
		setMemberClusterToLeave(allMemberClusters[0])
		checkIfMemberClusterHasLeft(allMemberClusters[0])

		By("Validating CRP status as completed on member cluster 2 and member cluster 3 only")
		crpStatusUpdatedActual = crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames[1:],
			[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true}, nil, nil)
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")

		By("Validating all work resources are still placed on member cluster 1")
		checkIfPlacedWorkResourcesOnMemberClustersConsistently([]*framework.Cluster{allMemberClusters[0]})
	})

	AfterEach(OncePerOrdered, func() {
		By("Cleaning up CRP and all resources")
		// Remove the custom deletion blocker finalizer from the CRP.
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		// Remove all the clusterStagedUpdateRuns.
		By("Deleting updateRuns")
		for _, name := range updateRunNames {
			ensureUpdateRunDeletion(name)
		}

		// Delete the clusterStagedUpdateStrategy.
		By("Deleting clusterStagedUpdateStrategy")
		ensureUpdateRunStrategyDeletion(strategyName)
	})

	Context("UpdateRun should delete the binding of a left cluster but resources are kept", Label("joinleave"), Ordered, Serial, func() {
		It("Should validate binding for member cluster 1 is set to Unscheduled", func() {
			bindingUnscheduledActual := bindingStateActual(crpName, allMemberClusterNames[0], placementv1beta1.BindingStateUnscheduled)
			Eventually(bindingUnscheduledActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark binding for member cluster %s as unscheduled", allMemberClusterNames[0])
		})

		It("Should create another staged update run for the same CRP", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st, 2)
			createStagedUpdateRunSucceed(updateRunNames[1], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should complete the second staged update run and complete the CRP", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[1], policySnapshotIndex1st, 2, defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[1], allMemberClusterNames[2]}}, []string{allMemberClusterNames[0]}, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[1])

			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames[1:],
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
		})

		It("Should verify cluster member 1 binding is deleted but resources are kept on member cluster 1", func() {
			Eventually(func() error {
				bindingList := &placementv1beta1.ClusterResourceBindingList{}
				matchingLabels := client.MatchingLabels{placementv1beta1.PlacementTrackingLabel: crpName}
				if err := hubClient.List(ctx, bindingList, matchingLabels); err != nil {
					return fmt.Errorf("failed to list bindings: %w", err)
				}
				for _, binding := range bindingList.Items {
					if binding.Spec.TargetCluster == allMemberClusterNames[0] {
						return fmt.Errorf("binding for member cluster 1 still exists, want it to be deleted")
					}
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Want member cluster 1 binding to be deleted")

			checkIfPlacedWorkResourcesOnMemberClustersConsistently(allMemberClusters)
		})

		It("Should delete resources from member cluster 1", func() {
			cleanWorkResourcesOnCluster(allMemberClusters[0])
		})

		It("Should be able to rejoin member cluster 1", func() {
			setMemberClusterToJoin(allMemberClusters[0])
			checkIfMemberClusterHasJoined(allMemberClusters[0])
		})
	})

	Context("Rejoin a member cluster when resources are not changed", Label("joinleave"), Ordered, Serial, func() {
		It("Should be able to rejoin member cluster 1", func() {
			setMemberClusterToJoin(allMemberClusters[0])
			checkIfMemberClusterHasJoined(allMemberClusters[0])
		})

		It("Should reschedule to member cluster 1 and create a new staged update run successfully", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st, 3)
			createStagedUpdateRunSucceed(updateRunNames[1], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should complete the staged update run, complete CRP, and rollout resources to all member clusters", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[1], policySnapshotIndex1st, 3, defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[0], allMemberClusterNames[1], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[0])

			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Should mark binding for member cluster 1 as bound", func() {
			bindingBoundActual := bindingStateActual(crpName, allMemberClusterNames[0], placementv1beta1.BindingStateBound)
			Eventually(bindingBoundActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark binding for member cluster %s as bound", allMemberClusterNames[0])
		})
	})

	Context("Rejoin a member cluster when resources are changed", Label("joinleave"), Ordered, Serial, func() {
		var newConfigMap corev1.ConfigMap

		It("Generate a new configMap", func() {
			newConfigMap = appConfigMap()
			newConfigMap.Data["data"] = "changed"
		})

		It("Should be able to rejoin member cluster 1", func() {
			setMemberClusterToJoin(allMemberClusters[0])
			checkIfMemberClusterHasJoined(allMemberClusters[0])
		})

		It("Should update the resources on hub cluster", func() {
			updateConfigMapSucceed(&newConfigMap)
		})

		It("Should have the latest resource snapshot with updated resources", func() {
			validateLatestResourceSnapshot(crpName, resourceSnapshotIndex2nd)
		})

		It("Should reschedule to member cluster 1 and create a new staged update run successfully", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st, 3)
			createStagedUpdateRunSucceed(updateRunNames[1], crpName, resourceSnapshotIndex2nd, strategyName)
		})

		It("Should complete the staged update run, complete CRP, and rollout updated resources to all member clusters", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[1], policySnapshotIndex1st, 3, defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[0], allMemberClusterNames[1], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[1])

			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex2nd, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex2nd, resourceSnapshotIndex2nd, resourceSnapshotIndex2nd}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Should have updated configmap on all member clusters", func() {
			for _, cluster := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(cluster, &newConfigMap)
				Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place updated configmap %s on cluster %s", newConfigMap.Name, cluster.ClusterName)
			}
		})

		It("Should mark binding for member cluster 1 as bound", func() {
			bindingBoundActual := bindingStateActual(crpName, allMemberClusterNames[0], placementv1beta1.BindingStateBound)
			Eventually(bindingBoundActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark binding for member cluster %s as bound", allMemberClusterNames[0])
		})
	})

	Context("Rejoin a member cluster after orphaned resources are deleted on the member cluster", Label("joinleave"), Ordered, Serial, func() {
		It("Should delete the orphaned resources on member cluster 1", func() {
			cleanWorkResourcesOnCluster(allMemberClusters[0])
		})

		It("Should be able to rejoin member cluster 1", func() {
			setMemberClusterToJoin(allMemberClusters[0])
			checkIfMemberClusterHasJoined(allMemberClusters[0])
		})

		It("Should reschedule to member cluster 1 and create a new staged update run successfully", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st, 3)
			createStagedUpdateRunSucceed(updateRunNames[1], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should complete the staged update run, complete CRP, and re-place resources to all member clusters", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[1], policySnapshotIndex1st, 3, defaultApplyStrategy, &strategy.Spec, [][]string{{allMemberClusterNames[0], allMemberClusterNames[1], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[1])

			crpStatusUpdatedActual := crpStatusWithExternalStrategyActual(workResourceIdentifiers(), resourceSnapshotIndex1st, true, allMemberClusterNames,
				[]string{resourceSnapshotIndex1st, resourceSnapshotIndex1st, resourceSnapshotIndex1st}, []bool{true, true, true}, nil, nil)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)

			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Should mark binding for member cluster 1 as bound", func() {
			bindingBoundActual := bindingStateActual(crpName, allMemberClusterNames[0], placementv1beta1.BindingStateBound)
			Eventually(bindingBoundActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark binding for member cluster %s as bound", allMemberClusterNames[0])
		})
	})

	Context("Rejoin a member cluster and change to rollout CRP with rollingUpdate", Label("joinleave"), Ordered, Serial, func() {
		It("Should be able to rejoin member cluster 1", func() {
			setMemberClusterToJoin(allMemberClusters[0])
			checkIfMemberClusterHasJoined(allMemberClusters[0])
		})

		It("Should update the CRP rollout strategy to use rollingUpdate", func() {
			Eventually(func() error {
				var crp placementv1beta1.ClusterResourcePlacement
				if err := hubClient.Get(ctx, client.ObjectKey{Name: crpName}, &crp); err != nil {
					return fmt.Errorf("failed to get CRP %s: %w", crpName, err)
				}
				crp.Spec.Strategy = placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				}
				return hubClient.Update(ctx, &crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP rollout strategy to rolling update")
		})

		It("Should verify resources are placed to member cluster 1 and binding status becomes bound", func() {
			// Verify CRP status shows all clusters as bounded with rolling update.
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, resourceSnapshotIndex1st)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected with rolling update")

			// Verify resources are placed on all member clusters.
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)

			// Verify binding for member cluster 1 becomes bound.
			bindingBoundActual := bindingStateActual(crpName, allMemberClusterNames[0], placementv1beta1.BindingStateBound)
			Eventually(bindingBoundActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark binding for member cluster %s as bound with rolling update", allMemberClusterNames[0])
		})
	})
})

func createStagedUpdateStrategySucceed(strategyName string) *placementv1beta1.ClusterStagedUpdateStrategy {
	strategy := &placementv1beta1.ClusterStagedUpdateStrategy{
		ObjectMeta: metav1.ObjectMeta{
			Name: strategyName,
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
	Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create ClusterStagedUpdateStrategy")
	return strategy
}

func validateLatestPolicySnapshot(crpName, wantPolicySnapshotIndex string, wantSelectedClusterCount int) {
	Eventually(func() (string, error) {
		var policySnapshotList placementv1beta1.ClusterSchedulingPolicySnapshotList
		if err := hubClient.List(ctx, &policySnapshotList, client.MatchingLabels{
			placementv1beta1.PlacementTrackingLabel: crpName,
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

func validateLatestResourceSnapshot(crpName, wantResourceSnapshotIndex string) {
	Eventually(func() (string, error) {
		crsList := &placementv1beta1.ClusterResourceSnapshotList{}
		if err := hubClient.List(ctx, crsList, client.MatchingLabels{
			placementv1beta1.PlacementTrackingLabel: crpName,
			placementv1beta1.IsLatestSnapshotLabel:  "true",
		}); err != nil {
			return "", fmt.Errorf("failed to list the latestresourcesnapshot: %w", err)
		}
		if len(crsList.Items) != 1 {
			return "", fmt.Errorf("got %d resourcesnapshots, want 1", len(crsList.Items))
		}
		return crsList.Items[0].Labels[placementv1beta1.ResourceIndexLabel], nil
	}, eventuallyDuration, eventuallyInterval).Should(Equal(wantResourceSnapshotIndex), "Resource snapshot index does not match")
}

func createStagedUpdateRunSucceed(updateRunName, crpName, resourceSnapshotIndex, strategyName string) {
	updateRun := &placementv1beta1.ClusterStagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: updateRunName,
		},
		Spec: placementv1beta1.UpdateRunSpec{
			PlacementName:            crpName,
			ResourceSnapshotIndex:    resourceSnapshotIndex,
			StagedUpdateStrategyName: strategyName,
		},
	}
	Expect(hubClient.Create(ctx, updateRun)).To(Succeed(), "Failed to create ClusterStagedUpdateRun %s", updateRunName)
}

func validateAndApproveClusterApprovalRequests(updateRunName, stageName string) {
	Eventually(func() error {
		appReqList := &placementv1beta1.ClusterApprovalRequestList{}
		if err := hubClient.List(ctx, appReqList, client.MatchingLabels{
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

func updateConfigMapSucceed(newConfigMap *corev1.ConfigMap) {
	cm := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: newConfigMap.Namespace, Name: newConfigMap.Name}
	Expect(hubClient.Get(ctx, key, cm)).To(Succeed(), "Failed to get configmap %s in namespace %s", newConfigMap.Name, newConfigMap.Namespace)
	cm.Data = newConfigMap.Data
	Expect(hubClient.Update(ctx, cm)).To(Succeed(), "Failed to update configmap %s in namespace %s", newConfigMap.Name, newConfigMap.Namespace)
}
