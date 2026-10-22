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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kfrolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
)

const (
	placementPolicyFailurePolicyNameTemplate                  = "pp-failure-%s-%d"
	clusterPlacementPolicyFailurePolicyNameTemplate           = "cpp-failure-%s-%d"
	placementPolicyFailurePolicyRunNameTemplate               = "pp-failure-%s-run-%d"
	clusterPlacementPolicyFailurePolicyRunNameTemplate        = "cpp-failure-%s-run-%d"
	placementPolicyFailurePolicyStrategyNameTemplate          = "pp-failure-strategy-%d"
	clusterPlacementPolicyFailurePolicyStrategyNameTemplate   = "cpp-failure-strategy-%d"
	placementPolicyFailurePolicyDeploymentNameTemplate        = "pp-failure-%s"
	clusterPlacementPolicyFailurePolicyDeploymentNameTemplate = "cpp-failure-%s"

	placementPolicyInitFailureNameTemplate                = "pp-init-failure-%d"
	placementPolicyInitFailureOtherNameTemplate           = "pp-init-failure-other-%d"
	placementPolicyInitFailureStrategyNameTemplate        = "pp-init-failure-strategy-%d"
	placementPolicyInitFailureSortingStrategyNameTemplate = "pp-init-failure-sorting-strategy-%d"
	placementPolicyNoStrategyRunNameTemplate              = "pp-init-failure-no-strategy-%d"
	placementPolicyNoPolicyRunNameTemplate                = "pp-init-failure-no-policy-%d"
	placementPolicyForeignSnapshotRunNameTemplate         = "pp-init-failure-foreign-snapshot-%d"
	placementPolicyNoSortingLabelRunNameTemplate          = "pp-init-failure-no-sorting-label-%d"

	// placementPolicyUnpullableImage is an image that no node can pull: the registry host does not resolve.
	placementPolicyUnpullableImage = "fleet-e2e.invalid/no-such-image:v1"
	// placementPolicyNoPodsQuotaName names the resource quota that blocks new pods in a namespace on a member cluster.
	placementPolicyNoPodsQuotaName = "pp-e2e-no-pods"
	// placementPolicyOtherConfigMapName names the ConfigMap that a second policy places.
	placementPolicyOtherConfigMapName = "pp-e2e-other-config"

	// placementPolicyFailedRunEventuallyDuration bounds the wait on a run whose cluster times out: the run gives a
	// cluster 1 minute, and checks on it in reconciles that each last up to the run controller's 30s refresh period.
	placementPolicyFailedRunEventuallyDuration = 3 * time.Minute
)

// failurePolicyCase is one of the runs that the failure policy spec rolls out, one at a time.
type failurePolicyCase struct {
	description string
	policyKey   types.NamespacedName
	strategyKey types.NamespacedName
	runKey      types.NamespacedName
	deployKey   types.NamespacedName

	initialSnapshotName string
}

// newFailurePolicyDeployment returns a Deployment for the failure policy spec; each has its own pod labels, so that
// the Deployments in the namespace never select each other's pods.
func newFailurePolicyDeployment(key types.NamespacedName) *appsv1.Deployment {
	deploy := appDeployment()
	deploy.Name = key.Name
	deploy.Namespace = key.Namespace
	podLabels := map[string]string{"app": key.Name}
	deploy.Spec.Selector = &metav1.LabelSelector{MatchLabels: podLabels}
	deploy.Spec.Template.Labels = maps.Clone(podLabels)
	return &deploy
}

// updateDeploymentOnHub applies the mutation to a Deployment on the hub cluster, retrying on conflicts.
func updateDeploymentOnHub(key types.NamespacedName, mutate func(deploy *appsv1.Deployment)) {
	GinkgoHelper()

	Eventually(func() error {
		deploy := &appsv1.Deployment{}
		if err := hubClient.Get(ctx, key, deploy); err != nil {
			return err
		}
		mutate(deploy)
		return hubClient.Update(ctx, deploy)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update Deployment %s on the hub cluster", key)
}

// The FEP-0001 placement experience is alpha and runs behind the enable-placement-policy-apis hub and member
// agent flags, which test/e2e/setup.sh turns on.
var _ = Describe("placement policy staged update run failures", Label("placementpolicy"), func() {
	// The four runs below each hold a staged update run worker while a cluster times out, and the workers are shared
	// by every run in the fleet; running them one at a time keeps the rest of the lane going.
	Context("placement policies rolled out by staged update runs with failure policies, one run at a time", Ordered, func() {
		nsName := appNamespace().Name
		ppStrategyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyFailurePolicyStrategyNameTemplate, GinkgoParallelProcess())}
		cppStrategyKey := types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyFailurePolicyStrategyNameTemplate, GinkgoParallelProcess())}

		newCase := func(clusterScoped bool, variant string) *failurePolicyCase {
			if clusterScoped {
				return &failurePolicyCase{
					description: fmt.Sprintf("cluster placement policy (%s)", variant),
					policyKey:   types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyFailurePolicyNameTemplate, variant, GinkgoParallelProcess())},
					strategyKey: cppStrategyKey,
					runKey:      types.NamespacedName{Name: fmt.Sprintf(clusterPlacementPolicyFailurePolicyRunNameTemplate, variant, GinkgoParallelProcess())},
					deployKey:   types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(clusterPlacementPolicyFailurePolicyDeploymentNameTemplate, variant)},
				}
			}
			return &failurePolicyCase{
				description: fmt.Sprintf("placement policy (%s)", variant),
				policyKey:   types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyFailurePolicyNameTemplate, variant, GinkgoParallelProcess())},
				strategyKey: ppStrategyKey,
				runKey:      types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyFailurePolicyRunNameTemplate, variant, GinkgoParallelProcess())},
				deployKey:   types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyFailurePolicyDeploymentNameTemplate, variant)},
			}
		}
		// Variant a allows no failure; variant b allows one.
		failingCases := []*failurePolicyCase{newCase(false, "a"), newCase(true, "a")}
		partialCases := []*failurePolicyCase{newCase(false, "b"), newCase(true, "b")}
		allCases := append(append([]*failurePolicyCase{}, failingCases...), partialCases...)

		createRun := func(c *failurePolicyCase, maxAllowedClusterFailures int32) {
			run := newStagedUpdateRunObject(c.runKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      c.policyKey.Name,
				StagedUpdateStrategyName: c.strategyKey.Name,
				FailurePolicy: &kfrolloutv1alpha1.StagedUpdateRunFailurePolicy{
					MaxAllowedClusterFailures:    ptr.To(maxAllowedClusterFailures),
					MaxWaitTimePerClusterMinutes: 1,
				},
			})
			Expect(hubClient.Create(ctx, run)).To(Succeed(), "Failed to create staged update run %s", c.runKey)
		}

		BeforeAll(func() {
			By("creating the work resources and a Deployment per run")
			createWorkResources()
			for _, c := range allCases {
				Expect(hubClient.Create(ctx, newFailurePolicyDeployment(c.deployKey))).To(Succeed(), "Failed to create Deployment %s", c.deployKey)
			}

			By("creating a policy per run, each placing its own Deployment on every member cluster")
			for _, c := range allCases {
				selector := kfplacementv1alpha1.ResourceSelector{APIGroup: appsv1.GroupName, APIVersion: "v1", Kind: "Deployment", Name: c.deployKey.Name}
				if c.policyKey.Namespace == "" {
					selector.Namespace = c.deployKey.Namespace
				}
				policy := newPlacementPolicyObject(c.policyKey, kfplacementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{selector},
				})
				Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create %s %s", c.description, c.policyKey)
			}

			By("creating a strategy per scope with a canary stage before a prod stage")
			for _, strategyKey := range []types.NamespacedName{ppStrategyKey, cppStrategyKey} {
				strategy := newStagedUpdateStrategyObject(strategyKey, kfrolloutv1alpha1.StagedUpdateStrategySpec{
					Stages: []kfrolloutv1alpha1.Stage{
						{Name: placementPolicyCanaryStageName, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{envLabelName: envCanary}}},
						{Name: placementPolicyProdStageName, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{envLabelName: envProd}}},
					},
				})
				Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create staged update strategy %s", strategyKey)
			}
		})

		AfterAll(func() {
			// The work resources, and the resource quota with the member namespace, must go even if the cleanup
			// below fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			for _, c := range allCases {
				ensureStagedUpdateRunRemoved(c.runKey)
			}
			ensureStagedUpdateStrategyRemoved(ppStrategyKey)
			ensureStagedUpdateStrategyRemoved(cppStrategyKey)
			for _, c := range allCases {
				ensurePlacementPolicyRemoved(c.policyKey)
			}
		})

		It("should place every Deployment on every member cluster", func() {
			for _, c := range allCases {
				// The first placement pulls the image on every node, so it gets more time.
				Eventually(allSynchronizedPlacementPolicyBindingsActual(c.policyKey, noTermsSelectorsForAllMemberClusters(), 1), longEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to place the Deployment of %s %s", c.description, c.policyKey)
				c.initialSnapshotName = latestPrimarySnapshotNameOfPlacementPolicy(c.policyKey)
				Expect(deploymentAvailableOnClustersActual(c.deployKey, allMemberClusters...)()).To(Succeed(), "Deployment %s is not available", c.deployKey)
			}
		})

		for _, c := range failingCases {
			It(fmt.Sprintf("can start a staged update run for the %s that rolls out an image that cannot be pulled, allowing no failure", c.description), func() {
				updateDeploymentOnHub(c.deployKey, func(deploy *appsv1.Deployment) {
					deploy.Spec.Template.Spec.Containers[0].Image = placementPolicyUnpullableImage
				})
				createRun(c, 0)
			})

			It(fmt.Sprintf("should fail the run of the %s at the canary cluster, leaving the prod stage untouched", c.description), func() {
				Eventually(stagedUpdateRunConditionActual(c.runKey, kfrolloutv1alpha1.StagedUpdateRunCondTypeCompleted, metav1.ConditionTrue,
					kfrolloutv1alpha1.StagedUpdateRunCompletedCondReasonFailed), placementPolicyFailedRunEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to fail staged update run %s", c.runKey)
				Expect(stagedUpdateRunClusterResultsActual(c.runKey, map[string]string{
					memberCluster2EastCanaryName: kfrolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed,
					memberCluster1EastProdName:   "",
					memberCluster3WestProdName:   "",
				})()).To(Succeed(), "Staged update run %s did not stop at the canary cluster", c.runKey)
				Expect(placementPolicyBindingSnapshotsActual(c.policyKey, map[string]string{
					memberCluster2EastCanaryName: resourceSnapshotNameToRolloutOf(c.runKey),
					memberCluster1EastProdName:   c.initialSnapshotName,
					memberCluster3WestProdName:   c.initialSnapshotName,
				})()).To(Succeed(), "The run of %s %s touched the prod clusters", c.description, c.policyKey)
				Eventually(placementPolicyBindingManagerActual(c.policyKey, nil), eventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to release the binding manager role of %s %s", c.description, c.policyKey)
			})
		}

		It("can block new pods in the namespace on the canary cluster with a resource quota", func() {
			quota := &corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: placementPolicyNoPodsQuotaName, Namespace: nsName},
				Spec: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{corev1.ResourcePods: resource.MustParse("0")},
				},
			}
			// The quota blocks only pods created from now on; the Deployments keep the pods they have.
			Expect(client.IgnoreAlreadyExists(memberCluster2EastCanary.KubeClient.Create(ctx, quota))).To(Succeed(),
				"Failed to create resource quota %s on cluster %s", client.ObjectKeyFromObject(quota), memberCluster2EastCanary.ClusterName)
		})

		for _, c := range partialCases {
			It(fmt.Sprintf("can start a staged update run for the %s that changes the pod template, allowing one failure", c.description), func() {
				updateDeploymentOnHub(c.deployKey, func(deploy *appsv1.Deployment) {
					deploy.Spec.Template.Annotations = map[string]string{"placement-policy-e2e/revision": "v1"}
				})
				createRun(c, 1)
			})

			It(fmt.Sprintf("should complete the run of the %s with a partial success, only the canary cluster failing", c.description), func() {
				Eventually(stagedUpdateRunConditionActual(c.runKey, kfrolloutv1alpha1.StagedUpdateRunCondTypeCompleted, metav1.ConditionTrue,
					kfrolloutv1alpha1.StagedUpdateRunCompletedCondReasonPartiallySucceeded), placementPolicyFailedRunEventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to complete staged update run %s with a partial success", c.runKey)
				Expect(stagedUpdateRunClusterResultsActual(c.runKey, map[string]string{
					memberCluster2EastCanaryName: kfrolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonFailed,
					memberCluster1EastProdName:   kfrolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded,
					memberCluster3WestProdName:   kfrolloutv1alpha1.StagedUpdateRunPerClusterCompletedCondReasonSucceeded,
				})()).To(Succeed(), "Staged update run %s did not fail the canary cluster alone", c.runKey)
				newSnapshotName := resourceSnapshotNameToRolloutOf(c.runKey)
				Expect(placementPolicyBindingSnapshotsActual(c.policyKey, map[string]string{
					memberCluster2EastCanaryName: newSnapshotName,
					memberCluster1EastProdName:   newSnapshotName,
					memberCluster3WestProdName:   newSnapshotName,
				})()).To(Succeed(), "The run of %s %s did not move every binding to the new resource snapshot", c.description, c.policyKey)
				Eventually(placementPolicyBindingManagerActual(c.policyKey, nil), eventuallyDuration, eventuallyInterval).
					Should(Succeed(), "Failed to release the binding manager role of %s %s", c.description, c.policyKey)
			})
		}
	})

	Context("placement policy with staged update runs that fail to initialize", Ordered, func() {
		nsName := appNamespace().Name
		policyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyInitFailureNameTemplate, GinkgoParallelProcess())}
		otherPolicyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyInitFailureOtherNameTemplate, GinkgoParallelProcess())}
		strategyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyInitFailureStrategyNameTemplate, GinkgoParallelProcess())}
		sortingStrategyKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyInitFailureSortingStrategyNameTemplate, GinkgoParallelProcess())}
		noStrategyRunKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyNoStrategyRunNameTemplate, GinkgoParallelProcess())}
		noPolicyRunKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyNoPolicyRunNameTemplate, GinkgoParallelProcess())}
		foreignSnapshotRunKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyForeignSnapshotRunNameTemplate, GinkgoParallelProcess())}
		noSortingLabelRunKey := types.NamespacedName{Namespace: nsName, Name: fmt.Sprintf(placementPolicyNoSortingLabelRunNameTemplate, GinkgoParallelProcess())}
		cmKey := types.NamespacedName{Namespace: nsName, Name: appConfigMap().Name}
		v0Data := appConfigMap().Data
		v1Data := map[string]string{"data": "test-v1"}

		var initialSnapshotName, otherSnapshotName string

		createRunAndWaitForInitFailure := func(runKey types.NamespacedName, spec kfrolloutv1alpha1.StagedUpdateRunSpec) {
			GinkgoHelper()

			Expect(hubClient.Create(ctx, newStagedUpdateRunObject(runKey, spec))).To(Succeed(), "Failed to create staged update run %s", runKey)
			// A run that fails to initialize reports that alone: it picks no resource snapshot and sets up no stage.
			wantStatus := kfrolloutv1alpha1.StagedUpdateRunStatus{
				Conditions: []metav1.Condition{
					placementPolicyCondition(kfrolloutv1alpha1.StagedUpdateRunCondTypeInitialized, metav1.ConditionFalse,
						kfrolloutv1alpha1.StagedUpdateRunInitializedCondReasonFailed),
				},
			}
			Eventually(stagedUpdateRunStatusActual(runKey, wantStatus), placementPolicyRolloutEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report that staged update run %s failed to initialize", runKey)
		}

		BeforeAll(func() {
			By("creating the work resources and a second ConfigMap")
			createWorkResources()
			otherCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: placementPolicyOtherConfigMapName, Namespace: nsName},
				Data:       map[string]string{"data": "other"},
			}
			Expect(hubClient.Create(ctx, otherCM)).To(Succeed(), "Failed to create ConfigMap %s", client.ObjectKeyFromObject(otherCM))

			By("creating a policy for the app ConfigMap, and another one for the second ConfigMap")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create placement policy %s", policyKey)
			otherPolicy := newPlacementPolicyObject(otherPolicyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{
					{APIVersion: "v1", Kind: "ConfigMap", Name: placementPolicyOtherConfigMapName},
				},
			})
			Expect(hubClient.Create(ctx, otherPolicy)).To(Succeed(), "Failed to create placement policy %s", otherPolicyKey)

			By("creating a valid strategy, and one that sorts clusters by a label that no cluster has")
			strategy := newStagedUpdateStrategyObject(strategyKey, kfrolloutv1alpha1.StagedUpdateStrategySpec{
				Stages: []kfrolloutv1alpha1.Stage{{Name: placementPolicyAllClustersStageName, LabelSelector: &metav1.LabelSelector{}}},
			})
			Expect(hubClient.Create(ctx, strategy)).To(Succeed(), "Failed to create staged update strategy %s", strategyKey)
			sortingStrategy := newStagedUpdateStrategyObject(sortingStrategyKey, kfrolloutv1alpha1.StagedUpdateStrategySpec{
				Stages: []kfrolloutv1alpha1.Stage{
					{
						Name:            placementPolicyAllClustersStageName,
						LabelSelector:   &metav1.LabelSelector{},
						SortingLabelKey: ptr.To("placement-policy-e2e/no-such-label"),
					},
				},
			})
			Expect(hubClient.Create(ctx, sortingStrategy)).To(Succeed(), "Failed to create staged update strategy %s", sortingStrategyKey)
		})

		AfterAll(func() {
			// The work resources, the second ConfigMap among them, must go even if the cleanup below fails; otherwise
			// they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			for _, runKey := range []types.NamespacedName{noStrategyRunKey, noPolicyRunKey, foreignSnapshotRunKey, noSortingLabelRunKey} {
				ensureStagedUpdateRunRemoved(runKey)
			}
			ensureStagedUpdateStrategyRemoved(strategyKey)
			ensureStagedUpdateStrategyRemoved(sortingStrategyKey)
			// ensurePlacementPolicyRemoved waits for the app ConfigMap to go, which only the main policy places.
			ensurePlacementPolicyRemoved(policyKey)
			deletePlacementPolicyAndWait(otherPolicyKey)
			Eventually(configMapAbsentOnClustersActual(types.NamespacedName{Namespace: nsName, Name: placementPolicyOtherConfigMapName}, allMemberClusters...),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the second ConfigMap from the member clusters")
		})

		It("should place both ConfigMaps on every member cluster", func() {
			initialSnapshotName = waitForPlacementPolicySynchronized(policyKey, noTermsSelectorsForAllMemberClusters(), 1)
			otherSnapshotName = waitForPlacementPolicySynchronized(otherPolicyKey, noTermsSelectorsForAllMemberClusters(), 1)
			Eventually(configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to place the initial version on every member cluster")
		})

		It("can change the ConfigMap on the hub", func() {
			updateConfigMapDataOnHub(cmKey, v1Data)
		})

		It("should fail to initialize a run whose strategy does not exist", func() {
			createRunAndWaitForInitFailure(noStrategyRunKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: "pp-e2e-no-such-strategy",
			})
		})

		It("should fail to initialize a run whose policy does not exist", func() {
			createRunAndWaitForInitFailure(noPolicyRunKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      "pp-e2e-no-such-policy",
				StagedUpdateStrategyName: strategyKey.Name,
			})
		})

		It("should fail to initialize a run that names a resource snapshot of another policy", func() {
			createRunAndWaitForInitFailure(foreignSnapshotRunKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: strategyKey.Name,
				ResourceSnapshotName:     otherSnapshotName,
			})
		})

		It("should fail to initialize a run whose strategy sorts clusters by a label they lack", func() {
			createRunAndWaitForInitFailure(noSortingLabelRunKey, kfrolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyKey.Name,
				StagedUpdateStrategyName: sortingStrategyKey.Name,
			})
		})

		It("should roll nothing out, with the binding manager role released", func() {
			// None of the runs got as far as taking a new resource snapshot, so the change is still pending.
			wantStatus := resourcesChangedPendingRolloutPlacementPolicyStatus(int32(len(allMemberClusters)))
			wantStatus.LatestResourceRevisionName = ptr.To(initialSnapshotName)
			statusActual := placementPolicyStatusActual(policyKey, wantStatus)
			snapshotsActual := placementPolicyBindingSnapshotsActual(policyKey, map[string]string{
				memberCluster1EastProdName:   initialSnapshotName,
				memberCluster2EastCanaryName: initialSnapshotName,
				memberCluster3WestProdName:   initialSnapshotName,
			})
			dataActual := configMapDataOnClustersActual(cmKey, v0Data, allMemberClusters...)
			unchangedActual := func() error {
				if err := statusActual(); err != nil {
					return err
				}
				if err := snapshotsActual(); err != nil {
					return err
				}
				return dataActual()
			}
			Eventually(unchangedActual, workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the change as still pending a rollout")
			Consistently(unchangedActual, consistentlyDuration, consistentlyInterval).
				Should(Succeed(), "A run that failed to initialize rolled something out")
		})
	})
})
