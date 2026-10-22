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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

// The test cases below use the work applier dedicated to the requeue and status update test cases
// (workApplierInstrumented), whose clients record the start of each reconciliation and the status updates on
// work/appliedWork objects, and whose requeue rate limiter uses short delays (see instrumentedRequeueRateLimiter).
//
// Note that the test cases must not let the work applier take over objects, as the takeover process might retrieve
// work objects, which would be mistaken for the start of reconciliations.
var _ = Describe("requeues and status updates", func() {
	Context("status updates skipped when nothing changes", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		workKey := types.NamespacedName{Namespace: memberReservedNSNameInstrumented, Name: workName}
		appliedWorkKey := types.NamespacedName{Name: workName}

		BeforeAll(func() {
			// Prepare the manifests.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			regularConfigMap := configMap.DeepCopy()
			regularConfigMap.Namespace = nsName
			regularConfigMapJSON := marshalK8sObjJSON(regularConfigMap)

			createWorkObject(workName, memberReservedNSNameInstrumented, nil, regularNSJSON, regularConfigMapJSON)
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

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSNameInstrumented, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		It("should update the appliedWork object status", func() {
			appliedNS := &corev1.Namespace{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Name: nsName}, appliedNS)).To(Succeed(), "Failed to retrieve the namespace")
			appliedConfigMap := &corev1.ConfigMap{}
			Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, appliedConfigMap)).To(Succeed(), "Failed to retrieve the config map")

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
					UID: appliedNS.UID,
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
					UID: appliedConfigMap.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("should have updated the work object status exactly twice and the appliedWork object status exactly once", func() {
			// The work object status is updated once to write ahead the manifests to process, and once to report the
			// results; the appliedWork object status is updated once to report the applied resources.
			Expect(hubClientRecorderInstrumented.statusUpdateCount(workKey)).To(Equal(2), "The work object status is not updated exactly twice")
			Expect(memberClientRecorderInstrumented.statusUpdateCount(appliedWorkKey)).To(Equal(1), "The appliedWork object status is not updated exactly once")
		})

		It("should skip the status updates in later reconciliations", func() {
			reconcileCount := len(hubClientRecorderInstrumented.reconcileStartTimestamps(workKey))
			Eventually(func() int {
				return len(hubClientRecorderInstrumented.reconcileStartTimestamps(workKey))
			}, 2*eventuallyDuration, eventuallyInterval).Should(BeNumerically(">=", reconcileCount+2), "The work object has not been reconciled at least twice more")

			Expect(hubClientRecorderInstrumented.statusUpdateCount(workKey)).To(Equal(2), "The work object status has been updated again with no change")
			Expect(memberClientRecorderInstrumented.statusUpdateCount(appliedWorkKey)).To(Equal(1), "The appliedWork object status has been updated again with no change")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSNameInstrumented)

			// Remove the config map manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSNameInstrumented, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("status updates skipped when nothing changes (linked work objects)", Ordered, func() {
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
		workNames := []string{primaryWorkName, secondaryWorkName}
		configMapNames := map[string]string{
			primaryWorkName:   primaryConfigMapName,
			secondaryWorkName: secondaryConfigMapName,
		}

		BeforeAll(func() {
			// Create the namespace directly in the member cluster, as the work objects do not feature the
			// namespace manifest.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare the manifests.
			configMapJSONs := map[string][]byte{}
			for _, workName := range workNames {
				regularConfigMap := configMap.DeepCopy()
				regularConfigMap.Namespace = nsName
				regularConfigMap.Name = configMapNames[workName]
				configMapJSONs[workName] = marshalK8sObjJSON(regularConfigMap)
			}

			// Create the secondary work object first, so that the work applier can find all the linked work
			// objects as soon as it starts processing the primary work object.
			secondaryWork := buildWorkObject(
				secondaryWorkName, memberReservedNSNameInstrumented,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				configMapJSONs[secondaryWorkName],
			)
			setPrimaryWorkAsOwner(secondaryWork, primaryWorkName)
			Expect(hubClient.Create(ctx, secondaryWork)).To(Succeed(), "Failed to create the secondary work object")

			primaryWork := buildWorkObject(
				primaryWorkName, memberReservedNSNameInstrumented,
				placementPolicyName, placementBindingName, primarySnapshotName,
				nil,
				configMapJSONs[primaryWorkName],
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
			for _, workName := range workNames {
				manifestStatuses := []placementv1alpha1.PerManifestStatus{
					{
						Identifier: placementv1alpha1.ManifestIdentifier{
							Ordinal:    0,
							APIGroup:   "",
							APIVersion: "v1",
							Kind:       "ConfigMap",
							Resource:   "configmaps",
							Name:       configMapNames[workName],
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

				workStatusUpdatedActual := workStatusUpdated(memberReservedNSNameInstrumented, workName, workConds, manifestStatuses)
				Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of work %s", workName)
			}
		})

		It("should update the appliedWork object statuses", func() {
			// Each appliedWork object reports the resources applied from its own work object.
			for _, workName := range workNames {
				appliedConfigMap := &corev1.ConfigMap{}
				Expect(memberClient1.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapNames[workName]}, appliedConfigMap)).To(Succeed(), "Failed to retrieve the config map %s", configMapNames[workName])

				appliedResources := []placementv1alpha1.AppliedResource{
					{
						AppliedResourceIdentifier: placementv1alpha1.AppliedResourceIdentifier{
							Ordinal:    0,
							APIGroup:   "",
							APIVersion: "v1",
							Kind:       "ConfigMap",
							Resource:   "configmaps",
							Name:       configMapNames[workName],
							Namespace:  nsName,
						},
						UID: appliedConfigMap.UID,
					},
				}
				appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient1, workName, appliedResources)
				Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the status of the appliedWork object %s", workName)
			}
		})

		It("should have updated each work object status exactly twice and each appliedWork object status exactly once", func() {
			for _, workName := range workNames {
				Expect(hubClientRecorderInstrumented.statusUpdateCount(types.NamespacedName{Namespace: memberReservedNSNameInstrumented, Name: workName})).To(Equal(2), "The status of work %s is not updated exactly twice", workName)
				Expect(memberClientRecorderInstrumented.statusUpdateCount(types.NamespacedName{Name: workName})).To(Equal(1), "The status of the appliedWork object %s is not updated exactly once", workName)
			}
		})

		It("should skip the status updates in later reconciliations", func() {
			// Only the primary work object is reconciled; the secondary work object is processed along with it.
			primaryWorkKey := types.NamespacedName{Namespace: memberReservedNSNameInstrumented, Name: primaryWorkName}
			reconcileCount := len(hubClientRecorderInstrumented.reconcileStartTimestamps(primaryWorkKey))
			Eventually(func() int {
				return len(hubClientRecorderInstrumented.reconcileStartTimestamps(primaryWorkKey))
			}, 2*eventuallyDuration, eventuallyInterval).Should(BeNumerically(">=", reconcileCount+2), "The primary work object has not been reconciled at least twice more")

			for _, workName := range workNames {
				Expect(hubClientRecorderInstrumented.statusUpdateCount(types.NamespacedName{Namespace: memberReservedNSNameInstrumented, Name: workName})).To(Equal(2), "The status of work %s has been updated again with no change", workName)
				Expect(memberClientRecorderInstrumented.statusUpdateCount(types.NamespacedName{Name: workName})).To(Equal(1), "The status of the appliedWork object %s has been updated again with no change", workName)
			}
		})

		AfterAll(func() {
			// There is no GC controller in the hub cluster test environment to delete the secondary work object along
			// with the primary one; delete both of them.
			for _, workName := range workNames {
				work := &placementv1alpha1.Work{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workName,
						Namespace: memberReservedNSNameInstrumented,
					},
				}
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, work))).To(Succeed(), "Failed to delete work object %s", workName)
			}

			// Remove the config maps manually, as there is no GC controller in the test environment.
			for _, workName := range workNames {
				regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapNames[workName])
				Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map %s", configMapNames[workName])
			}

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork objects.
			for _, workName := range workNames {
				appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
				Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object %s", workName)

				workRemovedActual := workRemovedActual(memberReservedNSNameInstrumented, workName)
				Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object %s", workName)
			}

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("requeue backoff (manifests not yet available), and reset upon processing result changes", Ordered, Label("slow"), func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		workKey := types.NamespacedName{Namespace: memberReservedNSNameInstrumented, Name: workName}

		// The time when the deployment is marked as available.
		var markedAsAvailableAt time.Time

		BeforeAll(func() {
			// Create the namespace directly in the member cluster, as the work object does not feature the
			// namespace manifest.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			Expect(memberClient1.Create(ctx, regularNS)).To(Succeed(), "Failed to create the namespace")

			// Prepare the manifest.
			regularDeploy := deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			createWorkObject(workName, memberReservedNSNameInstrumented, nil, regularDeployJSON)
		})

		It("should apply the manifest", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSNameInstrumented, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef := prepareAppliedWorkOwnerRef(memberClient1, workName)

			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(memberClient1, nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the deployment object")
		})

		It("should back off slowly, then back off fast", func() {
			// The deployment never becomes available in the test environment unless marked so; the processing result
			// stays the same, and the requeue delays grow as configured in instrumentedRequeueRateLimiter (2s, 2s, 3s,
			// 6s, 8s, 8s, ...).
			//
			// The gaps are counted from the second reconciliation of the work object, i.e., the fixed delay is not
			// checked here (it is checked after the processing result changes). The gap after the first reconciliation
			// also covers the reconciliation itself, which creates the appliedWork object and applies the manifest; it
			// might take seconds when the test environment has just started.
			wantGaps := []time.Duration{
				time.Second * 2,
				time.Second * 3,
				time.Second * 6,
				time.Second * 8,
				time.Second * 8,
			}
			requeueGapsActual := requeueGapsActual(hubClientRecorderInstrumented, workKey, time.Time{}, 1, wantGaps)
			Eventually(requeueGapsActual, 6*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to requeue the work object with backoff")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(memberClient1, nsName, deployName)
			markedAsAvailableAt = time.Now()
		})

		It("should requeue with the fixed delay again, as the processing result changes", func() {
			// The change in the member cluster triggers no event for the work object; the next reconciliation (up to
			// 8 seconds later) finds the deployment available, which resets the requeue counter. Without the reset,
			// the work object would have been requeued after 8 seconds again.
			requeueGapsActual := requeueGapsActual(hubClientRecorderInstrumented, workKey, markedAsAvailableAt, 0, []time.Duration{time.Second * 2})
			Eventually(requeueGapsActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to reset the requeue counter")
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

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSNameInstrumented, workName, workConds, manifestStatuses)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSNameInstrumented)

			// Remove the deployment manually, as there is no GC controller in the test environment.
			regularDeployRemovedActual := regularDeployRemovedActual(memberClient1, nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the deployment object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSNameInstrumented, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})

	Context("requeue backoff (manifests available), and reset upon work object changes", Ordered, Label("slow"), func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())
		workKey := types.NamespacedName{Namespace: memberReservedNSNameInstrumented, Name: workName}

		// The time when the work object is updated.
		var nudgedAt time.Time

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

			createWorkObject(workName, memberReservedNSNameInstrumented, nil, regularConfigMapJSON)
		})

		It("should apply the manifest", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient1, memberReservedNSNameInstrumented, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an appliedWork object")
			appliedWorkOwnerRef := prepareAppliedWorkOwnerRef(memberClient1, workName)

			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient1, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the config map object")
		})

		It("should skip to fast backoff", func() {
			// The config map is available right away; as all the applied manifests are available, the requeue rate
			// limiter skips the slow backoff stage (2s, 2s, 4s, 8s, 8s, ...).
			//
			// The gaps are counted from the second reconciliation of the work object, for the same reason as with
			// the not yet available manifests.
			wantGaps := []time.Duration{
				time.Second * 2,
				time.Second * 4,
				time.Second * 8,
				time.Second * 8,
			}
			requeueGapsActual := requeueGapsActual(hubClientRecorderInstrumented, workKey, time.Time{}, 1, wantGaps)
			Eventually(requeueGapsActual, 6*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to requeue the work object with backoff")
		})

		It("can update the work object", func() {
			nudgedAt = time.Now()
			nudgeWork(workName, memberReservedNSNameInstrumented)
		})

		It("should requeue after 2 seconds again, as the work object changes", func() {
			// The update triggers a reconciliation right away, which finds the work object changed and resets the
			// requeue counter; the work object is then requeued with the fixed delay (2s), and then with the initial
			// slow backoff delay (also 2s). Without the reset, the work object would have been requeued after 8
			// seconds again.
			//
			// A periodic requeue might start right after nudgedAt is recorded, yet still read the previous generation
			// of the work object from the cache; the reconciliation triggered by the update would then follow it
			// immediately. To stay robust, the gap is checked after skipping the first reconciliation at or after
			// nudgedAt; it is 2 seconds either way.
			requeueGapsActual := requeueGapsActual(hubClientRecorderInstrumented, workKey, nudgedAt, 1, []time.Duration{time.Second * 2})
			Eventually(requeueGapsActual, 2*eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to reset the requeue counter")
		})

		It("should update the work object status", func() {
			// The work object conditions observe the latest generation of the work object.
			workProcessedAtLatestGenerationActual := workProcessedAtLatestGenerationActual(memberReservedNSNameInstrumented, workName)
			Eventually(workProcessedAtLatestGenerationActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to process the latest generation of the work object")
		})

		AfterAll(func() {
			// Delete the work object and related resources.
			deleteWorkObject(workName, memberReservedNSNameInstrumented)

			// Remove the config map manually, as there is no GC controller in the test environment.
			regularConfigMapRemovedActual := regularConfigMapRemovedActual(memberClient1, nsName, configMapName)
			Eventually(regularConfigMapRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the config map object")

			// Simulate the GC controller by removing the foreground deletion finalizer from the appliedWork object.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient1, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the appliedWork object")

			workRemovedActual := workRemovedActual(memberReservedNSNameInstrumented, workName)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")

			// The namespace is left behind, as the environment prepared by the envtest package does not support
			// namespace deletion.
		})
	})
})
