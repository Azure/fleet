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

// Package workgenerator contains the controller logic for reconciling placement binding objects.
package workgenerator

import (
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var _ = Describe("reconciling placement bindings (single placement resource snapshot)", func() {
	Context("generate works", Ordered, func() {
		bindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		snapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0)
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		var workName string
		var snapshot placementv1alpha1.PlacementResourceSnapshotAccessor

		manifestIdentifiers := []placementv1alpha1.ManifestIdentifier{
			{
				Ordinal:    0,
				Name:       nsName,
				APIVersion: "v1",
				Kind:       nsKind,
				Resource:   nsResource,
			},
			{
				Ordinal:    1,
				Namespace:  nsName,
				Name:       deployName,
				APIGroup:   appsAPIGroup,
				APIVersion: "v1",
				Kind:       deployKind,
				Resource:   deployResource,
			},
		}

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Deployment object.
			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a placement resource snapshot with the NS and the Deployment objects.
			snapshot = createPlacementResourceSnapshot(appNamespaceName, snapshotName, placementPolicyName, 0,
				placementv1alpha1.SnapshottedResource{
					Identifier: placementv1alpha1.ObjectReference{
						Name:       nsName,
						APIVersion: "v1",
						Kind:       nsKind,
					},
					Manifest: runtime.RawExtension{Raw: regularNSJSON},
				},
				placementv1alpha1.SnapshottedResource{
					Identifier: placementv1alpha1.ObjectReference{
						Namespace:  nsName,
						Name:       deployName,
						APIGroup:   appsAPIGroup,
						APIVersion: "v1",
						Kind:       deployKind,
					},
					Manifest: runtime.RawExtension{Raw: regularDeployJSON},
				},
			)

			// Create a placement binding that binds the snapshot to a member cluster.
			binding := createPlacementBinding(appNamespaceName, bindingName, placementPolicyName, memberCluster1Name, snapshotName)
			workName = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, true,
				&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: "0"})
		})

		It("should add cleanup finalizer to the placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(appNamespaceName, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
		})

		It("should generate the work objects", func() {
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, appNamespaceName, bindingName, placementPolicyName, snapshot),
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(2, snapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workName, manifestIdentifiers)
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(2, snapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		AfterAll(func() {
			// Delete the placement binding; the work generator should clean up the work objects and then
			// remove the cleanup finalizer.
			removePlacementBinding(appNamespaceName, bindingName)

			workRemovedActual := workRemovedActual(workNS, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			bindingRemovedActual := placementBindingRemovedActual(appNamespaceName, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")

			snapshotRemovedActual := placementResourceSnapshotRemovedActual(appNamespaceName, snapshotName)
			Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement resource snapshot")
		})
	})

	Context("update works", Ordered, func() {
		bindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		oldSnapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0)
		newSnapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 1)
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		var workName string
		var oldSnapshot, newSnapshot placementv1alpha1.PlacementResourceSnapshotAccessor

		nsIdentifier := placementv1alpha1.ObjectReference{
			Name:       nsName,
			APIVersion: "v1",
			Kind:       nsKind,
		}
		deployIdentifier := placementv1alpha1.ObjectReference{
			Namespace:  nsName,
			Name:       deployName,
			APIGroup:   appsAPIGroup,
			APIVersion: "v1",
			Kind:       deployKind,
		}
		configMapIdentifier := placementv1alpha1.ObjectReference{
			Namespace:  nsName,
			Name:       configMapName,
			APIVersion: "v1",
			Kind:       configMapKind,
		}

		oldManifestIdentifiers := []placementv1alpha1.ManifestIdentifier{
			{
				Ordinal:    0,
				Name:       nsName,
				APIVersion: "v1",
				Kind:       nsKind,
				Resource:   nsResource,
			},
			{
				Ordinal:    1,
				Namespace:  nsName,
				Name:       deployName,
				APIGroup:   appsAPIGroup,
				APIVersion: "v1",
				Kind:       deployKind,
				Resource:   deployResource,
			},
		}
		newManifestIdentifiers := []placementv1alpha1.ManifestIdentifier{
			oldManifestIdentifiers[0],
			{
				Ordinal:    1,
				Namespace:  nsName,
				Name:       configMapName,
				APIVersion: "v1",
				Kind:       configMapKind,
				Resource:   configMapResource,
			},
		}

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Deployment object.
			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a placement resource snapshot with the NS and the Deployment objects.
			oldSnapshot = createPlacementResourceSnapshot(appNamespaceName, oldSnapshotName, placementPolicyName, 0,
				placementv1alpha1.SnapshottedResource{
					Identifier: nsIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularNSJSON},
				},
				placementv1alpha1.SnapshottedResource{
					Identifier: deployIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularDeployJSON},
				},
			)

			// Create a placement binding that binds the snapshot to a member cluster.
			binding := createPlacementBinding(appNamespaceName, bindingName, placementPolicyName, memberCluster1Name, oldSnapshotName)
			workName = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, true,
				&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: "0"})
		})

		It("should add cleanup finalizer to the placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(appNamespaceName, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
		})

		It("should generate the work objects", func() {
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, appNamespaceName, bindingName, placementPolicyName, oldSnapshot),
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(2, oldSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workName, oldManifestIdentifiers)
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(2, oldSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can roll out a new placement resource snapshot", func() {
			// Prepare a NS object (unchanged).
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a ConfigMap object (new); the Deployment object is dropped.
			regularConfigMap := configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Create a new placement resource snapshot with the NS and the new ConfigMap objects.
			newSnapshot = createPlacementResourceSnapshot(appNamespaceName, newSnapshotName, placementPolicyName, 1,
				placementv1alpha1.SnapshottedResource{
					Identifier: nsIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularNSJSON},
				},
				placementv1alpha1.SnapshottedResource{
					Identifier: configMapIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularConfigMapJSON},
				},
			)

			// Point the placement binding to the new snapshot.
			updatePlacementBindingResourceSnapshot(appNamespaceName, bindingName, newSnapshotName)
		})

		It("should update the work objects", func() {
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, appNamespaceName, bindingName, placementPolicyName, newSnapshot),
			}

			worksUpdatedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(2, newSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workName, newManifestIdentifiers)
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(2, newSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		AfterAll(func() {
			// Delete the placement binding; the work generator should clean up the work objects and then
			// remove the cleanup finalizer.
			removePlacementBinding(appNamespaceName, bindingName)

			workRemovedActual := workRemovedActual(workNS, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			bindingRemovedActual := placementBindingRemovedActual(appNamespaceName, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")

			oldSnapshotRemovedActual := placementResourceSnapshotRemovedActual(appNamespaceName, oldSnapshotName)
			Eventually(oldSnapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the old placement resource snapshot")

			newSnapshotRemovedActual := placementResourceSnapshotRemovedActual(appNamespaceName, newSnapshotName)
			Eventually(newSnapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the new placement resource snapshot")
		})
	})

	Context("refresh binding status upon work status changes", Ordered, func() {
		bindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		snapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0)
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		var workName string
		var snapshot placementv1alpha1.PlacementResourceSnapshotAccessor

		deployIdentifier := placementv1alpha1.ObjectReference{
			Namespace:  nsName,
			Name:       deployName,
			APIGroup:   appsAPIGroup,
			APIVersion: "v1",
			Kind:       deployKind,
		}

		manifestIdentifiers := []placementv1alpha1.ManifestIdentifier{
			{
				Ordinal:    0,
				Name:       nsName,
				APIVersion: "v1",
				Kind:       nsKind,
				Resource:   nsResource,
			},
			{
				Ordinal:    1,
				Namespace:  nsName,
				Name:       deployName,
				APIGroup:   appsAPIGroup,
				APIVersion: "v1",
				Kind:       deployKind,
				Resource:   deployResource,
			},
		}

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Deployment object.
			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a placement resource snapshot with the NS and the Deployment objects.
			snapshot = createPlacementResourceSnapshot(appNamespaceName, snapshotName, placementPolicyName, 0,
				placementv1alpha1.SnapshottedResource{
					Identifier: placementv1alpha1.ObjectReference{
						Name:       nsName,
						APIVersion: "v1",
						Kind:       nsKind,
					},
					Manifest: runtime.RawExtension{Raw: regularNSJSON},
				},
				placementv1alpha1.SnapshottedResource{
					Identifier: deployIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularDeployJSON},
				},
			)

			// Create a placement binding that binds the snapshot to a member cluster.
			binding := createPlacementBinding(appNamespaceName, bindingName, placementPolicyName, memberCluster1Name, snapshotName)
			workName = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, true,
				&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: "0"})
		})

		It("should add cleanup finalizer to the placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(appNamespaceName, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
		})

		It("should generate the work objects", func() {
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, appNamespaceName, bindingName, placementPolicyName, snapshot),
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(2, snapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workName, manifestIdentifiers)
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(2, snapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the deployment (and consequently the work object) as unavailable", func() {
			markWorkManifestAsUnavailable(workNS, workName, 1)
		})

		It("should refresh the placement binding status", func() {
			wantStatus := &placementv1alpha1.PlacementBindingStatus{
				Conditions: []metav1.Condition{
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeSynchronized,
						Status: metav1.ConditionTrue,
						Reason: placementv1alpha1.PlacementBindingSynchronizedCondReasonAllResourcesSynchronized,
					},
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeAvailable,
						Status: metav1.ConditionFalse,
						Reason: placementv1alpha1.PlacementBindingAvailableCondReasonSomeResourcesUnavailable,
					},
				},
				SelectedResources:     ptr.To(int32(2)),
				SynchronizedResources: ptr.To(int32(2)),
				AvailableResources:    ptr.To(int32(1)),
				FailedResources: []placementv1alpha1.FailedResource{
					{
						ObjectRef: deployIdentifier,
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeAvailable,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsUnavailableReason,
							},
						},
					},
				},
				LastProcessedResourceSnapshotName: ptr.To(snapshotName),
			}

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		AfterAll(func() {
			// Delete the placement binding; the work generator should clean up the work objects and then
			// remove the cleanup finalizer.
			removePlacementBinding(appNamespaceName, bindingName)

			workRemovedActual := workRemovedActual(workNS, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			bindingRemovedActual := placementBindingRemovedActual(appNamespaceName, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")

			snapshotRemovedActual := placementResourceSnapshotRemovedActual(appNamespaceName, snapshotName)
			Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement resource snapshot")
		})
	})

	Context("update works (no spec changes)", Ordered, func() {
		bindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		oldSnapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0)
		newSnapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 1)
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		var workName string
		var oldSnapshot, newSnapshot placementv1alpha1.PlacementResourceSnapshotAccessor
		var resources []placementv1alpha1.SnapshottedResource
		var oldBindingGeneration int64

		manifestIdentifiers := []placementv1alpha1.ManifestIdentifier{
			{
				Ordinal:    0,
				Name:       nsName,
				APIVersion: "v1",
				Kind:       nsKind,
				Resource:   nsResource,
			},
			{
				Ordinal:    1,
				Namespace:  nsName,
				Name:       deployName,
				APIGroup:   appsAPIGroup,
				APIVersion: "v1",
				Kind:       deployKind,
				Resource:   deployResource,
			},
		}

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Deployment object.
			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a placement resource snapshot with the NS and the Deployment objects.
			resources = []placementv1alpha1.SnapshottedResource{
				{
					Identifier: placementv1alpha1.ObjectReference{
						Name:       nsName,
						APIVersion: "v1",
						Kind:       nsKind,
					},
					Manifest: runtime.RawExtension{Raw: regularNSJSON},
				},
				{
					Identifier: placementv1alpha1.ObjectReference{
						Namespace:  nsName,
						Name:       deployName,
						APIGroup:   appsAPIGroup,
						APIVersion: "v1",
						Kind:       deployKind,
					},
					Manifest: runtime.RawExtension{Raw: regularDeployJSON},
				},
			}
			oldSnapshot = createPlacementResourceSnapshot(appNamespaceName, oldSnapshotName, placementPolicyName, 0, resources...)

			// Create a placement binding that binds the snapshot to a member cluster.
			binding := createPlacementBinding(appNamespaceName, bindingName, placementPolicyName, memberCluster1Name, oldSnapshotName)
			workName = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, true,
				&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: "0"})
		})

		It("should add cleanup finalizer to the placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(appNamespaceName, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
		})

		It("should generate the work objects", func() {
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, appNamespaceName, bindingName, placementPolicyName, oldSnapshot),
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(2, oldSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workName, manifestIdentifiers)
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(2, oldSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can roll out a new placement resource snapshot with the same content", func() {
			// Record the current generation of the placement binding.
			binding, err := getPlacementBinding(appNamespaceName, bindingName)
			Expect(err).To(Succeed(), "Failed to retrieve the placement binding")
			oldBindingGeneration = binding.GetGeneration()

			// Create a new placement resource snapshot with exactly the same resources.
			newSnapshot = createPlacementResourceSnapshot(appNamespaceName, newSnapshotName, placementPolicyName, 1, resources...)

			// Point the placement binding to the new snapshot.
			updatePlacementBindingResourceSnapshot(appNamespaceName, bindingName, newSnapshotName)
		})

		It("should update the work objects", func() {
			// The manifests stay the same; only the link to the primary placement resource snapshot changes.
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, appNamespaceName, bindingName, placementPolicyName, newSnapshot),
			}

			worksUpdatedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")
		})

		It("should refresh the placement binding status without waiting for the work objects to be re-processed", func() {
			// As the work objects have no spec changes, their existing status still applies; the placement binding
			// should be reported as synchronized and available, with the conditions observing the new generation.
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(2, newSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")

			binding, err := getPlacementBinding(appNamespaceName, bindingName)
			Expect(err).To(Succeed(), "Failed to retrieve the placement binding")
			Expect(binding.GetGeneration()).To(BeNumerically(">", oldBindingGeneration), "The placement binding generation has not been bumped")
		})

		AfterAll(func() {
			// Delete the placement binding; the work generator should clean up the work objects and then
			// remove the cleanup finalizer.
			removePlacementBinding(appNamespaceName, bindingName)

			workRemovedActual := workRemovedActual(workNS, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			bindingRemovedActual := placementBindingRemovedActual(appNamespaceName, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")

			oldSnapshotRemovedActual := placementResourceSnapshotRemovedActual(appNamespaceName, oldSnapshotName)
			Eventually(oldSnapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the old placement resource snapshot")

			newSnapshotRemovedActual := placementResourceSnapshotRemovedActual(appNamespaceName, newSnapshotName)
			Eventually(newSnapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the new placement resource snapshot")
		})
	})

	Context("backport status for failed manifests", Ordered, func() {
		bindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		snapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0)
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		var workName string
		var snapshot placementv1alpha1.PlacementResourceSnapshotAccessor

		deployIdentifier := placementv1alpha1.ObjectReference{
			Namespace:  nsName,
			Name:       deployName,
			APIGroup:   appsAPIGroup,
			APIVersion: "v1",
			Kind:       deployKind,
		}
		configMapIdentifier := placementv1alpha1.ObjectReference{
			Namespace:  nsName,
			Name:       configMapName,
			APIVersion: "v1",
			Kind:       configMapKind,
		}

		manifestIdentifiers := []placementv1alpha1.ManifestIdentifier{
			{
				Ordinal:    0,
				Name:       nsName,
				APIVersion: "v1",
				Kind:       nsKind,
				Resource:   nsResource,
			},
			{
				Ordinal:    1,
				Namespace:  nsName,
				Name:       deployName,
				APIGroup:   appsAPIGroup,
				APIVersion: "v1",
				Kind:       deployKind,
				Resource:   deployResource,
			},
			{
				Ordinal:    2,
				Namespace:  nsName,
				Name:       configMapName,
				APIVersion: "v1",
				Kind:       configMapKind,
				Resource:   configMapResource,
			},
		}

		// The diff details reported for the ConfigMap object, as if the member agent has failed to take over an
		// existing ConfigMap object with different data in the member cluster.
		//
		// Note that the timestamp is truncated to the second, as this is the precision kept by the API server.
		diffDetails := &placementv1alpha1.DiffDetails{
			ObservedInMemberClusterGeneration: ptr.To(int64(0)),
			FirstDiffedObservedTimestamp:      metav1.NewTime(time.Now().Truncate(time.Second)),
			ObservedDiffs: []placementv1alpha1.PatchDetail{
				{
					Path:          "/data/" + configMapDataKey,
					ValueInMember: altConfigMapDataValue1,
					ValueInHub:    configMapDataValue,
				},
			},
		}

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Deployment object.
			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Prepare a ConfigMap object.
			regularConfigMap := configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Create a placement resource snapshot with the NS, the Deployment, and the ConfigMap objects.
			snapshot = createPlacementResourceSnapshot(appNamespaceName, snapshotName, placementPolicyName, 0,
				placementv1alpha1.SnapshottedResource{
					Identifier: placementv1alpha1.ObjectReference{
						Name:       nsName,
						APIVersion: "v1",
						Kind:       nsKind,
					},
					Manifest: runtime.RawExtension{Raw: regularNSJSON},
				},
				placementv1alpha1.SnapshottedResource{
					Identifier: deployIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularDeployJSON},
				},
				placementv1alpha1.SnapshottedResource{
					Identifier: configMapIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularConfigMapJSON},
				},
			)

			// Create a placement binding that binds the snapshot to a member cluster.
			binding := createPlacementBinding(appNamespaceName, bindingName, placementPolicyName, memberCluster1Name, snapshotName)
			workName = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, true,
				&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: "0"})
		})

		It("should add cleanup finalizer to the placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(appNamespaceName, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
		})

		It("should generate the work objects", func() {
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, appNamespaceName, bindingName, placementPolicyName, snapshot),
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(3, snapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the deployment as unavailable, and the config map as failed to be applied (with diff details)", func() {
			markWorkManifestsAsFailed(workNS, workName, manifestIdentifiers,
				map[int]*placementv1alpha1.DiffDetails{2: diffDetails},
				sets.New(1),
			)
		})

		It("should refresh the placement binding status with the failure info", func() {
			wantStatus := &placementv1alpha1.PlacementBindingStatus{
				Conditions: []metav1.Condition{
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeSynchronized,
						Status: metav1.ConditionFalse,
						Reason: placementv1alpha1.PlacementBindingSynchronizedCondReasonFailedToSynchronizeSomeResources,
					},
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeAvailable,
						Status: metav1.ConditionFalse,
						Reason: placementv1alpha1.PlacementBindingAvailableCondReasonSomeResourcesUnavailable,
					},
				},
				SelectedResources:     ptr.To(int32(3)),
				SynchronizedResources: ptr.To(int32(2)),
				AvailableResources:    ptr.To(int32(1)),
				FailedResources: []placementv1alpha1.FailedResource{
					{
						ObjectRef: deployIdentifier,
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeAvailable,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsUnavailableReason,
							},
						},
					},
					{
						ObjectRef: configMapIdentifier,
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeApplied,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsFailedToApplyReason,
							},
						},
						DiffDetails: diffDetails,
					},
				},
				LastProcessedResourceSnapshotName: ptr.To(snapshotName),
			}

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		AfterAll(func() {
			// Delete the placement binding; the work generator should clean up the work objects and then
			// remove the cleanup finalizer.
			removePlacementBinding(appNamespaceName, bindingName)

			workRemovedActual := workRemovedActual(workNS, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			bindingRemovedActual := placementBindingRemovedActual(appNamespaceName, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")

			snapshotRemovedActual := placementResourceSnapshotRemovedActual(appNamespaceName, snapshotName)
			Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement resource snapshot")
		})
	})
})

var _ = Describe("reconciling placement bindings (multiple placement resource snapshots)", func() {
	Context("generate works", Ordered, func() {
		bindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		// The primary snapshot (sub-index 0) and two secondary snapshots (sub-indices 1 and 2) of the same index.
		snapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 0, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 0, 2),
		}
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		// The work objects (and the snapshots they are derived from), ordered by snapshot sub-index.
		workNames := make([]string, len(snapshotNames))
		snapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(snapshotNames))

		// Each snapshot (and consequently each work object) features its own set of differently named NS,
		// Deployment, and ConfigMap objects.
		nsNames := make([]string, len(snapshotNames))
		deployNames := make([]string, len(snapshotNames))
		configMapNames := make([]string, len(snapshotNames))
		manifestIdentifiersPerWork := make([][]placementv1alpha1.ManifestIdentifier, len(snapshotNames))
		for subIdx := range snapshotNames {
			nsNames[subIdx] = fmt.Sprintf(indexedNSNameTemplate, subIdx)
			deployNames[subIdx] = fmt.Sprintf(deployNameTemplate, subIdx)
			configMapNames[subIdx] = fmt.Sprintf(configMapNameTemplate, subIdx)
			manifestIdentifiersPerWork[subIdx] = []placementv1alpha1.ManifestIdentifier{
				{
					Ordinal:    0,
					Name:       nsNames[subIdx],
					APIVersion: "v1",
					Kind:       nsKind,
					Resource:   nsResource,
				},
				{
					Ordinal:    1,
					Namespace:  nsNames[subIdx],
					Name:       deployNames[subIdx],
					APIGroup:   appsAPIGroup,
					APIVersion: "v1",
					Kind:       deployKind,
					Resource:   deployResource,
				},
				{
					Ordinal:    2,
					Namespace:  nsNames[subIdx],
					Name:       configMapNames[subIdx],
					APIVersion: "v1",
					Kind:       configMapKind,
					Resource:   configMapResource,
				},
			}
		}

		BeforeAll(func() {
			resourcesPerSnapshot := make([][]placementv1alpha1.SnapshottedResource, len(snapshotNames))
			for subIdx := range snapshotNames {
				// Prepare a NS object.
				regularNS := ns.DeepCopy()
				regularNS.Name = nsNames[subIdx]
				regularNSJSON := marshalK8sObjJSON(regularNS)

				// Prepare a Deployment object.
				regularDeploy := deploy.DeepCopy()
				regularDeploy.Namespace = nsNames[subIdx]
				regularDeploy.Name = deployNames[subIdx]
				regularDeployJSON := marshalK8sObjJSON(regularDeploy)

				// Prepare a ConfigMap object.
				regularConfigMap := configMap.DeepCopy()
				regularConfigMap.Namespace = nsNames[subIdx]
				regularConfigMap.Name = configMapNames[subIdx]
				regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

				resourcesPerSnapshot[subIdx] = []placementv1alpha1.SnapshottedResource{
					{
						Identifier: placementv1alpha1.ObjectReference{
							Name:       nsNames[subIdx],
							APIVersion: "v1",
							Kind:       nsKind,
						},
						Manifest: runtime.RawExtension{Raw: regularNSJSON},
					},
					{
						Identifier: placementv1alpha1.ObjectReference{
							Namespace:  nsNames[subIdx],
							Name:       deployNames[subIdx],
							APIGroup:   appsAPIGroup,
							APIVersion: "v1",
							Kind:       deployKind,
						},
						Manifest: runtime.RawExtension{Raw: regularDeployJSON},
					},
					{
						Identifier: placementv1alpha1.ObjectReference{
							Namespace:  nsNames[subIdx],
							Name:       configMapNames[subIdx],
							APIVersion: "v1",
							Kind:       configMapKind,
						},
						Manifest: runtime.RawExtension{Raw: regularConfigMapJSON},
					},
				}
			}

			// Create the placement resource snapshots, with a different set of resources in each snapshot. The
			// secondary snapshots are created first, as the placement resource snapshot manager would do.
			for subIdx := len(snapshotNames) - 1; subIdx >= 0; subIdx-- {
				snapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(appNamespaceName, snapshotNames[subIdx], placementPolicyName,
					0, subIdx, len(snapshotNames), resourcesPerSnapshot[subIdx]...)
			}

			// Create a placement binding that binds the snapshots to a member cluster.
			binding := createPlacementBinding(appNamespaceName, bindingName, placementPolicyName, memberCluster1Name, snapshotNames[0])
			for subIdx := range snapshotNames {
				workNames[subIdx] = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, subIdx == 0,
					&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: strconv.Itoa(subIdx)})
			}
		})

		It("should add cleanup finalizer to the placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(appNamespaceName, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
		})

		It("should generate the work objects", func() {
			// Retrieve the primary work object; the secondary work objects should be owned by it.
			primaryWork := &placementv1alpha1.Work{}
			Eventually(func() error {
				return hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to retrieve the primary work object")
			ownerRef := &metav1.OwnerReference{
				APIVersion:         placementv1alpha1.GroupVersion.String(),
				Kind:               workKind,
				Name:               primaryWork.Name,
				UID:                primaryWork.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}

			wantWorks := make([]placementv1alpha1.Work, len(snapshotNames))
			for subIdx := range snapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], appNamespaceName, bindingName, placementPolicyName,
					snapshotNames[0], subIdx, len(snapshotNames), ownerRef, snapshots[subIdx])
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(9, snapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the primary work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workNames[0], manifestIdentifiersPerWork[0])
		})

		It("should not refresh the placement binding status until all work objects have been processed", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(9, snapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The placement binding status has been refreshed prematurely")
		})

		It("can mark the secondary work objects as applied and available", func() {
			for subIdx := 1; subIdx < len(snapshotNames); subIdx++ {
				markWorkAsAppliedAndAvailable(workNS, workNames[subIdx], manifestIdentifiersPerWork[subIdx])
			}
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(9, snapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		AfterAll(func() {
			// Delete the placement binding; the work generator should clean up the primary work object and
			// then remove the cleanup finalizer.
			removePlacementBinding(appNamespaceName, bindingName)

			primaryWorkRemovedActual := workRemovedActual(workNS, workNames[0])
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			bindingRemovedActual := placementBindingRemovedActual(appNamespaceName, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")

			// The secondary work objects are owned by the primary work object and normally would be garbage collected
			// by Kubernetes; however, the environment prepared by the envtest package does not run the built-in
			// garbage collector, so the test suite deletes them manually.
			for subIdx := 1; subIdx < len(snapshotNames); subIdx++ {
				secondaryWork := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: workNS,
						Name:      workNames[subIdx],
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, secondaryWork))).To(Succeed(), "Failed to delete the secondary work object")

				secondaryWorkRemovedActual := workRemovedActual(workNS, workNames[subIdx])
				Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")
			}

			for subIdx := range snapshotNames {
				snapshotRemovedActual := placementResourceSnapshotRemovedActual(appNamespaceName, snapshotNames[subIdx])
				Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement resource snapshot")
			}
		})
	})

	Context("update works", Ordered, func() {
		bindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		// Before the rollout: the primary snapshot (sub-index 0) and two secondary snapshots (sub-indices 1 and 2) of
		// index 0.
		oldSnapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 0, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 0, 2),
		}
		// After the first rollout: the primary snapshot (sub-index 0) and one secondary snapshot (sub-index 1) of
		// index 1.
		newSnapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 1, 1),
		}
		// After the second rollout: the primary snapshot (sub-index 0) and three secondary snapshots (sub-indices 1,
		// 2, and 3) of index 2.
		latestSnapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 2),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 2, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 2, 2),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 2, 3),
		}
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		// The work objects, ordered by the sub-index of the snapshots they are derived from; work objects derived
		// from snapshots of the same sub-index share the same name across snapshot indices.
		workNames := make([]string, len(latestSnapshotNames))
		oldSnapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(oldSnapshotNames))
		newSnapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(newSnapshotNames))
		latestSnapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(latestSnapshotNames))

		// The UIDs of the work objects that should be kept (updated rather than re-created) across the rollout.
		var primaryWorkUID, secondaryWorkUID types.UID
		var ownerRef *metav1.OwnerReference

		// The differently named NS, Deployment, and ConfigMap objects to place, one set per sub-index.
		nsNames := make([]string, len(latestSnapshotNames))
		deployNames := make([]string, len(latestSnapshotNames))
		configMapNames := make([]string, len(latestSnapshotNames))
		for subIdx := range latestSnapshotNames {
			nsNames[subIdx] = fmt.Sprintf(indexedNSNameTemplate, subIdx)
			deployNames[subIdx] = fmt.Sprintf(deployNameTemplate, subIdx)
			configMapNames[subIdx] = fmt.Sprintf(configMapNameTemplate, subIdx)
		}

		// Before the rollouts, each snapshot features its own set of NS, Deployment, and ConfigMap objects.
		oldManifestIdentifiersPerWork := make([][]placementv1alpha1.ManifestIdentifier, len(oldSnapshotNames))
		for subIdx := range oldSnapshotNames {
			oldManifestIdentifiersPerWork[subIdx] = []placementv1alpha1.ManifestIdentifier{
				nsManifestIdentifier(0, nsNames[subIdx]),
				deployManifestIdentifier(1, nsNames[subIdx], deployNames[subIdx]),
				configMapManifestIdentifier(2, nsNames[subIdx], configMapNames[subIdx]),
			}
		}

		// After the first rollout, the snapshot of sub-index 2 is gone; its Deployment and ConfigMap objects are moved to the
		// snapshots of sub-index 0 and 1 respectively, and its NS object is dropped.
		newManifestIdentifiersPerWork := [][]placementv1alpha1.ManifestIdentifier{
			{
				nsManifestIdentifier(0, nsNames[0]),
				deployManifestIdentifier(1, nsNames[0], deployNames[0]),
				configMapManifestIdentifier(2, nsNames[0], configMapNames[0]),
				deployManifestIdentifier(3, nsNames[2], deployNames[2]),
			},
			{
				nsManifestIdentifier(0, nsNames[1]),
				deployManifestIdentifier(1, nsNames[1], deployNames[1]),
				configMapManifestIdentifier(2, nsNames[1], configMapNames[1]),
				configMapManifestIdentifier(3, nsNames[2], configMapNames[2]),
			},
		}

		// After the second rollout, each snapshot again features its own set of NS, Deployment, and ConfigMap
		// objects.
		latestManifestIdentifiersPerWork := make([][]placementv1alpha1.ManifestIdentifier, len(latestSnapshotNames))
		for subIdx := range latestSnapshotNames {
			latestManifestIdentifiersPerWork[subIdx] = []placementv1alpha1.ManifestIdentifier{
				nsManifestIdentifier(0, nsNames[subIdx]),
				deployManifestIdentifier(1, nsNames[subIdx], deployNames[subIdx]),
				configMapManifestIdentifier(2, nsNames[subIdx], configMapNames[subIdx]),
			}
		}

		BeforeAll(func() {
			// Create the placement resource snapshots, with a different set of resources in each snapshot. The
			// secondary snapshots are created first, as the placement resource snapshot manager would do.
			for subIdx := len(oldSnapshotNames) - 1; subIdx >= 0; subIdx-- {
				oldSnapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(appNamespaceName, oldSnapshotNames[subIdx], placementPolicyName,
					0, subIdx, len(oldSnapshotNames),
					nsSnapshottedResource(nsNames[subIdx]),
					deploySnapshottedResource(nsNames[subIdx], deployNames[subIdx]),
					configMapSnapshottedResource(nsNames[subIdx], configMapNames[subIdx]),
				)
			}

			// Create a placement binding that binds the snapshots to a member cluster.
			binding := createPlacementBinding(appNamespaceName, bindingName, placementPolicyName, memberCluster1Name, oldSnapshotNames[0])
			for subIdx := range latestSnapshotNames {
				workNames[subIdx] = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, subIdx == 0,
					&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: strconv.Itoa(subIdx)})
			}
		})

		It("should add cleanup finalizer to the placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(appNamespaceName, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
		})

		It("should generate the work objects", func() {
			// Retrieve the primary work object; the secondary work objects should be owned by it.
			primaryWork := &placementv1alpha1.Work{}
			Eventually(func() error {
				return hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to retrieve the primary work object")
			primaryWorkUID = primaryWork.UID
			ownerRef = &metav1.OwnerReference{
				APIVersion:         placementv1alpha1.GroupVersion.String(),
				Kind:               workKind,
				Name:               primaryWork.Name,
				UID:                primaryWork.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}

			wantWorks := make([]placementv1alpha1.Work, len(oldSnapshotNames))
			for subIdx := range oldSnapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], appNamespaceName, bindingName, placementPolicyName,
					oldSnapshotNames[0], subIdx, len(oldSnapshotNames), ownerRef, oldSnapshots[subIdx])
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")

			// Record the UID of the secondary work object that should be kept across the rollout.
			secondaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[1]}, secondaryWork)).To(Succeed(), "Failed to retrieve the secondary work object")
			secondaryWorkUID = secondaryWork.UID
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(9, oldSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the work objects as applied and available", func() {
			for subIdx := range oldSnapshotNames {
				markWorkAsAppliedAndAvailable(workNS, workNames[subIdx], oldManifestIdentifiersPerWork[subIdx])
			}
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(9, oldSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can roll out a new set of placement resource snapshots", func() {
			// Create the new placement resource snapshots; the Deployment and ConfigMap objects from the old snapshot
			// of sub-index 2 are moved to the new snapshots of sub-index 0 and 1 respectively.
			newSnapshots[1] = createSubIndexedPlacementResourceSnapshot(appNamespaceName, newSnapshotNames[1], placementPolicyName,
				1, 1, len(newSnapshotNames),
				nsSnapshottedResource(nsNames[1]),
				deploySnapshottedResource(nsNames[1], deployNames[1]),
				configMapSnapshottedResource(nsNames[1], configMapNames[1]),
				configMapSnapshottedResource(nsNames[2], configMapNames[2]),
			)
			newSnapshots[0] = createSubIndexedPlacementResourceSnapshot(appNamespaceName, newSnapshotNames[0], placementPolicyName,
				1, 0, len(newSnapshotNames),
				nsSnapshottedResource(nsNames[0]),
				deploySnapshottedResource(nsNames[0], deployNames[0]),
				configMapSnapshottedResource(nsNames[0], configMapNames[0]),
				deploySnapshottedResource(nsNames[2], deployNames[2]),
			)

			// Point the placement binding to the new primary snapshot.
			updatePlacementBindingResourceSnapshot(appNamespaceName, bindingName, newSnapshotNames[0])
		})

		It("should update the work objects", func() {
			// The work object derived from the snapshot of sub-index 2 should be gone; the other two should be
			// updated in place, with the secondary one still owned by the (same) primary work object.
			wantWorks := make([]placementv1alpha1.Work, len(newSnapshotNames))
			for subIdx := range newSnapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], appNamespaceName, bindingName, placementPolicyName,
					newSnapshotNames[0], subIdx, len(newSnapshotNames), ownerRef, newSnapshots[subIdx])
			}

			worksUpdatedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")
		})

		It("should delete the work object that is no longer needed", func() {
			workRemovedActual := workRemovedActual(workNS, workNames[2])
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete the work object that is no longer needed")
		})

		It("should update the other work objects in place (not re-create them)", func() {
			primaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)).To(Succeed(), "Failed to retrieve the primary work object")
			Expect(primaryWork.UID).To(Equal(primaryWorkUID), "The primary work object has been re-created")

			secondaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[1]}, secondaryWork)).To(Succeed(), "Failed to retrieve the secondary work object")
			Expect(secondaryWork.UID).To(Equal(secondaryWorkUID), "The secondary work object has been re-created")
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(8, newSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the work objects as applied and available", func() {
			for subIdx := range newSnapshotNames {
				markWorkAsAppliedAndAvailable(workNS, workNames[subIdx], newManifestIdentifiersPerWork[subIdx])
			}
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(8, newSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can roll out another new set of placement resource snapshots", func() {
			// Create the latest placement resource snapshots, with a different set of resources in each snapshot. The
			// secondary snapshots are created first, as the placement resource snapshot manager would do.
			for subIdx := len(latestSnapshotNames) - 1; subIdx >= 0; subIdx-- {
				latestSnapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(appNamespaceName, latestSnapshotNames[subIdx], placementPolicyName,
					2, subIdx, len(latestSnapshotNames),
					nsSnapshottedResource(nsNames[subIdx]),
					deploySnapshottedResource(nsNames[subIdx], deployNames[subIdx]),
					configMapSnapshottedResource(nsNames[subIdx], configMapNames[subIdx]),
				)
			}

			// Point the placement binding to the latest primary snapshot.
			updatePlacementBindingResourceSnapshot(appNamespaceName, bindingName, latestSnapshotNames[0])
		})

		It("should update and create the work objects", func() {
			// The work objects derived from the snapshots of sub-index 0 and 1 should be updated; the ones derived from
			// the snapshots of sub-index 2 and 3 should be created, and owned by the (same) primary work object.
			wantWorks := make([]placementv1alpha1.Work, len(latestSnapshotNames))
			for subIdx := range latestSnapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], appNamespaceName, bindingName, placementPolicyName,
					latestSnapshotNames[0], subIdx, len(latestSnapshotNames), ownerRef, latestSnapshots[subIdx])
			}

			worksUpdatedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update and create the work objects")
		})

		It("should update the existing work objects in place (not re-create them)", func() {
			primaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)).To(Succeed(), "Failed to retrieve the primary work object")
			Expect(primaryWork.UID).To(Equal(primaryWorkUID), "The primary work object has been re-created")

			secondaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[1]}, secondaryWork)).To(Succeed(), "Failed to retrieve the secondary work object")
			Expect(secondaryWork.UID).To(Equal(secondaryWorkUID), "The secondary work object has been re-created")
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(12, latestSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the work objects as applied and available", func() {
			for subIdx := range latestSnapshotNames {
				markWorkAsAppliedAndAvailable(workNS, workNames[subIdx], latestManifestIdentifiersPerWork[subIdx])
			}
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(12, latestSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		AfterAll(func() {
			// Delete the placement binding; the work generator should clean up the primary work object and
			// then remove the cleanup finalizer.
			removePlacementBinding(appNamespaceName, bindingName)

			primaryWorkRemovedActual := workRemovedActual(workNS, workNames[0])
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			bindingRemovedActual := placementBindingRemovedActual(appNamespaceName, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")

			// The secondary work objects are owned by the primary work object and normally would be garbage collected
			// by Kubernetes; however, the environment prepared by the envtest package does not run the built-in
			// garbage collector, so the test suite deletes them manually (if they still exist).
			for subIdx := 1; subIdx < len(workNames); subIdx++ {
				secondaryWork := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: workNS,
						Name:      workNames[subIdx],
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, secondaryWork))).To(Succeed(), "Failed to delete the secondary work object")

				secondaryWorkRemovedActual := workRemovedActual(workNS, workNames[subIdx])
				Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")
			}

			allSnapshotNames := append(append(append([]string{}, oldSnapshotNames...), newSnapshotNames...), latestSnapshotNames...)
			for _, snapshotName := range allSnapshotNames {
				snapshotRemovedActual := placementResourceSnapshotRemovedActual(appNamespaceName, snapshotName)
				Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement resource snapshot")
			}
		})
	})

	Context("handle inconsistent resource snapshots", Ordered, func() {
		bindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		// Index 0: a single primary snapshot.
		snapshotNameIdx0 := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0)
		// Index 1: a primary snapshot that expects one secondary snapshot.
		snapshotNamesIdx1 := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 1, 1),
		}
		// Index 2: a primary snapshot that expects two secondary snapshots.
		snapshotNamesIdx2 := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 2),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 2, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 2, 2),
		}
		contentsHashIdx1 := "hash-1"
		mismatchedContentsHashIdx1 := "hash-1-mismatched"
		contentsHashIdx2 := "hash-2"
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		// The work objects, ordered by the sub-index of the snapshots they are derived from.
		workNames := make([]string, len(snapshotNamesIdx2))
		var snapshotIdx0 placementv1alpha1.PlacementResourceSnapshotAccessor
		snapshotsIdx2 := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(snapshotNamesIdx2))

		var primaryWorkUID types.UID
		// The expected state of the work objects and the placement binding status before any rollout
		// attempt; neither should change as long as the snapshots in use are inconsistent.
		var wantWorksBeforeRollouts []placementv1alpha1.Work
		var wantStatusBeforeRollouts *placementv1alpha1.PlacementBindingStatus

		// The differently named NS, Deployment, and ConfigMap objects to place, one set per sub-index.
		nsNames := make([]string, len(snapshotNamesIdx2))
		deployNames := make([]string, len(snapshotNamesIdx2))
		configMapNames := make([]string, len(snapshotNamesIdx2))
		manifestIdentifiersPerWork := make([][]placementv1alpha1.ManifestIdentifier, len(snapshotNamesIdx2))
		for subIdx := range snapshotNamesIdx2 {
			nsNames[subIdx] = fmt.Sprintf(indexedNSNameTemplate, subIdx)
			deployNames[subIdx] = fmt.Sprintf(deployNameTemplate, subIdx)
			configMapNames[subIdx] = fmt.Sprintf(configMapNameTemplate, subIdx)
			manifestIdentifiersPerWork[subIdx] = []placementv1alpha1.ManifestIdentifier{
				nsManifestIdentifier(0, nsNames[subIdx]),
				deployManifestIdentifier(1, nsNames[subIdx], deployNames[subIdx]),
				configMapManifestIdentifier(2, nsNames[subIdx], configMapNames[subIdx]),
			}
		}

		bindingAndWorksUnchangedActual := func() error {
			if err := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorksBeforeRollouts)(); err != nil {
				return err
			}
			return placementBindingStatusMatchesActual(appNamespaceName, bindingName, wantStatusBeforeRollouts)()
		}

		BeforeAll(func() {
			// Create a single placement resource snapshot of index 0.
			snapshotIdx0 = createPlacementResourceSnapshot(appNamespaceName, snapshotNameIdx0, placementPolicyName, 0,
				nsSnapshottedResource(nsNames[0]),
				deploySnapshottedResource(nsNames[0], deployNames[0]),
				configMapSnapshottedResource(nsNames[0], configMapNames[0]),
			)

			// Create a placement binding that binds the snapshot to a member cluster.
			binding := createPlacementBinding(appNamespaceName, bindingName, placementPolicyName, memberCluster1Name, snapshotNameIdx0)
			for subIdx := range snapshotNamesIdx2 {
				workNames[subIdx] = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, subIdx == 0,
					&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: strconv.Itoa(subIdx)})
			}
		})

		It("should add cleanup finalizer to the placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(appNamespaceName, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
		})

		It("should generate the work objects", func() {
			wantWorksBeforeRollouts = []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workNames[0], appNamespaceName, bindingName, placementPolicyName, snapshotIdx0),
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorksBeforeRollouts)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")

			// Record the UID of the primary work object, which should be kept across rollouts.
			primaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)).To(Succeed(), "Failed to retrieve the primary work object")
			primaryWorkUID = primaryWork.UID
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(3, snapshotNameIdx0)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workNames[0], manifestIdentifiersPerWork[0])
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(3, snapshotNameIdx0)

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")

			// Record the status (with the current observed generation), which should be kept as long as the
			// snapshots in use are inconsistent.
			binding, err := getPlacementBinding(appNamespaceName, bindingName)
			Expect(err).To(Succeed(), "Failed to retrieve the placement binding")
			wantStatusBeforeRollouts = wantStatus.DeepCopy()
			for idx := range wantStatusBeforeRollouts.Conditions {
				wantStatusBeforeRollouts.Conditions[idx].ObservedGeneration = binding.GetGeneration()
			}
		})

		It("can roll out a new primary snapshot (index 1) without its secondary snapshot", func() {
			// The new primary snapshot features new ConfigMap data, and expects one secondary snapshot, which is
			// absent for now.
			createSubIndexedPlacementResourceSnapshotWithHash(appNamespaceName, snapshotNamesIdx1[0], placementPolicyName,
				1, 0, len(snapshotNamesIdx1), contentsHashIdx1,
				nsSnapshottedResource(nsNames[0]),
				deploySnapshottedResource(nsNames[0], deployNames[0]),
				configMapSnapshottedResourceWithData(nsNames[0], configMapNames[0], map[string]string{configMapDataKey: altConfigMapDataValue1}),
			)

			// Point the placement binding to the new primary snapshot.
			updatePlacementBindingResourceSnapshot(appNamespaceName, bindingName, snapshotNamesIdx1[0])
		})

		It("should not update the placement binding status or the work objects", func() {
			Consistently(bindingAndWorksUnchangedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The placement binding status or the work objects have been updated unexpectedly")
		})

		It("can add a secondary snapshot (index 1) with a mismatched contents hash", func() {
			createSubIndexedPlacementResourceSnapshotWithHash(appNamespaceName, snapshotNamesIdx1[1], placementPolicyName,
				1, 1, len(snapshotNamesIdx1), mismatchedContentsHashIdx1,
				nsSnapshottedResource(nsNames[1]),
				deploySnapshottedResource(nsNames[1], deployNames[1]),
				configMapSnapshottedResource(nsNames[1], configMapNames[1]),
			)
		})

		It("should not update the placement binding status or the work objects", func() {
			Consistently(bindingAndWorksUnchangedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The placement binding status or the work objects have been updated unexpectedly")
		})

		It("can roll out a new primary snapshot (index 2) without its secondary snapshots", func() {
			// The new primary snapshot features new ConfigMap data, and expects two secondary snapshots, which are
			// absent for now.
			snapshotsIdx2[0] = createSubIndexedPlacementResourceSnapshotWithHash(appNamespaceName, snapshotNamesIdx2[0], placementPolicyName,
				2, 0, len(snapshotNamesIdx2), contentsHashIdx2,
				nsSnapshottedResource(nsNames[0]),
				deploySnapshottedResource(nsNames[0], deployNames[0]),
				configMapSnapshottedResourceWithData(nsNames[0], configMapNames[0], map[string]string{configMapDataKey: altConfigMapDataValue2}),
			)

			// Point the placement binding to the new primary snapshot.
			updatePlacementBindingResourceSnapshot(appNamespaceName, bindingName, snapshotNamesIdx2[0])
		})

		It("should not update the placement binding status or the work objects", func() {
			Consistently(bindingAndWorksUnchangedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The placement binding status or the work objects have been updated unexpectedly")
		})

		It("can add the missing secondary snapshots (index 2) with the expected contents hash", func() {
			for subIdx := len(snapshotNamesIdx2) - 1; subIdx >= 1; subIdx-- {
				snapshotsIdx2[subIdx] = createSubIndexedPlacementResourceSnapshotWithHash(appNamespaceName, snapshotNamesIdx2[subIdx], placementPolicyName,
					2, subIdx, len(snapshotNamesIdx2), contentsHashIdx2,
					nsSnapshottedResource(nsNames[subIdx]),
					deploySnapshottedResource(nsNames[subIdx], deployNames[subIdx]),
					configMapSnapshottedResource(nsNames[subIdx], configMapNames[subIdx]),
				)
			}
		})

		It("should update and create the work objects", func() {
			ownerRef := &metav1.OwnerReference{
				APIVersion:         placementv1alpha1.GroupVersion.String(),
				Kind:               workKind,
				Name:               workNames[0],
				UID:                primaryWorkUID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}
			wantWorks := make([]placementv1alpha1.Work, len(snapshotNamesIdx2))
			for subIdx := range snapshotNamesIdx2 {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], appNamespaceName, bindingName, placementPolicyName,
					snapshotNamesIdx2[0], subIdx, len(snapshotNamesIdx2), ownerRef, snapshotsIdx2[subIdx])
			}

			// The work generator does not watch placement resource snapshots; it picks up the newly added secondary
			// snapshots only when it retries the failed reconciliation, which is subject to exponential backoff.
			worksUpdatedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksUpdatedActual, eventuallyDurationForBackoffRetries, eventuallyInterval).Should(Succeed(), "Failed to update and create the work objects")
		})

		It("should update the primary work object in place (not re-create it)", func() {
			primaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)).To(Succeed(), "Failed to retrieve the primary work object")
			Expect(primaryWork.UID).To(Equal(primaryWorkUID), "The primary work object has been re-created")
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(9, snapshotNamesIdx2[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the work objects as applied and available", func() {
			for subIdx := range snapshotNamesIdx2 {
				markWorkAsAppliedAndAvailable(workNS, workNames[subIdx], manifestIdentifiersPerWork[subIdx])
			}
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(9, snapshotNamesIdx2[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		AfterAll(func() {
			// Delete the placement binding; the work generator should clean up the primary work object and
			// then remove the cleanup finalizer.
			removePlacementBinding(appNamespaceName, bindingName)

			primaryWorkRemovedActual := workRemovedActual(workNS, workNames[0])
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			bindingRemovedActual := placementBindingRemovedActual(appNamespaceName, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")

			// The secondary work objects are owned by the primary work object and normally would be garbage collected
			// by Kubernetes; however, the environment prepared by the envtest package does not run the built-in
			// garbage collector, so the test suite deletes them manually (if they still exist).
			for subIdx := 1; subIdx < len(workNames); subIdx++ {
				secondaryWork := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: workNS,
						Name:      workNames[subIdx],
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, secondaryWork))).To(Succeed(), "Failed to delete the secondary work object")

				secondaryWorkRemovedActual := workRemovedActual(workNS, workNames[subIdx])
				Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")
			}

			allSnapshotNames := append(append([]string{snapshotNameIdx0}, snapshotNamesIdx1...), snapshotNamesIdx2...)
			for _, snapshotName := range allSnapshotNames {
				snapshotRemovedActual := placementResourceSnapshotRemovedActual(appNamespaceName, snapshotName)
				Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement resource snapshot")
			}
		})
	})

	Context("update works (no spec changes)", Ordered, func() {
		bindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		// Before the rollout: the primary snapshot (sub-index 0) and one secondary snapshot (sub-index 1) of index 0.
		oldSnapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 0, 1),
		}
		// After the rollout: the primary snapshot (sub-index 0) and one secondary snapshot (sub-index 1) of index 1,
		// with exactly the same content as their counterparts of index 0.
		newSnapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 1, 1),
		}
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		// The work objects, ordered by the sub-index of the snapshots they are derived from.
		workNames := make([]string, len(oldSnapshotNames))
		workUIDs := make([]types.UID, len(oldSnapshotNames))
		oldSnapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(oldSnapshotNames))
		newSnapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(newSnapshotNames))
		resourcesPerSnapshot := make([][]placementv1alpha1.SnapshottedResource, len(oldSnapshotNames))
		var ownerRef *metav1.OwnerReference
		var oldBindingGeneration int64

		// Each snapshot features its own set of differently named NS, Deployment, and ConfigMap objects.
		nsNames := make([]string, len(oldSnapshotNames))
		deployNames := make([]string, len(oldSnapshotNames))
		configMapNames := make([]string, len(oldSnapshotNames))
		manifestIdentifiersPerWork := make([][]placementv1alpha1.ManifestIdentifier, len(oldSnapshotNames))
		for subIdx := range oldSnapshotNames {
			nsNames[subIdx] = fmt.Sprintf(indexedNSNameTemplate, subIdx)
			deployNames[subIdx] = fmt.Sprintf(deployNameTemplate, subIdx)
			configMapNames[subIdx] = fmt.Sprintf(configMapNameTemplate, subIdx)
			manifestIdentifiersPerWork[subIdx] = []placementv1alpha1.ManifestIdentifier{
				nsManifestIdentifier(0, nsNames[subIdx]),
				deployManifestIdentifier(1, nsNames[subIdx], deployNames[subIdx]),
				configMapManifestIdentifier(2, nsNames[subIdx], configMapNames[subIdx]),
			}
		}

		BeforeAll(func() {
			for subIdx := range oldSnapshotNames {
				resourcesPerSnapshot[subIdx] = []placementv1alpha1.SnapshottedResource{
					nsSnapshottedResource(nsNames[subIdx]),
					deploySnapshottedResource(nsNames[subIdx], deployNames[subIdx]),
					configMapSnapshottedResource(nsNames[subIdx], configMapNames[subIdx]),
				}
			}

			// Create the placement resource snapshots of index 0. The secondary snapshot is created first, as the
			// placement resource snapshot manager would do.
			for subIdx := len(oldSnapshotNames) - 1; subIdx >= 0; subIdx-- {
				oldSnapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(appNamespaceName, oldSnapshotNames[subIdx], placementPolicyName,
					0, subIdx, len(oldSnapshotNames), resourcesPerSnapshot[subIdx]...)
			}

			// Create a placement binding that binds the snapshots to a member cluster.
			binding := createPlacementBinding(appNamespaceName, bindingName, placementPolicyName, memberCluster1Name, oldSnapshotNames[0])
			for subIdx := range oldSnapshotNames {
				workNames[subIdx] = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, subIdx == 0,
					&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: strconv.Itoa(subIdx)})
			}
		})

		It("should add cleanup finalizer to the placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(appNamespaceName, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
		})

		It("should generate the work objects", func() {
			// Retrieve the primary work object; the secondary work object should be owned by it.
			primaryWork := &placementv1alpha1.Work{}
			Eventually(func() error {
				return hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to retrieve the primary work object")
			ownerRef = &metav1.OwnerReference{
				APIVersion:         placementv1alpha1.GroupVersion.String(),
				Kind:               workKind,
				Name:               primaryWork.Name,
				UID:                primaryWork.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}

			wantWorks := make([]placementv1alpha1.Work, len(oldSnapshotNames))
			for subIdx := range oldSnapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], appNamespaceName, bindingName, placementPolicyName,
					oldSnapshotNames[0], subIdx, len(oldSnapshotNames), ownerRef, oldSnapshots[subIdx])
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")

			// Record the UIDs of the work objects, which should be kept across the rollout.
			for subIdx := range oldSnapshotNames {
				work := &placementv1alpha1.Work{}
				Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[subIdx]}, work)).To(Succeed(), "Failed to retrieve the work object")
				workUIDs[subIdx] = work.UID
			}
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(6, oldSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark the work objects as applied and available", func() {
			for subIdx := range oldSnapshotNames {
				markWorkAsAppliedAndAvailable(workNS, workNames[subIdx], manifestIdentifiersPerWork[subIdx])
			}
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(6, oldSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can roll out a new set of placement resource snapshots with the same content", func() {
			// Record the current generation of the placement binding.
			binding, err := getPlacementBinding(appNamespaceName, bindingName)
			Expect(err).To(Succeed(), "Failed to retrieve the placement binding")
			oldBindingGeneration = binding.GetGeneration()

			// Create the new placement resource snapshots with exactly the same resources as their counterparts.
			for subIdx := len(newSnapshotNames) - 1; subIdx >= 0; subIdx-- {
				newSnapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(appNamespaceName, newSnapshotNames[subIdx], placementPolicyName,
					1, subIdx, len(newSnapshotNames), resourcesPerSnapshot[subIdx]...)
			}

			// Point the placement binding to the new primary snapshot.
			updatePlacementBindingResourceSnapshot(appNamespaceName, bindingName, newSnapshotNames[0])
		})

		It("should update the work objects", func() {
			// The manifests stay the same; only the link to the primary placement resource snapshot changes.
			wantWorks := make([]placementv1alpha1.Work, len(newSnapshotNames))
			for subIdx := range newSnapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], appNamespaceName, bindingName, placementPolicyName,
					newSnapshotNames[0], subIdx, len(newSnapshotNames), ownerRef, newSnapshots[subIdx])
			}

			worksUpdatedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")
		})

		It("should update the work objects in place (not re-create them)", func() {
			for subIdx := range newSnapshotNames {
				work := &placementv1alpha1.Work{}
				Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[subIdx]}, work)).To(Succeed(), "Failed to retrieve the work object")
				Expect(work.UID).To(Equal(workUIDs[subIdx]), "The work object has been re-created")
			}
		})

		It("should refresh the placement binding status without waiting for the work objects to be re-processed", func() {
			// As the work objects have no spec changes, their existing status still applies; the placement binding
			// should be reported as synchronized and available, with the conditions observing the new generation.
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(6, newSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")

			binding, err := getPlacementBinding(appNamespaceName, bindingName)
			Expect(err).To(Succeed(), "Failed to retrieve the placement binding")
			Expect(binding.GetGeneration()).To(BeNumerically(">", oldBindingGeneration), "The placement binding generation has not been bumped")
		})

		AfterAll(func() {
			// Delete the placement binding; the work generator should clean up the primary work object and
			// then remove the cleanup finalizer.
			removePlacementBinding(appNamespaceName, bindingName)

			primaryWorkRemovedActual := workRemovedActual(workNS, workNames[0])
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			bindingRemovedActual := placementBindingRemovedActual(appNamespaceName, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")

			// The secondary work object is owned by the primary work object and normally would be garbage collected
			// by Kubernetes; however, the environment prepared by the envtest package does not run the built-in
			// garbage collector, so the test suite deletes it manually.
			for subIdx := 1; subIdx < len(workNames); subIdx++ {
				secondaryWork := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: workNS,
						Name:      workNames[subIdx],
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, secondaryWork))).To(Succeed(), "Failed to delete the secondary work object")

				secondaryWorkRemovedActual := workRemovedActual(workNS, workNames[subIdx])
				Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")
			}

			allSnapshotNames := append(append([]string{}, oldSnapshotNames...), newSnapshotNames...)
			for _, snapshotName := range allSnapshotNames {
				snapshotRemovedActual := placementResourceSnapshotRemovedActual(appNamespaceName, snapshotName)
				Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement resource snapshot")
			}
		})
	})

	Context("backport status for failed manifests", Ordered, func() {
		bindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		// The primary snapshot (sub-index 0) and one secondary snapshot (sub-index 1) of index 0.
		snapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 0, 1),
		}
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		// The work objects (and the snapshots they are derived from), ordered by snapshot sub-index.
		workNames := make([]string, len(snapshotNames))
		snapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(snapshotNames))

		// Each snapshot features its own set of differently named NS, Deployment, and ConfigMap objects.
		nsNames := make([]string, len(snapshotNames))
		deployNames := make([]string, len(snapshotNames))
		configMapNames := make([]string, len(snapshotNames))
		manifestIdentifiersPerWork := make([][]placementv1alpha1.ManifestIdentifier, len(snapshotNames))
		for subIdx := range snapshotNames {
			nsNames[subIdx] = fmt.Sprintf(indexedNSNameTemplate, subIdx)
			deployNames[subIdx] = fmt.Sprintf(deployNameTemplate, subIdx)
			configMapNames[subIdx] = fmt.Sprintf(configMapNameTemplate, subIdx)
			manifestIdentifiersPerWork[subIdx] = []placementv1alpha1.ManifestIdentifier{
				nsManifestIdentifier(0, nsNames[subIdx]),
				deployManifestIdentifier(1, nsNames[subIdx], deployNames[subIdx]),
				configMapManifestIdentifier(2, nsNames[subIdx], configMapNames[subIdx]),
			}
		}

		// The diff details reported for the ConfigMap object in the primary work object, as if the member agent has
		// failed to take over an existing ConfigMap object with different data in the member cluster.
		//
		// Note that the timestamps are truncated to the second, as this is the precision kept by the API server.
		configMapDiffDetails := &placementv1alpha1.DiffDetails{
			ObservedInMemberClusterGeneration: ptr.To(int64(0)),
			FirstDiffedObservedTimestamp:      metav1.NewTime(time.Now().Truncate(time.Second)),
			ObservedDiffs: []placementv1alpha1.PatchDetail{
				{
					Path:          "/data/" + configMapDataKey,
					ValueInMember: altConfigMapDataValue1,
					ValueInHub:    configMapDataValue,
				},
			},
		}
		// The diff details reported for the Deployment object in the secondary work object, as if the member agent
		// has failed to take over an existing Deployment object with a different replica count in the member cluster.
		deployDiffDetails := &placementv1alpha1.DiffDetails{
			ObservedInMemberClusterGeneration: ptr.To(int64(2)),
			FirstDiffedObservedTimestamp:      metav1.NewTime(time.Now().Truncate(time.Second)),
			ObservedDiffs: []placementv1alpha1.PatchDetail{
				{
					Path:          "/spec/replicas",
					ValueInMember: "2",
					ValueInHub:    "1",
				},
			},
		}

		BeforeAll(func() {
			// Create the placement resource snapshots, with a different set of resources in each snapshot. The
			// secondary snapshot is created first, as the placement resource snapshot manager would do.
			for subIdx := len(snapshotNames) - 1; subIdx >= 0; subIdx-- {
				snapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(appNamespaceName, snapshotNames[subIdx], placementPolicyName,
					0, subIdx, len(snapshotNames),
					nsSnapshottedResource(nsNames[subIdx]),
					deploySnapshottedResource(nsNames[subIdx], deployNames[subIdx]),
					configMapSnapshottedResource(nsNames[subIdx], configMapNames[subIdx]),
				)
			}

			// Create a placement binding that binds the snapshots to a member cluster.
			binding := createPlacementBinding(appNamespaceName, bindingName, placementPolicyName, memberCluster1Name, snapshotNames[0])
			for subIdx := range snapshotNames {
				workNames[subIdx] = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, subIdx == 0,
					&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: strconv.Itoa(subIdx)})
			}
		})

		It("should add cleanup finalizer to the placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(appNamespaceName, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
		})

		It("should generate the work objects", func() {
			// Retrieve the primary work object; the secondary work object should be owned by it.
			primaryWork := &placementv1alpha1.Work{}
			Eventually(func() error {
				return hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to retrieve the primary work object")
			ownerRef := &metav1.OwnerReference{
				APIVersion:         placementv1alpha1.GroupVersion.String(),
				Kind:               workKind,
				Name:               primaryWork.Name,
				UID:                primaryWork.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}

			wantWorks := make([]placementv1alpha1.Work, len(snapshotNames))
			for subIdx := range snapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], appNamespaceName, bindingName, placementPolicyName,
					snapshotNames[0], subIdx, len(snapshotNames), ownerRef, snapshots[subIdx])
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, appNamespaceName, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(6, snapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		It("can mark some manifests in the primary work object as failed", func() {
			// The Deployment object fails to become available; the ConfigMap object fails to be applied (with diff
			// details).
			markWorkManifestsAsFailed(workNS, workNames[0], manifestIdentifiersPerWork[0],
				map[int]*placementv1alpha1.DiffDetails{2: configMapDiffDetails},
				sets.New(1),
			)
		})

		It("can mark some manifests in the secondary work object as failed", func() {
			// The Deployment object fails to be applied (with diff details); the ConfigMap object fails to become
			// available.
			markWorkManifestsAsFailed(workNS, workNames[1], manifestIdentifiersPerWork[1],
				map[int]*placementv1alpha1.DiffDetails{1: deployDiffDetails},
				sets.New(2),
			)
		})

		It("should refresh the placement binding status with the failure info from all work objects", func() {
			wantStatus := &placementv1alpha1.PlacementBindingStatus{
				Conditions: []metav1.Condition{
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeSynchronized,
						Status: metav1.ConditionFalse,
						Reason: placementv1alpha1.PlacementBindingSynchronizedCondReasonFailedToSynchronizeSomeResources,
					},
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeAvailable,
						Status: metav1.ConditionFalse,
						Reason: placementv1alpha1.PlacementBindingAvailableCondReasonSomeResourcesUnavailable,
					},
				},
				// In each work object, the NS object is applied and available; the Deployment and ConfigMap objects
				// have failed in different ways.
				SelectedResources:     ptr.To(int32(6)),
				SynchronizedResources: ptr.To(int32(4)),
				AvailableResources:    ptr.To(int32(2)),
				FailedResources: []placementv1alpha1.FailedResource{
					{
						ObjectRef: objectRefFromManifestIdentifier(manifestIdentifiersPerWork[0][1]),
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeAvailable,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsUnavailableReason,
							},
						},
					},
					{
						ObjectRef: objectRefFromManifestIdentifier(manifestIdentifiersPerWork[0][2]),
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeApplied,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsFailedToApplyReason,
							},
						},
						DiffDetails: configMapDiffDetails,
					},
					{
						ObjectRef: objectRefFromManifestIdentifier(manifestIdentifiersPerWork[1][1]),
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeApplied,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsFailedToApplyReason,
							},
						},
						DiffDetails: deployDiffDetails,
					},
					{
						ObjectRef: objectRefFromManifestIdentifier(manifestIdentifiersPerWork[1][2]),
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeAvailable,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsUnavailableReason,
							},
						},
					},
				},
				LastProcessedResourceSnapshotName: ptr.To(snapshotNames[0]),
			}

			statusUpdatedActual := placementBindingStatusUpdatedActual(appNamespaceName, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		})

		AfterAll(func() {
			// Delete the placement binding; the work generator should clean up the primary work object and
			// then remove the cleanup finalizer.
			removePlacementBinding(appNamespaceName, bindingName)

			primaryWorkRemovedActual := workRemovedActual(workNS, workNames[0])
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			bindingRemovedActual := placementBindingRemovedActual(appNamespaceName, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")

			// The secondary work object is owned by the primary work object and normally would be garbage collected
			// by Kubernetes; however, the environment prepared by the envtest package does not run the built-in
			// garbage collector, so the test suite deletes it manually.
			for subIdx := 1; subIdx < len(workNames); subIdx++ {
				secondaryWork := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: workNS,
						Name:      workNames[subIdx],
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, secondaryWork))).To(Succeed(), "Failed to delete the secondary work object")

				secondaryWorkRemovedActual := workRemovedActual(workNS, workNames[subIdx])
				Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")
			}

			for _, snapshotName := range snapshotNames {
				snapshotRemovedActual := placementResourceSnapshotRemovedActual(appNamespaceName, snapshotName)
				Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement resource snapshot")
			}
		})
	})
})

var _ = Describe("reconciling cluster placement bindings (single placement resource snapshot)", func() {
	Context("generate works", Ordered, func() {
		bindingName := fmt.Sprintf(clusterPlacementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(clusterPlacementPolicyNameTemplate, utils.RandStr())
		snapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0)
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		var workName string
		var snapshot placementv1alpha1.PlacementResourceSnapshotAccessor

		manifestIdentifiers := []placementv1alpha1.ManifestIdentifier{
			{
				Ordinal:    0,
				Name:       nsName,
				APIVersion: "v1",
				Kind:       nsKind,
				Resource:   nsResource,
			},
			{
				Ordinal:    1,
				Namespace:  nsName,
				Name:       deployName,
				APIGroup:   appsAPIGroup,
				APIVersion: "v1",
				Kind:       deployKind,
				Resource:   deployResource,
			},
		}

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Deployment object.
			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a cluster placement resource snapshot with the NS and the Deployment objects.
			snapshot = createPlacementResourceSnapshot(clusterScopedNS, snapshotName, placementPolicyName, 0,
				placementv1alpha1.SnapshottedResource{
					Identifier: placementv1alpha1.ObjectReference{
						Name:       nsName,
						APIVersion: "v1",
						Kind:       nsKind,
					},
					Manifest: runtime.RawExtension{Raw: regularNSJSON},
				},
				placementv1alpha1.SnapshottedResource{
					Identifier: placementv1alpha1.ObjectReference{
						Namespace:  nsName,
						Name:       deployName,
						APIGroup:   appsAPIGroup,
						APIVersion: "v1",
						Kind:       deployKind,
					},
					Manifest: runtime.RawExtension{Raw: regularDeployJSON},
				},
			)

			// Create a cluster placement binding that binds the snapshot to a member cluster.
			binding := createPlacementBinding(clusterScopedNS, bindingName, placementPolicyName, memberCluster1Name, snapshotName)
			workName = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, true,
				&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: "0"})
		})

		It("should add cleanup finalizer to the cluster placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(clusterScopedNS, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the cluster placement binding")
		})

		It("should generate the work objects", func() {
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, clusterScopedNS, bindingName, placementPolicyName, snapshot),
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(2, snapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workName, manifestIdentifiers)
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(2, snapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		AfterAll(func() {
			// Delete the cluster placement binding; the work generator should clean up the work objects and then
			// remove the cleanup finalizer.
			removePlacementBinding(clusterScopedNS, bindingName)

			workRemovedActual := workRemovedActual(workNS, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			bindingRemovedActual := placementBindingRemovedActual(clusterScopedNS, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement binding")

			snapshotRemovedActual := placementResourceSnapshotRemovedActual(clusterScopedNS, snapshotName)
			Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement resource snapshot")
		})
	})

	Context("update works", Ordered, func() {
		bindingName := fmt.Sprintf(clusterPlacementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(clusterPlacementPolicyNameTemplate, utils.RandStr())
		oldSnapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0)
		newSnapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 1)
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		var workName string
		var oldSnapshot, newSnapshot placementv1alpha1.PlacementResourceSnapshotAccessor

		nsIdentifier := placementv1alpha1.ObjectReference{
			Name:       nsName,
			APIVersion: "v1",
			Kind:       nsKind,
		}
		deployIdentifier := placementv1alpha1.ObjectReference{
			Namespace:  nsName,
			Name:       deployName,
			APIGroup:   appsAPIGroup,
			APIVersion: "v1",
			Kind:       deployKind,
		}
		configMapIdentifier := placementv1alpha1.ObjectReference{
			Namespace:  nsName,
			Name:       configMapName,
			APIVersion: "v1",
			Kind:       configMapKind,
		}

		oldManifestIdentifiers := []placementv1alpha1.ManifestIdentifier{
			{
				Ordinal:    0,
				Name:       nsName,
				APIVersion: "v1",
				Kind:       nsKind,
				Resource:   nsResource,
			},
			{
				Ordinal:    1,
				Namespace:  nsName,
				Name:       deployName,
				APIGroup:   appsAPIGroup,
				APIVersion: "v1",
				Kind:       deployKind,
				Resource:   deployResource,
			},
		}
		newManifestIdentifiers := []placementv1alpha1.ManifestIdentifier{
			oldManifestIdentifiers[0],
			{
				Ordinal:    1,
				Namespace:  nsName,
				Name:       configMapName,
				APIVersion: "v1",
				Kind:       configMapKind,
				Resource:   configMapResource,
			},
		}

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Deployment object.
			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a cluster placement resource snapshot with the NS and the Deployment objects.
			oldSnapshot = createPlacementResourceSnapshot(clusterScopedNS, oldSnapshotName, placementPolicyName, 0,
				placementv1alpha1.SnapshottedResource{
					Identifier: nsIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularNSJSON},
				},
				placementv1alpha1.SnapshottedResource{
					Identifier: deployIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularDeployJSON},
				},
			)

			// Create a cluster placement binding that binds the snapshot to a member cluster.
			binding := createPlacementBinding(clusterScopedNS, bindingName, placementPolicyName, memberCluster1Name, oldSnapshotName)
			workName = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, true,
				&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: "0"})
		})

		It("should add cleanup finalizer to the cluster placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(clusterScopedNS, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the cluster placement binding")
		})

		It("should generate the work objects", func() {
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, clusterScopedNS, bindingName, placementPolicyName, oldSnapshot),
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(2, oldSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workName, oldManifestIdentifiers)
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(2, oldSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can roll out a new cluster placement resource snapshot", func() {
			// Prepare a NS object (unchanged).
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a ConfigMap object (new); the Deployment object is dropped.
			regularConfigMap := configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Create a new cluster placement resource snapshot with the NS and the new ConfigMap objects.
			newSnapshot = createPlacementResourceSnapshot(clusterScopedNS, newSnapshotName, placementPolicyName, 1,
				placementv1alpha1.SnapshottedResource{
					Identifier: nsIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularNSJSON},
				},
				placementv1alpha1.SnapshottedResource{
					Identifier: configMapIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularConfigMapJSON},
				},
			)

			// Point the cluster placement binding to the new snapshot.
			updatePlacementBindingResourceSnapshot(clusterScopedNS, bindingName, newSnapshotName)
		})

		It("should update the work objects", func() {
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, clusterScopedNS, bindingName, placementPolicyName, newSnapshot),
			}

			worksUpdatedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(2, newSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workName, newManifestIdentifiers)
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(2, newSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		AfterAll(func() {
			// Delete the cluster placement binding; the work generator should clean up the work objects and then
			// remove the cleanup finalizer.
			removePlacementBinding(clusterScopedNS, bindingName)

			workRemovedActual := workRemovedActual(workNS, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			bindingRemovedActual := placementBindingRemovedActual(clusterScopedNS, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement binding")

			oldSnapshotRemovedActual := placementResourceSnapshotRemovedActual(clusterScopedNS, oldSnapshotName)
			Eventually(oldSnapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the old cluster placement resource snapshot")

			newSnapshotRemovedActual := placementResourceSnapshotRemovedActual(clusterScopedNS, newSnapshotName)
			Eventually(newSnapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the new cluster placement resource snapshot")
		})
	})

	Context("refresh binding status upon work status changes", Ordered, func() {
		bindingName := fmt.Sprintf(clusterPlacementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(clusterPlacementPolicyNameTemplate, utils.RandStr())
		snapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0)
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		var workName string
		var snapshot placementv1alpha1.PlacementResourceSnapshotAccessor

		deployIdentifier := placementv1alpha1.ObjectReference{
			Namespace:  nsName,
			Name:       deployName,
			APIGroup:   appsAPIGroup,
			APIVersion: "v1",
			Kind:       deployKind,
		}

		manifestIdentifiers := []placementv1alpha1.ManifestIdentifier{
			{
				Ordinal:    0,
				Name:       nsName,
				APIVersion: "v1",
				Kind:       nsKind,
				Resource:   nsResource,
			},
			{
				Ordinal:    1,
				Namespace:  nsName,
				Name:       deployName,
				APIGroup:   appsAPIGroup,
				APIVersion: "v1",
				Kind:       deployKind,
				Resource:   deployResource,
			},
		}

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Deployment object.
			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a cluster placement resource snapshot with the NS and the Deployment objects.
			snapshot = createPlacementResourceSnapshot(clusterScopedNS, snapshotName, placementPolicyName, 0,
				placementv1alpha1.SnapshottedResource{
					Identifier: placementv1alpha1.ObjectReference{
						Name:       nsName,
						APIVersion: "v1",
						Kind:       nsKind,
					},
					Manifest: runtime.RawExtension{Raw: regularNSJSON},
				},
				placementv1alpha1.SnapshottedResource{
					Identifier: deployIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularDeployJSON},
				},
			)

			// Create a cluster placement binding that binds the snapshot to a member cluster.
			binding := createPlacementBinding(clusterScopedNS, bindingName, placementPolicyName, memberCluster1Name, snapshotName)
			workName = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, true,
				&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: "0"})
		})

		It("should add cleanup finalizer to the cluster placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(clusterScopedNS, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the cluster placement binding")
		})

		It("should generate the work objects", func() {
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, clusterScopedNS, bindingName, placementPolicyName, snapshot),
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(2, snapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workName, manifestIdentifiers)
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(2, snapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the deployment (and consequently the work object) as unavailable", func() {
			markWorkManifestAsUnavailable(workNS, workName, 1)
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := &placementv1alpha1.PlacementBindingStatus{
				Conditions: []metav1.Condition{
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeSynchronized,
						Status: metav1.ConditionTrue,
						Reason: placementv1alpha1.PlacementBindingSynchronizedCondReasonAllResourcesSynchronized,
					},
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeAvailable,
						Status: metav1.ConditionFalse,
						Reason: placementv1alpha1.PlacementBindingAvailableCondReasonSomeResourcesUnavailable,
					},
				},
				SelectedResources:     ptr.To(int32(2)),
				SynchronizedResources: ptr.To(int32(2)),
				AvailableResources:    ptr.To(int32(1)),
				FailedResources: []placementv1alpha1.FailedResource{
					{
						ObjectRef: deployIdentifier,
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeAvailable,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsUnavailableReason,
							},
						},
					},
				},
				LastProcessedResourceSnapshotName: ptr.To(snapshotName),
			}

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		AfterAll(func() {
			// Delete the cluster placement binding; the work generator should clean up the work objects and then
			// remove the cleanup finalizer.
			removePlacementBinding(clusterScopedNS, bindingName)

			workRemovedActual := workRemovedActual(workNS, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			bindingRemovedActual := placementBindingRemovedActual(clusterScopedNS, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement binding")

			snapshotRemovedActual := placementResourceSnapshotRemovedActual(clusterScopedNS, snapshotName)
			Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement resource snapshot")
		})
	})

	Context("update works (no spec changes)", Ordered, func() {
		bindingName := fmt.Sprintf(clusterPlacementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(clusterPlacementPolicyNameTemplate, utils.RandStr())
		oldSnapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0)
		newSnapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 1)
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		var workName string
		var oldSnapshot, newSnapshot placementv1alpha1.PlacementResourceSnapshotAccessor
		var resources []placementv1alpha1.SnapshottedResource
		var oldBindingGeneration int64

		manifestIdentifiers := []placementv1alpha1.ManifestIdentifier{
			{
				Ordinal:    0,
				Name:       nsName,
				APIVersion: "v1",
				Kind:       nsKind,
				Resource:   nsResource,
			},
			{
				Ordinal:    1,
				Namespace:  nsName,
				Name:       deployName,
				APIGroup:   appsAPIGroup,
				APIVersion: "v1",
				Kind:       deployKind,
				Resource:   deployResource,
			},
		}

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Deployment object.
			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a cluster placement resource snapshot with the NS and the Deployment objects.
			resources = []placementv1alpha1.SnapshottedResource{
				{
					Identifier: placementv1alpha1.ObjectReference{
						Name:       nsName,
						APIVersion: "v1",
						Kind:       nsKind,
					},
					Manifest: runtime.RawExtension{Raw: regularNSJSON},
				},
				{
					Identifier: placementv1alpha1.ObjectReference{
						Namespace:  nsName,
						Name:       deployName,
						APIGroup:   appsAPIGroup,
						APIVersion: "v1",
						Kind:       deployKind,
					},
					Manifest: runtime.RawExtension{Raw: regularDeployJSON},
				},
			}
			oldSnapshot = createPlacementResourceSnapshot(clusterScopedNS, oldSnapshotName, placementPolicyName, 0, resources...)

			// Create a cluster placement binding that binds the snapshot to a member cluster.
			binding := createPlacementBinding(clusterScopedNS, bindingName, placementPolicyName, memberCluster1Name, oldSnapshotName)
			workName = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, true,
				&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: "0"})
		})

		It("should add cleanup finalizer to the cluster placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(clusterScopedNS, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the cluster placement binding")
		})

		It("should generate the work objects", func() {
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, clusterScopedNS, bindingName, placementPolicyName, oldSnapshot),
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(2, oldSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workName, manifestIdentifiers)
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(2, oldSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can roll out a new cluster placement resource snapshot with the same content", func() {
			// Record the current generation of the cluster placement binding.
			binding, err := getPlacementBinding(clusterScopedNS, bindingName)
			Expect(err).To(Succeed(), "Failed to retrieve the cluster placement binding")
			oldBindingGeneration = binding.GetGeneration()

			// Create a new cluster placement resource snapshot with exactly the same resources.
			newSnapshot = createPlacementResourceSnapshot(clusterScopedNS, newSnapshotName, placementPolicyName, 1, resources...)

			// Point the cluster placement binding to the new snapshot.
			updatePlacementBindingResourceSnapshot(clusterScopedNS, bindingName, newSnapshotName)
		})

		It("should update the work objects", func() {
			// The manifests stay the same; only the link to the primary cluster placement resource snapshot changes.
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, clusterScopedNS, bindingName, placementPolicyName, newSnapshot),
			}

			worksUpdatedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")
		})

		It("should refresh the cluster placement binding status without waiting for the work objects to be re-processed", func() {
			// As the work objects have no spec changes, their existing status still applies; the cluster placement binding
			// should be reported as synchronized and available, with the conditions observing the new generation.
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(2, newSnapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")

			binding, err := getPlacementBinding(clusterScopedNS, bindingName)
			Expect(err).To(Succeed(), "Failed to retrieve the cluster placement binding")
			Expect(binding.GetGeneration()).To(BeNumerically(">", oldBindingGeneration), "The cluster placement binding generation has not been bumped")
		})

		AfterAll(func() {
			// Delete the cluster placement binding; the work generator should clean up the work objects and then
			// remove the cleanup finalizer.
			removePlacementBinding(clusterScopedNS, bindingName)

			workRemovedActual := workRemovedActual(workNS, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			bindingRemovedActual := placementBindingRemovedActual(clusterScopedNS, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement binding")

			oldSnapshotRemovedActual := placementResourceSnapshotRemovedActual(clusterScopedNS, oldSnapshotName)
			Eventually(oldSnapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the old cluster placement resource snapshot")

			newSnapshotRemovedActual := placementResourceSnapshotRemovedActual(clusterScopedNS, newSnapshotName)
			Eventually(newSnapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the new cluster placement resource snapshot")
		})
	})

	Context("backport status for failed manifests", Ordered, func() {
		bindingName := fmt.Sprintf(clusterPlacementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(clusterPlacementPolicyNameTemplate, utils.RandStr())
		snapshotName := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0)
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		var workName string
		var snapshot placementv1alpha1.PlacementResourceSnapshotAccessor

		deployIdentifier := placementv1alpha1.ObjectReference{
			Namespace:  nsName,
			Name:       deployName,
			APIGroup:   appsAPIGroup,
			APIVersion: "v1",
			Kind:       deployKind,
		}
		configMapIdentifier := placementv1alpha1.ObjectReference{
			Namespace:  nsName,
			Name:       configMapName,
			APIVersion: "v1",
			Kind:       configMapKind,
		}

		manifestIdentifiers := []placementv1alpha1.ManifestIdentifier{
			{
				Ordinal:    0,
				Name:       nsName,
				APIVersion: "v1",
				Kind:       nsKind,
				Resource:   nsResource,
			},
			{
				Ordinal:    1,
				Namespace:  nsName,
				Name:       deployName,
				APIGroup:   appsAPIGroup,
				APIVersion: "v1",
				Kind:       deployKind,
				Resource:   deployResource,
			},
			{
				Ordinal:    2,
				Namespace:  nsName,
				Name:       configMapName,
				APIVersion: "v1",
				Kind:       configMapKind,
				Resource:   configMapResource,
			},
		}

		// The diff details reported for the ConfigMap object, as if the member agent has failed to take over an
		// existing ConfigMap object with different data in the member cluster.
		//
		// Note that the timestamp is truncated to the second, as this is the precision kept by the API server.
		diffDetails := &placementv1alpha1.DiffDetails{
			ObservedInMemberClusterGeneration: ptr.To(int64(0)),
			FirstDiffedObservedTimestamp:      metav1.NewTime(time.Now().Truncate(time.Second)),
			ObservedDiffs: []placementv1alpha1.PatchDetail{
				{
					Path:          "/data/" + configMapDataKey,
					ValueInMember: altConfigMapDataValue1,
					ValueInHub:    configMapDataValue,
				},
			},
		}

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Deployment object.
			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Prepare a ConfigMap object.
			regularConfigMap := configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			// Create a cluster placement resource snapshot with the NS, the Deployment, and the ConfigMap objects.
			snapshot = createPlacementResourceSnapshot(clusterScopedNS, snapshotName, placementPolicyName, 0,
				placementv1alpha1.SnapshottedResource{
					Identifier: placementv1alpha1.ObjectReference{
						Name:       nsName,
						APIVersion: "v1",
						Kind:       nsKind,
					},
					Manifest: runtime.RawExtension{Raw: regularNSJSON},
				},
				placementv1alpha1.SnapshottedResource{
					Identifier: deployIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularDeployJSON},
				},
				placementv1alpha1.SnapshottedResource{
					Identifier: configMapIdentifier,
					Manifest:   runtime.RawExtension{Raw: regularConfigMapJSON},
				},
			)

			// Create a cluster placement binding that binds the snapshot to a member cluster.
			binding := createPlacementBinding(clusterScopedNS, bindingName, placementPolicyName, memberCluster1Name, snapshotName)
			workName = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, true,
				&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: "0"})
		})

		It("should add cleanup finalizer to the cluster placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(clusterScopedNS, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the cluster placement binding")
		})

		It("should generate the work objects", func() {
			wantWorks := []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workName, clusterScopedNS, bindingName, placementPolicyName, snapshot),
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(3, snapshotName)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the deployment as unavailable, and the config map as failed to be applied (with diff details)", func() {
			markWorkManifestsAsFailed(workNS, workName, manifestIdentifiers,
				map[int]*placementv1alpha1.DiffDetails{2: diffDetails},
				sets.New(1),
			)
		})

		It("should refresh the cluster placement binding status with the failure info", func() {
			wantStatus := &placementv1alpha1.PlacementBindingStatus{
				Conditions: []metav1.Condition{
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeSynchronized,
						Status: metav1.ConditionFalse,
						Reason: placementv1alpha1.PlacementBindingSynchronizedCondReasonFailedToSynchronizeSomeResources,
					},
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeAvailable,
						Status: metav1.ConditionFalse,
						Reason: placementv1alpha1.PlacementBindingAvailableCondReasonSomeResourcesUnavailable,
					},
				},
				SelectedResources:     ptr.To(int32(3)),
				SynchronizedResources: ptr.To(int32(2)),
				AvailableResources:    ptr.To(int32(1)),
				FailedResources: []placementv1alpha1.FailedResource{
					{
						ObjectRef: deployIdentifier,
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeAvailable,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsUnavailableReason,
							},
						},
					},
					{
						ObjectRef: configMapIdentifier,
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeApplied,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsFailedToApplyReason,
							},
						},
						DiffDetails: diffDetails,
					},
				},
				LastProcessedResourceSnapshotName: ptr.To(snapshotName),
			}

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		AfterAll(func() {
			// Delete the cluster placement binding; the work generator should clean up the work objects and then
			// remove the cleanup finalizer.
			removePlacementBinding(clusterScopedNS, bindingName)

			workRemovedActual := workRemovedActual(workNS, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			bindingRemovedActual := placementBindingRemovedActual(clusterScopedNS, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement binding")

			snapshotRemovedActual := placementResourceSnapshotRemovedActual(clusterScopedNS, snapshotName)
			Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement resource snapshot")
		})
	})
})

var _ = Describe("reconciling cluster placement bindings (multiple placement resource snapshots)", func() {
	Context("generate works", Ordered, func() {
		bindingName := fmt.Sprintf(clusterPlacementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(clusterPlacementPolicyNameTemplate, utils.RandStr())
		// The primary snapshot (sub-index 0) and two secondary snapshots (sub-indices 1 and 2) of the same index.
		snapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 0, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 0, 2),
		}
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		// The work objects (and the snapshots they are derived from), ordered by snapshot sub-index.
		workNames := make([]string, len(snapshotNames))
		snapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(snapshotNames))

		// Each snapshot (and consequently each work object) features its own set of differently named NS,
		// Deployment, and ConfigMap objects.
		nsNames := make([]string, len(snapshotNames))
		deployNames := make([]string, len(snapshotNames))
		configMapNames := make([]string, len(snapshotNames))
		manifestIdentifiersPerWork := make([][]placementv1alpha1.ManifestIdentifier, len(snapshotNames))
		for subIdx := range snapshotNames {
			nsNames[subIdx] = fmt.Sprintf(indexedNSNameTemplate, subIdx)
			deployNames[subIdx] = fmt.Sprintf(deployNameTemplate, subIdx)
			configMapNames[subIdx] = fmt.Sprintf(configMapNameTemplate, subIdx)
			manifestIdentifiersPerWork[subIdx] = []placementv1alpha1.ManifestIdentifier{
				{
					Ordinal:    0,
					Name:       nsNames[subIdx],
					APIVersion: "v1",
					Kind:       nsKind,
					Resource:   nsResource,
				},
				{
					Ordinal:    1,
					Namespace:  nsNames[subIdx],
					Name:       deployNames[subIdx],
					APIGroup:   appsAPIGroup,
					APIVersion: "v1",
					Kind:       deployKind,
					Resource:   deployResource,
				},
				{
					Ordinal:    2,
					Namespace:  nsNames[subIdx],
					Name:       configMapNames[subIdx],
					APIVersion: "v1",
					Kind:       configMapKind,
					Resource:   configMapResource,
				},
			}
		}

		BeforeAll(func() {
			resourcesPerSnapshot := make([][]placementv1alpha1.SnapshottedResource, len(snapshotNames))
			for subIdx := range snapshotNames {
				// Prepare a NS object.
				regularNS := ns.DeepCopy()
				regularNS.Name = nsNames[subIdx]
				regularNSJSON := marshalK8sObjJSON(regularNS)

				// Prepare a Deployment object.
				regularDeploy := deploy.DeepCopy()
				regularDeploy.Namespace = nsNames[subIdx]
				regularDeploy.Name = deployNames[subIdx]
				regularDeployJSON := marshalK8sObjJSON(regularDeploy)

				// Prepare a ConfigMap object.
				regularConfigMap := configMap.DeepCopy()
				regularConfigMap.Namespace = nsNames[subIdx]
				regularConfigMap.Name = configMapNames[subIdx]
				regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

				resourcesPerSnapshot[subIdx] = []placementv1alpha1.SnapshottedResource{
					{
						Identifier: placementv1alpha1.ObjectReference{
							Name:       nsNames[subIdx],
							APIVersion: "v1",
							Kind:       nsKind,
						},
						Manifest: runtime.RawExtension{Raw: regularNSJSON},
					},
					{
						Identifier: placementv1alpha1.ObjectReference{
							Namespace:  nsNames[subIdx],
							Name:       deployNames[subIdx],
							APIGroup:   appsAPIGroup,
							APIVersion: "v1",
							Kind:       deployKind,
						},
						Manifest: runtime.RawExtension{Raw: regularDeployJSON},
					},
					{
						Identifier: placementv1alpha1.ObjectReference{
							Namespace:  nsNames[subIdx],
							Name:       configMapNames[subIdx],
							APIVersion: "v1",
							Kind:       configMapKind,
						},
						Manifest: runtime.RawExtension{Raw: regularConfigMapJSON},
					},
				}
			}

			// Create the placement resource snapshots, with a different set of resources in each snapshot. The
			// secondary snapshots are created first, as the placement resource snapshot manager would do.
			for subIdx := len(snapshotNames) - 1; subIdx >= 0; subIdx-- {
				snapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(clusterScopedNS, snapshotNames[subIdx], placementPolicyName,
					0, subIdx, len(snapshotNames), resourcesPerSnapshot[subIdx]...)
			}

			// Create a cluster placement binding that binds the snapshots to a member cluster.
			binding := createPlacementBinding(clusterScopedNS, bindingName, placementPolicyName, memberCluster1Name, snapshotNames[0])
			for subIdx := range snapshotNames {
				workNames[subIdx] = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, subIdx == 0,
					&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: strconv.Itoa(subIdx)})
			}
		})

		It("should add cleanup finalizer to the cluster placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(clusterScopedNS, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the cluster placement binding")
		})

		It("should generate the work objects", func() {
			// Retrieve the primary work object; the secondary work objects should be owned by it.
			primaryWork := &placementv1alpha1.Work{}
			Eventually(func() error {
				return hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to retrieve the primary work object")
			ownerRef := &metav1.OwnerReference{
				APIVersion:         placementv1alpha1.GroupVersion.String(),
				Kind:               workKind,
				Name:               primaryWork.Name,
				UID:                primaryWork.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}

			wantWorks := make([]placementv1alpha1.Work, len(snapshotNames))
			for subIdx := range snapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], clusterScopedNS, bindingName, placementPolicyName,
					snapshotNames[0], subIdx, len(snapshotNames), ownerRef, snapshots[subIdx])
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(9, snapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the primary work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workNames[0], manifestIdentifiersPerWork[0])
		})

		It("should not refresh the cluster placement binding status until all work objects have been processed", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(9, snapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The cluster placement binding status has been refreshed prematurely")
		})

		It("can mark the secondary work objects as applied and available", func() {
			for subIdx := 1; subIdx < len(snapshotNames); subIdx++ {
				markWorkAsAppliedAndAvailable(workNS, workNames[subIdx], manifestIdentifiersPerWork[subIdx])
			}
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(9, snapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		AfterAll(func() {
			// Delete the cluster placement binding; the work generator should clean up the primary work object and
			// then remove the cleanup finalizer.
			removePlacementBinding(clusterScopedNS, bindingName)

			primaryWorkRemovedActual := workRemovedActual(workNS, workNames[0])
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			bindingRemovedActual := placementBindingRemovedActual(clusterScopedNS, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement binding")

			// The secondary work objects are owned by the primary work object and normally would be garbage collected
			// by Kubernetes; however, the environment prepared by the envtest package does not run the built-in
			// garbage collector, so the test suite deletes them manually.
			for subIdx := 1; subIdx < len(snapshotNames); subIdx++ {
				secondaryWork := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: workNS,
						Name:      workNames[subIdx],
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, secondaryWork))).To(Succeed(), "Failed to delete the secondary work object")

				secondaryWorkRemovedActual := workRemovedActual(workNS, workNames[subIdx])
				Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")
			}

			for subIdx := range snapshotNames {
				snapshotRemovedActual := placementResourceSnapshotRemovedActual(clusterScopedNS, snapshotNames[subIdx])
				Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement resource snapshot")
			}
		})
	})

	Context("update works", Ordered, func() {
		bindingName := fmt.Sprintf(clusterPlacementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(clusterPlacementPolicyNameTemplate, utils.RandStr())
		// Before the rollout: the primary snapshot (sub-index 0) and two secondary snapshots (sub-indices 1 and 2) of
		// index 0.
		oldSnapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 0, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 0, 2),
		}
		// After the first rollout: the primary snapshot (sub-index 0) and one secondary snapshot (sub-index 1) of
		// index 1.
		newSnapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 1, 1),
		}
		// After the second rollout: the primary snapshot (sub-index 0) and three secondary snapshots (sub-indices 1,
		// 2, and 3) of index 2.
		latestSnapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 2),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 2, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 2, 2),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 2, 3),
		}
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		// The work objects, ordered by the sub-index of the snapshots they are derived from; work objects derived
		// from snapshots of the same sub-index share the same name across snapshot indices.
		workNames := make([]string, len(latestSnapshotNames))
		oldSnapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(oldSnapshotNames))
		newSnapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(newSnapshotNames))
		latestSnapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(latestSnapshotNames))

		// The UIDs of the work objects that should be kept (updated rather than re-created) across the rollout.
		var primaryWorkUID, secondaryWorkUID types.UID
		var ownerRef *metav1.OwnerReference

		// The differently named NS, Deployment, and ConfigMap objects to place, one set per sub-index.
		nsNames := make([]string, len(latestSnapshotNames))
		deployNames := make([]string, len(latestSnapshotNames))
		configMapNames := make([]string, len(latestSnapshotNames))
		for subIdx := range latestSnapshotNames {
			nsNames[subIdx] = fmt.Sprintf(indexedNSNameTemplate, subIdx)
			deployNames[subIdx] = fmt.Sprintf(deployNameTemplate, subIdx)
			configMapNames[subIdx] = fmt.Sprintf(configMapNameTemplate, subIdx)
		}

		// Before the rollouts, each snapshot features its own set of NS, Deployment, and ConfigMap objects.
		oldManifestIdentifiersPerWork := make([][]placementv1alpha1.ManifestIdentifier, len(oldSnapshotNames))
		for subIdx := range oldSnapshotNames {
			oldManifestIdentifiersPerWork[subIdx] = []placementv1alpha1.ManifestIdentifier{
				nsManifestIdentifier(0, nsNames[subIdx]),
				deployManifestIdentifier(1, nsNames[subIdx], deployNames[subIdx]),
				configMapManifestIdentifier(2, nsNames[subIdx], configMapNames[subIdx]),
			}
		}

		// After the first rollout, the snapshot of sub-index 2 is gone; its Deployment and ConfigMap objects are moved to the
		// snapshots of sub-index 0 and 1 respectively, and its NS object is dropped.
		newManifestIdentifiersPerWork := [][]placementv1alpha1.ManifestIdentifier{
			{
				nsManifestIdentifier(0, nsNames[0]),
				deployManifestIdentifier(1, nsNames[0], deployNames[0]),
				configMapManifestIdentifier(2, nsNames[0], configMapNames[0]),
				deployManifestIdentifier(3, nsNames[2], deployNames[2]),
			},
			{
				nsManifestIdentifier(0, nsNames[1]),
				deployManifestIdentifier(1, nsNames[1], deployNames[1]),
				configMapManifestIdentifier(2, nsNames[1], configMapNames[1]),
				configMapManifestIdentifier(3, nsNames[2], configMapNames[2]),
			},
		}

		// After the second rollout, each snapshot again features its own set of NS, Deployment, and ConfigMap
		// objects.
		latestManifestIdentifiersPerWork := make([][]placementv1alpha1.ManifestIdentifier, len(latestSnapshotNames))
		for subIdx := range latestSnapshotNames {
			latestManifestIdentifiersPerWork[subIdx] = []placementv1alpha1.ManifestIdentifier{
				nsManifestIdentifier(0, nsNames[subIdx]),
				deployManifestIdentifier(1, nsNames[subIdx], deployNames[subIdx]),
				configMapManifestIdentifier(2, nsNames[subIdx], configMapNames[subIdx]),
			}
		}

		BeforeAll(func() {
			// Create the placement resource snapshots, with a different set of resources in each snapshot. The
			// secondary snapshots are created first, as the placement resource snapshot manager would do.
			for subIdx := len(oldSnapshotNames) - 1; subIdx >= 0; subIdx-- {
				oldSnapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(clusterScopedNS, oldSnapshotNames[subIdx], placementPolicyName,
					0, subIdx, len(oldSnapshotNames),
					nsSnapshottedResource(nsNames[subIdx]),
					deploySnapshottedResource(nsNames[subIdx], deployNames[subIdx]),
					configMapSnapshottedResource(nsNames[subIdx], configMapNames[subIdx]),
				)
			}

			// Create a cluster placement binding that binds the snapshots to a member cluster.
			binding := createPlacementBinding(clusterScopedNS, bindingName, placementPolicyName, memberCluster1Name, oldSnapshotNames[0])
			for subIdx := range latestSnapshotNames {
				workNames[subIdx] = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, subIdx == 0,
					&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: strconv.Itoa(subIdx)})
			}
		})

		It("should add cleanup finalizer to the cluster placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(clusterScopedNS, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the cluster placement binding")
		})

		It("should generate the work objects", func() {
			// Retrieve the primary work object; the secondary work objects should be owned by it.
			primaryWork := &placementv1alpha1.Work{}
			Eventually(func() error {
				return hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to retrieve the primary work object")
			primaryWorkUID = primaryWork.UID
			ownerRef = &metav1.OwnerReference{
				APIVersion:         placementv1alpha1.GroupVersion.String(),
				Kind:               workKind,
				Name:               primaryWork.Name,
				UID:                primaryWork.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}

			wantWorks := make([]placementv1alpha1.Work, len(oldSnapshotNames))
			for subIdx := range oldSnapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], clusterScopedNS, bindingName, placementPolicyName,
					oldSnapshotNames[0], subIdx, len(oldSnapshotNames), ownerRef, oldSnapshots[subIdx])
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")

			// Record the UID of the secondary work object that should be kept across the rollout.
			secondaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[1]}, secondaryWork)).To(Succeed(), "Failed to retrieve the secondary work object")
			secondaryWorkUID = secondaryWork.UID
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(9, oldSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the work objects as applied and available", func() {
			for subIdx := range oldSnapshotNames {
				markWorkAsAppliedAndAvailable(workNS, workNames[subIdx], oldManifestIdentifiersPerWork[subIdx])
			}
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(9, oldSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can roll out a new set of cluster placement resource snapshots", func() {
			// Create the new placement resource snapshots; the Deployment and ConfigMap objects from the old snapshot
			// of sub-index 2 are moved to the new snapshots of sub-index 0 and 1 respectively.
			newSnapshots[1] = createSubIndexedPlacementResourceSnapshot(clusterScopedNS, newSnapshotNames[1], placementPolicyName,
				1, 1, len(newSnapshotNames),
				nsSnapshottedResource(nsNames[1]),
				deploySnapshottedResource(nsNames[1], deployNames[1]),
				configMapSnapshottedResource(nsNames[1], configMapNames[1]),
				configMapSnapshottedResource(nsNames[2], configMapNames[2]),
			)
			newSnapshots[0] = createSubIndexedPlacementResourceSnapshot(clusterScopedNS, newSnapshotNames[0], placementPolicyName,
				1, 0, len(newSnapshotNames),
				nsSnapshottedResource(nsNames[0]),
				deploySnapshottedResource(nsNames[0], deployNames[0]),
				configMapSnapshottedResource(nsNames[0], configMapNames[0]),
				deploySnapshottedResource(nsNames[2], deployNames[2]),
			)

			// Point the cluster placement binding to the new primary snapshot.
			updatePlacementBindingResourceSnapshot(clusterScopedNS, bindingName, newSnapshotNames[0])
		})

		It("should update the work objects", func() {
			// The work object derived from the snapshot of sub-index 2 should be gone; the other two should be
			// updated in place, with the secondary one still owned by the (same) primary work object.
			wantWorks := make([]placementv1alpha1.Work, len(newSnapshotNames))
			for subIdx := range newSnapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], clusterScopedNS, bindingName, placementPolicyName,
					newSnapshotNames[0], subIdx, len(newSnapshotNames), ownerRef, newSnapshots[subIdx])
			}

			worksUpdatedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")
		})

		It("should delete the work object that is no longer needed", func() {
			workRemovedActual := workRemovedActual(workNS, workNames[2])
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete the work object that is no longer needed")
		})

		It("should update the other work objects in place (not re-create them)", func() {
			primaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)).To(Succeed(), "Failed to retrieve the primary work object")
			Expect(primaryWork.UID).To(Equal(primaryWorkUID), "The primary work object has been re-created")

			secondaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[1]}, secondaryWork)).To(Succeed(), "Failed to retrieve the secondary work object")
			Expect(secondaryWork.UID).To(Equal(secondaryWorkUID), "The secondary work object has been re-created")
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(8, newSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the work objects as applied and available", func() {
			for subIdx := range newSnapshotNames {
				markWorkAsAppliedAndAvailable(workNS, workNames[subIdx], newManifestIdentifiersPerWork[subIdx])
			}
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(8, newSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can roll out another new set of cluster placement resource snapshots", func() {
			// Create the latest placement resource snapshots, with a different set of resources in each snapshot. The
			// secondary snapshots are created first, as the placement resource snapshot manager would do.
			for subIdx := len(latestSnapshotNames) - 1; subIdx >= 0; subIdx-- {
				latestSnapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(clusterScopedNS, latestSnapshotNames[subIdx], placementPolicyName,
					2, subIdx, len(latestSnapshotNames),
					nsSnapshottedResource(nsNames[subIdx]),
					deploySnapshottedResource(nsNames[subIdx], deployNames[subIdx]),
					configMapSnapshottedResource(nsNames[subIdx], configMapNames[subIdx]),
				)
			}

			// Point the cluster placement binding to the latest primary snapshot.
			updatePlacementBindingResourceSnapshot(clusterScopedNS, bindingName, latestSnapshotNames[0])
		})

		It("should update and create the work objects", func() {
			// The work objects derived from the snapshots of sub-index 0 and 1 should be updated; the ones derived from
			// the snapshots of sub-index 2 and 3 should be created, and owned by the (same) primary work object.
			wantWorks := make([]placementv1alpha1.Work, len(latestSnapshotNames))
			for subIdx := range latestSnapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], clusterScopedNS, bindingName, placementPolicyName,
					latestSnapshotNames[0], subIdx, len(latestSnapshotNames), ownerRef, latestSnapshots[subIdx])
			}

			worksUpdatedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update and create the work objects")
		})

		It("should update the existing work objects in place (not re-create them)", func() {
			primaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)).To(Succeed(), "Failed to retrieve the primary work object")
			Expect(primaryWork.UID).To(Equal(primaryWorkUID), "The primary work object has been re-created")

			secondaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[1]}, secondaryWork)).To(Succeed(), "Failed to retrieve the secondary work object")
			Expect(secondaryWork.UID).To(Equal(secondaryWorkUID), "The secondary work object has been re-created")
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(12, latestSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the work objects as applied and available", func() {
			for subIdx := range latestSnapshotNames {
				markWorkAsAppliedAndAvailable(workNS, workNames[subIdx], latestManifestIdentifiersPerWork[subIdx])
			}
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(12, latestSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		AfterAll(func() {
			// Delete the cluster placement binding; the work generator should clean up the primary work object and
			// then remove the cleanup finalizer.
			removePlacementBinding(clusterScopedNS, bindingName)

			primaryWorkRemovedActual := workRemovedActual(workNS, workNames[0])
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			bindingRemovedActual := placementBindingRemovedActual(clusterScopedNS, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement binding")

			// The secondary work objects are owned by the primary work object and normally would be garbage collected
			// by Kubernetes; however, the environment prepared by the envtest package does not run the built-in
			// garbage collector, so the test suite deletes them manually (if they still exist).
			for subIdx := 1; subIdx < len(workNames); subIdx++ {
				secondaryWork := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: workNS,
						Name:      workNames[subIdx],
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, secondaryWork))).To(Succeed(), "Failed to delete the secondary work object")

				secondaryWorkRemovedActual := workRemovedActual(workNS, workNames[subIdx])
				Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")
			}

			allSnapshotNames := append(append(append([]string{}, oldSnapshotNames...), newSnapshotNames...), latestSnapshotNames...)
			for _, snapshotName := range allSnapshotNames {
				snapshotRemovedActual := placementResourceSnapshotRemovedActual(clusterScopedNS, snapshotName)
				Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement resource snapshot")
			}
		})
	})

	Context("handle inconsistent resource snapshots", Ordered, func() {
		bindingName := fmt.Sprintf(clusterPlacementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(clusterPlacementPolicyNameTemplate, utils.RandStr())
		// Index 0: a single primary snapshot.
		snapshotNameIdx0 := fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0)
		// Index 1: a primary snapshot that expects one secondary snapshot.
		snapshotNamesIdx1 := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 1, 1),
		}
		// Index 2: a primary snapshot that expects two secondary snapshots.
		snapshotNamesIdx2 := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 2),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 2, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 2, 2),
		}
		contentsHashIdx1 := "hash-1"
		mismatchedContentsHashIdx1 := "hash-1-mismatched"
		contentsHashIdx2 := "hash-2"
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		// The work objects, ordered by the sub-index of the snapshots they are derived from.
		workNames := make([]string, len(snapshotNamesIdx2))
		var snapshotIdx0 placementv1alpha1.PlacementResourceSnapshotAccessor
		snapshotsIdx2 := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(snapshotNamesIdx2))

		var primaryWorkUID types.UID
		// The expected state of the work objects and the cluster placement binding status before any rollout
		// attempt; neither should change as long as the snapshots in use are inconsistent.
		var wantWorksBeforeRollouts []placementv1alpha1.Work
		var wantStatusBeforeRollouts *placementv1alpha1.PlacementBindingStatus

		// The differently named NS, Deployment, and ConfigMap objects to place, one set per sub-index.
		nsNames := make([]string, len(snapshotNamesIdx2))
		deployNames := make([]string, len(snapshotNamesIdx2))
		configMapNames := make([]string, len(snapshotNamesIdx2))
		manifestIdentifiersPerWork := make([][]placementv1alpha1.ManifestIdentifier, len(snapshotNamesIdx2))
		for subIdx := range snapshotNamesIdx2 {
			nsNames[subIdx] = fmt.Sprintf(indexedNSNameTemplate, subIdx)
			deployNames[subIdx] = fmt.Sprintf(deployNameTemplate, subIdx)
			configMapNames[subIdx] = fmt.Sprintf(configMapNameTemplate, subIdx)
			manifestIdentifiersPerWork[subIdx] = []placementv1alpha1.ManifestIdentifier{
				nsManifestIdentifier(0, nsNames[subIdx]),
				deployManifestIdentifier(1, nsNames[subIdx], deployNames[subIdx]),
				configMapManifestIdentifier(2, nsNames[subIdx], configMapNames[subIdx]),
			}
		}

		bindingAndWorksUnchangedActual := func() error {
			if err := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorksBeforeRollouts)(); err != nil {
				return err
			}
			return placementBindingStatusMatchesActual(clusterScopedNS, bindingName, wantStatusBeforeRollouts)()
		}

		BeforeAll(func() {
			// Create a single placement resource snapshot of index 0.
			snapshotIdx0 = createPlacementResourceSnapshot(clusterScopedNS, snapshotNameIdx0, placementPolicyName, 0,
				nsSnapshottedResource(nsNames[0]),
				deploySnapshottedResource(nsNames[0], deployNames[0]),
				configMapSnapshottedResource(nsNames[0], configMapNames[0]),
			)

			// Create a cluster placement binding that binds the snapshot to a member cluster.
			binding := createPlacementBinding(clusterScopedNS, bindingName, placementPolicyName, memberCluster1Name, snapshotNameIdx0)
			for subIdx := range snapshotNamesIdx2 {
				workNames[subIdx] = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, subIdx == 0,
					&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: strconv.Itoa(subIdx)})
			}
		})

		It("should add cleanup finalizer to the cluster placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(clusterScopedNS, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the cluster placement binding")
		})

		It("should generate the work objects", func() {
			wantWorksBeforeRollouts = []placementv1alpha1.Work{
				wantWorkForPrimaryPlacementResourceSnapshot(workNS, workNames[0], clusterScopedNS, bindingName, placementPolicyName, snapshotIdx0),
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorksBeforeRollouts)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")

			// Record the UID of the primary work object, which should be kept across rollouts.
			primaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)).To(Succeed(), "Failed to retrieve the primary work object")
			primaryWorkUID = primaryWork.UID
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(3, snapshotNameIdx0)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the work object as applied and available", func() {
			markWorkAsAppliedAndAvailable(workNS, workNames[0], manifestIdentifiersPerWork[0])
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(3, snapshotNameIdx0)

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")

			// Record the status (with the current observed generation), which should be kept as long as the
			// snapshots in use are inconsistent.
			binding, err := getPlacementBinding(clusterScopedNS, bindingName)
			Expect(err).To(Succeed(), "Failed to retrieve the cluster placement binding")
			wantStatusBeforeRollouts = wantStatus.DeepCopy()
			for idx := range wantStatusBeforeRollouts.Conditions {
				wantStatusBeforeRollouts.Conditions[idx].ObservedGeneration = binding.GetGeneration()
			}
		})

		It("can roll out a new primary snapshot (index 1) without its secondary snapshot", func() {
			// The new primary snapshot features new ConfigMap data, and expects one secondary snapshot, which is
			// absent for now.
			createSubIndexedPlacementResourceSnapshotWithHash(clusterScopedNS, snapshotNamesIdx1[0], placementPolicyName,
				1, 0, len(snapshotNamesIdx1), contentsHashIdx1,
				nsSnapshottedResource(nsNames[0]),
				deploySnapshottedResource(nsNames[0], deployNames[0]),
				configMapSnapshottedResourceWithData(nsNames[0], configMapNames[0], map[string]string{configMapDataKey: altConfigMapDataValue1}),
			)

			// Point the cluster placement binding to the new primary snapshot.
			updatePlacementBindingResourceSnapshot(clusterScopedNS, bindingName, snapshotNamesIdx1[0])
		})

		It("should not update the cluster placement binding status or the work objects", func() {
			Consistently(bindingAndWorksUnchangedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The cluster placement binding status or the work objects have been updated unexpectedly")
		})

		It("can add a secondary snapshot (index 1) with a mismatched contents hash", func() {
			createSubIndexedPlacementResourceSnapshotWithHash(clusterScopedNS, snapshotNamesIdx1[1], placementPolicyName,
				1, 1, len(snapshotNamesIdx1), mismatchedContentsHashIdx1,
				nsSnapshottedResource(nsNames[1]),
				deploySnapshottedResource(nsNames[1], deployNames[1]),
				configMapSnapshottedResource(nsNames[1], configMapNames[1]),
			)
		})

		It("should not update the cluster placement binding status or the work objects", func() {
			Consistently(bindingAndWorksUnchangedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The cluster placement binding status or the work objects have been updated unexpectedly")
		})

		It("can roll out a new primary snapshot (index 2) without its secondary snapshots", func() {
			// The new primary snapshot features new ConfigMap data, and expects two secondary snapshots, which are
			// absent for now.
			snapshotsIdx2[0] = createSubIndexedPlacementResourceSnapshotWithHash(clusterScopedNS, snapshotNamesIdx2[0], placementPolicyName,
				2, 0, len(snapshotNamesIdx2), contentsHashIdx2,
				nsSnapshottedResource(nsNames[0]),
				deploySnapshottedResource(nsNames[0], deployNames[0]),
				configMapSnapshottedResourceWithData(nsNames[0], configMapNames[0], map[string]string{configMapDataKey: altConfigMapDataValue2}),
			)

			// Point the cluster placement binding to the new primary snapshot.
			updatePlacementBindingResourceSnapshot(clusterScopedNS, bindingName, snapshotNamesIdx2[0])
		})

		It("should not update the cluster placement binding status or the work objects", func() {
			Consistently(bindingAndWorksUnchangedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The cluster placement binding status or the work objects have been updated unexpectedly")
		})

		It("can add the missing secondary snapshots (index 2) with the expected contents hash", func() {
			for subIdx := len(snapshotNamesIdx2) - 1; subIdx >= 1; subIdx-- {
				snapshotsIdx2[subIdx] = createSubIndexedPlacementResourceSnapshotWithHash(clusterScopedNS, snapshotNamesIdx2[subIdx], placementPolicyName,
					2, subIdx, len(snapshotNamesIdx2), contentsHashIdx2,
					nsSnapshottedResource(nsNames[subIdx]),
					deploySnapshottedResource(nsNames[subIdx], deployNames[subIdx]),
					configMapSnapshottedResource(nsNames[subIdx], configMapNames[subIdx]),
				)
			}
		})

		It("should update and create the work objects", func() {
			ownerRef := &metav1.OwnerReference{
				APIVersion:         placementv1alpha1.GroupVersion.String(),
				Kind:               workKind,
				Name:               workNames[0],
				UID:                primaryWorkUID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}
			wantWorks := make([]placementv1alpha1.Work, len(snapshotNamesIdx2))
			for subIdx := range snapshotNamesIdx2 {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], clusterScopedNS, bindingName, placementPolicyName,
					snapshotNamesIdx2[0], subIdx, len(snapshotNamesIdx2), ownerRef, snapshotsIdx2[subIdx])
			}

			// The work generator does not watch placement resource snapshots; it picks up the newly added secondary
			// snapshots only when it retries the failed reconciliation, which is subject to exponential backoff.
			worksUpdatedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksUpdatedActual, eventuallyDurationForBackoffRetries, eventuallyInterval).Should(Succeed(), "Failed to update and create the work objects")
		})

		It("should update the primary work object in place (not re-create it)", func() {
			primaryWork := &placementv1alpha1.Work{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)).To(Succeed(), "Failed to retrieve the primary work object")
			Expect(primaryWork.UID).To(Equal(primaryWorkUID), "The primary work object has been re-created")
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(9, snapshotNamesIdx2[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the work objects as applied and available", func() {
			for subIdx := range snapshotNamesIdx2 {
				markWorkAsAppliedAndAvailable(workNS, workNames[subIdx], manifestIdentifiersPerWork[subIdx])
			}
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(9, snapshotNamesIdx2[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		AfterAll(func() {
			// Delete the cluster placement binding; the work generator should clean up the primary work object and
			// then remove the cleanup finalizer.
			removePlacementBinding(clusterScopedNS, bindingName)

			primaryWorkRemovedActual := workRemovedActual(workNS, workNames[0])
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			bindingRemovedActual := placementBindingRemovedActual(clusterScopedNS, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement binding")

			// The secondary work objects are owned by the primary work object and normally would be garbage collected
			// by Kubernetes; however, the environment prepared by the envtest package does not run the built-in
			// garbage collector, so the test suite deletes them manually (if they still exist).
			for subIdx := 1; subIdx < len(workNames); subIdx++ {
				secondaryWork := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: workNS,
						Name:      workNames[subIdx],
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, secondaryWork))).To(Succeed(), "Failed to delete the secondary work object")

				secondaryWorkRemovedActual := workRemovedActual(workNS, workNames[subIdx])
				Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")
			}

			allSnapshotNames := append(append([]string{snapshotNameIdx0}, snapshotNamesIdx1...), snapshotNamesIdx2...)
			for _, snapshotName := range allSnapshotNames {
				snapshotRemovedActual := placementResourceSnapshotRemovedActual(clusterScopedNS, snapshotName)
				Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement resource snapshot")
			}
		})
	})

	Context("update works (no spec changes)", Ordered, func() {
		bindingName := fmt.Sprintf(clusterPlacementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(clusterPlacementPolicyNameTemplate, utils.RandStr())
		// Before the rollout: the primary snapshot (sub-index 0) and one secondary snapshot (sub-index 1) of index 0.
		oldSnapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 0, 1),
		}
		// After the rollout: the primary snapshot (sub-index 0) and one secondary snapshot (sub-index 1) of index 1,
		// with exactly the same content as their counterparts of index 0.
		newSnapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 1),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 1, 1),
		}
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		// The work objects, ordered by the sub-index of the snapshots they are derived from.
		workNames := make([]string, len(oldSnapshotNames))
		workUIDs := make([]types.UID, len(oldSnapshotNames))
		oldSnapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(oldSnapshotNames))
		newSnapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(newSnapshotNames))
		resourcesPerSnapshot := make([][]placementv1alpha1.SnapshottedResource, len(oldSnapshotNames))
		var ownerRef *metav1.OwnerReference
		var oldBindingGeneration int64

		// Each snapshot features its own set of differently named NS, Deployment, and ConfigMap objects.
		nsNames := make([]string, len(oldSnapshotNames))
		deployNames := make([]string, len(oldSnapshotNames))
		configMapNames := make([]string, len(oldSnapshotNames))
		manifestIdentifiersPerWork := make([][]placementv1alpha1.ManifestIdentifier, len(oldSnapshotNames))
		for subIdx := range oldSnapshotNames {
			nsNames[subIdx] = fmt.Sprintf(indexedNSNameTemplate, subIdx)
			deployNames[subIdx] = fmt.Sprintf(deployNameTemplate, subIdx)
			configMapNames[subIdx] = fmt.Sprintf(configMapNameTemplate, subIdx)
			manifestIdentifiersPerWork[subIdx] = []placementv1alpha1.ManifestIdentifier{
				nsManifestIdentifier(0, nsNames[subIdx]),
				deployManifestIdentifier(1, nsNames[subIdx], deployNames[subIdx]),
				configMapManifestIdentifier(2, nsNames[subIdx], configMapNames[subIdx]),
			}
		}

		BeforeAll(func() {
			for subIdx := range oldSnapshotNames {
				resourcesPerSnapshot[subIdx] = []placementv1alpha1.SnapshottedResource{
					nsSnapshottedResource(nsNames[subIdx]),
					deploySnapshottedResource(nsNames[subIdx], deployNames[subIdx]),
					configMapSnapshottedResource(nsNames[subIdx], configMapNames[subIdx]),
				}
			}

			// Create the placement resource snapshots of index 0. The secondary snapshot is created first, as the
			// placement resource snapshot manager would do.
			for subIdx := len(oldSnapshotNames) - 1; subIdx >= 0; subIdx-- {
				oldSnapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(clusterScopedNS, oldSnapshotNames[subIdx], placementPolicyName,
					0, subIdx, len(oldSnapshotNames), resourcesPerSnapshot[subIdx]...)
			}

			// Create a cluster placement binding that binds the snapshots to a member cluster.
			binding := createPlacementBinding(clusterScopedNS, bindingName, placementPolicyName, memberCluster1Name, oldSnapshotNames[0])
			for subIdx := range oldSnapshotNames {
				workNames[subIdx] = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, subIdx == 0,
					&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: strconv.Itoa(subIdx)})
			}
		})

		It("should add cleanup finalizer to the cluster placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(clusterScopedNS, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the cluster placement binding")
		})

		It("should generate the work objects", func() {
			// Retrieve the primary work object; the secondary work object should be owned by it.
			primaryWork := &placementv1alpha1.Work{}
			Eventually(func() error {
				return hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to retrieve the primary work object")
			ownerRef = &metav1.OwnerReference{
				APIVersion:         placementv1alpha1.GroupVersion.String(),
				Kind:               workKind,
				Name:               primaryWork.Name,
				UID:                primaryWork.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}

			wantWorks := make([]placementv1alpha1.Work, len(oldSnapshotNames))
			for subIdx := range oldSnapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], clusterScopedNS, bindingName, placementPolicyName,
					oldSnapshotNames[0], subIdx, len(oldSnapshotNames), ownerRef, oldSnapshots[subIdx])
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")

			// Record the UIDs of the work objects, which should be kept across the rollout.
			for subIdx := range oldSnapshotNames {
				work := &placementv1alpha1.Work{}
				Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[subIdx]}, work)).To(Succeed(), "Failed to retrieve the work object")
				workUIDs[subIdx] = work.UID
			}
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(6, oldSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark the work objects as applied and available", func() {
			for subIdx := range oldSnapshotNames {
				markWorkAsAppliedAndAvailable(workNS, workNames[subIdx], manifestIdentifiersPerWork[subIdx])
			}
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(6, oldSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can roll out a new set of cluster placement resource snapshots with the same content", func() {
			// Record the current generation of the cluster placement binding.
			binding, err := getPlacementBinding(clusterScopedNS, bindingName)
			Expect(err).To(Succeed(), "Failed to retrieve the cluster placement binding")
			oldBindingGeneration = binding.GetGeneration()

			// Create the new placement resource snapshots with exactly the same resources as their counterparts.
			for subIdx := len(newSnapshotNames) - 1; subIdx >= 0; subIdx-- {
				newSnapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(clusterScopedNS, newSnapshotNames[subIdx], placementPolicyName,
					1, subIdx, len(newSnapshotNames), resourcesPerSnapshot[subIdx]...)
			}

			// Point the cluster placement binding to the new primary snapshot.
			updatePlacementBindingResourceSnapshot(clusterScopedNS, bindingName, newSnapshotNames[0])
		})

		It("should update the work objects", func() {
			// The manifests stay the same; only the link to the primary placement resource snapshot changes.
			wantWorks := make([]placementv1alpha1.Work, len(newSnapshotNames))
			for subIdx := range newSnapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], clusterScopedNS, bindingName, placementPolicyName,
					newSnapshotNames[0], subIdx, len(newSnapshotNames), ownerRef, newSnapshots[subIdx])
			}

			worksUpdatedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the work objects")
		})

		It("should update the work objects in place (not re-create them)", func() {
			for subIdx := range newSnapshotNames {
				work := &placementv1alpha1.Work{}
				Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[subIdx]}, work)).To(Succeed(), "Failed to retrieve the work object")
				Expect(work.UID).To(Equal(workUIDs[subIdx]), "The work object has been re-created")
			}
		})

		It("should refresh the cluster placement binding status without waiting for the work objects to be re-processed", func() {
			// As the work objects have no spec changes, their existing status still applies; the cluster placement
			// binding should be reported as synchronized and available, with the conditions observing the new
			// generation.
			wantStatus := wantPlacementBindingStatusSyncedAndAvailable(6, newSnapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")

			binding, err := getPlacementBinding(clusterScopedNS, bindingName)
			Expect(err).To(Succeed(), "Failed to retrieve the cluster placement binding")
			Expect(binding.GetGeneration()).To(BeNumerically(">", oldBindingGeneration), "The cluster placement binding generation has not been bumped")
		})

		AfterAll(func() {
			// Delete the cluster placement binding; the work generator should clean up the primary work object and
			// then remove the cleanup finalizer.
			removePlacementBinding(clusterScopedNS, bindingName)

			primaryWorkRemovedActual := workRemovedActual(workNS, workNames[0])
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			bindingRemovedActual := placementBindingRemovedActual(clusterScopedNS, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement binding")

			// The secondary work object is owned by the primary work object and normally would be garbage collected
			// by Kubernetes; however, the environment prepared by the envtest package does not run the built-in
			// garbage collector, so the test suite deletes it manually.
			for subIdx := 1; subIdx < len(workNames); subIdx++ {
				secondaryWork := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: workNS,
						Name:      workNames[subIdx],
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, secondaryWork))).To(Succeed(), "Failed to delete the secondary work object")

				secondaryWorkRemovedActual := workRemovedActual(workNS, workNames[subIdx])
				Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")
			}

			allSnapshotNames := append(append([]string{}, oldSnapshotNames...), newSnapshotNames...)
			for _, snapshotName := range allSnapshotNames {
				snapshotRemovedActual := placementResourceSnapshotRemovedActual(clusterScopedNS, snapshotName)
				Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement resource snapshot")
			}
		})
	})

	Context("backport status for failed manifests", Ordered, func() {
		bindingName := fmt.Sprintf(clusterPlacementBindingNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(clusterPlacementPolicyNameTemplate, utils.RandStr())
		// The primary snapshot (sub-index 0) and one secondary snapshot (sub-index 1) of index 0.
		snapshotNames := []string{
			fmt.Sprintf(placementResourceSnapshotNameTemplate, placementPolicyName, 0),
			fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, placementPolicyName, 0, 1),
		}
		workNS := fmt.Sprintf(utils.NamespaceNameFormat, memberCluster1Name)

		// The work objects (and the snapshots they are derived from), ordered by snapshot sub-index.
		workNames := make([]string, len(snapshotNames))
		snapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(snapshotNames))

		// Each snapshot features its own set of differently named NS, Deployment, and ConfigMap objects.
		nsNames := make([]string, len(snapshotNames))
		deployNames := make([]string, len(snapshotNames))
		configMapNames := make([]string, len(snapshotNames))
		manifestIdentifiersPerWork := make([][]placementv1alpha1.ManifestIdentifier, len(snapshotNames))
		for subIdx := range snapshotNames {
			nsNames[subIdx] = fmt.Sprintf(indexedNSNameTemplate, subIdx)
			deployNames[subIdx] = fmt.Sprintf(deployNameTemplate, subIdx)
			configMapNames[subIdx] = fmt.Sprintf(configMapNameTemplate, subIdx)
			manifestIdentifiersPerWork[subIdx] = []placementv1alpha1.ManifestIdentifier{
				nsManifestIdentifier(0, nsNames[subIdx]),
				deployManifestIdentifier(1, nsNames[subIdx], deployNames[subIdx]),
				configMapManifestIdentifier(2, nsNames[subIdx], configMapNames[subIdx]),
			}
		}

		// The diff details reported for the ConfigMap object in the primary work object, as if the member agent has
		// failed to take over an existing ConfigMap object with different data in the member cluster.
		//
		// Note that the timestamps are truncated to the second, as this is the precision kept by the API server.
		configMapDiffDetails := &placementv1alpha1.DiffDetails{
			ObservedInMemberClusterGeneration: ptr.To(int64(0)),
			FirstDiffedObservedTimestamp:      metav1.NewTime(time.Now().Truncate(time.Second)),
			ObservedDiffs: []placementv1alpha1.PatchDetail{
				{
					Path:          "/data/" + configMapDataKey,
					ValueInMember: altConfigMapDataValue1,
					ValueInHub:    configMapDataValue,
				},
			},
		}
		// The diff details reported for the Deployment object in the secondary work object, as if the member agent
		// has failed to take over an existing Deployment object with a different replica count in the member cluster.
		deployDiffDetails := &placementv1alpha1.DiffDetails{
			ObservedInMemberClusterGeneration: ptr.To(int64(2)),
			FirstDiffedObservedTimestamp:      metav1.NewTime(time.Now().Truncate(time.Second)),
			ObservedDiffs: []placementv1alpha1.PatchDetail{
				{
					Path:          "/spec/replicas",
					ValueInMember: "2",
					ValueInHub:    "1",
				},
			},
		}

		BeforeAll(func() {
			// Create the placement resource snapshots, with a different set of resources in each snapshot. The
			// secondary snapshot is created first, as the placement resource snapshot manager would do.
			for subIdx := len(snapshotNames) - 1; subIdx >= 0; subIdx-- {
				snapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(clusterScopedNS, snapshotNames[subIdx], placementPolicyName,
					0, subIdx, len(snapshotNames),
					nsSnapshottedResource(nsNames[subIdx]),
					deploySnapshottedResource(nsNames[subIdx], deployNames[subIdx]),
					configMapSnapshottedResource(nsNames[subIdx], configMapNames[subIdx]),
				)
			}

			// Create a cluster placement binding that binds the snapshots to a member cluster.
			binding := createPlacementBinding(clusterScopedNS, bindingName, placementPolicyName, memberCluster1Name, snapshotNames[0])
			for subIdx := range snapshotNames {
				workNames[subIdx] = uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, subIdx == 0,
					&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: strconv.Itoa(subIdx)})
			}
		})

		It("should add cleanup finalizer to the cluster placement binding", func() {
			finalizerAddedActual := placementBindingFinalizerAddedActual(clusterScopedNS, bindingName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the cluster placement binding")
		})

		It("should generate the work objects", func() {
			// Retrieve the primary work object; the secondary work object should be owned by it.
			primaryWork := &placementv1alpha1.Work{}
			Eventually(func() error {
				return hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workNames[0]}, primaryWork)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to retrieve the primary work object")
			ownerRef := &metav1.OwnerReference{
				APIVersion:         placementv1alpha1.GroupVersion.String(),
				Kind:               workKind,
				Name:               primaryWork.Name,
				UID:                primaryWork.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}

			wantWorks := make([]placementv1alpha1.Work, len(snapshotNames))
			for subIdx := range snapshotNames {
				wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(workNS, workNames[subIdx], clusterScopedNS, bindingName, placementPolicyName,
					snapshotNames[0], subIdx, len(snapshotNames), ownerRef, snapshots[subIdx])
			}

			worksGeneratedActual := worksGeneratedActual(memberCluster1Name, clusterScopedNS, bindingName, wantWorks)
			Eventually(worksGeneratedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
		})

		It("should refresh the cluster placement binding status", func() {
			wantStatus := wantPlacementBindingStatusWaitingForSync(6, snapshotNames[0])

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		It("can mark some manifests in the primary work object as failed", func() {
			// The Deployment object fails to become available; the ConfigMap object fails to be applied (with diff
			// details).
			markWorkManifestsAsFailed(workNS, workNames[0], manifestIdentifiersPerWork[0],
				map[int]*placementv1alpha1.DiffDetails{2: configMapDiffDetails},
				sets.New(1),
			)
		})

		It("can mark some manifests in the secondary work object as failed", func() {
			// The Deployment object fails to be applied (with diff details); the ConfigMap object fails to become
			// available.
			markWorkManifestsAsFailed(workNS, workNames[1], manifestIdentifiersPerWork[1],
				map[int]*placementv1alpha1.DiffDetails{1: deployDiffDetails},
				sets.New(2),
			)
		})

		It("should refresh the cluster placement binding status with the failure info from all work objects", func() {
			wantStatus := &placementv1alpha1.PlacementBindingStatus{
				Conditions: []metav1.Condition{
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeSynchronized,
						Status: metav1.ConditionFalse,
						Reason: placementv1alpha1.PlacementBindingSynchronizedCondReasonFailedToSynchronizeSomeResources,
					},
					{
						Type:   placementv1alpha1.PlacementBindingCondTypeAvailable,
						Status: metav1.ConditionFalse,
						Reason: placementv1alpha1.PlacementBindingAvailableCondReasonSomeResourcesUnavailable,
					},
				},
				// In each work object, the NS object is applied and available; the Deployment and ConfigMap objects
				// have failed in different ways.
				SelectedResources:     ptr.To(int32(6)),
				SynchronizedResources: ptr.To(int32(4)),
				AvailableResources:    ptr.To(int32(2)),
				FailedResources: []placementv1alpha1.FailedResource{
					{
						ObjectRef: objectRefFromManifestIdentifier(manifestIdentifiersPerWork[0][1]),
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeAvailable,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsUnavailableReason,
							},
						},
					},
					{
						ObjectRef: objectRefFromManifestIdentifier(manifestIdentifiersPerWork[0][2]),
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeApplied,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsFailedToApplyReason,
							},
						},
						DiffDetails: configMapDiffDetails,
					},
					{
						ObjectRef: objectRefFromManifestIdentifier(manifestIdentifiersPerWork[1][1]),
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeApplied,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsFailedToApplyReason,
							},
						},
						DiffDetails: deployDiffDetails,
					},
					{
						ObjectRef: objectRefFromManifestIdentifier(manifestIdentifiersPerWork[1][2]),
						Conditions: []metav1.Condition{
							{
								Type:               placementv1alpha1.ManifestCondTypeAvailable,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             markedAsUnavailableReason,
							},
						},
					},
				},
				LastProcessedResourceSnapshotName: ptr.To(snapshotNames[0]),
			}

			statusUpdatedActual := placementBindingStatusUpdatedActual(clusterScopedNS, bindingName, wantStatus)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the cluster placement binding status")
		})

		AfterAll(func() {
			// Delete the cluster placement binding; the work generator should clean up the primary work object and
			// then remove the cleanup finalizer.
			removePlacementBinding(clusterScopedNS, bindingName)

			primaryWorkRemovedActual := workRemovedActual(workNS, workNames[0])
			Eventually(primaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the primary work object")

			bindingRemovedActual := placementBindingRemovedActual(clusterScopedNS, bindingName)
			Eventually(bindingRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement binding")

			// The secondary work object is owned by the primary work object and normally would be garbage collected
			// by Kubernetes; however, the environment prepared by the envtest package does not run the built-in
			// garbage collector, so the test suite deletes it manually.
			for subIdx := 1; subIdx < len(workNames); subIdx++ {
				secondaryWork := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: workNS,
						Name:      workNames[subIdx],
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, secondaryWork))).To(Succeed(), "Failed to delete the secondary work object")

				secondaryWorkRemovedActual := workRemovedActual(workNS, workNames[subIdx])
				Eventually(secondaryWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the secondary work object")
			}

			for _, snapshotName := range snapshotNames {
				snapshotRemovedActual := placementResourceSnapshotRemovedActual(clusterScopedNS, snapshotName)
				Eventually(snapshotRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cluster placement resource snapshot")
			}
		})
	})
})
