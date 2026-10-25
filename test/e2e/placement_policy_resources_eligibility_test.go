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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

const (
	placementPolicyReservedNamespaceNameTemplate = "pp-reserved-ns-%d"
	clusterPlacementPolicyIneligibleNameTemplate = "cpp-ineligible-%d"
	placementPolicyDefaultNamespaceNameTemplate  = "pp-default-ns-%d"
	placementPolicyControllerOwnedNameTemplate   = "pp-controller-owned-%d"

	// placementPolicyDefaultNamespaceConfigMapNameTemplate names the ConfigMap that a policy in the default namespace
	// places; the namespace is shared by every process, hence the process number.
	placementPolicyDefaultNamespaceConfigMapNameTemplate = "pp-e2e-default-ns-%d"
)

var _ = Describe("placement policy resource eligibility", Label("placementpolicy"), func() {
	Context("placement policy in a namespace reserved for the system", Ordered, func() {
		// Namespaces prefixed with kube- or fleet- are reserved; kube-public exists in every cluster.
		policyKey := types.NamespacedName{
			Namespace: "kube-public",
			Name:      fmt.Sprintf(placementPolicyReservedNamespaceNameTemplate, GinkgoParallelProcess()),
		}

		BeforeAll(func() {
			By("creating a placement policy in kube-public")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{
					{APIVersion: "v1", Kind: "ConfigMap", Name: "cluster-info"},
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should report the selected resources as not collected, as the namespace is ineligible", func() {
			Eventually(placementPolicyStatusWithoutResourceRevisionActual(policyKey, resourcesNotCollectedPlacementPolicyStatus(int32(len(allMemberClusters)))),
				eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the expected placement policy status")
			Expect(placementPolicyConditionMessageActual(policyKey, kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected,
				"placement policy is in a namespace that is ineligible for placement")()).To(Succeed(), "The policy reports another cause")
		})

		It("should not bind any cluster", func() {
			Consistently(noPlacementPolicyBindingsActual(policyKey), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "A cluster was bound")
		})
	})

	Context("cluster placement policy naming resources that cannot be placed", Ordered, func() {
		policyKey := types.NamespacedName{
			Name: fmt.Sprintf(clusterPlacementPolicyIneligibleNameTemplate, GinkgoParallelProcess()),
		}

		notCollectedActual := func(wantText string) func() error {
			statusActual := placementPolicyStatusWithoutResourceRevisionActual(policyKey, resourcesNotCollectedPlacementPolicyStatus(int32(len(allMemberClusters))))
			messageActual := placementPolicyConditionMessageActual(policyKey, kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected, wantText)
			return func() error {
				if err := statusActual(); err != nil {
					return err
				}
				return messageActual()
			}
		}

		BeforeAll(func() {
			By("creating the work resources")
			// Kubernetes adds the kube-root-ca.crt ConfigMap to every namespace, the app namespace included.
			createWorkResources()

			By("creating a cluster placement policy that names the kube-root-ca.crt ConfigMap")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{
					{APIVersion: "v1", Kind: "ConfigMap", Name: "kube-root-ca.crt", Namespace: appNamespace().Name},
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should report the kube-root-ca.crt ConfigMap as ineligible", func() {
			// A resource named in a selector is the user's explicit pick, so an ineligible one fails the selection
			// rather than being skipped. Kubernetes publishes the ConfigMap shortly after the namespace is created;
			// until then, the policy reports it as missing instead.
			Eventually(notCollectedActual("the built-in CA certificate config map is created automatically"), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the ConfigMap as ineligible")
		})

		It("can switch to naming the default namespace", func() {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().ResourceSelectors = []kfplacementv1alpha1.ResourceSelector{
					{APIVersion: "v1", Kind: "Namespace", Name: "default"},
				}
			})
		})

		It("should report the default namespace as ineligible", func() {
			// Resources in the default namespace may be placed, but not the namespace itself.
			Eventually(notCollectedActual("the default namespace itself cannot be placed"), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the default namespace as ineligible")
		})

		It("should not bind any cluster", func() {
			Consistently(noPlacementPolicyBindingsActual(policyKey), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "A cluster was bound")
		})
	})

	Context("placement policy in the default namespace", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: metav1.NamespaceDefault,
			Name:      fmt.Sprintf(placementPolicyDefaultNamespaceNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{
			Namespace: metav1.NamespaceDefault,
			Name:      fmt.Sprintf(placementPolicyDefaultNamespaceConfigMapNameTemplate, GinkgoParallelProcess()),
		}
		cmData := map[string]string{"data": "in-default"}

		BeforeAll(func() {
			By("creating a ConfigMap in the default namespace")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: cmKey.Name, Namespace: cmKey.Namespace},
				Data:       cmData,
			}
			Expect(hubClient.Create(ctx, cm)).To(Succeed(), "Failed to create ConfigMap %s", cmKey)

			By("creating a placement policy in the default namespace that selects the ConfigMap")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{
					{APIVersion: "v1", Kind: "ConfigMap", Name: cmKey.Name},
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The hub ConfigMap must go even if removing the policy fails; the default namespace outlives the specs.
			defer func() {
				cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cmKey.Name, Namespace: cmKey.Namespace}}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, cm))).To(Succeed(), "Failed to delete ConfigMap %s", cmKey)
			}()
			deletePlacementPolicyAndWait(policyKey)
			Eventually(configMapAbsentOnClustersActual(cmKey, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the ConfigMap from the member clusters")
		})

		It("should place the ConfigMap on every member cluster", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind every member cluster")
			Eventually(configMapManagedOnClustersActual(cmKey, cmData, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on every member cluster")
		})

		It("should report the policy as synchronized on every member cluster", func() {
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("placement policy whose label selector also matches a controller-owned ConfigMap", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{
			Namespace: nsName,
			Name:      fmt.Sprintf(placementPolicyControllerOwnedNameTemplate, GinkgoParallelProcess()),
		}
		eligibleKey := types.NamespacedName{Namespace: nsName, Name: "pp-e2e-eligible"}
		ownedKey := types.NamespacedName{Namespace: nsName, Name: "pp-e2e-owned"}
		// The owner carries no label, so the policy does not select it.
		ownerKey := types.NamespacedName{Namespace: nsName, Name: "pp-e2e-owner"}
		cmData := map[string]string{"data": "labeled"}
		selectedLabels := map[string]string{placementPolicySelectedLabelKey: "true"}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a labeled ConfigMap, and another one that a ConfigMap controls")
			eligible := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: eligibleKey.Name, Namespace: eligibleKey.Namespace, Labels: selectedLabels},
				Data:       cmData,
			}
			Expect(hubClient.Create(ctx, eligible)).To(Succeed(), "Failed to create ConfigMap %s", eligibleKey)
			owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ownerKey.Name, Namespace: ownerKey.Namespace}}
			Expect(hubClient.Create(ctx, owner)).To(Succeed(), "Failed to create ConfigMap %s", ownerKey)
			owned := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ownedKey.Name,
					Namespace: ownedKey.Namespace,
					Labels:    selectedLabels,
					// The owner must exist (with this UID); otherwise the garbage collector removes the ConfigMap.
					OwnerReferences: []metav1.OwnerReference{
						{APIVersion: "v1", Kind: "ConfigMap", Name: owner.Name, UID: owner.UID, Controller: ptr.To(true)},
					},
				},
				Data: cmData,
			}
			Expect(hubClient.Create(ctx, owned)).To(Succeed(), "Failed to create ConfigMap %s", ownedKey)

			By("creating a placement policy that selects ConfigMaps by label")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{
					{APIVersion: "v1", Kind: "ConfigMap", LabelSelector: &metav1.LabelSelector{MatchLabels: selectedLabels}},
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should select the labeled ConfigMap only, skipping the controller-owned one", func() {
			// A label selector may match resources that the user did not pick explicitly, so an ineligible one is
			// skipped rather than failing the selection.
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind every member cluster with the eligible ConfigMap alone")
			Eventually(configMapManagedOnClustersActual(eligibleKey, cmData, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the eligible ConfigMap on every member cluster")
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("should not place the controller-owned ConfigMap, or its owner", func() {
			Consistently(configMapAbsentOnClustersActual(ownedKey, allMemberClusters...), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The controller-owned ConfigMap was placed")
			Expect(configMapAbsentOnClustersActual(ownerKey, allMemberClusters...)()).To(Succeed(), "The unlabeled owner ConfigMap was placed")
		})
	})
})
