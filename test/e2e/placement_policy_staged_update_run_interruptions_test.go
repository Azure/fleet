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
	"sigs.k8s.io/controller-runtime/pkg/client"

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

	placementPolicyHandMadeRequestNameTemplate         = "pp-hand-made-request-%d"
	placementPolicyHandMadeRequestStrategyNameTemplate = "pp-hand-made-request-strategy-%d"
	placementPolicyHandMadeRequestRunNameTemplate      = "pp-hand-made-request-run-%d"

	clusterPlacementPolicySuspendedMidRunNameTemplate         = "cpp-suspended-mid-run-%d"
	clusterPlacementPolicySuspendedMidRunStrategyNameTemplate = "cpp-suspended-mid-run-strategy-%d"
	clusterPlacementPolicySuspendedMidRunRunNameTemplate      = "cpp-suspended-mid-run-run-%d"
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

	Context("placement policy whose staged update run finds a hand-made approval request under the name it uses", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyHandMadeRequestNameTemplate, GinkgoParallelProcess())}
		strategyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyHandMadeRequestStrategyNameTemplate, GinkgoParallelProcess())}
		runKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyHandMadeRequestRunNameTemplate, GinkgoParallelProcess())}
		// The name that the run gives the approval request of the canary stage's after-stage task; see
		// formatAfterStageApprovalRequestName in the staged update run controller (short names are not hashed).
		requestKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf("%s-after-stage-%s", runKey.Name, placementPolicyCanaryStageName)}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}

		var initialSnapshotName, newSnapshotName string

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
			ensureStagedUpdateRunRemoved(runKey)
			// The hand-made request outlives the run, should the spec fail before deleting it.
			Expect(client.IgnoreNotFound(hubClient.Delete(ctx, newApprovalRequestObject(requestKey)))).To(Succeed(), "Failed to delete approval request %s", requestKey)
			ensureStagedUpdateStrategyRemoved(strategyKey)
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the initial version on every member cluster", func() {
			initialSnapshotName = waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			Eventually(configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the initial version on every member cluster")
		})

		It("can create, and approve, an approval request under the name that the run will use for the canary stage", func() {
			request := newApprovalRequestObject(requestKey)
			request.SetSpec(kfrolloutv1alpha1.ApprovalRequestSpec{
				StagedUpdateRunName: runKey.Name,
				StageName:           placementPolicyCanaryStageName,
			})
			Expect(hubClient.Create(ctx, request)).To(Succeed(), "Failed to create approval request %s", requestKey)
			approvePlacementPolicyApprovalRequest(requestKey)
		})

		It("can change the ConfigMap on the hub, and roll the change out with a staged update run", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
			run := newStagedUpdateRunObject(runKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: strategyKey.Name,
			})
			Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create staged update run %s", runKey)
		})

		It("should roll the new version out to the canary cluster, then report that it does not own the approval request", func() {
			Eventually(stagedUpdateRunApprovalTaskActual(runKey, 0, false, requestKey.Name,
				kfrolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestCreated, metav1.ConditionFalse,
				kfrolloutv1alpha1.StagedUpdateRunTaskApprovalRequestCreatedCondReasonOwnershipMismatch),
				placementPolicyRolloutEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the approval request as not owned by the run")
			newSnapshotName = resourceSnapshotNameToRolloutOf(runKey)
			Expect(newSnapshotName).NotTo(Equal(initialSnapshotName), "Staged update run %s rolls out the initial resource snapshot", runKey)
			Eventually(onlyCanaryUpdatedActual(policyKey, cmKey, initialSnapshotName, newSnapshotName, v0Data, v1Data), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the new version out to the canary cluster only")
		})

		It("should not roll out to the prod clusters, although the hand-made request is approved", func() {
			Consistently(onlyCanaryUpdatedActual(policyKey, cmKey, initialSnapshotName, newSnapshotName, v0Data, v1Data), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The new version reached the prod clusters through an approval request that the run does not own")
		})

		It("can delete the hand-made approval request", func() {
			Expect(hubClient.Delete(ctx, newApprovalRequestObject(requestKey))).To(Succeed(), "Failed to delete approval request %s", requestKey)
		})

		It("should create its own approval request, which needs an approval as usual", func() {
			Eventually(approvalRequestActual(requestKey, runKey, placementPolicyCanaryStageName, false), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to create an approval request controlled by the run")
			Eventually(stagedUpdateRunApprovalTaskActual(runKey, 0, false, requestKey.Name,
				kfrolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestCreated, metav1.ConditionTrue,
				kfrolloutv1alpha1.StagedUpdateRunTaskApprovalRequestCreatedCondReasonCreated),
				placementPolicyRolloutEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the approval request as created")
			Consistently(onlyCanaryUpdatedActual(policyKey, cmKey, initialSnapshotName, newSnapshotName, v0Data, v1Data), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The new version reached the prod clusters before the approval")
		})

		It("can approve the canary stage", func() {
			approvePlacementPolicyApprovalRequest(requestKey)
		})

		It("should roll the new version out to every member cluster", func() {
			Eventually(stagedUpdateRunSucceededActual(runKey), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to complete staged update run %s", runKey)
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the new version on every member cluster")
			wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("cluster placement policy whose binding is suspended by hand while a staged update run is in progress", Ordered, func() {
		policyKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicySuspendedMidRunNameTemplate, GinkgoParallelProcess())}
		strategyKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicySuspendedMidRunStrategyNameTemplate, GinkgoParallelProcess())}
		runKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicySuspendedMidRunRunNameTemplate, GinkgoParallelProcess())}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}
		// A prod cluster, which the run reaches only after the approval.
		suspendedClusterName := memberCluster1EastProdName
		updatedClusterNames := []string{memberCluster2EastCanaryName, memberCluster3WestProdName}
		// The member clusters are known only once the suite is set up, after the spec tree is built.
		clusterCount := func() int32 { return int32(len(allMemberClusterNames)) }

		var initialSnapshotName, newSnapshotName string
		var bindingUIDsByCluster map[string]types.UID
		var approvalRequestKey types.NamespacedName

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that places the ConfigMap on every member cluster")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)

			By("creating a strategy with a canary stage, gated by an approval, before a prod stage")
			strategy := newStagedUpdateStrategyObject(strategyKey, canaryApprovalStrategySpec())
			Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create cluster staged update strategy %s", strategyKey)
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
			bindingUIDsByCluster = placementPolicyBindingUIDsByCluster(policyKey)
		})

		It("can change the ConfigMap on the hub, and roll the change out with a cluster staged update run", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
			run := newStagedUpdateRunObject(runKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: strategyKey.Name,
			})
			Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create cluster staged update run %s", runKey)
		})

		It("should roll the new version out to the canary cluster, then wait for an approval", func() {
			approvalRequestKey = waitForStageApprovalRequest(runKey, 0, false)
			newSnapshotName = resourceSnapshotNameToRolloutOf(runKey)
			Expect(newSnapshotName).NotTo(Equal(initialSnapshotName), "Cluster staged update run %s rolls out the initial resource snapshot", runKey)
			Eventually(onlyCanaryUpdatedActual(policyKey, cmKey, initialSnapshotName, newSnapshotName, v0Data, v1Data), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the new version out to the canary cluster only")
		})

		It("can suspend the binding for member cluster 1", func() {
			setPlacementPolicyBindingSuspended(policyKey, suspendedClusterName, true)
		})

		It("should remove the ConfigMap from member cluster 1", func() {
			Eventually(suspendedPlacementPolicyBindingActual(policyKey, suspendedClusterName, bindingUIDsByCluster[suspendedClusterName], initialSnapshotName),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to process the suspension of the binding")
			Eventually(appConfigMapRemovedFromClustersActual(memberCluster1EastProd), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to remove the ConfigMap from member cluster 1")
		})

		It("can approve the canary stage", func() {
			approvePlacementPolicyApprovalRequest(approvalRequestKey)
		})

		It("should complete the run, counting the suspended cluster as updated", func() {
			Eventually(stagedUpdateRunSucceededActual(runKey), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to complete cluster staged update run %s", runKey)
			Expect(stagedUpdateRunClusterResultsActual(runKey, map[string]string{
				memberCluster2EastCanaryName: kfrolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded,
				memberCluster1EastProdName:   kfrolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded,
				memberCluster3WestProdName:   kfrolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded,
			})()).To(Succeed(), "Cluster staged update run %s did not update every cluster", runKey)

			// The run skips the wait on a suspended binding; only the message of the cluster's Completed condition tells
			// that path apart.
			run, err := retrieveStagedUpdateRun(runKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get cluster staged update run %s", runKey)
			var completedCond *metav1.Condition
			for _, stage := range run.GetStatus().Stages {
				for _, cluster := range stage.Clusters {
					if cluster.ClusterName == suspendedClusterName {
						completedCond = meta.FindStatusCondition(cluster.Conditions, kfrolloutv1alpha1.StagedUpdateRunPerClusterCondTypeCompleted)
					}
				}
			}
			Expect(completedCond).NotTo(BeNil(), "Cluster staged update run %s reports no Completed condition for member cluster 1", runKey)
			Expect(completedCond.Message).To(ContainSubstring("binding is suspended"), "The Completed condition of member cluster 1 does not report the binding as suspended")
		})

		It("should have moved the suspended binding to the new resource snapshot, without placing it on member cluster 1", func() {
			suspendedActual := suspendedPlacementPolicyBindingActual(policyKey, suspendedClusterName, bindingUIDsByCluster[suspendedClusterName], newSnapshotName)
			Eventually(suspendedActual, workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to move the suspended binding to the new resource snapshot")
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, memberCluster2EastCanary, memberCluster3WestProd), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the new version on the other member clusters")

			// The suspended binding names the latest revision but has processed none, so it counts as neither
			// synchronized nor on an older revision.
			wantStatus := someClustersNotSynchronizedPlacementPolicyStatus(clusterCount(), clusterCount()-1)
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			statusActual := placementPolicyStatusActual(policyKey, wantStatus)
			Eventually(statusActual, workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")

			worksActual := placementPolicyWorkClustersActual(policyKey, updatedClusterNames...)
			removedActual := appConfigMapRemovedFromClustersActual(memberCluster1EastProd)
			Consistently(func() error {
				for _, actual := range []func() error{suspendedActual, worksActual, removedActual, statusActual} {
					if err := actual(); err != nil {
						return err
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The new version reached member cluster 1, or the suspended binding changed")
		})

		It("can resume the binding for member cluster 1", func() {
			setPlacementPolicyBindingSuspended(policyKey, suspendedClusterName, false)
		})

		It("should place the new version on member cluster 1", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to synchronize every binding on the new resource snapshot")
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the new version on every member cluster")
			wantStatus := allSynchronizedPlacementPolicyStatus(clusterCount())
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})
	})
})
