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
	"time"

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
	placementPolicyTimedWaitNameTemplate         = "pp-timed-wait-%d"
	placementPolicyTimedWaitStrategyNameTemplate = "pp-timed-wait-strategy-%d"
	placementPolicyTimedWaitRunNameTemplate      = "pp-timed-wait-run-%d"

	clusterPlacementPolicyBeforeStageApprovalNameTemplate         = "cpp-before-stage-approval-%d"
	clusterPlacementPolicyBeforeStageApprovalStrategyNameTemplate = "cpp-before-stage-approval-strategy-%d"
	clusterPlacementPolicyBeforeStageApprovalRunNameTemplate      = "cpp-before-stage-approval-run-%d"

	clusterPlacementPolicyPartialStrategyNameTemplate         = "cpp-partial-strategy-%d"
	clusterPlacementPolicyPartialStrategyStrategyNameTemplate = "cpp-partial-strategy-strategy-%d"
	clusterPlacementPolicyPartialStrategyRunNameTemplate      = "cpp-partial-strategy-run-%d"

	placementPolicyEastStageName = "east"

	// placementPolicyTimedWaitDuration is the wait time of the timed wait task in the specs; it is long enough to
	// observe the hold on the prod stage, and short enough to keep the lane fast.
	placementPolicyTimedWaitDuration = 30 * time.Second
)

// The FEP-0001 placement experience is alpha and runs behind the enable-placement-policy-apis hub and member
// agent flags, which test/e2e/setup.sh turns on.
var _ = Describe("placement policy staged update run stages", Label("placementpolicy"), func() {
	Context("placement policy rolled out with a timed wait after the canary stage", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyTimedWaitNameTemplate, GinkgoParallelProcess())}
		strategyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyTimedWaitStrategyNameTemplate, GinkgoParallelProcess())}
		runKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyTimedWaitRunNameTemplate, GinkgoParallelProcess())}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}
		prodClusters := []string{memberCluster1EastProdName, memberCluster3WestProdName}

		var initialSnapshotName, newSnapshotName string
		var bindingNamesByCluster map[string]string
		// waitStartedAt is when the timed wait started, as the run records it (to the second).
		var waitStartedAt time.Time

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a policy that places the ConfigMap on every member cluster")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)

			By("creating a strategy with a timed wait between the canary and prod stages")
			strategy := newStagedUpdateStrategyObject(strategyKey, kfrolloutv1alpha1.StagedUpdateStrategySpec{
				Stages: []kfrolloutv1alpha1.Stage{
					{
						Name:          placementPolicyCanaryStageName,
						LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{envLabelName: envCanary}},
						AfterStageTasks: []kfrolloutv1alpha1.StageTask{
							{Type: kfrolloutv1alpha1.StageTaskTypeTimedWait, WaitTime: &metav1.Duration{Duration: placementPolicyTimedWaitDuration}},
						},
					},
					{
						Name:          placementPolicyProdStageName,
						LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{envLabelName: envProd}},
					},
				},
			})
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
			bindingNamesByCluster = placementPolicyBindingNamesByCluster(policyKey)
		})

		It("can change the ConfigMap on the hub", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
		})

		It("can create a staged update run", func() {
			run := newStagedUpdateRunObject(runKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: strategyKey.Name,
			})
			Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create staged update run %s", runKey)
		})

		It("should roll the new version out to the canary cluster, then start the timed wait", func() {
			Eventually(func() error {
				run, err := retrieveStagedUpdateRun(runKey)
				if err != nil {
					return err
				}
				stages := run.GetStatus().Stages
				if len(stages) == 0 || len(stages[0].AfterStageTasks) == 0 {
					return fmt.Errorf("staged update run %s has no after-stage task status for the canary stage yet", runKey)
				}
				cond := meta.FindStatusCondition(stages[0].AfterStageTasks[0].Conditions, kfrolloutv1alpha1.StagedUpdateRunTaskCondTypeTimedWaitStarted)
				if cond == nil || cond.Status != metav1.ConditionTrue {
					return fmt.Errorf("staged update run %s has not started the timed wait yet (task status: %+v)", runKey, stages[0].AfterStageTasks[0])
				}
				waitStartedAt = cond.LastTransitionTime.Time
				return nil
			}, placementPolicyRolloutEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to start the timed wait")
			newSnapshotName = resourceSnapshotNameToRolloutOf(runKey)
			Expect(newSnapshotName).NotTo(Equal(initialSnapshotName), "Staged update run %s rolls out the initial resource snapshot", runKey)

			Eventually(onlyCanaryUpdatedActual(policyKey, cmKey, initialSnapshotName, newSnapshotName, v0Data, v1Data), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the new version out to the canary cluster only")
		})

		It("should not update the prod clusters before the wait time has elapsed", func() {
			// Hold until shortly before the wait ends, leaving a margin for the run to act on time.
			if holdFor := time.Until(waitStartedAt.Add(placementPolicyTimedWaitDuration - 5*time.Second)); holdFor > 0 {
				Consistently(onlyCanaryUpdatedActual(policyKey, cmKey, initialSnapshotName, newSnapshotName, v0Data, v1Data), holdFor, consistentlyInterval).
					Should(Succeed(), "The new version reached the prod clusters before the wait time elapsed")
			}
		})

		It("should complete the run once the wait time has elapsed", func() {
			wantStatus := kfrolloutv1alpha1.StagedUpdateRunStatus{
				Conditions:                    succeededStagedUpdateRunConditions(),
				ResourceSnapshotNameToRollout: newSnapshotName,
				Stages: []kfrolloutv1alpha1.PerStageStatus{
					{
						StageName: placementPolicyCanaryStageName,
						Clusters: []kfrolloutv1alpha1.PerClusterStatus{
							succeededStagedUpdateRunClusterStatus(memberCluster2EastCanaryName, bindingNamesByCluster[memberCluster2EastCanaryName]),
						},
						AfterStageTasks: []kfrolloutv1alpha1.PerStageTaskStatus{
							{
								Type:     kfrolloutv1alpha1.StageTaskTypeTimedWait,
								WaitTime: &metav1.Duration{Duration: placementPolicyTimedWaitDuration},
								Conditions: []metav1.Condition{
									placementPolicyCondition(kfrolloutv1alpha1.StagedUpdateRunTaskCondTypeTimedWaitStarted, metav1.ConditionTrue,
										kfrolloutv1alpha1.StagedUpdateRunTaskTimedWaitStartedCondReasonTimerStarted),
									placementPolicyCondition(kfrolloutv1alpha1.StagedUpdateRunTaskCondTypeWaitTimeElapsed, metav1.ConditionTrue,
										kfrolloutv1alpha1.StagedUpdateRunTaskWaitTimeElapsedCondReasonTimerElapsed),
								},
							},
						},
						Conditions:         succeededStagedUpdateRunStageConditions(),
						StartedTimestamp:   &metav1.Time{},
						CompletedTimestamp: &metav1.Time{},
						MaxConcurrency:     ptr.To(int32(1)),
					},
					{
						StageName: placementPolicyProdStageName,
						Clusters: []kfrolloutv1alpha1.PerClusterStatus{
							succeededStagedUpdateRunClusterStatus(prodClusters[0], bindingNamesByCluster[prodClusters[0]]),
							succeededStagedUpdateRunClusterStatus(prodClusters[1], bindingNamesByCluster[prodClusters[1]]),
						},
						Conditions:         succeededStagedUpdateRunStageConditions(),
						StartedTimestamp:   &metav1.Time{},
						CompletedTimestamp: &metav1.Time{},
						MaxConcurrency:     ptr.To(int32(1)),
					},
				},
			}
			Eventually(stagedUpdateRunStatusActual(runKey, wantStatus), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to complete staged update run %s", runKey)

			// The run measures the wait from the start time it recorded, which is kept to the second, so it may move
			// on up to a second before the full wait time has passed.
			run, err := retrieveStagedUpdateRun(runKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get staged update run %s", runKey)
			prodStartedAt := run.GetStatus().Stages[1].StartedTimestamp.Time
			Expect(prodStartedAt.Sub(waitStartedAt)).To(BeNumerically(">=", placementPolicyTimedWaitDuration-time.Second),
				"The prod stage started at %v, before the timed wait that started at %v elapsed", prodStartedAt, waitStartedAt)
		})

		It("should place the new version on every member cluster", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the new resource snapshot out to every binding")
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the new version on every member cluster")
		})
	})

	Context("cluster placement policy rolled out with an approval before the prod stage", Ordered, func() {
		policyKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyBeforeStageApprovalNameTemplate, GinkgoParallelProcess())}
		strategyKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyBeforeStageApprovalStrategyNameTemplate, GinkgoParallelProcess())}
		runKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyBeforeStageApprovalRunNameTemplate, GinkgoParallelProcess())}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}

		var initialSnapshotName, newSnapshotName string
		var approvalRequestKey types.NamespacedName

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that places the ConfigMap on every member cluster")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)

			By("creating a strategy whose prod stage waits for an approval before it starts")
			strategy := newStagedUpdateStrategyObject(strategyKey, kfrolloutv1alpha1.StagedUpdateStrategySpec{
				Stages: []kfrolloutv1alpha1.Stage{
					{
						Name:          placementPolicyCanaryStageName,
						LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{envLabelName: envCanary}},
					},
					{
						Name:             placementPolicyProdStageName,
						LabelSelector:    &metav1.LabelSelector{MatchLabels: map[string]string{envLabelName: envProd}},
						BeforeStageTasks: []kfrolloutv1alpha1.StageTask{{Type: kfrolloutv1alpha1.StageTaskTypeApproval}},
					},
				},
			})
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
		})

		It("can change the ConfigMap on the hub", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
		})

		It("can create a cluster staged update run", func() {
			run := newStagedUpdateRunObject(runKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: strategyKey.Name,
			})
			Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create cluster staged update run %s", runKey)
		})

		It("should roll the new version out to the canary cluster, then ask for an approval before the prod stage", func() {
			approvalRequestKey = waitForStageApprovalRequest(runKey, 1, true)
			Eventually(approvalRequestActual(approvalRequestKey, runKey, placementPolicyProdStageName, false), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to find the approval request for the prod stage")
			newSnapshotName = resourceSnapshotNameToRolloutOf(runKey)
			Expect(newSnapshotName).NotTo(Equal(initialSnapshotName), "Cluster staged update run %s rolls out the initial resource snapshot", runKey)

			Eventually(onlyCanaryUpdatedActual(policyKey, cmKey, initialSnapshotName, newSnapshotName, v0Data, v1Data), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the new version out to the canary cluster only")
		})

		It("should not update the prod clusters before the approval", func() {
			Consistently(onlyCanaryUpdatedActual(policyKey, cmKey, initialSnapshotName, newSnapshotName, v0Data, v1Data), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The new version reached the prod clusters before the approval")
		})

		It("can approve the prod stage", func() {
			approvePlacementPolicyApprovalRequest(approvalRequestKey)
		})

		It("should complete the run and place the new version on every member cluster", func() {
			Eventually(stagedUpdateRunSucceededActual(runKey), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to complete cluster staged update run %s", runKey)
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the new resource snapshot out to every binding")
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the new version on every member cluster")
		})
	})

	Context("cluster placement policy rolled out by a strategy whose stages leave out a cluster", Ordered, func() {
		policyKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyPartialStrategyNameTemplate, GinkgoParallelProcess())}
		strategyKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyPartialStrategyStrategyNameTemplate, GinkgoParallelProcess())}
		runKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyPartialStrategyRunNameTemplate, GinkgoParallelProcess())}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}

		var initialSnapshotName, newSnapshotName string

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that places the ConfigMap on every member cluster")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)

			By("creating a strategy whose stages cover the east region only")
			// A cluster belongs to the first stage that selects it, so the east stage gets cluster-1 alone; cluster-3,
			// in the west, is in no stage at all.
			strategy := newStagedUpdateStrategyObject(strategyKey, kfrolloutv1alpha1.StagedUpdateStrategySpec{
				Stages: []kfrolloutv1alpha1.Stage{
					{
						Name:          placementPolicyCanaryStageName,
						LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{envLabelName: envCanary}},
					},
					{
						Name:          placementPolicyEastStageName,
						LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{regionLabelName: regionEast}},
					},
				},
			})
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
		})

		It("can change the ConfigMap on the hub", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
		})

		It("can roll the change out with a cluster staged update run", func() {
			run := newStagedUpdateRunObject(runKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: strategyKey.Name,
			})
			Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create cluster staged update run %s", runKey)
		})

		It("should complete the run, updating only the clusters in its stages", func() {
			Eventually(stagedUpdateRunSucceededActual(runKey), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to complete cluster staged update run %s", runKey)
			Expect(stagedUpdateRunClusterResultsActual(runKey, map[string]string{
				memberCluster2EastCanaryName: kfrolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded,
				memberCluster1EastProdName:   kfrolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded,
			})()).To(Succeed(), "Cluster staged update run %s did not update exactly the clusters in its stages", runKey)
			newSnapshotName = resourceSnapshotNameToRolloutOf(runKey)
			Expect(newSnapshotName).NotTo(Equal(initialSnapshotName), "Cluster staged update run %s rolled out the initial resource snapshot", runKey)
		})

		It("should leave the cluster outside every stage on the initial version", func() {
			Eventually(placementPolicyBindingSnapshotsActual(policyKey, map[string]string{
				memberCluster2EastCanaryName: newSnapshotName,
				memberCluster1EastProdName:   newSnapshotName,
				memberCluster3WestProdName:   initialSnapshotName,
			}), workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to leave cluster-3 on the initial resource snapshot")
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, memberCluster1EastProd, memberCluster2EastCanary), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the new version on the east clusters")
			Expect(configMapDataOnClustersActual(cmKey, v0Data, memberCluster3WestProd)()).To(Succeed(), "The new version reached cluster-3")
		})

		It("should report that cluster as on an older resource revision, with the binding manager role released", func() {
			wantStatus := someClustersOnOlderResourceRevisionPlacementPolicyStatus(int32(len(allMemberClusters)), 2)
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})
	})
})
