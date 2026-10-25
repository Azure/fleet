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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

// A substring of the message that the controller reports when the placement policy of a staged update run cannot
// be found.
const noLinkedPlacementPolicyMsgSubstr = "No linked placement policy"

// The binding manager claim details of another (fictional) controller.
const (
	otherBindingManagerControllerName = "other-controller"
	otherBindingManagerAPIGroup       = "example.kubefleet.dev"
	otherBindingManagerAPIVersion     = "v1"
	otherBindingManagerKind           = "OtherRollout"
	otherBindingManagerNameTemplate   = "other-rollout-%s"
)

// beforeStageApprovalStages returns the stages of a staged update strategy with a single (canary) stage, which has
// a before-stage approval task and includes all the member clusters.
func beforeStageApprovalStages() []rolloutv1alpha1.Stage {
	return []rolloutv1alpha1.Stage{
		{
			Name:          canaryStageName,
			LabelSelector: &metav1.LabelSelector{},
			BeforeStageTasks: []rolloutv1alpha1.StageTask{
				{
					Type: rolloutv1alpha1.StageTaskTypeApproval,
				},
			},
		},
	}
}

// wantWaitingForBeforeStageApprovalStatus builds the status of a staged update run (of the stages that
// beforeStageApprovalStages returns, with member cluster 0 only) that is waiting for the before-stage approval.
func wantWaitingForBeforeStageApprovalStatus(
	snapshotName string,
	memberClusterNames, bindingNames []string,
	approvalRequestName string,
) *rolloutv1alpha1.StagedUpdateRunStatus {
	return &rolloutv1alpha1.StagedUpdateRunStatus{
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
}

var _ = Describe("managing the lifecycle of staged update runs", func() {
	Context("staged update runs waiting for approval", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)
		triggerApprovalRequestName := fmt.Sprintf(triggerApprovalRequestNameTemplate, randStr)

		// The (only) stage has a before-stage approval task, which is never approved; the staged update run stays
		// in progress throughout the test case.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should report that the staged update run is waiting for approval", func() {
			wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestName)
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("should add the cleanup finalizer to the staged update run", func() {
			finalizerAddedActual := stagedUpdateRunCleanupFinalizerAddedActual(appNamespaceName, runName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add the cleanup finalizer")
		})

		It("should claim the binding manager role of the placement policy", func() {
			roleHeldActual := bindingManagerRoleHeldActual(appNamespaceName, policyName, []placementv1alpha1.ObjectReference{
				wantStagedUpdateRunObjectRef(appNamespaceName, runName),
			})
			Eventually(roleHeldActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to claim the binding manager role")
		})

		It("can trigger a reconciliation of the staged update run", func() {
			// The run is not requeued while it waits for approval; trigger a reconciliation, so that the check below
			// covers what the controller does when it processes the run again.
			triggerStagedUpdateRunReconciliation(appNamespaceName, runName, canaryStageName, triggerApprovalRequestName)
		})

		It("should keep the cleanup finalizer and the binding manager role while waiting for approval", func() {
			wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestName)
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			finalizerAddedActual := stagedUpdateRunCleanupFinalizerAddedActual(appNamespaceName, runName)
			roleHeldActual := bindingManagerRoleHeldActual(appNamespaceName, policyName, []placementv1alpha1.ObjectReference{
				wantStagedUpdateRunObjectRef(appNamespaceName, runName),
			})
			Consistently(func() error {
				if err := rolloutStatusActual(); err != nil {
					return err
				}
				if err := finalizerAddedActual(); err != nil {
					return err
				}
				return roleHeldActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep the staged update run waiting for approval, with the cleanup finalizer and the binding manager role")
		})

		AfterAll(func() {
			// The staged update run is still in progress; its deletion is processed (the binding manager role is
			// released and the cleanup finalizer is removed) by the controller.
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newApprovalRequestObj(appNamespaceName, approvalRequestName),
					newApprovalRequestObj(appNamespaceName, triggerApprovalRequestName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("completed staged update runs", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		triggerApprovalRequestName := fmt.Sprintf(triggerApprovalRequestNameTemplate, randStr)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// The status of the staged update run once completed.
		wantCompletedStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
			Conditions: []metav1.Condition{
				stagedUpdateRunInitializedCond(),
				stagedUpdateRunStartedCond(),
				stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
			},
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

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			wantCompletedStatus.ResourceSnapshotNameToRollout = snapshotName
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

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("should complete the staged update run", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantCompletedStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		It("should remove the cleanup finalizer from the staged update run", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(appNamespaceName, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
		})

		It("should release the binding manager role of the placement policy", func() {
			roleReleasedActual := bindingManagerRoleReleasedActual(appNamespaceName, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		It("can point the placement binding to another resource snapshot and mark it as unavailable", func() {
			updatePlacementBindingSpec(appNamespaceName, bindingNames[0], func(spec *placementv1alpha1.PlacementBindingSpec) {
				spec.ResourceSnapshotName = conflictingResourceSnapshotName
			})
			setPlacementBindingConditions(appNamespaceName, bindingNames[0], conflictingResourceSnapshotName, metav1.ConditionFalse, metav1.ConditionFalse, 0)
		})

		It("can trigger a reconciliation of the completed staged update run", func() {
			// The controller does not watch placement bindings.
			triggerStagedUpdateRunReconciliation(appNamespaceName, runName, restStageName, triggerApprovalRequestName)
		})

		It("should not process the completed staged update run again", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantCompletedStatus)
			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], conflictingResourceSnapshotName)
			Consistently(func() error {
				if err := rolloutStatusActual(); err != nil {
					return err
				}
				return bindingUntouchedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Processed the completed staged update run again")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newApprovalRequestObj(appNamespaceName, triggerApprovalRequestName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("missing placement policy at creation", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		triggerApprovalRequestName := fmt.Sprintf(triggerApprovalRequestNameTemplate, randStr)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, strategy)...)

			// The placement policy has not been created yet.
			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should fail to initialize the staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, runName, noLinkedPlacementPolicyMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("should remove the cleanup finalizer from the staged update run", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(appNamespaceName, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
		})

		It("can create the placement policy", func() {
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			waitForObjectsToBeCached(policy)
		})

		It("can trigger a new reconciliation of the staged update run", func() {
			// The controller does not watch placement policies. The trigger leaves the run as is (in particular, the
			// run is not suspended): had the controller initialized the run now that the policy exists, it would go on
			// to claim the binding manager role and roll out, which the check below would catch.
			triggerStagedUpdateRunReconciliation(appNamespaceName, runName, restStageName, triggerApprovalRequestName)
		})

		It("should not initialize the staged update run again", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, runName, noLinkedPlacementPolicyMsgSubstr)
			roleReleasedActual := bindingManagerRoleReleasedActual(appNamespaceName, policyName)
			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], previousResourceSnapshotName)
			Consistently(func() error {
				if err := failedToInitializeActual(); err != nil {
					return err
				}
				if err := roleReleasedActual(); err != nil {
					return err
				}
				return bindingUntouchedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Initialized the staged update run, claimed the binding manager role, or rolled out unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newApprovalRequestObj(appNamespaceName, triggerApprovalRequestName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("placement policy deleted while waiting for approval", Ordered, func() {
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
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should report that the staged update run is waiting for approval", func() {
			wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestName)
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can delete the placement policy", func() {
			removeObjects(newPlacementPolicyObj(appNamespaceName, policyName))
		})

		It("can approve the approval request", func() {
			// The controller does not watch placement policies; it is the approval that triggers the processing
			// of the staged update run, which then finds the placement policy gone.
			approveApprovalRequest(appNamespaceName, approvalRequestName)
		})

		It("should fail the staged update run", func() {
			// The staged update run fails before it processes the stage (i.e., the approval is not processed).
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
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the staged update run as failed")

			completedMsgActual := stagedUpdateRunConditionMessageActual(appNamespaceName, runName,
				rolloutv1alpha1.StagedUpdateRunCondTypeCompleted, noLinkedPlacementPolicyMsgSubstr)
			Expect(completedMsgActual()).To(Succeed(), "Failed to report why the staged update run has failed")
		})

		It("should remove the cleanup finalizer from the staged update run", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(appNamespaceName, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
		})

		It("should not roll out to the member cluster", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], previousResourceSnapshotName)
			Expect(bindingUntouchedActual()).To(Succeed(), "Rolled out to the member cluster unexpectedly")
		})

		AfterAll(func() {
			// The envtest environment does not run the garbage collector; the resource snapshot that the deleted
			// placement policy owns must be removed explicitly.
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newApprovalRequestObj(appNamespaceName, approvalRequestName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("binding manager role held by another controller", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// The binding manager claim of another controller.
		otherBindingManager := &placementv1alpha1.BindingManager{
			ControllerName: otherBindingManagerControllerName,
			ObjectRefs: []placementv1alpha1.ObjectReference{
				{
					APIGroup:   otherBindingManagerAPIGroup,
					APIVersion: otherBindingManagerAPIVersion,
					Kind:       otherBindingManagerKind,
					Name:       fmt.Sprintf(otherBindingManagerNameTemplate, randStr),
				},
			},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			updatePlacementPolicyBindingManager(appNamespaceName, policyName, otherBindingManager)
			// Wait until the cache (which the controller shares) observes the claim of the other controller, so that
			// the staged update run never sees the role as free.
			otherRoleHeldActual := bindingManagerActual(appNamespaceName, policyName, otherBindingManager)
			Eventually(otherRoleHeldActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the claim of the other controller")
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should add the cleanup finalizer to the staged update run", func() {
			finalizerAddedActual := stagedUpdateRunCleanupFinalizerAddedActual(appNamespaceName, runName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add the cleanup finalizer")
		})

		It("should not initialize the staged update run while another controller holds the binding manager role", func() {
			// The controller adds the cleanup finalizer right before it tries to claim the role, and keeps retrying
			// (with backoff) afterwards; in the meantime, the status of the staged update run stays empty (binding
			// manager conflicts are not reported in the status), the role stays with the other controller, and the
			// placement binding stays untouched.
			statusUnchangedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, runName, &rolloutv1alpha1.StagedUpdateRunStatus{})
			otherRoleHeldActual := bindingManagerActual(appNamespaceName, policyName, otherBindingManager)
			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], previousResourceSnapshotName)
			Consistently(func() error {
				if err := statusUnchangedActual(); err != nil {
					return err
				}
				if err := otherRoleHeldActual(); err != nil {
					return err
				}
				return bindingUntouchedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Proceeded with the staged update run unexpectedly")
		})

		It("can release the binding manager role held by the other controller", func() {
			updatePlacementPolicyBindingManager(appNamespaceName, policyName, nil)
		})

		It("should initialize the staged update run", func() {
			wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestName)
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
		})

		It("should claim the binding manager role of the placement policy", func() {
			roleHeldActual := bindingManagerRoleHeldActual(appNamespaceName, policyName, []placementv1alpha1.ObjectReference{
				wantStagedUpdateRunObjectRef(appNamespaceName, runName),
			})
			Eventually(roleHeldActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to claim the binding manager role")
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

	Context("multiple staged update runs of the same placement policy", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		var snapshotName string

		// Both staged update runs roll out to the same placement policy, and wait for approval.
		runNames := []string{
			fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-0"),
			fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-1"),
		}
		approvalRequestNames := []string{
			fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runNames[0], canaryStageName),
			fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runNames[1], canaryStageName),
		}

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			for _, runName := range runNames {
				createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
					PlacementPolicyName:      policyName,
					StagedUpdateStrategyName: strategyName,
				})
			}
		})

		It("should report that both staged update runs are waiting for approval", func() {
			for idx, runName := range runNames {
				wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestNames[idx])
				rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
				Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of staged update run %s", runName)
			}
		})

		It("should hold the binding manager role of the placement policy on behalf of both staged update runs", func() {
			roleHeldActual := bindingManagerRoleHeldActual(appNamespaceName, policyName, []placementv1alpha1.ObjectReference{
				wantStagedUpdateRunObjectRef(appNamespaceName, runNames[0]),
				wantStagedUpdateRunObjectRef(appNamespaceName, runNames[1]),
			})
			Eventually(roleHeldActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to claim the binding manager role for both staged update runs")
		})

		It("can delete the first staged update run", func() {
			removeObjects(newStagedUpdateRunObj(appNamespaceName, runNames[0]))
		})

		It("should hold the binding manager role on behalf of the second staged update run only", func() {
			roleHeldActual := bindingManagerRoleHeldActual(appNamespaceName, policyName, []placementv1alpha1.ObjectReference{
				wantStagedUpdateRunObjectRef(appNamespaceName, runNames[1]),
			})
			Eventually(roleHeldActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role for the first staged update run only")
		})

		It("can approve the approval request of the second staged update run", func() {
			approveApprovalRequest(appNamespaceName, approvalRequestNames[1])
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("should complete the second staged update run", func() {
			wantStatus := wantSingleClusterCompletedStatus(snapshotName, memberClusterNames, bindingNames,
				[]rolloutv1alpha1.PerStageTaskStatus{
					wantApprovalTaskStatus(approvalRequestNames[1], perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
				})
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runNames[1], wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the second staged update run")
		})

		It("should release the binding manager role of the placement policy", func() {
			roleReleasedActual := bindingManagerRoleReleasedActual(appNamespaceName, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runNames[0]),
					newStagedUpdateRunObj(appNamespaceName, runNames[1]),
					newApprovalRequestObj(appNamespaceName, approvalRequestNames[0]),
					newApprovalRequestObj(appNamespaceName, approvalRequestNames[1]),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("staged update runs deleted while a member cluster is being updated", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		// The canary stage (with a before-stage approval task) updates member cluster 0 and then member cluster 1;
		// the staged update run is deleted while member cluster 0 is being updated.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		var runUID types.UID

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should create the approval request, with the staged update run as its controller", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(appNamespaceName, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the approval request")

			run, err := getStagedUpdateRun(appNamespaceName, runName)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the staged update run")
			runUID = run.GetUID()
		})

		It("can approve the approval request", func() {
			approveApprovalRequest(appNamespaceName, approvalRequestName)
		})

		It("should start rolling out to member cluster 0", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 0")
		})

		It("can delete the staged update run", func() {
			Expect(hubClient.Delete(ctx, newStagedUpdateRunObj(appNamespaceName, runName))).To(Succeed(), "Failed to delete the staged update run")
		})

		It("should remove the staged update run", func() {
			// The controller processes the deletion once the in-flight reconciliation (which waits for member
			// cluster 0 to become available) runs out of its processing time budget.
			runRemovedActual := objectRemovedActual(newStagedUpdateRunObj(appNamespaceName, runName))
			Eventually(runRemovedActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to remove the staged update run")
		})

		It("should release the binding manager role of the placement policy", func() {
			roleReleasedActual := bindingManagerRoleReleasedActual(appNamespaceName, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		It("should leave the placement bindings as they are", func() {
			// The placement binding of member cluster 0 is not rolled back, and the one of member cluster 1 is not
			// rolled out to.
			Consistently(func() error {
				if err := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)(); err != nil {
					return err
				}
				return placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[1], previousResourceSnapshotName)()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Changed the placement bindings unexpectedly")
		})

		It("should leave the approval request in place, still owned by the deleted staged update run", func() {
			// The envtest environment does not run the garbage collector, which would otherwise remove the approval
			// request as its owner is gone.
			ownerRefsActual := approvalRequestOwnerRefsActual(appNamespaceName, approvalRequestName,
				wantStagedUpdateRunOwnerRefs(appNamespaceName, runName, runUID))
			Expect(ownerRefsActual()).To(Succeed(), "Failed to keep the approval request as it is")
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

	Context("staged update runs deleted after their placement policy", Ordered, func() {
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
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should report that the staged update run is waiting for approval", func() {
			wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestName)
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")

			finalizerAddedActual := stagedUpdateRunCleanupFinalizerAddedActual(appNamespaceName, runName)
			Expect(finalizerAddedActual()).To(Succeed(), "The staged update run does not have the cleanup finalizer")
		})

		It("can delete the placement policy", func() {
			removeObjects(newPlacementPolicyObj(appNamespaceName, policyName))
		})

		It("can delete the staged update run", func() {
			Expect(hubClient.Delete(ctx, newStagedUpdateRunObj(appNamespaceName, runName))).To(Succeed(), "Failed to delete the staged update run")
		})

		It("should remove the cleanup finalizer and the staged update run, even though the placement policy is gone", func() {
			runRemovedActual := objectRemovedActual(newStagedUpdateRunObj(appNamespaceName, runName))
			Eventually(runRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newApprovalRequestObj(appNamespaceName, approvalRequestName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("staged update runs that failed to initialize", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			waitForObjectsToBeCached(append(fixtures, policy)...)

			// The staged update strategy does not exist.
			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should fail to initialize the staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, runName, strategyNotFoundMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("should remove the cleanup finalizer from the staged update run", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(appNamespaceName, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
		})

		It("can delete the staged update run", func() {
			Expect(hubClient.Delete(ctx, newStagedUpdateRunObj(appNamespaceName, runName))).To(Succeed(), "Failed to delete the staged update run")
		})

		It("should remove the staged update run right away", func() {
			runRemovedActual := objectRemovedActual(newStagedUpdateRunObj(appNamespaceName, runName))
			Eventually(runRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("staged update runs suspended while waiting for approval", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		// The staged update run is created at generation 1, suspended at generation 2, and resumed at generation 3.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// The status of the staged update run while it is suspended (waiting for approval). The controller refreshes
		// the observed generation of the Initialized condition only.
		wantSuspendedStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
			Conditions: []metav1.Condition{
				observedAt(2, stagedUpdateRunInitializedCond()),
				observedAt(1, stagedUpdateRunStartedCond()),
				observedAt(2, stagedUpdateRunSuspendedCond()),
			},
			Stages: []rolloutv1alpha1.PerStageStatus{
				{
					StageName: canaryStageName,
					Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
					BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
						wantApprovalTaskStatus(approvalRequestName, observedAt(1, perStageTaskApprovalRequestCreatedCond())),
					},
					Conditions:     []metav1.Condition{observedAt(1, perStageStartedCond())},
					MaxConcurrency: ptr.To(int32(1)),
				},
			},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			wantSuspendedStatus.ResourceSnapshotNameToRollout = snapshotName
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should report that the staged update run is waiting for approval", func() {
			wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestName)
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the staged update run")
		})

		It("can suspend the staged update run", func() {
			updateStagedUpdateRunSuspended(appNamespaceName, runName, true)
		})

		It("should mark the staged update run as suspended", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(appNamespaceName, runName, wantSuspendedStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to suspend the staged update run")
		})

		It("should remove the cleanup finalizer and release the binding manager role", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(appNamespaceName, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")

			roleReleasedActual := bindingManagerRoleReleasedActual(appNamespaceName, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		It("can approve the approval request while the staged update run is suspended", func() {
			approveApprovalRequest(appNamespaceName, approvalRequestName)
		})

		It("should not process the approval or roll out while the staged update run is suspended", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(appNamespaceName, runName, wantSuspendedStatus)
			approvalNotAcceptedActual := approvalRequestConditionsActual(appNamespaceName, approvalRequestName, []metav1.Condition{
				approvalRequestApprovedCond(metav1.ConditionTrue),
			})
			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], previousResourceSnapshotName)
			Consistently(func() error {
				if err := rolloutStatusActual(); err != nil {
					return err
				}
				if err := approvalNotAcceptedActual(); err != nil {
					return err
				}
				return bindingUntouchedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Proceeded with the suspended staged update run unexpectedly")
		})

		It("can resume the staged update run", func() {
			updateStagedUpdateRunSuspended(appNamespaceName, runName, false)
		})

		It("should mark the staged update run as resumed", func() {
			resumedActual := stagedUpdateRunConditionActual(appNamespaceName, runName, stagedUpdateRunResumedCond())
			Eventually(resumedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to resume the staged update run")
		})

		It("should accept the approval and start rolling out to the member cluster", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(appNamespaceName, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")

			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("should add the cleanup finalizer and claim the binding manager role again", func() {
			finalizerAddedActual := stagedUpdateRunCleanupFinalizerAddedActual(appNamespaceName, runName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add the cleanup finalizer")

			roleHeldActual := bindingManagerRoleHeldActual(appNamespaceName, policyName, []placementv1alpha1.ObjectReference{
				wantStagedUpdateRunObjectRef(appNamespaceName, runName),
			})
			Eventually(roleHeldActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to claim the binding manager role")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("should complete the staged update run", func() {
			// The controller does not refresh the observed generations of the conditions of stage tasks.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					observedAt(3, stagedUpdateRunInitializedCond()),
					observedAt(3, stagedUpdateRunResumedCond()),
					observedAt(3, stagedUpdateRunStartedCond()),
					observedAt(3, stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded)),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0,
								observedAt(3, perClusterStartedCond()),
								observedAt(3, perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded))),
						},
						BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(approvalRequestName,
								observedAt(1, perStageTaskApprovalRequestCreatedCond()),
								observedAt(3, perStageTaskApprovalRequestApprovedCond())),
						},
						Conditions: []metav1.Condition{
							observedAt(3, perStageStartedCond()),
							observedAt(3, perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonSucceeded)),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(appNamespaceName, runName, wantStatus)
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

	Context("staged update runs suspended while a member cluster is being updated", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		triggerApprovalRequestName := fmt.Sprintf(triggerApprovalRequestNameTemplate, randStr)

		// The (only) stage updates member cluster 0 and then member cluster 1. The staged update run is created at
		// generation 1, suspended at generation 2 (while member cluster 0 is being updated), and resumed at
		// generation 3.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		// The status of the staged update run while it is suspended. The controller refreshes the observed generation
		// of the Initialized condition only.
		wantSuspendedStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
			Conditions: []metav1.Condition{
				observedAt(2, stagedUpdateRunInitializedCond()),
				observedAt(1, stagedUpdateRunStartedCond()),
				observedAt(2, stagedUpdateRunSuspendedCond()),
			},
			Stages: []rolloutv1alpha1.PerStageStatus{
				{
					StageName: restStageName,
					Clusters: []rolloutv1alpha1.PerClusterStatus{
						wantPerClusterStatus(memberClusterNames, bindingNames, 0, observedAt(1, perClusterStartedCond())),
						wantPerClusterStatus(memberClusterNames, bindingNames, 1),
					},
					Conditions:     []metav1.Condition{observedAt(1, perStageStartedCond())},
					MaxConcurrency: ptr.To(int32(1)),
				},
			},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			wantSuspendedStatus.ResourceSnapshotNameToRollout = snapshotName
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil})
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

		It("should start rolling out to member cluster 0", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 0")
		})

		It("should report that member cluster 0 is being updated", func() {
			// The staged update run is suspended only after the persisted status shows that the update on member
			// cluster 0 has started; the reconciliation in flight at the time of the suspension fails to persist its
			// status (as the run has changed in the meantime).
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

		It("can suspend the staged update run", func() {
			updateStagedUpdateRunSuspended(appNamespaceName, runName, true)
		})

		It("should mark the staged update run as suspended", func() {
			// The suspension takes effect once the reconciliation in flight (which waits for member cluster 0, kept
			// unavailable until now) runs out of its processing time budget.
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(appNamespaceName, runName, wantSuspendedStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to suspend the staged update run")
		})

		It("should remove the cleanup finalizer and release the binding manager role", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(appNamespaceName, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")

			roleReleasedActual := bindingManagerRoleReleasedActual(appNamespaceName, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		It("can mark member cluster 0 as available while the staged update run is suspended", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("can trigger a reconciliation of the suspended staged update run", func() {
			// The controller does not watch placement bindings; without a reconciliation, the check below would pass
			// even if the controller ignored the suspension.
			triggerStagedUpdateRunReconciliation(appNamespaceName, runName, restStageName, triggerApprovalRequestName)
		})

		It("should not process the staged update run while it is suspended", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(appNamespaceName, runName, wantSuspendedStatus)
			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[1], previousResourceSnapshotName)
			Consistently(func() error {
				if err := rolloutStatusActual(); err != nil {
					return err
				}
				return bindingUntouchedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Proceeded with the suspended staged update run unexpectedly")
		})

		It("can resume the staged update run", func() {
			updateStagedUpdateRunSuspended(appNamespaceName, runName, false)
		})

		It("should start rolling out to member cluster 1", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 1")
		})

		It("can mark member cluster 1 as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[1], snapshotName)
		})

		It("should complete the staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					observedAt(3, stagedUpdateRunInitializedCond()),
					observedAt(3, stagedUpdateRunResumedCond()),
					observedAt(3, stagedUpdateRunStartedCond()),
					observedAt(3, stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded)),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0,
								observedAt(3, perClusterStartedCond()),
								observedAt(3, perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded))),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1,
								observedAt(3, perClusterStartedCond()),
								observedAt(3, perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded))),
						},
						Conditions: []metav1.Condition{
							observedAt(3, perStageStartedCond()),
							observedAt(3, perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonSucceeded)),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(appNamespaceName, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the staged update run")
		})

		It("should restart the update on member cluster 0 (and its wait time limit) upon resumption", func() {
			run, err := getStagedUpdateRun(appNamespaceName, runName)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the staged update run")
			resumedCond := meta.FindStatusCondition(run.GetStatus().Conditions, rolloutv1alpha1.StagedUpdateRunCondTypeSuspended)
			Expect(resumedCond).ToNot(BeNil(), "The staged update run has no Suspended condition")

			startedCond, err := getPerClusterCondition(appNamespaceName, runName, memberClusterNames[0], rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeStarted)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the Started condition of member cluster 0")
			Expect(startedCond).ToNot(BeNil(), "Member cluster 0 has no Started condition")
			Expect(startedCond.LastTransitionTime.Before(&resumedCond.LastTransitionTime)).To(BeFalse(),
				"The update on member cluster 0 started (%v) before the staged update run was resumed (%v)",
				startedCond.LastTransitionTime, resumedCond.LastTransitionTime)
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newApprovalRequestObj(appNamespaceName, triggerApprovalRequestName),
					newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("suspending and resuming finished staged update runs", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		missingStrategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, utils.RandStr())
		var snapshotName string

		// The first staged update run completes successfully; the second one fails to initialize (its staged update
		// strategy does not exist). Both are created at generation 1, suspended at generation 2, and resumed at
		// generation 3.
		completedRunName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-0")
		failedRunName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-1")

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// wantCompletedStatus returns the status of the completed staged update run, with its Completed condition
		// observed at the given generation. The controller refreshes the observed generation of the Completed
		// condition only.
		wantCompletedStatus := func(completedCondObsGen int64) *rolloutv1alpha1.StagedUpdateRunStatus {
			return &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					observedAt(1, stagedUpdateRunInitializedCond()),
					observedAt(1, stagedUpdateRunStartedCond()),
					observedAt(completedCondObsGen, stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded)),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0,
								observedAt(1, perClusterStartedCond()),
								observedAt(1, perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded))),
						},
						Conditions: []metav1.Condition{
							observedAt(1, perStageStartedCond()),
							observedAt(1, perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonSucceeded)),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, completedRunName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(appNamespaceName, bindingNames[0], snapshotName)
		})

		It("should complete the first staged update run", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(appNamespaceName, completedRunName, wantCompletedStatus(1))
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the first staged update run")
		})

		It("can create the second staged update run", func() {
			createStagedUpdateRun(appNamespaceName, failedRunName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: missingStrategyName,
			})
		})

		It("should fail to initialize the second staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, failedRunName, strategyNotFoundMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("can suspend both staged update runs", func() {
			updateStagedUpdateRunSuspended(appNamespaceName, completedRunName, true)
			updateStagedUpdateRunSuspended(appNamespaceName, failedRunName, true)
		})

		It("should refresh the observed generations of the terminal conditions, without marking the staged update runs as suspended", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(appNamespaceName, completedRunName, wantCompletedStatus(2))
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the Completed condition of the first staged update run")

			// The check expects the Initialized condition (the only condition) to be observed at the current
			// generation of the run.
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, failedRunName, strategyNotFoundMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the Initialized condition of the second staged update run")
		})

		It("can resume both staged update runs", func() {
			updateStagedUpdateRunSuspended(appNamespaceName, completedRunName, false)
			updateStagedUpdateRunSuspended(appNamespaceName, failedRunName, false)
		})

		It("should refresh the observed generations of the terminal conditions again, without marking the staged update runs as resumed", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(appNamespaceName, completedRunName, wantCompletedStatus(3))
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the Completed condition of the first staged update run")

			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, failedRunName, strategyNotFoundMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the Initialized condition of the second staged update run")

			bindingUntouchedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Expect(bindingUntouchedActual()).To(Succeed(), "The placement binding has been changed unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, completedRunName),
					newStagedUpdateRunObj(appNamespaceName, failedRunName),
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

var _ = Describe("managing the lifecycle of cluster staged update runs", func() {
	Context("cluster staged update runs waiting for approval", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)
		triggerApprovalRequestName := fmt.Sprintf(triggerApprovalRequestNameTemplate, randStr)

		// The (only) stage has a before-stage approval task, which is never approved; the cluster staged update run stays
		// in progress throughout the test case.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should report that the cluster staged update run is waiting for approval", func() {
			wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestName)
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("should add the cleanup finalizer to the cluster staged update run", func() {
			finalizerAddedActual := stagedUpdateRunCleanupFinalizerAddedActual(clusterScopedNS, runName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add the cleanup finalizer")
		})

		It("should claim the binding manager role of the cluster placement policy", func() {
			roleHeldActual := bindingManagerRoleHeldActual(clusterScopedNS, policyName, []placementv1alpha1.ObjectReference{
				wantStagedUpdateRunObjectRef(clusterScopedNS, runName),
			})
			Eventually(roleHeldActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to claim the binding manager role")
		})

		It("can trigger a reconciliation of the cluster staged update run", func() {
			// The run is not requeued while it waits for approval; trigger a reconciliation, so that the check below
			// covers what the controller does when it processes the run again.
			triggerStagedUpdateRunReconciliation(clusterScopedNS, runName, canaryStageName, triggerApprovalRequestName)
		})

		It("should keep the cleanup finalizer and the binding manager role while waiting for approval", func() {
			wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestName)
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			finalizerAddedActual := stagedUpdateRunCleanupFinalizerAddedActual(clusterScopedNS, runName)
			roleHeldActual := bindingManagerRoleHeldActual(clusterScopedNS, policyName, []placementv1alpha1.ObjectReference{
				wantStagedUpdateRunObjectRef(clusterScopedNS, runName),
			})
			Consistently(func() error {
				if err := rolloutStatusActual(); err != nil {
					return err
				}
				if err := finalizerAddedActual(); err != nil {
					return err
				}
				return roleHeldActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to keep the cluster staged update run waiting for approval, with the cleanup finalizer and the binding manager role")
		})

		AfterAll(func() {
			// The cluster staged update run is still in progress; its deletion is processed (the binding manager role is
			// released and the cleanup finalizer is removed) by the controller.
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newApprovalRequestObj(clusterScopedNS, approvalRequestName),
					newApprovalRequestObj(clusterScopedNS, triggerApprovalRequestName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("completed cluster staged update runs", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		triggerApprovalRequestName := fmt.Sprintf(triggerApprovalRequestNameTemplate, randStr)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// The status of the cluster staged update run once completed.
		wantCompletedStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
			Conditions: []metav1.Condition{
				stagedUpdateRunInitializedCond(),
				stagedUpdateRunStartedCond(),
				stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded),
			},
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

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			wantCompletedStatus.ResourceSnapshotNameToRollout = snapshotName
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

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("should complete the cluster staged update run", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantCompletedStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		It("should remove the cleanup finalizer from the cluster staged update run", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(clusterScopedNS, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
		})

		It("should release the binding manager role of the cluster placement policy", func() {
			roleReleasedActual := bindingManagerRoleReleasedActual(clusterScopedNS, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		It("can point the placement binding to another resource snapshot and mark it as unavailable", func() {
			updatePlacementBindingSpec(clusterScopedNS, bindingNames[0], func(spec *placementv1alpha1.PlacementBindingSpec) {
				spec.ResourceSnapshotName = conflictingResourceSnapshotName
			})
			setPlacementBindingConditions(clusterScopedNS, bindingNames[0], conflictingResourceSnapshotName, metav1.ConditionFalse, metav1.ConditionFalse, 0)
		})

		It("can trigger a reconciliation of the completed cluster staged update run", func() {
			// The controller does not watch placement bindings.
			triggerStagedUpdateRunReconciliation(clusterScopedNS, runName, restStageName, triggerApprovalRequestName)
		})

		It("should not process the completed cluster staged update run again", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantCompletedStatus)
			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], conflictingResourceSnapshotName)
			Consistently(func() error {
				if err := rolloutStatusActual(); err != nil {
					return err
				}
				return bindingUntouchedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Processed the completed cluster staged update run again")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newApprovalRequestObj(clusterScopedNS, triggerApprovalRequestName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("missing cluster placement policy at creation", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		triggerApprovalRequestName := fmt.Sprintf(triggerApprovalRequestNameTemplate, randStr)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, strategy)...)

			// The cluster placement policy has not been created yet.
			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should fail to initialize the cluster staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, runName, noLinkedPlacementPolicyMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("should remove the cleanup finalizer from the cluster staged update run", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(clusterScopedNS, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
		})

		It("can create the cluster placement policy", func() {
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			waitForObjectsToBeCached(policy)
		})

		It("can trigger a new reconciliation of the cluster staged update run", func() {
			// The controller does not watch placement policies. The trigger leaves the run as is (in particular, the
			// run is not suspended): had the controller initialized the run now that the policy exists, it would go on
			// to claim the binding manager role and roll out, which the check below would catch.
			triggerStagedUpdateRunReconciliation(clusterScopedNS, runName, restStageName, triggerApprovalRequestName)
		})

		It("should not initialize the cluster staged update run again", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, runName, noLinkedPlacementPolicyMsgSubstr)
			roleReleasedActual := bindingManagerRoleReleasedActual(clusterScopedNS, policyName)
			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], previousResourceSnapshotName)
			Consistently(func() error {
				if err := failedToInitializeActual(); err != nil {
					return err
				}
				if err := roleReleasedActual(); err != nil {
					return err
				}
				return bindingUntouchedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Initialized the cluster staged update run, claimed the binding manager role, or rolled out unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newApprovalRequestObj(clusterScopedNS, triggerApprovalRequestName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("cluster placement policy deleted while waiting for approval", Ordered, func() {
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
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should report that the cluster staged update run is waiting for approval", func() {
			wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestName)
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can delete the cluster placement policy", func() {
			removeObjects(newPlacementPolicyObj(clusterScopedNS, policyName))
		})

		It("can approve the cluster approval request", func() {
			// The controller does not watch placement policies; it is the approval that triggers the processing
			// of the cluster staged update run, which then finds the cluster placement policy gone.
			approveApprovalRequest(clusterScopedNS, approvalRequestName)
		})

		It("should fail the cluster staged update run", func() {
			// The cluster staged update run fails before it processes the stage (i.e., the approval is not processed).
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
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the cluster staged update run as failed")

			completedMsgActual := stagedUpdateRunConditionMessageActual(clusterScopedNS, runName,
				rolloutv1alpha1.StagedUpdateRunCondTypeCompleted, noLinkedPlacementPolicyMsgSubstr)
			Expect(completedMsgActual()).To(Succeed(), "Failed to report why the cluster staged update run has failed")
		})

		It("should remove the cleanup finalizer from the cluster staged update run", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(clusterScopedNS, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
		})

		It("should not roll out to the member cluster", func() {
			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], previousResourceSnapshotName)
			Expect(bindingUntouchedActual()).To(Succeed(), "Rolled out to the member cluster unexpectedly")
		})

		AfterAll(func() {
			// The envtest environment does not run the garbage collector; the resource snapshot that the deleted
			// cluster placement policy owns must be removed explicitly.
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newApprovalRequestObj(clusterScopedNS, approvalRequestName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("binding manager role held by another controller", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// The binding manager claim of another controller.
		otherBindingManager := &placementv1alpha1.BindingManager{
			ControllerName: otherBindingManagerControllerName,
			ObjectRefs: []placementv1alpha1.ObjectReference{
				{
					APIGroup:   otherBindingManagerAPIGroup,
					APIVersion: otherBindingManagerAPIVersion,
					Kind:       otherBindingManagerKind,
					Name:       fmt.Sprintf(otherBindingManagerNameTemplate, randStr),
				},
			},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			updatePlacementPolicyBindingManager(clusterScopedNS, policyName, otherBindingManager)
			// Wait until the cache (which the controller shares) observes the claim of the other controller, so that
			// the cluster staged update run never sees the role as free.
			otherRoleHeldActual := bindingManagerActual(clusterScopedNS, policyName, otherBindingManager)
			Eventually(otherRoleHeldActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the claim of the other controller")
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should add the cleanup finalizer to the cluster staged update run", func() {
			finalizerAddedActual := stagedUpdateRunCleanupFinalizerAddedActual(clusterScopedNS, runName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add the cleanup finalizer")
		})

		It("should not initialize the cluster staged update run while another controller holds the binding manager role", func() {
			// The controller adds the cleanup finalizer right before it tries to claim the role, and keeps retrying
			// (with backoff) afterwards; in the meantime, the status of the cluster staged update run stays empty
			// (binding manager conflicts are not reported in the status), the role stays with the other controller,
			// and the placement binding stays untouched.
			statusUnchangedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, runName, &rolloutv1alpha1.StagedUpdateRunStatus{})
			otherRoleHeldActual := bindingManagerActual(clusterScopedNS, policyName, otherBindingManager)
			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], previousResourceSnapshotName)
			Consistently(func() error {
				if err := statusUnchangedActual(); err != nil {
					return err
				}
				if err := otherRoleHeldActual(); err != nil {
					return err
				}
				return bindingUntouchedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Proceeded with the cluster staged update run unexpectedly")
		})

		It("can release the binding manager role held by the other controller", func() {
			updatePlacementPolicyBindingManager(clusterScopedNS, policyName, nil)
		})

		It("should initialize the cluster staged update run", func() {
			wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestName)
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")
		})

		It("should claim the binding manager role of the cluster placement policy", func() {
			roleHeldActual := bindingManagerRoleHeldActual(clusterScopedNS, policyName, []placementv1alpha1.ObjectReference{
				wantStagedUpdateRunObjectRef(clusterScopedNS, runName),
			})
			Eventually(roleHeldActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to claim the binding manager role")
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

	Context("multiple cluster staged update runs of the same cluster placement policy", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		var snapshotName string

		// Both cluster staged update runs roll out to the same cluster placement policy, and wait for approval.
		runNames := []string{
			fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-0"),
			fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-1"),
		}
		approvalRequestNames := []string{
			fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runNames[0], canaryStageName),
			fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runNames[1], canaryStageName),
		}

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			for _, runName := range runNames {
				createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
					PlacementPolicyName:      policyName,
					StagedUpdateStrategyName: strategyName,
				})
			}
		})

		It("should report that both cluster staged update runs are waiting for approval", func() {
			for idx, runName := range runNames {
				wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestNames[idx])
				rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
				Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of cluster staged update run %s", runName)
			}
		})

		It("should hold the binding manager role of the cluster placement policy on behalf of both cluster staged update runs", func() {
			roleHeldActual := bindingManagerRoleHeldActual(clusterScopedNS, policyName, []placementv1alpha1.ObjectReference{
				wantStagedUpdateRunObjectRef(clusterScopedNS, runNames[0]),
				wantStagedUpdateRunObjectRef(clusterScopedNS, runNames[1]),
			})
			Eventually(roleHeldActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to claim the binding manager role for both cluster staged update runs")
		})

		It("can delete the first cluster staged update run", func() {
			removeObjects(newStagedUpdateRunObj(clusterScopedNS, runNames[0]))
		})

		It("should hold the binding manager role on behalf of the second cluster staged update run only", func() {
			roleHeldActual := bindingManagerRoleHeldActual(clusterScopedNS, policyName, []placementv1alpha1.ObjectReference{
				wantStagedUpdateRunObjectRef(clusterScopedNS, runNames[1]),
			})
			Eventually(roleHeldActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role for the first cluster staged update run only")
		})

		It("can approve the cluster approval request of the second cluster staged update run", func() {
			approveApprovalRequest(clusterScopedNS, approvalRequestNames[1])
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("should complete the second cluster staged update run", func() {
			wantStatus := wantSingleClusterCompletedStatus(snapshotName, memberClusterNames, bindingNames,
				[]rolloutv1alpha1.PerStageTaskStatus{
					wantApprovalTaskStatus(approvalRequestNames[1], perStageTaskApprovalRequestCreatedCond(), perStageTaskApprovalRequestApprovedCond()),
				})
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runNames[1], wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the second cluster staged update run")
		})

		It("should release the binding manager role of the cluster placement policy", func() {
			roleReleasedActual := bindingManagerRoleReleasedActual(clusterScopedNS, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runNames[0]),
					newStagedUpdateRunObj(clusterScopedNS, runNames[1]),
					newApprovalRequestObj(clusterScopedNS, approvalRequestNames[0]),
					newApprovalRequestObj(clusterScopedNS, approvalRequestNames[1]),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("cluster staged update runs deleted while a member cluster is being updated", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		// The canary stage (with a before-stage approval task) updates member cluster 0 and then member cluster 1;
		// the cluster staged update run is deleted while member cluster 0 is being updated.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		var runUID types.UID

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should create the cluster approval request, with the cluster staged update run as its controller", func() {
			approvalRequestCreatedActual := approvalRequestCreatedActual(clusterScopedNS, runName, approvalRequestName, canaryStageName)
			Eventually(approvalRequestCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the cluster approval request")

			run, err := getStagedUpdateRun(clusterScopedNS, runName)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the cluster staged update run")
			runUID = run.GetUID()
		})

		It("can approve the cluster approval request", func() {
			approveApprovalRequest(clusterScopedNS, approvalRequestName)
		})

		It("should start rolling out to member cluster 0", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 0")
		})

		It("can delete the cluster staged update run", func() {
			Expect(hubClient.Delete(ctx, newStagedUpdateRunObj(clusterScopedNS, runName))).To(Succeed(), "Failed to delete the cluster staged update run")
		})

		It("should remove the cluster staged update run", func() {
			// The controller processes the deletion once the in-flight reconciliation (which waits for member
			// cluster 0 to become available) runs out of its processing time budget.
			runRemovedActual := objectRemovedActual(newStagedUpdateRunObj(clusterScopedNS, runName))
			Eventually(runRemovedActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster staged update run")
		})

		It("should release the binding manager role of the cluster placement policy", func() {
			roleReleasedActual := bindingManagerRoleReleasedActual(clusterScopedNS, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		It("should leave the placement bindings as they are", func() {
			// The placement binding of member cluster 0 is not rolled back, and the one of member cluster 1 is not
			// rolled out to.
			Consistently(func() error {
				if err := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)(); err != nil {
					return err
				}
				return placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[1], previousResourceSnapshotName)()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Changed the placement bindings unexpectedly")
		})

		It("should leave the cluster approval request in place, still owned by the deleted cluster staged update run", func() {
			// The envtest environment does not run the garbage collector, which would otherwise remove the approval
			// request as its owner is gone.
			ownerRefsActual := approvalRequestOwnerRefsActual(clusterScopedNS, approvalRequestName,
				wantStagedUpdateRunOwnerRefs(clusterScopedNS, runName, runUID))
			Expect(ownerRefsActual()).To(Succeed(), "Failed to keep the cluster approval request as it is")
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

	Context("cluster staged update runs deleted after their cluster placement policy", Ordered, func() {
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
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should report that the cluster staged update run is waiting for approval", func() {
			wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestName)
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")

			finalizerAddedActual := stagedUpdateRunCleanupFinalizerAddedActual(clusterScopedNS, runName)
			Expect(finalizerAddedActual()).To(Succeed(), "The cluster staged update run does not have the cleanup finalizer")
		})

		It("can delete the cluster placement policy", func() {
			removeObjects(newPlacementPolicyObj(clusterScopedNS, policyName))
		})

		It("can delete the cluster staged update run", func() {
			Expect(hubClient.Delete(ctx, newStagedUpdateRunObj(clusterScopedNS, runName))).To(Succeed(), "Failed to delete the cluster staged update run")
		})

		It("should remove the cleanup finalizer and the cluster staged update run, even though the cluster placement policy is gone", func() {
			runRemovedActual := objectRemovedActual(newStagedUpdateRunObj(clusterScopedNS, runName))
			Eventually(runRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newApprovalRequestObj(clusterScopedNS, approvalRequestName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("cluster staged update runs that failed to initialize", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			waitForObjectsToBeCached(append(fixtures, policy)...)

			// The cluster staged update strategy does not exist.
			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should fail to initialize the cluster staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, runName, strategyNotFoundMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("should remove the cleanup finalizer from the cluster staged update run", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(clusterScopedNS, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
		})

		It("can delete the cluster staged update run", func() {
			Expect(hubClient.Delete(ctx, newStagedUpdateRunObj(clusterScopedNS, runName))).To(Succeed(), "Failed to delete the cluster staged update run")
		})

		It("should remove the cluster staged update run right away", func() {
			runRemovedActual := objectRemovedActual(newStagedUpdateRunObj(clusterScopedNS, runName))
			Eventually(runRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster staged update run")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("cluster staged update runs suspended while waiting for approval", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		approvalRequestName := fmt.Sprintf(beforeStageApprovalRequestNameTemplate, runName, canaryStageName)

		// The cluster staged update run is created at generation 1, suspended at generation 2, and resumed at generation 3.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// The status of the cluster staged update run while it is suspended (waiting for approval). The controller refreshes
		// the observed generation of the Initialized condition only.
		wantSuspendedStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
			Conditions: []metav1.Condition{
				observedAt(2, stagedUpdateRunInitializedCond()),
				observedAt(1, stagedUpdateRunStartedCond()),
				observedAt(2, stagedUpdateRunSuspendedCond()),
			},
			Stages: []rolloutv1alpha1.PerStageStatus{
				{
					StageName: canaryStageName,
					Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
					BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
						wantApprovalTaskStatus(approvalRequestName, observedAt(1, perStageTaskApprovalRequestCreatedCond())),
					},
					Conditions:     []metav1.Condition{observedAt(1, perStageStartedCond())},
					MaxConcurrency: ptr.To(int32(1)),
				},
			},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			wantSuspendedStatus.ResourceSnapshotNameToRollout = snapshotName
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, beforeStageApprovalStages())
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should report that the cluster staged update run is waiting for approval", func() {
			wantStatus := wantWaitingForBeforeStageApprovalStatus(snapshotName, memberClusterNames, bindingNames, approvalRequestName)
			rolloutStatusActual := stagedUpdateRunRolloutStatusActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the progress of the cluster staged update run")
		})

		It("can suspend the cluster staged update run", func() {
			updateStagedUpdateRunSuspended(clusterScopedNS, runName, true)
		})

		It("should mark the cluster staged update run as suspended", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(clusterScopedNS, runName, wantSuspendedStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to suspend the cluster staged update run")
		})

		It("should remove the cleanup finalizer and release the binding manager role", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(clusterScopedNS, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")

			roleReleasedActual := bindingManagerRoleReleasedActual(clusterScopedNS, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		It("can approve the cluster approval request while the cluster staged update run is suspended", func() {
			approveApprovalRequest(clusterScopedNS, approvalRequestName)
		})

		It("should not process the approval or roll out while the cluster staged update run is suspended", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(clusterScopedNS, runName, wantSuspendedStatus)
			approvalNotAcceptedActual := approvalRequestConditionsActual(clusterScopedNS, approvalRequestName, []metav1.Condition{
				approvalRequestApprovedCond(metav1.ConditionTrue),
			})
			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], previousResourceSnapshotName)
			Consistently(func() error {
				if err := rolloutStatusActual(); err != nil {
					return err
				}
				if err := approvalNotAcceptedActual(); err != nil {
					return err
				}
				return bindingUntouchedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Proceeded with the suspended cluster staged update run unexpectedly")
		})

		It("can resume the cluster staged update run", func() {
			updateStagedUpdateRunSuspended(clusterScopedNS, runName, false)
		})

		It("should mark the cluster staged update run as resumed", func() {
			resumedActual := stagedUpdateRunConditionActual(clusterScopedNS, runName, stagedUpdateRunResumedCond())
			Eventually(resumedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to resume the cluster staged update run")
		})

		It("should accept the approval and start rolling out to the member cluster", func() {
			approvalAcceptedActual := approvalRequestApprovalAcceptedActual(clusterScopedNS, approvalRequestName)
			Eventually(approvalAcceptedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to accept the approval")

			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("should add the cleanup finalizer and claim the binding manager role again", func() {
			finalizerAddedActual := stagedUpdateRunCleanupFinalizerAddedActual(clusterScopedNS, runName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add the cleanup finalizer")

			roleHeldActual := bindingManagerRoleHeldActual(clusterScopedNS, policyName, []placementv1alpha1.ObjectReference{
				wantStagedUpdateRunObjectRef(clusterScopedNS, runName),
			})
			Eventually(roleHeldActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to claim the binding manager role")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("should complete the cluster staged update run", func() {
			// The controller does not refresh the observed generations of the conditions of stage tasks.
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					observedAt(3, stagedUpdateRunInitializedCond()),
					observedAt(3, stagedUpdateRunResumedCond()),
					observedAt(3, stagedUpdateRunStartedCond()),
					observedAt(3, stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded)),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: canaryStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0,
								observedAt(3, perClusterStartedCond()),
								observedAt(3, perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded))),
						},
						BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
							wantApprovalTaskStatus(approvalRequestName,
								observedAt(1, perStageTaskApprovalRequestCreatedCond()),
								observedAt(3, perStageTaskApprovalRequestApprovedCond())),
						},
						Conditions: []metav1.Condition{
							observedAt(3, perStageStartedCond()),
							observedAt(3, perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonSucceeded)),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(clusterScopedNS, runName, wantStatus)
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

	Context("cluster staged update runs suspended while a member cluster is being updated", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string
		triggerApprovalRequestName := fmt.Sprintf(triggerApprovalRequestNameTemplate, randStr)

		// The (only) stage updates member cluster 0 and then member cluster 1. The cluster staged update run is created at
		// generation 1, suspended at generation 2 (while member cluster 0 is being updated), and resumed at
		// generation 3.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		// The status of the cluster staged update run while it is suspended. The controller refreshes the observed generation
		// of the Initialized condition only.
		wantSuspendedStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
			Conditions: []metav1.Condition{
				observedAt(2, stagedUpdateRunInitializedCond()),
				observedAt(1, stagedUpdateRunStartedCond()),
				observedAt(2, stagedUpdateRunSuspendedCond()),
			},
			Stages: []rolloutv1alpha1.PerStageStatus{
				{
					StageName: restStageName,
					Clusters: []rolloutv1alpha1.PerClusterStatus{
						wantPerClusterStatus(memberClusterNames, bindingNames, 0, observedAt(1, perClusterStartedCond())),
						wantPerClusterStatus(memberClusterNames, bindingNames, 1),
					},
					Conditions:     []metav1.Condition{observedAt(1, perStageStartedCond())},
					MaxConcurrency: ptr.To(int32(1)),
				},
			},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			wantSuspendedStatus.ResourceSnapshotNameToRollout = snapshotName
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil, nil})
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

		It("should start rolling out to member cluster 0", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 0")
		})

		It("should report that member cluster 0 is being updated", func() {
			// The cluster staged update run is suspended only after the persisted status shows that the update on member
			// cluster 0 has started; the reconciliation in flight at the time of the suspension fails to persist its
			// status (as the run has changed in the meantime).
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

		It("can suspend the cluster staged update run", func() {
			updateStagedUpdateRunSuspended(clusterScopedNS, runName, true)
		})

		It("should mark the cluster staged update run as suspended", func() {
			// The suspension takes effect once the reconciliation in flight (which waits for member cluster 0, kept
			// unavailable until now) runs out of its processing time budget.
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(clusterScopedNS, runName, wantSuspendedStatus)
			Eventually(rolloutStatusActual, eventuallyDurationForBudgetExpiry, eventuallyInterval).Should(Succeed(), "Failed to suspend the cluster staged update run")
		})

		It("should remove the cleanup finalizer and release the binding manager role", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(clusterScopedNS, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")

			roleReleasedActual := bindingManagerRoleReleasedActual(clusterScopedNS, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		It("can mark member cluster 0 as available while the cluster staged update run is suspended", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("can trigger a reconciliation of the suspended cluster staged update run", func() {
			// The controller does not watch placement bindings; without a reconciliation, the check below would pass
			// even if the controller ignored the suspension.
			triggerStagedUpdateRunReconciliation(clusterScopedNS, runName, restStageName, triggerApprovalRequestName)
		})

		It("should not process the cluster staged update run while it is suspended", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(clusterScopedNS, runName, wantSuspendedStatus)
			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[1], previousResourceSnapshotName)
			Consistently(func() error {
				if err := rolloutStatusActual(); err != nil {
					return err
				}
				return bindingUntouchedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Proceeded with the suspended cluster staged update run unexpectedly")
		})

		It("can resume the cluster staged update run", func() {
			updateStagedUpdateRunSuspended(clusterScopedNS, runName, false)
		})

		It("should start rolling out to member cluster 1", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[1], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 1")
		})

		It("can mark member cluster 1 as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[1], snapshotName)
		})

		It("should complete the cluster staged update run", func() {
			wantStatus := &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					observedAt(3, stagedUpdateRunInitializedCond()),
					observedAt(3, stagedUpdateRunResumedCond()),
					observedAt(3, stagedUpdateRunStartedCond()),
					observedAt(3, stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded)),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0,
								observedAt(3, perClusterStartedCond()),
								observedAt(3, perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded))),
							wantPerClusterStatus(memberClusterNames, bindingNames, 1,
								observedAt(3, perClusterStartedCond()),
								observedAt(3, perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded))),
						},
						Conditions: []metav1.Condition{
							observedAt(3, perStageStartedCond()),
							observedAt(3, perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonSucceeded)),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(clusterScopedNS, runName, wantStatus)
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the cluster staged update run")
		})

		It("should restart the update on member cluster 0 (and its wait time limit) upon resumption", func() {
			run, err := getStagedUpdateRun(clusterScopedNS, runName)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the cluster staged update run")
			resumedCond := meta.FindStatusCondition(run.GetStatus().Conditions, rolloutv1alpha1.StagedUpdateRunCondTypeSuspended)
			Expect(resumedCond).ToNot(BeNil(), "The cluster staged update run has no Suspended condition")

			startedCond, err := getPerClusterCondition(clusterScopedNS, runName, memberClusterNames[0], rolloutv1alpha1.StagedUpdateRunPerClusterCondTypeStarted)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve the Started condition of member cluster 0")
			Expect(startedCond).ToNot(BeNil(), "Member cluster 0 has no Started condition")
			Expect(startedCond.LastTransitionTime.Before(&resumedCond.LastTransitionTime)).To(BeFalse(),
				"The update on member cluster 0 started (%v) before the cluster staged update run was resumed (%v)",
				startedCond.LastTransitionTime, resumedCond.LastTransitionTime)
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newApprovalRequestObj(clusterScopedNS, triggerApprovalRequestName),
					newPlacementResourceSnapshotObj(clusterScopedNS, snapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("suspending and resuming finished cluster staged update runs", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		missingStrategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, utils.RandStr())
		var snapshotName string

		// The first cluster staged update run completes successfully; the second one fails to initialize (its staged update
		// strategy does not exist). Both are created at generation 1, suspended at generation 2, and resumed at
		// generation 3.
		completedRunName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-0")
		failedRunName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-1")

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		// wantCompletedStatus returns the status of the completed cluster staged update run, with its Completed condition
		// observed at the given generation. The controller refreshes the observed generation of the Completed
		// condition only.
		wantCompletedStatus := func(completedCondObsGen int64) *rolloutv1alpha1.StagedUpdateRunStatus {
			return &rolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					observedAt(1, stagedUpdateRunInitializedCond()),
					observedAt(1, stagedUpdateRunStartedCond()),
					observedAt(completedCondObsGen, stagedUpdateRunCompletedCond(rolloutv1alpha1.StagedUpdateRunCompletedCondReasonSucceeded)),
				},
				ResourceSnapshotNameToRollout: snapshotName,
				Stages: []rolloutv1alpha1.PerStageStatus{
					{
						StageName: restStageName,
						Clusters: []rolloutv1alpha1.PerClusterStatus{
							wantPerClusterStatus(memberClusterNames, bindingNames, 0,
								observedAt(1, perClusterStartedCond()),
								observedAt(1, perClusterCompletedCond(rolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded))),
						},
						Conditions: []metav1.Condition{
							observedAt(1, perStageStartedCond()),
							observedAt(1, perStageCompletedCond(rolloutv1alpha1.StagedUpdateRunPerStageCompletedCondReasonSucceeded)),
						},
						MaxConcurrency: ptr.To(int32(1)),
					},
				},
			}
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, completedRunName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should start rolling out to the member cluster", func() {
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to the member cluster")
		})

		It("can mark the member cluster as available", func() {
			markPlacementBindingAsAvailable(clusterScopedNS, bindingNames[0], snapshotName)
		})

		It("should complete the first cluster staged update run", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(clusterScopedNS, completedRunName, wantCompletedStatus(1))
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to complete the first cluster staged update run")
		})

		It("can create the second cluster staged update run", func() {
			createStagedUpdateRun(clusterScopedNS, failedRunName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: missingStrategyName,
			})
		})

		It("should fail to initialize the second cluster staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, failedRunName, strategyNotFoundMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("can suspend both cluster staged update runs", func() {
			updateStagedUpdateRunSuspended(clusterScopedNS, completedRunName, true)
			updateStagedUpdateRunSuspended(clusterScopedNS, failedRunName, true)
		})

		It("should refresh the observed generations of the terminal conditions, without marking the cluster staged update runs as suspended", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(clusterScopedNS, completedRunName, wantCompletedStatus(2))
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the Completed condition of the first cluster staged update run")

			// The check expects the Initialized condition (the only condition) to be observed at the current
			// generation of the run.
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, failedRunName, strategyNotFoundMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the Initialized condition of the second cluster staged update run")
		})

		It("can resume both cluster staged update runs", func() {
			updateStagedUpdateRunSuspended(clusterScopedNS, completedRunName, false)
			updateStagedUpdateRunSuspended(clusterScopedNS, failedRunName, false)
		})

		It("should refresh the observed generations of the terminal conditions again, without marking the cluster staged update runs as resumed", func() {
			rolloutStatusActual := stagedUpdateRunRolloutStatusAsIsActual(clusterScopedNS, completedRunName, wantCompletedStatus(3))
			Eventually(rolloutStatusActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the Completed condition of the first cluster staged update run")

			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, failedRunName, strategyNotFoundMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the Initialized condition of the second cluster staged update run")

			bindingUntouchedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Expect(bindingUntouchedActual()).To(Succeed(), "The placement binding has been changed unexpectedly")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, completedRunName),
					newStagedUpdateRunObj(clusterScopedNS, failedRunName),
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
