/*
Copyright 2021 The Kubernetes Authors.

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

package workv1alpha1

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	testv1alpha1 "github.com/kubefleet-dev/kubefleet/test/apis/v1alpha1"
)

var _ = Describe("Work Status Reconciler", func() {
	var resourceNamespace string
	var workNamespace string
	var work *workv1alpha1.Work
	var cm, cm2 *corev1.ConfigMap

	var wns corev1.Namespace
	var rns corev1.Namespace

	BeforeEach(func() {
		workNamespace = "cluster-" + utilrand.String(5)
		resourceNamespace = utilrand.String(5)

		wns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: workNamespace,
			},
		}
		err := k8sClient.Create(context.Background(), &wns)
		Expect(err).ToNot(HaveOccurred())

		rns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourceNamespace,
			},
		}
		err = k8sClient.Create(context.Background(), &rns)
		Expect(err).ToNot(HaveOccurred())

		// Create the Work object with some type of Manifest resource.
		cm = &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "configmap-" + utilrand.String(5),
				Namespace: resourceNamespace,
			},
			Data: map[string]string{
				"test": "test",
			},
		}
		cm2 = &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "configmap2-" + utilrand.String(5),
				Namespace: resourceNamespace,
			},
			Data: map[string]string{
				"test": "test",
			},
		}

		By("Create work that contains two configMaps")
		work = &workv1alpha1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "work-" + utilrand.String(5),
				Namespace: workNamespace,
			},
			Spec: workv1alpha1.WorkSpec{
				Workload: workv1alpha1.WorkloadTemplate{
					Manifests: []workv1alpha1.Manifest{
						{
							RawExtension: runtime.RawExtension{Object: cm},
						},
						{
							RawExtension: runtime.RawExtension{Object: cm2},
						},
					},
				},
			},
		}
	})

	AfterEach(func() {
		// TODO: Ensure that all resources are being deleted.
		Expect(k8sClient.Delete(context.Background(), work)).Should(Succeed())
		Expect(k8sClient.Delete(context.Background(), &wns)).Should(Succeed())
		Expect(k8sClient.Delete(context.Background(), &rns)).Should(Succeed())
	})

	It("Should delete the manifest from the member cluster after it is removed from work", func() {
		By("Apply the work")
		Expect(k8sClient.Create(context.Background(), work)).ToNot(HaveOccurred())

		By("Make sure that the work is applied")
		currentWork := waitForWorkToApply(work.Name, workNamespace)
		var appliedWork workv1alpha1.AppliedWork
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
		Expect(len(appliedWork.Status.AppliedResources)).Should(Equal(2))

		By("Remove configMap 2 from the work")
		currentWork.Spec.Workload.Manifests = []workv1alpha1.Manifest{
			{
				RawExtension: runtime.RawExtension{Object: cm},
			},
		}
		Expect(k8sClient.Update(context.Background(), currentWork)).Should(Succeed())

		By("Verify that the resource is removed from the cluster")
		Eventually(func() bool {
			var configMap corev1.ConfigMap
			return apierrors.IsNotFound(k8sClient.Get(context.Background(), types.NamespacedName{Name: cm2.Name, Namespace: resourceNamespace}, &configMap))
		}, timeout, interval).Should(BeTrue())

		By("Verify that the appliedWork status is correct")
		Eventually(func() bool {
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
			return len(appliedWork.Status.AppliedResources) == 1
		}, timeout, interval).Should(BeTrue())
		Expect(appliedWork.Status.AppliedResources[0].Name).Should(Equal(cm.GetName()))
		Expect(appliedWork.Status.AppliedResources[0].Namespace).Should(Equal(cm.GetNamespace()))
		Expect(appliedWork.Status.AppliedResources[0].Version).Should(Equal(cm.GetObjectKind().GroupVersionKind().Version))
		Expect(appliedWork.Status.AppliedResources[0].Group).Should(Equal(cm.GetObjectKind().GroupVersionKind().Group))
		Expect(appliedWork.Status.AppliedResources[0].Kind).Should(Equal(cm.GetObjectKind().GroupVersionKind().Kind))
	})

	It("Should delete the shared manifest from the member cluster after it is removed from all works", func() {
		By("Create another work that contains configMap 2")
		work2 := work.DeepCopy()
		work2.Name = "work-" + utilrand.String(5)
		Expect(k8sClient.Create(context.Background(), work)).ToNot(HaveOccurred())
		Expect(k8sClient.Create(context.Background(), work2)).ToNot(HaveOccurred())

		By("Make sure that the appliedWork is updated")
		var appliedWork, appliedWork2 workv1alpha1.AppliedWork
		Eventually(func() bool {
			err := k8sClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)
			if err != nil {
				return false
			}
			return len(appliedWork.Status.AppliedResources) == 2
		}, timeout, interval).Should(BeTrue())

		By("Make sure that the appliedWork2 is updated")
		Eventually(func() bool {
			err := k8sClient.Get(context.Background(), types.NamespacedName{Name: work2.Name}, &appliedWork2)
			if err != nil {
				return false
			}
			return len(appliedWork2.Status.AppliedResources) == 2
		}, timeout, interval).Should(BeTrue())

		By("Remove configMap 2 from the work")
		currentWork := waitForWorkToApply(work.Name, workNamespace)
		currentWork.Spec.Workload.Manifests = []workv1alpha1.Manifest{
			{
				RawExtension: runtime.RawExtension{Object: cm},
			},
		}
		Expect(k8sClient.Update(context.Background(), currentWork)).Should(Succeed())
		currentWork = waitForWorkToApply(work.Name, workNamespace)
		Expect(len(currentWork.Status.ManifestConditions)).Should(Equal(1))

		By("Verify that configMap 2 is removed from the appliedWork")
		Eventually(func() bool {
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
			return len(appliedWork.Status.AppliedResources) == 1
		}, timeout, interval).Should(BeTrue())

		By("Verify that configMap 2 is not removed from the cluster")
		Consistently(func() bool {
			var configMap corev1.ConfigMap
			return k8sClient.Get(context.Background(), types.NamespacedName{Name: cm2.Name, Namespace: resourceNamespace}, &configMap) == nil
		}, timeout, interval).Should(BeTrue())

		By("Remove configMap 2 from the work2")
		currentWork = waitForWorkToApply(work2.Name, workNamespace)
		currentWork.Spec.Workload.Manifests = []workv1alpha1.Manifest{
			{
				RawExtension: runtime.RawExtension{Object: cm},
			},
		}
		Expect(k8sClient.Update(context.Background(), currentWork)).Should(Succeed())
		currentWork = waitForWorkToApply(work.Name, workNamespace)
		Expect(len(currentWork.Status.ManifestConditions)).Should(Equal(1))

		By("Verify that the resource is removed from the appliedWork")
		Eventually(func() bool {
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: work2.Name}, &appliedWork2)).Should(Succeed())
			return len(appliedWork2.Status.AppliedResources) == 1
		}, timeout, interval).Should(BeTrue())

		By("Verify that the cm2 is removed from the cluster")
		Eventually(func() bool {
			var configMap corev1.ConfigMap
			return apierrors.IsNotFound(k8sClient.Get(context.Background(), types.NamespacedName{Name: cm2.Name, Namespace: resourceNamespace}, &configMap))
		}, timeout, interval).Should(BeTrue())
	})

	It("Should delete the manifest from the member cluster even if there is apply failure", func() {
		By("Apply the work")
		Expect(k8sClient.Create(context.Background(), work)).ToNot(HaveOccurred())

		By("Make sure that the work is applied")
		currentWork := waitForWorkToApply(work.Name, workNamespace)
		var appliedWork workv1alpha1.AppliedWork
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
		Expect(len(appliedWork.Status.AppliedResources)).Should(Equal(2))

		By("replace configMap with a bad object from the work")
		testResource := &testv1alpha1.TestResource{
			TypeMeta: metav1.TypeMeta{
				APIVersion: testv1alpha1.GroupVersion.String(),
				Kind:       "TestResource",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "testresource-" + utilrand.String(5),
				// to ensure the resource is not applied.
				Namespace: "random-test-namespace",
			},
		}
		currentWork.Spec.Workload.Manifests = []workv1alpha1.Manifest{
			{
				RawExtension: runtime.RawExtension{Object: testResource},
			},
		}
		Expect(k8sClient.Update(context.Background(), currentWork)).Should(Succeed())

		By("Verify that the configMaps are removed from the cluster even if the new resource didn't apply")
		Eventually(func() bool {
			var configMap corev1.ConfigMap
			return apierrors.IsNotFound(k8sClient.Get(context.Background(), types.NamespacedName{Name: cm.Name, Namespace: resourceNamespace}, &configMap))
		}, timeout, interval).Should(BeTrue())

		Eventually(func() bool {
			var configMap corev1.ConfigMap
			return apierrors.IsNotFound(k8sClient.Get(context.Background(), types.NamespacedName{Name: cm2.Name, Namespace: resourceNamespace}, &configMap))
		}, timeout, interval).Should(BeTrue())

		By("Verify that the appliedWork status is correct")
		Eventually(func() bool {
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
			return len(appliedWork.Status.AppliedResources) == 0
		}, timeout, interval).Should(BeTrue())
	})

	It("Test the order of the manifest in the work alone does not trigger any operation in the member cluster", func() {
		By("Apply the work")
		Expect(k8sClient.Create(context.Background(), work)).ToNot(HaveOccurred())

		By("Make sure that the work is applied")
		currentWork := waitForWorkToApply(work.Name, workNamespace)
		var appliedWork workv1alpha1.AppliedWork
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
		Expect(len(appliedWork.Status.AppliedResources)).Should(Equal(2))

		By("Make sure that the manifests exist on the member cluster")
		Eventually(func() bool {
			var configMap corev1.ConfigMap
			return k8sClient.Get(context.Background(), types.NamespacedName{Name: cm2.Name, Namespace: resourceNamespace}, &configMap) == nil &&
				k8sClient.Get(context.Background(), types.NamespacedName{Name: cm.Name, Namespace: resourceNamespace}, &configMap) == nil
		}, timeout, interval).Should(BeTrue())

		By("Change the order of the two configs in the work")
		currentWork.Spec.Workload.Manifests = []workv1alpha1.Manifest{
			{
				RawExtension: runtime.RawExtension{Object: cm2},
			},
			{
				RawExtension: runtime.RawExtension{Object: cm},
			},
		}
		Expect(k8sClient.Update(context.Background(), currentWork)).Should(Succeed())

		By("Verify that nothing is removed from the cluster")
		Consistently(func() bool {
			var configMap corev1.ConfigMap
			return k8sClient.Get(context.Background(), types.NamespacedName{Name: cm2.Name, Namespace: resourceNamespace}, &configMap) == nil &&
				k8sClient.Get(context.Background(), types.NamespacedName{Name: cm.Name, Namespace: resourceNamespace}, &configMap) == nil
		}, timeout, time.Millisecond*25).Should(BeTrue())

		By("Verify that the appliedWork status is correct")
		Eventually(func() bool {
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
			return len(appliedWork.Status.AppliedResources) == 2
		}, timeout, interval).Should(BeTrue())
		Expect(appliedWork.Status.AppliedResources[0].Name).Should(Equal(cm2.GetName()))
		Expect(appliedWork.Status.AppliedResources[1].Name).Should(Equal(cm.GetName()))
	})
})
