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

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

const (
	placementPolicyTakeOverIfNoDiffNameTemplate = "pp-take-over-if-no-diff-%d"
	placementPolicyOwnedByOthersNameTemplate    = "pp-owned-by-others-%d"
)

var _ = Describe("placement policy taking over existing resources", Label("placementpolicy"), func() {
	Context("placement policy taking over a ConfigMap only if it has no differences", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{
			Namespace: nsName,
			Name:      fmt.Sprintf(placementPolicyTakeOverIfNoDiffNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		hubValue := appConfigMap().Data["data"]
		// The member copy differs from the hub copy in the key the hub sets, and has a key of its own.
		preExistingData := map[string]string{"data": "pre-existing", "member-only": "kept"}
		matchingData := map[string]string{"data": hubValue, "member-only": "kept"}
		// The policy places the ConfigMap on cluster-1 alone.
		aliasLabels := map[string]string{placementv1beta1.ClusterAliasLabel: memberCluster1EastProdName}
		wantSelectorsByCluster := func() map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
			return map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				memberCluster1EastProdName: {clusterSelectorTermsMatchingLabels(aliasLabels)},
			}
		}

		dataDiff := kfplacementv1alpha1.PatchDetail{Path: "/data/data", ValueInMember: preExistingData["data"], ValueInHub: hubValue}
		memberOnlyDiff := kfplacementv1alpha1.PatchDetail{Path: "/data/member-only", ValueInMember: preExistingData["member-only"]}
		notTakenOverActual := func(diffs ...kfplacementv1alpha1.PatchDetail) func() error {
			// A ConfigMap carries no generation, so the generation observed on the member cluster is 0.
			diffDetails := &kfplacementv1alpha1.DiffDetails{ObservedInMemberClusterGeneration: ptr.To(int64(0)), ObservedDiffs: diffs}
			return placementPolicyBindingsActual(policyKey, map[string]wantPlacementBinding{
				memberCluster1EastProdName: {
					selectors: wantSelectorsByCluster()[memberCluster1EastProdName],
					status:    configMapNotAppliedPlacementBindingStatus(cmKey, "FailedToTakeOver", diffDetails),
				},
			})
		}
		setComparisonOption := func(option kfplacementv1alpha1.ComparisonOption) {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().SyncStrategy = &kfplacementv1alpha1.SyncStrategy{
					WhenAlreadyExists: kfplacementv1alpha1.WhenAlreadyExistsOptionTakeOverIfNoDiff,
					ComparisonOption:  option,
				}
			})
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating the same ConfigMap, with different data, directly on member cluster 1")
			preExisting := appConfigMap()
			preExisting.Data = maps.Clone(preExistingData)
			createConfigMapOnMemberCluster(memberCluster1EastProd, &preExisting)

			By("creating a placement policy that takes over existing resources only if they have no differences, in full")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorMatchingLabels(aliasLabels, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
				SyncStrategy: &kfplacementv1alpha1.SyncStrategy{
					WhenAlreadyExists: kfplacementv1alpha1.WhenAlreadyExistsOptionTakeOverIfNoDiff,
					ComparisonOption:  kfplacementv1alpha1.ComparisonOptionFullComparison,
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The pre-existing ConfigMap goes with the member namespace, which the cleanup removes even if deleting the
			// policy fails; it stays unmanaged unless the specs got as far as the takeover.
			defer cleanupWorkResourcesOnHubAndMembers()
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should report every difference under full comparison, the member-only key included", func() {
			Eventually(notTakenOverActual(dataDiff, memberOnlyDiff), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the differences on the binding")
			Eventually(placementPolicyStatusActual(policyKey, someClustersNotSynchronizedPlacementPolicyStatus(1, 0)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("should leave the pre-existing ConfigMap on member cluster 1 as it is", func() {
			Consistently(configMapOrphanedOnClustersActual(cmKey, preExistingData, memberCluster1EastProd), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The pre-existing ConfigMap was changed or taken over")
		})

		It("can switch the policy to partial comparison", func() {
			setComparisonOption(kfplacementv1alpha1.ComparisonOptionPartialComparison)
		})

		It("should report only the difference in the key that the hub sets", func() {
			// Partial comparison judges only the fields that the hub copy specifies.
			Eventually(notTakenOverActual(dataDiff), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the difference on the binding")
		})

		It("can make the key that the hub sets match on member cluster 1", func() {
			updateConfigMapDataOnMemberCluster(memberCluster1EastProd, cmKey, matchingData)
		})

		It("should take the ConfigMap over, keeping the member-only key", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the ConfigMap as synchronized")
			Eventually(configMapManagedOnClustersActual(cmKey, matchingData, memberCluster1EastProd), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to take the ConfigMap over on member cluster 1")
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(1)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("placement policy taking over a ConfigMap that another object owns", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{
			Namespace: nsName,
			Name:      fmt.Sprintf(placementPolicyOwnedByOthersNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		ownerKey := types.NamespacedName{Namespace: nsName, Name: "pp-e2e-member-owner"}
		hubData := appConfigMap().Data
		preExistingData := map[string]string{"data": "pre-existing"}
		// The policy places the ConfigMap on cluster-1 alone.
		aliasLabels := map[string]string{placementv1beta1.ClusterAliasLabel: memberCluster1EastProdName}
		wantSelectorsByCluster := func() map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
			return map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				memberCluster1EastProdName: {clusterSelectorTermsMatchingLabels(aliasLabels)},
			}
		}

		var ownerRef metav1.OwnerReference

		// ownershipActual checks the data of the ConfigMap on cluster-1 and that the other object still owns it, along
		// with an AppliedWork of the placement policy APIs or not.
		ownershipActual := func(wantData map[string]string, wantCoOwnedByKubeFleet bool) func() error {
			dataActual := configMapDataOnClustersActual(cmKey, wantData, memberCluster1EastProd)
			return func() error {
				if err := dataActual(); err != nil {
					return err
				}
				cm := &corev1.ConfigMap{}
				if err := memberCluster1EastProd.KubeClient.Get(ctx, cmKey, cm); err != nil {
					return err
				}
				var otherOwnerRefs []metav1.OwnerReference
				for _, ref := range cm.OwnerReferences {
					if ref.Kind != kfplacementv1alpha1.AppliedWorkKind {
						otherOwnerRefs = append(otherOwnerRefs, ref)
					}
				}
				if diff := cmp.Diff(otherOwnerRefs, []metav1.OwnerReference{ownerRef}); diff != "" {
					return fmt.Errorf("ConfigMap %s non-KubeFleet owner references diff (-got, +want):\n%s", cmKey, diff)
				}
				if got := ownedByPlacementPolicyAppliedWork(cm); got != wantCoOwnedByKubeFleet {
					return fmt.Errorf("ConfigMap %s owned by an AppliedWork = %t, want %t (owner references: %+v)", cmKey, got, wantCoOwnedByKubeFleet, cm.OwnerReferences)
				}
				return nil
			}
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating the same ConfigMap directly on member cluster 1, owned by another ConfigMap")
			owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ownerKey.Name, Namespace: ownerKey.Namespace}}
			createConfigMapOnMemberCluster(memberCluster1EastProd, owner)
			Expect(memberCluster1EastProd.KubeClient.Get(ctx, ownerKey, owner)).To(Succeed(), "Failed to get ConfigMap %s on member cluster 1", ownerKey)
			// The owner must exist (with this UID); otherwise the garbage collector removes the ConfigMap.
			ownerRef = metav1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: owner.Name, UID: owner.UID}
			preExisting := appConfigMap()
			preExisting.Data = maps.Clone(preExistingData)
			preExisting.OwnerReferences = []metav1.OwnerReference{ownerRef}
			createConfigMapOnMemberCluster(memberCluster1EastProd, &preExisting)

			By("creating a placement policy that always takes over existing resources, but not co-owned ones")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorMatchingLabels(aliasLabels, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
				SyncStrategy: &kfplacementv1alpha1.SyncStrategy{
					WhenAlreadyExists: kfplacementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The ConfigMaps on cluster-1 go with the member namespace, which the cleanup removes even if deleting the
			// policy fails; the other owner keeps the ConfigMap there after the policy is gone.
			defer cleanupWorkResourcesOnHubAndMembers()
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should fail to take the ConfigMap over, as another object owns it", func() {
			// The default whenOwnedByOthers=ReportError forbids co-ownership, even with whenAlreadyExists=AlwaysTakeOver.
			wantBindings := map[string]wantPlacementBinding{
				memberCluster1EastProdName: {
					selectors: wantSelectorsByCluster()[memberCluster1EastProdName],
					status:    configMapNotAppliedPlacementBindingStatus(cmKey, "FailedToTakeOver", nil),
				},
			}
			Eventually(placementPolicyBindingsActual(policyKey, wantBindings), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the ConfigMap as not taken over")
			Eventually(placementPolicyStatusActual(policyKey, someClustersNotSynchronizedPlacementPolicyStatus(1, 0)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("should leave the ConfigMap on member cluster 1 as it is, owned by the other object alone", func() {
			Consistently(ownershipActual(preExistingData, false), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The ConfigMap was changed or taken over")
		})

		It("can switch the policy to share ownership", func() {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().SyncStrategy = &kfplacementv1alpha1.SyncStrategy{
					WhenAlreadyExists: kfplacementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
					WhenOwnedByOthers: kfplacementv1alpha1.WhenOwnedByOthersOptionShareOwnership,
				}
			})
		})

		It("should take the ConfigMap over as a co-owner", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the ConfigMap as synchronized")
			Eventually(ownershipActual(hubData, true), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to take the ConfigMap over as a co-owner")
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(1)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("can delete the placement policy", func() {
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should leave the ConfigMap on member cluster 1, owned by the other object alone", func() {
			// The garbage collector deletes an object only once all of its owners are gone; with another owner left, it
			// drops the reference to the deleted AppliedWork instead.
			Eventually(ownershipActual(hubData, false), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to release the ConfigMap")
			Consistently(ownershipActual(hubData, false), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The ConfigMap was removed, or KubeFleet took it back")
		})
	})
})
