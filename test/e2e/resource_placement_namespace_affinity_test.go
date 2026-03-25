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

package e2e

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

// The test suites below cover the namespace affinity scheduler plugin behavior.
// The plugin ensures that an RP is only scheduled onto clusters where the target
// namespace already exists (i.e. the namespace was placed there by a CRP).

// Test scenario (PickAll):
//   - A CRP with PickFixed policy places the namespace onto 2 out of 3 member clusters.
//   - An RP with PickAll policy selects a ConfigMap in that namespace.
//   - The namespace affinity plugin should restrict the RP to only the 2 clusters
//     that actually have the namespace; the RP must still be considered successful.
var _ = Describe("placing namespaced scoped resources using an RP with PickAll policy and namespace affinity", Label("resourceplacement", "namespaceaffinity"), func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
	rpKey := types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}

	BeforeEach(OncePerOrdered, func() {
		// Create the work resources (namespace + configmap) on the hub cluster.
		createWorkResources()

		// Create a CRP with PickFixed policy that places the namespace on only 2 of the 3
		// member clusters. The namespace affinity plugin will later use the namespace
		// presence information propagated from these 2 clusters to filter RP scheduling.
		createNamespaceOnlyCRPForTwoClusters(crpName)

		// Wait until the CRP has placed the namespace on exactly the 2 fixed clusters.
		// With PickFixed, non-targeted clusters are not listed as unselected — pass nil.
		crpStatusUpdatedActual := crpStatusUpdatedActual(
			workNamespaceIdentifiers(),
			[]string{memberCluster1EastProdName, memberCluster2EastCanaryName},
			nil,
			"0",
		)
		Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterEach(OncePerOrdered, func() {
		ensureRPAndRelatedResourcesDeleted(rpKey, allMemberClusters)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("namespace affinity restricts RP placement to clusters with the namespace", Ordered, func() {
		It("should create RP with PickAll policy successfully", func() {
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  appNamespace().Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status showing placement only on clusters with the namespace", func() {
			// The RP uses PickAll. Namespace affinity silently filters out cluster-3 (no namespace there),
			// so the scheduler considers the policy fulfilled — only cluster-1 and cluster-2 appear as selected.
			// cluster-3 does not appear as an unselected entry in RP status.
			// Use a longer timeout since namespace collection status must propagate before scheduling.
			rpStatusUpdatedActual := rpStatusUpdatedActual(
				appConfigMapIdentifiers(),
				[]string{memberCluster1EastProdName, memberCluster2EastCanaryName},
				nil,
				"0",
			)
			Eventually(rpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place the ConfigMap on the clusters that have the namespace", func() {
			for _, cluster := range []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary} {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on cluster %s", cluster.ClusterName)
			}
		})

		It("should not place the ConfigMap on the cluster without the namespace", func() {
			checkIfRemovedConfigMapFromMemberClusters([]*framework.Cluster{memberCluster3WestProd})
		})
	})
})

// Test scenario (PickN):
//   - A CRP with PickFixed policy places the namespace onto 2 out of 3 member clusters.
//   - An RP with PickN=2 policy selects a ConfigMap in that namespace.
//   - The namespace affinity plugin restricts eligible clusters to the 2 that have the namespace,
//     and PickN=2 is exactly fulfilled — the RP is considered successful.
var _ = Describe("placing namespaced scoped resources using an RP with PickN policy and namespace affinity", Label("resourceplacement", "namespaceaffinity"), func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
	rpKey := types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}

	BeforeEach(OncePerOrdered, func() {
		// Create the work resources (namespace + configmap) on the hub cluster.
		createWorkResources()

		// Create a CRP with PickFixed policy that places the namespace on only 2 of the 3
		// member clusters. The namespace affinity plugin will later use the namespace
		// presence information propagated from these 2 clusters to filter RP scheduling.
		createNamespaceOnlyCRPForTwoClusters(crpName)

		// Wait until the CRP has placed the namespace on exactly the 2 fixed clusters.
		// With PickFixed, non-targeted clusters are not listed as unselected — pass nil.
		crpStatusUpdatedActual := crpStatusUpdatedActual(
			workNamespaceIdentifiers(),
			[]string{memberCluster1EastProdName, memberCluster2EastCanaryName},
			nil,
			"0",
		)
		Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterEach(OncePerOrdered, func() {
		ensureRPAndRelatedResourcesDeleted(rpKey, allMemberClusters)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("PickN=2 is exactly fulfilled by the clusters that have the namespace", Ordered, func() {
		It("should create RP with PickN=2 policy successfully", func() {
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  appNamespace().Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(2)),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status showing placement only on the 2 clusters with the namespace", func() {
			// Namespace affinity restricts eligible clusters to only cluster-1 and cluster-2.
			// PickN=2 is exactly fulfilled — Scheduled=True, no unselected entries.
			rpStatusUpdatedActual := rpStatusUpdatedActual(
				appConfigMapIdentifiers(),
				[]string{memberCluster1EastProdName, memberCluster2EastCanaryName},
				nil,
				"0",
			)
			Eventually(rpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place the ConfigMap on the 2 clusters that have the namespace", func() {
			for _, cluster := range []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary} {
				resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
				Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on cluster %s", cluster.ClusterName)
			}
		})

		It("should not place the ConfigMap on the cluster without the namespace", func() {
			checkIfRemovedConfigMapFromMemberClusters([]*framework.Cluster{memberCluster3WestProd})
		})
	})
})

// Test scenario (PickN edge case):
//   - A CRP with PickFixed policy places the namespace onto only 1 out of 3 member clusters.
//   - An RP with PickN=2 policy selects a ConfigMap in that namespace.
//   - The namespace affinity plugin restricts eligible clusters to only the 1 that has the namespace.
//   - PickN=2 cannot be fulfilled with only 1 eligible cluster, so Scheduled=False but resources are still placed on the eligible cluster.
var _ = Describe("placing namespaced scoped resources using an RP with PickN=2 but namespace only on 1 cluster", Label("resourceplacement", "namespaceaffinity"), func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
	rpKey := types.NamespacedName{Name: rpName, Namespace: appNamespace().Name}

	BeforeEach(OncePerOrdered, func() {
		// Create the work resources (namespace + configmap) on the hub cluster.
		createWorkResources()

		// Create a CRP with PickFixed policy that places the namespace on only 1 of the 3
		// member clusters. This creates a scenario where PickN=2 cannot be satisfied.
		createNamespaceOnlyCRPForOneCluster(crpName)

		// Wait until the CRP has placed the namespace on exactly the 1 fixed cluster.
		crpStatusUpdatedActual := crpStatusUpdatedActual(
			workNamespaceIdentifiers(),
			[]string{memberCluster1EastProdName},
			nil,
			"0",
		)
		Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
	})

	AfterEach(OncePerOrdered, func() {
		ensureRPAndRelatedResourcesDeleted(rpKey, allMemberClusters)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	Context("PickN=2 cannot be fulfilled by only 1 eligible cluster", Ordered, func() {
		It("should create RP with PickN=2 policy successfully", func() {
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  appNamespace().Name,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: configMapSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(2)),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: ptr.To(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, rp)).To(Succeed(), "Failed to create RP")
		})

		It("should update RP status showing Scheduled=False due to insufficient clusters", func() {
			// Namespace affinity restricts eligible clusters to only cluster-1.
			// PickN=2 cannot be fulfilled with only 1 eligible cluster — Scheduled=False.
			// The scheduler places on the 1 available cluster but reports the policy as unfulfilled.

			rpStatusUpdatedActual := func() error {
				placement, err := retrievePlacement(rpKey)
				if err != nil {
					return fmt.Errorf("failed to get placement %s: %w", rpKey, err)
				}

				// Build expected status to match actual scheduler behavior:
				// - One selected cluster (kind-cluster-1)
				// - One unselected entry (for PickN unfulfillment)
				// - Overall Scheduled=False because PickN=2 cannot be satisfied
				wantStatus := &placementv1beta1.PlacementStatus{
					SelectedResources:     appConfigMapIdentifiers(),
					ObservedResourceIndex: "0",
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							Conditions: []metav1.Condition{
								{
									Type:               string(placementv1beta1.PerClusterAppliedConditionType),
									Status:             metav1.ConditionTrue,
									ObservedGeneration: placement.GetGeneration(),
									Reason:             "AllWorkHaveBeenApplied",
								},
								{
									Type:               string(placementv1beta1.PerClusterAvailableConditionType),
									Status:             metav1.ConditionTrue,
									ObservedGeneration: placement.GetGeneration(),
									Reason:             "AllWorkAreAvailable",
								},
								{
									Type:               string(placementv1beta1.PerClusterOverriddenConditionType),
									Status:             metav1.ConditionTrue,
									ObservedGeneration: placement.GetGeneration(),
									Reason:             "NoOverrideSpecified",
								},
								{
									Type:               string(placementv1beta1.PerClusterRolloutStartedConditionType),
									Status:             metav1.ConditionTrue,
									ObservedGeneration: placement.GetGeneration(),
									Reason:             "RolloutStarted",
								},
								{
									Type:               string(placementv1beta1.PerClusterScheduledConditionType),
									Status:             metav1.ConditionTrue,
									ObservedGeneration: placement.GetGeneration(),
									Reason:             "Scheduled",
								},
								{
									Type:               string(placementv1beta1.PerClusterWorkSynchronizedConditionType),
									Status:             metav1.ConditionTrue,
									ObservedGeneration: placement.GetGeneration(),
									Reason:             "AllWorkSynced",
								},
							},
						},
						{
							// Unselected entry for PickN policy unfulfillment
							Conditions: []metav1.Condition{
								{
									Type:               string(placementv1beta1.PerClusterScheduledConditionType),
									Status:             metav1.ConditionFalse,
									ObservedGeneration: placement.GetGeneration(),
									Reason:             "ScheduleFailed",
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourcePlacementAppliedConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: placement.GetGeneration(),
							Reason:             "ApplySucceeded",
						},
						{
							Type:               string(placementv1beta1.ResourcePlacementAvailableConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: placement.GetGeneration(),
							Reason:             "ResourceAvailable",
						},
						{
							Type:               string(placementv1beta1.ResourcePlacementOverriddenConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: placement.GetGeneration(),
							Reason:             "NoOverrideSpecified",
						},
						{
							Type:               string(placementv1beta1.ResourcePlacementRolloutStartedConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: placement.GetGeneration(),
							Reason:             "RolloutStarted",
						},
						{
							Type:               string(placementv1beta1.ResourcePlacementScheduledConditionType),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: placement.GetGeneration(),
							Reason:             "SchedulingPolicyUnfulfilled",
						},
						{
							Type:               string(placementv1beta1.ResourcePlacementWorkSynchronizedConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: placement.GetGeneration(),
							Reason:             "WorkSynchronized",
						},
					},
				}

				if diff := cmp.Diff(placement.GetPlacementStatus(), wantStatus, placementStatusCmpOptionsOnCreate...); diff != "" {
					return fmt.Errorf("Placement status diff (-got, +want): %s for placement %v", diff, rpKey)
				}
				return nil
			}
			Eventually(rpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place the ConfigMap on the only 1 cluster that has the namespace", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1EastProd)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on cluster %s", memberCluster1EastProd.ClusterName)
		})

		It("should not place the ConfigMap on the clusters without the namespace", func() {
			checkIfRemovedConfigMapFromMemberClusters([]*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd})
		})
	})
})
