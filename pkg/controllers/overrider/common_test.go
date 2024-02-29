/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package overrider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/labels"
)

const (
	eventuallyTimeout    = time.Second * 10
	consistentlyDuration = time.Second * 5
	interval             = time.Millisecond * 250
)

func getClusterResourceOverrideSpec() fleetv1alpha1.ClusterResourceOverrideSpec {
	return fleetv1alpha1.ClusterResourceOverrideSpec{
		ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
			{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
		},
		Policy: &fleetv1alpha1.OverridePolicy{
			OverrideRules: []fleetv1alpha1.OverrideRule{
				{
					JSONPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
						{
							Operator: fleetv1alpha1.JSONPatchOverrideOpReplace,
							Path:     "spec.replica",
							Value:    "3",
						},
					},
				},
			},
		},
	}
}

func getClusterResourceOverride(testOverrideName string) *fleetv1alpha1.ClusterResourceOverride {
	return &fleetv1alpha1.ClusterResourceOverride{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testOverrideName,
			Finalizers: []string{fleetv1alpha1.OverrideFinalizer},
		},
		Spec: getClusterResourceOverrideSpec(),
	}
}

func getClusterResourceOverrideSnapshot(testOverrideName string, index int) *fleetv1alpha1.ClusterResourceOverrideSnapshot {
	return &fleetv1alpha1.ClusterResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(fleetv1alpha1.OverrideSnapshotNameFmt, testOverrideName, index),
			Labels: map[string]string{
				fleetv1alpha1.OverrideIndexLabel:    strconv.Itoa(index),
				fleetv1beta1.IsLatestSnapshotLabel:  "true",
				fleetv1alpha1.OverrideTrackingLabel: testOverrideName,
			},
		},
		Spec: fleetv1alpha1.ClusterResourceOverrideSnapshotSpec{
			OverrideHash: []byte("hash"),
			OverrideSpec: getClusterResourceOverrideSpec(),
		},
	}
}

var _ = Describe("Test ClusterResourceOverride common logic", func() {
	var cro *fleetv1alpha1.ClusterResourceOverride
	var (
		testCROName = "test-common"
	)

	BeforeEach(func() {
		// we cannot apply the CRO to the cluster as it will trigger the real reconcile loop.
		cro = getClusterResourceOverride(testCROName)
		By("Creating five clusterResourceOverrideSnapshot")
		for i := 0; i < 5; i++ {
			snapshot := getClusterResourceOverrideSnapshot(testCROName, i)
			Expect(k8sClient.Create(ctx, snapshot)).Should(Succeed())
		}
	})

	AfterEach(func() {
		By("Deleting five clusterResourceOverrideSnapshot")
		for i := 0; i < 5; i++ {
			snapshot := getClusterResourceOverrideSnapshot(testCROName, i)
			Expect(k8sClient.Delete(ctx, snapshot)).Should(SatisfyAny(Succeed(), &utils.NotFoundMatcher{}))
		}
	})

	Context("Test handle override deleting", func() {
		It("Should not do anything if there is no finalizer", func() {
			By("Removing the overrideFinalizer")
			controllerutil.RemoveFinalizer(cro, fleetv1alpha1.OverrideFinalizer)
			Expect(commonReconciler.handleOverrideDeleting(ctx, nil, cro)).Should(Succeed())
		})

		It("Should not fail if there is no snapshots associated with the cro yet", func() {
			By("verifying that it handles no snapshot cases")
			cro.Name = "another-cro" //there is no snapshot associated with this CRO
			// we cannot apply the CRO to the cluster as it will trigger the real reconcile loop so the update can only return APIServerError
			Expect(errors.Is(commonReconciler.handleOverrideDeleting(context.Background(), getClusterResourceOverrideSnapshot(testCROName, 0), cro), controller.ErrAPIServerError)).Should(BeTrue())
			// make sure that we don't delete other CRO's snapshot
			for i := 0; i < 5; i++ {
				snapshot := getClusterResourceOverrideSnapshot(testCROName, i)
				Consistently(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot)
				}, consistentlyDuration, interval).Should(Succeed(), "snapshot should not be deleted")
			}
		})

		It("Should delete all the snapshots if there is finalizer", func() {
			By("verifying that all snapshots are deleted")
			// we cannot apply the CRO to the cluster as it will trigger the real reconcile loop so the update can only return APIServerError
			Expect(errors.Is(commonReconciler.handleOverrideDeleting(context.Background(), getClusterResourceOverrideSnapshot(testCROName, 0), cro), controller.ErrAPIServerError)).Should(BeTrue())
			for i := 0; i < 5; i++ {
				snapshot := getClusterResourceOverrideSnapshot(testCROName, i)
				Eventually(func() bool {
					return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot))
				}, eventuallyTimeout, interval).Should(BeTrue(), "snapshot should be deleted")
			}
		})
	})

	Context("Test list sorted override snapshots", func() {
		It("Should list all the snapshots associated with the override", func() {
			snapshotList, err := commonReconciler.listSortedOverrideSnapshots(ctx, utils.ClusterResourceOverrideSnapshotKind, cro.GetName())
			Expect(err).Should(Succeed())
			By("verifying that all snapshots are listed and sorted")
			Expect(snapshotList.Items).Should(HaveLen(5))
			index := -1
			for i := 0; i < 5; i++ {
				snapshot := snapshotList.Items[i]
				newIndex, err := labels.ExtractIndex(&snapshot, fleetv1alpha1.OverrideIndexLabel)
				Expect(err).Should(Succeed())
				Expect(newIndex == index+1).Should(BeTrue())
				index = newIndex
			}
		})

		It("Should list all the snapshots associated with the override", func() {
			snapshotList, err := commonReconciler.listSortedOverrideSnapshots(ctx, utils.ClusterResourceOverrideSnapshotKind, cro.GetName())
			Expect(err).Should(Succeed())
			By("verifying that all snapshots are listed and sorted")
			Expect(snapshotList.Items).Should(HaveLen(5))
			index := -1
			for i := 0; i < 5; i++ {
				snapshot := snapshotList.Items[i]
				newIndex, err := labels.ExtractIndex(&snapshot, fleetv1alpha1.OverrideIndexLabel)
				Expect(err).Should(Succeed())
				Expect(newIndex > index).Should(BeTrue())
				index = newIndex
			}
		})
	})

	Context("Test remove extra override snapshots", func() {
		It("Should not remove any snapshots if we no snapshot", func() {
			snapshotList := &unstructured.UnstructuredList{
				Items: []unstructured.Unstructured{},
			}
			// we have 0 snapshots, and the limit is 1, so we should not remove any
			err := commonReconciler.removeExtraSnapshot(ctx, snapshotList, 1)
			Expect(err).Should(Succeed())
		})

		It("Should not remove any snapshots if we have not reached the limit", func() {
			snapshotList, err := commonReconciler.listSortedOverrideSnapshots(ctx, utils.ClusterResourceOverrideSnapshotKind, cro.GetName())
			Expect(err).Should(Succeed())
			// we have 5 snapshots, and the limit is 6, so we should not remove any
			err = commonReconciler.removeExtraSnapshot(ctx, snapshotList, 6)
			Expect(err).Should(Succeed())
			By("verifying that all the snapshots remain")
			for i := 0; i < 5; i++ {
				snapshot := getClusterResourceOverrideSnapshot(testCROName, i)
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot)
				}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should not be deleted")
			}
		})

		It("Should remove 1 extra snapshots if we just reach the limit", func() {
			snapshotList, err := commonReconciler.listSortedOverrideSnapshots(ctx, utils.ClusterResourceOverrideSnapshotKind, cro.GetName())
			Expect(err).Should(Succeed())
			// we have 5 snapshots, and the limit is 5, so we should remove one. This is the base case.
			err = commonReconciler.removeExtraSnapshot(ctx, snapshotList, 5)
			Expect(err).Should(Succeed())
			By("verifying that the oldest snapshot is removed")
			snapshot := getClusterResourceOverrideSnapshot(testCROName, 0)
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot))
			}, eventuallyTimeout, interval).Should(BeTrue(), "snapshot should be deleted")
			By("verifying that only the oldest snapshot is removed")
			for i := 1; i < 5; i++ {
				snapshot := getClusterResourceOverrideSnapshot(testCROName, i)
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot)
				}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should not be deleted")
			}
		})

		It("Should remove all extra snapshots if we overshoot the limit", func() {
			snapshotList, err := commonReconciler.listSortedOverrideSnapshots(ctx, utils.ClusterResourceOverrideSnapshotKind, cro.GetName())
			Expect(err).Should(Succeed())
			// we have 5 snapshots, and the limit is 2, so we should remove 4
			err = commonReconciler.removeExtraSnapshot(ctx, snapshotList, 2)
			Expect(err).Should(Succeed())
			By("verifying that the older snapshots are removed")
			for i := 0; i < 4; i++ {
				snapshot := getClusterResourceOverrideSnapshot(testCROName, 0)
				Eventually(func() bool {
					return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot))
				}, eventuallyTimeout, interval).Should(BeTrue(), "snapshot should be deleted")
			}
			By("verifying that only the latest snapshot is kept")
			snapshot := getClusterResourceOverrideSnapshot(testCROName, 4)
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot)
			}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should not be deleted")
		})
	})

	Context("Test remove extra override snapshots", func() {
		It("Should keep the latest label as true if it's already true", func() {
			snapshot := getClusterResourceOverrideSnapshot(testCROName, 0)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.GetName()}, snapshot)).Should(Succeed())
			Expect(commonReconciler.ensureSnapshotLatest(ctx, snapshot)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.GetName()}, snapshot)).Should(Succeed())
			diff := cmp.Diff(map[string]string{
				fleetv1alpha1.OverrideIndexLabel:    strconv.Itoa(0),
				fleetv1beta1.IsLatestSnapshotLabel:  "true",
				fleetv1alpha1.OverrideTrackingLabel: testCROName,
			}, snapshot.GetLabels())
			Expect(diff).Should(BeEmpty(), diff)
		})

		It("Should update the latest label as true if it was false", func() {
			By("update a snapshot to be not latest")
			snapshot := getClusterResourceOverrideSnapshot(testCROName, 0)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.GetName()}, snapshot)).Should(Succeed())
			snapshot.SetLabels(map[string]string{
				fleetv1alpha1.OverrideIndexLabel:    strconv.Itoa(0),
				fleetv1beta1.IsLatestSnapshotLabel:  "false",
				fleetv1alpha1.OverrideTrackingLabel: testCROName,
			})
			Expect(k8sClient.Update(ctx, snapshot)).Should(Succeed())
			Expect(commonReconciler.ensureSnapshotLatest(ctx, snapshot)).Should(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.GetName()}, snapshot)).Should(Succeed())
			diff := cmp.Diff(map[string]string{
				fleetv1alpha1.OverrideIndexLabel:    strconv.Itoa(0),
				fleetv1beta1.IsLatestSnapshotLabel:  "true",
				fleetv1alpha1.OverrideTrackingLabel: testCROName,
			}, snapshot.GetLabels())
			Expect(diff).Should(BeEmpty(), diff)
		})
	})
})
