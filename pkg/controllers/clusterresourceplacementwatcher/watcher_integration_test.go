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
package clusterresourceplacementwatcher

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

const (
	testCRPName = "my-crp"
)

func clusterResourcePlacementForTest() *fleetv1beta1.ClusterResourcePlacement {
	return &fleetv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: testCRPName,
		},
		Spec: fleetv1beta1.ClusterResourcePlacementSpec{
			ResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
				{
					Group:   corev1.GroupName,
					Version: "v1",
					Kind:    "Service",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"region": "east"},
					},
				},
			},
			Policy: &fleetv1beta1.PlacementPolicy{},
		},
	}
}

// This container cannot be run in parallel with other ITs because it uses a shared fakePlacementController.
var _ = Describe("Test ClusterResourcePlacement Watcher", Serial, func() {
	const (
		eventuallyTimeout    = time.Second * 10
		consistentlyDuration = time.Second * 10
		interval             = time.Millisecond * 250
	)

	var (
		createdCRP = &fleetv1beta1.ClusterResourcePlacement{}
	)

	BeforeEach(func() {
		fakePlacementController.ResetQueue()
		By("By creating a new clusterResourcePlacement")
		createdCRP = clusterResourcePlacementForTest()
		Expect(k8sClient.Create(ctx, createdCRP)).Should(Succeed())

		By("By checking the placement queue before resetting")
		// The event could arrive after the resetting, which causes the flakiness.
		// It makes sure the queue is clear before proceed.
		Eventually(func() bool {
			return fakePlacementController.Key() == testCRPName
		}, eventuallyTimeout, interval).Should(BeTrue(), "placementController should receive the CRP name when creating CRP")

		By("By resetting the placement queue")
		fakePlacementController.ResetQueue()
	})

	Context("When updating clusterResourcePlacement", func() {
		BeforeEach(func() {
			By("By getting latest crp before updating")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRPName}, createdCRP)).Should(Succeed())
		})
		AfterEach(func() {
			By("By deleting crp")
			Expect(k8sClient.Delete(ctx, createdCRP)).Should(Succeed())

			By("By checking crp")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: testCRPName}, createdCRP))
			}, eventuallyTimeout, interval).Should(BeTrue(), "crp should be deleted")
		})

		It("Updating the spec and it should enqueue the event", func() {
			By("By updating the clusterResourcePlacement spec")
			revisionLimit := int32(3)
			createdCRP.Spec.RevisionHistoryLimit = &revisionLimit
			Expect(k8sClient.Update(ctx, createdCRP)).Should(Succeed())

			By("By checking placement controller queue")
			Eventually(func() bool {
				return fakePlacementController.Key() == testCRPName
			}, eventuallyTimeout, interval).Should(BeTrue(), "placementController should receive the CRP name")
		})

		It("Updating the status and it should ignore the event", func() {
			By("By updating the clusterResourcePlacement status")
			newCondition := metav1.Condition{
				Type:               string(fleetv1beta1.ClusterResourcePlacementAppliedConditionType),
				Status:             metav1.ConditionTrue,
				Reason:             "applied",
				ObservedGeneration: createdCRP.GetGeneration(),
			}
			createdCRP.SetConditions(newCondition)
			Expect(k8sClient.Status().Update(ctx, createdCRP)).Should(Succeed())

			By("By checking placement controller queue")
			Consistently(func() bool {
				return fakePlacementController.Key() == ""
			}, consistentlyDuration, interval).Should(BeTrue(), "watcher should ignore the update status event and not enqueue the request into the placementController queue")
		})
	})

	Context("When deleting clusterResourcePlacement", func() {
		It("Should enqueue the event", func() {
			By("By deleting crp")
			Expect(k8sClient.Delete(ctx, createdCRP)).Should(Succeed())

			By("By checking crp")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: testCRPName}, createdCRP))
			}, eventuallyTimeout, interval).Should(BeTrue(), "crp should be deleted")

			By("By checking placement controller queue")
			Eventually(func() bool {
				return fakePlacementController.Key() == testCRPName
			}, eventuallyTimeout, interval).Should(BeTrue(), "placementController should receive the CRP name")
		})
	})
})
