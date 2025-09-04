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
package clusterresourceplacementstatuswatcher

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

const (
	eventuallyTimeout         = time.Second * 10
	consistentlyTimeout       = time.Second * 15
	consistentlyCheckInterval = time.Millisecond * 250
	eventuallyCheckInterval   = time.Millisecond * 250
)

// This container cannot be run in parallel with other ITs because it uses a shared fakePlacementController.
var _ = Describe("Test ClusterResourcePlacementStatus Watcher - delete events", Serial, func() {
	var crp *placementv1beta1.ClusterResourcePlacement
	var crps *placementv1beta1.ClusterResourcePlacementStatus

	BeforeEach(func() {
		fakePlacementController.ResetQueue()
	})

	Context("When CRPS is deleted but CRP still exists", func() {
		It("Should enqueue CRP for reconciliation", func() {
			testCRPName := "test-crp-1"

			By("Creating a ClusterResourcePlacement")
			crp = buildCRP(testCRPName, placementv1beta1.NamespaceAccessible)
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed(), "failed to create ClusterResourcePlacement")

			By("Creating a ClusterResourcePlacementStatus in the target namespace")
			crps = buildCRPS(testCRPName, testNamespace)
			Expect(k8sClient.Create(ctx, crps)).Should(Succeed(), "failed to create ClusterResourcePlacementStatus")

			By("Ensuring placement controller queue is initially empty")
			consistentlyCheckPlacementControllerQueueIsEmpty()

			By("Updating the ClusterResourcePlacementStatus by adding a label")
			// Fetch the current CRPS to get the latest resource version
			updatedCRPS := &placementv1beta1.ClusterResourcePlacementStatus{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRPName, Namespace: testNamespace}, updatedCRPS)).Should(Succeed())

			// Add a label to the CRPS
			if updatedCRPS.Labels == nil {
				updatedCRPS.Labels = make(map[string]string)
			}
			updatedCRPS.Labels["test-label"] = "test-value"
			Expect(k8sClient.Update(ctx, updatedCRPS)).Should(Succeed(), "failed to update ClusterResourcePlacementStatus with label")

			By("Checking that CRP is NOT enqueued for reconciliation after label update")
			consistentlyCheckPlacementControllerQueueIsEmpty()

			By("Deleting the ClusterResourcePlacementStatus (simulating accidental deletion)")
			Expect(k8sClient.Delete(ctx, crps)).Should(Succeed(), "failed to delete ClusterResourcePlacementStatus")

			By("Checking that CRP is enqueued for reconciliation")
			Eventually(func() string {
				return fakePlacementController.Key()
			}, eventuallyTimeout, eventuallyCheckInterval).Should(Equal(testCRPName), "CRP should be enqueued for reconciliation")

			By("Cleaning up the ClusterResourcePlacement")
			Expect(k8sClient.Delete(ctx, crp)).Should(Succeed(), "failed to delete ClusterResourcePlacement")
		})
	})

	Context("When CRPS is deleted and CRP is also being deleted", func() {
		It("Should NOT enqueue CRP for reconciliation", func() {
			testCRPName := "test-crp-2"

			By("Creating a ClusterResourcePlacement")
			crp = &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testCRPName,
					Finalizers: []string{"test-finalizer"}, // Add finalizer to prevent immediate deletion
				},
				Spec: placementv1beta1.PlacementSpec{
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    testNamespace,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed(), "failed to create ClusterResourcePlacement")

			By("Creating a ClusterResourcePlacementStatus in the target namespace")
			crps = buildCRPS(testCRPName, testNamespace)
			Expect(k8sClient.Create(ctx, crps)).Should(Succeed(), "failed to create ClusterResourcePlacementStatus")

			By("Deleting the ClusterResourcePlacement (will have deletionTimestamp due to finalizer)")
			Expect(k8sClient.Delete(ctx, crp)).Should(Succeed(), "failed to delete ClusterResourcePlacement")

			// Verify CRP has deletionTimestamp
			updatedCRP := &placementv1beta1.ClusterResourcePlacement{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRPName}, updatedCRP)).Should(Succeed())
			Expect(updatedCRP.DeletionTimestamp).ToNot(BeNil(), "CRP should have deletionTimestamp")

			By("Deleting the ClusterResourcePlacementStatus")
			Expect(k8sClient.Delete(ctx, crps)).Should(Succeed(), "failed to delete ClusterResourcePlacementStatus")

			By("Checking that CRP is NOT enqueued for reconciliation")
			consistentlyCheckPlacementControllerQueueIsEmpty()

			By("Cleaning up by removing finalizer")
			updatedCRP.Finalizers = []string{}
			Expect(k8sClient.Update(ctx, updatedCRP)).Should(Succeed(), "failed to remove finalizer")
		})
	})

	Context("When CRPS is deleted but CRP doesn't exist", func() {
		It("Should NOT enqueue anything for reconciliation", func() {
			By("Creating a ClusterResourcePlacementStatus without corresponding CRP")
			crps = buildCRPS("non-existent-crp", testNamespace)
			Expect(k8sClient.Create(ctx, crps)).Should(Succeed(), "failed to create ClusterResourcePlacementStatus")

			By("Deleting the ClusterResourcePlacementStatus")
			Expect(k8sClient.Delete(ctx, crps)).Should(Succeed(), "failed to delete ClusterResourcePlacementStatus")

			By("Checking that nothing is enqueued for reconciliation")
			consistentlyCheckPlacementControllerQueueIsEmpty()
		})
	})

	Context("When CRPS is deleted but CRP has non-NamespaceAccessible StatusReportingScope", func() {
		It("Should NOT enqueue CRP for reconciliation", func() {
			testCRPName := "test-crp-cluster-scoped"

			By("Creating a ClusterResourcePlacement with ClusterScoped status reporting")
			crp = buildCRP(testCRPName, placementv1beta1.ClusterScopeOnly)
			Expect(k8sClient.Create(ctx, crp)).Should(Succeed(), "failed to create ClusterResourcePlacement")

			By("Creating a ClusterResourcePlacementStatus (simulating orphaned CRPS)")
			crps = buildCRPS(testCRPName, testNamespace)
			Expect(k8sClient.Create(ctx, crps)).Should(Succeed(), "failed to create ClusterResourcePlacementStatus")

			By("Deleting the ClusterResourcePlacementStatus")
			Expect(k8sClient.Delete(ctx, crps)).Should(Succeed(), "failed to delete ClusterResourcePlacementStatus")

			By("Checking that CRP is NOT enqueued for reconciliation due to non-NamespaceAccessible scope")
			consistentlyCheckPlacementControllerQueueIsEmpty()

			By("Cleaning up the ClusterResourcePlacement")
			Expect(k8sClient.Delete(ctx, crp)).Should(Succeed(), "failed to delete ClusterResourcePlacement")
		})
	})
})

func consistentlyCheckPlacementControllerQueueIsEmpty() {
	Consistently(func() string {
		return fakePlacementController.Key()
	}, consistentlyTimeout, consistentlyCheckInterval).Should(BeEmpty(), "placement controller queue should be empty")
}

func buildCRPS(name, namespace string) *placementv1beta1.ClusterResourcePlacementStatus {
	return &placementv1beta1.ClusterResourcePlacementStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		PlacementStatus: placementv1beta1.PlacementStatus{
			ObservedResourceIndex: "0",
		},
		LastUpdatedTime: metav1.Now(),
	}
}

func buildCRP(name string, statusReportingScope placementv1beta1.StatusReportingScope) *placementv1beta1.ClusterResourcePlacement {
	return &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: placementv1beta1.PlacementSpec{
			StatusReportingScope: statusReportingScope,
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    testNamespace,
				},
			},
		},
	}
}
