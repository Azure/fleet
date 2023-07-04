/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clustershedulingpolicysnapshot

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	testCRPName      = "my-crp"
	testSnapshotName = "my-snapshot"
)

func policySnapshot() *fleetv1beta1.ClusterSchedulingPolicySnapshot {
	return &fleetv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: testSnapshotName,
			Labels: map[string]string{
				fleetv1beta1.PolicyIndexLabel:      "1",
				fleetv1beta1.IsLatestSnapshotLabel: "true",
				fleetv1beta1.CRPTrackingLabel:      testCRPName,
			},
		},
		Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
			PolicyHash: []byte("hash"),
		},
	}
}

var _ = Describe("Test clusterSchedulingPolicySnapshot Controller", func() {
	const (
		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	var (
		createdSnapshot = &fleetv1beta1.ClusterSchedulingPolicySnapshot{}
	)

	BeforeEach(func() {
		fakePlacementController.ResetQueue()
		By("By creating a new clusterSchedulingPolicySnapshot")
		snapshot := policySnapshot()
		Expect(k8sClient.Create(ctx, snapshot)).Should(Succeed())
	})

	Context("When creating new clusterSchedulingPolicySnapshot", func() {
		AfterEach(func() {
			By("By deleting snapshot")
			createdSnapshot := policySnapshot()
			Expect(k8sClient.Delete(ctx, createdSnapshot)).Should(Succeed())

			By("By checking snapshot")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName}, createdSnapshot))
			}, duration, interval).Should(BeTrue(), "snapshot should be deleted")
		})

		It("Should ignore the event", func() {
			By("By checking placement controller queue")
			Consistently(func() bool {
				return fakePlacementController.Key() == ""
			}, duration, interval).Should(BeTrue(), "controller should ignore the create event and not enqueue the request into the placementController queue")

		})
	})

	Context("When updating clusterSchedulingPolicySnapshot", func() {
		AfterEach(func() {
			By("By deleting snapshot")
			createdSnapshot := policySnapshot()
			Expect(k8sClient.Delete(ctx, createdSnapshot)).Should(Succeed())

			By("By checking snapshot")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName}, createdSnapshot))
			}, duration, interval).Should(BeTrue(), "snapshot should be deleted")
		})

		It("Should enqueue the event", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName}, createdSnapshot)).Should(Succeed())

			By("By updating the clusterSchedulingPolicySnapshot")
			createdSnapshot.Spec.PolicyHash = []byte("modified-hash")
			Expect(k8sClient.Update(ctx, createdSnapshot)).Should(Succeed())

			By("By checking placement controller queue")
			Eventually(func() bool {
				return fakePlacementController.Key() == testCRPName
			}, timeout, interval).Should(BeTrue(), "placementController should receive the CRP name")
		})
	})

	Context("When deleting clusterSchedulingPolicySnapshot", func() {
		It("Should ignore the event", func() {
			By("By deleting snapshot")
			createdSnapshot := policySnapshot()
			Expect(k8sClient.Delete(ctx, createdSnapshot)).Should(Succeed())

			By("By checking snapshot")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName}, createdSnapshot))
			}, duration, interval).Should(BeTrue(), "snapshot should be deleted")

			By("By checking placement controller queue")
			Consistently(func() bool {
				return fakePlacementController.Key() == ""
			}, duration, interval).Should(BeTrue(), "controller should ignore the delete event and not enqueue the request into the placementController queue")
		})
	})
})
