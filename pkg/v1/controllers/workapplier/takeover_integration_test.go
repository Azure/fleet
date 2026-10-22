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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var _ = Describe("takeover", func() {
	Context("pre-existing objects with the default sync strategy (report error, no takeover)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		// The objects created in the member cluster before the work object.
		var preExistingConfigMap *corev1.ConfigMap
		var preExistingDeploy *appsv1.Deployment

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

			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create the objects in the member cluster before the work object, as if a user or another controller
			// has created them. The config map differs from its manifest, while the deployment is identical to its
			// manifest; the default sync strategy forbids takeovers regardless of diffs.
			preExistingConfigMap = regularConfigMap.DeepCopy()
			preExistingConfigMap.Data = map[string]string{
				configMapDataKey: "baz",
			}
			Expect(memberClient1.Create(ctx, preExistingConfigMap)).To(Succeed(), "Failed to create the pre-existing config map")

			preExistingDeploy = regularDeploy.DeepCopy()
			Expect(memberClient1.Create(ctx, preExistingDeploy)).To(Succeed(), "Failed to create the pre-existing deployment")

			// Create a new work object with no sync strategy; the work applier falls back to the default one.
			createWorkObject(workName, memberReservedNSName1, nil, regularConfigMapJSON, regularDeployJSON)
		})

		It("should add cleanup finalizer to the work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(memberReservedNSName1, workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the work object")
		})

		It("should prepare an appliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
		})

		It("should update the work object status", func() {
			// Prepare the status information.
			//
			// The manifests are not applied and have no Available condition; no diff is calculated either, as the
			// work applier never attempts to take over the objects.
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
						APIGroup:   "apps",
						APIVersion: "v1",
						Kind:       "Deployment",
						Resource:   "deployments",
						Name:       deployName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeNotTakenOver),
							ObservedGeneration: 1,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should report the missing ownership as the cause of the failures", func() {
			for _, ordinal := range []int{0, 1} {
				manifestAppliedCondMessageActual := manifestAppliedCondMessageActual(memberReservedNSName1, workName, ordinal, notTakenOverErrMsg)
				Eventually(manifestAppliedCondMessageActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the missing ownership for the manifest with ordinal %d", ordinal)
			}
		})

		It("should not report any applied resource in the appliedWork object status", func() {
			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, nil)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("should not take over or modify the pre-existing objects", func() {
			// The check spans at least one periodic requeue of the work object.
			configMapUntouchedActual := objectUntouchedActual(memberClient1, preExistingConfigMap)
			deployUntouchedActual := objectUntouchedActual(memberClient1, preExistingDeploy)
			Consistently(func() error {
				if err := configMapUntouchedActual(); err != nil {
					return err
				}
				return deployUntouchedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The pre-existing objects have been modified")
		})

		It("can delete the work object", func() {
			deleteWorkObject(workName, memberReservedNSName1)
		})

		It("should start deleting the appliedWork object in the foreground", func() {
			appliedWorkBeingDeletedActual := appliedWorkBeingDeletedInForegroundActual(memberClient1, workName)
			Eventually(appliedWorkBeingDeletedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to start deleting the appliedWork object in the foreground")
		})

		It("should remove the work object", func() {
			// As there is no GC controller in the test environment, the appliedWork object will be stuck in the
			// pending deletion state; the test case does not simulate the GC controller here, so that the work
			// applier goes through the forced cleanup path, which visits all the manifests that have an Applied
			// condition (including those that have not been taken over). The work applier proceeds to remove the
			// cleanup finalizer after 2 * cleanupWaitTime.
			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, 2*cleanupWaitTime+eventuallyDuration, eventuallyInterval*2).Should(Succeed(), "Failed to remove the work object")
		})

		It("should leave the pre-existing objects untouched", func() {
			Expect(objectUntouchedActual(memberClient1, preExistingConfigMap)()).To(Succeed(), "The pre-existing config map has been modified")
			Expect(objectUntouchedActual(memberClient1, preExistingDeploy)()).To(Succeed(), "The pre-existing deployment has been modified")
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

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// Remove the pre-existing objects; the work applier never manages them.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			regularDeployRemovedActual := regularDeployRemovedActual(memberClient1, nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("always take over", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularConfigMap *corev1.ConfigMap
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
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Create the config map in the member cluster before the work object, as if a user or another
			// controller has created it. The pre-existing config map differs from its manifest in a field that the
			// manifest specifies (the data), and features a label that the manifest does not specify.
			preExistingConfigMap = regularConfigMap.DeepCopy()
			preExistingConfigMap.Labels = map[string]string{
				memberOnlyLabelKey: memberOnlyLabelValue,
			}
			preExistingConfigMap.Data = map[string]string{
				configMapDataKey: "baz",
			}
			Expect(memberClient1.Create(ctx, preExistingConfigMap)).To(Succeed(), "Failed to create the pre-existing config map")

			// Create a new work object that always takes over pre-existing objects; the API server fills in the
			// defaults for the other fields in the sync strategy.
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
			}
			createWorkObject(workName, memberReservedNSName1, syncStrategy, regularConfigMapJSON)
		})

		It("should prepare an appliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)
		})

		It("should take over the pre-existing config map and apply the manifest", func() {
			// The data is overwritten with the value from the manifest, while the label that the manifest does not
			// specify is left alone.
			wantConfigMap := regularConfigMap.DeepCopy()
			wantConfigMap.Labels = map[string]string{
				memberOnlyLabelKey: memberOnlyLabelValue,
			}
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{*appliedWorkOwnerRef}
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Eventually(configMapObjectActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to take over the config map")

			Expect(objectNotRecreatedActual(memberClient1, preExistingConfigMap)()).To(Succeed(), "The config map has been re-created instead of being taken over")
		})

		It("should update the work object status", func() {
			// Prepare the status information.
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
					UID: preExistingConfigMap.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
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

	Context("take over if no diff (no diff)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		// The objects created in the member cluster before the work object.
		var preExistingNS *corev1.Namespace
		var preExistingDeploy *appsv1.Deployment

		BeforeAll(func() {
			// Prepare the manifests.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create the objects in the member cluster before the work object, as if a user or another controller
			// has created them; the objects are identical to their manifests.
			preExistingNS = regularNS.DeepCopy()
			Expect(memberClient1.Create(ctx, preExistingNS)).To(Succeed(), "Failed to create the pre-existing namespace")

			preExistingDeploy = regularDeploy.DeepCopy()
			Expect(memberClient1.Create(ctx, preExistingDeploy)).To(Succeed(), "Failed to create the pre-existing deployment")

			// Create a new work object that takes over pre-existing objects only if they have no diff; the API
			// server fills in the defaults for the other fields in the sync strategy (partial comparison).
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionTakeOverIfNoDiff,
			}
			createWorkObject(workName, memberReservedNSName1, syncStrategy, regularNSJSON, regularDeployJSON)
		})

		It("should prepare an appliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)
		})

		It("should take over the pre-existing objects", func() {
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to take over the namespace object")

			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(memberClient1, nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to take over the deployment object")

			Expect(objectNotRecreatedActual(memberClient1, preExistingNS)()).To(Succeed(), "The namespace has been re-created instead of being taken over")
			Expect(objectNotRecreatedActual(memberClient1, preExistingDeploy)()).To(Succeed(), "The deployment has been re-created instead of being taken over")
		})

		It("should update the work object status (deployment not yet available)", func() {
			// Verify that the manifests have been applied before marking the deployment as available; the takeover
			// and the apply op are separate writes, and the status update would otherwise race with the apply op.
			//
			// Note that the API server bumps the generation of a deployment upon annotation changes (in addition to
			// spec changes); the apply op adds the KubeFleet annotations to the taken-over deployment, which brings
			// its generation to 2.
			workConds := []metav1.Condition{
				{
					Type:   placementv1alpha1.WorkCondTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: placementv1alpha1.WorkAppliedCondAllManifestsAppliedReason,
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
						APIGroup:   "apps",
						APIVersion: "v1",
						Kind:       "Deployment",
						Resource:   "deployments",
						Name:       deployName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 2,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionFalse,
							Reason:             string(AvailabilityResultTypeNotYetAvailable),
							ObservedGeneration: 2,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(memberClient1, nsName, deployName)
		})

		It("should update the work object status (all available)", func() {
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
						APIGroup:   "apps",
						APIVersion: "v1",
						Kind:       "Deployment",
						Resource:   "deployments",
						Name:       deployName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyResTypeApplied),
							ObservedGeneration: 2,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 2,
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
						Kind:       "Namespace",
						Resource:   "namespaces",
						Name:       nsName,
					},
					UID: preExistingNS.UID,
				},
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
						Ordinal:    1,
						APIGroup:   "apps",
						APIVersion: "v1",
						Kind:       "Deployment",
						Resource:   "deployments",
						Name:       deployName,
						Namespace:  nsName,
					},
					UID: preExistingDeploy.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the deployment manually, as there is no GC controller in the test environment.
			regularDeployRemovedActual := regularDeployRemovedActual(memberClient1, nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("take over if no diff (diffs in managed fields, partial comparison)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularConfigMap *corev1.ConfigMap
		// The objects created in the member cluster before the work object.
		var preExistingConfigMap *corev1.ConfigMap
		var preExistingDeploy *appsv1.Deployment
		// The timestamp when the diffs on the deployment were first observed.
		var deployFirstDiffedTimestamp metav1.Time

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

			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create the objects in the member cluster before the work object, as if a user or another controller
			// has created them; each object differs from its manifest in a field that the manifest specifies.
			preExistingConfigMap = regularConfigMap.DeepCopy()
			preExistingConfigMap.Data = map[string]string{
				configMapDataKey: "baz",
			}
			Expect(memberClient1.Create(ctx, preExistingConfigMap)).To(Succeed(), "Failed to create the pre-existing config map")

			preExistingDeploy = regularDeploy.DeepCopy()
			preExistingDeploy.Spec.Replicas = ptr.To(int32(2))
			Expect(memberClient1.Create(ctx, preExistingDeploy)).To(Succeed(), "Failed to create the pre-existing deployment")

			// Create a new work object that takes over pre-existing objects only if they have no diff.
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionTakeOverIfNoDiff,
				ComparisonOption:  placementv1alpha1.ComparisonOptionPartialComparison,
			}
			createWorkObject(workName, memberReservedNSName1, syncStrategy, regularConfigMapJSON, regularDeployJSON)
		})

		It("should prepare an appliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)
		})

		It("should update the work object status", func() {
			// Prepare the status information.
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
							Reason:             string(ApplyResTypeFailedToTakeOver),
							ObservedGeneration: 0,
						},
					},
					DiffDetails: &placementv1alpha1.DiffDetails{
						ObservedInMemberClusterGeneration: ptr.To(int64(0)),
						ObservedDiffs: []placementv1alpha1.PatchDetail{
							{
								Path:          fmt.Sprintf("/data/%s", configMapDataKey),
								ValueInMember: "baz",
								ValueInHub:    "bar",
							},
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    1,
						APIGroup:   "apps",
						APIVersion: "v1",
						Kind:       "Deployment",
						Resource:   "deployments",
						Name:       deployName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeFailedToTakeOver),
							ObservedGeneration: 1,
						},
					},
					DiffDetails: &placementv1alpha1.DiffDetails{
						ObservedInMemberClusterGeneration: ptr.To(int64(1)),
						ObservedDiffs: []placementv1alpha1.PatchDetail{
							{
								Path:          "/spec/replicas",
								ValueInMember: "2",
								ValueInHub:    "1",
							},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")

			deployFirstDiffedTimestamp = retrieveManifestFirstDiffedTimestamp(memberReservedNSName1, workName, 1)
		})

		It("should not take over or modify the pre-existing objects", func() {
			// The check spans at least one periodic requeue of the work object.
			configMapUntouchedActual := objectUntouchedActual(memberClient1, preExistingConfigMap)
			deployUntouchedActual := objectUntouchedActual(memberClient1, preExistingDeploy)
			Consistently(func() error {
				if err := configMapUntouchedActual(); err != nil {
					return err
				}
				return deployUntouchedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The pre-existing objects have been modified")
		})

		It("can resolve the diffs on the config map in the member cluster", func() {
			gotConfigMap := &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, gotConfigMap)).To(Succeed(), "Failed to retrieve the config map")
			gotConfigMap.Data = regularConfigMap.Data
			Expect(memberClient1.Update(ctx, gotConfigMap)).To(Succeed(), "Failed to resolve the diffs on the config map")
		})

		It("should take over the config map", func() {
			// The change in the member cluster triggers no event for the work object; the config map is taken over
			// at the next requeue of the work object. The work object stays in the slow backoff stage at this
			// moment, where requeues are at most 15 seconds apart; allow more time for the next attempt to happen.
			wantConfigMap := regularConfigMap.DeepCopy()
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{*appliedWorkOwnerRef}
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Eventually(configMapObjectActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to take over the config map")

			Expect(objectNotRecreatedActual(memberClient1, preExistingConfigMap)()).To(Succeed(), "The config map has been re-created instead of being taken over")
		})

		It("should update the work object status (diffs resolved on the config map only)", func() {
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
						APIGroup:   "apps",
						APIVersion: "v1",
						Kind:       "Deployment",
						Resource:   "deployments",
						Name:       deployName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeFailedToTakeOver),
							ObservedGeneration: 1,
						},
					},
					DiffDetails: &placementv1alpha1.DiffDetails{
						ObservedInMemberClusterGeneration: ptr.To(int64(1)),
						ObservedDiffs: []placementv1alpha1.PatchDetail{
							{
								Path:          "/spec/replicas",
								ValueInMember: "2",
								ValueInHub:    "1",
							},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should keep the timestamp when the diffs on the deployment were first observed", func() {
			// The work object status has been rewritten since the timestamp was captured (the config map has been
			// taken over).
			gotTimestamp := retrieveManifestFirstDiffedTimestamp(memberReservedNSName1, workName, 1)
			Expect(gotTimestamp.Equal(&deployFirstDiffedTimestamp)).To(BeTrue(), "first diffed timestamp = %v, want %v", gotTimestamp, deployFirstDiffedTimestamp)
		})

		It("should not take over or modify the pre-existing deployment", func() {
			deployUntouchedActual := objectUntouchedActual(memberClient1, preExistingDeploy)
			Consistently(deployUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The pre-existing deployment has been modified")
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
						Name:       configMapName,
						Namespace:  nsName,
					},
					UID: preExistingConfigMap.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the objects manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			regularDeployRemovedActual := regularDeployRemovedActual(memberClient1, nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("take over if no diff (diffs in unmanaged fields only, partial comparison)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularConfigMap *corev1.ConfigMap
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
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Create the config map in the member cluster before the work object, as if a user or another
			// controller has created it. The pre-existing config map is identical to its manifest, except for a
			// label that the manifest does not specify.
			preExistingConfigMap = regularConfigMap.DeepCopy()
			preExistingConfigMap.Labels = map[string]string{
				memberOnlyLabelKey: memberOnlyLabelValue,
			}
			Expect(memberClient1.Create(ctx, preExistingConfigMap)).To(Succeed(), "Failed to create the pre-existing config map")

			// Create a new work object that takes over pre-existing objects only if they have no diff; partial
			// comparison considers only the fields that the manifest specifies.
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionTakeOverIfNoDiff,
				ComparisonOption:  placementv1alpha1.ComparisonOptionPartialComparison,
			}
			createWorkObject(workName, memberReservedNSName1, syncStrategy, regularConfigMapJSON)
		})

		It("should prepare an appliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)
		})

		It("should take over the pre-existing config map", func() {
			// The label that the manifest does not specify is left alone.
			wantConfigMap := regularConfigMap.DeepCopy()
			wantConfigMap.Labels = map[string]string{
				memberOnlyLabelKey: memberOnlyLabelValue,
			}
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{*appliedWorkOwnerRef}
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Eventually(configMapObjectActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to take over the config map")

			Expect(objectNotRecreatedActual(memberClient1, preExistingConfigMap)()).To(Succeed(), "The config map has been re-created instead of being taken over")
		})

		It("should update the work object status", func() {
			// No diff is reported.
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
					UID: preExistingConfigMap.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
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

	Context("take over if no diff (diffs in unmanaged fields only, full comparison)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		// The object created in the member cluster before the work object.
		var preExistingConfigMap *corev1.ConfigMap

		BeforeAll(func() {
			// Create the namespace directly in the member cluster, as the work object does not feature the
			// namespace manifest.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare the manifest.
			regularConfigMap := configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Create the config map in the member cluster before the work object, as if a user or another
			// controller has created it. The pre-existing config map is identical to its manifest, except for a
			// label that the manifest does not specify.
			preExistingConfigMap = regularConfigMap.DeepCopy()
			preExistingConfigMap.Labels = map[string]string{
				memberOnlyLabelKey: memberOnlyLabelValue,
			}
			Expect(memberClient1.Create(ctx, preExistingConfigMap)).To(Succeed(), "Failed to create the pre-existing config map")

			// Create a new work object that takes over pre-existing objects only if they have no diff; full
			// comparison considers all the fields, including those that the manifest does not specify.
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionTakeOverIfNoDiff,
				ComparisonOption:  placementv1alpha1.ComparisonOptionFullComparison,
			}
			createWorkObject(workName, memberReservedNSName1, syncStrategy, regularConfigMapJSON)
		})

		It("should prepare an appliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
		})

		It("should update the work object status", func() {
			// The label is reported as a diff, which exists only in the member cluster.
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
							Reason:             string(ApplyResTypeFailedToTakeOver),
							ObservedGeneration: 0,
						},
					},
					DiffDetails: &placementv1alpha1.DiffDetails{
						ObservedInMemberClusterGeneration: ptr.To(int64(0)),
						ObservedDiffs: []placementv1alpha1.PatchDetail{
							{
								Path:          fmt.Sprintf("/metadata/labels/%s", memberOnlyLabelKey),
								ValueInMember: memberOnlyLabelValue,
							},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should not take over or modify the pre-existing config map", func() {
			// The check spans at least one periodic requeue of the work object.
			configMapUntouchedActual := objectUntouchedActual(memberClient1, preExistingConfigMap)
			Consistently(configMapUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The pre-existing config map has been modified")
		})

		It("should not report any applied resource in the appliedWork object status", func() {
			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, nil)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// Remove the pre-existing config map; the work applier never manages it.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("co-ownership", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularConfigMap *corev1.ConfigMap
		var regularConfigMapJSON []byte
		// The object created in the member cluster before the work object.
		var preExistingConfigMap *corev1.ConfigMap
		// The config map as it is after KubeFleet has taken it over as a co-owner.
		var coOwnedConfigMap *corev1.ConfigMap

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

			// Create the config map in the member cluster before the work object, as if another controller has
			// created (and owns) it. The pre-existing config map differs from its manifest in a field that the
			// manifest specifies (the data).
			preExistingConfigMap = regularConfigMap.DeepCopy()
			preExistingConfigMap.OwnerReferences = []metav1.OwnerReference{externalOwnerRef}
			preExistingConfigMap.Data = map[string]string{
				configMapDataKey: "baz",
			}
			Expect(memberClient1.Create(ctx, preExistingConfigMap)).To(Succeed(), "Failed to create the pre-existing config map")

			// Create a new work object that always takes over pre-existing objects, but forbids co-ownership.
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
				WhenOwnedByOthers: placementv1alpha1.WhenOwnedByOthersOptionReportError,
			}
			createWorkObject(workName, memberReservedNSName1, syncStrategy, regularConfigMapJSON)
		})

		It("should prepare an appliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)
		})

		It("should update the work object status (co-ownership disallowed)", func() {
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
							Reason:             string(ApplyResTypeFailedToTakeOver),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should report the existing owner as the cause of the failure", func() {
			manifestAppliedCondMessageActual := manifestAppliedCondMessageActual(memberReservedNSName1, workName, 0, ownedByOthersErrMsg)
			Eventually(manifestAppliedCondMessageActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the existing owner as the cause of the failure")
		})

		It("should not take over or modify the pre-existing config map", func() {
			// The check spans at least one periodic requeue of the work object.
			configMapUntouchedActual := objectUntouchedActual(memberClient1, preExistingConfigMap)
			Consistently(configMapUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The pre-existing config map has been modified")
		})

		It("can update the work object to allow co-ownership", func() {
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
				WhenOwnedByOthers: placementv1alpha1.WhenOwnedByOthersOptionShareOwnership,
			}
			updateWorkObject(workName, memberReservedNSName1, syncStrategy, regularConfigMapJSON)
		})

		It("should take over the config map as a co-owner and apply the manifest", func() {
			// The existing owner is kept.
			wantConfigMap := regularConfigMap.DeepCopy()
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{externalOwnerRef, *appliedWorkOwnerRef}
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Eventually(configMapObjectActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to take over the config map as a co-owner")

			Expect(objectNotRecreatedActual(memberClient1, preExistingConfigMap)()).To(Succeed(), "The config map has been re-created instead of being taken over")

			coOwnedConfigMap = &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, coOwnedConfigMap)).To(Succeed(), "Failed to retrieve the config map")
		})

		It("should update the work object status (co-owned)", func() {
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

		It("should update the appliedWork object status (co-owned)", func() {
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
					UID: preExistingConfigMap.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("can update the work object to disallow co-ownership again", func() {
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
				WhenOwnedByOthers: placementv1alpha1.WhenOwnedByOthersOptionReportError,
			}
			updateWorkObject(workName, memberReservedNSName1, syncStrategy, regularConfigMapJSON)
		})

		It("should update the work object status (co-ownership disallowed after the takeover)", func() {
			// KubeFleet still co-owns the config map, but may no longer apply the manifest to it.
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

		It("should report the co-ownership as the cause of the failure", func() {
			manifestAppliedCondMessageActual := manifestAppliedCondMessageActual(memberReservedNSName1, workName, 0, coOwnershipDisallowedErrMsg)
			Eventually(manifestAppliedCondMessageActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the co-ownership as the cause of the failure")
		})

		It("should leave the co-owned config map as it is", func() {
			// The owner references stay as they are; the check spans at least one periodic requeue of the work
			// object.
			configMapUntouchedActual := objectUntouchedActual(memberClient1, coOwnedConfigMap)
			Consistently(configMapUntouchedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The co-owned config map has been modified")
		})

		It("should not report any applied resource in the appliedWork object status", func() {
			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, nil)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
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

	Context("objects placed by another work object (duplicate placement)", Ordered, func() {
		// The two work objects are not linked: each is the primary (and only) work object of its own placement
		// binding. Both place the same config map.
		workNameA := fmt.Sprintf(workNameTemplate, utils.RandStr())
		workNameB := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRefA *metav1.OwnerReference
		var appliedWorkOwnerRefB *metav1.OwnerReference
		// The config map as placed by each work object; the data differs.
		var configMapFromA *corev1.ConfigMap
		var configMapFromB *corev1.ConfigMap
		var configMapFromBJSON []byte
		// The config map as created in the member cluster via work A.
		var placedConfigMap *corev1.ConfigMap

		BeforeAll(func() {
			// Create the namespace directly in the member cluster, as the work objects do not feature the
			// namespace manifest.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare the manifests.
			configMapFromA = configMap.DeepCopy()
			configMapFromA.Namespace = nsName
			configMapFromAJSON := marshalK8sObjJSON(configMapFromA)

			configMapFromB = configMapFromA.DeepCopy()
			configMapFromB.Data = map[string]string{
				configMapDataKey: "baz",
			}
			configMapFromBJSON = marshalK8sObjJSON(configMapFromB)

			// Create work A with the default sync strategy.
			createWorkObject(workNameA, memberReservedNSName1, nil, configMapFromAJSON)
		})

		It("should apply the config map via work A", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workNameA)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object for work A")
			appliedWorkOwnerRefA = prepareAppliedWorkOwnerRef(memberClient1, workNameA)

			configMapObjectAppliedActual := configMapObjectAppliedActual(memberClient1, configMapFromA, appliedWorkOwnerRefA)
			Eventually(configMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map via work A")

			placedConfigMap = &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, placedConfigMap)).To(Succeed(), "Failed to retrieve the config map")
		})

		It("can create work B to place the same config map (default sync strategy)", func() {
			createWorkObject(workNameB, memberReservedNSName1, nil, configMapFromBJSON)
		})

		It("should prepare an appliedWork object for work B", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workNameB)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object for work B")
			appliedWorkOwnerRefB = prepareAppliedWorkOwnerRef(memberClient1, workNameB)
		})

		It("should report that work B has not taken over the config map", func() {
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
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workNameB, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of work B")
		})

		It("can update work B to always take over pre-existing objects", func() {
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
			}
			updateWorkObject(workNameB, memberReservedNSName1, syncStrategy, configMapFromBJSON)
		})

		It("should report that work B cannot take over the config map as it is owned by others", func() {
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
							Reason:             string(ApplyResTypeFailedToTakeOver),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workNameB, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of work B")

			manifestAppliedCondMessageActual := manifestAppliedCondMessageActual(memberReservedNSName1, workNameB, 0, ownedByOthersErrMsg)
			Eventually(manifestAppliedCondMessageActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the existing owner as the cause of the failure")
		})

		It("can update work B to allow co-ownership", func() {
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
				WhenOwnedByOthers: placementv1alpha1.WhenOwnedByOthersOptionShareOwnership,
			}
			updateWorkObject(workNameB, memberReservedNSName1, syncStrategy, configMapFromBJSON)
		})

		It("should report that work B cannot take over the config map as it has been placed by another work object", func() {
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
							Reason:             string(ApplyResTypeFailedToTakeOver),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workNameB, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of work B")

			// The reason stays the same as before; the message tells the two failures apart.
			manifestAppliedCondMessageActual := manifestAppliedCondMessageActual(memberReservedNSName1, workNameB, 0, placedInDuplicateErrMsg)
			Eventually(manifestAppliedCondMessageActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the duplicate placement as the cause of the failure")
		})

		It("should keep the config map as placed by work A", func() {
			// The check spans at least one periodic requeue of work B.
			configMapObjectAppliedActual := configMapObjectAppliedActual(memberClient1, configMapFromA, appliedWorkOwnerRefA)
			Consistently(configMapObjectAppliedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The config map placed by work A has been changed")
		})

		It("can delete work A", func() {
			deleteWorkObject(workNameA, memberReservedNSName1)
		})

		It("should remove work A", func() {
			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object of
			// work A. As there is no GC controller in the test environment, the config map is not deleted, and it keeps
			// a (now dangling) owner reference to that appliedWork object; this mimics a GC controller that has not
			// caught up yet.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workNameA)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object of work A")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workNameA)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work A")
		})

		It("should take over the config map via work B", func() {
			// Work B drops the owner reference left behind by the appliedWork object of work A, as work A no longer
			// exists, and then takes over the config map. Deleting work A triggers no event for work B; the takeover
			// happens at the next requeue of work B, which might be up to 15 seconds away in the slow backoff stage.
			configMapObjectAppliedActual := configMapObjectAppliedActual(memberClient1, configMapFromB, appliedWorkOwnerRefB)
			Eventually(configMapObjectAppliedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to take over the config map via work B")

			Expect(objectNotRecreatedActual(memberClient1, placedConfigMap)()).To(Succeed(), "The config map has been re-created instead of being taken over")
		})

		It("should update the status of work B", func() {
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

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workNameB, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of work B")
		})

		It("should update the appliedWork object status of work B", func() {
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
					UID: placedConfigMap.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workNameB, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the appliedWork status of work B")
		})

		AfterAll(func() {
			// Work A might still exist if the test case fails before work A is deleted, and work B might not exist yet
			// if the test case fails before work B is created.
			for _, workName := range []string{workNameA, workNameB} {
				work := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workName,
						Namespace: memberReservedNSName1,
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, work))).To(Succeed(), "Failed to delete work %s", workName)
			}

			// Remove the config map manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork objects.
			for _, workName := range []string{workNameA, workNameB} {
				appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
				Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object of work %s", workName)

				workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
				Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work %s", workName)
			}

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})
})
