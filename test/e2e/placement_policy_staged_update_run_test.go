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
	"maps"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kfrolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
)

const (
	placementPolicyStagedNameTemplate          = "pp-staged-%d"
	clusterPlacementPolicyStagedNameTemplate   = "cpp-staged-%d"
	placementPolicyStrategyNameTemplate        = "pp-strategy-%d"
	clusterPlacementPolicyStrategyNameTemplate = "cpp-strategy-%d"
	placementPolicyRunNameTemplate             = "pp-run-%d"
	clusterPlacementPolicyRunNameTemplate      = "cpp-run-%d"

	placementPolicyRollbackNameTemplate         = "pp-rollback-%d"
	placementPolicyRollbackStrategyNameTemplate = "pp-rollback-strategy-%d"
	placementPolicyRollForwardRunNameTemplate   = "pp-roll-forward-run-%d"
	placementPolicyRollBackRunNameTemplate      = "pp-roll-back-run-%d"

	clusterPlacementPolicyMutualExclusionNameTemplate         = "cpp-mutual-exclusion-%d"
	clusterPlacementPolicyMutualExclusionStrategyNameTemplate = "cpp-mutual-exclusion-strategy-%d"
	clusterPlacementPolicyMutualExclusionRunNameTemplate      = "cpp-mutual-exclusion-run-%d"

	placementPolicyGeneratedStrategyNameTemplate = "pp-generated-strategy-%d"
	placementPolicyGeneratedRunNameTemplate      = "pp-generated-run-%d"

	placementPolicyCanaryStageName      = "canary"
	placementPolicyProdStageName        = "prod"
	placementPolicyAllClustersStageName = "all"
)

// The FEP-0001 placement experience is alpha and runs behind the enable-placement-policy-apis hub and member
// agent flags, which test/e2e/setup.sh turns on.
var _ = Describe("placement policy staged update runs", Label("placementpolicy"), func() {
	// The two scopes run the same journey through different APIs: a PlacementPolicy with a StagedUpdateRun,
	// StagedUpdateStrategy, and ApprovalRequest in its namespace, and a ClusterPlacementPolicy with their
	// cluster-scoped counterparts.
	for _, clusterScoped := range []bool{false, true} {
		scopeDescription := "placement policy"
		policyNameTemplate, strategyNameTemplate, runNameTemplate := placementPolicyStagedNameTemplate, placementPolicyStrategyNameTemplate, placementPolicyRunNameTemplate
		if clusterScoped {
			scopeDescription = "cluster placement policy"
			policyNameTemplate, strategyNameTemplate, runNameTemplate = clusterPlacementPolicyStagedNameTemplate, clusterPlacementPolicyStrategyNameTemplate, clusterPlacementPolicyRunNameTemplate
		}

		Context(fmt.Sprintf("%s rolled out by a staged update run, with an approval between the canary and prod stages", scopeDescription), Ordered, func() {
			// Objects of the namespaced APIs live in the app namespace; the cluster-scoped ones have no namespace.
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
			prodClusters := []string{memberCluster1EastProdName, memberCluster3WestProdName}

			var initialSnapshotName, newSnapshotName string
			var approvalRequestKey types.NamespacedName
			// The binding of each cluster, as recorded in the run status.
			bindingNamesByCluster := map[string]string{}

			BeforeAll(func() {
				By("creating the work resources")
				createWorkResources()

				By("creating a policy that places the ConfigMap on every member cluster")
				policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: placementPolicyResourceSelectors(),
				})
				Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create %s %s", scopeDescription, policyKey)

				By("creating a strategy with a canary stage, gated by an approval, before a prod stage")
				strategy := newStagedUpdateStrategyObject(strategyKey, kfrolloutv1alpha1.StagedUpdateStrategySpec{
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
				})
				Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create staged update strategy %s", strategyKey)
			})

			AfterAll(func() {
				// The work resources must go even if the cleanup below fails; otherwise they leak into later specs.
				defer cleanupWorkResourcesOnHubAndMembers()
				// The run goes first, so that it gives up the binding manager role before the policy goes.
				ensureStagedUpdateRunRemoved(runKey)
				ensureStagedUpdateStrategyRemoved(strategyKey)
				ensurePlacementPolicyRemoved(policyKey)
			})

			It("should place the initial version on every member cluster", func() {
				wantSelectorsByCluster := make(map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly, len(allMemberClusterNames))
				for _, clusterName := range allMemberClusterNames {
					wantSelectorsByCluster[clusterName] = []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}}
				}
				Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster, 1), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to bind every member cluster")
				Eventually(configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to place the initial version on every member cluster")

				policy, err := retrievePlacementPolicy(policyKey)
				Expect(err).NotTo(HaveOccurred(), "Failed to get %s %s", scopeDescription, policyKey)
				snapshot, err := latestPrimarySnapshotOfPlacementPolicy(policy)
				Expect(err).NotTo(HaveOccurred(), "Failed to find the resource snapshot of %s %s", scopeDescription, policyKey)
				initialSnapshotName = snapshot.GetName()
				bindings, err := placementPolicyBindings(policy)
				Expect(err).NotTo(HaveOccurred(), "Failed to list the bindings of %s %s", scopeDescription, policyKey)
				for _, binding := range bindings {
					bindingNamesByCluster[binding.GetSpec().ClusterName] = binding.GetName()
				}
			})

			It("can change the ConfigMap on the hub", func() {
				updateConfigMapDataOnHub(cmKey, v1Data)
			})

			It("can create a staged update run that names no resource snapshot", func() {
				run := newStagedUpdateRunObject(runKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
					PlacementPolicyName:      policyKey.Name,
					StagedUpdateStrategyName: strategyKey.Name,
				})
				Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create staged update run %s", runKey)
			})

			It("should initialize the run with a new resource snapshot of the changed ConfigMap", func() {
				Eventually(func() error {
					run, err := retrieveStagedUpdateRun(runKey)
					if err != nil {
						return err
					}
					if !meta.IsStatusConditionTrue(run.GetStatus().Conditions, kfrolloutv1alpha1.StagedUpdateRunCondTypeInitialized) {
						return fmt.Errorf("staged update run %s is not initialized yet (conditions: %+v)", runKey, run.GetStatus().Conditions)
					}
					policy, err := retrievePlacementPolicy(policyKey)
					if err != nil {
						return err
					}
					snapshot, err := latestPrimarySnapshotOfPlacementPolicy(policy)
					if err != nil {
						return err
					}
					switch {
					case snapshot.GetName() == initialSnapshotName:
						return fmt.Errorf("no new resource snapshot has been taken yet")
					case run.GetStatus().ResourceSnapshotNameToRollout != snapshot.GetName():
						return fmt.Errorf("staged update run %s rolls out resource snapshot %q, want the latest one %q",
							runKey, run.GetStatus().ResourceSnapshotNameToRollout, snapshot.GetName())
					case ptr.Deref(policy.GetStatus().LatestResourceRevisionName, "") != snapshot.GetName():
						return fmt.Errorf("%s %s reports latest resource revision %v, want %q",
							scopeDescription, policyKey, policy.GetStatus().LatestResourceRevisionName, snapshot.GetName())
					}
					newSnapshotName = snapshot.GetName()
					return nil
				}, placementPolicyRolloutEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
			})

			// prodOnInitialRevisionActual checks that the canary cluster has the new revision while the prod clusters
			// still have the initial one.
			prodOnInitialRevisionActual := func() func() error {
				wantBindings := map[string]wantPlacementBinding{
					memberCluster2EastCanaryName: {
						selectors:    []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}},
						snapshotName: newSnapshotName,
						status:       synchronizedPlacementBindingStatus(1),
					},
				}
				for _, clusterName := range prodClusters {
					wantBindings[clusterName] = wantPlacementBinding{
						selectors:    []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}},
						snapshotName: initialSnapshotName,
						status:       synchronizedPlacementBindingStatus(1),
					}
				}
				bindingsActual := placementPolicyBindingsActual(policyKey, wantBindings)
				canaryActual := configMapDataOnClustersActual(cmKey, v1Data, memberCluster2EastCanary)
				prodActual := configMapDataOnClustersActual(cmKey, v0Data, memberCluster1EastProd, memberCluster3WestProd)
				return func() error {
					if err := bindingsActual(); err != nil {
						return err
					}
					if err := canaryActual(); err != nil {
						return err
					}
					return prodActual()
				}
			}

			It("should roll out the new version to the canary cluster only", func() {
				Eventually(prodOnInitialRevisionActual(), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to roll out the new version to the canary cluster only")
			})

			It("should report the prod clusters as on an older resource revision, with the run holding the binding manager role", func() {
				clusterCount := int32(len(allMemberClusters))
				wantStatus := kfplacementv1alpha1.PlacementPolicyStatus{
					Conditions: []metav1.Condition{
						placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected, metav1.ConditionTrue,
							kfplacementv1alpha1.PlacementPolicyResourceCollectedCondReasonAllResourcesCollected),
						placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeScheduled, metav1.ConditionTrue,
							kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters),
						placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized, metav1.ConditionFalse,
							kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonSomeClustersOnOlderResourceRevision),
						// Availability is judged only on clusters that have the latest revision synchronized.
						placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeAvailable, metav1.ConditionFalse,
							kfplacementv1alpha1.PlacementPolicyAvailableCondReasonSomeClustersUnavailable),
					},
					LatestResourceRevisionName: ptr.To(newSnapshotName),
					DesiredClusters:            ptr.To(clusterCount),
					ScheduledClusters:          ptr.To(clusterCount),
					SynchronizedClusters:       ptr.To(int32(1)),
					ResourcesAvailableClusters: ptr.To(int32(1)),
					ActiveClusterClaims:        ptr.To(int32(0)),
					BindingManager: &kfplacementv1alpha1.BindingManager{
						ControllerName: stagedUpdateRunBindingManagerName,
						ObjectRefs:     []kfplacementv1alpha1.ObjectReference{stagedUpdateRunObjectReference(runKey)},
					},
				}
				Eventually(placementPolicyStatusActual(policyKey, wantStatus), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to report the expected %s status", scopeDescription)
			})

			It("should create an approval request for the canary stage, controlled by the run", func() {
				approvalRequestKey = waitForStageApprovalRequest(runKey, 0, false)
				Eventually(approvalRequestActual(approvalRequestKey, runKey, placementPolicyCanaryStageName, false), eventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to find the approval request")
			})

			It("should not roll out to the prod clusters before the approval", func() {
				Consistently(prodOnInitialRevisionActual(), consistentlyDuration, consistentlyInterval).
					Should(Succeed(), "The new version reached the prod clusters before the approval")
			})

			It("can approve the canary stage", func() {
				approvePlacementPolicyApprovalRequest(approvalRequestKey)
			})

			It("should accept the approval", func() {
				Eventually(approvalRequestActual(approvalRequestKey, runKey, placementPolicyCanaryStageName, true), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to accept the approval")
			})

			It("should roll out the new version to every member cluster", func() {
				wantSelectorsByCluster := make(map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly, len(allMemberClusterNames))
				for _, clusterName := range allMemberClusterNames {
					wantSelectorsByCluster[clusterName] = []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}}
				}
				Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster, 1), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to roll out the new version to every member cluster")
				Eventually(configMapDataOnClustersActual(cmKey, v1Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to place the new version on every member cluster")
			})

			It("should complete the run successfully", func() {
				succeededCluster := func(clusterName string) kfrolloutv1alpha1.PerClusterStatus {
					return succeededStagedUpdateRunClusterStatus(clusterName, bindingNamesByCluster[clusterName])
				}
				// Timestamps are ignored in the comparison; non-nil wanted values check that they are set.
				wantStatus := kfrolloutv1alpha1.StagedUpdateRunStatus{
					Conditions:                    succeededStagedUpdateRunConditions(),
					ResourceSnapshotNameToRollout: newSnapshotName,
					Stages: []kfrolloutv1alpha1.PerStageStatus{
						{
							StageName: placementPolicyCanaryStageName,
							Clusters:  []kfrolloutv1alpha1.PerClusterStatus{succeededCluster(memberCluster2EastCanaryName)},
							AfterStageTasks: []kfrolloutv1alpha1.PerStageTaskStatus{
								{
									Type:                kfrolloutv1alpha1.StageTaskTypeApproval,
									ApprovalRequestName: approvalRequestKey.Name,
									Conditions: []metav1.Condition{
										placementPolicyCondition(kfrolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestCreated, metav1.ConditionTrue,
											kfrolloutv1alpha1.StagedUpdateRunTaskApprovalRequestCreatedCondReasonCreated),
										placementPolicyCondition(kfrolloutv1alpha1.StagedUpdateRunTaskCondTypeApprovalRequestApproved, metav1.ConditionTrue,
											kfrolloutv1alpha1.StagedUpdateRunTaskApprovalRequestApprovedCondReasonApproved),
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
							// Clusters in a stage with no sorting label key are ordered by name.
							Clusters: []kfrolloutv1alpha1.PerClusterStatus{
								succeededCluster(memberCluster1EastProdName),
								succeededCluster(memberCluster3WestProdName),
							},
							Conditions:         succeededStagedUpdateRunStageConditions(),
							StartedTimestamp:   &metav1.Time{},
							CompletedTimestamp: &metav1.Time{},
							MaxConcurrency:     ptr.To(int32(1)),
						},
					},
				}
				Eventually(stagedUpdateRunStatusActual(runKey, wantStatus), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to complete the staged update run")
			})

			It("should report the policy as synchronized on every member cluster, with the binding manager role released", func() {
				wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))
				wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
				Eventually(placementPolicyStatusActual(policyKey, wantStatus), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to report the expected %s status", scopeDescription)
			})
		})
	}

	Context("placement policy rolled back to its initial resource snapshot by a staged update run", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyRollbackNameTemplate, GinkgoParallelProcess())}
		strategyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyRollbackStrategyNameTemplate, GinkgoParallelProcess())}
		forwardRunKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyRollForwardRunNameTemplate, GinkgoParallelProcess())}
		backRunKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyRollBackRunNameTemplate, GinkgoParallelProcess())}
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

			By("creating a strategy with a single stage that covers every cluster")
			strategy := newStagedUpdateStrategyObject(strategyKey, kfrolloutv1alpha1.StagedUpdateStrategySpec{
				Stages: []kfrolloutv1alpha1.Stage{
					// An empty (not nil) label selector selects every cluster.
					{Name: placementPolicyAllClustersStageName, LabelSelector: &metav1.LabelSelector{}},
				},
			})
			Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create staged update strategy %s", strategyKey)
		})

		AfterAll(func() {
			// The work resources must go even if the cleanup below fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensureStagedUpdateRunRemoved(backRunKey)
			ensureStagedUpdateRunRemoved(forwardRunKey)
			ensureStagedUpdateStrategyRemoved(strategyKey)
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the initial version on every member cluster", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind every member cluster")
			Eventually(configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the initial version on every member cluster")

			policy, err := retrievePlacementPolicy(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get placement policy %s", policyKey)
			snapshot, err := latestPrimarySnapshotOfPlacementPolicy(policy)
			Expect(err).NotTo(HaveOccurred(), "Failed to find the resource snapshot of placement policy %s", policyKey)
			initialSnapshotName = snapshot.GetName()
		})

		It("can change the ConfigMap on the hub", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
		})

		It("can roll the change out with a staged update run", func() {
			run := newStagedUpdateRunObject(forwardRunKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: strategyKey.Name,
			})
			Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create staged update run %s", forwardRunKey)
			Eventually(stagedUpdateRunSucceededActual(forwardRunKey), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to complete staged update run %s", forwardRunKey)

			forwardRun, err := retrieveStagedUpdateRun(forwardRunKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get staged update run %s", forwardRunKey)
			newSnapshotName = forwardRun.GetStatus().ResourceSnapshotNameToRollout
			Expect(newSnapshotName).NotTo(Equal(initialSnapshotName), "Staged update run %s rolled out the initial resource snapshot", forwardRunKey)
		})

		It("should place the new version on every member cluster", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the new resource snapshot out to every binding")
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the new version on every member cluster")
		})

		It("can roll back with a staged update run that names the initial resource snapshot", func() {
			run := newStagedUpdateRunObject(backRunKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: strategyKey.Name,
				ResourceSnapshotName:     initialSnapshotName,
			})
			Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create staged update run %s", backRunKey)
			Eventually(stagedUpdateRunSucceededActual(backRunKey), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to complete staged update run %s", backRunKey)
		})

		It("should place the initial version on every member cluster again", func() {
			wantBindings := make(map[string]wantPlacementBinding, len(allMemberClusterNames))
			for _, clusterName := range allMemberClusterNames {
				wantBindings[clusterName] = wantPlacementBinding{
					selectors:    []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}},
					snapshotName: initialSnapshotName,
					status:       synchronizedPlacementBindingStatus(1),
				}
			}
			Eventually(placementPolicyBindingsActual(policyKey, wantBindings), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the initial resource snapshot back out to every binding")
			Eventually(configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the initial version on every member cluster again")
		})

		It("should report every cluster as on an older resource revision, the change being still the latest one", func() {
			// A rollback takes no new snapshot: the hub still matches the changed revision, which stays the latest, so
			// every cluster counts as outdated rather than as synchronized. Availability, too, is judged only on
			// clusters that have the latest revision synchronized.
			clusterCount := int32(len(allMemberClusters))
			wantStatus := kfplacementv1alpha1.PlacementPolicyStatus{
				Conditions: []metav1.Condition{
					placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected, metav1.ConditionTrue,
						kfplacementv1alpha1.PlacementPolicyResourceCollectedCondReasonAllResourcesCollected),
					placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeScheduled, metav1.ConditionTrue,
						kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters),
					placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized, metav1.ConditionFalse,
						kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonSomeClustersOnOlderResourceRevision),
					placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeAvailable, metav1.ConditionFalse,
						kfplacementv1alpha1.PlacementPolicyAvailableCondReasonSomeClustersUnavailable),
				},
				LatestResourceRevisionName: ptr.To(newSnapshotName),
				DesiredClusters:            ptr.To(clusterCount),
				ScheduledClusters:          ptr.To(clusterCount),
				SynchronizedClusters:       ptr.To(int32(0)),
				ResourcesAvailableClusters: ptr.To(int32(0)),
				ActiveClusterClaims:        ptr.To(int32(0)),
			}
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("cluster placement policy scaled up while a cluster staged update run holds the binding manager role", Ordered, func() {
		policyKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyMutualExclusionNameTemplate, GinkgoParallelProcess())}
		strategyKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyMutualExclusionStrategyNameTemplate, GinkgoParallelProcess())}
		runKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyMutualExclusionRunNameTemplate, GinkgoParallelProcess())}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}
		canaryLabels := map[string]string{envLabelName: envCanary}
		prodLabels := map[string]string{envLabelName: envProd}
		canarySelectors := []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{clusterSelectorTermsMatchingLabels(canaryLabels)}
		prodSelectors := []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{clusterSelectorTermsMatchingLabels(prodLabels)}

		// Which prod cluster ranks first depends on live capacity, so the specs learn it rather than assume it.
		var firstProdClusterName, secondProdClusterName, initialSnapshotName, newSnapshotName string
		var approvalRequestKey types.NamespacedName

		// heldBackActual checks the state while the run holds the binding manager role: the canary cluster has the new
		// revision, the first prod cluster still has the initial one, and the second prod cluster, which the raised
		// count now wants, is neither bound nor given the ConfigMap. The policy status already counts it as wanted.
		heldBackActual := func() func() error {
			bindingsActual := placementPolicyBindingsActual(policyKey, map[string]wantPlacementBinding{
				memberCluster2EastCanaryName: {selectors: canarySelectors, snapshotName: newSnapshotName, status: synchronizedPlacementBindingStatus(1)},
				firstProdClusterName:         {selectors: prodSelectors, snapshotName: initialSnapshotName, status: synchronizedPlacementBindingStatus(1)},
			})
			secondProdActual := configMapAbsentOnClustersActual(cmKey, memberClusterNamed(secondProdClusterName))
			statusActual := placementPolicyStatusActual(policyKey, kfplacementv1alpha1.PlacementPolicyStatus{
				Conditions: []metav1.Condition{
					placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeResourceCollected, metav1.ConditionTrue,
						kfplacementv1alpha1.PlacementPolicyResourceCollectedCondReasonAllResourcesCollected),
					placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeScheduled, metav1.ConditionTrue,
						kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters),
					placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeSynchronized, metav1.ConditionFalse,
						kfplacementv1alpha1.PlacementPolicySynchronizedCondReasonSomeClustersOnOlderResourceRevision),
					placementPolicyCondition(kfplacementv1alpha1.PlacementPolicyCondTypeAvailable, metav1.ConditionFalse,
						kfplacementv1alpha1.PlacementPolicyAvailableCondReasonSomeClustersUnavailable),
				},
				LatestResourceRevisionName: ptr.To(newSnapshotName),
				// The canary selector wants 1 cluster and the raised prod selector 2.
				DesiredClusters:   ptr.To(int32(3)),
				ScheduledClusters: ptr.To(int32(3)),
				// Only the canary cluster has the latest revision; the first prod cluster is on an older one, and the
				// second has no binding yet.
				SynchronizedClusters:       ptr.To(int32(1)),
				ResourcesAvailableClusters: ptr.To(int32(1)),
				ActiveClusterClaims:        ptr.To(int32(0)),
				BindingManager: &kfplacementv1alpha1.BindingManager{
					ControllerName: stagedUpdateRunBindingManagerName,
					ObjectRefs:     []kfplacementv1alpha1.ObjectReference{stagedUpdateRunObjectReference(runKey)},
				},
			})
			return func() error {
				if err := bindingsActual(); err != nil {
					return err
				}
				if err := secondProdActual(); err != nil {
					return err
				}
				return statusActual()
			}
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that selects the canary cluster and one prod cluster")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorMatchingLabels(canaryLabels, intstr.FromInt32(1)),
					clusterSelectorMatchingLabels(prodLabels, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)

			By("creating a strategy with a canary stage, gated by an approval, before a prod stage")
			strategy := newStagedUpdateStrategyObject(strategyKey, kfrolloutv1alpha1.StagedUpdateStrategySpec{
				Stages: []kfrolloutv1alpha1.Stage{
					{
						Name:            placementPolicyCanaryStageName,
						LabelSelector:   &metav1.LabelSelector{MatchLabels: canaryLabels},
						AfterStageTasks: []kfrolloutv1alpha1.StageTask{{Type: kfrolloutv1alpha1.StageTaskTypeApproval}},
					},
					{
						Name:          placementPolicyProdStageName,
						LabelSelector: &metav1.LabelSelector{MatchLabels: prodLabels},
					},
				},
			})
			Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create cluster staged update strategy %s", strategyKey)
		})

		AfterAll(func() {
			// The work resources must go even if the cleanup below fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			// The run goes first, so that it gives up the binding manager role before the policy goes.
			ensureStagedUpdateRunRemoved(runKey)
			ensureStagedUpdateStrategyRemoved(strategyKey)
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the initial version on the canary cluster and one prod cluster", func() {
			Eventually(func() error {
				bindings, err := placementPolicyBindingsByCluster(policyKey)
				if err != nil {
					return err
				}
				if _, found := bindings[memberCluster2EastCanaryName]; !found || len(bindings) != 2 {
					return fmt.Errorf("cluster placement policy %s binds clusters %v, want the canary cluster and one prod cluster",
						policyKey, slices.Sorted(maps.Keys(bindings)))
				}
				for clusterName := range bindings {
					if clusterName != memberCluster2EastCanaryName {
						firstProdClusterName = clusterName
					}
				}
				return nil
			}, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to bind the canary cluster and one prod cluster")
			secondProdClusterName = otherProdClusterName(firstProdClusterName)

			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				memberCluster2EastCanaryName: canarySelectors,
				firstProdClusterName:         prodSelectors,
			}, 1), workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to synchronize the bindings")
			Eventually(configMapDataOnClustersActual(cmKey, v0Data, memberCluster2EastCanary, memberClusterNamed(firstProdClusterName)), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the initial version on the bound clusters")

			policy, err := retrievePlacementPolicy(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get cluster placement policy %s", policyKey)
			snapshot, err := latestPrimarySnapshotOfPlacementPolicy(policy)
			Expect(err).NotTo(HaveOccurred(), "Failed to find the resource snapshot of cluster placement policy %s", policyKey)
			initialSnapshotName = snapshot.GetName()
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

		It("should roll the new version out to the canary cluster, then wait for an approval", func() {
			approvalRequestKey = waitForStageApprovalRequest(runKey, 0, false)
			run, err := retrieveStagedUpdateRun(runKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get cluster staged update run %s", runKey)
			newSnapshotName = run.GetStatus().ResourceSnapshotNameToRollout
			Expect(newSnapshotName).NotTo(Equal(initialSnapshotName), "Cluster staged update run %s rolls out the initial resource snapshot", runKey)

			Eventually(placementPolicyBindingsActual(policyKey, map[string]wantPlacementBinding{
				memberCluster2EastCanaryName: {selectors: canarySelectors, snapshotName: newSnapshotName, status: synchronizedPlacementBindingStatus(1)},
				firstProdClusterName:         {selectors: prodSelectors, snapshotName: initialSnapshotName, status: synchronizedPlacementBindingStatus(1)},
			}), placementPolicyRolloutEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll the new version out to the canary cluster only")
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, memberCluster2EastCanary), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the new version on the canary cluster")
		})

		It("can raise the prod selector's count to 2 while the run holds the binding manager role", func() {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				policy.GetSpec().ClusterSelectors[1].Count = ptr.To(intstr.FromInt32(2))
			})
		})

		It("should want the second prod cluster but hold off binding it while the run holds the role", func() {
			// Scheduling decisions and rollouts are mutually exclusive: the policy defers the new binding (retrying on its
			// backoff) until the run gives up the binding manager role.
			Eventually(heldBackActual(), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the second prod cluster as wanted")
			Consistently(heldBackActual(), consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The second prod cluster was bound while the run held the binding manager role")
		})

		It("can approve the canary stage", func() {
			approvePlacementPolicyApprovalRequest(approvalRequestKey)
		})

		It("should complete the run", func() {
			// The prod stage covers only the prod cluster that was bound when the run started.
			Eventually(stagedUpdateRunSucceededActual(runKey), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to complete cluster staged update run %s", runKey)
		})

		It("should bind the second prod cluster to the new resource snapshot once the run gives up the role", func() {
			// A new binding gets the latest resource snapshot, which the run has just rolled out everywhere else.
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				memberCluster2EastCanaryName: canarySelectors,
				firstProdClusterName:         prodSelectors,
				secondProdClusterName:        prodSelectors,
			}, 1), placementPolicyRolloutEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to bind the second prod cluster")
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the new version on every member cluster")

			wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})
	})

	Context("annotation-generated placement policy rolled out by a staged update run", Ordered, func() {
		nsName := appNamespace().Name
		strategyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyGeneratedStrategyNameTemplate, GinkgoParallelProcess())}
		runKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyGeneratedRunNameTemplate, GinkgoParallelProcess())}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}
		canaryTerms := clusterSelectorTermsMatchingLabels(map[string]string{envLabelName: envCanary})
		prodTerms := clusterSelectorTermsMatchingLabels(map[string]string{envLabelName: envProd})
		// The generated policy has two selectors, the canary cluster and every prod cluster; each binding records the
		// one that picked its cluster.
		wantSelectorsByCluster := func() map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
			return map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
				memberCluster2EastCanaryName: {canaryTerms},
				memberCluster1EastProdName:   {prodTerms},
				memberCluster3WestProdName:   {prodTerms},
			}
		}

		var configMap corev1.ConfigMap
		var policyKey types.NamespacedName
		var initialSnapshotName, newSnapshotName string
		var approvalRequestKey types.NamespacedName

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a strategy with a canary stage, gated by an approval, before a prod stage")
			strategy := newStagedUpdateStrategyObject(strategyKey, canaryApprovalStrategySpec())
			Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create staged update strategy %s", strategyKey)

			By("annotating the ConfigMap so that the hub agent generates a placement policy for the canary and prod clusters")
			configMap = appConfigMap()
			setClusterSelectorsAnnotation(&configMap, fmt.Sprintf("%s=%s;%s=%s,count=%s", envLabelName, envCanary, envLabelName, envProd, clusterSelectorCountAll))
		})

		AfterAll(func() {
			// The work resources must go even if the cleanup below fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			// The run goes first, so that it gives up the binding manager role before the policy goes.
			ensureStagedUpdateRunRemoved(runKey)
			ensureStagedUpdateStrategyRemoved(strategyKey)
			// Removing the annotation makes the hub agent delete the generated policy.
			setClusterSelectorsAnnotation(&configMap, "")
			if policyKey.Name != "" {
				ensurePlacementPolicyRemoved(policyKey)
			}
		})

		It("should generate a placement policy that places the ConfigMap on every member cluster", func() {
			// The provenance labels are how a user finds the generated policy, whose name is the hub agent's to choose.
			Eventually(func() error {
				policies := &kfplacementv1alpha1.PlacementPolicyList{}
				if err := hubClient.List(ctx, policies, client.InNamespace(cmKey.Namespace), generatedPolicyLabels("", "ConfigMap", cmKey.Name)); err != nil {
					return err
				}
				if len(policies.Items) != 1 {
					return fmt.Errorf("%d placement policies are generated for ConfigMap %s, want 1", len(policies.Items), cmKey)
				}
				policyKey = types.NamespacedName{Namespace: policies.Items[0].Namespace, Name: policies.Items[0].Name}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate a placement policy for ConfigMap %s", cmKey)
			initialSnapshotName = waitForPlacementPolicySynchronized(policyKey, wantSelectorsByCluster(), 1)
			Eventually(configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the initial version on every member cluster")
		})

		It("can change the ConfigMap on the hub, and roll the change out with a staged update run on the generated policy", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
			run := newStagedUpdateRunObject(runKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: strategyKey.Name,
			})
			Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create staged update run %s", runKey)
		})

		It("should roll the new version out to the canary cluster, then wait for an approval", func() {
			approvalRequestKey = waitForStageApprovalRequest(runKey, 0, false)
			newSnapshotName = resourceSnapshotNameToRolloutOf(runKey)
			Expect(newSnapshotName).NotTo(Equal(initialSnapshotName), "Staged update run %s rolls out the initial resource snapshot", runKey)
			onlyCanaryActual := onlyCanaryUpdatedActual(policyKey, cmKey, initialSnapshotName, newSnapshotName, v0Data, v1Data)
			Eventually(onlyCanaryActual, workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the new version out to the canary cluster only")
			Consistently(onlyCanaryActual, consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The new version reached the prod clusters before the approval")
		})

		It("can approve the canary stage", func() {
			approvePlacementPolicyApprovalRequest(approvalRequestKey)
		})

		It("should roll the new version out to every member cluster, keeping the generated policy as it was", func() {
			Eventually(stagedUpdateRunSucceededActual(runKey), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to complete staged update run %s", runKey)
			// The bindings still record the selectors that the annotation generated.
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, wantSelectorsByCluster(), 1), workloadEventuallyDuration, eventuallyInterval).
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
