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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

const (
	placementPolicySuspendedBindingNameTemplate        = "pp-suspended-binding-%d"
	clusterPlacementPolicySuspendedBindingNameTemplate = "cpp-suspended-binding-%d"
	placementPolicySuspendedAutoRolloutNameTemplate    = "pp-suspended-auto-rollout-%d"
)

// Users do not normally edit a binding's spec.suspended; upcoming APIs build on it, and these specs set it directly as a
// stand-in for them. On suspension the work generator removes the binding's Works and clears the binding's last
// processed resource snapshot; the work applier then handles the placed resources as whenPlacementDeleted asks. The
// placement policy controller leaves the field alone.
var _ = Describe("placement policy binding suspension", Label("placementpolicy"), func() {
	Context("placement policy with a binding suspended, then resumed", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicySuspendedBindingNameTemplate, GinkgoParallelProcess()),
		}
		suspendedClusterName := memberCluster2EastCanaryName
		otherClusterNames := []string{memberCluster1EastProdName, memberCluster3WestProdName}
		// The member clusters are known only once the suite is set up, after the spec tree is built.
		clusterCount := func() int32 { return int32(len(allMemberClusterNames)) }

		var snapshotName string
		var bindingUIDsByCluster map[string]types.UID
		var workNamesByCluster map[string][]string

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy that selects every member cluster")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the ConfigMap on every member cluster", func() {
			snapshotName = waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			Eventually(appConfigMapPlacedOnClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on every member cluster")

			bindingUIDsByCluster = placementPolicyBindingUIDsByCluster(policyKey)
			var err error
			workNamesByCluster, err = placementPolicyWorkNamesByCluster(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the Works of placement policy %s", policyKey)
		})

		It("can suspend the binding for member cluster 2", func() {
			setPlacementPolicyBindingSuspended(policyKey, suspendedClusterName, true)
		})

		It("should remove the Works and the ConfigMap of the suspended binding only", func() {
			Eventually(suspendedPlacementPolicyBindingActual(policyKey, suspendedClusterName, bindingUIDsByCluster[suspendedClusterName], snapshotName),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to process the suspension of the binding")
			Eventually(placementPolicyWorkClustersActual(policyKey, otherClusterNames...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the Works of the suspended binding")
			Eventually(appliedWorksAbsentOnClustersActual(map[string][]string{suspendedClusterName: workNamesByCluster[suspendedClusterName]}),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the AppliedWorks from member cluster 2")
			Eventually(appConfigMapRemovedFromClustersActual(memberCluster2EastCanary), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the ConfigMap from member cluster 2")
		})

		It("should report the policy as synchronized on the other member clusters only", func() {
			// The suspended binding has no resource snapshot processed, so it counts as neither synchronized nor on an
			// older resource revision.
			wantStatus := someClustersNotSynchronizedPlacementPolicyStatus(clusterCount(), clusterCount()-1)
			wantStatus.LatestResourceRevisionName = ptr.To(snapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("should keep the binding suspended, with no replacement, and leave the other member clusters as they were", func() {
			wantStatus := someClustersNotSynchronizedPlacementPolicyStatus(clusterCount(), clusterCount()-1)
			wantStatus.LatestResourceRevisionName = ptr.To(snapshotName)
			statusActual := placementPolicyStatusActual(policyKey, wantStatus)
			suspendedActual := suspendedPlacementPolicyBindingActual(policyKey, suspendedClusterName, bindingUIDsByCluster[suspendedClusterName], snapshotName)
			// Every binding is kept, and the policy has no other.
			bindingsActual := placementPolicyBindingOwnershipActual(policyKey, bindingUIDsByCluster, true)
			worksActual := placementPolicyWorkClustersActual(policyKey, otherClusterNames...)
			placedActual := appConfigMapPlacedOnClustersActual(memberCluster1EastProd, memberCluster3WestProd)
			removedActual := appConfigMapRemovedFromClustersActual(memberCluster2EastCanary)
			Consistently(func() error {
				for _, actual := range []func() error{statusActual, suspendedActual, bindingsActual, worksActual, placedActual, removedActual} {
					if err := actual(); err != nil {
						return err
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The suspended binding or the other member clusters changed")
		})

		It("can resume the binding for member cluster 2", func() {
			setPlacementPolicyBindingSuspended(policyKey, suspendedClusterName, false)
		})

		It("should place the ConfigMap on member cluster 2 again, through the same binding", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to synchronize every binding")
			Expect(placementPolicyBindingsKeptActual(policyKey, bindingUIDsByCluster)()).To(Succeed(), "A binding was replaced")
			Eventually(placementPolicyWorkClustersActual(policyKey, allMemberClusterNames...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to re-create the Works of the resumed binding")
			Eventually(appConfigMapPlacedOnClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on every member cluster")

			wantStatus := allSynchronizedPlacementPolicyStatus(clusterCount())
			wantStatus.LatestResourceRevisionName = ptr.To(snapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("cluster placement policy orphaning its resources, with a binding suspended, then resumed", Ordered, func() {
		policyKey := types.NamespacedName{
			Name: fmt.Sprintf(clusterPlacementPolicySuspendedBindingNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		suspendedClusterName := memberCluster1EastProdName
		otherClusterNames := []string{memberCluster2EastCanaryName, memberCluster3WestProdName}
		// The member clusters are known only once the suite is set up, after the spec tree is built.
		clusterCount := func() int32 { return int32(len(allMemberClusterNames)) }

		var snapshotName string
		var bindingUIDsByCluster map[string]types.UID
		var workNamesByCluster map[string][]string

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that selects every member cluster and orphans its resources when they are no longer placed")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
				SyncStrategy: &kfplacementv1alpha1.SyncStrategy{
					WhenPlacementDeleted: kfplacementv1alpha1.WhenPlacementDeletedOptionOrphanResources,
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The orphaned ConfigMaps go with the member namespaces, which the cleanup removes even if deleting the
			// policy fails.
			defer cleanupWorkResourcesOnHubAndMembers()
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should place the ConfigMap on every member cluster, managed by KubeFleet", func() {
			snapshotName = waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			for _, cluster := range allMemberClusters {
				Eventually(configMapTakenOverOnClusterActual(cluster, cmKey), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to place the ConfigMap on cluster %s", cluster.ClusterName)
			}

			bindingUIDsByCluster = placementPolicyBindingUIDsByCluster(policyKey)
			var err error
			workNamesByCluster, err = placementPolicyWorkNamesByCluster(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the Works of cluster placement policy %s", policyKey)
		})

		It("can suspend the binding for member cluster 1", func() {
			setPlacementPolicyBindingSuspended(policyKey, suspendedClusterName, true)
		})

		It("should remove the Works of the suspended binding, leaving the ConfigMap on member cluster 1, orphaned", func() {
			Eventually(suspendedPlacementPolicyBindingActual(policyKey, suspendedClusterName, bindingUIDsByCluster[suspendedClusterName], snapshotName),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to process the suspension of the binding")
			Eventually(placementPolicyWorkClustersActual(policyKey, otherClusterNames...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the Works of the suspended binding")
			// The resumed binding's Work, and with it its AppliedWork, gets the same name; wait for the old one to go
			// before resuming.
			Eventually(appliedWorksAbsentOnClustersActual(map[string][]string{suspendedClusterName: workNamesByCluster[suspendedClusterName]}),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the AppliedWorks from member cluster 1")

			orphanedActual := configMapOrphanedOnClustersActual(cmKey, appConfigMap().Data, memberCluster1EastProd)
			Eventually(orphanedActual, workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to orphan the ConfigMap on member cluster 1")
			Consistently(orphanedActual, consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The orphaned ConfigMap was removed, or KubeFleet took it back")
		})

		It("should report the policy as synchronized on the other member clusters only", func() {
			wantStatus := someClustersNotSynchronizedPlacementPolicyStatus(clusterCount(), clusterCount()-1)
			wantStatus.LatestResourceRevisionName = ptr.To(snapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})

		It("can resume the binding for member cluster 1", func() {
			setPlacementPolicyBindingSuspended(policyKey, suspendedClusterName, false)
		})

		It("should report the orphaned ConfigMap as not taken over", func() {
			// The default whenAlreadyExists=ReportError leaves the ConfigMap alone, now that KubeFleet no longer owns it.
			wantBindings := map[string]wantPlacementBinding{
				suspendedClusterName: {
					selectors:    []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}},
					snapshotName: snapshotName,
					status:       notTakenOverPlacementBindingStatus(cmKey),
				},
			}
			for _, clusterName := range otherClusterNames {
				wantBindings[clusterName] = wantPlacementBinding{
					selectors:    []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}},
					snapshotName: snapshotName,
					status:       synchronizedPlacementBindingStatus(1),
				}
			}
			Eventually(placementPolicyBindingsActual(policyKey, wantBindings), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the ConfigMap as not taken over")
			Expect(placementPolicyBindingsKeptActual(policyKey, bindingUIDsByCluster)()).To(Succeed(), "A binding was replaced")

			wantStatus := someClustersNotSynchronizedPlacementPolicyStatus(clusterCount(), clusterCount()-1)
			wantStatus.LatestResourceRevisionName = ptr.To(snapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
			Consistently(configMapOrphanedOnClustersActual(cmKey, appConfigMap().Data, memberCluster1EastProd), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The orphaned ConfigMap was taken over")
		})

		It("can switch the policy to take over existing resources that have no differences", func() {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().SyncStrategy = &kfplacementv1alpha1.SyncStrategy{
					WhenAlreadyExists:    kfplacementv1alpha1.WhenAlreadyExistsOptionTakeOverIfNoDiff,
					WhenPlacementDeleted: kfplacementv1alpha1.WhenPlacementDeletedOptionOrphanResources,
				}
			})
		})

		It("should take the ConfigMap on member cluster 1 over again, without a rollout", func() {
			// The orphaned ConfigMap still matches its hub copy.
			wantBindings := make(map[string]wantPlacementBinding, len(allMemberClusterNames))
			for _, clusterName := range allMemberClusterNames {
				wantBindings[clusterName] = wantPlacementBinding{
					selectors:    []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}},
					snapshotName: snapshotName,
					status:       synchronizedPlacementBindingStatus(1),
				}
			}
			Eventually(placementPolicyBindingsActual(policyKey, wantBindings), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to synchronize every binding")
			Eventually(configMapTakenOverOnClusterActual(memberCluster1EastProd, cmKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to take the ConfigMap over on member cluster 1")

			wantStatus := allSynchronizedPlacementPolicyStatus(clusterCount())
			wantStatus.LatestResourceRevisionName = ptr.To(snapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})
	})

	Context("placement policy auto-rolling out a change while a binding is suspended", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicySuspendedAutoRolloutNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}
		suspendedClusterName := memberCluster2EastCanaryName
		otherClusterNames := []string{memberCluster1EastProdName, memberCluster3WestProdName}
		otherClusters := func() []*framework.Cluster {
			return []*framework.Cluster{memberCluster1EastProd, memberCluster3WestProd}
		}
		// The member clusters are known only once the suite is set up, after the spec tree is built.
		clusterCount := func() int32 { return int32(len(allMemberClusterNames)) }

		var initialSnapshotName, newSnapshotName string
		var bindingUIDsByCluster map[string]types.UID

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy that selects every member cluster, with auto-rollout enabled")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			policy.SetAnnotations(autoRolloutAnnotations())
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the initial version on every member cluster", func() {
			initialSnapshotName = waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			Eventually(configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the initial version on every member cluster")
			bindingUIDsByCluster = placementPolicyBindingUIDsByCluster(policyKey)
		})

		It("can suspend the binding for member cluster 2", func() {
			setPlacementPolicyBindingSuspended(policyKey, suspendedClusterName, true)
		})

		It("should remove the Works and the ConfigMap of the suspended binding", func() {
			Eventually(suspendedPlacementPolicyBindingActual(policyKey, suspendedClusterName, bindingUIDsByCluster[suspendedClusterName], initialSnapshotName),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to process the suspension of the binding")
			Eventually(placementPolicyWorkClustersActual(policyKey, otherClusterNames...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the Works of the suspended binding")
			Eventually(appConfigMapRemovedFromClustersActual(memberCluster2EastCanary), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the ConfigMap from member cluster 2")
		})

		It("can change the ConfigMap on the hub", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
		})

		It("should roll the change out to every binding, but apply it only on the clusters whose binding is not suspended", func() {
			// Auto-rollout moves every binding of the policy to the new resource snapshot, the suspended one included;
			// the work generator does not act on the suspended one, though.
			Eventually(placementPolicyLatestSnapshotChangedActual(policyKey, initialSnapshotName, &newSnapshotName), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to take a new resource snapshot")
			suspendedActual := suspendedPlacementPolicyBindingActual(policyKey, suspendedClusterName, bindingUIDsByCluster[suspendedClusterName], newSnapshotName)
			Eventually(suspendedActual, placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to move the suspended binding to the new resource snapshot")
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, otherClusters()...), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the new version on the other member clusters")

			// The suspended binding names the latest revision but has processed none, so it counts as neither
			// synchronized nor on an older revision.
			wantStatus := someClustersNotSynchronizedPlacementPolicyStatus(clusterCount(), clusterCount()-1)
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			statusActual := placementPolicyStatusActual(policyKey, wantStatus)
			Eventually(statusActual, workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")

			worksActual := placementPolicyWorkClustersActual(policyKey, otherClusterNames...)
			removedActual := appConfigMapRemovedFromClustersActual(memberCluster2EastCanary)
			Consistently(func() error {
				for _, actual := range []func() error{suspendedActual, worksActual, removedActual, statusActual} {
					if err := actual(); err != nil {
						return err
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The change reached member cluster 2, or the suspended binding changed")
		})

		It("can resume the binding for member cluster 2", func() {
			setPlacementPolicyBindingSuspended(policyKey, suspendedClusterName, false)
		})

		It("should place the latest version on member cluster 2", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to synchronize every binding on the latest resource snapshot")
			Expect(placementPolicyBindingsKeptActual(policyKey, bindingUIDsByCluster)()).To(Succeed(), "A binding was replaced")
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the latest version on every member cluster")

			wantStatus := allSynchronizedPlacementPolicyStatus(clusterCount())
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})
})
