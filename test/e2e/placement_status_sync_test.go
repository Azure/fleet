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

	"github.com/google/go-cmp/cmp"
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

		It("should update CRP status with 2 clusters as expected", func() {
			expectedClusters := []string{memberCluster2EastCanaryName, memberCluster3WestProdName}
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), expectedClusters, nil, "0")
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

		It("should update CRP status with 3 clusters as expected", func() {
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
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

			// Wait for CRPS to be recreated by the controller
			Eventually(func() bool {
				crpStatus := &placementv1beta1.ClusterResourcePlacementStatus{}
				if err := hubClient.Get(ctx, crpStatusKey, crpStatus); err != nil {
					return false
				}
				return crpStatus.DeletionTimestamp == nil
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "ClusterResourcePlacementStatus should be recreated after manual deletion")

			// Verify the recreated CRPS matches the current CRP status
			crpsMatchesActual := statussyncutils.CRPSStatusMatchesCRPActual(ctx, hubClient, crpName, appNamespace().Name)
			Eventually(crpsMatchesActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Recreated ClusterResourcePlacementStatus should match current CRP status")
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
		var crpStatusBeforeWait placementv1beta1.PlacementStatus
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
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(2)),
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

		It("should create ClusterResourcePlacementStatus in the work resources namespace", func() {
			// Wait for CRP status to be updated.
			expectedClusters := []string{memberCluster2EastCanaryName, memberCluster3WestProdName}
			statusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), expectedClusters, nil, "0")
			Eventually(statusUpdatedActual, crpsEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP status")

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

		It("should copy CRP status after namespace deletion", func() {
			// Get the CRP status immediately after namespace deletion.
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				crpStatusBeforeWait = *crp.Status.DeepCopy()
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get CRP status after namespace deletion")
		})

		It("update CRP spec to trigger status change", func() {
			// Update CRP spec to trigger a status change.
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				// Change number of clusters to 1 to trigger status update.
				crp.Spec.Policy.NumberOfClusters = ptr.To(int32(3))
				return hubClient.Update(ctx, crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP spec to trigger status change")
		})

		It("should not update CRP status after namespace deletion", func() {
			// Since CRPS can't be updated (namespace doesn't exist), CRP status should remain unchanged.
			Consistently(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
					return err
				}
				currentStatus := crp.Status
				// Compare the entire status using cmp.Diff for better error reporting.
				if diff := cmp.Diff(crpStatusBeforeWait, currentStatus); diff != "" {
					return fmt.Errorf("CRP status changed after namespace deletion (-expected +actual):\n%s", diff)
				}

				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "CRP status should remain unchanged after namespace deletion")
		})
	})
})
