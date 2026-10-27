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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
)

const (
	placementPolicyCleanedUpNameTemplate            = "pp-cleaned-up-%d"
	placementPolicyRecreatedNameTemplate            = "pp-recreated-%d"
	placementPolicyRecreatedNarrowerNameTemplate    = "pp-recreated-narrower-%d"
	clusterPlacementPolicyQuickRecreateNameTemplate = "cpp-quick-recreate-%d"
	placementPolicyNamespaceDeletedNameTemplate     = "pp-namespace-deleted-%d"
)

// The FEP-0001 placement experience is alpha and runs behind the enable-placement-policy-apis hub and member
// agent flags, which test/e2e/setup.sh turns on.
var _ = Describe("placement policy deletion", Label("placementpolicy"), func() {
	Context("placement policy deleted with the default sync strategy", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyCleanedUpNameTemplate, GinkgoParallelProcess())}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}

		// The Works of the policy, keyed by the member cluster they are for; an AppliedWork carries the name of its Work.
		var workNamesByCluster map[string][]string

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy with the default sync strategy")
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

		It("should place the ConfigMap on every member cluster, managed by KubeFleet", func() {
			waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			for _, cluster := range allMemberClusters {
				Eventually(configMapTakenOverOnClusterActual(cluster, cmKey), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to place the ConfigMap on cluster %s", cluster.ClusterName)
			}

			var err error
			workNamesByCluster, err = placementPolicyWorkNamesByCluster(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the Works of placement policy %s", policyKey)
			for _, cluster := range allMemberClusters {
				Expect(workNamesByCluster[cluster.ClusterName]).NotTo(BeEmpty(), "Placement policy %s has no Work for cluster %s", policyKey, cluster.ClusterName)
				for _, name := range workNamesByCluster[cluster.ClusterName] {
					Expect(cluster.KubeClient.Get(ctx, types.NamespacedName{Name: name}, &kfplacementv1alpha1.AppliedWork{})).To(Succeed(),
						"Failed to find AppliedWork %s on cluster %s", name, cluster.ClusterName)
				}
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

		It("should remove the AppliedWorks and the ConfigMap from every member cluster", func() {
			Eventually(appliedWorksAbsentOnClustersActual(workNamesByCluster), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the AppliedWorks from the member clusters")
			Eventually(appConfigMapRemovedFromClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the ConfigMap from the member clusters")
		})

		It("should leave behind the namespace that the work applier created on every member cluster", func() {
			// The applier creates the namespace of a namespaced resource it applies, but does not manage it, so it
			// outlives the policy.
			Consistently(namespaceLeftUnmanagedOnClustersActual(nsName, allMemberClusters...), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The namespace that the work applier created was removed, or is managed by KubeFleet")
		})
	})

	Context("placement policy deleted while orphaning its dependents, then re-created under the same name", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyRecreatedNameTemplate, GinkgoParallelProcess())}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		// The policy selects every member cluster.
		newPolicy := func() kfplacementv1alpha1.PlacementPolicyAccessor {
			return newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
		}

		var bindingUIDsByCluster map[string]types.UID
		var workUIDsByWork map[string]types.UID
		var workNamesByCluster map[string][]string
		var cmResourceVersionsByCluster map[string]string
		// The resource snapshot of the original policy, and the one the re-created policy takes in its place.
		var oldSnapshotName, newSnapshotName string

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy")
			Expect(hubClient.Create(ctx, newPolicy())).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			// Deleting the hub namespace also removes the bindings and the resource snapshot, should the spec fail with
			// them still orphaned.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the ConfigMap on every member cluster", func() {
			waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			Eventually(appConfigMapPlacedOnClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on every member cluster")

			bindingUIDsByCluster = placementPolicyBindingUIDsByCluster(policyKey)
			oldSnapshotName = latestPrimarySnapshotNameOfPlacementPolicy(policyKey)
			var err error
			workUIDsByWork, err = placementPolicyWorkUIDs(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the Works of placement policy %s", policyKey)
			Expect(workUIDsByWork).NotTo(BeEmpty(), "Placement policy %s has no Works", policyKey)
			workNamesByCluster, err = placementPolicyWorkNamesByCluster(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the Works of placement policy %s", policyKey)
			cmResourceVersionsByCluster, err = configMapResourceVersionsByCluster(cmKey, allMemberClusters...)
			Expect(err).NotTo(HaveOccurred(), "Failed to read the ConfigMap on the member clusters")
		})

		It("can delete the placement policy, orphaning its dependents", func() {
			deletePlacementPolicyAndWait(policyKey, client.PropagationPolicy(metav1.DeletePropagationOrphan))
		})

		It("should leave the bindings and the resource snapshot behind, orphaned, with the Works and the ConfigMaps as they were", func() {
			// The garbage collector drops the dependents' owner references before it lets the policy go. Nothing collects
			// the orphaned resource snapshot until a policy of the same name comes back.
			orphanedActual := placementPolicyBindingOwnershipActual(policyKey, bindingUIDsByCluster, false)
			Eventually(orphanedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to orphan the bindings")
			snapshotsActual := placementPolicyPrimarySnapshotNamesActual(policyKey, oldSnapshotName)
			snapshotsOrphanedActual := placementPolicySnapshotsOrphanedActual(policyKey)
			Eventually(snapshotsOrphanedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to orphan the resource snapshot")

			worksActual := placementPolicyWorksKeptActual(policyKey, workUIDsByWork)
			cmActual := configMapUnchangedOnClustersActual(cmKey, cmResourceVersionsByCluster)
			Consistently(func() error {
				for _, actual := range []func() error{orphanedActual, snapshotsActual, snapshotsOrphanedActual, worksActual, cmActual} {
					if err := actual(); err != nil {
						return err
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The orphaned bindings, the resource snapshot, the Works, or the ConfigMaps changed")
		})

		It("can re-create the placement policy under the same name", func() {
			Expect(hubClient.Create(ctx, newPolicy())).To(Succeed(), "Failed to re-create placement policy %s", policyKey)
		})

		It("should collect the orphaned resource snapshot, and take a new one under a different name", func() {
			// The re-created policy never reuses a snapshot it does not own: it has the orphaned one collected, even though
			// the orphaned bindings still name it, and then starts over at index 0. Snapshot names hash the policy UID.
			Eventually(func() error {
				if err := placementPolicyLatestSnapshotChangedActual(policyKey, oldSnapshotName, &newSnapshotName)(); err != nil {
					return err
				}
				return placementPolicyPrimarySnapshotNamesActual(policyKey, newSnapshotName)()
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to replace the orphaned resource snapshot")

			policy, err := retrievePlacementPolicy(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get placement policy %s", policyKey)
			snapshot, err := latestPrimarySnapshotOfPlacementPolicy(policy)
			Expect(err).NotTo(HaveOccurred(), "Failed to find the resource snapshot of placement policy %s", policyKey)
			Expect(snapshot.GetLabels()[kfplacementv1alpha1.PlacementResourceSnapshotIndexLabelKey]).To(Equal("0"),
				"The re-created placement policy did not start its resource snapshots over at index 0")
		})

		It("should adopt the orphaned bindings, moving them to the new resource snapshot, and keep the Works and the ConfigMaps as they were", func() {
			adoptedActual := placementPolicyBindingOwnershipActual(policyKey, bindingUIDsByCluster, true)
			Eventually(adoptedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to adopt the orphaned bindings")
			// The bindings point to, and have processed, the policy's latest resource snapshot, i.e., the new one.
			synchronizedActual := allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			Eventually(synchronizedActual, workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to synchronize the adopted bindings on the new resource snapshot")
			wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			statusActual := placementPolicyStatusActual(policyKey, wantStatus)
			Eventually(statusActual, workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")

			// The Works keep their names, which come from the policy's namespaced name, and re-linking them to a resource
			// snapshot of the same contents changes only their metadata, so the member clusters have nothing to apply. The
			// resource versions recorded before the deletion show that the ConfigMaps never changed.
			worksActual := placementPolicyWorksKeptActual(policyKey, workUIDsByWork)
			cmActual := configMapUnchangedOnClustersActual(cmKey, cmResourceVersionsByCluster)
			Consistently(func() error {
				for _, actual := range []func() error{adoptedActual, synchronizedActual, statusActual, worksActual, cmActual} {
					if err := actual(); err != nil {
						return err
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The adopted bindings, the policy, the Works, or the ConfigMaps changed")
		})

		It("can delete the re-created placement policy", func() {
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should remove the bindings, Works, and resource snapshots of the policy from the hub", func() {
			// No resource snapshot of either policy is left.
			Eventually(placementPolicyLeftoversRemovedActual(policyKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the bindings and resource snapshots of the policy")
			Eventually(noPlacementPolicyWorksActual(policyKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the Works of the policy")
		})

		It("should remove the AppliedWorks and the ConfigMap from every member cluster", func() {
			Eventually(appliedWorksAbsentOnClustersActual(workNamesByCluster), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the AppliedWorks from the member clusters")
			Eventually(appConfigMapRemovedFromClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the ConfigMap from the member clusters")
		})
	})

	Context("placement policy deleted while orphaning its dependents, then re-created for fewer clusters", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyRecreatedNarrowerNameTemplate, GinkgoParallelProcess())}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		eastTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{regionLabelName: regionEast}},
		}
		eastClusterNames := []string{memberCluster1EastProdName, memberCluster2EastCanaryName}

		var bindingUIDsByCluster map[string]types.UID
		var workUIDsByWork map[string]types.UID
		var workNamesByCluster map[string][]string
		var cmResourceVersionsByCluster map[string]string
		// The resource version of cluster-3's binding once it has been orphaned.
		var orphanResourceVersion string
		var oldSnapshotName, newSnapshotName string

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
			// Deleting the hub namespace also removes cluster-3's binding, should the spec fail with it still orphaned.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the ConfigMap on every member cluster", func() {
			waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			Eventually(appConfigMapPlacedOnClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on every member cluster")

			bindingUIDsByCluster = placementPolicyBindingUIDsByCluster(policyKey)
			oldSnapshotName = latestPrimarySnapshotNameOfPlacementPolicy(policyKey)
			var err error
			workUIDsByWork, err = placementPolicyWorkUIDs(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the Works of placement policy %s", policyKey)
			workNamesByCluster, err = placementPolicyWorkNamesByCluster(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the Works of placement policy %s", policyKey)
			Expect(workNamesByCluster[memberCluster3WestProdName]).NotTo(BeEmpty(), "Placement policy %s has no Work for cluster-3", policyKey)
			cmResourceVersionsByCluster, err = configMapResourceVersionsByCluster(cmKey, allMemberClusters...)
			Expect(err).NotTo(HaveOccurred(), "Failed to read the ConfigMap on the member clusters")
		})

		It("can delete the placement policy, orphaning its dependents", func() {
			deletePlacementPolicyAndWait(policyKey, client.PropagationPolicy(metav1.DeletePropagationOrphan))
			Eventually(placementPolicyBindingOwnershipActual(policyKey, bindingUIDsByCluster, false), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to orphan the bindings")

			bindings, err := placementPolicyBindingsByCluster(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the bindings of placement policy %s", policyKey)
			orphanResourceVersion = bindings[memberCluster3WestProdName].GetResourceVersion()
		})

		It("can re-create the placement policy under the same name, selecting the east clusters only", func() {
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorWithTerms(eastTerms, intstr.FromString(clusterSelectorCountAll)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to re-create placement policy %s", policyKey)
		})

		It("should replace the orphaned resource snapshot, and adopt the bindings of the east clusters only", func() {
			Eventually(func() error {
				if err := placementPolicyLatestSnapshotChangedActual(policyKey, oldSnapshotName, &newSnapshotName)(); err != nil {
					return err
				}
				return placementPolicyPrimarySnapshotNamesActual(policyKey, newSnapshotName)()
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to replace the orphaned resource snapshot")

			Eventually(placementPolicyBindingControllersActual(policyKey, bindingUIDsByCluster, eastClusterNames...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to adopt the bindings of the east clusters alone")
			// The adopted bindings carry the new cluster selectors and the new resource snapshot.
			Eventually(allSynchronizedAdoptedPlacementPolicyBindingsActual(policyKey, sameSelectorsForClusters(eastTerms, eastClusterNames...), 1),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to synchronize the adopted bindings on the new resource snapshot")
			// cluster-3's orphan does not count toward the policy status.
			wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(eastClusterNames)))
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("should leave cluster-3's orphaned binding alone, with its Works and ConfigMap as they were", func() {
			// The orphan still names the collected resource snapshot, but it is in sync, and the work generator reads
			// snapshots only to (re)process a binding, so nothing happens to it.
			orphanActual := orphanedPlacementBindingUnchangedActual(policyKey, memberCluster3WestProdName, bindingUIDsByCluster[memberCluster3WestProdName], orphanResourceVersion)
			adoptedActual := placementPolicyBindingControllersActual(policyKey, bindingUIDsByCluster, eastClusterNames...)
			worksActual := placementPolicyWorksKeptActual(policyKey, workUIDsByWork)
			cmActual := configMapUnchangedOnClustersActual(cmKey, cmResourceVersionsByCluster)
			Consistently(func() error {
				for _, actual := range []func() error{orphanActual, adoptedActual, worksActual, cmActual} {
					if err := actual(); err != nil {
						return err
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Cluster-3's orphaned binding, the Works, or the ConfigMaps changed")
		})

		It("can delete cluster-3's orphaned binding by hand", func() {
			bindings, err := placementPolicyBindingsByCluster(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the bindings of placement policy %s", policyKey)
			orphan := bindings[memberCluster3WestProdName]
			Expect(orphan).NotTo(BeNil(), "Placement policy %s has no binding for cluster-3", policyKey)
			uid := orphan.GetUID()
			Expect(hubClient.Delete(ctx, orphan, client.Preconditions{UID: &uid})).To(Succeed(), "Failed to delete binding %s", orphan.GetName())
		})

		It("should remove the orphan's Works and cluster-3's ConfigMap, without needing the collected resource snapshot", func() {
			eastUIDs := map[string]types.UID{
				memberCluster1EastProdName:   bindingUIDsByCluster[memberCluster1EastProdName],
				memberCluster2EastCanaryName: bindingUIDsByCluster[memberCluster2EastCanaryName],
			}
			Eventually(placementPolicyBindingOwnershipActual(policyKey, eastUIDs, true), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove cluster-3's binding")
			Eventually(placementPolicyWorkClustersActual(policyKey, eastClusterNames...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove cluster-3's Works")
			Eventually(appliedWorksAbsentOnClustersActual(map[string][]string{memberCluster3WestProdName: workNamesByCluster[memberCluster3WestProdName]}),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the AppliedWorks from cluster-3")
			Eventually(appConfigMapRemovedFromClustersActual(memberCluster3WestProd), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the ConfigMap from cluster-3")

			// The policy never controlled the orphan, so it is left as it was.
			wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(eastClusterNames)))
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			statusActual := placementPolicyStatusActual(policyKey, wantStatus)
			cmActual := configMapUnchangedOnClustersActual(cmKey, map[string]string{
				memberCluster1EastProdName:   cmResourceVersionsByCluster[memberCluster1EastProdName],
				memberCluster2EastCanaryName: cmResourceVersionsByCluster[memberCluster2EastCanaryName],
			})
			Consistently(func() error {
				if err := statusActual(); err != nil {
					return err
				}
				return cmActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The placement policy or its ConfigMaps on the east clusters changed")
		})
	})

	Context("cluster placement policy deleted and re-created under the same name right away", Ordered, func() {
		policyKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyQuickRecreateNameTemplate, GinkgoParallelProcess())}
		newPolicy := func() kfplacementv1alpha1.PlacementPolicyAccessor {
			return newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
		}

		var oldBindingUIDsByCluster map[string]types.UID
		var oldSnapshotName, newSnapshotName string

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that selects every member cluster")
			Expect(hubClient.Create(ctx, newPolicy())).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the ConfigMap on every member cluster", func() {
			waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			oldBindingUIDsByCluster = placementPolicyBindingUIDsByCluster(policyKey)
			oldSnapshotName = latestPrimarySnapshotNameOfPlacementPolicy(policyKey)
		})

		It("can delete the cluster placement policy and re-create it right away", func() {
			// The deletion waits only for the policy itself to go; the garbage collector removes its bindings and
			// resource snapshots afterwards.
			deletePlacementPolicyAndWait(policyKey)
			Expect(hubClient.Create(ctx, newPolicy())).To(Succeed(), "Failed to re-create cluster placement policy %s", policyKey)

			// Whether any dependent of the deleted policy is still around depends on timing; record it, as it tells
			// whether the run went through the path that waits for them.
			bindings, bindingsErr := placementPolicyBindingsByCluster(policyKey)
			snapshots, snapshotsErr := placementPolicySnapshots(policyKey)
			GinkgoWriter.Printf("At re-creation: %d bindings (err: %v) and %d resource snapshots (err: %v) named the policy\n",
				len(bindings), bindingsErr, len(snapshots), snapshotsErr)
		})

		It("should converge on new bindings and a new resource snapshot, leaving nothing of the deleted policy behind", func() {
			// The re-created policy never reuses the deleted policy's resource snapshots: it waits for the garbage
			// collector, then starts over under a new name.
			Eventually(func() error {
				if err := placementPolicyLatestSnapshotChangedActual(policyKey, oldSnapshotName, &newSnapshotName)(); err != nil {
					return err
				}
				return placementPolicyPrimarySnapshotNamesActual(policyKey, newSnapshotName)()
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to take a new resource snapshot")

			// Only bindings that the re-created policy controls are left.
			bindingsActual := allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			Eventually(bindingsActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to bind every member cluster anew")
			newBindingUIDsByCluster := placementPolicyBindingUIDsByCluster(policyKey)
			for clusterName, uid := range newBindingUIDsByCluster {
				Expect(uid).NotTo(Equal(oldBindingUIDsByCluster[clusterName]), "The binding for cluster %s was kept from the deleted policy", clusterName)
			}

			// The Works share their names with those of the deleted policy; none of the old ones is left.
			worksActual := placementPolicyWorksLinkedToSnapshotActual(policyKey, newSnapshotName)
			Eventually(worksActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to replace the Works of the deleted policy")
			Eventually(placementPolicyWorkClustersActual(policyKey, allMemberClusterNames...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to generate Works for every member cluster")

			// The ConfigMap may have been removed and created anew in between, so its UID is not pinned.
			cmActual := appConfigMapPlacedOnClustersActual(allMemberClusters...)
			Eventually(cmActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place the ConfigMap on every member cluster")
			wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			statusActual := placementPolicyStatusActual(policyKey, wantStatus)
			Eventually(statusActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the expected cluster placement policy status")

			Consistently(func() error {
				for _, actual := range []func() error{bindingsActual, worksActual, cmActual, statusActual} {
					if err := actual(); err != nil {
						return err
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The re-created cluster placement policy did not stay settled")
		})
	})

	Context("placement policy whose namespace is deleted on the hub", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyNamespaceDeletedNameTemplate, GinkgoParallelProcess())}
		eastTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{regionLabelName: regionEast}},
		}
		// No member cluster is in this region, so the policy keeps a cluster claim outstanding.
		nowhereTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{regionLabelName: "nowhere"}},
		}

		var workNamesByCluster map[string][]string

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy that selects every east cluster, and one cluster in a region that has none")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorWithTerms(eastTerms, intstr.FromString(clusterSelectorCountAll)),
					clusterSelectorWithTerms(nowhereTerms, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs. The
			// policy goes with its namespace; deleting it here matters only should the spec fail before that.
			defer cleanupWorkResourcesOnHubAndMembers()
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should place the ConfigMap on both east clusters, and add a cluster claim for the other selector", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, sameSelectorsForClusters(eastTerms, memberCluster1EastProdName, memberCluster2EastCanaryName), 1),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to bind both east clusters")
			Eventually(appConfigMapPlacedOnClustersActual(memberCluster1EastProd, memberCluster2EastCanary), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on both east clusters")
			Eventually(placementPolicyClaimActual(policyKey, nowhereTerms), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to add the cluster claim")
			// Both east clusters count toward the first selector, and the second one wants a cluster it cannot find.
			Eventually(placementPolicyStatusActual(policyKey, unfulfilledPlacementPolicyStatus(3, 2, 1)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")

			Expect(placementPolicyWorkClustersActual(policyKey, memberCluster1EastProdName, memberCluster2EastCanaryName)()).
				To(Succeed(), "Placement policy %s does not have Works for exactly the east clusters", policyKey)
			var err error
			workNamesByCluster, err = placementPolicyWorkNamesByCluster(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the Works of placement policy %s", policyKey)
		})

		It("can delete the namespace of the placement policy on the hub", func() {
			ns := appNamespace()
			Expect(hubClient.Delete(ctx, &ns)).To(Succeed(), "Failed to delete namespace %s on the hub cluster", ns.Name)
		})

		It("should remove the placement policy, withdrawing its cluster claim", func() {
			Eventually(func() error {
				_, err := retrievePlacementPolicy(policyKey)
				if err == nil {
					return fmt.Errorf("placement policy %s still exists", policyKey)
				}
				return client.IgnoreNotFound(err)
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove placement policy %s", policyKey)
			Eventually(noPlacementPolicyClaimsActual(policyKey), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to withdraw the cluster claim")
		})

		It("should remove the bindings, Works, and resource snapshots of the policy, and the namespace, from the hub", func() {
			Eventually(placementPolicyLeftoversRemovedActual(policyKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the bindings and resource snapshots of the policy")
			Eventually(noPlacementPolicyWorksActual(policyKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the Works of the policy")
			Eventually(workNamespaceRemovedFromClusterActual(hubCluster), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove namespace %s from the hub cluster", nsName)
		})

		It("should remove the AppliedWorks and the ConfigMap from both east clusters", func() {
			Eventually(appliedWorksAbsentOnClustersActual(workNamesByCluster), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the AppliedWorks from the east clusters")
			Eventually(appConfigMapRemovedFromClustersActual(memberCluster1EastProd, memberCluster2EastCanary), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the ConfigMap from the east clusters")
		})
	})
})
