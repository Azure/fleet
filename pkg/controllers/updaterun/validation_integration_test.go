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
	"strconv"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var _ = Describe("UpdateRun validation tests", func() {
	Context("Test ClusterStagedUpdateRun validation", func() {
		var updateRun *placementv1beta1.ClusterStagedUpdateRun
		var crp *placementv1beta1.ClusterResourcePlacement
		var policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot
		var updateStrategy *placementv1beta1.ClusterStagedUpdateStrategy
		var resourceBindings []*placementv1beta1.ClusterResourceBinding
		var targetClusters []*clusterv1beta1.MemberCluster
		var unscheduledClusters []*clusterv1beta1.MemberCluster
		var resourceSnapshot *placementv1beta1.ClusterResourceSnapshot
		var clusterResourceOverride *placementv1beta1.ClusterResourceOverrideSnapshot
		var wantStatus *placementv1beta1.UpdateRunStatus

		BeforeEach(func() {
			testUpdateRunName = "updaterun-" + utils.RandStr()
			testCRPName = "crp-" + utils.RandStr()
			testResourceSnapshotName = testCRPName + "-" + testResourceSnapshotIndex + "-snapshot"
			testUpdateStrategyName = "updatestrategy-" + utils.RandStr()
			testCROName = "cro-" + utils.RandStr()
			updateRunNamespacedName = types.NamespacedName{Name: testUpdateRunName}

			updateRun = generateTestClusterStagedUpdateRun()
			crp = generateTestClusterResourcePlacement()
			updateStrategy = generateTestClusterStagedUpdateStrategy()
			clusterResourceOverride = generateTestClusterResourceOverride()
			resourceBindings, targetClusters, unscheduledClusters = generateTestClusterResourceBindingsAndClusters(1)
			policySnapshot = generateTestClusterSchedulingPolicySnapshot(1, len(targetClusters))
			resourceSnapshot = generateTestClusterResourceSnapshot()

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

			By("Creating a new cluster resource override")
			Expect(k8sClient.Create(ctx, clusterResourceOverride)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization succeeded")
			initialized := generateSucceededInitializationStatus(crp, updateRun, policySnapshot, updateStrategy, clusterResourceOverride)
			wantStatus = generateExecutionStartedStatus(updateRun, initialized)
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")
		})

		AfterEach(func() {
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

			By("Deleting the clusterResourceOverride")
			Expect(k8sClient.Delete(ctx, clusterResourceOverride)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
			clusterResourceOverride = nil

			By("Checking the update run status metrics are removed")
			// No metrics are emitted as all are removed after updateRun is deleted.
			validateUpdateRunMetricsEmitted()
			resetUpdateRunMetrics()
		})

		Context("Test validateCRP", func() {
			AfterEach(func() {
				By("Checking update run status metrics are emitted")
				validateUpdateRunMetricsEmitted(generateProgressingMetric(updateRun), generateFailedMetric(updateRun))
			})

			It("Should fail to validate if the CRP is not found", func() {
				By("Deleting the clusterResourcePlacement")
				Expect(k8sClient.Delete(ctx, crp)).Should(Succeed())

				By("Validating the validation failed")
				wantStatus = generateFailedValidationStatus(updateRun, wantStatus)
				validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "parent placement not found")
			})

			It("Should fail to validate if CRP does not have external rollout strategy type", func() {
				By("Updating CRP's rollout strategy type")
				crp.Spec.Strategy.Type = placementv1beta1.RollingUpdateRolloutStrategyType
				Expect(k8sClient.Update(ctx, crp)).To(Succeed())

				By("Validating the validation failed")
				wantStatus = generateFailedValidationStatus(updateRun, wantStatus)
				validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus,
					"parent placement does not have an external rollout strategy")
			})

			It("Should fail to valdiate if the ApplyStrategy in the CRP has changed", func() {
				By("Updating CRP's ApplyStrategy")
				crp.Spec.Strategy.ApplyStrategy.Type = placementv1beta1.ApplyStrategyTypeClientSideApply
				Expect(k8sClient.Update(ctx, crp)).To(Succeed())

				By("Validating the validation failed")
				wantStatus = generateFailedValidationStatus(updateRun, wantStatus)
				validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "the applyStrategy in the updateRun is outdated")
			})
		})

		Context("Test determinePolicySnapshot for PickN", func() {
			It("Should fail to validate if the latest policySnapshot is not found", func() {
				By("Deleting the policySnapshot")
				Expect(k8sClient.Delete(ctx, policySnapshot)).Should(Succeed())

				By("Validating the validation failed")
				wantStatus = generateFailedValidationStatus(updateRun, wantStatus)
				validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "no latest policy snapshot associated")

				By("Checking update run status metrics are emitted")
				validateUpdateRunMetricsEmitted(generateProgressingMetric(updateRun), generateFailedMetric(updateRun))
			})

			It("Should fail to validate if the latest policySnapshot has changed", func() {
				By("Deleting the old policySnapshot")
				Expect(k8sClient.Delete(ctx, policySnapshot)).Should(Succeed())

				By("Creating a new policySnapshot")
				newPolicySnapshot := generateTestClusterSchedulingPolicySnapshot(2, len(targetClusters))
				Expect(k8sClient.Create(ctx, newPolicySnapshot)).To(Succeed())

				By("Setting the latest policy snapshot condition as fully scheduled")
				meta.SetStatusCondition(&newPolicySnapshot.Status.Conditions, metav1.Condition{
					Type:               string(placementv1beta1.PolicySnapshotScheduled),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: newPolicySnapshot.Generation,
					Reason:             "scheduled",
				})
				Expect(k8sClient.Status().Update(ctx, newPolicySnapshot)).Should(Succeed(), "failed to update the policy snapshot condition")

				By("Validating the validation failed")
				wantStatus = generateFailedValidationStatus(updateRun, wantStatus)
				validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus,
					"the policy snapshot index used in the updateRun is outdated")

				By("Deleting the new policySnapshot")
				Expect(k8sClient.Delete(ctx, newPolicySnapshot)).Should(Succeed())

				By("Checking update run status metrics are emitted")
				validateUpdateRunMetricsEmitted(generateProgressingMetric(updateRun), generateFailedMetric(updateRun))
			})

			It("Should fail to validate if the cluster count has changed", func() {
				By("Updating the cluster count in the policySnapshot")
				policySnapshot.Annotations["kubernetes-fleet.io/number-of-clusters"] = strconv.Itoa(len(targetClusters) + 1)
				Expect(k8sClient.Update(ctx, policySnapshot)).Should(Succeed())

				By("Validating the validation failed")
				wantStatus = generateFailedValidationStatus(updateRun, wantStatus)
				validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus,
					"the cluster count initialized in the updateRun is outdated")

				By("Checking update run status metrics are emitted")
				validateUpdateRunMetricsEmitted(generateProgressingMetric(updateRun), generateFailedMetric(updateRun))
			})
		})

		Context("Test validateStagesStatus", func() {
			AfterEach(func() {
				By("Checking update run status metrics are emitted")
				validateUpdateRunMetricsEmitted(generateProgressingMetric(updateRun), generateFailedMetric(updateRun))
			})

			It("Should fail to validate if the UpdateStrategySnapshot is nil", func() {
				By("Updating the status.UpdateStrategySnapshot to nil")
				updateRun.Status.UpdateStrategySnapshot = nil
				Expect(k8sClient.Status().Update(ctx, updateRun)).Should(Succeed())

				By("Validating the validation failed")
				wantStatus = generateFailedValidationStatus(updateRun, wantStatus)
				wantStatus.UpdateStrategySnapshot = nil
				validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "the updateRun has nil updateStrategySnapshot")
			})

			It("Should fail to validate if the StagesStatus is nil", func() {
				By("Updating the status.StagesStatus to nil")
				updateRun.Status.StagesStatus = nil
				Expect(k8sClient.Status().Update(ctx, updateRun)).Should(Succeed())

				By("Validating the validation failed")
				wantStatus = generateFailedValidationStatus(updateRun, wantStatus)
				wantStatus.StagesStatus = nil
				validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "the updateRun has nil stagesStatus")
			})

			It("Should fail to validate if the DeletionStageStatus is nil", func() {
				By("Updating the status.DeletionStageStatus to nil")
				updateRun.Status.DeletionStageStatus = nil
				Expect(k8sClient.Status().Update(ctx, updateRun)).Should(Succeed())

				By("Validating the validation failed")
				wantStatus = generateFailedValidationStatus(updateRun, wantStatus)
				wantStatus.DeletionStageStatus = nil
				validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "the updateRun has nil deletionStageStatus")
			})

			It("Should fail to validate if the number of stages has changed", func() {
				By("Adding a stage to the updateRun status")
				updateRun.Status.UpdateStrategySnapshot.Stages = append(updateRun.Status.UpdateStrategySnapshot.Stages, placementv1beta1.StageConfig{
					Name: "stage3",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"group":  "dummy",
							"region": "no-exist",
						},
					},
				})
				Expect(k8sClient.Status().Update(ctx, updateRun)).Should(Succeed())

				By("Validating the validation failed")
				wantStatus = generateFailedValidationStatus(updateRun, wantStatus)
				wantStatus.UpdateStrategySnapshot.Stages = append(wantStatus.UpdateStrategySnapshot.Stages, placementv1beta1.StageConfig{
					Name: "stage3",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"group":  "dummy",
							"region": "no-exist",
						},
					},
				})
				validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "the number of stages in the updateRun has changed")
			})

			It("Should fail to validate if stage name has changed", func() {
				By("Changing the name of a stage")
				updateRun.Status.UpdateStrategySnapshot.Stages[0].Name = "stage3"
				Expect(k8sClient.Status().Update(ctx, updateRun)).Should(Succeed())

				By("Validating the validation failed")
				wantStatus = generateFailedValidationStatus(updateRun, wantStatus)
				wantStatus.UpdateStrategySnapshot.Stages[0].Name = "stage3"
				validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "index `0` stage name in the updateRun has changed")
			})

			It("Should fail to validate if the number of clusters has changed in a stage", func() {
				By("Changing 1st cluster's so that it's selected by the 1st stage")
				targetClusters[0].Labels["region"] = regionEastus
				Expect(k8sClient.Update(ctx, targetClusters[0])).Should(Succeed())

				By("Validating the validation failed")
				wantStatus = generateFailedValidationStatus(updateRun, wantStatus)
				validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "the number of clusters in index `0` stage has changed")
			})

			It("Should fail to validate if the cluster name has changed in a stage", func() {
				By("Changing the sorting key value to reorder the clusters")
				// Swap the index of cluster 1 and 3.
				targetClusters[1].Labels["index"], targetClusters[3].Labels["index"] =
					targetClusters[3].Labels["index"], targetClusters[1].Labels["index"]
				Expect(k8sClient.Update(ctx, targetClusters[1])).Should(Succeed())
				Expect(k8sClient.Update(ctx, targetClusters[3])).Should(Succeed())

				By("Validating the validation failed")
				wantStatus = generateFailedValidationStatus(updateRun, wantStatus)
				validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "the `3` cluster in the `0` stage has changed")
			})
		})
	})
	Context("Test determinePolicySnapshot for PickAll", func() {
		var updateRun *placementv1beta1.ClusterStagedUpdateRun
		var crp *placementv1beta1.ClusterResourcePlacement
		var policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot
		var updateStrategy *placementv1beta1.ClusterStagedUpdateStrategy
		var resourceBindings []*placementv1beta1.ClusterResourceBinding
		var targetClusters []*clusterv1beta1.MemberCluster
		var unscheduledClusters []*clusterv1beta1.MemberCluster
		var resourceSnapshot *placementv1beta1.ClusterResourceSnapshot
		var clusterResourceOverride *placementv1beta1.ClusterResourceOverrideSnapshot
		var wantStatus *placementv1beta1.UpdateRunStatus
		BeforeEach(func() {
			testUpdateRunName = "updaterun-" + utils.RandStr()
			testCRPName = "crp-" + utils.RandStr()
			testResourceSnapshotName = testCRPName + "-" + testResourceSnapshotIndex + "-snapshot"
			testUpdateStrategyName = "updatestrategy-" + utils.RandStr()
			testCROName = "cro-" + utils.RandStr()
			updateRunNamespacedName = types.NamespacedName{Name: testUpdateRunName}

			updateRun = generateTestClusterStagedUpdateRun()
			crp = generateTestClusterResourcePlacement()
			crp.Spec.Policy = &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			}
			updateStrategy = generateTestClusterStagedUpdateStrategy()
			clusterResourceOverride = generateTestClusterResourceOverride()
			resourceBindings, targetClusters, unscheduledClusters = generateTestClusterResourceBindingsAndClusters(1)
			policySnapshot = generateTestClusterSchedulingPolicySnapshot(1, len(targetClusters))
			policySnapshot.Spec.Policy = &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			}
			resourceSnapshot = generateTestClusterResourceSnapshot()

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

			By("Creating a new cluster resource override")
			Expect(k8sClient.Create(ctx, clusterResourceOverride)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization succeeded")
			initialized := generateSucceededInitializationStatus(crp, updateRun, policySnapshot, updateStrategy, clusterResourceOverride)
			wantStatus = generateExecutionStartedStatus(updateRun, initialized)
			validateClusterStagedUpdateRunStatus(ctx, updateRun, wantStatus, "")
		})

		AfterEach(func() {
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

			By("Deleting the clusterResourceOverride")
			Expect(k8sClient.Delete(ctx, clusterResourceOverride)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
			clusterResourceOverride = nil

			By("Checking the update run status metrics are removed")
			// No metrics are emitted as all are removed after updateRun is deleted.
			validateUpdateRunMetricsEmitted()
			resetUpdateRunMetrics()
		})

		It("Should not fail due to different cluster count if it's pickAll policy", func() {
			By("Setting the latest policy snapshot condition as fully scheduled")
			meta.SetStatusCondition(&policySnapshot.Status.Conditions, metav1.Condition{
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: policySnapshot.Generation,
				Reason:             "scheduled",
			})
			Expect(k8sClient.Status().Update(ctx, policySnapshot)).Should(Succeed(), "failed to update the policy snapshot condition")

			By("Validating the validation does not fail")
			validateClusterStagedUpdateRunStatusConsistently(ctx, updateRun, wantStatus, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateProgressingMetric(updateRun))
		})
	})
})

func validateClusterStagedUpdateRunStatus(
	ctx context.Context,
	updateRun *placementv1beta1.ClusterStagedUpdateRun,
	want *placementv1beta1.UpdateRunStatus,
	message string,
) {
	Eventually(func() error {
		if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
			return err
		}

		if diff := cmp.Diff(*want, updateRun.Status, cmpOptions...); diff != "" {
			return fmt.Errorf("status mismatch: (-want +got):\n%s", diff)
		}
		if message != "" {
			succeedCond := meta.FindStatusCondition(updateRun.Status.Conditions, string(placementv1beta1.StagedUpdateRunConditionSucceeded))
			if !strings.Contains(succeedCond.Message, message) {
				return fmt.Errorf("condition message mismatch: got %s, want %s", succeedCond.Message, message)
			}
		}
		return nil
	}, timeout, interval).Should(Succeed(), "failed to validate the clusterStagedUpdateRun status")
}

func validateClusterStagedUpdateRunStatusConsistently(
	ctx context.Context,
	updateRun *placementv1beta1.ClusterStagedUpdateRun,
	want *placementv1beta1.UpdateRunStatus,
	message string,
) {
	Consistently(func() error {
		if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
			return err
		}

		if diff := cmp.Diff(*want, updateRun.Status, cmpOptions...); diff != "" {
			return fmt.Errorf("status mismatch: (-want +got):\n%s", diff)
		}
		if message != "" {
			succeedCond := meta.FindStatusCondition(updateRun.Status.Conditions, string(placementv1beta1.StagedUpdateRunConditionSucceeded))
			if !strings.Contains(succeedCond.Message, message) {
				return fmt.Errorf("condition message mismatch: got %s, want %s", succeedCond.Message, message)
			}
		}
		return nil
	}, duration, interval).Should(Succeed(), "failed to validate the clusterStagedUpdateRun status consistently")
}

func generateFailedValidationStatus(
	updateRun *placementv1beta1.ClusterStagedUpdateRun,
	started *placementv1beta1.UpdateRunStatus,
) *placementv1beta1.UpdateRunStatus {
	started.Conditions[1] = generateFalseProgressingCondition(updateRun, placementv1beta1.StagedUpdateRunConditionProgressing, false)
	started.Conditions = append(started.Conditions, generateFalseCondition(updateRun, placementv1beta1.StagedUpdateRunConditionSucceeded))
	return started
}
