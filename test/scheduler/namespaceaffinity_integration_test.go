/*
Copyright 2025 The KubeFleet Authors.

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

package tests

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	"go.goms.io/fleet/pkg/propertyprovider"
)

var _ = Describe("scheduling ResourcePlacements with namespace affinity", func() {
	Context("PickAll policy, namespace exists on some clusters", Serial, Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		testNamespace := "test-namespace-affinity"
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, rpName, 0)

		// Clusters with namespace collection enabled and namespace exists
		clustersWithNamespace := []string{memberCluster1EastProd, memberCluster2EastProd}
		// Clusters with namespace collection enabled but namespace does NOT exist
		clustersWithoutNamespace := []string{memberCluster4CentralProd, memberCluster5CentralProd}
		// Clusters without namespace collection enabled (should be included - backward compatibility)
		clustersNoCollection := []string{memberCluster3EastCanary, memberCluster6WestProd, memberCluster7WestCanary}

		BeforeAll(func() {
			// Create the test namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
				},
			}
			Expect(hubClient.Create(ctx, ns)).Should(Succeed(), "Failed to create test namespace")

			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(types.NamespacedName{Name: rpName, Namespace: testNamespace})
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Set up namespace collection status on clusters
			// Clusters 1 and 2: namespace collection enabled, namespace exists
			for _, clusterName := range clustersWithNamespace {
				setNamespaceCollectionOnCluster(clusterName, true, map[string]string{
					testNamespace: "work-1",
				})
			}

			// Clusters 4 and 5: namespace collection enabled, namespace does NOT exist
			for _, clusterName := range clustersWithoutNamespace {
				setNamespaceCollectionOnCluster(clusterName, true, map[string]string{
					"other-namespace": "work-2",
				})
			}

			// Clusters 3, 6, and 7: namespace collection NOT enabled (backward compatibility test)
			for _, clusterName := range clustersNoCollection {
				setNamespaceCollectionOnCluster(clusterName, false, nil)
			}

			// Create the ResourcePlacement and its associated policy snapshot.
			createPickAllRPWithPolicySnapshot(testNamespace, rpName, policySnapshotName, nil)
		})

		It("should add scheduler cleanup finalizer to the RP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(types.NamespacedName{Name: rpName, Namespace: testNamespace})
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to RP")
		})

		It("should create bindings only for clusters with the namespace (or no collection enabled)", func() {
			// Should schedule to: clusters 1, 2 (have namespace), and 6 (no collection enabled)
			expectedClusters := append(clustersWithNamespace, clustersNoCollection...)
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(
				expectedClusters,
				zeroScoreByCluster,
				types.NamespacedName{Name: rpName, Namespace: testNamespace},
				policySnapshotName,
			)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create bindings for clusters without the namespace", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(clustersWithoutNamespace, types.NamespacedName{Name: rpName, Namespace: testNamespace})
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Bindings created for clusters without namespace")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Bindings created for clusters without namespace")
		})

		It("should report status correctly", func() {
			expectedClusters := append(clustersWithNamespace, clustersNoCollection...)
			filteredClusters := append(clustersWithoutNamespace, memberCluster8UnhealthyEastProd, memberCluster9LeftCentralProd)
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(
				expectedClusters,
				filteredClusters,
				types.NamespacedName{Name: policySnapshotName, Namespace: testNamespace},
			)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report correct policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report correct policy snapshot status")
		})

		AfterAll(func() {
			// Clean up namespace collection status
			for _, clusterName := range append(clustersWithNamespace, append(clustersWithoutNamespace, clustersNoCollection...)...) {
				clearNamespaceCollectionOnCluster(clusterName)
			}

			// Delete the ResourcePlacement.
			ensurePlacementAndAllRelatedResourcesDeletion(types.NamespacedName{Name: rpName, Namespace: testNamespace})

			// Delete the test namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
				},
			}
			_ = hubClient.Delete(ctx, ns)
		})
	})

	Context("PickAll policy, namespace added after scheduling", Serial, Ordered, func() {
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
		testNamespace := "test-namespace-dynamic"
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, rpName, 0)

		targetCluster := memberCluster3EastCanary

		BeforeAll(func() {
			// Create the test namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
				},
			}
			Expect(hubClient.Create(ctx, ns)).Should(Succeed(), "Failed to create test namespace")

			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(types.NamespacedName{Name: rpName, Namespace: testNamespace})
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Initially, cluster does NOT have the namespace
			setNamespaceCollectionOnCluster(targetCluster, true, map[string]string{
				"other-namespace": "work-1",
			})

			// Create the ResourcePlacement and its associated policy snapshot.
			createPickAllRPWithPolicySnapshot(testNamespace, rpName, policySnapshotName, nil)
		})

		It("should add scheduler cleanup finalizer to the RP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(types.NamespacedName{Name: rpName, Namespace: testNamespace})
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to RP")
		})

		It("should not create binding initially when namespace is missing", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual([]string{targetCluster}, types.NamespacedName{Name: rpName, Namespace: testNamespace})
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Binding created despite missing namespace")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Binding created despite missing namespace")
		})

		It("can add the namespace to the cluster", func() {
			// Update cluster to have the namespace
			setNamespaceCollectionOnCluster(targetCluster, true, map[string]string{
				testNamespace:     "work-new",
				"other-namespace": "work-1",
			})
		})

		It("should create binding after namespace is added", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(
				[]string{targetCluster},
				zeroScoreByCluster,
				types.NamespacedName{Name: rpName, Namespace: testNamespace},
				policySnapshotName,
			)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create binding after namespace added")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create binding after namespace added")
		})

		AfterAll(func() {
			// Clean up namespace collection status
			clearNamespaceCollectionOnCluster(targetCluster)

			// Delete the ResourcePlacement.
			ensurePlacementAndAllRelatedResourcesDeletion(types.NamespacedName{Name: rpName, Namespace: testNamespace})

			// Delete the test namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
				},
			}
			_ = hubClient.Delete(ctx, ns)
		})
	})

	Context("ClusterResourcePlacement should not be affected by namespace affinity", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)

		// All healthy clusters should be selected regardless of namespace collection
		expectedClusters := healthyClusters

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForPlacementActual(types.NamespacedName{Name: crpName})
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Set namespace collection on some clusters - should NOT affect CRP
			setNamespaceCollectionOnCluster(memberCluster1EastProd, true, map[string]string{})
			setNamespaceCollectionOnCluster(memberCluster2EastProd, true, map[string]string{
				"some-namespace": "work-1",
			})

			// Create the CRP (cluster-scoped) and its associated policy snapshot.
			createNilSchedulingPolicyCRPWithPolicySnapshot(crpName, policySnapshotName, nil)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := placementSchedulerFinalizerAddedActual(types.NamespacedName{Name: crpName})
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create bindings for all healthy clusters", func() {
			// CRP should schedule to all healthy clusters, namespace affinity should be skipped
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(
				expectedClusters,
				zeroScoreByCluster,
				types.NamespacedName{Name: crpName},
				policySnapshotName,
			)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		AfterAll(func() {
			// Clean up namespace collection status
			clearNamespaceCollectionOnCluster(memberCluster1EastProd)
			clearNamespaceCollectionOnCluster(memberCluster2EastProd)

			// Delete the CRP.
			ensurePlacementAndAllRelatedResourcesDeletion(types.NamespacedName{Name: crpName})
		})
	})
})

// setNamespaceCollectionOnCluster sets the namespace collection status on a member cluster.
func setNamespaceCollectionOnCluster(clusterName string, enabled bool, namespaces map[string]string) {
	Eventually(func() error {
		mc := &clusterv1beta1.MemberCluster{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: clusterName}, mc); err != nil {
			return err
		}

		// Update namespaces map
		mc.Status.Namespaces = namespaces

		// Update condition based on whether namespace collection is enabled
		if enabled {
			// Add/update the NamespaceCollectionSucceeded condition to True
			meta.SetStatusCondition(&mc.Status.Conditions, metav1.Condition{
				Type:    propertyprovider.NamespaceCollectionSucceededCondType,
				Status:  metav1.ConditionTrue,
				Reason:  propertyprovider.NamespaceCollectionSucceededReason,
				Message: propertyprovider.NamespaceCollectionSucceededMsg,
			})
		} else {
			// Remove the condition entirely (namespace collection not enabled)
			// This is different from ConditionFalse which means degraded/limit reached
			meta.RemoveStatusCondition(&mc.Status.Conditions, propertyprovider.NamespaceCollectionSucceededCondType)
		}

		return hubClient.Status().Update(ctx, mc)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to set namespace collection on cluster %s", clusterName)
}

// clearNamespaceCollectionOnCluster removes namespace collection status from a member cluster.
func clearNamespaceCollectionOnCluster(clusterName string) {
	Eventually(func() error {
		mc := &clusterv1beta1.MemberCluster{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: clusterName}, mc); err != nil {
			return err
		}

		// Clear namespaces map
		mc.Status.Namespaces = nil

		// Remove the NamespaceCollectionSucceeded condition
		meta.RemoveStatusCondition(&mc.Status.Conditions, propertyprovider.NamespaceCollectionSucceededCondType)

		return hubClient.Status().Update(ctx, mc)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to clear namespace collection on cluster %s", clusterName)
}
