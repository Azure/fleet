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

package rollout

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

const (
	timeout                = time.Second * 5
	interval               = time.Millisecond * 250
	consistentTimeout      = time.Second * 60
	consistentInterval     = time.Second * 5
	customBindingFinalizer = "custom-binding-finalizer"
	testNamespace          = "app" // to align with the test resources in rollout/manifests
)

var (
	ignoreCRBTypeMetaAndStatusFields = cmpopts.IgnoreFields(placementv1beta1.ClusterResourceBinding{}, "TypeMeta", "Status")
	ignoreRBTypeMetaAndStatusFields  = cmpopts.IgnoreFields(placementv1beta1.ResourceBinding{}, "TypeMeta", "Status")
	ignoreObjectMetaAutoGenFields    = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "CreationTimestamp", "Generation", "ResourceVersion", "SelfLink", "UID", "ManagedFields")
	ignoreCondLTTAndMessageFields    = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message")
)

var testCRPName string
var testRPName string

var _ = Describe("Test the rollout Controller", func() {

	var bindings []*placementv1beta1.ClusterResourceBinding
	var resourceSnapshots []*placementv1beta1.ClusterResourceSnapshot
	var rolloutCRP *placementv1beta1.ClusterResourcePlacement

	BeforeEach(func() {
		testCRPName = "crp" + utils.RandStr()
		bindings = make([]*placementv1beta1.ClusterResourceBinding, 0)
		resourceSnapshots = make([]*placementv1beta1.ClusterResourceSnapshot, 0)
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
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// Check that all bindings are bound.
		verifyBindingsRolledOut(controller.ConvertCRBArrayToBindingObjs(bindings), masterSnapshot, timeout)
	})

	It("should push apply strategy changes to all the bindings (if applicable) and refresh their status", func() {
		// Create a CRP.
		targetClusterCount := int32(3)
		rolloutCRP = clusterResourcePlacementForTest(
			testCRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetClusterCount),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed(), "Failed to create CRP")

		// Create a master cluster resource snapshot.
		resourceSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, resourceSnapshot)).Should(Succeed(), "Failed to create cluster resource snapshot")

		// Create all the bindings.
		clusters := make([]string, targetClusterCount)
		for i := 0; i < int(targetClusterCount); i++ {
			clusters[i] = "cluster-" + utils.RandStr()

			// Prepare bindings of various states.
			var binding *placementv1beta1.ClusterResourceBinding
			switch i % 3 {
			case 0:
				binding = generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, resourceSnapshot.Name, clusters[i])
			case 1:
				binding = generateClusterResourceBinding(placementv1beta1.BindingStateBound, resourceSnapshot.Name, clusters[i])
			default:
				binding = generateClusterResourceBinding(placementv1beta1.BindingStateUnscheduled, resourceSnapshot.Name, clusters[i])
			}
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed(), "Failed to create cluster resource binding")
			bindings = append(bindings, binding)
		}

		// Verify that all the bindings are updated per rollout strategy.
		Eventually(func() error {
			for _, binding := range bindings {
				gotBinding := &placementv1beta1.ClusterResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, gotBinding); err != nil {
					return fmt.Errorf("failed to get binding %s: %w", binding.Name, err)
				}

				wantBinding := &placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: binding.Name,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         binding.Spec.State,
						TargetCluster: binding.Spec.TargetCluster,
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
							WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
							WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
							Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
						},
					},
				}
				// The bound binding will have no changes; the scheduled binding, per given
				// rollout strategy, will be bound with the resource snapshot.
				if binding.Spec.State == placementv1beta1.BindingStateBound || binding.Spec.State == placementv1beta1.BindingStateScheduled {
					wantBinding.Spec.State = placementv1beta1.BindingStateBound
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
				gotBinding := &placementv1beta1.ClusterResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, gotBinding); err != nil {
					return fmt.Errorf("failed to get binding %s: %w", binding.Name, err)
				}

				wantBindingStatus := &placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
							Status:             metav1.ConditionTrue,
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: gotBinding.Generation,
						},
					},
				}
				// The scheduled binding will be set to the Bound state with the RolloutStarted
				// condition set to True; the bound binding will receive a True RolloutStarted
				// condition; the unscheduled binding will have no RolloutStarted condition update.
				if binding.Spec.State == placementv1beta1.BindingStateUnscheduled {
					wantBindingStatus = &placementv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{},
					}
				}
				if diff := cmp.Diff(
					&gotBinding.Status, wantBindingStatus,
					ignoreCondLTTAndMessageFields,
					cmpopts.EquateEmpty(),
				); diff != "" {
					return fmt.Errorf("binding status diff (%v/%v) (-got, +want):\n%s", binding.Spec.State, gotBinding.Spec.State, diff)
				}
			}
			return nil
		}, timeout, interval).Should(Succeed(), "Failed to verify that all the bindings have their status refreshed")

		// Update the CRP with a new apply strategy.
		rolloutCRP.Spec.Strategy.ApplyStrategy = &placementv1beta1.ApplyStrategy{
			ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
			WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
			WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeIfNoDiff,
			Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
			ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{
				ForceConflicts: true,
			},
		}
		Expect(k8sClient.Update(ctx, rolloutCRP)).Should(Succeed(), "Failed to update CRP")

		// Verify that all the bindings are updated with the new apply strategy.
		Eventually(func() error {
			for _, binding := range bindings {
				gotBinding := &placementv1beta1.ClusterResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, gotBinding); err != nil {
					return fmt.Errorf("failed to get binding %s: %w", binding.Name, err)
				}

				wantBinding := &placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: binding.Name,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         binding.Spec.State,
						TargetCluster: binding.Spec.TargetCluster,
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
							WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
							WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeIfNoDiff,
							Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
							ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{
								ForceConflicts: true,
							},
						},
					},
				}
				// The bound binding will have no changes; the scheduled binding, per given
				// rollout strategy, will be bound with the resource snapshot.
				if binding.Spec.State == placementv1beta1.BindingStateBound || binding.Spec.State == placementv1beta1.BindingStateScheduled {
					wantBinding.Spec.State = placementv1beta1.BindingStateBound
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
				gotBinding := &placementv1beta1.ClusterResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, gotBinding); err != nil {
					return fmt.Errorf("failed to get binding %s: %w", binding.Name, err)
				}

				wantBindingStatus := &placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
							Status:             metav1.ConditionTrue,
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: gotBinding.Generation,
						},
					},
				}
				// The scheduled binding will be set to the Bound state with the RolloutStarted
				// condition set to True; the bound binding will receive a True RolloutStarted
				// condition; the unscheduled binding will have no RolloutStarted condition update.
				if binding.Spec.State == placementv1beta1.BindingStateUnscheduled {
					wantBindingStatus = &placementv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{},
					}
				}
				if diff := cmp.Diff(
					&gotBinding.Status, wantBindingStatus,
					ignoreCondLTTAndMessageFields,
					cmpopts.EquateEmpty(),
				); diff != "" {
					return fmt.Errorf("binding status diff (%v/%v) (-got, +want):\n%s", binding.Spec.State, gotBinding.Spec.State, diff)
				}
			}
			return nil
		}, timeout, interval).Should(Succeed(), "Failed to verify that all the bindings have their status refreshed")
	})

	It("should trigger binding rollout for clusterResourceOverrideSnapshot but not resourceOverrideSnapshot with Namespaced scope", func() {
		// Create a CRP.
		targetClusterCount := int32(2)
		rolloutCRP = clusterResourcePlacementForTest(
			testCRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetClusterCount),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed(), "Failed to create CRP")

		// Create a master cluster resource snapshot.
		resourceSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, resourceSnapshot)).Should(Succeed(), "Failed to create cluster resource snapshot")

		// Create bindings.
		clusters := make([]string, targetClusterCount)
		for i := 0; i < int(targetClusterCount); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, resourceSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed(), "Failed to create cluster resource binding")
			bindings = append(bindings, binding)

			memberCluster := generateMemberCluster(i, clusters[i])
			Expect(k8sClient.Create(ctx, memberCluster)).Should(Succeed(), "Failed to create member cluster")
		}

		// Verify that all the bindings are rolled out initially.
		verifyBindingsRolledOut(controller.ConvertCRBArrayToBindingObjs(bindings), resourceSnapshot, timeout)

		// Mark the bindings to be available.
		for _, binding := range bindings {
			markBindingAvailable(binding, true)
		}

		// Create a resourceOverrideSnapshot with the same placement name but Namespaced scope and verify bindings are not updated.
		testROName1 := "ro" + utils.RandStr()
		resourceOverrideSnapshot1 := generateResourceOverrideSnapshot(testROName1, testCRPName, placementv1beta1.NamespaceScoped)
		By(fmt.Sprintf("Creating resourceOverrideSnapshot %s to refer a resourcePlacement", resourceOverrideSnapshot1.Name))
		Expect(k8sClient.Create(ctx, resourceOverrideSnapshot1)).Should(Succeed(), "Failed to create resource override snapshot")

		// Verify bindings are NOT updated (rollout not triggered) by resourceOverrideSnapshot.
		verifyBindingsNotUpdatedWithOverridesConsistently(controller.ConvertCRBArrayToBindingObjs(bindings), nil, nil)

		By(fmt.Sprintf("Updating resourceOverrideSnapshot %s to refer the clusterResourcePlacement instead", resourceOverrideSnapshot1.Name))
		resourceOverrideSnapshot1.Spec.OverrideSpec.Placement.Scope = placementv1beta1.ClusterScoped
		Expect(k8sClient.Update(ctx, resourceOverrideSnapshot1)).Should(Succeed(), "Failed to update resource override snapshot")

		// Verify bindings are NOT updated (rollout not triggered) by resourceOverrideSnapshot.
		// This is because rollout controller is not triggered by overrideSnapshot update events.
		verifyBindingsNotUpdatedWithOverridesConsistently(controller.ConvertCRBArrayToBindingObjs(bindings), nil, nil)

		// Create a clusterResourceOverrideSnapshot and verify it triggers rollout.
		testCROName := "cro" + utils.RandStr()
		clusterResourceOverrideSnapshot := generateClusterResourceOverrideSnapshot(testCROName, testCRPName)
		By(fmt.Sprintf("Creating clusterResourceOverrideSnapshot %s to refer the clusterResourcePlacement", clusterResourceOverrideSnapshot.Name))
		Expect(k8sClient.Create(ctx, clusterResourceOverrideSnapshot)).Should(Succeed(), "Failed to create cluster resource override snapshot")

		// Verify bindings are updated, note that both clusterResourceOverrideSnapshot and resourceOverrideSnapshot are set in the bindings.
		waitUntilRolloutCompleted(controller.ConvertCRBArrayToBindingObjs(bindings), []string{clusterResourceOverrideSnapshot.Name},
			[]placementv1beta1.NamespacedName{{Name: resourceOverrideSnapshot1.Name, Namespace: resourceOverrideSnapshot1.Namespace}})

		// Create another resourceOverrideSnapshot referencing the same CRP and verify bindings are updated again.
		testROName2 := "ro" + utils.RandStr()
		resourceOverrideSnapshot2 := generateResourceOverrideSnapshot(testROName2, testCRPName, placementv1beta1.ClusterScoped)
		By(fmt.Sprintf("Creating resourceOverrideSnapshot %s to refer a clusterResourcePlacement", resourceOverrideSnapshot2.Name))
		Expect(k8sClient.Create(ctx, resourceOverrideSnapshot2)).Should(Succeed(), "Failed to create resource override snapshot")

		// Verify bindings are updated, note that both clusterResourceOverrideSnapshot and resourceOverrideSnapshot are set in the bindings.
		waitUntilRolloutCompleted(controller.ConvertCRBArrayToBindingObjs(bindings), []string{clusterResourceOverrideSnapshot.Name},
			[]placementv1beta1.NamespacedName{
				{Name: resourceOverrideSnapshot1.Name, Namespace: resourceOverrideSnapshot1.Namespace},
				{Name: resourceOverrideSnapshot2.Name, Namespace: resourceOverrideSnapshot2.Namespace},
			},
		)

		// Clean up the override snapshots.
		Expect(k8sClient.Delete(ctx, resourceOverrideSnapshot1)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, clusterResourceOverrideSnapshot)).Should(Succeed())

		// Clean up the member clusters.
		for _, cluster := range clusters {
			memberCluster := &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster,
				},
			}
			Expect(k8sClient.Delete(ctx, memberCluster)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
	})

	It("Should rollout all the selected bindings when the rollout strategy is not set", func() {
		// create CRP
		var targetCluster int32 = 11
		// rolloutStrategy not set.
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			placementv1beta1.RolloutStrategy{})
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// Check that all bindings are bound.
		verifyBindingsRolledOut(controller.ConvertCRBArrayToBindingObjs(bindings), masterSnapshot, timeout)
	})

	It("Should rollout the selected and unselected bindings (not trackable resources)", func() {
		// create CRP
		var initTargetClusterNum int32 = 11
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, initTargetClusterNum),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, initTargetClusterNum)
		for i := 0; i < int(initTargetClusterNum); i++ {
			clusters[i] = "cluster-" + strconv.Itoa(i)
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// Check that all bindings are bound.
		verifyBindingsRolledOut(controller.ConvertCRBArrayToBindingObjs(bindings), masterSnapshot, timeout)

		// simulate that some of the bindings are available and not trackable.
		firstApplied := 3
		for i := 0; i < firstApplied; i++ {
			markBindingAvailable(bindings[i], false)
		}
		// simulate another scheduling decision, pick some cluster to unselect from the bottom of the list
		var newTargetClusterNum int32 = 9
		rolloutCRP.Spec.Policy.NumberOfClusters = &newTargetClusterNum
		Expect(k8sClient.Update(ctx, rolloutCRP)).Should(Succeed())
		secondRoundBindings := make([]*placementv1beta1.ClusterResourceBinding, 0)
		deletedBindings := make([]*placementv1beta1.ClusterResourceBinding, 0)
		stillScheduledClusterNum := 6 // the amount of clusters that are still scheduled in first round
		// simulate that some of the bindings are available
		// moved to before being set to unscheduled, otherwise, the rollout controller will try to delete the bindings before we mark them as available.
		for i := int(newTargetClusterNum); i < int(initTargetClusterNum); i++ {
			markBindingAvailable(bindings[i], false)
		}
		for i := int(initTargetClusterNum - 1); i >= stillScheduledClusterNum; i-- {
			binding := bindings[i]
			binding.Spec.State = placementv1beta1.BindingStateUnscheduled
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
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, "cluster-"+strconv.Itoa(int(initTargetClusterNum)+i))
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
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
				if binding.Spec.State != placementv1beta1.BindingStateBound || binding.Spec.ResourceSnapshotName != masterSnapshot.Name {
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
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + strconv.Itoa(i)
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// Check that all bindings are bound.
		verifyBindingsRolledOut(controller.ConvertCRBArrayToBindingObjs(bindings), masterSnapshot, timeout)

		// simulate that some of the bindings are available
		firstApplied := 3
		for i := 0; i < firstApplied; i++ {
			markBindingAvailable(bindings[i], true)
		}
		// simulate another scheduling decision, pick some cluster to unselect from the bottom of the list
		var newTarget int32 = 9
		rolloutCRP.Spec.Policy.NumberOfClusters = &newTarget
		Expect(k8sClient.Update(ctx, rolloutCRP)).Should(Succeed())
		secondRoundBindings := make([]*placementv1beta1.ClusterResourceBinding, 0)
		deletedBindings := make([]*placementv1beta1.ClusterResourceBinding, 0)
		stillScheduled := 6
		// simulate that some of the bindings are applied
		// moved to before being set to unscheduled, otherwise, the rollout controller will try to delete the bindings before we mark them as available.
		for i := int(newTarget); i < int(targetCluster); i++ {
			markBindingAvailable(bindings[i], true)
		}
		for i := int(targetCluster - 1); i >= stillScheduled; i-- {
			binding := bindings[i]
			binding.Spec.State = placementv1beta1.BindingStateUnscheduled
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
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, "cluster-"+strconv.Itoa(int(targetCluster)+i))
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
			secondRoundBindings = append(secondRoundBindings, binding)
		}
		// mark the master snapshot as not latest
		masterSnapshot.SetLabels(map[string]string{
			placementv1beta1.PlacementTrackingLabel: testCRPName,
			placementv1beta1.IsLatestSnapshotLabel:  "false"},
		)
		Expect(k8sClient.Update(ctx, masterSnapshot)).Should(Succeed())
		// create a new master resource snapshot
		newMasterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 1, true)
		Expect(k8sClient.Create(ctx, newMasterSnapshot)).Should(Succeed())
		// check that the second round of bindings are scheduled
		Eventually(func() bool {
			for _, binding := range secondRoundBindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State != placementv1beta1.BindingStateBound {
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
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		latestSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 1, true)
		Expect(k8sClient.Create(ctx, latestSnapshot)).Should(Succeed())
		By(fmt.Sprintf("resource snapshot %s created", latestSnapshot.Name))
		// generate scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, latestSnapshot.Name, clusters[i])
			bindings = append(bindings, binding)
		}
		// create two unscheduled bindings and delete them
		firstDeleteBinding := generateClusterResourceBinding(placementv1beta1.BindingStateUnscheduled, latestSnapshot.Name, clusters[0])
		firstDeleteBinding.Name = "delete-" + firstDeleteBinding.Name
		firstDeleteBinding.SetFinalizers([]string{customBindingFinalizer})
		Expect(k8sClient.Create(ctx, firstDeleteBinding)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, firstDeleteBinding)).Should(Succeed())
		secondDeleteBinding := generateClusterResourceBinding(placementv1beta1.BindingStateUnscheduled, latestSnapshot.Name, clusters[2])
		secondDeleteBinding.Name = "delete-" + secondDeleteBinding.Name
		secondDeleteBinding.SetFinalizers([]string{customBindingFinalizer})
		Expect(k8sClient.Create(ctx, secondDeleteBinding)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, secondDeleteBinding)).Should(Succeed())
		By("Created 2 deleting bindings")
		// create the normal binding after the deleting one
		for _, binding := range bindings {
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
		}

		// check that no bindings are rolled out
		verifyBindingsNotRolledOutConsistently(controller.ConvertCRBArrayToBindingObjs(bindings))

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
		verifyBindingsNotRolledOutConsistently(controller.ConvertCRBArrayToBindingObjs(bindings))

		By("Verified that the rollout is still blocked")
		// now we remove the finalizer of the second deleting binding
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secondDeleteBinding.GetName()}, secondDeleteBinding)).Should(Succeed())
		secondDeleteBinding.SetFinalizers([]string{})
		Expect(k8sClient.Update(ctx, secondDeleteBinding)).Should(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: secondDeleteBinding.GetName()}, secondDeleteBinding))
		}, timeout, interval).Should(BeTrue(), "the second deleting binding should now be deleted")
		By("Verified that the second deleting binding is deleted")
		// Check that the bindings are rolledout.
		// When there is a binding assigned to a cluster with another deleting bindings, the controller
		// will wait until the deleting binding is deleted before it rolls out the bindings.
		// It requeues the bindings every 5 sceconds by checking waitForResourcesToCleanUp func.
		// Leave 5 seconds for the controller to requeue the bindings and roll them out.
		verifyBindingsRolledOut(controller.ConvertCRBArrayToBindingObjs(bindings), latestSnapshot, 5*time.Second+timeout)
		By("Verified that the rollout is finally unblocked")
	})

	It("Should rollout both the old applied and failed to apply bound the new resources", func() {
		// create CRP
		var targetCluster int32 = 5
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + strconv.Itoa(i)
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// Check that all bindings are bound.
		verifyBindingsRolledOut(controller.ConvertCRBArrayToBindingObjs(bindings), masterSnapshot, timeout)

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
			placementv1beta1.PlacementTrackingLabel: testCRPName,
			placementv1beta1.IsLatestSnapshotLabel:  "false"},
		)
		Expect(k8sClient.Update(ctx, masterSnapshot)).Should(Succeed())
		// create a new master resource snapshot
		newMasterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 1, true)
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
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		// remove the strategy
		rolloutCRP.Spec.Strategy = placementv1beta1.RolloutStrategy{RollingUpdate: &placementv1beta1.RollingUpdateConfig{UnavailablePeriodSeconds: ptr.To(60)}}
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// Check that all bindings are bound.
		verifyBindingsRolledOut(controller.ConvertCRBArrayToBindingObjs(bindings), masterSnapshot, timeout)

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
			placementv1beta1.PlacementTrackingLabel: testCRPName,
			placementv1beta1.IsLatestSnapshotLabel:  "false"},
		)
		Expect(k8sClient.Update(ctx, masterSnapshot)).Should(Succeed())
		// create a new master resource snapshot
		newMasterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 1, true)
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
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		// Set MaxSurge to 0.
		rolloutCRP.Spec.Strategy.RollingUpdate.MaxSurge = &intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: 0,
		}
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest.
		masterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())

		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}

		// Check that all bindings are bound..
		verifyBindingsRolledOut(controller.ConvertCRBArrayToBindingObjs(bindings), masterSnapshot, timeout)

		// mark one binding as ready i.e. applied and available.
		availableBinding := 1
		for i := 0; i < availableBinding; i++ {
			markBindingApplied(bindings[i], true)
			markBindingAvailable(bindings[i], false)
		}
		// Current state: one ready binding and one canBeReadyBinding.
		// create a new scheduled binding.
		cluster3 := "cluster-" + utils.RandStr()
		newScheduledBinding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, cluster3)
		Expect(k8sClient.Create(ctx, newScheduledBinding)).Should(Succeed())
		By(fmt.Sprintf("resource binding %s created", newScheduledBinding.Name))
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
			if newScheduledBinding.Spec.State == placementv1beta1.BindingStateBound {
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
				bindings[i].Spec.State = placementv1beta1.BindingStateUnscheduled
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
				if bindings[i].Spec.State != placementv1beta1.BindingStateBound || bindings[i].Spec.ResourceSnapshotName != masterSnapshot.Name {
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
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		// Set MaxSurge to 0.
		rolloutCRP.Spec.Strategy.RollingUpdate.MaxSurge = &intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: 0,
		}
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())
		// create master resource snapshot that is latest.
		masterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())

		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}

		// Check that all bindings are bound.
		verifyBindingsRolledOut(controller.ConvertCRBArrayToBindingObjs(bindings), masterSnapshot, timeout)

		// Note: This scenario is very unlikely in production user has to change the target from 2->3->2,
		// where scheduler created new scheduled binding but user changed the target number from 3->2 again, before rollout controller reads CRP.
		// create a new scheduled binding.
		cluster3 := "cluster-" + utils.RandStr()
		newScheduledBinding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, cluster3)
		Expect(k8sClient.Create(ctx, newScheduledBinding)).Should(Succeed())
		By(fmt.Sprintf("resource binding %s created", newScheduledBinding.Name))
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
			if newScheduledBinding.Spec.State == placementv1beta1.BindingStateBound {
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
				if bindings[i].Spec.State != placementv1beta1.BindingStateBound || bindings[i].Spec.ResourceSnapshotName != masterSnapshot.Name {
					return false
				}
				return true
			}, 3*defaultUnavailablePeriod*time.Second, interval).Should(BeTrue(), "rollout controller should roll all remaining bindings to Bound state")
		}
	})

	It("Should rollout all the selected bindings when strategy type is changed from External to RollingUpdate", func() {
		By("Creating CRP with External strategy")
		var targetCluster int32 = 10
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(placementv1beta1.ExternalRolloutStrategyType, nil, nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())

		By("Creating the latest master resource snapshot")
		masterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))

		By("Creating scheduled bindings for master snapshot on target clusters")
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}

		By("Checking bindings are not rolled out consistently")
		verifyBindingsNotRolledOutConsistently(controller.ConvertCRBArrayToBindingObjs(bindings))

		By("Updating CRP rollout strategy type to RollingUpdate")
		rolloutCRP.Spec.Strategy.Type = placementv1beta1.RollingUpdateRolloutStrategyType
		rolloutCRP.Spec.Strategy.RollingUpdate = generateDefaultRollingUpdateConfig()
		Expect(k8sClient.Update(ctx, rolloutCRP)).Should(Succeed(), "Failed to update CRP")

		By("Verifying that rollout is unblocked")
		verifyBindingsRolledOut(controller.ConvertCRBArrayToBindingObjs(bindings), masterSnapshot, timeout)
	})

	It("Should rollout all the selected bindings when strategy type is changed from External to empty", func() {
		By("Creating CRP with External strategy")
		var targetCluster int32 = 10
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(placementv1beta1.ExternalRolloutStrategyType, nil, nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())

		By("Creating the latest master resource snapshot")
		masterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))

		By("Creating scheduled bindings for master snapshot on target clusters")
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}

		By("Checking bindings are not rolled out consistently")
		verifyBindingsNotRolledOutConsistently(controller.ConvertCRBArrayToBindingObjs(bindings))

		By("Updating CRP rollout strategy type to empty")
		rolloutCRP.Spec.Strategy.Type = ""
		rolloutCRP.Spec.Strategy.RollingUpdate = nil
		Expect(k8sClient.Update(ctx, rolloutCRP)).Should(Succeed(), "Failed to update CRP")

		By("Verifying that rollout is unblocked")
		verifyBindingsRolledOut(controller.ConvertCRBArrayToBindingObjs(bindings), masterSnapshot, timeout)
	})

	It("Should not rollout anymore if the rollout strategy type is changed from RollingUpdate to External", func() {
		By("Creating CRP with RollingUpdate strategy")
		var targetCluster int32 = 10
		rolloutCRP = clusterResourcePlacementForTest(testCRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutCRP)).Should(Succeed())

		By("Creating the latest master resource snapshot")
		masterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))

		By("Creating scheduled bindings for master snapshot on target clusters")
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + strconv.Itoa(i)
			binding := generateClusterResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i])
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}

		By("Checking bindings are rolled out")
		verifyBindingsRolledOut(controller.ConvertCRBArrayToBindingObjs(bindings), masterSnapshot, timeout)

		By("Updating CRP rollout strategy type to External")
		rolloutCRP.Spec.Strategy.Type = placementv1beta1.ExternalRolloutStrategyType
		rolloutCRP.Spec.Strategy.RollingUpdate = nil
		Expect(k8sClient.Update(ctx, rolloutCRP)).Should(Succeed(), "Failed to update CRP")

		By("Creating a new master resource snapshot")
		// Mark the master snapshot as not latest.
		masterSnapshot.SetLabels(map[string]string{
			placementv1beta1.PlacementTrackingLabel: testCRPName,
			placementv1beta1.IsLatestSnapshotLabel:  "false"},
		)
		Expect(k8sClient.Update(ctx, masterSnapshot)).Should(Succeed())
		// Create a new master resource snapshot.
		newMasterSnapshot := generateClusterResourceSnapshot(rolloutCRP.Name, 1, true)
		Expect(k8sClient.Create(ctx, newMasterSnapshot)).Should(Succeed())

		By("Checking bindings are not updated")
		// Check that resource snapshot is not updated on the bindings.
		Consistently(func() error {
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName()}, binding)
				if err != nil {
					return fmt.Errorf("failed to get binding %s: %w", binding.GetName(), err)
				}
				if binding.Spec.ResourceSnapshotName == newMasterSnapshot.Name {
					return fmt.Errorf("binding %s is updated to the new snapshot, which is unwanted", binding.GetName())
				}
			}
			return nil
		}, consistentTimeout, consistentInterval).Should(Succeed(), "rollout controller should not roll all the bindings to latest resource snapshot")
	})

	// TODO: should update scheduled bindings to the latest snapshot when it is updated to bound state.

	// TODO: should count the deleting bindings as can be Unavailable.

})

var _ = Describe("Test the rollout Controller for ResourcePlacement", func() {

	var bindings []*placementv1beta1.ResourceBinding
	var resourceSnapshots []*placementv1beta1.ResourceSnapshot
	var rolloutRP *placementv1beta1.ResourcePlacement

	BeforeEach(func() {
		testRPName = "rp" + utils.RandStr()
		bindings = make([]*placementv1beta1.ResourceBinding, 0)
		resourceSnapshots = make([]*placementv1beta1.ResourceSnapshot, 0)
	})

	AfterEach(func() {
		By("Deleting ResourceBindings")
		for _, binding := range bindings {
			Expect(k8sClient.Delete(ctx, binding)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
		bindings = nil
		By("Deleting ResourceSnapshots")
		for _, resourceSnapshot := range resourceSnapshots {
			Expect(k8sClient.Delete(ctx, resourceSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
		resourceSnapshots = nil
		By("Deleting ResourcePlacement")
		if rolloutRP != nil {
			Expect(k8sClient.Delete(ctx, rolloutRP)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
	})

	It("Should rollout all the selected bindings as soon as they are created", func() {
		// create RP
		var targetCluster int32 = 10
		rolloutRP = resourcePlacementForTest(testNamespace, testRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateResourceSnapshot(rolloutRP.Namespace, rolloutRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i], testNamespace)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// Check that all bindings are bound.
		verifyBindingsRolledOut(controller.ConvertRBArrayToBindingObjs(bindings), masterSnapshot, timeout)
	})

	It("should push apply strategy changes to all the bindings (if applicable) and refresh their status", func() {
		// Create a RP.
		targetClusterCount := int32(3)
		rolloutRP = resourcePlacementForTest(
			testNamespace, testRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetClusterCount),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutRP)).Should(Succeed(), "Failed to create RP")

		// Create a master resource snapshot.
		resourceSnapshot := generateResourceSnapshot(rolloutRP.Namespace, rolloutRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, resourceSnapshot)).Should(Succeed(), "Failed to create resource snapshot")

		// Create all the bindings.
		clusters := make([]string, targetClusterCount)
		for i := 0; i < int(targetClusterCount); i++ {
			clusters[i] = "cluster-" + utils.RandStr()

			// Prepare bindings of various states.
			var binding *placementv1beta1.ResourceBinding
			switch i % 3 {
			case 0:
				binding = generateResourceBinding(placementv1beta1.BindingStateScheduled, resourceSnapshot.Name, clusters[i], testNamespace)
			case 1:
				binding = generateResourceBinding(placementv1beta1.BindingStateBound, resourceSnapshot.Name, clusters[i], testNamespace)
			default:
				binding = generateResourceBinding(placementv1beta1.BindingStateUnscheduled, resourceSnapshot.Name, clusters[i], testNamespace)
			}
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed(), "Failed to create resource binding")
			bindings = append(bindings, binding)
		}

		// Verify that all the bindings are updated per rollout strategy.
		Eventually(func() error {
			for _, binding := range bindings {
				gotBinding := &placementv1beta1.ResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}, gotBinding); err != nil {
					return fmt.Errorf("failed to get binding %s: %w", binding.Name, err)
				}

				wantBinding := &placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      binding.Name,
						Namespace: binding.Namespace,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         binding.Spec.State,
						TargetCluster: binding.Spec.TargetCluster,
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
							WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
							WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
							Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
						},
					},
				}
				// The bound binding will have no changes; the scheduled binding, per given
				// rollout strategy, will be bound with the resource snapshot.
				if binding.Spec.State == placementv1beta1.BindingStateBound || binding.Spec.State == placementv1beta1.BindingStateScheduled {
					wantBinding.Spec.State = placementv1beta1.BindingStateBound
					wantBinding.Spec.ResourceSnapshotName = resourceSnapshot.Name
				}
				if diff := cmp.Diff(
					gotBinding, wantBinding,
					ignoreRBTypeMetaAndStatusFields, ignoreObjectMetaAutoGenFields,
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
				gotBinding := &placementv1beta1.ResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}, gotBinding); err != nil {
					return fmt.Errorf("failed to get binding %s: %w", binding.Name, err)
				}

				wantBindingStatus := &placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
							Status:             metav1.ConditionTrue,
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: gotBinding.Generation,
						},
					},
				}
				// The scheduled binding will be set to the Bound state with the RolloutStarted
				// condition set to True; the bound binding will receive a True RolloutStarted
				// condition; the unscheduled binding will have no RolloutStarted condition update.
				if binding.Spec.State == placementv1beta1.BindingStateUnscheduled {
					wantBindingStatus = &placementv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{},
					}
				}
				if diff := cmp.Diff(
					&gotBinding.Status, wantBindingStatus,
					ignoreCondLTTAndMessageFields,
					cmpopts.EquateEmpty(),
				); diff != "" {
					return fmt.Errorf("binding status diff (%v/%v) (-got, +want):\n%s", binding.Spec.State, gotBinding.Spec.State, diff)
				}
			}
			return nil
		}, timeout, interval).Should(Succeed(), "Failed to verify that all the bindings have their status refreshed")

		// Update the RP with a new apply strategy.
		rolloutRP.Spec.Strategy.ApplyStrategy = &placementv1beta1.ApplyStrategy{
			ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
			WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
			WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeIfNoDiff,
			Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
			ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{
				ForceConflicts: true,
			},
		}
		Expect(k8sClient.Update(ctx, rolloutRP)).Should(Succeed(), "Failed to update RP")

		// Verify that all the bindings are updated with the new apply strategy.
		Eventually(func() error {
			for _, binding := range bindings {
				gotBinding := &placementv1beta1.ResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}, gotBinding); err != nil {
					return fmt.Errorf("failed to get binding %s: %w", binding.Name, err)
				}

				wantBinding := &placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      binding.Name,
						Namespace: binding.Namespace,
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:         binding.Spec.State,
						TargetCluster: binding.Spec.TargetCluster,
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							ComparisonOption: placementv1beta1.ComparisonOptionTypeFullComparison,
							WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
							WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeIfNoDiff,
							Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
							ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{
								ForceConflicts: true,
							},
						},
					},
				}
				// The bound binding will have no changes; the scheduled binding, per given
				// rollout strategy, will be bound with the resource snapshot.
				if binding.Spec.State == placementv1beta1.BindingStateBound || binding.Spec.State == placementv1beta1.BindingStateScheduled {
					wantBinding.Spec.State = placementv1beta1.BindingStateBound
					wantBinding.Spec.ResourceSnapshotName = resourceSnapshot.Name
				}
				if diff := cmp.Diff(
					gotBinding, wantBinding,
					ignoreRBTypeMetaAndStatusFields, ignoreObjectMetaAutoGenFields,
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
				gotBinding := &placementv1beta1.ResourceBinding{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}, gotBinding); err != nil {
					return fmt.Errorf("failed to get binding %s: %w", binding.Name, err)
				}

				wantBindingStatus := &placementv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
							Status:             metav1.ConditionTrue,
							Reason:             condition.RolloutStartedReason,
							ObservedGeneration: gotBinding.Generation,
						},
					},
				}
				// The scheduled binding will be set to the Bound state with the RolloutStarted
				// condition set to True; the bound binding will receive a True RolloutStarted
				// condition; the unscheduled binding will have no RolloutStarted condition update.
				if binding.Spec.State == placementv1beta1.BindingStateUnscheduled {
					wantBindingStatus = &placementv1beta1.ResourceBindingStatus{
						Conditions: []metav1.Condition{},
					}
				}
				if diff := cmp.Diff(
					&gotBinding.Status, wantBindingStatus,
					ignoreCondLTTAndMessageFields,
					cmpopts.EquateEmpty(),
				); diff != "" {
					return fmt.Errorf("binding status diff (%v/%v) (-got, +want):\n%s", binding.Spec.State, gotBinding.Spec.State, diff)
				}
			}
			return nil
		}, timeout, interval).Should(Succeed(), "Failed to verify that all the bindings have their status refreshed")
	})

	It("Should rollout the selected and unselected bindings (not trackable resources)", func() {
		// create RP
		var initTargetClusterNum int32 = 11
		rolloutRP = resourcePlacementForTest(testNamespace, testRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, initTargetClusterNum),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateResourceSnapshot(rolloutRP.Namespace, rolloutRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, initTargetClusterNum)
		for i := 0; i < int(initTargetClusterNum); i++ {
			clusters[i] = "cluster-" + strconv.Itoa(i)
			binding := generateResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i], testNamespace)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// Check that all bindings are bound.
		verifyBindingsRolledOut(controller.ConvertRBArrayToBindingObjs(bindings), masterSnapshot, timeout)

		// simulate that some of the bindings are available and not trackable.
		firstApplied := 3
		for i := 0; i < firstApplied; i++ {
			markBindingAvailable(bindings[i], false)
		}
		// simulate another scheduling decision, pick some cluster to unselect from the bottom of the list
		var newTargetClusterNum int32 = 9
		rolloutRP.Spec.Policy.NumberOfClusters = &newTargetClusterNum
		Expect(k8sClient.Update(ctx, rolloutRP)).Should(Succeed())
		secondRoundBindings := make([]*placementv1beta1.ResourceBinding, 0)
		deletedBindings := make([]*placementv1beta1.ResourceBinding, 0)
		stillScheduledClusterNum := 6 // the amount of clusters that are still scheduled in first round
		// simulate that some of the bindings are available
		// moved to before being set to unscheduled, otherwise, the rollout controller will try to delete the bindings before we mark them as available.
		for i := int(newTargetClusterNum); i < int(initTargetClusterNum); i++ {
			markBindingAvailable(bindings[i], false)
		}
		for i := int(initTargetClusterNum - 1); i >= stillScheduledClusterNum; i-- {
			binding := bindings[i]
			binding.Spec.State = placementv1beta1.BindingStateUnscheduled
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
			binding := generateResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, "cluster-"+strconv.Itoa(int(initTargetClusterNum)+i), testNamespace)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			secondRoundBindings = append(secondRoundBindings, binding)
		}
		// check that the second round of bindings are scheduled
		Eventually(func() bool {
			for _, binding := range secondRoundBindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}, binding)
				if err != nil {
					return false
				}
				if binding.Spec.State != placementv1beta1.BindingStateBound || binding.Spec.ResourceSnapshotName != masterSnapshot.Name {
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
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}, binding); err != nil && !apierrors.IsNotFound(err) {
					return false
				}
			}
			return true
		}, 3*defaultUnavailablePeriod*time.Second, interval).Should(BeTrue(), "rollout controller should delete all the unselected bindings")
	})

	It("Should wait for deleting binding delete before we rollout", func() {
		// create RP
		var targetCluster int32 = 5
		rolloutRP = resourcePlacementForTest(testNamespace, testRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutRP)).Should(Succeed())
		// create master resource snapshot that is latest
		latestSnapshot := generateResourceSnapshot(rolloutRP.Namespace, rolloutRP.Name, 1, true)
		Expect(k8sClient.Create(ctx, latestSnapshot)).Should(Succeed())
		By(fmt.Sprintf("resource snapshot %s created", latestSnapshot.Name))
		// generate scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateResourceBinding(placementv1beta1.BindingStateScheduled, latestSnapshot.Name, clusters[i], testNamespace)
			bindings = append(bindings, binding)
		}
		// create two unscheduled bindings and delete them
		firstDeleteBinding := generateResourceBinding(placementv1beta1.BindingStateUnscheduled, latestSnapshot.Name, clusters[0], testNamespace)
		firstDeleteBinding.Name = "delete-" + firstDeleteBinding.Name
		firstDeleteBinding.SetFinalizers([]string{customBindingFinalizer})
		Expect(k8sClient.Create(ctx, firstDeleteBinding)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, firstDeleteBinding)).Should(Succeed())
		secondDeleteBinding := generateResourceBinding(placementv1beta1.BindingStateUnscheduled, latestSnapshot.Name, clusters[2], testNamespace)
		secondDeleteBinding.Name = "delete-" + secondDeleteBinding.Name
		secondDeleteBinding.SetFinalizers([]string{customBindingFinalizer})
		Expect(k8sClient.Create(ctx, secondDeleteBinding)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, secondDeleteBinding)).Should(Succeed())
		By("Created 2 deleting bindings")
		// create the normal binding after the deleting one
		for _, binding := range bindings {
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
		}
		// check that no bindings are rolled out
		verifyBindingsNotRolledOutConsistently(controller.ConvertRBArrayToBindingObjs(bindings))
		By("Verified that the rollout is blocked")
		// now we remove the finalizer of the first deleting binding
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: firstDeleteBinding.GetName(), Namespace: firstDeleteBinding.GetNamespace()}, firstDeleteBinding)).Should(Succeed())
		firstDeleteBinding.SetFinalizers([]string{})
		Expect(k8sClient.Update(ctx, firstDeleteBinding)).Should(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: firstDeleteBinding.GetName(), Namespace: firstDeleteBinding.GetNamespace()}, firstDeleteBinding))
		}, timeout, interval).Should(BeTrue(), "the first deleting binding should now be deleted")
		By("Verified that the first deleting binding is deleted")
		// check that no bindings are rolled out
		verifyBindingsNotRolledOutConsistently(controller.ConvertRBArrayToBindingObjs(bindings))
		By("Verified that the rollout is still blocked")
		// now we remove the finalizer of the second deleting binding
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secondDeleteBinding.GetName(), Namespace: secondDeleteBinding.GetNamespace()}, secondDeleteBinding)).Should(Succeed())
		secondDeleteBinding.SetFinalizers([]string{})
		Expect(k8sClient.Update(ctx, secondDeleteBinding)).Should(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: secondDeleteBinding.GetName(), Namespace: secondDeleteBinding.GetNamespace()}, secondDeleteBinding))
		}, timeout, interval).Should(BeTrue(), "the second deleting binding should now be deleted")
		By("Verified that the second deleting binding is deleted")
		// Check that the bindings are rolledout.
		// When there is a binding assigned to a cluster with another deleting bindings, the controller
		// will wait until the deleting binding is deleted before it rolls out the bindings.
		// It requeues the bindings every 5 sceconds by checking waitForResourcesToCleanUp func.
		// Leave 5 seconds for the controller to requeue the bindings and roll them out.
		verifyBindingsRolledOut(controller.ConvertRBArrayToBindingObjs(bindings), latestSnapshot, 5*time.Second+timeout)
		By("Verified that the rollout is finally unblocked")
	})

	It("Should rollout both the old applied and failed to apply bound the new resources", func() {
		// create RP
		var targetCluster int32 = 5
		rolloutRP = resourcePlacementForTest(testNamespace, testRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetCluster),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutRP)).Should(Succeed())
		// create master resource snapshot that is latest
		masterSnapshot := generateResourceSnapshot(rolloutRP.Namespace, rolloutRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
		By(fmt.Sprintf("master resource snapshot %s created", masterSnapshot.Name))
		// create scheduled bindings for master snapshot on target clusters
		clusters := make([]string, targetCluster)
		for i := 0; i < int(targetCluster); i++ {
			clusters[i] = "cluster-" + strconv.Itoa(i)
			binding := generateResourceBinding(placementv1beta1.BindingStateScheduled, masterSnapshot.Name, clusters[i], testNamespace)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding %s created", binding.Name))
			bindings = append(bindings, binding)
		}
		// Check that all bindings are bound.
		verifyBindingsRolledOut(controller.ConvertRBArrayToBindingObjs(bindings), masterSnapshot, timeout)
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
			placementv1beta1.PlacementTrackingLabel: testRPName,
			placementv1beta1.IsLatestSnapshotLabel:  "false"},
		)
		Expect(k8sClient.Update(ctx, masterSnapshot)).Should(Succeed())
		// create a new master resource snapshot
		newMasterSnapshot := generateResourceSnapshot(rolloutRP.Namespace, rolloutRP.Name, 1, true)
		Expect(k8sClient.Create(ctx, newMasterSnapshot)).Should(Succeed())
		Eventually(func() bool {
			allMatch := true
			for _, binding := range bindings {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}, binding)
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

	It("should trigger binding rollout for resourceOverrideSnapshot but not clusterResourceOverrideSnapshot", func() {
		// Create a RP.
		targetClusterCount := int32(2)
		rolloutRP = resourcePlacementForTest(
			testNamespace, testRPName,
			createPlacementPolicyForTest(placementv1beta1.PickNPlacementType, targetClusterCount),
			createPlacementRolloutStrategyForTest(placementv1beta1.RollingUpdateRolloutStrategyType, generateDefaultRollingUpdateConfig(), nil))
		Expect(k8sClient.Create(ctx, rolloutRP)).Should(Succeed(), "Failed to create RP")

		// Create a master resource snapshot.
		resourceSnapshot := generateResourceSnapshot(rolloutRP.Namespace, rolloutRP.Name, 0, true)
		Expect(k8sClient.Create(ctx, resourceSnapshot)).Should(Succeed(), "Failed to create resource snapshot")

		// Create bindings.
		clusters := make([]string, targetClusterCount)
		for i := 0; i < int(targetClusterCount); i++ {
			clusters[i] = "cluster-" + utils.RandStr()
			binding := generateResourceBinding(placementv1beta1.BindingStateScheduled, resourceSnapshot.Name, clusters[i], testNamespace)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed(), "Failed to create resource binding")
			bindings = append(bindings, binding)

			memberCluster := generateMemberCluster(i, clusters[i])
			Expect(k8sClient.Create(ctx, memberCluster)).Should(Succeed(), "Failed to create member cluster")
		}

		// Verify that all the bindings are rolled out initially.
		verifyBindingsRolledOut(controller.ConvertRBArrayToBindingObjs(bindings), resourceSnapshot, timeout)

		// Mark the bindings to be available.
		for _, binding := range bindings {
			markBindingAvailable(binding, true)
		}

		// Create a clusterResourceOverrideSnapshot and a resourceOverrideSnapshot with cluster-scope placement and verify bindings are not updated.
		testCROName := "cro" + utils.RandStr()
		clusterResourceOverrideSnapshot := generateClusterResourceOverrideSnapshot(testCROName, testRPName)
		By(fmt.Sprintf("Creating cluster resource override snapshot %s", clusterResourceOverrideSnapshot.Name))
		Expect(k8sClient.Create(ctx, clusterResourceOverrideSnapshot)).Should(Succeed(), "Failed to create cluster resource override snapshot")

		testROName1 := "ro" + utils.RandStr()
		resourceOverrideSnapshot1 := generateResourceOverrideSnapshot(testROName1, testRPName, placementv1beta1.ClusterScoped)
		By(fmt.Sprintf("Creating resource override snapshot %s", resourceOverrideSnapshot1.Name))
		Expect(k8sClient.Create(ctx, resourceOverrideSnapshot1)).Should(Succeed(), "Failed to create resource override snapshot")

		// Verify bindings are NOT updated (rollout not triggered) by clusterResourceOverrideSnapshot.
		verifyBindingsNotUpdatedWithOverridesConsistently(controller.ConvertRBArrayToBindingObjs(bindings), nil, nil)

		// Create a resourceOverrideSnapshot and verify it triggers rollout.
		testROName2 := "ro" + utils.RandStr()
		resourceOverrideSnapshot2 := generateResourceOverrideSnapshot(testROName2, testRPName, placementv1beta1.NamespaceScoped)
		By(fmt.Sprintf("Creating resource override snapshot %s", resourceOverrideSnapshot2.Name))
		Expect(k8sClient.Create(ctx, resourceOverrideSnapshot2)).Should(Succeed(), "Failed to create resource override snapshot")

		waitUntilRolloutCompleted(controller.ConvertRBArrayToBindingObjs(bindings), nil, []placementv1beta1.NamespacedName{
			{Name: resourceOverrideSnapshot2.Name, Namespace: resourceOverrideSnapshot2.Namespace},
		})

		// Clean up the override snapshots.
		Expect(k8sClient.Delete(ctx, resourceOverrideSnapshot1)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, resourceOverrideSnapshot2)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, clusterResourceOverrideSnapshot)).Should(Succeed())

		// Clean up the member clusters.
		for _, cluster := range clusters {
			memberCluster := &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster,
				},
			}
			Expect(k8sClient.Delete(ctx, memberCluster)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
	})
})

func resourcePlacementForTest(namespace, rpName string, policy *placementv1beta1.PlacementPolicy, strategy placementv1beta1.RolloutStrategy) *placementv1beta1.ResourcePlacement {
	return &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rpName,
			Namespace: namespace,
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "v1",
					Version: "v1",
					Kind:    "ConfigMap",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
				},
			},
			Policy:   policy,
			Strategy: strategy,
		},
	}
}

func verifyBindingsNotRolledOutConsistently(bindings []placementv1beta1.BindingObj) {
	// Wait until the client informer is populated.
	Eventually(func() error {
		for _, binding := range bindings {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}, binding)
			if err != nil {
				return fmt.Errorf("failed to get binding %s/%s: %w", binding.GetNamespace(), binding.GetName(), err)
			}
		}
		return nil
	}, timeout, interval).Should(Succeed(), "make sure the cache is populated")
	// Check that none of the bindings is bound.
	Consistently(func() error {
		for _, binding := range bindings {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}, binding)
			if err != nil {
				return fmt.Errorf("failed to get binding %s/%s: %w", binding.GetNamespace(), binding.GetName(), err)
			}
			if binding.GetBindingSpec().State == placementv1beta1.BindingStateBound {
				return fmt.Errorf("binding %s/%s is in bound state, which is unwanted", binding.GetNamespace(), binding.GetName())
			}
		}
		return nil
	}, consistentTimeout, consistentInterval).Should(Succeed(), "rollout controller should not roll any binding to Bound state")
}

func verifyBindingsRolledOut(bindings []placementv1beta1.BindingObj, masterSnapshot placementv1beta1.ResourceSnapshotObj, timeout time.Duration) {
	// Check that all bindings are bound and updated to the latest snapshot.
	Eventually(func() error {
		for _, binding := range bindings {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}, binding)
			if err != nil {
				return fmt.Errorf("failed to get binding %s/%s: %w", binding.GetNamespace(), binding.GetName(), err)
			}
			if binding.GetBindingSpec().State != placementv1beta1.BindingStateBound {
				return fmt.Errorf("binding %s/%s is not updated to Bound state, got: %s", binding.GetNamespace(), binding.GetName(), binding.GetBindingSpec().State)
			}
			if binding.GetBindingSpec().ResourceSnapshotName != masterSnapshot.GetName() {
				return fmt.Errorf("binding %s/%s is not updated to the latest snapshot, got: %s, want: %s", binding.GetNamespace(), binding.GetName(),
					binding.GetBindingSpec().ResourceSnapshotName, masterSnapshot.GetName())
			}
		}
		return nil
	}, timeout, interval).Should(Succeed(), "rollout controller should roll out all the bindings")
}

func verifyBindingsNotUpdatedWithOverridesConsistently(
	bindings []placementv1beta1.BindingObj,
	wantClusterResourceOverrideSnapshots []string,
	wantResourceOverrideSnapshots []placementv1beta1.NamespacedName,
) {
	Consistently(func() error {
		for _, binding := range bindings {
			bindingKey := types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}
			if _, err := checkIfBindingUpdatedWithOverrides(bindingKey, wantClusterResourceOverrideSnapshots, wantResourceOverrideSnapshots); err != nil {
				return fmt.Errorf("binding %s should not be updated with overrides: %w", bindingKey, err)
			}
		}
		return nil
	}, consistentTimeout, interval).Should(Succeed(), "Bindings should not be updated with new overrides consistently")
}

func waitUntilRolloutCompleted(
	bindings []placementv1beta1.BindingObj,
	wantClusterResourceOverrideSnapshots []string,
	wantResourceOverrideSnapshots []placementv1beta1.NamespacedName,
) {
	notUpdatedBindings := make(map[types.NamespacedName]bool, len(bindings))
	for _, binding := range bindings {
		notUpdatedBindings[types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}] = true
	}

	for len(notUpdatedBindings) > 0 {
		// In each round, try to find a binding that has been updated and update it to available so rollout can proceed.
		var gotBinding placementv1beta1.BindingObj
		var err error
		Eventually(func() error {
			for bindingKey := range notUpdatedBindings {
				gotBinding, err = checkIfBindingUpdatedWithOverrides(bindingKey, wantClusterResourceOverrideSnapshots, wantResourceOverrideSnapshots)
				if err != nil {
					continue // current binding not updated yet, continue to check the next one.
				}
				delete(notUpdatedBindings, bindingKey)
				return nil // found an updated binding, can exit this round.
			}
			return fmt.Errorf("failed to find a binding with updated overrides")
		}, timeout, interval).Should(Succeed(), "One of the bindings should be updated with overrides")
		// Mark the binding as available so rollout can proceed.
		markBindingAvailable(gotBinding, true)
	}
}

func checkIfBindingUpdatedWithOverrides(
	bindingKey types.NamespacedName,
	wantClusterResourceOverrideSnapshots []string,
	wantResourceOverrideSnapshots []placementv1beta1.NamespacedName,
) (placementv1beta1.BindingObj, error) {
	var gotBinding placementv1beta1.BindingObj
	if bindingKey.Namespace == "" {
		gotBinding = &placementv1beta1.ClusterResourceBinding{}
	} else {
		gotBinding = &placementv1beta1.ResourceBinding{}
	}
	if err := k8sClient.Get(ctx, bindingKey, gotBinding); err != nil {
		return gotBinding, fmt.Errorf("failed to get binding %s: %w", bindingKey, err)
	}

	// Check that RolloutStarted condition is True.
	if !condition.IsConditionStatusTrue(gotBinding.GetCondition(string(placementv1beta1.ResourceBindingRolloutStarted)), gotBinding.GetGeneration()) {
		return gotBinding, fmt.Errorf("binding %s RolloutStarted condition is not True", bindingKey)
	}

	// Check that override snapshots in spec are the want ones.
	cmpOptions := []cmp.Option{
		cmpopts.EquateEmpty(),
		cmpopts.SortSlices(func(a, b string) bool { return a < b }),
		cmpopts.SortSlices(func(a, b placementv1beta1.NamespacedName) bool {
			if a.Namespace == b.Namespace {
				return a.Name < b.Name
			}
			return a.Namespace < b.Namespace
		}),
	}
	if !cmp.Equal(gotBinding.GetBindingSpec().ClusterResourceOverrideSnapshots, wantClusterResourceOverrideSnapshots, cmpOptions...) ||
		!cmp.Equal(gotBinding.GetBindingSpec().ResourceOverrideSnapshots, wantResourceOverrideSnapshots, cmpOptions...) {
		return gotBinding, fmt.Errorf("binding %s override snapshots mismatch: want %v and %v, got %v and %v", bindingKey,
			wantClusterResourceOverrideSnapshots, wantResourceOverrideSnapshots,
			gotBinding.GetBindingSpec().ClusterResourceOverrideSnapshots, gotBinding.GetBindingSpec().ResourceOverrideSnapshots)
	}
	return gotBinding, nil
}

func markBindingAvailable(binding placementv1beta1.BindingObj, trackable bool) {
	Eventually(func() error {
		reason := "trackable"
		if !trackable {
			reason = condition.WorkNotAllManifestsTrackableReason
		}
		binding.SetConditions(metav1.Condition{
			Type:               string(placementv1beta1.ResourceBindingAvailable),
			Status:             metav1.ConditionTrue,
			Reason:             reason,
			ObservedGeneration: binding.GetGeneration(),
		})
		if err := k8sClient.Status().Update(ctx, binding); err != nil {
			if apierrors.IsConflict(err) {
				// get the binding again to avoid conflict
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}, binding)).Should(Succeed())
			}
			return err
		}
		return nil
	}, timeout, interval).Should(Succeed(), "should update the binding status successfully")
	By(fmt.Sprintf("resource binding `%s/%s` is marked as available", binding.GetNamespace(), binding.GetName()))
}

func markBindingApplied(binding placementv1beta1.BindingObj, success bool) {
	applyCondition := metav1.Condition{
		Type: string(placementv1beta1.ResourceBindingApplied),
	}
	if success {
		applyCondition.Status = metav1.ConditionTrue
		applyCondition.Reason = "applySucceeded"
	} else {
		applyCondition.Status = metav1.ConditionFalse
		applyCondition.Reason = "applyFailed"
	}
	Eventually(func() error {
		applyCondition.ObservedGeneration = binding.GetGeneration()
		binding.SetConditions(applyCondition)
		if err := k8sClient.Status().Update(ctx, binding); err != nil {
			if apierrors.IsConflict(err) {
				// get the binding again to avoid conflict
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: binding.GetName(), Namespace: binding.GetNamespace()}, binding)).Should(Succeed())
			}
			return err
		}
		return nil
	}, timeout, interval).Should(Succeed(), "should update the binding status successfully")
	By(fmt.Sprintf("resource binding `%s/%s` is marked as applied with status %t", binding.GetNamespace(), binding.GetName(), success))
}

func generateClusterResourceBinding(state placementv1beta1.BindingState, resourceSnapshotName, targetCluster string) *placementv1beta1.ClusterResourceBinding {
	binding := &placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-" + resourceSnapshotName + "-" + targetCluster,
			Labels: map[string]string{
				placementv1beta1.PlacementTrackingLabel: testCRPName,
			},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:         state,
			TargetCluster: targetCluster,
		},
	}
	if binding.Spec.State == placementv1beta1.BindingStateBound {
		binding.Spec.ResourceSnapshotName = resourceSnapshotName
	}
	return binding
}

func generateResourceBinding(state placementv1beta1.BindingState, resourceSnapshotName, targetCluster, namespace string) *placementv1beta1.ResourceBinding {
	binding := &placementv1beta1.ResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "binding-" + resourceSnapshotName + "-" + targetCluster,
			Namespace: namespace,
			Labels: map[string]string{
				placementv1beta1.PlacementTrackingLabel: testRPName,
			},
		},
		Spec: placementv1beta1.ResourceBindingSpec{
			State:         state,
			TargetCluster: targetCluster,
		},
	}
	if binding.Spec.State == placementv1beta1.BindingStateBound {
		binding.Spec.ResourceSnapshotName = resourceSnapshotName
	}
	return binding
}

func generateClusterResourceSnapshot(testCRPName string, resourceIndex int, isLatest bool) *placementv1beta1.ClusterResourceSnapshot {
	clusterResourceSnapshot := &placementv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, testCRPName, resourceIndex),
			Labels: map[string]string{
				placementv1beta1.PlacementTrackingLabel: testCRPName,
				placementv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(isLatest),
			},
			Annotations: map[string]string{
				placementv1beta1.ResourceGroupHashAnnotation:         "hash",
				placementv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
			},
		},
	}
	rawContents := [][]byte{
		testResourceCRD, testNameSpace, testResource, testConfigMap, testPdb,
	}
	for _, rawContent := range rawContents {
		clusterResourceSnapshot.Spec.SelectedResources = append(clusterResourceSnapshot.Spec.SelectedResources,
			placementv1beta1.ResourceContent{
				RawExtension: runtime.RawExtension{Raw: rawContent},
			},
		)
	}
	return clusterResourceSnapshot
}

func generateResourceSnapshot(namespace, testRPName string, resourceIndex int, isLatest bool) *placementv1beta1.ResourceSnapshot {
	resourceSnapshot := &placementv1beta1.ResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, testRPName, resourceIndex),
			Namespace: namespace,
			Labels: map[string]string{
				placementv1beta1.PlacementTrackingLabel: testRPName,
				placementv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(isLatest),
			},
			Annotations: map[string]string{
				placementv1beta1.ResourceGroupHashAnnotation:         "hash",
				placementv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
			},
		},
	}
	rawContents := [][]byte{
		testConfigMap,
	}
	for _, rawContent := range rawContents {
		resourceSnapshot.Spec.SelectedResources = append(resourceSnapshot.Spec.SelectedResources,
			placementv1beta1.ResourceContent{
				RawExtension: runtime.RawExtension{Raw: rawContent},
			},
		)
	}
	return resourceSnapshot
}

func generateMemberCluster(idx int, clusterName string) *clusterv1beta1.MemberCluster {
	clusterLabels := map[string]string{
		"index": strconv.Itoa(idx),
	}
	return &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   clusterName,
			Labels: clusterLabels,
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Name:      "testUser",
				Kind:      "ServiceAccount",
				Namespace: utils.FleetSystemNamespace,
			},
			HeartbeatPeriodSeconds: 60,
		},
	}
}

func generateClusterResourceOverrideSnapshot(testCROName, testPlacementName string) *placementv1beta1.ClusterResourceOverrideSnapshot {
	return &placementv1beta1.ClusterResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, testCROName, 0),
			Labels: map[string]string{
				placementv1beta1.OverrideIndexLabel:    "0",
				placementv1beta1.IsLatestSnapshotLabel: "true",
				placementv1beta1.OverrideTrackingLabel: testCROName,
			},
		},
		Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
			OverrideHash: []byte("cluster-override-hash"),
			OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
				Placement: &placementv1beta1.PlacementRef{
					Name:  testPlacementName,
					Scope: placementv1beta1.ClusterScoped,
				},
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/labels/test",
									Value:    apiextensionsv1.JSON{Raw: []byte(`"test"`)},
								},
							},
						},
					},
				},
				ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
						Name:    "app", // from manifests/test_namespace.yaml
					},
				},
			},
		},
	}
}

func generateResourceOverrideSnapshot(testROName, testPlacementName string, scope placementv1beta1.ResourceScope) *placementv1beta1.ResourceOverrideSnapshot {
	return &placementv1beta1.ResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, testROName, 0),
			Namespace: testNamespace,
			Labels: map[string]string{
				placementv1beta1.OverrideIndexLabel:    "0",
				placementv1beta1.IsLatestSnapshotLabel: "true",
				placementv1beta1.OverrideTrackingLabel: testROName,
			},
		},
		Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
			OverrideHash: []byte("resource-override-hash"),
			OverrideSpec: placementv1beta1.ResourceOverrideSpec{
				Placement: &placementv1beta1.PlacementRef{
					Name:  testPlacementName,
					Scope: scope,
				},
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/labels/test",
									Value:    apiextensionsv1.JSON{Raw: []byte(`"test"`)},
								},
							},
						},
					},
				},
				ResourceSelectors: []placementv1beta1.ResourceSelector{
					{
						Group:   "",
						Version: "v1",
						Kind:    "ConfigMap",
						Name:    "test-configmap",
					},
				},
			},
		},
	}
}
