/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workgenerator

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
)

var (
	memberClusterName string
	namespaceName     string
	testCRPName       string
)

var _ = Describe("Test clusterSchedulingPolicySnapshot Controller", func() {
	const (
		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("Test Bound ClusterResourceBinding", func() {
		BeforeEach(func() {
			memberClusterName = "cluster-" + utils.RandStr()
			testCRPName = "crp-" + utils.RandStr()
			namespaceName = fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			})).Should(Succeed())
			By(fmt.Sprintf("Cluster namespace %s created", memberClusterName))
		})

		AfterEach(func() {
			/*
				By("By deleting snapshot")
				Expect(k8sClient.Delete(ctx, createdSnapshot)).Should(Succeed())

				By("By checking snapshot")
				Eventually(func() bool {
					return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: testSnapshotName}, createdSnapshot))
				}, duration, interval).Should(BeTrue(), "snapshot should be deleted")

			*/
		})

		It("Should create the work in the target namespace with master resource snapshot only", func() {
			masterSnapshot := generateResourceSnapshot(0, 1, 0, [][]byte{
				testClonesetCRD, testNameSpace, testCloneset,
			})
			Expect(k8sClient.Create(ctx, masterSnapshot)).Should(Succeed())
			By(fmt.Sprintf("master resource snapshot  %s created", masterSnapshot.Name))

			binding := generateClusterResourceBinding(fleetv1beta1.BindingStateBound, masterSnapshot.Name, memberClusterName)
			Expect(k8sClient.Create(ctx, binding)).Should(Succeed())
			By(fmt.Sprintf("resource binding  %s created", binding.Name))
			work := v1alpha1.Work{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: masterSnapshot.Name, Namespace: namespaceName}, &work)
			}, timeout, interval).Should(Succeed(), "Failed to get the expected work in hub cluster")
			expectedManifest := []v1alpha1.Manifest{
				{
					RawExtension: runtime.RawExtension{Raw: testClonesetCRD},
				},
				{
					RawExtension: runtime.RawExtension{Raw: testNameSpace},
				},
				{
					RawExtension: runtime.RawExtension{Raw: testCloneset},
				},
			}
			diff := cmp.Diff(expectedManifest, work.Spec.Workload.Manifests)
			Expect(diff).Should(BeEmpty(), fmt.Sprintf("work spec(%s) mismatch (-want +got):\n%s", work.Name, diff))

			//TODO: check the work owner reference, labels, annotations
		})

		//TODO: check multiple resource snapshots leads to multiple works

		//TODO: check the work updates when the resource snapsot is updated in the binding

		//TODO: check the work updates when the resource snapsot is updated to have more resources in the binding

		//TODO: check the work updates when the resource snapsot is updated to have less resources in the binding
	})

	// TODO: check binding state change from bound to unselected

	// TODO: check binding state change from selected to bound

	// TODO: check binding deleting
})

func generateClusterResourceBinding(state fleetv1beta1.BindingState, resourceSnapshotName, targetCluster string) *fleetv1beta1.ClusterResourceBinding {
	return &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "resourcebinding-" + utils.RandStr(),
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

func generateResourceSnapshot(resourceIndex, numberResource, subIndex int, rawContents [][]byte) *fleetv1beta1.ClusterResourceSnapshot {
	snapshotName := fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, resourceIndex)
	if numberResource > 1 {
		snapshotName = fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, snapshotName, subIndex)
	}
	clusterResourceSnapshot := &fleetv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: snapshotName,
			Labels: map[string]string{
				fleetv1beta1.ResourceIndexLabel: strconv.Itoa(resourceIndex),
				fleetv1beta1.CRPTrackingLabel:   testCRPName,
			},
			Annotations: map[string]string{
				fleetv1beta1.NumberOfResourceSnapshotsAnnotation: strconv.Itoa(numberResource),
			},
		},
	}
	for _, rawContent := range rawContents {
		clusterResourceSnapshot.Spec.SelectedResources = append(clusterResourceSnapshot.Spec.SelectedResources, fleetv1beta1.ResourceContent{
			RawExtension: runtime.RawExtension{Raw: rawContent},
		})
	}
	return clusterResourceSnapshot
}
