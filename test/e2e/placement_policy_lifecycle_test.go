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

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const placementPolicyCleanedUpNameTemplate = "pp-cleaned-up-%d"

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

			clusterNamesByReservedNamespace := make(map[string]string, len(allMemberClusterNames))
			for _, clusterName := range allMemberClusterNames {
				clusterNamesByReservedNamespace[fmt.Sprintf(utils.NamespaceNameFormat, clusterName)] = clusterName
			}
			works, err := placementPolicyWorks(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the Works of placement policy %s", policyKey)
			workNamesByCluster = make(map[string][]string, len(allMemberClusterNames))
			for _, work := range works {
				clusterName, found := clusterNamesByReservedNamespace[work.Namespace]
				Expect(found).To(BeTrue(), "Work %s/%s of placement policy %s is not in the reserved namespace of a member cluster", work.Namespace, work.Name, policyKey)
				workNamesByCluster[clusterName] = append(workNamesByCluster[clusterName], work.Name)
			}
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
})
