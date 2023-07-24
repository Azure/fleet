/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package rollout

import (
	"fmt"
	"strconv"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	timeout  = time.Second * 10
	duration = time.Second * 20
	interval = time.Millisecond * 250
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
		By("Deleting ClusterResourceSnapshots")
		for _, resourceSnapshot := range resourceSnapshots {
			Expect(k8sClient.Delete(ctx, resourceSnapshot)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		}
		By("Deleting ClusterResourcePlacement")
		Expect(k8sClient.Delete(ctx, rolloutCRP)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
	})

	It("Should rollout all the selected bindings as soon as they are created", func() {
		// create CRP
		var targetCluster int32 = 10
		rolloutCRP = clusterResourcePlacementForTest(testCRPName, createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, targetCluster))
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

	It("Should rollout the selected and unselected bindings", func() {
		// create CRP
		var targetCluster int32 = 11
		rolloutCRP = clusterResourcePlacementForTest(testCRPName, createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, targetCluster))
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
				if binding.Spec.State != fleetv1beta1.BindingStateBound || binding.Spec.ResourceSnapshotName != masterSnapshot.Name {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")
		// simulate that some of the bindings are applied
		firstApplied := 3
		for i := 0; i < firstApplied; i++ {
			markBindingApplied(bindings[i])
		}
		// simulate another scheduling decision, pick some cluster to unselect from the bottom of the list
		var newTarget int32 = 9
		rolloutCRP.Spec.Policy.NumberOfClusters = &newTarget
		Expect(k8sClient.Update(ctx, rolloutCRP)).Should(Succeed())
		secondRoundBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0)
		deletedBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0)
		stillScheduled := 6
		for i := int(targetCluster - 1); i >= stillScheduled; i-- {
			binding := bindings[i]
			binding.Spec.State = fleetv1beta1.BindingStateUnscheduled
			Expect(k8sClient.Update(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding `%s` is marked as not scheduled", binding.Name))
			deletedBindings = append(deletedBindings, binding)
		}
		for i := 0; i < stillScheduled; i++ {
			secondRoundBindings = append(secondRoundBindings, bindings[i])
		}
		// simulate that some of the bindings are applied
		for i := firstApplied; i < int(newTarget); i++ {
			markBindingApplied(bindings[i])
		}
		newScheduled := int(newTarget) - stillScheduled
		for i := 0; i < newScheduled; i++ {
			binding := generateClusterResourceBinding(fleetv1beta1.BindingStateScheduled, masterSnapshot.Name, "cluster-"+strconv.Itoa(int(targetCluster)+i))
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			bindings = append(bindings, binding)
			secondRoundBindings = append(secondRoundBindings, binding)
		}
		// simulate that some of the bindings are applied
		for i := int(newTarget); i < int(targetCluster); i++ {
			markBindingApplied(bindings[i])
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
		}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")
		// simulate that the new bindings are applied
		for i := 0; i < len(secondRoundBindings); i++ {
			markBindingApplied(secondRoundBindings[i])
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
	})

	It("Should rollout both the new scheduling and the new resources", func() {
		// create CRP
		var targetCluster int32 = 11
		rolloutCRP = clusterResourcePlacementForTest(testCRPName, createPlacementPolicyForTest(fleetv1beta1.PickNPlacementType, targetCluster))
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
		// simulate that some of the bindings are applied
		firstApplied := 3
		for i := 0; i < firstApplied; i++ {
			markBindingApplied(bindings[i])
		}
		// simulate another scheduling decision, pick some cluster to unselect from the bottom of the list
		var newTarget int32 = 9
		rolloutCRP.Spec.Policy.NumberOfClusters = &newTarget
		Expect(k8sClient.Update(ctx, rolloutCRP)).Should(Succeed())
		secondRoundBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0)
		deletedBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0)
		stillScheduled := 6
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
		// simulate that some of the bindings are applied
		for i := firstApplied; i < int(newTarget); i++ {
			markBindingApplied(bindings[i])
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
		// simulate that some of the bindings are applied
		for i := int(newTarget); i < int(targetCluster); i++ {
			markBindingApplied(bindings[i])
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
		// simulate that the new bindings are applied
		for i := 0; i < len(secondRoundBindings); i++ {
			markBindingApplied(secondRoundBindings[i])
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
					// simulate the work generator to make the newly updated bindings to be applied
					markBindingApplied(binding)
				} else {
					misMatch = true
				}
			}
			return !misMatch
		}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to use the latest resource snapshot")
	})

	// TODO: should update scheduled bindings to the latest snapshot when it is updated to bound state.

	// TODO: should count the deleting bindings as can be Unavailable.

})

func markBindingApplied(binding *fleetv1beta1.ClusterResourceBinding) {
	binding.SetConditions(metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(fleetv1beta1.ResourceBindingApplied),
		Reason:             "applied",
		ObservedGeneration: binding.Generation,
	})
	Expect(k8sClient.Status().Update(ctx, binding)).Should(Succeed())
	By(fmt.Sprintf("resource binding `%s` is marked as applied", binding.Name))
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
		testClonesetCRD, testNameSpace, testCloneset, testConfigMap, testPdb,
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
