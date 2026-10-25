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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

// The name templates of the approval requests that the controller creates for before-stage and after-stage approval
// tasks; the placeholders are the name of the staged update run and the name of the stage.
const (
	beforeStageApprovalRequestNameTemplate = "%s-before-stage-%s"
	afterStageApprovalRequestNameTemplate  = "%s-after-stage-%s"
)

const (
	afterStageWaitTime     = time.Second * 5
	longAfterStageWaitTime = time.Second * 10
)

// wantSingleClusterCompletedStatus builds the status of a completed staged update run whose only (canary) stage has
// member cluster 0 only, along with the given before-stage tasks.
func wantSingleClusterCompletedStatus(
	snapshotName string,
	memberClusterNames, bindingNames []string,
	beforeStageTasks []rolloutv1alpha1.PerStageTaskStatus,
) *rolloutv1alpha1.StagedUpdateRunStatus {
	return &rolloutv1alpha1.StagedUpdateRunStatus{
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
				},
				BeforeStageTasks: beforeStageTasks,
				Conditions:       perStageSucceededConds(),
				MaxConcurrency:   ptr.To(int32(1)),
			},
		},
	}
}

var _ = Describe("processing stage tasks of staged update runs", func() {
	Context("before-stage approval", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		// The canary stage, which has a before-stage approval task, updates both member clusters at the same time.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:           canaryStageName,
					LabelSelector:  &metav1.LabelSelector{},
					MaxConcurrency: ptr.To(intstr.FromString("100%")),
					BeforeStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should create the approval request for the canary stage", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(appNamespaceName, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the approval request")
		})

		It("should report that the canary stage has started and is waiting for approval", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 1),
						BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							{
								Type:                rolloutv1alpha1.StageTaskTypeApproval,
								ApprovalRequestName: approvalRequestName,
								Conditions:          []metav1.Condition{perStageTaskApprovalRequestCreatedCond()},
							},
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(2)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("should not roll out to any member cluster before the approval", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(appNamespaceName, bindingNames, previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to member clusters unexpectedly")
		})

		It("can approve the approval request", func() {
			approveApprovalRequest(appNamespaceName, approvalRequestName)
		})

		It("should accept the approval", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(appNamespaceName, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the member clusters", func() {
			// The controller does not requeue a staged update run that is waiting for approval; it is the watch on
			// approval requests that triggers the processing of the run once the request is approved.
			bindingsUpdatedActual := placementBindingsResourceSnapshotActual(appNamespaceName, bindingNames, snapshotName)
			Eventually(bindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member clusters")
		})

		It("can mark the member clusters as available", func() {
			for _, bindingName := range bindingNames {
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
						},
						BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							{
								Type:                rolloutv1alpha1.StageTaskTypeApproval,
								ApprovalRequestName: approvalRequestName,
								Conditions: []metav1.Condition{
									perStageTaskApprovalRequestCreatedCond(),
									perStageTaskApprovalRequestApprovedCond(),
								},
							},
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(2)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		AfterAll(func() {
			// The envtest environment does not run the garbage collector; the approval request, which the staged
			// update run owns, must be removed explicitly.
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newApprovalRequestObj(appNamespaceName, approvalRequestName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("after-stage approval", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(afterStageApprovalRequestNameTemplate, runName, canaryStageName)

		// Member clusters 0 and 1 are canary clusters, which the canary stage (with an after-stage approval task)
		// updates one at a time; member cluster 2 is a production cluster.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 3)
		memberClusterLabels := []map[string]string{
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
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
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

		It("should start rolling out to member cluster 0", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 0")
		})

		It("should not create the approval request while member cluster 0 is being updated", func() {
			approvalRequestAbsentActual := approvalRequestAbsentActual(appNamespaceName, approvalRequestName)
			Consistently(approvalRequestAbsentActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Created the approval request unexpectedly")
		})

		It("can mark member cluster 0 as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("should start rolling out to member cluster 1", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 1")
		})

		It("should not create the approval request while member cluster 1 is being updated", func() {
			approvalRequestAbsentActual := approvalRequestAbsentActual(appNamespaceName, approvalRequestName)
			Consistently(approvalRequestAbsentActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Created the approval request unexpectedly")
		})

		It("can mark member cluster 1 as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[1], snapshotName)
		})

		It("should create the approval request once all the member clusters in the canary stage have been updated", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(appNamespaceName, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the approval request")
		})

		It("should report that the canary stage is waiting for approval", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 2),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")

			// The canary stage does not complete, and the prod stage does not start, before the approval.
			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[2], previousResourceSnapshotName)
			Consistently(bindingUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the prod stage unexpectedly")
			Expect(rolloutStatusActual()).To(Succeed(), "The status of the staged update run has changed unexpectedly")
		})

		It("can approve the approval request", func() {
			approveApprovalRequest(appNamespaceName, approvalRequestName)
		})

		It("should accept the approval", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(appNamespaceName, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the prod stage", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[2], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the prod stage")
		})

		It("can mark member cluster 2 as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[2], snapshotName)
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
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
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

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newApprovalRequestObj(appNamespaceName, approvalRequestName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("rejected approval", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// The status of the staged update run while it waits for approval.
		wantWaitingForApprovalStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
			Conditions: []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
			Stages: []rolloutv1alpha1.PerStageStatus{
				{
					StageName: canaryStageName,
					Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
					BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
						wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond()),
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
			wantWaitingForApprovalStatus.ResourceSnapshotNameToRollout = snapshotName
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          canaryStageName,
					LabelSelector: &metav1.LabelSelector{},
					BeforeStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should create the approval request", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(appNamespaceName, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the approval request")
		})

		It("should report that the canary stage is waiting for approval", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantWaitingForApprovalStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can reject the approval request", func() {
			rejectApprovalRequest(appNamespaceName, approvalRequestName)
		})

		It("should not accept the rejection or roll out to the member cluster", func() {
			Consistently(func() error {
				if err := approvalRequestConditionsActual(appNamespaceName, approvalRequestName, []metav1.Condition{
					approvalRequestApprovedCond(metav1.ConditionFalse),
				})(); err != nil {
					return err
				}
				return placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], previousResourceSnapshotName)()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Accepted the rejection or rolled out to the member cluster unexpectedly")
		})

		It("should keep the canary stage waiting for approval", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantWaitingForApprovalStatus)
			Expect(rolloutStatusActual()).To(Succeed(), "The status of the staged update run has changed unexpectedly")
		})

		It("can approve the approval request", func() {
			approveApprovalRequest(appNamespaceName, approvalRequestName)
		})

		It("should accept the approval", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(appNamespaceName, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("should complete the staged update run", func() {
			wantStatus := wantSingleClusterCompletedStatus(snapshotName, memberClusterNames, bindingNames,
				[]rolloutv1alpha1.PerStageTaskStatus{
					wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
				})
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newApprovalRequestObj(appNamespaceName, approvalRequestName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("after-stage timed wait", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member cluster 0 is a canary cluster, and member cluster 1 is a production cluster; the canary stage has an
		// after-stage timed wait task.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)
		memberClusterLabels := []map[string]string{
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
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: afterStageWaitTime},
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

		It("should start the after-stage timed wait task of the canary stage", func() {
			// The controller persists the status right after it starts the timed wait task, so the status is
			// observable for the whole wait time.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantTimedWaitTaskStatus(afterStageWaitTime, perStageTaskTimedWaitStartedCond()),
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
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to start the timed wait task")
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
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantTimedWaitTaskStatus(afterStageWaitTime, perStageTaskTimedWaitStartedCond(), perStageTaskWaitTimeElapsedCond()),
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

			// The wait time elapses at least the wait time after the task starts, and the canary stage completes (and
			// the prod stage starts) only afterwards.
			timedWaitLastedActual := perStageTaskTimedWaitLastedAtLeastActual(appNamespaceName, runName, canaryStageName, afterStageWaitTime)
			Expect(timedWaitLastedActual()).To(Succeed(), "Failed to wait for the wait time")
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

	Context("after-stage approval and timed wait", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		canaryApprovalRequestName := fmt.Sprintf(afterStageApprovalRequestNameTemplate, runName, canaryStageName)
		prodApprovalRequestName := fmt.Sprintf(afterStageApprovalRequestNameTemplate, runName, prodStageName)

		// Member cluster 0 is a canary cluster, and member cluster 1 is a production cluster. Both stages have an
		// after-stage approval task and an after-stage timed wait task: in the canary stage, the approval comes
		// before the wait time elapses; in the prod stage, the wait time elapses before the approval.
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
			AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
				wantApprovalTaskStatus(canaryApprovalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
				wantTimedWaitTaskStatus(longAfterStageWaitTime, perStageTaskTimedWaitStartedCond(), perStageTaskWaitTimeElapsedCond()),
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
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: longAfterStageWaitTime},
						},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: afterStageWaitTime},
						},
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

		It("should create the approval request for the canary stage", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(appNamespaceName, runName, canaryApprovalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the approval request")
		})

		It("can approve the approval request for the canary stage before the wait time elapses", func() {
			approveApprovalRequest(appNamespaceName, canaryApprovalRequestName)
		})

		It("should accept the approval, but keep waiting for the wait time to elapse", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(appNamespaceName, canaryApprovalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")

			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(canaryApprovalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
							wantTimedWaitTaskStatus(longAfterStageWaitTime, perStageTaskTimedWaitStartedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName: prodStageName,
						Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 1),
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(""),
							wantTimedWaitTaskStatus(afterStageWaitTime),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the production cluster once the wait time has elapsed", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to roll out to the production cluster")

			// The completion of the canary stage is persisted once the processing time budget runs out (as the
			// controller waits for the production cluster to become available in the meantime).
			timedWaitLastedActual := perStageTaskTimedWaitLastedAtLeastActual(appNamespaceName, runName, canaryStageName, longAfterStageWaitTime)
			Eventually(timedWaitLastedActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to wait for the wait time of the canary stage")
		})

		It("can mark the production cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[1], snapshotName)
		})

		It("should create the approval request for the prod stage", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(appNamespaceName, runName, prodApprovalRequestName, prodStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the approval request")
		})

		It("should keep waiting for approval after the wait time of the prod stage elapses", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(prodApprovalRequestName, perStageTaskApprovalRequestCreatedCond()),
							wantTimedWaitTaskStatus(afterStageWaitTime, perStageTaskTimedWaitStartedCond(), perStageTaskWaitTimeElapsedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to elapse the wait time of the prod stage")
			Consistently(rolloutStatusActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep waiting for approval")
		})

		It("can approve the approval request for the prod stage", func() {
			approveApprovalRequest(appNamespaceName, prodApprovalRequestName)
		})

		It("should accept the approval for the prod stage", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(appNamespaceName, prodApprovalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
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
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(prodApprovalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
							wantTimedWaitTaskStatus(afterStageWaitTime, perStageTaskTimedWaitStartedCond(), perStageTaskWaitTimeElapsedCond()),
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

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newApprovalRequestObj(appNamespaceName, canaryApprovalRequestName),
					newApprovalRequestObj(appNamespaceName, prodApprovalRequestName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("deleted approval request", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		var oldApprovalRequestUID types.UID

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          canaryStageName,
					LabelSelector: &metav1.LabelSelector{},
					BeforeStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should create the approval request", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(appNamespaceName, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the approval request")

			request, err := getApprovalRequest(appNamespaceName, approvalRequestName)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the approval request")
			oldApprovalRequestUID = request.GetUID()
		})

		It("can delete the approval request", func() {
			// The controller re-creates the approval request right away; do not wait for the name to be gone.
			Expect(hubClient.Delete(ctx, newApprovalRequestObj(appNamespaceName, approvalRequestName))).To(Succeed(), "Failed to delete the approval request")
		})

		It("should re-create the approval request", func() {
			approvalRequestReplacedActual := approvalRequestReplacedActual(appNamespaceName, approvalRequestName, oldApprovalRequestUID)
			Eventually(approvalRequestReplacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to re-create the approval request")

			approvalRequestCreatedActual := approvalRequestCreatedActual(appNamespaceName, runName, approvalRequestName, canaryStageName)
			Expect(approvalRequestCreatedActual()).To(Succeed(), "Failed to re-create the approval request as expected")
		})

		It("should not roll out to the member cluster before the approval", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], previousResourceSnapshotName)
			Consistently(bindingUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the member cluster unexpectedly")
		})

		It("can approve the re-created approval request", func() {
			approveApprovalRequest(appNamespaceName, approvalRequestName)
		})

		It("should accept the approval", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(appNamespaceName, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("should complete the staged update run", func() {
			wantStatus := wantSingleClusterCompletedStatus(snapshotName, memberClusterNames, bindingNames,
				[]rolloutv1alpha1.PerStageTaskStatus{
					wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
				})
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newApprovalRequestObj(appNamespaceName, approvalRequestName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("stale approval request from a deleted staged update run of the same name", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		var oldApprovalRequestUID types.UID

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          canaryStageName,
					LabelSelector: &metav1.LabelSelector{},
					BeforeStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should create the approval request", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(appNamespaceName, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the approval request")

			request, err := getApprovalRequest(appNamespaceName, approvalRequestName)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the approval request")
			oldApprovalRequestUID = request.GetUID()
		})

		It("can delete the staged update run", func() {
			removeObjects(newStagedUpdateRunObj(appNamespaceName, runName))

			// The envtest environment does not run the garbage collector; the approval request stays around.
			_, err := getApprovalRequest(appNamespaceName, approvalRequestName)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the approval request of the deleted staged update run")
		})

		It("can approve the approval request of the deleted staged update run", func() {
			// The approval of the stale approval request should not carry over to the re-created staged update run.
			approveApprovalRequest(appNamespaceName, approvalRequestName)
		})

		It("can re-create the staged update run", func() {
			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should replace the stale approval request with a new one owned by the re-created staged update run", func() {
			approvalRequestReplacedActual := approvalRequestReplacedActual(appNamespaceName, approvalRequestName, oldApprovalRequestUID)
			Eventually(approvalRequestReplacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to replace the stale approval request")

			// The check below verifies that the new approval request is owned by the re-created staged update run
			// (with the UID of the re-created run).
			approvalRequestCreatedActual := approvalRequestCreatedActual(appNamespaceName, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the new approval request as expected")

			approvalRequestNotApprovedActual := approvalRequestConditionsActual(appNamespaceName, approvalRequestName, nil)
			Expect(approvalRequestNotApprovedActual()).To(Succeed(), "The new approval request has been approved unexpectedly")
		})

		It("should not roll out to the member cluster before the new approval request is approved", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], previousResourceSnapshotName)
			Consistently(bindingUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the member cluster unexpectedly")
		})

		It("can approve the new approval request", func() {
			approveApprovalRequest(appNamespaceName, approvalRequestName)
		})

		It("should accept the approval", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(appNamespaceName, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("should complete the staged update run", func() {
			wantStatus := wantSingleClusterCompletedStatus(snapshotName, memberClusterNames, bindingNames,
				[]rolloutv1alpha1.PerStageTaskStatus{
					wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
				})
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newApprovalRequestObj(appNamespaceName, approvalRequestName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("approval request not owned by the staged update run", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          canaryStageName,
					LabelSelector: &metav1.LabelSelector{},
					BeforeStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
				},
			})

			// Create (and approve) an approval request of the expected name manually, before the staged update run is
			// created. Note that the request references the staged update run in its spec, so that its deletion
			// triggers the processing of the run.
			createManualApprovalRequest(appNamespaceName, approvalRequestName, runName, canaryStageName)
			approveApprovalRequest(appNamespaceName, approvalRequestName)
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should report that the approval request is not owned by the staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
						BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestOwnershipMismatchCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the ownership mismatch")
		})

		It("should not roll out to the member cluster, even though the approval request has been approved", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], previousResourceSnapshotName)
			Consistently(bindingUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the member cluster unexpectedly")
		})

		It("can delete the manually created approval request", func() {
			// The controller re-creates the approval request right away; do not wait for the name to be gone.
			Expect(hubClient.Delete(ctx, newApprovalRequestObj(appNamespaceName, approvalRequestName))).To(Succeed(), "Failed to delete the approval request")
		})

		It("should create its own approval request", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(appNamespaceName, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the approval request")
		})

		It("should report that the approval request has been created", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
						BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the creation of the approval request")
		})

		It("can approve the approval request", func() {
			approveApprovalRequest(appNamespaceName, approvalRequestName)
		})

		It("should accept the approval", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(appNamespaceName, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("should complete the staged update run", func() {
			wantStatus := wantSingleClusterCompletedStatus(snapshotName, memberClusterNames, bindingNames,
				[]rolloutv1alpha1.PerStageTaskStatus{
					wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
				})
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newApprovalRequestObj(appNamespaceName, approvalRequestName),
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

var _ = Describe("processing stage tasks of cluster staged update runs", func() {
	Context("before-stage approval", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		// The canary stage, which has a before-stage approval task, updates both member clusters at the same time.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:           canaryStageName,
					LabelSelector:  &metav1.LabelSelector{},
					MaxConcurrency: ptr.To(intstr.FromString("100%")),
					BeforeStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should create the cluster approval request for the canary stage", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(clusterScopedNS, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the cluster approval request")
		})

		It("should report that the canary stage has started and is waiting for approval", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 1),
						BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							{
								Type:                rolloutv1alpha1.StageTaskTypeApproval,
								ApprovalRequestName: approvalRequestName,
								Conditions:          []metav1.Condition{perStageTaskApprovalRequestCreatedCond()},
							},
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(2)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("should not roll out to any member cluster before the approval", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, bindingNames, previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to member clusters unexpectedly")
		})

		It("can approve the cluster approval request", func() {
			approveApprovalRequest(clusterScopedNS, approvalRequestName)
		})

		It("should accept the approval", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(clusterScopedNS, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the member clusters", func() {
			// The controller does not requeue a cluster staged update run that is waiting for approval; it is the watch on
			// cluster approval requests that triggers the processing of the run once the request is approved.
			bindingsUpdatedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, bindingNames, snapshotName)
			Eventually(bindingsUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member clusters")
		})

		It("can mark the member clusters as available", func() {
			for _, bindingName := range bindingNames {
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
						},
						BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							{
								Type:                rolloutv1alpha1.StageTaskTypeApproval,
								ApprovalRequestName: approvalRequestName,
								Conditions: []metav1.Condition{
									perStageTaskApprovalRequestCreatedCond(),
									perStageTaskApprovalRequestApprovedCond(),
								},
							},
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(2)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		AfterAll(func() {
			// The envtest environment does not run the garbage collector; the cluster approval request, which the staged
			// update run owns, must be removed explicitly.
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newApprovalRequestObj(clusterScopedNS, approvalRequestName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("after-stage approval", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(afterStageApprovalRequestNameTemplate, runName, canaryStageName)

		// Member clusters 0 and 1 are canary clusters, which the canary stage (with an after-stage approval task)
		// updates one at a time; member cluster 2 is a production cluster.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 3)
		memberClusterLabels := []map[string]string{
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
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
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

		It("should start rolling out to member cluster 0", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 0")
		})

		It("should not create the cluster approval request while member cluster 0 is being updated", func() {
			approvalRequestAbsentActual := approvalRequestAbsentActual(clusterScopedNS, approvalRequestName)
			Consistently(approvalRequestAbsentActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Created the cluster approval request unexpectedly")
		})

		It("can mark member cluster 0 as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("should start rolling out to member cluster 1", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 1")
		})

		It("should not create the cluster approval request while member cluster 1 is being updated", func() {
			approvalRequestAbsentActual := approvalRequestAbsentActual(clusterScopedNS, approvalRequestName)
			Consistently(approvalRequestAbsentActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Created the cluster approval request unexpectedly")
		})

		It("can mark member cluster 1 as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[1], snapshotName)
		})

		It("should create the cluster approval request once all the member clusters in the canary stage have been updated", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(clusterScopedNS, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the cluster approval request")
		})

		It("should report that the canary stage is waiting for approval", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName:      prodStageName,
						Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 2),
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")

			// The canary stage does not complete, and the prod stage does not start, before the approval.
			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[2], previousResourceSnapshotName)
			Consistently(bindingUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the prod stage unexpectedly")
			Expect(rolloutStatusActual()).To(Succeed(), "The status of the cluster staged update run has changed unexpectedly")
		})

		It("can approve the cluster approval request", func() {
			approveApprovalRequest(clusterScopedNS, approvalRequestName)
		})

		It("should accept the approval", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(clusterScopedNS, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the prod stage", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[2], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the prod stage")
		})

		It("can mark member cluster 2 as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[2], snapshotName)
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
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
						},
						Conditions:     perStageSucceededConds(),
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 2, perClusterUpdateSucceededConds()...),
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

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newApprovalRequestObj(clusterScopedNS, approvalRequestName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("rejected approval", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// The status of the cluster staged update run while it waits for approval.
		wantWaitingForApprovalStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
			Conditions: []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
			Stages: []rolloutv1alpha1.PerStageStatus{
				{
					StageName: canaryStageName,
					Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
					BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
						wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond()),
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
			wantWaitingForApprovalStatus.ResourceSnapshotNameToRollout = snapshotName
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          canaryStageName,
					LabelSelector: &metav1.LabelSelector{},
					BeforeStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should create the cluster approval request", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(clusterScopedNS, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the cluster approval request")
		})

		It("should report that the canary stage is waiting for approval", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantWaitingForApprovalStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can reject the cluster approval request", func() {
			rejectApprovalRequest(clusterScopedNS, approvalRequestName)
		})

		It("should not accept the rejection or roll out to the member cluster", func() {
			Consistently(func() error {
				if err := approvalRequestConditionsActual(clusterScopedNS, approvalRequestName, []metav1.Condition{
					approvalRequestApprovedCond(metav1.ConditionFalse),
				})(); err != nil {
					return err
				}
				return placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], previousResourceSnapshotName)()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Accepted the rejection or rolled out to the member cluster unexpectedly")
		})

		It("should keep the canary stage waiting for approval", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantWaitingForApprovalStatus)
			Expect(rolloutStatusActual()).To(Succeed(), "The status of the cluster staged update run has changed unexpectedly")
		})

		It("can approve the cluster approval request", func() {
			approveApprovalRequest(clusterScopedNS, approvalRequestName)
		})

		It("should accept the approval", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(clusterScopedNS, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("should complete the cluster staged update run", func() {
			wantStatus := wantSingleClusterCompletedStatus(snapshotName, memberClusterNames, bindingNames,
				[]rolloutv1alpha1.PerStageTaskStatus{
					wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
				})
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newApprovalRequestObj(clusterScopedNS, approvalRequestName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("after-stage timed wait", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member cluster 0 is a canary cluster, and member cluster 1 is a production cluster; the canary stage has an
		// after-stage timed wait task.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)
		memberClusterLabels := []map[string]string{
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
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: afterStageWaitTime},
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

		It("should start the after-stage timed wait task of the canary stage", func() {
			// The controller persists the status right after it starts the timed wait task, so the status is
			// observable for the whole wait time.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantTimedWaitTaskStatus(afterStageWaitTime, perStageTaskTimedWaitStartedCond()),
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
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to start the timed wait task")
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
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantTimedWaitTaskStatus(afterStageWaitTime, perStageTaskTimedWaitStartedCond(), perStageTaskWaitTimeElapsedCond()),
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

			// The wait time elapses at least the wait time after the task starts, and the canary stage completes (and
			// the prod stage starts) only afterwards.
			timedWaitLastedActual := perStageTaskTimedWaitLastedAtLeastActual(clusterScopedNS, runName, canaryStageName, afterStageWaitTime)
			Expect(timedWaitLastedActual()).To(Succeed(), "Failed to wait for the wait time")
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

	Context("after-stage approval and timed wait", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		canaryApprovalRequestName := fmt.Sprintf(afterStageApprovalRequestNameTemplate, runName, canaryStageName)
		prodApprovalRequestName := fmt.Sprintf(afterStageApprovalRequestNameTemplate, runName, prodStageName)

		// Member cluster 0 is a canary cluster, and member cluster 1 is a production cluster. Both stages have an
		// after-stage approval task and an after-stage timed wait task: in the canary stage, the approval comes
		// before the wait time elapses; in the prod stage, the wait time elapses before the approval.
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
			AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
				wantApprovalTaskStatus(canaryApprovalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
				wantTimedWaitTaskStatus(longAfterStageWaitTime, perStageTaskTimedWaitStartedCond(), perStageTaskWaitTimeElapsedCond()),
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
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: longAfterStageWaitTime},
						},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: afterStageWaitTime},
						},
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

		It("should create the cluster approval request for the canary stage", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(clusterScopedNS, runName, canaryApprovalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the cluster approval request")
		})

		It("can approve the cluster approval request for the canary stage before the wait time elapses", func() {
			approveApprovalRequest(clusterScopedNS, canaryApprovalRequestName)
		})

		It("should accept the approval, but keep waiting for the wait time to elapse", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(clusterScopedNS, canaryApprovalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")

			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0, perClusterUpdateSucceededConds()...),
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(canaryApprovalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
							wantTimedWaitTaskStatus(longAfterStageWaitTime, perStageTaskTimedWaitStartedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
					{
						StageName: prodStageName,
						Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 1),
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(""),
							wantTimedWaitTaskStatus(afterStageWaitTime),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the production cluster once the wait time has elapsed", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to roll out to the production cluster")

			// The completion of the canary stage is persisted once the processing time budget runs out (as the
			// controller waits for the production cluster to become available in the meantime).
			timedWaitLastedActual := perStageTaskTimedWaitLastedAtLeastActual(clusterScopedNS, runName, canaryStageName, longAfterStageWaitTime)
			Eventually(timedWaitLastedActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to wait for the wait time of the canary stage")
		})

		It("can mark the production cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[1], snapshotName)
		})

		It("should create the cluster approval request for the prod stage", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(clusterScopedNS, runName, prodApprovalRequestName, prodStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the cluster approval request")
		})

		It("should keep waiting for approval after the wait time of the prod stage elapses", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					wantCompletedCanaryStage,
					{
						StageName: prodStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(prodApprovalRequestName, perStageTaskApprovalRequestCreatedCond()),
							wantTimedWaitTaskStatus(afterStageWaitTime, perStageTaskTimedWaitStartedCond(), perStageTaskWaitTimeElapsedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to elapse the wait time of the prod stage")
			Consistently(rolloutStatusActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep waiting for approval")
		})

		It("can approve the cluster approval request for the prod stage", func() {
			approveApprovalRequest(clusterScopedNS, prodApprovalRequestName)
		})

		It("should accept the approval for the prod stage", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(clusterScopedNS, prodApprovalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
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
							wantPerClusterStatus(memberClusterNames, bindingNames, 1, perClusterUpdateSucceededConds()...),
						},
						AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(prodApprovalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
							wantTimedWaitTaskStatus(afterStageWaitTime, perStageTaskTimedWaitStartedCond(), perStageTaskWaitTimeElapsedCond()),
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

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newApprovalRequestObj(clusterScopedNS, canaryApprovalRequestName),
					newApprovalRequestObj(clusterScopedNS, prodApprovalRequestName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("deleted cluster approval request", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		var oldApprovalRequestUID types.UID

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          canaryStageName,
					LabelSelector: &metav1.LabelSelector{},
					BeforeStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should create the cluster approval request", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(clusterScopedNS, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the cluster approval request")

			request, err := getApprovalRequest(clusterScopedNS, approvalRequestName)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the cluster approval request")
			oldApprovalRequestUID = request.GetUID()
		})

		It("can delete the cluster approval request", func() {
			// The controller re-creates the cluster approval request right away; do not wait for the name to be gone.
			Expect(hubClient.Delete(ctx, newApprovalRequestObj(clusterScopedNS, approvalRequestName))).To(Succeed(), "Failed to delete the cluster approval request")
		})

		It("should re-create the cluster approval request", func() {
			approvalRequestReplacedActual := approvalRequestReplacedActual(clusterScopedNS, approvalRequestName, oldApprovalRequestUID)
			Eventually(approvalRequestReplacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to re-create the cluster approval request")

			approvalRequestCreatedActual := approvalRequestCreatedActual(clusterScopedNS, runName, approvalRequestName, canaryStageName)
			Expect(approvalRequestCreatedActual()).To(Succeed(), "Failed to re-create the cluster approval request as expected")
		})

		It("should not roll out to the member cluster before the approval", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], previousResourceSnapshotName)
			Consistently(bindingUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the member cluster unexpectedly")
		})

		It("can approve the re-created cluster approval request", func() {
			approveApprovalRequest(clusterScopedNS, approvalRequestName)
		})

		It("should accept the approval", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(clusterScopedNS, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("should complete the cluster staged update run", func() {
			wantStatus := wantSingleClusterCompletedStatus(snapshotName, memberClusterNames, bindingNames,
				[]rolloutv1alpha1.PerStageTaskStatus{
					wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
				})
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newApprovalRequestObj(clusterScopedNS, approvalRequestName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("stale cluster approval request from a deleted cluster staged update run of the same name", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		var oldApprovalRequestUID types.UID

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          canaryStageName,
					LabelSelector: &metav1.LabelSelector{},
					BeforeStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should create the cluster approval request", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(clusterScopedNS, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the cluster approval request")

			request, err := getApprovalRequest(clusterScopedNS, approvalRequestName)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the cluster approval request")
			oldApprovalRequestUID = request.GetUID()
		})

		It("can delete the cluster staged update run", func() {
			removeObjects(newStagedUpdateRunObj(clusterScopedNS, runName))

			// The envtest environment does not run the garbage collector; the cluster approval request stays around.
			_, err := getApprovalRequest(clusterScopedNS, approvalRequestName)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the cluster approval request of the deleted cluster staged update run")
		})

		It("can approve the cluster approval request of the deleted cluster staged update run", func() {
			// The approval of the stale cluster approval request should not carry over to the re-created cluster staged update run.
			approveApprovalRequest(clusterScopedNS, approvalRequestName)
		})

		It("can re-create the cluster staged update run", func() {
			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should replace the stale cluster approval request with a new one owned by the re-created cluster staged update run", func() {
			approvalRequestReplacedActual := approvalRequestReplacedActual(clusterScopedNS, approvalRequestName, oldApprovalRequestUID)
			Eventually(approvalRequestReplacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to replace the stale cluster approval request")

			// The check below verifies that the new cluster approval request is owned by the re-created cluster staged update run
			// (with the UID of the re-created run).
			approvalRequestCreatedActual := approvalRequestCreatedActual(clusterScopedNS, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the new cluster approval request as expected")

			approvalRequestNotApprovedActual := approvalRequestConditionsActual(clusterScopedNS, approvalRequestName, nil)
			Expect(approvalRequestNotApprovedActual()).To(Succeed(), "The new cluster approval request has been approved unexpectedly")
		})

		It("should not roll out to the member cluster before the new cluster approval request is approved", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], previousResourceSnapshotName)
			Consistently(bindingUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the member cluster unexpectedly")
		})

		It("can approve the new cluster approval request", func() {
			approveApprovalRequest(clusterScopedNS, approvalRequestName)
		})

		It("should accept the approval", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(clusterScopedNS, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("should complete the cluster staged update run", func() {
			wantStatus := wantSingleClusterCompletedStatus(snapshotName, memberClusterNames, bindingNames,
				[]rolloutv1alpha1.PerStageTaskStatus{
					wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
				})
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newApprovalRequestObj(clusterScopedNS, approvalRequestName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("cluster approval request not owned by the cluster staged update run", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          canaryStageName,
					LabelSelector: &metav1.LabelSelector{},
					BeforeStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
				},
			})

			// Create (and approve) a cluster approval request of the expected name manually, before the cluster staged update run is
			// created. Note that the request references the cluster staged update run in its spec, so that its deletion
			// triggers the processing of the run.
			createManualApprovalRequest(clusterScopedNS, approvalRequestName, runName, canaryStageName)
			approveApprovalRequest(clusterScopedNS, approvalRequestName)
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should report that the cluster approval request is not owned by the cluster staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
						BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestOwnershipMismatchCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the ownership mismatch")
		})

		It("should not roll out to the member cluster, even though the cluster approval request has been approved", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], previousResourceSnapshotName)
			Consistently(bindingUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to the member cluster unexpectedly")
		})

		It("can delete the manually created cluster approval request", func() {
			// The controller re-creates the cluster approval request right away; do not wait for the name to be gone.
			Expect(hubClient.Delete(ctx, newApprovalRequestObj(clusterScopedNS, approvalRequestName))).To(Succeed(), "Failed to delete the cluster approval request")
		})

		It("should create its own cluster approval request", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(clusterScopedNS, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the cluster approval request")
		})

		It("should report that the cluster approval request has been created", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    []metav1.Condition{stagedUpdateRunInitializedCond(), stagedUpdateRunStartedCond()},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
						BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond()),
						},
						Conditions:     []metav1.Condition{perStageStartedCond()},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the creation of the cluster approval request")
		})

		It("can approve the cluster approval request", func() {
			approveApprovalRequest(clusterScopedNS, approvalRequestName)
		})

		It("should accept the approval", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(clusterScopedNS, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("should complete the cluster staged update run", func() {
			wantStatus := wantSingleClusterCompletedStatus(snapshotName, memberClusterNames, bindingNames,
				[]rolloutv1alpha1.PerStageTaskStatus{
					wantApprovalTaskStatus(approvalRequestName, perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
				})
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newApprovalRequestObj(clusterScopedNS, approvalRequestName),
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
