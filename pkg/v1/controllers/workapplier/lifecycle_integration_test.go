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

var _ = Describe("lifecycle", func() {
	Context("owner namespaces of placement bindings (same placement binding name)", Ordered, func() {
		// Work A is the only work object of a cluster-scoped placement binding; work B (primary) and work B2
		// (secondary) are the work objects of a namespace-scoped placement binding of the same name. All the work
		// objects are linked to a primary placement resource snapshot of the same name, so that the linked work
		// counts would not match should the work applier mix the two placement bindings up.
		workAName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		workBName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		workB2Name := fmt.Sprintf(workNameTemplate, utils.RandStr())
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		placementBindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())
		primarySnapshotName := fmt.Sprintf(primarySnapshotNameTemplate, utils.RandStr())
		// The namespace of the namespace-scoped placement policy/binding; it only appears in the labels.
		ownerNSName := fmt.Sprintf("owner-ns-%s", utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		configMapAName := "configmap-a"
		configMapBName := "configmap-b"
		configMapB2Name := "configmap-b2"

		var appliedWorkOwnerRefA *metav1.OwnerReference
		var appliedWorkOwnerRefB *metav1.OwnerReference
		// The appliedWork object of work B2 before work A is deleted.
		var appliedWorkB2 *placementv1alpha1.AppliedWork

		// Each work object features one config map, which is applied and available.
		singleConfigMapStatus := func(configMapName string) []placementv1alpha1.PerManifestStatus {
			return []placementv1alpha1.PerManifestStatus{
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
		}

		BeforeAll(func() {
			// Create the namespace directly in the member cluster, as the work objects do not feature the
			// namespace manifest.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare the manifests.
			configMapJSONs := map[string][]byte{}
			for _, name := range []string{configMapAName, configMapBName, configMapB2Name} {
				regularConfigMap := configMap.DeepCopy()
				regularConfigMap.Namespace = nsName
				regularConfigMap.Name = name
				configMapJSONs[name] = marshalK8sObjJSON(regularConfigMap)
			}

			// Create the work objects of the namespace-scoped placement binding; the secondary work object goes
			// first, so that the work applier can find all the linked work objects as soon as it starts processing
			// the primary work object.
			workB2 := buildWorkObject(
				workB2Name, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				configMapJSONs[configMapB2Name],
			)
			workB2.Labels[placementv1alpha1.WorkOwnerNamespaceLabelKey] = ownerNSName
			setPrimaryWorkAsOwner(workB2, workBName)
			Expect(hubClient.Create(ctx, workB2)).To(Succeed(), "Failed to create work B2")

			workB := buildWorkObject(
				workBName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				configMapJSONs[configMapBName],
			)
			workB.Labels[placementv1alpha1.WorkOwnerNamespaceLabelKey] = ownerNSName
			workB.Annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = "2"
			Expect(hubClient.Create(ctx, workB)).To(Succeed(), "Failed to create work B")

			// Create the work object of the cluster-scoped placement binding (an empty owner namespace).
			workA := buildWorkObject(
				workAName, memberReservedNSName1,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				configMapJSONs[configMapAName],
			)
			workA.Annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = singleWorkLinkedWorkCountAnno
			Expect(hubClient.Create(ctx, workA)).To(Succeed(), "Failed to create work A")
		})

		It("should process the work objects of each placement binding on their own", func() {
			for _, workName := range []string{workAName, workBName, workB2Name} {
				finalizerAddedActual := workFinalizerAddedActual(memberReservedNSName1, workName)
				Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to work %s", workName)

				appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
				Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object for work %s", workName)
			}
			appliedWorkOwnerRefA = prepareAppliedWorkOwnerRef(memberClient1, workAName)
			appliedWorkOwnerRefB = prepareAppliedWorkOwnerRef(memberClient1, workBName)
			appliedWorkB2 = &placementv1alpha1.AppliedWork{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: workB2Name}, appliedWorkB2)).To(Succeed(), "Failed to retrieve the appliedWork object of work B2")

			// The config map from work A is owned by the appliedWork object of work A; the config maps from work B
			// and work B2 are both owned by the appliedWork object of work B (the primary work object).
			configMapAAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapAName, appliedWorkOwnerRefA)
			Eventually(configMapAAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map from work A")

			for _, name := range []string{configMapBName, configMapB2Name} {
				configMapAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, name, appliedWorkOwnerRefB)
				Eventually(configMapAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map %s", name)
			}
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
			for workName, configMapName := range map[string]string{
				workAName:  configMapAName,
				workBName:  configMapBName,
				workB2Name: configMapB2Name,
			} {
				workStatusUpdatedActual := workStatusUpdated(memberReservedNSName1, workName, workConds, singleConfigMapStatus(configMapName))
				Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of work %s", workName)
			}
		})

		It("can delete work A", func() {
			deleteWorkObject(workAName, memberReservedNSName1)
		})

		It("should remove work A", func() {
			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workAName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object of work A")

			workRemovedActual := workRemovedActual(memberReservedNSName1, workAName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work A")
		})

		It("should leave the work objects of the namespace-scoped placement binding alone", func() {
			// The cleanup of work A concerns only the work objects of the cluster-scoped placement binding; the
			// secondary work object of the namespace-scoped placement binding keeps its cleanup finalizer and
			// appliedWork object, and the applied config maps stay with the appliedWork object of work B.
			//
			// Had the cleanup touched work B2, the next periodic reconciliation of work B would add the finalizer
			// back and re-create the appliedWork object; the appliedWork object is hence checked by its UID.
			finalizerAddedActual := workFinalizerAddedActual(memberReservedNSName1, workB2Name)
			appliedWorkNotRecreatedActual := objectNotRecreatedActual(memberClient1, appliedWorkB2)
			configMapBAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapBName, appliedWorkOwnerRefB)
			configMapB2AppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapB2Name, appliedWorkOwnerRefB)
			Consistently(func() error {
				if err := finalizerAddedActual(); err != nil {
					return err
				}
				if err := appliedWorkNotRecreatedActual(); err != nil {
					return err
				}
				if err := configMapBAppliedActual(); err != nil {
					return err
				}
				return configMapB2AppliedActual()
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The work objects of the namespace-scoped placement binding have been changed")
		})

		AfterAll(func() {
			// Delete all the work objects (work A might still exist if the test case fails before it is deleted);
			// there is no GC controller in the hub cluster test environment to delete secondary work objects along
			// with their primary work objects.
			for _, workName := range []string{workAName, workBName, workB2Name} {
				work := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workName,
						Namespace: memberReservedNSName1,
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, work))).To(Succeed(), "Failed to delete work %s", workName)
			}

			// Remove the config maps manually, as there is no GC controller in the test environment.
			for _, name := range []string{configMapAName, configMapBName, configMapB2Name} {
				regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, name)
				Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map %s", name)
			}

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork objects.
			for _, workName := range []string{workAName, workBName, workB2Name} {
				appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
				Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object of work %s", workName)

				workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
				Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work %s", workName)
			}

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("leaving and re-joining the fleet", Ordered, func() {
		// The test case uses the work applier dedicated to the lifecycle test cases (which watches its own reserved
		// namespace), so that leaving the fleet never affects other test cases; no Serial decorator is needed.
		//
		// Work A uses the default sync strategy, while work B always takes over pre-existing objects.
		workAName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		workBName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		configMapAName := "configmap-a"
		configMapBName := "configmap-b"

		var configMapA *corev1.ConfigMap
		var updatedConfigMapA *corev1.ConfigMap
		var updatedConfigMapB *corev1.ConfigMap
		var syncStrategyB *placementv1alpha1.SyncStrategy
		// The config maps as applied in the member cluster before the member cluster leaves the fleet.
		var appliedConfigMapA *corev1.ConfigMap
		var appliedConfigMapB *corev1.ConfigMap
		// The appliedWork owner references before the member cluster leaves the fleet.
		var oldAppliedWorkOwnerRefA *metav1.OwnerReference
		var oldAppliedWorkOwnerRefB *metav1.OwnerReference

		BeforeAll(func() {
			// Re-join the fleet when the test case completes (or fails), so that the dedicated work applier keeps
			// processing work objects for the lifecycle test cases that run afterwards on the same Ginkgo process.
			DeferCleanup(func() {
				Expect(workApplierLifecycle.Join(ctx)).To(Succeed(), "Failed to re-join the fleet")
			})

			// Create the namespace directly in the member cluster, as the work objects do not feature the
			// namespace manifest.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare the manifests, and their updated versions.
			configMapA = configMap.DeepCopy()
			configMapA.Namespace = nsName
			configMapA.Name = configMapAName
			updatedConfigMapA = configMapA.DeepCopy()
			updatedConfigMapA.Data = map[string]string{
				configMapDataKey: "updated",
			}

			configMapB := configMap.DeepCopy()
			configMapB.Namespace = nsName
			configMapB.Name = configMapBName
			updatedConfigMapB = configMapB.DeepCopy()
			updatedConfigMapB.Data = map[string]string{
				configMapDataKey: "updated",
			}

			createWorkObject(workAName, memberReservedNSNameLifecycle, nil, marshalK8sObjJSON(configMapA))

			syncStrategyB = &placementv1alpha1.SyncStrategy{
				WhenAlreadyExists: placementv1alpha1.WhenAlreadyExistsOptionAlwaysTakeOver,
			}
			createWorkObject(workBName, memberReservedNSNameLifecycle, syncStrategyB, marshalK8sObjJSON(configMapB))
		})

		It("should apply the manifests", func() {
			for _, workName := range []string{workAName, workBName} {
				appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSNameLifecycle, workName)
				Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object for work %s", workName)
			}
			oldAppliedWorkOwnerRefA = prepareAppliedWorkOwnerRef(memberClient1, workAName)
			oldAppliedWorkOwnerRefB = prepareAppliedWorkOwnerRef(memberClient1, workBName)

			configMapAAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapAName, oldAppliedWorkOwnerRefA)
			Eventually(configMapAAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map from work A")
			configMapBAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapBName, oldAppliedWorkOwnerRefB)
			Eventually(configMapBAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map from work B")

			appliedConfigMapA = &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapAName}, appliedConfigMapA)).To(Succeed(), "Failed to retrieve the config map from work A")
			appliedConfigMapB = &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapBName}, appliedConfigMapB)).To(Succeed(), "Failed to retrieve the config map from work B")
		})

		It("can leave the fleet", func() {
			// Retry on errors, as the work applier might update the work objects at the same time.
			Eventually(func() error {
				return workApplierLifecycle.Leave(ctx)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to leave the fleet")
		})

		It("can leave the fleet again", func() {
			Expect(workApplierLifecycle.Leave(ctx)).To(Succeed(), "Failed to leave the fleet again")
		})

		It("should remove the cleanup finalizer from the work objects", func() {
			for _, workName := range []string{workAName, workBName} {
				finalizerRemovedActual := workFinalizerRemovedActual(memberReservedNSNameLifecycle, workName)
				Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the cleanup finalizer from work %s", workName)
			}
		})

		It("should delete the appliedWork objects with the orphan propagation policy", func() {
			for _, workName := range []string{workAName, workBName} {
				appliedWorkBeingDeletedWithOrphanActual := appliedWorkBeingDeletedWithOrphanActual(memberClient1, workName)
				Eventually(appliedWorkBeingDeletedWithOrphanActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete the appliedWork object of work %s with the orphan propagation policy", workName)
			}
		})

		It("can update the work objects", func() {
			updateWorkObject(workAName, memberReservedNSNameLifecycle, nil, marshalK8sObjJSON(updatedConfigMapA))
			updateWorkObject(workBName, memberReservedNSNameLifecycle, syncStrategyB, marshalK8sObjJSON(updatedConfigMapB))
		})

		It("should not process the work objects while away from the fleet", func() {
			finalizerARemovedActual := workFinalizerRemovedActual(memberReservedNSNameLifecycle, workAName)
			finalizerBRemovedActual := workFinalizerRemovedActual(memberReservedNSNameLifecycle, workBName)
			configMapAStableActual := configMapStableActual(memberClient1, nsName, configMapAName, appliedConfigMapA.UID, appliedConfigMapA.Data)
			configMapBStableActual := configMapStableActual(memberClient1, nsName, configMapBName, appliedConfigMapB.UID, appliedConfigMapB.Data)
			Consistently(func() error {
				for _, actual := range []func() error{finalizerARemovedActual, finalizerBRemovedActual, configMapAStableActual, configMapBStableActual} {
					if err := actual(); err != nil {
						return err
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The work objects have been processed while away from the fleet")
		})

		It("can simulate the GC controller", func() {
			// The GC controller removes the owner references to the appliedWork objects from the orphaned objects,
			// and then removes the orphan finalizer from the appliedWork objects.
			for _, name := range []string{configMapAName, configMapBName} {
				updateConfigMapInMemberCluster(memberClient1, nsName, name, func(configMap *corev1.ConfigMap) {
					configMap.OwnerReferences = nil
				})
			}
			for _, workName := range []string{workAName, workBName} {
				appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
				Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object of work %s", workName)
			}
		})

		It("can re-join the fleet", func() {
			Expect(workApplierLifecycle.Join(ctx)).To(Succeed(), "Failed to re-join the fleet")
		})

		It("should add the cleanup finalizer back and prepare new appliedWork objects", func() {
			for _, workName := range []string{workAName, workBName} {
				finalizerAddedActual := workFinalizerAddedActual(memberReservedNSNameLifecycle, workName)
				Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add the cleanup finalizer back to work %s", workName)

				appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSNameLifecycle, workName)
				Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare a new appliedWork object for work %s", workName)
			}

			Expect(prepareAppliedWorkOwnerRef(memberClient1, workAName).UID).NotTo(Equal(oldAppliedWorkOwnerRefA.UID), "The appliedWork object of work A has not been re-created")
			Expect(prepareAppliedWorkOwnerRef(memberClient1, workBName).UID).NotTo(Equal(oldAppliedWorkOwnerRefB.UID), "The appliedWork object of work B has not been re-created")
		})

		It("should not take over the orphaned object with the default sync strategy (work A)", func() {
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
						Name:       configMapAName,
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
			workStatusUpdatedActual := workStatusUpdated(memberReservedNSNameLifecycle, workAName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of work A")

			// The config map keeps the data from the original manifest, and has no owner.
			wantConfigMap := configMapA.DeepCopy()
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Consistently(configMapObjectActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The orphaned config map from work A has been changed")
		})

		It("should take over the orphaned object and apply the updated manifest (work B)", func() {
			newAppliedWorkOwnerRefB := prepareAppliedWorkOwnerRef(memberClient1, workBName)
			wantConfigMap := updatedConfigMapB.DeepCopy()
			wantConfigMap.OwnerReferences = []metav1.OwnerReference{*newAppliedWorkOwnerRefB}
			configMapObjectActual := configMapObjectActual(memberClient1, wantConfigMap)
			Eventually(configMapObjectActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to take over the orphaned config map from work B")

			Expect(objectNotRecreatedActual(memberClient1, appliedConfigMapB)()).To(Succeed(), "The config map from work B has been re-created instead of being taken over")
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
						Name:       configMapBName,
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
			workStatusUpdatedActual := workStatusUpdated(memberReservedNSNameLifecycle, workBName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of work B")
		})

		AfterAll(func() {
			// Make sure that the work applier has re-joined the fleet before cleaning up (in case the test case fails
			// while away from the fleet); the cleanup relies on the work applier.
			Expect(workApplierLifecycle.Join(ctx)).To(Succeed(), "Failed to re-join the fleet")

			for _, workName := range []string{workAName, workBName} {
				work := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workName,
						Namespace: memberReservedNSNameLifecycle,
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, work))).To(Succeed(), "Failed to delete work %s", workName)
			}

			// Remove the config maps manually, as there is no GC controller in the test environment.
			for _, name := range []string{configMapAName, configMapBName} {
				regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, name)
				Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map %s", name)
			}

			// Simulate the GC controller by removing the orphan/foreground deletion finalizers from the appliedWork
			// objects.
			for _, workName := range []string{workAName, workBName} {
				appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
				Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object of work %s", workName)

				workRemovedActual := workRemovedActual(memberReservedNSNameLifecycle, workName)
				Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work %s", workName)
			}

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("work objects that are not primary work objects", Ordered, func() {
		// Neither work object features the linked work count annotation, i.e., neither of them is a primary work
		// object. The standalone work object has no owner; the other work object is owned by a primary work object
		// that does not exist, as a secondary work object would be.
		standaloneWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		secondaryWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		nonExistentPrimaryWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The namespace is never created; were the work objects processed, the work applier would create it.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		standaloneConfigMapName := "configmap-standalone"
		secondaryConfigMapName := "configmap-secondary"

		BeforeAll(func() {
			// Prepare the manifests.
			standaloneConfigMap := configMap.DeepCopy()
			standaloneConfigMap.Namespace = nsName
			standaloneConfigMap.Name = standaloneConfigMapName

			secondaryConfigMap := configMap.DeepCopy()
			secondaryConfigMap.Namespace = nsName
			secondaryConfigMap.Name = secondaryConfigMapName

			standaloneWork := buildWorkObject(
				standaloneWorkName, memberReservedNSName1,
				fmt.Sprintf(placementPolicyNameTemplate, standaloneWorkName),
				fmt.Sprintf(placementBindingNameTemplate, standaloneWorkName),
				fmt.Sprintf(primarySnapshotNameTemplate, standaloneWorkName),
				nil,
				marshalK8sObjJSON(standaloneConfigMap),
			)
			Expect(hubClient.Create(ctx, standaloneWork)).To(Succeed(), "Failed to create the standalone work object")

			secondaryWork := buildWorkObject(
				secondaryWorkName, memberReservedNSName1,
				fmt.Sprintf(placementPolicyNameTemplate, nonExistentPrimaryWorkName),
				fmt.Sprintf(placementBindingNameTemplate, nonExistentPrimaryWorkName),
				fmt.Sprintf(primarySnapshotNameTemplate, nonExistentPrimaryWorkName),
				nil,
				marshalK8sObjJSON(secondaryConfigMap),
			)
			setPrimaryWorkAsOwner(secondaryWork, nonExistentPrimaryWorkName)
			Expect(hubClient.Create(ctx, secondaryWork)).To(Succeed(), "Failed to create the secondary work object")
		})

		It("should not process the work objects", func() {
			standaloneWorkNotProcessedActual := workNotProcessedActual(memberClient1, memberReservedNSName1, standaloneWorkName)
			secondaryWorkNotProcessedActual := workNotProcessedActual(memberClient1, memberReservedNSName1, secondaryWorkName)
			standaloneConfigMapAbsentActual := regularConfigMapAbsentActual(memberClient1, nsName, standaloneConfigMapName)
			secondaryConfigMapAbsentActual := regularConfigMapAbsentActual(memberClient1, nsName, secondaryConfigMapName)
			namespaceAbsentActual := namespaceAbsentActual(memberClient1, nsName)
			Consistently(func() error {
				for _, actual := range []func() error{
					standaloneWorkNotProcessedActual, secondaryWorkNotProcessedActual,
					standaloneConfigMapAbsentActual, secondaryConfigMapAbsentActual,
					namespaceAbsentActual,
				} {
					if err := actual(); err != nil {
						return err
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The work objects have been processed")
		})

		It("can delete the work objects", func() {
			deleteWorkObject(standaloneWorkName, memberReservedNSName1)
			deleteWorkObject(secondaryWorkName, memberReservedNSName1)
		})

		It("should remove the work objects", func() {
			// The work objects have no cleanup finalizer; they are removed right away.
			for _, workName := range []string{standaloneWorkName, secondaryWorkName} {
				workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
				Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work %s", workName)
			}
		})

		AfterAll(func() {
			// The work objects might still exist if the test case fails before they are deleted.
			for _, workName := range []string{standaloneWorkName, secondaryWorkName} {
				work := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workName,
						Namespace: memberReservedNSName1,
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, work))).To(Succeed(), "Failed to delete work %s", workName)

				workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
				Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work %s", workName)
			}
		})
	})

	Context("malformed primary work objects", Ordered, func() {
		// Each work object features the linked work count annotation (i.e., it is a primary work object), but misses
		// the information that the work applier needs to find its linked work objects.
		noBindingLabelWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		noSnapshotAnnotationWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		invalidLinkedWorkCountWorkName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		workNames := []string{noBindingLabelWorkName, noSnapshotAnnotationWorkName, invalidLinkedWorkCountWorkName}
		// The namespace is never created; were the work objects processed, the work applier would create it.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		// Each work object features a config map of its own.
		configMapNames := map[string]string{
			noBindingLabelWorkName:         "configmap-no-binding-label",
			noSnapshotAnnotationWorkName:   "configmap-no-snapshot-annotation",
			invalidLinkedWorkCountWorkName: "configmap-invalid-linked-work-count",
		}

		BeforeAll(func() {
			works := make(map[string]*placementv1alpha1.Work, len(workNames))
			for _, workName := range workNames {
				regularConfigMap := configMap.DeepCopy()
				regularConfigMap.Namespace = nsName
				regularConfigMap.Name = configMapNames[workName]

				work := buildWorkObject(
					workName, memberReservedNSName1,
					fmt.Sprintf(placementPolicyNameTemplate, workName),
					fmt.Sprintf(placementBindingNameTemplate, workName),
					fmt.Sprintf(primarySnapshotNameTemplate, workName),
					nil,
					marshalK8sObjJSON(regularConfigMap),
				)
				work.Annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = singleWorkLinkedWorkCountAnno
				works[workName] = work
			}

			delete(works[noBindingLabelWorkName].Labels, placementv1alpha1.WorkOwnedByPlacementBindingLabelKey)
			delete(works[noSnapshotAnnotationWorkName].Annotations, placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey)
			works[invalidLinkedWorkCountWorkName].Annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = "one"

			for _, workName := range workNames {
				Expect(hubClient.Create(ctx, works[workName])).To(Succeed(), "Failed to create work %s", workName)
			}
		})

		It("should not process the work objects", func() {
			actuals := []func() error{namespaceAbsentActual(memberClient1, nsName)}
			for _, workName := range workNames {
				actuals = append(actuals,
					workNotProcessedActual(memberClient1, memberReservedNSName1, workName),
					regularConfigMapAbsentActual(memberClient1, nsName, configMapNames[workName]),
				)
			}
			Consistently(func() error {
				for _, actual := range actuals {
					if err := actual(); err != nil {
						return err
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The malformed work objects have been processed")
		})

		It("can delete the work objects", func() {
			for _, workName := range workNames {
				deleteWorkObject(workName, memberReservedNSName1)
			}
		})

		It("should remove the work objects", func() {
			// The work objects have no cleanup finalizer; they are removed right away.
			for _, workName := range workNames {
				workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
				Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work %s", workName)
			}
		})

		AfterAll(func() {
			// The work objects might still exist if the test case fails before they are deleted.
			for _, workName := range workNames {
				work := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workName,
						Namespace: memberReservedNSName1,
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, work))).To(Succeed(), "Failed to delete work %s", workName)

				workRemovedActual := workRemovedActual(memberReservedNSName1, workName)
				Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work %s", workName)
			}
		})
	})

	Context("appliedWork objects deleted by an external actor", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		// The appliedWork owner reference and the config map before the appliedWork object is deleted.
		var oldAppliedWorkOwnerRef *metav1.OwnerReference
		var oldConfigMap *corev1.ConfigMap
		// The appliedWork owner reference and the config map after the work applier re-creates them.
		var newAppliedWorkOwnerRef *metav1.OwnerReference
		var newConfigMap *corev1.ConfigMap

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

			createWorkObject(workName, memberReservedNSName1, nil, regularConfigMapJSON)
		})

		It("should apply the manifest", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			oldAppliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)

			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, oldAppliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map")

			oldConfigMap = &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, oldConfigMap)).To(Succeed(), "Failed to retrieve the config map")
		})

		It("can delete the appliedWork object, as an external actor would", func() {
			// With the default (background) propagation policy, the appliedWork object is deleted right away.
			appliedWork := &placementv1alpha1.AppliedWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: workName,
				},
			}
			Expect(memberClient1.Delete(ctx, appliedWork)).To(Succeed(), "Failed to delete the appliedWork object")
		})

		It("can simulate the GC controller by deleting the dependents of the appliedWork object", func() {
			// The work applier might re-create the appliedWork object before the config map is deleted, and report
			// the config map, which is still owned by the deleted appliedWork object, as not taken over; the config
			// map is re-created at the next requeue of the work object anyway.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map")
		})

		It("should re-create the appliedWork object", func() {
			// The deletion triggers no event for the work object; the appliedWork object is re-created at the next
			// requeue of the work object.
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSName1, workName)
			Eventually(appliedWorkCreatedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to re-create the appliedWork object")

			newAppliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient1, workName)
			Expect(newAppliedWorkOwnerRef.UID).NotTo(Equal(oldAppliedWorkOwnerRef.UID), "The appliedWork object has not been re-created")
		})

		It("should re-create the config map with the new appliedWork object as its owner", func() {
			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, newAppliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to re-create the config map")

			newConfigMap = &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, newConfigMap)).To(Succeed(), "Failed to retrieve the config map")
			Expect(newConfigMap.UID).NotTo(Equal(oldConfigMap.UID), "The config map has not been re-created")
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

		It("should update the status of the new appliedWork object", func() {
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
					UID: newConfigMap.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of the new appliedWork object")
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
})
