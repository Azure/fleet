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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

const (
	placementPolicyInteropCRPNameTemplate   = "pp-interop-crp-%d"
	placementPolicyAfterCRPNameTemplate     = "pp-after-crp-%d"
	placementPolicySharedFirstNameTemplate  = "pp-shared-first-%d"
	placementPolicySharedSecondNameTemplate = "pp-shared-second-%d"
)

// placementPolicyOwnedByOtherKubeFleetAPIObjectsReason is the apply result type, copied into a binding's failed
// resources, of a manifest whose object an AppliedWork of the older placement APIs owns.
const placementPolicyOwnedByOtherKubeFleetAPIObjectsReason = "OwnedByOtherKubeFleetAPIObjects"

// configMapPlacedByOlderPlacementAPIsOnlyActual checks that the ConfigMap on the cluster matches its hub copy and is
// owned by an AppliedWork of the older placement APIs (e.g., that of a CRP), and by none of the placement policy APIs.
func configMapPlacedByOlderPlacementAPIsOnlyActual(cluster *framework.Cluster, key types.NamespacedName) func() error {
	return func() error {
		if err := validateConfigMapOnCluster(cluster, key); err != nil {
			return fmt.Errorf("cluster %s: %w", cluster.ClusterName, err)
		}
		cm := &corev1.ConfigMap{}
		if err := cluster.KubeClient.Get(ctx, key, cm); err != nil {
			return fmt.Errorf("cluster %s: failed to get ConfigMap %s: %w", cluster.ClusterName, key, err)
		}
		if ownedByPlacementPolicyAppliedWork(cm) {
			return fmt.Errorf("cluster %s: ConfigMap %s is owned by an AppliedWork of the placement policy APIs (owner references: %+v)",
				cluster.ClusterName, key, cm.OwnerReferences)
		}
		for _, ownerRef := range cm.OwnerReferences {
			if ownerRef.APIVersion == placementv1beta1.GroupVersion.String() && ownerRef.Kind == placementv1beta1.AppliedWorkKind {
				return nil
			}
		}
		return fmt.Errorf("cluster %s: ConfigMap %s is not owned by an AppliedWork of the older placement APIs (owner references: %+v)",
			cluster.ClusterName, key, cm.OwnerReferences)
	}
}

// The FEP-0001 placement experience is alpha and runs behind the enable-placement-policy-apis hub and member
// agent flags, which test/e2e/setup.sh turns on.
var _ = Describe("placement policy interoperability", Label("placementpolicy"), func() {
	Context("placement policy selecting a ConfigMap that a CRP has placed", Ordered, func() {
		crpKey := types.NamespacedName{Name: fmt.Sprintf(placementPolicyInteropCRPNameTemplate, GinkgoParallelProcess())}
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyAfterCRPNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		// Both the CRP and the policy place on cluster-1 alone.
		aliasLabels := map[string]string{placementv1beta1.ClusterAliasLabel: memberCluster1EastProdName}
		crpStatusActual := func() func() error {
			return customizedPlacementStatusUpdatedActual(crpKey, workResourceIdentifiers(), []string{memberCluster1EastProdName}, nil, "0", true)
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a CRP that places the app namespace, and the ConfigMap in it, on member cluster 1")
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpKey.Name,
					// The custom finalizer lets ensureCRPAndRelatedResourcesDeleted observe the CRP's own cleanup.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{memberCluster1EastProdName},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpKey.Name)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy or the CRP fails; otherwise they leak into later
			// specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			// The policy goes first: it never placed the ConfigMap, so removing it leaves the CRP's placement alone.
			deletePlacementPolicyAndWait(policyKey)
			ensureCRPAndRelatedResourcesDeleted(crpKey.Name, []*framework.Cluster{memberCluster1EastProd})
		})

		It("should place the namespace and the ConfigMap on member cluster 1 through the CRP", func() {
			Eventually(crpStatusActual(), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected CRP status")
			Eventually(workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the namespace and the ConfigMap on member cluster 1")
			Expect(configMapPlacedByOlderPlacementAPIsOnlyActual(memberCluster1EastProd, cmKey)()).
				To(Succeed(), "The ConfigMap on member cluster 1 is not managed by the CRP alone")
		})

		It("can create a placement policy that selects the same ConfigMap for member cluster 1, sharing ownership", func() {
			// Even the most permissive sync strategy, which takes over any existing object and shares its ownership,
			// must not take over an object that another KubeFleet placement API manages.
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorMatchingLabels(aliasLabels, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
				SyncStrategy: &kfplacementv1alpha1.SyncStrategy{
					WhenAlreadyExists: kfplacementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
					WhenOwnedByOthers: kfplacementv1alpha1.WhenOwnedByOthersOptionShareOwnership,
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		It("should report the ConfigMap as owned by another KubeFleet placement API", func() {
			wantBindings := map[string]wantPlacementBinding{
				memberCluster1EastProdName: {
					selectors: []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{clusterSelectorTermsMatchingLabels(aliasLabels)},
					status:    configMapNotAppliedPlacementBindingStatus(cmKey, placementPolicyOwnedByOtherKubeFleetAPIObjectsReason, nil),
				},
			}
			Eventually(placementPolicyBindingsActual(policyKey, wantBindings), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the ConfigMap as owned by another KubeFleet placement API")
			Eventually(placementPolicyStatusActual(policyKey, someClustersNotSynchronizedPlacementPolicyStatus(1, 0)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("should leave the CRP and its ConfigMap on member cluster 1 as they were", func() {
			statusActual := crpStatusActual()
			cmActual := configMapPlacedByOlderPlacementAPIsOnlyActual(memberCluster1EastProd, cmKey)
			Consistently(func() error {
				if err := statusActual(); err != nil {
					return err
				}
				return cmActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The CRP or its ConfigMap on member cluster 1 changed")
		})

		It("can delete the placement policy", func() {
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should still leave the CRP and its ConfigMap on member cluster 1 as they were", func() {
			// The policy never took the ConfigMap over, so removing its AppliedWork leaves the ConfigMap alone.
			statusActual := crpStatusActual()
			cmActual := configMapPlacedByOlderPlacementAPIsOnlyActual(memberCluster1EastProd, cmKey)
			Consistently(func() error {
				if err := statusActual(); err != nil {
					return err
				}
				return cmActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The CRP or its ConfigMap on member cluster 1 changed")
		})
	})

	Context("placement policies selecting the same ConfigMap for the same member cluster", Ordered, func() {
		nsName := appNamespace().Name
		firstPolicyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicySharedFirstNameTemplate, GinkgoParallelProcess())}
		secondPolicyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicySharedSecondNameTemplate, GinkgoParallelProcess())}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		// Both policies place on cluster-1 alone.
		aliasLabels := map[string]string{placementv1beta1.ClusterAliasLabel: memberCluster1EastProdName}
		newPolicy := func(key types.NamespacedName, syncStrategy *kfplacementv1alpha1.SyncStrategy) kfplacementv1alpha1.PlacementPolicyAccessor {
			return newPlacementPolicyObject(key, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorMatchingLabels(aliasLabels, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
				SyncStrategy:      syncStrategy,
			})
		}
		wantSelectorsByCluster := func() map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
			return map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				memberCluster1EastProdName: {clusterSelectorTermsMatchingLabels(aliasLabels)},
			}
		}
		// secondPolicyFailedActual checks that the second policy's binding reports the ConfigMap as not applied, for the
		// given reason, and that the policy reports the cluster as not synchronized.
		secondPolicyFailedActual := func(reason string) func() error {
			bindingsActual := placementPolicyBindingsActual(secondPolicyKey, map[string]wantPlacementBinding{
				memberCluster1EastProdName: {
					selectors: wantSelectorsByCluster()[memberCluster1EastProdName],
					status:    configMapNotAppliedPlacementBindingStatus(cmKey, reason, nil),
				},
			})
			statusActual := placementPolicyStatusActual(secondPolicyKey, someClustersNotSynchronizedPlacementPolicyStatus(1, 0))
			return func() error {
				if err := bindingsActual(); err != nil {
					return err
				}
				return statusActual()
			}
		}
		// firstPolicyUndisturbedActual checks that the first policy still has the ConfigMap placed on cluster-1 and
		// synchronized, with no other policy owning it.
		firstPolicyUndisturbedActual := func() func() error {
			bindingsActual := allSynchronizedPlacementPolicyBindingsActual(firstPolicyKey, wantSelectorsByCluster(), 1)
			statusActual := placementPolicyStatusActual(firstPolicyKey, allSynchronizedPlacementPolicyStatus(1))
			cmActual := configMapPlacedByPlacementPolicyActual(memberCluster1EastProd, cmKey, firstPolicyKey)
			return func() error {
				for _, actual := range []func() error{bindingsActual, statusActual, cmActual} {
					if err := actual(); err != nil {
						return err
					}
				}
				return nil
			}
		}

		var firstCMUID types.UID

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating the first placement policy, with the default sync strategy")
			Expect(hubClient.Create(ctx, newPolicy(firstPolicyKey, nil))).To(Succeed(), "Failed to create placement policy %s", firstPolicyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policies fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			// The first policy goes first; the ConfigMap can leave the member cluster only once neither policy places it.
			deletePlacementPolicyAndWait(firstPolicyKey)
			ensurePlacementPolicyRemoved(secondPolicyKey)
		})

		It("should place the ConfigMap on member cluster 1 through the first policy", func() {
			Eventually(firstPolicyUndisturbedActual(), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap through the first placement policy")

			cm := &corev1.ConfigMap{}
			Expect(memberCluster1EastProd.KubeClient.Get(ctx, cmKey, cm)).To(Succeed(), "Failed to get ConfigMap %s on member cluster 1", cmKey)
			firstCMUID = cm.UID
		})

		It("can create the second placement policy, which always takes over existing resources and shares their ownership", func() {
			// Even the most permissive sync strategy must not take over an object that another placement policy
			// manages.
			Expect(hubClient.Create(ctx, newPolicy(secondPolicyKey, &kfplacementv1alpha1.SyncStrategy{
				WhenAlreadyExists: kfplacementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
				WhenOwnedByOthers: kfplacementv1alpha1.WhenOwnedByOthersOptionShareOwnership,
			}))).To(Succeed(), "Failed to create placement policy %s", secondPolicyKey)
		})

		It("should fail to take the ConfigMap over for the second policy, leaving the first policy alone", func() {
			// KubeFleet refuses to place the same object twice, co-ownership or not.
			Eventually(secondPolicyFailedActual("FailedToTakeOver"), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the ConfigMap as not taken over for the second placement policy")
			Consistently(firstPolicyUndisturbedActual(), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The first placement policy or its ConfigMap changed")
		})

		It("can switch the second placement policy to the default sync strategy", func() {
			// With a takeover option, the second policy's work applier would drop the ConfigMap's owner reference to the
			// first policy's AppliedWork as soon as the first policy's Work is gone, and could take the ConfigMap over
			// before the garbage collector removes it. Under the default whenAlreadyExists=ReportError, the ConfigMap
			// must go first, so the second policy creates it anew.
			updatePlacementPolicy(secondPolicyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().SyncStrategy = nil
			})
		})

		It("should report the ConfigMap as not taken over for the second policy, leaving the first policy alone", func() {
			Eventually(secondPolicyFailedActual("NotTakenOver"), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the ConfigMap as not taken over for the second placement policy")
			Consistently(firstPolicyUndisturbedActual(), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The first placement policy or its ConfigMap changed")
		})

		It("can delete the first placement policy", func() {
			deletePlacementPolicyAndWait(firstPolicyKey)
		})

		It("should create the ConfigMap on member cluster 1 anew through the second policy", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(secondPolicyKey, wantSelectorsByCluster(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to synchronize the binding of the second placement policy")
			Eventually(configMapPlacedByPlacementPolicyActual(memberCluster1EastProd, cmKey, secondPolicyKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap through the second placement policy")
			Eventually(placementPolicyStatusActual(secondPolicyKey, allSynchronizedPlacementPolicyStatus(1)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")

			cm := &corev1.ConfigMap{}
			Expect(memberCluster1EastProd.KubeClient.Get(ctx, cmKey, cm)).To(Succeed(), "Failed to get ConfigMap %s on member cluster 1", cmKey)
			Expect(cm.UID).NotTo(Equal(firstCMUID), "The second placement policy took over the first policy's ConfigMap instead of creating it anew")
		})
	})
})
