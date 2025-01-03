/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

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

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	testv1alpha1 "go.goms.io/fleet/test/apis/v1alpha1"
)

var _ = Describe("Work Status Reconciler", func() {
	var resourceNamespace string
	var work *fleetv1beta1.Work
	var cm, cm2 *corev1.ConfigMap
	var rns corev1.Namespace

	BeforeEach(func() {
		resourceNamespace = utilrand.String(5)
		rns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourceNamespace,
			},
		}
		Expect(k8sClient.Create(context.Background(), &rns)).Should(Succeed(), "Failed to create the resource namespace")

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
		work = &fleetv1beta1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "work-" + utilrand.String(5),
				Namespace: testWorkNamespace,
			},
			Spec: fleetv1beta1.WorkSpec{
				Workload: fleetv1beta1.WorkloadTemplate{
					Manifests: []fleetv1beta1.Manifest{
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
		Expect(k8sClient.Delete(context.Background(), &rns)).Should(Succeed())
	})

	It("Should delete the manifest from the member cluster after it is removed from work", func() {
		By("Apply the work")
		Expect(k8sClient.Create(context.Background(), work)).ToNot(HaveOccurred())

		By("Make sure that the work is applied")
		currentWork := waitForWorkToApply(work.Name, testWorkNamespace)
		var appliedWork fleetv1beta1.AppliedWork
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
		Expect(len(appliedWork.Status.AppliedResources)).Should(Equal(2))

		By("Remove configMap 2 from the work")
		currentWork.Spec.Workload.Manifests = []fleetv1beta1.Manifest{
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

	It("Should delete the manifest from the member cluster even if there is apply failure", func() {
		By("Apply the work")
		Expect(k8sClient.Create(context.Background(), work)).ToNot(HaveOccurred())

		By("Make sure that the work is applied")
		currentWork := waitForWorkToApply(work.Name, testWorkNamespace)
		var appliedWork fleetv1beta1.AppliedWork
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
		currentWork.Spec.Workload.Manifests = []fleetv1beta1.Manifest{
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
		currentWork := waitForWorkToApply(work.Name, testWorkNamespace)
		var appliedWork fleetv1beta1.AppliedWork
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
		Expect(len(appliedWork.Status.AppliedResources)).Should(Equal(2))

		By("Make sure that the manifests exist on the member cluster")
		Eventually(func() bool {
			var configMap corev1.ConfigMap
			return k8sClient.Get(context.Background(), types.NamespacedName{Name: cm2.Name, Namespace: resourceNamespace}, &configMap) == nil &&
				k8sClient.Get(context.Background(), types.NamespacedName{Name: cm.Name, Namespace: resourceNamespace}, &configMap) == nil
		}, timeout, interval).Should(BeTrue())

		By("Change the order of the two configs in the work")
		currentWork.Spec.Workload.Manifests = []fleetv1beta1.Manifest{
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
