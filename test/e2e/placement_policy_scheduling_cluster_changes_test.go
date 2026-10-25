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

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

const (
	placementPolicyTolerationsNameTemplate           = "pp-tolerations-%d"
	placementPolicyTaintAfterBindNameTemplate        = "pp-taint-after-bind-%d"
	clusterPlacementPolicyRelabeledMatchNameTemplate = "cpp-relabeled-match-%d"
)

// Every context here taints or relabels a member cluster, so it must not run alongside other specs, and must undo
// the change afterwards, as every spec relies on the member clusters that the suite sets up.
var _ = Describe("placement policy scheduling as member clusters change", Label("placementpolicy"), func() {
	Context("placement policy tolerating a tainted cluster", Ordered, Serial, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyTolerationsNameTemplate, GinkgoParallelProcess()),
		}
		eastTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{regionLabelName: regionEast}},
		}
		taintedClusterNames := []string{memberCluster2EastCanaryName}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("tainting the canary cluster")
			addTaintsToMemberClusters(taintedClusterNames, buildTaints(taintedClusterNames))

			By("creating a placement policy that selects all east clusters")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorWithTerms(eastTerms, intstr.FromString(clusterSelectorCountAll)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go, and the taint too, even if removing the policy fails; otherwise they leak
			// into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			defer removeTaintsFromMemberClusters(taintedClusterNames)
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should bind the untainted east cluster only", func() {
			waitForPlacementPolicyPlacedOnlyOn(policyKey, eastTerms, memberCluster1EastProdName)
		})

		It("should not bind or place the ConfigMap on the tainted cluster", func() {
			bindingsActual := allSynchronizedPlacementPolicyBindingsActual(policyKey, sameSelectorsForClusters(eastTerms, memberCluster1EastProdName), 1)
			taintedActual := appConfigMapRemovedFromClustersActual(memberCluster2EastCanary)
			Consistently(func() error {
				if err := bindingsActual(); err != nil {
					return err
				}
				return taintedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The tainted cluster was bound, or got the ConfigMap")
		})

		It("can add a toleration for the taint", func() {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().Tolerations = placementPolicyTolerationsFor(taintedClusterNames...)
			})
		})

		It("should bind the tainted east cluster as well", func() {
			waitForPlacementPolicyPlacedOnlyOn(policyKey, eastTerms, memberCluster1EastProdName, memberCluster2EastCanaryName)
		})
	})

	Context("placement policy keeping a cluster that is tainted after it is bound", Ordered, Serial, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyTaintAfterBindNameTemplate, GinkgoParallelProcess()),
		}
		prodTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{envLabelName: envProd}},
		}

		// Which prod cluster ranks first depends on live capacity, so the specs learn it rather than assume it.
		var taintedClusterName, otherClusterName string
		var taintedBindingUID types.UID
		var taintedBindingCreatedAt metav1.Time

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy that selects one prod cluster")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorWithTerms(prodTerms, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go, and the taint too, even if removing the policy fails; otherwise they leak
			// into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			defer func() {
				if taintedClusterName != "" {
					removeTaintsFromMemberClusters([]string{taintedClusterName})
				}
			}()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should bind one prod cluster", func() {
			binding := waitForSinglePlacementPolicyBinding(policyKey)
			taintedClusterName, taintedBindingUID, taintedBindingCreatedAt = binding.GetSpec().ClusterName, binding.GetUID(), binding.GetCreationTimestamp()
			otherClusterName = otherProdClusterName(taintedClusterName)

			waitForPlacementPolicyPlacedOnlyOn(policyKey, prodTerms, taintedClusterName)
		})

		It("can taint the bound cluster", func() {
			// Once the taint is gone, the trim needs a well-defined newest binding: the one the taint causes.
			waitPastCreationSecond(taintedBindingCreatedAt)
			addTaintsToMemberClusters([]string{taintedClusterName}, buildTaints([]string{taintedClusterName}))
		})

		It("should keep the tainted cluster bound, and bind the other prod cluster in its place", func() {
			// The tainted cluster keeps its binding and its resources, so it is still a target of the resource
			// status; it no longer counts toward the selector, though, so the other prod cluster fills the gap.
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, sameSelectorsForClusters(prodTerms, taintedClusterName, otherClusterName), 1),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to bind the other prod cluster")
			Expect(placementPolicyBindingsKeptActual(policyKey, map[string]types.UID{taintedClusterName: taintedBindingUID})()).
				To(Succeed(), "Failed to keep the binding of the tainted cluster")
			Eventually(appConfigMapPlacedOnClustersActual(memberClusterNamed(taintedClusterName), memberClusterNamed(otherClusterName)),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place the ConfigMap on both prod clusters")

			wantStatus := allSynchronizedPlacementPolicyStatus(2)
			wantStatus.DesiredClusters = ptr.To(int32(1))
			wantStatus.ScheduledClusters = ptr.To(int32(1))
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("can remove the taint", func() {
			removeTaintsFromMemberClusters([]string{taintedClusterName})
		})

		It("should unbind the other prod cluster, and keep the originally bound one", func() {
			// Back in the running, the originally bound cluster has the older binding, so the count trims the newer
			// one.
			waitForPlacementPolicyPlacedOnlyOn(policyKey, prodTerms, taintedClusterName)
			Expect(placementPolicyBindingsKeptActual(policyKey, map[string]types.UID{taintedClusterName: taintedBindingUID})()).
				To(Succeed(), "Failed to keep the binding of the originally bound cluster")
		})
	})

	Context("cluster placement policy whose claim an existing cluster fulfills by starting to match", Ordered, Serial, func() {
		policyKey := types.NamespacedName{
			Name: fmt.Sprintf(clusterPlacementPolicyRelabeledMatchNameTemplate, GinkgoParallelProcess()),
		}
		// No member cluster is in the south region until cluster-3 is relabeled.
		southTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{regionLabelName: regionSouth}},
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that selects one south cluster")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorWithTerms(southTerms, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go, and cluster-3 must get its label back, even if removing the policy fails;
			// otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			defer setMemberClusterLabel(memberCluster3WestProdName, regionLabelName, regionWest)
			// Deleting the policy also withdraws its cluster claim, should the spec fail before the claim is.
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should add a cluster claim, binding no cluster", func() {
			Eventually(placementPolicyClaimActual(policyKey, southTerms), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to add the cluster claim")
			Eventually(placementPolicyStatusActual(policyKey, unfulfilledPlacementPolicyStatus(1, 0, 1)), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
			Expect(noPlacementPolicyBindingsActual(policyKey)()).To(Succeed(), "A cluster was bound")
		})

		It("can relabel cluster-3 into the south region", func() {
			setMemberClusterLabel(memberCluster3WestProdName, regionLabelName, regionSouth)
		})

		It("should bind the relabeled cluster, and report the selector as fulfilled", func() {
			// The wanted status has no active cluster claim.
			waitForPlacementPolicyPlacedOnlyOn(policyKey, southTerms, memberCluster3WestProdName)
		})

		It("should withdraw the cluster claim", func() {
			Eventually(noPlacementPolicyClaimsActual(policyKey), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to withdraw the cluster claim")
		})
	})
})
