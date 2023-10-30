/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// Note that this container will run in parallel with other containers.
var _ = Describe("Updating member cluster", func() {
	Context("Updating member cluster label", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		clusterLabelKey := "updating-member-key"
		clusterLabelValue := "updating-member-value"

		BeforeAll(func() {
			// Create the resources.
			createWorkResources()

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
					// Add a custom finalizer; this would allow us to better observe
					// the behavior of the controllers.
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickNPlacementType,
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: metav1.LabelSelector{
												MatchLabels: map[string]string{
													clusterLabelKey: clusterLabelValue,
												},
											},
										},
									},
								},
							},
						},
						NumberOfClusters: pointer.Int32(1),
					},
					Strategy: placementv1beta1.RolloutStrategy{
						Type: placementv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &placementv1beta1.RollingUpdateConfig{
							UnavailablePeriodSeconds: pointer.Int(2),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		It("should not place resources on any cluster", checkIfRemovedWorkResourcesFromAllMemberClusters)

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), nil, []string{memberCluster1Name})
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("update the member cluster label", func() {
			Eventually(func() error {
				mcObj := &clusterv1beta1.MemberCluster{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: memberCluster1Name}, mcObj); err != nil {
					return err
				}
				mcObj.Labels[clusterLabelKey] = clusterLabelValue
				return hubClient.Update(ctx, mcObj)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the member cluster")
		})

		It("should place resources on matching clusters", func() {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(memberCluster1)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on matching clusters")
		})

		It("should update CRP status as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), []string{memberCluster1Name}, nil)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			Eventually(func() error {
				mcObj := &clusterv1beta1.MemberCluster{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: memberCluster1Name}, mcObj); err != nil {
					return err
				}
				delete(mcObj.Labels, clusterLabelKey)
				return hubClient.Update(ctx, mcObj)
			}, eventuallyDuration, eventuallyInterval, "Failed to update the member cluster")

			ensureCRPAndRelatedResourcesDeletion(crpName, nil)
		})
	})
})
