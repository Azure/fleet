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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const (
	placementPolicyNoSelectorsNameTemplate    = "pp-no-selectors-%d"
	clusterPlacementPolicyAllProdNameTemplate = "cpp-all-prod-%d"
	placementPolicyOverlappingNameTemplate    = "pp-overlapping-%d"
	placementPolicyScaledNameTemplate         = "pp-scaled-%d"
	placementPolicyStickyNameTemplate         = "pp-sticky-%d"

	clusterPlacementPolicyBindingDeletedNameTemplate = "cpp-binding-deleted-%d"
)

// The FEP-0001 placement experience is alpha and runs behind the enable-placement-policy-apis hub and member
// agent flags, which test/e2e/setup.sh turns on.
var _ = Describe("placement policy scheduling", Label("placementpolicy"), func() {
	Context("placement policy with no cluster selectors", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyNoSelectorsNameTemplate, GinkgoParallelProcess()),
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy with no cluster selectors")
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

		It("should bind every member cluster to the latest resource snapshot", func() {
			// With no cluster selectors, the policy selects all clusters through a single selector without terms.
			wantSelectorsByCluster := make(map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly, len(allMemberClusterNames))
			for _, clusterName := range allMemberClusterNames {
				wantSelectorsByCluster[clusterName] = []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}}
			}
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster, 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind every member cluster")
		})

		It("should place the ConfigMap on every member cluster", func() {
			Eventually(appConfigMapPlacedOnClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on every member cluster")
		})

		It("should report the policy as scheduled, synchronized, and available on every member cluster", func() {
			// The fleet also has virtual member clusters that are never eligible for scheduling (see setup_test.go);
			// the counts cover the eligible clusters only.
			wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("cluster placement policy selecting all prod clusters", Ordered, func() {
		policyKey := types.NamespacedName{
			Name: fmt.Sprintf(clusterPlacementPolicyAllProdNameTemplate, GinkgoParallelProcess()),
		}
		prodClusters := []string{memberCluster1EastProdName, memberCluster3WestProdName}

		wantSelectorsByCluster := func() map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
			wantSelectors := make(map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly, len(prodClusters))
			for _, clusterName := range prodClusters {
				wantSelectors[clusterName] = []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
					{
						Terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
							{MatchLabels: map[string]string{envLabelName: envProd}},
						},
					},
				}
			}
			return wantSelectors
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that selects all prod clusters")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorMatchingLabels(map[string]string{envLabelName: envProd}, intstr.FromString(clusterSelectorCountAll)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should bind the prod clusters only, to the latest resource snapshot", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind the prod clusters")
		})

		It("should place the ConfigMap on the prod clusters", func() {
			Eventually(appConfigMapPlacedOnClustersActual(memberCluster1EastProd, memberCluster3WestProd), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on the prod clusters")
		})

		It("should report the policy as scheduled, synchronized, and available on the prod clusters", func() {
			wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(prodClusters)))
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})

		It("should not bind or place the ConfigMap on the canary cluster", func() {
			bindingsActual := allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster(), 1)
			canaryActual := appConfigMapRemovedFromClustersActual(memberCluster2EastCanary)
			Consistently(func() error {
				if err := bindingsActual(); err != nil {
					return err
				}
				return canaryActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The canary cluster was bound, or got the ConfigMap")
		})
	})

	Context("placement policy with overlapping cluster selectors", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyOverlappingNameTemplate, GinkgoParallelProcess()),
		}
		// Selector A picks cluster-1 by its alias; selector B picks both prod clusters, cluster-1 among them.
		aliasLabels := map[string]string{placementv1beta1.ClusterAliasLabel: memberCluster1EastProdName}
		prodLabels := map[string]string{envLabelName: envProd}

		wantSelectorsByCluster := func() map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
			// A cluster that satisfies several selectors records each of them, in selector order.
			return map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				memberCluster1EastProdName: {clusterSelectorTermsMatchingLabels(aliasLabels), clusterSelectorTermsMatchingLabels(prodLabels)},
				memberCluster3WestProdName: {clusterSelectorTermsMatchingLabels(prodLabels)},
			}
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy whose two cluster selectors both select cluster-1")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorMatchingLabels(aliasLabels, intstr.FromInt32(1)),
					clusterSelectorMatchingLabels(prodLabels, intstr.FromInt32(2)),
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

		It("should bind cluster-1 once for both selectors, and cluster-3 for the prod selector", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind the selected clusters")
		})

		It("should place the ConfigMap on the prod clusters", func() {
			Eventually(appConfigMapPlacedOnClustersActual(memberCluster1EastProd, memberCluster3WestProd), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on the prod clusters")
		})

		It("should count clusters per selector, and synchronized clusters per cluster", func() {
			// The desired and scheduled counts are sums over the selectors (1 + 2), so cluster-1 counts twice; the
			// synchronized and available counts are over the bound clusters.
			wantStatus := allSynchronizedPlacementPolicyStatus(2)
			wantStatus.DesiredClusters = ptr.To(int32(3))
			wantStatus.ScheduledClusters = ptr.To(int32(3))
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("should not bind or place the ConfigMap on the canary cluster", func() {
			bindingsActual := allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster(), 1)
			canaryActual := appConfigMapRemovedFromClustersActual(memberCluster2EastCanary)
			Consistently(func() error {
				if err := bindingsActual(); err != nil {
					return err
				}
				return canaryActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The canary cluster was bound, or got the ConfigMap")
		})
	})

	Context("placement policy scaled up and back down", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyScaledNameTemplate, GinkgoParallelProcess()),
		}
		eastLabels := map[string]string{regionLabelName: regionEast}
		eastSelectors := []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{clusterSelectorTermsMatchingLabels(eastLabels)}

		// Which east cluster ranks first depends on live capacity, so the specs learn it rather than assume it.
		var firstClusterName, secondClusterName string
		var firstBindingUID types.UID
		var firstBindingCreatedAt metav1.Time

		firstBindingKeptActual := func() func() error {
			return func() error {
				bindings, err := placementPolicyBindingsByCluster(policyKey)
				if err != nil {
					return err
				}
				binding, found := bindings[firstClusterName]
				if !found {
					return fmt.Errorf("placement policy %s has no binding for the first picked cluster %s", policyKey, firstClusterName)
				}
				if binding.GetUID() != firstBindingUID {
					return fmt.Errorf("the binding for the first picked cluster %s was replaced (UID %s, want %s)", firstClusterName, binding.GetUID(), firstBindingUID)
				}
				return nil
			}
		}

		setEastSelectorCount := func(count int32) {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().ClusterSelectors[0].Count = ptr.To(intstr.FromInt32(count))
			})
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy that selects one east cluster")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorMatchingLabels(eastLabels, intstr.FromInt32(1)),
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

		It("should bind one east cluster", func() {
			binding := waitForSinglePlacementPolicyBinding(policyKey)
			firstClusterName, firstBindingUID, firstBindingCreatedAt = binding.GetSpec().ClusterName, binding.GetUID(), binding.GetCreationTimestamp()
			secondClusterName = otherEastClusterName(firstClusterName)

			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				firstClusterName: eastSelectors,
			}, 1), workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to synchronize the binding")
			Eventually(appConfigMapPlacedOnClustersActual(memberClusterNamed(firstClusterName)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on the bound cluster")
		})

		It("can scale the selector up to 2 clusters", func() {
			// The scale-down below needs a well-defined newest binding.
			waitPastCreationSecond(firstBindingCreatedAt)
			setEastSelectorCount(2)
		})

		It("should bind the other east cluster as well, keeping the first binding", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				firstClusterName:  eastSelectors,
				secondClusterName: eastSelectors,
			}, 1), workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to bind both east clusters")
			Expect(firstBindingKeptActual()()).To(Succeed(), "Failed to keep the first binding")
			Eventually(appConfigMapPlacedOnClustersActual(memberCluster1EastProd, memberCluster2EastCanary), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on both east clusters")
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(2)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("can scale the selector back down to 1 cluster", func() {
			setEastSelectorCount(1)
		})

		It("should unbind the newer cluster and keep the first one", func() {
			// A scale-down undoes the latest picks, so the binding made for the scale-up goes, whichever cluster ranks
			// better now.
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				firstClusterName: eastSelectors,
			}, 1), workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to unbind the newer cluster")
			Expect(firstBindingKeptActual()()).To(Succeed(), "Failed to keep the first binding")
			// Deleting a binding deletes its Works, which removes the resources from the cluster by default.
			Eventually(appConfigMapRemovedFromClustersActual(memberClusterNamed(secondClusterName)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the ConfigMap from the unbound cluster")
			Expect(appConfigMapPlacedOnClustersActual(memberClusterNamed(firstClusterName))()).To(Succeed(), "The ConfigMap is gone from the cluster that kept its binding")
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(1)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	// The context relabels a member cluster, so it must not run alongside other specs.
	Context("placement policy sticking to a cluster that stops matching its selector", Ordered, Serial, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyStickyNameTemplate, GinkgoParallelProcess()),
		}
		canaryLabels := map[string]string{envLabelName: envCanary}
		// The changed terms still ask for the canary, which no cluster is once cluster-2 is relabeled.
		changedTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{envLabelName: envCanary, regionLabelName: regionEast}},
		}

		stickyActual := func() func() error {
			bindingsActual := allSynchronizedPlacementPolicyBindingsActual(policyKey, map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				memberCluster2EastCanaryName: {clusterSelectorTermsMatchingLabels(canaryLabels)},
			}, 1)
			placedActual := appConfigMapPlacedOnClustersActual(memberCluster2EastCanary)
			statusActual := placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(1))
			return func() error {
				if err := bindingsActual(); err != nil {
					return err
				}
				if err := placedActual(); err != nil {
					return err
				}
				return statusActual()
			}
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy that selects the canary cluster")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorMatchingLabels(canaryLabels, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go, and cluster-2 must get its label back, even if removing the policy fails;
			// otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			defer setMemberClusterLabel(memberCluster2EastCanaryName, envLabelName, labelsByClusterName[memberCluster2EastCanaryName][envLabelName])
			// Deleting the policy also withdraws its cluster claim.
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should bind the canary cluster and place the ConfigMap on it", func() {
			Eventually(stickyActual(), workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to bind the canary cluster")
		})

		It("can relabel the canary cluster as a prod cluster", func() {
			setMemberClusterLabel(memberCluster2EastCanaryName, envLabelName, envProd)
		})

		It("should keep the binding and the ConfigMap on the relabeled cluster, still counting it", func() {
			// Only a change to the selector's terms re-evaluates the clusters it has picked.
			Consistently(stickyActual(), consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep the relabeled cluster bound")
		})

		It("can change the selector terms", func() {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().ClusterSelectors[0].Terms = changedTerms
			})
		})

		It("should unbind the relabeled cluster and remove the ConfigMap from it", func() {
			Eventually(noPlacementPolicyBindingsActual(policyKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to unbind the relabeled cluster")
			Eventually(appConfigMapRemovedFromClustersActual(memberCluster2EastCanary), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the ConfigMap from the relabeled cluster")
		})

		It("should add a cluster claim carrying the changed terms", func() {
			Eventually(placementPolicyClaimActual(policyKey, changedTerms), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to add the cluster claim")
		})

		It("should report the selector as unfulfilled, with the claim outstanding", func() {
			// With no target cluster left, nothing is out of sync or unavailable.
			Eventually(placementPolicyStatusActual(policyKey, unfulfilledPlacementPolicyStatus(1, 0, 1)), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("cluster placement policy whose binding is deleted out of band", Ordered, func() {
		policyKey := types.NamespacedName{
			Name: fmt.Sprintf(clusterPlacementPolicyBindingDeletedNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		deletedClusterName := memberCluster2EastCanaryName

		var bindingUIDsByCluster map[string]types.UID
		var workUIDsByWork map[string]types.UID
		var cmUIDsByCluster map[string]types.UID

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that selects every member cluster")
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

		It("should place the ConfigMap on every member cluster", func() {
			waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			Eventually(appConfigMapPlacedOnClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on every member cluster")

			bindingUIDsByCluster = placementPolicyBindingUIDsByCluster(policyKey)
			var err error
			workUIDsByWork, err = placementPolicyWorkUIDs(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the Works of cluster placement policy %s", policyKey)
			cmUIDsByCluster, err = configMapUIDsByCluster(cmKey, allMemberClusters...)
			Expect(err).NotTo(HaveOccurred(), "Failed to read the ConfigMap on the member clusters")
		})

		It("can delete the binding for member cluster 2", func() {
			bindings, err := placementPolicyBindingsByCluster(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the bindings of cluster placement policy %s", policyKey)
			binding, found := bindings[deletedClusterName]
			Expect(found).To(BeTrue(), "Cluster placement policy %s has no binding for member cluster 2", policyKey)
			Expect(hubClient.Delete(ctx, binding)).To(Succeed(), "Failed to delete binding %s", binding.GetName())
		})

		It("should re-create the binding, and with it the Works and the ConfigMap, on member cluster 2 only", func() {
			// Deleting the binding deletes its Works, and the work applier removes the ConfigMap they placed (the default
			// whenPlacementDeleted=CleanUpResources). The policy then binds the cluster again, under the same binding
			// name, and the ConfigMap comes back as a new object.
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind every member cluster again")
			Eventually(appConfigMapPlacedOnClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on every member cluster again")

			deletedClusterNamespace := fmt.Sprintf(utils.NamespaceNameFormat, deletedClusterName)
			Eventually(func() error {
				bindings, err := placementPolicyBindingsByCluster(policyKey)
				if err != nil {
					return err
				}
				workUIDs, err := placementPolicyWorkUIDs(policyKey)
				if err != nil {
					return err
				}
				cmUIDs, err := configMapUIDsByCluster(cmKey, allMemberClusters...)
				if err != nil {
					return err
				}
				for clusterName, uid := range bindingUIDsByCluster {
					binding, found := bindings[clusterName]
					if !found {
						return fmt.Errorf("cluster placement policy %s has no binding for cluster %s", policyKey, clusterName)
					}
					if replaced := binding.GetUID() != uid; replaced != (clusterName == deletedClusterName) {
						return fmt.Errorf("the binding for cluster %s replaced = %t (UID %s, before %s)", clusterName, replaced, binding.GetUID(), uid)
					}
				}
				for clusterName, uid := range cmUIDsByCluster {
					if replaced := cmUIDs[clusterName] != uid; replaced != (clusterName == deletedClusterName) {
						return fmt.Errorf("the ConfigMap on cluster %s replaced = %t (UID %s, before %s)", clusterName, replaced, cmUIDs[clusterName], uid)
					}
				}
				// The Works of the other clusters are kept as they were; those of the re-created binding are new.
				for workKey, uid := range workUIDsByWork {
					onDeletedCluster := strings.HasPrefix(workKey, deletedClusterNamespace+"/")
					if gotUID, found := workUIDs[workKey]; found && (gotUID == uid) == onDeletedCluster {
						return fmt.Errorf("work %s replaced = %t (UID %s, before %s)", workKey, gotUID != uid, gotUID, uid)
					} else if !found && !onDeletedCluster {
						return fmt.Errorf("work %s is gone", workKey)
					}
				}
				return nil
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to replace the binding, its Works, and the ConfigMap of member cluster 2 only")

			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})
	})
})
