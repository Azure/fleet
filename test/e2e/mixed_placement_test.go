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

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workapplier"
)

var _ = Describe("mixed ClusterResourcePlacement and ResourcePlacement negative test cases", Label("resourceplacement"), func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())
	workNamespaceName := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	Context("conflicting placement decisions", Ordered, func() {
		BeforeAll(func() {
			By("creating work resources")
			createWorkResources()

			By("creating a CRP that selects the namespace")
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       crpName,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:          "",
							Kind:           "Namespace",
							Version:        "v1",
							Name:           workNamespaceName,
							SelectionScope: placementv1beta1.NamespaceOnly,
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{memberCluster2EastCanaryName, memberCluster3WestProdName},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)

			By("should update CRP status as expected")
			crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), []string{memberCluster2EastCanaryName, memberCluster3WestProdName}, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")

			By("creating an RP that tries to place resources in the same namespace")
			createRP(workNamespaceName, rpName)
		})

		AfterAll(func() {
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, allMemberClusters)
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, rp); err != nil {
					return err
				}

				appConfigMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
				wantStatus := placementv1beta1.PlacementStatus{
					Conditions: rpAppliedFailedConditions(rp.Generation),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							FailedPlacements: []placementv1beta1.FailedResourcePlacement{
								{
									ResourceIdentifier: placementv1beta1.ResourceIdentifier{
										Kind:      "ConfigMap",
										Name:      appConfigMapName,
										Version:   "v1",
										Namespace: workNamespaceName,
									},
									Condition: metav1.Condition{
										Type:               placementv1beta1.WorkConditionTypeApplied,
										Status:             metav1.ConditionFalse,
										Reason:             string(workapplier.ApplyOrReportDiffResTypeFailedToApply),
										ObservedGeneration: 0,
									},
								},
							},
							Conditions: perClusterApplyFailedConditions(rp.Generation),
						},
						{
							ClusterName:           memberCluster2EastCanaryName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterRolloutCompletedConditions(rp.Generation, true, false),
						},
						{
							ClusterName:           memberCluster3WestProdName,
							ObservedResourceIndex: "0",
							Conditions:            perClusterRolloutCompletedConditions(rp.Generation, true, false),
						},
					},
					SelectedResources:     appConfigMapIdentifiers(),
					ObservedResourceIndex: "0",
				}
				if diff := cmp.Diff(rp.Status, wantStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("RP status diff (-got, +want): %s", diff)
				}
				return nil
			}
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		It("should place resources on the specified clusters", func() {
			checkIfPlacedWorkResourcesOnMemberClusters(allMemberClusters[1:])
		})

		It("update CRP to select all the clusters", func() {
			updateFunc := func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				crp.Spec.Policy.ClusterNames = allMemberClusterNames
				// may hit 409
				return hubClient.Update(ctx, crp)
			}
			Eventually(updateFunc, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the crp %s", crpName)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP status as expected")
		})

		It("should place the resources on all member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
	})

	Context("RP cannot co-own the resources with CRP", Ordered, func() {
		BeforeAll(func() {
			By("creating work resources")
			createWorkResources()

			By("creating a CRP that uses PickAll strategy")
			createCRP(crpName)

			By("should update CRP status as expected")
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})

		It("creating an RP that uses PickAll strategy on the same namespace", func() {
			createRP(workNamespaceName, rpName)
		})

		It("should update RP status as expected", func() {
			rpStatusUpdatedActual := func() error {
				rp := &placementv1beta1.ResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, rp); err != nil {
					return err
				}

				appConfigMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
				failedPlacements := []placementv1beta1.FailedResourcePlacement{
					{
						ResourceIdentifier: placementv1beta1.ResourceIdentifier{
							Kind:      "ConfigMap",
							Name:      appConfigMapName,
							Version:   "v1",
							Namespace: workNamespaceName,
						},
						Condition: metav1.Condition{
							Type:               placementv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(workapplier.ApplyOrReportDiffResTypeFailedToTakeOver),
							ObservedGeneration: 0,
						},
					},
				}
				wantStatus := placementv1beta1.PlacementStatus{
					Conditions: rpAppliedFailedConditions(rp.Generation),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							FailedPlacements:      failedPlacements,
							Conditions:            perClusterApplyFailedConditions(rp.Generation),
						},
						{
							ClusterName:           memberCluster2EastCanaryName,
							ObservedResourceIndex: "0",
							FailedPlacements:      failedPlacements,
							Conditions:            perClusterApplyFailedConditions(rp.Generation),
						},
						{
							ClusterName:           memberCluster3WestProdName,
							ObservedResourceIndex: "0",
							FailedPlacements:      failedPlacements,
							Conditions:            perClusterApplyFailedConditions(rp.Generation),
						},
					},
					SelectedResources:     appConfigMapIdentifiers(),
					ObservedResourceIndex: "0",
				}
				if diff := cmp.Diff(rp.Status, wantStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("RP status diff (-got, +want): %s", diff)
				}
				return nil
			}
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})

		// It cannot use ensureRPAndRelatedResourcesDeleted since the configMap is not applied by RP.
		It("can delete the RP", func() {
			// Delete the RP.
			rp := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: appNamespace().Name,
				},
			}
			Expect(hubClient.Delete(ctx, rp)).To(Succeed(), "Failed to delete RP %s/%s", rp.Namespace, rpName)
		})

		It("should remove controller finalizers from RP", func() {
			finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: rpName, Namespace: appNamespace().Name})
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from RP %s/%s", appNamespace().Name, rpName)
		})

		It("remove the RP finalizers", func() {
			cleanupPlacement(types.NamespacedName{Name: rpName, Namespace: workNamespaceName})
		})
	})

	Context("CRP cannot co-own the resources with RP", Ordered, func() {
		BeforeAll(func() {
			By("creating work resources")
			createWorkResources()

			// Create the resources on the member clusters.
			for idx := range allMemberClusters {
				memberCluster := allMemberClusters[idx]
				By(fmt.Sprintf("creating namespace on member cluster %s", memberCluster.ClusterName))
				ns := appNamespace()
				Expect(memberCluster.KubeClient.Create(ctx, &ns)).To(Succeed())
			}

			By("creating an RP")
			createRP(workNamespaceName, rpName)

			By("should update RP status as expected")
			rpStatusUpdatedActual := rpStatusUpdatedActual(appConfigMapIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(rpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)

			By("creating a CRP that uses PickAll strategy")
			createCRP(crpName)
		})

		AfterAll(func() {
			ensureRPAndRelatedResourcesDeleted(types.NamespacedName{Name: rpName, Namespace: workNamespaceName}, allMemberClusters)
			By("CRP should take over the work resources")
			// ConfigMap on the hub was deleted in the previous step.
			crpStatusUpdatedActual := crpStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "1")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}

				appConfigMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
				failedPlacements := []placementv1beta1.FailedResourcePlacement{
					{
						ResourceIdentifier: placementv1beta1.ResourceIdentifier{
							Kind:      "ConfigMap",
							Name:      appConfigMapName,
							Version:   "v1",
							Namespace: workNamespaceName,
						},
						Condition: metav1.Condition{
							Type:               placementv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(workapplier.ApplyOrReportDiffResTypeFailedToTakeOver),
							ObservedGeneration: 0,
						},
					},
				}
				wantStatus := placementv1beta1.PlacementStatus{
					Conditions: crpAppliedFailedConditions(crp.Generation),
					PerClusterPlacementStatuses: []placementv1beta1.PerClusterPlacementStatus{
						{
							ClusterName:           memberCluster1EastProdName,
							ObservedResourceIndex: "0",
							FailedPlacements:      failedPlacements,
							Conditions:            perClusterApplyFailedConditions(crp.Generation),
						},
						{
							ClusterName:           memberCluster2EastCanaryName,
							ObservedResourceIndex: "0",
							FailedPlacements:      failedPlacements,
							Conditions:            perClusterApplyFailedConditions(crp.Generation),
						},
						{
							ClusterName:           memberCluster3WestProdName,
							ObservedResourceIndex: "0",
							FailedPlacements:      failedPlacements,
							Conditions:            perClusterApplyFailedConditions(crp.Generation),
						},
					},
					SelectedResources:     workResourceIdentifiers(),
					ObservedResourceIndex: "0",
				}
				if diff := cmp.Diff(crp.Status, wantStatus, placementStatusCmpOptions...); diff != "" {
					return fmt.Errorf("RP status diff (-got, +want): %s", diff)
				}
				return nil
			}
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update RP %s status as expected", rpName)
		})
	})
})
