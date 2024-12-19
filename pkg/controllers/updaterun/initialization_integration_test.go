/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package updaterun

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
)

var (
	cmpOptions = []cmp.Option{
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
		cmpopts.IgnoreFields(metav1.Condition{}, "Message"),
	}
)

var _ = Describe("Updaterun initialization tests", func() {
	var updateRun *placementv1alpha1.ClusterStagedUpdateRun
	var crp *placementv1beta1.ClusterResourcePlacement
	var policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot
	var updateStrategy *placementv1alpha1.ClusterStagedUpdateStrategy
	var resourceBindings []*placementv1beta1.ClusterResourceBinding
	var targetClusters []*clusterv1beta1.MemberCluster
	var unscheduledCluster []*clusterv1beta1.MemberCluster
	var resourceSnapshot *placementv1beta1.ClusterResourceSnapshot
	var clusterResourceOverride *placementv1alpha1.ClusterResourceOverrideSnapshot

	BeforeEach(func() {
		testUpdateRunName = "updaterun-" + utils.RandStr()
		testCRPName = "crp-" + utils.RandStr()
		testResourceSnapshotName = "snapshot-" + utils.RandStr()
		testUpdateStrategyName = "updatestrategy-" + utils.RandStr()
		testCROName = "cro-" + utils.RandStr()
		updateRunNamespacedName = types.NamespacedName{Name: testUpdateRunName}

		updateRun = generateTestClusterStagedUpdateRun()
		crp = generateTestClusterResourcePlacement()
		policySnapshot = generateTestClusterSchedulingPolicySnapshot(1)
		updateStrategy = generateTestClusterStagedUpdateStrategy()
		clusterResourceOverride = generateTestClusterResourceOverride()

		resourceBindings = make([]*placementv1beta1.ClusterResourceBinding, numTargetClusters+numUnscheduledClusters)
		targetClusters = make([]*clusterv1beta1.MemberCluster, numTargetClusters)
		for i := range targetClusters {
			// split the clusters into 2 regions
			region := "eastus"
			if i%2 == 0 {
				region = "westus"
			}
			// reserse the order of the clusters by index
			targetClusters[i] = generateTestMemberCluster(numTargetClusters-1-i, "cluster-"+strconv.Itoa(i), map[string]string{"group": "prod", "region": region})
			resourceBindings[i] = generateTestClusterResourceBinding(policySnapshot.Name, targetClusters[i].Name)
		}

		unscheduledCluster = make([]*clusterv1beta1.MemberCluster, numUnscheduledClusters)
		for i := range unscheduledCluster {
			unscheduledCluster[i] = generateTestMemberCluster(i, "unscheduled-cluster-"+strconv.Itoa(i), map[string]string{"group": "staging"})
			// update the policySnapshot name so that these clusters are considered to-be-deleted
			resourceBindings[numTargetClusters+i] = generateTestClusterResourceBinding(policySnapshot.Name+"a", unscheduledCluster[i].Name)
		}

		var err error
		testNamespace, err = json.Marshal(corev1.Namespace{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Namespace",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-namespace",
				Labels: map[string]string{
					"fleet.azure.com/name": "test-namespace",
				},
			},
		})
		Expect(err).To(Succeed())
		resourceSnapshot = generateTestClusterResourceSnapshot()

		// Set smaller wait time for testing
		stageUpdatingWaitTime = time.Second * 2
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
		for _, cluster := range unscheduledCluster {
			Expect(k8sClient.Delete(ctx, cluster)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
		targetClusters, unscheduledCluster = nil, nil

		By("Deleting the clusterStagedUpdateStrategy")
		Expect(k8sClient.Delete(ctx, updateStrategy)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		updateStrategy = nil

		By("Deleting the clusterResourceSnapshot")
		Expect(k8sClient.Delete(ctx, resourceSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		resourceSnapshot = nil

		By("Deleting the clusterResourceOverride")
		Expect(k8sClient.Delete(ctx, clusterResourceOverride)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		clusterResourceOverride = nil
	})

	Context("Test validateCRP", func() {
		It("Should fail to initialize if CRP is not found", func() {
			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "parent clusterResourcePlacement not found")
		})

		It("Should fail to initialize if CRP does not have external rollout strategy type", func() {
			By("Creating a new clusterResourcePlacement with rolling update strategy")
			crp.Spec.Strategy.Type = placementv1beta1.RollingUpdateRolloutStrategyType
			Expect(k8sClient.Create(ctx, crp)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun,
				"parent clusterResourcePlacement does not have an external rollout strategy")
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

	Context("Test determinePolicySnapshot", func() {
		BeforeEach(func() {
			By("Creating a new clusterResourcePlacement")
			Expect(k8sClient.Create(ctx, crp)).To(Succeed())
		})

		It("Should fail to initialize if the latest policy snapshot is not found", func() {
			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "no latest policy snapshot associated")
		})

		It("Should fail to initialize if there are multiple latest policy snapshots", func() {
			By("Creating 2 scheduling policy snapshots")
			snapshot2 := generateTestClusterSchedulingPolicySnapshot(2)
			Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())
			Expect(k8sClient.Create(ctx, snapshot2)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "more than one (2 in actual) latest policy snapshots associated")

			By("Deleting the second scheduling policy snapshot")
			Expect(k8sClient.Delete(ctx, snapshot2)).Should(Succeed())
		})

		It("Should fail to initialize if the latest policy snapshot has a nil policy", func() {
			By("Creating scheduling policy snapshot with nil policy")
			policySnapshot.Spec.Policy = nil
			Expect(k8sClient.Create(ctx, policySnapshot)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "does not have a policy")
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
				if updateRun.Status.PolicySnapshotIndexUsed != policySnapshot.Name {
					return fmt.Errorf("updateRun status `PolicySnapshotIndexUsed` mismatch: got %s, want %s", updateRun.Status.PolicySnapshotIndexUsed, policySnapshot.Name)
				}
				if updateRun.Status.PolicyObservedClusterCount != numberOfClustersAnnotation {
					return fmt.Errorf("updateRun status `PolicyObservedClusterCount` mismatch: got %d, want %d", updateRun.Status.PolicyObservedClusterCount, numberOfClustersAnnotation)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "failed to update the updateRun status with policy snapshot details")
		})

		It("Should copy the latest policy snapshot details to the updateRun status -- pickFixed policy", func() {
			By("Creating scheduling policy snapshot with pickFixed policy")
			policySnapshot.Spec.Policy.PlacementType = placementv1beta1.PickFixedPlacementType
			policySnapshot.Spec.Policy.ClusterNames = []string{"cluster-0", "cluster-1"}
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
				if updateRun.Status.PolicySnapshotIndexUsed != policySnapshot.Name {
					return fmt.Errorf("updateRun status `PolicySnapshotIndexUsed` mismatch: got %s, want %s", updateRun.Status.PolicySnapshotIndexUsed, policySnapshot.Name)
				}
				if updateRun.Status.PolicyObservedClusterCount != 2 {
					return fmt.Errorf("updateRun status `PolicyObservedClusterCount` mismatch: got %d, want %d", updateRun.Status.PolicyObservedClusterCount, 2)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "failed to update the updateRun status with policy snapshot details")
		})

		It("Should copy the latest policy snapshot details to the updateRun status -- pickAll policy", func() {
			By("Creating scheduling policy snapshot with pickAll policy")
			policySnapshot.Spec.Policy.PlacementType = placementv1beta1.PickAllPlacementType
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
				if updateRun.Status.PolicySnapshotIndexUsed != policySnapshot.Name {
					return fmt.Errorf("updateRun status `PolicySnapshotIndexUsed` mismatch: got %s, want %s", updateRun.Status.PolicySnapshotIndexUsed, policySnapshot.Name)
				}
				if updateRun.Status.PolicyObservedClusterCount != -1 {
					return fmt.Errorf("updateRun status `PolicyObservedClusterCount` mismatch: got %d, want %d", updateRun.Status.PolicyObservedClusterCount, -1)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "failed to update the updateRun status with policy snapshot details")
		})
	})

	Context("Test collectScheduledClusters", func() {
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

		It("Should fail to initialize if there is no selected or to-be-deleted cluster", func() {
			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "no scheduled or to-be-deleted clusterResourceBindings found")
		})

		It("Should not report error if there are only to-be-deleted clusters", func() {
			By("Creating a to-be-deleted clusterResourceBinding")
			binding := generateTestClusterResourceBinding(policySnapshot.Name+"a", "cluster-0")
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization not failed due to no selected cluster")
			// it should fail due to strategy not found
			validateFailedInitCondition(ctx, updateRun, "referenced clusterStagedUpdateStrategy not found")
		})

		It("Should fail to initialize if the bindings are not in Scheduled or Bound state", func() {
			By("Creating a not scheduled clusterResourceBinding")
			binding := generateTestClusterResourceBinding(policySnapshot.Name, "cluster-1")
			binding.Spec.State = placementv1beta1.BindingStateUnscheduled
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "state Unscheduled is not scheduled or bound")

			By("Deleting the clusterResourceBinding")
			Expect(k8sClient.Delete(ctx, binding)).Should(Succeed())
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
			for _, cluster := range unscheduledCluster {
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
			}

			By("Creating a bunch of ClusterResourceBindings")
			for _, binding := range resourceBindings {
				Expect(k8sClient.Create(ctx, binding)).To(Succeed())
			}
		})

		It("fail the initialization if the clusterStagedUpdateStrategy is not found", func() {
			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "referenced clusterStagedUpdateStrategy not found")
		})

		Context("Test computeStagedStatus", func() {
			Context("Test validateAfterStageTask", func() {
				It("Should fail to initialize if any after stage task has 2 same tasks", func() {
					By("Creating a clusterStagedUpdateStrategy with 2 same after stage tasks")
					updateStrategy.Spec.Stages[0].AfterStageTasks = []placementv1alpha1.AfterStageTask{
						{Type: placementv1alpha1.AfterStageTaskTypeTimedWait},
						{Type: placementv1alpha1.AfterStageTaskTypeTimedWait},
					}
					Expect(k8sClient.Create(ctx, updateStrategy)).To(Succeed())

					By("Creating a new clusterStagedUpdateRun")
					Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

					By("Validating the initialization failed")
					validateFailedInitCondition(ctx, updateRun, "afterStageTasks cannot have two tasks of the same type")
				})

				It("Should fail to initialize if the wait time is not valid", func() {
					By("Creating a clusterStagedUpdateStrategy with invalid wait time duration")
					updateStrategy.Spec.Stages[0].AfterStageTasks = []placementv1alpha1.AfterStageTask{
						{Type: placementv1alpha1.AfterStageTaskTypeTimedWait, WaitTime: metav1.Duration{Duration: time.Second * 0}},
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
				validateFailedInitCondition(ctx, updateRun, "some clusters are not placed in any stage")
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
				// initialization should fail.
				want.Conditions = []metav1.Condition{
					generateFalseCondition(updateRun, placementv1alpha1.StagedUpdateRunConditionInitialized),
				}

				if diff := cmp.Diff(*want, updateRun.Status, cmpOptions...); diff != "" {
					return fmt.Errorf("status mismatch: (-want +got):\n%s", diff)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "failed to validate the clusterStagedUpdateRun in the status")
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
			for _, cluster := range unscheduledCluster {
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
			}

			By("Creating a bunch of ClusterResourceBindings")
			for _, binding := range resourceBindings {
				Expect(k8sClient.Create(ctx, binding)).To(Succeed())
			}

			By("Creating a clusterStagedUpdateStrategy")
			Expect(k8sClient.Create(ctx, updateStrategy)).To(Succeed())
		})

		It("Should fail to initialize if the specified resource snapshot is not found", func() {
			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "resource snapshot `"+testResourceSnapshotName+"` not found")
		})

		It("Should fail to initialize if the specified resource snapshot is not associated with wanted CRP", func() {
			By("Creating a new resource snapshot associated with wrong CRP")
			resourceSnapshot.Labels[placementv1beta1.CRPTrackingLabel] = "not-exist-crp"
			Expect(k8sClient.Create(ctx, resourceSnapshot)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "resource snapshot `"+testResourceSnapshotName+"` is not associated with expected clusterResourcePlacement")
		})

		It("Should fail to initialize if the specified resource snapshot is not master snapshot", func() {
			By("Creating a new non-master resource snapshot")
			delete(resourceSnapshot.Annotations, string(placementv1beta1.ResourceGroupHashAnnotation))
			Expect(k8sClient.Create(ctx, resourceSnapshot)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the initialization failed")
			validateFailedInitCondition(ctx, updateRun, "resource snapshot `"+testResourceSnapshotName+"` is not a master snapshot")
		})

		It("Should put related ClusterResourceOverrides in the status", func() {
			By("Creating a new resource snapshot")
			Expect(k8sClient.Create(ctx, resourceSnapshot)).To(Succeed())

			By("Creating a new cluster resource override")
			Expect(k8sClient.Create(ctx, clusterResourceOverride)).To(Succeed())

			By("Creating a new clusterStagedUpdateRun")
			Expect(k8sClient.Create(ctx, updateRun)).To(Succeed())

			By("Validating the clusterStagedUpdateRun stats")
			want := generateSucceededInitializationStatus(crp, updateRun, policySnapshot, updateStrategy, clusterResourceOverride)
			Eventually(func() error {
				if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
					return err
				}

				if diff := cmp.Diff(*want, updateRun.Status, cmpOptions...); diff != "" {
					return fmt.Errorf("status mismatch: (-want +got):\n%s", diff)
				}

				return nil
			}, timeout, interval).Should(Succeed(), "failed to validate the clusterStagedUpdateRun initialized successfully")

			By("Validating the clusterStagedUpdateRun initialized consistently")
			Consistently(func() error {
				if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
					return err
				}
				if diff := cmp.Diff(*want, updateRun.Status, cmpOptions...); diff != "" {
					return fmt.Errorf("status mismatch: (-want +got):\n%s", diff)
				}
				return nil
			}, duration, interval).Should(Succeed(), "failed to validate the clusterStagedUpdateRun initialized consistently")
		})
	})
})

func validateFailedInitCondition(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun, message string) {
	Eventually(func() error {
		if err := k8sClient.Get(ctx, updateRunNamespacedName, updateRun); err != nil {
			return err
		}
		wantConditions := []metav1.Condition{generateFalseCondition(updateRun, placementv1alpha1.StagedUpdateRunConditionInitialized)}
		if diff := cmp.Diff(wantConditions, updateRun.Status.Conditions, cmpOptions...); diff != "" {
			return fmt.Errorf("condition mismatch: (-want +got):\n%s", diff)
		}
		initCond := meta.FindStatusCondition(updateRun.Status.Conditions, string(placementv1alpha1.StagedUpdateRunConditionInitialized))
		if !strings.Contains(initCond.Message, message) {
			return fmt.Errorf("condition message mismatch: got %s, want %s", initCond.Message, message)
		}
		return nil
	}, timeout, interval).Should(Succeed(), "failed to validate the failed initialization condition")
}

func generateSucceededInitializationStatus(
	crp *placementv1beta1.ClusterResourcePlacement,
	updateRun *placementv1alpha1.ClusterStagedUpdateRun,
	policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot,
	updateStrategy *placementv1alpha1.ClusterStagedUpdateStrategy,
	clusterResourceOverride *placementv1alpha1.ClusterResourceOverrideSnapshot,
) *placementv1alpha1.StagedUpdateRunStatus {
	return &placementv1alpha1.StagedUpdateRunStatus{
		PolicySnapshotIndexUsed:      policySnapshot.Name,
		PolicyObservedClusterCount:   numberOfClustersAnnotation,
		ApplyStrategy:                crp.Spec.Strategy.ApplyStrategy.DeepCopy(),
		StagedUpdateStrategySnapshot: &updateStrategy.Spec,
		StagesStatus: []placementv1alpha1.StageUpdatingStatus{
			{
				StageName: "stage1",
				Clusters: []placementv1alpha1.ClusterUpdatingStatus{
					{ClusterName: "cluster-9", ClusterResourceOverrideSnapshots: []string{clusterResourceOverride.Name}},
					{ClusterName: "cluster-7", ClusterResourceOverrideSnapshots: []string{clusterResourceOverride.Name}},
					{ClusterName: "cluster-5", ClusterResourceOverrideSnapshots: []string{clusterResourceOverride.Name}},
					{ClusterName: "cluster-3", ClusterResourceOverrideSnapshots: []string{clusterResourceOverride.Name}},
					{ClusterName: "cluster-1", ClusterResourceOverrideSnapshots: []string{clusterResourceOverride.Name}},
				},
				AfterStageTaskStatus: []placementv1alpha1.AfterStageTaskStatus{
					{Type: placementv1alpha1.AfterStageTaskTypeTimedWait},
				},
			},
			{
				StageName: "stage2",
				Clusters: []placementv1alpha1.ClusterUpdatingStatus{
					{ClusterName: "cluster-0"},
					{ClusterName: "cluster-2"},
					{ClusterName: "cluster-4"},
					{ClusterName: "cluster-6"},
					{ClusterName: "cluster-8"},
				},
				AfterStageTaskStatus: []placementv1alpha1.AfterStageTaskStatus{
					{
						Type:                placementv1alpha1.AfterStageTaskTypeApproval,
						ApprovalRequestName: updateRun.Name + "-stage2",
					},
				},
			},
		},
		DeletionStageStatus: &placementv1alpha1.StageUpdatingStatus{
			StageName: "kubernetes-fleet.io/deleteStage",
			Clusters: []placementv1alpha1.ClusterUpdatingStatus{
				{ClusterName: "unscheduled-cluster-0"},
				{ClusterName: "unscheduled-cluster-1"},
				{ClusterName: "unscheduled-cluster-2"},
			},
		},
		Conditions: []metav1.Condition{
			// initialization should succeed!
			generateTrueCondition(updateRun, placementv1alpha1.StagedUpdateRunConditionInitialized),
		},
	}
}
