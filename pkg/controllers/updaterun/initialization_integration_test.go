/*
Copyright 2025 The KubeFleet Authors.

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

package updaterun

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var (
	cmpOptions = []cmp.Option{
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
		cmpopts.IgnoreFields(metav1.Condition{}, "Message"),
		cmpopts.IgnoreFields(placementv1beta1.StageUpdatingStatus{}, "StartTime", "EndTime"),
	}
)

const (
	regionEastus = "eastus"
	regionWestus = "westus"
)

var _ = Describe("Updaterun initialization tests", func() {
	var updateRun *placementv1beta1.ClusterStagedUpdateRun
	var crp *placementv1beta1.ClusterResourcePlacement
	var policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot
	var updateStrategy *placementv1beta1.ClusterStagedUpdateStrategy
	var resourceBindings []*placementv1beta1.ClusterResourceBinding
	var targetClusters []*clusterv1beta1.MemberCluster
	var unscheduledClusters []*clusterv1beta1.MemberCluster
	var resourceSnapshot *placementv1beta1.ClusterResourceSnapshot
	var clusterResourceOverride *placementv1beta1.ClusterResourceOverrideSnapshot

	BeforeEach(func() {
		testUpdateRunName = "updaterun-" + utils.RandStr()
		testCRPName = "crp-" + utils.RandStr()
		testResourceSnapshotName = testCRPName + "-" + testResourceSnapshotIndex + "-snapshot"
		testUpdateStrategyName = "updatestrategy-" + utils.RandStr()
		testCROName = "cro-" + utils.RandStr()
		updateRunNamespacedName = types.NamespacedName{Name: testUpdateRunName}

		updateRun = generateTestClusterStagedUpdateRun()
		crp = generateTestClusterResourcePlacement()
		updateStrategy = generateTestClusterStagedUpdateStrategy()
		clusterResourceOverride = generateTestClusterResourceOverride()
		resourceBindings, targetClusters, unscheduledClusters = generateTestClusterResourceBindingsAndClusters(1)
		policySnapshot = generateTestClusterSchedulingPolicySnapshot(1, len(targetClusters))
		resourceSnapshot = generateTestClusterResourceSnapshot()

		// Set smaller wait time for testing
		stageUpdatingWaitTime = time.Second * 3
		clusterUpdatingWaitTime = time.Second * 2
	})

	AfterEach(func() {
		By("Deleting the clusterStagedUpdateRun")
		Expect(k8sClient.Delete(ctx, updateRun)).Should(Succeed())
		updateRun = nil

		By("Deleting the clusterResourcePlacement")
		Expect(k8sClient.Delete(ctx, crp)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		crp = nil

		By("Deleting the clusterSchedulingPolicySnapshot")
		Expect(k8sClient.Delete(ctx, policySnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		policySnapshot = nil

		By("Deleting the clusterResourceBindings")
		for _, binding := range resourceBindings {
			Expect(k8sClient.Delete(ctx, binding)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
		resourceBindings = nil

		By("Deleting the member clusters")
		for _, cluster := range targetClusters {
			Expect(k8sClient.Delete(ctx, cluster)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
		for _, cluster := range unscheduledClusters {
			Expect(k8sClient.Delete(ctx, cluster)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
		targetClusters, unscheduledClusters = nil, nil

		By("Deleting the clusterStagedUpdateStrategy")
		Expect(k8sClient.Delete(ctx, updateStrategy)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		updateStrategy = nil

		By("Deleting the clusterResourceSnapshot")
		Expect(k8sClient.Delete(ctx, resourceSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		resourceSnapshot = nil

		By("Deleting the clusterResourceOverride")
		Expect(k8sClient.Delete(ctx, clusterResourceOverride)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		clusterResourceOverride = nil

		By("Checking the update run status metrics are removed")
		// No metrics are emitted as all are removed after updateRun is deleted.
		validateUpdateRunMetricsEmitted()
		resetUpdateRunMetrics()
	})

	Context("Test validateCRP", func() {
		AfterEach(func() {
			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateInitializationFailedMetric(updateRun))
		})

		It("Should fail to initialize if CRP is not found", func() {
			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "parent placement not found")
		})

		It("Should fail to initialize if CRP does not have external rollout strategy type", func() {
			By("Creating a new clusterResourcePlacement with rolling update strategy")
			crp.Spec.Strategy.Type = placementv1beta1.RollingUpdateRolloutStrategyType
			Expect(k8sClient.Create(ctx, crp)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun,
				"parent placement does not have an external rollout strategy")
		})

		It("Should copy the ApplyStrategy in the CRP to the UpdateRun", func() {
			By("Creating a new clusterResourcePlacement")
			Expect(k8sClient.Create(ctx, crp)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the ApplyStrategy is copied")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
					return err
				}
				if diff := cmp.Diff(crp.Spec.Strategy.ApplyStrategy, updateRun.Status.ApplyStrategy, cmpOptions...); diff != "" {
					return fmt.Errorf("ApplyStrategy mismatch: (-want +got):\n%s", diff)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "failed to copy ApplyStrategy from CRP to UpdateRun")
		})
	})

	Context("Test determinePolicySnapshot for PickN", func() {
		BeforeEach(func() {
			By("Creating a new clusterResourcePlacement")
			Expect(k8sClient.Create(ctx, crp)).To(Succeed())
		})

		AfterEach(func() {
			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateInitializationFailedMetric(updateRun))
		})

		It("Should fail to initialize if the latest policy snapshot is not found", func() {
			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "no latest policy snapshot associated")
		})

		It("Should fail to initialize if there are multiple latest policy snapshots", func() {
			By("Creating 2 scheduling policy snapshots")
			snapshot2 := generateTestClusterSchedulingPolicySnapshot(2, len(targetClusters))
			Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())
			Expect(k8sClient.Create(ctx, snapshot2)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "more than one (2 in actual) latest policy snapshots associated")

			By("Deleting the second scheduling policy snapshot")
			Expect(k8sClient.Delete(ctx, snapshot2)).Should(Succeed())
		})

		It("Should fail to initialize if the latest policy snapshot does not have index label", func() {
			By("Creating scheduling policy snapshot without index label")
			delete(policySnapshot.Labels, placementv1beta1.PolicyIndexLabel)
			Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "does not have a policy index label")
		})

		It("Should fail to initialize if the latest policy snapshot does not have valid cluster count annotation", func() {
			By("Creating scheduling policy snapshot with invalid cluster count annotation")
			delete(policySnapshot.Annotations, placementv1beta1.NumberOfClustersAnnotation)
			Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "doesn't have valid cluster count annotation")
		})

		It("Should fail to initialize if the latest policy snapshot is not fully scheduled yet", func() {
			By("Creating scheduling policy snapshot without scheduled condition")
			Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "not fully scheduled yet")
		})

		It("Should copy the latest policy snapshot details to the updateRun status -- pickN policy", func() {
			By("Creating scheduling policy snapshot with pickN policy")
			Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())

			By("Setting the latest policy snapshot condition as fully scheduled")
			meta.SetStatusCondition(&policySnapshot.Status.Conditions, metav1.Condition{
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: policySnapshot.Generation,
				Reason:             "scheduled",
			})
			Expect(k8sClient.Status().Update(ctx, policySnapshot)).Should(Succeed(), "failed to update the policy snapshot condition")

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating policy snapshot details are copied to the updateRun status")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
					return err
				}
				if updateRun.Status.PolicySnapshotIndexUsed != policySnapshot.Labels[placementv1beta1.PolicyIndexLabel] {
					return fmt.Errorf("updateRun status `PolicySnapshotIndexUsed` mismatch: got %s, want %s", updateRun.Status.PolicySnapshotIndexUsed, policySnapshot.Labels[placementv1beta1.PolicyIndexLabel])
				}
				numberOfClustersAnnotation, err := strconv.Atoi(policySnapshot.Annotations[placementv1beta1.NumberOfClustersAnnotation])
				if err != nil {
					return fmt.Errorf("failed to parse number of clusters annotation from policy snapshot: %w", err)
				}
				if updateRun.Status.PolicyObservedClusterCount != numberOfClustersAnnotation {
					return fmt.Errorf("updateRun status `PolicyObservedClusterCount` mismatch: got %d, want %d", updateRun.Status.PolicyObservedClusterCount, numberOfClustersAnnotation)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "failed to update the updateRun status with policy snapshot details")
		})
	})

	Context("Test deleterminePolicySnapshot for PickFixed", func() {
		BeforeEach(func() {
			By("Creating a new clusterResourcePlacement")
			crp.Spec.Policy = &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickFixedPlacementType,
				ClusterNames:  []string{"cluster-0", "cluster-1"},
			}
			Expect(k8sClient.Create(ctx, crp)).To(Succeed())
		})

		AfterEach(func() {
			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateInitializationFailedMetric(updateRun))
		})
		It("Should copy the latest policy snapshot details to the updateRun status -- pickFixed policy", func() {
			By("Creating scheduling policy snapshot with pickFixed policy")
			policySnapshot.Spec.Policy = &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickFixedPlacementType,
				ClusterNames:  []string{"cluster-0", "cluster-1"},
			}
			Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())

			By("Setting the latest policy snapshot condition as fully scheduled")
			meta.SetStatusCondition(&policySnapshot.Status.Conditions, metav1.Condition{
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: policySnapshot.Generation,
				Reason:             "scheduled",
			})
			Expect(k8sClient.Status().Update(ctx, policySnapshot)).Should(Succeed(), "failed to update the policy snapshot condition")

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating policy snapshot details are copied to the updateRun status")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
					return err
				}
				if updateRun.Status.PolicySnapshotIndexUsed != policySnapshot.Labels[placementv1beta1.PolicyIndexLabel] {
					return fmt.Errorf("updateRun status `PolicySnapshotIndexUsed` mismatch: got %s, want %s", updateRun.Status.PolicySnapshotIndexUsed, policySnapshot.Labels[placementv1beta1.PolicyIndexLabel])
				}
				if updateRun.Status.PolicyObservedClusterCount != 2 {
					return fmt.Errorf("updateRun status `PolicyObservedClusterCount` mismatch: got %d, want %d", updateRun.Status.PolicyObservedClusterCount, 2)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "failed to update the updateRun status with policy snapshot details")
		})
	})

	Context("Test determinePolicySnapshot for PickAll", func() {
		BeforeEach(func() {
			By("Creating a new clusterResourcePlacement")
			crp.Spec.Policy = &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			}
			Expect(k8sClient.Create(ctx, crp)).To(Succeed())
		})
		AfterEach(func() {
			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateInitializationFailedMetric(updateRun))
		})

		It("Should copy the latest policy snapshot details to the updateRun status -- pickAll policy", func() {
			By("Creating scheduling policy snapshot with pickAll policy")
			policySnapshot.Spec.Policy = &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			}
			Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())

			By("Setting the latest policy snapshot condition as fully scheduled")
			meta.SetStatusCondition(&policySnapshot.Status.Conditions, metav1.Condition{
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: policySnapshot.Generation,
				Reason:             "scheduled",
			})
			Expect(k8sClient.Status().Update(ctx, policySnapshot)).Should(Succeed(), "failed to update the policy snapshot condition")

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating policy snapshot details are copied to the updateRun status")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
					return err
				}
				if updateRun.Status.PolicySnapshotIndexUsed != policySnapshot.Labels[placementv1beta1.PolicyIndexLabel] {
					return fmt.Errorf("updateRun status `PolicySnapshotIndexUsed` mismatch: got %s, want %s", updateRun.Status.PolicySnapshotIndexUsed, policySnapshot.Labels[placementv1beta1.PolicyIndexLabel])
				}
				if updateRun.Status.PolicyObservedClusterCount != -1 {
					return fmt.Errorf("updateRun status `PolicyObservedClusterCount` mismatch: got %d, want %d", updateRun.Status.PolicyObservedClusterCount, -1)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "failed to update the updateRun status with policy snapshot details")
		})
	})

	Context("Test collectScheduledClusters for PickN", func() {
		BeforeEach(func() {
			By("Creating a new clusterResourcePlacement")
			Expect(k8sClient.Create(ctx, crp)).To(Succeed())

			By("Creating scheduling policy snapshot")
			Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())

			By("Setting the latest policy snapshot condition as fully scheduled")
			meta.SetStatusCondition(&policySnapshot.Status.Conditions, metav1.Condition{
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: policySnapshot.Generation,
				Reason:             "scheduled",
			})
			Expect(k8sClient.Status().Update(ctx, policySnapshot)).Should(Succeed(), "failed to update the policy snapshot condition")
		})

		AfterEach(func() {
			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateInitializationFailedMetric(updateRun))
		})
		It("Should fail to initialize if there is no selected or to-be-deleted cluster", func() {
			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "no scheduled or to-be-deleted bindings found")
		})

		It("Should fail to initialize if the number of selected bindings does not match the observed cluster count", func() {
			By("Creating only one scheduled cluterResourceBinding")
			Expect(k8sClient.Create(ctx, resourceBindings[0])).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "the number of selected bindings 1 is not equal to the observed cluster count 10")
		})

		It("Should not fail to initialize if the bindings with latest policy snapshots are in Unscheduled state", func() {
			By("Creating a not scheduled clusterResourceBinding")
			binding := generateTestClusterResourceBinding(policySnapshot.Name, "cluster-1", placementv1beta1.BindingStateUnscheduled)
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed due to observedClusterCount mismatch, not binding state, and no selected clusters")
			validateFailedInitCondition(ctx, updateRun, "the number of selected bindings 0 is not equal to the observed cluster count 10")

			By("Deleting the clusterResourceBinding")
			Expect(k8sClient.Delete(ctx, binding)).Should(Succeed())
		})

		It("Should retry to initialize if the bindings with old policy snapshots are not in Unscheduled state", func() {
			By("Creating a scheduled clusterResourceBinding with old policy snapshot")
			binding := generateTestClusterResourceBinding(policySnapshot.Name+"a", "cluster-0", placementv1beta1.BindingStateScheduled)
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization not failed consistently")
			// Populate the cache first.
			Eventually(func() error {
				if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
					return err
				}
				return nil
			}, timeout, interval).Should(Succeed(), "failed to get the updateRun")
			Consistently(func() error {
				if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
					return err
				}
				initCond := meta.FindStatusCondition(updateRun.Status.Conditions, string(placementv1beta1.StagedUpdateRunConditionInitialized))
				if initCond != nil {
					return fmt.Errorf("got initialization condition: %v, want nil", initCond)
				}
				return nil
			}, duration, interval).Should(Succeed(), "the initialization should keep retrying, not failed")

			By("Deleting the clusterResourceBinding")
			Expect(k8sClient.Delete(ctx, binding)).Should(Succeed())
		})
	})

	Context("Test collectScheduledClusters for PickAll", func() {
		BeforeEach(func() {
			By("Creating a new clusterResourcePlacement")
			crp.Spec.Policy = &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			}
			Expect(k8sClient.Create(ctx, crp)).To(Succeed())

			By("Creating scheduling policy snapshot")
			policySnapshot.Spec.Policy = &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			}
			Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())

			By("Setting the latest policy snapshot condition as fully scheduled")
			meta.SetStatusCondition(&policySnapshot.Status.Conditions, metav1.Condition{
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: policySnapshot.Generation,
				Reason:             "scheduled",
			})
			Expect(k8sClient.Status().Update(ctx, policySnapshot)).Should(Succeed(), "failed to update the policy snapshot condition")
		})

		AfterEach(func() {
			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateInitializationFailedMetric(updateRun))
		})

		It("Should not report error if there are only to-be-deleted clusters", func() {
			By("Creating a to-be-deleted clusterResourceBinding")
			binding := generateTestClusterResourceBinding(policySnapshot.Name+"a", "cluster-0", placementv1beta1.BindingStateUnscheduled)
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization not failed due to no selected cluster")
			// it should fail due to strategy not found
			validateFailedInitCondition(ctx, updateRun, "referenced updateStrategy not found")
		})

		It("Should update the ObservedClusterCount to the number of scheduled bindings if it's pickAll policy", func() {
			By("Creating only one scheduled cluterResourceBinding")
			Expect(k8sClient.Create(ctx, resourceBindings[0])).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization not failed due to no selected cluster")
			// it should fail due to strategy not found
			validateFailedInitCondition(ctx, updateRun, "referenced updateStrategy not found")

			By("Validating the ObservedClusterCount is updated")
			Expect(updateRun.Status.PolicyObservedClusterCount).To(Equal(1), "failed to update the updateRun PolicyObservedClusterCount status")
		})
	})

	Context("Test generateStagesByStrategy", func() {
		BeforeEach(func() {
			By("Creating a new clusterResourcePlacement")
			Expect(k8sClient.Create(ctx, crp)).To(Succeed())

			By("Creating scheduling policy snapshot")
			Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())

			By("Setting the latest policy snapshot condition as fully scheduled")
			meta.SetStatusCondition(&policySnapshot.Status.Conditions, metav1.Condition{
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: policySnapshot.Generation,
				Reason:             "scheduled",
			})
			Expect(k8sClient.Status().Update(ctx, policySnapshot)).Should(Succeed(), "failed to update the policy snapshot condition")

			By("Creating the member clusters")
			for _, cluster := range targetClusters {
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
			}
			for _, cluster := range unscheduledClusters {
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
			}

			By("Creating a bunch of ClusterResourceBindings")
			for _, binding := range resourceBindings {
				Expect(k8sClient.Create(ctx, binding)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateInitializationFailedMetric(updateRun))
		})

		It("fail the initialization if the clusterStagedUpdateStrategy is not found", func() {
			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "referenced updateStrategy not found")
		})

		Context("Test computeRunStageStatus", func() {
			Context("Test validateAfterStageTask", func() {
				It("Should fail to initialize if any after stage task has 2 same tasks", func() {
					By("Creating a clusterStagedUpdateStrategy with 2 same after stage tasks")
					updateStrategy.Spec.Stages[0].AfterStageTasks = []placementv1beta1.AfterStageTask{
						{Type: placementv1beta1.AfterStageTaskTypeTimedWait, WaitTime: &metav1.Duration{Duration: time.Second * 1}},
						{Type: placementv1beta1.AfterStageTaskTypeTimedWait, WaitTime: &metav1.Duration{Duration: time.Second * 1}},
					}
					Expect(k8sClient.Create(ctx, updateStrategy)).To(Succeed())

					By("Creating a new clusterStagedUpdateRun")
					Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

					By("Validating the initialization failed")
					validateFailedInitCondition(ctx, updateRun, "afterStageTasks cannot have two tasks of the same type")
				})

				It("Should fail to initialize if the wait time is not valid", func() {
					By("Creating a clusterStagedUpdateStrategy with invalid wait time duration")
					updateStrategy.Spec.Stages[0].AfterStageTasks = []placementv1beta1.AfterStageTask{
						{Type: placementv1beta1.AfterStageTaskTypeTimedWait, WaitTime: &metav1.Duration{Duration: time.Second * 0}},
					}
					Expect(k8sClient.Create(ctx, updateStrategy)).To(Succeed())

					By("Creating a new clusterStagedUpdateRun")
					Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

					By("Validating the initialization failed")
					validateFailedInitCondition(ctx, updateRun, "has wait duration <= 0")
				})
			})

			It("Should fail to initialize if the sorting key label is invalid", func() {
				By("Creating a clusterStagedUpdateStrategy with invalid sorting key label")
				invalidKey := "not-exist-label"
				updateStrategy.Spec.Stages[0].SortingLabelKey = &invalidKey
				Expect(k8sClient.Create(ctx, updateStrategy)).To(Succeed())

				By("Creating a new clusterStagedUpdateRun")
				Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

				By("Validating the initialization failed")
				validateFailedInitCondition(ctx, updateRun, "the sorting label `not-exist-label:`")
			})

			It("Should fail to initialize if some cluster appears in multiple stages", func() {
				By("Creating a clusterStagedUpdateStrategy with overlapping stages")
				updateStrategy.Spec.Stages[1].LabelSelector = &metav1.LabelSelector{
					MatchLabels: map[string]string{"group": "prod"},
				}
				Expect(k8sClient.Create(ctx, updateStrategy)).To(Succeed())

				By("Creating a new clusterStagedUpdateRun")
				Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

				By("Validating the initialization failed")
				validateFailedInitCondition(ctx, updateRun, "appears in more than one stages")
			})

			It("Should fail to initialize if some cluster is not selected by any stage", func() {
				By("Creating a clusterStagedUpdateStrategy that does not select all scheduled clusters")
				updateStrategy.Spec.Stages = updateStrategy.Spec.Stages[:1]
				Expect(k8sClient.Create(ctx, updateStrategy)).To(Succeed())

				By("Creating a new clusterStagedUpdateRun")
				Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

				By("Validating the initialization failed")
				validateFailedInitCondition(ctx, updateRun, "some clusters are not placed in any stage, total 5, showing up to 10: cluster-0, cluster-2, cluster-4, cluster-6, cluster-8")
			})

			It("Should select all scheduled clusters if labelSelector is empty and select no clusters if labelSelector is nil", func() {
				By("Creating a clusterStagedUpdateStrategy with two stages, using empty labelSelector and nil labelSelector respectively")
				updateStrategy.Spec.Stages[0].LabelSelector = nil                     // no clusters selected
				updateStrategy.Spec.Stages[1].LabelSelector = &metav1.LabelSelector{} // all clusters selected
				Expect(k8sClient.Create(ctx, updateStrategy)).To(Succeed())

				By("Creating a new clusterStagedUpdateRun")
				Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

				By("Validating the clusterStagedUpdateRun status")
				Eventually(func() error {
					if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
						return err
					}

					want := generateSucceededInitializationStatus(crp, updateRun, policySnapshot, updateStrategy, clusterResourceOverride)
					// No clusters should be selected in the first stage.
					want.StagesStatus[0].Clusters = []placementv1beta1.ClusterUpdatingStatus{}
					// All clusters should be selected in the second stage and sorted by name.
					want.StagesStatus[1].Clusters = []placementv1beta1.ClusterUpdatingStatus{
						{ClusterName: "cluster-0"},
						{ClusterName: "cluster-1"},
						{ClusterName: "cluster-2"},
						{ClusterName: "cluster-3"},
						{ClusterName: "cluster-4"},
						{ClusterName: "cluster-5"},
						{ClusterName: "cluster-6"},
						{ClusterName: "cluster-7"},
						{ClusterName: "cluster-8"},
						{ClusterName: "cluster-9"},
					}
					// initialization should fail due to resourceSnapshot not found.
					want.Conditions = []metav1.Condition{
						generateFalseCondition(updateRun, placementv1beta1.StagedUpdateRunConditionInitialized),
					}

					if diff := cmp.Diff(*want, updateRun.Status, cmpOptions...); diff != "" {
						return fmt.Errorf("status mismatch: (-want +got):\n%s", diff)
					}
					return nil
				}, timeout, interval).Should(Succeed(), "failed to validate the clusterStagedUpdateRun status")
			})
		})

		It("Should generate the cluster update stage in the status as expected", func() {
			By("Creating a clusterStagedUpdateStrategy")
			Expect(k8sClient.Create(ctx, updateStrategy)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the clusterStagedUpdateRun status")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
					return err
				}

				want := generateSucceededInitializationStatus(crp, updateRun, policySnapshot, updateStrategy, clusterResourceOverride)
				for i := range want.StagesStatus[0].Clusters {
					// Remove the CROs, as they are not added in this test.
					want.StagesStatus[0].Clusters[i].ClusterResourceOverrideSnapshots = nil
				}
				// initialization should fail due to resourceSnapshot not found.
				want.Conditions = []metav1.Condition{
					generateFalseCondition(updateRun, placementv1beta1.StagedUpdateRunConditionInitialized),
				}

				if diff := cmp.Diff(*want, updateRun.Status, cmpOptions...); diff != "" {
					return fmt.Errorf("status mismatch: (-want +got):\n%s", diff)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "failed to validate the clusterStagedUpdateRun status")
		})
	})

	Context("Test recordOverrideSnapshots", func() {
		BeforeEach(func() {
			By("Creating a new clusterResourcePlacement")
			Expect(k8sClient.Create(ctx, crp)).To(Succeed())

			By("Creating scheduling policy snapshot")
			Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())

			By("Setting the latest policy snapshot condition as fully scheduled")
			meta.SetStatusCondition(&policySnapshot.Status.Conditions, metav1.Condition{
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: policySnapshot.Generation,
				Reason:             "scheduled",
			})
			Expect(k8sClient.Status().Update(ctx, policySnapshot)).Should(Succeed(), "failed to update the policy snapshot condition")

			By("Creating the member clusters")
			for _, cluster := range targetClusters {
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
			}
			for _, cluster := range unscheduledClusters {
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
			}

			By("Creating a bunch of ClusterResourceBindings")
			for _, binding := range resourceBindings {
				Expect(k8sClient.Create(ctx, binding)).To(Succeed())
			}

			By("Creating a clusterStagedUpdateStrategy")
			Expect(k8sClient.Create(ctx, updateStrategy)).To(Succeed())
		})

		It("Should fail to initialize if the specified resource snapshot index is invalid - not integer", func() {
			By("Creating a new clusterStagedUpdateRun with invalid resource snapshot index")
			updateRun.Spec.ResourceSnapshotIndex = "invalid-index"
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "invalid resource snapshot index `invalid-index` provided")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateInitializationFailedMetric(updateRun))
		})

		It("Should fail to initialize if the specified resource snapshot index is invalid - negative integer", func() {
			By("Creating a new clusterStagedUpdateRun with invalid resource snapshot index")
			updateRun.Spec.ResourceSnapshotIndex = "-1"
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "invalid resource snapshot index `-1` provided")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateInitializationFailedMetric(updateRun))
		})

		It("Should fail to initialize if the specified resource snapshot is not found - no resourceSnapshots at all", func() {
			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "no resourceSnapshots with index `0` found")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateInitializationFailedMetric(updateRun))
		})

		It("Should fail to initialize if the specified resource snapshot is not found - no CRP label found", func() {
			By("Creating a new resource snapshot associated with another CRP")
			resourceSnapshot.Labels[placementv1beta1.PlacementTrackingLabel] = "not-exist-crp"
			Expect(k8sClient.Create(ctx, resourceSnapshot)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "no resourceSnapshots with index `0` found")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateInitializationFailedMetric(updateRun))
		})

		It("Should fail to initialize if the specified resource snapshot is not found - no resource index label found", func() {
			By("Creating a new resource snapshot with a different index label")
			resourceSnapshot.Labels[placementv1beta1.ResourceIndexLabel] = testResourceSnapshotIndex + "1"
			Expect(k8sClient.Create(ctx, resourceSnapshot)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "no resourceSnapshots with index `0` found")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateInitializationFailedMetric(updateRun))
		})

		It("Should fail to initialize if the specified resource snapshot is not master snapshot", func() {
			By("Creating a new non-master resource snapshot")
			delete(resourceSnapshot.Annotations, string(placementv1beta1.ResourceGroupHashAnnotation))
			Expect(k8sClient.Create(ctx, resourceSnapshot)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "no master resourceSnapshot found for placement")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateInitializationFailedMetric(updateRun))
		})

		It("Should put related ClusterResourceOverrides in the status", func() {
			By("Creating a new resource snapshot")
			Expect(k8sClient.Create(ctx, resourceSnapshot)).To(Succeed())

			By("Creating a new cluster resource override")
			Expect(k8sClient.Create(ctx, clusterResourceOverride)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the clusterStagedUpdateRun stats")
			initialized := generateSucceededInitializationStatus(crp, updateRun, policySnapshot, updateStrategy, clusterResourceOverride)
			want := generateExecutionStartedStatus(updateRun, initialized)
			validateClusterStagedUpdateRunStatus(ctx, updateRun, want, "")

			By("Validating the clusterStagedUpdateRun initialized consistently")
			validateClusterStagedUpdateRunStatusConsistently(ctx, updateRun, want, "")

			By("Checking update run status metrics are emitted")
			validateUpdateRunMetricsEmitted(generateProgressingMetric(updateRun))
		})
	})
})

func validateFailedInitCondition(ctx context.Context, updateRun *placementv1beta1.ClusterStagedUpdateRun, message string) {
	Eventually(func() error {
		if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
			return err
		}
		wantConditions := []metav1.Condition{generateFalseCondition(updateRun, placementv1beta1.StagedUpdateRunConditionInitialized)}
		if diff := cmp.Diff(wantConditions, updateRun.Status.Conditions, cmpOptions...); diff != "" {
			return fmt.Errorf("condition mismatch: (-want +got):\n%s", diff)
		}
		initCond := meta.FindStatusCondition(updateRun.Status.Conditions, string(placementv1beta1.StagedUpdateRunConditionInitialized))
		if !strings.Contains(initCond.Message, message) {
			return fmt.Errorf("condition message mismatch: got %s, want %s", initCond.Message, message)
		}
		return nil
	}, timeout, interval).Should(Succeed(), "failed to validate the failed initialization condition")
}

func generateSucceededInitializationStatus(
	crp *placementv1beta1.ClusterResourcePlacement,
	updateRun *placementv1beta1.ClusterStagedUpdateRun,
	policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot,
	updateStrategy *placementv1beta1.ClusterStagedUpdateStrategy,
	clusterResourceOverride *placementv1beta1.ClusterResourceOverrideSnapshot,
) *placementv1beta1.UpdateRunStatus {
	status := &placementv1beta1.UpdateRunStatus{
		PolicySnapshotIndexUsed:    policySnapshot.Labels[placementv1beta1.PolicyIndexLabel],
		PolicyObservedClusterCount: 10,
		ApplyStrategy:              crp.Spec.Strategy.ApplyStrategy.DeepCopy(),
		UpdateStrategySnapshot:     &updateStrategy.Spec,
		StagesStatus: []placementv1beta1.StageUpdatingStatus{
			{
				StageName: "stage1",
				Clusters: []placementv1beta1.ClusterUpdatingStatus{
					{ClusterName: "cluster-9", ClusterResourceOverrideSnapshots: []string{clusterResourceOverride.Name}},
					{ClusterName: "cluster-7", ClusterResourceOverrideSnapshots: []string{clusterResourceOverride.Name}},
					{ClusterName: "cluster-5", ClusterResourceOverrideSnapshots: []string{clusterResourceOverride.Name}},
					{ClusterName: "cluster-3", ClusterResourceOverrideSnapshots: []string{clusterResourceOverride.Name}},
					{ClusterName: "cluster-1", ClusterResourceOverrideSnapshots: []string{clusterResourceOverride.Name}},
				},
			},
			{
				StageName: "stage2",
				Clusters: []placementv1beta1.ClusterUpdatingStatus{
					{ClusterName: "cluster-0"},
					{ClusterName: "cluster-2"},
					{ClusterName: "cluster-4"},
					{ClusterName: "cluster-6"},
					{ClusterName: "cluster-8"},
				},
			},
		},
		DeletionStageStatus: &placementv1beta1.StageUpdatingStatus{
			StageName: "kubernetes-fleet.io/deleteStage",
			Clusters: []placementv1beta1.ClusterUpdatingStatus{
				{ClusterName: "unscheduled-cluster-0"},
				{ClusterName: "unscheduled-cluster-1"},
				{ClusterName: "unscheduled-cluster-2"},
			},
		},
		Conditions: []metav1.Condition{
			// initialization should succeed!
			generateTrueCondition(updateRun, placementv1beta1.StagedUpdateRunConditionInitialized),
		},
	}
	for i := range status.StagesStatus {
		var tasks []placementv1beta1.AfterStageTaskStatus
		for _, task := range updateStrategy.Spec.Stages[i].AfterStageTasks {
			taskStatus := placementv1beta1.AfterStageTaskStatus{Type: task.Type}
			if task.Type == placementv1beta1.AfterStageTaskTypeApproval {
				taskStatus.ApprovalRequestName = updateRun.Name + "-" + status.StagesStatus[i].StageName
			}
			tasks = append(tasks, taskStatus)
		}
		status.StagesStatus[i].AfterStageTaskStatus = tasks
	}
	return status
}

func generateSucceededInitializationStatusForSmallClusters(
	crp *placementv1beta1.ClusterResourcePlacement,
	updateRun *placementv1beta1.ClusterStagedUpdateRun,
	policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot,
	updateStrategy *placementv1beta1.ClusterStagedUpdateStrategy,
) *placementv1beta1.UpdateRunStatus {
	status := &placementv1beta1.UpdateRunStatus{
		PolicySnapshotIndexUsed:    policySnapshot.Labels[placementv1beta1.PolicyIndexLabel],
		PolicyObservedClusterCount: 3,
		ApplyStrategy:              crp.Spec.Strategy.ApplyStrategy.DeepCopy(),
		UpdateStrategySnapshot:     &updateStrategy.Spec,
		StagesStatus: []placementv1beta1.StageUpdatingStatus{
			{
				StageName: "stage1",
				Clusters: []placementv1beta1.ClusterUpdatingStatus{
					{ClusterName: "cluster-0"},
					{ClusterName: "cluster-1"},
					{ClusterName: "cluster-2"},
				},
			},
		},
		DeletionStageStatus: &placementv1beta1.StageUpdatingStatus{
			StageName: "kubernetes-fleet.io/deleteStage",
			Clusters:  []placementv1beta1.ClusterUpdatingStatus{},
		},
		Conditions: []metav1.Condition{
			// initialization should succeed!
			generateTrueCondition(updateRun, placementv1beta1.StagedUpdateRunConditionInitialized),
		},
	}
	for i := range status.StagesStatus {
		var tasks []placementv1beta1.AfterStageTaskStatus
		for _, task := range updateStrategy.Spec.Stages[i].AfterStageTasks {
			taskStatus := placementv1beta1.AfterStageTaskStatus{Type: task.Type}
			if task.Type == placementv1beta1.AfterStageTaskTypeApproval {
				taskStatus.ApprovalRequestName = updateRun.Name + "-" + status.StagesStatus[i].StageName
			}
			tasks = append(tasks, taskStatus)
		}
		status.StagesStatus[i].AfterStageTaskStatus = tasks
	}
	return status
}

func generateExecutionStartedStatus(
	updateRun *placementv1beta1.ClusterStagedUpdateRun,
	initialized *placementv1beta1.UpdateRunStatus,
) *placementv1beta1.UpdateRunStatus {
	// Mark updateRun execution has started.
	initialized.Conditions = append(initialized.Conditions, generateTrueCondition(updateRun, placementv1beta1.StagedUpdateRunConditionProgressing))
	// Mark updateRun 1st stage has started.
	initialized.StagesStatus[0].Conditions = append(initialized.StagesStatus[0].Conditions, generateTrueCondition(updateRun, placementv1beta1.StageUpdatingConditionProgressing))
	// Mark updateRun 1st cluster in the 1st stage has started.
	initialized.StagesStatus[0].Clusters[0].Conditions = []metav1.Condition{generateTrueCondition(updateRun, placementv1beta1.ClusterUpdatingConditionStarted)}
	return initialized
}
