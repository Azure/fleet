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
	Context("Create and Update ClusterResourcePlacementStatus, StatusReportingScope is NamespaceAccessible", func() {
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
				err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp)
				return k8serrors.IsNotFound(err)
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
})
