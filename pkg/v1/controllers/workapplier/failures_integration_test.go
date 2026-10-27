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

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	// The data value of the config map in a duplicated manifest; it differs from the one in the config map fixture.
	duplicatedConfigMapDataVal = "qux"
)

var _ = Describe("partial failures", func() {
	Context("decoding errors mixed with valid manifests", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		malformedObjName := "malformed-1"

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularDeploy *appsv1.Deployment
		var regularConfigMap *corev1.ConfigMap
		var regularNSJSON []byte
		var regularDeployJSON []byte
		var regularConfigMapJSON []byte

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON = marshalK8sObjJSON(regularNS)

			// Prepare an object of an unknown API group.
			//
			// The hub API server requires that each manifest has the API version and kind fields set; here the
			// object is built from the config map fixture, with the type information overwritten.
			malformedObj := configMap.DeepCopy()
			malformedObj.TypeMeta = metav1.TypeMeta{
				APIVersion: "malformed/v10",
				Kind:       "Unknown",
			}
			malformedObj.Namespace = nsName
			malformedObj.Name = malformedObjName
			malformedObjJSON := marshalK8sObjJSON(malformedObj)

			// Prepare a deployment object of a known API group, but of an API version that is not served.
			unservedVersionDeploy := deploy.DeepCopy()
			unservedVersionDeploy.APIVersion = "apps/v1beta1"
			unservedVersionDeploy.Namespace = nsName
			unservedVersionDeploy.Name = deployName
			unservedVersionDeployJSON := marshalK8sObjJSON(unservedVersionDeploy)

			// Prepare a deployment object of a served API version, which is used to fix the manifest above later.
			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeploy.Name = deployName
			regularDeployJSON = marshalK8sObjJSON(regularDeploy)

			// Prepare a config map object.
			regularConfigMap = configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMap.Name = configMapName
			regularConfigMapJSON = marshalK8sObjJSON(regularConfigMap)

			// Create a new work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName1, nil, regularNSJSON, malformedObjJSON, unservedVersionDeployJSON, regularConfigMapJSON)
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

		It("should apply the valid manifests", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the ConfigMap object has been applied as expected.
			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, regularConfigMap)).To(Succeed(), "Failed to retrieve the ConfigMap object")
		})

		It("should not create any object for the manifests that cannot be decoded", func() {
			// In particular, the work applier should not silently fall back to a served API version for the
			// deployment manifest.
			regularDeployAbsentActual := regularDeployAbsentActual(memberClient1, nsName, deployName)
			Consistently(regularDeployAbsentActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The deployment object has been created unexpectedly")
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
			// Note that the manifests that cannot be REST mapped have an empty resource field in their
			// identifiers, and no Available condition.
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
						APIGroup:   "malformed",
						APIVersion: "v10",
						Kind:       "Unknown",
						Resource:   "",
						Name:       malformedObjName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeDecodingErred),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    2,
						APIGroup:   "apps",
						APIVersion: "v1beta1",
						Kind:       "Deployment",
						Resource:   "",
						Name:       deployName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeDecodingErred),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    3,
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
			// Only the applied manifests are reported.
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
					UID: regularNS.UID,
				},
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
						Ordinal:    3,
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

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("can update the work object to fix the manifests that cannot be decoded", func() {
			// Drop the manifest of the unknown API group, and switch the deployment manifest to a served API version.
			updateWorkObject(workName, memberReservedNSName1, nil, regularNSJSON, regularDeployJSON, regularConfigMapJSON)
		})

		It("should apply the deployment manifest", func() {
			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(memberClient1, nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the deployment object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, regularDeploy)).To(Succeed(), "Failed to retrieve the Deployment object")
		})

		It("should keep the config map object", func() {
			// The ordinal of the config map manifest has changed (3 -> 2); as the work applier looks for left-over
			// manifests without ordinals, the config map object should not be deleted, re-created, or overwritten.
			configMapStableActual := configMapStableActual(memberClient1, nsName, configMapName, regularConfigMap.UID, configMap.Data)
			Consistently(configMapStableActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The config map object has been deleted, re-created, or overwritten unexpectedly")
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
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    2,
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
			// The config map object is the same one as before (i.e., it has not been re-created).
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
					UID: regularNS.UID,
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
					UID: regularDeploy.UID,
				},
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
						Ordinal:    2,
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

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the applied manifests manually, as there is no GC controller in the test environment.
			regularDeployRemovedActual := regularDeployRemovedActual(memberClient1, nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")

			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Ensure that the appliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("manifests with generate name", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		generateNamePrefix := "cm-gen-"
		secondConfigMapName := "configmap-2"

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularConfigMap *corev1.ConfigMap
		var secondConfigMap *corev1.ConfigMap
		var regularNSJSON []byte
		var regularConfigMapJSON []byte
		var secondConfigMapJSON []byte

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON = marshalK8sObjJSON(regularNS)

			// Prepare a config map object with only the generate name set.
			generateNameOnlyConfigMap := configMap.DeepCopy()
			generateNameOnlyConfigMap.Namespace = nsName
			generateNameOnlyConfigMap.Name = ""
			generateNameOnlyConfigMap.GenerateName = generateNamePrefix
			generateNameOnlyConfigMapJSON := marshalK8sObjJSON(generateNameOnlyConfigMap)

			// Prepare a config map object with both the name and the generate name set; it is rejected as well.
			nameAndGenerateNameConfigMap := configMap.DeepCopy()
			nameAndGenerateNameConfigMap.Namespace = nsName
			nameAndGenerateNameConfigMap.Name = configMapName
			nameAndGenerateNameConfigMap.GenerateName = generateNamePrefix
			nameAndGenerateNameConfigMapJSON := marshalK8sObjJSON(nameAndGenerateNameConfigMap)

			// Prepare config map objects with only the name set, which are used to fix the manifests above later.
			regularConfigMap = configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMap.Name = configMapName
			regularConfigMapJSON = marshalK8sObjJSON(regularConfigMap)

			secondConfigMap = configMap.DeepCopy()
			secondConfigMap.Namespace = nsName
			secondConfigMap.Name = secondConfigMapName
			secondConfigMapJSON = marshalK8sObjJSON(secondConfigMap)

			// Create a new work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName1, nil, regularNSJSON, generateNameOnlyConfigMapJSON, nameAndGenerateNameConfigMapJSON)
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

		It("should apply the manifest without a generate name", func() {
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")
		})

		It("should not create any object for the manifests with a generate name", func() {
			configMapsWithPrefixAbsentActual := configMapsWithPrefixAbsentActual(memberClient1, nsName, generateNamePrefix)
			Consistently(configMapsWithPrefixAbsentActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "A config map object has been created from the manifest with only the generate name unexpectedly")

			regularConfigMapAbsentActual := regularConfigMapAbsentActual(memberClient1, nsName, configMapName)
			Consistently(regularConfigMapAbsentActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "A config map object has been created from the manifest with both the name and the generate name unexpectedly")
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
			// Note that the manifests with a generate name can be REST mapped, so the resource field in their
			// identifiers is populated; the name field of the manifest with only the generate name, however, is empty.
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
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       "",
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeFoundGenerateName),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    2,
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
							Reason:             string(ApplyResTypeFoundGenerateName),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should update the appliedWork object status", func() {
			// Only the applied manifest is reported.
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
					UID: regularNS.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("can update the work object to remove the generate names", func() {
			updateWorkObject(workName, memberReservedNSName1, nil, regularNSJSON, secondConfigMapJSON, regularConfigMapJSON)
		})

		It("should apply the manifests", func() {
			secondConfigMapObjectAppliedActual := configMapObjectAppliedActual(memberClient1, secondConfigMap, appliedWorkOwnerRef)
			Eventually(secondConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the second config map object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: secondConfigMapName}, secondConfigMap)).To(Succeed(), "Failed to retrieve the second ConfigMap object")

			firstConfigMapObjectAppliedActual := configMapObjectAppliedActual(memberClient1, regularConfigMap, appliedWorkOwnerRef)
			Eventually(firstConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, regularConfigMap)).To(Succeed(), "Failed to retrieve the ConfigMap object")
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
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       secondConfigMapName,
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
						Ordinal:    2,
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
						Kind:       "Namespace",
						Resource:   "namespaces",
						Name:       nsName,
					},
					UID: regularNS.UID,
				},
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
						Ordinal:    1,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       secondConfigMapName,
						Namespace:  nsName,
					},
					UID: secondConfigMap.UID,
				},
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
						Ordinal:    2,
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

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the applied manifests manually, as there is no GC controller in the test environment.
			firstConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(firstConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			secondConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, secondConfigMapName)
			Eventually(secondConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the second config map object")

			// Ensure that the appliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("manifests with no name or mismatching scopes", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		noNamespaceConfigMapName := "configmap-no-namespace"
		namespacedNSName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a config map object with neither the name nor the generate name set.
			noNameConfigMap := configMap.DeepCopy()
			noNameConfigMap.Namespace = nsName
			noNameConfigMap.Name = ""
			noNameConfigMap.GenerateName = ""
			noNameConfigMapJSON := marshalK8sObjJSON(noNameConfigMap)

			// Prepare a config map object (namespace-scoped) with no namespace set.
			noNamespaceConfigMap := configMap.DeepCopy()
			noNamespaceConfigMap.Namespace = ""
			noNamespaceConfigMap.Name = noNamespaceConfigMapName
			noNamespaceConfigMapJSON := marshalK8sObjJSON(noNamespaceConfigMap)

			// Prepare a namespace object (cluster-scoped) with a namespace set.
			namespacedNS := ns.DeepCopy()
			namespacedNS.Namespace = nsName
			namespacedNS.Name = namespacedNSName
			namespacedNSJSON := marshalK8sObjJSON(namespacedNS)

			// Create a new work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName1, nil, regularNSJSON, noNameConfigMapJSON, noNamespaceConfigMapJSON, namespacedNSJSON)
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

		It("should apply the valid manifest", func() {
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")
		})

		It("should not create any object for the rejected manifests", func() {
			Consistently(func() error {
				configMapList := &corev1.ConfigMapList{}
				if err := memberClient1.List(ctx, configMapList, client.InNamespace(nsName)); err != nil {
					return fmt.Errorf("failed to list ConfigMap objects: %w", err)
				}
				for idx := range configMapList.Items {
					// The root CA config map might be present in the namespace (if the test environment
					// publishes it); it is not created by the work applier.
					if configMapList.Items[idx].Name != "kube-root-ca.crt" {
						return fmt.Errorf("found an unexpected ConfigMap object (%s)", configMapList.Items[idx].Name)
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "A config map object has been created from the manifest with no name unexpectedly")

			noNamespaceConfigMapAbsentActual := regularConfigMapAbsentActual(memberClient1, "default", noNamespaceConfigMapName)
			Consistently(noNamespaceConfigMapAbsentActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "A config map object has been created from the manifest with no namespace unexpectedly")

			namespacedNSAbsentActual := namespaceAbsentActual(memberClient1, namespacedNSName)
			Consistently(namespacedNSAbsentActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "A namespace object has been created from the manifest with a namespace set unexpectedly")
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
			// Note that the manifest with no name can be REST mapped, so the resource field in its identifier is
			// populated; its name field, however, is empty. The manifests with mismatching scopes have an empty
			// resource field in their identifiers, as the scope check is part of the decoding process.
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
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       "",
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeNoName),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    2,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "",
						Name:       noNamespaceConfigMapName,
						Namespace:  "",
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeDecodingErred),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    3,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "Namespace",
						Resource:   "",
						Name:       namespacedNSName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeDecodingErred),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should update the appliedWork object status", func() {
			// Only the applied manifest is reported.
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
					UID: regularNS.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
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

	Context("duplicated manifests within one work object", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		// The work object stays unavailable (and thus in the slow backoff stage of the requeue rate limiter) as long
		// as the duplicate is present; each of the two windows below covers at least two requeues of the work object.
		duplicateNeverAppliedCheckDuration := time.Second * 15
		manifestStatusesStableCheckDuration := time.Second * 20

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularConfigMap *corev1.ConfigMap
		var duplicatedConfigMap *corev1.ConfigMap
		var regularNSJSON []byte
		var duplicatedConfigMapJSON []byte

		// The last transition times of the conditions on the statuses of the two copies of the duplicated manifest.
		var firstCopyAppliedCondLTT metav1.Time
		var firstCopyAvailableCondLTT metav1.Time
		var secondCopyAppliedCondLTT metav1.Time

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON = marshalK8sObjJSON(regularNS)

			// Prepare a config map object.
			regularConfigMap = configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMap.Name = configMapName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Prepare a duplicate of the config map object, with different data.
			duplicatedConfigMap = regularConfigMap.DeepCopy()
			duplicatedConfigMap.Data = map[string]string{
				configMapDataKey: duplicatedConfigMapDataVal,
			}
			duplicatedConfigMapJSON = marshalK8sObjJSON(duplicatedConfigMap)

			// Create a new work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName1, nil, regularNSJSON, regularConfigMapJSON, duplicatedConfigMapJSON)
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

		It("should apply the first occurrence of the duplicated manifest", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the ConfigMap object has been applied from the first occurrence.
			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, regularConfigMap)).To(Succeed(), "Failed to retrieve the ConfigMap object")
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
						Ordinal:    2,
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
							Reason:             string(ApplyResTypeDuplicated),
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
						Kind:       "Namespace",
						Resource:   "namespaces",
						Name:       nsName,
					},
					UID: regularNS.UID,
				},
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
						Ordinal:    1,
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

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("should never apply the duplicate", func() {
			configMapStableActual := configMapStableActual(memberClient1, nsName, configMapName, regularConfigMap.UID, configMap.Data)
			Consistently(configMapStableActual, duplicateNeverAppliedCheckDuration, consistentlyInterval).Should(Succeed(), "The duplicate has overwritten the config map object unexpectedly")
		})

		It("should keep the statuses of both copies of the duplicated manifest stable", func() {
			// Each copy of the duplicated manifest should get its own previous status back upon each requeue, so
			// that the work object status is not rewritten every time.
			//
			// Note that the work object must not be nudged here: a spec change triggers the write-ahead process,
			// which drops the status of the duplicate, and the conditions of the duplicate would then be rebuilt.
			firstCopyAppliedCondLTT = retrieveManifestCondLTT(memberReservedNSName1, workName, 1, placementv1alpha1.ManifestCondTypeApplied)
			firstCopyAvailableCondLTT = retrieveManifestCondLTT(memberReservedNSName1, workName, 1, placementv1alpha1.ManifestCondTypeAvailable)
			secondCopyAppliedCondLTT = retrieveManifestCondLTT(memberReservedNSName1, workName, 2, placementv1alpha1.ManifestCondTypeApplied)

			firstCopyAppliedCondStableActual := manifestCondLTTActual(memberReservedNSName1, workName, 1, placementv1alpha1.ManifestCondTypeApplied, firstCopyAppliedCondLTT)
			firstCopyAvailableCondStableActual := manifestCondLTTActual(memberReservedNSName1, workName, 1, placementv1alpha1.ManifestCondTypeAvailable, firstCopyAvailableCondLTT)
			secondCopyAppliedCondStableActual := manifestCondLTTActual(memberReservedNSName1, workName, 2, placementv1alpha1.ManifestCondTypeApplied, secondCopyAppliedCondLTT)
			Consistently(func() error {
				if err := firstCopyAppliedCondStableActual(); err != nil {
					return fmt.Errorf("first copy: %w", err)
				}
				if err := firstCopyAvailableCondStableActual(); err != nil {
					return fmt.Errorf("first copy: %w", err)
				}
				if err := secondCopyAppliedCondStableActual(); err != nil {
					return fmt.Errorf("second copy: %w", err)
				}
				return nil
			}, manifestStatusesStableCheckDuration, consistentlyInterval).Should(Succeed(), "The manifest statuses have been rebuilt unexpectedly")
		})

		It("can update the work object to remove the first occurrence of the duplicated manifest", func() {
			updateWorkObject(workName, memberReservedNSName1, nil, regularNSJSON, duplicatedConfigMapJSON)
		})

		It("should hand the config map object over to the remaining copy without re-creating it", func() {
			// The config map identifier is still present in the work object; as the work applier looks for left-over
			// manifests without ordinals, the config map object should not be deleted or re-created.
			configMapStableActual := configMapStableActual(memberClient1, nsName, configMapName, regularConfigMap.UID, duplicatedConfigMap.Data)
			Eventually(configMapStableActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to hand the config map object over to the remaining copy")

			configMapObjectAppliedActual := configMapObjectAppliedActual(memberClient1, duplicatedConfigMap, appliedWorkOwnerRef)
			Eventually(configMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the remaining copy of the config map object")
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

		It("should carry over the conditions of the first copy to the remaining copy", func() {
			// Upon the spec change, the write-ahead process should carry over the status of the first copy (the
			// one that has been applied), rather than that of the duplicate; consequently, the Applied and
			// Available conditions of the remaining copy never transition.
			appliedCondLTTActual := manifestCondLTTActual(memberReservedNSName1, workName, 1, placementv1alpha1.ManifestCondTypeApplied, firstCopyAppliedCondLTT)
			Expect(appliedCondLTTActual()).To(Succeed(), "The Applied condition has transitioned unexpectedly")

			availableCondLTTActual := manifestCondLTTActual(memberReservedNSName1, workName, 1, placementv1alpha1.ManifestCondTypeAvailable, firstCopyAvailableCondLTT)
			Expect(availableCondLTTActual()).To(Succeed(), "The Available condition has transitioned unexpectedly")
		})

		It("should update the appliedWork object status", func() {
			// The config map object is the same one as before (i.e., it has not been re-created).
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
					UID: regularNS.UID,
				},
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
						Ordinal:    1,
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

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the applied manifests manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Ensure that the appliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("duplicated manifests of the same group/kind but different versions within one work object", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		hpaName := "hpa-1"

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularHPA *autoscalingv2.HorizontalPodAutoscaler

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare an HPA object of the autoscaling/v2 API version.
			regularHPA = &autoscalingv2.HorizontalPodAutoscaler{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "autoscaling/v2",
					Kind:       "HorizontalPodAutoscaler",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: nsName,
					Name:      hpaName,
				},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       deployName,
					},
					MinReplicas: ptr.To(int32(1)),
					MaxReplicas: 3,
				},
			}
			regularHPAJSON := marshalK8sObjJSON(regularHPA)

			// Prepare an HPA object of the same name, but of the autoscaling/v1 API version (and with a different
			// spec). Both API versions are served; as the work applier identifies manifests without their API
			// versions, the two manifests are duplicates of each other.
			v1HPA := &autoscalingv1.HorizontalPodAutoscaler{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "autoscaling/v1",
					Kind:       "HorizontalPodAutoscaler",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: nsName,
					Name:      hpaName,
				},
				Spec: autoscalingv1.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       deployName,
					},
					MinReplicas: ptr.To(int32(1)),
					MaxReplicas: 5,
				},
			}
			v1HPAJSON := marshalK8sObjJSON(v1HPA)

			// Create a new work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName1, nil, regularNSJSON, regularHPAJSON, v1HPAJSON)
		})

		// For simplicity reasons, some steps (finalizer handling, appliedWork object creation, etc.) are omitted in
		// this test node, as the behaviors have been verified elsewhere.

		It("should apply the first occurrence of the duplicated manifest", func() {
			// Retrieve the owner reference of the appliedWork object.
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the HPA object has been applied from the first occurrence (autoscaling/v2).
			Eventually(func() error {
				gotHPA := &autoscalingv2.HorizontalPodAutoscaler{}
				if err := memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: hpaName}, gotHPA); err != nil {
					return fmt.Errorf("failed to retrieve the HPA object: %w", err)
				}

				wantOwnerRefs := []metav1.OwnerReference{*appliedWorkOwnerRef}
				if diff := cmp.Diff(gotHPA.OwnerReferences, wantOwnerRefs); diff != "" {
					return fmt.Errorf("owner references diff (-got +want):\n%s", diff)
				}
				if gotHPA.Spec.MaxReplicas != regularHPA.Spec.MaxReplicas {
					return fmt.Errorf("HPA max replicas = %d, want %d", gotHPA.Spec.MaxReplicas, regularHPA.Spec.MaxReplicas)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the HPA object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: hpaName}, regularHPA)).To(Succeed(), "Failed to retrieve the HPA object")
		})

		It("should update the work object status", func() {
			// Prepare the status information.
			//
			// The work object is not available because of the duplicate, even though the availability of the HPA
			// object itself cannot be tracked.
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
						APIGroup:   "autoscaling",
						APIVersion: "v2",
						Kind:       "HorizontalPodAutoscaler",
						Resource:   "horizontalpodautoscalers",
						Name:       hpaName,
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
							Reason:             string(AvailabilityResultTypeNotTrackable),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    2,
						APIGroup:   "autoscaling",
						APIVersion: "v1",
						Kind:       "HorizontalPodAutoscaler",
						Resource:   "horizontalpodautoscalers",
						Name:       hpaName,
						Namespace:  nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               placementv1alpha1.ManifestCondTypeApplied,
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeDuplicated),
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
						Kind:       "Namespace",
						Resource:   "namespaces",
						Name:       nsName,
					},
					UID: regularNS.UID,
				},
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
						Ordinal:    1,
						APIGroup:   "autoscaling",
						APIVersion: "v2",
						Kind:       "HorizontalPodAutoscaler",
						Resource:   "horizontalpodautoscalers",
						Name:       hpaName,
						Namespace:  nsName,
					},
					UID: regularHPA.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the applied manifests manually, as there is no GC controller in the test environment.
			regularHPARemovedActual := regularHPARemovedActual(memberClient1, nsName, hpaName)
			Eventually(regularHPARemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the HPA object")

			// Ensure that the appliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("removing all copies of a duplicated manifest from one work object", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularNSJSON []byte

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON = marshalK8sObjJSON(regularNS)

			// Prepare a config map object.
			regularConfigMap := configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMap.Name = configMapName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Prepare a duplicate of the config map object, with different data.
			duplicatedConfigMap := regularConfigMap.DeepCopy()
			duplicatedConfigMap.Data = map[string]string{
				configMapDataKey: duplicatedConfigMapDataVal,
			}
			duplicatedConfigMapJSON := marshalK8sObjJSON(duplicatedConfigMap)

			// Create a new work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName1, nil, regularNSJSON, regularConfigMapJSON, duplicatedConfigMapJSON)
		})

		// For simplicity reasons, some steps (finalizer handling, appliedWork object creation, etc.) are omitted in
		// this test node, as the behaviors have been verified elsewhere.

		It("should apply the first occurrence of the duplicated manifest", func() {
			// Retrieve the owner reference of the appliedWork object.
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the ConfigMap object has been applied from the first occurrence.
			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object")
		})

		It("should report both copies of the duplicated manifest in the work object status", func() {
			// This guarantees that both copies are present in the work object status before the removal.
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
						Ordinal:    2,
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
							Reason:             string(ApplyResTypeDuplicated),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("can update the work object to remove all copies of the duplicated manifest", func() {
			updateWorkObject(workName, memberReservedNSName1, nil, regularNSJSON)
		})

		It("should remove the config map object", func() {
			regularConfigMapAbsentActual := regularConfigMapAbsentActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapAbsentActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")
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
					UID: regularNS.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the applied manifests manually, as there is no GC controller in the test environment.
			//
			// The config map object should have been removed already; this is kept as a safety net.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Ensure that the appliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("duplicated manifest across the primary and a secondary work object", Ordered, func() {
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
		// Poll frequently when verifying the hand-over, so that a transient deletion of the config map object
		// would not go unnoticed.
		handOverCheckInterval := time.Millisecond * 250

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularConfigMap *corev1.ConfigMap
		var duplicatedConfigMap *corev1.ConfigMap
		var regularNSJSON []byte

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON = marshalK8sObjJSON(regularNS)

			// Prepare a config map object.
			regularConfigMap = configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMap.Name = configMapName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Prepare a duplicate of the config map object, with different data.
			duplicatedConfigMap = regularConfigMap.DeepCopy()
			duplicatedConfigMap.Data = map[string]string{
				configMapDataKey: duplicatedConfigMapDataVal,
			}
			duplicatedConfigMapJSON := marshalK8sObjJSON(duplicatedConfigMap)

			// Create the secondary work object first, so that the work applier can find all the linked work
			// objects as soon as it starts processing the primary work object.
			//
			// As KubeFleet does, the primary work object is set as the controller owner of all secondary work objects,
			// so that events on secondary work objects trigger reconciliations of the primary work object.
			secondaryWork := buildWorkObject(
				secondaryWorkName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				duplicatedConfigMapJSON,
			)
			setPrimaryWorkAsOwner(secondaryWork, primaryWorkName)
			Expect(hubClient.Create(ctx, secondaryWork)).To(Succeed(), "Failed to create the secondary work object")

			// Create the primary work object.
			primaryWork := buildWorkObject(
				primaryWorkName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				regularNSJSON, regularConfigMapJSON,
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

		It("should apply the manifests from the primary work object", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the ConfigMap object has been applied from the primary work object, which always wins.
			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, regularConfigMap)).To(Succeed(), "Failed to retrieve the ConfigMap object")
		})

		It("should update the work object statuses", func() {
			// The primary work object is applied and available.
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
			primaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, primaryWorkName, primaryWorkConds, primaryWorkManifestStatuses)
			Eventually(primaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the primary work object status")

			// The secondary work object reports the duplicate; its conditions reflect only its own manifests.
			secondaryWorkConds := []metav1.Condition{
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
							Status:             metav1.ConditionFalse,
							Reason:             string(ApplyResTypeDuplicated),
							ObservedGeneration: 0,
						},
					},
				},
			}
			secondaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, secondaryWorkName, secondaryWorkConds, secondaryWorkManifestStatuses)
			Eventually(secondaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the secondary work object status")
		})

		It("should update the appliedWork object statuses", func() {
			primaryAppliedResources := []placementv1alpha1.AppliedResource{
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
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
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
						Ordinal:    1,
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
			primaryAppliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, primaryWorkName, primaryAppliedResources)
			Eventually(primaryAppliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the primary appliedWork object status")

			// Nothing has been applied from the secondary work object.
			secondaryAppliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, secondaryWorkName, nil)
			Eventually(secondaryAppliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the secondary appliedWork object status")
		})

		It("should never apply the duplicate from the secondary work object", func() {
			configMapStableActual := configMapStableActual(memberClient1, nsName, configMapName, regularConfigMap.UID, configMap.Data)
			Consistently(configMapStableActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The duplicate has overwritten the config map object unexpectedly")
		})

		It("can update the primary work object to drop the config map manifest", func() {
			// The linked work count and the primary placement resource snapshot stay the same.
			updateLinkedWorkObject(primaryWorkName, memberReservedNSName1, primarySnapshotName, nil, regularNSJSON)
		})

		It("should hand the config map object over to the secondary work object without re-creating it", func() {
			// The config map identifier is still present in one of the linked work objects; the config map
			// object should not be deleted or re-created (the data is not checked here).
			configMapNotRecreatedActual := configMapStableActual(memberClient1, nsName, configMapName, regularConfigMap.UID, nil)
			Consistently(configMapNotRecreatedActual, consistentlyDuration, handOverCheckInterval).Should(Succeed(), "The config map object has been deleted or re-created unexpectedly")

			configMapStableActual := configMapStableActual(memberClient1, nsName, configMapName, regularConfigMap.UID, duplicatedConfigMap.Data)
			Eventually(configMapStableActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to hand the config map object over to the secondary work object")

			// The config map object should still be owned by the appliedWork object of the primary work object.
			configMapObjectAppliedActual := configMapObjectAppliedActual(memberClient1, duplicatedConfigMap, appliedWorkOwnerRef)
			Eventually(configMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object from the secondary work object")
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

		It("should update the appliedWork object statuses", func() {
			primaryAppliedResources := []placementv1alpha1.AppliedResource{
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
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

			// The config map object is the same one as before (i.e., it has not been re-created).
			secondaryAppliedResources := []placementv1alpha1.AppliedResource{
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
					UID: regularConfigMap.UID,
				},
			}
			secondaryAppliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, secondaryWorkName, secondaryAppliedResources)
			Eventually(secondaryAppliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the secondary appliedWork object status")
		})

		AfterAll(func() {
			// Delete the work objects.
			//
			// As there is no GC controller in the test environment (and the secondary work object is not owned by
			// the primary one in this test), both work objects are deleted explicitly.
			deleteWorkObject(primaryWorkName, memberReservedNSName1)
			deleteWorkObject(secondaryWorkName, memberReservedNSName1)

			// Remove the applied manifests manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Ensure that the appliedWork objects have been removed.
			primaryAppliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, primaryWorkName)
			Eventually(primaryAppliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary appliedWork object")

			// The secondary appliedWork object owns no resources, and is deleted by the work applier directly.
			secondaryAppliedWorkAbsentActual := appliedWorkAbsentActual(memberClient1, secondaryWorkName)
			Eventually(secondaryAppliedWorkAbsentActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary appliedWork object")

			// Ensure that the work objects have been removed.
			primaryWorkRemovedActual := workRemovedActual(memberReservedNSName1, primaryWorkName)
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			secondaryWorkRemovedActual := workRemovedActual(memberReservedNSName1, secondaryWorkName)
			Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("duplicated manifest across two secondary work objects", Ordered, func() {
		// The three work objects are linked to each other: they share the same owner placement policy/binding and
		// the same primary placement resource snapshot; the primary work object features the linked work count
		// annotation.
		primaryWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The secondary work objects are named after the primary one, so that the order of their names is known.
		secondaryWorkAName := fmt.Sprintf("%s-a", primaryWorkName)
		secondaryWorkBName := fmt.Sprintf("%s-b", primaryWorkName)
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, primaryWorkName)
		placementBindingName := fmt.Sprintf(placementBindingNameTemplate, primaryWorkName)
		primarySnapshotName := fmt.Sprintf(primarySnapshotNameTemplate, primaryWorkName)
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		configMapAOnlyName := "configmap-a-only"
		// The number of reconciliations to run when verifying that the winner of the duplicate stays the same.
		nudgeCount := 5

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var configMapA *corev1.ConfigMap
		var configMapB *corev1.ConfigMap
		var configMapAOnly *corev1.ConfigMap
		var configMapAOnlyJSON []byte
		var configMapUID types.UID
		var configMapAOnlyUID types.UID

		BeforeAll(func() {
			// Prepare a namespace object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare two copies of a config map object, with different data.
			configMapA = configMap.DeepCopy()
			configMapA.Namespace = nsName
			configMapA.Name = configMapName
			configMapA.Data = map[string]string{
				configMapDataKey: "a",
			}
			configMapAJSON := marshalK8sObjJSON(configMapA)

			configMapB = configMapA.DeepCopy()
			configMapB.Data = map[string]string{
				configMapDataKey: "b",
			}
			configMapBJSON := marshalK8sObjJSON(configMapB)

			// Prepare a different config map object, which replaces the duplicate in secondary work object A later.
			configMapAOnly = configMapA.DeepCopy()
			configMapAOnly.Name = configMapAOnlyName
			configMapAOnlyJSON = marshalK8sObjJSON(configMapAOnly)

			// Create the work objects in an order that does not match the order of their names: secondary work
			// object B first, then secondary work object A, and the primary work object at last, so that the work
			// applier can find all the linked work objects as soon as it starts processing the primary work object.
			//
			// As KubeFleet does, the primary work object is set as the controller owner of all secondary work objects,
			// so that events on secondary work objects trigger reconciliations of the primary work object.
			secondaryWorkB := buildWorkObject(
				secondaryWorkBName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				configMapBJSON,
			)
			setPrimaryWorkAsOwner(secondaryWorkB, primaryWorkName)
			Expect(hubClient.Create(ctx, secondaryWorkB)).To(Succeed(), "Failed to create secondary work object B")

			secondaryWorkA := buildWorkObject(
				secondaryWorkAName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				configMapAJSON,
			)
			setPrimaryWorkAsOwner(secondaryWorkA, primaryWorkName)
			Expect(hubClient.Create(ctx, secondaryWorkA)).To(Succeed(), "Failed to create secondary work object A")

			primaryWork := buildWorkObject(
				primaryWorkName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				regularNSJSON,
			)
			primaryWork.Annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = "3"
			Expect(hubClient.Create(ctx, primaryWork)).To(Succeed(), "Failed to create the primary work object")
		})

		It("should add cleanup finalizer to all the work objects", func() {
			primaryWorkFinalizerAddedActual := workFinalizerAddedActual(memberReservedNSName1, primaryWorkName)
			Eventually(primaryWorkFinalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the primary work object")

			secondaryWorkAFinalizerAddedActual := workFinalizerAddedActual(memberReservedNSName1, secondaryWorkAName)
			Eventually(secondaryWorkAFinalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to secondary work object A")

			secondaryWorkBFinalizerAddedActual := workFinalizerAddedActual(memberReservedNSName1, secondaryWorkBName)
			Eventually(secondaryWorkBFinalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to secondary work object B")
		})

		It("should prepare an appliedWork object for each work object", func() {
			primaryAppliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, primaryWorkName)
			Eventually(primaryAppliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object for the primary work object")

			secondaryAppliedWorkACreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, secondaryWorkAName)
			Eventually(secondaryAppliedWorkACreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object for secondary work object A")

			secondaryAppliedWorkBCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, secondaryWorkBName)
			Eventually(secondaryAppliedWorkBCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object for secondary work object B")

			// All the manifests across the linked work objects are owned by the appliedWork object of the
			// primary work object.
			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, primaryWorkName)
		})

		It("should apply the duplicate from the secondary work object whose name sorts first", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient1, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the ConfigMap object has been applied from secondary work object A.
			configMapObjectAppliedActual := configMapObjectAppliedActual(memberClient1, configMapA, appliedWorkOwnerRef)
			Eventually(configMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object from secondary work object A")

			gotConfigMap := &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, gotConfigMap)).To(Succeed(), "Failed to retrieve the ConfigMap object")
			configMapUID = gotConfigMap.UID
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
			// Note that workStatusUpdated sets the observed generations on the given conditions; use a copy here
			// as the conditions are shared between the work objects.
			primaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, primaryWorkName, append([]metav1.Condition{}, workConds...), primaryWorkManifestStatuses)
			Eventually(primaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the primary work object status")

			secondaryWorkAManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
			secondaryWorkAStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, secondaryWorkAName, append([]metav1.Condition{}, workConds...), secondaryWorkAManifestStatuses)
			Eventually(secondaryWorkAStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of secondary work object A")

			// Secondary work object B reports the duplicate.
			secondaryWorkBConds := []metav1.Condition{
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
			secondaryWorkBManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
							Reason:             string(ApplyResTypeDuplicated),
							ObservedGeneration: 0,
						},
					},
				},
			}
			secondaryWorkBStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, secondaryWorkBName, secondaryWorkBConds, secondaryWorkBManifestStatuses)
			Eventually(secondaryWorkBStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of secondary work object B")
		})

		It("should update the appliedWork object statuses", func() {
			primaryAppliedResources := []placementv1alpha1.AppliedResource{
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
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

			secondaryAAppliedResources := []placementv1alpha1.AppliedResource{
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
					UID: configMapUID,
				},
			}
			secondaryAppliedWorkAStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, secondaryWorkAName, secondaryAAppliedResources)
			Eventually(secondaryAppliedWorkAStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of the appliedWork object for secondary work object A")

			// Nothing has been applied from secondary work object B.
			secondaryAppliedWorkBStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, secondaryWorkBName, nil)
			Eventually(secondaryAppliedWorkBStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of the appliedWork object for secondary work object B")
		})

		It("should keep the winner of the duplicate stable across reconciliations", func() {
			// The linked work objects are listed from a cache, the order of which is random; the work applier
			// sorts the secondary work objects by name, so that the same copy wins upon every reconciliation.
			//
			// Here the primary work object is nudged a fixed number of times; each nudge triggers an immediate
			// reconciliation, which lists the linked work objects again.
			configMapStableActual := configMapStableActual(memberClient1, nsName, configMapName, configMapUID, configMapA.Data)

			secondaryWorkAConds := []metav1.Condition{
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
			secondaryWorkAManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
			secondaryWorkAStatusUnchangedActual := workStatusUpdated(memberReservedNSName1, secondaryWorkAName, secondaryWorkAConds, secondaryWorkAManifestStatuses)

			secondaryWorkBConds := []metav1.Condition{
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
			secondaryWorkBManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
							Reason:             string(ApplyResTypeDuplicated),
							ObservedGeneration: 0,
						},
					},
				},
			}
			secondaryWorkBStatusUnchangedActual := workStatusUpdated(memberReservedNSName1, secondaryWorkBName, secondaryWorkBConds, secondaryWorkBManifestStatuses)

			for i := 0; i < nudgeCount; i++ {
				nudgeWork(primaryWorkName, memberReservedNSName1)

				primaryWorkProcessedActual := workProcessedAtLatestGenerationActual(memberReservedNSName1, primaryWorkName)
				Eventually(primaryWorkProcessedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to process the primary work object (nudge #%d)", i)

				Expect(configMapStableActual()).To(Succeed(), "The winner of the duplicate has changed (nudge #%d)", i)
				Expect(secondaryWorkAStatusUnchangedActual()).To(Succeed(), "The status of secondary work object A has changed (nudge #%d)", i)
				Expect(secondaryWorkBStatusUnchangedActual()).To(Succeed(), "The status of secondary work object B has changed (nudge #%d)", i)
			}
		})

		It("can update secondary work object A to drop the duplicate", func() {
			// The spec change on secondary work object A triggers a reconciliation of the primary work object via
			// the owner reference.
			updateLinkedWorkObject(secondaryWorkAName, memberReservedNSName1, primarySnapshotName, nil, configMapAOnlyJSON)
		})

		It("should hand the config map object over to secondary work object B without re-creating it", func() {
			configMapStableActual := configMapStableActual(memberClient1, nsName, configMapName, configMapUID, configMapB.Data)
			Eventually(configMapStableActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to hand the config map object over to secondary work object B")

			// The config map object should still be owned by the appliedWork object of the primary work object.
			configMapObjectAppliedActual := configMapObjectAppliedActual(memberClient1, configMapB, appliedWorkOwnerRef)
			Eventually(configMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object from secondary work object B")
		})

		It("should apply the new manifest from secondary work object A", func() {
			configMapObjectAppliedActual := configMapObjectAppliedActual(memberClient1, configMapAOnly, appliedWorkOwnerRef)
			Eventually(configMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the new config map object from secondary work object A")

			gotConfigMap := &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapAOnlyName}, gotConfigMap)).To(Succeed(), "Failed to retrieve the new ConfigMap object")
			configMapAOnlyUID = gotConfigMap.UID
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
			// Note that workStatusUpdated sets the observed generations on the given conditions; use a copy here
			// as the conditions are shared between the work objects.
			primaryWorkStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, primaryWorkName, append([]metav1.Condition{}, workConds...), primaryWorkManifestStatuses)
			Eventually(primaryWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the primary work object status")

			secondaryWorkAManifestStatuses := []placementv1alpha1.PerManifestStatus{
				{
					Identifier: placementv1alpha1.ManifestIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       configMapAOnlyName,
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
			secondaryWorkAStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, secondaryWorkAName, append([]metav1.Condition{}, workConds...), secondaryWorkAManifestStatuses)
			Eventually(secondaryWorkAStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of secondary work object A")

			secondaryWorkBManifestStatuses := []placementv1alpha1.PerManifestStatus{
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
			secondaryWorkBStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, secondaryWorkBName, append([]metav1.Condition{}, workConds...), secondaryWorkBManifestStatuses)
			Eventually(secondaryWorkBStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of secondary work object B")
		})

		It("should update the appliedWork object statuses", func() {
			primaryAppliedResources := []placementv1alpha1.AppliedResource{
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
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

			secondaryAAppliedResources := []placementv1alpha1.AppliedResource{
				{
					AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
						Ordinal:    0,
						APIGroup:   "",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Resource:   "configmaps",
						Name:       configMapAOnlyName,
						Namespace:  nsName,
					},
					UID: configMapAOnlyUID,
				},
			}
			secondaryAppliedWorkAStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, secondaryWorkAName, secondaryAAppliedResources)
			Eventually(secondaryAppliedWorkAStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of the appliedWork object for secondary work object A")

			// The config map object is the same one as before (i.e., it has not been re-created).
			secondaryBAppliedResources := []placementv1alpha1.AppliedResource{
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
					UID: configMapUID,
				},
			}
			secondaryAppliedWorkBStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, secondaryWorkBName, secondaryBAppliedResources)
			Eventually(secondaryAppliedWorkBStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of the appliedWork object for secondary work object B")
		})

		AfterAll(func() {
			// Delete the work objects.
			//
			// As there is no GC controller in the test environment (and the secondary work objects are not owned by
			// the primary one in this test), all work objects are deleted explicitly.
			deleteWorkObject(primaryWorkName, memberReservedNSName1)
			deleteWorkObject(secondaryWorkAName, memberReservedNSName1)
			deleteWorkObject(secondaryWorkBName, memberReservedNSName1)

			// Remove the applied manifests manually, as there is no GC controller in the test environment.
			configMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(configMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			configMapAOnlyRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapAOnlyName)
			Eventually(configMapAOnlyRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the new config map object")

			// Ensure that the appliedWork objects have been removed.
			primaryAppliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, primaryWorkName)
			Eventually(primaryAppliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary appliedWork object")

			// The appliedWork objects for the secondary work objects own no resources, and are deleted by the work
			// applier directly.
			secondaryAppliedWorkAAbsentActual := appliedWorkAbsentActual(memberClient1, secondaryWorkAName)
			Eventually(secondaryAppliedWorkAAbsentActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object for secondary work object A")

			secondaryAppliedWorkBAbsentActual := appliedWorkAbsentActual(memberClient1, secondaryWorkBName)
			Eventually(secondaryAppliedWorkBAbsentActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object for secondary work object B")

			// Ensure that the work objects have been removed.
			primaryWorkRemovedActual := workRemovedActual(memberReservedNSName1, primaryWorkName)
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			secondaryWorkARemovedActual := workRemovedActual(memberReservedNSName1, secondaryWorkAName)
			Eventually(secondaryWorkARemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove secondary work object A")

			secondaryWorkBRemovedActual := workRemovedActual(memberReservedNSName1, secondaryWorkBName)
			Eventually(secondaryWorkBRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove secondary work object B")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("namespace absent with namespace auto-creation disabled", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The namespace is not created by the work applier, nor by the test case at first.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularConfigMap *corev1.ConfigMap

		BeforeAll(func() {
			// Prepare a config map object.
			regularConfigMap = configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMap.Name = configMapName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Forbid the work applier from creating namespaces; the API server fills in the defaults for the
			// other fields in the sync strategy.
			syncStrategy := &placementv1alpha1.SyncStrategy{
				WhenNamespaceDoesNotExist: placementv1alpha1.WhenNamespaceDoesNotExistOptionReportError,
			}

			// Create a new work object with the manifest JSON.
			createWorkObject(workName, memberReservedNSName1, syncStrategy, regularConfigMapJSON)
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

		It("should not apply the manifest or create the namespace", func() {
			// Keep the window short; the namespace is created outside the work applier later, which can only be
			// picked up at the next requeue of the work object.
			regularConfigMapAbsentActual := regularConfigMapAbsentActual(memberClient1, nsName, configMapName)
			namespaceAbsentActual := namespaceAbsentActual(memberClient1, nsName)
			Consistently(func() error {
				if err := regularConfigMapAbsentActual(); err != nil {
					return err
				}
				return namespaceAbsentActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The config map object or the namespace has been created unexpectedly")
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
							Reason:             string(ApplyResTypeFailedToApply),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should report the missing namespace as the cause of the failure", func() {
			manifestAppliedCondMessageActual := manifestAppliedCondMessageActual(memberReservedNSName1, workName, 0, fmt.Sprintf("namespaces %q not found", nsName))
			Eventually(manifestAppliedCondMessageActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the missing namespace in the work object status")
		})

		It("should not report any applied resource in the appliedWork object status", func() {
			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, nil)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("can create the namespace outside the work applier", func() {
			regularNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
				},
			}
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")
		})

		It("should apply the manifest", func() {
			// Creating the namespace triggers no event for the work object; the manifest is applied at the next
			// requeue of the work object. The work object is unavailable and stays in the slow backoff stage at
			// this moment, where requeues are at most 15 seconds apart; allow more time for the next attempt to
			// happen.
			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object")

			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, regularConfigMap)).To(Succeed(), "Failed to retrieve the ConfigMap object")
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
					UID: regularConfigMap.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("should leave the namespace unmanaged", func() {
			// The work applier must not adopt an object that it has never placed.
			namespaceNotManagedActual := namespaceNotManagedActual(memberClient1, nsName)
			Consistently(namespaceNotManagedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The namespace has been adopted by the work applier unexpectedly")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSName1)

			// Remove the applied manifests manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Ensure that the appliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})
})
