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
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

const (
	placementPolicyCleanedUpNameTemplate        = "pp-cleaned-up-%d"
	placementPolicyRecreatedNameTemplate        = "pp-recreated-%d"
	placementPolicyNamespaceDeletedNameTemplate = "pp-namespace-deleted-%d"
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
		var cmResourceVersionsByCluster map[string]string

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy")
			Expect(hubClient.Create(ctx, newPolicy())).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			// Deleting the hub namespace also removes the bindings, should the spec fail with them still orphaned, and
			// the resource snapshot that the re-created policy reuses without owning it.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the ConfigMap on every member cluster", func() {
			waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			Eventually(appConfigMapPlacedOnClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on every member cluster")

			bindingUIDsByCluster = placementPolicyBindingUIDsByCluster(policyKey)
			var err error
			workUIDsByWork, err = placementPolicyWorkUIDs(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the Works of placement policy %s", policyKey)
			Expect(workUIDsByWork).NotTo(BeEmpty(), "Placement policy %s has no Works", policyKey)
			cmResourceVersionsByCluster, err = configMapResourceVersionsByCluster(cmKey, allMemberClusters...)
			Expect(err).NotTo(HaveOccurred(), "Failed to read the ConfigMap on the member clusters")
		})

		It("can delete the placement policy, orphaning its dependents", func() {
			deletePlacementPolicyAndWait(policyKey, client.PropagationPolicy(metav1.DeletePropagationOrphan))
		})

		It("should leave the bindings behind, orphaned, with their Works and the ConfigMaps as they were", func() {
			// The garbage collector drops the dependents' owner references before it lets the policy go.
			orphanedActual := placementPolicyBindingOwnershipActual(policyKey, bindingUIDsByCluster, false)
			Eventually(orphanedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to orphan the bindings")

			worksActual := placementPolicyWorksKeptActual(policyKey, workUIDsByWork)
			cmActual := configMapUnchangedOnClustersActual(cmKey, cmResourceVersionsByCluster)
			Consistently(func() error {
				if err := orphanedActual(); err != nil {
					return err
				}
				if err := worksActual(); err != nil {
					return err
				}
				return cmActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The orphaned bindings, their Works, or the ConfigMaps changed")
		})

		It("can re-create the placement policy under the same name", func() {
			Expect(hubClient.Create(ctx, newPolicy())).To(Succeed(), "Failed to re-create placement policy %s", policyKey)
		})

		// The deletion orphaned the resource snapshot too, and the re-created policy reuses it without taking it over.
		// Until that is fixed, the specs check only the bindings, and leave out the snapshot and the resource status of
		// the policy, which is reported against it.
		It("should adopt the orphaned bindings, keeping them, their Works, and the ConfigMaps as they were", func() {
			adoptedActual := placementPolicyBindingOwnershipActual(policyKey, bindingUIDsByCluster, true)
			Eventually(adoptedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to adopt the orphaned bindings")

			worksActual := placementPolicyWorksKeptActual(policyKey, workUIDsByWork)
			cmActual := configMapUnchangedOnClustersActual(cmKey, cmResourceVersionsByCluster)
			Consistently(func() error {
				if err := adoptedActual(); err != nil {
					return err
				}
				if err := worksActual(); err != nil {
					return err
				}
				return cmActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The adopted bindings, their Works, or the ConfigMaps changed")
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
