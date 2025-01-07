/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	testv1alpha1 "go.goms.io/fleet/test/apis/v1alpha1"
	"go.goms.io/fleet/test/utils/controller"
)

var _ = Describe("Work Controller", func() {
	var cm *corev1.ConfigMap
	var work *fleetv1beta1.Work
	const defaultNS = "default"

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
			work = createWorkWithManifest(cm)
			err := hubClient.Create(context.Background(), work)
			Expect(err).ToNot(HaveOccurred())

			resultWork := waitForWorkToBeAvailable(work.GetName())
			Expect(len(resultWork.Status.ManifestConditions)).Should(Equal(1))
			expectedResourceID := fleetv1beta1.WorkResourceIdentifier{
				Ordinal:   0,
				Group:     "",
				Version:   "v1",
				Kind:      "ConfigMap",
				Resource:  "configmaps",
				Namespace: cmNamespace,
				Name:      cm.Name,
			}
			Expect(cmp.Diff(resultWork.Status.ManifestConditions[0].Identifier, expectedResourceID)).Should(BeEmpty())
			expected := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingApplyResultTypeApplied),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
				},
			}
			Expect(controller.CompareConditions(expected, resultWork.Status.ManifestConditions[0].Conditions)).Should(BeEmpty())
			expected = []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingApplyResultTypeApplied),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
				},
			}
			Expect(controller.CompareConditions(expected, resultWork.Status.Conditions)).Should(BeEmpty())

			By("Check applied config map")
			var configMap corev1.ConfigMap
			Expect(memberClient.Get(context.Background(), types.NamespacedName{Name: cmName, Namespace: cmNamespace}, &configMap)).Should(Succeed())
			Expect(cmp.Diff(configMap.Labels, cm.Labels)).Should(BeEmpty())
			Expect(cmp.Diff(configMap.Data, cm.Data)).Should(BeEmpty())

			Expect(hubClient.Delete(ctx, work)).Should(Succeed(), "Failed to deleted the work")
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
			work = createWorkWithManifest(cm)
			Expect(hubClient.Create(context.Background(), work)).ToNot(HaveOccurred())

			By("wait for the work to be available")
			waitForWorkToBeAvailable(work.GetName())

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
			resultWork := waitForWorkToApply(work.GetName())
			rawCM, err := json.Marshal(cm)
			Expect(err).Should(Succeed())
			resultWork.Spec.Workload.Manifests[0].Raw = rawCM
			Expect(hubClient.Update(ctx, resultWork)).Should(Succeed())

			By("wait for the change of the work to be applied")
			waitForWorkToApply(work.GetName())

			By("verify that applied configMap took all the changes")
			verifyAppliedConfigMap(cm)

			Expect(hubClient.Delete(ctx, work)).Should(Succeed(), "Failed to deleted the work")
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
			work = createWorkWithManifest(cm)
			err := hubClient.Create(context.Background(), work)
			Expect(err).ToNot(HaveOccurred())

			By("wait for the work to be applied")
			waitForWorkToApply(work.GetName())

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
			Expect(memberClient.Update(context.Background(), appliedCM)).Should(Succeed())

			By("Get the last applied config map and verify it's updated")
			var modifiedCM corev1.ConfigMap
			Expect(memberClient.Get(context.Background(), types.NamespacedName{Name: cm.GetName(), Namespace: cm.GetNamespace()}, &modifiedCM)).Should(Succeed())
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
			resultWork := waitForWorkToApply(work.GetName())
			rawCM, err := json.Marshal(cm)
			Expect(err).Should(Succeed())
			resultWork.Spec.Workload.Manifests[0].Raw = rawCM
			Expect(hubClient.Update(context.Background(), resultWork)).Should(Succeed())

			By("wait for the change of the work to be applied")
			waitForWorkToApply(work.GetName())

			By("Get the last applied config map")
			Expect(memberClient.Get(context.Background(), types.NamespacedName{Name: cmName, Namespace: cmNamespace}, appliedCM)).Should(Succeed())

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

			Expect(hubClient.Delete(ctx, work)).Should(Succeed(), "Failed to deleted the work")
		})

		It("Should pick up the crd change correctly", func() {
			testResourceName := "test-resource-name"
			testResourceNamespace := defaultNS
			testResource := &testv1alpha1.TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: testv1alpha1.GroupVersion.String(),
					Kind:       "TestResource",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      testResourceName,
					Namespace: testResourceNamespace,
				},
				Spec: testv1alpha1.TestResourceSpec{
					Foo: "foo",
					Bar: "bar",
					LabelSelector: &metav1.LabelSelector{
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
			work = createWorkWithManifest(testResource)
			err := hubClient.Create(context.Background(), work)
			Expect(err).ToNot(HaveOccurred())

			By("wait for the work to be applied")
			waitForWorkToBeAvailable(work.GetName())

			By("Check applied TestResource")
			var appliedTestResource testv1alpha1.TestResource
			Expect(memberClient.Get(context.Background(), types.NamespacedName{Name: testResourceName, Namespace: testResourceNamespace}, &appliedTestResource)).Should(Succeed())

			By("verify the TestResource spec")
			Expect(cmp.Diff(appliedTestResource.Spec, testResource.Spec)).Should(BeEmpty())

			By("Modify and update the applied TestResource")
			// add/modify/remove a match
			appliedTestResource.Spec.LabelSelector.MatchExpressions = []metav1.LabelSelectorRequirement{
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
			appliedTestResource.Spec.Items = []string{"a", "b"}
			appliedTestResource.Spec.Foo = "foo1"
			appliedTestResource.Spec.Bar = "bar1"
			Expect(memberClient.Update(context.Background(), &appliedTestResource)).Should(Succeed())

			By("Verify applied TestResource modified")
			var modifiedTestResource testv1alpha1.TestResource
			Expect(memberClient.Get(context.Background(), types.NamespacedName{Name: testResourceName, Namespace: testResourceNamespace}, &modifiedTestResource)).Should(Succeed())
			Expect(cmp.Diff(appliedTestResource.Spec, modifiedTestResource.Spec)).Should(BeEmpty())

			By("Modify the TestResource")
			testResource.Spec.LabelSelector.MatchExpressions = []metav1.LabelSelectorRequirement{
				{
					Key:      "region",
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{"us", "asia", "eu"},
				},
			}
			testResource.Spec.Foo = "foo2"
			testResource.Spec.Bar = "bar2"
			By("update the work")
			resultWork := waitForWorkToApply(work.GetName())
			rawTR, err := json.Marshal(testResource)
			Expect(err).Should(Succeed())
			resultWork.Spec.Workload.Manifests[0].Raw = rawTR
			Expect(hubClient.Update(context.Background(), resultWork)).Should(Succeed())
			waitForWorkToApply(work.GetName())

			By("Get the last applied TestResource")
			Expect(memberClient.Get(context.Background(), types.NamespacedName{Name: testResourceName, Namespace: testResourceNamespace}, &appliedTestResource)).Should(Succeed())

			By("Check the TestResource spec, its an override for arrays")
			expectedItems := []string{"a", "b"}
			Expect(cmp.Diff(appliedTestResource.Spec.Items, expectedItems)).Should(BeEmpty())
			Expect(cmp.Diff(appliedTestResource.Spec.LabelSelector, testResource.Spec.LabelSelector)).Should(BeEmpty())
			Expect(cmp.Diff(appliedTestResource.Spec.Foo, "foo2")).Should(BeEmpty())
			Expect(cmp.Diff(appliedTestResource.Spec.Bar, "bar2")).Should(BeEmpty())

			Expect(hubClient.Delete(ctx, work)).Should(Succeed(), "Failed to deleted the work")
		})

		It("Check that the apply still works if the last applied annotation does not exist", func() {
			ctx = context.Background()
			cmName := "test-merge-without-lastapply"
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
			work = createWorkWithManifest(cm)
			err := hubClient.Create(ctx, work)
			Expect(err).Should(Succeed())

			By("wait for the work to be applied")
			waitForWorkToApply(work.GetName())

			By("Check applied configMap")
			appliedCM := verifyAppliedConfigMap(cm)

			By("Delete the last applied annotation from the current resource")
			delete(appliedCM.Annotations, fleetv1beta1.LastAppliedConfigAnnotation)
			Expect(memberClient.Update(ctx, appliedCM)).Should(Succeed())

			By("Get the last applied config map and verify it does not have the last applied annotation")
			var modifiedCM corev1.ConfigMap
			Expect(memberClient.Get(ctx, types.NamespacedName{Name: cm.GetName(), Namespace: cm.GetNamespace()}, &modifiedCM)).Should(Succeed())
			Expect(modifiedCM.Annotations[fleetv1beta1.LastAppliedConfigAnnotation]).Should(BeEmpty())

			By("Modify the manifest")
			// modify one data
			cm.Data["data1"] = "modifiedValue"
			// add a conflict data
			cm.Data["data2"] = "added by manifest"
			// change label key3 with a new value
			cm.Labels["labelKey3"] = "added-back-by-manifest"

			By("update the work")
			resultWork := waitForWorkToApply(work.GetName())
			rawCM, err := json.Marshal(cm)
			Expect(err).Should(Succeed())
			resultWork.Spec.Workload.Manifests[0].Raw = rawCM
			Expect(hubClient.Update(ctx, resultWork)).Should(Succeed())

			By("wait for the change of the work to be applied")
			waitForWorkToApply(work.GetName())

			By("Check applied configMap is modified even without the last applied annotation")
			Expect(memberClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cmNamespace}, appliedCM)).Should(Succeed())
			verifyAppliedConfigMap(cm)

			Expect(hubClient.Delete(ctx, work)).Should(Succeed(), "Failed to deleted the work")
		})

		It("Check that failed to apply manifest has the proper identification", func() {
			testResourceName := "test-resource-name-failed"
			// to ensure apply fails.
			namespace := "random-test-namespace"
			testResource := &testv1alpha1.TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: testv1alpha1.GroupVersion.String(),
					Kind:       "TestResource",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      testResourceName,
					Namespace: namespace,
				},
				Spec: testv1alpha1.TestResourceSpec{
					Foo: "foo",
				},
			}
			work = createWorkWithManifest(testResource)
			err := hubClient.Create(context.Background(), work)
			Expect(err).ToNot(HaveOccurred())

			By("wait for the work to be applied, apply condition set to failed")
			var resultWork fleetv1beta1.Work
			Eventually(func() bool {
				err := hubClient.Get(context.Background(), types.NamespacedName{Name: work.Name, Namespace: work.GetNamespace()}, &resultWork)
				if err != nil {
					return false
				}
				applyCond := meta.FindStatusCondition(resultWork.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
				if applyCond == nil || applyCond.Status != metav1.ConditionFalse || applyCond.ObservedGeneration != resultWork.Generation {
					return false
				}
				if !meta.IsStatusConditionFalse(resultWork.Status.ManifestConditions[0].Conditions, fleetv1beta1.WorkConditionTypeApplied) {
					return false
				}
				return true
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue())
			expectedResourceID := fleetv1beta1.WorkResourceIdentifier{
				Ordinal:   0,
				Group:     testv1alpha1.GroupVersion.Group,
				Version:   testv1alpha1.GroupVersion.Version,
				Resource:  "testresources",
				Kind:      testResource.Kind,
				Namespace: testResource.GetNamespace(),
				Name:      testResource.GetName(),
			}
			Expect(cmp.Diff(resultWork.Status.ManifestConditions[0].Identifier, expectedResourceID)).Should(BeEmpty())
		})
	})

	// This test will set the work controller to leave and then join again.
	// It cannot run parallel with other tests.
	Context("Test multiple work propagation", Serial, func() {
		var works []*fleetv1beta1.Work

		AfterEach(func() {
			for _, staleWork := range works {
				err := hubClient.Delete(context.Background(), staleWork)
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
				Expect(workApplier.Join(ctx)).Should(Succeed())
				work = createWorkWithManifest(cm)
				err := hubClient.Create(ctx, work)
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
				return workApplier.Leave(ctx)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())

			By("make sure the manifests have no finalizer and its status match the member cluster")
			newData := map[string]string{
				"test-key-1":     "test-value-1",
				"test-key-2":     "test-value-2",
				"test-key-3":     "test-value-3",
				"new-test-key-1": "test-value-4",
				"new-test-key-2": "test-value-5",
			}
			for i := 0; i < numWork; i++ {
				var resultWork fleetv1beta1.Work
				Expect(hubClient.Get(ctx, types.NamespacedName{Name: works[i].GetName(), Namespace: memberReservedNSName}, &resultWork)).Should(Succeed())
				Expect(controllerutil.ContainsFinalizer(&resultWork, fleetv1beta1.WorkFinalizer)).Should(BeFalse())
				// make sure that leave can be called as many times as possible
				// The work may be updated and may hit 409 error.
				Eventually(func() error {
					return workApplier.Leave(ctx)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to set the work controller to leave")
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
				Expect(hubClient.Update(ctx, &resultWork)).Should(Succeed())
			}

			By("make sure the update in the work is not picked up")
			Consistently(func() bool {
				for i := 0; i < numWork; i++ {
					By(fmt.Sprintf("updated the work = %s", works[i].GetName()))
					var resultWork fleetv1beta1.Work
					err := hubClient.Get(context.Background(), types.NamespacedName{Name: works[i].GetName(), Namespace: memberReservedNSName}, &resultWork)
					Expect(err).Should(Succeed())
					Expect(controllerutil.ContainsFinalizer(&resultWork, fleetv1beta1.WorkFinalizer)).Should(BeFalse())
					applyCond := meta.FindStatusCondition(resultWork.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
					if applyCond != nil && applyCond.Status == metav1.ConditionTrue && applyCond.ObservedGeneration == resultWork.Generation {
						return false
					}
					By("check if the config map is not changed")
					Expect(memberClient.Get(ctx, types.NamespacedName{Name: cmNames[i], Namespace: cmNamespace}, &configMap)).Should(Succeed())
					Expect(cmp.Diff(configMap.Data, data)).Should(BeEmpty())
				}
				return true
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue())

			By("enable the work controller again")
			Expect(workApplier.Join(ctx)).Should(Succeed())

			By("make sure the work change get picked up")
			for i := 0; i < numWork; i++ {
				resultWork := waitForWorkToApply(works[i].GetName())
				Expect(len(resultWork.Status.ManifestConditions)).Should(Equal(1))
				Expect(meta.IsStatusConditionTrue(resultWork.Status.ManifestConditions[0].Conditions, fleetv1beta1.WorkConditionTypeApplied)).Should(BeTrue())
				By("the work is applied, check if the applied config map is updated")
				Expect(memberClient.Get(ctx, types.NamespacedName{Name: cmNames[i], Namespace: cmNamespace}, &configMap)).Should(Succeed())
				Expect(cmp.Diff(configMap.Data, newData)).Should(BeEmpty())
			}
		})
	})
})

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
		Expect(memberClient.Create(context.Background(), &rns)).Should(Succeed(), "Failed to create the resource namespace")

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
				Namespace: memberReservedNSName,
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
		Expect(hubClient.Delete(context.Background(), work)).Should(Succeed())
		Expect(memberClient.Delete(context.Background(), &rns)).Should(Succeed())
	})

	It("Should delete the manifest from the member cluster after it is removed from work", func() {
		By("Apply the work")
		Expect(hubClient.Create(context.Background(), work)).ToNot(HaveOccurred())

		By("Make sure that the work is applied")
		currentWork := waitForWorkToApply(work.Name)
		var appliedWork fleetv1beta1.AppliedWork
		Expect(memberClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
		Expect(len(appliedWork.Status.AppliedResources)).Should(Equal(2))

		By("Remove configMap 2 from the work")
		currentWork.Spec.Workload.Manifests = []fleetv1beta1.Manifest{
			{
				RawExtension: runtime.RawExtension{Object: cm},
			},
		}
		Expect(hubClient.Update(context.Background(), currentWork)).Should(Succeed())

		By("Verify that the resource is removed from the cluster")
		Eventually(func() bool {
			var configMap corev1.ConfigMap
			return apierrors.IsNotFound(memberClient.Get(context.Background(), types.NamespacedName{Name: cm2.Name, Namespace: resourceNamespace}, &configMap))
		}, eventuallyDuration, eventuallyInterval).Should(BeTrue())

		By("Verify that the appliedWork status is correct")
		Eventually(func() bool {
			Expect(memberClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
			return len(appliedWork.Status.AppliedResources) == 1
		}, eventuallyDuration, eventuallyInterval).Should(BeTrue())
		Expect(appliedWork.Status.AppliedResources[0].Name).Should(Equal(cm.GetName()))
		Expect(appliedWork.Status.AppliedResources[0].Namespace).Should(Equal(cm.GetNamespace()))
		Expect(appliedWork.Status.AppliedResources[0].Version).Should(Equal(cm.GetObjectKind().GroupVersionKind().Version))
		Expect(appliedWork.Status.AppliedResources[0].Group).Should(Equal(cm.GetObjectKind().GroupVersionKind().Group))
		Expect(appliedWork.Status.AppliedResources[0].Kind).Should(Equal(cm.GetObjectKind().GroupVersionKind().Kind))
	})

	It("Should delete the manifest from the member cluster even if there is apply failure", func() {
		By("Apply the work")
		Expect(hubClient.Create(context.Background(), work)).ToNot(HaveOccurred())

		By("Make sure that the work is applied")
		currentWork := waitForWorkToApply(work.Name)
		var appliedWork fleetv1beta1.AppliedWork
		Expect(memberClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
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
		Expect(hubClient.Update(context.Background(), currentWork)).Should(Succeed())

		By("Verify that the configMaps are removed from the cluster even if the new resource didn't apply")
		Eventually(func() bool {
			var configMap corev1.ConfigMap
			return apierrors.IsNotFound(memberClient.Get(context.Background(), types.NamespacedName{Name: cm.Name, Namespace: resourceNamespace}, &configMap))
		}, eventuallyDuration, eventuallyInterval).Should(BeTrue())

		Eventually(func() bool {
			var configMap corev1.ConfigMap
			return apierrors.IsNotFound(memberClient.Get(context.Background(), types.NamespacedName{Name: cm2.Name, Namespace: resourceNamespace}, &configMap))
		}, eventuallyDuration, eventuallyInterval).Should(BeTrue())

		By("Verify that the appliedWork status is correct")
		Eventually(func() bool {
			Expect(memberClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
			return len(appliedWork.Status.AppliedResources) == 0
		}, eventuallyDuration, eventuallyInterval).Should(BeTrue())
	})

	It("Test the order of the manifest in the work alone does not trigger any operation in the member cluster", func() {
		By("Apply the work")
		Expect(hubClient.Create(context.Background(), work)).ToNot(HaveOccurred())

		By("Make sure that the work is applied")
		currentWork := waitForWorkToApply(work.Name)
		var appliedWork fleetv1beta1.AppliedWork
		Expect(memberClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
		Expect(len(appliedWork.Status.AppliedResources)).Should(Equal(2))

		By("Make sure that the manifests exist on the member cluster")
		Eventually(func() bool {
			var configMap corev1.ConfigMap
			return memberClient.Get(context.Background(), types.NamespacedName{Name: cm2.Name, Namespace: resourceNamespace}, &configMap) == nil &&
				memberClient.Get(context.Background(), types.NamespacedName{Name: cm.Name, Namespace: resourceNamespace}, &configMap) == nil
		}, eventuallyDuration, eventuallyInterval).Should(BeTrue())

		By("Change the order of the two configs in the work")
		currentWork.Spec.Workload.Manifests = []fleetv1beta1.Manifest{
			{
				RawExtension: runtime.RawExtension{Object: cm2},
			},
			{
				RawExtension: runtime.RawExtension{Object: cm},
			},
		}
		Expect(hubClient.Update(context.Background(), currentWork)).Should(Succeed())

		By("Verify that nothing is removed from the cluster")
		Consistently(func() bool {
			var configMap corev1.ConfigMap
			return memberClient.Get(context.Background(), types.NamespacedName{Name: cm2.Name, Namespace: resourceNamespace}, &configMap) == nil &&
				memberClient.Get(context.Background(), types.NamespacedName{Name: cm.Name, Namespace: resourceNamespace}, &configMap) == nil
		}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

		By("Verify that the appliedWork status is correct")
		Eventually(func() bool {
			Expect(memberClient.Get(context.Background(), types.NamespacedName{Name: work.Name}, &appliedWork)).Should(Succeed())
			return len(appliedWork.Status.AppliedResources) == 2
		}, eventuallyDuration, eventuallyInterval).Should(BeTrue())
		Expect(appliedWork.Status.AppliedResources[0].Name).Should(Equal(cm2.GetName()))
		Expect(appliedWork.Status.AppliedResources[1].Name).Should(Equal(cm.GetName()))
	})
})
