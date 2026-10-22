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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var _ = Describe("cleanup", func() {
	Context("orphan resources upon placement deletion (linked work objects)", Ordered, func() {
		// The two work objects are linked to each other: they share the same owner placement policy/binding and the
		// same primary placement resource snapshot; the primary work object features the linked work count
		// annotation.
		primaryWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		secondaryWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, primaryWorkName)
		placementBindingName := fmt.Sprintf(placementBindingNameTemplate, primaryWorkName)
		primarySnapshotName := fmt.Sprintf(primarySnapshotNameTemplate, primaryWorkName)
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		primaryConfigMapName := "configmap-primary"
		secondaryConfigMapName := "configmap-secondary"

		var appliedWorkOwnerRef *metav1.OwnerReference
		// The config maps as applied in the member cluster.
		var appliedPrimaryConfigMap *corev1.ConfigMap
		var appliedSecondaryConfigMap *corev1.ConfigMap

		BeforeAll(func() {
			// Create the namespace directly in the member cluster, as the work objects do not feature the
			// namespace manifest.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare the manifests.
			primaryConfigMap := configMap.DeepCopy()
			primaryConfigMap.Namespace = nsName
			primaryConfigMap.Name = primaryConfigMapName
			primaryConfigMapJSON := marshalK8sObjJSON(primaryConfigMap)

			secondaryConfigMap := configMap.DeepCopy()
			secondaryConfigMap.Namespace = nsName
			secondaryConfigMap.Name = secondaryConfigMapName
			secondaryConfigMapJSON := marshalK8sObjJSON(secondaryConfigMap)

			// Create the secondary work object first, so that the work applier can find all the linked work
			// objects as soon as it starts processing the primary work object. The secondary work object features
			// no sync strategy, as the work applier always uses the one on the primary work object.
			secondaryWork := buildWorkObject(
				secondaryWorkName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				secondaryConfigMapJSON,
			)
			setPrimaryWorkAsOwner(secondaryWork, primaryWorkName)
			Expect(hubClient.Create(ctx, secondaryWork)).To(Succeed(), "Failed to create the secondary work object")

			// Create the primary work object, which orphans the applied resources upon placement deletion.
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenPlacementDeleted: placementv1alpha1.WhenPlacementDeletedOptionOrphanResources,
			}
			primaryWork := buildWorkObject(
				primaryWorkName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				syncStrategy,
				primaryConfigMapJSON,
			)
			primaryWork.Annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = "2"
			Expect(hubClient.Create(ctx, primaryWork)).To(Succeed(), "Failed to create the primary work object")
		})

		It("should apply the manifests", func() {
			// All the manifests across the linked work objects are owned by the appliedWork object of the primary
			// work object.
			primaryAppliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, primaryWorkName)
			Eventually(primaryAppliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object for the primary work object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, primaryWorkName)

			secondaryAppliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, secondaryWorkName)
			Eventually(secondaryAppliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object for the secondary work object")

			secondaryFinalizerAddedActual := workFinalizerAddedActual(memberReservedNSName1, secondaryWorkName)
			Eventually(secondaryFinalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the secondary work object")

			primaryConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, primaryConfigMapName, appliedWorkOwnerRef)
			Eventually(primaryConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map from the primary work object")

			secondaryConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, secondaryConfigMapName, appliedWorkOwnerRef)
			Eventually(secondaryConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map from the secondary work object")

			appliedPrimaryConfigMap = &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: primaryConfigMapName}, appliedPrimaryConfigMap)).To(Succeed(), "Failed to retrieve the config map from the primary work object")
			appliedSecondaryConfigMap = &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: secondaryConfigMapName}, appliedSecondaryConfigMap)).To(Succeed(), "Failed to retrieve the config map from the secondary work object")
		})

		It("can delete the primary work object", func() {
			deleteWorkObject(primaryWorkName, memberReservedNSName1)
		})

		It("should start deleting the primary appliedWork object with the orphan propagation policy", func() {
			appliedWorkBeingDeletedWithOrphanActual := appliedWorkBeingDeletedWithOrphanActual(memberClient1, primaryWorkName)
			Eventually(appliedWorkBeingDeletedWithOrphanActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to start deleting the primary appliedWork object with the orphan propagation policy")
		})

		It("should unblock the appliedWork deletion on all applied objects", func() {
			// As there is no GC controller in the test environment, the appliedWork object stays in the pending
			// deletion state; the work applier unblocks the deletion after cleanupWaitTime, and marks the
			// appliedWork object as forcibly deleted.
			for _, configMapName := range []string{primaryConfigMapName, secondaryConfigMapName} {
				configMapOwnerRefUnblockedActual := appliedWorkOwnerRefUnblockedActual(memberClient1, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: nsName, Name: configMapName}}, appliedWorkOwnerRef)
				Eventually(configMapOwnerRefUnblockedActual, cleanupWaitTime+eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to unblock the appliedWork deletion on the config map %s", configMapName)
			}

			appliedWorkForcedDeletedActual := objectAnnotationActual(memberClient1, &placementv1alpha1.AppliedWork{ObjectMeta: metav1.ObjectMeta{Name: primaryWorkName}}, appliedWorkForcedDeletedAnnotationKey, "true")
			Eventually(appliedWorkForcedDeletedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the primary appliedWork object as forcibly deleted")
		})

		It("should remove the cleanup finalizer from the secondary work object", func() {
			// The work applier proceeds with the cleanup after 2 * cleanupWaitTime.
			secondaryFinalizerRemovedActual := workFinalizerRemovedActual(memberReservedNSName1, secondaryWorkName)
			Eventually(secondaryFinalizerRemovedActual, 2*cleanupWaitTime+eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer from the secondary work object")
		})

		It("should remove the secondary appliedWork object", func() {
			secondaryAppliedWorkAbsentActual := appliedWorkAbsentActual(memberClient1, secondaryWorkName)
			Eventually(secondaryAppliedWorkAbsentActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary appliedWork object")
		})

		It("should remove the primary work object", func() {
			primaryWorkRemovedActual := workRemovedActual(memberReservedNSName1, primaryWorkName)
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")
		})

		It("should leave the primary appliedWork object to the GC controller", func() {
			// The work applier stops waiting for the appliedWork object after 2 * cleanupWaitTime.
			appliedWorkBeingDeletedWithOrphanActual := appliedWorkBeingDeletedWithOrphanActual(memberClient1, primaryWorkName)
			Expect(appliedWorkBeingDeletedWithOrphanActual()).To(Succeed(), "The primary appliedWork object is no longer pending deletion with the orphan propagation policy")
		})

		It("should leave the applied objects in place", func() {
			Expect(objectNotRecreatedActual(memberClient1, appliedPrimaryConfigMap)()).To(Succeed(), "The config map from the primary work object has been deleted")
			Expect(objectNotRecreatedActual(memberClient1, appliedSecondaryConfigMap)()).To(Succeed(), "The config map from the secondary work object has been deleted")
		})

		AfterAll(func() {
			// There is no GC controller in the hub cluster test environment to delete the secondary work object along
			// with the primary one; delete both of them (the primary work object might still exist if the test case
			// fails before it is deleted).
			for _, workName := range []string{primaryWorkName, secondaryWorkName} {
				work := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workName,
						Namespace: memberReservedNSName1,
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, work))).To(Succeed(), "Failed to delete work object %s", workName)
			}

			// Remove the config maps manually, as there is no GC controller in the test environment.
			for _, configMapName := range []string{primaryConfigMapName, secondaryConfigMapName} {
				regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
				Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map %s", configMapName)
			}

			// Simulate the GC controller by removing the orphan/foreground deletion finalizers from the appliedWork
			// objects.
			for _, workName := range []string{primaryWorkName, secondaryWorkName} {
				appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
				Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object %s", workName)

				workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
				Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object %s", workName)
			}

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("leftover objects no longer owned by KubeFleet", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		// The config map that stays in the work object throughout the test case.
		keptConfigMapName := "configmap-kept"

		var appliedWorkOwnerRef *metav1.OwnerReference
		var keptConfigMapJSON []byte
		var appliedKeptConfigMap *corev1.ConfigMap
		// The config map as it is after the KubeFleet owner reference has been removed.
		var unownedConfigMap *corev1.ConfigMap

		BeforeAll(func() {
			// Create the namespace directly in the member cluster, as the work object does not feature the
			// namespace manifest.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare the manifests.
			regularConfigMap := configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			keptConfigMap := configMap.DeepCopy()
			keptConfigMap.Namespace = nsName
			keptConfigMap.Name = keptConfigMapName
			keptConfigMapJSON = marshalK8sObjJSON(keptConfigMap)

			// Create a new work object with no sync strategy; by default, the work applier does not take over
			// objects that it does not own.
			createWorkObject(workName, memberReservedNSName1, nil, regularConfigMapJSON, keptConfigMapJSON)
		})

		It("should apply the manifests", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			configMapAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(configMapAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map")

			keptConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, keptConfigMapName, appliedWorkOwnerRef)
			Eventually(keptConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the kept config map")

			appliedKeptConfigMap = &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: keptConfigMapName}, appliedKeptConfigMap)).To(Succeed(), "Failed to retrieve the kept config map")
		})

		It("can remove the KubeFleet owner reference from the config map", func() {
			// This mimics a user taking the object away from KubeFleet; deleting and re-creating the object (without
			// the owner reference) leads to the same result.
			unownedConfigMap = updateConfigMapInMemberCluster(memberClient1, nsName, configMapName, func(configMap *corev1.ConfigMap) {
				configMap.OwnerReferences = nil
			})
		})

		It("should report that the config map is not taken over", func() {
			// The change in the member cluster triggers no event for the work object; it is picked up at the next
			// requeue of the work object.
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAppliedCondNotAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAvailableCondNotAllManifestsAvailableReason,
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
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeNotTakenOver),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    1,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       keptConfigMapName,
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

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report that the config map is not taken over")
		})

		It("can remove the manifest from the work object", func() {
			updateWorkObject(workName, memberReservedNSName1, nil, keptConfigMapJSON)
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
						Name:       keptConfigMapName,
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

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should leave the config map as it is", func() {
			// The work applier no longer owns the config map; the leftover cleanup must not delete it.
			configMapUntouchedActual := objectUntouchedActual(memberClient1, unownedConfigMap)
			Consistently(configMapUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The config map has been changed or deleted")
		})

		It("should update the appliedWork object status", func() {
			appliedResources := []placementv1alpha1.AppliedResource{
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       keptConfigMapName,
						Namespace:  nsName,
					},
					UID: appliedKeptConfigMap.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the config maps manually, as there is no GC controller in the test environment.
			for _, name := range []string{configMapName, keptConfigMapName} {
				regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, name)
				Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map %s", name)
			}

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("leftover objects never taken over", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		// The config map that stays in the work object throughout the test case.
		keptConfigMapName := "configmap-kept"

		var keptConfigMapJSON []byte
		// The object created in the member cluster before the work object.
		var preExistingConfigMap *corev1.ConfigMap

		BeforeAll(func() {
			// Create the namespace directly in the member cluster, as the work object does not feature the
			// namespace manifest.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare the manifests.
			regularConfigMap := configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			keptConfigMap := configMap.DeepCopy()
			keptConfigMap.Namespace = nsName
			keptConfigMap.Name = keptConfigMapName
			keptConfigMapJSON = marshalK8sObjJSON(keptConfigMap)

			// Create the config map in the member cluster before the work object, as if a user or another
			// controller has created it.
			preExistingConfigMap = regularConfigMap.DeepCopy()
			preExistingConfigMap.Data = map[string]string{
				configMapDataKey: "baz",
			}
			Expect(memberClient1.Create(ctx, preExistingConfigMap)).To(Succeed(), "Failed to create the pre-existing config map")

			// Create a new work object with no sync strategy; by default, the work applier does not take over
			// pre-existing objects.
			createWorkObject(workName, memberReservedNSName1, nil, regularConfigMapJSON, keptConfigMapJSON)
		})

		It("should report that the pre-existing config map is not taken over", func() {
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAppliedCondNotAllManifestsAppliedReason,
				},
				{
					Type:   placementv1alpha1.WorkCondTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: placementv1alpha1.WorkAvailableCondNotAllManifestsAvailableReason,
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
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeNotTakenOver),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    1,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       keptConfigMapName,
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

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report that the pre-existing config map is not taken over")
		})

		It("can remove the manifest from the work object", func() {
			updateWorkObject(workName, memberReservedNSName1, nil, keptConfigMapJSON)
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
						Name:       keptConfigMapName,
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

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should leave the pre-existing config map as it is", func() {
			// The work applier has never owned the config map; the leftover cleanup must not delete it.
			configMapUntouchedActual := objectUntouchedActual(memberClient1, preExistingConfigMap)
			Consistently(configMapUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The pre-existing config map has been changed or deleted")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the config maps manually, as there is no GC controller in the test environment.
			for _, name := range []string{configMapName, keptConfigMapName} {
				regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, name)
				Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map %s", name)
			}

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("leftover objects co-owned with other sources", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		// The config map that stays in the work object throughout the test case.
		keptConfigMapName := "configmap-kept"

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularConfigMap *corev1.ConfigMap
		var keptConfigMapJSON []byte
		var appliedKeptConfigMap *corev1.ConfigMap
		var syncStrategy *placementv1alpha1.SyncStrategy
		// The object created in the member cluster before the work object.
		var preExistingConfigMap *corev1.ConfigMap

		BeforeAll(func() {
			// Create the namespace directly in the member cluster, as the work object does not feature the
			// namespace manifest.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare the manifests.
			regularConfigMap = configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			keptConfigMap := configMap.DeepCopy()
			keptConfigMap.Namespace = nsName
			keptConfigMap.Name = keptConfigMapName
			keptConfigMapJSON = marshalK8sObjJSON(keptConfigMap)

			// Create the config map in the member cluster before the work object, as if another controller has
			// created (and owns) it.
			preExistingConfigMap = regularConfigMap.DeepCopy()
			preExistingConfigMap.OwnerReferences = []metav1.OwnerReference{externalOwnerRef}
			preExistingConfigMap.Data = map[string]string{
				configMapDataKey: "baz",
			}
			Expect(memberClient1.Create(ctx, preExistingConfigMap)).To(Succeed(), "Failed to create the pre-existing config map")

			// Create a new work object that takes over pre-existing objects as a co-owner.
			syncStrategy = &placementv1alpha1.SyncStrategy{
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
				WhenOwnedByOthers: placementv1alpha1.WhenOwnedByOthersOptionShareOwnership,
			}
			createWorkObject(workName, memberReservedNSName1, syncStrategy, regularConfigMapJSON, keptConfigMapJSON)
		})

		It("should apply the manifests", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			// The pre-existing config map is co-owned by KubeFleet and the other source.
			wantConfigMap := regularConfigMap.DeepCopy()
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{externalOwnerRef, *appliedWorkOwnerRef}
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Eventually(configMapObjectActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to take over the config map as a co-owner")

			keptConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, keptConfigMapName, appliedWorkOwnerRef)
			Eventually(keptConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the kept config map")

			appliedKeptConfigMap = &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: keptConfigMapName}, appliedKeptConfigMap)).To(Succeed(), "Failed to retrieve the kept config map")
		})

		It("can remove the manifest of the co-owned config map from the work object", func() {
			updateWorkObject(workName, memberReservedNSName1, syncStrategy, keptConfigMapJSON)
		})

		It("should drop the KubeFleet ownership of the config map, but not delete it", func() {
			// Only the owner reference to the appliedWork object is removed; the other owner, and the data applied
			// while KubeFleet co-owned the object, are left alone.
			wantConfigMap := regularConfigMap.DeepCopy()
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{externalOwnerRef}
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Eventually(configMapObjectActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to drop the KubeFleet ownership of the config map")
			Consistently(configMapObjectActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The config map has been changed or deleted after KubeFleet dropped its ownership")

			Expect(objectNotRecreatedActual(memberClient1, preExistingConfigMap)()).To(Succeed(), "The config map has been re-created")
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
						Name:       keptConfigMapName,
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

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should update the appliedWork object status", func() {
			appliedResources := []placementv1alpha1.AppliedResource{
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       keptConfigMapName,
						Namespace:  nsName,
					},
					UID: appliedKeptConfigMap.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the config maps manually, as there is no GC controller in the test environment.
			for _, name := range []string{configMapName, keptConfigMapName} {
				regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, name)
				Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map %s", name)
			}

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})
})
