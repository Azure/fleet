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

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const (
	canaryStageAfterStageWaitTime = time.Minute * 10
	prodStageAfterStageWaitTime   = time.Minute * 5

	defaultConcurrencyStageName = "default"
	fixedConcurrencyStageName   = "fixed"
	halfConcurrencyStageName    = "half"
	tenthConcurrencyStageName   = "tenth"

	groupLabelValueA = "a"
	groupLabelValueB = "b"
	groupLabelValueC = "c"
	groupLabelValueD = "d"
)

// Substrings of the messages that the controller reports when it fails to initialize a staged update run.
const (
	missingSortingLabelMsgSubstr    = "yet the label is missing on a cluster"
	nonIntegerSortingLabelMsgSubstr = "failed to parse sorting label value as integer"
	noPlacementBindingsMsgSubstr    = "no placement bindings are found"
	strategyNotFoundMsgSubstr       = "staged update strategy cannot be found"
	snapshotNotFoundMsgSubstr       = "the explicitly specified resource snapshot does not exist"
	snapshotNotPrimaryMsgSubstr     = "is not a primary resource snapshot"
	snapshotNotOwnedMsgSubstr       = "is not owned by the linked placement policy"
	snapshotDeletedMsgSubstr        = "has been marked for deletion"
)

var _ = Describe("initializing staged update runs", func() {
	// Unless a test case verifies an initialization failure, the staged update runs in this block are created with
	// the suspended flag set; the controller initializes such runs, but does not start rolling out changes, which
	// leaves the runs in a stable, fully initialized state.

	Context("label-selected stages", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member clusters 0 and 2 are canary clusters; member clusters 1, 3, and 4 are production clusters.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 5)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
			{envLabelKey: envLabelValueProd},
		}

		var policy placementv1alpha1.PlacementPolicyAccessor

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueCanary},
					},
					BeforeStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: canaryStageAfterStageWaitTime},
						},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
					MaxConcurrency: ptr.To(intstr.FromInt32(2)),
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: prodStageAfterStageWaitTime},
						},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the staged update run", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName: canaryStageName,
					Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 2),
					BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
					AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: canaryStageAfterStageWaitTime},
						},
					},
					// The max concurrency defaults to 1.
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					StageName: prodStageName,
					Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 1, 3, 4),
					AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: prodStageAfterStageWaitTime},
						},
					},
					MaxConcurrency: ptr.To(int32(2)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)

			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
		})

		It("should create a primary placement resource snapshot for the placement policy", func() {
			snapshotCreatedActual := primaryPlacementResourceSnapshotCreatedActual(policy, 0)
			Eventually(snapshotCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the placement resource snapshot")
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

	Context("overlapping, nil, and empty label selectors", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member cluster 1 is selected by both the east stage and the prod stage; member cluster 3 is selected by
		// none of the stages with a non-empty label selector.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary, regionLabelKey: regionLabelValueEast},
			{envLabelKey: envLabelValueProd, regionLabelKey: regionLabelValueEast},
			{envLabelKey: envLabelValueProd, regionLabelKey: regionLabelValueWest},
			{envLabelKey: envLabelValueCanary, regionLabelKey: regionLabelValueWest},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: eastStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{regionLabelKey: regionLabelValueEast},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
				},
				{
					// A stage with a nil label selector includes no clusters at all.
					Name: emptyStageName,
				},
				{
					// A stage with an empty label selector includes all the clusters that no previous stage has
					// included.
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the staged update run", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      eastStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 1),
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					// Member cluster 1 has been included in the east stage, which comes first.
					StageName:      prodStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 2),
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					StageName:      emptyStageName,
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					StageName:      restStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 3),
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)

			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
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

	Context("clusters selected by no stage", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member cluster 2 has no env label, and is selected by none of the stages.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
			{regionLabelKey: regionLabelValueWest},
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
				Suspended:                true,
			})
		})

		It("should initialize the staged update run, leaving out the clusters selected by no stage", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      canaryStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					StageName:      prodStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 1, 3),
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)

			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
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

	Context("clusters sorted by an integer label", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// The label values are picked so that their numerical order (1, 2, 2, 10) differs from both their lexical
		// order (1, 10, 2, 2) and the order of the cluster names; member clusters 1 and 3 share the same value.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueProd, orderLabelKey: "10"},
			{envLabelKey: envLabelValueProd, orderLabelKey: "2"},
			{envLabelKey: envLabelValueProd, orderLabelKey: "1"},
			{envLabelKey: envLabelValueProd, orderLabelKey: "2"},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
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
				Suspended:                true,
			})
		})

		It("should initialize the staged update run, with clusters sorted by the label value (and then by name)", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      prodStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 2, 1, 3, 0),
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)

			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
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

	Context("clusters missing the sorting label", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)

		// Member cluster 1 does not have the sorting label.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueProd, orderLabelKey: "1"},
			{envLabelKey: envLabelValueProd},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
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

		It("should fail to initialize the staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, runName, missingSortingLabelMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("clusters with a non-integer sorting label", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)

		// The sorting label value of member cluster 1 is not an integer.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueProd, orderLabelKey: "1"},
			{envLabelKey: envLabelValueProd, orderLabelKey: "first"},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
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

		It("should fail to initialize the staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, runName, nonIntegerSortingLabelMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("max concurrency resolution", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member cluster 0 is in group A; member clusters 1-2 are in group B; member clusters 3-7 are in group C;
		// member clusters 8-10 are in group D.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 11)
		memberClusterGroups := []string{
			groupLabelValueA,
			groupLabelValueB, groupLabelValueB,
			groupLabelValueC, groupLabelValueC, groupLabelValueC, groupLabelValueC, groupLabelValueC,
			groupLabelValueD, groupLabelValueD, groupLabelValueD,
		}
		memberClusterLabels := make([]map[string]string, 0, len(memberClusterGroups))
		for _, group := range memberClusterGroups {
			memberClusterLabels = append(memberClusterLabels, map[string]string{groupLabelKey: group})
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					// No max concurrency is specified; the API server sets it to 1 by default.
					Name: defaultConcurrencyStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{groupLabelKey: groupLabelValueA},
					},
				},
				{
					Name: fixedConcurrencyStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{groupLabelKey: groupLabelValueB},
					},
					MaxConcurrency: ptr.To(intstr.FromInt32(3)),
				},
				{
					Name: halfConcurrencyStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{groupLabelKey: groupLabelValueC},
					},
					MaxConcurrency: ptr.To(intstr.FromString("50%")),
				},
				{
					Name: tenthConcurrencyStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{groupLabelKey: groupLabelValueD},
					},
					MaxConcurrency: ptr.To(intstr.FromString("10%")),
				},
				{
					Name:           emptyStageName,
					MaxConcurrency: ptr.To(intstr.FromString("100%")),
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the staged update run, with the max concurrency of each stage resolved", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      defaultConcurrencyStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					// An integer value is used as it is, even if it exceeds the number of clusters in the stage.
					StageName:      fixedConcurrencyStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 1, 2),
					MaxConcurrency: ptr.To(int32(3)),
				},
				{
					// 50% of 5 clusters is 2.5, which is rounded down.
					StageName:      halfConcurrencyStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 3, 4, 5, 6, 7),
					MaxConcurrency: ptr.To(int32(2)),
				},
				{
					// 10% of 3 clusters is 0.3, which is rounded down to 0 and then raised to the minimum of 1.
					StageName:      tenthConcurrencyStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 8, 9, 10),
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					// 100% of no clusters is 0, which is raised to the minimum of 1.
					StageName:      emptyStageName,
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)

			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
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

	Context("placement bindings and member clusters to leave out", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		otherPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member clusters 0 and 1 are bound to the placement policy as usual; the others should be left out:
		// * member cluster 2 is bound to another placement policy;
		// * the placement binding of member cluster 3 has been marked for deletion;
		// * member cluster 4 does not exist; and
		// * member cluster 5 has been marked for deletion.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 6)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)

			fixtures := make([]client.Object, 0, 2*len(memberClusterNames)+2)
			fixtures = append(fixtures, policy)
			for idx := range memberClusterNames {
				if idx != 4 {
					fixtures = append(fixtures, createMemberCluster(memberClusterNames[idx], nil))
				}
				bindingPolicyName := policyName
				if idx == 2 {
					bindingPolicyName = otherPolicyName
				}
				fixtures = append(fixtures, createPlacementBinding(appNamespaceName, bindingNames[idx], bindingPolicyName, memberClusterNames[idx], previousResourceSnapshotName))
			}
			markObjectForDeletion(newPlacementBindingObj(appNamespaceName, bindingNames[3]))
			markObjectForDeletion(&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberClusterNames[5]}})

			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, strategy)...)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the staged update run, leaving out the placement bindings and member clusters that do not count", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      restStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 1),
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)

			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
		})

		AfterAll(func() {
			removeTestFinalizer(newPlacementBindingObj(appNamespaceName, bindingNames[3]))
			removeTestFinalizer(&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberClusterNames[5]}})
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

	Context("no placement bindings that count", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		otherPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)

		// Member cluster 0 does not exist; member cluster 1 has been marked for deletion; member cluster 2 is bound
		// to another placement policy.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 3)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			binding0 := createPlacementBinding(appNamespaceName, bindingNames[0], policyName, memberClusterNames[0], previousResourceSnapshotName)
			memberCluster1 := createMemberCluster(memberClusterNames[1], nil)
			binding1 := createPlacementBinding(appNamespaceName, bindingNames[1], policyName, memberClusterNames[1], previousResourceSnapshotName)
			memberCluster2 := createMemberCluster(memberClusterNames[2], nil)
			binding2 := createPlacementBinding(appNamespaceName, bindingNames[2], otherPolicyName, memberClusterNames[2], previousResourceSnapshotName)
			markObjectForDeletion(&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberClusterNames[1]}})

			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(policy, binding0, memberCluster1, binding1, memberCluster2, binding2, strategy)

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should fail to initialize the staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, runName, noPlacementBindingsMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		AfterAll(func() {
			removeTestFinalizer(&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberClusterNames[1]}})
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("placement bindings in another namespace", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member cluster 1 is bound by a placement binding in another namespace, which carries the same placement
		// policy name.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			memberCluster0 := createMemberCluster(memberClusterNames[0], nil)
			binding0 := createPlacementBinding(appNamespaceName, bindingNames[0], policyName, memberClusterNames[0], previousResourceSnapshotName)
			memberCluster1 := createMemberCluster(memberClusterNames[1], nil)
			binding1 := createPlacementBinding(otherAppNamespaceName, bindingNames[1], policyName, memberClusterNames[1], previousResourceSnapshotName)

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
				Suspended:                true,
			})
		})

		It("should initialize the staged update run, leaving out the placement bindings in other namespaces", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      restStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)

			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
		})

		AfterAll(func() {
			removeObjects(
				newStagedUpdateRunObj(appNamespaceName, runName),
				newPlacementResourceSnapshotObj(appNamespaceName, snapshotName),
				newStagedUpdateStrategyObj(appNamespaceName, strategyName),
				newPlacementPolicyObj(appNamespaceName, policyName),
				newConfigMapObj(configMapName),
				newPlacementBindingObj(appNamespaceName, bindingNames[0]),
				&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberClusterNames[0]}},
				newPlacementBindingObj(otherAppNamespaceName, bindingNames[1]),
				&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberClusterNames[1]}},
			)
		})
	})

	Context("missing staged update strategy", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		triggerApprovalRequestName := fmt.Sprintf(triggerApprovalRequestNameTemplate, randStr)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			waitForObjectsToBeCached(append(fixtures, policy)...)

			// The staged update strategy has not been created yet.
			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should fail to initialize the staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, runName, strategyNotFoundMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("should release the binding manager role of the placement policy", func() {
			// The controller claims the role before it initializes the run, and releases it once the run has failed
			// to initialize.
			roleReleasedActual := bindingManagerRoleReleasedActual(appNamespaceName, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		It("can create the staged update strategy", func() {
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(strategy)
		})

		It("can trigger a new reconciliation of the staged update run", func() {
			// The controller does not watch staged update strategies. The trigger leaves the run as is (in particular,
			// the run is not suspended): had the controller initialized the run now that the strategy exists, it would
			// go on to claim the binding manager role and roll out, which the check below would catch.
			triggerStagedUpdateRunReconciliation(appNamespaceName, runName, restStageName, triggerApprovalRequestName)
		})

		It("should not initialize the staged update run again", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, runName, strategyNotFoundMsgSubstr)
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

	Context("staged update strategy in another namespace", Ordered, func() {
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
			// The staged update strategy is created in a namespace other than that of the staged update run.
			strategy := createStagedUpdateStrategy(otherAppNamespaceName, strategyName, []rolloutv1alpha1.Stage{
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

		It("should fail to initialize the staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, runName, strategyNotFoundMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, runName),
					newStagedUpdateStrategyObj(otherAppNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("resource snapshot selection (no resource snapshot specified)", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		firstRunName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-0")
		secondRunName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-1")
		thirdRunName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-2")
		var firstSnapshotName string
		var secondSnapshotName string

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)
		wantStages := []rolloutv1alpha1.PerStageStatus{
			{
				StageName:      restStageName,
				Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
				MaxConcurrency: ptr.To(int32(1)),
			},
		}

		var policy placementv1alpha1.PlacementPolicyAccessor

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			firstSnapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			secondSnapshotName = primaryPlacementResourceSnapshotName(policy, 1)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)
		})

		It("should create a resource snapshot when none exists", func() {
			createStagedUpdateRun(appNamespaceName, firstRunName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})

			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(firstSnapshotName, wantStages)
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, firstRunName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")

			snapshotCreatedActual := primaryPlacementResourceSnapshotCreatedActual(policy, 0)
			Eventually(snapshotCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the placement resource snapshot")
		})

		It("should reuse the latest resource snapshot when the selected resources have not changed", func() {
			createStagedUpdateRun(appNamespaceName, secondRunName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})

			// Had a new resource snapshot been created, the run would roll out that one (of index 1) instead.
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(firstSnapshotName, wantStages)
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, secondRunName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
		})

		It("can update the selected resources", func() {
			updateConfigMapData(configMapName, map[string]string{configMapDataKey: altConfigMapDataValue})
		})

		It("should create a new resource snapshot when the selected resources have changed", func() {
			createStagedUpdateRun(appNamespaceName, thirdRunName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})

			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(secondSnapshotName, wantStages)
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, thirdRunName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")

			snapshotCreatedActual := primaryPlacementResourceSnapshotCreatedActual(policy, 1)
			Eventually(snapshotCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the placement resource snapshot")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(appNamespaceName, firstRunName),
					newStagedUpdateRunObj(appNamespaceName, secondRunName),
					newStagedUpdateRunObj(appNamespaceName, thirdRunName),
					newPlacementResourceSnapshotObj(appNamespaceName, firstSnapshotName),
					newPlacementResourceSnapshotObj(appNamespaceName, secondSnapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("resource snapshot selection (resource snapshot specified)", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		otherPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)

		// A primary snapshot of the placement policy (index 0, sub-index 0), along with its secondary snapshot
		// (index 0, sub-index 1).
		var primarySnapshotName string
		var secondarySnapshotName string
		// A primary snapshot of the placement policy (index 1) that has been marked for deletion.
		var deletedSnapshotName string
		// A primary snapshot of another placement policy.
		var otherPolicySnapshotName string
		// A snapshot that does not exist.
		var missingSnapshotName string

		runNames := make([]string, 0, 5)
		for idx := 0; idx < 5; idx++ {
			runNames = append(runNames, fmt.Sprintf(stagedUpdateRunNameTemplate, fmt.Sprintf("%s-%d", randStr, idx)))
		}

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			primarySnapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			secondarySnapshotName = secondaryPlacementResourceSnapshotName(policy, 0, 1)
			deletedSnapshotName = primaryPlacementResourceSnapshotName(policy, 1)
			missingSnapshotName = primaryPlacementResourceSnapshotName(policy, 2)
			otherPolicy := createPlacementPolicy(appNamespaceName, otherPolicyName, configMapName)
			otherPolicySnapshotName = primaryPlacementResourceSnapshotName(otherPolicy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})

			primarySnapshot := createPlacementResourceSnapshot(primarySnapshotName, policy, 0, 0, 2)
			secondarySnapshot := createPlacementResourceSnapshot(secondarySnapshotName, policy, 0, 1, 2)
			deletedSnapshot := createPlacementResourceSnapshot(deletedSnapshotName, policy, 1, 0, 1)
			otherPolicySnapshot := createPlacementResourceSnapshot(otherPolicySnapshotName, otherPolicy, 0, 0, 1)
			markObjectForDeletion(deletedSnapshot)

			waitForObjectsToBeCached(append(fixtures, policy, otherPolicy, strategy, primarySnapshot, secondarySnapshot, deletedSnapshot, otherPolicySnapshot)...)
		})

		It("should initialize the staged update run with the specified resource snapshot", func() {
			createStagedUpdateRun(appNamespaceName, runNames[0], rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				ResourceSnapshotName:     primarySnapshotName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})

			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      restStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(primarySnapshotName, wantStages)
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, runNames[0], wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
		})

		It("should fail to initialize the staged update run if the specified resource snapshot does not exist", func() {
			createStagedUpdateRun(appNamespaceName, runNames[1], rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				ResourceSnapshotName:     missingSnapshotName,
				StagedUpdateStrategyName: strategyName,
			})

			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, runNames[1], snapshotNotFoundMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("should fail to initialize the staged update run if the specified resource snapshot is not a primary one", func() {
			createStagedUpdateRun(appNamespaceName, runNames[2], rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				ResourceSnapshotName:     secondarySnapshotName,
				StagedUpdateStrategyName: strategyName,
			})

			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, runNames[2], snapshotNotPrimaryMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("should fail to initialize the staged update run if the specified resource snapshot belongs to another placement policy", func() {
			createStagedUpdateRun(appNamespaceName, runNames[3], rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				ResourceSnapshotName:     otherPolicySnapshotName,
				StagedUpdateStrategyName: strategyName,
			})

			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, runNames[3], snapshotNotOwnedMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("should fail to initialize the staged update run if the specified resource snapshot has been marked for deletion", func() {
			createStagedUpdateRun(appNamespaceName, runNames[4], rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				ResourceSnapshotName:     deletedSnapshotName,
				StagedUpdateStrategyName: strategyName,
			})

			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(appNamespaceName, runNames[4], snapshotDeletedMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		AfterAll(func() {
			removeTestFinalizer(newPlacementResourceSnapshotObj(appNamespaceName, deletedSnapshotName))

			objs := make([]client.Object, 0, len(runNames))
			for _, runName := range runNames {
				objs = append(objs, newStagedUpdateRunObj(appNamespaceName, runName))
			}
			removeObjects(slices.Concat(
				objs,
				[]client.Object{
					newPlacementResourceSnapshotObj(appNamespaceName, primarySnapshotName),
					newPlacementResourceSnapshotObj(appNamespaceName, secondarySnapshotName),
					newPlacementResourceSnapshotObj(appNamespaceName, deletedSnapshotName),
					newPlacementResourceSnapshotObj(appNamespaceName, otherPolicySnapshotName),
					newStagedUpdateStrategyObj(appNamespaceName, strategyName),
					newPlacementPolicyObj(appNamespaceName, policyName),
					newPlacementPolicyObj(appNamespaceName, otherPolicyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(appNamespaceName, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("one-shot initialization", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Initially, member cluster 0 is a canary cluster, and member clusters 1 and 2 are production clusters;
		// member cluster 3 joins (with a staging label) after the staged update run has been initialized.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
			{envLabelKey: envLabelValueProd},
		}

		wantStages := []rolloutv1alpha1.PerStageStatus{
			{
				StageName:      canaryStageName,
				Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
				MaxConcurrency: ptr.To(int32(1)),
			},
			{
				StageName:      prodStageName,
				Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 1, 2),
				MaxConcurrency: ptr.To(int32(1)),
			},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(appNamespaceName, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(appNamespaceName, policyName, memberClusterNames[:3], bindingNames[:3], memberClusterLabels)
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

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the staged update run", func() {
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
		})

		It("can change the staged update strategy, the member clusters, and the placement bindings", func() {
			// Were the staged update run to be initialized again after the changes, the canary stage would include
			// member cluster 3 only, and the prod stage would include member clusters 0, 1, and 2.
			updateStagedUpdateStrategyStages(appNamespaceName, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueStaging},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
				},
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			updateMemberClusterLabels(memberClusterNames[0], map[string]string{envLabelKey: envLabelValueProd})
			createMemberCluster(memberClusterNames[3], map[string]string{envLabelKey: envLabelValueStaging})
			createPlacementBinding(appNamespaceName, bindingNames[3], policyName, memberClusterNames[3], previousResourceSnapshotName)

			// Wait until the cache observes all the changes, so that a new initialization (if any) would see them.
			Eventually(func() error {
				strategy := newStagedUpdateStrategyObj(appNamespaceName, strategyName)
				if err := hubClient.Get(ctx, client.ObjectKeyFromObject(strategy), strategy); err != nil {
					return err
				}
				if len(strategy.GetSpec().Stages) != 3 {
					return fmt.Errorf("the staged update strategy has %d stages, want 3", len(strategy.GetSpec().Stages))
				}

				memberCluster := &clusterv1beta1.MemberCluster{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: memberClusterNames[0]}, memberCluster); err != nil {
					return err
				}
				if memberCluster.Labels[envLabelKey] != envLabelValueProd {
					return fmt.Errorf("member cluster %s has env label %q, want %q", memberClusterNames[0], memberCluster.Labels[envLabelKey], envLabelValueProd)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the changes in the cache")
			waitForObjectsToBeCached(
				&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberClusterNames[3]}},
				newPlacementBindingObj(appNamespaceName, bindingNames[3]),
			)
		})

		It("can resume the staged update run", func() {
			updateStagedUpdateRunSuspended(appNamespaceName, runName, false)
		})

		It("should mark the staged update run as resumed", func() {
			resumedActual := stagedUpdateRunConditionActual(appNamespaceName, runName, metav1.Condition{
				Type:   rolloutv1alpha1.StagedUpdateRunCondTypeSuspended,
				Status: metav1.ConditionFalse,
				Reason: rolloutv1alpha1.StagedUpdateRunSuspendedCondReasonResumed,
			})
			Eventually(resumedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to resume the staged update run")
		})

		It("should start rolling out to the member cluster in the first stage as initialized", func() {
			// Member cluster 0 belongs to the canary stage only as initialized.
			bindingUpdatedActual := placementBindingResourceSnapshotActual(appNamespaceName, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 0")

			// The rollout is blocked on member cluster 0 (its placement binding never becomes available); in
			// particular, member cluster 3 (which a new initialization would have put in the canary stage) is
			// never rolled out to.
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(appNamespaceName, bindingNames[1:], previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to other member clusters unexpectedly")
		})

		It("should keep the stages as initialized", func() {
			// The check runs only once the rollout has started, i.e., once the controller has processed the resumed run
			// (including its initialization step); the Resumed condition alone does not prove this, as the controller
			// sets it before it gets to the initialization step.
			stageLayoutActual := stagedUpdateRunStageLayoutActual(appNamespaceName, runName, wantStages)
			Eventually(stageLayoutActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to keep the stages as initialized")
		})

		AfterAll(func() {
			// Note that deleting the placement bindings also unblocks the rollout (the controller fails to find
			// the binding it is waiting for), so that the staged update run can be removed in time.
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

	Context("staged update runs created as suspended", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

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

			createStagedUpdateRun(appNamespaceName, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the staged update run and mark it as suspended (without starting it)", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      restStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 1),
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(appNamespaceName, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the staged update run")
		})

		It("should remove the cleanup finalizer from the staged update run", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(appNamespaceName, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
		})

		It("should release the binding manager role of the placement policy", func() {
			roleReleasedActual := bindingManagerRoleReleasedActual(appNamespaceName, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		It("should not roll out to any member cluster", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(appNamespaceName, bindingNames, previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to member clusters unexpectedly")
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

var _ = Describe("initializing cluster staged update runs", func() {
	// Unless a test case verifies an initialization failure, the cluster staged update runs in this block are created
	// with the suspended flag set; the controller initializes such runs, but does not start rolling out changes, which
	// leaves the runs in a stable, fully initialized state.

	Context("label-selected stages", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member clusters 0 and 2 are canary clusters; member clusters 1, 3, and 4 are production clusters.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 5)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
			{envLabelKey: envLabelValueProd},
		}

		var policy placementv1alpha1.PlacementPolicyAccessor

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueCanary},
					},
					BeforeStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: canaryStageAfterStageWaitTime},
						},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
					MaxConcurrency: ptr.To(intstr.FromInt32(2)),
					AfterStageTasks: []rolloutv1alpha1.StageTask{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: prodStageAfterStageWaitTime},
						},
					},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the cluster staged update run", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName: canaryStageName,
					Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 2),
					BeforeStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
					},
					AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: canaryStageAfterStageWaitTime},
						},
					},
					// The max concurrency defaults to 1.
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					StageName: prodStageName,
					Clusters:  wantPerClusterStatuses(memberClusterNames, bindingNames, 1, 3, 4),
					AfterStageTasks: []rolloutv1alpha1.PerStageTaskStatus{
						{
							Type: rolloutv1alpha1.StageTaskTypeApproval,
						},
						{
							Type:     rolloutv1alpha1.StageTaskTypeTimedWait,
							WaitTime: &metav1.Duration{Duration: prodStageAfterStageWaitTime},
						},
					},
					MaxConcurrency: ptr.To(int32(2)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)

			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")
		})

		It("should create a primary cluster placement resource snapshot for the cluster placement policy", func() {
			snapshotCreatedActual := primaryPlacementResourceSnapshotCreatedActual(policy, 0)
			Eventually(snapshotCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the cluster placement resource snapshot")
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

	Context("overlapping, nil, and empty label selectors", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member cluster 1 is selected by both the east stage and the prod stage; member cluster 3 is selected by
		// none of the stages with a non-empty label selector.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary, regionLabelKey: regionLabelValueEast},
			{envLabelKey: envLabelValueProd, regionLabelKey: regionLabelValueEast},
			{envLabelKey: envLabelValueProd, regionLabelKey: regionLabelValueWest},
			{envLabelKey: envLabelValueCanary, regionLabelKey: regionLabelValueWest},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: eastStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{regionLabelKey: regionLabelValueEast},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
				},
				{
					// A stage with a nil label selector includes no clusters at all.
					Name: emptyStageName,
				},
				{
					// A stage with an empty label selector includes all the clusters that no previous stage has
					// included.
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the cluster staged update run", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      eastStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 1),
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					// Member cluster 1 has been included in the east stage, which comes first.
					StageName:      prodStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 2),
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					StageName:      emptyStageName,
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					StageName:      restStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 3),
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)

			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")
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

	Context("clusters selected by no stage", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member cluster 2 has no env label, and is selected by none of the stages.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
			{regionLabelKey: regionLabelValueWest},
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
				Suspended:                true,
			})
		})

		It("should initialize the cluster staged update run, leaving out the clusters selected by no stage", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      canaryStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					StageName:      prodStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 1, 3),
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)

			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")
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

	Context("clusters sorted by an integer label", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// The label values are picked so that their numerical order (1, 2, 2, 10) differs from both their lexical
		// order (1, 10, 2, 2) and the order of the cluster names; member clusters 1 and 3 share the same value.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueProd, orderLabelKey: "10"},
			{envLabelKey: envLabelValueProd, orderLabelKey: "2"},
			{envLabelKey: envLabelValueProd, orderLabelKey: "1"},
			{envLabelKey: envLabelValueProd, orderLabelKey: "2"},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
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
				Suspended:                true,
			})
		})

		It("should initialize the cluster staged update run, with clusters sorted by the label value (and then by name)", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      prodStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 2, 1, 3, 0),
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)

			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")
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

	Context("clusters missing the sorting label", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)

		// Member cluster 1 does not have the sorting label.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueProd, orderLabelKey: "1"},
			{envLabelKey: envLabelValueProd},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
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

		It("should fail to initialize the cluster staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, runName, missingSortingLabelMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("clusters with a non-integer sorting label", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)

		// The sorting label value of member cluster 1 is not an integer.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 2)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueProd, orderLabelKey: "1"},
			{envLabelKey: envLabelValueProd, orderLabelKey: "first"},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
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

		It("should fail to initialize the cluster staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, runName, nonIntegerSortingLabelMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("max concurrency resolution", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member cluster 0 is in group A; member clusters 1-2 are in group B; member clusters 3-7 are in group C;
		// member clusters 8-10 are in group D.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 11)
		memberClusterGroups := []string{
			groupLabelValueA,
			groupLabelValueB, groupLabelValueB,
			groupLabelValueC, groupLabelValueC, groupLabelValueC, groupLabelValueC, groupLabelValueC,
			groupLabelValueD, groupLabelValueD, groupLabelValueD,
		}
		memberClusterLabels := make([]map[string]string, 0, len(memberClusterGroups))
		for _, group := range memberClusterGroups {
			memberClusterLabels = append(memberClusterLabels, map[string]string{groupLabelKey: group})
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, memberClusterLabels)
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					// No max concurrency is specified; the API server sets it to 1 by default.
					Name: defaultConcurrencyStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{groupLabelKey: groupLabelValueA},
					},
				},
				{
					Name: fixedConcurrencyStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{groupLabelKey: groupLabelValueB},
					},
					MaxConcurrency: ptr.To(intstr.FromInt32(3)),
				},
				{
					Name: halfConcurrencyStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{groupLabelKey: groupLabelValueC},
					},
					MaxConcurrency: ptr.To(intstr.FromString("50%")),
				},
				{
					Name: tenthConcurrencyStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{groupLabelKey: groupLabelValueD},
					},
					MaxConcurrency: ptr.To(intstr.FromString("10%")),
				},
				{
					Name:           emptyStageName,
					MaxConcurrency: ptr.To(intstr.FromString("100%")),
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the cluster staged update run, with the max concurrency of each stage resolved", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      defaultConcurrencyStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					// An integer value is used as it is, even if it exceeds the number of clusters in the stage.
					StageName:      fixedConcurrencyStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 1, 2),
					MaxConcurrency: ptr.To(int32(3)),
				},
				{
					// 50% of 5 clusters is 2.5, which is rounded down.
					StageName:      halfConcurrencyStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 3, 4, 5, 6, 7),
					MaxConcurrency: ptr.To(int32(2)),
				},
				{
					// 10% of 3 clusters is 0.3, which is rounded down to 0 and then raised to the minimum of 1.
					StageName:      tenthConcurrencyStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 8, 9, 10),
					MaxConcurrency: ptr.To(int32(1)),
				},
				{
					// 100% of no clusters is 0, which is raised to the minimum of 1.
					StageName:      emptyStageName,
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)

			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")
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

	Context("placement bindings and member clusters to leave out", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		otherPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Member clusters 0 and 1 are bound to the cluster placement policy as usual; the others should be left out:
		// * member cluster 2 is bound to another cluster placement policy;
		// * the placement binding of member cluster 3 has been marked for deletion;
		// * member cluster 4 does not exist; and
		// * member cluster 5 has been marked for deletion.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 6)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)

			fixtures := make([]client.Object, 0, 2*len(memberClusterNames)+2)
			fixtures = append(fixtures, policy)
			for idx := range memberClusterNames {
				if idx != 4 {
					fixtures = append(fixtures, createMemberCluster(memberClusterNames[idx], nil))
				}
				bindingPolicyName := policyName
				if idx == 2 {
					bindingPolicyName = otherPolicyName
				}
				fixtures = append(fixtures, createPlacementBinding(clusterScopedNS, bindingNames[idx], bindingPolicyName, memberClusterNames[idx], previousResourceSnapshotName))
			}
			markObjectForDeletion(newPlacementBindingObj(clusterScopedNS, bindingNames[3]))
			markObjectForDeletion(&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberClusterNames[5]}})

			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, strategy)...)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the cluster staged update run, leaving out the placement bindings and member clusters that do not count", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      restStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 1),
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)

			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")
		})

		AfterAll(func() {
			removeTestFinalizer(newPlacementBindingObj(clusterScopedNS, bindingNames[3]))
			removeTestFinalizer(&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberClusterNames[5]}})
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

	Context("no placement bindings that count", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		otherPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)

		// Member cluster 0 does not exist; member cluster 1 has been marked for deletion; member cluster 2 is bound
		// to another cluster placement policy.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 3)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			binding0 := createPlacementBinding(clusterScopedNS, bindingNames[0], policyName, memberClusterNames[0], previousResourceSnapshotName)
			memberCluster1 := createMemberCluster(memberClusterNames[1], nil)
			binding1 := createPlacementBinding(clusterScopedNS, bindingNames[1], policyName, memberClusterNames[1], previousResourceSnapshotName)
			memberCluster2 := createMemberCluster(memberClusterNames[2], nil)
			binding2 := createPlacementBinding(clusterScopedNS, bindingNames[2], otherPolicyName, memberClusterNames[2], previousResourceSnapshotName)
			markObjectForDeletion(&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberClusterNames[1]}})

			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(policy, binding0, memberCluster1, binding1, memberCluster2, binding2, strategy)

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should fail to initialize the cluster staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, runName, noPlacementBindingsMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		AfterAll(func() {
			removeTestFinalizer(&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberClusterNames[1]}})
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, runName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("missing cluster staged update strategy", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		triggerApprovalRequestName := fmt.Sprintf(triggerApprovalRequestNameTemplate, randStr)

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			waitForObjectsToBeCached(append(fixtures, policy)...)

			// The cluster staged update strategy has not been created yet.
			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
			})
		})

		It("should fail to initialize the cluster staged update run", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, runName, strategyNotFoundMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("should release the binding manager role of the cluster placement policy", func() {
			// The controller claims the role before it initializes the run, and releases it once the run has failed
			// to initialize.
			roleReleasedActual := bindingManagerRoleReleasedActual(clusterScopedNS, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		It("can create the cluster staged update strategy", func() {
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(strategy)
		})

		It("can trigger a new reconciliation of the cluster staged update run", func() {
			// The controller does not watch staged update strategies. The trigger leaves the run as is (in particular,
			// the run is not suspended): had the controller initialized the run now that the strategy exists, it would
			// go on to claim the binding manager role and roll out, which the check below would catch.
			triggerStagedUpdateRunReconciliation(clusterScopedNS, runName, restStageName, triggerApprovalRequestName)
		})

		It("should not initialize the cluster staged update run again", func() {
			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, runName, strategyNotFoundMsgSubstr)
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

	Context("resource snapshot selection (no resource snapshot specified)", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		firstRunName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-0")
		secondRunName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-1")
		thirdRunName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr+"-2")
		var firstSnapshotName string
		var secondSnapshotName string

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)
		wantStages := []rolloutv1alpha1.PerStageStatus{
			{
				StageName:      restStageName,
				Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
				MaxConcurrency: ptr.To(int32(1)),
			},
		}

		var policy placementv1alpha1.PlacementPolicyAccessor

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy = createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			firstSnapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			secondSnapshotName = primaryPlacementResourceSnapshotName(policy, 1)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			waitForObjectsToBeCached(append(fixtures, policy, strategy)...)
		})

		It("should create a resource snapshot when none exists", func() {
			createStagedUpdateRun(clusterScopedNS, firstRunName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})

			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(firstSnapshotName, wantStages)
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, firstRunName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")

			snapshotCreatedActual := primaryPlacementResourceSnapshotCreatedActual(policy, 0)
			Eventually(snapshotCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the cluster placement resource snapshot")
		})

		It("should reuse the latest resource snapshot when the selected resources have not changed", func() {
			createStagedUpdateRun(clusterScopedNS, secondRunName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})

			// Had a new resource snapshot been created, the run would roll out that one (of index 1) instead.
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(firstSnapshotName, wantStages)
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, secondRunName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")
		})

		It("can update the selected resources", func() {
			updateConfigMapData(configMapName, map[string]string{configMapDataKey: altConfigMapDataValue})
		})

		It("should create a new resource snapshot when the selected resources have changed", func() {
			createStagedUpdateRun(clusterScopedNS, thirdRunName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})

			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(secondSnapshotName, wantStages)
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, thirdRunName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")

			snapshotCreatedActual := primaryPlacementResourceSnapshotCreatedActual(policy, 1)
			Eventually(snapshotCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the cluster placement resource snapshot")
		})

		AfterAll(func() {
			removeObjects(slices.Concat(
				[]client.Object{
					newStagedUpdateRunObj(clusterScopedNS, firstRunName),
					newStagedUpdateRunObj(clusterScopedNS, secondRunName),
					newStagedUpdateRunObj(clusterScopedNS, thirdRunName),
					newPlacementResourceSnapshotObj(clusterScopedNS, firstSnapshotName),
					newPlacementResourceSnapshotObj(clusterScopedNS, secondSnapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("resource snapshot selection (resource snapshot specified)", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		otherPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)

		// A primary snapshot of the cluster placement policy (index 0, sub-index 0), along with its secondary snapshot
		// (index 0, sub-index 1).
		var primarySnapshotName string
		var secondarySnapshotName string
		// A primary snapshot of the cluster placement policy (index 1) that has been marked for deletion.
		var deletedSnapshotName string
		// A primary snapshot of another cluster placement policy.
		var otherPolicySnapshotName string
		// A snapshot that does not exist.
		var missingSnapshotName string

		runNames := make([]string, 0, 5)
		for idx := 0; idx < 5; idx++ {
			runNames = append(runNames, fmt.Sprintf(stagedUpdateRunNameTemplate, fmt.Sprintf("%s-%d", randStr, idx)))
		}

		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 1)

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			primarySnapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			secondarySnapshotName = secondaryPlacementResourceSnapshotName(policy, 0, 1)
			deletedSnapshotName = primaryPlacementResourceSnapshotName(policy, 1)
			missingSnapshotName = primaryPlacementResourceSnapshotName(policy, 2)
			otherPolicy := createPlacementPolicy(clusterScopedNS, otherPolicyName, configMapName)
			otherPolicySnapshotName = primaryPlacementResourceSnapshotName(otherPolicy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames, bindingNames, []map[string]string{nil})
			strategy := createStagedUpdateStrategy(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})

			primarySnapshot := createPlacementResourceSnapshot(primarySnapshotName, policy, 0, 0, 2)
			secondarySnapshot := createPlacementResourceSnapshot(secondarySnapshotName, policy, 0, 1, 2)
			deletedSnapshot := createPlacementResourceSnapshot(deletedSnapshotName, policy, 1, 0, 1)
			otherPolicySnapshot := createPlacementResourceSnapshot(otherPolicySnapshotName, otherPolicy, 0, 0, 1)
			markObjectForDeletion(deletedSnapshot)

			waitForObjectsToBeCached(append(fixtures, policy, otherPolicy, strategy, primarySnapshot, secondarySnapshot, deletedSnapshot, otherPolicySnapshot)...)
		})

		It("should initialize the cluster staged update run with the specified resource snapshot", func() {
			createStagedUpdateRun(clusterScopedNS, runNames[0], rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				ResourceSnapshotName:     primarySnapshotName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})

			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      restStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(primarySnapshotName, wantStages)
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, runNames[0], wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")
		})

		It("should fail to initialize the cluster staged update run if the specified resource snapshot does not exist", func() {
			createStagedUpdateRun(clusterScopedNS, runNames[1], rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				ResourceSnapshotName:     missingSnapshotName,
				StagedUpdateStrategyName: strategyName,
			})

			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, runNames[1], snapshotNotFoundMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("should fail to initialize the cluster staged update run if the specified resource snapshot is not a primary one", func() {
			createStagedUpdateRun(clusterScopedNS, runNames[2], rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				ResourceSnapshotName:     secondarySnapshotName,
				StagedUpdateStrategyName: strategyName,
			})

			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, runNames[2], snapshotNotPrimaryMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("should fail to initialize the cluster staged update run if the specified resource snapshot belongs to another cluster placement policy", func() {
			createStagedUpdateRun(clusterScopedNS, runNames[3], rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				ResourceSnapshotName:     otherPolicySnapshotName,
				StagedUpdateStrategyName: strategyName,
			})

			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, runNames[3], snapshotNotOwnedMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		It("should fail to initialize the cluster staged update run if the specified resource snapshot has been marked for deletion", func() {
			createStagedUpdateRun(clusterScopedNS, runNames[4], rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				ResourceSnapshotName:     deletedSnapshotName,
				StagedUpdateStrategyName: strategyName,
			})

			failedToInitializeActual := stagedUpdateRunFailedToInitializeActual(clusterScopedNS, runNames[4], snapshotDeletedMsgSubstr)
			Eventually(failedToInitializeActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the initialization failure")
		})

		AfterAll(func() {
			removeTestFinalizer(newPlacementResourceSnapshotObj(clusterScopedNS, deletedSnapshotName))

			objs := make([]client.Object, 0, len(runNames))
			for _, runName := range runNames {
				objs = append(objs, newStagedUpdateRunObj(clusterScopedNS, runName))
			}
			removeObjects(slices.Concat(
				objs,
				[]client.Object{
					newPlacementResourceSnapshotObj(clusterScopedNS, primarySnapshotName),
					newPlacementResourceSnapshotObj(clusterScopedNS, secondarySnapshotName),
					newPlacementResourceSnapshotObj(clusterScopedNS, deletedSnapshotName),
					newPlacementResourceSnapshotObj(clusterScopedNS, otherPolicySnapshotName),
					newStagedUpdateStrategyObj(clusterScopedNS, strategyName),
					newPlacementPolicyObj(clusterScopedNS, policyName),
					newPlacementPolicyObj(clusterScopedNS, otherPolicyName),
					newConfigMapObj(configMapName),
				},
				memberClusterAndBindingObjs(clusterScopedNS, memberClusterNames, bindingNames),
			)...)
		})
	})

	Context("one-shot initialization", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

		// Initially, member cluster 0 is a canary cluster, and member clusters 1 and 2 are production clusters;
		// member cluster 3 joins (with a staging label) after the cluster staged update run has been initialized.
		memberClusterNames, bindingNames := memberClusterAndBindingNames(randStr, 4)
		memberClusterLabels := []map[string]string{
			{envLabelKey: envLabelValueCanary},
			{envLabelKey: envLabelValueProd},
			{envLabelKey: envLabelValueProd},
		}

		wantStages := []rolloutv1alpha1.PerStageStatus{
			{
				StageName:      canaryStageName,
				Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0),
				MaxConcurrency: ptr.To(int32(1)),
			},
			{
				StageName:      prodStageName,
				Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 1, 2),
				MaxConcurrency: ptr.To(int32(1)),
			},
		}

		BeforeAll(func() {
			createConfigMap(configMapName)
			policy := createPlacementPolicy(clusterScopedNS, policyName, configMapName)
			// The snapshot names factor in the UID of the placement policy, which is only known once it is created.
			snapshotName = primaryPlacementResourceSnapshotName(policy, 0)
			fixtures := createMemberClustersAndBindings(clusterScopedNS, policyName, memberClusterNames[:3], bindingNames[:3], memberClusterLabels)
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

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the cluster staged update run", func() {
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")
		})

		It("can change the cluster staged update strategy, the member clusters, and the placement bindings", func() {
			// Were the cluster staged update run to be initialized again after the changes, the canary stage would include
			// member cluster 3 only, and the prod stage would include member clusters 0, 1, and 2.
			updateStagedUpdateStrategyStages(clusterScopedNS, strategyName, []rolloutv1alpha1.Stage{
				{
					Name: canaryStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueStaging},
					},
				},
				{
					Name: prodStageName,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{envLabelKey: envLabelValueProd},
					},
				},
				{
					Name:          restStageName,
					LabelSelector: &metav1.LabelSelector{},
				},
			})
			updateMemberClusterLabels(memberClusterNames[0], map[string]string{envLabelKey: envLabelValueProd})
			createMemberCluster(memberClusterNames[3], map[string]string{envLabelKey: envLabelValueStaging})
			createPlacementBinding(clusterScopedNS, bindingNames[3], policyName, memberClusterNames[3], previousResourceSnapshotName)

			// Wait until the cache observes all the changes, so that a new initialization (if any) would see them.
			Eventually(func() error {
				strategy := newStagedUpdateStrategyObj(clusterScopedNS, strategyName)
				if err := hubClient.Get(ctx, client.ObjectKeyFromObject(strategy), strategy); err != nil {
					return err
				}
				if len(strategy.GetSpec().Stages) != 3 {
					return fmt.Errorf("the cluster staged update strategy has %d stages, want 3", len(strategy.GetSpec().Stages))
				}

				memberCluster := &clusterv1beta1.MemberCluster{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: memberClusterNames[0]}, memberCluster); err != nil {
					return err
				}
				if memberCluster.Labels[envLabelKey] != envLabelValueProd {
					return fmt.Errorf("member cluster %s has env label %q, want %q", memberClusterNames[0], memberCluster.Labels[envLabelKey], envLabelValueProd)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the changes in the cache")
			waitForObjectsToBeCached(
				&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberClusterNames[3]}},
				newPlacementBindingObj(clusterScopedNS, bindingNames[3]),
			)
		})

		It("can resume the cluster staged update run", func() {
			updateStagedUpdateRunSuspended(clusterScopedNS, runName, false)
		})

		It("should mark the cluster staged update run as resumed", func() {
			resumedActual := stagedUpdateRunConditionActual(clusterScopedNS, runName, metav1.Condition{
				Type:   rolloutv1alpha1.StagedUpdateRunCondTypeSuspended,
				Status: metav1.ConditionFalse,
				Reason: rolloutv1alpha1.StagedUpdateRunSuspendedCondReasonResumed,
			})
			Eventually(resumedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to resume the cluster staged update run")
		})

		It("should start rolling out to the member cluster in the first stage as initialized", func() {
			// Member cluster 0 belongs to the canary stage only as initialized.
			bindingUpdatedActual := placementBindingResourceSnapshotActual(clusterScopedNS, bindingNames[0], snapshotName)
			Eventually(bindingUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to roll out to member cluster 0")

			// The rollout is blocked on member cluster 0 (its placement binding never becomes available); in
			// particular, member cluster 3 (which a new initialization would have put in the canary stage) is
			// never rolled out to.
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, bindingNames[1:], previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to other member clusters unexpectedly")
		})

		It("should keep the stages as initialized", func() {
			// The check runs only once the rollout has started, i.e., once the controller has processed the resumed run
			// (including its initialization step); the Resumed condition alone does not prove this, as the controller
			// sets it before it gets to the initialization step.
			stageLayoutActual := stagedUpdateRunStageLayoutActual(clusterScopedNS, runName, wantStages)
			Eventually(stageLayoutActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to keep the stages as initialized")
		})

		AfterAll(func() {
			// Note that deleting the placement bindings also unblocks the rollout (the controller fails to find
			// the binding it is waiting for), so that the cluster staged update run can be removed in time.
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

	Context("cluster staged update runs created as suspended", Ordered, func() {
		randStr := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, randStr)
		policyName := fmt.Sprintf(placementPolicyNameTemplate, randStr)
		strategyName := fmt.Sprintf(stagedUpdateStrategyNameTemplate, randStr)
		runName := fmt.Sprintf(stagedUpdateRunNameTemplate, randStr)
		var snapshotName string

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

			createStagedUpdateRun(clusterScopedNS, runName, rolloutv1alpha1.StagedUpdateRunSpec{
				PlacementPolicyName:      policyName,
				StagedUpdateStrategyName: strategyName,
				Suspended:                true,
			})
		})

		It("should initialize the cluster staged update run and mark it as suspended (without starting it)", func() {
			wantStages := []rolloutv1alpha1.PerStageStatus{
				{
					StageName:      restStageName,
					Clusters:       wantPerClusterStatuses(memberClusterNames, bindingNames, 0, 1),
					MaxConcurrency: ptr.To(int32(1)),
				},
			}
			wantStatus := wantStagedUpdateRunStatusInitializedAndSuspended(snapshotName, wantStages)
			statusUpdatedActual := stagedUpdateRunStatusUpdatedActual(clusterScopedNS, runName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to initialize the cluster staged update run")
		})

		It("should remove the cleanup finalizer from the cluster staged update run", func() {
			finalizerRemovedActual := stagedUpdateRunCleanupFinalizerRemovedActual(clusterScopedNS, runName)
			Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer")
		})

		It("should release the binding manager role of the cluster placement policy", func() {
			roleReleasedActual := bindingManagerRoleReleasedActual(clusterScopedNS, policyName)
			Eventually(roleReleasedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to release the binding manager role")
		})

		It("should not roll out to any member cluster", func() {
			bindingsUntouchedActual := placementBindingsResourceSnapshotActual(clusterScopedNS, bindingNames, previousResourceSnapshotName)
			Consistently(bindingsUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Rolled out to member clusters unexpectedly")
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
