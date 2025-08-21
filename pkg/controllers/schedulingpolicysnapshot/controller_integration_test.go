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

package schedulingpolicysnapshot

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

const (
	testCRPName      = "my-crp"
	testRPName       = "my-rp"
	testSnapshotName = "my-snapshot"
	testNamespace    = "test-namespace"

	eventuallyTimeout    = time.Second * 10
	consistentlyDuration = time.Second * 10
	interval             = time.Millisecond * 250
)

func clusterSchedulingPolicySnapshotForTest() *fleetv1beta1.ClusterSchedulingPolicySnapshot {
	return &fleetv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: testSnapshotName,
			Labels: map[string]string{
				fleetv1beta1.PolicyIndexLabel:       "1",
				fleetv1beta1.IsLatestSnapshotLabel:  "true",
				fleetv1beta1.PlacementTrackingLabel: testCRPName,
			},
		},
		Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
			PolicyHash: []byte("hash"),
		},
	}
}

func schedulingPolicySnapshotForTest() *fleetv1beta1.SchedulingPolicySnapshot {
	return &fleetv1beta1.SchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testSnapshotName,
			Namespace: testNamespace,
			Labels: map[string]string{
				fleetv1beta1.PolicyIndexLabel:       "1",
				fleetv1beta1.IsLatestSnapshotLabel:  "true",
				fleetv1beta1.PlacementTrackingLabel: testRPName,
			},
		},
		Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
			PolicyHash: []byte("hash"),
		},
	}
}

var _ = Describe("Test clusterSchedulingPolicySnapshot Controller", func() {
	var (
		createdSnapshot = &fleetv1beta1.ClusterSchedulingPolicySnapshot{}
	)

	BeforeEach(func() {
		fakePlacementController.ResetQueue()
		By("By creating a new clusterSchedulingPolicySnapshot")
		snapshot := clusterSchedulingPolicySnapshotForTest()
		Expect(k8sClient.Create(ctx, snapshot)).Should(Succeed())
	})

	Context("When creating new clusterSchedulingPolicySnapshot", func() {
		AfterEach(func() {
			By("By deleting snapshot")
			createdSnapshot = clusterSchedulingPolicySnapshotForTest()
			Expect(k8sClient.Delete(ctx, createdSnapshot)).Should(Succeed())

			By("By checking snapshot")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName}, createdSnapshot))
			}, eventuallyTimeout, interval).Should(BeTrue(), "snapshot should be deleted")
		})

		It("Should ignore the event", func() {
			By("By checking placement controller queue")
			Consistently(func() bool {
				return fakePlacementController.Key() == ""
			}, consistentlyDuration, interval).Should(BeTrue(), "controller should ignore the create event and not enqueue the request into the placementController queue")

		})
	})

	Context("When updating clusterSchedulingPolicySnapshot", func() {
		BeforeEach(func() {
			By("By resetting the placement queue")
			fakePlacementController.ResetQueue()
		})

		AfterEach(func() {
			By("By deleting snapshot")
			createdSnapshot = clusterSchedulingPolicySnapshotForTest()
			Expect(k8sClient.Delete(ctx, createdSnapshot)).Should(Succeed())

			By("By checking snapshot")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName}, createdSnapshot))
			}, eventuallyTimeout, interval).Should(BeTrue(), "snapshot should be deleted")
		})

		It("Updating the spec and should enqueue the event", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName}, createdSnapshot)).Should(Succeed())

			By("By updating the clusterSchedulingPolicySnapshot spec")
			createdSnapshot.Spec.PolicyHash = []byte("modified-hash")
			Expect(k8sClient.Update(ctx, createdSnapshot)).Should(Succeed())

			By("By checking placement controller queue")
			Eventually(func() bool {
				return fakePlacementController.Key() == testCRPName
			}, eventuallyTimeout, interval).Should(BeTrue(), "placementController should receive the CRP name")
		})

		It("Updating the status and should enqueue the event", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName}, createdSnapshot)).Should(Succeed())

			By("By updating the clusterSchedulingPolicySnapshot status")
			newCondition := metav1.Condition{
				Type:               string(fleetv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionTrue,
				Reason:             "scheduled",
				ObservedGeneration: createdSnapshot.GetGeneration(),
			}
			meta.SetStatusCondition(&createdSnapshot.Status.Conditions, newCondition)
			Expect(k8sClient.Status().Update(ctx, createdSnapshot)).Should(Succeed())

			By("By checking placement controller queue")
			Eventually(func() bool {
				return fakePlacementController.Key() == testCRPName
			}, eventuallyTimeout, interval).Should(BeTrue(), "placementController should receive the CRP name")
		})
	})

	Context("When deleting clusterSchedulingPolicySnapshot", func() {
		BeforeEach(func() {
			By("By resetting the placement queue")
			fakePlacementController.ResetQueue()
		})

		It("Should ignore the event", func() {
			By("By deleting snapshot")
			createdSnapshot = clusterSchedulingPolicySnapshotForTest()
			Expect(k8sClient.Delete(ctx, createdSnapshot)).Should(Succeed())

			By("By checking snapshot")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName}, createdSnapshot))
			}, eventuallyTimeout, interval).Should(BeTrue(), "snapshot should be deleted")

			By("By checking placement controller queue")
			Consistently(func() bool {
				return fakePlacementController.Key() == ""
			}, consistentlyDuration, interval).Should(BeTrue(), "controller should ignore the delete event and not enqueue the request into the placementController queue")
		})
	})
})

var _ = Describe("Test schedulingPolicySnapshot Controller", func() {
	var (
		createdSnapshot = &fleetv1beta1.SchedulingPolicySnapshot{}
		key             = controller.GetObjectKeyFromNamespaceName(testNamespace, testRPName)
	)

	BeforeEach(func() {
		fakePlacementController.ResetQueue()
		By("By creating a new schedulingPolicySnapshot")
		snapshot := schedulingPolicySnapshotForTest()
		Expect(k8sClient.Create(ctx, snapshot)).Should(Succeed())
	})

	Context("When creating new schedulingPolicySnapshot", func() {
		AfterEach(func() {
			By("By deleting snapshot")
			createdSnapshot = schedulingPolicySnapshotForTest()
			Expect(k8sClient.Delete(ctx, createdSnapshot)).Should(Succeed())

			By("By checking snapshot")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName, Namespace: testNamespace}, createdSnapshot))
			}, eventuallyTimeout, interval).Should(BeTrue(), "snapshot should be deleted")
		})

		It("Should ignore the event", func() {
			By("By checking placement controller queue")
			Consistently(func() bool {
				return fakePlacementController.Key() == ""
			}, consistentlyDuration, interval).Should(BeTrue(), "controller should ignore the create event and not enqueue the request into the placementController queue")

		})
	})

	Context("When updating schedulingPolicySnapshot", func() {
		BeforeEach(func() {
			By("By resetting the placement queue")
			fakePlacementController.ResetQueue()
		})

		AfterEach(func() {
			By("By deleting snapshot")
			createdSnapshot = schedulingPolicySnapshotForTest()
			Expect(k8sClient.Delete(ctx, createdSnapshot)).Should(Succeed())

			By("By checking snapshot")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName, Namespace: testNamespace}, createdSnapshot))
			}, eventuallyTimeout, interval).Should(BeTrue(), "snapshot should be deleted")
		})

		It("Updating the spec and should enqueue the event", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName, Namespace: testNamespace}, createdSnapshot)).Should(Succeed())

			By("By updating the schedulingPolicySnapshot spec")
			createdSnapshot.Spec.PolicyHash = []byte("modified-hash")
			Expect(k8sClient.Update(ctx, createdSnapshot)).Should(Succeed())

			By("By checking placement controller queue")
			Eventually(func() bool {
				return fakePlacementController.Key() == key
			}, eventuallyTimeout, interval).Should(BeTrue(), "placementController should receive the RP key")
		})

		It("Updating the status and should enqueue the event", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName, Namespace: testNamespace}, createdSnapshot)).Should(Succeed())

			By("By updating the schedulingPolicySnapshot status")
			newCondition := metav1.Condition{
				Type:               string(fleetv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionTrue,
				Reason:             "scheduled",
				ObservedGeneration: createdSnapshot.GetGeneration(),
			}
			meta.SetStatusCondition(&createdSnapshot.Status.Conditions, newCondition)
			Expect(k8sClient.Status().Update(ctx, createdSnapshot)).Should(Succeed())

			By("By checking placement controller queue")
			Eventually(func() bool {
				return fakePlacementController.Key() == key
			}, eventuallyTimeout, interval).Should(BeTrue(), "placementController should receive the RP key")
		})
	})

	Context("When deleting schedulingPolicySnapshot", func() {
		BeforeEach(func() {
			By("By resetting the placement queue")
			fakePlacementController.ResetQueue()
		})

		It("Should ignore the event", func() {
			By("By deleting snapshot")
			createdSnapshot = schedulingPolicySnapshotForTest()
			Expect(k8sClient.Delete(ctx, createdSnapshot)).Should(Succeed())

			By("By checking snapshot")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName, Namespace: testNamespace}, createdSnapshot))
			}, eventuallyTimeout, interval).Should(BeTrue(), "snapshot should be deleted")

			By("By checking placement controller queue")
			Consistently(func() bool {
				return fakePlacementController.Key() == ""
			}, consistentlyDuration, interval).Should(BeTrue(), "controller should ignore the delete event and not enqueue the request into the placementController queue")
		})
	})
})
