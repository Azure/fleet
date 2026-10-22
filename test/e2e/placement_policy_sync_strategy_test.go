/*
Copyright 2026 The KubeFleet Authors.

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
	"maps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

const (
	clusterPlacementPolicyTakeOverNameTemplate = "cpp-take-over-%d"
	placementPolicyDriftNameTemplate           = "pp-drift-%d"
	placementPolicyOrphanNameTemplate          = "pp-orphan-%d"
	placementPolicyDeploymentNameTemplate      = "pp-deployment-%d"
)

// The FEP-0001 placement experience is alpha and runs behind the enable-placement-policy-apis hub and member
// agent flags, which test/e2e/setup.sh turns on.
var _ = Describe("placement policy sync strategy", Label("placementpolicy"), func() {
	Context("cluster placement policy placing a ConfigMap that already exists, unmanaged, on a member cluster", Ordered, func() {
		policyKey := types.NamespacedName{
			Name: fmt.Sprintf(clusterPlacementPolicyTakeOverNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		preExistingData := map[string]string{"data": "pre-existing"}
		otherClusters := []string{memberCluster2EastCanaryName, memberCluster3WestProdName}

		var initialSnapshotName string

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating the same ConfigMap, with different data, directly on member cluster 1")
			preExisting := appConfigMap()
			preExisting.Data = maps.Clone(preExistingData)
			createConfigMapOnMemberCluster(memberCluster1EastProd, &preExisting)

			By("creating a cluster placement policy with the default sync strategy")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the ConfigMap on the other member clusters", func() {
			Eventually(appConfigMapPlacedOnClustersActual(memberCluster2EastCanary, memberCluster3WestProd), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on the other member clusters")
		})

		It("should report the ConfigMap as not taken over on member cluster 1", func() {
			// The default whenAlreadyExists=ReportError leaves an unmanaged object alone.
			wantBindings := map[string]wantPlacementBinding{
				memberCluster1EastProdName: {
					selectors: []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}},
					status:    notTakenOverPlacementBindingStatus(cmKey),
				},
			}
			for _, clusterName := range otherClusters {
				wantBindings[clusterName] = wantPlacementBinding{
					selectors: []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}},
					status:    synchronizedPlacementBindingStatus(1),
				}
			}
			Eventually(placementPolicyBindingsActual(policyKey, wantBindings), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the ConfigMap as not taken over")

			policy, err := retrievePlacementPolicy(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get cluster placement policy %s", policyKey)
			snapshot, err := latestPrimarySnapshotOfPlacementPolicy(policy)
			Expect(err).NotTo(HaveOccurred(), "Failed to find the resource snapshot of cluster placement policy %s", policyKey)
			initialSnapshotName = snapshot.GetName()
		})

		It("should report the policy as not synchronized on member cluster 1", func() {
			clusterCount := int32(len(allMemberClusters))
			wantStatus := someClustersNotSynchronizedPlacementPolicyStatus(clusterCount, clusterCount-1)
			wantStatus.LatestResourceRevisionName = ptr.To(initialSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})

		It("should leave the pre-existing ConfigMap on member cluster 1 as it is", func() {
			Consistently(configMapDataOnClustersActual(cmKey, preExistingData, memberCluster1EastProd), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The pre-existing ConfigMap was overwritten")
		})

		It("can switch the policy to take over existing resources", func() {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().SyncStrategy = &kfplacementv1alpha1.SyncStrategy{
					WhenAlreadyExists: kfplacementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
				}
			})
		})

		It("should carry the new sync strategy to every binding without a rollout", func() {
			// The bindings must stay on the same resource snapshot; the actual also checks that each one carries the
			// policy's (API server defaulted) sync strategy.
			wantBindings := make(map[string]wantPlacementBinding, len(allMemberClusterNames))
			for _, clusterName := range allMemberClusterNames {
				wantBindings[clusterName] = wantPlacementBinding{
					selectors:    []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}},
					snapshotName: initialSnapshotName,
					status:       synchronizedPlacementBindingStatus(1),
				}
			}
			Eventually(placementPolicyBindingsActual(policyKey, wantBindings), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to carry the new sync strategy to every binding")
		})

		It("should take over the ConfigMap on member cluster 1", func() {
			Eventually(configMapTakenOverOnClusterActual(memberCluster1EastProd, cmKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to take over the ConfigMap on member cluster 1")
		})

		It("should report the policy as synchronized on every member cluster, on the same resource revision", func() {
			wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(initialSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})
	})

	Context("placement policy overwriting drifts by default, then reporting them", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyDriftNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		hubData := appConfigMap().Data
		driftedData := map[string]string{"data": "drifted"}
		// The policy places the ConfigMap on cluster-1 alone, where the specs edit it.
		aliasLabels := map[string]string{placementv1beta1.ClusterAliasLabel: memberCluster1EastProdName}
		wantSelectorsByCluster := func() map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
			return map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				memberCluster1EastProdName: {clusterSelectorTermsMatchingLabels(aliasLabels)},
			}
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy with the default sync strategy that selects cluster-1")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorMatchingLabels(aliasLabels, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the ConfigMap on member cluster 1", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind member cluster 1")
			Eventually(configMapDataOnClustersActual(cmKey, hubData, memberCluster1EastProd), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on member cluster 1")
		})

		It("can change the ConfigMap on member cluster 1", func() {
			updateConfigMapDataOnMemberCluster(memberCluster1EastProd, cmKey, driftedData)
		})

		It("should overwrite the drift, as the default whenDrifted=ApplyAnyway asks", func() {
			// The work applier notices member-side changes on its periodic requeue, which the E2E setup caps at 5s.
			Eventually(configMapDataOnClustersActual(cmKey, hubData, memberCluster1EastProd), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to overwrite the drift")
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to keep the binding synchronized")
		})

		It("can switch the policy to report drifts", func() {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().SyncStrategy = &kfplacementv1alpha1.SyncStrategy{
					WhenDrifted: kfplacementv1alpha1.WhenDriftedOptionReportError,
				}
			})
		})

		It("should carry the new sync strategy to the binding without a rollout", func() {
			// The actual also checks that the binding carries the policy's (API server defaulted) sync strategy.
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to carry the new sync strategy to the binding")
		})

		It("can change the ConfigMap on member cluster 1 again", func() {
			updateConfigMapDataOnMemberCluster(memberCluster1EastProd, cmKey, driftedData)
		})

		It("should report the drift on the binding and the policy", func() {
			// A ConfigMap carries no generation, so the generation observed on the member cluster is 0.
			wantDiffDetails := &kfplacementv1alpha1.DiffDetails{
				ObservedInMemberClusterGeneration: ptr.To(int64(0)),
				ObservedDiffs: []kfplacementv1alpha1.PatchDetail{
					{Path: "/data/data", ValueInMember: driftedData["data"], ValueInHub: hubData["data"]},
				},
			}
			wantBindings := map[string]wantPlacementBinding{
				memberCluster1EastProdName: {
					selectors: wantSelectorsByCluster()[memberCluster1EastProdName],
					status:    configMapNotAppliedPlacementBindingStatus(cmKey, "FoundDrifts", wantDiffDetails),
				},
			}
			Eventually(placementPolicyBindingsActual(policyKey, wantBindings), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the drift on the binding")
			Eventually(placementPolicyStatusActual(policyKey, someClustersNotSynchronizedPlacementPolicyStatus(1, 0)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the drift on the policy")
		})

		It("should leave the drift on member cluster 1 as it is", func() {
			Consistently(configMapDataOnClustersActual(cmKey, driftedData, memberCluster1EastProd), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The drift was overwritten")
		})

		It("can revert the drift on member cluster 1", func() {
			updateConfigMapDataOnMemberCluster(memberCluster1EastProd, cmKey, hubData)
		})

		It("should report the policy as synchronized again", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the binding as synchronized again")
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(1)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the policy as synchronized again")
		})
	})

	Context("placement policy orphaning its resources when deleted", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyOrphanNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy that orphans its resources when deleted")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
				SyncStrategy: &kfplacementv1alpha1.SyncStrategy{
					WhenPlacementDeleted: kfplacementv1alpha1.WhenPlacementDeletedOptionOrphanResources,
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The orphaned ConfigMaps go with the member namespaces, which the cleanup removes even if deleting the
			// policy fails.
			defer cleanupWorkResourcesOnHubAndMembers()
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should place the ConfigMap on every member cluster, managed by KubeFleet", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind every member cluster")
			for _, cluster := range allMemberClusters {
				Eventually(configMapTakenOverOnClusterActual(cluster, cmKey), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to place the ConfigMap on cluster %s", cluster.ClusterName)
			}
		})

		It("can delete the placement policy", func() {
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should remove the bindings, Works, and resource snapshots of the policy from the hub", func() {
			Eventually(placementPolicyLeftoversRemovedActual(policyKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the bindings and resource snapshots of the policy")
			Eventually(noPlacementPolicyWorksActual(policyKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the Works of the policy")
		})

		It("should leave the ConfigMap on every member cluster, no longer owned by KubeFleet", func() {
			orphanedActual := configMapOrphanedOnClustersActual(cmKey, appConfigMap().Data, allMemberClusters...)
			Eventually(orphanedActual, workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to orphan the ConfigMap on the member clusters")
			Consistently(orphanedActual, consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The orphaned ConfigMap was removed, or KubeFleet took it back")
		})
	})

	Context("placement policy placing a Deployment", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyDeploymentNameTemplate, GinkgoParallelProcess()),
		}
		deployKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appDeployment().Name}

		BeforeAll(func() {
			By("creating the work resources and a Deployment")
			createWorkResources()
			deploy := appDeployment()
			Expect(hubClient.Create(ctx, &deploy)).To(Succeed(), "Failed to create Deployment %s", deployKey)

			By("creating a placement policy that selects the ConfigMap and the Deployment")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: append(placementPolicyResourceSelectors(), kfplacementv1alpha1.ResourceSelector{
					APIGroup:   appsv1.GroupName,
					APIVersion: "v1",
					Kind:       "Deployment",
					Name:       deployKey.Name,
				}),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources, the Deployment among them, must go even if removing the policy fails; otherwise they
			// leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should roll the Deployment out on every member cluster", func() {
			// The Deployment becomes available only once its image is pulled and its pod is ready.
			Eventually(deploymentAvailableOnClustersActual(deployKey, allMemberClusters...), longEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the Deployment out on every member cluster")
		})

		It("should report both resources as synchronized and available on every binding", func() {
			// The work applier tracks the availability of a Deployment by its rollout; a ConfigMap is available once
			// applied.
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 2), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the resources as available on every binding")
		})

		It("should report the policy as available on every member cluster", func() {
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})
})
