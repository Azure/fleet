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
	testReason1                      = "testReason1"
	testReason2                      = "testReason2"

	eventuallyTimeout    = time.Second * 10
	consistentlyDuration = time.Second * 10
	interval             = time.Millisecond * 250
)

// This container cannot be run in parallel with other ITs because it uses a shared fakePlacementController.
var _ = Describe("Test ClusterResourceBinding Watcher - create, delete events", Serial, func() {
	var crb *fleetv1beta1.ClusterResourceBinding
	It("When creating, deleting clusterResourceBinding", func() {
		fakePlacementController.ResetQueue()
		By("Creating a new clusterResourceBinding")
		crb = clusterResourceBindingForTest()
		Expect(k8sClient.Create(ctx, crb)).Should(Succeed(), "failed to create cluster resource binding")

		By("Checking placement controller queue")
		consistentlyCheckPlacementControllerQueueIsEmpty()

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRBName}, crb)).Should(Succeed(), "failed to get cluster resource binding")

		By("Deleting clusterResourceBinding")
		Expect(k8sClient.Delete(ctx, crb)).Should(Succeed(), "failed to delete cluster resource binding")

		By("Checking placement controller queue")
		consistentlyCheckPlacementControllerQueueIsEmpty()
	})
})

// This container cannot be run in parallel with other ITs because it uses a shared fakePlacementController.
var _ = Describe("Test ClusterResourceBinding Watcher - update metadata", Serial, func() {
	var crb *fleetv1beta1.ClusterResourceBinding
	BeforeEach(func() {
		fakePlacementController.ResetQueue()
		By("Creating a new clusterResourceBinding")
		crb = clusterResourceBindingForTest()
		Expect(k8sClient.Create(ctx, crb)).Should(Succeed(), "failed to create cluster resource binding")
		fakePlacementController.ResetQueue()
	})

	AfterEach(func() {
		By("Deleting the clusterResourceBinding")
		Expect(k8sClient.Delete(ctx, crb)).Should(Succeed(), "failed to delete cluster resource binding")
	})

	It("Should not enqueue the clusterResourcePlacement name for reconciling, when only meta data changed", func() {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRBName}, crb)).Should(Succeed(), "failed to get cluster resource binding")
		labels := crb.GetLabels()
		labels["test-key"] = "test-value"
		crb.SetLabels(labels)
		Expect(k8sClient.Update(ctx, crb)).Should(Succeed(), "failed to update cluster resource binding")

		By("Checking placement controller queue")
		consistentlyCheckPlacementControllerQueueIsEmpty()
	})

	It("Should not enqueue the clusterResourcePlacement name for reconciling, when only spec changed", func() {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRBName}, crb)).Should(Succeed(), "failed to get cluster resource binding")
		crb.Spec.State = fleetv1beta1.BindingStateBound
		Expect(k8sClient.Update(ctx, crb)).Should(Succeed(), "failed to update cluster resource binding")

		By("Checking placement controller queue")
		consistentlyCheckPlacementControllerQueueIsEmpty()
	})
})

// This container cannot be run in parallel with other ITs because it uses a shared fakePlacementController. These tests are also ordered.
var _ = Describe("Test ClusterResourceBinding Watcher - update status", Serial, Ordered, func() {
	var crb *fleetv1beta1.ClusterResourceBinding
	var currentTime metav1.Time
	BeforeAll(func() {
		currentTime = metav1.Now()
		fakePlacementController.ResetQueue()
		By("Creating a new clusterResourceBinding")
		crb = clusterResourceBindingForTest()
		Expect(k8sClient.Create(ctx, crb)).Should(Succeed(), "failed to create cluster resource binding")
		fakePlacementController.ResetQueue()
	})

	AfterAll(func() {
		crb.Name = testCRBName
		By("Deleting the clusterResourceBinding")
		Expect(k8sClient.Delete(ctx, crb)).Should(Succeed(), "failed to delete cluster resource binding")
	})

	It("Should enqueue the clusterResourcePlacement name for reconciling, when clusterResourceBinding status changes - RolloutStarted", func() {
		validateWhenUpdateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingRolloutStarted, crb.Generation, metav1.ConditionTrue, testReason1)
		validateWhenUpdateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingRolloutStarted, crb.Generation, metav1.ConditionFalse, testReason1)
	})

	It("Should enqueue the clusterResourcePlacement name for reconciling, when clusterResourceBinding status changes - Overridden", func() {
		validateWhenUpdateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingOverridden, crb.Generation, metav1.ConditionTrue, testReason1)
		validateWhenUpdateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingOverridden, crb.Generation, metav1.ConditionFalse, testReason1)
	})

	It("Should enqueue the clusterResourcePlacement name for reconciling, when clusterResourceBinding status changes - WorkCreated", func() {
		validateWhenUpdateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingWorkSynchronized, crb.Generation, metav1.ConditionTrue, testReason1)
		validateWhenUpdateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingWorkSynchronized, crb.Generation, metav1.ConditionFalse, testReason1)
	})

	It("Should enqueue the clusterResourcePlacement name for reconciling, when clusterResourceBinding status changes - Applied", func() {
		validateWhenUpdateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingApplied, crb.Generation, metav1.ConditionTrue, testReason1)
		validateWhenUpdateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingApplied, crb.Generation, metav1.ConditionFalse, testReason1)
	})

	It("Should enqueue the clusterResourcePlacement name for reconciling, when clusterResourceBinding status changes - Available", func() {
		validateWhenUpdateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingAvailable, crb.Generation, metav1.ConditionTrue, testReason1)
		validateWhenUpdateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingAvailable, crb.Generation, metav1.ConditionFalse, testReason1)
	})

	It("Should enqueue the clusterResourcePlacement name for reconciling, when condition's reason changes", func() {
		validateWhenUpdateClusterResourceBindingStatusWithCondition(fleetv1beta1.ResourceBindingOverridden, crb.Generation, metav1.ConditionFalse, testReason2)
	})

	It("Should not enqueue the clusterResourcePlacement name for reconciling, when condition's observed generation changes", func() {
		crb := &fleetv1beta1.ClusterResourceBinding{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRBName}, crb)).Should(Succeed(), "failed to get cluster resource binding")
		condition := metav1.Condition{
			Type:               string(fleetv1beta1.ResourceBindingOverridden),
			ObservedGeneration: crb.Generation + 1,
			Status:             metav1.ConditionFalse,
			Reason:             testReason2,
			LastTransitionTime: currentTime,
		}
		By(fmt.Sprintf("Updating the clusterResourceBinding status - %s, %d, %s, %s", fleetv1beta1.ResourceBindingOverridden, crb.Generation, metav1.ConditionFalse, testReason2))
		crb.SetConditions(condition)
		Expect(k8sClient.Status().Update(ctx, crb)).Should(Succeed(), "failed to update cluster resource binding status")

		consistentlyCheckPlacementControllerQueueIsEmpty()
	})

	It("Should not enqueue the clusterResourcePlacement name for reconciling, when only condition's last transition time changes", func() {
		crb := &fleetv1beta1.ClusterResourceBinding{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRBName}, crb)).Should(Succeed(), "failed to get cluster resource binding")
		newTime := metav1.NewTime(currentTime.Add(10 * time.Second))
		condition := metav1.Condition{
			Type:               string(fleetv1beta1.ResourceBindingOverridden),
			ObservedGeneration: crb.Generation + 1,
			Status:             metav1.ConditionFalse,
			Reason:             testReason2,
			LastTransitionTime: newTime,
		}
		By(fmt.Sprintf("Updating the clusterResourceBinding status - %s, %d, %s, %s", fleetv1beta1.ResourceBindingOverridden, crb.Generation, metav1.ConditionFalse, testReason2))
		crb.SetConditions(condition)
		Expect(k8sClient.Status().Update(ctx, crb)).Should(Succeed(), "failed to update cluster resource binding status")

		consistentlyCheckPlacementControllerQueueIsEmpty()
	})

	Context("Should enqueue the clusterResourcePlacement name for reconciling, when the failed placement list has changed", Serial, Ordered, func() {
		It("Should enqueue the clusterResourcePlacement name for reconciling, when there are new failed placements", func() {
			crb := &fleetv1beta1.ClusterResourceBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRBName}, crb)).Should(Succeed(), "failed to get cluster resource binding")
			crb.Status.FailedPlacements = []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						Reason:             "fakeFailedAvailableReason",
						Message:            "fakeFailedAvailableMessage",
						LastTransitionTime: metav1.Now(),
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						Reason:             "fakeFailedAvailableReason",
						Message:            "fakeFailedAvailableMessage",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, crb)).Should(Succeed(), "failed to update cluster resource binding status")

			By("Checking placement controller queue")
			eventuallyCheckPlacementControllerQueue(crb.GetLabels()[fleetv1beta1.CRPTrackingLabel])
			fakePlacementController.ResetQueue()
		})

		It("Should enqueue the clusterResourcePlacement name for reconciling, when there are one less failed placements", func() {
			crb := &fleetv1beta1.ClusterResourceBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRBName}, crb)).Should(Succeed(), "failed to get cluster resource binding")
			crb.Status.FailedPlacements = []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					Condition: metav1.Condition{
						Type:               fleetv1beta1.WorkConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						Reason:             "fakeFailedAvailableReason",
						Message:            "fakeFailedAvailableMessage",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, crb)).Should(Succeed(), "failed to update cluster resource binding status")

			By("Checking placement controller queue")
			eventuallyCheckPlacementControllerQueue(crb.GetLabels()[fleetv1beta1.CRPTrackingLabel])
			fakePlacementController.ResetQueue()
		})

		It("Should enqueue the clusterResourcePlacement name for reconciling, when there are no more failed placements", func() {
			crb := &fleetv1beta1.ClusterResourceBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRBName}, crb)).Should(Succeed(), "failed to get cluster resource binding")
			crb.Status.FailedPlacements = []fleetv1beta1.FailedResourcePlacement{}
			Expect(k8sClient.Status().Update(ctx, crb)).Should(Succeed(), "failed to update cluster resource binding status")

			By("Checking placement controller queue")
			eventuallyCheckPlacementControllerQueue(crb.GetLabels()[fleetv1beta1.CRPTrackingLabel])
			fakePlacementController.ResetQueue()
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

func validateWhenUpdateClusterResourceBindingStatusWithCondition(conditionType fleetv1beta1.ResourceBindingConditionType, observedGeneration int64, status metav1.ConditionStatus, reason string) {
	crb := &fleetv1beta1.ClusterResourceBinding{}
	By(fmt.Sprintf("Updating the clusterResourceBinding status - %s, %d, %s, %s", conditionType, observedGeneration, status, reason))
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testCRBName}, crb)).Should(Succeed(), "failed to get cluster resource binding")
	condition := metav1.Condition{
		Type:               string(conditionType),
		ObservedGeneration: observedGeneration,
		Status:             status,
		Reason:             reason,
		LastTransitionTime: metav1.Now(),
	}
	crb.SetConditions(condition)
	Expect(k8sClient.Status().Update(ctx, crb)).Should(Succeed(), "failed to update cluster resource binding status")

	By("Checking placement controller queue")
	eventuallyCheckPlacementControllerQueue(crb.GetLabels()[fleetv1beta1.CRPTrackingLabel])
	fakePlacementController.ResetQueue()
}

func eventuallyCheckPlacementControllerQueue(key string) {
	Eventually(func() bool {
		return fakePlacementController.Key() == key
	}, eventuallyTimeout, interval).Should(BeTrue(), "placementController should receive the cluster resource placement name")
}

func consistentlyCheckPlacementControllerQueueIsEmpty() {
	Consistently(func() bool {
		return fakePlacementController.Key() == ""
	}, consistentlyDuration, interval).Should(BeTrue(), "watcher should ignore the create event and not enqueue the request into the placementController queue")
}
