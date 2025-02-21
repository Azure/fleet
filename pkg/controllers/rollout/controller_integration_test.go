/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package rollout

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/workapplier"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
)

const (
	timeout                = time.Second * 5
	interval               = time.Millisecond * 250
	consistentTimeout      = time.Second * 60
	consistentInterval     = time.Second * 5
	customBindingFinalizer = "custom-binding-finalizer"
)

var (
	ignoreCRBTypeMetaAndStatusFields = cmpopts.IgnoreFields(fleetv1beta1.ClusterResourceBinding{}, "TypeMeta", "Status")
	ignoreObjectMetaAutoGenFields    = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "CreationTimestamp", "Generation", "ResourceVersion", "SelfLink", "UID", "ManagedFields")
	ignoreCondLTTAndMessageFields    = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message")
)

var testCRPName string

var _ = Describe("Test the rollout Controller", func() {

	var bindings []*fleetv1beta1.ClusterResourceBinding
	var resourceSnapshots []*fleetv1beta1.ClusterResourceSnapshot
	var rolloutCRP *fleetv1beta1.ClusterResourcePlacement

	BeforeEach(func() {
		testCRPName = "crp" + utils.RandStr()
		bindings = make([]*fleetv1beta1.ClusterResourceBinding, 0)
		resourceSnapshots = make([]*fleetv1beta1.ClusterResourceSnapshot, 0)
	})

	AfterEach(func() {
		By("Deleting ClusterResourceBindings")
		for _, binding := range bindings {
			Expect(k8sClient.Delete(ctx, binding)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
		bindings = nil
		By("Deleting ClusterResourceSnapshots")
		for _, resourceSnapshot := range resourceSnapshots {
			Expect(k8sClient.Delete(ctx, resourceSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
		resourceSnapshots = nil
		By("Deleting ClusterResourcePlacement")
		Expect(k8sClient.Delete(ctx, rolloutCRP)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
	})

	It("Should rollout all the selected bindings as soon as they are created", func() {
		// create CRP
		var targetCluster int32 = 10
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(fleetv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// check that all bindings are bound
		Eventually(func() bool {
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State != fleetv1beta1.BindingStateBound || binding.Spec.ResourceSnapshotName != masterSnapshot.Name {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")
	})

	It("should push apply strategy changes to all the bindings (if applicable) and refresh their status", func() {
		// Create a CRP.
		targetClusterCount := int32(3)
		rolloutCRP = clusterResourcePlacementForTest(
			testCRPName,
			createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, targetClusterCount),
			createPlacementRolloutStrategyForTest(fleetv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed(), "Failed to create CRP")

		// Create a master cluster resource snapshot.
		resourceSnapshot := generateResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, resourceSnapshot)).Should(Succeed(), "Failed to create cluster resource snapshot")

		// Create all the bindings.
		clusters := make([]string, targetClusterCount)
		for i := 0; i < int(targetClusterCount); i++ {
			clusters[i] = "cluster-" + utils.RandStr()

			// Prepare bindings of various states.
			var binding *fleetv1beta1.ClusterResourceBinding
			switch {
			case i%3 == 0:
				binding = generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, resourceSnapshot.Name, clusters[i])
			case i%3 == 1:
				binding = generateClusterResourceBinding(fleetv1beta1.BindingStateBound, resourceSnapshot.Name, clusters[i])
			default:
				binding = generateClusterResourceBinding(fleetv1beta1.BindingStateUnscheduled, resourceSnapshot.Name, clusters[i])
			}
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed(), "Failed to create cluster resource binding")
			bindings = append(bindings, binding)
		}

		// Verify that all the bindings are updated per rollout strategy.
		Eventually(func() error {
			for _, binding := range bindings {
				gotBinding := &fleetv1beta1.ClusterResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, gotBinding); err != nil {
					return fmt.Errorf("failed to get binding %s: %w", binding.Name, err)
				}

				wantBinding := &fleetv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: binding.Name,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:         binding.Spec.State,
						TargetCluster: binding.Spec.TargetCluster,
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							ComparisonOption: fleetv1beta1.ComparisonOptionTypePartialComparison,
							WhenToApply:      fleetv1beta1.WhenToApplyTypeAlways,
							WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeAlways,
							Type:             fleetv1beta1.ApplyStrategyTypeClientSideApply,
						},
					},
				}
				// The bound binding will have no changes; the scheduled binding, per given
				// rollout strategy, will be bound with the resource snapshot.
				if binding.Spec.State == fleetv1beta1.BindingStateBound || binding.Spec.State == fleetv1beta1.BindingStateScheduled {
					wantBinding.Spec.State = fleetv1beta1.BindingStateBound
					wantBinding.Spec.ResourceSnapshotName = resourceSnapshot.Name
				}
				if diff := cmp.Diff(
					gotBinding, wantBinding,
					ignoreCRBTypeMetaAndStatusFields, ignoreObjectMetaAutoGenFields,
					// For this spec, labels and annotations are irrelevant.
					cmpopts.IgnoreFields(metav1.ObjectMeta{}, "Labels", "Annotations"),
				); diff != "" {
					return fmt.Errorf("binding diff (-got, +want):\n%s", diff)
				}
			}
			return nil
		}, timeout, interval).Should(Succeed(), "Failed to verify that all the bindings are bound")

		// Verify that all bindings have their status refreshed (i.e., have fresh RolloutStarted
		// conditions).
		Eventually(func() error {
			for _, binding := range bindings {
				gotBinding := &fleetv1beta1.ClusterResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, gotBinding); err != nil {
					return fmt.Errorf("failed to get binding %s: %w", binding.Name, err)
				}

				wantBindingStatus := &fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Status:             metav1.ConditionTrue,
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: gotBinding.Generation,
						},
					},
				}
				// The scheduled binding will be set to the Bound state with the RolloutStarted
				// condition set to True; the unscheduled binding will receive a False
				// RolloutStarted condition; the bound bindings will receive a True RolloutStarted
				// condition.
				if binding.Spec.State == fleetv1beta1.BindingStateUnscheduled {
					wantBindingStatus = &fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionFalse,
								Reason:             condition.RolloutNotStartedYetReason,
								ObservedGeneration: gotBinding.Generation,
							},
						},
					}
				}
				if diff := cmp.Diff(
					&gotBinding.Status, wantBindingStatus,
					ignoreCondLTTAndMessageFields,
				); diff != "" {
					return fmt.Errorf("binding status diff (%v/%v) (-got, +want):\n%s", binding.Spec.State, gotBinding.Spec.State, diff)
				}
			}
			return nil
		}, timeout, interval).Should(Succeed(), "Failed to verify that all the bindings have their status refreshed")

		// Update the CRP with a new apply strategy.
		rolloutCRP.Spec.Strategy.ApplyStrategy = &fleetv1beta1.ApplyStrategy{
			ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
			WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
			WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeIfNoDiff,
			Type:             fleetv1beta1.ApplyStrategyTypeServerSideApply,
			ServerSideApplyConfig: &fleetv1beta1.ServerSideApplyConfig{
				ForceConflicts: true,
			},
		}
		Expect(k8sClient.Update(ctx, rolloutCRP)).Should(Succeed(), "Failed to update CRP")

		// Verify that all the bindings are updated with the new apply strategy.
		Eventually(func() error {
			for _, binding := range bindings {
				gotBinding := &fleetv1beta1.ClusterResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, gotBinding); err != nil {
					return fmt.Errorf("failed to get binding %s: %w", binding.Name, err)
				}

				wantBinding := &fleetv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: binding.Name,
					},
					Spec: fleetv1beta1.ResourceBindingSpec{
						State:         binding.Spec.State,
						TargetCluster: binding.Spec.TargetCluster,
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
							WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
							WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeIfNoDiff,
							Type:             fleetv1beta1.ApplyStrategyTypeServerSideApply,
							ServerSideApplyConfig: &fleetv1beta1.ServerSideApplyConfig{
								ForceConflicts: true,
							},
						},
					},
				}
				// The bound binding will have no changes; the scheduled binding, per given
				// rollout strategy, will be bound with the resource snapshot.
				if binding.Spec.State == fleetv1beta1.BindingStateBound || binding.Spec.State == fleetv1beta1.BindingStateScheduled {
					wantBinding.Spec.State = fleetv1beta1.BindingStateBound
					wantBinding.Spec.ResourceSnapshotName = resourceSnapshot.Name
				}
				if diff := cmp.Diff(
					gotBinding, wantBinding,
					ignoreCRBTypeMetaAndStatusFields, ignoreObjectMetaAutoGenFields,
					// For this spec, labels and annotations are irrelevant.
					cmpopts.IgnoreFields(metav1.ObjectMeta{}, "Labels", "Annotations"),
				); diff != "" {
					return fmt.Errorf("binding diff (-got, +want):\n%s", diff)
				}
			}
			return nil
		}, timeout, interval).Should(Succeed(), "Failed to update all bindings with the new apply strategy")

		// Verify that all bindings have their status refreshed (i.e., have fresh RolloutStarted
		// conditions).
		Eventually(func() error {
			for _, binding := range bindings {
				gotBinding := &fleetv1beta1.ClusterResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, gotBinding); err != nil {
					return fmt.Errorf("failed to get binding %s: %w", binding.Name, err)
				}

				wantBindingStatus := &fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Status:             metav1.ConditionTrue,
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: gotBinding.Generation,
						},
					},
				}
				// The scheduled binding will be set to the Bound state with the RolloutStarted
				// condition set to True; the unscheduled binding will receive a False
				// RolloutStarted condition; the bound bindings will receive a True RolloutStarted
				// condition.
				if binding.Spec.State == fleetv1beta1.BindingStateUnscheduled {
					wantBindingStatus = &fleetv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
								Status:             metav1.ConditionFalse,
								Reason:             condition.RolloutNotStartedYetReason,
								ObservedGeneration: gotBinding.Generation,
							},
						},
					}
				}
				if diff := cmp.Diff(
					&gotBinding.Status, wantBindingStatus,
					ignoreCondLTTAndMessageFields,
				); diff != "" {
					return fmt.Errorf("binding status diff (%v/%v) (-got, +want):\n%s", binding.Spec.State, gotBinding.Spec.State, diff)
				}
			}
			return nil
		}, timeout, interval).Should(Succeed(), "Failed to verify that all the bindings have their status refreshed")
	})

	It("Should rollout all the selected bindings when the rollout strategy is not set", func() {
		// create CRP
		var targetCluster int32 = 11
		// rolloutStrategy not set.
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, targetCluster),
			fleetv1beta1.RolloutStrategy{})
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// check that all bindings are scheduled
		Eventually(func() bool {
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State != fleetv1beta1.BindingStateBound || binding.Spec.ResourceSnapshotName != masterSnapshot.Name {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")
	})

	It("Should rollout the selected and unselected bindings (not trackable resources)", func() {
		// create CRP
		var initTargetClusterNum int32 = 11
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, initTargetClusterNum),
			createPlacementRolloutStrategyForTest(fleetv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, initTargetClusterNum)
		for i := 0; i < int(initTargetClusterNum); i++ {
			clusters[i] = "cluster-" + strconv.Itoa(i)
			binding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// check that all bindings are scheduled
		Eventually(func() bool {
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State != fleetv1beta1.BindingStateBound || binding.Spec.ResourceSnapshotName != masterSnapshot.Name {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")
		// simulate that some of the bindings are available and not trackable.
		firstApplied := 3
		for i := 0; i < firstApplied; i++ {
			markBindingAvailable(bindings[i], false)
		}
		// simulate another scheduling decision, pick some cluster to unselect from the bottom of the list
		var newTargetClusterNum int32 = 9
		rolloutCRP.Spec.Policy.NumberOfClusters = &newTargetClusterNum
		Expect(k8sClient.Update(ctx, rolloutCRP)).Should(Succeed())
		secondRoundBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0)
		deletedBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0)
		stillScheduledClusterNum := 6 // the amount of clusters that are still scheduled in first round
		// simulate that some of the bindings are available
		// moved to before being set to unscheduled, otherwise, the rollout controller will try to delete the bindings before we mark them as available.
		for i := int(newTargetClusterNum); i < int(initTargetClusterNum); i++ {
			markBindingAvailable(bindings[i], false)
		}
		for i := int(initTargetClusterNum - 1); i >= stillScheduledClusterNum; i-- {
			binding := bindings[i]
			binding.Spec.State = fleetv1beta1.BindingStateUnscheduled
			Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding `%s` is marked as not scheduled", binding.Name))
			deletedBindings = append(deletedBindings, binding)
		}
		for i := 0; i < stillScheduledClusterNum; i++ {
			secondRoundBindings = append(secondRoundBindings, bindings[i])
		}
		// simulate that some of the bindings are available and not trackable
		for i := firstApplied; i < int(newTargetClusterNum); i++ {
			markBindingAvailable(bindings[i], false)
		}
		newlyScheduledClusterNum := int(newTargetClusterNum) - stillScheduledClusterNum
		for i := 0; i < newlyScheduledClusterNum; i++ {
			binding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, "cluster-"+strconv.Itoa(int(initTargetClusterNum)+i))
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			bindings = append(bindings, binding)
			secondRoundBindings = append(secondRoundBindings, binding)
		}
		// check that the second round of bindings are scheduled
		Eventually(func() bool {
			for _, binding := range secondRoundBindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State != fleetv1beta1.BindingStateBound || binding.Spec.ResourceSnapshotName != masterSnapshot.Name {
					return false
				}
			}
			return true
		}, 3*defaultUnavailablePeriod*time.Second, interval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")
		// simulate that the new bindings are available and not trackable
		for i := 0; i < len(secondRoundBindings); i++ {
			markBindingAvailable(secondRoundBindings[i], false)
		}
		// check that the unselected bindings are deleted after 3 times of the default unavailable period
		Eventually(func() bool {
			for _, binding := range deletedBindings {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding); err != nil && !apierrors.IsNotFound(err) {
					return false
				}
			}
			return true
		}, 3*defaultUnavailablePeriod*time.Second, interval).Should(BeTrue(), "rollout controller should delete all the unselected bindings")
	})

	It("Should rollout both the new scheduling and the new resources (trackable)", func() {
		// create CRP
		var targetCluster int32 = 11
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(fleetv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + strconv.Itoa(i)
			binding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// check that all bindings are scheduled
		Eventually(func() bool {
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State != fleetv1beta1.BindingStateBound {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")
		// simulate that some of the bindings are available
		firstApplied := 3
		for i := 0; i < firstApplied; i++ {
			markBindingAvailable(bindings[i], true)
		}
		// simulate another scheduling decision, pick some cluster to unselect from the bottom of the list
		var newTarget int32 = 9
		rolloutCRP.Spec.Policy.NumberOfClusters = &newTarget
		Expect(k8sClient.Update(ctx, rolloutCRP)).Should(Succeed())
		secondRoundBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0)
		deletedBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0)
		stillScheduled := 6
		// simulate that some of the bindings are applied
		// moved to before being set to unscheduled, otherwise, the rollout controller will try to delete the bindings before we mark them as available.
		for i := int(newTarget); i < int(targetCluster); i++ {
			markBindingAvailable(bindings[i], true)
		}
		for i := int(targetCluster - 1); i >= stillScheduled; i-- {
			binding := bindings[i]
			binding.Spec.State = fleetv1beta1.BindingStateUnscheduled
			Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding `%s` is marked as not scheduled", binding.Name))
			deletedBindings = append(deletedBindings, binding)
		}
		// save the bindings that are still scheduled
		for i := 0; i < stillScheduled; i++ {
			secondRoundBindings = append(secondRoundBindings, bindings[i])
		}
		// simulate that some of the bindings are available
		for i := firstApplied; i < int(newTarget); i++ {
			markBindingAvailable(bindings[i], true)
		}
		// create the newly scheduled bindings
		newScheduled := int(newTarget) - stillScheduled
		for i := 0; i < newScheduled; i++ {
			binding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, "cluster-"+strconv.Itoa(int(targetCluster)+i))
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			bindings = append(bindings, binding)
			secondRoundBindings = append(secondRoundBindings, binding)
		}
		// mark the master snapshot as not latest
		masterSnapshot.SetLabels(map[string]string{
			fleetv1beta1.CRPTrackingLabel:      testCRPName,
			fleetv1beta1.IsLatestSnapshotLabel: "false"},
		)
		Expect(k8sClient.Update(ctx, masterSnapshot)).Should(Succeed())
		// create a new master resource snapshot
		newMasterSnapshot := generateResourceSnapshot(rolloutCRP.Name, 1, true)
		Expect(k8sClient.Create(ctx, newMasterSnapshot)).Should(Succeed())
		// check that the second round of bindings are scheduled
		Eventually(func() bool {
			for _, binding := range secondRoundBindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State != fleetv1beta1.BindingStateBound {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")
		// simulate that the new bindings are available
		for i := 0; i < len(secondRoundBindings); i++ {
			markBindingAvailable(secondRoundBindings[i], true)
		}
		// check that the unselected bindings are deleted
		Eventually(func() bool {
			for _, binding := range deletedBindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if !apierrors.IsNotFound(err) {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue(), "rollout controller should delete all the unselected bindings")
		// check that the second round of bindings are also moved to use the latest resource snapshot
		Eventually(func() bool {
			misMatch := true
			for _, binding := range secondRoundBindings {
				misMatch = false
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.ResourceSnapshotName == newMasterSnapshot.Name {
					// simulate the work generator to make the newly updated bindings to be available
					markBindingAvailable(binding, true)
				} else {
					misMatch = true
				}
			}
			return !misMatch
		}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to use the latest resource snapshot")
	})

	It("Should wait for deleting binding delete before we rollout", func() {
		// create CRP
		var targetCluster int32 = 5
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(fleetv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		latestSnapshot := generateResourceSnapshot(rolloutCRP.Name, 1, true)
		Expect(k8sClient.Create(ctx, latestSnapshot)).Should(Succeed())
		By(fmt.Sprintf("resource snapshot %s created", latestSnapshot.Name))
		// generate scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, latestSnapshot.Name, clusters[i])
			bindings = append(bindings, binding)
		}
		// create two unscheduled bindings and delete them
		firstDeleteBinding := generateClusterResourceBinding(fleetv1beta1.BindingStateUnscheduled, latestSnapshot.Name, clusters[0])
		firstDeleteBinding.Name = "delete-" + firstDeleteBinding.Name
		firstDeleteBinding.SetFinalizers([]string{customBindingFinalizer})
		Expect(k8sClient.Create(ctx, firstDeleteBinding)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, firstDeleteBinding)).Should(Succeed())
		secondDeleteBinding := generateClusterResourceBinding(fleetv1beta1.BindingStateUnscheduled, latestSnapshot.Name, clusters[2])
		secondDeleteBinding.Name = "delete-" + secondDeleteBinding.Name
		secondDeleteBinding.SetFinalizers([]string{customBindingFinalizer})
		Expect(k8sClient.Create(ctx, secondDeleteBinding)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, secondDeleteBinding)).Should(Succeed())
		By("Created 2 deleting bindings")
		// create the normal binding after the deleting one
		for _, binding := range bindings {
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
		}
		// wait until the client informer is populated
		Eventually(func() error {
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return err
				}
			}
			return nil
		}, timeout, interval).Should(Succeed(), "make sure the cache is populated")
		// check that no bindings are rolled out
		Consistently(func(g Gomega) bool {
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				g.Expect(err).Should(Succeed())
				if binding.Spec.State == fleetv1beta1.BindingStateBound {
					return false
				}
			}
			return true
		}, consistentTimeout, consistentInterval).Should(BeTrue(), "rollout controller should not roll the bindings")
		By("Verified that the rollout is blocked")
		// now we remove the finalizer of the first deleting binding
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: firstDeleteBinding.GetName()}, firstDeleteBinding)).Should(Succeed())
		firstDeleteBinding.SetFinalizers([]string{})
		Expect(k8sClient.Update(ctx, firstDeleteBinding)).Should(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: firstDeleteBinding.GetName()}, firstDeleteBinding))
		}, timeout, interval).Should(BeTrue(), "the first deleting binding should now be deleted")
		By("Verified that the first deleting binding is deleted")
		// check that no bindings are rolled out
		Consistently(func() bool {
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State == fleetv1beta1.BindingStateBound {
					return false
				}
			}
			return true
		}, consistentTimeout, consistentInterval).Should(BeTrue(), "rollout controller should not roll the bindings")
		By("Verified that the rollout is still blocked")
		// now we remove the finalizer of the second deleting binding
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secondDeleteBinding.GetName()}, secondDeleteBinding)).Should(Succeed())
		secondDeleteBinding.SetFinalizers([]string{})
		Expect(k8sClient.Update(ctx, secondDeleteBinding)).Should(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: secondDeleteBinding.GetName()}, secondDeleteBinding))
		}, timeout, interval).Should(BeTrue(), "the second deleting binding should now be deleted")
		By("Verified that the second deleting binding is deleted")
		// check that the bindings are rolledout
		Eventually(func() bool {
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State != fleetv1beta1.BindingStateBound {
					return false
				}
			}
			return true
		}, consistentTimeout, consistentInterval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")
		By("Verified that the rollout is finally unblocked")
	})

	It("Should rollout both the old applied and failed to apply bond the new resources", func() {
		// create CRP
		var targetCluster int32 = 5
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(fleetv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + strconv.Itoa(i)
			binding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// check that all bindings are scheduled
		Eventually(func() bool {
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State != fleetv1beta1.BindingStateBound {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")
		// simulate that some of the bindings are available successfully
		applySuccessfully := 3
		for i := 0; i < applySuccessfully; i++ {
			markBindingAvailable(bindings[i], true)
		}
		// simulate that some of the bindings fail to apply
		for i := applySuccessfully; i < int(targetCluster); i++ {
			markBindingApplied(bindings[i], false)
		}
		// mark the master snapshot as not latest
		masterSnapshot.SetLabels(map[string]string{
			fleetv1beta1.CRPTrackingLabel:      testCRPName,
			fleetv1beta1.IsLatestSnapshotLabel: "false"},
		)
		Expect(k8sClient.Update(ctx, masterSnapshot)).Should(Succeed())
		// create a new master resource snapshot
		newMasterSnapshot := generateResourceSnapshot(rolloutCRP.Name, 1, true)
		Expect(k8sClient.Create(ctx, newMasterSnapshot)).Should(Succeed())
		Eventually(func() bool {
			allMatch := true
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					allMatch = false
				}
				if binding.Spec.ResourceSnapshotName == newMasterSnapshot.Name {
					// simulate the work generator to make the newly updated bindings to be available
					markBindingAvailable(binding, true)
				} else {
					allMatch = false
				}
			}
			return allMatch
		}, 5*defaultUnavailablePeriod*time.Second, interval).Should(BeTrue(), "rollout controller should roll all the bindings to use the latest resource snapshot")
	})

	It("Should wait designated time before rolling out ", func() {
		// create CRP
		var targetCluster int32 = 11
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(fleetv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		// remove the strategy
		rolloutCRP.Spec.Strategy = fleetv1beta1.RolloutStrategy{RollingUpdate: &fleetv1beta1.RollingUpdateConfig{UnavailablePeriodSeconds: ptr.To(60)}}
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// check that all bindings are scheduled
		Eventually(func() bool {
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State != fleetv1beta1.BindingStateBound || binding.Spec.ResourceSnapshotName != masterSnapshot.Name {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")

		// simulate that some of the bindings are available successfully
		applySuccessfully := 3
		for i := 0; i < applySuccessfully; i++ {
			markBindingAvailable(bindings[i], true)
		}
		// simulate that some of the bindings fail to apply
		for i := applySuccessfully; i < int(targetCluster); i++ {
			markBindingApplied(bindings[i], false)
		}
		// mark the master snapshot as not latest
		masterSnapshot.SetLabels(map[string]string{
			fleetv1beta1.CRPTrackingLabel:      testCRPName,
			fleetv1beta1.IsLatestSnapshotLabel: "false"},
		)
		Expect(k8sClient.Update(ctx, masterSnapshot)).Should(Succeed())
		// create a new master resource snapshot
		newMasterSnapshot := generateResourceSnapshot(rolloutCRP.Name, 1, true)
		Expect(k8sClient.Create(ctx, newMasterSnapshot)).Should(Succeed())
		Consistently(func() bool {
			allMatch := true
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					allMatch = false
				}
				if binding.Spec.ResourceSnapshotName != newMasterSnapshot.Name {
					return true
				}
			}
			return allMatch
		}, consistentTimeout, consistentInterval).Should(BeTrue(), "rollout controller should not roll all the bindings to use the latest resource snapshot")

		Eventually(func() bool {
			allMatch := true
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					allMatch = false
				}
				if binding.Spec.ResourceSnapshotName == newMasterSnapshot.Name {
					// simulate the work generator to make the newly updated bindings to be available
					markBindingAvailable(binding, true)
				} else {
					allMatch = false
				}
			}
			return allMatch
		}, 5*time.Minute, interval).Should(BeTrue(), "rollout controller should roll all the bindings to use the latest resource snapshot")
	})

	It("Rollout should be blocked, then unblocked by eviction - evict unscheduled binding", func() {
		// create CRP
		var targetCluster int32 = 2
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(fleetv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		// Set MaxSurge to 0.
		rolloutCRP.Spec.Strategy.RollingUpdate.MaxSurge = &intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: 0,
		}
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest.
		masterSnapshot := generateResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())

		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			bindings = append(bindings, binding)
		}

		// check that all bindings are bound.
		Eventually(func() bool {
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State != fleetv1beta1.BindingStateBound || binding.Spec.ResourceSnapshotName != masterSnapshot.Name {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")

		// mark one binding as ready i.e. applied and available.
		availableBinding := 1
		for i := 0; i < availableBinding; i++ {
			markBindingApplied(bindings[i], true)
			markBindingAvailable(bindings[i], false)
		}
		// Current state: one ready binding and one canBeReadyBinding.
		// create a new scheduled binding.
		cluster3 = "cluster-" + utils.RandStr()
		newScheduledBinding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, cluster3)
		Expect(k8sClient.Create(ctx, newScheduledBinding)).Should(Succeed())
		By(fmt.Sprintf("resource binding  %s created", newScheduledBinding.Name))
		// add new scheduled binding to list of bindings.
		bindings = append(bindings, newScheduledBinding)

		// ensure new binding exists.
		Eventually(func() bool {
			return !apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: newScheduledBinding.Name}, newScheduledBinding))
		}, timeout, interval).Should(BeTrue(), "new scheduled binding is not found")

		// check if new scheduled binding is not bound.
		Consistently(func() error {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: newScheduledBinding.Name}, newScheduledBinding)
			if err != nil {
				return err
			}
			if newScheduledBinding.Spec.State == fleetv1beta1.BindingStateBound {
				return fmt.Errorf("binding %s is in bound state, which is unexpected", newScheduledBinding.Name)
			}
			return nil
		}, timeout, interval).Should(BeNil(), "rollout controller shouldn't roll new scheduled binding to bound state")

		// Current state: rollout is blocked by maxSurge being 0.
		// mark first available bound binding as unscheduled and ensure it's not removed.
		unscheduledBinding := 1
		for i := 0; i < unscheduledBinding; i++ {
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: bindings[i].Name}, bindings[i])
				if err != nil {
					return err
				}
				bindings[i].Spec.State = fleetv1beta1.BindingStateUnscheduled
				return k8sClient.Update(ctx, bindings[i])
			}, timeout, interval).Should(BeNil(), "failed to update binding spec to unscheduled")

			// Ensure unscheduled binding is not removed.
			Consistently(func() bool {
				return !apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: bindings[i].Name}, bindings[i]))
			}, timeout, interval).Should(BeTrue(), "rollout controller doesn't remove unscheduled binding")
		}

		// simulate eviction by deleting unscheduled binding.
		for i := 0; i < unscheduledBinding; i++ {
			Expect(k8sClient.Delete(ctx, bindings[i])).Should(Succeed())
		}

		// check to see if rollout is unblocked due to eviction.
		for i := unscheduledBinding; i < len(bindings); i++ {
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: bindings[i].GetName()}, bindings[i])
				if err != nil {
					return false
				}
				if bindings[i].Spec.State != fleetv1beta1.BindingStateBound || bindings[i].Spec.ResourceSnapshotName != masterSnapshot.Name {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue(), "rollout controller should roll all remaining bindings to Bound state")
		}
	})

	It("Rollout should be blocked, then unblocked by eviction - evict bound binding", func() {
		// create CRP
		var targetCluster int32 = 2
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(fleetv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		// Set MaxSurge to 0.
		rolloutCRP.Spec.Strategy.RollingUpdate.MaxSurge = &intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: 0,
		}
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest.
		masterSnapshot := generateResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())

		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			bindings = append(bindings, binding)
		}

		// check that all bindings are bound.
		Eventually(func() bool {
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State != fleetv1beta1.BindingStateBound || binding.Spec.ResourceSnapshotName != masterSnapshot.Name {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")

		// Note: This scenario is very unlikely in production user has to change the target from 2->3->2,
		// where scheduler created new scheduled binding but user changed the target number from 3->2 again, before rollout controller reads CRP.
		// create a new scheduled binding.
		cluster3 = "cluster-" + utils.RandStr()
		newScheduledBinding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, cluster3)
		Expect(k8sClient.Create(ctx, newScheduledBinding)).Should(Succeed())
		By(fmt.Sprintf("resource binding  %s created", newScheduledBinding.Name))
		// add new scheduled binding to list of bindings.
		bindings = append(bindings, newScheduledBinding)

		// ensure new binding exists.
		Eventually(func() bool {
			return !apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: newScheduledBinding.Name}, newScheduledBinding))
		}, timeout, interval).Should(BeTrue(), "new scheduled binding is not found")

		// Current state: rollout is blocked by maxSurge being 0.
		// check if new scheduled binding is not bound.
		Consistently(func() error {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: newScheduledBinding.Name}, newScheduledBinding)
			if err != nil {
				return err
			}
			if newScheduledBinding.Spec.State == fleetv1beta1.BindingStateBound {
				return fmt.Errorf("binding %s is in bound state, which is unexpected", newScheduledBinding.Name)
			}
			return nil
		}, 3*defaultUnavailablePeriod*time.Second, interval).Should(BeNil(), "rollout controller shouldn't roll new scheduled binding to bound state")

		// simulate eviction by deleting first bound binding.
		firstBoundBinding := 1
		for i := 0; i < firstBoundBinding; i++ {
			Expect(k8sClient.Delete(ctx, bindings[i])).Should(Succeed())
		}

		// check to see if the remaining two bindings are bound.
		for i := firstBoundBinding; i < len(bindings); i++ {
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: bindings[i].GetName()}, bindings[i])
				if err != nil {
					return false
				}
				if bindings[i].Spec.State != fleetv1beta1.BindingStateBound || bindings[i].Spec.ResourceSnapshotName != masterSnapshot.Name {
					return false
				}
				return true
			}, 3*defaultUnavailablePeriod*time.Second, interval).Should(BeTrue(), "rollout controller should roll all remaining bindings to Bound state")
		}
	})

	// TODO: should update scheduled bindings to the latest snapshot when it is updated to bound state.

	// TODO: should count the deleting bindings as can be Unavailable.

})

func markBindingAvailable(binding *fleetv1beta1.ClusterResourceBinding, trackable bool) {
	Eventually(func() error {
		reason := "trackable"
		if !trackable {
			reason = workapplier.WorkNotAllManifestsTrackableReason
		}
		binding.SetConditions(metav1.Condition{
			Type:               string(fleetv1beta1.ResourceBindingAvailable),
			Status:             metav1.ConditionTrue,
			Reason:             reason,
			ObservedGeneration: binding.Generation,
		})
		if err := k8sClient.Status().Update(ctx, binding); err != nil {
			if apierrors.IsConflict(err) {
				// get the binding again to avoid conflict
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
			}
			return err
		}
		return nil
	}, timeout, interval).Should(Succeed(), "should update the binding status successfully")
	By(fmt.Sprintf("resource binding `%s` is marked as available", binding.Name))
}

func markBindingApplied(binding *fleetv1beta1.ClusterResourceBinding, success bool) {
	applyCondition := metav1.Condition{
		Type: string(fleetv1beta1.ResourceBindingApplied),
	}
	if success {
		applyCondition.Status = metav1.ConditionTrue
		applyCondition.Reason = "applySucceeded"
	} else {
		applyCondition.Status = metav1.ConditionFalse
		applyCondition.Reason = "applyFailed"
	}
	Eventually(func() error {
		applyCondition.ObservedGeneration = binding.Generation
		binding.SetConditions(applyCondition)
		if err := k8sClient.Status().Update(ctx, binding); err != nil {
			if apierrors.IsConflict(err) {
				// get the binding again to avoid conflict
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.Name}, binding)).Should(Succeed())
			}
			return err
		}
		return nil
	}, timeout, interval).Should(Succeed(), "should update the binding status successfully")
	By(fmt.Sprintf("resource binding `%s` is marked as applied with status %t", binding.Name, success))
}

func generateClusterResourceBinding(state fleetv1beta1.BindingState, resourceSnapshotName, targetCluster string) *fleetv1beta1.ClusterResourceBinding {
	binding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-" + resourceSnapshotName + "-" + targetCluster,
			Labels: map[string]string{
				fleetv1beta1.CRPTrackingLabel: testCRPName,
			},
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			State:         state,
			TargetCluster: targetCluster,
		},
	}
	if binding.Spec.State == fleetv1beta1.BindingStateBound {
		binding.Spec.ResourceSnapshotName = resourceSnapshotName
	}
	return binding
}

func generateResourceSnapshot(testCRPName string, resourceIndex int, isLatest bool) *fleetv1beta1.ClusterResourceSnapshot {
	clusterResourceSnapshot := &fleetv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, resourceIndex),
			Labels: map[string]string{
				fleetv1beta1.CRPTrackingLabel:      testCRPName,
				fleetv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(isLatest),
			},
			Annotations: map[string]string{
				fleetv1beta1.ResourceGroupHashAnnotation: "hash",
			},
		},
	}
	rawContents := [][]byte{
		testResourceCRD, testNameSpace, testResource, testConfigMap, testPdb,
	}
	for _, rawContent := range rawContents {
		clusterResourceSnapshot.Spec.SelectedResources = append(clusterResourceSnapshot.Spec.SelectedResources,
			fleetv1beta1.ResourceContent{
				RawExtension: runtime.RawExtension{Raw: rawContent},
			},
		)
	}
	return clusterResourceSnapshot
}
