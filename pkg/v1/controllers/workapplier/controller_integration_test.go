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
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var _ = Describe("single work object", func() {
	Context("core ops (apply manifests)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularDeploy *appsv1.Deployment

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a deployment object.
			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeploy.Name = deployName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a new work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName1, nil, regularNSJSON, regularDeployJSON)
		})

		It("should add cleanup finalizer to the work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(memberReservedNSName1, workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the work object")
		})

		It("should prepare an appliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)
		})

		It("should apply the manifests", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the Deployment object has been applied as expected.
			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(memberClient1, nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the deployment object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, regularDeploy)).To(Succeed(), "Failed to retrieve the Deployment object")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(memberClient1, nsName, deployName)
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

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should update the appliedWork object status", func() {
			// Prepare the status information.
			appliedResources := []placementv1alpha1.AppliedResource{
				{
					ManifestIdentifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "Namespace",
						Resource:   "namespaces",
						Name:       nsName,
					},
					UID: regularNS.UID,
				},
				{
					ManifestIdentifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    1,
						APIGroup:   "apps",
						APIVersion: "v1",
						Kind:       "Deployment",
						Resource:   "deployments",
						Name:       deployName,
						Namespace:  nsName,
					},
					UID: regularDeploy.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Ensure applied manifest has been removed.
			regularDeployRemovedActual := regularDeployRemovedActual(memberClient1, nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")

			// Ensure that the appliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("garbage collection (delete work object)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularDeploy *appsv1.Deployment

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a deployment object.
			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeploy.Name = deployName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a new work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName1, nil, regularNSJSON, regularDeployJSON)
		})

		// For simplicity reasons, some steps (finalizer handling, appliedWork object status update, etc.) are
		// omitted in this test node, as the behaviors have been verified elsewhere.

		It("should prepare an appliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)
		})

		It("should apply the manifests", func() {
			// Retrieve the owner reference of the appliedWork object.
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the Deployment object has been applied as expected.
			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(memberClient1, nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the deployment object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, regularDeploy)).To(Succeed(), "Failed to retrieve the Deployment object")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(memberClient1, nsName, deployName)
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

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
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
			// pending deletion state; the work applier will unblock the deletion after cleanupWaitTime, and
			// proceed to remove the cleanup finalizer after 2 * cleanupWaitTime.
			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, 2*cleanupWaitTime+eventuallyDuration, eventuallyInterval*2).Should(Succeed(), "Failed to remove the work object")
		})

		It("should unblock the appliedWork deletion on all applied objects", func() {
			// The owner references stay (they are removed by the GC controller in a real cluster), but they should
			// no longer block the deletion of the appliedWork object.
			nsOwnerRefUnblockedActual := appliedWorkOwnerRefUnblockedActual(memberClient1, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}, appliedWorkOwnerRef)
			Eventually(nsOwnerRefUnblockedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to unblock the appliedWork deletion on the namespace object")

			deployOwnerRefUnblockedActual := appliedWorkOwnerRefUnblockedActual(memberClient1, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: nsName, Name: deployName}}, appliedWorkOwnerRef)
			Eventually(deployOwnerRefUnblockedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to unblock the appliedWork deletion on the deployment object")
		})

		It("should delete the appliedWork object", func() {
			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")
		})

		AfterAll(func() {
			// Remove the deployment object manually, as there is no GC controller in the test environment.
			regularDeployRemovedActual := regularDeployRemovedActual(memberClient1, nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("leftover cleanup (remove manifests from work object)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularDeploy *appsv1.Deployment
		var regularNSJSON []byte

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON = marshalK8sObjJSON(regularNS)

			// Prepare a deployment object.
			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeploy.Name = deployName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a new work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName1, nil, regularNSJSON, regularDeployJSON)
		})

		// For simplicity reasons, some steps (finalizer handling, appliedWork object creation, etc.) are omitted in
		// this test node, as the behaviors have been verified elsewhere.

		It("should prepare an appliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)
		})

		It("should apply the manifests", func() {
			// Retrieve the owner reference of the appliedWork object.
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the Deployment object has been applied as expected.
			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(memberClient1, nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the deployment object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, regularDeploy)).To(Succeed(), "Failed to retrieve the Deployment object")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(memberClient1, nsName, deployName)
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

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("can update the work object to remove the deployment manifest", func() {
			updateWorkObject(workName, memberReservedNSName1, nil, regularNSJSON)
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

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should remove the deployment object", func() {
			regularDeployAbsentActual := regularDeployAbsentActual(memberClient1, nsName, deployName)
			Eventually(regularDeployAbsentActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Ensure that the appliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})
})

var _ = Describe("multiple work objects", func() {
	Context("core ops (apply manifests)", Ordered, func() {
		// The two work objects are linked to each other: they share the same owner placement policy/binding and
		// the same primary placement resource snapshot; the primary work object features the linked work count
		// annotation.
		primaryWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		secondaryWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, primaryWorkName)
		placementBindingName := fmt.Sprintf(placementBindingNameTemplate, primaryWorkName)
		primarySnapshotName := fmt.Sprintf(primarySnapshotNameTemplate, primaryWorkName)
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularDeploy *appsv1.Deployment
		var regularConfigMap *corev1.ConfigMap

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a deployment object.
			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeploy.Name = deployName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Prepare a config map object.
			regularConfigMap = configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMap.Name = configMapName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Create the secondary work object first, so that the work applier can find all the linked work
			// objects as soon as it starts processing the primary work object.
			//
			// Note that normally KubeFleet sets the primary work object as the owner of all secondary work objects;
			// this is skipped here as there is no GC controller in the test environment.
			secondaryWork := buildWorkObject(
				secondaryWorkName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				regularConfigMapJSON,
			)
			Expect(hubClient.Create(ctx, secondaryWork)).To(Succeed(), "Failed to create the secondary work object")

			// Create the primary work object.
			primaryWork := buildWorkObject(
				primaryWorkName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				regularNSJSON, regularDeployJSON,
			)
			primaryWork.Annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = "2"
			Expect(hubClient.Create(ctx, primaryWork)).To(Succeed(), "Failed to create the primary work object")
		})

		It("should add cleanup finalizer to all the work objects", func() {
			primaryWorkFinalizerAddedActual := workFinalizerAddedActual(memberReservedNSName1, primaryWorkName)
			Eventually(primaryWorkFinalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the primary work object")

			secondaryWorkFinalizerAddedActual := workFinalizerAddedActual(memberReservedNSName1, secondaryWorkName)
			Eventually(secondaryWorkFinalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the secondary work object")
		})

		It("should prepare an appliedWork object for each work object", func() {
			primaryAppliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, primaryWorkName)
			Eventually(primaryAppliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object for the primary work object")

			secondaryAppliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, secondaryWorkName)
			Eventually(secondaryAppliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object for the secondary work object")

			// All the manifests across the linked work objects are owned by the appliedWork object of the
			// primary work object.
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, primaryWorkName)
		})

		It("should apply the manifests", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the Deployment object has been applied as expected.
			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(memberClient1, nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the deployment object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, regularDeploy)).To(Succeed(), "Failed to retrieve the Deployment object")

			// Ensure that the ConfigMap object has been applied as expected.
			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, regularConfigMap)).To(Succeed(), "Failed to retrieve the ConfigMap object")
		})

		It("should update the work object statuses (deployment not yet available)", func() {
			// The primary work object is not yet available, as the deployment has not become available.
			primaryWorkConds := []metav1.Condition{
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
			primaryWorkManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
							ObservedGeneration: 1,
						},
						{
							Type:               placementv1alpha1.ManifestCondTypeAvailable,
							Status:             metav1.ConditionFalse,
							Reason:             string(AvailabilityResultTypeNotYetAvailable),
							ObservedGeneration: 1,
						},
					},
				},
			}
			primaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, primaryWorkName, primaryWorkConds, primaryWorkManifestStatuses)
			Eventually(primaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the primary work object status")

			// The secondary work object is available.
			//
			// Note that the ordinals of the manifests are relative to their own work object.
			secondaryWorkConds := []metav1.Condition{
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
			secondaryWorkManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
			secondaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, secondaryWorkName, secondaryWorkConds, secondaryWorkManifestStatuses)
			Eventually(secondaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the secondary work object status")
		})

		It("should update the appliedWork object statuses", func() {
			// Each appliedWork object reports the resources applied from its own work object.
			primaryAppliedResources := []placementv1alpha1.AppliedResource{
				{
					ManifestIdentifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "Namespace",
						Resource:   "namespaces",
						Name:       nsName,
					},
					UID: regularNS.UID,
				},
				{
					ManifestIdentifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    1,
						APIGroup:   "apps",
						APIVersion: "v1",
						Kind:       "Deployment",
						Resource:   "deployments",
						Name:       deployName,
						Namespace:  nsName,
					},
					UID: regularDeploy.UID,
				},
			}
			primaryAppliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, primaryWorkName, primaryAppliedResources)
			Eventually(primaryAppliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the primary appliedWork object status")

			secondaryAppliedResources := []placementv1alpha1.AppliedResource{
				{
					ManifestIdentifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       configMapName,
						Namespace:  nsName,
					},
					UID: regularConfigMap.UID,
				},
			}
			secondaryAppliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, secondaryWorkName, secondaryAppliedResources)
			Eventually(secondaryAppliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the secondary appliedWork object status")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(memberClient1, nsName, deployName)
		})

		It("should update the work object statuses (all available)", func() {
			// The primary work object becomes available.
			primaryWorkConds := []metav1.Condition{
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
			primaryWorkManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
			primaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, primaryWorkName, primaryWorkConds, primaryWorkManifestStatuses)
			Eventually(primaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the primary work object status")

			// The secondary work object stays available.
			secondaryWorkConds := []metav1.Condition{
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
			secondaryWorkManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
			secondaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, secondaryWorkName, secondaryWorkConds, secondaryWorkManifestStatuses)
			Eventually(secondaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the secondary work object status")
		})

		AfterAll(func() {
			// Delete the work objects.
			//
			// As there is no GC controller in the test environment (and the secondary work object is not owned by
			// the primary one in this test), both work objects are deleted explicitly.
			deleteWorkObject(primaryWorkName, memberReservedNSName1)
			deleteWorkObject(secondaryWorkName, memberReservedNSName1)

			// Remove the applied manifests manually, as there is no GC controller in the test environment.
			regularDeployRemovedActual := regularDeployRemovedActual(memberClient1, nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")

			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Ensure that the appliedWork objects have been removed.
			primaryAppliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, primaryWorkName)
			Eventually(primaryAppliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary appliedWork object")

			secondaryAppliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, secondaryWorkName)
			Eventually(secondaryAppliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary appliedWork object")

			// Ensure that the work objects have been removed.
			primaryWorkRemovedActual := workRemovedActual(memberReservedNSName1, primaryWorkName)
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			secondaryWorkRemovedActual := workRemovedActual(memberReservedNSName1, secondaryWorkName)
			Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("garbage collection (delete primary work object)", Ordered, func() {
		// The two work objects are linked to each other: they share the same owner placement policy/binding and
		// the same primary placement resource snapshot; the primary work object features the linked work count
		// annotation.
		primaryWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		secondaryWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, primaryWorkName)
		placementBindingName := fmt.Sprintf(placementBindingNameTemplate, primaryWorkName)
		primarySnapshotName := fmt.Sprintf(primarySnapshotNameTemplate, primaryWorkName)
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularDeploy *appsv1.Deployment
		var regularConfigMap *corev1.ConfigMap

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a deployment object.
			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeploy.Name = deployName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Prepare a config map object.
			regularConfigMap = configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMap.Name = configMapName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Create the secondary work object first, so that the work applier can find all the linked work
			// objects as soon as it starts processing the primary work object.
			//
			// Note that normally KubeFleet sets the primary work object as the owner of all secondary work objects;
			// this is skipped here as it has no effect on the work applier, and there is no GC controller in the
			// test environment.
			secondaryWork := buildWorkObject(
				secondaryWorkName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				regularConfigMapJSON,
			)
			Expect(hubClient.Create(ctx, secondaryWork)).To(Succeed(), "Failed to create the secondary work object")

			// Create the primary work object.
			primaryWork := buildWorkObject(
				primaryWorkName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				regularNSJSON, regularDeployJSON,
			)
			primaryWork.Annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = "2"
			Expect(hubClient.Create(ctx, primaryWork)).To(Succeed(), "Failed to create the primary work object")
		})

		// For simplicity reasons, some steps (finalizer handling, appliedWork object creation, etc.) are omitted in
		// this test node, as the behaviors have been verified elsewhere.

		It("should apply the manifests", func() {
			// Retrieve the owner reference of the appliedWork object of the primary work object; all the manifests
			// across the linked work objects are owned by this appliedWork object.
			primaryAppliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, primaryWorkName)
			Eventually(primaryAppliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object for the primary work object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, primaryWorkName)

			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			// Ensure that the Deployment object has been applied as expected.
			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(memberClient1, nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the deployment object")

			// Ensure that the ConfigMap object has been applied as expected.
			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(memberClient1, nsName, deployName)
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

			primaryWorkManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
			// Note that workStatusUpdated sets the observed generations on the given conditions; use a copy here
			// as the conditions are shared between the two work objects.
			primaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, primaryWorkName, append([]metav1.Condition{}, workConds...), primaryWorkManifestStatuses)
			Eventually(primaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the primary work object status")

			secondaryWorkManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
			secondaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, secondaryWorkName, append([]metav1.Condition{}, workConds...), secondaryWorkManifestStatuses)
			Eventually(secondaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the secondary work object status")
		})

		It("can delete the primary work object", func() {
			deleteWorkObject(primaryWorkName, memberReservedNSName1)
		})

		It("should start deleting the primary appliedWork object in the foreground", func() {
			appliedWorkBeingDeletedActual := appliedWorkBeingDeletedInForegroundActual(memberClient1, primaryWorkName)
			Eventually(appliedWorkBeingDeletedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to start deleting the primary appliedWork object in the foreground")
		})

		It("should remove the primary work object", func() {
			// As there is no GC controller in the test environment, the primary appliedWork object will be stuck in
			// the pending deletion state; the work applier will unblock the deletion after cleanupWaitTime, and
			// proceed to remove the cleanup finalizers after 2 * cleanupWaitTime.
			workRemovedActual := workRemovedActual(memberReservedNSName1, primaryWorkName)
			Eventually(workRemovedActual, 2*cleanupWaitTime+eventuallyDuration, eventuallyInterval*2).Should(Succeed(), "Failed to remove the primary work object")
		})

		It("should remove the cleanup finalizer from the secondary work object", func() {
			// In a real cluster the secondary work object is owned by the primary one and will be garbage collected;
			// here only the finalizer removal is verified.
			workFinalizerRemovedActual := workFinalizerRemovedActual(memberReservedNSName1, secondaryWorkName)
			Eventually(workFinalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer from the secondary work object")
		})

		It("should remove the secondary appliedWork object", func() {
			// The secondary appliedWork object owns no resources, and is deleted by the work applier directly.
			appliedWorkAbsentActual := appliedWorkAbsentActual(memberClient1, secondaryWorkName)
			Eventually(appliedWorkAbsentActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary appliedWork object")
		})

		It("should unblock the primary appliedWork deletion on all applied objects", func() {
			// The owner references stay (they are removed by the GC controller in a real cluster), but they should
			// no longer block the deletion of the primary appliedWork object.
			nsOwnerRefUnblockedActual := appliedWorkOwnerRefUnblockedActual(memberClient1, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}, appliedWorkOwnerRef)
			Eventually(nsOwnerRefUnblockedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to unblock the appliedWork deletion on the namespace object")

			deployOwnerRefUnblockedActual := appliedWorkOwnerRefUnblockedActual(memberClient1, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: nsName, Name: deployName}}, appliedWorkOwnerRef)
			Eventually(deployOwnerRefUnblockedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to unblock the appliedWork deletion on the deployment object")

			configMapOwnerRefUnblockedActual := appliedWorkOwnerRefUnblockedActual(memberClient1, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: nsName, Name: configMapName}}, appliedWorkOwnerRef)
			Eventually(configMapOwnerRefUnblockedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to unblock the appliedWork deletion on the config map object")
		})

		It("should delete the primary appliedWork object", func() {
			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, primaryWorkName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary appliedWork object")
		})

		AfterAll(func() {
			// Delete the secondary work object manually, as there is no GC controller in the test environment.
			deleteWorkObject(secondaryWorkName, memberReservedNSName1)
			secondaryWorkRemovedActual := workRemovedActual(memberReservedNSName1, secondaryWorkName)
			Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")

			// Remove the applied manifests manually, as there is no GC controller in the test environment.
			regularDeployRemovedActual := regularDeployRemovedActual(memberClient1, nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")

			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("leftover cleanup (remove and shuffle manifests from linked work objects)", Ordered, func() {
		// The two work objects are linked to each other: they share the same owner placement policy/binding and
		// the same primary placement resource snapshot; the primary work object features the linked work count
		// annotation.
		primaryWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		secondaryWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, primaryWorkName)
		placementBindingName := fmt.Sprintf(placementBindingNameTemplate, primaryWorkName)
		primarySnapshotName := fmt.Sprintf(primarySnapshotNameTemplate, primaryWorkName)
		// The name of the new primary placement resource snapshot, as created by a rollout.
		newPrimarySnapshotName := fmt.Sprintf("%s-new", primarySnapshotName)
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularDeploy *appsv1.Deployment
		var regularConfigMap *corev1.ConfigMap
		var regularNSJSON []byte
		var regularDeployJSON []byte

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON = marshalK8sObjJSON(regularNS)

			// Prepare a deployment object.
			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeploy.Name = deployName
			regularDeployJSON = marshalK8sObjJSON(regularDeploy)

			// Prepare a config map object.
			regularConfigMap = configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMap.Name = configMapName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Create the secondary work object first, so that the work applier can find all the linked work
			// objects as soon as it starts processing the primary work object.
			//
			// Note that normally KubeFleet sets the primary work object as the owner of all secondary work objects;
			// this is skipped here as it has no effect on the work applier, and there is no GC controller in the
			// test environment.
			secondaryWork := buildWorkObject(
				secondaryWorkName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				regularConfigMapJSON,
			)
			Expect(hubClient.Create(ctx, secondaryWork)).To(Succeed(), "Failed to create the secondary work object")

			// Create the primary work object.
			primaryWork := buildWorkObject(
				primaryWorkName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				regularNSJSON, regularDeployJSON,
			)
			primaryWork.Annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = "2"
			Expect(hubClient.Create(ctx, primaryWork)).To(Succeed(), "Failed to create the primary work object")
		})

		// For simplicity reasons, some steps (finalizer handling, appliedWork object creation, etc.) are omitted in
		// this test node, as the behaviors have been verified elsewhere.

		It("should apply the manifests", func() {
			// Retrieve the owner reference of the appliedWork object of the primary work object; all the manifests
			// across the linked work objects are owned by this appliedWork object.
			primaryAppliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, primaryWorkName)
			Eventually(primaryAppliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object for the primary work object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, primaryWorkName)

			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the Deployment object has been applied as expected.
			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(memberClient1, nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the deployment object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, regularDeploy)).To(Succeed(), "Failed to retrieve the Deployment object")

			// Ensure that the ConfigMap object has been applied as expected.
			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(memberClient1, nsName, deployName)
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

			primaryWorkManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
			// Note that workStatusUpdated sets the observed generations on the given conditions; use a copy here
			// as the conditions are shared between the two work objects.
			primaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, primaryWorkName, append([]metav1.Condition{}, workConds...), primaryWorkManifestStatuses)
			Eventually(primaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the primary work object status")

			secondaryWorkManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
			secondaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, secondaryWorkName, append([]metav1.Condition{}, workConds...), secondaryWorkManifestStatuses)
			Eventually(secondaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the secondary work object status")
		})

		It("can update the primary work object to keep only the namespace manifest (rollout in progress)", func() {
			// Mimic a rollout: the primary placement resource snapshot has been bumped, and the primary work object
			// is refreshed first; the deployment manifest is to be moved to the secondary work object, which has not
			// been refreshed yet.
			updateLinkedWorkObject(primaryWorkName, memberReservedNSName1, newPrimarySnapshotName, nil, regularNSJSON)
		})

		It("should not remove the deployment object", func() {
			// The linked work objects are now in an inconsistent state (the secondary work object is still linked
			// to the old primary placement resource snapshot); the work applier should hold off any processing,
			// including the removal of the deployment object.
			Consistently(func() error {
				gotDeploy := &appsv1.Deployment{}
				if err := memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, gotDeploy); err != nil {
					return fmt.Errorf("failed to retrieve the Deployment object: %w", err)
				}
				if !gotDeploy.DeletionTimestamp.IsZero() {
					return fmt.Errorf("deployment object has been marked for deletion")
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The deployment object has been removed unexpectedly")
		})

		It("can update the secondary work object to add the deployment manifest and drop the config map manifest", func() {
			updateLinkedWorkObject(secondaryWorkName, memberReservedNSName1, newPrimarySnapshotName, nil, regularDeployJSON)
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

			primaryWorkManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
			// While the linked work objects were in an inconsistent state, the work applier kept requeueing the
			// primary work object with exponential backoff; allow more time for the next attempt to happen.
			primaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, primaryWorkName, append([]metav1.Condition{}, workConds...), primaryWorkManifestStatuses)
			Eventually(primaryWorkStatusUpdatedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the primary work object status")

			// Note that the ordinals of the manifests are relative to their own work object.
			secondaryWorkManifestStatuses := []placementv1alpha1.PerManifestStatus{
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
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
			secondaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, secondaryWorkName, append([]metav1.Condition{}, workConds...), secondaryWorkManifestStatuses)
			Eventually(secondaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the secondary work object status")
		})

		It("should update the appliedWork object statuses", func() {
			primaryAppliedResources := []placementv1alpha1.AppliedResource{
				{
					ManifestIdentifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "Namespace",
						Resource:   "namespaces",
						Name:       nsName,
					},
					UID: regularNS.UID,
				},
			}
			primaryAppliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, primaryWorkName, primaryAppliedResources)
			Eventually(primaryAppliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the primary appliedWork object status")

			// The deployment object is the same one as before (i.e., it has not been re-created).
			secondaryAppliedResources := []placementv1alpha1.AppliedResource{
				{
					ManifestIdentifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "apps",
						APIVersion: "v1",
						Kind:       "Deployment",
						Resource:   "deployments",
						Name:       deployName,
						Namespace:  nsName,
					},
					UID: regularDeploy.UID,
				},
			}
			secondaryAppliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, secondaryWorkName, secondaryAppliedResources)
			Eventually(secondaryAppliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the secondary appliedWork object status")
		})

		It("should keep the deployment object", func() {
			// The deployment object should still be owned by the appliedWork object of the primary work object.
			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(memberClient1, nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to keep the deployment object")

			gotDeploy := &appsv1.Deployment{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, gotDeploy)).To(Succeed(), "Failed to retrieve the Deployment object")
			Expect(gotDeploy.UID).To(Equal(regularDeploy.UID), "The deployment object has been re-created unexpectedly")
		})

		It("should remove the config map object", func() {
			regularConfigMapAbsentActual := regularConfigMapAbsentActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapAbsentActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")
		})

		AfterAll(func() {
			// Delete the work objects.
			//
			// As there is no GC controller in the test environment (and the secondary work object is not owned by
			// the primary one in this test), both work objects are deleted explicitly.
			deleteWorkObject(primaryWorkName, memberReservedNSName1)
			deleteWorkObject(secondaryWorkName, memberReservedNSName1)

			// Remove the applied manifests manually, as there is no GC controller in the test environment.
			regularDeployRemovedActual := regularDeployRemovedActual(memberClient1, nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")

			// Ensure that the appliedWork objects have been removed.
			primaryAppliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, primaryWorkName)
			Eventually(primaryAppliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary appliedWork object")

			secondaryAppliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, secondaryWorkName)
			Eventually(secondaryAppliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary appliedWork object")

			// Ensure that the work objects have been removed.
			primaryWorkRemovedActual := workRemovedActual(memberReservedNSName1, primaryWorkName)
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			secondaryWorkRemovedActual := workRemovedActual(memberReservedNSName1, secondaryWorkName)
			Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})
})
