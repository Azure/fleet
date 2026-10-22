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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kfrolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
)

const (
	placementPolicyAutoRolloutNameTemplate                    = "pp-auto-rollout-%d"
	placementPolicyAutoRolloutDisabledNameTemplate            = "pp-auto-rollout-disabled-%d"
	placementPolicyAutoRolloutCoexistenceNameTemplate         = "pp-auto-rollout-coexist-%d"
	placementPolicyAutoRolloutCoexistenceStrategyNameTemplate = "pp-auto-rollout-coexist-strategy-%d"
	placementPolicyAutoRolloutCoexistenceRunNameTemplate      = "pp-auto-rollout-coexist-run-%d"

	// placementPolicyAutoRolloutMinimumWaitPeriod is the minimum wait period that the specs set on their policies, the
	// shortest one allowed.
	placementPolicyAutoRolloutMinimumWaitPeriod = time.Minute

	// placementPolicyAutoRolloutDisabledConsistentlyDuration is how long the specs watch a pending change that
	// auto-rollout must leave alone.
	placementPolicyAutoRolloutDisabledConsistentlyDuration = 30 * time.Second

	// placementPolicyAutoRolloutRetryEventuallyDuration bounds the wait on an auto-rollout that a staged update run has
	// held back: auto-rollout does not react to the run giving up the binding manager role, and retries its claim on a
	// backoff that starts at 5s and doubles, up to 180s.
	placementPolicyAutoRolloutRetryEventuallyDuration = 3 * time.Minute
)

// autoRolloutAnnotations returns the annotations that enable auto-rollout on a policy with the specs' minimum wait
// period.
func autoRolloutAnnotations() map[string]string {
	return map[string]string{
		kfrolloutv1alpha1.AutoRolloutEnabledAnnotationKey:           kfrolloutv1alpha1.AutoRolloutEnabledAnnotationValue,
		kfrolloutv1alpha1.AutoRolloutMinimumWaitPeriodAnnotationKey: placementPolicyAutoRolloutMinimumWaitPeriod.String(),
	}
}

// lastAutoRolloutAttemptOf returns when auto-rollout last attempted a rollout of the policy, as recorded on it.
func lastAutoRolloutAttemptOf(key types.NamespacedName) time.Time {
	GinkgoHelper()

	policy, err := retrievePlacementPolicy(key)
	Expect(err).NotTo(HaveOccurred(), "Failed to get placement policy %s", key)
	val, found := policy.GetAnnotations()[kfrolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey]
	Expect(found).To(BeTrue(), "Placement policy %s records no auto-rollout attempt", key)
	attemptAt, err := time.Parse(time.RFC3339Nano, val)
	Expect(err).NotTo(HaveOccurred(), "Placement policy %s records an invalid auto-rollout attempt timestamp %q", key, val)
	return attemptAt
}

// The FEP-0001 placement experience is alpha and runs behind the enable-placement-policy-apis hub and member
// agent flags, which test/e2e/setup.sh turns on.
var _ = Describe("placement policy auto-rollout", Label("placementpolicy"), func() {
	Context("placement policy with auto-rollout enabled", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyAutoRolloutNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}
		v2Data := map[string]string{"data": "test-v2"}

		var initialSnapshotName, newSnapshotName string
		var lastAttemptAt time.Time

		allClustersSelectors := func() map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
			wantSelectorsByCluster := make(map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly, len(allMemberClusterNames))
			for _, clusterName := range allMemberClusterNames {
				wantSelectorsByCluster[clusterName] = []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}}
			}
			return wantSelectorsByCluster
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy with auto-rollout enabled")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			// The minimum wait period does not hold back the first auto-rollout, which follows no earlier one.
			policy.SetAnnotations(autoRolloutAnnotations())
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should place the initial version on every member cluster", func() {
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, allClustersSelectors(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to bind every member cluster")
			Eventually(configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the initial version on every member cluster")

			policy, err := retrievePlacementPolicy(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get placement policy %s", policyKey)
			snapshot, err := latestPrimarySnapshotOfPlacementPolicy(policy)
			Expect(err).NotTo(HaveOccurred(), "Failed to find the resource snapshot of placement policy %s", policyKey)
			initialSnapshotName = snapshot.GetName()
		})

		It("should not have attempted an auto-rollout for the initial placement", func() {
			// The initial placement needs no rollout: new bindings get the latest resource snapshot directly.
			policy, err := retrievePlacementPolicy(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get placement policy %s", policyKey)
			Expect(policy.GetAnnotations()).NotTo(HaveKey(kfrolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey),
				"An auto-rollout was attempted for the initial placement")
		})

		It("can change the ConfigMap on the hub", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
		})

		It("should roll out the new version to every member cluster without a staged update run", func() {
			Eventually(func() error {
				policy, err := retrievePlacementPolicy(policyKey)
				if err != nil {
					return err
				}
				snapshot, err := latestPrimarySnapshotOfPlacementPolicy(policy)
				if err != nil {
					return err
				}
				if snapshot.GetName() == initialSnapshotName {
					return fmt.Errorf("no new resource snapshot has been taken yet")
				}
				newSnapshotName = snapshot.GetName()
				return nil
			}, placementPolicyRolloutEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to take a new resource snapshot")
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, allClustersSelectors(), 1), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll out the new resource snapshot to every binding")
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the new version on every member cluster")
		})

		It("should record the auto-rollout attempt on the policy", func() {
			Eventually(func() error {
				policy, err := retrievePlacementPolicy(policyKey)
				if err != nil {
					return err
				}
				val, found := policy.GetAnnotations()[kfrolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey]
				if !found {
					return fmt.Errorf("placement policy %s has no last rollout attempt timestamp", policyKey)
				}
				if _, err := time.Parse(time.RFC3339, val); err != nil {
					return fmt.Errorf("placement policy %s has an invalid last rollout attempt timestamp %q: %w", policyKey, val, err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to record the auto-rollout attempt")
		})

		It("should report the policy as synchronized on every member cluster, with the binding manager role released", func() {
			wantStatus := allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})

		It("can change the ConfigMap on the hub again, right after the auto-rollout", func() {
			lastAttemptAt = lastAutoRolloutAttemptOf(policyKey)
			updateConfigMapDataOnHub(cmKey, v2Data)
		})

		It("should hold the second change back until the minimum wait period since the last attempt has passed", func() {
			wantStatus := resourcesChangedPendingRolloutPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(newSnapshotName)
			statusActual := placementPolicyStatusActual(policyKey, wantStatus)
			dataActual := configMapDataOnClustersActual(cmKey, v1Data, allMemberClusters...)
			heldActual := func() error {
				if err := statusActual(); err != nil {
					return err
				}
				return dataActual()
			}
			Eventually(heldActual, workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the second change as pending a rollout")
			// Hold until shortly before the period ends, leaving a margin for the controller to act on time.
			if holdFor := time.Until(lastAttemptAt.Add(placementPolicyAutoRolloutMinimumWaitPeriod - 5*time.Second)); holdFor > 0 {
				Consistently(heldActual, holdFor, consistentlyInterval).
					Should(Succeed(), "The second change was rolled out before the minimum wait period passed")
			}
		})

		It("should roll the second change out once the minimum wait period has passed", func() {
			Eventually(configMapDataOnClustersActual(cmKey, v2Data, allMemberClusters...), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the second change on every member cluster")
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, allClustersSelectors(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the latest resource snapshot out to every binding")
			Expect(latestPrimarySnapshotNameOfPlacementPolicy(policyKey)).NotTo(Equal(newSnapshotName), "No new resource snapshot was taken for the second change")

			secondAttemptAt := lastAutoRolloutAttemptOf(policyKey)
			Expect(secondAttemptAt.Sub(lastAttemptAt)).To(BeNumerically(">=", placementPolicyAutoRolloutMinimumWaitPeriod),
				"The second auto-rollout attempt at %v came less than the minimum wait period after the first one at %v", secondAttemptAt, lastAttemptAt)
		})
	})

	Context("placement policy with auto-rollout not enabled", Ordered, func() {
		policyKey := types.NamespacedName{
			Namespace: appNamespace().Name,
			Name:      fmt.Sprintf(placementPolicyAutoRolloutDisabledNameTemplate, GinkgoParallelProcess()),
		}
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}

		var initialSnapshotName string

		// pendingActual checks that the change waits for a rollout: no new snapshot, every cluster still on the initial
		// version, and no auto-rollout attempt recorded.
		pendingActual := func() func() error {
			wantStatus := resourcesChangedPendingRolloutPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(initialSnapshotName)
			statusActual := placementPolicyStatusActual(policyKey, wantStatus)
			wantBindings := make(map[string]wantPlacementBinding, len(allMemberClusterNames))
			for _, clusterName := range allMemberClusterNames {
				wantBindings[clusterName] = wantPlacementBinding{
					selectors:    []kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{}},
					snapshotName: initialSnapshotName,
					status:       synchronizedPlacementBindingStatus(1),
				}
			}
			bindingsActual := placementPolicyBindingsActual(policyKey, wantBindings)
			dataActual := configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...)
			return func() error {
				if err := statusActual(); err != nil {
					return err
				}
				if err := bindingsActual(); err != nil {
					return err
				}
				if err := dataActual(); err != nil {
					return err
				}
				policy, err := retrievePlacementPolicy(policyKey)
				if err != nil {
					return err
				}
				if val, found := policy.GetAnnotations()[kfrolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey]; found {
					return fmt.Errorf("placement policy %s records an auto-rollout attempt at %s", policyKey, val)
				}
				return nil
			}
		}

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy without the auto-rollout annotations")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if removing the policy fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
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

		It("should keep the change pending a rollout while the policy has no auto-rollout annotation", func() {
			Eventually(pendingActual(), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the change as pending a rollout")
			// An enabled auto-rollout acts within seconds of a change that follows no earlier attempt.
			Consistently(pendingActual(), placementPolicyAutoRolloutDisabledConsistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The change was rolled out without the auto-rollout annotation")
		})

		It("can set the auto-rollout annotation to false", func() {
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				annotations := policy.GetAnnotations()
				if annotations == nil {
					annotations = make(map[string]string)
				}
				annotations[kfrolloutv1alpha1.AutoRolloutEnabledAnnotationKey] = "false"
				policy.SetAnnotations(annotations)
			})
		})

		It("should keep the change pending a rollout with the annotation set to false", func() {
			Consistently(pendingActual(), placementPolicyAutoRolloutDisabledConsistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The change was rolled out with auto-rollout set to false")
		})
	})

	Context("placement policy with auto-rollout enabled, while a staged update run holds the binding manager role", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyAutoRolloutCoexistenceNameTemplate, GinkgoParallelProcess())}
		strategyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyAutoRolloutCoexistenceStrategyNameTemplate, GinkgoParallelProcess())}
		runKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyAutoRolloutCoexistenceRunNameTemplate, GinkgoParallelProcess())}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}

		var initialSnapshotName string
		var approvalRequestKey types.NamespacedName

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a placement policy with auto-rollout enabled")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			policy.SetAnnotations(autoRolloutAnnotations())
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)

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

		It("can create a staged update run, which holds the binding manager role while it waits for an approval", func() {
			run := newStagedUpdateRunObject(runKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: strategyKey.Name,
			})
			Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create staged update run %s", runKey)
			// Nothing has changed on the hub yet, so the run rolls out the initial snapshot, which every cluster already
			// has; its canary stage goes straight to the approval.
			approvalRequestKey = waitForStageApprovalRequest(runKey, 0, false)
			Expect(resourceSnapshotNameToRolloutOf(runKey)).To(Equal(initialSnapshotName), "Staged update run %s rolls out another resource snapshot", runKey)
			Eventually(placementPolicyBindingManagerActual(policyKey, &kfplacementv1alpha1.BindingManager{
				ControllerName: stagedUpdateRunBindingManagerName,
				ObjectRefs:     []kfplacementv1alpha1.ObjectReference{stagedUpdateRunObjectReference(runKey)},
			}), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to hold the binding manager role")
		})

		It("can change the ConfigMap on the hub", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
		})

		It("should not auto-roll the change out while the run holds the binding manager role", func() {
			wantStatus := resourcesChangedPendingRolloutPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(initialSnapshotName)
			wantStatus.BindingManager = &kfplacementv1alpha1.BindingManager{
				ControllerName: stagedUpdateRunBindingManagerName,
				ObjectRefs:     []kfplacementv1alpha1.ObjectReference{stagedUpdateRunObjectReference(runKey)},
			}
			statusActual := placementPolicyStatusActual(policyKey, wantStatus)
			dataActual := configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...)
			heldActual := func() error {
				if err := statusActual(); err != nil {
					return err
				}
				if err := dataActual(); err != nil {
					return err
				}
				// Auto-rollout records an attempt only once it has claimed the role.
				policy, err := retrievePlacementPolicy(policyKey)
				if err != nil {
					return err
				}
				if val, found := policy.GetAnnotations()[kfrolloutv1alpha1.AutoRolloutLastRolloutAttemptTimestampAnnotationKey]; found {
					return fmt.Errorf("placement policy %s records an auto-rollout attempt at %s", policyKey, val)
				}
				return nil
			}
			Eventually(heldActual, workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the change as pending a rollout")
			// The window is kept short: every failed claim doubles auto-rollout's backoff.
			Consistently(heldActual, consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "The change was auto-rolled out while the run held the binding manager role")
		})

		It("can approve the canary stage", func() {
			approvePlacementPolicyApprovalRequest(approvalRequestKey)
		})

		It("should complete the run", func() {
			Eventually(stagedUpdateRunSucceededActual(runKey), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to complete staged update run %s", runKey)
		})

		It("should auto-roll the change out once the run has given up the binding manager role", func() {
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, allMemberClusters...), placementPolicyAutoRolloutRetryEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to auto-roll the change out to every member cluster")
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, noTermsSelectorsForAllMemberClusters(), 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the latest resource snapshot out to every binding")
			Expect(latestPrimarySnapshotNameOfPlacementPolicy(policyKey)).NotTo(Equal(initialSnapshotName), "No new resource snapshot was taken for the change")
			Expect(lastAutoRolloutAttemptOf(policyKey)).NotTo(BeZero(), "Placement policy %s records no auto-rollout attempt", policyKey)
			Eventually(placementPolicyStatusActual(policyKey, allSynchronizedPlacementPolicyStatus(int32(len(allMemberClusters)))), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected placement policy status")
		})
	})

	Context("annotation-generated placement policy with auto-rollout set up on it", Ordered, func() {
		cmKey := types.NamespacedName{Namespace: appNamespace().Name, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}
		canaryLabels := map[string]string{envLabelName: envCanary}
		canarySelectors := map[string][]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{
			memberCluster2EastCanaryName: {clusterSelectorTermsMatchingLabels(canaryLabels)},
		}

		var configMap corev1.ConfigMap
		var policyKey types.NamespacedName
		var initialSnapshotName string

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("annotating the ConfigMap so that the hub agent generates a placement policy for the canary cluster")
			configMap = appConfigMap()
			setClusterSelectorsAnnotation(&configMap, fmt.Sprintf("%s=%s", envLabelName, envCanary))
		})

		AfterAll(func() {
			// The work resources must go even if the cleanup below fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			// Removing the annotation makes the hub agent delete the generated policy.
			if configMap.Name != "" {
				setClusterSelectorsAnnotation(&configMap, "")
			}
			if policyKey.Name != "" {
				ensurePlacementPolicyRemoved(policyKey)
			}
		})

		It("should generate a placement policy that places the ConfigMap on the canary cluster", func() {
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
			initialSnapshotName = waitForPlacementPolicySynchronized(policyKey, canarySelectors, 1)
			Eventually(configMapDataOnClustersActual(cmKey, v0Data, memberCluster2EastCanary), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the initial version on the canary cluster")
		})

		It("can set up auto-rollout on the generated policy", func() {
			// The hub agent owns the spec of a generated policy, but leaves the annotations it does not generate alone.
			updatePlacementPolicy(policyKey, func(policy kfplacementv1alpha1.PlacementPolicyAccessor) {
				annotations := policy.GetAnnotations()
				if annotations == nil {
					annotations = make(map[string]string)
				}
				maps.Copy(annotations, autoRolloutAnnotations())
				policy.SetAnnotations(annotations)
			})
		})

		It("can change the ConfigMap on the hub", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
		})

		It("should auto-roll the change out to the canary cluster", func() {
			Eventually(configMapDataOnClustersActual(cmKey, v1Data, memberCluster2EastCanary), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to auto-roll the change out to the canary cluster")
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, canarySelectors, 1), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to roll the latest resource snapshot out to the canary binding")
			Expect(latestPrimarySnapshotNameOfPlacementPolicy(policyKey)).NotTo(Equal(initialSnapshotName), "No new resource snapshot was taken for the change")

			policy, err := retrievePlacementPolicy(policyKey)
			Expect(err).NotTo(HaveOccurred(), "Failed to get placement policy %s", policyKey)
			Expect(policy.GetAnnotations()).To(HaveKeyWithValue(kfrolloutv1alpha1.AutoRolloutEnabledAnnotationKey, kfrolloutv1alpha1.AutoRolloutEnabledAnnotationValue),
				"The auto-rollout annotation is gone from the generated policy")
			Expect(lastAutoRolloutAttemptOf(policyKey)).NotTo(BeZero(), "Placement policy %s records no auto-rollout attempt", policyKey)
		})
	})
})
