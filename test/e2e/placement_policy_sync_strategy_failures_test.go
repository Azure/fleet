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
	"encoding/json"
	"fmt"
	"maps"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

const (
	placementPolicyUnavailableNameTemplate     = "pp-unavailable-%d"
	placementPolicyNoNamespaceNameTemplate     = "pp-no-namespace-%d"
	placementPolicyServerSideApplyNameTemplate = "pp-server-side-apply-%d"
)

const (
	// placementPolicyOtherFieldManagerName is the field manager that the specs write member cluster objects as, in
	// place of another controller.
	placementPolicyOtherFieldManagerName = "pp-e2e-other-manager"
	// placementPolicyWorkApplierFieldManagerName is the field manager that the placement policy work applier applies
	// manifests as. It is spelled out rather than imported so that a rename shows up as a failed spec, the same way it
	// would show up for a user inspecting the managed fields.
	placementPolicyWorkApplierFieldManagerName = "kubefleet-member-agent"
)

// configMapDataKeyOwnersActual checks that the ConfigMap on the cluster has exactly the given data, and that its managed
// fields record exactly the given field managers as owners of its data.data field, the one key the app ConfigMap has.
func configMapDataKeyOwnersActual(cluster *framework.Cluster, key types.NamespacedName, wantData map[string]string, wantOwners ...string) func() error {
	dataActual := configMapDataOnClustersActual(key, wantData, cluster)
	return func() error {
		if err := dataActual(); err != nil {
			return err
		}
		cm := &corev1.ConfigMap{}
		if err := cluster.KubeClient.Get(ctx, key, cm); err != nil {
			return fmt.Errorf("cluster %s: failed to get ConfigMap %s: %w", cluster.ClusterName, key, err)
		}
		var gotOwners []string
		for _, entry := range cm.ManagedFields {
			if entry.FieldsV1 == nil {
				continue
			}
			fields := map[string]map[string]any{}
			if err := json.Unmarshal(entry.FieldsV1.Raw, &fields); err != nil {
				return fmt.Errorf("cluster %s: failed to parse the fields that %s manages on ConfigMap %s: %w", cluster.ClusterName, entry.Manager, key, err)
			}
			if _, found := fields["f:data"]["f:data"]; found {
				gotOwners = append(gotOwners, entry.Manager)
			}
		}
		if diff := cmp.Diff(gotOwners, wantOwners, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
			return fmt.Errorf("cluster %s: ConfigMap %s data.data field managers diff (-got, +want):\n%s", cluster.ClusterName, key, diff)
		}
		return nil
	}
}

var _ = Describe("placement policy sync strategy failures", Label("placementpolicy"), func() {
	Context("placement policy placing a Deployment whose image cannot be pulled", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{
			Namespace: nsName,
			Name:      fmt.Sprintf(placementPolicyUnavailableNameTemplate, GinkgoParallelProcess()),
		}
		deployKey := types.NamespacedName{Namespace: nsName, Name: appDeployment().Name}
		// The policy places the resources on cluster-1 alone.
		aliasLabels := map[string]string{placementv1beta1.ClusterAliasLabel: memberCluster1EastProdName}

		BeforeAll(func() {
			By("creating the work resources and a Deployment whose image cannot be pulled")
			createWorkResources()
			deploy := appDeployment()
			deploy.Spec.Template.Spec.Containers[0].Image = placementPolicyUnpullableImage
			Expect(hubClient.Create(ctx, &deploy)).To(Succeed(), "Failed to create Deployment %s", deployKey)

			By("creating a placement policy that places the ConfigMap and the Deployment on cluster-1")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorMatchingLabels(aliasLabels, intstr.FromInt32(1)),
				},
				ResourceSelectors: append(placementPolicyResourceSelectors(), kfplacementv1alpha1.ResourceSelector{
					APIGroup:   appsv1.GroupName,
					APIVersion: "v1",
					Kind:       "Deployment",
					Name:       deployKey.Name,
				}),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources, the Deployment among them, must go even if removing the policy fails; otherwise they
			// leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should apply both resources on member cluster 1", func() {
			Eventually(appConfigMapPlacedOnClustersActual(memberCluster1EastProd), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the ConfigMap on member cluster 1")
			Eventually(func() error {
				return memberCluster1EastProd.KubeClient.Get(ctx, deployKey, &appsv1.Deployment{})
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place the Deployment on member cluster 1")
		})

		It("should report the Deployment as applied but unavailable on the binding", func() {
			Eventually(func() error {
				// The condition copied from the work applier carries the generation of the Deployment on the member
				// cluster.
				deploy := &appsv1.Deployment{}
				if err := memberCluster1EastProd.KubeClient.Get(ctx, deployKey, deploy); err != nil {
					return err
				}
				unavailableCond := placementPolicyCondition(kfplacementv1alpha1.ManifestCondTypeAvailable, metav1.ConditionFalse, "Unavailable")
				unavailableCond.ObservedGeneration = deploy.Generation
				wantBindings := map[string]wantPlacementBinding{
					memberCluster1EastProdName: {
						selectors: []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{clusterSelectorTermsMatchingLabels(aliasLabels)},
						status: kfplacementv1alpha1.PlacementBindingStatus{
							Conditions: []metav1.Condition{
								placementPolicyCondition(kfplacementv1alpha1.PlacementBindingCondTypeSynchronized, metav1.ConditionTrue,
									kfplacementv1alpha1.PlacementBindingSynchronizedCondReasonAllResourcesSynchronized),
								placementPolicyCondition(kfplacementv1alpha1.PlacementBindingCondTypeAvailable, metav1.ConditionFalse,
									kfplacementv1alpha1.PlacementBindingAvailableCondReasonSomeResourcesUnavailable),
							},
							SelectedResources:     ptr.To(int32(2)),
							SynchronizedResources: ptr.To(int32(2)),
							AvailableResources:    ptr.To(int32(1)),
							FailedResources: []kfplacementv1alpha1.FailedResource{
								{
									ObjectRef: kfplacementv1alpha1.ObjectReference{
										Namespace:  deployKey.Namespace,
										Name:       deployKey.Name,
										APIGroup:   appsv1.GroupName,
										APIVersion: "v1",
										Kind:       "Deployment",
									},
									Conditions: []metav1.Condition{unavailableCond},
								},
							},
						},
					},
				}
				return placementPolicyBindingsActual(policyKey, wantBindings)()
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the Deployment as unavailable on the binding")
		})

		It("should report the policy as synchronized, but unavailable on member cluster 1", func() {
			wantStatus := allSynchronizedPlacementPolicyStatus(1)
			meta.SetStatusCondition(&wantStatus.Conditions, placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeAvailable,
				metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicyAvailableCondReasonSomeClustersUnavailable))
			wantStatus.ResourcesAvailableClusters = ptr.To(int32(0))
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("placement policy reporting a missing namespace instead of creating it", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{
			Namespace: nsName,
			Name:      fmt.Sprintf(placementPolicyNoNamespaceNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		// The policy places the ConfigMap on cluster-1 alone.
		aliasLabels := map[string]string{placementv1beta1.ClusterAliasLabel: memberCluster1EastProdName}
		wantSelectorsByCluster := func() map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
			return map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				memberCluster1EastProdName: {clusterSelectorTermsMatchingLabels(aliasLabels)},
			}
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("checking that the namespace is absent on cluster-1")
			// Each process reuses its namespace name, and an earlier spec of this process removes the namespace from
			// the member clusters as it cleans up.
			Eventually(namespaceAbsentOnClustersActual(nsName, memberCluster1EastProd), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "The namespace is still on member cluster 1")

			By("creating a placement policy that does not create missing namespaces")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorMatchingLabels(aliasLabels, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
				SyncStrategy: &kfplacementv1alpha1.SyncStrategy{
					WhenNamespaceDoesNotExist: kfplacementv1alpha1.WhenNamespaceDoesNotExistOptionReportError,
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should report the ConfigMap as not applied", func() {
			// The ConfigMap is not on the member cluster, so the condition copied from the work applier observes no
			// generation.
			wantBindings := map[string]wantPlacementBinding{
				memberCluster1EastProdName: {
					selectors: wantSelectorsByCluster()[memberCluster1EastProdName],
					status:    configMapNotAppliedPlacementBindingStatus(cmKey, "FailedToApply", nil),
				},
			}
			Eventually(placementPolicyBindingsActual(policyKey, wantBindings), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the ConfigMap as not applied")
			Eventually(placementPolicyStatusActual(policyKey, someClustersNotSynchronizedPlacementPolicyStatus(1, 0)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("should create neither the namespace nor the ConfigMap on member cluster 1", func() {
			namespaceActual := namespaceAbsentOnClustersActual(nsName, memberCluster1EastProd)
			configMapActual := configMapAbsentOnClustersActual(cmKey, memberCluster1EastProd)
			Consistently(func() error {
				if err := namespaceActual(); err != nil {
					return err
				}
				return configMapActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The namespace or the ConfigMap was created")
		})

		It("can create the namespace on member cluster 1", func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
			Expect(memberCluster1EastProd.KubeClient.Create(ctx, ns)).To(Succeed(), "Failed to create namespace %s on member cluster 1", nsName)
		})

		It("should apply the ConfigMap, leaving the namespace unmanaged", func() {
			// The work applier retries the failed apply on its backoff, which the E2E setup caps at 5s.
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the ConfigMap as applied")
			Eventually(configMapTakenOverOnClusterActual(memberCluster1EastProd, cmKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to apply the ConfigMap on member cluster 1")
			Expect(namespaceLeftUnmanagedOnClustersActual(nsName, memberCluster1EastProd)()).To(Succeed(), "The namespace is managed by KubeFleet")
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(1)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("placement policy server-side applying a field that another field manager owns", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{
			Namespace: nsName,
			Name:      fmt.Sprintf(placementPolicyServerSideApplyNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		otherManagerData := map[string]string{"data": "set-by-another-manager"}
		// The policy places the ConfigMap on cluster-1 alone.
		aliasLabels := map[string]string{placementv1beta1.ClusterAliasLabel: memberCluster1EastProdName}
		wantSelectorsByCluster := func() map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
			return map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				memberCluster1EastProdName: {clusterSelectorTermsMatchingLabels(aliasLabels)},
			}
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating the same ConfigMap, with different data, directly on member cluster 1, as another field manager")
			preExisting := appConfigMap()
			preExisting.Data = maps.Clone(otherManagerData)
			createConfigMapOnMemberCluster(memberCluster1EastProd, &preExisting, client.FieldOwner(placementPolicyOtherFieldManagerName))

			By("creating a placement policy that server-side applies its resources, without forcing conflicts")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorMatchingLabels(aliasLabels, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
				SyncStrategy: &kfplacementv1alpha1.SyncStrategy{
					ApplyMethod:       kfplacementv1alpha1.ApplyMethodServerSideApply,
					WhenAlreadyExists: kfplacementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
				},
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The ConfigMap on cluster-1 goes with the member namespace, which the cleanup removes even if the policy
			// never applied it.
			defer cleanupWorkResourcesOnHubAndMembers()
			deletePlacementPolicyAndWait(policyKey)
		})

		It("should fail to apply the ConfigMap, as the field it sets belongs to another field manager", func() {
			wantBindings := map[string]wantPlacementBinding{
				memberCluster1EastProdName: {
					selectors: wantSelectorsByCluster()[memberCluster1EastProdName],
					status:    configMapNotAppliedPlacementBindingStatus(cmKey, "FailedToApply", nil),
				},
			}
			Eventually(placementPolicyBindingsActual(policyKey, wantBindings), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the ConfigMap as not applied")
			Eventually(placementPolicyStatusActual(policyKey, someClustersNotSynchronizedPlacementPolicyStatus(1, 0)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("should leave the field on member cluster 1 as the other field manager set it", func() {
			Consistently(configMapDataKeyOwnersActual(memberCluster1EastProd, cmKey, otherManagerData, placementPolicyOtherFieldManagerName),
				consistentlyDuration, consistentlyInterval).Should(Succeed(), "The field was changed, or its owner")
		})

		It("can switch the policy to force conflicts", func() {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().SyncStrategy.ServerSideApplyOptions = &kfplacementv1alpha1.ServerSideApplyOptions{ForceConflicts: true}
			})
		})

		It("should apply the ConfigMap, taking the field over from the other field manager", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the ConfigMap as synchronized")
			Eventually(configMapDataKeyOwnersActual(memberCluster1EastProd, cmKey, appConfigMap().Data, placementPolicyWorkApplierFieldManagerName),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to take the field over")
			Eventually(configMapTakenOverOnClusterActual(memberCluster1EastProd, cmKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to take the ConfigMap over on member cluster 1")
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(1)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})
})
