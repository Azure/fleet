/*
Copyright 2026 The KubeFleet Authors.

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

package workapplier

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

// The test cases below use the work applier dedicated to the wave test cases (workApplierWaves), which pauses for
// parallelizerFixedDelay after processing each wave of manifests. Objects in the same wave are created at about the
// same time; objects in consecutive waves are created at least parallelizerFixedDelay apart. Had an object not been
// applied in the expected wave, it would have been applied in a later reconciliation, at least one requeue delay
// (and several wave delays) later; the gaps are hence also checked against an upper bound.
var _ = Describe("waves", func() {
	Context("single wave", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		priorityClassName := fmt.Sprintf("priority-class-%s", utils.RandStr())

		BeforeAll(func() {
			// Prepare the manifests; namespaces and priority classes are both in the first wave.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			regularPriorityClass := &schedulingv1.PriorityClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "scheduling.k8s.io/v1",
					Kind:       "PriorityClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: priorityClassName,
				},
				Value: 1000,
			}
			regularPriorityClassJSON := marshalK8sObjJSON(regularPriorityClass)

			createWorkObject(workName, memberReservedNSNameWaves, nil, regularNSJSON, regularPriorityClassJSON)
		})

		It("should update the work object status", func() {
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAvailableCondAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "Namespace",
						Resource:   "namespaces",
						Name:       nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    1,
						APIGroup:   "scheduling.k8s.io",
						APIVersion: "v1",
						Kind:       "PriorityClass",
						Resource:   "priorityclasses",
						Name:       priorityClassName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 1,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 1,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSNameWaves, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should create the objects in the same wave", func() {
			nsCreationTimestamp := retrieveCreationTimestamp(memberClient1, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})
			priorityClassCreationTimestamp := retrieveCreationTimestamp(memberClient1, &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: priorityClassName}})
			gap := priorityClassCreationTimestamp.Sub(nsCreationTimestamp)
			Expect(gap).To(BeNumerically("~", 0, time.Second), "Objects in the same wave are not created at about the same time (gap: %v)", gap)
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSNameWaves)

			// Remove the priority class manually, as there is no GC controller in the test environment.
			priorityClass := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: priorityClassName}}
			Expect(client.IgnoreNotFound(memberClient1.Delete(ctx, priorityClass))).To(Succeed(), "Failed to delete the priority class")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSNameWaves, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("two consecutive waves", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		BeforeAll(func() {
			// Prepare the manifests; namespaces are in the first wave (wave 0), and config maps are in the second
			// wave (wave 1).
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			regularConfigMap := configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// List the config map first; the manifest order does not matter.
			createWorkObject(workName, memberReservedNSNameWaves, nil, regularConfigMapJSON, regularNSJSON)
		})

		It("should update the work object status", func() {
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAvailableCondAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       configMapName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    1,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "Namespace",
						Resource:   "namespaces",
						Name:       nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSNameWaves, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should create the objects in waves", func() {
			nsCreationTimestamp := retrieveCreationTimestamp(memberClient1, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})
			configMapCreationTimestamp := retrieveCreationTimestamp(memberClient1, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: nsName, Name: configMapName}})
			gap := configMapCreationTimestamp.Sub(nsCreationTimestamp)
			Expect(gap).To(BeNumerically(">=", parallelizerFixedDelay), "The config map is not created one wave after the namespace (gap: %v)", gap)
			Expect(gap).To(BeNumerically("<", 2*parallelizerFixedDelay), "The config map is not created in the wave right after the namespace (gap: %v)", gap)
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSNameWaves)

			// Remove the config map manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSNameWaves, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("two non-consecutive waves", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		roleName := "role-1"

		BeforeAll(func() {
			// Prepare the manifests; namespaces are in the first wave (wave 0), and roles are in the third wave (wave
			// 2). No manifest is in the second wave (wave 1).
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			regularRole := &rbacv1.Role{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "Role",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: nsName,
					Name:      roleName,
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"configmaps"},
						Verbs:     []string{"get"},
					},
				},
			}
			regularRoleJSON := marshalK8sObjJSON(regularRole)

			createWorkObject(workName, memberReservedNSNameWaves, nil, regularNSJSON, regularRoleJSON)
		})

		It("should update the work object status", func() {
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAvailableCondAllManifestsAvailableReason,
				},
			}
			manifestStatuses := []placementv1alpha1.PerManifestStatus{
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "Namespace",
						Resource:   "namespaces",
						Name:       nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    1,
						APIGroup:   "rbac.authorization.k8s.io",
						APIVersion: "v1",
						Kind:       "Role",
						Resource:   "roles",
						Name:       roleName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSNameWaves, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should create the objects in waves, skipping the empty wave", func() {
			// No delay is added for the empty wave.
			nsCreationTimestamp := retrieveCreationTimestamp(memberClient1, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})
			roleCreationTimestamp := retrieveCreationTimestamp(memberClient1, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Namespace: nsName, Name: roleName}})
			gap := roleCreationTimestamp.Sub(nsCreationTimestamp)
			Expect(gap).To(BeNumerically(">=", parallelizerFixedDelay), "The role is not created one wave after the namespace (gap: %v)", gap)
			Expect(gap).To(BeNumerically("<", 2*parallelizerFixedDelay), "The role is not created in the wave right after the namespace (gap: %v)", gap)
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSNameWaves)

			// Remove the role manually, as there is no GC controller in the test environment.
			role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Namespace: nsName, Name: roleName}}
			Expect(client.IgnoreNotFound(memberClient1.Delete(ctx, role))).To(Succeed(), "Failed to delete the role")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSNameWaves, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("waves across linked work objects", Ordered, func() {
		// The two work objects are linked to each other: they share the same owner placement policy/binding and the
		// same primary placement resource snapshot; the primary work object features the linked work count
		// annotation.
		primaryWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		secondaryWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, primaryWorkName)
		placementBindingName := fmt.Sprintf(placementBindingNameTemplate, primaryWorkName)
		primarySnapshotName := fmt.Sprintf(primarySnapshotNameTemplate, primaryWorkName)
		// The namespace is created from the namespace manifest in the secondary work object.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		primaryWorkKey := types.NamespacedName{Namespace: memberReservedNSNameWaves, Name: primaryWorkName}
		secondaryWorkKey := types.NamespacedName{Namespace: memberReservedNSNameWaves, Name: secondaryWorkName}

		BeforeAll(func() {
			// Prepare the manifests.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			regularConfigMap := configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// The secondary work object features the namespace manifest.
			secondaryWork := buildWorkObject(
				secondaryWorkName, memberReservedNSNameWaves,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				regularNSJSON,
			)
			setPrimaryWorkAsOwner(secondaryWork, primaryWorkName)
			Expect(hubClient.Create(ctx, secondaryWork)).To(Succeed(), "Failed to create the secondary work object")

			// The primary work object features the config map manifest, and forbids the work applier from creating
			// namespaces on its own; the config map can be applied only after the namespace manifest from the
			// secondary work object has been applied.
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenNamespaceDoesNotExist: placementv1alpha1.WhenNamespaceDoesNotExistOptionReportError,
			}
			primaryWork := buildWorkObject(
				primaryWorkName, memberReservedNSNameWaves,
				placementPolicyName, placementBindingName, primarySnapshotName,
				syncStrategy,
				regularConfigMapJSON,
			)
			primaryWork.Annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = "2"
			Expect(hubClient.Create(ctx, primaryWork)).To(Succeed(), "Failed to create the primary work object")
		})

		It("should update the work object statuses", func() {
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAvailableCondAllManifestsAvailableReason,
				},
			}
			primaryManifestStatuses := []placementv1alpha1.PerManifestStatus{
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       configMapName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
			}
			secondaryManifestStatuses := []placementv1alpha1.PerManifestStatus{
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "Namespace",
						Resource:   "namespaces",
						Name:       nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
			}

			primaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSNameWaves, primaryWorkName, workConds, primaryManifestStatuses)
			Eventually(primaryWorkStatusUpdatedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of the primary work object")

			secondaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSNameWaves, secondaryWorkName, workConds, secondaryManifestStatuses)
			Eventually(secondaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of the secondary work object")
		})

		It("should apply the manifests in waves across the linked work objects", func() {
			nsCreationTimestamp := retrieveCreationTimestamp(memberClient1, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})
			configMapCreationTimestamp := retrieveCreationTimestamp(memberClient1, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: nsName, Name: configMapName}})
			gap := configMapCreationTimestamp.Sub(nsCreationTimestamp)
			Expect(gap).To(BeNumerically(">=", parallelizerFixedDelay), "The config map is not created one wave after the namespace (gap: %v)", gap)
			Expect(gap).To(BeNumerically("<", 2*parallelizerFixedDelay), "The config map is not created in the wave right after the namespace (gap: %v)", gap)
		})

		It("should apply all the manifests in one pass", func() {
			// Each work object status is updated exactly twice in the first (and only effective) reconciliation:
			// once to write ahead the manifests to process, and once to report the results. Had the config map
			// failed to apply in the first reconciliation, the primary work object status would have been updated
			// again once the config map got applied.
			Expect(hubClientRecorderWaves.statusUpdateCount(primaryWorkKey)).To(Equal(2), "The status of the primary work object is not updated exactly twice")
			Expect(hubClientRecorderWaves.statusUpdateCount(secondaryWorkKey)).To(Equal(2), "The status of the secondary work object is not updated exactly twice")
		})

		AfterAll(func() {
			// There is no GC controller in the hub cluster test environment to delete the secondary work object along
			// with the primary one; delete both of them.
			for _, workName := range []string{primaryWorkName, secondaryWorkName} {
				work := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workName,
						Namespace: memberReservedNSNameWaves,
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, work))).To(Succeed(), "Failed to delete work object %s", workName)
			}

			// Remove the config map manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork objects.
			for _, workName := range []string{primaryWorkName, secondaryWorkName} {
				appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
				Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object %s", workName)

				workRemovedActual := workRemovedActual(memberReservedNSNameWaves, workName)
				Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object %s", workName)
			}

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})
})
