/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/test/e2e/framework"
)

const (
	// The current stage wait between clusters are 15 seconds
	updateRunEventuallyDuration = time.Minute

	resourceSnapshotIndex1st = "0"
	resourceSnapshotIndex2nd = "1"
	policySnapshotIndex1st   = "0"
	policySnapshotIndex2nd   = "1"
	policySnapshotIndex3rd   = "2"
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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
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
			newConfigMap.Data["data"] = "new"
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
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[0], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should rollout resources to member-cluster-2 only and completes stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[1]})
			checkIfRemovedWorkResourcesFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[0], allMemberClusters[2]})
			validateAndApproveClusterApprovalRequests(updateRunNames[0], envCanary)
		})

		It("Should rollout resources to member-cluster-1 first because of its name", func() {
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[0]})
		})

		It("Should rollout resources to all the members and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[0], policySnapshotIndex1st, len(allMemberClusters), nil, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[0])
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("Should update the configmap successfully on hub but not change member clusters", func() {
			Eventually(func() error { return hubClient.Update(ctx, &newConfigMap) }, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update configmap on hub")

			for _, cluster := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(cluster, &oldConfigMap)
				Consistently(configMapActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep configmap %s data as expected", oldConfigMap.Name)
			}
		})

		It("Should create a new latest resource snapshot", func() {
			crsList := &placementv1beta1.ClusterResourceSnapshotList{}
			Eventually(func() error {
				if err := hubClient.List(ctx, crsList, client.MatchingLabels{placementv1beta1.CRPTrackingLabel: crpName, placementv1beta1.IsLatestSnapshotLabel: "true"}); err != nil {
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

		It("Should rollout resources to member-cluster-2 only and completes stage canary", func() {
			By("Verify that the new configmap is updated on member-cluster-2")
			configMapActual := configMapPlacedOnClusterActual(allMemberClusters[1], &newConfigMap)
			Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update to the new configmap %s on cluster %s", newConfigMap.Name, allMemberClusterNames[1])
			By("Verify that the configmap is not updated on member-cluster-1 and member-cluster-3")
			for _, cluster := range []*framework.Cluster{allMemberClusters[0], allMemberClusters[2]} {
				configMapActual := configMapPlacedOnClusterActual(cluster, &oldConfigMap)
				Consistently(configMapActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep configmap %s data as expected", newConfigMap.Name)
			}
			validateAndApproveClusterApprovalRequests(updateRunNames[1], envCanary)
		})

		It("Should rollout resources to member-cluster-1 and member-cluster-3 too and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[1], policySnapshotIndex1st, len(allMemberClusters), nil, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[1])
			By("Verify that new the configmap is updated on all member clusters")
			for idx := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(allMemberClusters[idx], &newConfigMap)
				Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update to the new configmap %s on cluster %s as expected", newConfigMap.Name, allMemberClusterNames[idx])
			}
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
			validateAndApproveClusterApprovalRequests(updateRunNames[2], envCanary)
		})

		It("Should rollback resources to member-cluster-1 and member-cluster-3 too and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[2], policySnapshotIndex1st, len(allMemberClusters), nil, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[1])
			for idx := range allMemberClusters {
				configMapActual := configMapPlacedOnClusterActual(allMemberClusters[idx], &oldConfigMap)
				Eventually(configMapActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to rollback the configmap %s data on cluster %s as expected", oldConfigMap.Name, allMemberClusterNames[idx])
			}
		})
	})

	Context("Test cluster scale out and shrink with staged update run", Ordered, func() {
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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
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
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[0], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should rollout resources to member-cluster-2 only and completes stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[1]})
			checkIfRemovedWorkResourcesFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[0], allMemberClusters[2]})
			validateAndApproveClusterApprovalRequests(updateRunNames[0], envCanary)
		})

		It("Should rollout resources to member-cluster-1 too but not member-cluster-3 and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[0], policySnapshotIndex1st, 2, nil, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[0])
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[0], allMemberClusters[1]})
			checkIfRemovedWorkResourcesFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[2]})
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
			validateLatestPolicySnapshot(crpName, policySnapshotIndex2nd)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[1], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should still have resources on member-cluster-1 and member-cluster-2 only and completes stage canary", func() {
			// this check is meaningless as resources were already placed on member-cluster-1 and member-cluster-2
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[0], allMemberClusters[1]})
			// TODO: need a way to check the status of staged update run that are have member-cluster-1 and member-cluster-2 updated
			checkIfRemovedWorkResourcesFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[2]})
			validateAndApproveClusterApprovalRequests(updateRunNames[1], envCanary)
		})

		It("Should rollout resources to member-cluster-3 too and complete the staged update run successfully", func() {
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[1], policySnapshotIndex2nd, 3, nil, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, nil, nil)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[1])
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
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
			validateLatestPolicySnapshot(crpName, policySnapshotIndex3rd)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunNames[2], crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should still have resources on all member clusters and complete stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersConsistently(allMemberClusters)
			validateAndApproveClusterApprovalRequests(updateRunNames[2], envCanary)
		})

		It("Should remove resources on member-cluster-1 and member-cluster-2 and complete the staged update run successfully", func() {
			// need to go through two stages
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunNames[2], policySnapshotIndex3rd, 1, nil, &strategy.Spec, [][]string{{}, {allMemberClusterNames[2]}}, []string{allMemberClusterNames[0], allMemberClusterNames[1]}, nil, nil)
			Eventually(updateRunSucceededActual, 2*updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunNames[2])
			checkIfRemovedWorkResourcesFromMemberClusters([]*framework.Cluster{allMemberClusters[0], allMemberClusters[1]})
			checkIfPlacedWorkResourcesOnMemberClustersConsistently([]*framework.Cluster{allMemberClusters[2]})
		})
	})

	Context("Test staged update run with overrides", Ordered, func() {
		var strategy *placementv1beta1.ClusterStagedUpdateStrategy
		updateRunName := fmt.Sprintf(updateRunNameWithSubIndexTemplate, GinkgoParallelProcess(), 0)
		croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())
		roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
		roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create a test namespace and a configMap inside it on the hub cluster.
			createWorkResources()

			// Create the cro before crp so that the observed resource index is predictable.
			cro := &placementv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: croName,
				},
				Spec: placementv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: workResourceSelector(),
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
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
								JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
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
			ro := &placementv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: roNamespace,
				},
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: configMapSelector(),
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{regionLabelName: regionEast, envLabelName: envCanary},
											},
										},
									},
								},
								JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
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

			// Create the CRP with external rollout strategy and pick fixed policy.
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

		It("Should successfully schedule the crp", func() {
			validateLatestPolicySnapshot(crpName, policySnapshotIndex1st)
		})

		It("Should create a staged update run successfully", func() {
			createStagedUpdateRunSucceed(updateRunName, crpName, resourceSnapshotIndex1st, strategyName)
		})

		It("Should rollout resources to member-cluster-2 only and completes stage canary", func() {
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun([]*framework.Cluster{allMemberClusters[1]})
			checkIfRemovedWorkResourcesFromMemberClustersConsistently([]*framework.Cluster{allMemberClusters[0], allMemberClusters[2]})
			validateAndApproveClusterApprovalRequests(updateRunName, envCanary)
		})

		It("Should rollout resources to member-cluster-1 and member-cluster-3 too and complete the staged update run successfully", func() {
			wantCROs := map[string][]string{allMemberClusterNames[0]: {croName + "-0"}}                                                                                       // with override snapshot index 0
			wantROs := map[string][]placementv1beta1.NamespacedName{allMemberClusterNames[1]: {placementv1beta1.NamespacedName{Namespace: roNamespace, Name: roName + "-0"}}} // with override snapshot index 0
			updateRunSucceededActual := updateRunStatusSucceededActual(updateRunName, policySnapshotIndex1st, len(allMemberClusters), nil, &strategy.Spec, [][]string{{allMemberClusterNames[1]}, {allMemberClusterNames[0], allMemberClusterNames[2]}}, nil, wantCROs, wantROs)
			Eventually(updateRunSucceededActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to validate updateRun %s succeeded", updateRunName)
			checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
		})

		It("should have override annotations on the member cluster 1 and member cluster 2", func() {
			wantCROAnnotations := map[string]string{croTestAnnotationKey: fmt.Sprintf("%s-%d", croTestAnnotationValue, 0)}
			wantROAnnotations := map[string]string{roTestAnnotationKey: fmt.Sprintf("%s-%d", roTestAnnotationValue, 1)}
			Expect(validateAnnotationOfWorkNamespaceOnCluster(allMemberClusters[0], wantCROAnnotations)).Should(Succeed(), "Failed to override the annotation of work namespace on %s", allMemberClusters[0].ClusterName)
			Expect(validateOverrideAnnotationOfConfigMapOnCluster(allMemberClusters[0], wantCROAnnotations)).Should(Succeed(), "Failed to override the annotation of configmap on %s", allMemberClusters[0].ClusterName)
			Expect(validateOverrideAnnotationOfConfigMapOnCluster(allMemberClusters[1], wantROAnnotations)).Should(Succeed(), "Failed to override the annotation of configmap on %s", allMemberClusters[1].ClusterName)
		})
	})
})

func checkIfPlacedWorkResourcesOnAllMemberClusters() {
	checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(allMemberClusters)
}

func checkIfPlacedWorkResourcesOnMemberClustersInUpdateRun(clusters []*framework.Cluster) {
	for idx := range clusters {
		memberCluster := clusters[idx]
		workResourcesPlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster)
		Eventually(workResourcesPlacedActual, updateRunEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place work resources on member cluster %s", memberCluster.ClusterName)
	}
}

func createStagedUpdateStrategySucceed(strategyName string) *placementv1beta1.ClusterStagedUpdateStrategy {
	strategy := &placementv1beta1.ClusterStagedUpdateStrategy{
		ObjectMeta: metav1.ObjectMeta{
			Name: strategyName,
		},
		Spec: placementv1beta1.StagedUpdateStrategySpec{
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
							WaitTime: metav1.Duration{
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

func validateLatestPolicySnapshot(crpName, wantPolicySnapshotIndex string) {
	Eventually(func() (string, error) {
		var policySnapshotList placementv1beta1.ClusterSchedulingPolicySnapshotList
		if err := hubClient.List(ctx, &policySnapshotList, client.MatchingLabels{
			placementv1beta1.CRPTrackingLabel:      crpName,
			placementv1beta1.IsLatestSnapshotLabel: "true",
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
		return latestPolicySnapshot.Labels[placementv1beta1.PolicyIndexLabel], nil
	}, eventuallyDuration, eventuallyInterval).Should(Equal(wantPolicySnapshotIndex), "Policy snapshot index does not match")
}

func validateLatestResourceSnapshot(crpName, wantResourceSnapshotIndex string) {
	Eventually(func() (string, error) {
		crsList := &placementv1beta1.ClusterResourceSnapshotList{}
		if err := hubClient.List(ctx, crsList, client.MatchingLabels{
			placementv1beta1.CRPTrackingLabel:      crpName,
			placementv1beta1.IsLatestSnapshotLabel: "true",
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
		Spec: placementv1beta1.StagedUpdateRunSpec{
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
