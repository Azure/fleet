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
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

const (
	placementPolicyLabelSelectedNameTemplate    = "pp-label-selected-%d"
	placementPolicyPendingScaleUpNameTemplate   = "pp-pending-scale-up-%d"
	clusterPlacementPolicyNamespaceNameTemplate = "cpp-namespace-%d"
	placementPolicyOtherNamespaceNameTemplate   = "pp-other-namespace-%d"
	placementPolicyMissingResourceNameTemplate  = "pp-missing-resource-%d"

	// placementPolicyClusterRoleNameTemplate names the ClusterRole that the specs place; it differs from the names
	// that the CRP specs use, so the two never collide.
	placementPolicyClusterRoleNameTemplate = "pp-e2e-reader-%d"
	// placementPolicyMissingConfigMapName names a ConfigMap that a policy selects before it exists.
	placementPolicyMissingConfigMapName = "pp-e2e-missing-config"

	// placementPolicySelectedLabelKey is the label that the ConfigMaps selected by label carry.
	placementPolicySelectedLabelKey = "placement-policy-e2e/selected"

	placementPolicyResourceDeletedNameTemplate = "pp-resource-deleted-%d"
)

// newPlacementPolicySelectedConfigMap returns a ConfigMap that carries the label the specs select ConfigMaps by.
func newPlacementPolicySelectedConfigMap(key types.NamespacedName, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
			Labels:    map[string]string{placementPolicySelectedLabelKey: "true"},
		},
		Data: maps.Clone(data),
	}
}

// placementPolicyLabelSelectedResourceSelectors returns the resource selectors that select ConfigMaps by the label
// that newPlacementPolicySelectedConfigMap sets.
func placementPolicyLabelSelectedResourceSelectors() []kfplacementv1alpha1.ResourceSelector {
	return []kfplacementv1alpha1.ResourceSelector{
		{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{placementPolicySelectedLabelKey: "true"},
			},
		},
	}
}

// The FEP-0001 placement experience is alpha and runs behind the enable-placement-policy-apis hub and member
// agent flags, which test/e2e/setup.sh turns on.
var _ = Describe("placement policy resource selection", Label("placementpolicy"), func() {
	Context("placement policy selecting ConfigMaps by label, with resource changes pending a rollout", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{
			Namespace: nsName,
			Name:      fmt.Sprintf(placementPolicyLabelSelectedNameTemplate, GinkgoParallelProcess()),
		}
		cmAKey := types.NamespacedName{Namespace: nsName, Name: "selected-config-a"}
		cmBKey := types.NamespacedName{Namespace: nsName, Name: "selected-config-b"}
		cmCKey := types.NamespacedName{Namespace: nsName, Name: "selected-config-c"}
		// The app ConfigMap that createWorkResources creates carries no label, so the policy must leave it out.
		unselectedCMKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		v0Data := map[string]string{"data": "v0"}
		v1Data := map[string]string{"data": "v1"}

		var initialSnapshotName string

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating two ConfigMaps that carry the selected label")
			for _, key := range []types.NamespacedName{cmAKey, cmBKey} {
				Expect(hubClient.Create(ctx, newPlacementPolicySelectedConfigMap(key, v0Data))).To(Succeed(), "Failed to create ConfigMap %s", key)
			}

			By("creating a placement policy that selects ConfigMaps by label")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyLabelSelectedResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the labeled ConfigMaps, and only those, on every member cluster", func() {
			wantSelectorsByCluster := make(map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly, len(allMemberClusterNames))
			for _, clusterName := range allMemberClusterNames {
				wantSelectorsByCluster[clusterName] = []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}}
			}
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster, 2), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind every member cluster")
			for _, key := range []types.NamespacedName{cmAKey, cmBKey} {
				Eventually(configMapDataOnClustersActual(key, v0Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to place ConfigMap %s on every member cluster", key)
			}
			Expect(configMapAbsentOnClustersActual(unselectedCMKey, allMemberClusters...)()).To(Succeed(), "An unlabeled ConfigMap was placed")

			policy, err := retrievePlacementPolicy(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get placement policy %s", policyKey)
			snapshot, err := latestPrimarySnapshotOfPlacementPolicy(policy)
			Expect(err).NotTo(HaveOccurred(), "Failed to find the resource snapshot of placement policy %s", policyKey)
			initialSnapshotName = snapshot.GetName()
		})

		It("should report the policy as synchronized on every member cluster", func() {
			wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(initialSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("can change a selected ConfigMap and add another one on the hub", func() {
			updateConfigMapDataOnHub(cmAKey, v1Data)
			Expect(hubClient.Create(ctx, newPlacementPolicySelectedConfigMap(cmCKey, v1Data))).To(Succeed(), "Failed to create ConfigMap %s", cmCKey)
		})

		It("should report the resource changes as pending a rollout, without taking a new resource snapshot", func() {
			// The clusters still hold, synchronized and available, the revision that remains the latest one; only the
			// Synchronized condition tells that the hub has moved on since.
			wantStatus := resourcesChangedPendingRolloutPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(initialSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the resource changes as pending a rollout")
		})

		It("should keep the member clusters on the initial resource revision", func() {
			wantBindings := make(map[string]wantPlacementBinding, len(allMemberClusterNames))
			for _, clusterName := range allMemberClusterNames {
				wantBindings[clusterName] = wantPlacementBinding{
					selectors:    []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}},
					snapshotName: initialSnapshotName,
					status:       synchronizedPlacementBindingStatus(2),
				}
			}
			bindingsActual := placementPolicyBindingsActual(policyKey, wantBindings)
			changedCMActual := configMapDataOnClustersActual(cmAKey, v0Data, allMemberClusters...)
			addedCMActual := configMapAbsentOnClustersActual(cmCKey, allMemberClusters...)
			Consistently(func() error {
				policy, err := retrievePlacementPolicy(policyKey)
				if err != nil {
					return err
				}
				snapshot, err := latestPrimarySnapshotOfPlacementPolicy(policy)
				if err != nil {
					return err
				}
				if snapshot.GetName() != initialSnapshotName {
					return fmt.Errorf("a new resource snapshot %s was taken without a rollout", snapshot.GetName())
				}
				if err := bindingsActual(); err != nil {
					return err
				}
				if err := changedCMActual(); err != nil {
					return err
				}
				return addedCMActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The resource changes reached the member clusters without a rollout")
		})
	})

	Context("placement policy scaled up while resource changes are pending a rollout", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyPendingScaleUpNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}
		eastLabels := map[string]string{regionLabelName: regionEast}
		eastSelectors := []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{clusterSelectorTermsMatchingLabels(eastLabels)}

		// Which east cluster ranks first depends on live capacity, so the specs learn it rather than assume it.
		var firstClusterName, secondClusterName, initialSnapshotName string

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

		It("should place the initial version on one east cluster", func() {
			firstClusterName = waitForSinglePlacementPolicyBinding(policyKey).GetSpec().ClusterName
			secondClusterName = otherEastClusterName(firstClusterName)

			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				firstClusterName: eastSelectors,
			}, 1), workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to synchronize the binding")
			Eventually(configMapDataOnClustersActual(cmKey, v0Data, memberClusterNamed(firstClusterName)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the initial version on the bound cluster")

			policy, err := retrievePlacementPolicy(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get placement policy %s", policyKey)
			snapshot, err := latestPrimarySnapshotOfPlacementPolicy(policy)
			Expect(err).NotTo(HaveOccurred(), "Failed to find the resource snapshot of placement policy %s", policyKey)
			initialSnapshotName = snapshot.GetName()
		})

		It("can change the ConfigMap on the hub", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
		})

		It("should report the change as pending a rollout", func() {
			wantStatus := resourcesChangedPendingRolloutPlacementPolicyStatus(1)
			wantStatus.LatestResourceRevisionName = ptr.To(initialSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the change as pending a rollout")
		})

		It("can scale the selector up to 2 clusters", func() {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().ClusterSelectors[0].Count = ptr.To(intstr.FromInt32(2))
			})
		})

		It("should bind the other east cluster to the initial resource snapshot, not to the changed ConfigMap", func() {
			// A new binding gets the latest existing resource snapshot; only a rollout takes a new one.
			wantBindings := make(map[string]wantPlacementBinding, 2)
			for _, clusterName := range []string{firstClusterName, secondClusterName} {
				wantBindings[clusterName] = wantPlacementBinding{
					selectors:    eastSelectors,
					snapshotName: initialSnapshotName,
					status:       synchronizedPlacementBindingStatus(1),
				}
			}
			Eventually(placementPolicyBindingsActual(policyKey, wantBindings), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind the other east cluster to the initial resource snapshot")
			Eventually(configMapDataOnClustersActual(cmKey, v0Data, memberCluster1EastProd, memberCluster2EastCanary), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the initial version on both east clusters")

			wantStatus := resourcesChangedPendingRolloutPlacementPolicyStatus(2)
			wantStatus.LatestResourceRevisionName = ptr.To(initialSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the change as still pending a rollout")
		})
	})

	Context("cluster placement policy selecting a namespace, re-created to select a ConfigMap in it too", Ordered, func() {
		policyKey := types.NamespacedName{
			Name: fmt.Sprintf(clusterPlacementPolicyNamespaceNameTemplate, GinkgoParallelProcess()),
		}
		nsName := appNamespace().Name
		clusterRoleName := fmt.Sprintf(placementPolicyClusterRoleNameTemplate, GinkgoParallelProcess())

		namespaceAndClusterRoleSelectors := func() []kfplacementv1alpha1.ResourceSelector {
			return []kfplacementv1alpha1.ResourceSelector{
				{APIVersion: "v1", Kind: "Namespace", Name: nsName},
				{APIGroup: rbacv1.GroupName, APIVersion: "v1", Kind: "ClusterRole", Name: clusterRoleName},
			}
		}
		allClustersSelectors := func() map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
			wantSelectorsByCluster := make(map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly, len(allMemberClusterNames))
			for _, clusterName := range allMemberClusterNames {
				wantSelectorsByCluster[clusterName] = []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}}
			}
			return wantSelectorsByCluster
		}

		BeforeAll(func() {
			By("creating the work resources and a ClusterRole")
			createWorkResources()
			clusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName},
				Rules: []rbacv1.PolicyRule{
					{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get", "list"}},
				},
			}
			Expect(hubClient.Create(ctx, clusterRole)).To(Succeed(), "Failed to create ClusterRole %s", clusterRoleName)

			By("creating a cluster placement policy that selects the namespace and the ClusterRole by name")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: namespaceAndClusterRoleSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources and the ClusterRole must go even if removing the policy fails; otherwise they leak
			// into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			defer func() {
				clusterRole := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName}}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, clusterRole))).To(Succeed(), "Failed to delete ClusterRole %s", clusterRoleName)
			}()
			ensurePlacementPolicyRemoved(policyKey)
			Eventually(clusterRoleAbsentOnClustersActual(clusterRoleName, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the ClusterRole from the member clusters")
		})

		It("should place the namespace and the ClusterRole on every member cluster", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, allClustersSelectors(), 2), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind every member cluster")
			Eventually(namespacePlacedOnClustersActual(nsName, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the namespace on every member cluster")
			Eventually(clusterRolePlacedOnClustersActual(clusterRoleName, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ClusterRole on every member cluster")
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})

		It("should not place the ConfigMap in the namespace", func() {
			// Unlike a CRP, a cluster placement policy that selects a namespace places the namespace object alone.
			Consistently(appConfigMapRemovedFromClustersActual(allMemberClusters...), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The ConfigMap in the selected namespace was placed")
		})

		It("can delete the cluster placement policy", func() {
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should remove the namespace and the ClusterRole from every member cluster", func() {
			Eventually(namespaceAbsentOnClustersActual(nsName, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the namespace from the member clusters")
			Eventually(clusterRoleAbsentOnClustersActual(clusterRoleName, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the ClusterRole from the member clusters")
		})

		It("should leave no binding or resource snapshot of the deleted policy behind", func() {
			Eventually(placementPolicyLeftoversRemovedActual(policyKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the bindings and resource snapshots of the deleted policy")
		})

		It("can re-create the cluster placement policy, selecting the ConfigMap in the namespace too", func() {
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: append(namespaceAndClusterRoleSelectors(), kfplacementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       appConfigMap().Name,
					Namespace:  nsName,
				}),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to re-create cluster placement policy %s", policyKey)
		})

		It("should place the namespace, the ClusterRole, and the ConfigMap on every member cluster", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, allClustersSelectors(), 3), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind every member cluster")
			Eventually(namespacePlacedOnClustersActual(nsName, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the namespace on every member cluster")
			Eventually(clusterRolePlacedOnClustersActual(clusterRoleName, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ClusterRole on every member cluster")
			Eventually(appConfigMapPlacedOnClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on every member cluster")
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})
	})

	// The two contexts below hit the two kinds of selection errors that only the controller can catch: a selection the
	// user got wrong, and a selected resource that does not exist.
	Context("placement policy selecting a ConfigMap from another namespace", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{
			Namespace: nsName,
			Name:      fmt.Sprintf(placementPolicyOtherNamespaceNameTemplate, GinkgoParallelProcess()),
		}
		otherNamespace := nsName + "-other"

		notCollectedActual := func() func() error {
			statusActual := placementPolicyStatusWithoutResourceRevisionActual(policyKey, resourcesNotCollectedPlacementPolicyStatus(int32(len(allMemberClusters))))
			bindingsActual := noPlacementPolicyBindingsActual(policyKey)
			return func() error {
				if err := statusActual(); err != nil {
					return err
				}
				return bindingsActual()
			}
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy whose resource selector names another namespace")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{
					{APIVersion: "v1", Kind: "ConfigMap", Name: appConfigMap().Name, Namespace: otherNamespace},
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should report the resources as not collected, binding no cluster", func() {
			Eventually(notCollectedActual(), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the resources as not collected")
		})

		It("should not bind any cluster or place the ConfigMap", func() {
			placedActual := appConfigMapRemovedFromClustersActual(allMemberClusters...)
			Consistently(func() error {
				if err := notCollectedActual()(); err != nil {
					return err
				}
				return placedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The policy bound a cluster, or placed the ConfigMap")
		})

		It("can fix the resource selector to name the policy's own namespace", func() {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().ResourceSelectors[0].Namespace = nsName
			})
		})

		It("should place the ConfigMap on every member cluster", func() {
			wantSelectorsByCluster := make(map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly, len(allMemberClusterNames))
			for _, clusterName := range allMemberClusterNames {
				wantSelectorsByCluster[clusterName] = []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}}
			}
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster, 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind every member cluster")
			Eventually(appConfigMapPlacedOnClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on every member cluster")
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("placement policy selecting a ConfigMap that does not exist yet", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{
			Namespace: nsName,
			Name:      fmt.Sprintf(placementPolicyMissingResourceNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		missingCMKey := types.NamespacedName{Namespace: nsName, Name: placementPolicyMissingConfigMapName}
		missingCMData := map[string]string{"data": "created-later"}

		notCollectedActual := func() func() error {
			statusActual := placementPolicyStatusWithoutResourceRevisionActual(policyKey, resourcesNotCollectedPlacementPolicyStatus(int32(len(allMemberClusters))))
			bindingsActual := noPlacementPolicyBindingsActual(policyKey)
			return func() error {
				if err := statusActual(); err != nil {
					return err
				}
				return bindingsActual()
			}
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy that selects an existing ConfigMap and a missing one by name")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{
					{APIVersion: "v1", Kind: "ConfigMap", Name: cmKey.Name},
					{APIVersion: "v1", Kind: "ConfigMap", Name: missingCMKey.Name},
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources, the once-missing ConfigMap among them, must go even if removing the policy fails;
			// otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should report the resources as not collected, binding no cluster", func() {
			Eventually(notCollectedActual(), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the resources as not collected")
		})

		It("should not bind any cluster or place the existing ConfigMap", func() {
			placedActual := appConfigMapRemovedFromClustersActual(allMemberClusters...)
			Consistently(func() error {
				if err := notCollectedActual()(); err != nil {
					return err
				}
				return placedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The policy bound a cluster, or placed the existing ConfigMap")
		})

		It("can create the missing ConfigMap on the hub", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: missingCMKey.Name, Namespace: missingCMKey.Namespace},
				Data:       maps.Clone(missingCMData),
			}
			Expect(hubClient.Create(ctx, cm)).To(Succeed(), "Failed to create ConfigMap %s", missingCMKey)
		})

		It("should place both ConfigMaps on every member cluster", func() {
			// The resource watcher re-queues the policy as soon as the selected ConfigMap appears.
			wantSelectorsByCluster := make(map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly, len(allMemberClusterNames))
			for _, clusterName := range allMemberClusterNames {
				wantSelectorsByCluster[clusterName] = []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}}
			}
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster, 2), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind every member cluster")
			Eventually(appConfigMapPlacedOnClustersActual(allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the existing ConfigMap on every member cluster")
			Eventually(configMapDataOnClustersActual(missingCMKey, missingCMData, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the once-missing ConfigMap on every member cluster")
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("placement policy auto-rolling out the deletion of a ConfigMap it selects by label", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{
			Namespace: nsName,
			Name:      fmt.Sprintf(placementPolicyResourceDeletedNameTemplate, GinkgoParallelProcess()),
		}
		keptCMKey := types.NamespacedName{Namespace: nsName, Name: "selected-config-a"}
		deletedCMKey := types.NamespacedName{Namespace: nsName, Name: "selected-config-b"}
		v0Data := map[string]string{"data": "v0"}

		var initialSnapshotName, newSnapshotName string
		var keptCMUIDsByCluster map[string]types.UID

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating two ConfigMaps that carry the selected label")
			for _, key := range []types.NamespacedName{keptCMKey, deletedCMKey} {
				Expect(hubClient.Create(ctx, newPlacementPolicySelectedConfigMap(key, v0Data))).To(Succeed(), "Failed to create ConfigMap %s", key)
			}

			By("creating a placement policy that selects ConfigMaps by label, with auto-rollout enabled")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyLabelSelectedResourceSelectors(),
			})
			policy.SetAnnotations(autoRolloutAnnotations())
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place both ConfigMaps on every member cluster", func() {
			initialSnapshotName = waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 2)
			for _, key := range []types.NamespacedName{keptCMKey, deletedCMKey} {
				Eventually(configMapDataOnClustersActual(key, v0Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to place ConfigMap %s on every member cluster", key)
			}

			var err error
			keptCMUIDsByCluster, err = configMapUIDsByCluster(keptCMKey, allMemberClusters...)
			Expect(err).NotTo(HaveOccurred(), "Failed to read ConfigMap %s on the member clusters", keptCMKey)
		})

		It("can delete one of the ConfigMaps on the hub", func() {
			cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: deletedCMKey.Name, Namespace: deletedCMKey.Namespace}}
			Expect(hubClient.Delete(ctx, cm)).To(Succeed(), "Failed to delete ConfigMap %s on the hub cluster", deletedCMKey)
		})

		It("should roll the deletion out, removing the ConfigMap from every member cluster", func() {
			// The deletion is a resource change like any other: auto-rollout takes a resource snapshot without the
			// ConfigMap, and the work applier removes what the Works no longer carry.
			Eventually(placementPolicyLatestSnapshotChangedActual(policyKey, initialSnapshotName, &newSnapshotName), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to take a resource snapshot without the deleted ConfigMap")
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the new resource snapshot out to every binding")
			Eventually(configMapAbsentOnClustersActual(deletedCMKey, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the deleted ConfigMap from the member clusters")

			wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("should leave the other ConfigMap on every member cluster as it was", func() {
			dataActual := configMapDataOnClustersActual(keptCMKey, v0Data, allMemberClusters...)
			Consistently(func() error {
				if err := dataActual(); err != nil {
					return err
				}
				uids, err := configMapUIDsByCluster(keptCMKey, allMemberClusters...)
				if err != nil {
					return err
				}
				if diff := cmp.Diff(uids, keptCMUIDsByCluster); diff != "" {
					return fmt.Errorf("ConfigMap %s UIDs diff, keyed by cluster (-got, +want):\n%s", keptCMKey, diff)
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The other ConfigMap was changed or re-created")
		})
	})
})
