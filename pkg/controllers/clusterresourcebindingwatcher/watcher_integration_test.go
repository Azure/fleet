/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package clusterresourcebindingwatcher

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	testCRPName                      = "test-crp"
	testCRBName                      = "test-crb"
	testResourceSnapshotName         = "test-rs"
	testSchedulingPolicySnapshotName = "test-sps"
	testTargetCluster                = "test-cluster"
	testReason                       = "test-reason"

	eventuallyTimeout    = time.Second * 10
	consistentlyDuration = time.Second * 10
	interval             = time.Millisecond * 250
)

// This container cannot be run in parallel with other ITs because it uses a shared fakePlacementController.
var _ = Describe("Test ClusterResourceBinding Watcher - create, delete events", Serial, func() {
	var crb *fleetv1beta1.ClusterResourceBinding
	It("When creating, deleting clusterResourceBinding", func() {
		By("Creating a new clusterResourceBinding")
		crb = clusterResourceBindingForTest()
		Expect(k8sClient.Create(ctx, crb)).Should(Succeed(), "failed to create cluster resource binding")

		By("Checking placement controller queue")
		consistentlyCheckPlacementControllerQueueIsEmpty("watcher should ignore the create event and not enqueue the request into the placementController queue")

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRBName}, crb)).Should(Succeed(), "failed to get cluster resource binding")

		By("Deleting clusterResourceBinding")
		Expect(k8sClient.Delete(ctx, crb)).Should(Succeed(), "failed to delete cluster resource binding")

		By("Checking placement controller queue")
		consistentlyCheckPlacementControllerQueueIsEmpty("watcher should ignore the delete event and not enqueue the request into the placementController queue")
	})
})

// This container cannot be run in parallel with other ITs because it uses a shared fakePlacementController.
var _ = Describe("Test ClusterResourceBinding Watcher - update events", Serial, func() {
	var crb *fleetv1beta1.ClusterResourceBinding
	Context("When updating clusterResourceBinding spec, status", func() {
		BeforeEach(func() {
			fakePlacementController.ResetQueue()
			By("Creating a new clusterResourceBinding")
			crb = clusterResourceBindingForTest()
			Expect(k8sClient.Create(ctx, crb)).Should(Succeed(), "failed to create cluster resource binding")
		})

		AfterEach(func() {
			crb.Name = testCRBName
			fakePlacementController.ResetQueue()
			By("Deleting the clusterResourceBinding")
			Expect(k8sClient.Delete(ctx, crb)).Should(Succeed(), "failed to delete cluster resource binding")
		})

		It("Should not enqueue the clusterResourcePlacement name for reconciling, when clusterResourceBinding spec, status doesn't change", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRBName}, crb)).Should(Succeed(), "failed to get cluster resource binding")
			labels := crb.GetLabels()
			labels["test-key"] = "test-value"
			crb.SetLabels(labels)
			Expect(k8sClient.Update(ctx, crb)).Should(Succeed(), "failed to update cluster resource binding")

			By("By checking placement controller queue")
			consistentlyCheckPlacementControllerQueueIsEmpty("watcher should ignore the update labels event and not enqueue the request into the placementController queue")
		})

		It("Should enqueue the clusterResourcePlacement name for reconciling, when clusterResourceBinding generation changes", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRBName}, crb)).Should(Succeed(), "failed to get cluster resource binding")
			crb.Spec.State = fleetv1beta1.BindingStateBound
			Expect(k8sClient.Update(ctx, crb)).Should(Succeed(), "failed to update cluster resource binding")

			By("By checking placement controller queue")
			eventuallyCheckPlacementControllerQueue(crb.GetLabels()[fleetv1beta1.CRPTrackingLabel], "placementController should receive the cluster resource placement name")
		})

		It("Should enqueue the clusterResourcePlacement name for reconciling, when clusterResourceBinding status changes - RolloutStarted", func() {
			updateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingRolloutStarted)
		})

		It("Should enqueue the clusterResourcePlacement name for reconciling, when clusterResourceBinding status changes - Overridden", func() {
			updateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingOverridden)
		})

		It("Should enqueue the clusterResourcePlacement name for reconciling, when clusterResourceBinding status changes - WorkCreated", func() {
			updateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingWorkCreated)
		})

		It("Should enqueue the clusterResourcePlacement name for reconciling, when clusterResourceBinding status changes - Applied", func() {
			updateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingApplied)
		})

		It("Should enqueue the clusterResourcePlacement name for reconciling, when clusterResourceBinding status changes - Available", func() {
			updateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingAvailable)
		})
	})
})

func clusterResourceBindingForTest() *fleetv1beta1.ClusterResourceBinding {
	return &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   testCRBName,
			Labels: map[string]string{fleetv1beta1.CRPTrackingLabel: testCRPName},
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			State:                        fleetv1beta1.BindingStateScheduled,
			ResourceSnapshotName:         testResourceSnapshotName,
			SchedulingPolicySnapshotName: testSchedulingPolicySnapshotName,
			TargetCluster:                testTargetCluster,
		},
	}
}

func updateClusterResourceBindingStatusWithCondition(conditionType fleetv1beta1.ResourceBindingConditionType) {
	crb := &fleetv1beta1.ClusterResourceBinding{}
	By(fmt.Sprintf("Updating the clusterResourceBinding status - %s Successful", conditionType))
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRBName}, crb)).Should(Succeed(), "failed to get cluster resource binding")
	crb.Status.Conditions = []metav1.Condition{
		{
			Type:               string(conditionType),
			Status:             metav1.ConditionTrue,
			Reason:             testReason,
			LastTransitionTime: metav1.Now(),
		},
	}
	Expect(k8sClient.Status().Update(ctx, crb)).Should(Succeed(), "failed to update cluster resource binding status")

	By("Checking placement controller queue")
	eventuallyCheckPlacementControllerQueue(crb.GetLabels()[fleetv1beta1.CRPTrackingLabel], "placementController should receive the cluster resource placement name")

	fakePlacementController.ResetQueue()

	By(fmt.Sprintf("By updating the clusterResourceBinding status - %s Failed", conditionType))
	crb.Status.Conditions = []metav1.Condition{
		{
			Type:               string(conditionType),
			Status:             metav1.ConditionFalse,
			Reason:             testReason,
			LastTransitionTime: metav1.Now(),
		},
	}
	Expect(k8sClient.Status().Update(ctx, crb)).Should(Succeed(), "failed to update cluster resource binding status")

	By("Checking placement controller queue")
	eventuallyCheckPlacementControllerQueue(crb.GetLabels()[fleetv1beta1.CRPTrackingLabel], "placementController should receive the cluster resource placement name")
}

func eventuallyCheckPlacementControllerQueue(key, description string) {
	By("By checking placement controller queue")
	Eventually(func() bool {
		return fakePlacementController.Key() == key
	}, eventuallyTimeout, interval).Should(BeTrue(), description)
}

func consistentlyCheckPlacementControllerQueueIsEmpty(description string) {
	Consistently(func() bool {
		return fakePlacementController.Key() == ""
	}, consistentlyDuration, interval).Should(BeTrue(), description)
}
