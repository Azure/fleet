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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var _ = Describe("apply ops", func() {
	Context("client-side apply (manifest updates and changes made by third parties)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var originalConfigMap *corev1.ConfigMap
		var updatedConfigMap *corev1.ConfigMap
		var updatedConfigMapJSON []byte

		BeforeAll(func() {
			// Create the namespace directly in the member cluster, as the work object does not feature the
			// namespace manifest.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare the original manifest.
			originalConfigMap = configMap.DeepCopy()
			originalConfigMap.Namespace = nsName
			originalConfigMap.Labels = map[string]string{
				"label-kept":    "v1",
				"label-removed": "v1",
			}
			originalConfigMap.Annotations = map[string]string{
				"annotation-kept": "v1",
			}
			originalConfigMap.Data = map[string]string{
				configMapDataKey: "bar",
				"data-kept":      "v1",
			}
			originalConfigMapJSON := marshalK8sObjJSON(originalConfigMap)

			// Prepare the updated manifest, which changes a label value, removes a label, adds an annotation, and
			// changes a data value.
			updatedConfigMap = originalConfigMap.DeepCopy()
			updatedConfigMap.Labels = map[string]string{
				"label-kept": "v2",
			}
			updatedConfigMap.Annotations = map[string]string{
				"annotation-kept":  "v1",
				"annotation-added": "v2",
			}
			updatedConfigMap.Data = map[string]string{
				configMapDataKey: "baz",
				"data-kept":      "v1",
			}
			updatedConfigMapJSON = marshalK8sObjJSON(updatedConfigMap)

			// Create a new work object with no sync strategy; by default, the work applier uses client-side apply
			// and overwrites drifts.
			createWorkObject(workName, memberReservedNSName1, nil, originalConfigMapJSON)
		})

		It("should apply the manifest", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			wantConfigMap := originalConfigMap.DeepCopy()
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{*appliedWorkOwnerRef}
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Eventually(configMapObjectActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map")
		})

		It("can make changes to the config map as a third party", func() {
			// Add a label and a data key that the manifest does not specify, and change a data value that the
			// manifest specifies.
			updateConfigMapInMemberCluster(memberClient1, nsName, configMapName, func(configMap *corev1.ConfigMap) {
				configMap.Labels[memberOnlyLabelKey] = memberOnlyLabelValue
				configMap.Data[memberOnlyLabelKey] = memberOnlyLabelValue
				configMap.Data[configMapDataKey] = "changed-by-third-party"
			})
		})

		It("should revert the change on the field that the manifest specifies, and keep the other changes", func() {
			// The change in the member cluster triggers no event for the work object; the change is reverted at the
			// next requeue of the work object.
			wantConfigMap := originalConfigMap.DeepCopy()
			wantConfigMap.Labels[memberOnlyLabelKey] = memberOnlyLabelValue
			wantConfigMap.Data[memberOnlyLabelKey] = memberOnlyLabelValue
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{*appliedWorkOwnerRef}
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Eventually(configMapObjectActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to revert the change on the field that the manifest specifies")
		})

		It("can update the manifest", func() {
			updateWorkObject(workName, memberReservedNSName1, nil, updatedConfigMapJSON)
		})

		It("should apply the updated manifest, and keep the changes made by the third party", func() {
			// The three-way merge removes the label that the original manifest specifies but the updated one does
			// not, while leaving the label and the data key added by the third party alone.
			wantConfigMap := updatedConfigMap.DeepCopy()
			wantConfigMap.Labels[memberOnlyLabelKey] = memberOnlyLabelValue
			wantConfigMap.Data[memberOnlyLabelKey] = memberOnlyLabelValue
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{*appliedWorkOwnerRef}
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Eventually(configMapObjectActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the updated manifest")
		})

		It("should update the work object status", func() {
			// The work object conditions observe the latest generation of the work object.
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
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the config map manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("client-side apply falling back to server-side apply (oversized objects)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var oversizedConfigMap *corev1.ConfigMap
		var updatedOversizedConfigMap *corev1.ConfigMap

		BeforeAll(func() {
			// Create the namespace directly in the member cluster, as the work object does not feature the
			// namespace manifest.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare a config map that is too large for its last applied configuration to fit in an annotation
			// (the total size of all annotations on an object is capped at 256 KiB).
			oversizedConfigMap = configMap.DeepCopy()
			oversizedConfigMap.Namespace = nsName
			oversizedConfigMap.Data = map[string]string{
				configMapDataKey: "bar",
				"large":          strings.Repeat("a", 300*1024),
			}
			oversizedConfigMapJSON := marshalK8sObjJSON(oversizedConfigMap)

			updatedOversizedConfigMap = oversizedConfigMap.DeepCopy()
			updatedOversizedConfigMap.Data[configMapDataKey] = "baz"

			// Create a new work object with no sync strategy; by default, the work applier uses client-side apply.
			createWorkObject(workName, memberReservedNSName1, nil, oversizedConfigMapJSON)
		})

		It("should apply the manifest", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			wantConfigMap := oversizedConfigMap.DeepCopy()
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{*appliedWorkOwnerRef}
			configMapObjectWithDataDigestActual := configMapObjectWithDataDigestActual(memberClient1, wantConfigMap)
			Eventually(configMapObjectWithDataDigestActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map")
		})

		It("should leave the last applied configuration annotation empty", func() {
			objectAnnotationActual := objectAnnotationActual(memberClient1, oversizedConfigMap, placementv1alpha1.LastAppliedConfigAnnotationKey, "")
			Expect(objectAnnotationActual()).To(Succeed(), "Failed to verify the last applied configuration annotation")
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
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("can update the manifest", func() {
			updateWorkObject(workName, memberReservedNSName1, nil, marshalK8sObjJSON(updatedOversizedConfigMap))
		})

		It("should apply the updated manifest", func() {
			wantConfigMap := updatedOversizedConfigMap.DeepCopy()
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{*appliedWorkOwnerRef}
			configMapObjectWithDataDigestActual := configMapObjectWithDataDigestActual(memberClient1, wantConfigMap)
			Eventually(configMapObjectWithDataDigestActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the updated config map")
		})

		It("should have applied the updated manifest via server-side apply", func() {
			// Had the work applier used client-side apply (a patch op), the changed field would have been managed
			// by an Update operation instead.
			fieldManagersActual := fieldManagersActual(memberClient1, oversizedConfigMap, []string{"f:data", fmt.Sprintf("f:%s", configMapDataKey)}, []string{
				fmt.Sprintf("%s/%s", fieldManagerName, metav1.ManagedFieldsOperationApply),
			})
			Eventually(fieldManagersActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to verify the field managers of the changed field")
		})

		It("should update the work object status (updated manifest)", func() {
			// The work object conditions observe the latest generation of the work object.
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
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the config map manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("server-side apply (field ownership conflicts)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularConfigMap *corev1.ConfigMap
		var regularConfigMapJSON []byte
		// The object created in the member cluster before the work object.
		var preExistingConfigMap *corev1.ConfigMap

		BeforeAll(func() {
			// Create the namespace directly in the member cluster, as the work object does not feature the
			// namespace manifest.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare the manifest.
			regularConfigMap = configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON = marshalK8sObjJSON(regularConfigMap)

			// Create the config map in the member cluster before the work object, with a different value for the
			// data key that the manifest specifies; the field is managed by another field manager.
			preExistingConfigMap = regularConfigMap.DeepCopy()
			preExistingConfigMap.Data = map[string]string{
				configMapDataKey: "baz",
			}
			Expect(memberClient1.Create(ctx, preExistingConfigMap, client.FieldOwner(thirdPartyFieldManagerName))).To(Succeed(), "Failed to create the pre-existing config map")

			// Create a new work object that uses server-side apply without forcing conflicts (the server-side apply
			// options are absent), and that always takes over pre-existing objects.
			syncStrategy := &placementv1alpha1.SyncStrategy{
				ApplyMethod:       placementv1alpha1.ApplyMethodServerSideApply,
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
			}
			createWorkObject(workName, memberReservedNSName1, syncStrategy, regularConfigMapJSON)
		})

		It("should prepare an appliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)
		})

		It("should update the work object status (conflict)", func() {
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
							Reason:             string(ApplyResTypeFailedToApply),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should report the conflict as the cause of the failure", func() {
			manifestAppliedCondMessageActual := manifestAppliedCondMessageActual(memberReservedNSName1, workName, 0, fmt.Sprintf("conflict with %q", thirdPartyFieldManagerName))
			Eventually(manifestAppliedCondMessageActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the conflict as the cause of the failure")
		})

		It("should take over the config map, but not apply the manifest", func() {
			// The check spans at least one periodic requeue of the work object.
			wantConfigMap := preExistingConfigMap.DeepCopy()
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{*appliedWorkOwnerRef}
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Consistently(configMapObjectActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The config map has been changed unexpectedly")
		})

		It("can update the work object to force conflicts", func() {
			syncStrategy := &placementv1alpha1.SyncStrategy{
				ApplyMethod: placementv1alpha1.ApplyMethodServerSideApply,
				ServerSideApplyOptions: &placementv1alpha1.ServerSideApplyOptions{
					ForceConflicts: true,
				},
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
			}
			updateWorkObject(workName, memberReservedNSName1, syncStrategy, regularConfigMapJSON)
		})

		It("should apply the manifest", func() {
			wantConfigMap := regularConfigMap.DeepCopy()
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{*appliedWorkOwnerRef}
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Eventually(configMapObjectActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map")

			Expect(objectNotRecreatedActual(memberClient1, preExistingConfigMap)()).To(Succeed(), "The config map has been re-created instead of being taken over")
		})

		It("should take over the ownership of the conflicting field", func() {
			fieldManagersActual := fieldManagersActual(memberClient1, preExistingConfigMap, []string{"f:data", fmt.Sprintf("f:%s", configMapDataKey)}, []string{
				fmt.Sprintf("%s/%s", fieldManagerName, metav1.ManagedFieldsOperationApply),
			})
			Eventually(fieldManagersActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to take over the ownership of the conflicting field")
		})

		It("should update the work object status (applied)", func() {
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
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the config map manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("namespace auto-creation (default)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The namespace does not exist in the member cluster at first; the work applier creates it.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var appliedConfigMap *corev1.ConfigMap

		BeforeAll(func() {
			// Prepare the manifest.
			regularConfigMap := configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Create a new work object with no sync strategy; by default, the work applier creates the namespace of
			// a manifest if the namespace does not exist yet.
			createWorkObject(workName, memberReservedNSName1, nil, regularConfigMapJSON)
		})

		It("should prepare an appliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)
		})

		It("should create the namespace, but not manage it", func() {
			namespaceNotManagedActual := namespaceNotManagedActual(memberClient1, nsName)
			Eventually(namespaceNotManagedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the namespace")
		})

		It("should apply the manifest", func() {
			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map")

			appliedConfigMap = &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, appliedConfigMap)).To(Succeed(), "Failed to retrieve the config map")
		})

		It("should update the work object status", func() {
			// The namespace is not reported, as it is not a manifest.
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
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should update the appliedWork object status", func() {
			// The namespace is not reported as an applied resource.
			appliedResources := []placementv1alpha1.AppliedResource{
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       configMapName,
						Namespace:  nsName,
					},
					UID: appliedConfigMap.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("can delete the work object", func() {
			deleteWorkObject(workName, memberReservedNSName1)
		})

		It("should remove the work object", func() {
			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")
		})

		It("should leave the namespace as it is", func() {
			namespaceNotManagedActual := namespaceNotManagedActual(memberClient1, nsName)
			Consistently(namespaceNotManagedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The namespace has been changed after the work object is deleted")
		})

		AfterAll(func() {
			// The work object might still exist if the test case fails before the work object is deleted.
			work := &placementv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: memberReservedNSName1,
				},
			}
			Expect(client.IgnoreNotFound(hubClient.Delete(ctx, work))).To(Succeed(), "Failed to delete the work object")

			// Remove the config map manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

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
