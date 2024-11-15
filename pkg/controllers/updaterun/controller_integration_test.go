/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package updaterun

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	timeout  = time.Second * 10
	interval = time.Millisecond * 250
	duration = time.Second * 30
)

var (
	testUpdateRunName        string
	testCRPName              string
	testResourceSnapshotName string
	testUpdateStrategyName   string
	updateRunNamespacedName  types.NamespacedName
)

var _ = Describe("Test the clusterStagedUpdateRun controller", func() {

	BeforeEach(func() {
		testUpdateRunName = "updaterun-" + utils.RandStr()
		testCRPName = "crp-" + utils.RandStr()
		testResourceSnapshotName = "snapshot-" + utils.RandStr()
		testUpdateStrategyName = "updatestrategy-" + utils.RandStr()
		updateRunNamespacedName = types.NamespacedName{Name: testUpdateRunName}
	})

	Context("Test reconciling a clusterStagedUpdateRun", func() {
		It("Should add the finalizer to the clusterStagedUpdateRun", func() {
			By("Creating a new clusterStagedUpdateRun")
			updateRun := getTestClusterStagedUpdateRun(testUpdateRunName)
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())

			By("Checking the finalizer is added")
			validateUpdateRunHasFinalizer(ctx, k8sClient, updateRun)

			By("Deleting the clusterStagedUpdateRun")
			Expect(k8sClient.Delete(ctx, updateRun)).Should(Succeed())

			By("Checking the clusterStagedUpdateRun is deleted")
			validateUpdateRunIsDeleted(ctx, k8sClient, updateRunNamespacedName)
		})
	})

	Context("Test deleting a clusterStagedUpdateRun", func() {
		It("Should delete the clusterStagedUpdateRun without any clusterApprovalRequests", func() {
			By("Creating a new clusterStagedUpdateRun")
			updateRun := getTestClusterStagedUpdateRun(testUpdateRunName)
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())

			By("Checking the finalizer is added")
			validateUpdateRunHasFinalizer(ctx, k8sClient, updateRun)

			By("Deleting the clusterStagedUpdateRun")
			Expect(k8sClient.Delete(ctx, updateRun)).Should(Succeed())

			By("Checking the clusterStagedUpdateRun is deleted")
			validateUpdateRunIsDeleted(ctx, k8sClient, updateRunNamespacedName)
		})

		It("Should delete all ClusterApprovalRequest objects associated with the clusterStagedUpdateRun", func() {
			By("Creating a new clusterStagedUpdateRun")
			updateRun := getTestClusterStagedUpdateRun(testUpdateRunName)
			Expect(k8sClient.Create(ctx, updateRun)).Should(Succeed())

			By("Creating ClusterApprovalRequests")
			approvalRequests := []*placementv1alpha1.ClusterApprovalRequest{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "req1",
						Labels: map[string]string{
							placementv1alpha1.TargetUpdateRunLabel: testUpdateRunName,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "req2",
						Labels: map[string]string{
							placementv1alpha1.TargetUpdateRunLabel: testUpdateRunName,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "req3",
						Labels: map[string]string{
							placementv1alpha1.TargetUpdateRunLabel: testUpdateRunName + "1", // different update run
						},
					},
				},
			}
			for _, req := range approvalRequests {
				Expect(k8sClient.Create(ctx, req)).Should(Succeed())
			}

			By("Checking the finalizer is added")
			validateUpdateRunHasFinalizer(ctx, k8sClient, updateRun)

			By("Deleting the clusterStagedUpdateRun")
			Expect(k8sClient.Delete(ctx, updateRun)).Should(Succeed())

			By("Checking the clusterStagedUpdateRun is deleted")
			validateUpdateRunIsDeleted(ctx, k8sClient, updateRunNamespacedName)

			By("Checking the clusterApprovalRequests are deleted")
			Eventually(func() (int, error) {
				appReqList := &placementv1alpha1.ClusterApprovalRequestList{}
				if err := k8sClient.List(ctx, appReqList); err != nil {
					return -1, err
				}
				return len(appReqList.Items), nil
			}, duration, interval).Should(Equal(1))
		})

	})
})

func getTestClusterStagedUpdateRun(name string) *placementv1alpha1.ClusterStagedUpdateRun {
	return &placementv1alpha1.ClusterStagedUpdateRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: placementv1alpha1.StagedUpdateRunSpec{
			PlacementName:            testCRPName,
			ResourceSnapshotIndex:    testResourceSnapshotName,
			StagedUpdateStrategyName: testUpdateStrategyName,
		},
	}
}

func validateUpdateRunHasFinalizer(ctx context.Context, k8sClient client.Client, updateRun *placementv1alpha1.ClusterStagedUpdateRun) {
	namepsacedName := types.NamespacedName{Name: updateRun.Name}
	Eventually(func() error {
		if err := k8sClient.Get(ctx, namepsacedName, updateRun); err != nil {
			return fmt.Errorf("failed to get clusterStagedUpdateRun %s: %w", namepsacedName, err)
		}
		if !controllerutil.ContainsFinalizer(updateRun, placementv1alpha1.ClusterStagedUpdateRunFinalizer) {
			return fmt.Errorf("finalizer not added to clusterStagedUpdateRun %s", namepsacedName)
		}
		return nil
	}, timeout, interval).Should(Succeed(), "Failed to add finalizer to clusterStagedUpdateRun %s", namepsacedName)
}

func validateUpdateRunIsDeleted(ctx context.Context, k8sClient client.Client, name types.NamespacedName) {
	Eventually(func() error {
		updateRun := &placementv1alpha1.ClusterStagedUpdateRun{}
		if err := k8sClient.Get(ctx, name, updateRun); !errors.IsNotFound(err) {
			return fmt.Errorf("clusterStagedUpdateRun %s still exists or an unexpected error occurred: %w", name, err)
		}
		return nil
	}, timeout, interval).Should(Succeed(), "Failed to remove clusterStagedUpdateRun %s ", name)
}
