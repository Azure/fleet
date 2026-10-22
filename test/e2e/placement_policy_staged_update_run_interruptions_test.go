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

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kfrolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
)

const (
	placementPolicySuspendedNameTemplate                = "pp-suspended-%d"
	clusterPlacementPolicySuspendedNameTemplate         = "cpp-suspended-%d"
	placementPolicySuspendedStrategyNameTemplate        = "pp-suspended-strategy-%d"
	clusterPlacementPolicySuspendedStrategyNameTemplate = "cpp-suspended-strategy-%d"
	placementPolicySuspendedRunNameTemplate             = "pp-suspended-run-%d"
	clusterPlacementPolicySuspendedRunNameTemplate      = "cpp-suspended-run-%d"

	placementPolicyRunDeletedNameTemplate         = "pp-run-deleted-%d"
	placementPolicyRunDeletedStrategyNameTemplate = "pp-run-deleted-strategy-%d"
	placementPolicyDeletedRunNameTemplate         = "pp-deleted-run-%d"
	placementPolicyFollowUpRunNameTemplate        = "pp-follow-up-run-%d"
)

// canaryApprovalStrategySpec returns a strategy with a canary stage, gated by an approval, before a prod stage.
func canaryApprovalStrategySpec() kfrolloutv1alpha1.StagedUpdateStrategySpec {
	return kfrolloutv1alpha1.StagedUpdateStrategySpec{
		Stages: []kfrolloutv1alpha1.Stage{
			{
				Name:            placementPolicyCanaryStageName,
				LabelSelector:   &metav1.LabelSelector{MatchLabels: map[string]string{envLabelName: envCanary}},
				AfterStageTasks: []kfrolloutv1alpha1.StageTask{{Type: kfrolloutv1alpha1.StageTaskTypeApproval}},
			},
			{
				Name:          placementPolicyProdStageName,
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{envLabelName: envProd}},
			},
		},
	}
}

// The FEP-0001 placement experience is alpha and runs behind the enable-placement-policy-apis hub and member
// agent flags, which test/e2e/setup.sh turns on.
var _ = Describe("placement policy staged update run interruptions", Label("placementpolicy"), func() {
	// A suspended run gives up the binding manager role, so the two scopes run the same journey through the
	// namespaced and the cluster-scoped APIs.
	for _, clusterScoped := range []bool{false, true} {
		scopeDescription := "placement policy"
		policyNameTemplate, strategyNameTemplate, runNameTemplate := placementPolicySuspendedNameTemplate, placementPolicySuspendedStrategyNameTemplate, placementPolicySuspendedRunNameTemplate
		if clusterScoped {
			scopeDescription = "cluster placement policy"
			policyNameTemplate, strategyNameTemplate, runNameTemplate = clusterPlacementPolicySuspendedNameTemplate, clusterPlacementPolicySuspendedStrategyNameTemplate, clusterPlacementPolicySuspendedRunNameTemplate
		}

		Context(fmt.Sprintf("%s rolled out by a staged update run that is suspended and resumed", scopeDescription), Ordered, func() {
			objNamespace := ""
			if !clusterScoped {
				objNamespace = appNamespace().Name
			}
			policyKey := types.NamespacedName{Namespace: objNamespace, Name: fmt.Sprintf(policyNameTemplate, GinkgoParallelProcess())}
			strategyKey := types.NamespacedName{Namespace: objNamespace, Name: fmt.Sprintf(strategyNameTemplate, GinkgoParallelProcess())}
			runKey := types.NamespacedName{Namespace: objNamespace, Name: fmt.Sprintf(runNameTemplate, GinkgoParallelProcess())}
			cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
			v0Data := appConfigMap().Data
			v1Data := map[string]string{"data": "test-v1"}

			var initialSnapshotName, newSnapshotName string
			var approvalRequestKey types.NamespacedName

			setSuspended := func(suspended bool) {
				updateStagedUpdateRun(runKey, func(run kfrolloutv1alpha1.StagedUpdateRunAccessor) {
					run.GetSpec().Suspended = suspended
				})
			}

			BeforeAll(func() {
				By("creating the work resources")
				createWorkResources()

				By("creating a policy that places the ConfigMap on every member cluster")
				policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: placementPolicyResourceSelectors(),
				})
				Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create %s %s", scopeDescription, policyKey)

				By("creating a strategy with a canary stage, gated by an approval, before a prod stage")
				strategy := newStagedUpdateStrategyObject(strategyKey, canaryApprovalStrategySpec())
				Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create staged update strategy %s", strategyKey)
			})

			AfterAll(func() {
				// The work resources must go even if the cleanup below fails; otherwise they leak into later specs.
				defer cleanupWorkResourcesOnHubAndMembers()
				ensureStagedUpdateRunRemoved(runKey)
				ensureStagedUpdateStrategyRemoved(strategyKey)
				ensurePlacementPolicyRemoved(policyKey)
			})

			It("should place the initial version on every member cluster", func() {
				initialSnapshotName = waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
				Eventually(configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to place the initial version on every member cluster")
			})

			It("can change the ConfigMap on the hub", func() {
				updateConfigMapDataOnHub(cmKey, v1Data)
			})

			It("can create a staged update run that is suspended", func() {
				run := newStagedUpdateRunObject(runKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
					PlacementPolicyName:      policyKey.Name,
					StagedUpdateStrategyName: strategyKey.Name,
					Suspended:                true,
				})
				Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create staged update run %s", runKey)
			})

			It("should initialize the run with a new resource snapshot, then give up the binding manager role", func() {
				Eventually(stagedUpdateRunConditionActual(runKey, kfrolloutv1alpha1.StagedUpdateRunCondTypeInitialized, metav1.ConditionTrue,
					kfrolloutv1alpha1.StagedUpdateRunInitializedCondReasonPreppedResourceSnapshotAndAllStages), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to initialize staged update run %s", runKey)
				Eventually(stagedUpdateRunConditionActual(runKey, kfrolloutv1alpha1.StagedUpdateRunCondTypeSuspended, metav1.ConditionTrue,
					kfrolloutv1alpha1.StagedUpdateRunSuspendedCondReasonSuspended), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to mark staged update run %s as suspended", runKey)
				newSnapshotName = resourceSnapshotNameToRolloutOf(runKey)
				Expect(newSnapshotName).NotTo(Equal(initialSnapshotName), "Staged update run %s rolls out the initial resource snapshot", runKey)
				Eventually(placementPolicyBindingManagerActual(policyKey, nil), eventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to give up the binding manager role")
			})

			It("should not roll anything out while the run is suspended", func() {
				snapshotsActual := placementPolicyBindingSnapshotsActual(policyKey, map[string]string{
					memberCluster1EastProdName:   initialSnapshotName,
					memberCluster2EastCanaryName: initialSnapshotName,
					memberCluster3WestProdName:   initialSnapshotName,
				})
				dataActual := configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...)
				Consistently(func() error {
					if err := snapshotsActual(); err != nil {
						return err
					}
					if err := dataActual(); err != nil {
						return err
					}
					run, err := retrieveStagedUpdateRun(runKey)
					if err != nil {
						return err
					}
					if cond := meta.FindStatusCondition(run.GetStatus().Conditions, kfrolloutv1alpha1.StagedUpdateRunCondTypeStarted); cond != nil {
						return fmt.Errorf("suspended staged update run %s has started (condition: %+v)", runKey, cond)
					}
					return nil
				}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The suspended staged update run rolled something out")
			})

			It("can resume the run", func() {
				setSuspended(false)
			})

			It("should mark the run as resumed, roll the new version out to the canary cluster, and wait for an approval", func() {
				Eventually(stagedUpdateRunConditionActual(runKey, kfrolloutv1alpha1.StagedUpdateRunCondTypeSuspended, metav1.ConditionFalse,
					kfrolloutv1alpha1.StagedUpdateRunSuspendedCondReasonResumed), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to mark staged update run %s as resumed", runKey)
				approvalRequestKey = waitForStageApprovalRequest(runKey, 0, false)
				Eventually(onlyCanaryUpdatedActual(policyKey, cmKey, initialSnapshotName, newSnapshotName, v0Data, v1Data), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to roll the new version out to the canary cluster only")
				Eventually(placementPolicyBindingManagerActual(policyKey, &kfplacementv1alpha1.BindingManager{
					ControllerName: stagedUpdateRunBindingManagerName,
					ObjectRefs:     []kfplacementv1alpha1.ObjectReference{stagedUpdateRunObjectReference(runKey)},
				}), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to hold the binding manager role again")
			})

			It("can suspend the run while it waits for the approval", func() {
				setSuspended(true)
				Eventually(stagedUpdateRunConditionActual(runKey, kfrolloutv1alpha1.StagedUpdateRunCondTypeSuspended, metav1.ConditionTrue,
					kfrolloutv1alpha1.StagedUpdateRunSuspendedCondReasonSuspended), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to mark staged update run %s as suspended again", runKey)
				Eventually(placementPolicyBindingManagerActual(policyKey, nil), eventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to give up the binding manager role again")
			})

			It("can approve the canary stage", func() {
				approvePlacementPolicyApprovalRequest(approvalRequestKey)
			})

			It("should not accept the approval or move on to the prod stage while the run is suspended", func() {
				stateActual := onlyCanaryUpdatedActual(policyKey, cmKey, initialSnapshotName, newSnapshotName, v0Data, v1Data)
				Consistently(func() error {
					if err := stateActual(); err != nil {
						return err
					}
					run, err := retrieveStagedUpdateRun(runKey)
					if err != nil {
						return err
					}
					task := run.GetStatus().Stages[0].AfterStageTasks[0]
					if cond := meta.FindStatusCondition(task.Conditions, kfrolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestApproved); cond != nil {
						return fmt.Errorf("suspended staged update run %s has taken the approval (condition: %+v)", runKey, cond)
					}
					request, err := retrieveApprovalRequest(approvalRequestKey)
					if err != nil {
						return err
					}
					if cond := meta.FindStatusCondition(request.GetStatus().Conditions, kfrolloutv1alpha1.ApprovalRequestCondTypeApprovalAccepted); cond != nil {
						return fmt.Errorf("approval request %s was accepted while the run is suspended (condition: %+v)", approvalRequestKey, cond)
					}
					return nil
				}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The suspended staged update run acted on the approval")
			})

			It("can resume the run again", func() {
				setSuspended(false)
			})

			It("should accept the approval and complete the run", func() {
				Eventually(approvalRequestActual(approvalRequestKey, runKey, placementPolicyCanaryStageName, true), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to accept the approval")
				Eventually(stagedUpdateRunSucceededActual(runKey), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to complete staged update run %s", runKey)
				Expect(stagedUpdateRunConditionActual(runKey, kfrolloutv1alpha1.StagedUpdateRunCondTypeSuspended, metav1.ConditionFalse,
					kfrolloutv1alpha1.StagedUpdateRunSuspendedCondReasonResumed)()).To(Succeed(), "Staged update run %s is not marked as resumed", runKey)
			})

			It("should place the new version on every member cluster, with the binding manager role released", func() {
				Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to roll the new resource snapshot out to every binding")
				Eventually(configMapDataOnClustersActual(cmKey, v1Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to place the new version on every member cluster")
				wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))
				wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
				Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to report the expected %s status", scopeDescription)
			})
		})
	}

	Context("placement policy whose staged update run is deleted while it waits for an approval", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyRunDeletedNameTemplate, GinkgoParallelProcess())}
		strategyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyRunDeletedStrategyNameTemplate, GinkgoParallelProcess())}
		deletedRunKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyDeletedRunNameTemplate, GinkgoParallelProcess())}
		followUpRunKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyFollowUpRunNameTemplate, GinkgoParallelProcess())}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}

		var initialSnapshotName, newSnapshotName string
		var deletedRunApprovalRequestKey types.NamespacedName

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a policy that places the ConfigMap on every member cluster")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)

			By("creating a strategy with a canary stage, gated by an approval, before a prod stage")
			strategy := newStagedUpdateStrategyObject(strategyKey, canaryApprovalStrategySpec())
			Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create staged update strategy %s", strategyKey)
		})

		AfterAll(func() {
			// The work resources must go even if the cleanup below fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensureStagedUpdateRunRemoved(followUpRunKey)
			ensureStagedUpdateRunRemoved(deletedRunKey)
			ensureStagedUpdateStrategyRemoved(strategyKey)
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the initial version on every member cluster", func() {
			initialSnapshotName = waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			Eventually(configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the initial version on every member cluster")
		})

		It("can change the ConfigMap on the hub", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
		})

		It("can create a staged update run", func() {
			run := newStagedUpdateRunObject(deletedRunKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: strategyKey.Name,
			})
			Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create staged update run %s", deletedRunKey)
		})

		It("should roll the new version out to the canary cluster, then wait for an approval", func() {
			deletedRunApprovalRequestKey = waitForStageApprovalRequest(deletedRunKey, 0, false)
			newSnapshotName = resourceSnapshotNameToRolloutOf(deletedRunKey)
			Expect(newSnapshotName).NotTo(Equal(initialSnapshotName), "Staged update run %s rolls out the initial resource snapshot", deletedRunKey)
			Eventually(onlyCanaryUpdatedActual(policyKey, cmKey, initialSnapshotName, newSnapshotName, v0Data, v1Data), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the new version out to the canary cluster only")
		})

		It("can delete the run", func() {
			ensureStagedUpdateRunRemoved(deletedRunKey)
		})

		It("should remove the approval request and release the binding manager role", func() {
			Eventually(approvalRequestAbsentActual(deletedRunApprovalRequestKey), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the approval request of the deleted run")
			Eventually(placementPolicyBindingManagerActual(policyKey, nil), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to release the binding manager role")
		})

		It("should keep the clusters where the deleted run left them", func() {
			wantStatus := someClustersOnOlderResourceRevisionPlacementPolicyStatus(int32(len(allMemberClusters)), 1)
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the canary cluster alone as on the latest resource revision")
			Consistently(onlyCanaryUpdatedActual(policyKey, cmKey, initialSnapshotName, newSnapshotName, v0Data, v1Data), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The clusters moved after the run was deleted")
		})

		It("can finish the rollout with a new staged update run", func() {
			run := newStagedUpdateRunObject(followUpRunKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: strategyKey.Name,
			})
			Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create staged update run %s", followUpRunKey)

			// The hub has not changed since, so the new run rolls out the same resource snapshot, and the canary
			// stage, already up to date, goes straight to its approval.
			approvePlacementPolicyApprovalRequest(waitForStageApprovalRequest(followUpRunKey, 0, false))
			Eventually(stagedUpdateRunSucceededActual(followUpRunKey), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to complete staged update run %s", followUpRunKey)
			Expect(resourceSnapshotNameToRolloutOf(followUpRunKey)).To(Equal(newSnapshotName), "Staged update run %s rolls out another resource snapshot", followUpRunKey)
		})

		It("should place the new version on every member cluster, with the binding manager role released", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the new resource snapshot out to every binding")
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the new version on every member cluster")
			wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})
})
