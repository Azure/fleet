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
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
)

const (
	clusterPlacementPolicyMinCountNameTemplate            = "cpp-min-count-%d"
	placementPolicyKeepSearchingNameTemplate              = "pp-keep-searching-%d"
	placementPolicyLabelExpressionsNameTemplate           = "pp-label-expressions-%d"
	clusterPlacementPolicyPropertyExpressionsNameTemplate = "cpp-property-expressions-%d"
	placementPolicyInvalidSelectorNameTemplate            = "pp-invalid-selector-%d"
	placementPolicyClaimTermsChangedNameTemplate          = "pp-claim-terms-changed-%d"
)

// setPlacementPolicySelectorTerms replaces the terms of the policy's first cluster selector. A change to the terms
// re-evaluates the clusters that the selector has picked from scratch.
func setPlacementPolicySelectorTerms(key types.NamespacedName, terms []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm) {
	GinkgoHelper()

	updatePlacementPolicy(key, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
		policy.GetSpec().ClusterSelectors[0].Terms = terms
	})
}

var _ = Describe("placement policy scheduling with cluster selector options", Label("placementpolicy"), func() {
	Context("cluster placement policy that reaches its minimum count but not its count", Ordered, func() {
		policyKey := types.NamespacedName{
			Name: fmt.Sprintf(clusterPlacementPolicyMinCountNameTemplate, GinkgoParallelProcess()),
		}
		// Only two member clusters are in the east region.
		eastTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{regionLabelName: regionEast}},
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that wants 3 east clusters, and at least 2")
			selector := clusterSelectorWithTerms(eastTerms, intstr.FromInt32(3))
			selector.MinCount = ptr.To(int32(2))
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors:  []kfplacementv1alpha1.ClusterSelector{selector},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			// Deleting the policy also withdraws its cluster claim.
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should bind both east clusters and place the ConfigMap on them", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, sameSelectorsForClusters(eastTerms, memberCluster1EastProdName, memberCluster2EastCanaryName), 1),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to bind both east clusters")
			Eventually(appConfigMapPlacedOnClustersActual(memberCluster1EastProd, memberCluster2EastCanary), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on both east clusters")
		})

		It("should add a cluster claim for the missing cluster", func() {
			Eventually(placementPolicyClaimActual(policyKey, eastTerms), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to add the cluster claim")
		})

		It("should report the selector as short of its count, with its minimum count reached", func() {
			// Reaching minCount but not count still reports the policy as not scheduled; only the message tells the two
			// cases apart.
			Eventually(placementPolicyStatusActual(policyKey, unfulfilledPlacementPolicyStatus(3, 2, 1)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
			Eventually(placementPolicyConditionMessageActual(policyKey, kfplacementv1alpha1.PlacementPolicyCondTypeScheduled, "have reached their minimum cluster counts"),
				eventuallyDuration, eventuallyInterval).Should(Succeed(), "The Scheduled condition message does not report the minimum count as reached")
		})
	})

	Context("placement policy that keeps searching for an unfulfillable selector", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyKeepSearchingNameTemplate, GinkgoParallelProcess()),
		}
		// No member cluster is in this region.
		nowhereTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{regionLabelName: "nowhere"}},
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy that keeps searching for a cluster in a region that has none")
			selector := clusterSelectorWithTerms(nowhereTerms, intstr.FromInt32(1))
			selector.WhenUnfulfilled = kfplacementv1alpha1.WhenUnfulfilledOptionKeepSearching
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors:  []kfplacementv1alpha1.ClusterSelector{selector},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should report the selector as unfulfilled, with no cluster claim", func() {
			Eventually(placementPolicyStatusActual(policyKey, unfulfilledPlacementPolicyStatus(1, 0, 0)), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("should neither add a cluster claim nor bind a cluster", func() {
			claimsActual := noPlacementPolicyClaimsActual(policyKey)
			bindingsActual := noPlacementPolicyBindingsActual(policyKey)
			Consistently(func() error {
				if err := claimsActual(); err != nil {
					return err
				}
				return bindingsActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "A cluster claim was added, or a cluster bound")
		})

		It("can switch the selector to add a cluster claim when unfulfilled", func() {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().ClusterSelectors[0].WhenUnfulfilled = kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim
			})
		})

		It("should add a cluster claim carrying the selector terms", func() {
			Eventually(placementPolicyClaimActual(policyKey, nowhereTerms), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to add the cluster claim")
			Eventually(placementPolicyStatusActual(policyKey, unfulfilledPlacementPolicyStatus(1, 0, 1)), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("placement policy selecting clusters by label expressions", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyLabelExpressionsNameTemplate, GinkgoParallelProcess()),
		}
		// Every member cluster carries the env label, and none carries this one, so Exists and DoesNotExist are
		// each checked against a label that is present and one that is absent.
		const neverSetLabelName = "fleet-e2e/never-set"

		labelExpressionTerm := func(exprs ...kfplacementv1alpha1.LabelClusterPropertyExpression) kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm {
			return kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{MatchLabelExpressions: exprs}
		}
		inTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			labelExpressionTerm(kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: envLabelName, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorIn, Values: []string{envCanary},
			}),
		}
		notInTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			labelExpressionTerm(kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: envLabelName, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorNotIn, Values: []string{envCanary},
			}),
		}
		// Every member cluster satisfies both expressions.
		matchingPresenceTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			labelExpressionTerm(
				kfplacementv1alpha1.LabelClusterPropertyExpression{Key: envLabelName, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists},
				kfplacementv1alpha1.LabelClusterPropertyExpression{Key: neverSetLabelName, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorDoesNotExist},
			),
		}
		// No member cluster satisfies either term.
		mismatchingPresenceTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			labelExpressionTerm(kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: envLabelName, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorDoesNotExist,
			}),
			labelExpressionTerm(kfplacementv1alpha1.LabelClusterPropertyExpression{
				Key: neverSetLabelName, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorExists,
			}),
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy that selects all clusters whose env label is in the given values")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorWithTerms(inTerms, intstr.FromString(clusterSelectorCountAll)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			// Deleting the policy also withdraws its cluster claim.
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should bind the canary cluster for the In expression", func() {
			waitForPlacementPolicyPlacedOnlyOn(policyKey, inTerms, memberCluster2EastCanaryName)
		})

		It("can switch to a NotIn expression", func() {
			setPlacementPolicySelectorTerms(policyKey, notInTerms)
		})

		It("should bind the prod clusters for the NotIn expression", func() {
			waitForPlacementPolicyPlacedOnlyOn(policyKey, notInTerms, memberCluster1EastProdName, memberCluster3WestProdName)
		})

		It("can switch to Exists and DoesNotExist expressions that every cluster satisfies", func() {
			setPlacementPolicySelectorTerms(policyKey, matchingPresenceTerms)
		})

		It("should bind every member cluster", func() {
			waitForPlacementPolicyPlacedOnlyOn(policyKey, matchingPresenceTerms, allMemberClusterNames...)
		})

		It("can switch to Exists and DoesNotExist expressions that no cluster satisfies", func() {
			setPlacementPolicySelectorTerms(policyKey, mismatchingPresenceTerms)
		})

		It("should unbind every member cluster and add a cluster claim", func() {
			Eventually(noPlacementPolicyBindingsActual(policyKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to unbind every member cluster")
			Eventually(appConfigMapRemovedFromClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the ConfigMap from every member cluster")
			Eventually(placementPolicyClaimActual(policyKey, mismatchingPresenceTerms), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to add the cluster claim")
			// A count: All selector wants at least one cluster.
			Eventually(placementPolicyStatusActual(policyKey, unfulfilledPlacementPolicyStatus(1, 0, 1)), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("cluster placement policy selecting clusters by cluster property expressions", Ordered, func() {
		policyKey := types.NamespacedName{
			Name: fmt.Sprintf(clusterPlacementPolicyPropertyExpressionsNameTemplate, GinkgoParallelProcess()),
		}
		nodeCountTerms := func(operator kfplacementv1alpha1.LabelClusterPropertyExpressionOperator, value string) []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm {
			return []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
				{
					MatchClusterPropertyExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
						{Key: propertyprovider.NodeCountProperty, Operator: operator, Values: []string{value}},
					},
				},
			}
		}
		// The member clusters have 2, 3, and 4 nodes respectively, control plane included (see kindconfigs/).
		atLeastThreeNodesTerms := nodeCountTerms(kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGe, "3")
		exactlyTwoNodesTerms := nodeCountTerms(kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorEq, "2")

		BeforeAll(func() {
			// The node count property comes from the property provider; the legacy property selector specs are
			// gated the same way.
			if !isAzurePropertyProviderEnabled {
				Skip("Skipping this test spec as Azure property provider is not enabled in the test environment")
			}

			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that selects all clusters with at least 3 nodes")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorWithTerms(atLeastThreeNodesTerms, intstr.FromString(clusterSelectorCountAll)),
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

		It("should bind the clusters with at least 3 nodes", func() {
			waitForPlacementPolicyPlacedOnlyOn(policyKey, atLeastThreeNodesTerms, memberCluster2EastCanaryName, memberCluster3WestProdName)
		})

		It("can switch to clusters with exactly 2 nodes", func() {
			setPlacementPolicySelectorTerms(policyKey, exactlyTwoNodesTerms)
		})

		It("should bind the cluster with exactly 2 nodes only", func() {
			waitForPlacementPolicyPlacedOnlyOn(policyKey, exactlyTwoNodesTerms, memberCluster1EastProdName)
		})
	})

	Context("placement policy whose cluster selector cannot be evaluated", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyInvalidSelectorNameTemplate, GinkgoParallelProcess()),
		}
		prodTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{envLabelName: envProd}},
		}
		// Admission accepts Gt on any key, but only a cluster property can be compared numerically; a label
		// expression with Gt fails when the selector is evaluated.
		invalidTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{
				MatchLabelExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: envLabelName, Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"1"}},
				},
			},
		}

		var bindingUIDs map[string]types.UID
		// The policy generation that the last round with valid selectors observed.
		var validGeneration int64

		invalidSelectorStatus := func() kfplacementv1alpha1.PlacementPolicyStatus {
			// Only the Scheduled condition is re-evaluated, and the cluster counts are cleared; the resource revision,
			// the synchronization and availability counts, and their conditions are left as the last valid round
			// reported them.
			lastValid := func(cond metav1.Condition) metav1.Condition {
				cond.ObservedGeneration = validGeneration
				return cond
			}
			return kfplacementv1alpha1.PlacementPolicyStatus{
				Conditions: []metav1.Condition{
					lastValid(placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected, metav1.ConditionTrue,
						kfplacementv1alpha1.PlacementPolicyResourceCollectedCondReasonAllResourcesCollected)),
					placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeScheduled, metav1.ConditionFalse,
						placementPolicyScheduledCondReasonInvalidClusterSelectors),
					lastValid(placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized, metav1.ConditionTrue,
						kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonAllClustersSynchronized)),
					lastValid(placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeAvailable, metav1.ConditionTrue,
						kfplacementv1alpha1.PlacementPolicyAvailableCondReasonAllClustersAvailable)),
				},
				SynchronizedClusters:       ptr.To(int32(2)),
				ResourcesAvailableClusters: ptr.To(int32(2)),
				ActiveClusterClaims:        ptr.To(int32(0)),
			}
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy that selects all prod clusters")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorWithTerms(prodTerms, intstr.FromString(clusterSelectorCountAll)),
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

		It("should bind the prod clusters", func() {
			waitForPlacementPolicyPlacedOnlyOn(policyKey, prodTerms, memberCluster1EastProdName, memberCluster3WestProdName)
			bindingUIDs = placementPolicyBindingUIDsByCluster(policyKey)

			policy, err := retrievePlacementPolicy(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get placement policy %s", policyKey)
			validGeneration = policy.GetGeneration()
		})

		It("can change the selector to one that cannot be evaluated", func() {
			setPlacementPolicySelectorTerms(policyKey, invalidTerms)
		})

		It("should report the cluster selectors as invalid, leaving the resource status as it was", func() {
			Eventually(placementPolicyStatusActual(policyKey, invalidSelectorStatus()), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("should keep the existing bindings and the ConfigMap on the prod clusters", func() {
			// The bindings still record the last valid terms.
			bindingsActual := allSynchronizedPlacementPolicyBindingsActual(policyKey, sameSelectorsForClusters(prodTerms, memberCluster1EastProdName, memberCluster3WestProdName), 1)
			keptActual := placementPolicyBindingsKeptActual(policyKey, bindingUIDs)
			placedActual := appConfigMapPlacedOnClustersActual(memberCluster1EastProd, memberCluster3WestProd)
			statusActual := placementPolicyStatusActual(policyKey, invalidSelectorStatus())
			Consistently(func() error {
				for _, actual := range []func() error{bindingsActual, keptActual, placedActual, statusActual} {
					if err := actual(); err != nil {
						return err
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The bindings, the ConfigMap, or the status changed while the selector was invalid")
		})

		It("can fix the selector", func() {
			setPlacementPolicySelectorTerms(policyKey, prodTerms)
		})

		It("should report the policy as scheduled again, keeping the bindings", func() {
			waitForPlacementPolicyPlacedOnlyOn(policyKey, prodTerms, memberCluster1EastProdName, memberCluster3WestProdName)
			Expect(placementPolicyBindingsKeptActual(policyKey, bindingUIDs)()).To(Succeed(), "Failed to keep the bindings")
		})
	})

	Context("placement policy whose unfulfilled selector changes its terms", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyClaimTermsChangedNameTemplate, GinkgoParallelProcess()),
		}
		// No member cluster is in either region.
		nowhereTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{regionLabelName: "nowhere"}},
		}
		elsewhereTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{regionLabelName: "elsewhere"}},
		}

		var claimName string
		var claimUID types.UID

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy that selects one cluster in a region that has none")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorWithTerms(nowhereTerms, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			// Deleting the policy also withdraws its cluster claim.
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should add a cluster claim carrying the selector terms", func() {
			Eventually(placementPolicyClaimActual(policyKey, nowhereTerms), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to add the cluster claim")
			Eventually(placementPolicyStatusActual(policyKey, unfulfilledPlacementPolicyStatus(1, 0, 1)), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")

			claims, err := claimsOfPolicy(policyKey.Name, policyKey.Namespace)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the cluster claims of placement policy %s", policyKey)
			Expect(claims.Items).To(HaveLen(1), "Placement policy %s does not have exactly one cluster claim", policyKey)
			claimName, claimUID = claims.Items[0].Name, claims.Items[0].UID
		})

		It("can change the terms of the selector", func() {
			setPlacementPolicySelectorTerms(policyKey, elsewhereTerms)
		})

		It("should replace the cluster claim under the same name, carrying the new terms", func() {
			// A claim serves only the terms it was issued for: the policy withdraws it and issues a new one, under the
			// same name, which derives from the policy and the selector index.
			claimActual := placementPolicyClaimActual(policyKey, elsewhereTerms)
			Eventually(func() error {
				if err := claimActual(); err != nil {
					return err
				}
				claims, err := claimsOfPolicy(policyKey.Name, policyKey.Namespace)
				if err != nil {
					return err
				}
				if len(claims.Items) != 1 {
					return fmt.Errorf("placement policy %s has %d cluster claims, want 1", policyKey, len(claims.Items))
				}
				claim := &claims.Items[0]
				switch {
				case claim.Name != claimName:
					return fmt.Errorf("the cluster claim of placement policy %s is named %s, want %s", policyKey, claim.Name, claimName)
				case claim.UID == claimUID:
					return fmt.Errorf("the cluster claim %s of placement policy %s has not been replaced (UID %s)", claim.Name, policyKey, claim.UID)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to replace the cluster claim")
			Eventually(placementPolicyStatusActual(policyKey, unfulfilledPlacementPolicyStatus(1, 0, 1)), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})
})
