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

package overrider

import (
	"fmt"
	"strconv"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

func getResourceOverrideSpec() placementv1beta1.ResourceOverrideSpec {
	return placementv1beta1.ResourceOverrideSpec{
		ResourceSelectors: []placementv1beta1.ResourceSelector{
			{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
		},
		Policy: &placementv1beta1.OverridePolicy{
			OverrideRules: []placementv1beta1.OverrideRule{
				{
					JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
						{
							Operator: placementv1beta1.JSONPatchOverrideOpReplace,
							Path:     "spec.replica",
							Value:    apiextensionsv1.JSON{Raw: []byte("3")},
						},
					},
				},
			},
		},
	}
}

func getResourceOverride(testOverrideName, namespaceName string) *placementv1beta1.ResourceOverride {
	return &placementv1beta1.ResourceOverride{
		TypeMeta: metav1.TypeMeta{
			APIVersion: placementv1beta1.GroupVersion.String(),
			Kind:       placementv1beta1.ResourceOverrideKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testOverrideName,
			Namespace: namespaceName,
		},
		Spec: getResourceOverrideSpec(),
	}
}

func getResourceOverrideSnapshot(testOverrideName, namespaceName string, index int) *placementv1beta1.ResourceOverrideSnapshot {
	return &placementv1beta1.ResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(placementv1beta1.OverrideSnapshotNameFmt, testOverrideName, index),
			Namespace: namespaceName,
			Labels: map[string]string{
				placementv1beta1.OverrideIndexLabel:    strconv.Itoa(index),
				placementv1beta1.IsLatestSnapshotLabel: "true",
				placementv1beta1.OverrideTrackingLabel: testOverrideName,
			},
		},
		Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
			OverrideHash: []byte("hash"),
			OverrideSpec: getResourceOverrideSpec(),
		},
	}
}

var _ = Describe("Test ResourceOverride controller logic", func() {
	var ro *placementv1beta1.ResourceOverride
	var testROName string
	var namespaceName string

	BeforeEach(func() {
		namespaceName = fmt.Sprintf("%s-%s", overrideNamespace, utils.RandStr())
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}
		Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())
		testROName = fmt.Sprintf("test-ro-%s", utils.RandStr())
		ro = getResourceOverride(testROName, namespaceName)
	})

	AfterEach(func() {
		By("Deleting the RO")
		Expect(k8sClient.Delete(ctx, ro)).Should(SatisfyAny(Succeed(), &utils.NotFoundMatcher{}))
		By("Deleting the namespace")
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}
		Expect(k8sClient.Delete(ctx, namespace)).Should(SatisfyAny(Succeed(), &utils.NotFoundMatcher{}))
	})

	It("Test create a new resourceOverride should result in one new snapshot", func() {
		By("Creating a new RO")
		Expect(k8sClient.Create(ctx, ro)).Should(Succeed())
		By("Checking if the finalizer is added to the RO")
		Eventually(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: ro.Name, Namespace: ro.Namespace}, ro); err != nil {
				return err
			}
			if !controllerutil.ContainsFinalizer(ro, placementv1beta1.OverrideFinalizer) {
				return fmt.Errorf("finalizer not added")
			}
			return nil
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should have finalizer")

		By("Checking if a new snapshot is created")
		snapshot := getResourceOverrideSnapshot(testROName, namespaceName, 0) //index starts from 0
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")
		By("Checking if the label is correct")
		diff := cmp.Diff(map[string]string{
			placementv1beta1.OverrideIndexLabel:    strconv.Itoa(0),
			placementv1beta1.IsLatestSnapshotLabel: "true",
			placementv1beta1.OverrideTrackingLabel: testROName,
		}, snapshot.GetLabels())
		Expect(diff).Should(BeEmpty(), "Snapshot label mismatch (-want, +got)")
		By("Checking if the spec is correct")
		diff = cmp.Diff(ro.Spec, snapshot.Spec.OverrideSpec)
		Expect(diff).Should(BeEmpty(), "Snapshot spec mismatch (-want, +got)")
		By("Make sure no other snapshot is created")
		snapshot = getResourceOverrideSnapshot(testROName, namespaceName, 1)
		Consistently(func() bool {
			return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot))
		}, consistentlyDuration, interval).Should(BeTrue(), "snapshot should not exist")
	})

	It("Should create another new snapshot when a RO is updated", func() {
		By("Creating a new RO")
		Expect(k8sClient.Create(ctx, ro)).Should(Succeed())

		By("Waiting for a new snapshot is created")
		snapshot := getResourceOverrideSnapshot(testROName, namespaceName, 0)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")

		By("Updating an existing RO")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ro.Name, Namespace: ro.Namespace}, ro)).Should(Succeed())
		ro.Spec.Policy = &placementv1beta1.OverridePolicy{
			OverrideRules: []placementv1beta1.OverrideRule{
				{
					JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
						{
							Operator: placementv1beta1.JSONPatchOverrideOpRemove,
							Path:     "spec.replica",
						},
					},
				},
			},
		}
		Expect(k8sClient.Update(ctx, ro)).Should(Succeed())
		By("Checking if a new snapshot is created")
		snapshot = getResourceOverrideSnapshot(testROName, namespaceName, 1)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")
		By("Checking if the label is correct")
		diff := cmp.Diff(map[string]string{
			placementv1beta1.OverrideIndexLabel:    strconv.Itoa(1),
			placementv1beta1.IsLatestSnapshotLabel: "true",
			placementv1beta1.OverrideTrackingLabel: testROName,
		}, snapshot.GetLabels())
		Expect(diff).Should(BeEmpty(), diff, "Snapshot label mismatch (-want, +got)")
		By("Checking if the spec is correct")
		diff = cmp.Diff(ro.Spec, snapshot.Spec.OverrideSpec)
		Expect(diff).Should(BeEmpty(), diff, "Snapshot spec mismatch (-want, +got)")
		By("Checking if the old snapshot is updated")
		snapshot = getResourceOverrideSnapshot(testROName, namespaceName, 0)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)).Should(Succeed())
		By("Make sure the old snapshot is correctly marked as not latest")
		diff = cmp.Diff(map[string]string{
			placementv1beta1.OverrideIndexLabel:    strconv.Itoa(0),
			placementv1beta1.IsLatestSnapshotLabel: "false",
			placementv1beta1.OverrideTrackingLabel: testROName,
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
		snapshot := getResourceOverrideSnapshot(testROName, namespaceName, 0)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")
		By("Updating an existing RO")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ro.Name, Namespace: ro.Namespace}, ro)).Should(Succeed())
		ro.Spec.Policy = &placementv1beta1.OverridePolicy{
			OverrideRules: []placementv1beta1.OverrideRule{
				{
					JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
						{
							Operator: placementv1beta1.JSONPatchOverrideOpRemove,
							Path:     "spec.replica",
						},
					},
				},
			},
		}
		Expect(k8sClient.Update(ctx, ro)).Should(Succeed())
		By("Checking if a new snapshot is created")
		snapshot = getResourceOverrideSnapshot(testROName, namespaceName, 1)
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
			snapshots := &placementv1beta1.ResourceOverrideSnapshotList{}
			err := k8sClient.List(ctx, snapshots, client.InNamespace(snapshot.Namespace), client.MatchingLabels{placementv1beta1.OverrideTrackingLabel: testROName})
			return err == nil && len(snapshots.Items) == 0
		}, consistentlyDuration, interval).Should(BeTrue(), "snapshot should be deleted")
	})
})
