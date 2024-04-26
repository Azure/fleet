/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package overrider

import (
	"fmt"
	"strconv"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
)

func getResourceOverrideSpec() fleetv1alpha1.ResourceOverrideSpec {
	return fleetv1alpha1.ResourceOverrideSpec{
		ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
							Value:    apiextensionsv1.JSON{Raw: []byte("3")},
						},
					},
				},
			},
		},
	}
}

func getResourceOverride(testOverrideName string) *fleetv1alpha1.ResourceOverride {
	return &fleetv1alpha1.ResourceOverride{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testOverrideName,
			Namespace: "default",
		},
		Spec: getResourceOverrideSpec(),
	}
}

func getResourceOverrideSnapshot(testOverrideName string, index int) *fleetv1alpha1.ResourceOverrideSnapshot {
	return &fleetv1alpha1.ResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(fleetv1alpha1.OverrideSnapshotNameFmt, testOverrideName, index),
			Namespace: "default",
			Labels: map[string]string{
				fleetv1alpha1.OverrideIndexLabel:    strconv.Itoa(index),
				fleetv1beta1.IsLatestSnapshotLabel:  "true",
				fleetv1alpha1.OverrideTrackingLabel: testOverrideName,
			},
		},
		Spec: fleetv1alpha1.ResourceOverrideSnapshotSpec{
			OverrideHash: []byte("hash"),
			OverrideSpec: getResourceOverrideSpec(),
		},
	}
}

var _ = Describe("Test ResourceOverride controller logic", func() {
	var ro *fleetv1alpha1.ResourceOverride
	var testROName string

	BeforeEach(func() {
		testROName = fmt.Sprintf("test-ro-%s", utils.RandStr())
		ro = getResourceOverride(testROName)
	})

	AfterEach(func() {
		By("Deleting the RO")
		Expect(k8sClient.Delete(ctx, ro)).Should(SatisfyAny(Succeed(), &utils.NotFoundMatcher{}))
	})

	It("Test create a new resourceOverride should result in one new snapshot", func() {
		By("Creating a new RO")
		Expect(k8sClient.Create(ctx, ro)).Should(Succeed())
		By("Checking if the finalizer is added to the RO")
		Eventually(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: ro.Name, Namespace: ro.Namespace}, ro); err != nil {
				return err
			}
			if !controllerutil.ContainsFinalizer(ro, fleetv1alpha1.OverrideFinalizer) {
				return fmt.Errorf("finalizer not added")
			}
			return nil
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should have finalizer")

		By("Checking if a new snapshot is created")
		snapshot := getResourceOverrideSnapshot(testROName, 0) //index starts from 0
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")
		By("Checking if the label is correct")
		diff := cmp.Diff(map[string]string{
			fleetv1alpha1.OverrideIndexLabel:    strconv.Itoa(0),
			fleetv1beta1.IsLatestSnapshotLabel:  "true",
			fleetv1alpha1.OverrideTrackingLabel: testROName,
		}, snapshot.GetLabels())
		Expect(diff).Should(BeEmpty(), "Snapshot label mismatch (-want, +got)")
		By("Checking if the spec is correct")
		diff = cmp.Diff(ro.Spec, snapshot.Spec.OverrideSpec)
		Expect(diff).Should(BeEmpty(), "Snapshot spec mismatch (-want, +got)")
		By("Make sure no other snapshot is created")
		snapshot = getResourceOverrideSnapshot(testROName, 1)
		Consistently(func() bool {
			return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot))
		}, consistentlyDuration, interval).Should(BeTrue(), "snapshot should not exist")
	})

	It("Should create another new snapshot when a RO is updated", func() {
		By("Creating a new RO")
		Expect(k8sClient.Create(ctx, ro)).Should(Succeed())

		By("Waiting for a new snapshot is created")
		snapshot := getResourceOverrideSnapshot(testROName, 0)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")

		By("Updating an existing RO")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ro.Name, Namespace: ro.Namespace}, ro)).Should(Succeed())
		ro.Spec.Policy = &fleetv1alpha1.OverridePolicy{
			OverrideRules: []fleetv1alpha1.OverrideRule{
				{
					JSONPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
						{
							Operator: fleetv1alpha1.JSONPatchOverrideOpRemove,
							Path:     "spec.replica",
						},
					},
				},
			},
		}
		Expect(k8sClient.Update(ctx, ro)).Should(Succeed())
		By("Checking if a new snapshot is created")
		snapshot = getResourceOverrideSnapshot(testROName, 1)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")
		By("Checking if the label is correct")
		diff := cmp.Diff(map[string]string{
			fleetv1alpha1.OverrideIndexLabel:    strconv.Itoa(1),
			fleetv1beta1.IsLatestSnapshotLabel:  "true",
			fleetv1alpha1.OverrideTrackingLabel: testROName,
		}, snapshot.GetLabels())
		Expect(diff).Should(BeEmpty(), diff, "Snapshot label mismatch (-want, +got)")
		By("Checking if the spec is correct")
		diff = cmp.Diff(ro.Spec, snapshot.Spec.OverrideSpec)
		Expect(diff).Should(BeEmpty(), diff, "Snapshot spec mismatch (-want, +got)")
		By("Checking if the old snapshot is updated")
		snapshot = getResourceOverrideSnapshot(testROName, 0)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)).Should(Succeed())
		By("Make sure the old snapshot is correctly marked as not latest")
		diff = cmp.Diff(map[string]string{
			fleetv1alpha1.OverrideIndexLabel:    strconv.Itoa(0),
			fleetv1beta1.IsLatestSnapshotLabel:  "false",
			fleetv1alpha1.OverrideTrackingLabel: testROName,
		}, snapshot.GetLabels())
		Expect(diff).Should(BeEmpty(), diff, "Snapshot label mismatch (-want, +got)")
		By("Make sure the old snapshot spec is not the same as the current RO")
		diff = cmp.Diff(ro.Spec, snapshot.Spec.OverrideSpec)
		Expect(diff).ShouldNot(BeEmpty(), diff, "Snapshot spec mismatch (-want, +got)")
	})

	It("Should delete all snapshots when a Resource override is deleted", func() {
		By("Creating a new RO")
		Expect(k8sClient.Create(ctx, ro)).Should(Succeed())
		By("Waiting for a new snapshot is created")
		snapshot := getResourceOverrideSnapshot(testROName, 0)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")
		By("Updating an existing RO")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ro.Name, Namespace: ro.Namespace}, ro)).Should(Succeed())
		ro.Spec.Policy = &fleetv1alpha1.OverridePolicy{
			OverrideRules: []fleetv1alpha1.OverrideRule{
				{
					JSONPatchOverrides: []fleetv1alpha1.JSONPatchOverride{
						{
							Operator: fleetv1alpha1.JSONPatchOverrideOpRemove,
							Path:     "spec.replica",
						},
					},
				},
			},
		}
		Expect(k8sClient.Update(ctx, ro)).Should(Succeed())
		By("Checking if a new snapshot is created")
		snapshot = getResourceOverrideSnapshot(testROName, 1)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")
		By("Deleting the existing RO")
		Expect(k8sClient.Delete(ctx, ro)).Should(Succeed())
		By("Make sure the RO is deleted")
		Eventually(func() bool {
			return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: ro.Name, Namespace: ro.Namespace}, ro))
		}, eventuallyTimeout, interval).Should(BeTrue(), "ro should be deleted")
		By("Make sure all snapshots are deleted")
		Consistently(func() bool {
			snapshots := &fleetv1alpha1.ResourceOverrideSnapshotList{}
			err := k8sClient.List(ctx, snapshots, client.InNamespace(snapshot.Namespace), client.MatchingLabels{fleetv1alpha1.OverrideTrackingLabel: testROName})
			return err == nil && len(snapshots.Items) == 0
		}, consistentlyDuration, interval).Should(BeTrue(), "snapshot should be deleted")
	})
})
