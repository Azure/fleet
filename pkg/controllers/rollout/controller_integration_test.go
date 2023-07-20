/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package rollout

import (
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
)

var testCRPName string

var _ = Describe("Test clusterSchedulingPolicySnapshot Controller", func() {
	const (
		timeout  = time.Second * 10
		duration = time.Second * 20
		interval = time.Millisecond * 250
	)

	var bindings []*fleetv1beta1.ClusterResourceBinding
	var resourceSnapshots []*fleetv1beta1.ClusterResourceSnapshot
	var rolloutCRP *fleetv1beta1.ClusterResourcePlacement

	Context("Test ", func() {
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

		FIt("Should rollout the selected bindings", func() {
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
					if binding.Spec.State != fleetv1beta1.BindingStateBound {
						return false
					}
				}
				return true
			}, timeout, interval).Should(BeTrue(), "rollout controller should roll all the bindings to Bound state")
		})
	})
})

func generateClusterResourceBinding(state fleetv1beta1.BindingState, resourceSnapshotName, targetCluster string) *fleetv1beta1.ClusterResourceBinding {
	return &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding-" + resourceSnapshotName + "-" + targetCluster,
			Labels: map[string]string{
				fleetv1beta1.CRPTrackingLabel: testCRPName,
			},
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			State:                state,
			ResourceSnapshotName: resourceSnapshotName,
			TargetCluster:        targetCluster,
		},
	}
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
