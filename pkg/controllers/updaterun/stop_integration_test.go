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

package updaterun

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
)

var _ = Describe("UpdateRun stop tests", func() {
	var updateRun *placementv1beta1.ClusterStagedUpdateRun
	var crp *placementv1beta1.ClusterResourcePlacement
	var policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot
	var updateStrategy *placementv1beta1.ClusterStagedUpdateStrategy
	var resourceBindings []*placementv1beta1.ClusterResourceBinding
	var targetClusters []*clusterv1beta1.MemberCluster
	var unscheduledClusters []*clusterv1beta1.MemberCluster
	var resourceSnapshot *placementv1beta1.ClusterResourceSnapshot
	var wantStatus *placementv1beta1.UpdateRunStatus
	var numTargetClusters int
	var numUnscheduledClusters int

	BeforeEach(OncePerOrdered, func() {
		testUpdateRunName = "updaterun-" + utils.RandStr()
		testCRPName = "crp-" + utils.RandStr()
		testResourceSnapshotName = testCRPName + "-" + testResourceSnapshotIndex + "-snapshot"
		testUpdateStrategyName = "updatestrategy-" + utils.RandStr()
		testCROName = "cro-" + utils.RandStr()
		updateRunNamespacedName = types.NamespacedName{Name: testUpdateRunName}

		updateRun = generateTestClusterStagedUpdateRun()
		crp = generateTestClusterResourcePlacement()
		// 1 BeforeStageTask: Approval
		// 2 AfterStageTasks: Approval + TimedWait
		updateStrategy = generateTestClusterStagedUpdateStrategyWithSingleStage([]placementv1beta1.StageTask{
			{
				Type: placementv1beta1.StageTaskTypeApproval,
			},
		}, []placementv1beta1.StageTask{
			{
				Type: placementv1beta1.StageTaskTypeApproval,
			},
			{
				Type: placementv1beta1.StageTaskTypeTimedWait,
				WaitTime: &metav1.Duration{
					Duration: time.Second * 4,
				},
			},
		})
		resourceBindings, targetClusters, unscheduledClusters = generateSmallTestClusterResourceBindingsAndClusters(1, 3)
		policySnapshot = generateTestClusterSchedulingPolicySnapshot(1, len(targetClusters))
		resourceSnapshot = generateTestClusterResourceSnapshot()
		numTargetClusters, numUnscheduledClusters = len(targetClusters), len(unscheduledClusters)

		// Set smaller wait time for testing
		stageUpdatingWaitTime = time.Second * 3
		clusterUpdatingWaitTime = time.Second * 2

		By("Creating a new clusterResourcePlacement")
		Expect(k8sClient.Create(ctx, crp)).To(Succeed())

		By("Creating scheduling policy snapshot")
		Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())

		By("Setting the latest policy snapshot condition as fully scheduled")
		meta.SetStatusCondition(&policySnapshot.Status.Conditions, metav1.Condition{
			Type:               string(placementv1beta1.PolicySnapshotScheduled),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: policySnapshot.Generation,
			Reason:             "scheduled",
		})
		Expect(k8sClient.Status().Update(ctx, policySnapshot)).Should(Succeed(), "failed to update the policy snapshot condition")

		By("Creating the member clusters")
		for _, cluster := range targetClusters {
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		}
		for _, cluster := range unscheduledClusters {
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		}

		By("Creating a bunch of ClusterResourceBindings")
		for _, binding := range resourceBindings {
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())
		}

		By("Creating a clusterStagedUpdateStrategy")
		Expect(k8sClient.Create(ctx, updateStrategy)).To(Succeed())

		By("Creating a new resource snapshot")
		Expect(k8sClient.Create(ctx, resourceSnapshot)).To(Succeed())
	})

	AfterEach(OncePerOrdered, func() {
		By("Deleting the clusterStagedUpdateRun")
		Expect(k8sClient.Delete(ctx, updateRun)).Should(Succeed())
		updateRun = nil

		By("Deleting the clusterResourcePlacement")
		Expect(k8sClient.Delete(ctx, crp)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		crp = nil

		By("Deleting the clusterSchedulingPolicySnapshot")
		Expect(k8sClient.Delete(ctx, policySnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		policySnapshot = nil

		By("Deleting the clusterResourceBindings")
		for _, binding := range resourceBindings {
			Expect(k8sClient.Delete(ctx, binding)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
		resourceBindings = nil

		By("Deleting the member clusters")
		for _, cluster := range targetClusters {
			Expect(k8sClient.Delete(ctx, cluster)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
		for _, cluster := range unscheduledClusters {
			Expect(k8sClient.Delete(ctx, cluster)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
		targetClusters, unscheduledClusters = nil, nil

		By("Deleting the clusterStagedUpdateStrategy")
		Expect(k8sClient.Delete(ctx, updateStrategy)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		updateStrategy = nil

		By("Deleting the clusterResourceSnapshot")
		Expect(k8sClient.Delete(ctx, resourceSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		resourceSnapshot = nil

		By("Checking update run status metrics are removed")
		// No metrics are emitted as all are removed after updateRun is deleted.
		validateUpdateRunMetricsEmitted()
		resetUpdateRunMetrics()
	})

	Context("Cluster staged update run should have stopped when state Stop", Ordered, func() {
		var wantApprovalRequest *placementv1beta1.ClusterApprovalRequest
		BeforeAll(func() {
			// Add finalizer to one of the bindings for unscheduled cluster to test deletion stage later.
			binding := resourceBindings[numTargetClusters] // first unscheduled cluster
			binding.Finalizers = append(binding.Finalizers, "block-deletion-for-test")
			Expect(k8sClient.Update(ctx, binding)).Should(Succeed(), "failed to add finalizer to binding for deletion stage test")

			By("Creating a new clusterStagedUpdateRun")
			updateRun.Spec.State = placementv1beta1.StateRun
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization succeeded and the execution has not started")
			initialized := generateSucceededInitializationStatusForSmallClusters(crp, updateRun, testResourceSnapshotIndex, policySnapshot, updateStrategy, 3)
			wantStatus = generateExecutionNotStartedStatus(updateRun, initialized)
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")

			By("Validating the first beforeStage approvalRequest has been created")
			wantApprovalRequest = &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: updateRun.Status.StagesStatus[0].BeforeStageTaskStatus[0].ApprovalRequestName,
					Labels: map[string]string{
						placementv1beta1.TargetUpdatingStageNameLabel:   updateRun.Status.StagesStatus[0].StageName,
						placementv1beta1.TargetUpdateRunLabel:           updateRun.Name,
						placementv1beta1.TaskTypeLabel:                  placementv1beta1.BeforeStageTaskLabelValue,
						placementv1beta1.IsLatestUpdateRunApprovalLabel: "true",
					},
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: updateRun.Name,
					TargetStage:     updateRun.Status.StagesStatus[0].StageName,
				},
			}
			validateApprovalRequestCreated(wantApprovalRequest)

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateWaitingMetric(placementv1beta1.StateRun, updateRun))
		})

		It("Should stop the update run in BeforeStageTask for 1st stage when state is Stop", func() {
			By("Updating updateRun state to Stop")
			updateRun = updateClusterStagedUpdateRunState(updateRun.Name, placementv1beta1.StateStop)
			// Update the test's want status to match the new generation.
			updateAllStatusConditionsGeneration(wantStatus, updateRun.Generation)

			By("Validating the update run is stopped")
			// Mark stage progressing condition as stopped.
			meta.SetStatusCondition(&wantStatus.StagesStatus[0].Conditions, generateFalseConditionWithReason(updateRun, placementv1beta1.StageUpdatingConditionProgressing, condition.StageUpdatingStoppedReason))
			// Mark update run stopped.
			meta.SetStatusCondition(&wantStatus.Conditions, generateFalseConditionWithReason(updateRun, placementv1beta1.StagedUpdateRunConditionProgressing, condition.UpdateRunStoppedReason))
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun))
		})

		It("Should accept the approval request and not rollout 1st stage while in Stop state", func() {
			By("Approving the approvalRequest")
			approveClusterApprovalRequest(ctx, wantApprovalRequest.Name)

			By("Validating update run is still stopped")
			validateClusterStagedUpdateRunStatusConsistently(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun))
		})

		It("Should start executing stage 1 of the update run when state is Run", func() {
			By("Updating updateRun state to Run")
			updateRun = updateClusterStagedUpdateRunState(updateRun.Name, placementv1beta1.StateRun)
			// Update the test's want status to match the new generation.
			updateAllStatusConditionsGeneration(wantStatus, updateRun.Generation)

			By("Validating the approvalRequest has ApprovalAccepted status")
			validateApprovalRequestAccepted(ctx, wantApprovalRequest.Name)

			By("Validating update run is running")
			wantStatus = generateExecutionStartedStatus(updateRun, wantStatus)
			// Approval task has been approved.
			wantStatus.StagesStatus[0].BeforeStageTaskStatus[0].Conditions = append(wantStatus.StagesStatus[0].BeforeStageTaskStatus[0].Conditions,
				generateTrueCondition(updateRun, placementv1beta1.StageTaskConditionApprovalRequestApproved))
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun))
		})

		It("Should mark the 1st cluster in the 1st stage as succeeded after marking the binding available", func() {
			By("Validating the 1st clusterResourceBinding is updated to Bound")
			binding := resourceBindings[0] // cluster-0
			validateBindingState(ctx, binding, resourceSnapshot.Name, updateRun, 0)

			By("Updating the 1st clusterResourceBinding to Available")
			meta.SetStatusCondition(&binding.Status.Conditions, generateTrueCondition(binding, placementv1beta1.ResourceBindingAvailable))
			Expect(k8sClient.Status().Update(ctx, binding)).Should(Succeed(), "failed to update the binding status")

			By("Validating the 1st cluster has succeeded and 2nd cluster has started")
			wantStatus.StagesStatus[0].Clusters[0].Conditions = append(wantStatus.StagesStatus[0].Clusters[0].Conditions, generateTrueCondition(updateRun, placementv1beta1.ClusterUpdatingConditionSucceeded))
			wantStatus.StagesStatus[0].Clusters[1].Conditions = append(wantStatus.StagesStatus[0].Clusters[1].Conditions, generateTrueCondition(updateRun, placementv1beta1.ClusterUpdatingConditionStarted))
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")

			By("Validating the 1st stage has startTime set")
			Expect(updateRun.Status.StagesStatus[0].StartTime).ShouldNot(BeNil())

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun))
		})

		It("Should be stopping in the middle of cluster updating when update run state is Stop", func() {
			By("Updating updateRun state to Stop")
			updateRun = updateClusterStagedUpdateRunState(updateRun.Name, placementv1beta1.StateStop)
			// Update the test's want status to match the new generation.
			updateAllStatusConditionsGeneration(wantStatus, updateRun.Generation)

			By("Validating the update run is stopping")
			// 2nd cluster has started condition but no succeeded condition.
			// Mark stage progressing condition as unknown with stopping reason.
			meta.SetStatusCondition(&wantStatus.StagesStatus[0].Conditions, generateProgressingUnknownConditionWithReason(updateRun, condition.StageUpdatingStoppingReason))
			// Mark updateRun progressing condition as unknown with stopping reason.
			meta.SetStatusCondition(&wantStatus.Conditions, generateProgressingUnknownConditionWithReason(updateRun, condition.UpdateRunStoppingReason))
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun), generateStoppingMetric(placementv1beta1.StateStop, updateRun))
		})

		It("Should wait for cluster to finish updating so update run should still be stopping", func() {
			By("Validating the 2nd cluster has NOT succeeded and the update run is still stopping")
			validateClusterStagedUpdateRunStatusConsistently(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun), generateStoppingMetric(placementv1beta1.StateStop, updateRun))
		})

		It("Should have completely stopped after the in-progress cluster has finished updating", func() {
			By("Validating the 2nd clusterResourceBinding is updated to Bound")
			binding := resourceBindings[1] // cluster-1
			validateBindingState(ctx, binding, resourceSnapshot.Name, updateRun, 0)

			By("Updating the 2nd clusterResourceBinding to Available")
			meta.SetStatusCondition(&binding.Status.Conditions, generateTrueCondition(binding, placementv1beta1.ResourceBindingAvailable))
			Expect(k8sClient.Status().Update(ctx, binding)).Should(Succeed(), "failed to update the binding status")

			By("Validating the 2nd cluster has succeeded and the update run has completely stopped")
			// Mark 2nd cluster succeeded.
			meta.SetStatusCondition(&wantStatus.StagesStatus[0].Clusters[1].Conditions, generateTrueCondition(updateRun, placementv1beta1.ClusterUpdatingConditionSucceeded))
			// Mark stage progressing condition as false with stopped reason.
			meta.SetStatusCondition(&wantStatus.StagesStatus[0].Conditions, generateFalseProgressingCondition(updateRun, placementv1beta1.StageUpdatingConditionProgressing, condition.StageUpdatingStoppedReason))
			// Mark updateRun progressing condition as false with stopped reason.
			meta.SetStatusCondition(&wantStatus.Conditions, generateFalseProgressingCondition(updateRun, placementv1beta1.StagedUpdateRunConditionProgressing, condition.UpdateRunStoppedReason))
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun), generateStoppingMetric(placementv1beta1.StateStop, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun))

			By("Validating update run is in stopped state")
			validateClusterStagedUpdateRunStatusConsistently(ctx, updateRun, wantStatus, "")

			By("Validating 3rd clusterResourceBinding is NOT updated to Bound")
			binding = resourceBindings[2] // cluster-2
			validateNotBoundBindingState(ctx, binding)
		})

		It("Should continue executing stage 1 of the update run when state is Run", func() {
			By("Updating updateRun state to Run")
			updateRun = updateClusterStagedUpdateRunState(updateRun.Name, placementv1beta1.StateRun)
			// Update the test's want status to match the new generation.
			updateAllStatusConditionsGeneration(wantStatus, updateRun.Generation)

			By("Validating update run is running")
			// Mark 3rd cluster started.
			meta.SetStatusCondition(&wantStatus.StagesStatus[0].Clusters[2].Conditions, generateTrueCondition(updateRun, placementv1beta1.ClusterUpdatingConditionStarted))
			// Mark stage progressing condition as true with progressing reason.
			meta.SetStatusCondition(&wantStatus.StagesStatus[0].Conditions, generateTrueCondition(updateRun, placementv1beta1.StageUpdatingConditionProgressing))
			// Mark updateRun progressing condition as true with progressing reason.
			meta.SetStatusCondition(&wantStatus.Conditions, generateTrueCondition(updateRun, placementv1beta1.StagedUpdateRunConditionProgressing))
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateStoppingMetric(placementv1beta1.StateStop, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun))
		})

		It("Should mark the 3rd cluster in the 1st stage as succeeded after marking the binding available", func() {
			By("Validating the 3rd clusterResourceBinding is updated to Bound")
			binding := resourceBindings[2] // cluster-2
			validateBindingState(ctx, binding, resourceSnapshot.Name, updateRun, 0)

			By("Updating the 3rd clusterResourceBinding to Available")
			meta.SetStatusCondition(&binding.Status.Conditions, generateTrueCondition(binding, placementv1beta1.ResourceBindingAvailable))
			Expect(k8sClient.Status().Update(ctx, binding)).Should(Succeed(), "failed to update the binding status")

			By("Validating the 3rd cluster has succeeded")
			wantStatus.StagesStatus[0].Clusters[2].Conditions = append(wantStatus.StagesStatus[0].Clusters[2].Conditions, generateTrueCondition(updateRun, placementv1beta1.ClusterUpdatingConditionSucceeded))

			// Approval request for AfterStageTasks is created.
			meta.SetStatusCondition(&wantStatus.StagesStatus[0].AfterStageTaskStatus[0].Conditions, generateTrueCondition(updateRun, placementv1beta1.StageTaskConditionApprovalRequestCreated))
			// Stage is waiting for AfterStageTasks to complete.
			meta.SetStatusCondition(&wantStatus.StagesStatus[0].Conditions, generateFalseCondition(updateRun, placementv1beta1.StageUpdatingConditionProgressing))
			meta.SetStatusCondition(&wantStatus.Conditions, generateFalseCondition(updateRun, placementv1beta1.StagedUpdateRunConditionProgressing))
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateStoppingMetric(placementv1beta1.StateStop, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun), generateWaitingMetric(placementv1beta1.StateRun, updateRun))
		})

		It("Should have approval request created for 1st stage AfterStageTask", func() {
			By("Validating the approvalRequest has been created")
			wantApprovalRequest = &placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: updateRun.Status.StagesStatus[0].AfterStageTaskStatus[0].ApprovalRequestName,
					Labels: map[string]string{
						placementv1beta1.TargetUpdatingStageNameLabel:   updateRun.Status.StagesStatus[0].StageName,
						placementv1beta1.TargetUpdateRunLabel:           updateRun.Name,
						placementv1beta1.TaskTypeLabel:                  placementv1beta1.AfterStageTaskLabelValue,
						placementv1beta1.IsLatestUpdateRunApprovalLabel: "true",
					},
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: updateRun.Name,
					TargetStage:     updateRun.Status.StagesStatus[0].StageName,
				},
			}
			validateApprovalRequestCreated(wantApprovalRequest)

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateStoppingMetric(placementv1beta1.StateStop, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun), generateWaitingMetric(placementv1beta1.StateRun, updateRun))
		})

		It("Should stop the update run in AfterStageTask for 1st stage when state is Stop", func() {
			By("Updating updateRun state to Stop")
			updateRun = updateClusterStagedUpdateRunState(updateRun.Name, placementv1beta1.StateStop)
			// Update the test's want status to match the new generation.
			updateAllStatusConditionsGeneration(wantStatus, updateRun.Generation)

			By("Validating the update run is stopped")
			// Mark stage progressing condition as stopped.
			meta.SetStatusCondition(&wantStatus.StagesStatus[0].Conditions, generateFalseConditionWithReason(updateRun, placementv1beta1.StageUpdatingConditionProgressing, condition.StageUpdatingStoppedReason))
			// Mark update run stopped.
			meta.SetStatusCondition(&wantStatus.Conditions, generateFalseConditionWithReason(updateRun, placementv1beta1.StagedUpdateRunConditionProgressing, condition.UpdateRunStoppedReason))
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateStoppingMetric(placementv1beta1.StateStop, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun), generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun))
		})

		It("Should not continue to delete stage after approval when still stopped", func() {
			By("Approving the approvalRequest")
			approveClusterApprovalRequest(ctx, wantApprovalRequest.Name)

			By("Validating the to-be-deleted bindings are NOT deleted")
			Consistently(func() error {
				for i := numTargetClusters; i < numTargetClusters+numUnscheduledClusters; i++ {
					binding := placementv1beta1.ClusterResourceBinding{}
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceBindings[i].Name}, &binding); err != nil {
						return fmt.Errorf("get binding %s returned a not-found error or another error: %w", binding.Name, err)
					}

					if !binding.DeletionTimestamp.IsZero() {
						return fmt.Errorf("binding %s is being deleted when it should not be", binding.Name)
					}
				}
				return nil
			}, duration, interval).Should(Succeed(), "failed to validate the to-be-deleted bindings still exist")

			By("Validating update run is stopped")
			validateClusterStagedUpdateRunStatusConsistently(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateStoppingMetric(placementv1beta1.StateStop, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun), generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun))
		})

		It("Should complete the 1st stage once it starts running again when wait time passed and approval request approved then move on to the Delete stage", func() {
			By("Updating updateRun state to Run")
			updateRun = updateClusterStagedUpdateRunState(updateRun.Name, placementv1beta1.StateRun)
			// Update the test's want status to match the new generation.
			updateAllStatusConditionsGeneration(wantStatus, updateRun.Generation)

			By("Validating the approvalRequest has ApprovalAccepted status")
			validateApprovalRequestAccepted(ctx, wantApprovalRequest.Name)

			By("Validating both after stage tasks have completed and Deletion has started")
			// Approval AfterStageTask completed.
			wantStatus.StagesStatus[0].AfterStageTaskStatus[0].Conditions = append(wantStatus.StagesStatus[0].AfterStageTaskStatus[0].Conditions,
				generateTrueCondition(updateRun, placementv1beta1.StageTaskConditionApprovalRequestApproved))
			// Timedwait AfterStageTask completed.
			wantStatus.StagesStatus[0].AfterStageTaskStatus[1].Conditions = append(wantStatus.StagesStatus[0].AfterStageTaskStatus[1].Conditions,
				generateTrueCondition(updateRun, placementv1beta1.StageTaskConditionWaitTimeElapsed))
			// 1st stage completed, mark progressing condition reason as succeeded and add succeeded condition.
			wantStatus.StagesStatus[0].Conditions[0] = generateFalseProgressingCondition(updateRun, placementv1beta1.StageUpdatingConditionProgressing, condition.StageUpdatingSucceededReason)
			wantStatus.StagesStatus[0].Conditions = append(wantStatus.StagesStatus[0].Conditions, generateTrueCondition(updateRun, placementv1beta1.StageUpdatingConditionSucceeded))
			// Deletion stage started. Mark deletion stage progressing condition as true with progressing reason.
			meta.SetStatusCondition(&wantStatus.DeletionStageStatus.Conditions, generateTrueCondition(updateRun, placementv1beta1.StageUpdatingConditionProgressing))
			// Mark 1 cluster started and the other clusters as succeeded in deletion stage.
			for i := range wantStatus.DeletionStageStatus.Clusters {
				wantStatus.DeletionStageStatus.Clusters[i].Conditions = append(wantStatus.DeletionStageStatus.Clusters[i].Conditions, generateTrueCondition(updateRun, placementv1beta1.ClusterUpdatingConditionStarted))
				if i != 0 { // first unscheduled cluster is still deleting
					wantStatus.DeletionStageStatus.Clusters[i].Conditions = append(wantStatus.DeletionStageStatus.Clusters[i].Conditions, generateTrueCondition(updateRun, placementv1beta1.ClusterUpdatingConditionSucceeded))
				}
			}
			// Mark updateRun progressing condition as true with progressing reason.
			meta.SetStatusCondition(&wantStatus.Conditions, generateTrueCondition(updateRun, placementv1beta1.StagedUpdateRunConditionProgressing))
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")

			By("Validating the 1st stage has endTime set")
			Expect(updateRun.Status.StagesStatus[0].EndTime).ShouldNot(BeNil())

			By("Validating the waitTime after stage task only completes after the wait time")
			waitStartTime := meta.FindStatusCondition(updateRun.Status.StagesStatus[0].Conditions, string(placementv1beta1.StageUpdatingConditionProgressing)).LastTransitionTime.Time
			waitEndTime := meta.FindStatusCondition(updateRun.Status.StagesStatus[0].AfterStageTaskStatus[1].Conditions, string(placementv1beta1.StageTaskConditionWaitTimeElapsed)).LastTransitionTime.Time
			Expect(waitStartTime.Add(updateStrategy.Spec.Stages[0].AfterStageTasks[1].WaitTime.Duration).After(waitEndTime)).Should(BeFalse(),
				fmt.Sprintf("waitEndTime %v did not pass waitStartTime %v long enough, want at least %v", waitEndTime, waitStartTime, updateStrategy.Spec.Stages[0].AfterStageTasks[1].WaitTime.Duration))

			By("Validating the creation time of the approval request is before the complete time of the timedwait task")
			approvalCreateTime := meta.FindStatusCondition(updateRun.Status.StagesStatus[0].AfterStageTaskStatus[0].Conditions, string(placementv1beta1.StageTaskConditionApprovalRequestCreated)).LastTransitionTime.Time
			Expect(approvalCreateTime.Before(waitEndTime)).Should(BeTrue())

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateStoppingMetric(placementv1beta1.StateStop, updateRun), generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun))
		})

		It("Should stop the update run in deletion stage when state is Stop", func() {
			By("Updating updateRun state to Stop")
			updateRun = updateClusterStagedUpdateRunState(updateRun.Name, placementv1beta1.StateStop)
			// Update the test's want status to match the new generation.
			updateAllStatusConditionsGeneration(wantStatus, updateRun.Generation)

			By("Validating the update run is stopping")
			// Mark stage progressing condition as stopping.
			meta.SetStatusCondition(&wantStatus.DeletionStageStatus.Conditions, generateProgressingUnknownConditionWithReason(updateRun, condition.StageUpdatingStoppingReason))
			// Mark update run stopped.
			meta.SetStatusCondition(&wantStatus.Conditions, generateProgressingUnknownConditionWithReason(updateRun, condition.UpdateRunStoppingReason))
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun), generateStoppingMetric(placementv1beta1.StateStop, updateRun))
		})

		It("Should not complete deletion stage when in progress clusters still deleting while stopped", func() {
			By("Validating the first unscheduled cluster resource binding has started deleting but is NOT deleted")
			Consistently(func() error {
				binding := &placementv1beta1.ClusterResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceBindings[numTargetClusters].Name}, binding); err != nil {
					return fmt.Errorf("get binding %s returned a not-found error or another error: %w", binding.Name, err)
				}
				if binding.DeletionTimestamp.IsZero() {
					return fmt.Errorf("binding %s is not marked for deletion yet", binding.Name)
				}
				return nil
			}, duration, interval).Should(Succeed(), "failed to validate the to-be-deleted bindings for unscheduled-cluster-0 still exist")

			By("Validating update run is stopping")
			validateClusterStagedUpdateRunStatusConsistently(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun), generateStoppingMetric(placementv1beta1.StateStop, updateRun))
		})

		It("Should stop completely after in-progress deletion is done when state is Stop", func() {
			By("Removing the finalizer on the in-progress deletion binding to allow deletion to complete")
			Eventually(func() error {
				binding := &placementv1beta1.ClusterResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceBindings[numTargetClusters].Name}, binding); err != nil {
					return fmt.Errorf("get binding %s returned a not-found error or another error: %w", binding.Name, err)
				}
				if len(binding.Finalizers) == 0 {
					return nil
				}
				binding.Finalizers = []string{}
				if err := k8sClient.Update(ctx, binding); err != nil {
					return fmt.Errorf("failed to remove finalizer from binding %s: %w", binding.Name, err)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "failed to remove finalizer from binding for deletion stage test")

			By("Validating the binding is deleted")
			Eventually(func() error {
				binding := &placementv1beta1.ClusterResourceBinding{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceBindings[numTargetClusters].Name}, binding)
				if err == nil {
					return fmt.Errorf("binding %s is not deleted", binding.Name)
				}
				if !apierrors.IsNotFound(err) {
					return fmt.Errorf("get binding %s does not return a not-found error: %w", binding.Name, err)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "failed to validate the to-be-deleted binding for unscheduled-cluster-0 is deleted")

			By("Validating the update run is completely stopped")
			// Mark the first unscheduled cluster succeeded.
			meta.SetStatusCondition(&wantStatus.DeletionStageStatus.Clusters[0].Conditions, generateTrueCondition(updateRun, placementv1beta1.ClusterUpdatingConditionSucceeded))
			// Mark deletion stage progressing condition as stopped.
			meta.SetStatusCondition(&wantStatus.DeletionStageStatus.Conditions, generateFalseConditionWithReason(updateRun, placementv1beta1.StageUpdatingConditionProgressing, condition.StageUpdatingStoppedReason))
			// Mark update run stopped.
			meta.SetStatusCondition(&wantStatus.Conditions, generateFalseConditionWithReason(updateRun, placementv1beta1.StagedUpdateRunConditionProgressing, condition.UpdateRunStoppedReason))
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun), generateStoppingMetric(placementv1beta1.StateStop, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun))
		})

		It("Should complete delete stage and complete the update run when state is Run", func() {
			By("Updating updateRun state to Run")
			updateRun = updateClusterStagedUpdateRunState(updateRun.Name, placementv1beta1.StateRun)
			// Update the test's want status to match the new generation.
			updateAllStatusConditionsGeneration(wantStatus, updateRun.Generation)

			By("Validating the to-be-deleted bindings are all deleted")
			Eventually(func() error {
				for i := numTargetClusters; i < numTargetClusters+numUnscheduledClusters; i++ {
					binding := &placementv1beta1.ClusterResourceBinding{}
					err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceBindings[i].Name}, binding)
					if err == nil {
						return fmt.Errorf("binding %s is not deleted", binding.Name)
					}
					if !apierrors.IsNotFound(err) {
						return fmt.Errorf("get binding %s does not return a not-found error: %w", binding.Name, err)
					}

					if !binding.DeletionTimestamp.IsZero() {
						return fmt.Errorf("binding %s is not deleted yet", binding.Name)
					}
				}
				return nil
			}, timeout, interval).Should(Succeed(), "failed to validate the deletion of the to-be-deleted bindings")

			By("Validating the delete stage and the clusterStagedUpdateRun has completed")
			for i := range wantStatus.DeletionStageStatus.Clusters {
				meta.SetStatusCondition(&wantStatus.DeletionStageStatus.Clusters[i].Conditions, generateTrueCondition(updateRun, placementv1beta1.ClusterUpdatingConditionSucceeded))
			}
			// Mark the stage progressing condition as false with succeeded reason and add succeeded condition.
			wantStatus.DeletionStageStatus.Conditions[0] = generateFalseProgressingCondition(updateRun, placementv1beta1.StageUpdatingConditionProgressing, condition.StageUpdatingSucceededReason)
			wantStatus.DeletionStageStatus.Conditions = append(wantStatus.DeletionStageStatus.Conditions, generateTrueCondition(updateRun, placementv1beta1.StageUpdatingConditionSucceeded))
			// Mark updateRun progressing condition as false with succeeded reason and add succeeded condition.
			meta.SetStatusCondition(&wantStatus.Conditions, generateFalseProgressingCondition(updateRun, placementv1beta1.StagedUpdateRunConditionProgressing, condition.UpdateRunSucceededReason))
			wantStatus.Conditions = append(wantStatus.Conditions, generateTrueCondition(updateRun, placementv1beta1.StagedUpdateRunConditionSucceeded))
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateWaitingMetric(placementv1beta1.StateRun, updateRun), generateProgressingMetric(placementv1beta1.StateRun, updateRun), generateStoppingMetric(placementv1beta1.StateStop, updateRun), generateStoppedMetric(placementv1beta1.StateStop, updateRun), generateSucceededMetric(placementv1beta1.StateRun, updateRun))
		})
	})
})

func updateClusterStagedUpdateRunState(updateRunName string, state placementv1beta1.State) *placementv1beta1.ClusterStagedUpdateRun {
	updateRun := &placementv1beta1.ClusterStagedUpdateRun{}
	Eventually(func() error {
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: updateRunName}, updateRun); err != nil {
			return fmt.Errorf("failed to get ClusterStagedUpdateRun %s", updateRunName)
		}

		updateRun.Spec.State = state
		if err := k8sClient.Update(ctx, updateRun); err != nil {
			return fmt.Errorf("failed to update ClusterStagedUpdateRun %s", updateRunName)
		}
		return nil
	}, timeout, interval).Should(Succeed(), "Failed to update ClusterStagedUpdateRun %s state to %s", updateRunName, state)
	return updateRun
}

func validateApprovalRequestAccepted(ctx context.Context, approvalRequestName string) {
	Eventually(func() (bool, error) {
		var approvalRequest placementv1beta1.ClusterApprovalRequest
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: approvalRequestName}, &approvalRequest); err != nil {
			return false, err
		}
		return condition.IsConditionStatusTrue(meta.FindStatusCondition(approvalRequest.Status.Conditions, string(placementv1beta1.ApprovalRequestConditionApprovalAccepted)), approvalRequest.Generation), nil
	}, timeout, interval).Should(BeTrue(), "failed to validate the approvalRequest %s is accepted", approvalRequestName)
}
