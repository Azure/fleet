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

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

const timeout = time.Second * 10
const interval = time.Millisecond * 250

var _ = Describe("Work Controller", func() {
	var workNamespace string
	var ns corev1.Namespace
	var cm *corev1.ConfigMap
	var work *workv1alpha1.Work
	const defaultNS = "default"

	BeforeEach(func() {
		workNamespace = "work-" + utilrand.String(5)
		// Create namespace
		ns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: workNamespace,
			},
		}
		err := k8sClient.Create(context.Background(), &ns)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
		err := k8sClient.Delete(context.Background(), &ns)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("Test single work propagation", func() {
		It("Should have a configmap deployed correctly", func() {
			cmName := "testcm"
			cmNamespace := defaultNS
			cm = &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: cmNamespace,
				},
				Data: map[string]string{
					"test": "test",
				},
			}

			By("create the work")
			work = createWorkWithManifest(workNamespace, cm)
			err := k8sClient.Create(context.Background(), work)
			Expect(err).ToNot(HaveOccurred())

			resultWork := waitForWorkToApply(work.GetName(), work.GetNamespace())
			Expect(len(resultWork.Status.ManifestConditions)).Should(Equal(1))
			Expect(meta.IsStatusConditionTrue(resultWork.Status.Conditions, ConditionTypeApplied)).Should(BeTrue())
			Expect(meta.IsStatusConditionTrue(resultWork.Status.ManifestConditions[0].Conditions, ConditionTypeApplied)).Should(BeTrue())
			expectedResourceID := workv1alpha1.ResourceIdentifier{
				Ordinal:   0,
				Group:     "",
				Version:   "v1",
				Kind:      "ConfigMap",
				Resource:  "configmaps",
				Namespace: cmNamespace,
				Name:      cm.Name,
			}
			Expect(cmp.Diff(resultWork.Status.ManifestConditions[0].Identifier, expectedResourceID)).Should(BeEmpty())

			By("Check applied config map")
			var configMap corev1.ConfigMap
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: cmName, Namespace: cmNamespace}, &configMap)).Should(Succeed())
			Expect(cmp.Diff(configMap.Labels, cm.Labels)).Should(BeEmpty())
			Expect(cmp.Diff(configMap.Data, cm.Data)).Should(BeEmpty())

		})

		It("Should apply the same manifest in two work properly", func() {
			cmName := "test-multiple-owner"
			cmNamespace := defaultNS
			cm := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: cmNamespace,
				},
				Data: map[string]string{
					"data1": "test1",
				},
			}

			work1 := createWorkWithManifest(workNamespace, cm)
			work2 := work1.DeepCopy()
			work2.Name = "test-work-2"

			By("create the first work")
			err := k8sClient.Create(context.Background(), work1)
			Expect(err).ToNot(HaveOccurred())

			By("create the second work")
			err = k8sClient.Create(context.Background(), work2)
			Expect(err).ToNot(HaveOccurred())

			waitForWorkToApply(work1.GetName(), workNamespace)
			waitForWorkToApply(work2.GetName(), workNamespace)

			By("Check applied config map")
			var configMap corev1.ConfigMap
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: cmName, Namespace: cmNamespace}, &configMap)).Should(Succeed())
			Expect(len(configMap.Data)).Should(Equal(1))
			Expect(configMap.Data["data1"]).Should(Equal(cm.Data["data1"]))
			Expect(len(configMap.OwnerReferences)).Should(Equal(2))
			Expect(configMap.OwnerReferences[0].APIVersion).Should(Equal(workv1alpha1.GroupVersion.String()))
			Expect(configMap.OwnerReferences[0].Kind).Should(Equal(workv1alpha1.AppliedWorkKind))
			Expect(configMap.OwnerReferences[1].APIVersion).Should(Equal(workv1alpha1.GroupVersion.String()))
			Expect(configMap.OwnerReferences[1].Kind).Should(Equal(workv1alpha1.AppliedWorkKind))
			// GC does not work in the testEnv
			By("delete the second work")
			Expect(k8sClient.Delete(context.Background(), work2)).Should(Succeed())
			By("check that the applied work2 is deleted")
			var appliedWork workv1alpha1.AppliedWork
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: work2.Name}, &appliedWork)
				return apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("delete the first work")
			Expect(k8sClient.Delete(context.Background(), work1)).Should(Succeed())
			By("check that the applied work1 and config map is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: work2.Name}, &appliedWork)
				return apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})

		It("Should pick up the built-in manifest change correctly", func() {
			cmName := "testconfig"
			cmNamespace := defaultNS
			cm = &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: cmNamespace,
					Labels: map[string]string{
						"labelKey1": "value1",
						"labelKey2": "value2",
					},
					Annotations: map[string]string{
						"annotationKey1": "annotation1",
						"annotationKey2": "annotation2",
					},
				},
				Data: map[string]string{
					"data1": "test1",
				},
			}

			By("create the work")
			work = createWorkWithManifest(workNamespace, cm)
			Expect(k8sClient.Create(context.Background(), work)).ToNot(HaveOccurred())

			By("wait for the work to be applied")
			waitForWorkToApply(work.GetName(), work.GetNamespace())

			By("Check applied config map")
			verifyAppliedConfigMap(cm)

			By("Modify the configMap manifest")
			// add new data
			cm.Data["data2"] = "test2"
			// modify one data
			cm.Data["data1"] = "newValue"
			// modify label key1
			cm.Labels["labelKey1"] = "newValue"
			// remove label key2
			delete(cm.Labels, "labelKey2")
			// add annotations key3
			cm.Annotations["annotationKey3"] = "annotation3"
			// remove annotations key1
			delete(cm.Annotations, "annotationKey1")

			By("update the work")
			resultWork := waitForWorkToApply(work.GetName(), work.GetNamespace())
			rawCM, err := json.Marshal(cm)
			Expect(err).Should(Succeed())
			resultWork.Spec.Workload.Manifests[0].Raw = rawCM
			Expect(k8sClient.Update(ctx, resultWork)).Should(Succeed())

			By("wait for the change of the work to be applied")
			waitForWorkToApply(work.GetName(), work.GetNamespace())

			By("verify that applied configMap took all the changes")
			verifyAppliedConfigMap(cm)
		})

		It("Should merge the third party change correctly", func() {
			cmName := "test-merge"
			cmNamespace := defaultNS
			cm = &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: cmNamespace,
					Labels: map[string]string{
						"labelKey1": "value1",
						"labelKey2": "value2",
						"labelKey3": "value3",
					},
				},
				Data: map[string]string{
					"data1": "test1",
				},
			}

			By("create the work")
			work = createWorkWithManifest(workNamespace, cm)
			err := k8sClient.Create(context.Background(), work)
			Expect(err).ToNot(HaveOccurred())

			By("wait for the work to be applied")
			waitForWorkToApply(work.GetName(), work.GetNamespace())

			By("Check applied configMap")
			appliedCM := verifyAppliedConfigMap(cm)

			By("Modify and update the applied configMap")
			// add a new data
			appliedCM.Data["data2"] = "another value"
			// add a new data
			appliedCM.Data["data3"] = "added data by third party"
			// modify label key1
			appliedCM.Labels["labelKey1"] = "third-party-label"
			// remove label key2 and key3
			delete(cm.Labels, "labelKey2")
			delete(cm.Labels, "labelKey3")
			Expect(k8sClient.Update(context.Background(), appliedCM)).Should(Succeed())

			By("Get the last applied config map and verify it's updated")
			var modifiedCM corev1.ConfigMap
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: cm.GetName(), Namespace: cm.GetNamespace()}, &modifiedCM)).Should(Succeed())
			Expect(cmp.Diff(appliedCM.Labels, modifiedCM.Labels)).Should(BeEmpty())
			Expect(cmp.Diff(appliedCM.Data, modifiedCM.Data)).Should(BeEmpty())

			By("Modify the manifest")
			// modify one data
			cm.Data["data1"] = "modifiedValue"
			// add a conflict data
			cm.Data["data2"] = "added by manifest"
			// change label key3 with a new value
			cm.Labels["labelKey3"] = "added-back-by-manifest"

			By("update the work")
			resultWork := waitForWorkToApply(work.GetName(), work.GetNamespace())
			rawCM, err := json.Marshal(cm)
			Expect(err).Should(Succeed())
			resultWork.Spec.Workload.Manifests[0].Raw = rawCM
			Expect(k8sClient.Update(context.Background(), resultWork)).Should(Succeed())

			By("wait for the change of the work to be applied")
			waitForWorkToApply(work.GetName(), work.GetNamespace())

			By("Get the last applied config map")
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: cmName, Namespace: cmNamespace}, appliedCM)).Should(Succeed())

			By("Check the config map data")
			// data1's value picks up our change
			// data2 is value is overridden by our change
			// data3 is added by the third party
			expectedData := map[string]string{
				"data1": "modifiedValue",
				"data2": "added by manifest",
				"data3": "added data by third party",
			}
			Expect(cmp.Diff(appliedCM.Data, expectedData)).Should(BeEmpty())

			By("Check the config map label")
			// key1's value is override back even if we didn't change it
			// key2 is deleted by third party since we didn't change it
			// key3's value added back after we change the value
			expectedLabel := map[string]string{
				"labelKey1": "value1",
				"labelKey3": "added-back-by-manifest",
			}
			Expect(cmp.Diff(appliedCM.Labels, expectedLabel)).Should(BeEmpty())
		})

		It("Should pick up the crd change correctly", func() {
			cloneName := "testcloneset"
			cloneNamespace := defaultNS
			cloneSet := &kruisev1alpha1.CloneSet{
				TypeMeta: metav1.TypeMeta{
					APIVersion: kruisev1alpha1.SchemeGroupVersion.String(),
					Kind:       "CloneSet",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      cloneName,
					Namespace: cloneNamespace,
				},
				Spec: kruisev1alpha1.CloneSetSpec{
					Selector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "region",
								Operator: metav1.LabelSelectorOpNotIn,
								Values:   []string{"us", "eu"},
							},
							{
								Key:      "prod",
								Operator: metav1.LabelSelectorOpDoesNotExist,
							},
						},
					},
				},
			}

			By("create the work")
			work = createWorkWithManifest(workNamespace, cloneSet)
			err := k8sClient.Create(context.Background(), work)
			Expect(err).ToNot(HaveOccurred())

			By("wait for the work to be applied")
			waitForWorkToApply(work.GetName(), work.GetNamespace())

			By("Check applied CloneSet")
			var appliedCloneSet kruisev1alpha1.CloneSet
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: cloneName, Namespace: cloneNamespace}, &appliedCloneSet)).Should(Succeed())

			By("verify the CloneSet spec")
			Expect(cmp.Diff(appliedCloneSet.Spec, cloneSet.Spec)).Should(BeEmpty())

			By("Modify and update the applied CloneSet")
			// add/modify/remove a match
			appliedCloneSet.Spec.Selector.MatchExpressions = []metav1.LabelSelectorRequirement{
				{
					Key:      "region",
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{"asia"},
				},
				{
					Key:      "extra",
					Operator: metav1.LabelSelectorOpExists,
				},
			}
			appliedCloneSet.Spec.ScaleStrategy.PodsToDelete = []string{"a", "b"}
			appliedCloneSet.Spec.MinReadySeconds = 10
			Expect(k8sClient.Update(context.Background(), &appliedCloneSet)).Should(Succeed())

			By("Verify applied CloneSet modified")
			var modifiedCloneSet kruisev1alpha1.CloneSet
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: cloneName, Namespace: cloneNamespace}, &modifiedCloneSet)).Should(Succeed())
			Expect(cmp.Diff(appliedCloneSet.Spec, modifiedCloneSet.Spec)).Should(BeEmpty())

			By("Modify the cloneset")
			cloneSet.Spec.Selector.MatchExpressions = []metav1.LabelSelectorRequirement{
				{
					Key:      "region",
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{"us", "asia", "eu"},
				},
			}
			cloneSet.Spec.Replicas = pointer.Int32Ptr(10)
			cloneSet.Spec.MinReadySeconds = 1
			maxuavail := intstr.FromInt(10)
			cloneSet.Spec.ScaleStrategy.MaxUnavailable = &maxuavail
			By("update the work")
			resultWork := waitForWorkToApply(work.GetName(), work.GetNamespace())
			rawCM, err := json.Marshal(cloneSet)
			Expect(err).Should(Succeed())
			resultWork.Spec.Workload.Manifests[0].Raw = rawCM
			Expect(k8sClient.Update(context.Background(), resultWork)).Should(Succeed())
			waitForWorkToApply(work.GetName(), work.GetNamespace())

			By("Get the last applied cloneset")
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: cloneName, Namespace: cloneNamespace}, &appliedCloneSet)).Should(Succeed())

			By("Check the cloneset spec, its an overide for arrays")
			expectStrategy := kruisev1alpha1.CloneSetScaleStrategy{
				PodsToDelete:   []string{"a", "b"},
				MaxUnavailable: &maxuavail,
			}
			Expect(cmp.Diff(appliedCloneSet.Spec.ScaleStrategy, expectStrategy)).Should(BeEmpty())
			Expect(cmp.Diff(appliedCloneSet.Spec.Selector, cloneSet.Spec.Selector)).Should(BeEmpty())
			Expect(cmp.Diff(appliedCloneSet.Spec.Replicas, pointer.Int32Ptr(10))).Should(BeEmpty())
			Expect(cmp.Diff(appliedCloneSet.Spec.MinReadySeconds, int32(1))).Should(BeEmpty())
		})

		It("Check that owner references is merged instead of override", func() {
			cmName := "test-ownerreference-merge"
			cmNamespace := defaultNS
			cm = &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: cmNamespace,
				},
				Data: map[string]string{
					"test": "test",
				},
			}

			By("create the work")
			work = createWorkWithManifest(workNamespace, cm)
			Expect(k8sClient.Create(context.Background(), work)).ToNot(HaveOccurred())

			By("create another work that includes the configMap")
			work2 := createWorkWithManifest(workNamespace, cm)
			Expect(k8sClient.Create(context.Background(), work2)).ToNot(HaveOccurred())

			By("wait for the change of the work1 to be applied")
			waitForWorkToApply(work.GetName(), work.GetNamespace())

			By("wait for the change of the work2 to be applied")
			waitForWorkToApply(work2.GetName(), work2.GetNamespace())

			By("verify the owner reference is merged")
			var appliedCM corev1.ConfigMap
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: cm.GetName(), Namespace: cm.GetNamespace()}, &appliedCM)).Should(Succeed())

			By("Check the config map label")
			Expect(len(appliedCM.OwnerReferences)).Should(Equal(2))
			Expect(appliedCM.OwnerReferences[0].APIVersion).Should(Equal(workv1alpha1.GroupVersion.String()))
			Expect(appliedCM.OwnerReferences[0].Name).Should(SatisfyAny(Equal(work.GetName()), Equal(work2.GetName())))
			Expect(appliedCM.OwnerReferences[1].APIVersion).Should(Equal(workv1alpha1.GroupVersion.String()))
			Expect(appliedCM.OwnerReferences[1].Name).Should(SatisfyAny(Equal(work.GetName()), Equal(work2.GetName())))
		})

		It("Check that failed to apply manifest has the proper identification", func() {
			broadcastName := "testfail"
			namespace := defaultNS
			broadcastJob := &kruisev1alpha1.BroadcastJob{
				TypeMeta: metav1.TypeMeta{
					APIVersion: kruisev1alpha1.SchemeGroupVersion.String(),
					Kind:       "BroadcastJob",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      broadcastName,
					Namespace: namespace,
				},
				Spec: kruisev1alpha1.BroadcastJobSpec{
					Paused: true,
				},
			}
			work = createWorkWithManifest(workNamespace, broadcastJob)
			err := k8sClient.Create(context.Background(), work)
			Expect(err).ToNot(HaveOccurred())

			By("wait for the work to be applied")
			var resultWork workv1alpha1.Work
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: work.Name, Namespace: work.GetNamespace()}, &resultWork)
				if err != nil {
					return false
				}
				applyCond := meta.FindStatusCondition(resultWork.Status.Conditions, ConditionTypeApplied)
				if applyCond == nil || applyCond.Status != metav1.ConditionFalse || applyCond.ObservedGeneration != resultWork.Generation {
					return false
				}
				if !meta.IsStatusConditionFalse(resultWork.Status.ManifestConditions[0].Conditions, ConditionTypeApplied) {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())
			expectedResourceID := workv1alpha1.ResourceIdentifier{
				Ordinal:   0,
				Group:     "apps.kruise.io",
				Version:   "v1alpha1",
				Kind:      "BroadcastJob",
				Namespace: broadcastJob.GetNamespace(),
				Name:      broadcastJob.GetName(),
			}
			Expect(cmp.Diff(resultWork.Status.ManifestConditions[0].Identifier, expectedResourceID)).Should(BeEmpty())
		})
	})

	Context("Test multiple work propagation", func() {
		var works []*workv1alpha1.Work

		AfterEach(func() {
			for _, staleWork := range works {
				err := k8sClient.Delete(context.Background(), staleWork)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		It("Test join and leave work correctly", func() {
			By("create the works")
			var configMap corev1.ConfigMap
			cmNamespace := defaultNS
			var cmNames []string
			numWork := 10
			data := map[string]string{
				"test-key-1": "test-value-1",
				"test-key-2": "test-value-2",
				"test-key-3": "test-value-3",
			}

			for i := 0; i < numWork; i++ {
				cmName := "testcm-" + utilrand.String(10)
				cmNames = append(cmNames, cmName)
				cm = &corev1.ConfigMap{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "ConfigMap",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      cmName,
						Namespace: cmNamespace,
					},
					Data: data,
				}
				// make sure we can call join as many as possible
				Expect(workController.Join(ctx)).Should(Succeed())
				work = createWorkWithManifest(workNamespace, cm)
				err := k8sClient.Create(ctx, work)
				Expect(err).ToNot(HaveOccurred())
				By(fmt.Sprintf("created the work = %s", work.GetName()))
				works = append(works, work)
			}

			By("make sure the works are handled")
			for i := 0; i < numWork; i++ {
				waitForWorkToBeHandled(works[i].GetName(), works[i].GetNamespace())
			}

			By("mark the work controller as leave")
			Eventually(func() error {
				return workController.Leave(ctx)
			}, timeout, interval).Should(Succeed())

			By("make sure the manifests have no finalizer and its status match the member cluster")
			newData := map[string]string{
				"test-key-1":     "test-value-1",
				"test-key-2":     "test-value-2",
				"test-key-3":     "test-value-3",
				"new-test-key-1": "test-value-4",
				"new-test-key-2": "test-value-5",
			}
			for i := 0; i < numWork; i++ {
				var resultWork workv1alpha1.Work
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: works[i].GetName(), Namespace: workNamespace}, &resultWork)).Should(Succeed())
				Expect(controllerutil.ContainsFinalizer(work, workFinalizer)).Should(BeFalse())
				applyCond := meta.FindStatusCondition(resultWork.Status.Conditions, ConditionTypeApplied)
				if applyCond != nil && applyCond.Status == metav1.ConditionTrue && applyCond.ObservedGeneration == resultWork.Generation {
					By("the work is applied, check if the applied config map is still there")
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cmNames[i], Namespace: cmNamespace}, &configMap)).Should(Succeed())
					Expect(cmp.Diff(configMap.Data, data)).Should(BeEmpty())
				} else {
					By("the work is not applied, verify that the applied config map is not there")
					err := k8sClient.Get(ctx, types.NamespacedName{Name: cmNames[i], Namespace: cmNamespace}, &configMap)
					Expect(apierrors.IsNotFound(err)).Should(BeTrue())
				}
				// make sure that leave can be called as many times as possible
				Expect(workController.Leave(ctx)).Should(Succeed())
				By(fmt.Sprintf("change the work = %s", work.GetName()))
				cm = &corev1.ConfigMap{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "ConfigMap",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      cmNames[i],
						Namespace: cmNamespace,
					},
					Data: newData,
				}
				rawCM, err := json.Marshal(cm)
				Expect(err).Should(Succeed())
				resultWork.Spec.Workload.Manifests[0].Raw = rawCM
				Expect(k8sClient.Update(ctx, &resultWork)).Should(Succeed())
			}

			By("make sure the update in the work is not picked up")
			Consistently(func() bool {
				for i := 0; i < numWork; i++ {
					By(fmt.Sprintf("updated the work = %s", works[i].GetName()))
					var resultWork workv1alpha1.Work
					err := k8sClient.Get(context.Background(), types.NamespacedName{Name: works[i].GetName(), Namespace: workNamespace}, &resultWork)
					Expect(err).Should(Succeed())
					Expect(controllerutil.ContainsFinalizer(&resultWork, workFinalizer)).Should(BeFalse())
					applyCond := meta.FindStatusCondition(resultWork.Status.Conditions, ConditionTypeApplied)
					if applyCond != nil && applyCond.Status == metav1.ConditionTrue && applyCond.ObservedGeneration == resultWork.Generation {
						return false
					}
					By("check if the config map is not changed")
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cmNames[i], Namespace: cmNamespace}, &configMap)).Should(Succeed())
					Expect(cmp.Diff(configMap.Data, data)).Should(BeEmpty())
				}
				return true
			}, timeout, interval).Should(BeTrue())

			By("enable the work controller again")
			Expect(workController.Join(ctx)).Should(Succeed())

			By("make sure the work change get picked up")
			for i := 0; i < numWork; i++ {
				resultWork := waitForWorkToApply(works[i].GetName(), works[i].GetNamespace())
				Expect(len(resultWork.Status.ManifestConditions)).Should(Equal(1))
				Expect(meta.IsStatusConditionTrue(resultWork.Status.ManifestConditions[0].Conditions, ConditionTypeApplied)).Should(BeTrue())
				By("the work is applied, check if the applied config map is updated")
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cmNames[i], Namespace: cmNamespace}, &configMap)).Should(Succeed())
				Expect(cmp.Diff(configMap.Data, newData)).Should(BeEmpty())
			}
		})
	})
})
