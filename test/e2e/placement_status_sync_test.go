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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	statussyncutils "github.com/kubefleet-dev/kubefleet/test/utils/crpstatussync"
)

const (
	crpsEventuallyDuration = time.Second * 20
)

var _ = Describe("ClusterResourcePlacementStatus E2E Tests", Ordered, func() {
	Context("Create, Update & Delete ClusterResourcePlacementStatus, StatusReportingScope is NamespaceAccessible", func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create test resources that will be selected by the CRP.
			createWorkResources()

			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(2)),
					},
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		AfterAll(func() {
			// CRP is already deleted, ensure related resources are cleaned up.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})

		It("should update CRP status with 2 clusters with synced condition set to true as expected", func() {
			expectedClusters := []string{memberCluster2EastCanaryName, memberCluster3WestProdName}
			statusUpdatedActual := namespaceAccessibleCRPStatusUpdatedActual(workResourceIdentifiers(), expectedClusters, nil, "0", metav1.ConditionTrue, false)
			Eventually(statusUpdatedActual, crpsEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status with 2 clusters")
		})

		It("should sync ClusterResourcePlacementStatus with initial CRP status (2 clusters)", func() {
			crpsMatchesActual := statussyncutils.CRPSStatusMatchesCRPActual(ctx, hubClient, crpName, appNamespace().Name)
			Eventually(crpsMatchesActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "ClusterResourcePlacementStatus should match expected structure and CRP status for 2 clusters")
		})

		It("should update CRP to select 3 clusters", func() {
			// Update CRP to select 3 clusters.
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				crp.Spec.Policy.NumberOfClusters = ptr.To(int32(3))
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP to select 3 clusters")
		})

		It("should update CRP status with 3 clusters with synced condition set to true as expected", func() {
			statusUpdatedActual := namespaceAccessibleCRPStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0", metav1.ConditionTrue, false)
			Eventually(statusUpdatedActual, crpsEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status with 3 clusters")
		})

		It("should sync ClusterResourcePlacementStatus with updated CRP status (3 clusters)", func() {
			crpsMatchesActual := statussyncutils.CRPSStatusMatchesCRPActual(ctx, hubClient, crpName, appNamespace().Name)
			Eventually(crpsMatchesActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "ClusterResourcePlacementStatus should match expected structure and CRP status for 3 clusters")
		})

		It("should delete ClusterResourcePlacementStatus manually", func() {
			crpStatus := &placementv1beta1.ClusterResourcePlacementStatus{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crpName,
					Namespace: appNamespace().Name,
				},
			}
			Expect(hubClient.Delete(ctx, crpStatus)).To(Succeed(), "Failed to delete ClusterResourcePlacementStatus")
		})

		It("should recreate ClusterResourcePlacementStatus automatically after manual deletion", func() {
			crpStatusKey := types.NamespacedName{
				Name:      crpName,
				Namespace: appNamespace().Name,
			}

			// Wait for CRPS to be recreated by the controller.
			Eventually(func() bool {
				crpStatus := &placementv1beta1.ClusterResourcePlacementStatus{}
				if err := hubClient.Get(ctx, crpStatusKey, crpStatus); err != nil {
					return false
				}
				return crpStatus.DeletionTimestamp == nil
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "ClusterResourcePlacementStatus should be recreated after manual deletion")

			// Verify the recreated CRPS matches the current CRP status.
			crpsMatchesActual := statussyncutils.CRPSStatusMatchesCRPActual(ctx, hubClient, crpName, appNamespace().Name)
			Eventually(crpsMatchesActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Recreated ClusterResourcePlacementStatus should match current CRP status")
		})

		It("CRP status should remain unchanged", func() {
			statusUpdatedActual := namespaceAccessibleCRPStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0", metav1.ConditionTrue, false)
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update CRP status with 3 clusters")
		})

		It("delete CRP", func() {
			// Delete the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
			}
			Expect(hubClient.Delete(ctx, crp)).Should(SatisfyAny(Succeed()), "Failed to delete CRP")

			// Wait for the CRP to be deleted.
			Eventually(func() bool {
				return k8serrors.IsNotFound(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "CRP should be deleted")
		})

		It("should ensure ClusterResourcePlacementStatus is deleted after CRP deletion", func() {
			crpStatus := &placementv1beta1.ClusterResourcePlacementStatus{}
			crpStatusKey := types.NamespacedName{
				Name:      crpName,
				Namespace: appNamespace().Name,
			}

			Eventually(func() bool {
				return k8serrors.IsNotFound(hubClient.Get(ctx, crpStatusKey, crpStatus))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "ClusterResourcePlacementStatus should be deleted after CRP deletion")

			Consistently(func() bool {
				return k8serrors.IsNotFound(hubClient.Get(ctx, crpStatusKey, crpStatus))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue(), "ClusterResourcePlacementStatus should be deleted after CRP deletion")
		})
	})

	Context("StatusReportingScope is ClusterScopeOnly", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create test resources that will be selected by the CRP.
			createWorkResources()

			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       crpName,
					Finalizers: []string{customDeletionBlockerFinalizer},
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					StatusReportingScope: placementv1beta1.ClusterScopeOnly,
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		AfterAll(func() {
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})

		It("should update CRP status as expected", func() {
			crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status as expected")
		})

		It("should not create ClusterResourcePlacementStatus when StatusReportingScope is ClusterScopeOnly", func() {
			crpStatus := &placementv1beta1.ClusterResourcePlacementStatus{}
			crpStatusKey := types.NamespacedName{
				Name:      crpName,
				Namespace: appNamespace().Name,
			}

			Consistently(func() bool {
				return k8serrors.IsNotFound(hubClient.Get(ctx, crpStatusKey, crpStatus))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue(), "ClusterResourcePlacementStatus should not be created when StatusReportingScope is ClusterScopeOnly")
		})
	})

	Context("Namespace deletion with ClusterResourcePlacementStatus, StatusReportingScope is NamespaceAccessible", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create test resources that will be selected by the CRP.
			createWorkResources()

			// Create CRP that targets the work resources namespace with NamespaceAccessible scope.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		AfterAll(func() {
			// Clean up resources.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})

		It("should update CRP status with synced condition set to true", func() {
			// Wait for CRP status to be updated.
			statusUpdatedActual := namespaceAccessibleCRPStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0", metav1.ConditionTrue, false)
			Eventually(statusUpdatedActual, crpsEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status")
		})

		It("should create ClusterResourcePlacementStatus in the work resources namespace", func() {
			// Verify CRPS is created in the work resources namespace.
			crpsMatchesActual := statussyncutils.CRPSStatusMatchesCRPActual(ctx, hubClient, crpName, appNamespace().Name)
			Eventually(crpsMatchesActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "ClusterResourcePlacementStatus should be created in work resources namespace")
		})

		It("should delete the namespace containing ClusterResourcePlacementStatus", func() {
			// Delete the work resources namespace.
			workNamespace := appNamespace()
			Expect(hubClient.Delete(ctx, &workNamespace)).To(Succeed(), "Failed to delete work resources namespace")

			// Wait for the namespace to be deleted.
			Eventually(func() bool {
				return k8serrors.IsNotFound(hubClient.Get(ctx, types.NamespacedName{Name: workNamespace.Name}, &workNamespace))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Work resources namespace should be deleted")
		})

		It("should not automatically recreate the namespace", func() {
			// The namespace should remain deleted as it won't be automatically recreated.
			Consistently(func() bool {
				workNamespace := &corev1.Namespace{}
				return k8serrors.IsNotFound(hubClient.Get(ctx, types.NamespacedName{Name: appNamespace().Name}, workNamespace))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue(), "Namespace should remain deleted")
		})

		It("should update CRP status with Status synced condition set to false", func() {
			// Wait for CRP status to be updated.
			statusUpdatedActual := namespaceAccessibleCRPStatusUpdatedActual([]placementv1beta1.ResourceIdentifier{}, allMemberClusterNames, nil, "1", metav1.ConditionFalse, false)
			Eventually(statusUpdatedActual, crpsEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status")
		})

		It("should recreate namespace manually", func() {
			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: appNamespace().Name,
				},
			}
			Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to recreate work resources namespace")

			// Wait for namespace creation
			Eventually(func() error {
				return hubClient.Get(ctx, types.NamespacedName{Name: appNamespace().Name}, &ns)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), fmt.Sprintf("Failed to get %s", appNamespace().Name))
		})

		It("should update CRP status with Status synced condition set to true", func() {
			// Wait for CRP status to be updated.
			statusUpdatedActual := namespaceAccessibleCRPStatusUpdatedActual(workNamespaceIdentifiers(), allMemberClusterNames, nil, "2", metav1.ConditionTrue, false)
			Eventually(statusUpdatedActual, crpsEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status")
		})
	})

	Context("NamespaceAccessible CRP with namespace selector change for consistency validation - namespace exists", func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		newNamespaceName := "new-namespace"
		BeforeAll(func() {
			// Create work resources that will be selected by the CRP.
			createWorkResources()

			// Create another namespace.
			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: newNamespaceName,
				},
			}
			Expect(hubClient.Create(ctx, &ns)).To(Succeed(), "Failed to create new-namespace")
			// Wait for namespace creation
			Eventually(func() error {
				return hubClient.Get(ctx, types.NamespacedName{Name: newNamespaceName}, &ns)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), fmt.Sprintf("Failed to get %s", newNamespaceName))

			// Create CRP with NamespaceAccessible scope.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		AfterAll(func() {
			// Clean up resources.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

			// Delete the additional namespace.
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: newNamespaceName,
				},
			}
			Expect(hubClient.Delete(ctx, ns)).To(Succeed(), "Failed to delete new-namespace")
		})

		It("should update CRP status with initial namespace selection", func() {
			statusUpdatedActual := namespaceAccessibleCRPStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0", metav1.ConditionTrue, false)
			Eventually(statusUpdatedActual, crpsEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status")
		})

		It("should create CRPS in the initial namespace and verify its status", func() {
			crpsMatchesActual := statussyncutils.CRPSStatusMatchesCRPActual(ctx, hubClient, crpName, appNamespace().Name)
			Eventually(crpsMatchesActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "CRPS should be created in initial namespace and match CRP status")
		})

		It("should update CRP to select a different namespace", func() {
			// Update CRP to select the new-namespace instead.
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				crp.Spec.ResourceSelectors = []placementv1beta1.ResourceSelectorTerm{
					{
						Group:   corev1.GroupName,
						Version: "v1",
						Kind:    "Namespace",
						Name:    newNamespaceName,
					},
				}
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP to select different namespace")
		})

		It("should update CRP status with scheduled condition set to false", func() {
			statusUpdatedActual := namespaceAccessibleCRPStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0", metav1.ConditionTrue, true)
			Eventually(statusUpdatedActual, crpsEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status")
		})

		It("should update CRP to select original namespace", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				crp.Spec.ResourceSelectors = workResourceSelector()
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP to select different namespace")
		})

		It("should update CRP status with scheduled condition set to true", func() {
			statusUpdatedActual := namespaceAccessibleCRPStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0", metav1.ConditionTrue, false)
			Eventually(statusUpdatedActual, crpsEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status")
		})
	})

	Context("NamespaceAccessible CRP with namespace selector change for consistency validation - namespace doesn't exist", func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Create work resources that will be selected by the CRP.
			createWorkResources()

			// Create CRP with NamespaceAccessible scope.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
		})

		AfterAll(func() {
			// Clean up resources.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})

		It("should update CRP status with initial namespace selection", func() {
			statusUpdatedActual := namespaceAccessibleCRPStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0", metav1.ConditionTrue, false)
			Eventually(statusUpdatedActual, crpsEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status")
		})

		It("should create CRPS in the initial namespace and verify its status", func() {
			crpsMatchesActual := statussyncutils.CRPSStatusMatchesCRPActual(ctx, hubClient, crpName, appNamespace().Name)
			Eventually(crpsMatchesActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "CRPS should be created in initial namespace and match CRP status")
		})

		It("should update CRP to select a non-existent namespace", func() {
			// Update CRP to select the non-existent-namespace instead.
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				crp.Spec.ResourceSelectors = []placementv1beta1.ResourceSelectorTerm{
					{
						Group:   corev1.GroupName,
						Version: "v1",
						Kind:    "Namespace",
						Name:    "non-existent-namespace",
					},
				}
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP to select different namespace")
		})

		It("should update CRP status with scheduled condition set to false", func() {
			statusUpdatedActual := namespaceAccessibleCRPStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0", metav1.ConditionTrue, true)
			Eventually(statusUpdatedActual, crpsEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status")
		})
	})
})
