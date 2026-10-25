/*
Copyright 2026 The KubeFleet Authors.

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

package stagedupdaterun

import (
	"fmt"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

// A substring of the message that the controller reports when it completes the update on a member cluster whose
// placement binding is suspended (whether the binding is suspended before or during the update).
const bindingSuspendedMsgSubstr = "binding is suspended"

// Substrings of the messages that the controller reports when it fails to update a member cluster.
const (
	resourceSnapshotConflictMsgSubstr    = "resource snapshot name conflict"
	bindingNotFoundMsgSubstr             = "the binding cannot be found"
	bindingDeletingOrRetargetedMsgSubstr = "the binding has been marked for deletion or its cluster name does not match"
	clusterTimeoutMsgSubstr              = "has failed due to timeout"
)

// The format of the substring of the message that the controller reports when a stage fails as the cluster failures
// have exceeded the threshold; the placeholders are the number of clusters that have failed to update in the stage
// and the number of all the clusters in the stage.
const stageFailedMsgSubstrFmt = "%d out of %d clusters failed to update; total failures across stages have exceeded the threshold"

// The label of the test cases that take more than a minute to complete; use `--label-filter='!slow'` to skip them.
const slowTestLabel = "slow"

const (
	emptyStageAfterStageWaitTime = time.Second * 5

	// The resource snapshot name that a test case points a placement binding to, as if another staged update run
	// has rolled out to the member cluster.
	conflictingResourceSnapshotName = "conflicting-resource-snapshot"
	// The member cluster name that a test case retargets a placement binding to; no member cluster of this name
	// needs to exist.
	retargetedClusterName = "retargeted-cluster"
)

var _ = Describe("processing staged update runs", func() {
	Context("multiple stages, one cluster at a time", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member cluster 0 is the only canary cluster; member clusters 1, 2, and 3 are production clusters, which
		// the prod stage sorts by their order labels. That is, the rollout order is member cluster 0 (in the canary
		// stage), and then member clusters 2, 3, and 1 (in the prod stage).
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd, orderLabelKey: "30"},
			{envLabelKey: envLabelValueProd, orderLabelKey: "10"},
			{envLabelKey: envLabelValueProd, orderLabelKey: "20"},
		}

		// The canary stage, as it is expected to be once completed.
		wantCompletedCanaryStage := rolloutv1alpha1.PerStageStatus{
			StageName: canaryStageName,
			Clusters: []rolloutv1alpha1.PerClusterStatus{
				wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
			},
			Conditions:     perStageSucceededConds(),
			MaxConcurrency: ptr.To(int32(1)),
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueCanary},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
					SortingLabelKey: ptr.To(orderLabelKey),
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to the canary cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the canary cluster")
		})

		It("should not roll out to the production clusters while the canary cluster is being updated", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(appNamespaceName, []string{bindingNames[1], bindingNames[2], bindingNames[3]}, previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the production clusters unexpectedly")
		})

		It("should report that the canary cluster is being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 2, 3, 1),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can mark the canary cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("should start rolling out to the first production cluster (by sorting order)", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[2], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the first production cluster")
		})

		It("should not roll out to other production clusters while the first one is being updated", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(appNamespaceName, []string{bindingNames[3], bindingNames[1]}, previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to other production clusters unexpectedly")
		})

		It("should report that the canary stage has completed and the first production cluster is being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 3),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can mark the first production cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[2], snapshotName)
		})

		It("should start rolling out to the second production cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[3], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the second production cluster")
		})

		It("should not roll out to the last production cluster while the second one is being updated", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[1], previousResourceSnapshotName)
			Consistently(bindingUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the last production cluster unexpectedly")
		})

		It("should report that the second production cluster is being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 3, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can mark the second production cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[3], snapshotName)
		})

		It("should start rolling out to the last production cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the last production cluster")
		})

		It("should report that the last production cluster is being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 3, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterStartedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can mark the last production cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[1], snapshotName)
		})

		It("should complete the staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 3, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")

			stagesCompletedInOrderActual := stagedUpdateRunStagesCompletedInOrderActual(appNamespaceName, runName)
			Expect(stagesCompletedInOrderActual()).To(Succeed(), "Failed to complete the stages in order")
		})

		It("should have rolled out to all the member clusters", func() {
			bindingsUpdatedActual := placementBindingsResourceSnapshotActual(appNamespaceName, bindingNames, snapshotName)
			Expect(bindingsUpdatedActual()).To(Succeed(), "Failed to roll out to all the member clusters")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("concurrent updates within a stage", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member clusters 0-2 are canary clusters, which the canary stage updates two at a time; member clusters
		// 3-5 are production clusters, which the prod stage updates all at once.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 6)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
			{envLabelKey: envLabelValueProd},
			{envLabelKey: envLabelValueProd},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueCanary},
					},
					MaxConcurrency: ptr.To(intstr.FromInt32(2)),
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
					MaxConcurrency: ptr.To(intstr.FromString("100%")),
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to two canary clusters at the same time", func() {
			bindingsUpdatedActual := placementBindingsResourceSnapshotActual(appNamespaceName, bindingNames[0:2], snapshotName)
			Eventually(bindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to two canary clusters")
		})

		It("should not roll out to more member clusters while two canary clusters are being updated", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(appNamespaceName, bindingNames[2:], previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to more member clusters unexpectedly")
		})

		It("should report that two canary clusters are being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 2),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(2)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 3, 4, 5),
						MaxConcurrency: ptr.To(int32(3)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can mark one of the two canary clusters as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[1], snapshotName)
		})

		It("should start rolling out to the last canary cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[2], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the last canary cluster")
		})

		It("should not roll out to the production clusters while canary clusters are being updated", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(appNamespaceName, bindingNames[3:], previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the production clusters unexpectedly")
		})

		It("can mark the other canary clusters as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[2], snapshotName)
		})

		It("should start rolling out to all the production clusters at the same time", func() {
			bindingsUpdatedActual := placementBindingsResourceSnapshotActual(appNamespaceName, bindingNames[3:], snapshotName)
			Eventually(bindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to all the production clusters")
		})

		It("should report that all the production clusters are being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(2)),
					},
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 3, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 4, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 5, perClusterStartedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(3)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can mark all the production clusters as available", func() {
			for _, bindingName := range bindingNames[3:] {
				markPlacementBindingAsAvailable(appNamespaceName, bindingName, snapshotName)
			}
		})

		It("should complete the staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(2)),
					},
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 3, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 4, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 5, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(3)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")

			stagesCompletedInOrderActual := stagedUpdateRunStagesCompletedInOrderActual(appNamespaceName, runName)
			Expect(stagesCompletedInOrderActual()).To(Succeed(), "Failed to complete the stages in order")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("placement binding availability detection", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// The status of the staged update run while the (only) member cluster is being updated.
		wantInProgressStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
			Conditions: []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
			Stages: []rolloutv1alpha1.PerStageStatus{
				{
					StageName: restStageName,
					Clusters: []rolloutv1alpha1.PerClusterStatus{
						wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
					},
					Conditions:     []metav1.Condition{perStageStartedCond()},
					MaxConcurrency: ptr.To(int32(1)),
				},
			},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			wantInProgressStatus.ResourceSnapshotNameToRollout = snapshotName
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("should report that the member cluster is being updated", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantInProgressStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can mark the placement binding as synchronized and available, observed at a previous generation", func() {
			setPlacementBindingConditions(appNamespaceName, bindingNames[0], snapshotName, metav1.ConditionTrue, metav1.ConditionTrue, -1)
		})

		It("should not consider the member cluster as updated (stale observation)", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantInProgressStatus)
			Consistently(rolloutStatusActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Considered the member cluster as updated unexpectedly")
		})

		It("can mark the placement binding as synchronized but not available, observed at the current generation", func() {
			setPlacementBindingConditions(appNamespaceName, bindingNames[0], snapshotName, metav1.ConditionTrue, metav1.ConditionFalse, 0)
		})

		It("should not consider the member cluster as updated (not available)", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantInProgressStatus)
			Consistently(rolloutStatusActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Considered the member cluster as updated unexpectedly")
		})

		It("can mark the placement binding as synchronized and available, observed at the current generation", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("should complete the staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("placement bindings already up to date", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// The placement binding of member cluster 0 already points to the resource snapshot that the staged update
		// run is going to roll out, and has been marked as available; the one of member cluster 1 points to the
		// previous resource snapshot.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			memberCluster0 := createMemberCluster(memberClusterNames[0], nil)
			binding0 := createPlacementBinding(appNamespaceName, bindingNames[0], policyName, memberClusterNames[0], snapshotName)
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
			memberCluster1 := createMemberCluster(memberClusterNames[1], nil)
			binding1 := createPlacementBinding(appNamespaceName, bindingNames[1], policyName, memberClusterNames[1], previousResourceSnapshotName)
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(policy, memberCluster0, binding0, memberCluster1, binding1, strategy)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should complete the update on member cluster 0 and start rolling out to member cluster 1", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 1")
		})

		It("should report that member cluster 0 has been updated and member cluster 1 is being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterStartedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can mark member cluster 1 as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[1], snapshotName)
		})

		It("should complete the staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		It("should not have updated the placement binding of member cluster 0", func() {
			// The placement binding has stayed at its initial generation, i.e., its spec has never been updated.
			bindingGenerationActual := placementBindingGenerationActual(appNamespaceName, bindingNames[0], 1)
			Expect(bindingGenerationActual()).To(Succeed(), "The placement binding of member cluster 0 has been updated unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("suspended placement bindings", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// The placement binding of member cluster 0 is suspended before the rollout starts; the one of member
		// cluster 1 is suspended while the member cluster is being updated.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil})
			updatePlacementBindingSuspended(appNamespaceName, bindingNames[0], true)
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should roll out to member cluster 0, and move on to member cluster 1 right away", func() {
			// The placement binding of member cluster 0 is suspended, so the controller does not wait for it to
			// become available.
			bindingsUpdatedActual := placementBindingsResourceSnapshotActual(appNamespaceName, bindingNames, snapshotName)
			Eventually(bindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member clusters")
		})

		It("can suspend the placement binding of member cluster 1", func() {
			updatePlacementBindingSuspended(appNamespaceName, bindingNames[1], true)
		})

		It("should complete the staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		It("should report that the updates have completed as the placement bindings are suspended", func() {
			clusterCompletedMsgActual := perClusterConditionMessageActual(appNamespaceName, runName, memberClusterNames[0],
				rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted, bindingSuspendedMsgSubstr)
			Expect(clusterCompletedMsgActual()).To(Succeed(), "Failed to report that the placement binding of member cluster 0 is suspended")

			clusterCompletedMsgActual = perClusterConditionMessageActual(appNamespaceName, runName, memberClusterNames[1],
				rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted, bindingSuspendedMsgSubstr)
			Expect(clusterCompletedMsgActual()).To(Succeed(), "Failed to report that the placement binding of member cluster 1 is suspended")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("empty stage with after-stage tasks", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member cluster 0 is a canary cluster, and member cluster 1 is a production cluster; the empty stage, which
		// comes between the canary stage and the prod stage, includes no clusters, but has an after-stage timed wait
		// task.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
		}

		// The canary stage, as it is expected to be once completed.
		wantCompletedCanaryStage := rolloutv1alpha1.PerStageStatus{
			StageName: canaryStageName,
			Clusters: []rolloutv1alpha1.PerClusterStatus{
				wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
			},
			Conditions:     perStageSucceededConds(),
			MaxConcurrency: ptr.To(int32(1)),
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueCanary},
					},
				},
				{
					Name: emptyStageName,
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: emptyStageAfterStageWaitTime},
						},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to the canary cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the canary cluster")
		})

		It("can mark the canary cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("should start the empty stage and its after-stage timed wait task", func() {
			// The controller persists the status right after it starts the timed wait task, so the status is
			// observable for the whole wait time.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: emptyStageName,
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							{
								Type:       rolloutv1alpha1.StageTaskTypeTimedWait,
								WaitTime:   &metav1.Duration{Duration: emptyStageAfterStageWaitTime},
								Conditions: []metav1.Condition{perStageTaskTimedWaitStartedCond()},
							},
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 1),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to start the empty stage")
		})

		It("should start rolling out to the production cluster once the wait time has elapsed", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to roll out to the production cluster")
		})

		It("can mark the production cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[1], snapshotName)
		})

		It("should complete the staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: emptyStageName,
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							{
								Type:       rolloutv1alpha1.StageTaskTypeTimedWait,
								WaitTime:   &metav1.Duration{Duration: emptyStageAfterStageWaitTime},
								Conditions: []metav1.Condition{perStageTaskTimedWaitStartedCond(), perStageTaskWaitTimeElapsedCond()},
							},
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")

			stagesCompletedInOrderActual := stagedUpdateRunStagesCompletedInOrderActual(appNamespaceName, runName)
			Expect(stagesCompletedInOrderActual()).To(Succeed(), "Failed to complete the stages in order")

			stageLastedActual := stagedUpdateRunStageLastedAtLeastActual(appNamespaceName, runName, emptyStageName, emptyStageAfterStageWaitTime)
			Expect(stageLastedActual()).To(Succeed(), "Failed to wait for the after-stage timed wait task of the empty stage")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("resource snapshot conflicts", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// While member cluster 0 is being updated, its placement binding is pointed to another resource snapshot,
		// as if another staged update run has rolled out to it.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			// The staged update run uses the default failure policy, which allows one cluster failure.
			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to member cluster 0", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 0")
		})

		It("should report that member cluster 0 is being updated", func() {
			// The conflict is injected only after the persisted status shows that the update has started on the
			// member cluster; otherwise, a reconciliation that reads a staged update run which has yet to record the
			// start might consider the placement binding as not updated yet, and point it back to the resource
			// snapshot to roll out.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can point the placement binding of member cluster 0 to another resource snapshot", func() {
			updatePlacementBindingSpec(appNamespaceName, bindingNames[0], func(spec *placementv1alpha1.PlacementBindingSpec) {
				spec.ResourceSnapshotName = conflictingResourceSnapshotName
			})
		})

		It("should move on to member cluster 1", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 1")
		})

		It("can mark member cluster 1 as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[1], snapshotName)
		})

		It("should complete the staged update run with partial success", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonPartiallySucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateFailedConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						Conditions: []metav1.Condition{
							perStageStartedCond(),
							perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonPartiallySucceeded),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		It("should report the conflict on member cluster 0", func() {
			clusterCompletedMsgActual := perClusterConditionMessageActual(appNamespaceName, runName, memberClusterNames[0],
				rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted, resourceSnapshotConflictMsgSubstr)
			Expect(clusterCompletedMsgActual()).To(Succeed(), "Failed to report the conflict on member cluster 0")
		})

		It("should not point the placement binding of member cluster 0 back to the resource snapshot to roll out", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], conflictingResourceSnapshotName)
			Expect(bindingUntouchedActual()).To(Succeed(), "The placement binding of member cluster 0 has been updated unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("placement bindings deleted after initialization", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// The placement binding of member cluster 0 is deleted after the staged update run has been initialized.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			// The staged update run uses the default failure policy, which allows one cluster failure.
			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the staged update run", func() {
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      restStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 1),
					MaxConcurrency: ptr.To(int32(1)),
				},
			})
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
		})

		It("can delete the placement binding of member cluster 0", func() {
			// Note that removeObjects waits until the cache observes the deletion.
			removeObjects(newPlacementBindingObj(appNamespaceName, bindingNames[0]))
		})

		It("can resume the staged update run", func() {
			updateStagedUpdateRunSuspended(appNamespaceName, runName, false)
		})

		It("should move on to member cluster 1", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 1")
		})

		It("can mark member cluster 1 as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[1], snapshotName)
		})

		It("should complete the staged update run with partial success", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunResumedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonPartiallySucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							// The update fails before it starts, as the placement binding cannot be found.
							wantPerClusterStatus(memberClusterNames, bindingNames, 0,
								perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed)),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						Conditions: []metav1.Condition{
							perStageStartedCond(),
							perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonPartiallySucceeded),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		It("should report that the placement binding of member cluster 0 cannot be found", func() {
			clusterCompletedMsgActual := perClusterConditionMessageActual(appNamespaceName, runName, memberClusterNames[0],
				rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted, bindingNotFoundMsgSubstr)
			Expect(clusterCompletedMsgActual()).To(Succeed(), "Failed to report that the placement binding of member cluster 0 cannot be found")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("placement bindings marked for deletion or retargeted after initialization", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// After the staged update run has been initialized, the placement binding of member cluster 0 is marked
		// for deletion, and the one of member cluster 1 is retargeted to another member cluster.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 3)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil, nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			// The staged update run allows two cluster failures.
			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
				FailurePolicy: &rolloutv1alpha1.StagedUpdateRunFailurePolicy{
					MaxAllowedClusterFailures: ptr.To(int32(2)),
				},
			})
		})

		It("should initialize the staged update run", func() {
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      restStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 1, 2),
					MaxConcurrency: ptr.To(int32(1)),
				},
			})
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
		})

		It("can mark the placement binding of member cluster 0 for deletion", func() {
			markObjectForDeletion(newPlacementBindingObj(appNamespaceName, bindingNames[0]))
		})

		It("can retarget the placement binding of member cluster 1 to another member cluster", func() {
			updatePlacementBindingSpec(appNamespaceName, bindingNames[1], func(spec *placementv1alpha1.PlacementBindingSpec) {
				spec.ClusterName = retargetedClusterName
			})
		})

		It("can resume the staged update run", func() {
			updateStagedUpdateRunSuspended(appNamespaceName, runName, false)
		})

		It("should move on to member cluster 2", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[2], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 2")
		})

		It("can mark member cluster 2 as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[2], snapshotName)
		})

		It("should complete the staged update run with partial success", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunResumedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonPartiallySucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							// The updates fail before they start, as the placement bindings no longer qualify.
							wantPerClusterStatus(memberClusterNames, bindingNames, 0,
								perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed)),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1,
								perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed)),
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
						},
						Conditions: []metav1.Condition{
							perStageStartedCond(),
							perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonPartiallySucceeded),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		It("should report why the updates on member clusters 0 and 1 have failed", func() {
			for _, idx := range []int{0, 1} {
				clusterCompletedMsgActual := perClusterConditionMessageActual(appNamespaceName, runName, memberClusterNames[idx],
					rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted, bindingDeletingOrRetargetedMsgSubstr)
				Expect(clusterCompletedMsgActual()).To(Succeed(), "Failed to report why the update on member cluster %d has failed", idx)
			}
		})

		It("should not roll out to member clusters 0 and 1", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(appNamespaceName, bindingNames[0:2], previousResourceSnapshotName)
			Expect(bindingsUntouchedActual()).To(Succeed(), "Rolled out to member clusters 0 and 1 unexpectedly")
		})

		AfterAll(func() {
			removeTestFinalizer(newPlacementBindingObj(appNamespaceName, bindingNames[0]))
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("cluster failures exceeding the threshold within a stage", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member clusters 0-2 are canary clusters, which the canary stage updates two at a time; member cluster 3 is
		// a production cluster. The staged update run tolerates no cluster failures: the update on member cluster 0
		// fails while member cluster 1 is still being updated (its placement binding never becomes available).
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueCanary},
					},
					MaxConcurrency: ptr.To(intstr.FromInt32(2)),
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				FailurePolicy: &rolloutv1alpha1.StagedUpdateRunFailurePolicy{
					MaxAllowedClusterFailures: ptr.To(int32(0)),
				},
			})
		})

		It("should start rolling out to member clusters 0 and 1", func() {
			bindingsUpdatedActual := placementBindingsResourceSnapshotActual(appNamespaceName, bindingNames[0:2], snapshotName)
			Eventually(bindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member clusters 0 and 1")
		})

		It("should report that member clusters 0 and 1 are being updated", func() {
			// The conflict below is injected only after the persisted status shows that the updates have started.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 2),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(2)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 3),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can point the placement binding of member cluster 0 to another resource snapshot", func() {
			updatePlacementBindingSpec(appNamespaceName, bindingNames[0], func(spec *placementv1alpha1.PlacementBindingSpec) {
				spec.ResourceSnapshotName = conflictingResourceSnapshotName
			})
		})

		It("should fail the staged update run, canceling the in-flight update on member cluster 1", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonFailed),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateFailedConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1,
								perClusterStartedCond(),
								perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonCanceled)),
							// The update on member cluster 2 never starts.
							wantPerClusterStatus(memberClusterNames, bindingNames, 2),
						},
						Conditions: []metav1.Condition{
							perStageStartedCond(),
							perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonFailed),
						},
						MaxConcurrency: ptr.To(int32(2)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 3),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the staged update run as failed")
		})

		It("should report that the canary stage has failed as the cluster failures have exceeded the threshold", func() {
			// The message counts only the clusters that have failed to update (i.e., not the canceled ones).
			stageCompletedMsgActual := perStageConditionMessageActual(appNamespaceName, runName, canaryStageName,
				rolloutv1alpha1.StagedUpdateRunPerStageCondTypeCompleted, fmt.Sprintf(stageFailedMsgSubstrFmt, 1, 3))
			Expect(stageCompletedMsgActual()).To(Succeed(), "Failed to report why the canary stage has failed")
		})

		It("should not roll out to member clusters 2 and 3", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(appNamespaceName, bindingNames[2:], previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to member clusters 2 and 3 unexpectedly")
		})

		It("should remove the cleanup finalizer from the staged update run", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(appNamespaceName, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
		})

		It("should release the binding manager role of the placement policy", func() {
			roleReleasedActual := bindingManagerRoleReleasedActual(appNamespaceName, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("cluster failures exceeding the threshold across stages", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member clusters 0 and 1 are canary clusters; member clusters 2 and 3 are production clusters. The staged
		// update run tolerates one cluster failure: the update on member cluster 0 fails in the canary stage, and
		// then the one on member cluster 2 fails in the prod stage.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
			{envLabelKey: envLabelValueProd},
		}

		// The canary stage, as it is expected to be once completed.
		wantCompletedCanaryStage := rolloutv1alpha1.PerStageStatus{
			StageName: canaryStageName,
			Clusters: []rolloutv1alpha1.PerClusterStatus{
				wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateFailedConds()...),
				wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
			},
			Conditions: []metav1.Condition{
				perStageStartedCond(),
				perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonPartiallySucceeded),
			},
			MaxConcurrency: ptr.To(int32(1)),
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueCanary},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			// The staged update run uses the default failure policy, which allows one cluster failure.
			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to member cluster 0", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 0")
		})

		It("should report that member cluster 0 is being updated", func() {
			// The conflict below is injected only after the persisted status shows that the update has started.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 2, 3),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can point the placement binding of member cluster 0 to another resource snapshot", func() {
			updatePlacementBindingSpec(appNamespaceName, bindingNames[0], func(spec *placementv1alpha1.PlacementBindingSpec) {
				spec.ResourceSnapshotName = conflictingResourceSnapshotName
			})
		})

		It("should move on to member cluster 1", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 1")
		})

		It("can mark member cluster 1 as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[1], snapshotName)
		})

		It("should start rolling out to member cluster 2", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[2], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 2")
		})

		It("should report that the canary stage has completed with partial success and member cluster 2 is being updated", func() {
			// Once this status is persisted, the controller processes the prod stage in later reconciliations, where
			// it counts the cluster failure in the canary stage from the persisted status; the conflict below is
			// injected only after this point.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 3),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can point the placement binding of member cluster 2 to another resource snapshot", func() {
			updatePlacementBindingSpec(appNamespaceName, bindingNames[2], func(spec *placementv1alpha1.PlacementBindingSpec) {
				spec.ResourceSnapshotName = conflictingResourceSnapshotName
			})
		})

		It("should fail the staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonFailed),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateFailedConds()...),
							// The update on member cluster 3 never starts.
							wantPerClusterStatus(memberClusterNames, bindingNames, 3),
						},
						Conditions: []metav1.Condition{
							perStageStartedCond(),
							perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonFailed),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the staged update run as failed")
		})

		It("should report that the prod stage has failed as the cluster failures across stages have exceeded the threshold", func() {
			stageCompletedMsgActual := perStageConditionMessageActual(appNamespaceName, runName, prodStageName,
				rolloutv1alpha1.StagedUpdateRunPerStageCondTypeCompleted, fmt.Sprintf(stageFailedMsgSubstrFmt, 1, 2))
			Expect(stageCompletedMsgActual()).To(Succeed(), "Failed to report why the prod stage has failed")
		})

		It("should not roll out to member cluster 3", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[3], previousResourceSnapshotName)
			Consistently(bindingUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to member cluster 3 unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("processing time budget expiries", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// The (only) member cluster stays unavailable for multiple processing time budgets, so that the controller
		// picks up the in-flight update in new reconciliations.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// The status of the staged update run while the member cluster is being updated.
		wantInProgressStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
			Conditions: []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
			Stages: []rolloutv1alpha1.PerStageStatus{
				{
					StageName: restStageName,
					Clusters: []rolloutv1alpha1.PerClusterStatus{
						wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
					},
					Conditions:     []metav1.Condition{perStageStartedCond()},
					MaxConcurrency: ptr.To(int32(1)),
				},
			},
		}

		// The placement binding is created at generation 1, and is bumped to generation 2 when the controller points it
		// to the resource snapshot to roll out.
		wantBindingGeneration := int64(2)

		var startedCondLTT metav1.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			wantInProgressStatus.ResourceSnapshotNameToRollout = snapshotName
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("should report that the member cluster is being updated once the processing time budget runs out", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantInProgressStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")

			startedCond, err := getPerClusterCondition(appNamespaceName, runName, memberClusterNames[0], rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeStarted)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the Started condition of the member cluster")
			Expect(startedCond).ToNot(BeNil(), "The member cluster has no Started condition")
			startedCondLTT = startedCond.LastTransitionTime
		})

		It("should keep waiting for the member cluster across processing time budget expiries", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantInProgressStatus)
			Consistently(rolloutStatusActual, consistentlyDurationAcrossBudgetExpiries, consistentlyInterval).Should(Succeed(), "Failed to keep waiting for the member cluster")
		})

		It("should keep the update on the member cluster as it started", func() {
			// The Started condition is not reset (which would also reset the wait time limit of the cluster).
			startedCond, err := getPerClusterCondition(appNamespaceName, runName, memberClusterNames[0], rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeStarted)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the Started condition of the member cluster")
			Expect(startedCond).ToNot(BeNil(), "The member cluster has no Started condition")
			Expect(startedCond.LastTransitionTime.Equal(&startedCondLTT)).To(BeTrue(),
				"The Started condition of the member cluster has been reset (last transition time: %v, want %v)", startedCond.LastTransitionTime, startedCondLTT)

			// The placement binding is not updated again.
			bindingGenerationActual := placementBindingGenerationActual(appNamespaceName, bindingNames[0], wantBindingGeneration)
			Expect(bindingGenerationActual()).To(Succeed(), "The placement binding has been updated again unexpectedly")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("should complete the staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		It("should not have updated the placement binding again", func() {
			bindingGenerationActual := placementBindingGenerationActual(appNamespaceName, bindingNames[0], wantBindingGeneration)
			Expect(bindingGenerationActual()).To(Succeed(), "The placement binding has been updated again unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})
})

var _ = Describe("processing cluster staged update runs", func() {
	Context("multiple stages, one cluster at a time", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member cluster 0 is the only canary cluster; member clusters 1, 2, and 3 are production clusters, which
		// the prod stage sorts by their order labels. That is, the rollout order is member cluster 0 (in the canary
		// stage), and then member clusters 2, 3, and 1 (in the prod stage).
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd, orderLabelKey: "30"},
			{envLabelKey: envLabelValueProd, orderLabelKey: "10"},
			{envLabelKey: envLabelValueProd, orderLabelKey: "20"},
		}

		// The canary stage, as it is expected to be once completed.
		wantCompletedCanaryStage := rolloutv1alpha1.PerStageStatus{
			StageName: canaryStageName,
			Clusters: []rolloutv1alpha1.PerClusterStatus{
				wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
			},
			Conditions:     perStageSucceededConds(),
			MaxConcurrency: ptr.To(int32(1)),
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueCanary},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
					SortingLabelKey: ptr.To(orderLabelKey),
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to the canary cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the canary cluster")
		})

		It("should not roll out to the production clusters while the canary cluster is being updated", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, []string{bindingNames[1], bindingNames[2], bindingNames[3]}, previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the production clusters unexpectedly")
		})

		It("should report that the canary cluster is being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 2, 3, 1),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can mark the canary cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("should start rolling out to the first production cluster (by sorting order)", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[2], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the first production cluster")
		})

		It("should not roll out to other production clusters while the first one is being updated", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, []string{bindingNames[3], bindingNames[1]}, previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to other production clusters unexpectedly")
		})

		It("should report that the canary stage has completed and the first production cluster is being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 3),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can mark the first production cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[2], snapshotName)
		})

		It("should start rolling out to the second production cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[3], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the second production cluster")
		})

		It("should not roll out to the last production cluster while the second one is being updated", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[1], previousResourceSnapshotName)
			Consistently(bindingUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the last production cluster unexpectedly")
		})

		It("should report that the second production cluster is being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 3, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can mark the second production cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[3], snapshotName)
		})

		It("should start rolling out to the last production cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the last production cluster")
		})

		It("should report that the last production cluster is being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 3, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterStartedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can mark the last production cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[1], snapshotName)
		})

		It("should complete the cluster staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 3, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")

			stagesCompletedInOrderActual := stagedUpdateRunStagesCompletedInOrderActual(clusterScopedNS, runName)
			Expect(stagesCompletedInOrderActual()).To(Succeed(), "Failed to complete the stages in order")
		})

		It("should have rolled out to all the member clusters", func() {
			bindingsUpdatedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, bindingNames, snapshotName)
			Expect(bindingsUpdatedActual()).To(Succeed(), "Failed to roll out to all the member clusters")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("concurrent updates within a stage", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member clusters 0-2 are canary clusters, which the canary stage updates two at a time; member clusters
		// 3-5 are production clusters, which the prod stage updates all at once.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 6)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
			{envLabelKey: envLabelValueProd},
			{envLabelKey: envLabelValueProd},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueCanary},
					},
					MaxConcurrency: ptr.To(intstr.FromInt32(2)),
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
					MaxConcurrency: ptr.To(intstr.FromString("100%")),
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to two canary clusters at the same time", func() {
			bindingsUpdatedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, bindingNames[0:2], snapshotName)
			Eventually(bindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to two canary clusters")
		})

		It("should not roll out to more member clusters while two canary clusters are being updated", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, bindingNames[2:], previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to more member clusters unexpectedly")
		})

		It("should report that two canary clusters are being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 2),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(2)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 3, 4, 5),
						MaxConcurrency: ptr.To(int32(3)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can mark one of the two canary clusters as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[1], snapshotName)
		})

		It("should start rolling out to the last canary cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[2], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the last canary cluster")
		})

		It("should not roll out to the production clusters while canary clusters are being updated", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, bindingNames[3:], previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the production clusters unexpectedly")
		})

		It("can mark the other canary clusters as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[2], snapshotName)
		})

		It("should start rolling out to all the production clusters at the same time", func() {
			bindingsUpdatedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, bindingNames[3:], snapshotName)
			Eventually(bindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to all the production clusters")
		})

		It("should report that all the production clusters are being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(2)),
					},
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 3, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 4, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 5, perClusterStartedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(3)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can mark all the production clusters as available", func() {
			for _, bindingName := range bindingNames[3:] {
				markPlacementBindingAsAvailable(clusterScopedNS, bindingName, snapshotName)
			}
		})

		It("should complete the cluster staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(2)),
					},
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 3, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 4, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 5, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(3)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")

			stagesCompletedInOrderActual := stagedUpdateRunStagesCompletedInOrderActual(clusterScopedNS, runName)
			Expect(stagesCompletedInOrderActual()).To(Succeed(), "Failed to complete the stages in order")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("placement binding availability detection", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// The status of the cluster staged update run while the (only) member cluster is being updated.
		wantInProgressStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
			Conditions: []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
			Stages: []rolloutv1alpha1.PerStageStatus{
				{
					StageName: restStageName,
					Clusters: []rolloutv1alpha1.PerClusterStatus{
						wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
					},
					Conditions:     []metav1.Condition{perStageStartedCond()},
					MaxConcurrency: ptr.To(int32(1)),
				},
			},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			wantInProgressStatus.ResourceSnapshotNameToRollout = snapshotName
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("should report that the member cluster is being updated", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantInProgressStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can mark the placement binding as synchronized and available, observed at a previous generation", func() {
			setPlacementBindingConditions(clusterScopedNS, bindingNames[0], snapshotName, metav1.ConditionTrue, metav1.ConditionTrue, -1)
		})

		It("should not consider the member cluster as updated (stale observation)", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantInProgressStatus)
			Consistently(rolloutStatusActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Considered the member cluster as updated unexpectedly")
		})

		It("can mark the placement binding as synchronized but not available, observed at the current generation", func() {
			setPlacementBindingConditions(clusterScopedNS, bindingNames[0], snapshotName, metav1.ConditionTrue, metav1.ConditionFalse, 0)
		})

		It("should not consider the member cluster as updated (not available)", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantInProgressStatus)
			Consistently(rolloutStatusActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Considered the member cluster as updated unexpectedly")
		})

		It("can mark the placement binding as synchronized and available, observed at the current generation", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("should complete the cluster staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("placement bindings already up to date", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// The placement binding of member cluster 0 already points to the resource snapshot that the staged update
		// run is going to roll out, and has been marked as available; the one of member cluster 1 points to the
		// previous resource snapshot.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			memberCluster0 := createMemberCluster(memberClusterNames[0], nil)
			binding0 := createPlacementBinding(clusterScopedNS, bindingNames[0], policyName, memberClusterNames[0], snapshotName)
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
			memberCluster1 := createMemberCluster(memberClusterNames[1], nil)
			binding1 := createPlacementBinding(clusterScopedNS, bindingNames[1], policyName, memberClusterNames[1], previousResourceSnapshotName)
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(policy, memberCluster0, binding0, memberCluster1, binding1, strategy)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should complete the update on member cluster 0 and start rolling out to member cluster 1", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 1")
		})

		It("should report that member cluster 0 has been updated and member cluster 1 is being updated", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterStartedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can mark member cluster 1 as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[1], snapshotName)
		})

		It("should complete the cluster staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		It("should not have updated the placement binding of member cluster 0", func() {
			// The placement binding has stayed at its initial generation, i.e., its spec has never been updated.
			bindingGenerationActual := placementBindingGenerationActual(clusterScopedNS, bindingNames[0], 1)
			Expect(bindingGenerationActual()).To(Succeed(), "The placement binding of member cluster 0 has been updated unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("suspended placement bindings", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// The placement binding of member cluster 0 is suspended before the rollout starts; the one of member
		// cluster 1 is suspended while the member cluster is being updated.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil})
			updatePlacementBindingSuspended(clusterScopedNS, bindingNames[0], true)
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should roll out to member cluster 0, and move on to member cluster 1 right away", func() {
			// The placement binding of member cluster 0 is suspended, so the controller does not wait for it to
			// become available.
			bindingsUpdatedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, bindingNames, snapshotName)
			Eventually(bindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member clusters")
		})

		It("can suspend the placement binding of member cluster 1", func() {
			updatePlacementBindingSuspended(clusterScopedNS, bindingNames[1], true)
		})

		It("should complete the cluster staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		It("should report that the updates have completed as the placement bindings are suspended", func() {
			clusterCompletedMsgActual := perClusterConditionMessageActual(clusterScopedNS, runName, memberClusterNames[0],
				rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted, bindingSuspendedMsgSubstr)
			Expect(clusterCompletedMsgActual()).To(Succeed(), "Failed to report that the placement binding of member cluster 0 is suspended")

			clusterCompletedMsgActual = perClusterConditionMessageActual(clusterScopedNS, runName, memberClusterNames[1],
				rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted, bindingSuspendedMsgSubstr)
			Expect(clusterCompletedMsgActual()).To(Succeed(), "Failed to report that the placement binding of member cluster 1 is suspended")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("empty stage with after-stage tasks", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member cluster 0 is a canary cluster, and member cluster 1 is a production cluster; the empty stage, which
		// comes between the canary stage and the prod stage, includes no clusters, but has an after-stage timed wait
		// task.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
		}

		// The canary stage, as it is expected to be once completed.
		wantCompletedCanaryStage := rolloutv1alpha1.PerStageStatus{
			StageName: canaryStageName,
			Clusters: []rolloutv1alpha1.PerClusterStatus{
				wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
			},
			Conditions:     perStageSucceededConds(),
			MaxConcurrency: ptr.To(int32(1)),
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueCanary},
					},
				},
				{
					Name: emptyStageName,
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: emptyStageAfterStageWaitTime},
						},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to the canary cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the canary cluster")
		})

		It("can mark the canary cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("should start the empty stage and its after-stage timed wait task", func() {
			// The controller persists the status right after it starts the timed wait task, so the status is
			// observable for the whole wait time.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: emptyStageName,
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							{
								Type:       rolloutv1alpha1.StageTaskTypeTimedWait,
								WaitTime:   &metav1.Duration{Duration: emptyStageAfterStageWaitTime},
								Conditions: []metav1.Condition{perStageTaskTimedWaitStartedCond()},
							},
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 1),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to start the empty stage")
		})

		It("should start rolling out to the production cluster once the wait time has elapsed", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to roll out to the production cluster")
		})

		It("can mark the production cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[1], snapshotName)
		})

		It("should complete the cluster staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: emptyStageName,
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							{
								Type:       rolloutv1alpha1.StageTaskTypeTimedWait,
								WaitTime:   &metav1.Duration{Duration: emptyStageAfterStageWaitTime},
								Conditions: []metav1.Condition{perStageTaskTimedWaitStartedCond(), perStageTaskWaitTimeElapsedCond()},
							},
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")

			stagesCompletedInOrderActual := stagedUpdateRunStagesCompletedInOrderActual(clusterScopedNS, runName)
			Expect(stagesCompletedInOrderActual()).To(Succeed(), "Failed to complete the stages in order")

			stageLastedActual := stagedUpdateRunStageLastedAtLeastActual(clusterScopedNS, runName, emptyStageName, emptyStageAfterStageWaitTime)
			Expect(stageLastedActual()).To(Succeed(), "Failed to wait for the after-stage timed wait task of the empty stage")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("resource snapshot conflicts", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// While member cluster 0 is being updated, its placement binding is pointed to another resource snapshot,
		// as if another cluster staged update run has rolled out to it.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			// The cluster staged update run uses the default failure policy, which allows one cluster failure.
			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to member cluster 0", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 0")
		})

		It("should report that member cluster 0 is being updated", func() {
			// The conflict is injected only after the persisted status shows that the update has started on the
			// member cluster; otherwise, a reconciliation that reads a cluster staged update run which has yet to record the
			// start might consider the placement binding as not updated yet, and point it back to the resource
			// snapshot to roll out.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can point the placement binding of member cluster 0 to another resource snapshot", func() {
			updatePlacementBindingSpec(clusterScopedNS, bindingNames[0], func(spec *placementv1alpha1.PlacementBindingSpec) {
				spec.ResourceSnapshotName = conflictingResourceSnapshotName
			})
		})

		It("should move on to member cluster 1", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 1")
		})

		It("can mark member cluster 1 as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[1], snapshotName)
		})

		It("should complete the cluster staged update run with partial success", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonPartiallySucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateFailedConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						Conditions: []metav1.Condition{
							perStageStartedCond(),
							perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonPartiallySucceeded),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		It("should report the conflict on member cluster 0", func() {
			clusterCompletedMsgActual := perClusterConditionMessageActual(clusterScopedNS, runName, memberClusterNames[0],
				rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted, resourceSnapshotConflictMsgSubstr)
			Expect(clusterCompletedMsgActual()).To(Succeed(), "Failed to report the conflict on member cluster 0")
		})

		It("should not point the placement binding of member cluster 0 back to the resource snapshot to roll out", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], conflictingResourceSnapshotName)
			Expect(bindingUntouchedActual()).To(Succeed(), "The placement binding of member cluster 0 has been updated unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("placement bindings deleted after initialization", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// The placement binding of member cluster 0 is deleted after the cluster staged update run has been initialized.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			// The cluster staged update run uses the default failure policy, which allows one cluster failure.
			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the cluster staged update run", func() {
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      restStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 1),
					MaxConcurrency: ptr.To(int32(1)),
				},
			})
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")
		})

		It("can delete the placement binding of member cluster 0", func() {
			// Note that removeObjects waits until the cache observes the deletion.
			removeObjects(newPlacementBindingObj(clusterScopedNS, bindingNames[0]))
		})

		It("can resume the cluster staged update run", func() {
			updateStagedUpdateRunSuspended(clusterScopedNS, runName, false)
		})

		It("should move on to member cluster 1", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 1")
		})

		It("can mark member cluster 1 as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[1], snapshotName)
		})

		It("should complete the cluster staged update run with partial success", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunResumedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonPartiallySucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							// The update fails before it starts, as the placement binding cannot be found.
							wantPerClusterStatus(memberClusterNames, bindingNames, 0,
								perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed)),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						Conditions: []metav1.Condition{
							perStageStartedCond(),
							perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonPartiallySucceeded),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		It("should report that the placement binding of member cluster 0 cannot be found", func() {
			clusterCompletedMsgActual := perClusterConditionMessageActual(clusterScopedNS, runName, memberClusterNames[0],
				rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted, bindingNotFoundMsgSubstr)
			Expect(clusterCompletedMsgActual()).To(Succeed(), "Failed to report that the placement binding of member cluster 0 cannot be found")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("placement bindings marked for deletion or retargeted after initialization", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// After the cluster staged update run has been initialized, the placement binding of member cluster 0 is marked
		// for deletion, and the one of member cluster 1 is retargeted to another member cluster.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 3)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil, nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			// The cluster staged update run allows two cluster failures.
			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
				FailurePolicy: &rolloutv1alpha1.StagedUpdateRunFailurePolicy{
					MaxAllowedClusterFailures: ptr.To(int32(2)),
				},
			})
		})

		It("should initialize the cluster staged update run", func() {
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      restStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 1, 2),
					MaxConcurrency: ptr.To(int32(1)),
				},
			})
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")
		})

		It("can mark the placement binding of member cluster 0 for deletion", func() {
			markObjectForDeletion(newPlacementBindingObj(clusterScopedNS, bindingNames[0]))
		})

		It("can retarget the placement binding of member cluster 1 to another member cluster", func() {
			updatePlacementBindingSpec(clusterScopedNS, bindingNames[1], func(spec *placementv1alpha1.PlacementBindingSpec) {
				spec.ClusterName = retargetedClusterName
			})
		})

		It("can resume the cluster staged update run", func() {
			updateStagedUpdateRunSuspended(clusterScopedNS, runName, false)
		})

		It("should move on to member cluster 2", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[2], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 2")
		})

		It("can mark member cluster 2 as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[2], snapshotName)
		})

		It("should complete the cluster staged update run with partial success", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunResumedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonPartiallySucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							// The updates fail before they start, as the placement bindings no longer qualify.
							wantPerClusterStatus(memberClusterNames, bindingNames, 0,
								perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed)),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1,
								perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed)),
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
						},
						Conditions: []metav1.Condition{
							perStageStartedCond(),
							perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonPartiallySucceeded),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		It("should report why the updates on member clusters 0 and 1 have failed", func() {
			for _, idx := range []int{0, 1} {
				clusterCompletedMsgActual := perClusterConditionMessageActual(clusterScopedNS, runName, memberClusterNames[idx],
					rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted, bindingDeletingOrRetargetedMsgSubstr)
				Expect(clusterCompletedMsgActual()).To(Succeed(), "Failed to report why the update on member cluster %d has failed", idx)
			}
		})

		It("should not roll out to member clusters 0 and 1", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, bindingNames[0:2], previousResourceSnapshotName)
			Expect(bindingsUntouchedActual()).To(Succeed(), "Rolled out to member clusters 0 and 1 unexpectedly")
		})

		AfterAll(func() {
			removeTestFinalizer(newPlacementBindingObj(clusterScopedNS, bindingNames[0]))
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("cluster failures exceeding the threshold within a stage", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member clusters 0-2 are canary clusters, which the canary stage updates two at a time; member cluster 3 is
		// a production cluster. The cluster staged update run tolerates no cluster failures: the update on member cluster 0
		// fails while member cluster 1 is still being updated (its placement binding never becomes available).
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueCanary},
					},
					MaxConcurrency: ptr.To(intstr.FromInt32(2)),
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				FailurePolicy: &rolloutv1alpha1.StagedUpdateRunFailurePolicy{
					MaxAllowedClusterFailures: ptr.To(int32(0)),
				},
			})
		})

		It("should start rolling out to member clusters 0 and 1", func() {
			bindingsUpdatedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, bindingNames[0:2], snapshotName)
			Eventually(bindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member clusters 0 and 1")
		})

		It("should report that member clusters 0 and 1 are being updated", func() {
			// The conflict below is injected only after the persisted status shows that the updates have started.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 2),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(2)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 3),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can point the placement binding of member cluster 0 to another resource snapshot", func() {
			updatePlacementBindingSpec(clusterScopedNS, bindingNames[0], func(spec *placementv1alpha1.PlacementBindingSpec) {
				spec.ResourceSnapshotName = conflictingResourceSnapshotName
			})
		})

		It("should fail the cluster staged update run, canceling the in-flight update on member cluster 1", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonFailed),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateFailedConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1,
								perClusterStartedCond(),
								perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonCanceled)),
							// The update on member cluster 2 never starts.
							wantPerClusterStatus(memberClusterNames, bindingNames, 2),
						},
						Conditions: []metav1.Condition{
							perStageStartedCond(),
							perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonFailed),
						},
						MaxConcurrency: ptr.To(int32(2)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 3),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the cluster staged update run as failed")
		})

		It("should report that the canary stage has failed as the cluster failures have exceeded the threshold", func() {
			// The message counts only the clusters that have failed to update (i.e., not the canceled ones).
			stageCompletedMsgActual := perStageConditionMessageActual(clusterScopedNS, runName, canaryStageName,
				rolloutv1alpha1.StagedUpdateRunPerStageCondTypeCompleted, fmt.Sprintf(stageFailedMsgSubstrFmt, 1, 3))
			Expect(stageCompletedMsgActual()).To(Succeed(), "Failed to report why the canary stage has failed")
		})

		It("should not roll out to member clusters 2 and 3", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, bindingNames[2:], previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to member clusters 2 and 3 unexpectedly")
		})

		It("should remove the cleanup finalizer from the cluster staged update run", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(clusterScopedNS, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
		})

		It("should release the binding manager role of the cluster placement policy", func() {
			roleReleasedActual := bindingManagerRoleReleasedActual(clusterScopedNS, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("cluster failures exceeding the threshold across stages", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member clusters 0 and 1 are canary clusters; member clusters 2 and 3 are production clusters. The staged
		// update run tolerates one cluster failure: the update on member cluster 0 fails in the canary stage, and
		// then the one on member cluster 2 fails in the prod stage.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
			{envLabelKey: envLabelValueProd},
		}

		// The canary stage, as it is expected to be once completed.
		wantCompletedCanaryStage := rolloutv1alpha1.PerStageStatus{
			StageName: canaryStageName,
			Clusters: []rolloutv1alpha1.PerClusterStatus{
				wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateFailedConds()...),
				wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
			},
			Conditions: []metav1.Condition{
				perStageStartedCond(),
				perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonPartiallySucceeded),
			},
			MaxConcurrency: ptr.To(int32(1)),
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueCanary},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			// The cluster staged update run uses the default failure policy, which allows one cluster failure.
			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to member cluster 0", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 0")
		})

		It("should report that member cluster 0 is being updated", func() {
			// The conflict below is injected only after the persisted status shows that the update has started.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 2, 3),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can point the placement binding of member cluster 0 to another resource snapshot", func() {
			updatePlacementBindingSpec(clusterScopedNS, bindingNames[0], func(spec *placementv1alpha1.PlacementBindingSpec) {
				spec.ResourceSnapshotName = conflictingResourceSnapshotName
			})
		})

		It("should move on to member cluster 1", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 1")
		})

		It("can mark member cluster 1 as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[1], snapshotName)
		})

		It("should start rolling out to member cluster 2", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[2], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 2")
		})

		It("should report that the canary stage has completed with partial success and member cluster 2 is being updated", func() {
			// Once this status is persisted, the controller processes the prod stage in later reconciliations, where
			// it counts the cluster failure in the canary stage from the persisted status; the conflict below is
			// injected only after this point.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterStartedCond()),
							wantPerClusterStatus(memberClusterNames, bindingNames, 3),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can point the placement binding of member cluster 2 to another resource snapshot", func() {
			updatePlacementBindingSpec(clusterScopedNS, bindingNames[2], func(spec *placementv1alpha1.PlacementBindingSpec) {
				spec.ResourceSnapshotName = conflictingResourceSnapshotName
			})
		})

		It("should fail the cluster staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonFailed),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateFailedConds()...),
							// The update on member cluster 3 never starts.
							wantPerClusterStatus(memberClusterNames, bindingNames, 3),
						},
						Conditions: []metav1.Condition{
							perStageStartedCond(),
							perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonFailed),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the cluster staged update run as failed")
		})

		It("should report that the prod stage has failed as the cluster failures across stages have exceeded the threshold", func() {
			stageCompletedMsgActual := perStageConditionMessageActual(clusterScopedNS, runName, prodStageName,
				rolloutv1alpha1.StagedUpdateRunPerStageCondTypeCompleted, fmt.Sprintf(stageFailedMsgSubstrFmt, 1, 2))
			Expect(stageCompletedMsgActual()).To(Succeed(), "Failed to report why the prod stage has failed")
		})

		It("should not roll out to member cluster 3", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[3], previousResourceSnapshotName)
			Consistently(bindingUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to member cluster 3 unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("processing time budget expiries", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// The (only) member cluster stays unavailable for multiple processing time budgets, so that the controller
		// picks up the in-flight update in new reconciliations.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// The status of the cluster staged update run while the member cluster is being updated.
		wantInProgressStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
			Conditions: []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
			Stages: []rolloutv1alpha1.PerStageStatus{
				{
					StageName: restStageName,
					Clusters: []rolloutv1alpha1.PerClusterStatus{
						wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterStartedCond()),
					},
					Conditions:     []metav1.Condition{perStageStartedCond()},
					MaxConcurrency: ptr.To(int32(1)),
				},
			},
		}

		// The placement binding is created at generation 1, and is bumped to generation 2 when the controller points it
		// to the resource snapshot to roll out.
		wantBindingGeneration := int64(2)

		var startedCondLTT metav1.Time

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			wantInProgressStatus.ResourceSnapshotNameToRollout = snapshotName
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("should report that the member cluster is being updated once the processing time budget runs out", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantInProgressStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")

			startedCond, err := getPerClusterCondition(clusterScopedNS, runName, memberClusterNames[0], rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeStarted)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the Started condition of the member cluster")
			Expect(startedCond).ToNot(BeNil(), "The member cluster has no Started condition")
			startedCondLTT = startedCond.LastTransitionTime
		})

		It("should keep waiting for the member cluster across processing time budget expiries", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantInProgressStatus)
			Consistently(rolloutStatusActual, consistentlyDurationAcrossBudgetExpiries, consistentlyInterval).Should(Succeed(), "Failed to keep waiting for the member cluster")
		})

		It("should keep the update on the member cluster as it started", func() {
			// The Started condition is not reset (which would also reset the wait time limit of the cluster).
			startedCond, err := getPerClusterCondition(clusterScopedNS, runName, memberClusterNames[0], rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeStarted)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the Started condition of the member cluster")
			Expect(startedCond).ToNot(BeNil(), "The member cluster has no Started condition")
			Expect(startedCond.LastTransitionTime.Equal(&startedCondLTT)).To(BeTrue(),
				"The Started condition of the member cluster has been reset (last transition time: %v, want %v)", startedCond.LastTransitionTime, startedCondLTT)

			// The placement binding is not updated again.
			bindingGenerationActual := placementBindingGenerationActual(clusterScopedNS, bindingNames[0], wantBindingGeneration)
			Expect(bindingGenerationActual()).To(Succeed(), "The placement binding has been updated again unexpectedly")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("should complete the cluster staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					stagedUpdateRunInitializedCond(),
					stagedUpdateRunStartedCond(),
					stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		It("should not have updated the placement binding again", func() {
			bindingGenerationActual := placementBindingGenerationActual(clusterScopedNS, bindingNames[0], wantBindingGeneration)
			Expect(bindingGenerationActual()).To(Succeed(), "The placement binding has been updated again unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})
})

var _ = Describe("processing staged update runs and cluster staged update runs (per-cluster wait time limits)", Label(slowTestLabel), func() {
	// The API requires the per-cluster wait time limit to be at least one minute. To pay for the wait only once, the
	// context below creates all of its staged update runs, of both scopes, at the same time, and verifies them
	// together, rather than following the one-Describe-per-scope layout of the other test cases.
	Context("member clusters that time out", Ordered, func() {
		type testCase struct {
			ns                        string
			maxAllowedClusterFailures int32

			configMapName      string
			policyName         string
			strategyName       string
			runName            string
			snapshotName       string
			memberClusterNames []string
			bindingNames       []string
		}

		// newTestCase prepares a staged update run (cluster-scoped if the namespace is empty) whose member cluster 0
		// never becomes available; if the run tolerates a cluster failure, its stage also has member cluster 1, which
		// becomes available.
		newTestCase := func(ns string, maxAllowedClusterFailures int32) *testCase {
			randStr := utils.RandStr()
			policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
			clusterCount := 1
			if maxAllowedClusterFailures > 0 {
				clusterCount = 2
			}
			memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, clusterCount)
			return &testCase{
				ns:                        ns,
				maxAllowedClusterFailures: maxAllowedClusterFailures,
				configMapName:             fmt.Sprintf(configMapNameTemplate, randStr),
				policyName:                policyName,
				strategyName:              fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr),
				runName:                   fmt.Sprintf(stagedUpdateRunNameTemplate, randStr),
				memberClusterNames:        memberClusterNames,
				bindingNames:              bindingNames,
			}
		}

		// For each scope, one staged update run tolerates one cluster failure, and the other tolerates none.
		testCases := []*testCase{
			newTestCase(appNamespaceName, 1),
			newTestCase(appNamespaceName, 0),
			newTestCase(clusterScopedNS, 1),
			newTestCase(clusterScopedNS, 0),
		}

		BeforeAll(func() {
			for _, tc := range testCases {
				createConfigMap(tc.configMapName)
				policy := createPlacementPolicy(tc.ns, tc.policyName, tc.configMapName)
				// The snapshot name factors in the UID of the placement policy, which is only known once it is created.
				tc.snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
				fixtures := createMemberClustersAndBindings(tc.ns, tc.policyName, tc.memberClusterNames, tc.bindingNames,
					make([]map[string]string, len(tc.memberClusterNames)))
				// All the member clusters in the stage are updated at the same time.
				strategy := createStagedUpdateStrategy(tc.ns, tc.strategyName, []rolloutv1alpha1.Stage{
					{
						Name:           restStageName,
						LabelSelector:  &metav1.LabelSelector{},
						MaxConcurrency: ptr.To(intstr.FromString("100%")),
					},
				})
				waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

				createStagedUpdateRun(tc.ns, tc.runName, rolloutv1alpha1.StagedUpdateRunSpec{
					PlacementPolicyName:      tc.policyName,
					StagedUpdateStrategyName: tc.strategyName,
					FailurePolicy: &rolloutv1alpha1.StagedUpdateRunFailurePolicy{
						MaxAllowedClusterFailures:    ptr.To(tc.maxAllowedClusterFailures),
						MaxWaitTimePerClusterMinutes: 1,
					},
				})
			}
		})

		It("should start rolling out to all the member clusters", func() {
			for _, tc := range testCases {
				bindingsUpdatedActual := placementBindingsResourceSnapshotActual(tc.ns, tc.bindingNames, tc.snapshotName)
				Eventually(bindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member clusters of run %s", tc.runName)
			}
		})

		It("can mark the member clusters that do not time out as available", func() {
			for _, tc := range testCases {
				if len(tc.bindingNames) > 1 {
					markPlacementBindingAsAvailable(tc.ns, tc.bindingNames[1], tc.snapshotName)
				}
			}
		})

		It("should fail the updates on the member clusters that time out, and complete the runs accordingly", func() {
			for _, tc := range testCases {
				// The update on member cluster 0 fails once the wait time limit is reached.
				clusterStatuses := []rolloutv1alpha1.PerClusterStatus{
					wantPerClusterStatus(tc.memberClusterNames, tc.bindingNames, 0, perClusterUpdateFailedConds()...),
				}
				runCompletedReason := rolloutv1alpha1.StagedUpdateRunCompletedCondReasonFailed
				stageCompletedReason := rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonFailed
				if tc.maxAllowedClusterFailures > 0 {
					// The failure is tolerated; the update on member cluster 1 succeeds.
					clusterStatuses = append(clusterStatuses,
						wantPerClusterStatus(tc.memberClusterNames, tc.bindingNames, 1, perClusterUpdateSucceededConds()...))
					runCompletedReason = rolloutv1alpha1.StagedUpdateRunCompletedCondReasonPartiallySucceeded
					stageCompletedReason = rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonPartiallySucceeded
				}

				wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
					Conditions: []metav1.Condition{
						stagedUpdateRunInitializedCond(),
						stagedUpdateRunStartedCond(),
						stagedUpdateRunCompletedCond(runCompletedReason),
					},
					ResourceSnapshotNameToRollout: tc.snapshotName,
					Stages: []rolloutv1alpha1.PerStageStatus{
						{
							StageName:      restStageName,
							Clusters:       clusterStatuses,
							Conditions:     []metav1.Condition{perStageStartedCond(), perStageCompletedCond(stageCompletedReason)},
							MaxConcurrency: ptr.To(int32(len(clusterStatuses))),
						},
					},
				}
				rolloutStatusActual := stagedUpdateRunRolloutStatusActual(tc.ns, tc.runName, wantStatus)
				Eventually(rolloutStatusActual, eventuallyDurationForClusterTimeout, eventuallyInterval).Should(Succeed(), "Failed to complete run %s", tc.runName)
			}
		})

		It("should report the timeouts", func() {
			for _, tc := range testCases {
				clusterCompletedMsgActual := perClusterConditionMessageActual(tc.ns, tc.runName, tc.memberClusterNames[0],
					rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted, clusterTimeoutMsgSubstr)
				Expect(clusterCompletedMsgActual()).To(Succeed(), "Failed to report the timeout in run %s", tc.runName)

				// The stage lasts at least as long as the wait time limit.
				stageLastedActual := stagedUpdateRunStageLastedAtLeastActual(tc.ns, tc.runName, restStageName, time.Minute)
				Expect(stageLastedActual()).To(Succeed(), "Failed to wait for the wait time limit in run %s", tc.runName)
			}
		})

		AfterAll(func() {
			for _, tc := range testCases {
				removeObjects(slices.Concat(
					[]client.Object{
						newStagedUpdateRunObj(tc.ns, tc.runName),
						newPlacementResourceSnapshotObj(tc.ns, tc.snapshotName),
						newStagedUpdateStrategyObj(tc.ns, tc.strategyName),
						newPlacementPolicyObj(tc.ns, tc.policyName),
						newConfigMapObj(tc.configMapName),
					},
					memberClusterAndBindingObjs(tc.ns, tc.memberClusterNames, tc.bindingNames),
				)...)
			}
		})
	})
})
