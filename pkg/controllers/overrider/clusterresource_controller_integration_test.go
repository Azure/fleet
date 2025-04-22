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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1alpha1"
	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

func getClusterResourceOverrideSpec() fleetv1alpha1.ClusterResourceOverrideSpec {
	return fleetv1alpha1.ClusterResourceOverrideSpec{
		ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
			{
				Group:   "",
				Version: "v1",
				Kind:    "Namespace",
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

func getClusterResourceOverride(testOverrideName string) *fleetv1alpha1.ClusterResourceOverride {
	return &fleetv1alpha1.ClusterResourceOverride{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fleetv1alpha1.GroupVersion.String(),
			Kind:       fleetv1alpha1.ClusterResourceOverrideKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: testOverrideName,
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

var _ = Describe("Test ClusterResourceOverride controller logic", func() {
	var cro *fleetv1alpha1.ClusterResourceOverride
	var testCROName string

	BeforeEach(func() {
		testCROName = fmt.Sprintf("test-cro-%s", utils.RandStr())
		cro = getClusterResourceOverride(testCROName)
	})

	AfterEach(func() {
		By("Deleting the CRO")
		Expect(k8sClient.Delete(ctx, cro)).Should(SatisfyAny(Succeed(), &utils.NotFoundMatcher{}))
	})

	It("Test create a new CRO should result in one new snapshot", func() {
		By("Creating a new CRO")
		Expect(k8sClient.Create(ctx, cro)).Should(Succeed())
		By("Checking if the finalizer is added to the CRO")
		Eventually(func() error {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: cro.Name}, cro)
			if err != nil {
				return err
			}
			if !controllerutil.ContainsFinalizer(cro, fleetv1alpha1.OverrideFinalizer) {
				return fmt.Errorf("finalizer not added")
			}
			return nil
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should have finalizer")

		By("Checking if a new snapshot is created")
		snapshot := getClusterResourceOverrideSnapshot(testCROName, 0) //index starts from 0
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")
		By("Checking if the label is correct")
		diff := cmp.Diff(map[string]string{
			fleetv1alpha1.OverrideIndexLabel:    strconv.Itoa(0),
			fleetv1beta1.IsLatestSnapshotLabel:  "true",
			fleetv1alpha1.OverrideTrackingLabel: testCROName,
		}, snapshot.GetLabels())
		Expect(diff).Should(BeEmpty(), "Snapshot label mismatch (-want, +got)")
		By("Checking if the spec is correct")
		diff = cmp.Diff(cro.Spec, snapshot.Spec.OverrideSpec)
		Expect(diff).Should(BeEmpty(), "Snapshot spec mismatch (-want, +got)")
		By("Make sure no other snapshot is created")
		snapshot = getClusterResourceOverrideSnapshot(testCROName, 1)
		Consistently(func() bool {
			return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot))
		}, consistentlyDuration, interval).Should(BeTrue(), "snapshot should not exist")
	})

	It("Should create another new snapshot when a CRO is updated", func() {
		By("Creating a new CRO")
		Expect(k8sClient.Create(ctx, cro)).Should(Succeed())

		By("Waiting for a new snapshot is created")
		snapshot := getClusterResourceOverrideSnapshot(testCROName, 0)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")

		By("Updating an existing CRO")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cro.Name}, cro)).Should(Succeed())
		cro.Spec.Policy = &fleetv1alpha1.OverridePolicy{
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
		Expect(k8sClient.Update(ctx, cro)).Should(Succeed())
		By("Checking if a new snapshot is created")
		snapshot = getClusterResourceOverrideSnapshot(testCROName, 1)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")
		By("Checking if the label is correct")
		diff := cmp.Diff(map[string]string{
			fleetv1alpha1.OverrideIndexLabel:    strconv.Itoa(1),
			fleetv1beta1.IsLatestSnapshotLabel:  "true",
			fleetv1alpha1.OverrideTrackingLabel: testCROName,
		}, snapshot.GetLabels())
		Expect(diff).Should(BeEmpty(), diff, "Snapshot label mismatch (-want, +got)")
		By("Checking if the spec is correct")
		diff = cmp.Diff(cro.Spec, snapshot.Spec.OverrideSpec)
		Expect(diff).Should(BeEmpty(), diff, "Snapshot spec mismatch (-want, +got)")
		By("Checking if the old snapshot is updated")
		snapshot = getClusterResourceOverrideSnapshot(testCROName, 0)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot)).Should(Succeed())
		By("Make sure the old snapshot is correctly marked as not latest")
		diff = cmp.Diff(map[string]string{
			fleetv1alpha1.OverrideIndexLabel:    strconv.Itoa(0),
			fleetv1beta1.IsLatestSnapshotLabel:  "false",
			fleetv1alpha1.OverrideTrackingLabel: testCROName,
		}, snapshot.GetLabels())
		Expect(diff).Should(BeEmpty(), diff, "Snapshot label mismatch (-want, +got)")
		By("Make sure the old snapshot spec is not the same as the current CRO")
		diff = cmp.Diff(cro.Spec, snapshot.Spec.OverrideSpec)
		Expect(diff).ShouldNot(BeEmpty(), diff, "Snapshot spec mismatch (-want, +got)")
	})

	It("Should delete all snapshots when a CRO is deleted", func() {
		By("Creating a new CRO")
		Expect(k8sClient.Create(ctx, cro)).Should(Succeed())
		By("Waiting for a new snapshot is created")
		snapshot := getClusterResourceOverrideSnapshot(testCROName, 0)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")
		By("Updating an existing CRO")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cro.Name}, cro)).Should(Succeed())
		cro.Spec.Policy = &fleetv1alpha1.OverridePolicy{
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
		Expect(k8sClient.Update(ctx, cro)).Should(Succeed())
		By("Checking if a new snapshot is created")
		snapshot = getClusterResourceOverrideSnapshot(testCROName, 1)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot)
		}, eventuallyTimeout, interval).Should(Succeed(), "snapshot should exist")
		By("Deleting the existing CRO")
		Expect(k8sClient.Delete(ctx, cro)).Should(Succeed())
		By("Make sure the CRO is deleted")
		Eventually(func() bool {
			return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: cro.Name}, cro))
		}, eventuallyTimeout, interval).Should(BeTrue(), "cro should be deleted")
		By("Make sure all snapshots are deleted")
		for i := 0; i < 2; i++ {
			snapshot := getClusterResourceOverrideSnapshot(testCROName, i)
			Consistently(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot))
			}, consistentlyDuration, interval).Should(BeTrue(), "snapshot should be deleted")
		}
	})
})
